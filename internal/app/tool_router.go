package app

import (
	"strings"

	"mauler/internal/settings"
)

func selectToolsForTurn(cfg settings.ToolsConfig, firstUserText string, autoContinues int, totalToolCallsMade int) map[string]bool {
	if autoContinues > 0 {
		return broadenToolSelection(routeToolsForTask(cfg, firstUserText), cfg, firstUserText, totalToolCallsMade)
	}
	return routeToolsForTask(cfg, firstUserText)
}

func selectToolsForTurnWithState(cfg settings.ToolsConfig, firstUserText string, autoContinues int, totalToolCallsMade int, state TerminalStateSnapshot) map[string]bool {
	selected := selectToolsForTurn(cfg, firstUserText, autoContinues, totalToolCallsMade)
	if needsLiveSystemInfoTool(firstUserText) || needsWindowsHostInspectionTool(firstUserText) || needsLiveExternalInfoTool(firstUserText) {
		return selected
	}
	return applyTerminalStateToolRouting(selected, firstUserText, state)
}

func applyTerminalStateToolRouting(selected map[string]bool, firstUserText string, state TerminalStateSnapshot) map[string]bool {
	out := cloneBoolMap(selected)
	stateName := strings.TrimSpace(state.State)
	if stateName == "" && !needsOperationalTool(firstUserText) && !looksShellCentricTask(firstUserText) {
		return out
	}
	switch stateName {
	case "connected":
		for _, name := range []string{"terminal_read", "terminal_send", "shell", "http_probe", "progress", "memory", "session_search", "read_tool_result"} {
			out[name] = true
		}
		delete(out, "start_listener")
	case "listener":
		for _, name := range []string{"terminal_read", "terminal_send", "shell", "http_probe", "progress", "memory", "session_search", "read_tool_result"} {
			out[name] = true
		}
		delete(out, "start_listener")
	case "running", "busy", "interactive_prompt":
		for _, name := range []string{"terminal_read", "terminal_send", "shell", "http_probe", "read_tool_result", "progress", "memory", "session_search"} {
			out[name] = true
		}
		delete(out, "start_listener")
	}
	return out
}

func routeToolsForTask(cfg settings.ToolsConfig, firstUserText string) map[string]bool {
	lower := strings.ToLower(strings.TrimSpace(firstUserText))
	if needsLiveSystemInfoTool(lower) || needsWindowsHostInspectionTool(lower) {
		// Keep this deterministic: a required-tool time/date request should not
		// let a small local model choose skill, memory, workspace inspection, or
		// the WSL interactive terminal. Windows host facts use a per-call native
		// PowerShell backend applied by the execution router.
		return map[string]bool{"shell": true}
	}
	if needsLiveExternalInfoTool(lower) {
		// Current public facts require provenance. Keep the opening choice small
		// while allowing either a direct API probe or sourced public research.
		selected := map[string]bool{"http_probe": true}
		addAlwaysAvailableTools(selected)
		addResearchTools(selected)
		return selected
	}
	selected := map[string]bool{}
	addAlwaysAvailableTools(selected)
	if looksLikeImageGenerationTask(lower) {
		selected[generateImageToolName] = true
	}

	if lower == "" {
		addReadTools(selected)
		return selected
	}

	switch {
	case looksReportOrDocsTask(lower):
		addOpsToolsForPhase(selected, "report")
		if explicitWebResearchIntent(lower) {
			addResearchTools(selected)
		}
	case needsOperationalTool(lower) || looksShellCentricTask(lower):
		addOpsToolsForPhase(selected, opsPhaseFromText(lower))
		if explicitWebResearchIntent(lower) {
			addResearchTools(selected)
		}
		if explicitBrowserIntent(lower) {
			addBrowserTools(selected)
		}
	case explicitWebResearchIntent(lower) || looksResearchTask(lower):
		addResearchTools(selected)
		if looksCodeOrWorkspaceTask(lower) {
			addLeanReadTools(selected)
		}
	case looksCodeOrWorkspaceTask(lower) || needsInspectionTool(lower):
		if promptLooksReadOnly(lower) || looksReadOnlyInspectionTask(lower) {
			addInspectionTools(selected)
		} else {
			addCodeTools(selected)
		}
	default:
		addLeanReadTools(selected)
		addMemoryTools(selected)
		if isUnrestrictedToolset(cfg) {
			addBasicShellTools(selected)
		}
	}
	if looksEngagementGridTask(lower) {
		selected["engagement"] = true
	}

	if len(selected) < 6 {
		addReadTools(selected)
	}
	if looksPublicExploitLookup(lower) {
		// A methodology skill is not current vulnerability evidence. Keeping the
		// broad master skill in this narrow route encouraged local models to reread
		// it instead of calling web_search/fetch_url.
		delete(selected, "skill")
	}
	return selected
}

