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

func TestIdempotentReadKeyOnlyTracksReadTools(t *testing.T) {
	if idempotentReadKey("write_file", json.RawMessage(`{"path":"a"}`)) != "" {
		t.Fatal("write_file must not be tracked as an idempotent read")
	}
	if idempotentReadKey("read_file", json.RawMessage(`{"path":"a"}`)) == "" {
		t.Fatal("read_file should produce a key")
	}
	if idempotentReadKey("read_file", json.RawMessage("")) != "" {
		t.Fatal("missing args (logging off) must produce no key")
	}
}

func TestRepeatedIdenticalReadBlockFiresOnThirdCall(t *testing.T) {
	args := `{"path":"main.go"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read_file", Input: args, Status: "done"},
		{Name: "read_file", Input: `{ "path": "main.go" }`, Status: "done"}, // same call, reformatted
	}}
	tc := readToolCall("read_file", args)
	msg := repeatedIdenticalReadBlock(run, tc)
	if msg == "" || !strings.Contains(msg, "read_file skipped") {
		t.Fatalf("third identical read should be blocked, got %q", msg)
	}
}

func TestRepeatedIdenticalReadBlockAllowsSecondCall(t *testing.T) {
	args := `{"path":"main.go"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read_file", Input: args, Status: "done"},
	}}
	if msg := repeatedIdenticalReadBlock(run, readToolCall("read_file", args)); msg != "" {
		t.Fatalf("a single re-read (e.g. after compaction) must be allowed, got %q", msg)
	}
}

func TestRepeatedIdenticalReadBlockDistinguishesArgs(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read_file", Input: `{"path":"a.go"}`, Status: "done"},
		{Name: "read_file", Input: `{"path":"a.go"}`, Status: "done"},
	}}
	if msg := repeatedIdenticalReadBlock(run, readToolCall("read_file", `{"path":"b.go"}`)); msg != "" {
		t.Fatalf("a read of a different path must not be blocked, got %q", msg)
	}
}

func TestRepeatedPreToolRecoveryIgnoredEscalatesAfterReadSkip(t *testing.T) {
	args := `{"path":"main.go"}`
	// A prior skip carrying the soft-recovery suffix means the model already got
	// the nudge and repeated the call — the next attempt should hard-stop.
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "read_file", Input: args, Status: "skipped", Result: "read_file skipped: ...\nRecovery: this repeated command was skipped without stopping the run."},
	}}
	if !repeatedPreToolRecoveryIgnored(run, readToolCall("read_file", args)) {
		t.Fatal("an ignored duplicate-read nudge should escalate to a hard stop")
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
		"qwen3.6-27b":                       27,
		"gemma-4-26B-A4B-it-QAT-Q4_0.gguf":  26, // MoE: total, not active 4
		"some-model-8b-instruct":            8,
		"qwen3-2b":                          2,
		"no-size-here":                      0,
		"llama-3.1-q4_k_m":                  0, // quant token must not match
	}
	for id, want := range cases {
		if got := modelParamBillions(id); got != want {
			t.Errorf("modelParamBillions(%q) = %g, want %g", id, got, want)
		}
	}
}
