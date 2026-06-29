package app

import (
	"encoding/json"
	"fmt"
)

type LoopMetrics struct {
	ToolCalls      int    `json:"tool_calls"`
	AutoContinues  int    `json:"auto_continues"`
	Truncations    int    `json:"truncations"`
	ToolErrors     int    `json:"tool_errors"`
	Compactions    int    `json:"compactions"`
	ContextClears  int    `json:"context_clears"`
	StopReason     string `json:"stop_reason,omitempty"`
	DurationMs     int64  `json:"duration_ms,omitempty"`
	PromptTokens   int    `json:"prompt_tokens,omitempty"`
	CompletionToks int    `json:"completion_tokens,omitempty"`
}

func buildLoopMetrics(run TaskRun) LoopMetrics {
	return LoopMetrics{
		ToolCalls:      len(run.Tools),
		AutoContinues:  countRunEvents(run.Events, "continue"),
		Truncations:    countRunEvents(run.Events, "truncated"),
		ToolErrors:     countRunEvents(run.Events, "tool_error"),
		Compactions:    countRunEvents(run.Events, "compaction"),
		ContextClears:  countRunEvents(run.Events, "context_clear"),
		StopReason:     run.StopReason,
		DurationMs:     run.DurationMs,
		PromptTokens:   run.PromptTokens,
		CompletionToks: run.CompletionTokens,
	}
}

func (m LoopMetrics) Detail() string {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("tool_calls=%d auto_continues=%d truncations=%d tool_errors=%d compactions=%d context_clears=%d",
			m.ToolCalls, m.AutoContinues, m.Truncations, m.ToolErrors, m.Compactions, m.ContextClears)
	}
	return string(data)
}
