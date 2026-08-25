package settings

// GenerationParams holds inference sampling parameters for one mode.
type GenerationParams struct {
	Temperature     float64 `toml:"temperature" json:"temperature"`
	TopP            float64 `toml:"top_p" json:"top_p"`
	TopK            int     `toml:"top_k" json:"top_k"`
	MinP            float64 `toml:"min_p" json:"min_p"`
	PresencePenalty float64 `toml:"presence_penalty" json:"presence_penalty"`
	RepeatPenalty   float64 `toml:"repeat_penalty" json:"repeat_penalty"`
	MaxTokens       int     `toml:"max_tokens" json:"max_tokens"`
	Seed            int64   `toml:"seed" json:"seed"` // -1 = random
}

// Provider is a named endpoint/backend configuration.
type Provider struct {
	Name      string `toml:"name" json:"name"`
	Backend   string `toml:"backend" json:"backend"` // llamacpp | lmstudio | openai-compatible
	BaseURL   string `toml:"base_url" json:"base_url"`
	APIKeyEnv string `toml:"api_key_env" json:"api_key_env"`
}

// Profile is a named model behaviour configuration.
type Profile struct {
	Name          string `toml:"name" json:"name"`
	Provider      string `toml:"provider" json:"provider"`
	ModelID       string `toml:"model_id" json:"model_id"`
	CtxTokens     int    `toml:"ctx_tokens" json:"ctx_tokens"`
	Thinking      bool   `toml:"thinking" json:"thinking"`
	PreserveThink bool   `toml:"preserve_thinking" json:"preserve_thinking"`
	MMProj        string `toml:"mmproj" json:"mmproj"` // path to vision projector .gguf
	// InferenceBridge KV-cache launch controls. FP16 is the quality-first default;
	// Q8_0 is available for profiles that need more context/VRAM headroom.
	KVCachePrecision string           `toml:"kv_cache_precision" json:"kv_cache_precision"`
	KVCacheTypeK     string           `toml:"kv_cache_type_k" json:"kv_cache_type_k"`
	KVCacheTypeV     string           `toml:"kv_cache_type_v" json:"kv_cache_type_v"`
	ThinkGeneral     GenerationParams `toml:"thinking_general" json:"thinking_general"`
	ThinkCoding      GenerationParams `toml:"thinking_coding" json:"thinking_coding"`
	NoThink          GenerationParams `toml:"nothinking" json:"nothinking"`
	// MTP speculative decoding (llama.cpp b9180+): 1.4–2.2× faster generation.
	// SpecType: "" (disabled) | "draft-mtp"
	// SpecDraftNMax: number of draft tokens per step (2 is a safe default)
	SpecType       string `toml:"spec_type" json:"spec_type"`
	SpecDraftNMax  int    `toml:"spec_draft_n_max" json:"spec_draft_n_max"`
	SpecDraftModel string `toml:"spec_draft_model" json:"spec_draft_model"`

	// Legacy provider fields. Kept to migrate existing profiles.toml files.
	Backend   string `toml:"backend,omitempty" json:"backend,omitempty"`
	BaseURL   string `toml:"base_url,omitempty" json:"base_url,omitempty"`
	APIKeyEnv string `toml:"api_key_env,omitempty" json:"api_key_env,omitempty"`
}

// ActiveParams returns the correct GenerationParams for the current mode.
func (p Profile) ActiveParams(coding bool) GenerationParams {
	if !p.Thinking {
		return p.NoThink
	}
	if coding {
		return p.ThinkCoding
	}
	return p.ThinkGeneral
}

