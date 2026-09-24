package app

import (
	"path/filepath"
	"testing"

	"mauler/internal/ledger"
)

func TestAgentSessionRegistryTracksListenerAndConnection(t *testing.T) {
	app := &App{ledger: ledger.New(filepath.Join(t.TempDir(), "ledger.jsonl"))}

	app.upsertAgentSession(AgentSession{
		ID:              "listener:4444",
		Kind:            "listener",
		State:           "listening",
		Port:            4444,
		Lhost:           "10.10.15.223",
		TerminalSession: "shell-1",
	})
	app.updateAgentSessionsFromTerminalState(TerminalStateSnapshot{
		Session: "shell-1",
		State:   "connected",
		Summary: "Shared terminal appears to contain a connected live session",
		Lines:   []string{"connect to [10.10.15.223] from connected.htb", "uid=999(asterisk) gid=1000(asterisk)"},
	})

	sessions := app.ListAgentSessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1: %#v", len(sessions), sessions)
	}
	got := sessions[0]
	if got.State != "connected" {
		t.Fatalf("state = %q, want connected", got.State)
	}
	if got.User != "asterisk" {
		t.Fatalf("user = %q, want asterisk", got.User)
	}
	if got.Port != 4444 || got.Lhost != "10.10.15.223" {
		t.Fatalf("listener identity was not preserved: %#v", got)
	}

	events, err := app.ledger.List(10)
	if err != nil {
		t.Fatal(err)
	}
	var sawConnected bool
	for _, event := range events {
		if event.Kind == "session_state" && event.Status == "connected" {
			sawConnected = true
		}
	}
	if !sawConnected {
		t.Fatalf("session_state connected event not recorded: %#v", events)
	}
}

func TestRecordToolRoutingStateEmitsOnlyTransitions(t *testing.T) {
	app := &App{ledger: ledger.New(filepath.Join(t.TempDir(), "ledger.jsonl"))}

	app.recordToolRoutingState("run-1", "use the webshell to check id", "auto", "auto", nil, 0, 0, TerminalStateSnapshot{State: "ready"})
	app.recordToolRoutingState("run-1", "use the webshell to check id", "auto", "auto", nil, 0, 0, TerminalStateSnapshot{State: "ready"})
	app.recordToolRoutingState("run-1", "start a reverse shell listener", "auto", "auto", nil, 0, 0, TerminalStateSnapshot{State: "ready"})

	events, err := app.ledger.List(10)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, event := range events {
		if event.Kind == "tool_routing" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("tool_routing events = %d, want 2: %#v", count, events)
	}
}
