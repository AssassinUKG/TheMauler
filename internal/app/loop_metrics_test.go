package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestInitialRunPromptTextFallsBackToRunPrompt(t *testing.T) {
	run := TaskRun{Prompt: "inspect the repo and fix the bug"}
	if got := initialRunPromptText(llm.Message{}, run); got != run.Prompt {
		t.Fatalf("initial prompt = %q, want run prompt", got)
	}
	if got := toolChoiceFor(initialRunPromptText(llm.Message{}, run), 0, 0); got != "required" {
		t.Fatalf("resumed inspection task tool choice = %q, want required", got)
	}
}

func TestLoopMetricsDerivesRunHealth(t *testing.T) {
	run := TaskRun{
		StopReason:       "tool_budget_exhausted",
		DurationMs:       123,
		PromptTokens:     1000,
		CompletionTokens: 250,
		Tools: []TaskToolEvent{
			{Name: "read", Input: `{"path":"a.go"}`, Status: "done"},
			{Name: "read", Input: `{"path":"a.go"}`, Status: "skipped", Result: "Recovery: this repeated command was skipped without stopping the run."},
			{Name: "grep", Status: "error"},
		},
		Events: []TaskRunEvent{
			{Kind: "continue"},
			{Kind: "truncated"},
			{Kind: "tool_error"},
			{Kind: "compaction"},
			{Kind: "context_clear"},
			{Kind: "verifier_required"},
			{Kind: "tool_routing", Detail: "phase=webshell\ntool_count=31"},
			{Kind: "prompt_budget", Message: "Prompt budget over 20%"},
		},
	}

	metrics := buildLoopMetrics(run)
	if metrics.ToolCalls != 3 || metrics.AutoContinues != 1 || metrics.Truncations != 1 || metrics.ToolErrors != 2 || metrics.Compactions != 1 || metrics.ContextClears != 1 {
		t.Fatalf("bad metrics: %#v", metrics)
	}
	if metrics.RepeatedToolInputs != 1 || metrics.RepeatedSkips != 1 || metrics.VerifierPrompts != 1 || metrics.MaxRoutedTools != 31 || metrics.PromptWarnings != 1 {
		t.Fatalf("bad metrics: %#v", metrics)
	}
	if metrics.StabilityScore >= 100 || metrics.StabilityScore <= 0 {
		t.Fatalf("expected degraded but nonzero stability score: %#v", metrics)
	}
	var decoded LoopMetrics
	if err := json.Unmarshal([]byte(metrics.Detail()), &decoded); err != nil {
		t.Fatalf("metrics detail is not JSON: %v", err)
	}
	if decoded.StopReason != "tool_budget_exhausted" {
		t.Fatalf("decoded metrics = %#v", decoded)
	}
}

func TestRepeatGuardCatchesPagerVariants(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Input: `{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -100"}`, Status: "done"},
		{Name: "shell", Input: `{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -50"}`, Status: "done"},
		{Name: "shell", Input: `{"command":"curl -s --max-time 5 http://10.129.26.26/ | head -30"}`, Status: "done"},
		{Name: "shell", Input: `{"command":"curl -s --max-time 5 http://10.129.26.26/"}`, Status: "done"},
	}}
	if got := repeatedToolInputCount(run.Tools); got != 3 {
		t.Fatalf("pager-only curl variants should count as 3 repeats, got %d", got)
	}
}