// ToolsConfig holds tool-related settings.
type ToolsConfig struct {
	Enabled                  bool                `toml:"enabled" json:"enabled"`
	ConfirmReads             bool                `toml:"confirm_reads" json:"confirm_reads"`
	ConfirmWrites            bool                `toml:"confirm_writes" json:"confirm_writes"`
	ConfirmExec              bool                `toml:"confirm_exec" json:"confirm_exec"`
	BashTimeout              int                 `toml:"bash_timeout" json:"bash_timeout"`
	ShellBackend             string              `toml:"shell_backend" json:"shell_backend"` // auto | powershell | cmd | bash | wsl
	ShellMode                string              `toml:"shell_mode" json:"shell_mode"`       // isolated | shared_terminal
	ShellDistro              string              `toml:"shell_distro" json:"shell_distro"`   // optional WSL distro name when shell_backend = wsl
	ShellUser                string              `toml:"shell_user" json:"shell_user"`       // optional WSL user, e.g. root
	ArtifactTimeout          int                 `toml:"artifact_timeout" json:"artifact_timeout"`
	WebEngine                string              `toml:"web_engine" json:"web_engine"`
	WebBaseURL               string              `toml:"web_base_url" json:"web_base_url"`
	WebAPIKeyEnv             string              `toml:"web_api_key_env" json:"web_api_key_env"`
	BraveAPIKey              string              `toml:"brave_api_key" json:"brave_api_key"`
	MaxSearches              int                 `toml:"max_searches" json:"max_searches"`
	MaxFetches               int                 `toml:"max_fetches" json:"max_fetches"`
	MaxFailedFetches         int                 `toml:"max_failed_fetches" json:"max_failed_fetches"`
	MaxBrowserActions        int                 `toml:"max_browser_actions" json:"max_browser_actions"`
	MaxToolResultChars       int                 `toml:"max_tool_result_chars" json:"max_tool_result_chars"`             // 0 = no truncation
	ToolResultPreviewChars   int                 `toml:"tool_result_preview_chars" json:"tool_result_preview_chars"`     // chars kept in context when a result is offloaded
	ToolResultAggregateChars int                 `toml:"tool_result_aggregate_chars" json:"tool_result_aggregate_chars"` // per-turn aggregate context cap for tool results
	ProtectedPaths           []string            `toml:"protected_paths" json:"protected_paths"`                         // never modify/delete through Mauler tools
	RedactSecrets            bool                `toml:"redact_secrets" json:"redact_secrets"`                           // when true, redact keys/passwords from tool output before the model sees them (off by default; pentest workflows need recovered creds verbatim)
	ActiveToolset            string              `toml:"active_toolset" json:"active_toolset"`
	Toolsets                 map[string][]string `toml:"toolsets" json:"toolsets"`
	EnabledTools             map[string]bool     `toml:"enabled_tools" json:"enabled_tools"`
	SafeRules                []ToolSafeRule      `toml:"safe_rules" json:"safe_rules"`
	// ToolGrammarConstraint, when true, sends a GBNF grammar that forces valid
	// tool-call JSON for non-native (repair-text/mixed) local models. Off by
	// default; experimental and backend-specific — verify against the running
	// llama.cpp build before relying on it.
	ToolGrammarConstraint bool `toml:"tool_grammar_constraint" json:"tool_grammar_constraint"`
}

// ToolSafeRule allows a previously approved exact tool request to run without
// another confirmation prompt.
type ToolSafeRule struct {
	ID        string `toml:"id" json:"id"`
	Tool      string `toml:"tool" json:"tool"`
	InputHash string `toml:"input_hash" json:"input_hash"`
	Label     string `toml:"label" json:"label"`
	CreatedAt string `toml:"created_at" json:"created_at"`
}

// AgentModePreset configures one autonomous working mode.
type AgentModePreset struct {
	Enabled         bool            `toml:"enabled" json:"enabled"`
	Profile         string          `toml:"profile" json:"profile"`
	ContextBudget   int             `toml:"context_budget" json:"context_budget"`
	Autonomy        string          `toml:"autonomy" json:"autonomy"` // ask | balanced | full
	Toolset         string          `toml:"toolset" json:"toolset"`
	Instructions    string          `toml:"instructions" json:"instructions"`
	ToolPermissions map[string]bool `toml:"tool_permissions" json:"tool_permissions"`
}

