package settings

import "testing"

func TestNormaliseSettingsBackfillsNewDefaults(t *testing.T) {
	cfg := Settings{
		Tools: ToolsConfig{
			EnabledTools: map[string]bool{
				"read_file": true,
			},
		},
		Memory: MemoryConfig{MaxEntries: 10},
	}

	normaliseSettings(&cfg)

	if cfg.Tools.WebEngine != "auto" {
		t.Fatalf("WebEngine = %q, want auto", cfg.Tools.WebEngine)
	}
	if cfg.Tools.MaxSearches == 0 || cfg.Tools.MaxFetches == 0 || cfg.Tools.MaxBrowserActions == 0 {
		t.Fatalf("tool budgets were not backfilled: %#v", cfg.Tools)
	}
	if cfg.Agents.ModeOverride != "Auto" || cfg.Agents.MaxToolCalls == 0 || len(cfg.Agents.Presets) == 0 {
		t.Fatalf("agent defaults were not backfilled: %#v", cfg.Agents)
	}
	if cfg.Memory.MaxInject == 0 || cfg.Memory.MaxEntryChars == 0 {
		t.Fatalf("memory numeric defaults were not backfilled: %#v", cfg.Memory)
	}
	if _, ok := cfg.Tools.EnabledTools["browser_open"]; !ok {
		t.Fatalf("new enabled tool defaults were not merged: %#v", cfg.Tools.EnabledTools)
	}
	if _, ok := cfg.Tools.EnabledTools["todo_create"]; !ok {
		t.Fatalf("todo tool defaults were not merged: %#v", cfg.Tools.EnabledTools)
	}
	if _, ok := cfg.Tools.EnabledTools["read_pdf"]; !ok {
		t.Fatalf("PDF read tool default was not merged: %#v", cfg.Tools.EnabledTools)
	}
	if _, ok := cfg.Tools.EnabledTools["subagent_review"]; !ok {
		t.Fatalf("subagent tool defaults were not merged: %#v", cfg.Tools.EnabledTools)
	}
	if cfg.Tools.ActiveToolset != "balanced" || len(cfg.Tools.Toolsets["unrestricted"]) == 0 {
		t.Fatalf("toolset defaults were not backfilled: active=%q toolsets=%#v", cfg.Tools.ActiveToolset, cfg.Tools.Toolsets)
	}
}

func TestNormaliseSettingsPreservesEmptyProtectedPaths(t *testing.T) {
	cfg := Settings{
		Tools: ToolsConfig{
			ProtectedPaths: []string{},
		},
		Memory: MemoryConfig{MaxEntries: 10},
	}

	normaliseSettings(&cfg)

	if len(cfg.Tools.ProtectedPaths) != 0 {
		t.Fatalf("empty protected paths should stay empty, got %#v", cfg.Tools.ProtectedPaths)
	}
}

func TestNormaliseSettingsBackfillsEmptyMemoryConfig(t *testing.T) {
	cfg := Settings{}

	normaliseSettings(&cfg)

	if !cfg.Memory.Enabled || !cfg.Memory.AutoInject || cfg.Memory.MaxEntries == 0 || cfg.Memory.MaxInject == 0 {
		t.Fatalf("empty memory config was not defaulted: %#v", cfg.Memory)
	}
}

func TestNormaliseSettingsWithoutEnabledToolsStillNormalisesSafeRulesAndLogging(t *testing.T) {
	cfg := Settings{
		Tools: ToolsConfig{
			SafeRules: []ToolSafeRule{
				{Tool: " shell ", InputHash: "abc"},
				{Tool: "shell", InputHash: "abc"},
				{Tool: "", InputHash: "bad"},
			},
		},
	}

	normaliseSettings(&cfg)

	if cfg.Logging.MaxRuns == 0 {
		t.Fatalf("logging defaults were skipped: %#v", cfg.Logging)
	}
	if len(cfg.Tools.SafeRules) != 1 || cfg.Tools.SafeRules[0].Tool != "shell" {
		t.Fatalf("safe rules were not normalised: %#v", cfg.Tools.SafeRules)
	}
}

func TestEffectiveEnabledToolsAppliesActiveToolsetAsCoarseGate(t *testing.T) {
	cfg := DefaultSettings().Tools
	cfg.ActiveToolset = "safe"
	cfg.EnabledTools["write_file"] = true
	cfg.EnabledTools["web_search"] = true

	effective := EffectiveEnabledTools(cfg)

	if !effective["read_file"] || !effective["read_pdf"] || !effective["todo_create"] || !effective["subagent_review"] {
		t.Fatalf("safe toolset should include read/planning tools: %#v", effective)
	}
	if effective["write_file"] || effective["web_search"] || effective["browser_agent"] || effective["subagent_testfix"] {
		t.Fatalf("safe toolset should block write/web/browser-agent tools: %#v", effective)
	}
}

