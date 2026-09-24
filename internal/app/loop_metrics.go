package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type LoopMetrics struct {
	ToolCalls                 int    `json:"tool_calls"`
	AutoContinues             int    `json:"auto_continues"`
	Truncations               int    `json:"truncations"`
	ToolErrors                int    `json:"tool_errors"`
	Compactions               int    `json:"compactions"`
	ContextClears             int    `json:"context_clears"`
	VerifierPrompts           int    `json:"verifier_prompts"`
	ReviewCycles              int    `json:"review_cycles"`
	VerifyGateFails           int    `json:"verify_gate_fails"`
	CompletionRailFails       int    `json:"completion_rail_fails"`
	ReviewerChangeRequests    int    `json:"reviewer_change_requests"`
	RepeatedToolInputs        int    `json:"repeated_tool_inputs"`
	RepeatedIdenticalOutcomes int    `json:"repeated_identical_outcomes"`
	RepeatedSkips             int    `json:"repeated_skips"`
	ToolCycleDetected         bool   `json:"tool_cycle_detected"`
	ToolCyclePeriod           int    `json:"tool_cycle_period,omitempty"`
	ToolRoutingEvents         int    `json:"tool_routing_events"`
	MaxRoutedTools            int    `json:"max_routed_tools"`
	PromptWarnings            int    `json:"prompt_warnings"`
	StabilityScore            int    `json:"stability_score"`
	StopReason                string `json:"stop_reason,omitempty"`
	DurationMs                int64  `json:"duration_ms,omitempty"`
	PromptTokens              int    `json:"prompt_tokens,omitempty"`
	CompletionToks            int    `json:"completion_tokens,omitempty"`
}

func buildLoopMetrics(run TaskRun) LoopMetrics {
	metrics := LoopMetrics{
		ToolCalls:                 len(run.Tools),
		AutoContinues:             countRunEvents(run.Events, "continue"),
		Truncations:               countRunEvents(run.Events, "truncated"),
		ToolErrors:                countRunEvents(run.Events, "tool_error") + countToolsWithBadStatus(run.Tools),
		Compactions:               countRunEvents(run.Events, "compaction"),
		ContextClears:             countRunEvents(run.Events, "context_clear"),
		VerifierPrompts:           countRunEvents(run.Events, "verifier_required"),
		ReviewCycles:              countRunEvents(run.Events, "review_gate"),
		VerifyGateFails:           countReviewGateFailures(run.Events, "verify"),
		CompletionRailFails:       countReviewGateFailures(run.Events, "completion"),
		ReviewerChangeRequests:    countReviewGateFailures(run.Events, "reviewer"),
		RepeatedToolInputs:        repeatedToolInputCount(run.Tools),
		RepeatedIdenticalOutcomes: repeatedIdenticalOutcomeCount(run.Tools),
		RepeatedSkips:             repeatedToolSkipCount(run.Tools),
		ToolRoutingEvents:         countRunEvents(run.Events, "tool_routing"),
		MaxRoutedTools:            maxRoutedToolCount(run.Events),
		PromptWarnings:            promptWarningCount(run.Events),
		StopReason:                run.StopReason,
		DurationMs:                run.DurationMs,
		PromptTokens:              run.PromptTokens,
		CompletionToks:            run.CompletionTokens,
	}
	metrics.ToolCycleDetected, metrics.ToolCyclePeriod = detectToolCycle(run.Tools)
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

func countReviewGateFailures(events []TaskRunEvent, family string) int {
	count := 0
	for _, event := range events {
		if event.Kind != "review_gate" || strings.TrimSpace(event.Detail) == "" {
			continue
		}
		var verdicts []VerifyVerdict
		if err := json.Unmarshal([]byte(event.Detail), &verdicts); err != nil {
			continue
		}
		for _, verdict := range verdicts {
			if !reviewVerdictMatchesFamily(verdict.Gate, family) {
				continue
			}
			status := strings.ToLower(strings.TrimSpace(verdict.Status))
			if status != "" && status != "pass" && status != "skip" {
				count++
			}
		}
	}
	return count
}

func reviewVerdictMatchesFamily(gate, family string) bool {
	gate = strings.ToLower(strings.TrimSpace(gate))
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "verify":
		return gate == "verify" || gate == "build" || gate == "test" || gate == "lint"
	case "completion":
		return gate == "completion" || gate == "spec_coverage" || gate == "deliverable"
	case "reviewer":
		return gate == "reviewer"
	default:
		return gate == family
	}
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

func repeatedIdenticalOutcomeCount(tools []TaskToolEvent) int {
	counts := map[string]int{}
	for _, tool := range tools {
		key := normaliseLoopToolOutcome(tool)
		if key == "" {
			continue
		}
		counts[key]++
	}
	repeatedEvents := 0
	for _, count := range counts {
		if count > 1 {
			repeatedEvents += count
		}
	}
	return repeatedEvents
}

func normaliseLoopToolOutcome(tool TaskToolEvent) string {
	name := strings.ToLower(strings.TrimSpace(tool.Name))
	result := normaliseToolResult(tool.Result)
	if name == "" || result == "" {
		return ""
	}
	return name + "\x00" + result
}

func normaliseToolResult(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}
	if len(result) > 4096 {
		result = result[:4096]
	}
	replacements := []struct {
		re   *regexp.Regexp
		with string
	}{
		{regexp.MustCompile(`(?i)result_id\s*=\s*"?[A-Za-z0-9._/\-]+"?`), `result_id=<id>`},
		{regexp.MustCompile(`(?i)\brequest[_-]?id\s*[:=]\s*"?[A-Za-z0-9._/\-]+"?`), `request_id=<id>`},
		{regexp.MustCompile(`(?i)\brun[_-]?id\s*[:=]\s*"?[A-Za-z0-9._/\-]+"?`), `run_id=<id>`},
		{regexp.MustCompile(`\b0x[0-9a-fA-F]{6,}\b`), `0x<hex>`},
		{regexp.MustCompile(`\b[0-9a-fA-F]{32,64}\b`), `<hex>`},
		{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ][0-9:.]+(?:Z|[+-]\d{2}:?\d{2})?\b`), `<timestamp>`},
		{regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}(?:\.\d+)?\b`), `<time>`},
	}
	for _, replacement := range replacements {
		result = replacement.re.ReplaceAllString(result, replacement.with)
	}
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")
	return strings.ToLower(strings.TrimSpace(result))
}

