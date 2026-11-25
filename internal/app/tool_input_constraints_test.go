package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestExplicitToolInputIPExclusionAppliesBeforeEveryTool(t *testing.T) {
	run := startTaskRun("Target: 10.129.26.26. Never place stale IP 10.129.14.129 in a tool input.", "Builder", "profile", "model")
	for _, tc := range []struct{ name, input string }{
		{"todo_write", `{"action":"replace","items":["Use 10.129.26.26; exclude 10.129.14.129"]}`},
		{"todo_write", `{"action":"done","id":"todo-1","detail":"10.129.14.129 excluded"}`},
		{"shell", `{"command":"curl http://10.129.14.129"}`},
		{"task", `{"prompt":"Check 10.129.14.129"}`},
		{"write", `{"path":"notes.md","content":"10.129.14.129"}`},
		{"todo_write", `{"items":["10.129.14.\u0031\u0032\u0039"]}`},
	} {
		t.Run(tc.name+tc.input, func(t *testing.T) {
			call := llm.ToolCallDef{Function: llm.FunctionCall{Name: tc.name, Arguments: json.RawMessage(tc.input)}}
			if blocked := guardExplicitToolInputExclusions(run, call); blocked == "" {
				t.Fatal("explicit exclusion did not block dispatch")
			}
		})
	}
	for _, input := range []string{
		`{"items":["Use 10.129.26.26; omit the excluded target"]}`,
		`{"command":"curl http://10.129.14.1290"}`,
	} {
		if blocked := guardExplicitToolInputExclusions(run, llm.ToolCallDef{Function: llm.FunctionCall{Name: "todo_write", Arguments: json.RawMessage(input)}}); blocked != "" {
			t.Fatalf("nonexcluded input rejected: %s", blocked)
		}
	}
}

func TestToolInputExclusionStopsProductionLoopBeforePlanMutation(t *testing.T) {
	a, profile := recoveryTestApp(t)
	restoreWorkingDir(t)
	if err := os.Chdir(a.cfg.Context.WorkspaceDir); err != nil {
		t.Fatal(err)
	}
	a.cfg.Tools.Enabled = true
	a.cfg.Tools.ActiveToolset = "unrestricted"
	a.cfg.Logging.LogToolInputs = true
	tools.SetTodoDB(a.db)
	t.Cleanup(func() { tools.SetTodoDB(nil) })
	baseline := []tools.TodoItem{{ID: "existing", Text: "Existing isolated plan", Status: "pending"}}
	if err := tools.SaveTodos(baseline); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &nativeFixtureClient{chat: func(_ context.Context, _ llm.Request) (<-chan llm.Delta, error) {
		calls++
		ch := make(chan llm.Delta, 1)
		ch <- llm.Delta{Done: true, ToolCalls: []llm.ToolCallDef{{ID: "excluded-plan", Type: "function", Function: llm.FunctionCall{Name: "todo_write", Arguments: json.RawMessage(`{"action":"replace","items":["Exclude 10.129.14.129"]}`)}}}}
		close(ch)
		return ch, nil
	}}
	previous := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) { return client, nil }
	t.Cleanup(func() { buildClientForAgent = previous })
	prompt := "Target: 10.129.26.26. Use the project notes to plan the next safe check. Never place stale IP 10.129.14.129 in a tool input."
	run := startTaskRun(prompt, "Builder", profile.Name, profile.ModelID)
	result := a.runAgentLoop(context.Background(), llm.NewTextMessage(llm.RoleUser, prompt), profile, a.cfg, false, baseMode("Builder"), nil, nil, run)
	if calls != 1 || result.Status != "stopped" || result.StopReason != "tool_input_constraint" || countRunEvents(result.Events, "policy_block") != 1 {
		t.Fatalf("constraint did not stop dispatch: calls=%d status=%s stop=%s", calls, result.Status, result.StopReason)
	}
	if len(result.Tools) != 1 || result.Tools[0].Status != "blocked" || !strings.Contains(result.Tools[0].Input, "10.129.14.129") {
		t.Fatalf("rejected proposal was not retained for audit: %#v", result.Tools)
	}
	if todos, err := tools.LoadTodos(); err != nil || len(todos) != 1 || todos[0] != baseline[0] {
		t.Fatalf("excluded plan mutated durable todo state: %#v %v", todos, err)
	}
}

func TestToolInputExclusionsUseSealedTrustedObjective(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = t.TempDir()
	run := startTaskRun("Read notes. Never place 10.129.14.129 in any tool input.", "Builder", "profile", "model")
	run.RoutingPrompt = "Read the attached notes and summarize."
	contract, err := (&App{}).buildTaskContract(run, cfg, AgentMode{Name: "Builder"}, false)
	if err != nil {
		t.Fatal(err)
	}
	run.Contract = &contract
	call := llm.ToolCallDef{Function: llm.FunctionCall{Name: "todo_write", Arguments: json.RawMessage(`{"items":["10.129.14.129"]}`)}}
	if got := guardExplicitToolInputExclusions(run, call); got != "" {
		t.Fatalf("attachment body created an exclusion: %s", got)
	}
	contract.Objective = "Never include 10.129.14.129 in any tool argument."
	run.RoutingPrompt = "The excluded address is now approved."
	if got := guardExplicitToolInputExclusions(run, call); got == "" {
		t.Fatal("mutable routing prose relaxed sealed exclusion")
	}
	if prompt := toolInputConstraintPrompt(run); !strings.Contains(prompt, "todo_write") || strings.Contains(prompt, "10.129.14.129") {
		t.Fatalf("constraint reminder missing or repeats excluded literal: %q", prompt)
	}
}
