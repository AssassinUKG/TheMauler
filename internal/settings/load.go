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
		return &s, nil
	}

	if _, err := toml.DecodeFile(path, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	normaliseSettings(&s)
	return &s, nil
}

func normaliseSettings(s *Settings) {
	defaults := DefaultSettings()
	if s.Tools.WebEngine == "" {
		s.Tools.WebEngine = defaults.Tools.WebEngine
	}
	if s.Tools.MaxSearches <= 0 {
		s.Tools.MaxSearches = defaults.Tools.MaxSearches
	}
	if s.Tools.MaxFetches <= 0 {
		s.Tools.MaxFetches = defaults.Tools.MaxFetches
	}
	if s.Tools.MaxFailedFetches <= 0 {
		s.Tools.MaxFailedFetches = defaults.Tools.MaxFailedFetches
	}
	if s.Tools.MaxBrowserActions <= 0 {
		s.Tools.MaxBrowserActions = defaults.Tools.MaxBrowserActions
	}
	if s.Tools.ActiveToolset == "" {
		s.Tools.ActiveToolset = defaults.Tools.ActiveToolset
	}
	if s.Tools.Toolsets == nil {
		s.Tools.Toolsets = defaults.Tools.Toolsets
	} else {
		for name, tools := range defaults.Tools.Toolsets {
			if existing, ok := s.Tools.Toolsets[name]; ok {
				s.Tools.Toolsets[name] = mergeToolset(existing, tools)
			} else {
				s.Tools.Toolsets[name] = tools
			}
		}
	}
	if s.Agents.ModeOverride == "" {
		s.Agents.ModeOverride = defaults.Agents.ModeOverride
	}
	if s.Agents.DefaultAutonomy == "" {
		s.Agents.DefaultAutonomy = defaults.Agents.DefaultAutonomy
	}
	if s.Agents.MaxToolCalls <= 0 {
		s.Agents.MaxToolCalls = defaults.Agents.MaxToolCalls
	}
	if s.Agents.Presets == nil {
		s.Agents.Presets = defaults.Agents.Presets
	} else {
		for name, preset := range defaults.Agents.Presets {
			if _, ok := s.Agents.Presets[name]; !ok {
				s.Agents.Presets[name] = preset
			}
		}
	}
	if s.Context.CompactionAt <= 0 {
		s.Context.CompactionAt = defaults.Context.CompactionAt
	}
	if s.Context.ProjectDocMaxBytes <= 0 {
		s.Context.ProjectDocMaxBytes = defaults.Context.ProjectDocMaxBytes
	}
	if len(s.Context.ProjectDocFallbackFilenames) == 0 {
		s.Context.ProjectDocFallbackFilenames = defaults.Context.ProjectDocFallbackFilenames
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
	if s.Tools.EnabledTools == nil {
		s.Tools.EnabledTools = defaults.Tools.EnabledTools
	} else {
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
}

// EffectiveEnabledTools combines the selected toolset with the explicit
// per-tool enabled map. Toolset membership is the coarse gate; EnabledTools is
// the final per-tool override.
func EffectiveEnabledTools(cfg ToolsConfig) map[string]bool {
	defaults := DefaultSettings().Tools
	toolsets := cfg.Toolsets
	if toolsets == nil {
		toolsets = defaults.Toolsets
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
		allowed[name] = true
	}
	out := map[string]bool{}
	for name := range defaults.EnabledTools {
		out[name] = allowed[name]
	}
	for name, enabled := range cfg.EnabledTools {
		if !allowed[name] {
			out[name] = false
			continue
		}
		out[name] = enabled
	}
	if out["shell"] {
		out["bash"] = true
	}
	if !out["shell"] {
		out["bash"] = false
	}
	return out
}

func mergeToolset(existing, defaults []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(defaults))
	for _, name := range existing {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range defaults {
		name = strings.TrimSpace(name)
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
	return &pf, nil
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
	canonical := make(map[string]string)
	for name, provider := range pf.Providers {
		if provider.Backend == "anthropic" {
			delete(pf.Providers, name)
			continue
		}
		provider.Name = name
		provider.BaseURL = strings.TrimRight(provider.BaseURL, "/")
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
		if profile.Backend == "anthropic" || name == "claude-sonnet" || providerOnlyProfile(name, profile) {
			delete(pf.Profiles, name)
			continue
		}
		if profile.Provider == "" || profile.Backend != "" || profile.BaseURL != "" {
			profile.Provider = providerNameForProfile(name, profile, pf, canonical)
		}
		profile.Backend = ""
		profile.BaseURL = ""
		profile.APIKeyEnv = ""
		pf.Profiles[name] = profile
	}
	for name, provider := range pf.Providers {
		key := providerKey(provider.Backend, provider.BaseURL)
		if canonical[key] != name {
			delete(pf.Providers, name)
		}
	}
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
