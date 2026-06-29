package app

import (
	"context"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestRecommendProfileSettingsEnablesMTPOnlyForMTPQwen(t *testing.T) {
	profile := settings.Profile{
		Name:      "qwen3.6-mtp",
		ModelID:   "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf",
		CtxTokens: 32768,
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.SpecType != "draft-mtp" || rec.SpecDraftNMax != 2 {
		t.Fatalf("expected conservative MTP recommendation, got spec_type=%q n=%d", rec.SpecType, rec.SpecDraftNMax)
	}
	if !rec.Thinking || !rec.PreserveThink {
		t.Fatalf("qwen recommendation should enable thinking flags: %#v", rec)
	}
}

func TestRecommendProfileSettingsClearsMTPForPlainQwen(t *testing.T) {
	profile := settings.Profile{
		Name:          "qwen3.6-think",
		ModelID:       "Qwen3.6-27B-UD-Q4_K_XL.gguf",
		CtxTokens:     32768,
		SpecType:      "draft-mtp",
		SpecDraftNMax: 3,
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.SpecType != "" || rec.SpecDraftNMax != 0 {
		t.Fatalf("plain qwen should not keep MTP enabled: %#v", rec)
	}
}

func TestRecommendProfileSettingsDisablesThinkingForGemma(t *testing.T) {
	profile := settings.Profile{
		Name:          "gemma4-test",
		ModelID:       "gemma-4-31B-it-Q4_K_S.gguf",
		CtxTokens:     65000,
		Thinking:      true,
		PreserveThink: true,
		SpecType:      "draft-mtp",
		SpecDraftNMax: 2,
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.Thinking || rec.PreserveThink || rec.SpecType != "" || rec.SpecDraftNMax != 0 {
		t.Fatalf("gemma recommendation should disable thinking/MTP: %#v", rec)
	}
}

func TestBenchmarkCasesIncludeGeneralCodingAndToolProtocol(t *testing.T) {
	cases := benchmarkCases(settings.Profile{Name: "qwen3.6-think", ModelID: "qwen"})
	if len(cases) != 4 {
		t.Fatalf("benchmark case count = %d, want 4", len(cases))
	}
	if cases[0].Name != "General chat" || cases[1].Name != "Coding" || cases[2].Name != "JSON discipline" || cases[3].Name != "Tool protocol" {
		t.Fatalf("unexpected benchmark cases: %#v", cases)
	}
	if !cases[2].ExpectJSON {
		t.Fatalf("json discipline case should expect JSON: %#v", cases[2])
	}
	if len(cases[3].Tools) == 0 || cases[3].ToolChoice != "auto" {
		t.Fatalf("tool protocol case should expose tools: %#v", cases[3])
	}
}

func TestBenchmarkCasesFromInputsAllowsCustomScenarioAndToolMode(t *testing.T) {
	cases := benchmarkCasesFromInputs([]BenchmarkSpecInput{{
		Name:        "Tool required",
		System:      "Use tools.",
		User:        "Read package.json.",
		MaxTokens:   32,
		Temperature: 0.2,
		TopP:        0.8,
		TopK:        20,
		ExpectJSON:  false,
		ToolMode:    "required",
	}})
	if len(cases) != 1 {
		t.Fatalf("custom benchmark case count = %d, want 1", len(cases))
	}
	if cases[0].ToolChoice != "required" || len(cases[0].Tools) == 0 {
		t.Fatalf("custom tool mode was not applied: %#v", cases[0])
	}
	if cases[0].Temperature != 0.2 || cases[0].TopP != 0.8 || cases[0].TopK != 20 {
		t.Fatalf("custom sampling was not applied: %#v", cases[0])
	}
}

func TestLooksLikeBenchmarkOutputLeak(t *testing.T) {
	if !looksLikeBenchmarkOutputLeak("<start_of_turn>system") {
		t.Fatalf("expected template token leak")
	}
	if !looksLikeBenchmarkOutputLeak("Thinking process:\n1. analyze") {
		t.Fatalf("expected reasoning text leak")
	}
	if looksLikeBenchmarkOutputLeak("Local models are useful for privacy.") {
		t.Fatalf("plain answer should not be marked as leaked output")
	}
}

func TestContextTierAndRole(t *testing.T) {
	tests := []struct {
		ctx  int
		tier string
		role string
	}{
		{32768, "fast", "Fast daily"},
		{65536, "balanced", "Large-code"},
		{98304, "large", "Wide-context"},
		{120000, "ceiling", "Max-context"},
	}
	for _, tt := range tests {
		if got := contextTier(tt.ctx); got != tt.tier {
			t.Fatalf("contextTier(%d)=%q, want %q", tt.ctx, got, tt.tier)
		}
		if got := contextRole(tt.ctx); got != tt.role {
			t.Fatalf("contextRole(%d)=%q, want %q", tt.ctx, got, tt.role)
		}
	}
}

func TestBenchmarkScorePenalizesLeaksAndMissingTools(t *testing.T) {
	result := ProfileBenchmarkResult{
		TokensPerSecond: 30,
		Scenarios: []BenchmarkCase{
			{Name: "General chat", Status: "ok"},
			{Name: "JSON discipline", Status: "warn", ExpectedJSON: true, ValidJSON: false},
			{Name: "Tool protocol", Status: "warn"},
			{Name: "Coding", Status: "ok", OutputLeak: true},
		},
	}
	if got := benchmarkScore(result); got >= 100 || got <= 0 {
		t.Fatalf("benchmarkScore should be penalized but positive, got %d", got)
	}
}

func TestBenchmarkLoadsRequestedContextAndReportsActual(t *testing.T) {
	oldBuilder := buildClientForAgent
	mock := &benchmarkLoadMockClient{actualCtx: 8192}
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return mock, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })

	app := &App{history: agent.NewHistory(32768)}
	result := app.runBenchmarkProfile(
		settings.Profile{Name: "ctx-probe", ModelID: "model.gguf", CtxTokens: 32768},
		settings.Provider{Name: "llama", Backend: "llamacpp", BaseURL: "http://localhost:8080/v1"},
		[]BenchmarkSpecInput{{
			Name:      "Tiny",
			System:    "system",
			User:      "hi",
			MaxTokens: 8,
			ToolMode:  "none",
		}},
	)

	if mock.loadCalls != 1 {
		t.Fatalf("load calls = %d, want 1", mock.loadCalls)
	}
	if result.ActualCtxTokens != 8192 {
		t.Fatalf("actual ctx = %d, want 8192", result.ActualCtxTokens)
	}
	if result.Status != "warn" {
		t.Fatalf("expected warning when actual ctx is below requested: %#v", result)
	}
}

type benchmarkLoadMockClient struct {
	loadCalls int
	actualCtx int
}

func (c *benchmarkLoadMockClient) LoadModel(context.Context) error {
	c.loadCalls++
	return nil
}

func (c *benchmarkLoadMockClient) ActualContextLength(context.Context) int {
	return c.actualCtx
}

func (c *benchmarkLoadMockClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	go func() {
		defer close(ch)
		ch <- llm.Delta{Content: "ok", Usage: &llm.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}}
	}()
	return ch, nil
}

func (c *benchmarkLoadMockClient) Models(context.Context) ([]string, error) {
	return []string{"model.gguf"}, nil
}
func (c *benchmarkLoadMockClient) Ping(context.Context) error { return nil }
func (c *benchmarkLoadMockClient) Name() string               { return "benchmark-load-mock" }
