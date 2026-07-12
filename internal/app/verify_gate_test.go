package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestVerifyGateDetectsGoProject(t *testing.T) {
	withTempWorkingDir(t)
	mustWriteFile(t, "go.mod", "module example.com/verifygate\n\ngo 1.23\n")

	commands := detectVerifyCommands(settings.DefaultSettings())

	if len(commands) != 3 {
		t.Fatalf("commands = %#v, want build/vet/test", commands)
	}
	for i, want := range []string{"go build ./...", "go vet ./...", "go test ./..."} {
		if commands[i].Command != want {
			t.Fatalf("command %d = %q, want %q", i, commands[i].Command, want)
		}
	}
}

func TestVerifyGateParsesPassFail(t *testing.T) {
	pass := verifyVerdictFromCommand(verifyCommand{Gate: "build", Command: "go build ./..."}, "ok", nil)
	if pass.Status != "pass" || pass.Blocking {
		t.Fatalf("pass verdict = %#v", pass)
	}

	fail := verifyVerdictFromCommand(verifyCommand{Gate: "build", Command: "go build ./..."}, "compile failed", os.ErrInvalid)
	if fail.Status != "fail" || !fail.Blocking || len(fail.Improvements) == 0 {
		t.Fatalf("fail verdict = %#v", fail)
	}
}

func TestVerifyGateBlockingSplit(t *testing.T) {
	tests := []struct {
		gate string
		want bool
	}{
		{gate: "build", want: true},
		{gate: "test", want: true},
		{gate: "lint", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.gate, func(t *testing.T) {
			got := verifyVerdictFromCommand(verifyCommand{Gate: tc.gate, Command: tc.gate}, "failed", os.ErrInvalid)
			if got.Blocking != tc.want {
				t.Fatalf("blocking = %v, want %v: %#v", got.Blocking, tc.want, got)
			}
		})
	}
}

func TestVerifyGateIsInconclusiveForUnknownProject(t *testing.T) {
	withTempWorkingDir(t)
	cfg := settings.DefaultSettings()
	run := startTaskRun("add a helper", "Builder", "profile", "model")

	verdicts := (&App{}).runVerifyGate(context.Background(), &run, &cfg)

	if len(verdicts) != 1 || verdicts[0].Status != "inconclusive" || !verdicts[0].Blocking {
		t.Fatalf("verdicts = %#v, want one blocking inconclusive result", verdicts)
	}
}

func TestVerifyGateRespectsTimeout(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Tools.ShellBackend = "auto"
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"sleep 5"}
	cfg.Agents.ReviewLoop.VerifyTimeoutSec = 1
	run := startTaskRun("fix the tests", "Builder", "profile", "model")
	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done"})

	verdicts := (&App{}).runVerifyGate(context.Background(), &run, &cfg)

	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %#v, want one", verdicts)
	}
	if verdicts[0].Status != "error" || !verdicts[0].Blocking {
		t.Fatalf("timeout verdict = %#v, want blocking error", verdicts[0])
	}
}

func TestVerifyGateSkipsNonCodingRun(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"go test ./..."}
	run := startTaskRun("research the repo and summarize it", "Researcher", "profile", "model")

	verdicts := (&App{}).runVerifyGate(context.Background(), &run, &cfg)

	if len(verdicts) != 0 {
		t.Fatalf("non-gateable run returned verdicts: %#v", verdicts)
	}
}

func withTempWorkingDir(t *testing.T) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	return dir
}

func mustWriteFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil && filepath.Dir(name) != "." {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	if err := os.WriteFile(name, []byte(strings.ReplaceAll(content, "\r\n", "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