// ReviewLoopConfig controls the same-profile self-review loop that gates run completion.
// It never swaps models; every review turn uses the run's active profile.
type ReviewLoopConfig struct {
	Enabled            bool     `toml:"enabled" json:"enabled"`
	OnlyAutonomous     bool     `toml:"only_autonomous" json:"only_autonomous"`
	MaxReviewCycles    int      `toml:"max_review_cycles" json:"max_review_cycles"`
	VerifyGate         bool     `toml:"verify_gate" json:"verify_gate"`
	VerifyCommands     []string `toml:"verify_commands" json:"verify_commands"`
	VerifyTimeoutSec   int      `toml:"verify_timeout_sec" json:"verify_timeout_sec"`
	CompletionRails    bool     `toml:"completion_rails" json:"completion_rails"`
	CompletionBlocking bool     `toml:"completion_blocking" json:"completion_blocking"`
	ReviewerPass       bool     `toml:"reviewer_pass" json:"reviewer_pass"`
	ReviewerMaxTools   int      `toml:"reviewer_max_tools" json:"reviewer_max_tools"`
}

// AgentsConfig holds auto-agent routing and safety settings.
type AgentsConfig struct {
	ModeOverride          string                     `toml:"mode_override" json:"mode_override"` // Auto | Manual | Builder | Fixer | Reviewer | Researcher | Planner | Bug Bounty Hunter
	DefaultAutonomy       string                     `toml:"default_autonomy" json:"default_autonomy"`
	OfflineOnly           bool                       `toml:"offline_only" json:"offline_only"`
	MaxToolCalls          int                        `toml:"max_tool_calls" json:"max_tool_calls"`
	MaxRunSeconds         int                        `toml:"max_run_seconds" json:"max_run_seconds"` // 0 = unlimited
	EscalationProfile     string                     `toml:"escalation_profile" json:"escalation_profile"`
	RequirePlan           bool                       `toml:"require_plan" json:"require_plan"`
	NoThinkAfterToolCalls int                        `toml:"no_think_after_tool_calls" json:"no_think_after_tool_calls"` // 0 = use default (2)
	ReasoningEffort       string                     `toml:"reasoning_effort" json:"reasoning_effort"`                   // auto | none | minimal | low | medium | high | xhigh
	ThinkingMode          string                     `toml:"thinking_mode" json:"thinking_mode"`                         // auto | on | off; chat-level override for supported models
	ReviewLoop            ReviewLoopConfig           `toml:"review_loop" json:"review_loop"`
	Presets               map[string]AgentModePreset `toml:"presets" json:"presets"`
}

type WorkspaceFolder struct {
	Path string `toml:"path" json:"path"`
	Name string `toml:"name" json:"name"`
	Role string `toml:"role" json:"role"` // root | notes | loot | scans | scripts | reference | folder
}

// WorkspacePreference keeps small UI/runtime choices scoped to one canonical
// workspace so an agent selected for security work does not leak into another
// project. Conversation and evidence data remain in their existing stores.
type WorkspacePreference struct {
	Path      string `toml:"path" json:"path"`
	AgentMode string `toml:"agent_mode" json:"agent_mode"`
}

// LabScopeTarget is one operator-owned project scope rule. Value accepts an
// exact IP/host, CIDR, host:port, or URL (including a path prefix). Excluded
// entries are explicit deny rules evaluated before all allow rules.
type LabScopeTarget struct {
	Value       string `toml:"value" json:"value"`
	Kind        string `toml:"kind" json:"kind"`               // ip | cidr | hostname | url | invalid
	Environment string `toml:"environment" json:"environment"` // auto | external | internal
	Label       string `toml:"label" json:"label"`
	Notes       string `toml:"notes" json:"notes"`
	Excluded    bool   `toml:"excluded" json:"excluded"`
}

