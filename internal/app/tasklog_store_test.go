package app

import (
	"path/filepath"
	"testing"

	"mauler/internal/store"
)

func TestTaskRunDBSaveLoadAndReplace(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	run := TaskRun{
		ID:        "task-1",
		Prompt:    "do thing",
		Mode:      "Builder",
		Profile:   "qwen",
		Model:     "model",
		Status:    "done",
		State:     "done",
		StartedAt: "2026-06-15T10:00:00+01:00",
		EndedAt:   "2026-06-15T10:01:00+01:00",
		Summary:   "first",
		Tools:     []TaskToolEvent{{Name: "shell", Status: "done", Input: "{}", Result: "ok", Timestamp: "2026-06-15T10:00:30+01:00"}},
		Events:    []TaskRunEvent{{Kind: "state", Message: "testing", Timestamp: "2026-06-15T10:00:20+01:00"}},
	}
	if err := saveTaskRunDB(db, run, nil); err != nil {
		t.Fatal(err)
	}
	run.Summary = "replaced"
	run.Tools = append(run.Tools, TaskToolEvent{Name: "write_file", Status: "done", Timestamp: "2026-06-15T10:00:40+01:00"})
	if err := saveTaskRunDB(db, run, nil); err != nil {
		t.Fatal(err)
	}

	runs, err := loadTaskRunsDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Summary != "replaced" || len(runs[0].Tools) != 2 || len(runs[0].Events) != 1 {
		t.Fatalf("unexpected loaded run: %#v", runs[0])
	}
}

func TestTaskRunDBRetentionAndClear(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &settingsLoggingConfig{enabled: true, maxRuns: 2}
	for _, run := range []TaskRun{
		{ID: "old", StartedAt: "2026-06-15T09:00:00+01:00", Status: "done"},
		{ID: "mid", StartedAt: "2026-06-15T10:00:00+01:00", Status: "done"},
		{ID: "new", StartedAt: "2026-06-15T11:00:00+01:00", Status: "done"},
	} {
		if err := saveTaskRunDB(db, run, cfg); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := loadTaskRunsDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != "new" || runs[1].ID != "mid" {
		t.Fatalf("retained runs = %#v", runs)
	}
	if err := clearTaskRunsDB(db); err != nil {
		t.Fatal(err)
	}
	runs, err = loadTaskRunsDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("clear left runs: %#v", runs)
	}
}

func TestTaskRunJSONExportImportUsesSQLiteStore(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := &App{db: db}
	original := []TaskRun{
		{
			ID:        "task-export",
			Prompt:    "export me",
			Status:    "done",
			StartedAt: "2026-06-15T12:00:00+01:00",
			Tools:     []TaskToolEvent{{Name: "shell", Status: "done", Result: "ok"}},
			Events:    []TaskRunEvent{{Kind: "state", Message: "done"}},
		},
	}
	if err := replaceTaskRunsDB(db, original); err != nil {
		t.Fatal(err)
	}
	exported, err := app.ExportTaskRunsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ClearTaskRuns(); err != nil {
		t.Fatal(err)
	}
	if count, err := app.ImportTaskRunsJSON(exported); err != nil || count != 1 {
		t.Fatalf("import count/error = %d/%v", count, err)
	}
	runs, err := app.ListTaskRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != "task-export" || len(runs[0].Tools) != 1 || len(runs[0].Events) != 1 {
		t.Fatalf("imported runs = %#v", runs)
	}
}
