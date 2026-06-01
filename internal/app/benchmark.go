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
	Status             string           `json:"status"`
	ID                 string           `json:"id,omitempty"`
	CreatedAt          string           `json:"created_at,omitempty"`
	ProfileName        string           `json:"profile_name,omitempty"`
	ProviderName       string           `json:"provider_name,omitempty"`
	ModelID            string           `json:"model_id,omitempty"`
	CtxTokens          int              `json:"ctx_tokens,omitempty"`
	ContextTier        string           `json:"context_tier,omitempty"`
	ContextRole        string           `json:"context_role,omitempty"`
	Score              int              `json:"score,omitempty"`
	Summary            string           `json:"summary"`
	Notes              []string         `json:"notes"`
	RecommendedProfile settings.Profile `json:"recommended_profile"`
	Scenarios          []BenchmarkCase  `json:"scenarios"`
	PromptTokens       int              `json:"prompt_tokens,omitempty"`
	CompletionTokens   int              `json:"completion_tokens,omitempty"`
	TTFMS              int64            `json:"ttf_ms,omitempty"`
	TotalMS            int64            `json:"total_ms,omitempty"`
	TokensPerSecond    float64          `json:"tokens_per_second,omitempty"`
}

type BenchmarkCase struct {
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	Summary          string  `json:"summary"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TTFMS            int64   `json:"ttf_ms,omitempty"`
	TotalMS          int64   `json:"total_ms,omitempty"`
	TokensPerSecond  float64 `json:"tokens_per_second,omitempty"`
	StructuredTools  int     `json:"structured_tools,omitempty"`
	RepairedTools    int     `json:"repaired_tools,omitempty"`
	InlineToolMarkup bool    `json:"inline_tool_markup,omitempty"`
	OutputLeak       bool    `json:"output_leak,omitempty"`
	ValidJSON        bool    `json:"valid_json,omitempty"`
	ExpectedJSON     bool    `json:"expected_json,omitempty"`
	ResponseChars    int     `json:"response_chars,omitempty"`
	Error            string  `json:"error,omitempty"`
}

// BenchmarkProfile recommends profile settings from the runtime registry and,
// when possible, runs a tiny live generation probe against the selected provider.
func (a *App) BenchmarkProfile(profile settings.Profile, provider settings.Provider) ProfileBenchmarkResult {
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

	client, err := buildClient(recommended)
	if err != nil {
		result.Status = "warn"
		result.Summary = "Recommendations generated, but the provider client could not be created."
		result.Notes = append(result.Notes, err.Error())
		return result
	}

	cases := benchmarkCases(recommended)
	for _, spec := range cases {
		result.Scenarios = append(result.Scenarios, runBenchmarkCase(client, spec))
	}
	result.PromptTokens, result.CompletionTokens = 0, 0
	var totalTPS float64
	var tpsCount int
	for _, sc := range result.Scenarios {
		result.PromptTokens += sc.PromptTokens
		result.CompletionTokens += sc.CompletionTokens
		if result.TTFMS == 0 || (sc.TTFMS > 0 && sc.TTFMS < result.TTFMS) {
			result.TTFMS = sc.TTFMS
		}
		result.TotalMS += sc.TotalMS
		if sc.TokensPerSecond > 0 {
			totalTPS += sc.TokensPerSecond
			tpsCount++
		}
		if sc.Status == "warn" {
			result.Status = "warn"
		}
	}
	if tpsCount > 0 {
		result.TokensPerSecond = totalTPS / float64(tpsCount)
	}
	result.Score = benchmarkScore(result)
	result.Summary = fmt.Sprintf(
		"%s context benchmark: score %d, avg %.1f tok/s across %d scenarios",
		result.ContextRole,
		result.Score,
		result.TokensPerSecond,
		len(result.Scenarios),
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

type benchmarkSpec struct {
	Name       string
	System     string
	User       string
	MaxTokens  int
	Tools      []llm.ToolDef
	ToolChoice string
	ExpectJSON bool
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
			User:       "Use the read_file tool to inspect package.json.",
			MaxTokens:  96,
			Tools:      benchmarkToolDefs(),
			ToolChoice: "auto",
		},
	}
}

func benchmarkToolDefs() []llm.ToolDef {
	return []llm.ToolDef{{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:        "read_file",
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
		Temperature:      0,
		TopP:             1,
		TopK:             1,
		Seed:             1,
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
		if delta.Content != "" && firstToken.IsZero() {
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
		if usage.CompletionTokens > 0 && total.Seconds() > 0 {
			out.TokensPerSecond = float64(usage.CompletionTokens) / total.Seconds()
		}
	} else {
		approxTokens := text.Len() / 4
		if approxTokens < 1 && text.Len() > 0 {
			approxTokens = 1
		}
		out.CompletionTokens = approxTokens
		if approxTokens > 0 && total.Seconds() > 0 {
			out.TokensPerSecond = float64(approxTokens) / total.Seconds()
		}
	}
	if out.Summary == "" {
		out.Summary = fmt.Sprintf("TTFT %d ms, %.1f tok/s", out.TTFMS, out.TokensPerSecond)
		if len(spec.Tools) > 0 {
			out.Summary = fmt.Sprintf("%s, structured tools=%d, repaired tools=%d", out.Summary, out.StructuredTools, out.RepairedTools)
		}
		if out.OutputLeak {
			out.Summary += ", leaked reasoning/template text"
		}
	}
	return out
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
	if strings.EqualFold(rp.Family, "qwen3.6") {
		rec.Thinking = true
		rec.PreserveThink = true
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
		rec.NoThink.Temperature = 0.7
		rec.NoThink.TopP = 0.8
		rec.NoThink.TopK = 20
		rec.NoThink.MinP = 0
		rec.NoThink.PresencePenalty = 1.5
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
	} else if strings.EqualFold(rp.Family, "gemma4") {
		rec.Thinking = false
		rec.PreserveThink = false
		rec.SpecType = ""
		rec.SpecDraftNMax = 0
		rec.NoThink.Temperature = 0.7
		rec.NoThink.TopP = 0.9
		rec.NoThink.TopK = 40
		rec.NoThink.MinP = 0
		rec.NoThink.PresencePenalty = 0
		notes = append(notes, "Gemma4 currently gets best Mauler stability with thinking and MTP disabled, plus text-tool repair kept active.")
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
