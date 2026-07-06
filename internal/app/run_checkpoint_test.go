package app

import (
	"path/filepath"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/store"
)

func TestRunCheckpointRoundTrip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := settings.DefaultSettings()
	app := &App{
		cfg:      &cfg,
		db:       db,
		history:  agent.NewHistory(8192),
		rollback: &agent.Rollback{},
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "hello checkpoint"))
	run := startTaskRun("hello checkpoint", "Builder", "mock", "mock-model")
	run.Tools = []TaskToolEvent{{Name: "read", Status: "done"}}

	app.saveRunCheckpoint(run, cfg)

	checkpoints, err := app.ListResumableRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 {
		t.Fatalf("checkpoint count = %d, want 1", len(checkpoints))
	}
	cp := checkpoints[0]
	if cp.RunID != run.ID || cp.Prompt != run.Prompt || len(cp.Messages) != 1 {
		t.Fatalf("bad checkpoint: %#v", cp)
	}
	if cp.Messages[0].Content != "hello checkpoint" {
		t.Fatalf("checkpoint messages = %#v", cp.Messages)
	}
}

func TestMaybeCheckpointThrottles(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := settings.DefaultSettings()
	app := &App{
		cfg:      &cfg,
		db:       db,
		history:  agent.NewHistory(8192),
		rollback: &agent.Rollback{},
	}
	run := startTaskRun("prompt", "Builder", "mock", "mock-model")
	run.Tools = []TaskToolEvent{
		{Name: "read", Status: "done"},
		{Name: "grep", Status: "done"},
		{Name: "read", Status: "done"},
	}

	app.maybeCheckpoint(run, cfg, 4)
	if checkpoints, err := app.ListResumableRuns(); err != nil || len(checkpoints) != 0 {
		t.Fatalf("checkpoint before throttle boundary = %#v err=%v", checkpoints, err)
	}

	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done"})
	app.maybeCheckpoint(run, cfg, 4)
	checkpoints, err := app.ListResumableRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 {
		t.Fatalf("checkpoint count at boundary = %d, want 1", len(checkpoints))
	}
}

func TestDeleteRunCheckpoint(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := settings.DefaultSettings()
	app := &App{
		cfg:      &cfg,
		db:       db,
		history:  agent.NewHistory(8192),
		rollback: &agent.Rollback{},
	}
	run := startTaskRun("prompt", "Builder", "mock", "mock-model")

	app.saveRunCheckpoint(run, cfg)
	app.deleteRunCheckpoint(run.ID)

	checkpoints, err := app.ListResumableRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 0 {
		t.Fatalf("checkpoint should be deleted: %#v", checkpoints)
	}
}