func detectToolCycle(tools []TaskToolEvent) (bool, int) {
	signatures := make([]string, 0, len(tools))
	for _, tool := range tools {
		signature := normaliseLoopToolInput(tool)
		if signature == "" {
			signature = strings.ToLower(strings.TrimSpace(tool.Name))
		}
		if signature != "" {
			signatures = append(signatures, signature)
		}
	}
	for _, period := range []int{3, 2} {
		if len(signatures) < period*2 {
			continue
		}
		tail := signatures[len(signatures)-period*2:]
		if !allDistinct(tail[:period]) {
			continue
		}
		matches := true
		for i := 0; i < period; i++ {
			if tail[i] != tail[i+period] {
				matches = false
				break
			}
		}
		if matches {
			return true, period
		}
	}
	return false, 0
}

func allDistinct(values []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func repeatedToolSkipCount(tools []TaskToolEvent) int {
	count := 0
	for _, tool := range tools {
		status := strings.ToLower(strings.TrimSpace(tool.Status))
		result := strings.ToLower(tool.Result)
		if status == "skipped" || status == "cached" ||
			strings.Contains(result, "repeated command was skipped") ||
			strings.Contains(result, "[cached_tool_result]") {
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
	score -= m.RepeatedIdenticalOutcomes * 6
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
	if m.ToolCycleDetected {
		score -= 8
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

func (m LoopMetrics) LoopStalled() bool {
	if m.RepeatedIdenticalOutcomes >= 2 || m.ToolCycleDetected {
		return true
	}
	if m.StabilityScore > 20 {
		return false
	}
	if m.RepeatedToolInputs >= 2 || m.RepeatedSkips >= 2 {
		return true
	}
	return m.ToolErrors >= 4
}

type loopCircuitBreakerAction string

const (
	loopCircuitBreakerNone   loopCircuitBreakerAction = ""
	loopCircuitBreakerInject loopCircuitBreakerAction = "inject"
	loopCircuitBreakerPause  loopCircuitBreakerAction = "pause"
	loopCircuitBreakerReset  loopCircuitBreakerAction = "reset"
)

func decideLoopCircuitBreaker(metrics LoopMetrics, armed bool, tripMetrics LoopMetrics, toolCountAtTrip, currentToolCount int) loopCircuitBreakerAction {
	if !metrics.LoopStalled() {
		if armed && metrics.StabilityScore >= 45 {
			return loopCircuitBreakerReset
		}
		return loopCircuitBreakerNone
	}
	if armed && currentToolCount > toolCountAtTrip {
		if !loopSignalsWorsened(metrics, tripMetrics) {
			return loopCircuitBreakerReset
		}
		return loopCircuitBreakerPause
	}
	if !armed {
		return loopCircuitBreakerInject
	}
	return loopCircuitBreakerNone
}

func loopSignalsWorsened(current, trip LoopMetrics) bool {
	return current.RepeatedToolInputs > trip.RepeatedToolInputs ||
		current.RepeatedIdenticalOutcomes > trip.RepeatedIdenticalOutcomes ||
		current.RepeatedSkips > trip.RepeatedSkips ||
		current.ToolErrors > trip.ToolErrors ||
		(current.ToolCycleDetected && current.ToolCyclePeriod == trip.ToolCyclePeriod)
}

func loopCircuitBreakerPrompt(metrics LoopMetrics) string {
	return fmt.Sprintf("Loop-health is critical: stability_score=%d, repeated_tool_inputs=%d, repeated_identical_outcomes=%d, repeated_skips=%d, tool_errors=%d, tool_cycle_detected=%t, tool_cycle_period=%d. Your last actions repeated or failed without producing new evidence. Stop repeating. First decide whether the evidence already gathered is sufficient to answer the user's request. If it is sufficient, call no more tools and answer the original request directly now. If evidence is genuinely missing, state the single blocking fact, then take one DIFFERENT action only: for public research use web_search/fetch_url because skill and memory excerpts are methodology, not current evidence; for target work follow redirects with -L or the Location URL, switch back to the confirmed target IP, inspect an existing artifact/result_id once, or change the hypothesis/input. Do not rerun the same command with only head/tail/timeout/count changes.",
		metrics.StabilityScore, metrics.RepeatedToolInputs, metrics.RepeatedIdenticalOutcomes, metrics.RepeatedSkips, metrics.ToolErrors, metrics.ToolCycleDetected, metrics.ToolCyclePeriod)
}

func loopCircuitBreakerStopDetail(metrics LoopMetrics) string {
	return fmt.Sprintf("Loop circuit-breaker paused the run after a corrective prompt because loop-health stayed critical: stability_score=%d repeated_tool_inputs=%d repeated_identical_outcomes=%d repeated_skips=%d tool_errors=%d tool_cycle_detected=%t tool_cycle_period=%d. The agent must change evidence path before continuing.",
		metrics.StabilityScore, metrics.RepeatedToolInputs, metrics.RepeatedIdenticalOutcomes, metrics.RepeatedSkips, metrics.ToolErrors, metrics.ToolCycleDetected, metrics.ToolCyclePeriod)
}
