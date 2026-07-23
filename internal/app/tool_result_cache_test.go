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

func TestCachedToolResultInvalidatedBySuccessfulFileMutation(t *testing.T) {
	input := `{"path":"src/task.txt"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Status: "done", Input: input, Result: "status = TODO"},
		{Name: "edit", Status: "done", Input: `{"path":"src/task.txt","old":"TODO","new":"DONE"}`, Result: "edited"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "read", Arguments: json.RawMessage(input)}}

	if got := cachedToolResultForCall(run, tc); got != "" {
		t.Fatalf("read cache must be invalidated after a successful file mutation, got %q", got)
	}
}

func TestCachedToolResultUsesFreshReadAfterFileMutation(t *testing.T) {
	input := `{"path":"src/task.txt"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Status: "done", Input: input, Result: "status = TODO"},
		{Name: "edit", Status: "done", Input: `{"path":"src/task.txt","old":"TODO","new":"DONE"}`, Result: "edited"},
		{Name: "read", Status: "done", Input: input, Result: "status = DONE"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "read", Arguments: json.RawMessage(input)}}

	got := cachedToolResultForCall(run, tc)
	if !strings.Contains(got, "status = DONE") || strings.Contains(got, "status = TODO") {
		t.Fatalf("cache should use the newest post-mutation read: %q", got)
	}
}

func TestCachedToolResultForOffloadedResultPointsToReadToolResult(t *testing.T) {
	input := `{"path":"large-output.txt"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Status: "done", Input: input, Result: "HEAD\n\n[tool result offloaded: 50000 chars omitted. result_id=run-1/result-9. Do NOT re-run the command to see more - call read_tool_result with this result_id.]\n\nTAIL"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "read", Arguments: json.RawMessage(input)}}

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

func TestCachedToolResultIgnoresShellCall(t *testing.T) {
	input := `{"command":"curl -sk https://connected.htb/health"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "HTTP/1.1 200 OK"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(input)}}

	if got := cachedToolResultForCall(run, tc); got != "" {
		t.Fatalf("shell must not use the generic cache, got %q", got)
	}
}

func TestCachedToolResultIgnoresShellPagerVariant(t *testing.T) {
	input := `{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -100"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "301 Moved Permanently"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(`{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -30"}`)}}

	if got := cachedToolResultForCall(run, tc); got != "" {
		t.Fatalf("shell pager variants must not use the generic cache, got %q", got)
	}
}

func TestRepeatedCachedToolCallDetectsSecondCacheReplay(t *testing.T) {
	input := `{"path":"Connected.md"}`
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "read", Arguments: json.RawMessage(input)}}
	first := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Status: "done", Input: input, Result: "target: 10.129.23.158"},
	}}
	if repeatedCachedToolCall(first, tc) {
		t.Fatalf("first cache replay should not be treated as repeated")
	}
	second := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Status: "done", Input: input, Result: "target: 10.129.23.158"},
		{Name: "read", Status: "cached", Input: input, Result: "[cached_tool_result]"},
	}}
	if !repeatedCachedToolCall(second, tc) {
		t.Fatalf("second identical cached replay should be detected")
	}
}

func TestCachedEmptyGlobResultForCall(t *testing.T) {
	input := `{"pattern":"*.txt","dir":"loot"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "glob", Status: "done", Input: input, Result: "[glob_result]\nstate: empty\npattern: *.txt\ndir: loot\nmatches: 0\nrepeat_policy: do_not_re_glob_same_path"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "glob", Arguments: json.RawMessage(`{"dir":"loot/","pattern":"*.txt"}`)}}

	got := cachedEmptyGlobResultForCall(run, tc)
	for _, want := range []string{"[empty_glob_cached]", "state: cached_empty", "do_not_repeat", "matches: 0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("empty glob cache missing %q:\n%s", want, got)
		}
	}
}

func TestCachedEmptyGlobResultForCallIgnoresChangedPattern(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "glob", Status: "done", Input: `{"pattern":"*.txt","dir":"loot"}`, Result: "[glob_result]\nstate: empty\nmatches: 0"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "glob", Arguments: json.RawMessage(`{"dir":"loot","pattern":"*.md"}`)}}

	if got := cachedEmptyGlobResultForCall(run, tc); got != "" {
		t.Fatalf("changed glob pattern should not hit empty cache:\n%s", got)
	}
}

func TestCachedEmptyGlobResultInvalidatedByFileMutation(t *testing.T) {
	input := `{"pattern":"*.txt","dir":"loot"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "glob", Status: "done", Input: input, Result: "[glob_result]\nstate: empty\nmatches: 0"},
		{Name: "write", Status: "done", Input: `{"path":"loot/new.txt","content":"new"}`, Result: "wrote"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "glob", Arguments: json.RawMessage(input)}}

	if got := cachedEmptyGlobResultForCall(run, tc); got != "" {
		t.Fatalf("empty glob cache must be invalidated after file creation, got %q", got)
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
