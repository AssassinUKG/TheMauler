package app

import (
	"fmt"
	"strings"

	"mauler/internal/llm"
)

// validateToolCallBoundary enforces the exact tool set sent for this model turn.
// Provider-side normalisation is useful, but Mauler remains the final authority:
// a model/backend must never be able to execute a tool that was omitted or sent
// under tool_choice=none.
func validateToolCallBoundary(calls []llm.ToolCallDef, defs []llm.ToolDef, choice string) string {
	if len(calls) == 0 {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(choice), "none") || len(defs) == 0 {
		return fmt.Sprintf("backend returned %d tool call(s) on a text-only turn where no tool schemas were available", len(calls))
	}
	seenIDs := map[string]string{}
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			return "backend returned a structured tool call with an empty function name"
		}
		if !toolCallAdvertised(defs, name) {
			return fmt.Sprintf("backend returned unadvertised tool %q; available tools were: %s", name, toolProtocolToolNames(defs))
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			continue
		}
		signature := name + "\x00" + strings.TrimSpace(string(call.Function.Arguments))
		if previous, exists := seenIDs[id]; exists && previous != signature {
			return fmt.Sprintf("backend reused tool_call id %q for conflicting calls", id)
		}
		seenIDs[id] = signature
	}
	return ""
}

func buildToolBoundaryRecoveryPrompt(reason string, defs []llm.ToolDef) string {
	if len(defs) == 0 {
		return "The previous response violated the tool boundary: " + reason + ". No tools are available on this turn. Reply once in plain text only; do not emit function calls, tool markup, or simulated tool results."
	}
	return "The previous response violated the tool boundary: " + reason + ". Retry once using only an advertised tool with its exact schema. Available tools: " + toolProtocolToolNames(defs) + ". Do not simulate a result."
}