func TestEffectiveEnabledToolsHonoursPerToolDisableInsideToolset(t *testing.T) {
	cfg := DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	cfg.EnabledTools["shell"] = false
	cfg.EnabledTools["browser_agent"] = true

	effective := EffectiveEnabledTools(cfg)

	if effective["shell"] || effective["bash"] {
		t.Fatalf("shell disable should also disable bash alias: %#v", effective)
	}
	if !effective["browser_agent"] {
		t.Fatalf("unrestricted toolset should allow browser_agent when enabled: %#v", effective)
	}
}

func TestDefaultProfilesIncludeModernLocalProviderPresets(t *testing.T) {
	pf := DefaultProfiles()

	for name, wantURL := range map[string]string{
		"sglang-local": "http://localhost:30000/v1",
		"vllm-local":   "http://localhost:8000/v1",
	} {
		provider, ok := pf.Providers[name]
		if !ok {
			t.Fatalf("missing provider %s", name)
		}
		if provider.Backend != "openai-compatible" || provider.BaseURL != wantURL {
			t.Fatalf("%s provider = %#v", name, provider)
		}
	}

	qwen := pf.Profiles["qwen3.6-think"]
	if qwen.ThinkGeneral.PresencePenalty != 0.0 {
		t.Fatalf("thinking general presence_penalty = %v, want 0.0", qwen.ThinkGeneral.PresencePenalty)
	}
	if qwen.ThinkCoding.PresencePenalty != 0.0 {
		t.Fatalf("thinking coding presence_penalty = %v, want 0.0", qwen.ThinkCoding.PresencePenalty)
	}
	if qwen.NoThink.PresencePenalty != 1.5 {
		t.Fatalf("nothinking presence_penalty = %v, want 1.5", qwen.NoThink.PresencePenalty)
	}

	gemmaQAT, ok := pf.Profiles["gemma4-26b-a4b-qat"]
	if !ok {
		t.Fatalf("missing gemma4-26b-a4b-qat default profile")
	}
	if gemmaQAT.ModelID != "gemma-4-26B-A4B-it-QAT-Q4_0.gguf" {
		t.Fatalf("gemma4-26b-a4b-qat model_id = %q, want live InferenceBridge id", gemmaQAT.ModelID)
	}
	if gemmaQAT.CtxTokens != 49152 || gemmaQAT.Thinking || gemmaQAT.PreserveThink {
		t.Fatalf("gemma4-26b-a4b-qat defaults = %#v, want snappy 49,152-token nothinking profile", gemmaQAT)
	}
	if gemmaQAT.NoThink.Temperature != 1.0 || gemmaQAT.NoThink.TopP != 0.95 || gemmaQAT.NoThink.TopK != 64 || gemmaQAT.NoThink.MinP != 0.05 {
		t.Fatalf("gemma4-26b-a4b-qat sampling = %#v, want live InferenceBridge temp/top_p/top_k/min_p", gemmaQAT.NoThink)
	}
}

func TestMigrateProvidersBackfillsGemma426BQATAndRemovesStale12B(t *testing.T) {
	pf := &ProfilesFile{
		Providers: map[string]Provider{},
		Profiles: map[string]Profile{
			"gemma4-12b":     {Name: "gemma4-12b", Provider: "llamacpp-local", ModelID: "google/gemma-4-12B-it"},
			"gemma4-12b-qat": {Name: "gemma4-12b-qat", Provider: "llamacpp-local", ModelID: "google/gemma-4-12B-it-QAT"},
		},
	}

	migrateProviders(pf)

	if _, ok := pf.Profiles["gemma4-26b-a4b-qat"]; !ok {
		t.Fatalf("gemma4-26b-a4b-qat profile was not backfilled: %#v", pf.Profiles)
	}
	if _, ok := pf.Profiles["gemma4-12b"]; ok {
		t.Fatalf("stale gemma4-12b profile was not removed")
	}
	if _, ok := pf.Profiles["gemma4-12b-qat"]; ok {
		t.Fatalf("stale gemma4-12b-qat profile was not removed")
	}
}

func TestMigrateProvidersRepairsBadGemma426BQATDefaults(t *testing.T) {
	pf := &ProfilesFile{
		Providers: map[string]Provider{},
		Profiles: map[string]Profile{
			"gemma4-26b-a4b-qat": {
				Name:      "gemma4-26b-a4b-qat",
				Provider:  "llamacpp-local",
				ModelID:   "unsloth/gemma-4-26B-A4B-it-qat-GGUF",
				CtxTokens: 131072,
			},
		},
	}

	migrateProviders(pf)

	got := pf.Profiles["gemma4-26b-a4b-qat"]
	if got.ModelID != "gemma-4-26B-A4B-it-QAT-Q4_0.gguf" || got.CtxTokens != 49152 {
		t.Fatalf("repaired profile = %#v, want live InferenceBridge model id and context", got)
	}
}
