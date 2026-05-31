package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mauler/internal/llm"
	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

type ProfileBenchmarkResult struct {
	Status             string           `json:"status"`
	Summary            string           `json:"summary"`
	Notes              []string         `json:"notes"`
	RecommendedProfile settings.Profile `json:"recommended_profile"`
	PromptTokens       int              `json:"prompt_tokens,omitempty"`
	CompletionTokens   int              `json:"completion_tokens,omitempty"`
	TTFMS              int64            `json:"ttf_ms,omitempty"`
	TotalMS            int64            `json:"total_ms,omitempty"`
	TokensPerSecond    float64          `json:"tokens_per_second,omitempty"`
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
		Summary:            "Generated profile recommendations.",
		Notes:              notes,
		RecommendedProfile: recommended,
	}

	client, err := buildClient(recommended)
	if err != nil {
		result.Status = "warn"
		result.Summary = "Recommendations generated, but the provider client could not be created."
		result.Notes = append(result.Notes, err.Error())
		return result
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req := llm.Request{
		Messages: []llm.Message{
			llm.NewTextMessage(llm.RoleSystem, "You are a benchmark probe. Answer with exactly one short sentence."),
			llm.NewTextMessage(llm.RoleUser, "Say: benchmark ok"),
		},
		MaxTokens:        32,
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
		result.Status = "warn"
		result.Summary = "Recommendations generated, but the live benchmark request failed."
		result.Notes = append(result.Notes, err.Error())
		return result
	}
	var firstToken time.Time
	var completionChars int
	var usage *llm.Usage
	for delta := range ch {
		if delta.Error != nil {
			result.Status = "warn"
			result.Summary = "Recommendations generated, but the live benchmark stream failed."
			result.Notes = append(result.Notes, delta.Error.Error())
			return result
		}
		if delta.Content != "" && firstToken.IsZero() {
			firstToken = time.Now()
		}
		completionChars += len(delta.Content)
		if delta.Usage != nil {
			usage = delta.Usage
		}
	}
	total := time.Since(start)
	if !firstToken.IsZero() {
		result.TTFMS = firstToken.Sub(start).Milliseconds()
	}
	result.TotalMS = total.Milliseconds()
	if usage != nil {
		result.PromptTokens = usage.PromptTokens
		result.CompletionTokens = usage.CompletionTokens
		if usage.CompletionTokens > 0 && total.Seconds() > 0 {
			result.TokensPerSecond = float64(usage.CompletionTokens) / total.Seconds()
		}
	} else if completionChars > 0 && total.Seconds() > 0 {
		approxTokens := completionChars / 4
		if approxTokens < 1 {
			approxTokens = 1
		}
		result.CompletionTokens = approxTokens
		result.TokensPerSecond = float64(approxTokens) / total.Seconds()
		result.Notes = append(result.Notes, "Backend did not return token usage; tokens/sec is estimated from output length.")
	}
	if result.TTFMS > 0 || result.TokensPerSecond > 0 {
		result.Summary = fmt.Sprintf("Probe complete: TTFT %d ms, %.1f tok/s", result.TTFMS, result.TokensPerSecond)
	}
	return result
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
