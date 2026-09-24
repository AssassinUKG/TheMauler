package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

func TestCreateWorkspaceProjectAtCreatesRegistersAndSwitches(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	parent := t.TempDir()
	previous := t.TempDir()
	restoreWorkingDir(t)
	if err := os.Chdir(previous); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(previous)
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg: &cfg, profiles: &profiles, history: agent.NewHistory(4096),
		rollback: &agent.Rollback{}, registry: tools.New(), suppressEvents: true,
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "previous project context"))

	created, err := app.createWorkspaceProjectAt(parent, "Client Alpha")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.ToSlash(filepath.Join(parent, "Client Alpha"))
	if created != want || app.GetWorkingDir() != want {
		t.Fatalf("created=%q cwd=%q want=%q", created, app.GetWorkingDir(), want)
	}
	if info, err := os.Stat(filepath.FromSlash(created)); err != nil || !info.IsDir() {
		t.Fatalf("created project is unavailable: info=%#v err=%v", info, err)
	}
	if app.history.TokenCount() != 0 {
		t.Fatalf("previous project history survived switch: %d tokens", app.history.TokenCount())
	}
	if app.cfg.Context.ActiveLabProfile == "" || app.cfg.Context.Lab.Name != "Client Alpha" {
		t.Fatalf("active project not registered: %#v", app.cfg.Context)
	}
	found := false
	for _, profile := range app.cfg.Context.LabProfiles {
		if profile.ID == app.cfg.Context.ActiveLabProfile && profile.WorkspaceDir == want && profile.Name == "Client Alpha" {
			found = true
		}
	}
	if !found {
		t.Fatalf("durable project profile missing: %#v", app.cfg.Context.LabProfiles)
	}
}

func TestCreateWorkspaceProjectRejectsUnsafeOrExistingNames(t *testing.T) {
	for _, name := range []string{"", "..", `child/name`, `child\\name`, "CON", "trailing. ", strings.Repeat("x", 97)} {
		if _, err := validateWorkspaceProjectName(name); err == nil {
			t.Fatalf("unsafe name %q was accepted", name)
		}
	}
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "Existing"), 0o750); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	_, err := app.createWorkspaceProjectAt(parent, "Existing")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing folder error = %v", err)
	}
}
