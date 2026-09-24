package app

import (
	"os"
	"path/filepath"
	"testing"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

func TestScratchWorkspacePreservesConversationAndPromotesWithoutMovingFiles(t *testing.T) {
	configDir := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("MAULER_CONFIG_DIR", configDir)
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}

	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(workspace)
	profiles := settings.DefaultProfiles()
	history := agent.NewHistory(4096)
	history.Append(llm.NewTextMessage(llm.RoleUser, "keep this conversation"))
	app := &App{
		cfg: cfgPtr(cfg), profiles: &profiles, history: history,
		rollback: &agent.Rollback{}, browserWorkflowOwner: newBrowserWorkflowOwner(),
	}

	created, err := app.CreateScratchWorkspace("API investigation")
	if err != nil {
		t.Fatal(err)
	}
	if !created.Active || !created.Exists || !created.PromotionEligible || created.ReviewAfterUnix <= created.CreatedUnix {
		t.Fatalf("unexpected scratch status: %#v", created)
	}
	if got := app.history.Messages(); len(got) != 1 || messageText(got[0]) != "keep this conversation" {
		t.Fatalf("conversation changed while attaching scratch: %#v", got)
	}
	if _, err := os.Stat(scratchWorkspaceMarkerPath(created.Path)); err != nil {
		t.Fatalf("scratch marker missing: %v", err)
	}

	artifact := filepath.Join(filepath.FromSlash(created.Path), "evidence.txt")
	if err := os.WriteFile(artifact, []byte("preserve me"), 0o640); err != nil {
		t.Fatal(err)
	}
	promoted, err := app.PromoteScratchWorkspace("API evidence")
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Path != created.Path || !promoted.Exists || promoted.Active {
		t.Fatalf("unexpected promoted status: %#v", promoted)
	}
	if data, err := os.ReadFile(artifact); err != nil || string(data) != "preserve me" {
		t.Fatalf("promotion moved or changed evidence: %q %v", data, err)
	}
	if app.cfg.Context.ScratchWorkspaceDir != "" || app.cfg.Context.ActiveLabProfile == "" {
		t.Fatalf("scratch metadata was not promoted: %#v", app.cfg.Context)
	}
	found := false
	for _, profile := range app.cfg.Context.LabProfiles {
		if profile.ID == app.cfg.Context.ActiveLabProfile && profile.WorkspaceDir == created.Path {
			found = true
		}
	}
	if !found {
		t.Fatal("promoted workspace profile not found")
	}
}

func cfgPtr(cfg settings.Settings) *settings.Settings { return &cfg }
