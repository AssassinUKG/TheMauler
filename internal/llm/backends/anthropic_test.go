package backends

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestBuildAnthropicBodyConvertsMessagesAndTools(t *testing.T) {
	body, err := buildAnthropicBody("claude-test", llm.Request{
		Messages: []llm.Message{
			llm.NewTextMessage(llm.RoleSystem, "system rules"),
			llm.NewTextMessage(llm.RoleUser, "hello"),
			{
				Role:    llm.RoleAssistant,
				Content: "calling",
				ToolCalls: []llm.ToolCallDef{{
					ID:   "toolu_1",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "read_file",
						Arguments: json.RawMessage(`{"path":"a.txt"}`),
					},
				}},
			},
			{Role: llm.RoleTool, ToolCallID: "toolu_1", Name: "read_file", Content: "file body"},
		},
		Tools: []llm.ToolDef{{
			Type: "function",
			Function: llm.ToolFunctionDef{
				Name:        "read_file",
				Description: "Read a file",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		}},
		ToolChoice: "required",
		MaxTokens:  123,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "claude-test" || got["system"] != "system rules" || got["max_tokens"].(float64) != 123 {
		t.Fatalf("bad body: %s", body)
	}
	choice := got["tool_choice"].(map[string]any)
	if choice["type"] != "any" {
		t.Fatalf("tool_choice = %#v", choice)
	}
	if !strings.Contains(string(body), `"tool_result"`) || !strings.Contains(string(body), `"tool_use"`) {
		t.Fatalf("body missing tool content: %s", body)
	}
}

func TestAnthropicSSEParsingTextToolAndUsage(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		``,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\""}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"a.txt\"}"}}`,
		``,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":7}}`,
		``,
	}, "\n")

	ch := make(chan llm.Delta, 8)
	parseAnthropicSSE(context.Background(), strings.NewReader(raw), ch)
	close(ch)

	var text string
	var calls []llm.ToolCallDef
	var truncated bool
	var usage *llm.Usage
	for delta := range ch {
		text += delta.Content
		calls = append(calls, delta.ToolCalls...)
		if delta.Truncated {
			truncated = true
		}
		if delta.Usage != nil {
			usage = delta.Usage
		}
		if delta.Error != nil {
			t.Fatalf("unexpected parse error: %v", delta.Error)
		}
	}
	if text != "hello" {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 1 || calls[0].Function.Name != "read_file" || string(calls[0].Function.Arguments) != `{"path":"a.txt"}` {
		t.Fatalf("calls = %#v", calls)
	}
	if !truncated {
		t.Fatal("expected truncation delta")
	}
	if usage == nil || usage.CompletionTokens != 7 {
		t.Fatalf("usage = %#v", usage)
	}
}
