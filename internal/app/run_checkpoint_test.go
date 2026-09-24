package app

import (
	"path/filepath"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/controlplane"
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
	contract, err := controlplane.NewTaskContract(controlplane.ContractInput{
		RunID: run.ID, Objective: run.Prompt, WorkspaceRoot: t.TempDir(),
		CreatedAt: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	control, err := controlplane.NewMachineState(contract, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	run.Contract, run.Control = &contract, &control

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
	if cp.Run.Contract == nil || cp.Run.Control == nil || cp.Run.Contract.Digest != contract.Digest || cp.Run.Control.Phase != controlplane.PhaseIntake {
		t.Fatalf("checkpoint control plane = %#v", cp.Run)
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

func TestResumedTaskRunCreatesNewGenerationLinkedToParent(t *testing.T) {
	parent := startTaskRun("continue the task", "Builder", "old-profile", "old-model")
	cp := RunCheckpoint{
		RunID:   parent.ID,
		Prompt:  parent.Prompt,
		Run:     parent,
		SavedAt: "2026-09-03T10:00:00Z",
	}
	resumed := resumedTaskRun(cp, "Fixer", "new-profile", "new-model")
	if resumed.ID == parent.ID || resumed.ParentRunID != parent.ID {
		t.Fatalf("resume ownership = child %q parent %q, original %q", resumed.ID, resumed.ParentRunID, parent.ID)
	}
	if resumed.Generation <= parent.Generation {
		t.Fatalf("resume generation = %d, want newer than %d", resumed.Generation, parent.Generation)
	}
	if resumed.Mode != "Fixer" || resumed.Profile != "new-profile" || resumed.Model != "new-model" {
		t.Fatalf("resume did not use selected runtime: %#v", resumed)
	}
}

func TestNamedConversationCheckpointRoundTripAndDelete(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg: &cfg, profiles: &profiles, db: db,
		history: agent.NewHistory(8192), rollback: &agent.Rollback{},
		currentMode: "Researcher", conversationMode: conversationModeAgent,
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "continue the authorised assessment"))
	app.history.Append(llm.NewTextMessage(llm.RoleAssistant, "Current evidence is recorded."))

	cp, err := app.SaveConversationCheckpoint("Before validation", "client-chat")
	if err != nil {
		t.Fatalf("SaveConversationCheckpoint: %v", err)
	}
	if !cp.Explicit || cp.Name != "Before validation" || cp.ConversationName != "client-chat" || cp.ConversationMode != conversationModeAgent {
		t.Fatalf("named checkpoint = %#v", cp)
	}
	if cp.Prompt != "continue the authorised assessment" || len(cp.ChatMessages) != 2 {
		t.Fatalf("checkpoint transcript = %#v", cp)
	}
	listed, err := app.ListResumableRuns()
	if err != nil || len(listed) != 1 || len(listed[0].ChatMessages) != 2 {
		t.Fatalf("listed checkpoints = %#v err=%v", listed, err)
	}
	if _, err := app.SaveConversationCheckpoint("before VALIDATION", "client-chat"); err == nil {
		t.Fatal("expected case-insensitive duplicate-name rejection")
	}
	if err := app.DeleteResumableRun(cp.RunID); err != nil {
		t.Fatalf("DeleteResumableRun: %v", err)
	}
	listed, err = app.ListResumableRuns()
	if err != nil || len(listed) != 0 {
		t.Fatalf("deleted checkpoints = %#v err=%v", listed, err)
	}
}

func TestNamedCheckpointResumeRemainsPersistent(t *testing.T) {
	parent := startTaskRun("continue", "Researcher", "profile", "model")
	cp := RunCheckpoint{RunID: parent.ID, Explicit: true, ConversationMode: conversationModeDirect, Prompt: parent.Prompt, Run: parent}
	resumed := resumedTaskRun(cp, "Researcher", "profile", "model")
	if !resumed.persistentCheckpoint {
		t.Fatal("named checkpoint resume must remain reusable")
	}
}
