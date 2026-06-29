package ledger

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"mauler/internal/store"
)

func TestRecordNormalisesUTF8AndPersists(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))
	badText := "before " + string([]byte{0xff, 0xfe}) + " after"

	_, err := l.Record(Event{
		Kind:    "tool_result",
		Message: badText,
		Output:  badText,
	})
	if err != nil {
		t.Fatalf("Record returned error: %v", err)
	}

	events, err := l.List(0)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if !utf8.ValidString(events[0].Message) || !utf8.ValidString(events[0].Output) {
		t.Fatalf("event strings were not valid UTF-8: %#v", events[0])
	}
	if !strings.Contains(events[0].Message, "\uFFFD") {
		t.Fatalf("invalid bytes were not replaced: %q", events[0].Message)
	}
}

func TestListReturnsNewestFirstAndHonoursLimit(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))

	for _, message := range []string{"oldest", "middle", "newest"} {
		if _, err := l.Record(Event{Kind: "event", Message: message}); err != nil {
			t.Fatalf("Record(%q) returned error: %v", message, err)
		}
	}

	events, err := l.List(2)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Message != "newest" || events[1].Message != "middle" {
		t.Fatalf("events not newest-first: %#v", events)
	}
}

func TestClearEmptiesLedger(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))
	if _, err := l.Record(Event{Kind: "event", Message: "keep briefly"}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if err := l.Clear(); err != nil {
		t.Fatalf("Clear returned error: %v", err)
	}
	events, err := l.List(0)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("got %d events after clear, want 0", len(events))
	}
}

func TestPruneRemovesMatchingEventsAndPreservesOrder(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))
	for _, event := range []Event{
		{Kind: "event", Message: "old keep"},
		{Kind: "tool_error", Message: "remove me", Status: "error"},
		{Kind: "event", Message: "new keep"},
	} {
		if _, err := l.Record(event); err != nil {
			t.Fatalf("Record returned error: %v", err)
		}
	}

	removed, err := l.Prune(func(event Event) bool {
		return event.Kind == "tool_error"
	})
	if err != nil {
		t.Fatalf("Prune returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d events, want 1", removed)
	}

	events, err := l.List(0)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events after prune, want 2", len(events))
	}
	if events[0].Message != "new keep" || events[1].Message != "old keep" {
		t.Fatalf("kept event order changed: %#v", events)
	}
}

func TestRecordMirrorsToSQLite(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := New(filepath.Join(dir, "run-ledger.jsonl"))
	l.AttachDB(db)

	if _, err := l.Record(Event{
		RunID:     "run-1",
		Kind:      "file_change",
		Source:    "tool",
		Tool:      "write_file",
		Status:    "created",
		Files:     []string{"/tmp/a.txt"},
		Artifacts: []string{".mauler_artifacts/a.txt"},
		Metadata:  map[string]string{"action": "created"},
	}); err != nil {
		t.Fatal(err)
	}

	var kind, file, artifact, meta string
	if err := db.QueryRow(`select kind from ledger_events where run_id = 'run-1'`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`select path from ledger_event_files`).Scan(&file); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`select path from ledger_event_artifacts`).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`select value from ledger_event_metadata where key = 'action'`).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if kind != "file_change" || file != "/tmp/a.txt" || artifact != ".mauler_artifacts/a.txt" || meta != "created" {
		t.Fatalf("bad sqlite mirror: kind=%q file=%q artifact=%q meta=%q", kind, file, artifact, meta)
	}
}

func TestListUsesSQLiteMirrorWhenAttached(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := New(filepath.Join(dir, "run-ledger.jsonl"))
	l.AttachDB(db)
	if _, err := l.Record(Event{
		Kind:      "artifact_done",
		Message:   "done",
		Files:     []string{"report.md"},
		Artifacts: []string{".mauler_artifacts/report.md"},
		Metadata:  map[string]string{"phase": "report"},
	}); err != nil {
		t.Fatal(err)
	}

	events, err := l.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	event := events[0]
	if len(event.Files) != 1 || event.Files[0] != "report.md" ||
		len(event.Artifacts) != 1 || event.Artifacts[0] != ".mauler_artifacts/report.md" ||
		event.Metadata["phase"] != "report" {
		t.Fatalf("sqlite-backed list lost child data: %#v", event)
	}
}

func TestBackfillDBFromJSONLImportsExistingLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run-ledger.jsonl")
	old := New(path)
	if _, err := old.Record(Event{Kind: "event", Message: "old"}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := New(path)
	l.AttachDB(db)

	imported, err := l.BackfillDBFromJSONL()
	if err != nil {
		t.Fatal(err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}
	events, err := l.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Message != "old" {
		t.Fatalf("backfilled events = %#v", events)
	}
}

func TestClearRemovesSQLiteLedgerEvents(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l := New(filepath.Join(dir, "run-ledger.jsonl"))
	l.AttachDB(db)
	if _, err := l.Record(Event{Kind: "event", Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`select count(*) from ledger_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ledger_events count = %d, want 0", count)
	}
}
