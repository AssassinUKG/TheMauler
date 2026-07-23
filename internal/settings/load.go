package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ConfigDir returns the mauler config directory: ~/.config/mauler.
func ConfigDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MAULER_CONFIG_DIR")); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home dir: %w", err)
	}
	return filepath.Join(home, ".config", "mauler"), nil
}

// Load reads ~/.config/mauler/settings.toml.
// Returns defaults if the file does not exist.
func Load() (*Settings, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}

	s := DefaultSettings()
	path := filepath.Join(dir, "settings.toml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		logValidationAdjustments("settings", s.Validate())
		return &s, nil
	}

	if _, err := toml.DecodeFile(path, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	normaliseSettings(&s)
	logValidationAdjustments("settings", s.Validate())
	return &s, nil
}

func normaliseSettings(s *Settings) {
	defaults := DefaultSettings()
	if s.Tools.WebEngine == "" {
		s.Tools.WebEngine = defaults.Tools.WebEngine
	}
	if s.Tools.MaxSearches <= 0 {
		s.Tools.MaxSearches = defaults.Tools.MaxSearches
	} else if s.Tools.MaxSearches == 8 {
		s.Tools.MaxSearches = defaults.Tools.MaxSearches
	}
	if s.Tools.MaxFetches <= 0 {
		s.Tools.MaxFetches = defaults.Tools.MaxFetches
	} else if s.Tools.MaxFetches == 12 {
		s.Tools.MaxFetches = defaults.Tools.MaxFetches
	}
	if s.Tools.MaxFailedFetches <= 0 {
		s.Tools.MaxFailedFetches = defaults.Tools.MaxFailedFetches
	} else if s.Tools.MaxFailedFetches == 5 {
		s.Tools.MaxFailedFetches = defaults.Tools.MaxFailedFetches
	}
	if s.Tools.MaxBrowserActions <= 0 {
		s.Tools.MaxBrowserActions = defaults.Tools.MaxBrowserActions
	} else if s.Tools.MaxBrowserActions == 35 {
		s.Tools.MaxBrowserActions = defaults.Tools.MaxBrowserActions
	}
	if s.Tools.MaxToolResultChars == 8000 {
		s.Tools.MaxToolResultChars = defaults.Tools.MaxToolResultChars
	}
	if s.Tools.ToolResultPreviewChars <= 0 {
		s.Tools.ToolResultPreviewChars = defaults.Tools.ToolResultPreviewChars
	}
	if s.Tools.ToolResultAggregateChars <= 0 {
		s.Tools.ToolResultAggregateChars = defaults.Tools.ToolResultAggregateChars
	} else if s.Tools.ToolResultAggregateChars == 200000 {
		// Migrate the old full-detail default. Keeping 200k chars of tool output
		// in one model turn crowds a 40k context and hurts agent reliability.
		s.Tools.ToolResultAggregateChars = defaults.Tools.ToolResultAggregateChars
	}
	if s.Tools.BashTimeout <= 0 {
		s.Tools.BashTimeout = defaults.Tools.BashTimeout
	}
	if s.Tools.ShellMode != "isolated" && s.Tools.ShellMode != "shared_terminal" {
		s.Tools.ShellMode = defaults.Tools.ShellMode
	}
	if s.Tools.ActiveToolset == "" {
		s.Tools.ActiveToolset = defaults.Tools.ActiveToolset
	}
	s.Tools.ShellDistro = strings.TrimSpace(s.Tools.ShellDistro)
	s.Tools.ShellUser = strings.TrimSpace(s.Tools.ShellUser)
	if s.Tools.Toolsets == nil {
		s.Tools.Toolsets = defaults.Tools.Toolsets
	} else {
		s.Tools.Toolsets = normaliseToolsetNames(s.Tools.Toolsets)
		for name, tools := range defaults.Tools.Toolsets {
			if existing, ok := s.Tools.Toolsets[name]; ok {
				s.Tools.Toolsets[name] = mergeToolset(existing, tools)
			} else {
				s.Tools.Toolsets[name] = tools
			}
		}
	}
	emptyAgentsConfig := s.Agents.ModeOverride == "" &&
		s.Agents.DefaultAutonomy == "" &&
		s.Agents.MaxToolCalls == 0 &&
		s.Agents.MaxRunSeconds == 0 &&
		s.Agents.Presets == nil
	if s.Agents.ModeOverride == "" {
		s.Agents.ModeOverride = defaults.Agents.ModeOverride
	}
	if s.Agents.DefaultAutonomy == "" {
		s.Agents.DefaultAutonomy = defaults.Agents.DefaultAutonomy
	}
	s.Agents.ReasoningEffort = normaliseReasoningEffortSetting(s.Agents.ReasoningEffort, defaults.Agents.ReasoningEffort)
	if s.Agents.NoThinkAfterToolCalls <= 0 || s.Agents.NoThinkAfterToolCalls == 1 || s.Agents.NoThinkAfterToolCalls == 3 {
		s.Agents.NoThinkAfterToolCalls = defaults.Agents.NoThinkAfterToolCalls
	}
	if s.Agents.MaxToolCalls <= 0 || s.Agents.MaxToolCalls == 40 || s.Agents.MaxToolCalls == 100 {
		s.Agents.MaxToolCalls = defaults.Agents.MaxToolCalls
	}
	if emptyAgentsConfig {
		s.Agents.MaxRunSeconds = defaults.Agents.MaxRunSeconds
	}
	if s.Agents.Presets == nil {
		s.Agents.Presets = defaults.Agents.Presets
	} else {
		for name, preset := range defaults.Agents.Presets {
			if _, ok := s.Agents.Presets[name]; !ok {
				s.Agents.Presets[name] = preset
			}
		}
		migrateOpsPreset(s.Agents.Presets, defaults.Agents.Presets)
		normaliseAgentPresetPermissions(s.Agents.Presets)
	}
	normaliseEnvironment(&s.Environment, defaults.Environment)
	if s.Context.CompactionAt <= 0 {
		s.Context.CompactionAt = defaults.Context.CompactionAt
	}
	if s.Context.ProjectDocMaxBytes <= 0 {
		s.Context.ProjectDocMaxBytes = defaults.Context.ProjectDocMaxBytes
	}
	if len(s.Context.ProjectDocFallbackFilenames) == 0 {
		s.Context.ProjectDocFallbackFilenames = defaults.Context.ProjectDocFallbackFilenames
	} else {
		s.Context.ProjectDocFallbackFilenames = mergeProjectDocFallbacks(
			s.Context.ProjectDocFallbackFilenames,
			defaults.Context.ProjectDocFallbackFilenames,
		)
	}
	s.Context.WorkspaceDir = filepath.ToSlash(strings.TrimSpace(s.Context.WorkspaceDir))
	s.Context.OpenFolders = normaliseWorkspaceFolders(s.Context.OpenFolders, s.Context.WorkspaceDir)
	s.Context.WorkspacePreferences = normaliseWorkspacePreferences(s.Context.WorkspacePreferences)
	s.Context.Lab = normaliseLabContext(s.Context.Lab, defaults.Context.Lab)
	s.Context.LabProfiles = normaliseLabProfiles(s.Context.LabProfiles, s.Context.Lab, s.Context.WorkspaceDir)
	if strings.TrimSpace(s.Context.ActiveLabProfile) == "" {
		s.Context.ActiveLabProfile = s.Context.Lab.ID
	}
	if s.Memory.MaxEntries <= 0 {
		s.Memory = defaults.Memory
	} else {
		if s.Memory.MaxInject <= 0 {
			s.Memory.MaxInject = defaults.Memory.MaxInject
		}
		if s.Memory.MaxEntryChars <= 0 {
			s.Memory.MaxEntryChars = defaults.Memory.MaxEntryChars
		}
	}
	normaliseTelegram(&s.Telegram, defaults.Telegram)
	if s.Tools.EnabledTools == nil {
		s.Tools.EnabledTools = defaults.Tools.EnabledTools
	} else {
		s.Tools.EnabledTools = normaliseEnabledToolNames(s.Tools.EnabledTools)
		for name, enabled := range defaults.Tools.EnabledTools {
			if _, ok := s.Tools.EnabledTools[name]; !ok {
				s.Tools.EnabledTools[name] = enabled
			}
		}
	}
	s.Tools.SafeRules = normaliseSafeRules(s.Tools.SafeRules)
	if s.Logging.MaxRuns <= 0 {
		s.Logging.MaxRuns = defaults.Logging.MaxRuns
	}
	if s.UI.TerminalHeight <= 0 {
		s.UI.TerminalHeight = defaults.UI.TerminalHeight
		s.UI.TerminalDefaultOpen = defaults.UI.TerminalDefaultOpen
	}
	s.UI.Theme = strings.TrimSpace(s.UI.Theme)
	if s.UI.Theme == "" {
		s.UI.Theme = defaults.UI.Theme
	}
	if s.UI.AccentColor == "" {
		s.UI.AccentColor = defaults.UI.AccentColor
	}
	if s.UI.PrimaryColor == "" {
		s.UI.PrimaryColor = defaults.UI.PrimaryColor
	}
}

