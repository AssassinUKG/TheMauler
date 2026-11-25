package app

import (
	"errors"
	"regexp"
	"strings"
)

type toolExecutionOutcome struct {
	Status       string
	EventKind    string
	EventMessage string
	RunState     string
	ControlErr   error
}

var (
	toolStateMachineDeniedRE = regexp.MustCompile(`(?i)\[tool_state_machine\s+[^\]]*allowed=false[^\]]*\]`)
	shellResultStateRE       = regexp.MustCompile(`(?i)\[shell_result\s+state=([a-z_]+)\b`)
	backgroundJobStateRE     = regexp.MustCompile(`(?i)\[background job\s+[^:]+:\s*(started|running|done|error|timeout|cancelled)\b`)
)

// classifyToolExecutionOutcome turns code-owned result contracts into ledger
// state. A nil Go error only means the tool returned normally; it does not mean
// an action was allowed, finished, or produced completion evidence.
func classifyToolExecutionOutcome(result string, runErr error) toolExecutionOutcome {
	if runErr != nil {
		return toolExecutionOutcome{Status: "error", EventKind: "tool_error", EventMessage: "Recoverable tool failure", RunState: "recovering", ControlErr: runErr}
	}
	if toolStateMachineDeniedRE.MatchString(result) {
		err := errors.New("tool state machine denied execution")
		return toolExecutionOutcome{Status: "blocked", EventKind: "tool_state_machine", EventMessage: "Tool call was not executed", RunState: "recovering", ControlErr: err}
	}
	if strings.Contains(result, "[verifier_required:http_endpoint]") {
		err := errors.New("HTTP endpoint response did not verify the requested action")
		return toolExecutionOutcome{Status: "inconclusive", EventKind: "tool_inconclusive", EventMessage: "HTTP action remained unverified", RunState: "recovering", ControlErr: err}
	}
	if strings.Contains(result, "Started background job ") && strings.Contains(result, "Poll after ") {
		return toolExecutionOutcome{Status: "running"}
	}
	state := ""
	if match := shellResultStateRE.FindStringSubmatch(result); len(match) == 2 {
		state = strings.ToLower(match[1])
	} else if match := backgroundJobStateRE.FindStringSubmatch(result); len(match) == 2 {
		state = strings.ToLower(match[1])
	}
	switch state {
	case "started", "running":
		return toolExecutionOutcome{Status: "running"}
	case "error", "timeout", "cancelled", "canceled":
		err := errors.New("tool result contract reported " + state)
		return toolExecutionOutcome{Status: "error", EventKind: "tool_error", EventMessage: "Tool result contract reported failure", RunState: "recovering", ControlErr: err}
	default:
		return toolExecutionOutcome{Status: "done"}
	}
}
