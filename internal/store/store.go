package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"mauler/internal/settings"

	_ "modernc.org/sqlite"
)

const CurrentSchemaVersion = 12

func DefaultPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
}

func OpenDefault() (*sql.DB, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Open(path)
}

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_, _ = db.Exec(`PRAGMA journal_mode=DELETE`)
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(db *sql.DB) error {
	version, err := schemaVersion(db)
	if err != nil {
		return err
	}
	for version < CurrentSchemaVersion {
		next := version + 1
		if err := runMigration(db, next); err != nil {
			return err
		}
		version = next
	}
	return nil
}

func schemaVersion(db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func runMigration(db *sql.DB, version int) error {
	switch version {
	case 1:
		return migrateV1SessionRecall(db)
	case 2:
		return migrateV2TaskRuns(db)
	case 3:
		return migrateV3LedgerEvents(db)
	case 4:
		return migrateV4Todos(db)
	case 5:
		return migrateV5Memory(db)
	case 6:
		return migrateV6LearningDecisions(db)
	case 7:
		return migrateV7RunCheckpoints(db)
	case 8:
		return migrateV8ChannelWorkQueue(db)
	case 9:
		return migrateV9AppState(db)
	case 10:
		return migrateV10Engagements(db)
	case 11:
		return migrateV11TaskRunClaimants(db)
	case 12:
		return migrateV12TaskRunControlPlane(db)
	default:
		return fmt.Errorf("unknown schema migration %d", version)
	}
}

func migrateV12TaskRunControlPlane(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
ALTER TABLE task_runs ADD COLUMN contract_json TEXT NOT NULL DEFAULT '';
ALTER TABLE task_runs ADD COLUMN contract_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE task_runs ADD COLUMN control_phase TEXT NOT NULL DEFAULT '';
ALTER TABLE task_runs ADD COLUMN control_state_json TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_task_runs_control_phase ON task_runs(control_phase);
CREATE INDEX IF NOT EXISTS idx_task_runs_contract_digest ON task_runs(contract_digest);
PRAGMA user_version=12;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV11TaskRunClaimants(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
ALTER TABLE task_runs ADD COLUMN claimant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE task_runs ADD COLUMN claimant_alias TEXT NOT NULL DEFAULT '';
ALTER TABLE task_runs ADD COLUMN origin TEXT NOT NULL DEFAULT '';
PRAGMA user_version=11;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV1SessionRecall(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  scope TEXT,
  model TEXT,
  updated_at TEXT NOT NULL,
  message_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  idx INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT,
  tool_name TEXT,
  tool_calls TEXT,
  timestamp TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, idx);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content);
PRAGMA user_version=1;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV3LedgerEvents(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS ledger_events (
  event_pk INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL,
  run_id TEXT,
  kind TEXT NOT NULL,
  source TEXT,
  tool TEXT,
  status TEXT,
  state TEXT,
  message TEXT,
  detail TEXT,
  input TEXT,
  output TEXT,
  error TEXT,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  timestamp TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ledger_events_run ON ledger_events(run_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_ledger_events_kind ON ledger_events(kind, timestamp);
CREATE INDEX IF NOT EXISTS idx_ledger_events_source ON ledger_events(source, timestamp);
CREATE INDEX IF NOT EXISTS idx_ledger_events_tool ON ledger_events(tool, timestamp);
CREATE INDEX IF NOT EXISTS idx_ledger_events_status ON ledger_events(status, timestamp);
CREATE INDEX IF NOT EXISTS idx_ledger_events_timestamp ON ledger_events(timestamp);

CREATE TABLE IF NOT EXISTS ledger_event_files (
  event_pk INTEGER NOT NULL REFERENCES ledger_events(event_pk) ON DELETE CASCADE,
  path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ledger_event_files_path ON ledger_event_files(path);

CREATE TABLE IF NOT EXISTS ledger_event_artifacts (
  event_pk INTEGER NOT NULL REFERENCES ledger_events(event_pk) ON DELETE CASCADE,
  path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ledger_event_artifacts_path ON ledger_event_artifacts(path);

CREATE TABLE IF NOT EXISTS ledger_event_metadata (
  event_pk INTEGER NOT NULL REFERENCES ledger_events(event_pk) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value TEXT
);
CREATE INDEX IF NOT EXISTS idx_ledger_event_metadata_key ON ledger_event_metadata(key);

PRAGMA user_version=3;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV2TaskRuns(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS task_runs (
  id TEXT PRIMARY KEY,
  prompt TEXT,
  mode TEXT,
  profile TEXT,
  model TEXT,
  status TEXT,
  state TEXT,
  stop_reason TEXT,
  stop_detail TEXT,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  total_tokens INTEGER NOT NULL DEFAULT 0,
  summary TEXT,
  response TEXT
);
CREATE INDEX IF NOT EXISTS idx_task_runs_started_at ON task_runs(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_runs_status ON task_runs(status);
CREATE INDEX IF NOT EXISTS idx_task_runs_stop_reason ON task_runs(stop_reason);

CREATE TABLE IF NOT EXISTS task_run_tools (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
  idx INTEGER NOT NULL,
  name TEXT NOT NULL,
  input TEXT,
  result TEXT,
  status TEXT,
  timestamp TEXT,
  duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_task_run_tools_run ON task_run_tools(run_id, idx);
CREATE INDEX IF NOT EXISTS idx_task_run_tools_name ON task_run_tools(name);

CREATE TABLE IF NOT EXISTS task_run_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL REFERENCES task_runs(id) ON DELETE CASCADE,
  idx INTEGER NOT NULL,
  kind TEXT,
  message TEXT,
  detail TEXT,
  timestamp TEXT
);
CREATE INDEX IF NOT EXISTS idx_task_run_events_run ON task_run_events(run_id, idx);
CREATE INDEX IF NOT EXISTS idx_task_run_events_kind ON task_run_events(kind);

PRAGMA user_version=2;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV4Todos(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS todos (
  id TEXT PRIMARY KEY,
  idx INTEGER NOT NULL,
  text TEXT NOT NULL,
  status TEXT NOT NULL,
  detail TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_todos_idx ON todos(idx);
CREATE INDEX IF NOT EXISTS idx_todos_status ON todos(status);

PRAGMA user_version=4;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV5Memory(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS memory_entries (
  id TEXT PRIMARY KEY,
  scope TEXT,
  title TEXT,
  content TEXT NOT NULL,
  kind TEXT NOT NULL,
  confidence TEXT NOT NULL,
  source TEXT NOT NULL,
  importance INTEGER NOT NULL DEFAULT 3,
  pinned INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_used_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_memory_entries_scope ON memory_entries(scope);
CREATE INDEX IF NOT EXISTS idx_memory_entries_kind ON memory_entries(kind);
CREATE INDEX IF NOT EXISTS idx_memory_entries_updated_at ON memory_entries(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_entries_importance ON memory_entries(importance DESC);

CREATE TABLE IF NOT EXISTS memory_tags (
  memory_id TEXT NOT NULL REFERENCES memory_entries(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  PRIMARY KEY (memory_id, tag)
);
CREATE INDEX IF NOT EXISTS idx_memory_tags_tag ON memory_tags(tag);

PRAGMA user_version=5;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV6LearningDecisions(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS learning_decisions (
  candidate_id TEXT PRIMARY KEY,
  run_id TEXT,
  type TEXT,
  title TEXT,
  decision TEXT NOT NULL,
  reason TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_learning_decisions_decision ON learning_decisions(decision);
CREATE INDEX IF NOT EXISTS idx_learning_decisions_run ON learning_decisions(run_id);
CREATE INDEX IF NOT EXISTS idx_learning_decisions_created_at ON learning_decisions(created_at DESC);

PRAGMA user_version=6;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV7RunCheckpoints(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS run_checkpoints (
  run_id TEXT PRIMARY KEY,
  prompt TEXT,
  mode TEXT,
  profile TEXT,
  payload TEXT NOT NULL,
  saved_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_run_checkpoints_saved_at ON run_checkpoints(saved_at DESC);

PRAGMA user_version=7;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV8ChannelWorkQueue(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS channel_work_queue (
  id TEXT PRIMARY KEY,
  envelope_json TEXT NOT NULL,
  route_json TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_channel_work_queue_status ON channel_work_queue(status, created_at);
CREATE INDEX IF NOT EXISTS idx_channel_work_queue_created_at ON channel_work_queue(created_at);
PRAGMA user_version=8;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV9AppState(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
PRAGMA user_version=9;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func migrateV10Engagements(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS engagements (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  workspace TEXT NOT NULL,
  workflow_id TEXT NOT NULL,
  workflow_version TEXT,
  workflow_digest TEXT,
  checklist_id TEXT NOT NULL,
  checklist_version TEXT,
  checklist_digest TEXT,
  workflow_json TEXT NOT NULL,
  checklist_json TEXT NOT NULL,
  state_json TEXT NOT NULL,
  revision INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_engagements_workspace ON engagements(workspace, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_engagements_updated_at ON engagements(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_engagements_pack ON engagements(workflow_id, workflow_version, checklist_id, checklist_version);

PRAGMA user_version=10;
`); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
