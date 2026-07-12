package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestReviewPhaseProceedsWhenAllPass(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyGate = false
	cfg.Agents.ReviewLoop.ReviewerPass = false
	run := gateableReviewRun()
	cycles := 0

	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, AgentMode{Name: "Builder"}, true, &cycles, false)

	if !decision.Proceed || decision.InjectedPrompt != "" || cycles != 0 {
		t.Fatalf("decision = %#v cycles=%d, want proceed without injection", decision, cycles)
	}
}

func TestReviewPhaseLoopsOnBlockingVerdict(t *testing.T) {
	withTempWorkingDir(t)
	mustWriteFile(t, "go.mod", "module example.com/reviewphase\n\ngo 1.23\n")
	mustWriteFile(t, "main.go", "package main\n\nfunc broken() string { return 123 }\n")
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"go build ./..."}
	cfg.Agents.ReviewLoop.ReviewerPass = false
	run := gateableReviewRun()
	cycles := 0

	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, AgentMode{Name: "Builder"}, true, &cycles, false)

	if decision.Proceed || cycles != 1 {
		t.Fatalf("decision = %#v cycles=%d, want retry", decision, cycles)
	}
	for _, want := range []string{"[review_gate:failed]", "next_tool: edit", "go build ./...", "do_not_repeat"} {
		if !strings.Contains(decision.InjectedPrompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, decision.InjectedPrompt)
		}
	}
}

func TestReviewPhaseRespectsMaxCycles(t *testing.T) {
	withTempWorkingDir(t)
	mustWriteFile(t, "go.mod", "module example.com/reviewphasecap\n\ngo 1.23\n")
	mustWriteFile(t, "main.go", "package main\n\nfunc broken() string { return 123 }\n")
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.MaxReviewCycles = 0
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"go build ./..."}
	cfg.Agents.ReviewLoop.ReviewerPass = false
	run := gateableReviewRun()
	cycles := 0

	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, AgentMode{Name: "Builder"}, true, &cycles, false)

	if !decision.Proceed || decision.StopReason != "review_incomplete" || decision.SummaryNote == "" {
		t.Fatalf("decision = %#v, want honest capped stop", decision)
	}
}

func TestReviewPhaseSkipsNonGateableRun(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"go build ./..."}
	run := startTaskRun("map this repo", "Researcher", "profile", "model")
	cycles := 0

	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, AgentMode{Name: "Researcher"}, true, &cycles, false)

	if !decision.Proceed || cycles != 0 || len(run.Events) != 0 {
		t.Fatalf("decision = %#v cycles=%d events=%#v, want skip", decision, cycles, run.Events)
	}
}

