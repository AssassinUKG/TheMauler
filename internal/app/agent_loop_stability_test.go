package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func readToolCall(name, args string) llm.ToolCallDef {
	return llm.ToolCallDef{
		Function: llm.FunctionCall{Name: name, Arguments: json.RawMessage(args)},
	}
}

func TestCanonicalToolArgsIgnoresKeyOrderAndWhitespace(t *testing.T) {
	a := canonicalToolArgs(json.RawMessage(`{"path":"a.go","limit":10}`))
	b := canonicalToolArgs(json.RawMessage(`{ "limit": 10 , "path": "a.go" }`))
	if a == "" || a != b {
		t.Fatalf("canonical forms should match: %q vs %q", a, b)
	}
}

func TestCanonicalToolArgsFallsBackForNonObject(t *testing.T) {
	if got := canonicalToolArgs(json.RawMessage(`  "raw"  `)); got != `"raw"` {
		t.Fatalf("non-object args should be compared literally, got %q", got)
	}
	if got := canonicalToolArgs(json.RawMessage("   ")); got != "" {
		t.Fatalf("empty args should yield empty key, got %q", got)
	}
}

func TestNormalizeShellCommandForDedup(t *testing.T) {
	base := "curl -s --max-time 5 http://10.129.26.26/"
	variants := []string{
		base,
		base + " | head -30",
		base + " | head -100",
		base + " | tail -n 20",
		base + " | cat",
	}
	want := normalizeShellCommandForDedup(base)
	for _, variant := range variants {
		if got := normalizeShellCommandForDedup(variant); got != want {
			t.Fatalf("normalizeShellCommandForDedup(%q) = %q, want %q", variant, got, want)
		}
	}
	if got := normalizeShellCommandForDedup("curl http://10.129.26.27/ | head -30"); got == want {
		t.Fatalf("different target URL must not collide: %q", got)
	}
	if got := normalizeShellCommandForDedup("nmap -sV 10.129.26.26"); got != "nmap -sV 10.129.26.26" {
		t.Fatalf("non-pager command changed: %q", got)
	}
}

func TestCanonicalToolArgsNormalizesCommandPagerOnlyTail(t *testing.T) {
	a := canonicalToolArgs(json.RawMessage(`{"command":"curl http://x/ | head -30","timeout":5}`))
	b := canonicalToolArgs(json.RawMessage(`{"timeout":5,"command":"curl http://x/ | head -100"}`))
	if a == "" || a != b {
		t.Fatalf("canonical command args should ignore pager-only tails: %q vs %q", a, b)
	}
}

func TestIdempotentReadKeyOnlyTracksReadTools(t *testing.T) {
	if idempotentReadKey("write", json.RawMessage(`{"path":"a"}`)) != "" {
		t.Fatal("write must not be tracked as an idempotent read")
	}
	if idempotentReadKey("read", json.RawMessage(`{"path":"a"}`)) == "" {
		t.Fatal("read should produce a key")
	}
	if idempotentReadKey("read", json.RawMessage("")) != "" {
		t.Fatal("missing args (logging off) must produce no key")
	}
}

func TestRepeatedIdenticalReadBlockFiresOnThirdCall(t *testing.T) {
	args := `{"path":"main.go"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Input: args, Status: "done"},
		{Name: "read", Input: `{ "path": "main.go" }`, Status: "done"}, // same call, reformatted
	}}
	tc := readToolCall("read", args)
	msg := repeatedIdenticalReadBlock(run, tc)
	if msg == "" || !strings.Contains(msg, "read cache hit") {
		t.Fatalf("third identical read should be blocked, got %q", msg)
	}
}

func TestRepeatedIdenticalReadBlockAllowsSecondCall(t *testing.T) {
	args := `{"path":"main.go"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Input: args, Status: "done"},
	}}
	if msg := repeatedIdenticalReadBlock(run, readToolCall("read", args)); msg != "" {
		t.Fatalf("a single re-read (e.g. after compaction) must be allowed, got %q", msg)
	}
}

