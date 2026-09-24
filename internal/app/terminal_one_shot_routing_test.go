package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestRouteTerminalHTTPCommandToIsolatedShell(t *testing.T) {
	command := `curl -s http://bedside.htb/ | head -80; curl -s http://bedside.htb:3000/`
	call := llm.ToolCallDef{
		ID: "call-1",
		Function: llm.FunctionCall{
			Name:      "terminal_send",
			Arguments: json.RawMessage(`{"command":"curl -s http://bedside.htb/ | head -80; curl -s http://bedside.htb:3000/","wait_ms":700}`),
		},
	}
	defs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "terminal_send"}},
		{Function: llm.ToolFunctionDef{Name: "shell"}},
	}

	got, note, changed := routeTerminalHTTPCommandToShell(call, defs)
	if !changed {
		t.Fatal("expected independent HTTP terminal command to be rewritten")
	}
	if got.ID != call.ID || got.Function.Name != "shell" {
		t.Fatalf("rewritten call = %#v, want same id and shell tool", got)
	}
	var args struct {
		Command string `json:"command"`
		Backend string `json:"backend"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(got.Function.Arguments, &args); err != nil {
		t.Fatal(err)
	}
	if args.Command != command || args.Backend != "wsl" || args.Timeout != 30 {
		t.Fatalf("rewritten args = %#v", args)
	}
	if !strings.Contains(note, "isolated WSL one-shot") {
		t.Fatalf("rewrite note should explain the evidence path: %q", note)
	}
	if !shellCallPrefersIsolatedBackend(got) {
		t.Fatal("explicit WSL rewrite must bypass the shared terminal")
	}
}

func TestRouteTerminalHTTPCommandRequiresAdvertisedShell(t *testing.T) {
	call := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "terminal_send",
		Arguments: json.RawMessage(`{"command":"wget http://bedside.htb/file"}`),
	}}
	got, _, changed := routeTerminalHTTPCommandToShell(call, []llm.ToolDef{{Function: llm.ToolFunctionDef{Name: "terminal_send"}}})
	if changed || got.Function.Name != "terminal_send" {
		t.Fatalf("call should remain unchanged when shell is unavailable: %#v", got)
	}
}

func TestAdviseReadyTerminalRoutesHTTPProbeAwayFromPTY(t *testing.T) {
	got := adviseTerminalRun(TerminalStateSnapshot{State: "ready"}, "timeout 8 curl -s http://bedside.htb:3000/")
	if got.Allowed || got.RecommendedTool != "http_probe" {
		t.Fatalf("ready terminal HTTP command should be routed, got %#v", got)
	}
}

func TestNonHTTPInteractiveCommandIsNotRewritten(t *testing.T) {
	call := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "terminal_send",
		Arguments: json.RawMessage(`{"command":"nmap -sV 10.129.83.45"}`),
	}}
	defs := []llm.ToolDef{{Function: llm.ToolFunctionDef{Name: "shell"}}}
	got, _, changed := routeTerminalHTTPCommandToShell(call, defs)
	if changed || got.Function.Name != "terminal_send" {
		t.Fatalf("non-HTTP live command should not be rewritten: %#v", got)
	}
}
