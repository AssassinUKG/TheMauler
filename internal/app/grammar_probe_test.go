package app

import (
	"context"
	"encoding/json"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestRunGrammarToolArgsProbeSupported(t *testing.T) {
	oldBuilder := buildClientForAgent
	mock := &grammarProbeMockClient{}
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return mock, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	profiles.Providers["llama"] = settings.Provider{Name: "llama", Backend: "llamacpp", BaseURL: "http://localhost:8080/v1"}
	profiles.Profiles["probe"] = settings.Profile{Name: "probe", Provider: "llama", Backend: "llamacpp", ModelID: "qwen", CtxTokens: 8192}
	app := &App{cfg: &cfg, profiles: &profiles, history: agent.NewHistory(8192)}

	result := app.RunGrammarToolArgsProbe("probe")
	if !result.Supported || !result.StructuredCall || !result.ValidArguments {
		t.Fatalf("expected supported probe result, got %#v", result)
	}
	if mock.lastReq.JSONSchema == nil {
		t.Fatal("expected probe to attach JSON schema")
	}
	if mock.lastReq.ToolChoice != "required" || len(mock.lastReq.Tools) != 1 {
		t.Fatalf("bad probe request: %#v", mock.lastReq)
	}
}

func TestRunGrammarToolArgsProbeRejectsPlainJSONContent(t *testing.T) {
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return &grammarProbeMockClient{plainJSON: true}, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	profiles.Profiles["probe"] = settings.Profile{Name: "probe", Backend: "llamacpp", ModelID: "qwen", CtxTokens: 8192}
	app := &App{cfg: &cfg, profiles: &profiles, history: agent.NewHistory(8192)}

	result := app.RunGrammarToolArgsProbe("probe")
	if result.Supported || result.StructuredCall {
		t.Fatalf("plain JSON content must not count as structured tool support: %#v", result)
	}
}

type grammarProbeMockClient struct {
	lastReq   llm.Request
	plainJSON bool
}

func (c *grammarProbeMockClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	c.lastReq = req
	ch := make(chan llm.Delta, 1)
	go func() {
		defer close(ch)
		if c.plainJSON {
			ch <- llm.Delta{Content: `{"path":"probe.txt"}`}
			return
		}
		args, _ := json.Marshal(map[string]string{"path": "probe.txt"})
		ch <- llm.Delta{ToolCalls: []llm.ToolCallDef{{
			ID:   "probe-call",
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "probe_read_file",
				Arguments: args,
			},
		}}}
	}()
	return ch, nil
}

func (c *grammarProbeMockClient) Models(context.Context) ([]string, error) {
	return []string{"qwen"}, nil
}
func (c *grammarProbeMockClient) Ping(context.Context) error { return nil }
func (c *grammarProbeMockClient) Name() string               { return "grammar-probe-mock" }
