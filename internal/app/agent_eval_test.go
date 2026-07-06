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
			{Name: "read", Status: "done", Result: "hello"},
			{Name: "edit", Status: "done", Result: "updated"},
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
	if !result.StatusPass || !result.ArtifactPass || !result.HygienePass {
		t.Fatalf("expected all pass dimensions, got %#v", result)
	}
	if result.ToolCalls != 2 || result.AutoContinues != 1 || result.Truncations != 1 || result.ToolErrors != 1 {
		t.Fatalf("bad counters: %#v", result)
	}
	if result.ToolSuccessRate != 100 || result.RepeatToolRate != 0 || result.FalseDone {
		t.Fatalf("bad reliability counters: %#v", result)
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
	if result.StatusPass || result.ArtifactPass || result.HygienePass {
		t.Fatalf("expected all pass dimensions to fail, got %#v", result)
	}
	if !result.FalseDone {
		t.Fatalf("done run with missing expected artifact should be marked false_done: %#v", result)
	}
	for _, want := range []string{"status=", "missing expected substring", "forbidden", "auto_continues", "false_done"} {
		if !strings.Contains(result.FailReason, want) {
			t.Fatalf("fail reason missing %q: %s", want, result.FailReason)
		}
	}
}

func TestAgentEvalReliabilityCounters(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "ok.txt"), []byte("fixed"), 0o640); err != nil {
		t.Fatal(err)
	}
	input := `{"command":"curl http://target/"}`
	run := TaskRun{
		Status: "done",
		Tools: []TaskToolEvent{
			{Name: "shell", Input: input, Status: "done", Result: "ok"},
			{Name: "shell", Input: input, Status: "skipped", Result: "Cached result preview"},
			{Name: "grep", Input: `{"pattern":"x"}`, Status: "error", Result: "bad"},
		},
		Events: []TaskRunEvent{
			{Kind: "verifier_required"},
			{Kind: "prompt_budget", Message: "warn over 20"},
			{Kind: "tool_routing", Detail: "tool_count=31"},
		},
	}
	scenario := AgentEvalScenario{
		ExpectStatus:     "done",
		ExpectFiles:      map[string]string{"ok.txt": "fixed"},
		MaxAutoContinues: 1,
	}
	var result AgentEvalResult

	scoreAgentEvalResult(&result, run, workspace, scenario)

	if result.ToolSuccessRate != 66 {
		t.Fatalf("tool success rate = %d, want 66: %#v", result.ToolSuccessRate, result)
	}
	if result.RepeatedToolInputs != 1 || result.RepeatedSkips != 1 || result.RepeatToolRate != 66 {
		t.Fatalf("bad repeat counters: %#v", result)
	}
	if result.VerifierPrompts != 1 || result.MaxRoutedTools != 31 || result.PromptWarnings != 1 {
		t.Fatalf("bad routing/verifier counters: %#v", result)
	}
	if result.HygienePass || !strings.Contains(result.FailReason, "max_routed_tools=31") {
		t.Fatalf("expected reliability hygiene failure, got %#v", result)
	}
}

func TestAgentEvalScoringSeparatesArtifactAndHygiene(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "ok.txt"), []byte("fixed"), 0o640); err != nil {
		t.Fatal(err)
	}
	run := TaskRun{
		Status: "done",
		Events: []TaskRunEvent{
			{Kind: "continue"},
			{Kind: "continue"},
			{Kind: "continue"},
		},
	}
	scenario := AgentEvalScenario{
		ExpectStatus:     "done",
		ExpectFiles:      map[string]string{"ok.txt": "fixed"},
		MaxAutoContinues: 1,
	}
	var result AgentEvalResult

	scoreAgentEvalResult(&result, run, workspace, scenario)

	if result.Pass || !result.StatusPass || !result.ArtifactPass || result.HygienePass {
		t.Fatalf("expected correct artifact/status but hygiene fail, got %#v", result)
	}
	if !strings.Contains(result.FailReason, "auto_continues") {
		t.Fatalf("missing hygiene fail reason: %q", result.FailReason)
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
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
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

func TestAgentEvalReportPersistence(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	report := AgentEvalReport{
		ID:        "agent-eval-test",
		CreatedAt: "2026-06-30T00:00:00Z",
		Profile:   "mock",
		PassCount: 1,
		Total:     1,
		Results:   []AgentEvalResult{{Name: "case", Pass: true, Status: "done"}},
	}
	if err := saveAgentEvalReport(report); err != nil {
		t.Fatalf("save eval report: %v", err)
	}
	reports, err := loadAgentEvalReports()
	if err != nil {
		t.Fatalf("load eval reports: %v", err)
	}
	if len(reports) != 1 || reports[0].ID != "agent-eval-test" || reports[0].Results[0].Name != "case" {
		t.Fatalf("reports = %#v", reports)
	}
	if err := (&App{}).ClearAgentEvalReports(); err != nil {
		t.Fatalf("clear eval reports: %v", err)
	}
	reports, err = loadAgentEvalReports()
	if err != nil {
		t.Fatalf("load after clear: %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("reports after clear = %#v", reports)
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
				"path": "main.go",
				"old":  "return 123",
				"new":  "return \"123\"",
			})
			ch <- llm.Delta{ToolCalls: []llm.ToolCallDef{{
				ID:   "call-edit",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "edit",
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
