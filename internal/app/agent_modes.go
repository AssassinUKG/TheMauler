package app

import (
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"mauler/internal/settings"
)

func selectAgentMode(text string, cfg settings.Settings) AgentMode {
	override := strings.TrimSpace(cfg.Agents.ModeOverride)
	if override == "" {
		override = "Auto"
	}
	if strings.EqualFold(override, "Manual") {
		return manualAgentMode()
	}
	var mode AgentMode
	if !strings.EqualFold(override, "Auto") {
		mode = baseMode(override)
	} else {
		mode = classifyAgentMode(text)
	}
	return applyPresetToMode(mode, cfg.Agents.Presets)
}

func baseMode(name string) AgentMode {
	switch strings.ToLower(name) {
	case "builder":
		return AgentMode{Name: "Builder", Description: "Implement features and verify them."}
	case "fixer":
		return AgentMode{Name: "Fixer", Description: "Diagnose failures and patch them."}
	case "reviewer":
		return AgentMode{Name: "Reviewer", Description: "Find bugs, risks, regressions, and missing tests."}
	case "researcher":
		return AgentMode{Name: "Researcher", Description: "Search, fetch, compare sources, and synthesize."}
	case "planner":
		return AgentMode{Name: "Planner", Description: "Plan architecture and next steps."}
	default:
		return AgentMode{Name: "Auto", Description: "General coding agent."}
	}
}

func applyPresetToMode(mode AgentMode, presets map[string]settings.AgentModePreset) AgentMode {
	preset, ok := presets[mode.Name]
	if !ok || !preset.Enabled {
		return mode
	}
	if strings.TrimSpace(preset.Instructions) != "" {
		mode.Instructions = strings.TrimSpace(preset.Instructions)
	}
	if preset.ContextBudget > 0 {
		mode.ContextBudget = preset.ContextBudget
	}
	return mode
}

func applyAgentPreset(cfg *settings.Settings, pf *settings.ProfilesFile, mode AgentMode, profile *settings.Profile, autonomous *bool) {
	preset, ok := cfg.Agents.Presets[mode.Name]
	if !ok || !preset.Enabled {
		return
	}
	if preset.Profile != "" {
		if p, ok := pf.Profiles[preset.Profile]; ok && strings.TrimSpace(p.ModelID) != "" {
			*profile = applyProvider(p, pf)
		}
	}
	if strings.EqualFold(preset.Autonomy, "full") {
		*autonomous = true
	}
	if strings.EqualFold(preset.Autonomy, "ask") {
		*autonomous = false
	}
	if strings.TrimSpace(preset.Toolset) != "" {
		cfg.Tools.ActiveToolset = strings.TrimSpace(preset.Toolset)
	}
	if len(preset.ToolPermissions) > 0 {
		if cfg.Tools.EnabledTools == nil {
			cfg.Tools.EnabledTools = map[string]bool{}
		}
		for name, enabled := range preset.ToolPermissions {
			cfg.Tools.EnabledTools[name] = enabled
		}
	}
	if cfg.Agents.OfflineOnly {
		if cfg.Tools.EnabledTools == nil {
			cfg.Tools.EnabledTools = map[string]bool{}
		}
		for _, name := range []string{
			"web_search", "fetch_url",
			"browser_open", "browser_snapshot", "browser_click", "browser_type", "browser_extract", "browser_screenshot",
		} {
			cfg.Tools.EnabledTools[name] = false
		}
	}
}

func applyWorkingContextBudget(a *App, presetBudget, profileContext int) bool {
	if a == nil || presetBudget <= 0 {
		return false
	}
	effective := presetBudget
	if profileContext > 0 && profileContext < effective {
		effective = profileContext
	}
	if effective <= 0 {
		return false
	}
	a.mu.Lock()
	changed := a.history != nil && a.history.Budget() != effective
	if changed {
		a.history.SetBudget(effective)
	}
	a.mu.Unlock()
	if changed && a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:budget_updated", effective)
	}
	return changed
}
