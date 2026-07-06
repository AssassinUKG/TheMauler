package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestMemoryBindingsRecordLedgerEvents(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := New()
	t.Cleanup(func() { app.OnShutdown(context.Background()) })

	entry, err := app.AddMemory("Preferred shell", "Use WSL for pentest work.", []string{"ops", "wsl"})
	if err != nil {
		t.Fatalf("AddMemory returned error: %v", err)
	}
	entry.Content = "Use Kali WSL for pentest work."
	if _, err := app.SaveMemoryEntry(entry); err != nil {
		t.Fatalf("SaveMemoryEntry returned error: %v", err)
	}
	if err := app.DeleteMemoryEntry(entry.ID); err != nil {
		t.Fatalf("DeleteMemoryEntry returned error: %v", err)
	}

	events, err := app.ListLedgerEvents(10)
	if err != nil {
		t.Fatalf("ListLedgerEvents returned error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %#v", len(events), events)
	}
	wantKinds := []string{"memory_delete", "memory_write", "memory_write"}
	wantStatuses := []string{"deleted", "updated", "created"}
	for i := range wantKinds {
		if events[i].Kind != wantKinds[i] || events[i].Status != wantStatuses[i] {
			t.Fatalf("event %d = %s/%s, want %s/%s: %#v", i, events[i].Kind, events[i].Status, wantKinds[i], wantStatuses[i], events[i])
		}
	}
	if events[1].Metadata["kind"] != "note" || !strings.Contains(events[1].Metadata["tags"], "wsl") {
		t.Fatalf("memory metadata missing kind/tags: %#v", events[1])
	}
}

func TestSkillBindingsRecordLedgerEvents(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := New()
	t.Cleanup(func() { app.OnShutdown(context.Background()) })

	skill, err := app.SaveSkill(Skill{
		Name:        "Kali WSL Flow",
		Description: "Use for Kali WSL pentest work.",
		Version:     "1.0.0",
		Tags:        []string{"ops", "wsl"},
		Body:        "## Steps\n\n1. Use WSL.",
	})
	if err != nil {
		t.Fatalf("SaveSkill returned error: %v", err)
	}
	if err := app.DeleteSkill(skill.Name); err != nil {
		t.Fatalf("DeleteSkill returned error: %v", err)
	}

	events, err := app.ListLedgerEvents(10)
	if err != nil {
		t.Fatalf("ListLedgerEvents returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %#v", len(events), events)
	}
	if events[0].Kind != "skill_delete" || events[0].Message != skill.Name {
		t.Fatalf("newest event should be skill delete: %#v", events[0])
	}
	if events[1].Kind != "skill_write" || events[1].Message != skill.Name || !strings.Contains(events[1].Output, "Use WSL") {
		t.Fatalf("older event should be skill write with raw skill output: %#v", events[1])
	}
}

func TestCategorizedToolLedgerEvents(t *testing.T) {
	l := ledger.New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))
	app := &App{ledger: l}

	for _, tc := range []llm.ToolCallDef{
		{ID: "call-web", Function: llm.FunctionCall{Name: "web_search", Arguments: []byte(`{"query":"x"}`)}},
		{ID: "call-browser", Function: llm.FunctionCall{Name: "browser", Arguments: []byte(`{"action":"open","url":"https://example.com"}`)}},
		{ID: "call-todo", Function: llm.FunctionCall{Name: "todo_write", Arguments: []byte(`{"action":"done","id":"todo-1"}`)}},
		{ID: "call-sub", Function: llm.FunctionCall{Name: "task", Arguments: []byte(`{"type":"review","task":"review"}`)}},
	} {
		app.recordCategorizedToolLedger("run-1", tc, "done", "ok", 12)
	}

	events, err := app.ListLedgerEvents(10)
	if err != nil {
		t.Fatalf("ListLedgerEvents returned error: %v", err)
	}
	got := map[string]bool{}
	for _, event := range events {
		got[event.Kind] = true
		if event.RunID != "run-1" || event.Status != "done" || event.DurationMs != 12 {
			t.Fatalf("bad category event: %#v", event)
		}
	}
	for _, want := range []string{"web_research", "browser_action", "planner_event", "subagent_result"} {
		if !got[want] {
			t.Fatalf("missing categorized event %q in %#v", want, events)
		}
	}
}

func TestModelLoadRecordsLedgerRetriesAndSuccess(t *testing.T) {
	withFastModelLoadRetry(t)
	l := ledger.New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))
	app := &App{ledger: l}
	client := &countingLoader{failuresBeforeSuccess: 1, loadErr: errors.New("bridge starting")}
	profile := settings.Profile{
		Backend:   "lmstudio",
		BaseURL:   "http://localhost:1234/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatalf("ensureModelLoaded returned error: %v", err)
	}

	events, err := app.ListLedgerEvents(10)
	if err != nil {
		t.Fatalf("ListLedgerEvents returned error: %v", err)
	}
	var sawRetry, sawOK bool
	for _, event := range events {
		if event.Kind != "model_load" {
			continue
		}
		if event.Status == "retry" && strings.Contains(event.Error, "bridge starting") {
			sawRetry = true
		}
		if event.Status == "ok" && event.Message == modelLoadKey(profile) {
			sawOK = true
		}
	}
	if !sawRetry || !sawOK {
		t.Fatalf("missing model retry/success events: %#v", events)
	}
}
