package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/llm"
	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

type ProfileBenchmarkResult struct {
	Status                  string           `json:"status"`
	ID                      string           `json:"id,omitempty"`
	CreatedAt               string           `json:"created_at,omitempty"`
	ProfileName             string           `json:"profile_name,omitempty"`
	ProviderName            string           `json:"provider_name,omitempty"`
	ModelID                 string           `json:"model_id,omitempty"`
	CtxTokens               int              `json:"ctx_tokens,omitempty"`
	ActualCtxTokens         int              `json:"actual_ctx_tokens,omitempty"`
	ContextTier             string           `json:"context_tier,omitempty"`
	ContextRole             string           `json:"context_role,omitempty"`
	Score                   int              `json:"score,omitempty"`
	Summary                 string           `json:"summary"`
	Notes                   []string         `json:"notes"`
	RecommendedProfile      settings.Profile `json:"recommended_profile"`
	Scenarios               []BenchmarkCase  `json:"scenarios"`
	PromptTokens            int              `json:"prompt_tokens,omitempty"`
	CompletionTokens        int              `json:"completion_tokens,omitempty"`
	TTFMS                   int64            `json:"ttf_ms,omitempty"`
	TotalMS                 int64            `json:"total_ms,omitempty"`
	LoadMS                  int64            `json:"load_ms,omitempty"`
	Warmup                  *BenchmarkCase   `json:"warmup,omitempty"`
	MeasuredTextRuns        int              `json:"measured_text_runs,omitempty"`
	TextTTFMS               int64            `json:"text_ttf_ms,omitempty"`
	TextTokensPerSecond     float64          `json:"text_tokens_per_second,omitempty"`
	DecodeTokensPerSecond   float64          `json:"decode_tokens_per_second,omitempty"`
	EndToEndTokensPerSecond float64          `json:"end_to_end_tokens_per_second,omitempty"`
	PromptTokensPerSecond   float64          `json:"prompt_tokens_per_second,omitempty"`
	PromptMS                float64          `json:"prompt_ms,omitempty"`
	DecodeMS                float64          `json:"decode_ms,omitempty"`
	TimingSource            string           `json:"timing_source,omitempty"`
	TokensPerSecond         float64          `json:"tokens_per_second,omitempty"`
}

type BenchmarkCase struct {
	Name                    string  `json:"name"`
	Status                  string  `json:"status"`
	Summary                 string  `json:"summary"`
	PromptTokens            int     `json:"prompt_tokens,omitempty"`
	CompletionTokens        int     `json:"completion_tokens,omitempty"`
	Iteration               int     `json:"iteration,omitempty"`
	TTFMS                   int64   `json:"ttf_ms,omitempty"`
	TotalMS                 int64   `json:"total_ms,omitempty"`
	PromptMS                float64 `json:"prompt_ms,omitempty"`
	DecodeMS                float64 `json:"decode_ms,omitempty"`
	PromptTokensPerSecond   float64 `json:"prompt_tokens_per_second,omitempty"`
	DecodeTokensPerSecond   float64 `json:"decode_tokens_per_second,omitempty"`
	EndToEndTokensPerSecond float64 `json:"end_to_end_tokens_per_second,omitempty"`
	TimingSource            string  `json:"timing_source,omitempty"`
	TokensPerSecond         float64 `json:"tokens_per_second,omitempty"`
	StructuredTools         int     `json:"structured_tools,omitempty"`
	RepairedTools           int     `json:"repaired_tools,omitempty"`
	InlineToolMarkup        bool    `json:"inline_tool_markup,omitempty"`
	OutputLeak              bool    `json:"output_leak,omitempty"`
	ValidJSON               bool    `json:"valid_json,omitempty"`
	ExpectedJSON            bool    `json:"expected_json,omitempty"`
	ResponseChars           int     `json:"response_chars,omitempty"`
	Error                   string  `json:"error,omitempty"`
}

type BenchmarkSpecInput struct {
	Name            string  `json:"name"`
	System          string  `json:"system"`
	User            string  `json:"user"`
	MaxTokens       int     `json:"max_tokens"`
	Temperature     float64 `json:"temperature"`
	TopP            float64 `json:"top_p"`
	TopK            int     `json:"top_k"`
	MinP            float64 `json:"min_p"`
	PresencePenalty float64 `json:"presence_penalty"`
	Seed            int64   `json:"seed"`
	ExpectJSON      bool    `json:"expect_json"`
	ToolMode        string  `json:"tool_mode"`
}

