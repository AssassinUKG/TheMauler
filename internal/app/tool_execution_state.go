package app

import (
	"fmt"
	"strings"
)

type toolExecutionDecision struct {
	Allowed         bool
	State           string
	RecommendedTool string
	Message         string
}

func adviseTerminalRun(state TerminalStateSnapshot, command string) toolExecutionDecision {
	stateName := strings.TrimSpace(state.State)
	if stateName == "" {
		stateName = "unknown"
	}
	command = strings.TrimSpace(command)
	switch stateName {
	case "missing", "ready", "closed":
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "shared terminal can accept a new command",
		}
	case "running":
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal is already running a command; read progress instead of typing a second command into the same PTY",
		}
	case "listener":
		if looksLikeReverseShellTrigger(command) {
			return toolExecutionDecision{
				State:           stateName,
				RecommendedTool: "shell",
				Message:         "shared terminal appears to own a listener/live session; trigger reverse-shell callbacks through http_probe, shell, or a separate webshell path, then use terminal_read/terminal_send on the listener",
			}
		}
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal appears to own a listener/live session; inspect it with terminal_read or interact with terminal_send instead of typing an unrelated command",
		}
	case "connected":
		if !looksLikeSessionInteractionCommand(command) {
			tool := independentCommandTool(command)
			return toolExecutionDecision{
				State:           stateName,
				RecommendedTool: tool,
				Message:         independentCommandRoutingMessage(stateName, tool),
			}
		}
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "shared terminal appears to contain a connected live shell/session; terminal_send can drive it",
		}
	case "interactive_prompt":
		if !looksLikeSessionInteractionCommand(command) && !isLikelyInteractiveInput(command) {
			tool := independentCommandTool(command)
			return toolExecutionDecision{
				State:           stateName,
				RecommendedTool: tool,
				Message:         independentCommandRoutingMessage(stateName, tool),
			}
		}
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_send",
			Message:         "shared terminal is waiting for input; answer the prompt with terminal_send or recover/restart before launching a new command",
		}
	case "busy":
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal has output but no clean prompt; read once or use Recover before assuming the target failed",
		}
	default:
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal state is unclear; inspect it before running another live command",
		}
	}
}

func adviseStartListener(state TerminalStateSnapshot) toolExecutionDecision {
	stateName := strings.TrimSpace(state.State)
	if stateName == "" {
		stateName = "unknown"
	}
	switch stateName {
	case "missing", "ready", "closed":
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "shared terminal can start a listener",
		}
	case "listener":
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "a listener already appears to be waiting in the shared terminal; do not start a second listener on top of it",
		}
	case "connected":
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_send",
			Message:         "a live shell/session appears connected in the shared terminal; use terminal_send/terminal_read instead of replacing it with a new listener",
		}
	case "running":
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal is already running a command; read progress before starting a listener",
		}
	case "interactive_prompt":
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_send",
			Message:         "shared terminal is waiting for input; answer or recover it before starting a listener",
		}
	case "busy":
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal has output but no clean prompt; inspect or recover it before starting a listener",
		}
	default:
		return toolExecutionDecision{
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal state is unclear; inspect it before starting a listener",
		}
	}
}

func adviseTerminalSend(state TerminalStateSnapshot, args terminalSendArgs) toolExecutionDecision {
	stateName := strings.TrimSpace(state.State)
	if stateName == "" {
		stateName = "unknown"
	}
	keys := strings.TrimSpace(args.Keys)
	control := strings.TrimSpace(strings.ToLower(args.Control))
	isControlOnly := keys == "" && control != ""
	isInterrupt := terminalSendHasInterrupt(args)
	isPureEnter := terminalSendIsPureEnter(args)
	looksCommand := keys != "" && (strings.Contains(keys, " ") || strings.ContainsAny(keys, "/\\|;&>$<") || strings.HasSuffix(keys, ".py") || strings.HasPrefix(keys, "cd "))
	switch stateName {
	case "missing", "closed":
		return toolExecutionDecision{
			Allowed:         false,
			State:           stateName,
			RecommendedTool: "terminal_send",
			Message:         "no live shared terminal is ready for typed input; start a command with terminal_send command=... or use shell for a one-shot",
		}
	case "ready":
		if isControlOnly {
			return toolExecutionDecision{
				Allowed: true,
				State:   stateName,
				Message: "control key can be sent to the ready terminal",
			}
		}
		if looksCommand {
			return toolExecutionDecision{
				Allowed:         false,
				State:           stateName,
				RecommendedTool: "terminal_send",
				Message:         "shared terminal is idle and the input looks like a new command; use terminal_send command=... so the command is tracked as a live tool action",
			}
		}
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "short typed input can be sent to the ready terminal",
		}
	case "listener", "connected", "interactive_prompt":
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "terminal_send is appropriate for this live or prompting terminal state",
		}
	case "running":
		if isInterrupt || control == "d" || control == "z" || isLikelyInteractiveInput(keys) {
			return toolExecutionDecision{
				Allowed: true,
				State:   stateName,
				Message: "control/interactive input is allowed while the terminal is running",
			}
		}
		return toolExecutionDecision{
			Allowed:         false,
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal is running; read progress or send Ctrl-C instead of typing a new command into it",
		}
	case "busy":
		if isInterrupt || control == "d" || isPureEnter {
			return toolExecutionDecision{
				Allowed: true,
				State:   stateName,
				Message: "bounded recovery input is allowed for a busy terminal",
			}
		}
		return toolExecutionDecision{
			Allowed:         false,
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal is busy and has no clean prompt; inspect it or recover before sending input",
		}
	default:
		return toolExecutionDecision{
			Allowed:         false,
			State:           stateName,
			RecommendedTool: "terminal_read",
			Message:         "shared terminal state is unclear; inspect it before sending input",
		}
	}
}

