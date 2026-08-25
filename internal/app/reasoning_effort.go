package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

func (a *App) thinkingSiblingProfile(profile settings.Profile) (settings.Profile, bool) {
	if profile.Thinking || a == nil {
		return settings.Profile{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.profiles == nil {
		return settings.Profile{}, false
	}
	var fallback settings.Profile
	for name, candidate := range a.profiles.Profiles {
		if !candidate.Thinking || candidate.ModelID != profile.ModelID || candidate.Provider != profile.Provider {
			continue
		}
		candidate.Name = firstNonEmpty(candidate.Name, name)
		if strings.Contains(strings.ToLower(name), "think") && !strings.Contains(strings.ToLower(name), "nothink") {
			return candidate, true
		}
		fallback = candidate
	}
	return fallback, fallback.ModelID != ""
}

func (a *App) runHighEffortPlanningPass(ctx context.Context, client llm.Client, profile settings.Profile, prompt string) (string, settings.Profile, error) {
	sibling, ok := a.thinkingSiblingProfile(profile)
	if !ok || strings.TrimSpace(prompt) == "" {
		return "", settings.Profile{}, nil
	}
	a.mu.Lock()
	msgs := a.history.Messages()
	a.mu.Unlock()
	msgs = append(msgs, llm.NewTextMessage(llm.RoleSystem, "Perform a short private planning pass for the task. Analyze risks, dependencies, and the next few concrete steps. Do not call tools, do not claim work is complete, and keep the visible plan under 1200 words."))
	req := buildChatRequest(sibling, msgs, nil, "none", false, false, "high")
	if req.MaxTokens <= 0 || req.MaxTokens > 2048 {
		req.MaxTokens = 2048
	}
	req.SpecType, req.SpecDraftNMax = "", 0
	ch, err := client.Chat(ctx, req)
	if err != nil {
		return "", sibling, err
	}
	var visible, thinking strings.Builder
	for delta := range ch {
		if delta.Error != nil {
			return "", sibling, delta.Error
		}
		visible.WriteString(delta.Content)
		thinking.WriteString(delta.Thinking)
	}
	plan := strings.TrimSpace(visible.String())
	if plan == "" {
		plan = strings.TrimSpace(thinking.String())
	}
	return truncateRunes(plan, 6000), sibling, nil
}

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
	case "reviewer", "planner":
		return "xhigh"
	case "fixer":
		return "high"
	case "researcher", "ops", "builder", "auto", "manual":
		return "medium"
	default:
		return "medium"
	}
}

func normaliseReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "none", "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}

func isQwen38Profile(profile settings.Profile) bool {
	rp, ok := runtimeprofile.Match(profile)
	return ok && strings.EqualFold(rp.Family, "qwen3.8")
}

func normaliseThinkingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on", "off":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "auto"
	}
}

// applyThinkingMode resolves the Chat-level override without mutating the saved
// profile. "on" is honoured only when the code-owned runtime profile says the
// model supports thinking (or an unknown profile already opted into thinking).
func applyThinkingMode(profile settings.Profile, mode string) (settings.Profile, string, bool) {
	mode = normaliseThinkingMode(mode)
	switch mode {
	case "off":
		profile.Thinking = false
		profile.PreserveThink = false
		return profile, "off", true
	case "on":
		supported := profile.Thinking
		if rp, ok := runtimeprofile.Match(profile); ok {
			supported = rp.Supports.Thinking
		}
		if !supported {
			return profile, "unsupported", false
		}
		profile.Thinking = true
		profile.PreserveThink = true
		return profile, "on", true
	default:
		return profile, "auto", true
	}
}

func effectiveEffortForThinkingMode(effort, mode string, profile settings.Profile) string {
	mode = normaliseThinkingMode(mode)
	normalized := normaliseReasoningEffort(effort)
	if normalized == "" {
		normalized = "medium"
	}
	if mode == "on" && !effortToThinking(normalized, profile).enableThinking {
		return "medium"
	}
	return normalized
}

func shouldForceNoThinking(profile settings.Profile, mode string, totalToolCalls, threshold, noToolContinues int) bool {
	if normaliseThinkingMode(mode) == "on" || !profile.Thinking {
		return false
	}
	if threshold <= 0 {
		threshold = 2
	}
	return totalToolCalls >= threshold || noToolContinues > 0
}

func thinkingModePrompt(mode string) string {
	switch normaliseThinkingMode(mode) {
	case "on":
		return "Chat thinking override: ON for supported models. Keep new model thinking enabled on every turn; reasoning effort may change depth but must not switch to none/minimal/direct mode. "
	case "off":
		return "Chat thinking override: OFF. Use the direct/no-thinking sampler for this run and do not attempt to enable model thinking. "
	default:
		// Profile/auto is the long-standing default. Keep the canonical prompt packet byte-for-byte
		// lean in that mode so this optional UI control cannot displace routed project rules.
		return ""
	}
}

func providerReasoningEffort(effort string, enableThinking bool, profile settings.Profile) string {
	normalized := normaliseReasoningEffort(effort)
	if !enableThinking || normalized == "minimal" || normalized == "none" {
		if isQwen38Profile(profile) {
			return ""
		}
		return "none"
	}
	if isQwen38Profile(profile) {
		switch normalized {
		case "low", "medium", "xhigh":
			return normalized
		case "high":
			return "xhigh"
		default:
			return "medium"
		}
	}
	return normalized
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
	case "minimal", "none":
		return effortPlan{enableThinking: false, maxTokensCap: 1024, coding: true}
	case "low":
		if isQwen38Profile(profile) {
			return effortPlan{enableThinking: profile.Thinking, maxTokensCap: 0, coding: true}
		}
		return effortPlan{enableThinking: false, maxTokensCap: 0, coding: true}
	case "high", "xhigh":
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
			Description: "Set reasoning effort for subsequent turns in this task. Use none/minimal for direct command execution, low or medium for ordinary work, high for debugging, and xhigh for difficult planning or final review.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "effort": {
      "type": "string",
      "enum": ["none", "minimal", "low", "medium", "high", "xhigh"],
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
		return `{"ok":false,"error":"effort must be one of none, minimal, low, medium, high, xhigh"}`, false
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