func TestRepeatedIdenticalOutcomeCount(t *testing.T) {
	cases := []struct {
		name    string
		tools   []TaskToolEvent
		want    int
		stalled bool
	}{
		{
			name: "identical input and result stalls",
			tools: []TaskToolEvent{
				{Name: "shell", Input: `{"command":"curl -s http://target/"}`, Result: "HTTP/1.1 301 Moved Permanently"},
				{Name: "shell", Input: `{"command":"curl -s http://target/"}`, Result: "HTTP/1.1 301 Moved Permanently"},
			},
			want:    2,
			stalled: true,
		},
		{
			name: "same input with different results is polling not outcome repeat",
			tools: []TaskToolEvent{
				{Name: "read", Input: `{"path":"job.log"}`, Result: "status: queued"},
				{Name: "read", Input: `{"path":"job.log"}`, Result: "status: running"},
				{Name: "read", Input: `{"path":"job.log"}`, Result: "status: done"},
			},
			want: 0,
		},
		{
			name: "different inputs with same result stalls",
			tools: []TaskToolEvent{
				{Name: "shell", Input: `{"command":"curl -s http://target/"}`, Result: "Connection refused"},
				{Name: "shell", Input: `{"command":"curl -sv --max-time 5 http://target/"}`, Result: "Connection refused"},
			},
			want:    2,
			stalled: true,
		},
		{
			name: "volatile token only differences normalize",
			tools: []TaskToolEvent{
				{Name: "read_tool_result", Input: `{"result_id":"run-1/result-1"}`, Result: "2026-07-07T09:01:02Z result_id=run-1/result-1 value=0xDEADBEEFCAFEBABE"},
				{Name: "read_tool_result", Input: `{"result_id":"run-1/result-2"}`, Result: "2026-07-07T09:03:04Z result_id=run-1/result-2 value=0xABADBABECAFED00D"},
			},
			want:    2,
			stalled: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := repeatedIdenticalOutcomeCount(tc.tools)
			if got != tc.want {
				t.Fatalf("repeatedIdenticalOutcomeCount() = %d, want %d", got, tc.want)
			}
			metrics := buildLoopMetrics(TaskRun{Tools: tc.tools})
			if metrics.RepeatedIdenticalOutcomes != tc.want {
				t.Fatalf("metrics repeated outcomes = %d, want %d", metrics.RepeatedIdenticalOutcomes, tc.want)
			}
			if metrics.LoopStalled() != tc.stalled {
				t.Fatalf("LoopStalled() = %t, want %t for %#v", metrics.LoopStalled(), tc.stalled, metrics)
			}
		})
	}
}

func TestDetectToolCycle(t *testing.T) {
	cases := []struct {
		name       string
		toolNames  []string
		wantDetect bool
		wantPeriod int
	}{
		{name: "period two", toolNames: []string{"read", "grep", "read", "grep"}, wantDetect: true, wantPeriod: 2},
		{name: "period three", toolNames: []string{"read", "grep", "glob", "read", "grep", "glob"}, wantDetect: true, wantPeriod: 3},
		{name: "plain repeats are not alternation cycles", toolNames: []string{"read", "read", "read"}, wantDetect: false},
		{name: "non repeating tail", toolNames: []string{"read", "grep", "glob", "shell"}, wantDetect: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools := make([]TaskToolEvent, 0, len(tc.toolNames))
			for _, name := range tc.toolNames {
				tools = append(tools, TaskToolEvent{Name: name})
			}
			gotDetect, gotPeriod := detectToolCycle(tools)
			if gotDetect != tc.wantDetect || gotPeriod != tc.wantPeriod {
				t.Fatalf("detectToolCycle() = (%t,%d), want (%t,%d)", gotDetect, gotPeriod, tc.wantDetect, tc.wantPeriod)
			}
			metrics := buildLoopMetrics(TaskRun{Tools: tools})
			if metrics.ToolCycleDetected != tc.wantDetect || metrics.ToolCyclePeriod != tc.wantPeriod {
				t.Fatalf("metrics cycle = (%t,%d), want (%t,%d)", metrics.ToolCycleDetected, metrics.ToolCyclePeriod, tc.wantDetect, tc.wantPeriod)
			}
			if tc.wantDetect && !metrics.LoopStalled() {
				t.Fatalf("cycle metrics should stall: %#v", metrics)
			}
		})
	}
}

