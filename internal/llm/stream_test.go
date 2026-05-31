package llm

import (
	"context"
	"strings"
	"testing"
)

func collectSSE(input string) []Delta {
	ch := make(chan Delta, 32)
	ParseSSE(context.Background(), strings.NewReader(input), ch)
	close(ch)
	var out []Delta
	for d := range ch {
		out = append(out, d)
	}
	return out
}

// --- finish_reason: length ---

func TestParseSSEMarksLengthFinishAsTruncated(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"Right - let me write the updated document now."},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
		"",
	}, "\n")
	ch := make(chan Delta, 4)
	ParseSSE(context.Background(), strings.NewReader(input), ch)
	close(ch)

	var content string
	var truncated bool
	for delta := range ch {
		content += delta.Content
		if delta.Truncated {
			truncated = true
		}
	}
	if content != "Right - let me write the updated document now." {
		t.Fatalf("content = %q", content)
	}
	if !truncated {
		t.Fatalf("finish_reason length was not marked truncated")
	}
}

// --- finish_reason: stop ---

func TestParseSSEStopFinishIsNotTruncated(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	for _, d := range collectSSE(input) {
		if d.Truncated {
			t.Fatalf("stop finish should not be truncated")
		}
	}
}

// --- finish_reason: eos (llama.cpp variant) ---

func TestParseSSEEosFinishIsNotTruncated(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"eos"}]}`,
		"",
	}, "\n")
	deltas := collectSSE(input)
	var sawDone bool
	for _, d := range deltas {
		if d.Done {
			sawDone = true
		}
		if d.Truncated {
			t.Fatal("eos should not be truncated")
		}
	}
	if !sawDone {
		t.Fatal("expected Done delta")
	}
}

// --- text streaming ---

func TestParseSSEAccumulatesTextChunks(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":", world"},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	var combined string
	for _, d := range collectSSE(input) {
		combined += d.Content
	}
	if combined != "Hello, world" {
		t.Fatalf("combined = %q", combined)
	}
}

// --- thinking tokens ---

func TestParseSSEThinkingContent(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"thinking..."},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	var thinking, content string
	for _, d := range collectSSE(input) {
		thinking += d.Thinking
		content += d.Content
	}
	if thinking != "thinking..." {
		t.Fatalf("thinking = %q", thinking)
	}
	if content != "answer" {
		t.Fatalf("content = %q", content)
	}
}

func TestParseSSEReasoningAlias(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"reasoning":"thinking alias"},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}, "\n\n")
	var thinking, content string
	for _, d := range collectSSE(input) {
		thinking += d.Thinking
		content += d.Content
	}
	if thinking != "thinking alias" || content != "answer" {
		t.Fatalf("thinking=%q content=%q", thinking, content)
	}
}

// --- tool calls ---

func TestParseSSEToolCallAccumulation(t *testing.T) {
	// Arguments arrive in fragments across chunks
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"write_file","arguments":""}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"","function":{"name":"","arguments":"{\"path\":"}}]},"finish_reason":""}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"","type":"","function":{"name":"","arguments":"\"out.txt\"}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n")
	deltas := collectSSE(input)
	var toolCalls []ToolCallDef
	for _, d := range deltas {
		if len(d.ToolCalls) > 0 {
			toolCalls = d.ToolCalls
		}
	}
	if len(toolCalls) == 0 {
		t.Fatal("expected tool calls")
	}
	if toolCalls[0].Function.Name != "write_file" {
		t.Fatalf("name = %q", toolCalls[0].Function.Name)
	}
	if string(toolCalls[0].Function.Arguments) != `{"path":"out.txt"}` {
		t.Fatalf("args = %q", toolCalls[0].Function.Arguments)
	}
	if toolCalls[0].ID != "call1" {
		t.Fatalf("id = %q", toolCalls[0].ID)
	}
}

func TestParseSSEMultipleToolCalls(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}},{"index":1,"id":"b","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"b.txt\"}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n")
	var calls []ToolCallDef
	for _, d := range collectSSE(input) {
		if len(d.ToolCalls) > 0 {
			calls = d.ToolCalls
		}
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
}

func TestParseSSEToolCallArgumentsObject(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"glob","arguments":{"pattern":"**/*.go"}}}]},"finish_reason":"tool_calls"}]}`,
		"",
	}, "\n")
	var calls []ToolCallDef
	for _, d := range collectSSE(input) {
		if len(d.ToolCalls) > 0 {
			calls = d.ToolCalls
		}
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "glob" || string(calls[0].Function.Arguments) != `{"pattern":"**/*.go"}` {
		t.Fatalf("bad tool call: %#v", calls[0])
	}
}

// --- usage chunk ---

func TestParseSSEUsageChunk(t *testing.T) {
	// Usage-only chunk arrives before [DONE] (OpenAI / LM Studio pattern)
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":""}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	var usage *Usage
	for _, d := range collectSSE(input) {
		if d.Usage != nil {
			usage = d.Usage
		}
	}
	if usage == nil {
		t.Fatal("expected usage delta")
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 5 || usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v", usage)
	}
}

// --- [DONE] sentinel ---

func TestParseSSEDoneSentinelBreaksLoop(t *testing.T) {
	input := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		`data: {"choices":[{"index":0,"delta":{"content":"after done"},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	var combined string
	for _, d := range collectSSE(input) {
		combined += d.Content
	}
	if strings.Contains(combined, "after done") {
		t.Fatalf("[DONE] should stop parsing, got %q", combined)
	}
}

// --- malformed JSON is silently skipped ---

func TestParseSSESkipsMalformedJSON(t *testing.T) {
	input := strings.Join([]string{
		`data: not valid json`,
		`data: {"choices":[{"index":0,"delta":{"content":"valid"},"finish_reason":"stop"}]}`,
		"",
	}, "\n")
	var combined string
	for _, d := range collectSSE(input) {
		combined += d.Content
	}
	if combined != "valid" {
		t.Fatalf("expected only valid content, got %q", combined)
	}
}

// --- context cancellation ---

func TestParseSSEContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	input := `data: {"choices":[{"index":0,"delta":{"content":"x"},"finish_reason":"stop"}]}` + "\n"
	ch := make(chan Delta, 4)
	ParseSSE(ctx, strings.NewReader(input), ch)
	close(ch)

	var sawError bool
	for d := range ch {
		if d.Error != nil {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("expected error delta on cancelled context")
	}
}