func normaliseTelegram(cfg *TelegramConfig, defaults TelegramConfig) {
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.BotUsername = strings.TrimPrefix(strings.TrimSpace(cfg.BotUsername), "@")
	cfg.DefaultProject = strings.ReplaceAll(
		filepath.ToSlash(strings.TrimSpace(cfg.DefaultProject)),
		`\`,
		"/",
	)
	cfg.DefaultProfile = strings.TrimSpace(cfg.DefaultProfile)
	cfg.DefaultMode = strings.TrimSpace(cfg.DefaultMode)
	if cfg.DefaultMode == "" {
		cfg.DefaultMode = defaults.DefaultMode
	}
	cfg.DefaultToolset = strings.TrimSpace(cfg.DefaultToolset)
	if cfg.DefaultToolset == "" {
		cfg.DefaultToolset = defaults.DefaultToolset
	}
	if cfg.ProgressIntervalS <= 0 {
		cfg.ProgressIntervalS = defaults.ProgressIntervalS
	}
	cfg.VoiceReplies = strings.ToLower(strings.TrimSpace(cfg.VoiceReplies))
	if cfg.VoiceReplies == "" {
		cfg.VoiceReplies = defaults.VoiceReplies
	}
	switch cfg.VoiceReplies {
	case "off", "on_voice", "always":
	default:
		cfg.VoiceReplies = defaults.VoiceReplies
	}
	cfg.TranscriptionMode = strings.ToLower(strings.TrimSpace(cfg.TranscriptionMode))
	if cfg.TranscriptionMode == "" {
		cfg.TranscriptionMode = defaults.TranscriptionMode
	}
	switch cfg.TranscriptionMode {
	case "disabled", "local", "openai_compatible":
	default:
		cfg.TranscriptionMode = defaults.TranscriptionMode
	}
	cfg.TranscriptionURL = strings.TrimSpace(cfg.TranscriptionURL)
	cfg.AllowFrom = mergeStringList(cfg.AllowFrom, nil)
}

func normaliseEnvironment(env *EnvironmentConfig, defaults EnvironmentConfig) {
	if strings.TrimSpace(env.MainOS) == "" {
		env.MainOS = defaults.MainOS
	}
	if strings.TrimSpace(env.AIShellBackend) == "" {
		env.AIShellBackend = defaults.AIShellBackend
	}
	if strings.TrimSpace(env.AIShellDistro) == "" {
		env.AIShellDistro = defaults.AIShellDistro
	}
	if strings.TrimSpace(env.AIShellUser) == "" {
		env.AIShellUser = defaults.AIShellUser
	}
	if strings.TrimSpace(env.TargetWorkBackend) == "" {
		env.TargetWorkBackend = defaults.TargetWorkBackend
	}
	if strings.TrimSpace(env.ListenerBackend) == "" {
		env.ListenerBackend = defaults.ListenerBackend
	}
	if strings.TrimSpace(env.ListenerCommand) == "" {
		env.ListenerCommand = defaults.ListenerCommand
	}
	if strings.TrimSpace(env.LHOSTSource) == "" {
		env.LHOSTSource = defaults.LHOSTSource
	}
	if strings.TrimSpace(env.UserCorrectionPolicy) == "" {
		env.UserCorrectionPolicy = defaults.UserCorrectionPolicy
	}
	if strings.TrimSpace(env.ReverseShellGuidance) == "" {
		env.ReverseShellGuidance = defaults.ReverseShellGuidance
	}
	env.MainOS = strings.TrimSpace(env.MainOS)
	env.AIShellBackend = strings.TrimSpace(env.AIShellBackend)
	env.AIShellDistro = strings.TrimSpace(env.AIShellDistro)
	env.AIShellUser = strings.TrimSpace(env.AIShellUser)
	env.TargetWorkBackend = strings.TrimSpace(env.TargetWorkBackend)
	env.ListenerBackend = strings.TrimSpace(env.ListenerBackend)
	env.ListenerCommand = strings.TrimSpace(env.ListenerCommand)
	env.LHOSTSource = strings.TrimSpace(env.LHOSTSource)
	env.ManualLHOST = strings.TrimSpace(env.ManualLHOST)
	env.UserCorrectionPolicy = strings.TrimSpace(env.UserCorrectionPolicy)
}

func normaliseLabContext(lab LabContext, defaults LabContext) LabContext {
	lab.ID = strings.TrimSpace(lab.ID)
	if lab.ID == "" {
		lab.ID = defaults.ID
	}
	lab.Name = strings.TrimSpace(lab.Name)
	if lab.Name == "" {
		lab.Name = defaults.Name
	}
	lab.Target = strings.TrimSpace(lab.Target)
	lab.Hostname = strings.TrimSpace(lab.Hostname)
	lab.VPNInterface = strings.TrimSpace(lab.VPNInterface)
	lab.LatestArtifact = filepath.ToSlash(strings.TrimSpace(lab.LatestArtifact))
	lab.OpsProfile = strings.TrimSpace(lab.OpsProfile)
	if lab.OpsProfile == "" {
		lab.OpsProfile = defaults.OpsProfile
	}
	lab.EvidencePolicy = normaliseEvidencePolicy(lab.EvidencePolicy, lab.OpsProfile)
	lab.AccessPreference = strings.TrimSpace(lab.AccessPreference)
	if lab.AccessPreference == "" {
		lab.AccessPreference = defaults.AccessPreference
	}
	lab.Notes = strings.TrimSpace(lab.Notes)
	return lab
}

func normaliseLabProfiles(profiles []LabProfile, active LabContext, workspaceDir string) []LabProfile {
	out := make([]LabProfile, 0, len(profiles)+1)
	seen := map[string]bool{}
	for _, profile := range profiles {
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID == "" {
			continue
		}
		if seen[profile.ID] {
			continue
		}
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			profile.Name = profile.ID
		}
		profile.WorkspaceDir = filepath.ToSlash(strings.TrimSpace(profile.WorkspaceDir))
		profile.Target = strings.TrimSpace(profile.Target)
		profile.Hostname = strings.TrimSpace(profile.Hostname)
		profile.VPNInterface = strings.TrimSpace(profile.VPNInterface)
		profile.LatestArtifact = filepath.ToSlash(strings.TrimSpace(profile.LatestArtifact))
		profile.OpsProfile = strings.TrimSpace(profile.OpsProfile)
		if profile.OpsProfile == "" {
			profile.OpsProfile = "pentesting"
		}
		profile.EvidencePolicy = normaliseEvidencePolicy(profile.EvidencePolicy, profile.OpsProfile)
		profile.AccessPreference = strings.TrimSpace(profile.AccessPreference)
		if profile.AccessPreference == "" {
			profile.AccessPreference = "auto"
		}
		profile.Notes = strings.TrimSpace(profile.Notes)
		out = append(out, profile)
		seen[profile.ID] = true
	}
	if !seen[active.ID] {
		out = append([]LabProfile{{
			ID:               active.ID,
			Name:             active.Name,
			WorkspaceDir:     filepath.ToSlash(strings.TrimSpace(workspaceDir)),
			Target:           active.Target,
			Hostname:         active.Hostname,
			VPNInterface:     active.VPNInterface,
			LatestArtifact:   active.LatestArtifact,
			OpsProfile:       active.OpsProfile,
			EvidencePolicy:   active.EvidencePolicy,
			AccessPreference: active.AccessPreference,
			Notes:            active.Notes,
		}}, out...)
	}
	return out
}

func normaliseEvidencePolicy(policy, opsProfile string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "discovery-first", "discovery_first", "discovery":
		return "discovery_first"
	case "research-assisted", "research_assisted", "research":
		return "research_assisted"
	case "reference-allowed", "reference_allowed", "reference":
		return "reference_allowed"
	case "fastest-path", "fastest_path", "fast":
		return "fastest_path"
	default:
		switch strings.ToLower(strings.TrimSpace(opsProfile)) {
		case "htb", "ctf":
			return "discovery_first"
		default:
			return "research_assisted"
		}
	}
}

func migrateOpsPreset(presets, defaults map[string]AgentModePreset) {
	if presets == nil || defaults == nil {
		return
	}
	current, ok := presets["Ops"]
	next, hasDefault := defaults["Ops"]
	if !ok || !hasDefault {
		return
	}
	if strings.EqualFold(current.Toolset, "local-code") &&
		current.ToolPermissions != nil &&
		!current.ToolPermissions["web_search"] &&
		!current.ToolPermissions["fetch_url"] {
		presets["Ops"] = next
	}
}

func normaliseAgentPresetPermissions(presets map[string]AgentModePreset) {
	for name, preset := range presets {
		preset.Toolset = strings.TrimSpace(preset.Toolset)
		preset.Profile = strings.TrimSpace(preset.Profile)
		preset.Autonomy = strings.TrimSpace(preset.Autonomy)
		preset.ToolPermissions = normaliseEnabledToolNames(preset.ToolPermissions)
		presets[name] = preset
	}
}

func normaliseReasoningEffortSetting(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(value))
	case "auto", "":
		if strings.TrimSpace(fallback) == "" {
			return "auto"
		}
		return strings.ToLower(strings.TrimSpace(fallback))
	default:
		return "auto"
	}
}

func mergeProjectDocFallbacks(existing, defaults []string) []string {
	return mergeStringList(existing, defaults)
}

func normaliseWorkspaceFolders(folders []WorkspaceFolder, agentRoot string) []WorkspaceFolder {
	seen := map[string]bool{}
	var out []WorkspaceFolder
	add := func(folder WorkspaceFolder) {
		folder.Path = filepath.ToSlash(strings.TrimSpace(folder.Path))
		folder.Name = strings.TrimSpace(folder.Name)
		folder.Role = strings.TrimSpace(folder.Role)
		if folder.Role == "" {
			folder.Role = "folder"
		}
		if folder.Path == "" || seen[strings.ToLower(folder.Path)] {
			return
		}
		if folder.Name == "" {
			folder.Name = filepath.Base(folder.Path)
		}
		seen[strings.ToLower(folder.Path)] = true
		out = append(out, folder)
	}
	if agentRoot != "" {
		add(WorkspaceFolder{Path: agentRoot, Role: "root"})
	}
	for _, folder := range folders {
		add(folder)
	}
	return out
}

func normaliseWorkspacePreferences(preferences []WorkspacePreference) []WorkspacePreference {
	seen := map[string]bool{}
	out := make([]WorkspacePreference, 0, len(preferences))
	for _, preference := range preferences {
		preference.Path = filepath.ToSlash(strings.TrimSpace(preference.Path))
		preference.AgentMode = strings.TrimSpace(preference.AgentMode)
		if preference.Path == "" || preference.AgentMode == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(preference.Path))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, preference)
	}
	return out
}

func mergeStringList(existing, defaults []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(defaults))
	for _, name := range existing {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	for _, name := range defaults {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// EffectiveEnabledTools combines the selected toolset with the explicit
// per-tool enabled map. Toolset membership is the coarse gate; EnabledTools is
// the final per-tool override.
func EffectiveEnabledTools(cfg ToolsConfig) map[string]bool {
	defaults := DefaultSettings().Tools
	toolsets := cfg.Toolsets
	if toolsets == nil {
		toolsets = defaults.Toolsets
	} else {
		toolsets = normaliseToolsetNames(toolsets)
	}
	active := strings.TrimSpace(cfg.ActiveToolset)
	if active == "" {
		active = defaults.ActiveToolset
	}
	allowedList, ok := toolsets[active]
	if !ok || len(allowedList) == 0 {
		allowedList = toolsets[defaults.ActiveToolset]
	}
	allowed := map[string]bool{}
	for _, name := range allowedList {
		name = canonicalToolName(name)
		if name == "" {
			continue
		}
		allowed[name] = true
	}
	out := map[string]bool{}
	for name := range defaults.EnabledTools {
		out[name] = allowed[name]
	}
	for name, enabled := range normaliseEnabledToolNames(cfg.EnabledTools) {
		if !allowed[name] {
			out[name] = false
			continue
		}
		out[name] = enabled
	}
	return out
}

var legacyToolNameMap = map[string]string{
	"read_file":          "read",
	"read_many":          "read",
	"read_chunks":        "read",
	"file_outline":       "read",
	"read_pdf":           "read",
	"write_file":         "write",
	"edit_file":          "edit",
	"bash":               "shell",
	"terminal_run":       "terminal_send",
	"sqlite_schema":      "sqlite",
	"sqlite_query":       "sqlite",
	"skills_list":        "skill",
	"skill_view":         "skill",
	"todo_create":        "todo_write",
	"todo_update":        "todo_write",
	"todo_done":          "todo_write",
	"todo_blocked":       "todo_write",
	"todo_list":          "todo_write",
	"todo_clear":         "todo_write",
	"browser_open":       "browser",
	"browser_snapshot":   "browser",
	"browser_click":      "browser",
	"browser_type":       "browser",
	"browser_extract":    "browser",
	"browser_screenshot": "browser",
	"browser_close":      "browser",
	"browser_agent":      "browser",
	"subagent_explore":   "task",
	"subagent_research":  "task",
	"subagent_review":    "task",
	"subagent_testfix":   "task",
	"subagent_summarize": "task",
}

func canonicalToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if next, ok := legacyToolNameMap[name]; ok {
		return next
	}
	return name
}

func normaliseEnabledToolNames(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for name, enabled := range in {
		canonical := canonicalToolName(name)
		if canonical == "" {
			continue
		}
		if existing, ok := out[canonical]; ok {
			out[canonical] = existing || enabled
		} else {
			out[canonical] = enabled
		}
	}
	return out
}

func normaliseToolsetNames(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for set, names := range in {
		seen := map[string]bool{}
		for _, name := range names {
			canonical := canonicalToolName(name)
			if canonical == "" || seen[canonical] {
				continue
			}
			seen[canonical] = true
			out[set] = append(out[set], canonical)
		}
	}
	return out
}

func mergeToolset(existing, defaults []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(defaults))
	for _, name := range existing {
		name = canonicalToolName(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range defaults {
		name = canonicalToolName(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func normaliseSafeRules(rules []ToolSafeRule) []ToolSafeRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]ToolSafeRule, 0, len(rules))
	seen := map[string]bool{}
	for _, rule := range rules {
		rule.Tool = strings.TrimSpace(rule.Tool)
		rule.InputHash = strings.TrimSpace(rule.InputHash)
		if rule.Tool == "" || rule.InputHash == "" {
			continue
		}
		key := rule.Tool + "|" + rule.InputHash
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rule)
	}
	return out
}

// LoadProfiles reads ~/.config/mauler/profiles.toml.
// Returns defaults if the file does not exist.
func LoadProfiles() (*ProfilesFile, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "profiles.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		p := DefaultProfiles()
		logValidationAdjustments("profiles", p.Validate())
		return &p, nil
	}

	var pf ProfilesFile
	if _, err := toml.DecodeFile(path, &pf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if pf.Profiles == nil {
		pf.Profiles = make(map[string]Profile)
	}
	migrateProviders(&pf)
	logValidationAdjustments("profiles", pf.Validate())
	return &pf, nil
}

func logValidationAdjustments(scope string, adjustments []string) {
	for _, adjustment := range adjustments {
		_, _ = fmt.Fprintf(os.Stderr, "[settings] %s auto-corrected: %s\n", scope, adjustment)
	}
}

func migrateProviders(pf *ProfilesFile) {
	if pf.Providers == nil {
		pf.Providers = make(map[string]Provider)
	}
	defaults := DefaultProfiles()
	for name, provider := range defaults.Providers {
		if _, ok := pf.Providers[name]; !ok {
			pf.Providers[name] = provider
		}
	}
	for name, profile := range defaults.Profiles {
		if _, ok := pf.Profiles[name]; !ok {
			pf.Profiles[name] = profile
		}
	}
	normaliseGemma426BQATProfile(pf, defaults)
	delete(pf.Profiles, "gemma4-12b")
	delete(pf.Profiles, "gemma4-12b-qat")
	canonical := make(map[string]string)
	for name, provider := range pf.Providers {
		provider.Name = name
		provider.BaseURL = strings.TrimRight(provider.BaseURL, "/")
		if provider.Backend == "anthropic" && provider.BaseURL == "" {
			provider.BaseURL = "https://api.anthropic.com/v1"
		}
		pf.Providers[name] = provider
		key := providerKey(provider.Backend, provider.BaseURL)
		if existing, ok := canonical[key]; ok {
			if preferProviderName(name, existing) {
				canonical[key] = name
			}
		} else {
			canonical[key] = name
		}
	}
	for name, profile := range pf.Profiles {
		if name == "claude-sonnet" || providerOnlyProfile(name, profile) {
			delete(pf.Profiles, name)
			continue
		}
		if profile.Provider == "" || profile.Backend != "" || profile.BaseURL != "" {
			profile.Provider = providerNameForProfile(name, profile, pf, canonical)
		}
		profile.Backend = ""
		profile.BaseURL = ""
		profile.APIKeyEnv = ""
		normaliseRepeatPenalty(&profile)
		pf.Profiles[name] = profile
	}
	for name, provider := range pf.Providers {
		key := providerKey(provider.Backend, provider.BaseURL)
		if canonical[key] != name {
			delete(pf.Providers, name)
		}
	}
}

func normaliseRepeatPenalty(profile *Profile) {
	if profile == nil {
		return
	}
	repeat := 1.0
	model := strings.ToLower(profile.Name + " " + profile.ModelID)
	if strings.Contains(model, "qwen3.6") || strings.Contains(model, "qwen-3.6") {
		repeat = 1.05
	}
	if strings.Contains(model, "gemma4-26b-a4b-qat") && strings.Contains(model, "hauhaucs-balanced") {
		repeat = 1.1
	}
	for _, params := range []*GenerationParams{&profile.ThinkGeneral, &profile.ThinkCoding, &profile.NoThink} {
		if params.RepeatPenalty <= 0 {
			params.RepeatPenalty = repeat
		}
	}
}

func normaliseGemma426BQATProfile(pf *ProfilesFile, defaults ProfilesFile) {
	const name = "gemma4-26b-a4b-qat"
	profile, ok := pf.Profiles[name]
	if !ok {
		return
	}
	def := defaults.Profiles[name]
	modelID := strings.TrimSpace(profile.ModelID)
	if modelID == "" || strings.EqualFold(modelID, "unsloth/gemma-4-26B-A4B-it-qat-GGUF") {
		profile.ModelID = def.ModelID
	}
	if profile.CtxTokens <= 0 || profile.CtxTokens == 131072 || profile.CtxTokens == 49664 {
		profile.CtxTokens = def.CtxTokens
	}
	if profile.NoThink.MinP == 0 {
		profile.NoThink.MinP = def.NoThink.MinP
	}
	if profile.ThinkGeneral.MinP == 0 {
		profile.ThinkGeneral.MinP = def.ThinkGeneral.MinP
	}
	if profile.ThinkCoding.MinP == 0 {
		profile.ThinkCoding.MinP = def.ThinkCoding.MinP
	}
	pf.Profiles[name] = profile
}

func providerOnlyProfile(name string, profile Profile) bool {
	if name == "lmstudio-default" && profile.ModelID == "" {
		return true
	}
	return profile.ModelID == "" && strings.Contains(strings.ToLower(name), "provider")
}

func providerNameForProfile(name string, profile Profile, pf *ProfilesFile, canonical map[string]string) string {
	if profile.Backend == "" && profile.BaseURL == "" {
		if _, ok := pf.Providers[profile.Provider]; ok {
			return profile.Provider
		}
		return "lmstudio-local"
	}
	backend := profile.Backend
	if backend == "" {
		backend = "lmstudio"
	}
	baseURL := strings.TrimRight(profile.BaseURL, "/")
	if baseURL == "" && backend == "llamacpp" {
		baseURL = "http://localhost:8080/v1"
	} else if baseURL == "" && backend == "anthropic" {
		baseURL = "https://api.anthropic.com/v1"
	}
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	key := providerKey(backend, baseURL)
	if existing, ok := canonical[key]; ok {
		return existing
	}
	providerName := name + "-provider"
	pf.Providers[providerName] = Provider{
		Name:      providerName,
		Backend:   backend,
		BaseURL:   baseURL,
		APIKeyEnv: profile.APIKeyEnv,
	}
	canonical[key] = providerName
	return providerName
}

func providerKey(backend, baseURL string) string {
	return backend + "|" + strings.TrimRight(baseURL, "/")
}

func preferProviderName(candidate, current string) bool {
	if strings.HasSuffix(current, "-local") {
		return false
	}
	if strings.HasSuffix(candidate, "-local") {
		return true
	}
	if strings.Contains(current, "qwen") && !strings.Contains(candidate, "qwen") {
		return true
	}
	return len(candidate) < len(current)
}
