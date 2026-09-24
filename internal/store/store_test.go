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
		"repo_index_generations", "repo_index_roots", "repo_index_files",
		"repo_index_chunks", "repo_index_chunks_fts", "repo_index_active",
		"repository_reviews",
	} {
		var name string
		if err := db.QueryRow(`select name from sqlite_schema where name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("missing schema object %s: %v", table, err)
		}
	}
}

func TestMigrateV18AddsDurableRepositoryReviews(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 17; version++ {
		if err := runMigration(db, version); err != nil {
			db.Close()
			t.Fatalf("migration %d: %v", version, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO repository_reviews(
review_id, workspace, generation_id, manifest_digest, plan_digest, requested_shards,
state, status_json, plan_json, submissions_json, merge_json, started_at, updated_at
) VALUES ('review-1', 'C:/repo', 'generation-1', 'manifest', 'plan', 2,
'interrupted', '{}', '{}', '[]', '{}', '2026-09-24T00:00:00Z', '2026-09-24T00:00:01Z')`); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM repository_reviews WHERE review_id='review-1'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "interrupted" {
		t.Fatalf("repository review state = %q", state)
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

func TestMigrateV13PreservesRunsAndAddsGenerationDefault(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 12; version++ {
		if err := runMigration(db, version); err != nil {
			db.Close()
			t.Fatalf("migration %d: %v", version, err)
		}
	}
	if _, err := db.Exec(`insert into task_runs (id, started_at) values ('legacy-generation-run', '2026-09-03T09:00:00Z')`); err != nil {
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
	var generation uint64
	if err := db.QueryRow(`select generation from task_runs where id = 'legacy-generation-run'`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != 0 {
		t.Fatalf("legacy generation default = %d, want 0", generation)
	}
}

func TestMigrateV14PreservesRunsAndAddsParentDefault(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 13; version++ {
		if err := runMigration(db, version); err != nil {
			db.Close()
			t.Fatalf("migration %d: %v", version, err)
		}
	}
	if _, err := db.Exec(`insert into task_runs (id, started_at) values ('legacy-parent-run', '2026-09-03T10:00:00Z')`); err != nil {
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
	var parentRunID string
	if err := db.QueryRow(`select parent_run_id from task_runs where id = 'legacy-parent-run'`).Scan(&parentRunID); err != nil {
		t.Fatal(err)
	}
	if parentRunID != "" {
		t.Fatalf("legacy parent run id default = %q, want empty", parentRunID)
	}
}

func TestMigrateV15PreservesRunsAndAddsConversationEpochDefault(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 14; version++ {
		if err := runMigration(db, version); err != nil {
			db.Close()
			t.Fatalf("migration %d: %v", version, err)
		}
	}
	if _, err := db.Exec(`insert into task_runs (id, started_at) values ('legacy-epoch-run', '2026-09-15T10:00:00Z')`); err != nil {
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
	var epoch uint64
	if err := db.QueryRow(`select conversation_epoch from task_runs where id = 'legacy-epoch-run'`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if epoch != 0 {
		t.Fatalf("legacy conversation epoch default = %d, want 0", epoch)
	}
}

func TestMigrateV16PreservesRunsAndAddsFinalizedArtifactsDefault(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 15; version++ {
		if err := runMigration(db, version); err != nil {
			db.Close()
			t.Fatalf("migration %d: %v", version, err)
		}
	}
	if _, err := db.Exec(`insert into task_runs (id, started_at) values ('legacy-artifact-run', '2026-09-15T11:00:00Z')`); err != nil {
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
	var artifacts string
	if err := db.QueryRow(`select finalized_artifacts_json from task_runs where id = 'legacy-artifact-run'`).Scan(&artifacts); err != nil {
		t.Fatal(err)
	}
	if artifacts != "[]" {
		t.Fatalf("legacy finalized artifacts default = %q, want []", artifacts)
	}
}
