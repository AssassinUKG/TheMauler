package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestAgentEvalScoring(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "ok.txt"), []byte("hello fixed world"), 0o640); err != nil {
		t.Fatal(err)
	}
	run := TaskRun{
		Status:     "done",
		DurationMs: 25,
		Tools: []TaskToolEvent{
			{Name: "read_file", Status: "done", Result: "hello"},
			{Name: "edit_file", Status: "done", Result: "updated"},
		},
		Events: []TaskRunEvent{
			{Kind: "continue"},
			{Kind: "truncated"},
			{Kind: "tool_error"},
		},
	}
	scenario := AgentEvalScenario{
		ExpectStatus:     "done",
		ExpectFiles:      map[string]string{"ok.txt": "fixed"},
		ForbidSubstr:     []string{"SECRET"},
		MaxAutoContinues: 1,
	}
	var result AgentEvalResult

	scoreAgentEvalResult(&result, run, workspace, scenario)

	if !result.Pass {
		t.Fatalf("expected pass, got %q", result.FailReason)
	}
	if result.ToolCalls != 2 || result.AutoContinues != 1 || result.Truncations != 1 || result.ToolErrors != 1 {
		t.Fatalf("bad counters: %#v", result)
	}
}

func TestAgentEvalScoringFailures(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "ok.txt"), []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	run := TaskRun{
		Status: "done",
		Tools:  []TaskToolEvent{{Name: "shell", Status: "done", Result: "SECRET=abc"}},
		Events: []TaskRunEvent{{Kind: "continue"}, {Kind: "continue"}},
	}
	scenario := AgentEvalScenario{
		ExpectStatus:     "stopped",
		ExpectFiles:      map[string]string{"ok.txt": "new"},
		ForbidSubstr:     []string{"SECRET"},
		MaxAutoContinues: 1,
	}
	var result AgentEvalResult

	scoreAgentEvalResult(&result, run, workspace, scenario)

	if result.Pass {
		t.Fatalf("expected fail")
	}
	for _, want := range []string{"status=", "missing expected substring", "forbidden", "auto_continues"} {
		if !strings.Contains(result.FailReason, want) {
			t.Fatalf("fail reason missing %q: %s", want, result.FailReason)
		}
	}
}

func TestLoadAgentEvalScenarios(t *testing.T) {
	scenarios, err := loadAgentEvalScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(scenarios) < 6 {
		t.Fatalf("scenario count = %d, want at least 6", len(scenarios))
	}
	seen := map[string]bool{}
	for _, scenario := range scenarios {
		seen[scenario.Name] = true
		if strings.TrimSpace(scenario.Prompt) == "" {
			t.Fatalf("scenario %q has empty prompt", scenario.Name)
		}
	}
	for _, name := range []string{"read-and-summarize", "edit-then-verify", "chunked-write", "duplicate-read-guard", "grep-then-edit", "stop-cleanly-on-budget"} {
		if !seen[name] {
			t.Fatalf("missing seed scenario %q", name)
		}
	}
}

func TestAgentEvalSmokeWithMockClient(t *testing.T) {
	restoreWorkingDir(t)
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return &agentEvalMockClient{}, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })

	cfg := settings.DefaultSettings()
	cfg.ActiveProfile = "mock"
	cfg.Tools.ActiveToolset = "unrestricted"
	cfg.Tools.ConfirmWrites = false
	cfg.Tools.ConfirmExec = false
	cfg.Agents.MaxToolCalls = 8
	cfg.Agents.MaxRunSeconds = 0
	cfg.Context.CompactionAt = 0.99
	profile := settings.Profile{
		Name:      "mock",
		Provider:  "mock",
		ModelID:   "mock-model",
		Backend:   "mock",
		CtxTokens: 8192,
		NoThink:   settings.GenerationParams{MaxTokens: 256, TopP: 1},
	}
	profiles := settings.ProfilesFile{
		Providers: map[string]settings.Provider{"mock": {Name: "mock", Backend: "mock"}},
		Profiles:  map[string]settings.Profile{"mock": profile},
	}

	evalApp := &App{cfg: &cfg, profiles: &profiles}
	report := evalApp.runAgentEvalScenarios("mock", []AgentEvalScenario{{
		Name:   "edit-then-verify",
		Prompt: "Fix the compile error in main.go.",
		Workspace: map[string]string{
			"main.go": "package main\n\nfunc broken() string {\n\treturn 123\n}\n",
		},
		Mode:             "Fixer",
		MaxToolCalls:     8,
		ExpectStatus:     "done",
		ExpectFiles:      map[string]string{"main.go": "return \"123\""},
		MaxAutoContinues: 1,
	}})

	if report.Total != 1 || report.PassCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}

type agentEvalMockClient struct {
	turn int
}

func (c *agentEvalMockClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	c.turn++
	go func(turn int) {
		defer close(ch)
		if turn == 1 {
			args, _ := json.Marshal(map[string]string{
				"path":       "main.go",
				"old_string": "return 123",
				"new_string": "return \"123\"",
			})
			ch <- llm.Delta{ToolCalls: []llm.ToolCallDef{{
				ID:   "call-edit",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "edit_file",
					Arguments: args,
				},
			}}}
			return
		}
		ch <- llm.Delta{Content: "Fixed main.go and verified the edit."}
	}(c.turn)
	return ch, nil
}

func (c *agentEvalMockClient) Models(context.Context) ([]string, error) {
	return []string{"mock-model"}, nil
}
func (c *agentEvalMockClient) Ping(context.Context) error { return nil }
func (c *agentEvalMockClient) Name() string               { return "mock-agent-eval" }