type LabContext struct {
	ID               string           `toml:"id" json:"id"`
	Name             string           `toml:"name" json:"name"`
	Target           string           `toml:"target" json:"target"`
	Hostname         string           `toml:"hostname" json:"hostname"`
	ScopeTargets     []LabScopeTarget `toml:"scope_targets" json:"scope_targets"`
	VPNInterface     string           `toml:"vpn_interface" json:"vpn_interface"`
	LatestArtifact   string           `toml:"latest_artifact" json:"latest_artifact"`
	OpsProfile       string           `toml:"ops_profile" json:"ops_profile"`
	EvidencePolicy   string           `toml:"evidence_policy" json:"evidence_policy"`     // discovery_first | research_assisted | reference_allowed | fastest_path
	AccessPreference string           `toml:"access_preference" json:"access_preference"` // auto | webshell | reverse_shell | bind_shell | none
	Notes            string           `toml:"notes" json:"notes"`
}

type LabProfile struct {
	ID               string           `toml:"id" json:"id"`
	Name             string           `toml:"name" json:"name"`
	WorkspaceDir     string           `toml:"workspace_dir" json:"workspace_dir"`
	Target           string           `toml:"target" json:"target"`
	Hostname         string           `toml:"hostname" json:"hostname"`
	ScopeTargets     []LabScopeTarget `toml:"scope_targets" json:"scope_targets"`
	VPNInterface     string           `toml:"vpn_interface" json:"vpn_interface"`
	LatestArtifact   string           `toml:"latest_artifact" json:"latest_artifact"`
	OpsProfile       string           `toml:"ops_profile" json:"ops_profile"`
	EvidencePolicy   string           `toml:"evidence_policy" json:"evidence_policy"`
	AccessPreference string           `toml:"access_preference" json:"access_preference"`
	Notes            string           `toml:"notes" json:"notes"`
}

type EnvironmentConfig struct {
	MainOS               string `toml:"main_os" json:"main_os"`                   // auto | windows | linux | macos
	AIShellBackend       string `toml:"ai_shell_backend" json:"ai_shell_backend"` // auto | powershell | cmd | bash | wsl
	AIShellDistro        string `toml:"ai_shell_distro" json:"ai_shell_distro"`
	AIShellUser          string `toml:"ai_shell_user" json:"ai_shell_user"`
	TargetWorkBackend    string `toml:"target_work_backend" json:"target_work_backend"` // ai_shell | windows_powershell | wsl | bash
	ListenerBackend      string `toml:"listener_backend" json:"listener_backend"`       // windows_powershell | ai_shell | wsl | bash | manual
	ListenerCommand      string `toml:"listener_command" json:"listener_command"`
	LHOSTSource          string `toml:"lhost_source" json:"lhost_source"` // selected_vpn_interface | manual | auto
	ManualLHOST          string `toml:"manual_lhost" json:"manual_lhost"`
	PreferTerminalTools  bool   `toml:"prefer_terminal_tools" json:"prefer_terminal_tools"`
	ReverseShellGuidance string `toml:"reverse_shell_guidance" json:"reverse_shell_guidance"`
	UserCorrectionPolicy string `toml:"user_correction_policy" json:"user_correction_policy"` // latest_user_wins | normal
}

// ContextConfig holds context window and compaction settings.
type ContextConfig struct {
	AutoInjectFile              bool                  `toml:"auto_inject_file" json:"auto_inject_file"`
	AutoInjectCursor            bool                  `toml:"auto_inject_cursor" json:"auto_inject_cursor"`
	CompactionAt                float64               `toml:"compaction_at" json:"compaction_at"` // fraction, default 0.85
	ShowCompaction              bool                  `toml:"show_compaction" json:"show_compaction"`
	MAULERMDPath                string                `toml:"mauler_md_path" json:"mauler_md_path"` // explicit single file; empty = layered auto-discover
	ProjectDocMaxBytes          int                   `toml:"project_doc_max_bytes" json:"project_doc_max_bytes"`
	ProjectDocFallbackFilenames []string              `toml:"project_doc_fallback_filenames" json:"project_doc_fallback_filenames"`
	WorkspaceDir                string                `toml:"workspace_dir" json:"workspace_dir"`
	OpenFolders                 []WorkspaceFolder     `toml:"open_folders" json:"open_folders"`
	WorkspacePreferences        []WorkspacePreference `toml:"workspace_preferences" json:"workspace_preferences"`
	Lab                         LabContext            `toml:"lab" json:"lab"`
	ActiveLabProfile            string                `toml:"active_lab_profile" json:"active_lab_profile"`
	LabProfiles                 []LabProfile          `toml:"lab_profiles" json:"lab_profiles"`
}

