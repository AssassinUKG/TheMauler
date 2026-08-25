package app

import (
	"encoding/json"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

// needsWindowsHostInspectionTool identifies requests whose evidence lives on the
// Windows host rather than in the configured WSL/Kali target environment. Keep
// this deliberately narrow: normal repository and authorised target work must
// continue to use the user's selected backend.
func needsWindowsHostInspectionTool(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if hasAny(lower, "in wsl", "inside wsl", "on wsl", "in kali", "inside kali", "on kali", "linux process", "linux service") &&
		!hasAny(lower, "windows", "my pc", "this pc", "host pc", "windows host") {
		return false
	}

	if hasAny(lower,
		"running a game", "game is running", "game called", "find the game", "locate the game process",
		"task manager", "windows process", "windows service", "windows host process", "windows host service",
		"what processes are running", "which processes are running", "list running processes",
		"find the running process", "find this process", "process id", "process name", "find the pid",
		"gpu usage", "vram usage", "windows gpu", "windows app is running", "program is running on my pc",
	) {
		return true
	}

	host := hasAny(lower, "windows", "my pc", "this pc", "my computer", "host pc", "windows host")
	subject := hasAny(lower, "process", "service", "running app", "running program", "running game", "window title", "task manager", "gpu", "vram")
	action := hasAny(lower, "find", "check", "show", "list", "inspect", "locate", "is running", "what is running", "what's running", "whats running")
	return host && subject && action
}

// enforceTaskShellBackend adds a code-owned per-call backend for host facts. It
// also removes a redundant powershell.exe wrapper so the native PowerShell
// process receives variables such as $p and $_ without another parser touching
// them first.
func enforceTaskShellBackend(tc llm.ToolCallDef, taskText string) (llm.ToolCallDef, string, bool) {
	if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), "shell") || !needsWindowsHostInspectionTool(taskText) {
		return tc, "", false
	}
	var args map[string]interface{}
	if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
		return tc, "", false
	}
	changed := !strings.EqualFold(strings.TrimSpace(stringArg(args, "backend")), "powershell")
	args["backend"] = "powershell"
	if command := stringArg(args, "command"); command != "" {
		if inner, ok := tools.UnwrapNestedPowerShellCommand(command); ok {
			args["command"] = inner
			changed = true
		}
	}
	if !changed {
		return tc, "", false
	}
	raw, err := marshalToolArgsNoHTMLEscape(args)
	if err != nil {
		return tc, "", false
	}
	tc.Function.Arguments = raw
	return tc, "Windows host inspection routed to the native PowerShell one-shot backend; WSL/Kali remains unchanged for target work.", true
}

func shellCallPrefersIsolatedBackend(tc llm.ToolCallDef) bool {
	if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), "shell") {
		return false
	}
	var args struct {
		Backend string `json:"backend"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args.Backend)) {
	case "powershell", "pwsh", "cmd":
		return true
	}
	_, nestedPowerShell := tools.UnwrapNestedPowerShellCommand(args.Command)
	return nestedPowerShell
}

func sharedTerminalCallUsesDifferentBackend(configured string, raw json.RawMessage) bool {
	var args struct {
		Backend string `json:"backend"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return false
	}
	requested := strings.ToLower(strings.TrimSpace(args.Backend))
	if requested != "" && requested != "auto" {
		return requested != resolveSharedTerminalBackend(configured)
	}
	_, nestedPowerShell := tools.UnwrapNestedPowerShellCommand(args.Command)
	return nestedPowerShell
}

func isWindowsHostCommandThroughWSL(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return false
	}
	powerShellOneShot := hasAny(lower, "powershell.exe", "powershell ", "pwsh.exe", "pwsh ") &&
		hasAny(lower, " -command ", " -command\"", " -file ", " -file\"", " -c ")
	return powerShellOneShot || hasAny(lower,
		"tasklist.exe", "/windows/system32/tasklist.exe", "wmic process", "get-process", "get-service", "get-ciminstance", "get-counter", "nvidia-smi.exe",
	)
}

func terminalWSLHostCommandDecision(cfg settings.ToolsConfig, state TerminalStateSnapshot, command string) (toolExecutionDecision, bool) {
	if resolveSharedTerminalBackend(cfg.ShellBackend) != "wsl" || !isWindowsHostCommandThroughWSL(command) || looksLikeInteractiveListenerCommand(command) {
		return toolExecutionDecision{}, false
	}
	stateName := strings.TrimSpace(state.State)
	if stateName == "" {
		stateName = "unknown"
	}
	// A connected or prompting session can itself be a remote Windows shell. In
	// that state the command belongs inside the live session and must not be
	// mistaken for a query about the local Windows host.
	if stateName == "connected" || stateName == "interactive_prompt" || stateName == "listener" {
		return toolExecutionDecision{}, false
	}
	return toolExecutionDecision{
		State:           stateName,
		RecommendedTool: "shell",
		Message:         "This is a Windows-host command, but the live terminal is WSL/Kali. Run it as a separate shell one-shot with backend=powershell. Do not wrap it in powershell.exe, create a .ps1 through Bash, or retry the same quoting path; Mauler will preserve PowerShell variables such as $p and $_ on the native backend.",
	}, true
}
