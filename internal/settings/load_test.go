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
	if cfg.Agents.ModeOverride != "Auto" || cfg.Agents.MaxToolCalls == 0 || cfg.Agents.MaxRunSeconds == 0 || len(cfg.Agents.Presets) == 0 {
		t.Fatalf("agent defaults were not backfilled: %#v", cfg.Agents)
	}
	if cfg.Agents.NoThinkAfterToolCalls != 2 {
		t.Fatalf("no_think_after_tool_calls = %d, want 2", cfg.Agents.NoThinkAfterToolCalls)
	}
	if cfg.Agents.ThinkingMode != "auto" {
		t.Fatalf("thinking_mode = %q, want auto", cfg.Agents.ThinkingMode)
	}
	if cfg.Memory.MaxInject == 0 || cfg.Memory.MaxEntryChars == 0 {
		t.Fatalf("memory numeric defaults were not backfilled: %#v", cfg.Memory)
	}
	if cfg.Telegram.DefaultMode != "Auto" || cfg.Telegram.DefaultToolset != "unrestricted" || cfg.Telegram.ProgressIntervalS == 0 {
		t.Fatalf("telegram defaults were not backfilled: %#v", cfg.Telegram)
	}
	if _, ok := cfg.Tools.EnabledTools["browser"]; !ok {
		t.Fatalf("new enabled tool defaults were not merged: %#v", cfg.Tools.EnabledTools)
	}
	if _, ok := cfg.Tools.EnabledTools["todo_write"]; !ok {
		t.Fatalf("todo tool defaults were not merged: %#v", cfg.Tools.EnabledTools)
	}
	if _, ok := cfg.Tools.EnabledTools["read"]; !ok {
		t.Fatalf("read tool default was not merged: %#v", cfg.Tools.EnabledTools)
	}
	if _, ok := cfg.Tools.EnabledTools["task"]; !ok {
		t.Fatalf("subagent tool defaults were not merged: %#v", cfg.Tools.EnabledTools)
	}
	if cfg.Tools.ActiveToolset != "run-lean" || len(cfg.Tools.Toolsets["unrestricted"]) == 0 || len(cfg.Tools.Toolsets["explore"]) == 0 {
		t.Fatalf("toolset defaults were not backfilled: active=%q toolsets=%#v", cfg.Tools.ActiveToolset, cfg.Tools.Toolsets)
	}
}

func TestNormaliseSettingsPreservesThinkingMode(t *testing.T) {
	for _, mode := range []string{"auto", "on", "off"} {
		cfg := DefaultSettings()
		cfg.Agents.ThinkingMode = mode
		normaliseSettings(&cfg)
		if cfg.Agents.ThinkingMode != mode {
			t.Fatalf("thinking mode %q normalized to %q", mode, cfg.Agents.ThinkingMode)
		}
	}
	cfg := DefaultSettings()
	cfg.Agents.ThinkingMode = "invalid"
	normaliseSettings(&cfg)
	if cfg.Agents.ThinkingMode != "auto" {
		t.Fatalf("invalid thinking mode = %q, want auto", cfg.Agents.ThinkingMode)
	}
}

func TestNormaliseTelegramSettings(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Telegram = TelegramConfig{
		BotUsername:       " @TheMaulerBot ",
		DefaultProject:    `C:\Users\richa\Documents\HTB`,
		DefaultMode:       "",
		DefaultToolset:    "",
		ProgressIntervalS: 0,
		VoiceReplies:      "bad",
		TranscriptionMode: "bad",
		AllowFrom:         []string{" 123 ", "123", " ", "@operator"},
	}

	normaliseSettings(&cfg)

	if cfg.Telegram.BotUsername != "TheMaulerBot" {
		t.Fatalf("bot username was not normalised: %#v", cfg.Telegram)
	}
	if cfg.Telegram.DefaultProject != "C:/Users/richa/Documents/HTB" {
		t.Fatalf("default project was not slash-normalised: %q", cfg.Telegram.DefaultProject)
	}
	if cfg.Telegram.DefaultMode != "Auto" || cfg.Telegram.DefaultToolset != "unrestricted" {
		t.Fatalf("telegram routing defaults were not restored: %#v", cfg.Telegram)
	}
	if cfg.Telegram.ProgressIntervalS != 10 || cfg.Telegram.VoiceReplies != "on_voice" || cfg.Telegram.TranscriptionMode != "local" {
		t.Fatalf("telegram invalid options were not restored: %#v", cfg.Telegram)
	}
	if len(cfg.Telegram.AllowFrom) != 2 || cfg.Telegram.AllowFrom[0] != "123" || cfg.Telegram.AllowFrom[1] != "@operator" {
		t.Fatalf("allow list was not normalised: %#v", cfg.Telegram.AllowFrom)
	}
}

