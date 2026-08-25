package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestRecommendProfileSettingsEnablesMTPOnlyForMTPQwen(t *testing.T) {
	profile := settings.Profile{
		Name:          "qwen3.6-mtp-think",
		ModelID:       "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf",
		CtxTokens:     32768,
		Thinking:      true,
		PreserveThink: true,
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.SpecType != "draft-mtp" || rec.SpecDraftNMax != 2 {
		t.Fatalf("expected conservative MTP recommendation, got spec_type=%q n=%d", rec.SpecType, rec.SpecDraftNMax)
	}
	if !rec.Thinking || !rec.PreserveThink {
		t.Fatalf("qwen recommendation should enable thinking flags: %#v", rec)
	}
}

func TestRecommendProfileSettingsKeepsImportedQwenNoThinking(t *testing.T) {
	profile := settings.Profile{
		Name:      "inference-bridge-qwen",
		ModelID:   "Qwen3.6-27B-Fable-Fus-711-UnHeretic-NM-DAU-NEO-MAX-NEO-Q4_K_M.gguf",
		CtxTokens: 0,
		NoThink:   settings.GenerationParams{MaxTokens: 8192, Seed: -1},
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.Thinking || rec.PreserveThink {
		t.Fatalf("imported Qwen should stay no-thinking by default: %#v", rec)
	}
	if rec.CtxTokens != 32768 {
		t.Fatalf("Qwen template context = %d, want conservative 32768", rec.CtxTokens)
	}
	if rec.NoThink.Temperature != 0.2 || rec.NoThink.TopP != 0.95 || rec.NoThink.TopK != 20 || rec.NoThink.PresencePenalty != 0 {
		t.Fatalf("Qwen no-thinking defaults = %#v", rec.NoThink)
	}
}

func TestRecommendProfileSettingsUsesHauhauCSModelCardDefaults(t *testing.T) {
	profile := settings.Profile{
		Name:      "hf-import",
		ModelID:   "Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced-Q4_K_M.gguf",
		CtxTokens: 0,
		NoThink:   settings.GenerationParams{MaxTokens: 8192, Seed: -1},
	}

	rec, notes := recommendProfileSettings(profile)
	if rec.CtxTokens != 40000 {
		t.Fatalf("HauhauCS context = %d, want tested 40000", rec.CtxTokens)
	}
	if rec.NoThink.Temperature != 0.6 || rec.NoThink.TopP != 0.9 || rec.NoThink.TopK != 64 || rec.NoThink.MinP != 0.05 {
		t.Fatalf("HauhauCS model-card defaults = %#v", rec.NoThink)
	}
	if rec.SpecType != "" || rec.SpecDraftNMax != 0 {
		t.Fatalf("separate MTP drafter must not be enabled automatically: %#v", rec)
	}
	if !strings.Contains(strings.Join(notes, " "), "separate MTP drafter") {
		t.Fatalf("missing MTP drafter guidance: %#v", notes)
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

func TestRecommendProfileSettingsUsesQwen38OfficialAgentModes(t *testing.T) {
	rec, notes := recommendProfileSettings(settings.Profile{
		Name:      "qwen3.8-agent-stability",
		ModelID:   "Qwen3.8-27B-Q4_K_M.gguf",
		CtxTokens: 35000,
	})
	if !rec.Thinking || !rec.PreserveThink {
		t.Fatalf("Qwen3.8 agent recommendation must preserve thinking: %#v", rec)
	}
	if rec.ThinkGeneral.Temperature != 1.0 || rec.ThinkGeneral.TopP != 0.95 || rec.ThinkGeneral.TopK != 20 || rec.ThinkGeneral.RepeatPenalty != 1.0 {
		t.Fatalf("Qwen3.8 thinking defaults = %#v", rec.ThinkGeneral)
	}
	if rec.NoThink.Temperature != 0.7 || rec.NoThink.TopP != 0.8 || rec.NoThink.TopK != 20 || rec.NoThink.PresencePenalty != 1.5 || rec.NoThink.RepeatPenalty != 1.0 {
		t.Fatalf("Qwen3.8 no-thinking defaults = %#v", rec.NoThink)
	}
	if rec.SpecType != "draft-mtp" || rec.SpecDraftNMax != 2 {
		t.Fatalf("Qwen3.8 conservative MTP defaults = %#v", rec)
	}
	joined := strings.Join(notes, " ")
	if !strings.Contains(joined, "preserve_thinking") || !strings.Contains(joined, "GGUF header probe") {
		t.Fatalf("Qwen3.8 recommendation notes = %#v", notes)
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

func TestRecommendProfileSettingsUsesCurrentSmallGemmaTemplate(t *testing.T) {
	profile := settings.Profile{
		Name:          "qwen3.6-nothink-copy-copy:matrix-row",
		ModelID:       "gemma-4-12B-it-Q4_K_M.gguf",
		CtxTokens:     0,
		Thinking:      true,
		PreserveThink: true,
		NoThink:       settings.GenerationParams{MaxTokens: 8192, Seed: -1},
	}

	rec, notes := recommendProfileSettings(profile)
	if rec.CtxTokens != 65536 {
		t.Fatalf("Gemma 4 12B context = %d, want 65536", rec.CtxTokens)
	}
	if !rec.Thinking || !rec.PreserveThink {
		t.Fatalf("explicit Gemma 4 12B thinking choice was lost: %#v", rec)
	}
	if rec.NoThink.Temperature != 1.0 || rec.NoThink.TopP != 0.95 || rec.NoThink.TopK != 64 || rec.NoThink.MinP != 0 || rec.NoThink.RepeatPenalty != 1.0 {
		t.Fatalf("Gemma 4 12B official defaults = %#v", rec.NoThink)
	}
	joined := strings.Join(notes, " ")
	if !strings.Contains(joined, "gemma4-12b") || !strings.Contains(joined, "native function-calling") {
		t.Fatalf("small Gemma template notes = %#v", notes)
	}
}

func TestRecommendProfileSettingsUsesCurrentQwen35Templates(t *testing.T) {
	t.Run("Qwen3.5 4B direct", func(t *testing.T) {
		rec, _ := recommendProfileSettings(settings.Profile{
			Name:      "borrowed-qwen3.6-profile",
			ModelID:   "Qwen3.5-4B-Q4_K_M.gguf",
			CtxTokens: 0,
			NoThink:   settings.GenerationParams{MaxTokens: 8192, Seed: -1},
		})
		if rec.CtxTokens != 65536 || rec.NoThink.Temperature != 0.7 || rec.NoThink.TopP != 0.8 || rec.NoThink.TopK != 20 || rec.NoThink.PresencePenalty != 1.5 {
			t.Fatalf("Qwen3.5 4B defaults = %#v", rec)
		}
	})

	t.Run("Qwythos reasoning", func(t *testing.T) {
		rec, _ := recommendProfileSettings(settings.Profile{
			Name:      "borrowed-qwen3.6-profile",
			ModelID:   "Qwythos-9B-Claude-Mythos-5-1M-Q5_K_M.gguf",
			CtxTokens: 0,
			NoThink:   settings.GenerationParams{MaxTokens: 8192, Seed: -1},
		})
		if rec.CtxTokens != 65536 || rec.NoThink.Temperature != 0.6 || rec.NoThink.TopP != 0.95 || rec.NoThink.TopK != 20 || rec.NoThink.RepeatPenalty != 1.05 {
			t.Fatalf("Qwythos defaults = %#v", rec)
		}
		if rec.SpecType != "" || rec.SpecDraftNMax != 0 {
			t.Fatalf("normal Qwythos GGUF should not enable MTP: %#v", rec)
		}
	})
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

func TestSummarizeBenchmarkResultUsesOnlyRepeatedTextRunsForHeadlineSpeed(t *testing.T) {
	result := ProfileBenchmarkResult{
		Status: "ok",
		Scenarios: []BenchmarkCase{
			{Name: "Text speed", Iteration: 1, Status: "ok", TTFMS: 100, TotalMS: 1000, TokensPerSecond: 100, DecodeTokensPerSecond: 100, EndToEndTokensPerSecond: 80, TimingSource: "backend_decode"},
			{Name: "Text speed", Iteration: 2, Status: "ok", TTFMS: 200, TotalMS: 1100, TokensPerSecond: 110, DecodeTokensPerSecond: 110, EndToEndTokensPerSecond: 82, TimingSource: "backend_decode"},
			{Name: "Text speed", Iteration: 3, Status: "ok", TTFMS: 300, TotalMS: 1200, TokensPerSecond: 120, DecodeTokensPerSecond: 120, EndToEndTokensPerSecond: 84, TimingSource: "backend_decode"},
			{Name: "Tool call", Iteration: 1, Status: "ok", TotalMS: 5000, TokensPerSecond: 2, EndToEndTokensPerSecond: 2, TimingSource: "wall_clock_e2e", StructuredTools: 1},
		},
	}

	summarizeBenchmarkResult(&result)

	if result.TextTokensPerSecond != 110 || result.TokensPerSecond != 110 {
		t.Fatalf("headline text speed = %.1f / legacy %.1f, want 110 without tool latency", result.TextTokensPerSecond, result.TokensPerSecond)
	}
	if result.DecodeTokensPerSecond != 110 || result.EndToEndTokensPerSecond != 82 {
		t.Fatalf("decode/e2e averages = %.1f/%.1f, want 110/82", result.DecodeTokensPerSecond, result.EndToEndTokensPerSecond)
	}
	if result.TextTTFMS != 200 || result.MeasuredTextRuns != 3 {
		t.Fatalf("text timing = %d ms across %d runs, want 200 across 3", result.TextTTFMS, result.MeasuredTextRuns)
	}
	if result.TimingSource != "backend_decode" {
		t.Fatalf("text timing source = %q, want backend_decode", result.TimingSource)
	}
}

func TestRunBenchmarkCaseLabelsBackendAndWallClockTiming(t *testing.T) {
	t.Run("backend decode", func(t *testing.T) {
		client := &benchmarkTimingMockClient{usage: llm.Usage{
			PromptTokens:              10,
			CompletionTokens:          20,
			TotalTokens:               30,
			PromptTokensPerSecond:     500,
			CompletionTokensPerSecond: 120,
			PromptMilliseconds:        20,
			CompletionMilliseconds:    166.7,
		}}
		result := runBenchmarkCase(client, benchmarkSpec{Name: "Text speed", System: "system", User: "hello", MaxTokens: 32})
		if result.TimingSource != "backend_decode" || result.DecodeTokensPerSecond != 120 || result.EndToEndTokensPerSecond <= 0 {
			t.Fatalf("backend timing result = %#v", result)
		}
	})

	t.Run("wall clock fallback", func(t *testing.T) {
		client := &benchmarkTimingMockClient{usage: llm.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}}
		result := runBenchmarkCase(client, benchmarkSpec{Name: "Text speed", System: "system", User: "hello", MaxTokens: 32})
		if result.TimingSource != "wall_clock_e2e" || result.DecodeTokensPerSecond != 0 || result.EndToEndTokensPerSecond <= 0 {
			t.Fatalf("wall-clock timing result = %#v", result)
		}
	})
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
		t.Fatalf("force load calls = %d, want 1", mock.loadCalls)
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
	return nil
}

func (c *benchmarkLoadMockClient) ForceLoadModel(context.Context) error {
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

type benchmarkTimingMockClient struct {
	usage llm.Usage
}

func (c *benchmarkTimingMockClient) Chat(context.Context, llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	go func() {
		time.Sleep(time.Millisecond)
		ch <- llm.Delta{Content: "benchmark response", Usage: &c.usage}
		close(ch)
	}()
	return ch, nil
}

func (c *benchmarkTimingMockClient) Models(context.Context) ([]string, error) {
	return []string{"timing-model"}, nil
}
func (c *benchmarkTimingMockClient) Ping(context.Context) error { return nil }
func (c *benchmarkTimingMockClient) Name() string               { return "benchmark-timing-mock" }
