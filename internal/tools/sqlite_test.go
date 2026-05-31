package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteSchemaAndQueryReadOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`create table task_runs (id text primary key, status text); insert into task_runs values ('run-1', 'done')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	schema, err := (&SQLiteSchema{}).Run(context.Background(), mustSQLiteJSON(t, map[string]any{"path": dbPath}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(schema, "task_runs") || !strings.Contains(schema, "status TEXT") {
		t.Fatalf("schema missing table/columns:\n%s", schema)
	}

	result, err := (&SQLiteQuery{}).Run(context.Background(), mustSQLiteJSON(t, map[string]any{
		"path":  dbPath,
		"query": "select id, status from task_runs",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "id=run-1") || !strings.Contains(result, "status=done") {
		t.Fatalf("query missing row:\n%s", result)
	}
}

func TestSQLiteQueryRejectsWritesAndMultipleStatements(t *testing.T) {
	for _, query := range []string{
		"delete from task_runs",
		"select 1; delete from task_runs",
	} {
		if _, err := normalizeReadOnlySQLiteQuery(query); err == nil {
			t.Fatalf("query %q should be rejected", query)
		}
	}
}

func mustSQLiteJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
