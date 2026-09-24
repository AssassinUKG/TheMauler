package app

import (
	"os"
	"path/filepath"
	"testing"

	"mauler/internal/settings"
)

func TestRepositoryIndexSourcesAreWorkspaceScopedAndReadOnly(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	workspace := t.TempDir()
	extra := t.TempDir()
	file := filepath.Join(extra, "evidence.md")
	if err := os.WriteFile(file, []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	app := &App{cfg: &cfg, suppressEvents: true}
	if err := app.addRepositoryIndexSources([]string{extra, file, workspace}); err != nil {
		t.Fatal(err)
	}
	sources := app.GetRepositoryIndexSources()
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
	policy, _ := app.repositoryIndexPolicy(workspace)
	if len(policy.Roots) != 3 {
		t.Fatalf("roots = %#v", policy.Roots)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("selection mutated source: %v", err)
	}
	if err := app.updateRepositoryIndexSources([]string{filepath.ToSlash(file)}, false); err != nil {
		t.Fatal(err)
	}
	if got := app.GetRepositoryIndexSources(); len(got) != 1 || got[0].Kind != "folder" {
		t.Fatalf("sources after removal = %#v", got)
	}
}
