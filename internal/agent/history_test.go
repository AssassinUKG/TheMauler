package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestHistoryCountsToolCallArguments(t *testing.T) {
	h := NewHistory(1000)
	args := strings.Repeat("x", 4000)
	h.Append(llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCallDef{{
			ID:   "call-1",
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "write_file",
				Arguments: json.RawMessage(`{"content":"` + args + `"}`),
			},
		}},
	})

	if h.TokenCount() < 900 {
		t.Fatalf("tool call arguments were undercounted: %d", h.TokenCount())
	}
}

func TestNeedsCompactionWithReserveUsesEffectiveBudget(t *testing.T) {
	h := NewHistory(1000)
	h.Append(llm.NewTextMessage(llm.RoleUser, strings.Repeat("x", 2000)))

	if !h.NeedsCompactionWithReserve(0.85, 500) {
		t.Fatalf("expected compaction once overhead reserve lowers effective budget")
	}
}

func TestCompactDropsOrphanedToolResultAtBoundary(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.NewTextMessage(llm.RoleSystem, "system"))
	h.Append(llm.NewTextMessage(llm.RoleUser, "first"))
	h.Append(llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCallDef{{
			ID:       "call-1",
			Type:     "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
		}},
	})
	h.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: "orphan if kept alone"})
	h.Append(llm.NewTextMessage(llm.RoleUser, "tail"))

	h.Compact("summary", 1, 2)

	for i, msg := range h.Messages() {
		if msg.Role == llm.RoleTool && (i == 0 || len(h.Messages()[i-1].ToolCalls) == 0) {
			t.Fatalf("compact kept orphaned tool message at %d: %#v", i, h.Messages())
		}
	}
}

func TestClearOldToolResultsKeepsRecentAndShrinksContext(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.NewTextMessage(llm.RoleSystem, "system"))
	for i := 1; i <= 3; i++ {
		h.Append(llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: "call",
			Name:       "read_file",
			Content:    strings.Repeat(string(rune('a'+i)), 2000),
		})
	}
	before := h.TokenCount()
	stats := h.ClearOldToolResults(1)
	if stats.Cleared != 2 {
		t.Fatalf("expected 2 cleared tool results, got %d", stats.Cleared)
	}
	if h.TokenCount() >= before {
		t.Fatalf("expected context to shrink: before=%d after=%d", before, h.TokenCount())
	}
	msgs := h.Messages()
	if !strings.Contains(messageContentText(msgs[1]), "Tool result cleared") {
		t.Fatalf("old tool result was not replaced: %#v", msgs[1].Content)
	}
	if strings.Contains(messageContentText(msgs[3]), "Tool result cleared") {
		t.Fatalf("most recent tool result should be retained")
	}
}
