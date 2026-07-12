package settings

import "testing"

func TestValidateClampsCompaction(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Context.CompactionAt = 1
	cfg.Agents.MaxToolCalls = -5
	cfg.Agents.MaxRunSeconds = -1

	adjustments := cfg.Validate()

	if cfg.Context.CompactionAt != 0.85 {
		t.Fatalf("compaction_at = %v, want 0.85", cfg.Context.CompactionAt)
	}
	if cfg.Agents.MaxToolCalls != 200 {
		t.Fatalf("max_tool_calls = %d, want 200", cfg.Agents.MaxToolCalls)
	}
	if cfg.Agents.MaxRunSeconds != 0 {
		t.Fatalf("max_run_seconds = %d, want 0", cfg.Agents.MaxRunSeconds)
	}
	if len(adjustments) != 3 {
		t.Fatalf("adjustments = %#v, want 3 entries", adjustments)
	}
}

func TestReviewLoopConfigDefaultsPopulate(t *testing.T) {
	cfg := DefaultSettings()

	rl := cfg.Agents.ReviewLoop
	if !rl.Enabled || !rl.OnlyAutonomous || !rl.VerifyGate || !rl.CompletionRails || !rl.ReviewerPass {
		t.Fatalf("review loop defaults should enable the gate chain: %#v", rl)
	}
	if rl.MaxReviewCycles != 2 || rl.VerifyTimeoutSec != 120 || rl.ReviewerMaxTools != 15 {
		t.Fatalf("unexpected review loop numeric defaults: %#v", rl)
	}
	if len(rl.VerifyCommands) != 0 {
		t.Fatalf("verify commands should default to auto-detection, got %#v", rl.VerifyCommands)
	}
}

func TestReviewLoopConfigClamps(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Agents.ReviewLoop.MaxReviewCycles = 99
	cfg.Agents.ReviewLoop.VerifyTimeoutSec = 0
	cfg.Agents.ReviewLoop.ReviewerMaxTools = 99

	adjustments := cfg.Validate()

	if cfg.Agents.ReviewLoop.MaxReviewCycles != 5 {
		t.Fatalf("max_review_cycles = %d, want 5", cfg.Agents.ReviewLoop.MaxReviewCycles)
	}
	if cfg.Agents.ReviewLoop.VerifyTimeoutSec != 120 {
		t.Fatalf("verify_timeout_sec = %d, want 120", cfg.Agents.ReviewLoop.VerifyTimeoutSec)
	}
	if cfg.Agents.ReviewLoop.ReviewerMaxTools != 40 {
		t.Fatalf("reviewer_max_tools = %d, want 40", cfg.Agents.ReviewLoop.ReviewerMaxTools)
	}
	if len(adjustments) != 3 {
		t.Fatalf("adjustments = %#v, want 3 entries", adjustments)
	}
}

func TestReviewLoopConfigClampNegativeCycleToZero(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Agents.ReviewLoop.MaxReviewCycles = -1

	adjustments := cfg.Validate()

	if cfg.Agents.ReviewLoop.MaxReviewCycles != 0 {
		t.Fatalf("max_review_cycles = %d, want 0", cfg.Agents.ReviewLoop.MaxReviewCycles)
	}
	if len(adjustments) != 1 {
		t.Fatalf("adjustments = %#v, want 1 entry", adjustments)
	}
}

func TestValidateClampsMaxTokensToHalfCtx(t *testing.T) {
	pf := ProfilesFile{
		Providers: map[string]Provider{"local": {Name: "local"}},
		Profiles: map[string]Profile{
			"tiny": {
				Name:      "tiny",
				Provider:  "local",
				CtxTokens: 4096,
				ThinkGeneral: GenerationParams{
					MaxTokens: 5000,
					TopP:      0.95,
				},
				ThinkCoding: GenerationParams{
					MaxTokens: 4096,
					TopP:      0.95,
				},
				NoThink: GenerationParams{
					MaxTokens: 2048,
					TopP:      0.95,
				},
			},
		},
	}

	adjustments := pf.Validate()

	profile := pf.Profiles["tiny"]
	if profile.ThinkGeneral.MaxTokens != 2048 || profile.ThinkCoding.MaxTokens != 2048 || profile.NoThink.MaxTokens != 2048 {
		t.Fatalf("max tokens were not clamped to half context: %#v", profile)
	}
	if len(adjustments) != 2 {
		t.Fatalf("adjustments = %#v, want 2 entries", adjustments)
	}
}