// BenchmarkProfile recommends profile settings from the runtime registry and,
// when possible, runs a tiny live generation probe against the selected provider.
func (a *App) BenchmarkProfile(profile settings.Profile, provider settings.Provider) ProfileBenchmarkResult {
	return a.runBenchmarkProfile(profile, provider, nil)
}

func (a *App) BenchmarkProfileWithCases(profile settings.Profile, provider settings.Provider, inputs []BenchmarkSpecInput) ProfileBenchmarkResult {
	return a.runBenchmarkProfile(profile, provider, inputs)
}

func (a *App) LoadBenchmarkModel(profile settings.Profile, provider settings.Provider) ProfileBenchmarkResult {
	profile.Backend = provider.Backend
	profile.BaseURL = provider.BaseURL
	profile.APIKeyEnv = provider.APIKeyEnv
	result := ProfileBenchmarkResult{
		Status:             "ok",
		ID:                 fmt.Sprintf("bench-load-%s", time.Now().Format("20060102-150405")),
		CreatedAt:          time.Now().Format(time.RFC3339),
		ProfileName:        profile.Name,
		ProviderName:       provider.Name,
		ModelID:            profile.ModelID,
		CtxTokens:          profile.CtxTokens,
		ContextTier:        contextTier(profile.CtxTokens),
		ContextRole:        contextRole(profile.CtxTokens),
		Summary:            "Model loaded for benchmark.",
		RecommendedProfile: profile,
	}
	client, err := buildClientForAgent(profile)
	if err != nil {
		result.Status = "warn"
		result.Summary = "Provider client could not be created."
		result.Notes = append(result.Notes, err.Error())
		return result
	}
	loadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := loadBenchmarkModelExact(loadCtx, a, client, profile); err != nil {
		result.Status = "warn"
		result.Summary = "Provider could not load the requested benchmark context."
		result.Notes = append(result.Notes, err.Error())
		return result
	}
	result.ActualCtxTokens = benchmarkActualContext(loadCtx, client)
	if result.ActualCtxTokens > 0 {
		result.Summary = fmt.Sprintf("Loaded %s at requested %d context; backend reports %d.", profile.ModelID, profile.CtxTokens, result.ActualCtxTokens)
	}
	if result.ActualCtxTokens > 0 && result.ActualCtxTokens < profile.CtxTokens {
		result.Status = "warn"
		result.Notes = append(result.Notes, fmt.Sprintf("Backend reported actual context %d after requesting %d.", result.ActualCtxTokens, profile.CtxTokens))
	}
	return result
}

