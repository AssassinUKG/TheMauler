package sessionstore

import (
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/store"
)

func TestStoreAndSearchSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	err := StoreSession(dbPath, "autosave", "/workspace/project", "qwen3.6", []Message{
		{Role: "user", Content: "Please fix the LM Studio duplicate model loading bug."},
		{Role: "assistant", Content: "We fixed the model-load cache and context check."},
		{Role: "tool", Content: "ok", ToolName: "edit_file"},
	})
	if err != nil {
		t.Fatalf("store session: %v", err)
	}

	results, err := Search(dbPath, "duplicate model loading", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least one search result")
	}
	if results[0].SessionName != "autosave" || !strings.Contains(strings.ToLower(results[0].Content), "duplicate") {
		t.Fatalf("unexpected first result: %#v", results[0])
	}
}

func TestStoreSessionReplacesExistingSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	if err := StoreSession(dbPath, "autosave", "/workspace/project", "qwen3.6", []Message{{Role: "user", Content: "old token"}}); err != nil {
		t.Fatalf("store old: %v", err)
	}
	if err := StoreSession(dbPath, "autosave", "/workspace/project", "qwen3.6", []Message{{Role: "user", Content: "new token"}}); err != nil {
		t.Fatalf("store new: %v", err)
	}

	oldResults, err := Search(dbPath, "old token", 5)
	if err != nil {
		t.Fatalf("search old: %v", err)
	}
	if len(oldResults) != 0 {
		t.Fatalf("old replaced session should not be searchable: %#v", oldResults)
	}
	newResults, err := Search(dbPath, "new token", 5)
	if err != nil {
		t.Fatalf("search new: %v", err)
	}
	if len(newResults) != 1 {
		t.Fatalf("expected replacement result, got %#v", newResults)
	}
}

func TestDeleteSessionRemovesIndexRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	if err := StoreSession(dbPath, "autosave", "/workspace/project", "qwen3.6", []Message{{Role: "user", Content: "delete sentinel"}}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := DeleteSession(dbPath, "autosave", "/workspace/project"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	results, err := Search(dbPath, "delete sentinel", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("deleted session should not be searchable: %#v", results)
	}
}

func TestClearRemovesAllIndexedSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	if err := StoreSession(dbPath, "one", "/workspace/project", "qwen3.6", []Message{{Role: "user", Content: "alpha sentinel"}}); err != nil {
		t.Fatalf("store one: %v", err)
	}
	if err := StoreSession(dbPath, "two", "/workspace/project", "qwen3.6", []Message{{Role: "user", Content: "beta sentinel"}}); err != nil {
		t.Fatalf("store two: %v", err)
	}
	if err := Clear(dbPath); err != nil {
		t.Fatalf("clear: %v", err)
	}
	results, err := Search(dbPath, "sentinel", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("cleared sessions should not be searchable: %#v", results)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewStore(db)
	record := CheckpointRecord{
		RunID:   "run-1",
		Prompt:  "prompt",
		Mode:    "Builder",
		Profile: "mock",
		Payload: `{"run_id":"run-1"}`,
		SavedAt: "2026-06-22T10:00:00Z",
	}

	if err := s.SaveCheckpoint(record); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	got, ok, err := s.LoadCheckpoint("run-1")
	if err != nil || !ok {
		t.Fatalf("load checkpoint: ok=%v err=%v", ok, err)
	}
	if got != record {
		t.Fatalf("checkpoint = %#v, want %#v", got, record)
	}

	record.Payload = `{"run_id":"run-1","updated":true}`
	record.SavedAt = "2026-06-22T10:01:00Z"
	if err := s.SaveCheckpoint(record); err != nil {
		t.Fatalf("upsert checkpoint: %v", err)
	}
	list, err := s.ListCheckpoints()
	if err != nil {
		t.Fatalf("list checkpoints: %v", err)
	}
	if len(list) != 1 || list[0].Payload != record.Payload {
		t.Fatalf("checkpoint list = %#v", list)
	}
	if err := s.DeleteCheckpoint("run-1"); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	if _, ok, err := s.LoadCheckpoint("run-1"); err != nil || ok {
		t.Fatalf("deleted checkpoint load: ok=%v err=%v", ok, err)
	}
}
