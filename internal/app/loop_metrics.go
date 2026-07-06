package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type LoopMetrics struct {
	ToolCalls          int    `json:"tool_calls"`
	AutoContinues      int    `json:"auto_continues"`
	Truncations        int    `json:"truncations"`
	ToolErrors         int    `json:"tool_errors"`
	Compactions        int    `json:"compactions"`
	ContextClears      int    `json:"context_clears"`
	VerifierPrompts    int    `json:"verifier_prompts"`
	RepeatedToolInputs int    `json:"repeated_tool_inputs"`
	RepeatedSkips      int    `json:"repeated_skips"`
	ToolRoutingEvents  int    `json:"tool_routing_events"`
	MaxRoutedTools     int    `json:"max_routed_tools"`
	PromptWarnings     int    `json:"prompt_warnings"`
	StabilityScore     int    `json:"stability_score"`
	StopReason         string `json:"stop_reason,omitempty"`
	DurationMs         int64  `json:"duration_ms,omitempty"`
	PromptTokens       int    `json:"prompt_tokens,omitempty"`
	CompletionToks     int    `json:"completion_tokens,omitempty"`
}

func buildLoopMetrics(run TaskRun) LoopMetrics {
	metrics := LoopMetrics{
		ToolCalls:          len(run.Tools),
		AutoContinues:      countRunEvents(run.Events, "continue"),
		Truncations:        countRunEvents(run.Events, "truncated"),
		ToolErrors:         countRunEvents(run.Events, "tool_error") + countToolsWithBadStatus(run.Tools),
		Compactions:        countRunEvents(run.Events, "compaction"),
		ContextClears:      countRunEvents(run.Events, "context_clear"),
		VerifierPrompts:    countRunEvents(run.Events, "verifier_required"),
		RepeatedToolInputs: repeatedToolInputCount(run.Tools),
		RepeatedSkips:      repeatedToolSkipCount(run.Tools),
		ToolRoutingEvents:  countRunEvents(run.Events, "tool_routing"),
		MaxRoutedTools:     maxRoutedToolCount(run.Events),
		PromptWarnings:     promptWarningCount(run.Events),
		StopReason:         run.StopReason,
		DurationMs:         run.DurationMs,
		PromptTokens:       run.PromptTokens,
		CompletionToks:     run.CompletionTokens,
	}
	metrics.StabilityScore = loopStabilityScore(metrics)
	return metrics
}

func (m LoopMetrics) Detail() string {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("tool_calls=%d auto_continues=%d truncations=%d tool_errors=%d compactions=%d context_clears=%d",
			m.ToolCalls, m.AutoContinues, m.Truncations, m.ToolErrors, m.Compactions, m.ContextClears)
	}
	return string(data)
}

func countToolsWithBadStatus(tools []TaskToolEvent) int {
	count := 0
	for _, tool := range tools {
		status := strings.ToLower(strings.TrimSpace(tool.Status))
		if status == "error" || status == "blocked" || status == "denied" {
			count++
		}
	}
	return count
}

func repeatedToolInputCount(tools []TaskToolEvent) int {
	counts := map[string]int{}
	for _, tool := range tools {
		key := normaliseLoopToolInput(tool)
		if key == "" {
			continue
		}
		counts[key]++
	}
	repeats := 0
	for _, count := range counts {
		if count > 1 {
			repeats += count - 1
		}
	}
	return repeats
}

func normaliseLoopToolInput(tool TaskToolEvent) string {
	input := strings.TrimSpace(tool.Input)
	if input == "" {
		return ""
	}
	if normalized := normaliseLoopToolJSONInput(input); normalized != "" {
		input = normalized
	}
	input = regexp.MustCompile(`\s+`).ReplaceAllString(input, " ")
	input = regexp.MustCompile(`"timeout"\s*:\s*\d+`).ReplaceAllString(input, `"timeout":<n>`)
	input = regexp.MustCompile(`"wait_ms"\s*:\s*\d+`).ReplaceAllString(input, `"wait_ms":<n>`)
	return strings.ToLower(strings.TrimSpace(tool.Name) + ":" + input)
}

func normaliseLoopToolJSONInput(input string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(input), &obj); err != nil {
		return ""
	}
	if command, ok := obj["command"].(string); ok {
		obj["command"] = normalizeShellCommandForDedup(command)
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(data)
}

func repeatedToolSkipCount(tools []TaskToolEvent) int {
	count := 0
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Status), "skipped") ||
			strings.Contains(strings.ToLower(tool.Result), "repeated command was skipped") {
			count++
		}
	}
	return count
}

func maxRoutedToolCount(events []TaskRunEvent) int {
	maxTools := 0
	re := regexp.MustCompile(`(?m)^tool_count=(\d+)`)
	for _, event := range events {
		if event.Kind != "tool_routing" {
			continue
		}
		if m := re.FindStringSubmatch(event.Detail); len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil && n > maxTools {
				maxTools = n
			}
		}
	}
	return maxTools
}

func promptWarningCount(events []TaskRunEvent) int {
	count := 0
	for _, event := range events {
		if event.Kind != "prompt_budget" {
			continue
		}
		text := strings.ToLower(event.Message + "\n" + event.Detail)
		if strings.Contains(text, "warn") || strings.Contains(text, "over_20_pct=true") || strings.Contains(text, "over 20") {
			count++
		}
	}
	return count
}

func loopStabilityScore(m LoopMetrics) int {
	score := 100
	score -= m.ToolErrors * 8
	score -= m.RepeatedToolInputs * 5
	score -= m.RepeatedSkips * 4
	score -= m.Truncations * 6
	score -= m.AutoContinues * 2
	score -= m.Compactions * 3
	score -= m.ContextClears * 2
	score -= m.PromptWarnings * 5
	score -= m.VerifierPrompts * 1
	if m.MaxRoutedTools > 24 {
		score -= (m.MaxRoutedTools - 24) / 2
	}
	if m.StopReason != "" && m.StopReason != "user_stopped" {
		score -= 12
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}