func TestValidateClampsSamplers(t *testing.T) {
	pf := ProfilesFile{
		Providers: map[string]Provider{},
		Profiles: map[string]Profile{
			"bad": {
				CtxTokens: 0,
				ThinkGeneral: GenerationParams{
					Temperature: -0.1,
					TopP:        2,
					TopK:        -1,
					MinP:        1.2,
					MaxTokens:   -10,
				},
				ThinkCoding: GenerationParams{
					Temperature: 3,
					TopP:        0,
					MaxTokens:   9000,
				},
				NoThink: GenerationParams{
					Temperature: 0.7,
					TopP:        0.8,
					TopK:        20,
					MinP:        0,
					MaxTokens:   1024,
				},
			},
		},
	}

	adjustments := pf.Validate()

	profile := pf.Profiles["bad"]
	if profile.CtxTokens != 8192 {
		t.Fatalf("ctx_tokens = %d, want 8192", profile.CtxTokens)
	}
	if profile.ThinkGeneral.Temperature != 0 || profile.ThinkGeneral.TopP != 1 || profile.ThinkGeneral.TopK != 0 || profile.ThinkGeneral.MinP != 1 || profile.ThinkGeneral.MaxTokens != 2048 {
		t.Fatalf("thinking_general was not clamped: %#v", profile.ThinkGeneral)
	}
	if profile.ThinkCoding.Temperature != 2 || profile.ThinkCoding.TopP != 1 || profile.ThinkCoding.MaxTokens != 4096 {
		t.Fatalf("thinking_coding was not clamped: %#v", profile.ThinkCoding)
	}
	if len(adjustments) == 0 {
		t.Fatal("expected validation adjustments")
	}
}

func TestValidateRecordsUnknownProviderWithoutChangingIt(t *testing.T) {
	pf := ProfilesFile{
		Providers: map[string]Provider{"known": {Name: "known"}},
		Profiles: map[string]Profile{
			"bad-provider": {
				Provider:     "missing",
				CtxTokens:    8192,
				ThinkGeneral: validGenerationParams(),
				ThinkCoding:  validGenerationParams(),
				NoThink:      validGenerationParams(),
			},
		},
	}

	adjustments := pf.Validate()

	if pf.Profiles["bad-provider"].Provider != "missing" {
		t.Fatalf("provider was changed: %#v", pf.Profiles["bad-provider"])
	}
	if len(adjustments) != 1 {
		t.Fatalf("adjustments = %#v, want unknown provider record", adjustments)
	}
}

func TestValidateIsIdempotent(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Context.CompactionAt = 2
	cfg.Agents.MaxToolCalls = 5000
	if got := cfg.Validate(); len(got) == 0 {
		t.Fatal("expected first settings validation to adjust")
	}
	if got := cfg.Validate(); len(got) != 0 {
		t.Fatalf("second settings validation adjusted again: %#v", got)
	}

	pf := ProfilesFile{
		Providers: map[string]Provider{"local": {Name: "local"}},
		Profiles: map[string]Profile{
			"bad": {
				Provider:     "local",
				CtxTokens:    -1,
				ThinkGeneral: GenerationParams{Temperature: 4, TopP: -1, TopK: -1, MinP: -1, MaxTokens: -1},
				ThinkCoding:  GenerationParams{Temperature: 4, TopP: -1, TopK: -1, MinP: -1, MaxTokens: -1},
				NoThink:      GenerationParams{Temperature: 4, TopP: -1, TopK: -1, MinP: -1, MaxTokens: -1},
			},
		},
	}
	if got := pf.Validate(); len(got) == 0 {
		t.Fatal("expected first profiles validation to adjust")
	}
	if got := pf.Validate(); len(got) != 0 {
		t.Fatalf("second profiles validation adjusted again: %#v", got)
	}
}

func validGenerationParams() GenerationParams {
	return GenerationParams{
		Temperature: 0.7,
		TopP:        0.95,
		TopK:        20,
		MinP:        0,
		MaxTokens:   2048,
	}
}
