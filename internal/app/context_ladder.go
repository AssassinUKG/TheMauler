package app

import (
	"context"
	"fmt"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

func (a *App) applyMicrocompactStage(run *TaskRun, cfg *settings.Settings, toolDefs []llm.ToolDef, reason string) bool {
	if a == nil || run == nil || cfg == nil {
		return false
	}
	a.mu.Lock()
	stats := a.history.MicrocompactThinking(2)
	needsCompact := a.history.NeedsCompactionWithReserve(cfg.Context.CompactionAt, compactionReserveTokens(toolDefs))
	a.mu.Unlock()
	if stats.Compacted <= 0 {
		return needsCompact
	}
	run.addEvent("context_ladder", "Microcompacted old thinking traces", fmt.Sprintf("reason=%s\ncompacted=%d\nbefore_tokens=%d\nafter_tokens=%d\nneeds_compaction=%t", reason, stats.Compacted, stats.BeforeTokens, stats.AfterTokens, needsCompact))
	return needsCompact
}

func (a *App) appendProgressUpdate(ctx context.Context, run *TaskRun, section, content string) {
	if a == nil || run == nil || strings.TrimSpace(content) == "" {
		return
	}
	tool := &progressTool{app: a}
	raw, err := marshalToolArgsNoHTMLEscape(map[string]any{
		"action":  "append",
		"section": section,
		"content": content,
	})
	if err != nil {
		return
	}
	result, err := tool.Run(ctx, raw)
	status := "done"
	if err != nil {
		status = "error"
		result = err.Error()
	}
	run.addEvent("progress", "Progress artifact update", fmt.Sprintf("section=%s\nstatus=%s\n%s", section, status, result))
}

func progressContentForContextDrop(run TaskRun) string {
	var sb strings.Builder
	sb.WriteString("Context was compacted or old tool results were cleared.\n\n")
	if strings.TrimSpace(run.Prompt) != "" {
		sb.WriteString("Objective: ")
		sb.WriteString(truncateRunes(run.Prompt, 500))
		sb.WriteString("\n\n")
	}
	if len(run.Tools) > 0 {
		sb.WriteString("Recent tools:\n")
		start := len(run.Tools) - 6
		if start < 0 {
			start = 0
		}
		for _, tool := range run.Tools[start:] {
			sb.WriteString("- ")
			sb.WriteString(tool.Name)
			if tool.Status != "" {
				sb.WriteString(" (")
				sb.WriteString(tool.Status)
				sb.WriteString(")")
			}
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func progressContentForRunFinish(run TaskRun) string {
	var sb strings.Builder
	if strings.TrimSpace(run.Prompt) != "" {
		sb.WriteString("Objective: ")
		sb.WriteString(truncateRunes(run.Prompt, 500))
		sb.WriteString("\n\n")
	}
	sb.WriteString("Status: ")
	sb.WriteString(run.Status)
	if run.StopReason != "" {
		sb.WriteString(" (")
		sb.WriteString(run.StopReason)
		sb.WriteString(")")
	}
	sb.WriteString("\n\n")
	if strings.TrimSpace(run.Response) != "" {
		sb.WriteString("Summary: ")
		sb.WriteString(truncateRunes(run.Response, 1000))
		sb.WriteString("\n\n")
	}
	if len(run.Tools) > 0 {
		sb.WriteString("Tool trail:\n")
		start := len(run.Tools) - 10
		if start < 0 {
			start = 0
		}
		for _, tool := range run.Tools[start:] {
			sb.WriteString("- ")
			sb.WriteString(tool.Name)
			if tool.Status != "" {
				sb.WriteString(" (")
				sb.WriteString(tool.Status)
				sb.WriteString(")")
			}
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}
