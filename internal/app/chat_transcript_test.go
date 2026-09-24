package app

import (
	"encoding/json"
	"testing"

	"mauler/internal/llm"
)

func TestSessionTranscriptPreservesAssistantTurnsThinkingAndToolExchange(t *testing.T) {
	assistant := llm.NewTextMessage(llm.RoleAssistant, "I found the detailed result and will verify one final item.")
	assistant.ReasoningContent = "bounded reasoning trace"
	assistant.ToolCalls = []llm.ToolCallDef{{
		ID:   "call-1",
		Type: "function",
		Function: llm.FunctionCall{
			Name:      "http_probe",
			Arguments: json.RawMessage(`{"url":"https://example.test"}`),
		},
	}}
	toolResult := llm.Message{
		Role: llm.RoleTool, Content: "HTTP 200", ToolCallID: "call-1", Name: "http_probe",
	}

	got := toSessionChatMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "private primary system prompt"),
		llm.NewTextMessage(llm.RoleUser, "Check the site"),
		assistant,
		toolResult,
	})
	if len(got) != 4 {
		t.Fatalf("transcript entries = %d, want user + assistant + call + result: %#v", len(got), got)
	}
	if got[1].Role != llm.RoleAssistant || got[1].Thinking != "bounded reasoning trace" {
		t.Fatalf("assistant turn lost thinking: %#v", got[1])
	}
	if got[2].Role != "tool_call" || got[2].ToolName != "http_probe" || got[2].ToolCallID != "call-1" {
		t.Fatalf("tool call not preserved: %#v", got[2])
	}
	if got[3].Role != "tool_result" || got[3].ToolName != "http_probe" || got[3].Content != "HTTP 200" {
		t.Fatalf("tool result not preserved: %#v", got[3])
	}
	for _, message := range got {
		if message.Content == "private primary system prompt" {
			t.Fatal("primary system prompt must remain outside the user transcript")
		}
	}
}
