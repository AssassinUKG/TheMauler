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
	mode = applyPresetToMode(mode, cfg.Agents.Presets)
	if strings.TrimSpace(mode.DefaultEffort) == "" {
		mode.DefaultEffort = defaultReasoningEffortForMode(mode)
	}
	return mode
}

func looksOpsWorkspaceTask(text string, cfg settings.Settings) bool {
	lower := strings.ToLower(text)
	if !hasAny(lower, "carry on", "continue", "resume", "next", "box", "target", "hack", "hacking", "foothold", "user", "root", "flag", "scan", "enumerate", "exploit") {
		return false
	}
	if looksCodebaseTask(lower) && !hasAny(lower, "htb", "box", "target", ".htb", "user flag", "root flag") {
		return false
	}
	context := strings.ToLower(strings.Join([]string{
		cfg.Context.WorkspaceDir,
		cfg.Context.Lab.Target,
		cfg.Context.Lab.VPNInterface,
		cfg.Context.Lab.LatestArtifact,
		cfg.Context.Lab.OpsProfile,
	}, "\n"))
	for _, folder := range cfg.Context.OpenFolders {
		context += "\n" + strings.ToLower(folder.Path) + "\n" + strings.ToLower(folder.Role)
	}
	return hasAny(context, "htb", "hackthebox", ".htb", "writeup", "writeups", "loot", "scans", "nmap", "vpn", "tun0", "pentesting", "ctf")
}

func baseMode(name string) AgentMode {
	switch strings.ToLower(name) {
	case "ops":
		return AgentMode{Name: "Ops", Description: "Operate inside a lab target with shell-first evidence gathering."}
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
	case "bug bounty hunter", "bug-bounty-hunter", "bounty hunter", "bug bounty":
		return AgentMode{
			Name:         "Bug Bounty Hunter",
			Description:  "Post-recon manual testing planner.",
			Instructions: bugBountyHunterPrompt,
		}
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
		if mode.Name == "Bug Bounty Hunter" && strings.TrimSpace(mode.Instructions) != "" {
			mode.Instructions = strings.TrimSpace(mode.Instructions) + "\n\nWorkspace/operator addendum:\n" + strings.TrimSpace(preset.Instructions)
		} else {
			mode.Instructions = strings.TrimSpace(preset.Instructions)
		}
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
	preserveToolAccess := preserveExplicitToolset(cfg.Tools.ActiveToolset, preset.Toolset)
	if strings.TrimSpace(preset.Toolset) != "" && !preserveToolAccess {
		cfg.Tools.ActiveToolset = strings.TrimSpace(preset.Toolset)
	}
	if len(preset.ToolPermissions) > 0 {
		if cfg.Tools.EnabledTools == nil {
			cfg.Tools.EnabledTools = map[string]bool{}
		}
		for name, enabled := range preset.ToolPermissions {
			if preserveToolAccess && !enabled {
				continue
			}
			cfg.Tools.EnabledTools[name] = enabled
		}
	}
	if cfg.Agents.OfflineOnly {
		if cfg.Tools.EnabledTools == nil {
			cfg.Tools.EnabledTools = map[string]bool{}
		}
		for _, name := range []string{
			"web_search", "fetch_url",
			"browser",
		} {
			cfg.Tools.EnabledTools[name] = false
		}
	}
}

func preserveExplicitToolset(current, preset string) bool {
	current = strings.ToLower(strings.TrimSpace(current))
	preset = strings.ToLower(strings.TrimSpace(preset))
	if current == "" || preset == "" || current == preset {
		return false
	}
	// Access presets are user intent. If the user has explicitly opened the run up
	// to unrestricted, auto-agent routing may change style, but must not silently
	// narrow tool access to a mode preset such as Researcher/web-research.
	return current == "unrestricted"
}

// workingContextOutputReserve is held back from the model's loaded context so a full
// history plus the model's response don't overflow the KV cache. Combined with the 0.85
// compaction trigger this leaves ample room for generation.
const workingContextOutputReserve = 8192

func applyWorkingContextBudget(a *App, presetBudget, profileContext int) bool {
	if a == nil {
		return false
	}
	effective := effectiveWorkingContextBudget(presetBudget, profileContext)
	if effective <= 0 {
		return false
	}
	a.mu.Lock()
	changed := a.history != nil && a.history.Budget() != effective
	if changed {
		a.history.SetBudget(effective)
	}
	if profileContext > 0 {
		a.contextWindow = profileContext
		a.configuredContextWindow = profileContext
	}
	a.mu.Unlock()
	if changed && a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:budget_updated", effective)
	}
	return changed
}

func effectiveWorkingContextBudget(presetBudget, profileContext int) int {
	// Track the model's actual loaded context (minus a response reserve) rather than a
	// fixed per-mode cap. The old behavior min(presetBudget, profileContext) pinned a 64k
	// model to a stale 32k preset, so the token bar and compaction used half the window.
	effective := presetBudget
	if profileContext > 0 {
		usable := profileContext - workingContextOutputReserve
		if usable < 4096 {
			usable = profileContext
		}
		effective = usable
	}
	return effective
}
