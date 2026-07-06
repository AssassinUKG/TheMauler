package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

const (
	reasoningEffortToolName = "set_reasoning_effort"
	maxEffortChangesPerRun  = 6
)

type effortPlan struct {
	enableThinking bool
	maxTokensCap   int
	coding         bool
}

func defaultReasoningEffortForMode(mode AgentMode) string {
	switch strings.ToLower(strings.TrimSpace(mode.Name)) {
	case "reviewer", "planner", "researcher":
		return "medium"
	case "fixer", "ops":
		return "high"
	case "builder", "auto", "manual":
		return "medium"
	default:
		return "medium"
	}
}

func normaliseReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}

func configuredReasoningEffort(cfg settings.Settings, mode AgentMode) string {
	if effort := normaliseReasoningEffort(cfg.Agents.ReasoningEffort); effort != "" {
		return effort
	}
	if effort := normaliseReasoningEffort(mode.DefaultEffort); effort != "" {
		return effort
	}
	return defaultReasoningEffortForMode(mode)
}

func effortToThinking(effort string, profile settings.Profile) effortPlan {
	switch normaliseReasoningEffort(effort) {
	case "minimal":
		return effortPlan{enableThinking: false, maxTokensCap: 1024, coding: true}
	case "low":
		return effortPlan{enableThinking: false, maxTokensCap: 0, coding: true}
	case "high":
		return effortPlan{enableThinking: profile.Thinking, maxTokensCap: 0, coding: false}
	default:
		return effortPlan{enableThinking: profile.Thinking, maxTokensCap: 0, coding: false}
	}
}

func reasoningEffortToolDef() llm.ToolDef {
	return llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        reasoningEffortToolName,
			Description: "Set reasoning effort for subsequent turns in this task. Use low/minimal for rote reads, small edits, formatting, or command execution; use high for ambiguous design, debugging, or complex analysis.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "effort": {
      "type": "string",
      "enum": ["minimal", "low", "medium", "high"],
      "description": "Reasoning depth for subsequent model turns."
    }
  },
  "required": ["effort"],
  "additionalProperties": false
}`),
		},
	}
}

type reasoningEffortArgs struct {
	Effort string `json:"effort"`
}

func isReasoningEffortTool(name string) bool {
	return name == reasoningEffortToolName
}

func applyReasoningEffortTool(current *string, changes *int, raw json.RawMessage) (string, bool) {
	if current == nil || changes == nil {
		return `{"ok":false,"error":"reasoning effort state unavailable"}`, false
	}
	if *changes >= maxEffortChangesPerRun {
		return fmt.Sprintf(`{"ok":false,"error":"reasoning effort change cap reached","cap":%d,"effort":%q}`, maxEffortChangesPerRun, *current), false
	}
	var args reasoningEffortArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return fmt.Sprintf(`{"ok":false,"error":"invalid JSON: %s"}`, jsonEscape(err.Error())), false
	}
	effort := normaliseReasoningEffort(args.Effort)
	if effort == "" {
		return `{"ok":false,"error":"effort must be one of minimal, low, medium, high"}`, false
	}
	*current = effort
	*changes++
	return fmt.Sprintf(`{"ok":true,"effort":%q}`, effort), true
}

func jsonEscape(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return strings.Trim(string(data), `"`)
}

func appendReasoningEffortToolDef(defs []llm.ToolDef) []llm.ToolDef {
	for _, def := range defs {
		if def.Function.Name == reasoningEffortToolName {
			return defs
		}
	}
	return append(defs, reasoningEffortToolDef())
}

func countBudgetedToolCalls(calls []llm.ToolCallDef) int {
	n := 0
	for _, call := range calls {
		if !isReasoningEffortTool(call.Function.Name) {
			n++
		}
	}
	return n
}
