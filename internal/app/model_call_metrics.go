package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

type promptBudgetSnapshot struct {
	SystemTokens       int
	ConversationTokens int
	ToolSchemaTokens   int
	TotalTokens        int
	ContextWindow      int
	SystemPct          float64
	Over20Pct          bool
	SystemTarget       int
	ToolSchemaTarget   int
	OverSystemTarget   bool
	OverToolTarget     bool
	SystemHash         string
	PromptHash         string
	ToolSchemaHash     string
	SelectedTools      []string
}

const (
	promptBudgetSystemTargetTokens = 3000
	promptBudgetToolTargetTokens   = 1500
)

func buildPromptBudgetSnapshot(msgs []llm.Message, toolDefs []llm.ToolDef, contextWindow int) promptBudgetSnapshot {
	systemChars := 0
	conversationChars := 0
	var promptMaterial strings.Builder
	for _, msg := range msgs {
		material := messageText(msg)
		promptMaterial.WriteString(msg.Role)
		promptMaterial.WriteByte('\n')
		promptMaterial.WriteString(material)
		promptMaterial.WriteByte('\n')
		if msg.Role == llm.RoleSystem {
			systemChars += len(material) + len(msg.Role) + 24
			continue
		}
		conversationChars += len(material) + len(msg.Role) + len(msg.Name) + len(msg.ToolCallID) + 24
		if len(msg.ToolCalls) > 0 {
			if data, err := json.Marshal(msg.ToolCalls); err == nil {
				conversationChars += len(data)
				promptMaterial.Write(data)
				promptMaterial.WriteByte('\n')
			}
		}
	}
	toolChars := 0
	if len(toolDefs) > 0 {
		if data, err := json.Marshal(toolDefs); err == nil {
			toolChars = len(data)
		}
	}
	systemTokens := estimateCharsAsTokens(systemChars)
	conversationTokens := estimateCharsAsTokens(conversationChars)
	toolTokens := estimateCharsAsTokens(toolChars)
	total := systemTokens + conversationTokens + toolTokens + 512
	denom := contextWindow
	if denom <= 0 {
		denom = total
	}
	systemPct := 0.0
	if denom > 0 {
		systemPct = float64(systemTokens+toolTokens) / float64(denom)
	}
	systemText := systemPromptText(msgs)
	return promptBudgetSnapshot{
		SystemTokens:       systemTokens,
		ConversationTokens: conversationTokens,
		ToolSchemaTokens:   toolTokens,
		TotalTokens:        total,
		ContextWindow:      contextWindow,
		SystemPct:          systemPct,
		Over20Pct:          systemPct > 0.20,
		SystemTarget:       promptBudgetSystemTargetTokens,
		ToolSchemaTarget:   promptBudgetToolTargetTokens,
		OverSystemTarget:   systemTokens > promptBudgetSystemTargetTokens,
		OverToolTarget:     toolTokens > promptBudgetToolTargetTokens,
		SystemHash:         shortHash(systemText),
		PromptHash:         shortHash(promptMaterial.String()),
		ToolSchemaHash:     hashToolDefs(toolDefs),
		SelectedTools:      toolNamesFromDefs(toolDefs),
	}
}

func estimateCharsAsTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return chars/3 + 1
}

func systemPromptText(msgs []llm.Message) string {
	var parts []string
	for _, msg := range msgs {
		if msg.Role == llm.RoleSystem {
			parts = append(parts, messageText(msg))
		}
	}
	return strings.Join(parts, "\n\n")
}

func hashToolDefs(toolDefs []llm.ToolDef) string {
	if len(toolDefs) == 0 {
		return ""
	}
	canonical := append([]llm.ToolDef(nil), toolDefs...)
	sort.SliceStable(canonical, func(i, j int) bool {
		return strings.TrimSpace(canonical[i].Function.Name) < strings.TrimSpace(canonical[j].Function.Name)
	})
	data, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	return shortHash(string(data))
}

