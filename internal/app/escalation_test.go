package app

import (
	"context"
	"encoding/json"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestEscalationProducesRecoveryToolCall(t *testing.T) {
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return &escalationMockClient{}, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })

	cfg := settings.DefaultSettings()
	cfg.Agents.EscalationProfile = "frontier"
	frontier := settings.Profile{Name: "frontier", Provider: "anthropic", ModelID: "claude-test", Backend: "anthropic", NoThink: settings.GenerationParams{MaxTokens: 512, TopP: 1}}
	profiles := settings.ProfilesFile{
		Providers: map[string]settings.Provider{"anthropic": {Name: "anthropic", Backend: "anthropic"}},
		Profiles:  map[string]settings.Profile{"frontier": frontier},
	}
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(8192),
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "fix main.go"))
	run := startTaskRun("fix main.go", "Builder", "local", "local-model")
	used := 0

	attempt, ok := app.tryEscalation(context.Background(), &cfg, settings.Profile{Name: "local", ModelID: "local-model"}, &run, "auto_continue_exhausted", "truncated", nil, true, &used)

	if !ok || used != 1 {
		t.Fatalf("escalation ok=%v used=%d", ok, used)
	}
	if len(attempt.ToolCalls) != 1 || attempt.ToolCalls[0].Function.Name != "read" {
		t.Fatalf("bad escalation tool calls: %#v", attempt.ToolCalls)
	}
	msgs := app.history.Messages()
	if len(msgs) != 2 || len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("escalation response not appended to history: %#v", msgs)
	}
	if countRunEvents(run.Events, "escalation") == 0 {
		t.Fatalf("expected escalation events: %#v", run.Events)
	}
}

func TestEscalationCappedPerRun(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.EscalationProfile = "frontier"
	profiles := settings.DefaultProfiles()
	profiles.Profiles["frontier"] = settings.Profile{Name: "frontier", ModelID: "claude-test", Backend: "anthropic"}
	app := &App{cfg: &cfg, profiles: &profiles, history: agent.NewHistory(8192)}
	run := startTaskRun("prompt", "Builder", "local", "model")
	used := maxEscalationsPerRun

	if _, ok := app.tryEscalation(context.Background(), &cfg, settings.Profile{}, &run, "x", "y", nil, false, &used); ok {
		t.Fatal("escalation should be capped")
	}
}

type escalationMockClient struct{}

func (c *escalationMockClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	go func() {
		defer close(ch)
		args, _ := json.Marshal(map[string]string{"path": "main.go"})
		ch <- llm.Delta{
			Content: "Escalated step.",
			ToolCalls: []llm.ToolCallDef{{
				ID:   "esc-1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read",
					Arguments: args,
				},
			}},
		}
	}()
	return ch, nil
}

func (c *escalationMockClient) Models(context.Context) ([]string, error) {
	return []string{"claude-test"}, nil
}
func (c *escalationMockClient) Ping(context.Context) error { return nil }
func (c *escalationMockClient) Name() string               { return "mock-escalation" }