func (a *App) runBenchmarkProfile(profile settings.Profile, provider settings.Provider, inputs []BenchmarkSpecInput) ProfileBenchmarkResult {
	profile.Backend = provider.Backend
	profile.BaseURL = provider.BaseURL
	profile.APIKeyEnv = provider.APIKeyEnv

	recommended, notes := recommendProfileSettings(profile)
	result := ProfileBenchmarkResult{
		Status:             "ok",
		ID:                 fmt.Sprintf("bench-%s", time.Now().Format("20060102-150405")),
		CreatedAt:          time.Now().Format(time.RFC3339),
		ProfileName:        profile.Name,
		ProviderName:       provider.Name,
		ModelID:            profile.ModelID,
		CtxTokens:          profile.CtxTokens,
		ContextTier:        contextTier(profile.CtxTokens),
		ContextRole:        contextRole(profile.CtxTokens),
		Summary:            "Generated profile recommendations.",
		Notes:              notes,
		RecommendedProfile: recommended,
	}
	defer func() {
		_ = saveBenchmarkRun(result)
	}()

	client, err := buildClientForAgent(recommended)
	if err != nil {
		result.Status = "warn"
		result.Summary = "Recommendations generated, but the provider client could not be created."
		result.Notes = append(result.Notes, err.Error())
		return result
	}
	loadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	loadStarted := time.Now()
	if err := loadBenchmarkModelExact(loadCtx, a, client, recommended); err != nil {
		cancel()
		result.Status = "warn"
		result.Summary = "Recommendations generated, but the provider could not load the requested benchmark context."
		result.Notes = append(result.Notes, err.Error())
		return result
	}
	result.LoadMS = time.Since(loadStarted).Milliseconds()
	result.ActualCtxTokens = benchmarkActualContext(loadCtx, client)
	cancel()
	if result.ActualCtxTokens > 0 && result.ActualCtxTokens < recommended.CtxTokens {
		result.Status = "warn"
		result.Notes = append(result.Notes, fmt.Sprintf("Backend reported actual context %d after requesting %d; this run is not a valid %dk context measurement.", result.ActualCtxTokens, recommended.CtxTokens, recommended.CtxTokens/1024))
	}

	cases := benchmarkCases(recommended)
	if len(inputs) > 0 {
		cases = benchmarkCasesFromInputs(inputs)
	}
	if warmupSpec, ok := primaryTextBenchmarkSpec(cases); ok {
		warmupSpec.Name = "Warm-up"
		warmupSpec.MaxTokens = clampInt(warmupSpec.MaxTokens, 16, 48, 32)
		warmup := runBenchmarkCase(client, warmupSpec)
		warmup.Iteration = 1
		result.Warmup = &warmup
		if warmup.Status != "ok" {
			result.Notes = append(result.Notes, "Warm-up did not complete cleanly; measured runs may still include model startup cost.")
		}
	}
	for _, spec := range cases {
		repeats := 1
		if isPrimaryTextBenchmarkSpec(spec) {
			repeats = 3
		}
		for iteration := 1; iteration <= repeats; iteration++ {
			scenario := runBenchmarkCase(client, spec)
			scenario.Iteration = iteration
			result.Scenarios = append(result.Scenarios, scenario)
		}
	}
	summarizeBenchmarkResult(&result)
	result.Score = benchmarkScore(result)
	result.Summary = fmt.Sprintf(
		"%s context benchmark: score %d, text %.1f tok/s (%s), warm TTFT %d ms across %d measured text runs",
		result.ContextRole,
		result.Score,
		result.TextTokensPerSecond,
		benchmarkTimingSourceLabel(result.TimingSource),
		result.TextTTFMS,
		result.MeasuredTextRuns,
	)
	return result
}

func (a *App) ListBenchmarkRuns() []ProfileBenchmarkResult {
	runs, err := loadBenchmarkRuns()
	if err != nil {
		return nil
	}
	return runs
}

func (a *App) ClearBenchmarkRuns() error {
	path, err := benchmarkRunsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func benchmarkActualContext(ctx context.Context, client llm.Client) int {
	cq, ok := client.(interface {
		ActualContextLength(context.Context) int
	})
	if !ok {
		return 0
	}
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return cq.ActualContextLength(qctx)
}

func loadBenchmarkModelExact(ctx context.Context, app *App, client llm.Client, profile settings.Profile) error {
	if loader, ok := client.(interface{ ForceLoadModel(context.Context) error }); ok {
		return loader.ForceLoadModel(ctx)
	}
	if app != nil {
		return app.ensureModelLoaded(ctx, client, profile)
	}
	if loader, ok := client.(interface{ LoadModel(context.Context) error }); ok {
		return loader.LoadModel(ctx)
	}
	return nil
}

type benchmarkSpec struct {
	Name            string
	System          string
	User            string
	MaxTokens       int
	Temperature     float64
	TopP            float64
	TopK            int
	MinP            float64
	PresencePenalty float64
	Seed            int64
	Tools           []llm.ToolDef
	ToolChoice      string
	ExpectJSON      bool
}

func benchmarkCases(profile settings.Profile) []benchmarkSpec {
	return []benchmarkSpec{
		{
			Name:      "General chat",
			System:    "You are a concise local assistant. Answer naturally.",
			User:      "In two short sentences, explain why local language models are useful.",
			MaxTokens: 96,
		},
		{
			Name:      "Coding",
			System:    "You are a senior coding assistant. Return compact, correct code only when asked.",
			User:      "Write a small TypeScript function named clamp that clamps a number between min and max. Include one example call.",
			MaxTokens: 160,
		},
		{
			Name:       "JSON discipline",
			System:     "You return strict JSON only when asked. Do not wrap JSON in Markdown.",
			User:       `Return exactly one JSON object with keys "language", "safe", and "score". Use language "typescript", safe true, score 7.`,
			MaxTokens:  96,
			ExpectJSON: true,
		},
		{
			Name:       "Tool protocol",
			System:     "You are a tool-using coding agent. If a tool is available and relevant, call it.",
			User:       "Use the read tool to inspect package.json.",
			MaxTokens:  96,
			Tools:      benchmarkToolDefs(),
			ToolChoice: "auto",
		},
	}
}

func benchmarkCasesFromInputs(inputs []BenchmarkSpecInput) []benchmarkSpec {
	out := make([]benchmarkSpec, 0, len(inputs))
	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		system := strings.TrimSpace(input.System)
		user := strings.TrimSpace(input.User)
		if name == "" || user == "" {
			continue
		}
		if system == "" {
			system = "You are a concise local assistant."
		}
		spec := benchmarkSpec{
			Name:            name,
			System:          system,
			User:            user,
			MaxTokens:       clampInt(input.MaxTokens, 16, 8192, 128),
			Temperature:     input.Temperature,
			TopP:            input.TopP,
			TopK:            input.TopK,
			MinP:            input.MinP,
			PresencePenalty: input.PresencePenalty,
			Seed:            input.Seed,
			ExpectJSON:      input.ExpectJSON,
		}
		switch strings.ToLower(strings.TrimSpace(input.ToolMode)) {
		case "auto", "required":
			spec.Tools = benchmarkToolDefs()
			spec.ToolChoice = strings.ToLower(strings.TrimSpace(input.ToolMode))
		}
		out = append(out, spec)
	}
	if len(out) == 0 {
		return benchmarkCases(settings.Profile{})
	}
	return out
}

