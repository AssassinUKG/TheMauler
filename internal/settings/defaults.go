package settings

// DefaultSettings returns sane global defaults (RTX 3090 / WSL2 baseline).
func DefaultSettings() Settings {
	return Settings{
		ActiveProfile: "qwen3.6-think",
		Tools: ToolsConfig{
			Enabled:            true,
			ConfirmReads:       false,
			ConfirmWrites:      true,
			ConfirmExec:        true,
			BashTimeout:        30,
			ShellBackend:       "auto",
			ArtifactTimeout:    30,
			WebEngine:          "auto",
			WebBaseURL:         "",
			WebAPIKeyEnv:       "",
			MaxSearches:        4,
			MaxFetches:         6,
			MaxFailedFetches:   3,
			MaxBrowserActions:  20,
			MaxToolResultChars: 8000,
			ActiveToolset:      "balanced",
			Toolsets:           DefaultToolsets(),
			EnabledTools: map[string]bool{
				"read_file":          true,
				"read_many":          true,
				"file_outline":       true,
				"read_chunks":        true,
				"read_pdf":           true,
				"write_file":         true,
				"edit_file":          true,
				"shell":              true,
				"bash":               true,
				"glob":               true,
				"grep":               true,
				"session_search":     true,
				"sqlite_schema":      true,
				"sqlite_query":       true,
				"todo_create":        true,
				"todo_update":        true,
				"todo_done":          true,
				"todo_blocked":       true,
				"todo_list":          true,
				"todo_clear":         true,
				"web_search":         true,
				"fetch_url":          true,
				"browser_open":       true,
				"browser_snapshot":   true,
				"browser_click":      true,
				"browser_type":       true,
				"browser_extract":    true,
				"browser_screenshot": true,
				"browser_close":      true,
				"browser_agent":      true,
				"skills_list":        true,
				"skill_view":         true,
				"subagent_research":  true,
				"subagent_review":    true,
				"subagent_testfix":   true,
				"subagent_summarize": true,
			},
		},
		Agents: AgentsConfig{
			ModeOverride:          "Auto",
			DefaultAutonomy:       "balanced",
			OfflineOnly:           false,
			MaxToolCalls:          40,
			RequirePlan:           true,
			NoThinkAfterToolCalls: 3,
			Presets:               defaultAgentPresets(),
		},
		Context: ContextConfig{
			AutoInjectFile:              false,
			AutoInjectCursor:            false,
			CompactionAt:                0.85,
			ShowCompaction:              true,
			MAULERMDPath:                "",
			ProjectDocMaxBytes:          32768,
			ProjectDocFallbackFilenames: []string{"MAULER.md", "AGENTS.md"},
		},
		Memory: MemoryConfig{
			Enabled:       true,
			AutoInject:    true,
			MaxEntries:    200,
			MaxInject:     8,
			MaxEntryChars: 1200,
		},
		Skills: SkillsConfig{
			Enabled:    true,
			AutoInject: true,
			MaxInject:  3,
			SkillsDir:  "",
		},
		Image: ImageConfig{
			VisionEnabled:    true,
			ClipboardMethod:  "auto",
			DisplayMethod:    "sixel",
			MaxDisplayWidth:  200,
			WSLPathTranslate: true,
		},
		UI: UIConfig{
			Theme:           "dark",
			AccentColor:     "#007acc",
			PrimaryColor:    "#007acc",
			StatusBar:       true,
			TokenCounter:    true,
			ThinkIndicator:  true,
			SyntaxHighlight: true,
			DiffColours:     true,
			ChatTimestamps:  false,
			TreeWidth:       0.20,
			ChatWidth:       0.50,
			ArtifactWidth:   0.30,
		},
		Logging: LoggingConfig{
			Enabled:        true,
			LogToolInputs:  true,
			LogToolResults: true,
			LogResponses:   true,
			MaxRuns:        500,
		},
		LogLevel: "info",
	}
}

func DefaultToolsets() map[string][]string {
	coreRead := []string{"read_file", "read_many", "file_outline", "read_chunks", "read_pdf", "glob", "grep", "session_search", "sqlite_schema", "sqlite_query", "todo_create", "todo_update", "todo_done", "todo_blocked", "todo_list", "todo_clear", "skills_list", "skill_view", "subagent_review", "subagent_summarize"}
	localCode := append(append([]string{}, coreRead...), "write_file", "edit_file", "shell", "bash")
	webResearch := append(append([]string{}, coreRead...), "web_search", "fetch_url", "browser_open", "browser_snapshot", "browser_extract", "browser_screenshot", "subagent_research")
	browser := append(append([]string{}, coreRead...), "browser_open", "browser_snapshot", "browser_click", "browser_type", "browser_extract", "browser_screenshot", "browser_close", "browser_agent")
	unrestricted := append(append([]string{}, localCode...), "web_search", "fetch_url", "browser_open", "browser_snapshot", "browser_click", "browser_type", "browser_extract", "browser_screenshot", "browser_close", "browser_agent", "subagent_research", "subagent_testfix")
	return map[string][]string{
		"safe":         append([]string{}, coreRead...),
		"local-code":   append(append([]string{}, localCode...), "subagent_testfix"),
		"web-research": webResearch,
		"browser":      browser,
		"memory":       []string{"session_search", "sqlite_schema", "sqlite_query", "todo_create", "todo_update", "todo_done", "todo_blocked", "todo_list", "todo_clear"},
		"offline":      append(append([]string{}, localCode...), "subagent_testfix"),
		"balanced":     append(append([]string{}, localCode...), "web_search", "fetch_url", "browser_open", "browser_snapshot", "browser_extract", "browser_screenshot", "subagent_research", "subagent_testfix"),
		"unrestricted": unrestricted,
	}
}