// MemoryConfig holds durable project-memory settings.
type MemoryConfig struct {
	Enabled    bool `toml:"enabled" json:"enabled"`
	AutoInject bool `toml:"auto_inject" json:"auto_inject"`
	// DisableAutoDistill turns OFF auto-saving run lessons into memory at finish.
	// Stored as an opt-out so the zero value (absent in older configs) keeps the
	// default-on behavior; the UI presents it positively as "Auto-distill lessons".
	DisableAutoDistill bool `toml:"disable_auto_distill" json:"disable_auto_distill"`
	MaxEntries         int  `toml:"max_entries" json:"max_entries"`
	MaxInject          int  `toml:"max_inject" json:"max_inject"`
	MaxEntryChars      int  `toml:"max_entry_chars" json:"max_entry_chars"`
}

// SkillsConfig holds procedural-memory skill settings.
type SkillsConfig struct {
	Enabled    bool   `toml:"enabled" json:"enabled"`
	AutoInject bool   `toml:"auto_inject" json:"auto_inject"`
	MaxInject  int    `toml:"max_inject" json:"max_inject"`
	SkillsDir  string `toml:"skills_dir" json:"skills_dir"` // empty = ~/.config/mauler/skills
}

// ImageConfig holds vision/image settings.
type ImageConfig struct {
	VisionEnabled    bool   `toml:"vision_enabled" json:"vision_enabled"`
	ClipboardMethod  string `toml:"clipboard_method" json:"clipboard_method"` // auto | xclip | wl-paste | powershell
	DisplayMethod    string `toml:"display_method" json:"display_method"`     // sixel | kitty | text
	MaxDisplayWidth  int    `toml:"max_display_width" json:"max_display_width"`
	WSLPathTranslate bool   `toml:"wsl_path_translate" json:"wsl_path_translate"`

	// Video ingestion. Local vision models cannot decode raw video, so pasted
	// or dropped clips are sampled into keyframes (fed as images) plus an
	// optional audio transcript. Requires ffmpeg in PATH; transcription
	// additionally requires whisper.
	VideoEnabled    bool `toml:"video_enabled" json:"video_enabled"`
	VideoMaxFrames  int  `toml:"video_max_frames" json:"video_max_frames"`
	VideoFrameWidth int  `toml:"video_frame_width" json:"video_frame_width"`
	VideoTranscribe bool `toml:"video_transcribe" json:"video_transcribe"`
}

// TelegramConfig holds remote bot settings. The bot runs through the channel
// bus, so side chats, control commands, and work requests stay separate from
// the active project run.
type TelegramConfig struct {
	Enabled           bool     `toml:"enabled" json:"enabled"`
	Token             string   `toml:"token" json:"token"`
	BotUsername       string   `toml:"bot_username" json:"bot_username"`
	RequireMention    bool     `toml:"require_mention" json:"require_mention"`
	AllowFrom         []string `toml:"allow_from" json:"allow_from"`
	DefaultProject    string   `toml:"default_project" json:"default_project"`
	DefaultProfile    string   `toml:"default_profile" json:"default_profile"`
	DefaultMode       string   `toml:"default_mode" json:"default_mode"`
	DefaultToolset    string   `toml:"default_toolset" json:"default_toolset"`
	SendProgress      bool     `toml:"send_progress" json:"send_progress"`
	ProgressIntervalS int      `toml:"progress_interval_s" json:"progress_interval_s"`
	VoiceReplies      string   `toml:"voice_replies" json:"voice_replies"`           // off | on_voice | always
	TranscriptionMode string   `toml:"transcription_mode" json:"transcription_mode"` // disabled | local | openai_compatible
	TranscriptionURL  string   `toml:"transcription_url" json:"transcription_url"`
}