func benchmarkToolDefs() []llm.ToolDef {
	return []llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "read",
			Description: "Read a UTF-8 text file from the current workspace.",
			Parameters:  json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		},
	}}
}

func runBenchmarkCase(client llm.Client, spec benchmarkSpec) BenchmarkCase {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	req := llm.Request{
		Messages: []llm.Message{
			llm.NewTextMessage(llm.RoleSystem, spec.System),
			llm.NewTextMessage(llm.RoleUser, spec.User),
		},
		Tools:            spec.Tools,
		ToolChoice:       spec.ToolChoice,
		MaxTokens:        spec.MaxTokens,
		Temperature:      spec.Temperature,
		TopP:             defaultFloat(spec.TopP, 1),
		TopK:             defaultInt(spec.TopK, 1),
		MinP:             spec.MinP,
		PresencePenalty:  spec.PresencePenalty,
		Seed:             defaultInt64(spec.Seed, 1),
		EnableThinking:   false,
		PreserveThinking: false,
	}
	start := time.Now()
	ch, err := client.Chat(ctx, req)
	if err != nil {
		return BenchmarkCase{Name: spec.Name, Status: "warn", Summary: "request failed", Error: err.Error()}
	}
	out := BenchmarkCase{Name: spec.Name, Status: "ok"}
	var firstToken time.Time
	var usage *llm.Usage
	var text strings.Builder
	var structured []llm.ToolCallDef
	for delta := range ch {
		if delta.Error != nil {
			out.Status = "warn"
			out.Summary = "stream failed"
			out.Error = delta.Error.Error()
			return out
		}
		if (delta.Content != "" || delta.Thinking != "" || len(delta.ToolCalls) > 0) && firstToken.IsZero() {
			firstToken = time.Now()
		}
		text.WriteString(delta.Content)
		if len(delta.ToolCalls) > 0 {
			structured = append(structured, delta.ToolCalls...)
		}
		if delta.Usage != nil {
			usage = delta.Usage
		}
	}
	total := time.Since(start)
	out.TotalMS = total.Milliseconds()
	if !firstToken.IsZero() {
		out.TTFMS = firstToken.Sub(start).Milliseconds()
	}
	out.ResponseChars = text.Len()
	out.ExpectedJSON = spec.ExpectJSON
	if spec.ExpectJSON {
		var value map[string]any
		out.ValidJSON = json.Unmarshal([]byte(strings.TrimSpace(text.String())), &value) == nil
		if !out.ValidJSON {
			out.Status = "warn"
			out.Summary = "invalid JSON output"
		}
	}
	out.OutputLeak = looksLikeBenchmarkOutputLeak(text.String())
	out.StructuredTools = len(structured)
	if len(spec.Tools) > 0 {
		repaired := parseInlineToolMarkup(text.String(), spec.Tools)
		out.RepairedTools = len(repaired)
		out.InlineToolMarkup = containsInlineToolMarkup(text.String())
		if len(structured) == 0 && len(repaired) == 0 {
			out.Status = "warn"
			out.Summary = "no structured or repairable tool call"
		}
	}
	if out.OutputLeak && out.Status == "ok" {
		out.Status = "warn"
	}
	if usage != nil {
		out.PromptTokens = usage.PromptTokens
		out.CompletionTokens = usage.CompletionTokens
		out.PromptMS = usage.PromptMilliseconds
		out.DecodeMS = usage.CompletionMilliseconds
		out.PromptTokensPerSecond = usage.PromptTokensPerSecond
		if usage.CompletionTokens > 0 && total.Seconds() > 0 {
			out.EndToEndTokensPerSecond = float64(usage.CompletionTokens) / total.Seconds()
		}
		if usage.CompletionTokensPerSecond > 0 {
			out.DecodeTokensPerSecond = usage.CompletionTokensPerSecond
			out.TokensPerSecond = out.DecodeTokensPerSecond
			out.TimingSource = "backend_decode"
		} else if out.EndToEndTokensPerSecond > 0 {
			out.TokensPerSecond = out.EndToEndTokensPerSecond
			out.TimingSource = "wall_clock_e2e"
		}
	} else {
		approxTokens := text.Len() / 4
		if approxTokens < 1 && text.Len() > 0 {
			approxTokens = 1
		}
		out.CompletionTokens = approxTokens
		if approxTokens > 0 && total.Seconds() > 0 {
			out.EndToEndTokensPerSecond = float64(approxTokens) / total.Seconds()
			out.TokensPerSecond = out.EndToEndTokensPerSecond
			out.TimingSource = "estimated_wall_clock"
		}
	}
	if out.Summary == "" {
		out.Summary = fmt.Sprintf("TTFT %d ms, %.1f tok/s (%s)", out.TTFMS, out.TokensPerSecond, benchmarkTimingSourceLabel(out.TimingSource))
		if len(spec.Tools) > 0 {
			out.Summary = fmt.Sprintf("%s, structured tools=%d, repaired tools=%d", out.Summary, out.StructuredTools, out.RepairedTools)
		}
		if out.OutputLeak {
			out.Summary += ", leaked reasoning/template text"
		}
	}
	return out
}