func shortHash(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

func toolNamesFromDefs(toolDefs []llm.ToolDef) []string {
	names := make([]string, 0, len(toolDefs))
	for _, def := range toolDefs {
		if name := strings.TrimSpace(def.Function.Name); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func recordPromptBudget(run *TaskRun, budget promptBudgetSnapshot, turn int) {
	if run == nil {
		return
	}
	detail := fmt.Sprintf("turn=%d\nestimated_total_tokens=%d\nsystem_tokens=%d\nsystem_target_tokens=%d\ntool_schema_tokens=%d\ntool_schema_target_tokens=%d\nconversation_tokens=%d\ncontext_window=%d\nsystem_plus_tools_pct=%.3f\nover_system_target=%t\nover_tool_schema_target=%t\nover_20_pct=%t\nsystem_prompt_hash=%s\nprompt_hash=%s\ntool_schema_hash=%s\nselected_tools=%s",
		turn,
		budget.TotalTokens,
		budget.SystemTokens,
		budget.SystemTarget,
		budget.ToolSchemaTokens,
		budget.ToolSchemaTarget,
		budget.ConversationTokens,
		budget.ContextWindow,
		budget.SystemPct,
		budget.OverSystemTarget,
		budget.OverToolTarget,
		budget.Over20Pct,
		budget.SystemHash,
		budget.PromptHash,
		budget.ToolSchemaHash,
		strings.Join(budget.SelectedTools, ","),
	)
	message := "Prompt budget snapshot"
	status := "ok"
	if budget.OverSystemTarget || budget.OverToolTarget || budget.Over20Pct {
		message = promptBudgetWarningMessage(budget)
		status = "warn"
	}
	run.addEvent("prompt_budget", message, detail)
	run.recordLedger(ledger.Event{
		Kind:    "prompt_budget",
		Source:  "model",
		Status:  status,
		Message: message,
		Detail:  detail,
		Metadata: map[string]string{
			"turn":                strconv.Itoa(turn),
			"estimated_tokens":    strconv.Itoa(budget.TotalTokens),
			"system_tokens":       strconv.Itoa(budget.SystemTokens),
			"system_target":       strconv.Itoa(budget.SystemTarget),
			"tool_schema_tokens":  strconv.Itoa(budget.ToolSchemaTokens),
			"tool_schema_target":  strconv.Itoa(budget.ToolSchemaTarget),
			"conversation_tokens": strconv.Itoa(budget.ConversationTokens),
			"context_window":      strconv.Itoa(budget.ContextWindow),
			"system_pct":          fmt.Sprintf("%.3f", budget.SystemPct),
			"over_20_pct":         strconv.FormatBool(budget.Over20Pct),
			"over_system_target":  strconv.FormatBool(budget.OverSystemTarget),
			"over_tool_target":    strconv.FormatBool(budget.OverToolTarget),
			"system_prompt_hash":  budget.SystemHash,
			"prompt_hash":         budget.PromptHash,
			"tool_schema_hash":    budget.ToolSchemaHash,
			"selected_tools":      strings.Join(budget.SelectedTools, ","),
		},
	})
}

func promptBudgetWarningMessage(budget promptBudgetSnapshot) string {
	var reasons []string
	if budget.OverSystemTarget {
		reasons = append(reasons, fmt.Sprintf("system %d>%d", budget.SystemTokens, budget.SystemTarget))
	}
	if budget.OverToolTarget {
		reasons = append(reasons, fmt.Sprintf("tools %d>%d", budget.ToolSchemaTokens, budget.ToolSchemaTarget))
	}
	if budget.Over20Pct {
		reasons = append(reasons, "system+tools over 20 percent")
	}
	if len(reasons) == 0 {
		return "Prompt budget snapshot"
	}
	return "Prompt budget over target: " + strings.Join(reasons, ", ")
}

type modelCallObserver struct {
	turn            int
	clientName      string
	modelID         string
	toolChoice      string
	toolCount       int
	selectedTools   []string
	modelLoadFlag   string
	promptBudget    promptBudgetSnapshot
	startedAt       time.Time
	firstDeltaAt    time.Time
	lastDeltaAt     time.Time
	previousDeltaAt time.Time
	deltaCount      int
	contentDeltas   int
	interDeltaTotal time.Duration
}

func newModelCallObserver(turn int, clientName string, profile settings.Profile, req llm.Request, budget promptBudgetSnapshot, modelLoadFlag string) modelCallObserver {
	return modelCallObserver{
		turn:          turn,
		clientName:    clientName,
		modelID:       profile.ModelID,
		toolChoice:    req.ToolChoice,
		toolCount:     len(req.Tools),
		selectedTools: budget.SelectedTools,
		modelLoadFlag: modelLoadFlag,
		promptBudget:  budget,
		startedAt:     time.Now(),
	}
}

func (o *modelCallObserver) observe(delta llm.Delta) {
	hasPayload := delta.Content != "" || delta.Thinking != "" || len(delta.ToolCalls) > 0 || delta.Usage != nil || delta.Done || delta.Truncated || delta.Error != nil
	if !hasPayload {
		return
	}
	now := time.Now()
	if o.firstDeltaAt.IsZero() {
		o.firstDeltaAt = now
	}
	if !o.previousDeltaAt.IsZero() {
		o.interDeltaTotal += now.Sub(o.previousDeltaAt)
	}
	o.previousDeltaAt = now
	o.lastDeltaAt = now
	o.deltaCount++
	if delta.Content != "" || delta.Thinking != "" {
		o.contentDeltas++
	}
}

func (o modelCallObserver) record(run *TaskRun, usage *llm.Usage, status string, err error) {
	if run == nil {
		return
	}
	now := time.Now()
	last := o.lastDeltaAt
	if last.IsZero() {
		last = now
	}
	duration := last.Sub(o.startedAt)
	ttftMs := int64(0)
	if !o.firstDeltaAt.IsZero() {
		ttftMs = o.firstDeltaAt.Sub(o.startedAt).Milliseconds()
	}
	avgGapMs := int64(0)
	if o.deltaCount > 1 {
		avgGapMs = (o.interDeltaTotal / time.Duration(o.deltaCount-1)).Milliseconds()
	}
	promptTokens, completionTokens, totalTokens := 0, 0, 0
	tokensPerSecond := 0.0
	if usage != nil {
		promptTokens = usage.PromptTokens
		completionTokens = usage.CompletionTokens
		totalTokens = usage.TotalTokens
		if totalTokens <= 0 {
			totalTokens = promptTokens + completionTokens
		}
		if usage.CompletionTokensPerSecond > 0 {
			tokensPerSecond = usage.CompletionTokensPerSecond
		} else if completionTokens > 0 && duration.Seconds() > 0 {
			tokensPerSecond = float64(completionTokens) / duration.Seconds()
		}
	}
	if status == "" {
		status = "ok"
	}
	message := "Model call completed"
	errorText := ""
	if err != nil {
		status = "error"
		message = "Model call failed"
		errorText = err.Error()
	}
	backendPromptTPS, cachedPromptTokens := 0.0, 0
	if usage != nil {
		backendPromptTPS = usage.PromptTokensPerSecond
		cachedPromptTokens = usage.CachedPromptTokens
	}
	detail := fmt.Sprintf("turn=%d\nclient=%s\nmodel=%s\nstatus=%s\nmodel_load=%s\ntool_choice=%s\ntools=%d\nselected_tools=%s\nttft_ms=%d\nduration_ms=%d\navg_inter_delta_ms=%d\ndeltas=%d\ncontent_deltas=%d\nprompt_tokens=%d\ncached_prompt_tokens=%d\ncompletion_tokens=%d\ntotal_tokens=%d\nprompt_tokens_per_second=%.2f\ntokens_per_second=%.2f\nsystem_prompt_hash=%s\nprompt_hash=%s\ntool_schema_hash=%s",
		o.turn,
		o.clientName,
		o.modelID,
		status,
		o.modelLoadFlag,
		o.toolChoice,
		o.toolCount,
		strings.Join(o.selectedTools, ","),
		ttftMs,
		duration.Milliseconds(),
		avgGapMs,
		o.deltaCount,
		o.contentDeltas,
		promptTokens,
		cachedPromptTokens,
		completionTokens,
		totalTokens,
		backendPromptTPS,
		tokensPerSecond,
		o.promptBudget.SystemHash,
		o.promptBudget.PromptHash,
		o.promptBudget.ToolSchemaHash,
	)
	run.addEvent("model_call", message, detail)
	run.recordLedger(ledger.Event{
		Kind:       "model_call",
		Source:     "model",
		Status:     status,
		Message:    message,
		Detail:     detail,
		Error:      errorText,
		DurationMs: duration.Milliseconds(),
		Metadata: map[string]string{
			"turn":                     strconv.Itoa(o.turn),
			"client":                   o.clientName,
			"model":                    o.modelID,
			"model_load":               o.modelLoadFlag,
			"tool_choice":              o.toolChoice,
			"tool_count":               strconv.Itoa(o.toolCount),
			"selected_tools":           strings.Join(o.selectedTools, ","),
			"ttft_ms":                  strconv.FormatInt(ttftMs, 10),
			"duration_ms":              strconv.FormatInt(duration.Milliseconds(), 10),
			"avg_inter_delta_ms":       strconv.FormatInt(avgGapMs, 10),
			"deltas":                   strconv.Itoa(o.deltaCount),
			"content_deltas":           strconv.Itoa(o.contentDeltas),
			"prompt_tokens":            strconv.Itoa(promptTokens),
			"cached_prompt_tokens":     strconv.Itoa(cachedPromptTokens),
			"completion_tokens":        strconv.Itoa(completionTokens),
			"total_tokens":             strconv.Itoa(totalTokens),
			"prompt_tokens_per_second": fmt.Sprintf("%.2f", backendPromptTPS),
			"tokens_per_second":        fmt.Sprintf("%.2f", tokensPerSecond),
			"system_prompt_hash":       o.promptBudget.SystemHash,
			"prompt_hash":              o.promptBudget.PromptHash,
			"tool_schema_hash":         o.promptBudget.ToolSchemaHash,
			"prompt_budget_total":      strconv.Itoa(o.promptBudget.TotalTokens),
			"system_pct":               fmt.Sprintf("%.3f", o.promptBudget.SystemPct),
		},
	})
}

func modelLoadFlagForCall(client llm.Client, profile settings.Profile, keyBefore string) string {
	if _, ok := client.(interface{ LoadModel(context.Context) error }); !ok {
		return "skipped"
	}
	key := modelLoadKey(profile)
	if key != "" && (key == keyBefore || modelLoadKeySameRuntime(profile, keyBefore)) {
		return "reused"
	}
	return "loaded"
}