func TestNormaliseSettingsMigratesOldBudgetAndOpsDefaults(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Tools.MaxSearches = 8
	cfg.Tools.MaxFetches = 12
	cfg.Tools.MaxFailedFetches = 5
	cfg.Tools.MaxBrowserActions = 35
	cfg.Tools.MaxToolResultChars = 8000
	cfg.Tools.ToolResultPreviewChars = 0
	cfg.Tools.ToolResultAggregateChars = 0
	cfg.Agents.MaxToolCalls = 100
	cfg.Agents.Presets["Ops"] = AgentModePreset{
		Enabled:  true,
		Autonomy: "balanced",
		Toolset:  "local-code",
		ToolPermissions: map[string]bool{
			"shell": true, "bash": true, "web_search": false, "fetch_url": false,
		},
	}

	normaliseSettings(&cfg)

	if cfg.Tools.MaxSearches != 16 || cfg.Tools.MaxFetches != 32 || cfg.Tools.MaxFailedFetches != 10 || cfg.Tools.MaxBrowserActions != 80 || cfg.Tools.MaxToolResultChars != 12000 || cfg.Tools.ToolResultPreviewChars != 2000 || cfg.Tools.ToolResultAggregateChars != 24000 {
		t.Fatalf("old tool budgets were not migrated: %#v", cfg.Tools)
	}
	if cfg.Agents.MaxToolCalls != 200 {
		t.Fatalf("old max tool calls was not migrated: %d", cfg.Agents.MaxToolCalls)
	}
	ops := cfg.Agents.Presets["Ops"]
	if ops.Toolset != "ops-lean" || !ops.ToolPermissions["web_search"] || !ops.ToolPermissions["fetch_url"] || ops.ToolPermissions["bash"] {
		t.Fatalf("old Ops preset was not migrated: %#v", ops)
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
	cfg.EnabledTools["write"] = true
	cfg.EnabledTools["web_search"] = true

	effective := EffectiveEnabledTools(cfg)

	if !effective["read"] || !effective["todo_write"] || !effective["skill"] || !effective["sqlite"] {
		t.Fatalf("safe toolset should include read/planning tools: %#v", effective)
	}
	if effective["write"] || effective["web_search"] || effective["browser"] || effective["task"] {
		t.Fatalf("safe toolset should block write/web/browser-agent tools: %#v", effective)
	}
}

func TestInteractiveTerminalToolsOnlyInShellBearingToolsets(t *testing.T) {
	defaults := DefaultSettings()
	if !defaults.Tools.EnabledTools["terminal_send"] || !defaults.Tools.EnabledTools["terminal_read"] {
		t.Fatalf("interactive terminal tools missing from EnabledTools: %#v", defaults.Tools.EnabledTools)
	}
	shellBearing := map[string]bool{
		"local-code":   true,
		"ops-lean":     true,
		"offline":      true,
		"balanced":     true,
		"run-lean":     true,
		"unrestricted": true,
	}
	for name := range defaults.Tools.Toolsets {
		cfg := defaults.Tools
		cfg.ActiveToolset = name
		effective := EffectiveEnabledTools(cfg)
		if shellBearing[name] {
			if !effective["terminal_send"] || !effective["terminal_read"] {
				t.Fatalf("%s should allow interactive terminal tools: %#v", name, effective)
			}
			continue
		}
		if effective["terminal_send"] || effective["terminal_read"] {
			t.Fatalf("%s should not allow interactive terminal tools: %#v", name, effective)
		}
	}
}

func TestDefaultToolsetsKeepBashAliasOutOfModelSurface(t *testing.T) {
	toolsets := DefaultToolsets()
	for name, tools := range toolsets {
		for _, tool := range tools {
			if tool == "bash" {
				t.Fatalf("%s toolset should not expose bash compatibility alias: %#v", name, tools)
			}
		}
	}
	if got := len(toolsets["run-lean"]); got > 24 {
		t.Fatalf("run-lean should stay compact, got %d tools: %#v", got, toolsets["run-lean"])
	}
	if got := len(toolsets["ops-lean"]); got > 28 {
		t.Fatalf("ops-lean should stay compact, got %d tools: %#v", got, toolsets["ops-lean"])
	}
}

func TestBugBountyReviewDefaultsAreReadOriented(t *testing.T) {
	cfg := DefaultSettings()
	preset, ok := cfg.Agents.Presets["Bug Bounty Hunter"]
	if !ok || !preset.Enabled {
		t.Fatalf("Bug Bounty Hunter preset missing or disabled: %#v", preset)
	}
	if preset.Toolset != "bug-bounty-review" || preset.Autonomy != "ask" {
		t.Fatalf("Bug Bounty Hunter defaults = %#v, want ask/bug-bounty-review", preset)
	}

	allowed := map[string]bool{}
	for _, name := range cfg.Tools.Toolsets["bug-bounty-review"] {
		allowed[name] = true
	}
	for _, name := range []string{"read", "glob", "grep", "session_search", "todo_write", "engagement", "evidence_bundle", "http_probe", "web_search", "fetch_url", "browser", "task"} {
		if !allowed[name] {
			t.Fatalf("bug-bounty-review should include %q: %#v", name, cfg.Tools.Toolsets["bug-bounty-review"])
		}
	}
	for _, name := range []string{"write", "edit", "shell", "terminal_send", "start_listener", "run_script"} {
		if allowed[name] || preset.ToolPermissions[name] {
			t.Fatalf("bug-bounty-review should block mutating/active tool %q", name)
		}
	}
}

func TestNormaliseSettingsBackfillsBugBountyAgentWithoutOverwritingCustomPresets(t *testing.T) {
	cfg := DefaultSettings()
	delete(cfg.Agents.Presets, "Bug Bounty Hunter")
	cfg.Agents.Presets["Reviewer"] = AgentModePreset{Enabled: true, Instructions: "keep my custom reviewer"}
	delete(cfg.Tools.Toolsets, "bug-bounty-review")

	normaliseSettings(&cfg)

	if _, ok := cfg.Agents.Presets["Bug Bounty Hunter"]; !ok {
		t.Fatal("Bug Bounty Hunter preset was not backfilled")
	}
	if got := cfg.Agents.Presets["Reviewer"].Instructions; got != "keep my custom reviewer" {
		t.Fatalf("custom preset was overwritten: %q", got)
	}
	if len(cfg.Tools.Toolsets["bug-bounty-review"]) == 0 {
		t.Fatal("bug-bounty-review toolset was not backfilled")
	}
}

func TestEffectiveEnabledToolsHonoursPerToolDisableInsideToolset(t *testing.T) {
	cfg := DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	cfg.EnabledTools["shell"] = false
	cfg.EnabledTools["browser"] = true

	effective := EffectiveEnabledTools(cfg)

	if effective["shell"] || effective["bash"] {
		t.Fatalf("shell disable should also disable bash alias: %#v", effective)
	}
	if !effective["browser"] {
		t.Fatalf("unrestricted toolset should allow browser when enabled: %#v", effective)
	}
}

func TestEffectiveEnabledToolsDoesNotAdvertiseBashAlias(t *testing.T) {
	cfg := DefaultSettings().Tools
	cfg.ActiveToolset = "run-lean"

	effective := EffectiveEnabledTools(cfg)

	if !effective["shell"] {
		t.Fatalf("balanced toolset should keep shell enabled: %#v", effective)
	}
	if effective["bash"] {
		t.Fatalf("bash compatibility alias should not be model-facing by default: %#v", effective)
	}
}

func TestNormaliseSettingsCanonicalizesAgentPresetToolPermissions(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Agents.Presets["Ops"] = AgentModePreset{
		Enabled: true,
		ToolPermissions: map[string]bool{
			"bash":         true,
			"read_file":    true,
			"skill_view":   true,
			"terminal_run": true,
			"browser_open": true,
		},
	}

	normaliseSettings(&cfg)

	perms := cfg.Agents.Presets["Ops"].ToolPermissions
	for _, legacy := range []string{"bash", "read_file", "skill_view", "terminal_run", "browser_open"} {
		if perms[legacy] {
			t.Fatalf("legacy preset permission %q survived normalization: %#v", legacy, perms)
		}
	}
	for _, canonical := range []string{"shell", "read", "skill", "terminal_send", "browser"} {
		if !perms[canonical] {
			t.Fatalf("canonical preset permission %q missing after normalization: %#v", canonical, perms)
		}
	}
}

func TestDefaultProfilesIncludeModernLocalProviderPresets(t *testing.T) {
	pf := DefaultProfiles()
	inferenceBridge, ok := pf.Providers["inference-bridge"]
	if !ok || inferenceBridge.Backend != "llamacpp" || inferenceBridge.BaseURL != "http://127.0.0.1:8800/v1" {
		t.Fatalf("inference-bridge provider = %#v, want the primary local endpoint", inferenceBridge)
	}

	for name, wantURL := range map[string]string{
		"openrouter":   "https://openrouter.ai/api/v1",
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
	if pf.Providers["openrouter"].APIKeyEnv != "OPENROUTER_API_KEY" {
		t.Fatalf("openrouter provider = %#v", pf.Providers["openrouter"])
	}

	qwen38 := pf.Profiles["qwen3.8-agent-stability"]
	if qwen38.Provider != "inference-bridge" || qwen38.ModelID != "Qwen3.8-27B-Q4_K_M.gguf" || qwen38.CtxTokens != 35000 || !qwen38.Thinking || !qwen38.PreserveThink {
		t.Fatalf("Qwen3.8 default = %#v, want the role-aware 35K agent profile", qwen38)
	}
	if qwen38.NoThink.Temperature != 0.7 || qwen38.NoThink.TopP != 0.8 || qwen38.NoThink.PresencePenalty != 1.5 || qwen38.NoThink.RepeatPenalty != 1.0 {
		t.Fatalf("Qwen3.8 no-thinking sampling = %#v", qwen38.NoThink)
	}

	qwen := pf.Profiles["qwen3.6-think"]
	if qwen.Provider != "inference-bridge" || qwen.ModelID != "Qwen3.6-27B-UD-Q4_K_XL.gguf" || qwen.CtxTokens != 35000 {
		t.Fatalf("Qwen default = %#v, want the verified UD-Q4_K_XL winner at 35K", qwen)
	}
	if qwen.ThinkGeneral.PresencePenalty != 0.0 {
		t.Fatalf("thinking general presence_penalty = %v, want 0.0", qwen.ThinkGeneral.PresencePenalty)
	}
	if qwen.ThinkCoding.PresencePenalty != 0.0 {
		t.Fatalf("thinking coding presence_penalty = %v, want 0.0", qwen.ThinkCoding.PresencePenalty)
	}
	if qwen.NoThink.PresencePenalty != 0.0 {
		t.Fatalf("nothinking presence_penalty = %v, want 0.0", qwen.NoThink.PresencePenalty)
	}
	if chat := pf.Profiles["qwen3.6-chat"]; chat.Thinking || chat.PreserveThink {
		t.Fatalf("qwen3.6-chat should default to no-thinking for snappy chat/tool reliability: %#v", chat)
	}
	if noThink := pf.Profiles["qwen3.6-nothink"]; noThink.Provider != "inference-bridge" || noThink.ModelID != "Qwen3.6-27B-UD-Q4_K_XL.gguf" || noThink.CtxTokens != 35000 {
		t.Fatalf("qwen3.6-nothink default = %#v, want the verified local winner", noThink)
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

func TestMigrateProvidersBackfillsFamilyRepeatPenalty(t *testing.T) {
	pf := &ProfilesFile{
		Providers: map[string]Provider{"inference-bridge": {Name: "inference-bridge", Backend: "llamacpp", BaseURL: "http://127.0.0.1:8800/v1"}},
		Profiles: map[string]Profile{
			"qwen": {
				Name:     "qwen",
				Provider: "inference-bridge",
				ModelID:  "Qwen3.6-27B-Fable-Fus-Q4_K_M.gguf",
			},
			"gemma": {
				Name:     "gemma",
				Provider: "inference-bridge",
				ModelID:  "Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced-Q4_K_M.gguf",
			},
		},
	}

	migrateProviders(pf)
	if got := pf.Profiles["qwen"].NoThink.RepeatPenalty; got != 1.05 {
		t.Fatalf("Qwen repeat_penalty = %v, want 1.05", got)
	}
	if got := pf.Profiles["gemma"].NoThink.RepeatPenalty; got != 1.1 {
		t.Fatalf("HauhauCS Gemma repeat_penalty = %v, want 1.1", got)
	}
}