func primaryTextBenchmarkSpec(cases []benchmarkSpec) (benchmarkSpec, bool) {
	for _, spec := range cases {
		if isPrimaryTextBenchmarkSpec(spec) {
			return spec, true
		}
	}
	return benchmarkSpec{}, false
}

func isPrimaryTextBenchmarkSpec(spec benchmarkSpec) bool {
	if len(spec.Tools) > 0 || spec.ExpectJSON {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(spec.Name))
	return name == "text speed" || name == "general chat" || strings.Contains(name, "text speed")
}

func summarizeBenchmarkResult(result *ProfileBenchmarkResult) {
	if result == nil {
		return
	}
	result.PromptTokens, result.CompletionTokens = 0, 0
	var textTPS, decodeTPS, e2eTPS, promptTPS, promptMS, decodeMS float64
	var textTPSCount, decodeCount, e2eCount, promptTPSCount, promptMSCount, decodeMSCount int
	var textTTFTTotal int64
	var textTTFTCount int
	timingSources := map[string]bool{}
	for _, sc := range result.Scenarios {
		result.PromptTokens += sc.PromptTokens
		result.CompletionTokens += sc.CompletionTokens
		result.TotalMS += sc.TotalMS
		if sc.Status == "warn" {
			result.Status = "warn"
		}
		if !isPrimaryTextBenchmarkName(sc.Name) {
			continue
		}
		result.MeasuredTextRuns++
		if sc.TTFMS > 0 {
			textTTFTTotal += sc.TTFMS
			textTTFTCount++
		}
		if sc.TokensPerSecond > 0 {
			textTPS += sc.TokensPerSecond
			textTPSCount++
		}
		if sc.DecodeTokensPerSecond > 0 {
			decodeTPS += sc.DecodeTokensPerSecond
			decodeCount++
		}
		if sc.EndToEndTokensPerSecond > 0 {
			e2eTPS += sc.EndToEndTokensPerSecond
			e2eCount++
		}
		if sc.PromptTokensPerSecond > 0 {
			promptTPS += sc.PromptTokensPerSecond
			promptTPSCount++
		}
		if sc.PromptMS > 0 {
			promptMS += sc.PromptMS
			promptMSCount++
		}
		if sc.DecodeMS > 0 {
			decodeMS += sc.DecodeMS
			decodeMSCount++
		}
		if sc.TimingSource != "" {
			timingSources[sc.TimingSource] = true
		}
	}
	if textTTFTCount > 0 {
		result.TextTTFMS = textTTFTTotal / int64(textTTFTCount)
		result.TTFMS = result.TextTTFMS
	}
	if textTPSCount > 0 {
		result.TextTokensPerSecond = textTPS / float64(textTPSCount)
		result.TokensPerSecond = result.TextTokensPerSecond
	}
	if decodeCount > 0 {
		result.DecodeTokensPerSecond = decodeTPS / float64(decodeCount)
	}
	if e2eCount > 0 {
		result.EndToEndTokensPerSecond = e2eTPS / float64(e2eCount)
	}
	if promptTPSCount > 0 {
		result.PromptTokensPerSecond = promptTPS / float64(promptTPSCount)
	}
	if promptMSCount > 0 {
		result.PromptMS = promptMS / float64(promptMSCount)
	}
	if decodeMSCount > 0 {
		result.DecodeMS = decodeMS / float64(decodeMSCount)
	}
	switch len(timingSources) {
	case 0:
		result.TimingSource = "unavailable"
	case 1:
		for source := range timingSources {
			result.TimingSource = source
		}
	default:
		result.TimingSource = "mixed"
	}
}