func looksLikeImageGenerationTask(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return hasAny(lower,
		"generate an image", "generate image", "create an image", "create image",
		"make an image", "make image", "draw an image", "render an image",
		"image generation", "text to image", "text-to-image", "picture of",
		"illustration of", "concept art", "wallpaper", "poster image",
	)
}

func broadenToolSelection(selected map[string]bool, cfg settings.ToolsConfig, firstUserText string, totalToolCallsMade int) map[string]bool {
	out := cloneBoolMap(selected)
	lower := strings.ToLower(strings.TrimSpace(firstUserText))
	if needsLiveSystemInfoTool(lower) || needsWindowsHostInspectionTool(lower) {
		return map[string]bool{"shell": true}
	}
	if needsLiveExternalInfoTool(lower) {
		out := map[string]bool{"http_probe": true}
		addAlwaysAvailableTools(out)
		addResearchTools(out)
		return out
	}
	addAlwaysAvailableTools(out)
	if looksEngagementGridTask(lower) {
		out["engagement"] = true
	}
	if needsOperationalTool(lower) || looksShellCentricTask(lower) {
		addOpsToolsForPhase(out, opsPhaseFromText(lower))
		if totalToolCallsMade >= 4 {
			out["evidence_bundle"] = true
		}
		if looksReportOrDocsTask(lower) {
			addWriteTools(out)
		}
		if explicitWebResearchIntent(lower) {
			addResearchTools(out)
		}
		if explicitBrowserIntent(lower) {
			addBrowserTools(out)
		}
		return out
	}
	addReadTools(out)
	addMemoryTools(out)
	addSkillTools(out)
	if isUnrestrictedToolset(cfg) {
		addWriteTools(out)
		addShellTools(out)
		addEvidenceTools(out)
		if totalToolCallsMade >= 8 || looksCodeOrWorkspaceTask(lower) {
			out["task"] = true
		}
		if explicitWebResearchIntent(lower) {
			addResearchTools(out)
		}
		if explicitBrowserIntent(lower) {
			addBrowserTools(out)
		}
	}
	return out
}

func addAlwaysAvailableTools(selected map[string]bool) {
	for _, name := range []string{
		reasoningEffortToolName,
		"read_tool_result",
		"skill",
		"todo_write",
	} {
		selected[name] = true
	}
}

func looksEngagementGridTask(lower string) bool {
	lower = strings.ToLower(strings.TrimSpace(lower))
	return lower == "engagement" || hasAny(lower,
		"engagement grid", "engagement checklist", "active engagement", "project engagement",
		"create engagement", "open engagement", "engagement status", "engagement notes",
		"claim engagement", "finish engagement", "advance engagement", "engagement finding",
		"grid checklist", "claim the next check", "record the finding in the grid",
	)
}

func addLeanReadTools(selected map[string]bool) {
	for _, name := range []string{"read", "glob", "grep"} {
		selected[name] = true
	}
}

func addReadTools(selected map[string]bool) {
	for _, name := range []string{"read", "glob", "grep"} {
		selected[name] = true
	}
}

func addWriteTools(selected map[string]bool) {
	for _, name := range []string{"write", "edit"} {
		selected[name] = true
	}
}

func addShellTools(selected map[string]bool) {
	for _, name := range []string{"shell", "run_script", "terminal_send", "terminal_read", "start_listener"} {
		selected[name] = true
	}
}

func addBasicShellTools(selected map[string]bool) {
	for _, name := range []string{"shell", "terminal_send", "terminal_read"} {
		selected[name] = true
	}
}

func addOpsShellTools(selected map[string]bool) {
	addBasicShellTools(selected)
	selected["start_listener"] = true
}

func addResearchTools(selected map[string]bool) {
	for _, name := range []string{"web_search", "fetch_url", "task"} {
		selected[name] = true
	}
}

func addBrowserTools(selected map[string]bool) {
	selected["browser"] = true
}

func addEvidenceTools(selected map[string]bool) {
	for _, name := range []string{"http_probe", "evidence_bundle"} {
		selected[name] = true
	}
}

func addMemoryTools(selected map[string]bool) {
	for _, name := range []string{"memory", "session_search", "todo_write"} {
		selected[name] = true
	}
}

func addSkillTools(selected map[string]bool) {
	selected["skill"] = true
}

func addCodeTools(selected map[string]bool) {
	addReadTools(selected)
	addWriteTools(selected)
	addShellTools(selected)
	selected["task"] = true
}

