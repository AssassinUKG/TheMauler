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
		Tools:            []TaskToolEvent{{Name: "read_file"}, {Name: "grep"}},
		Events: []TaskRunEvent{
			{Kind: "continue"},
			{Kind: "truncated"},
			{Kind: "tool_error"},
			{Kind: "compaction"},
			{Kind: "context_clear"},
		},
	}

	metrics := buildLoopMetrics(run)
	if metrics.ToolCalls != 2 || metrics.AutoContinues != 1 || metrics.Truncations != 1 || metrics.ToolErrors != 1 || metrics.Compactions != 1 || metrics.ContextClears != 1 {
		t.Fatalf("bad metrics: %#v", metrics)
	}
	var decoded LoopMetrics
	if err := json.Unmarshal([]byte(metrics.Detail()), &decoded); err != nil {
		t.Fatalf("metrics detail is not JSON: %v", err)
	}
	if decoded.StopReason != "tool_budget_exhausted" {
		t.Fatalf("decoded metrics = %#v", decoded)
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

func TestSystemPromptRequiresTodoToolPlan(t *testing.T) {
	cfg := settings.DefaultSettings()
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Builder", Description: "build"}, nil, nil)
	for _, want := range []string{"first call todo_create", "3-8 step plan", "todo_update/todo_done/todo_blocked"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}
