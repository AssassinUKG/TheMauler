package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestPromptBudgetSnapshotHashesAndWarnsOverTwentyPercent(t *testing.T) {
	toolParams := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	msgs := []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, stringsRepeat("system ", 120)),
		llm.NewTextMessage(llm.RoleUser, "summarize the project"),
	}
	tools := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFunctionDef{Name: "read", Description: "Read", Parameters: toolParams}},
		{Type: "function", Function: llm.ToolFunctionDef{Name: "grep", Description: "Search", Parameters: toolParams}},
	}

	snap := buildPromptBudgetSnapshot(msgs, tools, 512)
	if snap.SystemHash == "" || snap.PromptHash == "" || snap.ToolSchemaHash == "" {
		t.Fatalf("expected hashes, got %#v", snap)
	}
	if !snap.Over20Pct {
		t.Fatalf("expected system/tool budget warning, got pct %.3f", snap.SystemPct)
	}
	if got := strings.Join(snap.SelectedTools, ","); got != "grep,read" {
		t.Fatalf("selected tools = %q", got)
	}
}

func TestHashToolDefsIsOrderIndependent(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	read := llm.ToolDef{Type: "function", Function: llm.ToolFunctionDef{Name: "read", Description: "Read", Parameters: params}}
	grep := llm.ToolDef{Type: "function", Function: llm.ToolFunctionDef{Name: "grep", Description: "Search", Parameters: params}}

	forward := hashToolDefs([]llm.ToolDef{read, grep})
	reverse := hashToolDefs([]llm.ToolDef{grep, read})
	if forward == "" || reverse == "" {
		t.Fatal("expected non-empty tool-schema hashes")
	}
	if forward != reverse {
		t.Fatalf("tool-schema hash changed with declaration order: %q != %q", forward, reverse)
	}
}

func TestPromptBudgetSnapshotWarnsOnAbsoluteTargets(t *testing.T) {
	largeDescription := stringsRepeat("schema ", 1400)
	toolParams := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	msgs := []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "compact system prompt"),
		llm.NewTextMessage(llm.RoleUser, "inspect the project"),
	}
	tools := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFunctionDef{Name: "oversized_tool", Description: largeDescription, Parameters: toolParams}},
	}

	snap := buildPromptBudgetSnapshot(msgs, tools, 40000)
	if snap.Over20Pct {
		t.Fatalf("did not expect percentage warning in large context: %.3f", snap.SystemPct)
	}
	if !snap.OverToolTarget {
		t.Fatalf("expected tool target warning, got %d <= %d", snap.ToolSchemaTokens, snap.ToolSchemaTarget)
	}

	run := startTaskRun("prompt", "Auto", "profile", "model")
	recordPromptBudget(&run, snap, 1)
	var found bool
	for _, event := range run.Events {
		if event.Kind != "prompt_budget" {
			continue
		}
		found = true
		if !strings.Contains(event.Message, "tools") || !strings.Contains(event.Detail, "over_tool_schema_target=true") {
			t.Fatalf("prompt budget event missing absolute target warning: %#v", event)
		}
	}
	if !found {
		t.Fatal("prompt_budget event was not recorded")
	}
}

func TestModelCallObserverRecordsTimingMetadata(t *testing.T) {
	run := startTaskRun("prompt", "Auto", "profile", "model")
	budget := promptBudgetSnapshot{
		SystemHash:     "sys",
		PromptHash:     "prompt",
		ToolSchemaHash: "tools",
		SelectedTools:  []string{"read"},
		TotalTokens:    100,
		SystemPct:      0.1,
	}
	req := llm.Request{ToolChoice: "auto", Tools: []llm.ToolDef{{Type: "function", Function: llm.ToolFunctionDef{Name: "read"}}}}
	obs := newModelCallObserver(2, "test-client", settings.Profile{ModelID: "qwen"}, req, budget, "loaded")
	obs.startedAt = time.Now().Add(-30 * time.Millisecond)
	obs.observe(llm.Delta{Content: "a"})
	time.Sleep(time.Millisecond)
	usage := &llm.Usage{
		PromptTokens:              10,
		CompletionTokens:          2,
		TotalTokens:               12,
		CachedPromptTokens:        4,
		PromptTokensPerSecond:     1031.5,
		CompletionTokensPerSecond: 29.86,
	}
	obs.observe(llm.Delta{Content: "b", Usage: usage})
	obs.record(&run, usage, "ok", nil)

	var found bool
	for _, event := range run.Events {
		if event.Kind == "model_call" {
			found = true
			if !strings.Contains(event.Detail, "ttft_ms=") ||
				!strings.Contains(event.Detail, "tool_schema_hash=tools") ||
				!strings.Contains(event.Detail, "cached_prompt_tokens=4") ||
				!strings.Contains(event.Detail, "prompt_tokens_per_second=1031.50") ||
				!strings.Contains(event.Detail, "tokens_per_second=29.86") {
				t.Fatalf("model_call detail missing metrics: %s", event.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("model_call event not recorded: %#v", run.Events)
	}
}

func stringsRepeat(s string, count int) string {
	var out strings.Builder
	for i := 0; i < count; i++ {
		out.WriteString(s)
	}
	return out.String()
}
