package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/ledger"
	"mauler/internal/llm"
)

func TestRecordFileChangeClassifiesCreatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scratch.txt")
	tc := toolCallForFileChangeTest("write", map[string]any{
		"path":    path,
		"content": "temporary\n",
	})
	before := snapshotToolTarget(tc)
	if before.Exists {
		t.Fatal("precondition failed: target should not exist before write")
	}
	if err := os.WriteFile(path, []byte("temporary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := ledger.New(filepath.Join(dir, "ledger.jsonl"))
	app := &App{ledger: l}

	app.recordFileChange("run-created", tc, before, "Verification: write confirmed for scratch.txt (10 bytes).", 12)

	events, err := l.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Kind != "file_change" || event.Status != "created" {
		t.Fatalf("event kind/status = %s/%s, want file_change/created", event.Kind, event.Status)
	}
	if event.Metadata["action"] != "created" || !strings.Contains(event.Metadata["cleanup_hint"], "safe candidate") {
		t.Fatalf("metadata did not mark cleanup candidate: %#v", event.Metadata)
	}
	if len(event.Files) != 1 || !strings.Contains(event.Files[0], "scratch.txt") {
		t.Fatalf("files = %#v, want scratch path", event.Files)
	}
	if event.Metadata["after_sha256"] == "" {
		t.Fatalf("after_sha256 missing: %#v", event.Metadata)
	}
}

func TestRecordFileChangeClassifiesModifiedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tc := toolCallForFileChangeTest("edit", map[string]any{
		"path":       path,
		"old":        "before",
		"new":        "after",
	})
	before := snapshotToolTarget(tc)
	if !before.Exists {
		t.Fatal("precondition failed: target should exist before edit")
	}
	if err := os.WriteFile(path, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := ledger.New(filepath.Join(dir, "ledger.jsonl"))
	app := &App{ledger: l}

	app.recordFileChange("run-modified", tc, before, "Verification: edit confirmed for existing.txt (6 bytes).", 7)

	events, err := l.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Kind != "file_change" || event.Status != "modified" {
		t.Fatalf("event kind/status = %s/%s, want file_change/modified", event.Kind, event.Status)
	}
	if event.Metadata["action"] != "modified" || !strings.Contains(event.Metadata["cleanup_hint"], "do not delete") {
		t.Fatalf("metadata should protect modified files from cleanup: %#v", event.Metadata)
	}
	if event.Metadata["before_sha256"] == "" || event.Metadata["after_sha256"] == "" || event.Metadata["before_sha256"] == event.Metadata["after_sha256"] {
		t.Fatalf("hash metadata should show before/after change: %#v", event.Metadata)
	}
}

func TestFileChangesToolListsCreatedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.tmp")
	tc := toolCallForFileChangeTest("write", map[string]any{
		"path":    path,
		"content": "temporary\n",
	})
	before := snapshotToolTarget(tc)
	if err := os.WriteFile(path, []byte("temporary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := ledger.New(filepath.Join(dir, "ledger.jsonl"))
	app := &App{ledger: l}
	app.recordFileChange("run-created", tc, before, "Verification: write confirmed for cleanup.tmp (10 bytes).", 12)

	out, err := (&fileChangesTool{app: app}).Run(t.Context(), json.RawMessage(`{"status":"created","limit":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cleanup.tmp") || !strings.Contains(out, "safe candidate") {
		t.Fatalf("file_changes output missing cleanup record: %s", out)
	}
}

func toolCallForFileChangeTest(name string, args map[string]any) llm.ToolCallDef {
	data, _ := json.Marshal(args)
	return llm.ToolCallDef{
		ID: "call-test",
		Function: llm.FunctionCall{
			Name:      name,
			Arguments: data,
		},
	}
}