func TestDetectToolCycleDoesNotTreatProgressingPlanUpdatesAsALoop(t *testing.T) {
	tools := []TaskToolEvent{
		{Name: "todo_write", Input: `{"action":"update","id":"todo-1","status":"done"}`},
		{Name: "write", Input: `{"path":"generated/long.txt","content":"Line 1"}`},
		{Name: "todo_write", Input: `{"action":"update","id":"todo-2","status":"done"}`},
		{Name: "write", Input: `{"path":"generated/long.txt","content":"Line 2","append":true}`},
	}
	if detected, period := detectToolCycle(tools); detected || period != 0 {
		t.Fatalf("progressing plan/action sequence detected as cycle: (%t,%d)", detected, period)
	}
}

func TestLoopStalledPredicate(t *testing.T) {
	cases := []struct {
		name string
		m    LoopMetrics
		want bool
	}{
		{
			name: "healthy does not stall",
			m:    LoopMetrics{StabilityScore: 80, RepeatedToolInputs: 4},
			want: false,
		},
		{
			name: "low stability with repeats stalls",
			m:    LoopMetrics{StabilityScore: 20, RepeatedToolInputs: 2},
			want: true,
		},
		{
			name: "low stability with repeated skips stalls",
			m:    LoopMetrics{StabilityScore: 0, RepeatedSkips: 2},
			want: true,
		},
		{
			name: "low stability with many tool errors stalls",
			m:    LoopMetrics{StabilityScore: 15, ToolErrors: 4},
			want: true,
		},
		{
			name: "low stability without loop signals waits",
			m:    LoopMetrics{StabilityScore: 10, ToolErrors: 1},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.LoopStalled(); got != tc.want {
				t.Fatalf("LoopStalled() = %t, want %t for %#v", got, tc.want, tc.m)
			}
		})
	}
}

func TestLoopMetricsCountsReviewGateFailures(t *testing.T) {
	run := startTaskRun("fix build", "Builder", "profile", "model")
	recordReviewGateEvent(&run, 0, []VerifyVerdict{
		{Gate: "build", Status: "fail", Blocking: true},
		{Gate: "lint", Status: "pass"},
		{Gate: "completion", Status: "fail"},
		{Gate: "reviewer", Status: "fail"},
	})

	metrics := buildLoopMetrics(run)

	if metrics.ReviewCycles != 1 || metrics.VerifyGateFails != 1 || metrics.CompletionRailFails != 1 || metrics.ReviewerChangeRequests != 1 {
		t.Fatalf("review metrics = %#v", metrics)
	}
}

func TestCircuitBreakerInjectsOnceThenPauses(t *testing.T) {
	metrics := LoopMetrics{StabilityScore: 0, RepeatedToolInputs: 3}
	if got := decideLoopCircuitBreaker(metrics, false, LoopMetrics{}, 0, 3); got != loopCircuitBreakerInject {
		t.Fatalf("first stalled turn = %q, want inject", got)
	}
	if got := decideLoopCircuitBreaker(metrics, true, metrics, 3, 3); got != loopCircuitBreakerNone {
		t.Fatalf("same turn after injection = %q, want none", got)
	}
	worse := metrics
	worse.RepeatedToolInputs++
	if got := decideLoopCircuitBreaker(worse, true, metrics, 3, 4); got != loopCircuitBreakerPause {
		t.Fatalf("next stalled tool turn = %q, want pause", got)
	}
	recovered := LoopMetrics{StabilityScore: 60}
	if got := decideLoopCircuitBreaker(recovered, true, metrics, 3, 4); got != loopCircuitBreakerReset {
		t.Fatalf("recovered turn = %q, want reset", got)
	}
}

func TestCircuitBreakerResetsAfterNovelOutcomeEvenWhenHistoricalRepeatRemains(t *testing.T) {
	trip := LoopMetrics{StabilityScore: 35, RepeatedIdenticalOutcomes: 2}
	current := trip
	current.StabilityScore = 40
	if got := decideLoopCircuitBreaker(current, true, trip, 7, 8); got != loopCircuitBreakerReset {
		t.Fatalf("novel corrective action = %q, want reset", got)
	}
}

