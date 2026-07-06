package app

import (
	"fmt"
	"strings"

	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

const opsToolBudgetSoftCap = 20

func opsPhaseForTask(firstUserText string) string {
	return opsPhaseFromText(strings.ToLower(strings.TrimSpace(firstUserText)))
}

func opsPhaseForTaskWithState(firstUserText string, state TerminalStateSnapshot) string {
	stateName := strings.TrimSpace(state.State)
	switch stateName {
	case "connected", "listener", "running", "busy", "interactive_prompt":
		return "live_terminal"
	default:
		return opsPhaseForTask(firstUserText)
	}
}

func toolDefsAndChoiceForTurnWithState(registry *tools.Registry, cfg settings.ToolsConfig, firstUserText string, autoContinues int, totalToolCallsMade int, state TerminalStateSnapshot) ([]llm.ToolDef, string) {
	toolChoice := toolChoiceFor(firstUserText, autoContinues, totalToolCallsMade)
	if !cfg.Enabled || toolChoice == "none" {
		return nil, toolChoice
	}
	enabled := settings.EffectiveEnabledTools(cfg)
	if shouldHideHostResearchTools(cfg, firstUserText) {
		enabled = cloneToolEnabledMap(enabled)
		for _, name := range []string{"web_search", "fetch_url", "browser"} {
			enabled[name] = false
		}
		enabled["task"] = false
	}
	selected := selectToolsForTurnWithState(cfg, firstUserText, autoContinues, totalToolCallsMade, state)
	defs := registry.ToEnabledToolDefsFor(enabled, selected)
	if enabled[reasoningEffortToolName] && selected[reasoningEffortToolName] {
		defs = appendReasoningEffortToolDef(defs)
	}
	defs = trimOpsToolDefsIfNeededWithState(defs, cfg, firstUserText, state)
	if len(defs) == 0 && toolChoice == "required" {
		toolChoice = "none"
	}
	return defs, toolChoice
}

func trimOpsToolDefsIfNeeded(defs []llm.ToolDef, cfg settings.ToolsConfig, firstUserText string) []llm.ToolDef {
	return trimOpsToolDefsIfNeededWithState(defs, cfg, firstUserText, TerminalStateSnapshot{})
}

func trimOpsToolDefsIfNeededWithState(defs []llm.ToolDef, cfg settings.ToolsConfig, firstUserText string, state TerminalStateSnapshot) []llm.ToolDef {
	if len(defs) <= opsToolBudgetSoftCap {
		return defs
	}
	lower := strings.ToLower(strings.TrimSpace(firstUserText))
	stateName := strings.TrimSpace(state.State)
	liveTerminal := stateName == "connected" || stateName == "listener" || stateName == "running" || stateName == "busy" || stateName == "interactive_prompt"
	if !liveTerminal && !(needsOperationalTool(lower) || looksShellCentricTask(lower)) {
		return defs
	}
	if explicitBrowserIntent(lower) || explicitWebResearchIntent(lower) {
		return defs
	}
	allowed := selectToolsForTurnWithState(cfg, firstUserText, 0, 0, state)
	out := make([]llm.ToolDef, 0, len(defs))
	for _, def := range defs {
		if allowed[def.Function.Name] {
			out = append(out, def)
		}
	}
	if len(out) == 0 || len(out) >= len(defs) {
		return defs
	}
	return out
}

func (a *App) recordToolRoutingState(runID, firstUserText, toolChoice string, toolDefs []llm.ToolDef, autoContinues, totalToolCallsMade int, terminalState TerminalStateSnapshot) bool {
	if a == nil {
		return false
	}
	phase := opsPhaseForTaskWithState(firstUserText, terminalState)
	names := enabledToolNames(toolDefs)
	if len(names) > opsToolBudgetSoftCap && (needsOperationalTool(strings.ToLower(firstUserText)) || looksShellCentricTask(strings.ToLower(firstUserText))) {
		a.recordLedger(ledger.Event{
			RunID:   runID,
			Kind:    "tool_routing_warning",
			Source:  "tool_router",
			Status:  "over_budget",
			State:   phase,
			Message: fmt.Sprintf("Ops routed %d tools over budget %d", len(names), opsToolBudgetSoftCap),
			Detail:  strings.Join(names, ", "),
			Metadata: map[string]string{
				"phase":       phase,
				"tool_count":  fmt.Sprintf("%d", len(names)),
				"tool_budget": fmt.Sprintf("%d", opsToolBudgetSoftCap),
			},
		})
	}
	key := fmt.Sprintf("%s|%s|%s|%s|%d|%s", runID, phase, toolChoice, terminalState.State, len(names), strings.Join(names, ","))
	a.sessionMu.Lock()
	if a.lastOpsPhaseKey == key {
		a.sessionMu.Unlock()
		return false
	}
	a.lastOpsPhaseKey = key
	a.sessionMu.Unlock()
	a.recordLedger(ledger.Event{
		RunID:   runID,
		Kind:    "tool_routing",
		Source:  "tool_router",
		Status:  phase,
		State:   phase,
		Message: fmt.Sprintf("phase=%s choice=%s tools=%d", phase, toolChoice, len(names)),
		Detail:  strings.Join(names, ", "),
		Metadata: map[string]string{
			"phase":          phase,
			"tool_choice":    toolChoice,
			"tool_count":     fmt.Sprintf("%d", len(names)),
			"auto_turns":     fmt.Sprintf("%d", autoContinues),
			"tool_calls":     fmt.Sprintf("%d", totalToolCallsMade),
			"terminal_state": terminalState.State,
		},
	})
	return true
}
