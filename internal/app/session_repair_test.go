package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestInspectAndApplySessionRepairKeepsBackupOutOfSessionList(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	app := &App{cfg: &cfg, profiles: &profiles, history: agent.NewHistory(8192), rollback: &agent.Rollback{}}
	name := "damaged-session"
	path := writeSavedSessionFixture(t, name, []llm.Message{
		llm.NewTextMessage(llm.RoleAssistant, "stale leading answer"),
		llm.NewTextMessage(llm.RoleUser, "actual task"),
		llm.NewTextMessage(llm.RoleUser, "more detail"),
	})
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	preview, err := app.InspectSessionRepair(name)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Valid || preview.Status != "repaired" || len(preview.Actions) != 2 || preview.Applied {
		t.Fatalf("preview = %#v", preview)
	}
	afterPreview, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterPreview) != string(original) {
		t.Fatal("repair preview changed saved session")
	}

	applied, err := app.RepairSession(name)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.BackupPath == "" {
		t.Fatalf("apply result = %#v", applied)
	}
	if _, err := os.Stat(applied.BackupPath); err != nil {
		t.Fatalf("repair backup missing: %v", err)
	}
	names, err := app.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != name {
		t.Fatalf("backup leaked into session list: %#v", names)
	}

	var repaired []llm.Message
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &repaired); err != nil {
		t.Fatal(err)
	}
	if len(repaired) != 1 || repaired[0].Role != llm.RoleUser || !strings.Contains(repaired[0].Content.(string), "more detail") {
		t.Fatalf("saved repair = %#v", repaired)
	}
}

func TestLoadSessionUsesSafeRepairedTranscriptWithoutRewritingFile(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{history: agent.NewHistory(8192), rollback: &agent.Rollback{}}
	path := writeSavedSessionFixture(t, "load-repair", []llm.Message{
		llm.NewTextMessage(llm.RoleAssistant, "stale"),
		llm.NewTextMessage(llm.RoleUser, "task"),
	})
	original, _ := os.ReadFile(path)

	loaded, err := app.LoadSession("load-repair")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Role != llm.RoleUser || loaded[0].Content != "task" {
		t.Fatalf("loaded repaired session = %#v", loaded)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("LoadSession should not rewrite the saved transcript")
	}
}

func TestRepairSessionRejectsActiveRun(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{agentRunning: true}
	if _, err := app.RepairSession("anything"); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("active repair error = %v", err)
	}
}

func writeSavedSessionFixture(t *testing.T, name string, messages []llm.Message) string {
	t.Helper()
	dir, err := sessionsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}