func TestCircuitBreakerPromptIsActionable(t *testing.T) {
	prompt := loopCircuitBreakerPrompt(LoopMetrics{StabilityScore: 0, RepeatedToolInputs: 3, ToolErrors: 1})
	for _, want := range []string{"Loop-health is critical", "Stop repeating", "evidence already gathered is sufficient", "call no more tools", "answer the original request directly", "DIFFERENT action", "web_search/fetch_url", "methodology, not current evidence", "confirmed target IP", "Do not rerun"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRepeatedToolSkipCountIncludesCachedResults(t *testing.T) {
	tools := []TaskToolEvent{
		{Name: "read", Status: "done", Result: "fresh evidence"},
		{Name: "read", Status: "cached", Result: "cached evidence"},
		{Name: "read", Status: "done", Result: "[cached_tool_result] use prior evidence"},
	}
	if got := repeatedToolSkipCount(tools); got != 2 {
		t.Fatalf("cached result count = %d, want 2", got)
	}
}

func TestProjectResumePromptDiscouragesOrientationRereads(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".mauler"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mauler", "project-recap.md"), []byte("target found\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	cfg := settings.DefaultSettings()
	cfg.Context.Lab.Target = "10.129.26.26"
	prompt := buildProjectResumePrompt(cfg)
	for _, want := range []string{"Orientation rule", ".mauler/project-recap.md", ".mauler/progress.md", "Do not call read", "injected recap/progress excerpts"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("resume prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestGoalReminderPromptIncludesOriginalTask(t *testing.T) {
	prompt := goalReminderPrompt(TaskRun{Prompt: "finish the writeup"})
	for _, want := range []string{"Original task reminder", "finish the writeup", "Continue from the latest verified state"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("goal reminder missing %q:\n%s", want, prompt)
		}
	}
}

func TestGoalReminderPromptIncludesLatestVerifiedState(t *testing.T) {
	run := startTaskRun("carry on", "Ops", "profile", "model")
	run.addTool("shell", `{"command":"cat fuzz_results.txt | jq -r '.results[] | select(.status >= 200 and .status < 400)'"}`, "200 robots.txt http://connected.htb/robots.txt\n[shared_terminal/wsl exit 0, 14ms]", "done", 14)
	got := goalReminderPrompt(run)
	for _, want := range []string{"Latest verified state", "robots.txt", "Continue from the latest verified state above"} {
		if !strings.Contains(got, want) {
			t.Fatalf("goal reminder missing %q:\n%s", want, got)
		}
	}
}

func TestGoalReminderShellEvidenceSkipsFallbackNoise(t *testing.T) {
	run := startTaskRun("carry on", "Ops", "profile", "model")
	run.addTool("shell", `{"command":"curl -v http://connected.htb/admin/ | head -30"}`, "[isolated shell fallback: shared terminal is busy]\n* Added connected.htb:80:10.129.245.100 to DNS cache\n% Total    % Received % Xferd\n<!doctype html>\n<title>FreePBX Administration</title>\n[wsl exit 0, 100ms]", "done", 100)
	got := goalReminderPrompt(run)
	if strings.Contains(got, "isolated shell fallback") || strings.Contains(got, "% Total") || strings.Contains(got, "DNS cache") {
		t.Fatalf("goal reminder should skip shell transport noise:\n%s", got)
	}
	if !strings.Contains(got, "FreePBX Administration") {
		t.Fatalf("goal reminder should keep useful evidence:\n%s", got)
	}
}

func TestSystemPromptRequiresTodoToolPlan(t *testing.T) {
	cfg := settings.DefaultSettings()
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Builder", Description: "build"}, nil, nil)
	for _, want := range []string{"use todo_write", "3-8 step plan", "update it as phases change"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSystemPromptIncludesProbeCaptureInspectDiscipline(t *testing.T) {
	cfg := settings.DefaultSettings()
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Builder", Description: "build"}, nil, nil)
	for _, want := range []string{"Loop discipline", "test one hypothesis at a time", "vary repeats only when they can produce new evidence", "save bulky reusable output as an artifact", "inspect artifacts locally"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}