func isPrimaryTextBenchmarkName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == "text speed" || lower == "general chat" || strings.Contains(lower, "text speed")
}

func benchmarkTimingSourceLabel(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "backend_decode":
		return "backend decode"
	case "wall_clock_e2e":
		return "warm end-to-end"
	case "estimated_wall_clock":
		return "estimated end-to-end"
	case "mixed":
		return "mixed timing"
	default:
		return "timing unavailable"
	}
}

func contextTier(ctxTokens int) string {
	switch {
	case ctxTokens <= 0:
		return "unknown"
	case ctxTokens <= 32768:
		return "fast"
	case ctxTokens <= 65536:
		return "balanced"
	case ctxTokens <= 98304:
		return "large"
	default:
		return "ceiling"
	}
}

func contextRole(ctxTokens int) string {
	switch contextTier(ctxTokens) {
	case "fast":
		return "Fast daily"
	case "balanced":
		return "Large-code"
	case "large":
		return "Wide-context"
	case "ceiling":
		return "Max-context"
	default:
		return "Unknown"
	}
}

func benchmarkScore(result ProfileBenchmarkResult) int {
	score := 100
	if result.TokensPerSecond > 0 {
		switch {
		case result.TokensPerSecond < 8:
			score -= 25
		case result.TokensPerSecond < 15:
			score -= 12
		case result.TokensPerSecond < 25:
			score -= 5
		}
	}
	if result.TTFMS > 5000 {
		score -= 10
	} else if result.TTFMS > 2500 {
		score -= 5
	}
	for _, sc := range result.Scenarios {
		if sc.Status == "warn" {
			score -= 10
		}
		if sc.OutputLeak {
			score -= 15
		}
		if sc.ExpectedJSON && !sc.ValidJSON {
			score -= 10
		}
		if sc.Name == "Tool protocol" && sc.StructuredTools == 0 && sc.RepairedTools > 0 {
			score -= 6
		}
		if sc.Name == "Tool protocol" && sc.StructuredTools == 0 && sc.RepairedTools == 0 {
			score -= 20
		}
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func looksLikeBenchmarkOutputLeak(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<start_of_turn>") ||
		strings.Contains(lower, "<end_of_turn>") ||
		strings.Contains(lower, "<|channel>") ||
		strings.Contains(lower, "<channel|>") ||
		strings.Contains(lower, "thinking process:")
}

func benchmarkRunsPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "benchmark-runs.json"), nil
}

