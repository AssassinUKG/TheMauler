package app

import (
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/ledger"
)

func TestBuildExecutionStatePromptIncludesSessionsFactsAndRouting(t *testing.T) {
	app := &App{ledger: ledger.New(filepath.Join(t.TempDir(), "ledger.jsonl"))}
	app.upsertAgentSession(AgentSession{
		ID:              "listener:4444",
		Kind:            "listener",
		State:           "connected",
		Port:            4444,
		Lhost:           "10.10.15.223",
		User:            "asterisk",
		TerminalSession: "shell-1",
		LastEvidence:    "connected live session",
	})
	app.recordLedger(ledger.Event{
		Kind:   "evidence_pin",
		Source: "tool_result",
		Tool:   "shell",
		Detail: "Target IP: 10.129.23.158\nhttps://connected.htb/shell.php?cmd=id",
	})

	prompt := app.buildExecutionStatePrompt(
		"use the webshell then continue the reverse shell",
		"auto",
		nil,
		TerminalStateSnapshot{Session: "shell-1", State: "connected", Summary: "Shared terminal appears connected"},
	)
	for _, want := range []string{
		"Current execution state packet",
		"route: phase=live_terminal",
		"terminal: state=connected",
		"session: id=listener:4444 kind=listener state=connected port=4444",
		"fact: target=10.129.23.158",
		"fact: webshell=https://connected.htb/shell.php?cmd=id",
		"use terminal_send/terminal_read only for commands inside that live session",
		"use http_probe or shell for independent HTTP/webshell/curl/wget checks",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
