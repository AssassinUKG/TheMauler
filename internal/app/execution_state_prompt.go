package app

import (
	"fmt"
	"strings"

	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/tools"
)

const maxExecutionStateFacts = 8

func (a *App) buildExecutionStatePrompt(firstUserText, toolChoice string, toolDefs []llm.ToolDef, terminalState TerminalStateSnapshot) string {
	if a == nil {
		return ""
	}
	var events []ledger.Event
	if a.ledger != nil {
		events, _ = a.ledger.List(160)
	}
	facts := deriveRunFacts(events, maxExecutionStateFacts)
	sessions := a.ListAgentSessions()
	phase := opsPhaseForTaskWithState(firstUserText, terminalState)
	names := enabledToolNames(toolDefs)
	browserStatus := tools.GetBrowserWorkflowStatus(a.browserWorkflowOwnerID())

	var sb strings.Builder
	sb.WriteString("Current execution state packet (fresh, compact; prefer this over stale chat when routing tools):\n")
	sb.WriteString(fmt.Sprintf("- route: phase=%s tool_choice=%s tool_count=%d tools=%s\n", phase, toolChoice, len(names), strings.Join(names, ",")))
	sb.WriteString("- capability truth: the tools named above are the tools available to this model turn. If the requested action has a matching tool, call it directly; do not claim that tool access is missing, substitute a delegated task, or promise an action without the matching tool call. If no matching tool is named, report the missing capability once without pretending the action is underway.\n")
	if explicitBrowserIntent(firstUserText) && !containsExactString(names, "browser") {
		sb.WriteString("- browser blocker: this request needs interactive browser capability, but browser is not in the routed tools. Do not pretend to navigate or complete the workflow. Tell the user to enable Browser in Inspector > Agent > Tools or switch to the browser/unrestricted toolset, then retry in Project Agent.\n")
	}
	sb.WriteString(activeBrowserExecutionStatePrompt(browserStatus, containsExactString(names, "browser")))
	if terminalState.State != "" {
		sb.WriteString(fmt.Sprintf("- terminal: state=%s session=%s summary=%s\n", terminalState.State, terminalState.Session, truncateRunes(terminalState.Summary, 180)))
	}
	for _, session := range sessions {
		sb.WriteString("- session: ")
		sb.WriteString(formatSessionPromptLine(session))
		sb.WriteString("\n")
	}
	for _, fact := range facts {
		sb.WriteString("- fact: ")
		if fact.Kind != "" {
			sb.WriteString(fact.Kind + "=")
		}
		sb.WriteString(truncateRunes(fact.Text, 180))
		if fact.Source != "" {
			sb.WriteString(" source=" + fact.Source)
		}
		sb.WriteString("\n")
	}
	if engagementPacket := a.buildEngagementPromptPacket(); engagementPacket != "" {
		sb.WriteString(engagementPacket)
	}
	sb.WriteString("- discipline: use the recommended live path. If terminal is connected, use terminal_send/terminal_read only for commands inside that live session; use http_probe or shell for independent HTTP/webshell/curl/wget checks. If terminal is listener, trigger callbacks through http_probe/shell/webshell and watch with terminal_read. If terminal is running/busy, read or recover; do not start another terminal command.\n")
	return sb.String()
}

func containsExactString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func formatSessionPromptLine(session AgentSession) string {
	parts := []string{
		"id=" + session.ID,
		"kind=" + firstNonEmpty(session.Kind, "terminal"),
		"state=" + firstNonEmpty(session.State, "unknown"),
	}
	if session.Port > 0 {
		parts = append(parts, fmt.Sprintf("port=%d", session.Port))
	}
	if session.Lhost != "" {
		parts = append(parts, "lhost="+session.Lhost)
	}
	if session.User != "" {
		parts = append(parts, "user="+session.User)
	}
	if session.Hostname != "" {
		parts = append(parts, "host="+session.Hostname)
	}
	if session.LastEvidence != "" {
		parts = append(parts, "evidence="+truncateRunes(session.LastEvidence, 120))
	}
	return strings.Join(parts, " ")
}