func addInspectionTools(selected map[string]bool) {
	addReadTools(selected)
	addMemoryTools(selected)
	selected["task"] = true
}

func addOpsTools(selected map[string]bool) {
	addOpsToolsForPhase(selected, "default")
}

func addOpsToolsForPhase(selected map[string]bool, phase string) {
	addAlwaysAvailableTools(selected)
	switch phase {
	case "report":
		addLeanReadTools(selected)
		addWriteTools(selected)
		addEvidenceTools(selected)
		addMemoryTools(selected)
		selected["progress"] = true
	case "reverse_shell":
		addOpsReadTools(selected)
		for _, name := range []string{"shell", "terminal_send", "terminal_read", "start_listener", "progress", "memory", "session_search"} {
			selected[name] = true
		}
	case "webshell":
		addOpsReadTools(selected)
		for _, name := range []string{"shell", "terminal_send", "terminal_read", "http_probe", "read_tool_result", "progress", "memory", "session_search"} {
			selected[name] = true
		}
	case "recon":
		addOpsReadTools(selected)
		for _, name := range []string{"shell", "terminal_send", "terminal_read", "http_probe", "progress", "memory", "session_search"} {
			selected[name] = true
		}
	default:
		addOpsReadTools(selected)
		addOpsShellTools(selected)
		selected["memory"] = true
		selected["session_search"] = true
		selected["http_probe"] = true
		selected["progress"] = true
	}
}

func addOpsReadTools(selected map[string]bool) {
	addLeanReadTools(selected)
}

func opsPhaseFromText(lower string) string {
	switch {
	case looksReportOrDocsTask(lower):
		return "report"
	case hasAny(lower, "reverse shell", "revshell", "listener", "callback", "lhost", "lport", "nc -lv", "ncat", "meterpreter", "connect back"):
		return "reverse_shell"
	case hasAny(lower, "webshell", "web shell", "shell.php", "cmd=", "cmd%3d", "upload shell", "php shell", "foothold"):
		return "webshell"
	case hasAny(lower, "exploit", "exploitation", "privesc", "privilege escalation", "root shell", "user shell"):
		return "default"
	case hasAny(lower, "recon", "enumerate", "enumeration", "scan", "nmap", "ports", "services", "banner", "gobuster", "ffuf"):
		return "recon"
	default:
		return "default"
	}
}

func isUnrestrictedToolset(cfg settings.ToolsConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.ActiveToolset), "unrestricted")
}

func looksResearchTask(lower string) bool {
	return hasAny(lower,
		"research", "compare", "source", "sources", "bibliography", "latest", "current",
		"look up", "look online", "find online", "documentation", "docs for", "official docs",
	)
}

func explicitBrowserIntent(lower string) bool {
	return hasAny(lower,
		"browser", "open page", "click", "screenshot", "visible page", "inspect page",
		"use chrome", "use the browser", "web ui",
	)
}

func looksReportOrDocsTask(lower string) bool {
	documentNoun := hasAny(lower,
		"report", "writeup", "write-up", "documentation", "readme", "notes", "deliverable",
		"summary document", "architecture document", "plan document",
	)
	if !documentNoun {
		return false
	}
	return promptExplicitlyRequestsMutation(lower) || hasAny(lower, "finish ", "complete ")
}

func looksCodeOrWorkspaceTask(lower string) bool {
	if promptLooksLikeAPIInventory(lower) && !promptExplicitlyRequestsMutation(lower) {
		return false
	}
	return looksCodebaseTask(lower) || hasAny(lower,
		"fix", "repair", "restore", "bug", "implement", "patch", "refactor", "update", "edit", "change", "modify", "wire", "test", "tests", "build",
		"compile", "lint", "typecheck", "type-check", "function", "class", "component",
		"file", "files", "folder", "directory", "grep", "search", "find in", "read ",
	)
}

func looksReadOnlyInspectionTask(lower string) bool {
	if hasAny(lower, "do not edit", "don't edit", "dont edit", "without editing", "no edits", "no changes", "do not change", "don't change", "dont change") {
		return true
	}
	if hasAny(lower, "fix", "implement", "patch", "refactor", "update", "edit", "change", "modify", "write", "create", "add ", "build", "compile", "run tests", "test ") {
		return false
	}
	return hasAny(lower,
		"inspect this repo", "inspect the repo", "inspect this project", "inspect the project",
		"find where", "find how", "where is", "where are", "summarize the files", "summarise the files",
		"map the architecture", "show me where", "read and summarize", "read and summarise",
	)
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