func loadBenchmarkRuns() ([]ProfileBenchmarkResult, error) {
	path, err := benchmarkRunsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []ProfileBenchmarkResult{}, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []ProfileBenchmarkResult
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func saveBenchmarkRun(run ProfileBenchmarkResult) error {
	runs, err := loadBenchmarkRuns()
	if err != nil {
		return err
	}
	runs = append([]ProfileBenchmarkResult{run}, runs...)
	if len(runs) > 200 {
		runs = runs[:200]
	}
	path, err := benchmarkRunsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func recommendProfileSettings(profile settings.Profile) (settings.Profile, []string) {
	rec := profile
	var notes []string
	rp, ok := runtimeprofile.Match(profile)
	if !ok {
		notes = append(notes, "No model-family runtime profile matched; kept existing settings except for safe max_tokens bounds.")
		clampMaxTokens(&rec)
		return rec, notes
	}
	notes = append(notes, fmt.Sprintf("Matched runtime profile %s with %s adapter.", rp.Name, rp.Adapter))
	if rp.ChatTemplate != "" {
		note := fmt.Sprintf("Chat template: %s", rp.ChatTemplate)
		if rp.RequiresJinja {
			note += "; keep InferenceBridge/llama.cpp Jinja enabled so the GGUF's own template formats chat and tools"
		}
		notes = append(notes, note+".")
	}
	applyRuntimeSampling := func(params *settings.GenerationParams, defaults runtimeprofile.Defaults, maxTokens int) {
		params.Temperature = defaults.Temperature
		params.TopP = defaults.TopP
		params.TopK = defaults.TopK
		params.MinP = defaults.MinP
		params.PresencePenalty = defaults.PresencePenalty
		params.RepeatPenalty = defaults.RepeatPenalty
		if params.MaxTokens <= 0 {
			params.MaxTokens = maxTokens
		}
	}
	if strings.EqualFold(rp.Family, "qwen3.8") {
		// Qwen3.8's official agent path keeps thinking enabled and preserved.
		// Mauler adaptively switches to the no-thinking sampler after tool work,
		// so both parameter sets must remain model-card correct.
		rec.Thinking = true
		rec.PreserveThink = true
		for _, params := range []*settings.GenerationParams{&rec.ThinkGeneral, &rec.ThinkCoding} {
			params.Temperature = 1.0
			params.TopP = 0.95
			params.TopK = 20
			params.MinP = 0
			params.PresencePenalty = 0
			params.RepeatPenalty = 1.0
			if params.MaxTokens <= 0 {
				params.MaxTokens = 8192
			}
		}
		applyRuntimeSampling(&rec.NoThink, rp.Defaults, 8192)
		rec.SpecType = "draft-mtp"
		if rec.SpecDraftNMax <= 0 {
			rec.SpecDraftNMax = 2
		}
		notes = append(notes,
			"Qwen3.8 agent mode keeps thinking enabled with preserve_thinking; Mauler falls back to the official no-thinking sampler after the configured tool threshold.",
			"Enabled conservative draft-mtp n=2; the GGUF header probe remains authoritative and disables it automatically when MTP heads are absent.",
		)
	} else if strings.EqualFold(rp.Family, "qwen3.6") {
		// Thinking is a user-facing profile choice. Imported/local profiles stay
		// no-thinking by default; an explicitly configured thinking profile keeps it.
		rec.PreserveThink = rec.Thinking && rec.PreserveThink
		rec.ThinkCoding.Temperature = 0.6
		rec.ThinkCoding.TopP = 0.95
		rec.ThinkCoding.TopK = 20
		rec.ThinkCoding.MinP = 0
		rec.ThinkCoding.PresencePenalty = 0
		rec.ThinkGeneral.Temperature = 1.0
		rec.ThinkGeneral.TopP = 0.95
		rec.ThinkGeneral.TopK = 20
		rec.ThinkGeneral.MinP = 0
		rec.ThinkGeneral.PresencePenalty = 0
		applyRuntimeSampling(&rec.NoThink, rp.Defaults, 8192)
		if rec.ThinkGeneral.MaxTokens <= 0 {
			rec.ThinkGeneral.MaxTokens = 8192
		}
		if rec.ThinkCoding.MaxTokens <= 0 {
			rec.ThinkCoding.MaxTokens = 8192
		}
		if runtimeprofile.LooksMTPModel(profile) {
			rec.SpecType = "draft-mtp"
			if rec.SpecDraftNMax <= 0 {
				rec.SpecDraftNMax = 2
			}
			notes = append(notes, "Model name looks MTP-capable; enabled draft-mtp with conservative spec_draft_n_max.")
		} else {
			rec.SpecType = ""
			rec.SpecDraftNMax = 0
			notes = append(notes, "Model name does not include MTP; left draft-mtp disabled.")
		}
		notes = append(notes, "Qwen local agent template keeps no-thinking as the default; enable thinking only for an explicit planner/reviewer profile.")
	} else if strings.EqualFold(rp.Family, "qwen3.5") {
		// Qwen3.5 uses the GGUF Jinja enable_thinking switch rather than the
		// older /think and /nothink soft prompts. Keep imported profiles direct
		// by default while preserving an explicit thinking-profile choice.
		rec.PreserveThink = rec.Thinking && rec.PreserveThink
		rec.ThinkGeneral.Temperature = 1.0
		rec.ThinkGeneral.TopP = 0.95
		rec.ThinkGeneral.TopK = 20
		rec.ThinkGeneral.MinP = 0
		rec.ThinkGeneral.PresencePenalty = 1.5
		rec.ThinkGeneral.RepeatPenalty = 1.0
		rec.ThinkCoding.Temperature = 0.6
		rec.ThinkCoding.TopP = 0.95
		rec.ThinkCoding.TopK = 20
		rec.ThinkCoding.MinP = 0
		rec.ThinkCoding.PresencePenalty = 0
		rec.ThinkCoding.RepeatPenalty = 1.0
		applyRuntimeSampling(&rec.NoThink, rp.Defaults, 8192)
		if rec.ThinkGeneral.MaxTokens <= 0 {
			rec.ThinkGeneral.MaxTokens = 8192
		}
		if rec.ThinkCoding.MaxTokens <= 0 {
			rec.ThinkCoding.MaxTokens = 8192
		}
		if runtimeprofile.LooksMTPModel(profile) {
			rec.SpecType = "draft-mtp"
			if rec.SpecDraftNMax <= 0 {
				rec.SpecDraftNMax = 2
			}
			notes = append(notes, "Model name includes an MTP artifact marker; enabled draft-mtp conservatively.")
		} else {
			rec.SpecType = ""
			rec.SpecDraftNMax = 0
			notes = append(notes, "Selected GGUF does not include an MTP marker; left draft-mtp disabled.")
		}
		notes = append(notes, "Qwen3.5 uses the embedded Jinja enable_thinking switch; imported agent profiles stay no-thinking unless thinking is explicitly selected.")
	} else if strings.EqualFold(rp.Family, "gemma4") {
		if rp.Supports.Thinking {
			rec.PreserveThink = rec.Thinking && rec.PreserveThink
		} else {
			rec.Thinking = false
			rec.PreserveThink = false
		}
		rec.SpecType = ""
		rec.SpecDraftNMax = 0
		applyRuntimeSampling(&rec.NoThink, rp.Defaults, 8192)
		applyRuntimeSampling(&rec.ThinkGeneral, rp.Defaults, 8192)
		applyRuntimeSampling(&rec.ThinkCoding, rp.Defaults, 8192)
		if rp.Supports.Thinking {
			notes = append(notes, "Gemma4 thinking is controlled by the embedded Jinja template; no-thinking remains the default for tool-heavy Mauler runs.")
		} else {
			notes = append(notes, "This Gemma4 runtime profile keeps thinking disabled for the currently verified Mauler tool path.")
		}
		if strings.EqualFold(rp.ToolProtocol, "native-openai") {
			notes = append(notes, "This Gemma4 template uses its native function-calling format; keep current llama.cpp Jinja support enabled.")
		} else {
			notes = append(notes, "Gemma4 text-tool repair remains enabled as a compatibility safety net for this profile.")
		}
		if rp.HuggingFaceRepo != "" {
			notes = append(notes, fmt.Sprintf("Hugging Face template matched %s.", rp.HuggingFaceRepo))
		}
		if rp.Supports.MTP {
			notes = append(notes, "This model family offers a separate MTP drafter; Mauler leaves speculation off until that matching draft GGUF is configured and verified.")
		}
	}
	if rec.CtxTokens <= 0 {
		rec.CtxTokens = rp.RecommendedCtx
		notes = append(notes, fmt.Sprintf("Context was empty; set recommended ctx_tokens=%d.", rec.CtxTokens))
	}
	clampMaxTokens(&rec)
	return rec, notes
}

func clampMaxTokens(profile *settings.Profile) {
	const safeMax = 8192
	if profile.ThinkGeneral.MaxTokens <= 0 || profile.ThinkGeneral.MaxTokens > safeMax {
		profile.ThinkGeneral.MaxTokens = 4096
	}
	if profile.ThinkCoding.MaxTokens <= 0 || profile.ThinkCoding.MaxTokens > safeMax {
		profile.ThinkCoding.MaxTokens = safeMax
	}
	if profile.NoThink.MaxTokens <= 0 || profile.NoThink.MaxTokens > safeMax {
		profile.NoThink.MaxTokens = 4096
	}
}

func defaultFloat(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultInt64(value, fallback int64) int64 {
	if value == 0 {
		return fallback
	}
	return value
}
