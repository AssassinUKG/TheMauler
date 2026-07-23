package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/channelbus"
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

func TestAgentEvalScoringEnforcesToolDisciplineAndOptionalCaseInsensitiveArtifact(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "math.go"), []byte("func Max() {}\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	run := TaskRun{
		Status: "done",
		Tools: []TaskToolEvent{{
			Name: "shell", Status: "done", Input: `{"command":"curl http://10.129.14.129"}`,
		}},
	}
	scenario := AgentEvalScenario{
		ExpectStatus:               "done",
		ExpectFiles:                map[string]string{"math.go": "max"},
		ExpectFilesCaseInsensitive: true,
		ForbidTools:                []string{"shell"},
		ForbidToolInputSubstr:      []string{"10.129.14.129"},
		MaxAutoContinues:           1,
	}
	var result AgentEvalResult
	scoreAgentEvalResult(&result, run, workspace, scenario)
	if !result.ArtifactPass || result.HygienePass || result.Pass {
		t.Fatalf("discipline scoring = %#v", result)
	}
	if !strings.Contains(result.FailReason, "forbidden tool used") || !strings.Contains(result.FailReason, "forbidden substring") {
		t.Fatalf("discipline failure reason = %q", result.FailReason)
	}
}

func TestCanonicalAgentEvalSettingsIgnoreLivePolicyAndReviewerOverrides(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.MaxRunSeconds = 1
	cfg.Agents.ReviewLoop.ReviewerPass = true
	cfg.Agents.ReviewLoop.VerifyCommands = []string{"false"}
	cfg.Tools.Enabled = false
	cfg.Tools.ActiveToolset = "offline"
	cfg.Tools.ConfirmWrites = true
	cfg.Tools.EnabledTools = map[string]bool{"read": false}

	got := canonicalAgentEvalSettings(cfg)
	if got.Agents.MaxRunSeconds != 0 || got.Agents.ReviewLoop.ReviewerPass || len(got.Agents.ReviewLoop.VerifyCommands) != 0 {
		t.Fatalf("agent envelope was not canonicalized: %#v", got.Agents)
	}
	if !got.Tools.Enabled || got.Tools.ActiveToolset != "unrestricted" || got.Tools.ConfirmWrites || !got.Tools.EnabledTools["read"] {
		t.Fatalf("tool envelope was not canonicalized: %#v", got.Tools)
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
	cfg.Agents.ReviewLoop.VerifyGate = false
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
	reviewerPass := false
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
		ReviewerPass:     &reviewerPass,
	}})

	if report.Total != 1 || report.PassCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestAgentEvalPreflightBlocksLiveRunWithoutChangingWorkingDir(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	app := &App{cfg: &cfg, agentRunning: true}
	report := app.runAgentEvalScenarios("mock", []AgentEvalScenario{{Name: "must-not-run", Prompt: "write a file"}})
	if report.Total != 1 || len(report.Results) != 1 || report.Results[0].Status != "blocked" {
		t.Fatalf("expected blocked preflight, got %#v", report)
	}
	if !strings.Contains(report.Results[0].FailReason, "task or artifact is active") {
		t.Fatalf("unexpected preflight reason: %q", report.Results[0].FailReason)
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("eval preflight changed cwd: before=%q after=%q", before, after)
	}
	if app.evalRunning {
		t.Fatal("blocked eval left evalRunning set")
	}
}

func TestAgentEvalPreflightBlocksQueuedChannelWork(t *testing.T) {
	cfg := settings.DefaultSettings()
	queue := channelbus.NewQueue()
	queue.Enqueue(channelbus.Envelope{Source: "test", Text: "queued work"}, channelbus.Route{Lane: "work"})
	app := &App{cfg: &cfg, channelQueue: queue}
	report := app.runAgentEvalScenarios("mock", []AgentEvalScenario{{Name: "must-not-run", Prompt: "write a file"}})
	if len(report.Results) != 1 || report.Results[0].Status != "blocked" || !strings.Contains(report.Results[0].FailReason, "channel work queue") {
		t.Fatalf("expected queued-work block, got %#v", report)
	}
	if app.evalRunning {
		t.Fatal("blocked eval left evalRunning set")
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

func TestAgentEvalRepeatedReportMetrics(t *testing.T) {
	report := AgentEvalReport{
		Repeats:      5,
		FixtureCount: 2,
		Total:        10,
		PassCount:    9,
		Results: []AgentEvalResult{
			{Pass: true, ToolCalls: 4, RepeatedToolInputs: 1, ToolErrors: 1, RecoveryEvents: 1, Recovered: true, DurationMs: 1000},
			{Pass: false, FalseDone: true, ToolCalls: 2, RepeatedSkips: 1, PolicyViolations: 1, HumanInterventions: 1, RecoveryEvents: 1, DurationMs: 3000},
		},
	}
	report.FixturePassCount = 1
	populateAgentEvalReportMetrics(&report)
	if report.FullPass || report.PassPower != "not pass^5" {
		t.Fatalf("failed repeated report claimed full reliability: %#v", report)
	}
	if report.UnsupportedCompletionRate != 10 || report.DuplicateActionRate != float64(2)*100/6 || report.ToolErrorRate != float64(1)*100/6 {
		t.Fatalf("bad repeated rates: %#v", report)
	}
	if report.RecoverySuccessRate != 50 || report.AverageToolCalls != 0.6 || report.AverageDurationMs != 400 {
		t.Fatalf("bad repeated aggregates: %#v", report)
	}
	if report.PolicyViolations != 1 || report.HumanInterventions != 1 {
		t.Fatalf("missing policy/intervention totals: %#v", report)
	}
}

func TestNormalizeAgentEvalRepeats(t *testing.T) {
	for input, want := range map[int]int{-1: 1, 0: 1, 1: 1, 5: 5, 99: 10} {
		if got := normalizeAgentEvalRepeats(input); got != want {
			t.Fatalf("repeats(%d)=%d want %d", input, got, want)
		}
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
			args, _ := json.Marshal(map[string]any{
				"action": "replace",
				"items":  []string{"Inspect the compile error", "Edit main.go", "Verify the result"},
			})
			ch <- llm.Delta{ToolCalls: []llm.ToolCallDef{{
				ID:   "call-plan",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "todo_write",
					Arguments: args,
				},
			}}}
			return
		}
		if turn == 2 {
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

func TestLoadAgentEvalScenariosIncludesReliabilityGate(t *testing.T) {
	scenarios, err := loadAgentEvalScenarios()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, scenario := range scenarios {
		seen[scenario.Name] = true
	}
	for _, want := range []string{"redirect-loop-guard", "off-target-ip-guard", "terminal-routing-discipline", "compact-arg-repair", "verify-blocks-compile-error", "completion-covers-both-asks"} {
		if !seen[want] {
			t.Fatalf("missing agent eval scenario %q; have %#v", want, seen)
		}
	}
}