// AudioConfig controls local desktop speech input and streamed spoken replies.
type AudioConfig struct {
	Enabled        bool    `toml:"enabled" json:"enabled"`
	Mode           string  `toml:"mode" json:"mode"` // push_to_talk | open_mic
	STTEngine      string  `toml:"stt_engine" json:"stt_engine"`
	TTSEngine      string  `toml:"tts_engine" json:"tts_engine"`
	Voice          string  `toml:"voice" json:"voice"`
	Speed          float64 `toml:"speed" json:"speed"`
	InputDevice    string  `toml:"input_device" json:"input_device"`
	VADThreshold   float64 `toml:"vad_threshold" json:"vad_threshold"`
	BargeIn        bool    `toml:"barge_in" json:"barge_in"`
	SpeakReplies   bool    `toml:"speak_replies" json:"speak_replies"`
	SpeakToolNotes bool    `toml:"speak_tool_notes" json:"speak_tool_notes"`
	ClauseMinChars int     `toml:"clause_min_chars" json:"clause_min_chars"`
}

// UIConfig holds display and layout settings.
type UIConfig struct {
	Theme               string  `toml:"theme" json:"theme"` // mauler-ops | slate | light | dark legacy
	AccentColor         string  `toml:"accent_color" json:"accent_color"`
	PrimaryColor        string  `toml:"primary_color" json:"primary_color"`
	StatusBar           bool    `toml:"status_bar" json:"status_bar"`
	TokenCounter        bool    `toml:"token_counter" json:"token_counter"`
	ThinkIndicator      bool    `toml:"think_indicator" json:"think_indicator"`
	SyntaxHighlight     bool    `toml:"syntax_highlight" json:"syntax_highlight"`
	DiffColours         bool    `toml:"diff_colours" json:"diff_colours"`
	ChatTimestamps      bool    `toml:"chat_timestamps" json:"chat_timestamps"`
	ToolCountdown       bool    `toml:"tool_countdown" json:"tool_countdown"`
	TerminalDefaultOpen bool    `toml:"terminal_default_open" json:"terminal_default_open"`
	TerminalHeight      int     `toml:"terminal_height" json:"terminal_height"`
	TreeWidth           float64 `toml:"tree_width" json:"tree_width"` // fraction of terminal width
	ChatWidth           float64 `toml:"chat_width" json:"chat_width"`
	ArtifactWidth       float64 `toml:"artifact_width" json:"artifact_width"`
}

// LoggingConfig controls what gets persisted in task run logs.
type LoggingConfig struct {
	Enabled        bool `toml:"enabled" json:"enabled"`
	LogToolInputs  bool `toml:"log_tool_inputs" json:"log_tool_inputs"`
	LogToolResults bool `toml:"log_tool_results" json:"log_tool_results"`
	LogResponses   bool `toml:"log_responses" json:"log_responses"`
	MaxRuns        int  `toml:"max_runs" json:"max_runs"`
}

// Settings is the global (non-profile) configuration.
type Settings struct {
	ActiveProfile string            `toml:"active_profile" json:"active_profile"`
	Tools         ToolsConfig       `toml:"tools" json:"tools"`
	Agents        AgentsConfig      `toml:"agents" json:"agents"`
	Environment   EnvironmentConfig `toml:"environment" json:"environment"`
	Context       ContextConfig     `toml:"context" json:"context"`
	Memory        MemoryConfig      `toml:"memory" json:"memory"`
	Skills        SkillsConfig      `toml:"skills" json:"skills"`
	Image         ImageConfig       `toml:"image" json:"image"`
	Telegram      TelegramConfig    `toml:"telegram" json:"telegram"`
	Audio         AudioConfig       `toml:"audio" json:"audio"`
	UI            UIConfig          `toml:"ui" json:"ui"`
	Logging       LoggingConfig     `toml:"logging" json:"logging"`
	LogLevel      string            `toml:"log_level" json:"log_level"`
}

// ProfilesFile is the top-level structure of profiles.toml.
type ProfilesFile struct {
	Providers map[string]Provider `toml:"providers" json:"providers"`
	Profiles  map[string]Profile  `toml:"profiles" json:"profiles"`
}
