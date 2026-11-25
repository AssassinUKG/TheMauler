package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestValidateToolCallBoundary(t *testing.T) {
	defs := []llm.ToolDef{{Type: "function", Function: llm.ToolFunctionDef{Name: "read"}}}
	readCall := llm.ToolCallDef{ID: "call-1", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)}}
	if got := validateToolCallBoundary([]llm.ToolCallDef{readCall}, defs, "auto"); got != "" {
		t.Fatalf("advertised call rejected: %s", got)
	}
	if got := validateToolCallBoundary([]llm.ToolCallDef{readCall}, nil, "none"); !strings.Contains(got, "text-only turn") {
		t.Fatalf("none boundary result = %q", got)
	}
	unknown := readCall
	unknown.Function.Name = "shell"
	if got := validateToolCallBoundary([]llm.ToolCallDef{unknown}, defs, "auto"); !strings.Contains(got, "unadvertised") {
		t.Fatalf("unknown boundary result = %q", got)
	}
	conflict := readCall
	conflict.Function.Arguments = json.RawMessage(`{"path":"b.txt"}`)
	if got := validateToolCallBoundary([]llm.ToolCallDef{readCall, conflict}, defs, "auto"); !strings.Contains(got, "conflicting") {
		t.Fatalf("conflicting id result = %q", got)
	}
}
