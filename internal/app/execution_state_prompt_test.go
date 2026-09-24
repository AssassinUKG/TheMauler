package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
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
		[]llm.ToolDef{
			{Type: "function", Function: llm.ToolFunctionDef{Name: "write"}},
			{Type: "function", Function: llm.ToolFunctionDef{Name: "fetch_url"}},
		},
		TerminalStateSnapshot{Session: "shell-1", State: "connected", Summary: "Shared terminal appears connected"},
	)
	for _, want := range []string{
		"Current execution state packet",
		"route: phase=live_terminal",
		"tool_count=2 tools=fetch_url,write",
		"the tools named above are the tools available to this model turn",
		"do not claim that tool access is missing",
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

func TestBuildExecutionStatePromptDoesNotClaimBrowserBlockerWhenRouted(t *testing.T) {
	app := New()
	app.suppressEvents = true
	prompt := app.buildExecutionStatePrompt(
		"log in to the supplied site",
		"auto",
		[]llm.ToolDef{{Type: "function", Function: llm.ToolFunctionDef{Name: "browser"}}},
		TerminalStateSnapshot{},
	)
	if strings.Contains(prompt, "browser blocker") {
		t.Fatalf("browser blocker was emitted despite routed browser tool:\n%s", prompt)
	}
	if !strings.Contains(prompt, "tools=browser") {
		t.Fatalf("browser capability truth missing:\n%s", prompt)
	}
}

func TestActiveBrowserReferenceRequiresSnapshotTool(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	registry := tools.New()
	defs, choice := toolDefsAndChoiceForTurnWithState(registry, cfg, "can you see it?", 0, 0, TerminalStateSnapshot{})
	defs, choice, changed := applyActiveBrowserTurnRouting(registry, cfg, "can you see it?", defs, choice, tools.BrowserRuntimeStatus{
		Active: true, Visible: true, State: "ready", URL: "http://homeassistant:8123/dashboard-mainmushroom/0",
	})
	if !changed || choice != "required" || len(defs) != 1 || defs[0].Function.Name != "browser" {
		t.Fatalf("active page reference routing = choice %q defs %#v changed=%t, want required browser only", choice, enabledToolNames(defs), changed)
	}
}

func TestActiveBrowserStatePromptIsActionableAndStripsURLSecrets(t *testing.T) {
	prompt := activeBrowserExecutionStatePrompt(tools.BrowserRuntimeStatus{
		Active: true, Visible: true, State: "ready", URL: "https://example.test/dashboard?token=secret#private",
		Title: "Dashboard\nHome", ActiveTab: "t2", TabCount: 2,
	}, true)
	for _, want := range []string{"active_browser:", "current_url=https://example.test/dashboard", "title=\"Dashboard Home\"", "call browser action=snapshot", "cannot observe the rendered page"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("active browser state prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, secret := range []string{"token=secret", "#private"} {
		if strings.Contains(prompt, secret) {
			t.Fatalf("active browser state prompt leaked %q:\n%s", secret, prompt)
		}
	}
}

func TestExplicitBrowserWindowRequestForcesVisibleOpen(t *testing.T) {
	call := llm.ToolCallDef{Function: llm.FunctionCall{
		Name: "browser", Arguments: json.RawMessage(`{"action":"open","url":"https://www.google.com","visible":false}`),
	}}
	routed, note, changed := enforceTaskBrowserVisibility(call, "open google in the browser and find a nearby mechanic")
	if !changed || !strings.Contains(note, "visible=true") {
		t.Fatalf("visible browser rewrite missing: changed=%t note=%q", changed, note)
	}
	var args struct {
		Visible bool `json:"visible"`
	}
	if err := json.Unmarshal(routed.Function.Arguments, &args); err != nil || !args.Visible {
		t.Fatalf("rewritten browser args = %s, err=%v", routed.Function.Arguments, err)
	}

	headless, _, changed := enforceTaskBrowserVisibility(call, "run this in a headless browser")
	if changed || string(headless.Function.Arguments) != string(call.Function.Arguments) {
		t.Fatalf("explicit headless request was unexpectedly rewritten: %s", headless.Function.Arguments)
	}
}

func TestBlockedBrowserHostIsNotReopened(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{{
		Name: "browser", Status: "done",
		Input:  `{"action":"open","url":"https://www.yell.com/uk/search?q=car"}`,
		Result: "title: Attention Required! | Cloudflare",
	}}}
	call := llm.ToolCallDef{Function: llm.FunctionCall{
		Name: "browser", Arguments: json.RawMessage(`{"action":"open","url":"https://www.yell.com/uk/search?q=garage"}`),
	}}
	if got := duplicateBlockedBrowserOpenSkip(run, call); !strings.Contains(got, "already returned an access block") {
		t.Fatalf("blocked host reopen was not skipped: %q", got)
	}
}