func adviseShellCall(state TerminalStateSnapshot, command string) toolExecutionDecision {
	stateName := strings.TrimSpace(state.State)
	if stateName == "" {
		stateName = "unknown"
	}
	command = strings.TrimSpace(command)
	switch stateName {
	case "listener":
		if looksLikeReverseShellTrigger(command) {
			return toolExecutionDecision{
				Allowed: true,
				State:   stateName,
				Message: "shared terminal is listening; shell is allowed as the separate reverse-shell trigger path",
			}
		}
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "shared terminal is listening; shell is allowed only for separate one-shot checks, not listener interaction",
		}
	case "connected":
		if looksLikeSessionInteractionCommand(command) {
			return toolExecutionDecision{
				State:           stateName,
				RecommendedTool: "terminal_send",
				Message:         "a live shell/session is connected in the shared terminal; send session commands with terminal_send and inspect with terminal_read instead of running them in the local shell",
			}
		}
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "shell is allowed for separate local one-shot checks while the connected session remains in the terminal",
		}
	case "running", "interactive_prompt", "busy":
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "shell is allowed as a separate isolated one-shot path while the shared terminal is not ready",
		}
	default:
		return toolExecutionDecision{
			Allowed: true,
			State:   stateName,
			Message: "shell can run as a deterministic one-shot path",
		}
	}
}

func looksLikeSessionInteractionCommand(command string) bool {
	lower := strings.TrimSpace(strings.ToLower(command))
	if lower == "" || strings.Contains(lower, "curl ") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return false
	}
	sessionCommands := []string{
		"id", "whoami", "pwd", "hostname", "uname -a", "cat user.txt", "cat root.txt",
		"ls", "ls -la", "cd ", "python -c", "python3 -c", "sh -i", "bash -i",
	}
	for _, item := range sessionCommands {
		if lower == item || strings.HasPrefix(lower, item+" ") || strings.HasPrefix(lower, item) {
			return true
		}
	}
	return false
}

func independentCommandTool(command string) string {
	lower := strings.ToLower(strings.TrimSpace(command))
	if strings.Contains(lower, "curl ") || strings.Contains(lower, "wget ") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return "http_probe"
	}
	return "shell"
}

func independentCommandRoutingMessage(stateName, tool string) string {
	var target string
	if tool == "http_probe" {
		target = "http_probe for repeated/base HTTP checks, or shell if exact curl flags/pipelines are required"
	} else {
		target = "shell as a separate one-shot process"
	}
	return fmt.Sprintf("shared terminal is %s; this is an independent local/probe command, not live-session input. Run it with %s. Do not type it into the live session. Operators are auto-unescaped by TheMauler; write &, >, < literally and do not add &amp;/&gt;/&lt; escaping.", stateName, target)
}

func isLikelyInteractiveInput(keys string) bool {
	text := strings.TrimSpace(strings.ToLower(keys))
	if text == "" {
		return false
	}
	if looksLikeSessionInteractionCommand(text) {
		return false
	}
	if len(text) <= 80 && !strings.ContainsAny(text, "|;&<>") {
		return true
	}
	return false
}

func formatToolExecutionBlock(decision toolExecutionDecision, snapshot TerminalStateSnapshot) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[tool_state_machine state=%s allowed=%t", decision.State, decision.Allowed)
	if decision.RecommendedTool != "" {
		fmt.Fprintf(&sb, " recommended=%s", decision.RecommendedTool)
	}
	sb.WriteString("]\n")
	sb.WriteString("contract:\n")
	sb.WriteString("  state: " + firstNonEmpty(decision.State, snapshot.State, "unknown") + "\n")
	if decision.RecommendedTool != "" {
		sb.WriteString("  next_tool: " + decision.RecommendedTool + "\n")
	}
	sb.WriteString("  evidence: " + truncateRunes(firstNonEmpty(snapshot.Summary, decision.Message), 220) + "\n")
	if !decision.Allowed {
		sb.WriteString("  do_not_repeat: do not retry the same blocked tool call until the terminal state changes\n")
	}
	if strings.TrimSpace(snapshot.Summary) != "" {
		sb.WriteString("terminal: " + strings.TrimSpace(snapshot.Summary) + "\n")
	}
	sb.WriteString("decision: " + strings.TrimSpace(decision.Message) + "\n")
	if len(snapshot.Lines) > 0 {
		sb.WriteString("recent terminal:\n")
		for _, line := range cleanTerminalLines(snapshot.Lines) {
			if strings.TrimSpace(line) == "" {
				continue
			}
			sb.WriteString(line + "\n")
		}
	}
	return sb.String()
}
