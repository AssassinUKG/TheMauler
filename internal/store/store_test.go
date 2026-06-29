package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenRunsMigrationsAndSetsUserVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", version, CurrentSchemaVersion)
	}
	for _, table := range []string{
		"sessions", "messages", "messages_fts",
		"task_runs", "task_run_tools", "task_run_events",
		"ledger_events", "ledger_event_files", "ledger_event_artifacts", "ledger_event_metadata",
		"todos",
		"memory_entries", "memory_tags",
		"learning_decisions",
	} {
		var name string
		if err := db.QueryRow(`select name from sqlite_schema where name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("missing schema object %s: %v", table, err)
		}
	}
}

func TestMigrateExistingV0DatabaseIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`create table sessions (id text primary key, name text not null, scope text, model text, updated_at text not null, message_count integer not null default 0)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("user_version = %d, want %d", version, CurrentSchemaVersion)
	}
}