func TestRepeatedIdenticalReadBlockDistinguishesArgs(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Input: `{"path":"a.go"}`, Status: "done"},
		{Name: "read", Input: `{"path":"a.go"}`, Status: "done"},
	}}
	if msg := repeatedIdenticalReadBlock(run, readToolCall("read", `{"path":"b.go"}`)); msg != "" {
		t.Fatalf("a read of a different path must not be blocked, got %q", msg)
	}
}

func TestRepeatedIdenticalReadRecoveryKeepsRunRecoveringAfterReadSkip(t *testing.T) {
	args := `{"path":"main.go"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read", Input: args, Status: "done", Result: "package main\n"},
		{Name: "read", Input: args, Status: "done", Result: "package main\n"},
		{Name: "read", Input: args, Status: "skipped", Result: "read cache hit: ...\nRecovery: this repeated command was skipped without stopping the run."},
	}}
	decision := evaluatePreToolRecoveryPolicy(run, readToolCall("read", args))
	if decision.HardStop || decision.StopReason != "" || decision.ToolStatus != "skipped" || decision.RunState != "recovering" {
		t.Fatalf("duplicate successful reads should keep recovering, got %#v", decision)
	}
	if !strings.Contains(decision.Message, "Cached result preview") {
		t.Fatalf("duplicate read should return cached evidence, got %q", decision.Message)
	}
}

func TestToolChoiceForRequiresToolOnInspectionFirstTurn(t *testing.T) {
	if got := toolChoiceFor("look at the repo and explore the codebase", 0, 0); got != "required" {
		t.Fatalf("inspection first turn should force a tool call, got %q", got)
	}
	if got := toolChoiceFor("enumerate the HTB target and get user flag", 0, 0); got != "required" {
		t.Fatalf("operational first turn should force a tool call, got %q", got)
	}
}

func TestToolChoiceForStaysAutoMidTaskAndConversational(t *testing.T) {
	if got := toolChoiceFor("look at the repo", 0, 3); got != "auto" {
		t.Fatalf("mid-task turns must stay auto, got %q", got)
	}
	if got := toolChoiceFor("hi, how are you?", 0, 0); got != "none" {
		t.Fatalf("conversational opener should be none, got %q", got)
	}
}

func TestModelParamBillions(t *testing.T) {
	cases := map[string]float64{
		"qwen3.6-27b":                      27,
		"gemma-4-26B-A4B-it-QAT-Q4_0.gguf": 26, // MoE: total, not active 4
		"some-model-8b-instruct":           8,
		"qwen3-2b":                         2,
		"no-size-here":                     0,
		"llama-3.1-q4_k_m":                 0, // quant token must not match
	}
	for id, want := range cases {
		if got := modelParamBillions(id); got != want {
			t.Errorf("modelParamBillions(%q) = %g, want %g", id, got, want)
		}
	}
}

func TestMostRepeatedScriptInvocation(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Input: `{"command":"python3 /tmp/x/exploit.py --command id"}`},
		{Name: "shell", Input: `{"command":"python3 /tmp/x/exploit.py --command 'ls -la'"}`},
		{Name: "shell", Input: `{"command":"python3 /tmp/x/exploit.py --command whoami"}`},
		{Name: "shell", Input: `{"command":"cat /etc/passwd"}`},
		{Name: "shell", Input: `{"command":"python3 /tmp/x/exploit.py --command 'find / -perm -4000'"}`},
	}}
	sig, n := mostRepeatedScriptInvocation(run)
	if sig != "/tmp/x/exploit.py" || n != 4 {
		t.Fatalf("got sig=%q n=%d, want /tmp/x/exploit.py 4", sig, n)
	}
	if _, n := mostRepeatedScriptInvocation(TaskRun{Tools: []TaskToolEvent{{Name: "shell", Input: `{"command":"ls"}`}}}); n != 0 {
		t.Fatalf("expected 0 for no scripts, got %d", n)
	}
}
