package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/llm"
)

// enforceAgentModeToolPolicy provides an action-level boundary where a compact
// tool contains both passive and state-changing operations. Toolset membership
// alone cannot safely distinguish browser snapshot from form interaction.
func enforceAgentModeToolPolicy(mode AgentMode, activeToolset string, call llm.ToolCallDef) error {
	if !strings.EqualFold(strings.TrimSpace(mode.Name), "Bug Bounty Hunter") {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(activeToolset), "unrestricted") {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(call.Function.Name), "browser") {
		return nil
	}
	var input struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(call.Function.Arguments, &input); err != nil {
		return fmt.Errorf("Bug Bounty Hunter browser policy could not classify malformed arguments: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(input.Action)) {
	case "open", "snapshot", "extract", "screenshot", "status", "pause", "takeover", "resume", "close":
		return nil
	case "click", "type", "agent":
		return fmt.Errorf("Bug Bounty Hunter is a planning-only agent; browser action %q is blocked by default. Switch to an explicitly authorised active-testing agent or toolset for stateful interaction", input.Action)
	default:
		return fmt.Errorf("Bug Bounty Hunter browser policy does not allow unknown action %q", input.Action)
	}
}
