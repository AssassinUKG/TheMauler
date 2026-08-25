package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestWindowsHostInspectionIntentMatchesRunningGameRequest(t *testing.T) {
	for _, prompt := range []string{
		"I'm running a game called foundation galatic frontier can you find it?",
		"Find the running process on my PC and show its PID",
		"Check Windows Task Manager for this app",
		"What is my GPU and VRAM usage?",
	} {
		if !needsWindowsHostInspectionTool(prompt) {
			t.Fatalf("expected Windows host inspection route for %q", prompt)
		}
	}
	for _, prompt := range []string{
		"find the nginx process inside WSL",
		"run nmap in Kali against the HTB target",
		"inspect the process function in this repository",
	} {
		if needsWindowsHostInspectionTool(prompt) {
			t.Fatalf("must preserve WSL/workspace routing for %q", prompt)
		}
	}
}

func TestWindowsHostInspectionAdvertisesOnlyRequiredShell(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	prompt := "I'm running a game called foundation galatic frontier can you find it?"

	selected := selectToolsForTurnWithState(cfg, prompt, 2, 4, TerminalStateSnapshot{State: "connected"})
	if len(selected) != 1 || !selected["shell"] {
		t.Fatalf("Windows host task should stay on one-shot shell across continuations: %#v", selected)
	}
	registry := tools.New()
	defs, choice := toolDefsAndChoiceForTurnWithState(registry, cfg, prompt, 0, 0, TerminalStateSnapshot{State: "connected"})
	if choice != "required" || len(defs) != 1 || defs[0].Function.Name != "shell" {
		t.Fatalf("Windows host first turn should require only shell: choice=%q tools=%s", choice, toolProtocolToolNames(defs))
	}
}

func TestEnforceTaskShellBackendUnwrapsPowerShellWithoutMangle(t *testing.T) {
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"command":"powershell.exe -NoProfile -Command \"$p = Get-Process | Where-Object { $_.ProcessName -match 'foundation|galactic|frontier' }; $p | Select-Object Id, ProcessName, Path\""}`),
	}}
	got, note, changed := enforceTaskShellBackend(tc, "I'm running a game called foundation galatic frontier can you find it?")
	if !changed || !strings.Contains(note, "native PowerShell") {
		t.Fatalf("expected an explicit backend rewrite: changed=%v note=%q", changed, note)
	}
	var args map[string]interface{}
	if err := json.Unmarshal(got.Function.Arguments, &args); err != nil {
		t.Fatal(err)
	}
	if args["backend"] != "powershell" {
		t.Fatalf("backend = %#v, want powershell", args["backend"])
	}
	command, _ := args["command"].(string)
	if strings.Contains(strings.ToLower(command), "powershell.exe") {
		t.Fatalf("redundant nested PowerShell wrapper remains: %q", command)
	}
	for _, variable := range []string{"$p", "$_", "$_.ProcessName"} {
		if !strings.Contains(command, variable) {
			t.Fatalf("rewritten command lost %s: %q", variable, command)
		}
	}
	if !shellCallPrefersIsolatedBackend(got) {
		t.Fatalf("native PowerShell call must bypass shared WSL terminal")
	}
}

func TestWSLTerminalRejectsWindowsHostOneShotButKeepsListener(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ShellBackend = "wsl"
	state := TerminalStateSnapshot{State: "ready"}
	command := `powershell.exe -NoProfile -Command "Get-Process | Where-Object { $_.ProcessName -match 'foundation' }"`
	decision, routed := terminalWSLHostCommandDecision(cfg, state, command)
	if !routed || decision.Allowed || decision.RecommendedTool != "shell" {
		t.Fatalf("Windows host command should be routed away from WSL terminal: %#v routed=%v", decision, routed)
	}
	if !strings.Contains(decision.Message, "backend=powershell") || !strings.Contains(decision.Message, "$_") {
		t.Fatalf("routing repair lacks actionable preservation guidance: %q", decision.Message)
	}

	listener := `powershell.exe -NoProfile -Command "ncat.exe -lvp 4444"`
	if _, routed := terminalWSLHostCommandDecision(cfg, state, listener); routed {
		t.Fatalf("interactive Windows listener should remain a terminal workflow")
	}
	if _, routed := terminalWSLHostCommandDecision(cfg, TerminalStateSnapshot{State: "connected"}, `Get-Process`); routed {
		t.Fatalf("a connected remote Windows shell must remain an interactive terminal workflow")
	}
}

func TestSharedTerminalBackendMismatchBypassesWSL(t *testing.T) {
	raw := json.RawMessage(`{"command":"Get-Process","backend":"powershell"}`)
	if !sharedTerminalCallUsesDifferentBackend("wsl", raw) {
		t.Fatal("explicit native PowerShell call should bypass shared WSL terminal")
	}
	if sharedTerminalCallUsesDifferentBackend("wsl", json.RawMessage(`{"command":"nmap -sV 10.10.10.10","backend":"wsl"}`)) {
		t.Fatal("explicit WSL target call should keep shared WSL terminal")
	}
}