func defaultAgentPresets() map[string]AgentModePreset {
	return map[string]AgentModePreset{
		"Builder": {
			Enabled:       true,
			Autonomy:      "balanced",
			Toolset:       "balanced",
			ContextBudget: 32768,
			Instructions:  "Implement requested changes end to end, keep edits scoped, update docs when behavior changes, and run focused verification.",
			ToolPermissions: map[string]bool{
				"read_file": true, "read_pdf": true, "write_file": true, "edit_file": true, "shell": true, "glob": true, "grep": true,
				"web_search": true, "fetch_url": true, "browser_open": true, "browser_snapshot": true, "browser_extract": true,
			},
		},
		"Fixer": {
			Enabled:       true,
			Autonomy:      "balanced",
			Toolset:       "balanced",
			ContextBudget: 32768,
			Instructions:  "Reproduce or inspect failures first, identify the smallest likely cause, patch narrowly, and verify the exact failure path.",
			ToolPermissions: map[string]bool{
				"read_file": true, "read_pdf": true, "write_file": true, "edit_file": true, "shell": true, "glob": true, "grep": true,
				"web_search": true, "fetch_url": true,
			},
		},
		"Reviewer": {
			Enabled:       true,
			Autonomy:      "ask",
			Toolset:       "safe",
			ContextBudget: 24576,
			Instructions:  "Prioritize bugs, regressions, missing tests, and safety risks. Prefer read-only inspection unless explicitly asked to patch.",
			ToolPermissions: map[string]bool{
				"read_file": true, "read_pdf": true, "glob": true, "grep": true, "web_search": true, "fetch_url": true,
				"write_file": false, "edit_file": false, "shell": false,
			},
		},
		"Researcher": {
			Enabled:       true,
			Autonomy:      "balanced",
			Toolset:       "web-research",
			ContextBudget: 24576,
			Instructions:  "Search with a budget, rank sources by quality, fetch only promising sources, and stop with uncertainty after repeated failures.",
			ToolPermissions: map[string]bool{
				"read_file": true, "read_pdf": true, "glob": true, "grep": true, "web_search": true, "fetch_url": true,
				"browser_open": true, "browser_snapshot": true, "browser_click": true, "browser_type": true, "browser_extract": true, "browser_screenshot": true,
				"write_file": false, "edit_file": false,
			},
		},
		"Planner": {
			Enabled:       true,
			Autonomy:      "ask",
			Toolset:       "safe",
			ContextBudget: 16384,
			Instructions:  "Read enough context to plan, surface tradeoffs, and avoid file writes unless the user explicitly asks to implement.",
			ToolPermissions: map[string]bool{
				"read_file": true, "read_pdf": true, "glob": true, "grep": true, "web_search": true, "fetch_url": true,
				"write_file": false, "edit_file": false, "shell": false,
			},
		},
		"Auto": {
			Enabled:       true,
			Autonomy:      "balanced",
			Toolset:       "balanced",
			ContextBudget: 32768,
			Instructions:  "Choose the right working style for the task, inspect before editing, and verify changes when practical.",
		},
	}
}

// DefaultProfiles returns default model profiles tuned for RTX 3090 24 GB VRAM.
func DefaultProfiles() ProfilesFile {
	// Unsloth-recommended params per mode
	thinkCoding := GenerationParams{
		Temperature:     0.6,
		TopP:            0.95,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 0.0,
		MaxTokens:       8192,
		Seed:            -1,
	}
	thinkGeneral := GenerationParams{
		Temperature:     1.0,
		TopP:            0.95,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 0.0,
		MaxTokens:       4096,
		Seed:            -1,
	}
	noThink := GenerationParams{
		Temperature:     0.7,
		TopP:            0.8,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 1.5,
		MaxTokens:       4096,
		Seed:            -1,
	}

	// Base Qwen3.6-27B config — UD-Q4_K_XL on 3090 at 32K ctx
	qwenBase := Profile{
		Provider:      "llamacpp-local",
		ModelID:       "qwen/qwen3.6-27b",
		CtxTokens:     32768,
		Thinking:      true,
		PreserveThink: true,
		ThinkGeneral:  thinkGeneral,
		ThinkCoding:   thinkCoding,
		NoThink:       noThink,
	}

	qwenThink := qwenBase
	qwenThink.Name = "qwen3.6-think"

	qwenChat := qwenBase
	qwenChat.Name = "qwen3.6-chat"

	qwenNoThink := qwenBase
	qwenNoThink.Name = "qwen3.6-nothink"
	qwenNoThink.Thinking = false
	qwenNoThink.PreserveThink = false

	return ProfilesFile{
		Providers: map[string]Provider{
			"llamacpp-local": {
				Name:    "llamacpp-local",
				Backend: "llamacpp",
				BaseURL: "http://localhost:8080/v1",
			},
			"lmstudio-local": {
				Name:    "lmstudio-local",
				Backend: "lmstudio",
				BaseURL: "http://localhost:1234/v1",
			},
			"sglang-local": {
				Name:    "sglang-local",
				Backend: "openai-compatible",
				BaseURL: "http://localhost:30000/v1",
			},
			"vllm-local": {
				Name:    "vllm-local",
				Backend: "openai-compatible",
				BaseURL: "http://localhost:8000/v1",
			},
		},
		Profiles: map[string]Profile{
			"qwen3.6-think":   qwenThink,
			"qwen3.6-chat":    qwenChat,
			"qwen3.6-nothink": qwenNoThink,
		},
	}
}
