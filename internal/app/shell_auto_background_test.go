package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestAutoBackgroundShellCallRewritesLongScans(t *testing.T) {
	if !shouldAutoBackgroundCommand("nmap -sC -sV -A -oN scans/full.txt 10.10.10.10", 10) {
		t.Fatal("classifier should treat this nmap command as long-running")
	}
	raw, note, ok := autoBackgroundShellCall(llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"command":"nmap -sC -sV -A -oN scans/full.txt 10.10.10.10","timeout":10}`),
	}})
	if !ok {
		t.Fatal("expected long nmap command to be auto-backgrounded")
	}
	if !strings.Contains(note, "nmap") {
		t.Fatalf("note missing command: %q", note)
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	if args["background"] != true {
		t.Fatalf("background not set: %#v", args)
	}
	if _, ok := args["timeout"]; ok {
		t.Fatalf("timeout should be removed when auto-backgrounding: %#v", args)
	}
}

func TestAutoBackgroundShellCallLeavesShortCommands(t *testing.T) {
	_, _, ok := autoBackgroundShellCall(llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"command":"id","timeout":5}`),
	}})
	if ok {
		t.Fatal("short command should not be auto-backgrounded")
	}
}
