package settings

// DefaultSettings returns sane global defaults (RTX 3090 / WSL2 baseline).
func DefaultSettings() Settings {
	return Settings{
		ActiveProfile: "qwen3.8-agent-stability",
		Tools: ToolsConfig{
			Enabled:                  true,
			ConfirmReads:             false,
			ConfirmWrites:            true,
			ConfirmExec:              true,
			BashTimeout:              120,
			ShellBackend:             "auto",
			ShellMode:                "shared_terminal",
			ShellDistro:              "",
			ShellUser:                "",
			ArtifactTimeout:          30,
			WebEngine:                "auto",
			WebBaseURL:               "",
			WebAPIKeyEnv:             "",
			MaxSearches:              16,
			MaxFetches:               32,
			MaxFailedFetches:         10,
			MaxBrowserActions:        80,
			MaxToolResultChars:       12000,
			ToolResultPreviewChars:   2000,
			ToolResultAggregateChars: 24000,
			ProtectedPaths:           nil,
			ActiveToolset:            "run-lean",
			TaskRoutingMode:          "auto",
			Toolsets:                 DefaultToolsets(),
			EnabledTools: map[string]bool{
				"read":                 true,
				"write":                true,
				"edit":                 true,
				"shell":                true,
				"run_script":           true,
				"glob":                 true,
				"grep":                 true,
				"session_search":       true,
				"sqlite":               true,
				"todo_write":           true,
				"engagement":           true,
				"web_search":           true,
				"fetch_url":            true,
				"browser":              true,
				"skill":                true,
				"memory":               true,
				"file_changes":         true,
				"progress":             true,
				"http_probe":           true,
				"evidence_bundle":      true,
				"terminal_send":        true,
				"terminal_read":        true,
				"start_listener":       true,
				"task":                 true,
				"set_reasoning_effort": true,
				"read_tool_result":     true,
				"generate_image":       true,
			},
		},
		Agents: AgentsConfig{
			ModeOverride:            "Auto",
			DefaultConversationMode: "adaptive",
			DefaultAutonomy:         "balanced",
			OfflineOnly:             false,
			MaxToolCalls:            200,
			MaxRunSeconds:           1800,
			RequirePlan:             true,
			NoThinkAfterToolCalls:   2,
			ReasoningEffort:         "auto",
			ThinkingMode:            "auto",
			ReviewLoop: ReviewLoopConfig{
				Enabled:            true,
				OnlyAutonomous:     true,
				MaxReviewCycles:    2,
				VerifyGate:         true,
				VerifyTimeoutSec:   120,
				CompletionRails:    true,
				CompletionBlocking: true,
				ReviewerPass:       true,
				ReviewerMaxTools:   15,
			},
			Presets: defaultAgentPresets(),
		},
		Environment: EnvironmentConfig{
			MainOS:               "windows",
			AIShellBackend:       "wsl",
			AIShellDistro:        "kali-linux",
			AIShellUser:          "root",
			TargetWorkBackend:    "ai_shell",
			ListenerBackend:      "windows_powershell",
			ListenerCommand:      "ncat.exe -lvp {port}",
			LHOSTSource:          "selected_vpn_interface",
			PreferTerminalTools:  true,
			UserCorrectionPolicy: "latest_user_wins",
			ReverseShellGuidance: "Use WSL/Kali for outbound target recon/exploit work. Use Windows PowerShell ncat.exe for reverse-shell listeners because VPN callbacks may not reliably reach WSL. Once a shell connects, commands in that terminal are target-shell commands.",
		},
		Context: ContextConfig{
			AutoInjectFile:              false,
			AutoInjectCursor:            false,
			CompactionAt:                0.85,
			ShowCompaction:              true,
			MAULERMDPath:                "",
			ProjectDocMaxBytes:          32768,
			ProjectDocFallbackFilenames: []string{"MAULER.md", "AGENTS.md"},
			Lab: LabContext{
				ID:               "default",
				Name:             "HTB / Pentest box",
				Hostname:         "boxname.htb",
				OpsProfile:       "pentesting",
				EvidencePolicy:   "research_assisted",
				AccessPreference: "auto",
			},
			ActiveLabProfile: "default",
			LabProfiles: []LabProfile{{
				ID:               "default",
				Name:             "HTB / Pentest box",
				Hostname:         "boxname.htb",
				OpsProfile:       "pentesting",
				EvidencePolicy:   "research_assisted",
				AccessPreference: "auto",
			}},
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
			VideoEnabled:     true,
			VideoMaxFrames:   8,
			VideoFrameWidth:  768,
			VideoTranscribe:  true,
		},
		Telegram: TelegramConfig{
			Enabled:           false,
			RequireMention:    true,
			AllowFrom:         nil,
			DefaultMode:       "Auto",
			DefaultToolset:    "unrestricted",
			SendProgress:      true,
			ProgressIntervalS: 10,
			VoiceReplies:      "on_voice",
			TranscriptionMode: "local",
		},
		Audio: AudioConfig{
			Enabled:        true,
			Mode:           "push_to_talk",
			STTEngine:      "whisper",
			TTSEngine:      "auto",
			Voice:          "af_heart",
			Speed:          1.0,
			VADThreshold:   0.55,
			BargeIn:        true,
			SpeakReplies:   true,
			ClauseMinChars: 36,
		},
		UI: UIConfig{
			Theme:               "mauler-ops",
			AccentColor:         "#4ade80",
			PrimaryColor:        "#16a34a",
			StatusBar:           true,
			TokenCounter:        true,
			ThinkIndicator:      true,
			SyntaxHighlight:     true,
			DiffColours:         true,
			ChatTimestamps:      false,
			ToolCountdown:       true,
			TerminalDefaultOpen: true,
			TerminalHeight:      260,
			TreeWidth:           0.20,
			ChatWidth:           0.50,
			ArtifactWidth:       0.30,
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
	coreRead := []string{"read", "glob", "grep", "sqlite", "session_search", "skill", "memory", "progress", "read_tool_result", "todo_write", "generate_image"}
	localCode := append(append([]string{}, coreRead...), "write", "edit", "shell", "run_script", "terminal_send", "terminal_read", "start_listener", "http_probe", "evidence_bundle", "file_changes", "set_reasoning_effort", "task", "engagement")
	runLean := []string{"read", "write", "edit", "glob", "grep", "shell", "terminal_send", "terminal_read", "run_script", "http_probe", "start_listener", "evidence_bundle", "memory", "progress", "read_tool_result", "todo_write", "skill", "set_reasoning_effort", "task", "engagement"}
	opsLean := append(append([]string{}, runLean...), "session_search")
	explore := []string{"read", "glob", "grep"}
	webResearch := []string{"read", "glob", "grep", "web_search", "fetch_url", "browser", "memory", "progress", "read_tool_result", "todo_write", "generate_image", "task"}
	bugBountyReview := []string{"read", "glob", "grep", "session_search", "memory", "progress", "read_tool_result", "todo_write", "engagement", "evidence_bundle", "http_probe", "web_search", "fetch_url", "browser", "task"}
	browser := append(append([]string{}, coreRead...), "browser", "web_search", "fetch_url")
	unrestricted := append(append([]string{}, localCode...), "web_search", "fetch_url", "browser")
	return map[string][]string{
		"safe":              append([]string{}, coreRead...),
		"run-lean":          runLean,
		"ops-lean":          opsLean,
		"local-code":        localCode,
		"explore":           explore,
		"web-research":      webResearch,
		"bug-bounty-review": bugBountyReview,
		"browser":           browser,
		"memory":            {"memory", "file_changes", "progress", "session_search", "read_tool_result", "sqlite", "todo_write", "skill"},
		"offline":           localCode,
		"balanced":          runLean,
		"unrestricted":      unrestricted,
	}
}

func defaultAgentPresets() map[string]AgentModePreset {
	return map[string]AgentModePreset{
		"Ops": {
			Enabled:       true,
			Profile:       "qwen3.8-uncensored-agent-stability",
			Autonomy:      "balanced",
			Toolset:       "ops-lean",
			ContextBudget: 32768,
			Instructions:  "Operate against authorised lab/client targets from WSL/Kali first. Use WSL shell for target interaction, target DNS, VPN-routed traffic, scans, curl/ffuf/gobuster, exploit checks, and evidence capture. Public web research is allowed for CVEs, docs, tool syntax, and exploit background, but verify everything against live target evidence before acting. Keep notes and report evidence current. Do not perform remediation or fix client systems unless the user explicitly changes the task.",
			ToolPermissions: map[string]bool{
				"read": true, "write": true, "edit": true, "shell": true, "run_script": true, "terminal_send": true, "terminal_read": true, "glob": true, "grep": true, "http_probe": true, "evidence_bundle": true,
				"skill": true, "file_changes": true, "progress": true, "todo_write": true, "engagement": true, "web_search": true, "fetch_url": true, "browser": true, "task": true,
			},
		},
		"Builder": {
			Enabled:       true,
			Autonomy:      "balanced",
			Toolset:       "balanced",
			ContextBudget: 32768,
			Instructions:  "Implement requested changes end to end, keep edits scoped, update docs when behavior changes, and run focused verification.",
			ToolPermissions: map[string]bool{
				"read": true, "file_changes": true, "write": true, "edit": true, "shell": true, "run_script": true, "terminal_send": true, "terminal_read": true, "glob": true, "grep": true,
				"web_search": true, "fetch_url": true, "browser": true, "task": true,
			},
		},
		"Fixer": {
			Enabled:       true,
			Autonomy:      "balanced",
			Toolset:       "balanced",
			ContextBudget: 32768,
			Instructions:  "Reproduce or inspect failures first, identify the smallest likely cause, patch narrowly, and verify the exact failure path.",
			ToolPermissions: map[string]bool{
				"read": true, "file_changes": true, "write": true, "edit": true, "shell": true, "run_script": true, "terminal_send": true, "terminal_read": true, "glob": true, "grep": true,
				"web_search": true, "fetch_url": true, "task": true,
			},
		},
		"Reviewer": {
			Enabled:       true,
			Autonomy:      "ask",
			Toolset:       "safe",
			ContextBudget: 24576,
			Instructions:  "Prioritize bugs, regressions, missing tests, and safety risks. Prefer read-only inspection unless explicitly asked to patch.",
			ToolPermissions: map[string]bool{
				"read": true, "file_changes": true, "glob": true, "grep": true, "web_search": true, "fetch_url": true,
				"write": false, "edit": false, "shell": false,
			},
		},
		"Researcher": {
			Enabled:       true,
			Autonomy:      "balanced",
			Toolset:       "web-research",
			ContextBudget: 24576,
			Instructions:  "Search with a budget, rank sources by quality, fetch only promising sources, and stop with uncertainty after repeated failures.",
			ToolPermissions: map[string]bool{
				"read": true, "file_changes": true, "glob": true, "grep": true, "web_search": true, "fetch_url": true, "browser": true, "task": true,
				"write": false, "edit": false,
			},
		},
		"Planner": {
			Enabled:       true,
			Autonomy:      "ask",
			Toolset:       "safe",
			ContextBudget: 16384,
			Instructions:  "Read enough context to plan, surface tradeoffs, and avoid file writes unless the user explicitly asks to implement.",
			ToolPermissions: map[string]bool{
				"read": true, "file_changes": true, "glob": true, "grep": true, "web_search": true, "fetch_url": true,
				"write": false, "edit": false, "shell": false,
			},
		},
		"Bug Bounty Hunter": {
			Enabled:       true,
			Profile:       "qwen3.8-uncensored-agent-stability",
			Autonomy:      "ask",
			Toolset:       "bug-bounty-review",
			ContextBudget: 32768,
			Instructions:  "Treat Critical, High, Medium, and Low strictly as manual testing priority, not vulnerability severity. Prefer a concise, deduplicated, evidence-linked assessment over a scanner-style list.",
			ToolPermissions: map[string]bool{
				"read": true, "glob": true, "grep": true, "session_search": true, "memory": true,
				"progress": true, "read_tool_result": true, "todo_write": true, "engagement": true,
				"evidence_bundle": true, "http_probe": true, "web_search": true, "fetch_url": true,
				"browser": true, "task": true,
				"write": false, "edit": false, "shell": false, "run_script": false,
				"terminal_send": false, "terminal_read": false, "start_listener": false,
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
	// Qwen3.8 official generation defaults. Local output stays capped at 8K
	// because the verified RTX 3090 working context is 35K, not the model's
	// much larger native/cloud context ceiling.
	qwen38Thinking := GenerationParams{
		Temperature:     1.0,
		TopP:            0.95,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 0.0,
		RepeatPenalty:   1.0,
		MaxTokens:       8192,
		Seed:            -1,
	}
	qwen38NoThink := GenerationParams{
		Temperature:     0.7,
		TopP:            0.8,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 1.5,
		RepeatPenalty:   1.0,
		MaxTokens:       8192,
		Seed:            -1,
	}
	// Unsloth-recommended params per mode
	thinkCoding := GenerationParams{
		Temperature:     0.6,
		TopP:            0.95,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 0.0,
		RepeatPenalty:   1.05,
		MaxTokens:       16384,
		Seed:            -1,
	}
	thinkGeneral := GenerationParams{
		Temperature:     1.0,
		TopP:            0.95,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 0.0,
		RepeatPenalty:   1.05,
		MaxTokens:       8192,
		Seed:            -1,
	}
	noThink := GenerationParams{
		Temperature:     0.7,
		TopP:            0.8,
		TopK:            20,
		MinP:            0.0,
		PresencePenalty: 0.0,
		RepeatPenalty:   1.05,
		MaxTokens:       8192,
		Seed:            -1,
	}
	gemma4NoThink := GenerationParams{
		Temperature:     1.0,
		TopP:            0.95,
		TopK:            64,
		MinP:            0.05,
		PresencePenalty: 0.0,
		RepeatPenalty:   1.0,
		MaxTokens:       8192,
		Seed:            -1,
	}

	// Base Qwen3.6-27B config — tournament-winning UD-Q4_K_XL on the
	// user's RTX 3090 at the verified 35K working context.
	qwenBase := Profile{
		Provider:         "inference-bridge",
		ModelID:          "Qwen3.6-27B-UD-Q4_K_XL.gguf",
		CtxTokens:        35000,
		Thinking:         true,
		PreserveThink:    true,
		KVCachePrecision: "f16",
		KVCacheTypeK:     "f16",
		KVCacheTypeV:     "f16",
		ThinkGeneral:     thinkGeneral,
		ThinkCoding:      thinkCoding,
		NoThink:          noThink,
	}

	qwenThink := qwenBase
	qwenThink.Name = "qwen3.6-think"

	qwenChat := qwenBase
	qwenChat.Name = "qwen3.6-chat"
	qwenChat.Thinking = false
	qwenChat.PreserveThink = false

	qwenNoThink := qwenBase
	qwenNoThink.Name = "qwen3.6-nothink"
	qwenNoThink.Thinking = false
	qwenNoThink.PreserveThink = false

	qwen38 := Profile{
		Name:             "qwen3.8-agent-stability",
		Provider:         "inference-bridge",
		ModelID:          "Qwen3.8-27B-Q4_K_M.gguf",
		CtxTokens:        35000,
		Thinking:         true,
		PreserveThink:    true,
		KVCachePrecision: "f16",
		KVCacheTypeK:     "f16",
		KVCacheTypeV:     "f16",
		ThinkGeneral:     qwen38Thinking,
		ThinkCoding:      qwen38Thinking,
		NoThink:          qwen38NoThink,
		SpecType:         "draft-mtp",
		SpecDraftNMax:    2,
	}
	qwen38Uncensored := qwen38
	qwen38Uncensored.Name = "qwen3.8-uncensored-agent-stability"
	qwen38Uncensored.ModelID = "Qwen3.8-27B-Uncensored-Q4_K_M.gguf"

	return ProfilesFile{
		Providers: map[string]Provider{
			"inference-bridge": {
				Name:    "inference-bridge",
				Backend: "llamacpp",
				BaseURL: "http://127.0.0.1:8800/v1",
			},
			"openrouter": {
				Name:      "openrouter",
				Backend:   "openai-compatible",
				BaseURL:   "https://openrouter.ai/api/v1",
				APIKeyEnv: "OPENROUTER_API_KEY",
			},
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
			"qwen3.8-agent-stability":            qwen38,
			"qwen3.8-uncensored-agent-stability": qwen38Uncensored,
			"qwen3.6-think":                      qwenThink,
			"qwen3.6-chat":                       qwenChat,
			"qwen3.6-nothink":                    qwenNoThink,
			"gemma4-26b-a4b-qat": {
				Name:             "gemma4-26b-a4b-qat",
				Provider:         "llamacpp-local",
				ModelID:          "gemma-4-26B-A4B-it-QAT-Q4_0.gguf",
				CtxTokens:        49152,
				Thinking:         false,
				PreserveThink:    false,
				KVCachePrecision: "f16",
				KVCacheTypeK:     "f16",
				KVCacheTypeV:     "f16",
				ThinkGeneral:     gemma4NoThink,
				ThinkCoding:      gemma4NoThink,
				NoThink:          gemma4NoThink,
			},
		},
	}
}
