package app

import (
	"encoding/json"
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
