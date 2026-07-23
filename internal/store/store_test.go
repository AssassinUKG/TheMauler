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
		"run_checkpoints",
		"channel_work_queue",
		"app_state",
		"engagements",
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

func TestMigrateV11PreservesExistingTaskRunsAndAddsClaimantDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 10; version++ {
		if err := runMigration(db, version); err != nil {
			db.Close()
			t.Fatalf("migration %d: %v", version, err)
		}
	}
	if _, err := db.Exec(`insert into task_runs (id, started_at) values ('legacy-run', '2026-07-13T12:00:00Z')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var claimantID, claimantAlias, origin string
	if err := db.QueryRow(`select claimant_id, claimant_alias, origin from task_runs where id = 'legacy-run'`).Scan(&claimantID, &claimantAlias, &origin); err != nil {
		t.Fatal(err)
	}
	if claimantID != "" || claimantAlias != "" || origin != "" {
		t.Fatalf("legacy claimant defaults = %q %q %q", claimantID, claimantAlias, origin)
	}
}

func TestMigrateV12PreservesRunsAndAddsControlPlaneDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 11; version++ {
		if err := runMigration(db, version); err != nil {
			db.Close()
			t.Fatalf("migration %d: %v", version, err)
		}
	}
	if _, err := db.Exec(`insert into task_runs (id, started_at) values ('legacy-control-run', '2026-07-15T09:00:00Z')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var contractJSON, digest, phase, stateJSON string
	if err := db.QueryRow(`select contract_json, contract_digest, control_phase, control_state_json from task_runs where id = 'legacy-control-run'`).Scan(&contractJSON, &digest, &phase, &stateJSON); err != nil {
		t.Fatal(err)
	}
	if contractJSON != "" || digest != "" || phase != "" || stateJSON != "" {
		t.Fatalf("legacy control defaults = %q %q %q %q", contractJSON, digest, phase, stateJSON)
	}
}
