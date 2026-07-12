package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestReviewerPassReadOnlyToolset(t *testing.T) {
	cfg := settings.DefaultSettings()
	registry := tools.New()

	defs := reviewerReadOnlyToolDefs(registry, &cfg)

	names := map[string]bool{}
	for _, def := range defs {
		names[def.Function.Name] = true
	}
	for _, want := range []string{"read", "glob", "grep"} {
		if !names[want] {
			t.Fatalf("reviewer toolset missing %s: %#v", want, names)
		}
	}
	for _, forbidden := range []string{"write", "edit", "shell"} {
		if names[forbidden] {
			t.Fatalf("reviewer toolset included destructive tool %s: %#v", forbidden, names)
		}
	}
}

func TestReviewerPassParsesVerdict(t *testing.T) {
	restore := mockReviewerClient(t, &reviewerMockClient{
		responses: []reviewerMockResponse{
			{text: "I inspected the packet."},
			{text: `{"verdict":"request_changes","severity":"blocking","items":["handle empty input"]}`},
		},
	})
	defer restore()
	cfg := settings.DefaultSettings()
	run := gateableReviewRun()

	verdict := ((*App)(nil)).runReviewerPass(context.Background(), &run, reviewerTestProfile(), &cfg)

	if verdict.Status != "fail" || !verdict.Blocking || !strings.Contains(strings.Join(verdict.Improvements, "\n"), "empty input") {
		t.Fatalf("verdict = %#v, want blocking request_changes", verdict)
	}
}

func TestReviewerPassIsInconclusiveOnGarbage(t *testing.T) {
	restore := mockReviewerClient(t, &reviewerMockClient{
		responses: []reviewerMockResponse{
			{text: "looks fine"},
			{text: "not json"},
			{text: "looks fine again"},
			{text: "still not json"},
		},
	})
	defer restore()
	cfg := settings.DefaultSettings()
	run := gateableReviewRun()

	verdict := ((*App)(nil)).runReviewerPass(context.Background(), &run, reviewerTestProfile(), &cfg)

	if verdict.Status != "inconclusive" || !verdict.Blocking {
		t.Fatalf("garbage reviewer output should be blocking/inconclusive, got %#v", verdict)
	}
}

func TestReviewerPassIsInconclusiveOnProviderFailure(t *testing.T) {
	restore := mockReviewerClient(t, &reviewerMockClient{responses: []reviewerMockResponse{{err: errors.New("provider offline")}, {err: errors.New("provider offline")}}})
	defer restore()
	cfg := settings.DefaultSettings()
	run := gateableReviewRun()

	verdict := ((*App)(nil)).runReviewerPass(context.Background(), &run, reviewerTestProfile(), &cfg)

	if verdict.Status != "inconclusive" || !verdict.Blocking || !strings.Contains(verdict.Summary, "provider offline") {
		t.Fatalf("provider failure should be blocking/inconclusive, got %#v", verdict)
	}
}

func TestReviewerPassRespectsToolCap(t *testing.T) {
	client := &reviewerMockClient{
		responses: []reviewerMockResponse{
			{calls: []llm.ToolCallDef{{
				ID:   "call-read",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read",
					Arguments: json.RawMessage(`{"path":"missing.go"}`),
				},
			}}},
			{text: `{"verdict":"approve","severity":"advisory","items":["top risk: missing file in test"]}`},
		},
	}
	restore := mockReviewerClient(t, client)
	defer restore()
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.ReviewerMaxTools = 1
	run := gateableReviewRun()

	verdict := ((*App)(nil)).runReviewerPass(context.Background(), &run, reviewerTestProfile(), &cfg)

	if verdict.Status != "pass" {
		t.Fatalf("verdict = %#v, want pass", verdict)
	}
	if len(client.requests) < 2 {
		t.Fatalf("expected two reviewer requests, got %d", len(client.requests))
	}
	if len(client.requests[0].Tools) == 0 {
		t.Fatalf("first reviewer turn should expose read-only tools")
	}
	if len(client.requests[1].Tools) != 0 || client.requests[1].ToolChoice != "none" || client.requests[1].JSONSchema == nil {
		t.Fatalf("second reviewer turn should be JSON-only after tool cap: %#v", client.requests[1])
	}
}

func TestReviewerVerdictFromTextApprovesWithItems(t *testing.T) {
	verdict := reviewerVerdictFromText("```json\n{\"verdict\":\"approve\",\"severity\":\"advisory\",\"items\":[\"top risk\"]}\n```")

	if verdict.Status != "pass" || len(verdict.Improvements) != 1 || verdict.Improvements[0] != "top risk" {
		t.Fatalf("verdict = %#v", verdict)
	}
}

func reviewerTestProfile() settings.Profile {
	return settings.Profile{
		Name:      "mock-reviewer",
		ModelID:   "mock-reviewer",
		CtxTokens: 8192,
		NoThink:   settings.GenerationParams{MaxTokens: 512, TopP: 1},
	}
}

type reviewerMockResponse struct {
	text  string
	calls []llm.ToolCallDef
	err   error
}

type reviewerMockClient struct {
	responses []reviewerMockResponse
	requests  []llm.Request
}

func (c *reviewerMockClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	c.requests = append(c.requests, req)
	ch := make(chan llm.Delta, 1)
	idx := len(c.requests) - 1
	if idx < len(c.responses) && c.responses[idx].err != nil {
		return nil, c.responses[idx].err
	}
	go func() {
		defer close(ch)
		if idx >= len(c.responses) {
			ch <- llm.Delta{Content: `{"verdict":"approve","severity":"advisory","items":[]}`}
			return
		}
		resp := c.responses[idx]
		ch <- llm.Delta{Content: resp.text, ToolCalls: resp.calls}
	}()
	return ch, nil
}

func (c *reviewerMockClient) Models(context.Context) ([]string, error) {
	return []string{"mock-reviewer"}, nil
}
func (c *reviewerMockClient) Ping(context.Context) error { return nil }
func (c *reviewerMockClient) Name() string               { return "mock-reviewer" }

func mockReviewerClient(t *testing.T, client *reviewerMockClient) func() {
	t.Helper()
	old := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) { return client, nil }
	return func() { buildClientForAgent = old }
}
