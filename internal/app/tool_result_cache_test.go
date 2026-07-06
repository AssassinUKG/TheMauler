package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestCachedToolResultForReadCall(t *testing.T) {
	input := `{"path":"Connected.md"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Status: "done", Input: input, Result: "target: 10.129.23.158"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "read", Arguments: json.RawMessage(`{ "path": "Connected.md" }`)}}

	got := cachedToolResultForCall(run, tc)
	for _, want := range []string{"[cached_tool_result]", "state: cached", "tool: read", "next_tool: proceed", "do_not_repeat:", "10.129.23.158"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected cached read result to contain %q, got %q", want, got)
		}
	}
}

func TestCachedToolResultForOffloadedResultPointsToReadToolResult(t *testing.T) {
	input := `{"command":"nmap -sC -sV 10.129.23.158"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "HEAD\n\n[tool result offloaded: 50000 chars omitted. result_id=run-1/result-9. Do NOT re-run the command to see more - call read_tool_result with this result_id.]\n\nTAIL"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(input)}}

	got := cachedToolResultForCall(run, tc)
	for _, want := range []string{"[cached_tool_result]", "result_id: run-1/result-9", "next_tool: read_tool_result", "Do NOT re-run"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected cached offloaded result to contain %q, got %q", want, got)
		}
	}
}

func TestCachedToolResultIDTrimsPunctuation(t *testing.T) {
	got := cachedToolResultID(`[more available: call read_tool_result with result_id="run-3/grep-4", offset=1000]`)
	if got != "run-3/grep-4" {
		t.Fatalf("expected trimmed result id, got %q", got)
	}
}

func TestCachedToolResultForShellCall(t *testing.T) {
	input := `{"command":"curl -sk https://connected.htb/health"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "HTTP/1.1 200 OK"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(input)}}

	got := cachedToolResultForCall(run, tc)
	if !strings.Contains(got, "[cached_tool_result]") || !strings.Contains(got, "HTTP/1.1 200 OK") {
		t.Fatalf("expected cached read result, got %q", got)
	}
}

func TestCachedToolResultForShellPagerVariant(t *testing.T) {
	input := `{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -100"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "301 Moved Permanently"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(`{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -30"}`)}}

	got := cachedToolResultForCall(run, tc)
	if !strings.Contains(got, "[cached_tool_result]") || !strings.Contains(got, "301 Moved Permanently") {
		t.Fatalf("pager-only shell variant should hit cache, got %q", got)
	}
}

func TestRepeatedCachedToolCallDetectsSecondCacheReplay(t *testing.T) {
	input := `{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -100"}`
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(input)}}
	first := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "301 Moved Permanently"},
	}}
	if repeatedCachedToolCall(first, tc) {
		t.Fatalf("first cache replay should not be treated as repeated")
	}
	second := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "301 Moved Permanently"},
		{Name: "shell", Status: "cached", Input: input, Result: "[cached_tool_result]"},
	}}
	if !repeatedCachedToolCall(second, tc) {
		t.Fatalf("second identical cached replay should be detected")
	}
}

func TestCachedToolResultIgnoresFailedAndMutatingTools(t *testing.T) {
	readInput := `{"path":"missing.txt"}`
	writeInput := `{"path":"Connected.md","content":"x"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Status: "error", Input: readInput, Result: "not found"},
		{Name: "write", Status: "done", Input: writeInput, Result: "wrote"},
	}}
	if got := cachedToolResultForCall(run, llm.ToolCallDef{Function: llm.FunctionCall{Name: "read", Arguments: json.RawMessage(readInput)}}); got != "" {
		t.Fatalf("failed reads must not cache, got %q", got)
	}
	if got := cachedToolResultForCall(run, llm.ToolCallDef{Function: llm.FunctionCall{Name: "write", Arguments: json.RawMessage(writeInput)}}); got != "" {
		t.Fatalf("mutating tools must not cache, got %q", got)
	}
}