func TestReviewPhaseLoopsOnBlockingCompletionRail(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyGate = false
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	cfg.Agents.ReviewLoop.ReviewerPass = false
	run := startTaskRun("add min and max helpers", "Builder", "profile", "model")
	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"math.go"}`, Result: "updated min helper"})
	cycles := 0

	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, AgentMode{Name: "Builder"}, true, &cycles, false)

	if decision.Proceed || cycles != 1 || !strings.Contains(decision.InjectedPrompt, "max") {
		t.Fatalf("decision = %#v cycles=%d, want completion retry for max", decision, cycles)
	}
}

func TestReviewPhaseLoopsOnReviewerRequestChanges(t *testing.T) {
	restore := mockReviewerClient(t, &reviewerMockClient{
		responses: []reviewerMockResponse{
			{text: "reviewed packet"},
			{text: `{"verdict":"request_changes","severity":"blocking","items":["add nil-input test"]}`},
		},
	})
	defer restore()
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyGate = false
	cfg.Agents.ReviewLoop.CompletionRails = false
	run := gateableReviewRun()
	cycles := 0

	decision := ((*App)(nil)).runReviewPhase(context.Background(), &run, reviewerTestProfile(), &cfg, AgentMode{Name: "Builder"}, true, &cycles, false)

	if decision.Proceed || cycles != 1 || !strings.Contains(decision.InjectedPrompt, "nil-input") {
		t.Fatalf("decision = %#v cycles=%d, want reviewer retry", decision, cycles)
	}
}

func TestReviewPhaseStopsWhenReviewerIsInconclusive(t *testing.T) {
	restore := mockReviewerClient(t, &reviewerMockClient{
		responses: []reviewerMockResponse{
			{text: "not a verdict"}, {text: "still malformed"},
			{text: "not a verdict"}, {text: "still malformed"},
		},
	})
	defer restore()
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyGate = false
	cfg.Agents.ReviewLoop.CompletionRails = false
	run := gateableReviewRun()
	cycles := 0

	decision := ((*App)(nil)).runReviewPhase(context.Background(), &run, reviewerTestProfile(), &cfg, AgentMode{Name: "Builder"}, true, &cycles, false)

	if !decision.Proceed || decision.StopReason != "review_incomplete" || cycles != 0 || !strings.Contains(decision.StopDetail, "malformed") {
		t.Fatalf("decision = %#v cycles=%d, want explicit inconclusive stop", decision, cycles)
	}
}

func TestReviewPhaseHonorsBudgetExhaustion(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"go build ./..."}
	run := gateableReviewRun()
	cycles := 0

	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, AgentMode{Name: "Builder"}, true, &cycles, true)

	if !decision.Proceed || cycles != 0 {
		t.Fatalf("decision = %#v cycles=%d, want budget skip", decision, cycles)
	}
}

func TestReviewPhaseInjectsOneConsolidatedMessage(t *testing.T) {
	blocking := []VerifyVerdict{
		{Gate: "build", Status: "fail", Blocking: true, Summary: "build failed", Improvements: []string{"fix build"}},
		{Gate: "test", Status: "fail", Blocking: true, Summary: "test failed", Improvements: []string{"fix tests"}},
	}

	prompt := buildReviewGateFailurePrompt(blocking, 1, 2)

	if strings.Count(prompt, "[review_gate:failed]") != 1 {
		t.Fatalf("prompt should have one header:\n%s", prompt)
	}
	for _, want := range []string{"build failed", "fix build", "test failed", "fix tests"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func gateableReviewRun() TaskRun {
	run := startTaskRun("fix the compile error", "Builder", "profile", "model")
	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done"})
	return run
}

func TestReviewPhaseOnlyAutonomous(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"go build ./..."}
	run := gateableReviewRun()
	cycles := 0

	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, AgentMode{Name: "Builder"}, false, &cycles, false)

	if !decision.Proceed || cycles != 0 {
		t.Fatalf("decision = %#v cycles=%d, want non-autonomous skip", decision, cycles)
	}
}

func TestBlockingReviewVerdicts(t *testing.T) {
	verdicts := []VerifyVerdict{
		{Status: "pass", Blocking: true},
		{Status: "skip", Blocking: true},
		{Status: "fail", Blocking: false},
		{Status: "error", Blocking: true},
	}

	got := blockingReviewVerdicts(verdicts)

	if len(got) != 1 || got[0].Status != "error" {
		t.Fatalf("blocking verdicts = %#v", got)
	}
}

func TestReviewPhaseRecordsReviewGateEvent(t *testing.T) {
	run := gateableReviewRun()

	recordReviewGateEvent(&run, 1, []VerifyVerdict{{Gate: "build", Status: "pass"}})

	if len(run.Events) != 1 || run.Events[0].Kind != "review_gate" || !strings.Contains(run.Events[0].Detail, `"gate": "build"`) {
		t.Fatalf("events = %#v", run.Events)
	}
}

func TestGateForVerifyCommand(t *testing.T) {
	for command, want := range map[string]string{
		"go test ./...":      "test",
		"go vet ./...":       "lint",
		"npm run build":      "build",
		"custom check thing": "verify",
	} {
		if got := gateForVerifyCommand(command); got != want {
			t.Fatalf("gateForVerifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestTrimEvidence(t *testing.T) {
	short := "hello"
	if trimEvidence(short) != short {
		t.Fatalf("short evidence changed")
	}
	long := strings.Repeat("x", 5000)
	if got := trimEvidence(long); len(got) >= len(long) || !strings.Contains(got, "...") {
		t.Fatalf("long evidence not trimmed: len=%d", len(got))
	}
}

func TestNPMVerifyScripts(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.WriteFile("package.json", []byte(`{"scripts":{"build":"vite build","test":"vitest"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	commands := npmVerifyScripts("package.json")

	if len(commands) != 2 || commands[0].Command != "npm run build" || commands[1].Command != "npm test" {
		t.Fatalf("commands = %#v", commands)
	}
}
