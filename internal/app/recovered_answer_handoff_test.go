package app

import (
	"strings"
	"testing"
)

func TestCanHandOffRecoveredReadOnlyAnswer(t *testing.T) {
	run := TaskRun{
		Prompt:     "Can you tell me any issues in the domains from my files?",
		StopReason: "loop_circuit_breaker",
		Tools: []TaskToolEvent{
			{Name: "read", Status: "done", Result: "TLS 1.0 enabled"},
			{Name: "read", Status: "cached", Result: "[cached_tool_result] TLS 1.0 enabled"},
		},
	}
	summary := "## Findings\n\nTLS 1.0 was reported in the supplied scan files.\n\n## Caveats\n\nConfirm the scanner result manually. If you want, I can produce a consolidated report."
	if !canHandOffRecoveredAnswer(run, summary) {
		t.Fatal("evidence-backed read-only recovery answer should be deliverable")
	}
	prompt := recoveryReportPrompt(run)
	for _, want := range []string{"Answer handoff mode", "Answer the user's original request directly", "Do not lead with \"What failed\""} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("handoff prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRecoveredAnswerHandoffRejectsMutationAndJunk(t *testing.T) {
	run := TaskRun{
		Prompt:     "Fix the issues in these files",
		StopReason: "loop_circuit_breaker",
		Tools: []TaskToolEvent{
			{Name: "read", Status: "done", Result: "evidence"},
			{Name: "write", Status: "done", Result: "changed file"},
		},
	}
	if canHandOffRecoveredAnswer(run, "Changes complete.") {
		t.Fatal("mutation run must not be relabelled as a recovered read-only answer")
	}
	run.Prompt = "Can you tell me what is in this file?"
	run.Tools = run.Tools[:1]
	if canHandOffRecoveredAnswer(run, "...") {
		t.Fatal("junk summary must not be relabelled as a recovered answer")
	}
}
