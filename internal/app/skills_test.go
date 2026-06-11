package app

import (
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestRelevantSkillsDoesNotAutoInjectMasterUnlessExplicitlyRequested(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "master_skill.md")
	mustWrite(t, source, "unique navigator workflow instructions")
	if _, _, err := saveMasterSkillSource(source); err != nil {
		t.Fatal(err)
	}
	cfg := settings.SkillsConfig{Enabled: true, AutoInject: true, MaxInject: 3}

	if got := relevantSkills(cfg, "please follow the navigator workflow"); len(got) != 0 {
		t.Fatalf("master skill should not auto-inject unless explicitly requested: %#v", got)
	}
	got := relevantSkills(cfg, "use the master skill for this task")
	if len(got) != 1 || got[0].Name != "master" || strings.Contains(got[0].Body, "unique navigator workflow instructions") {
		t.Fatalf("expected explicit master skill request to inject only lazy metadata, got: %#v", got)
	}
	if strings.Contains(got[0].Body, filepath.ToSlash(filepath.Dir(source))) {
		t.Fatalf("master skill lazy body should not duplicate absolute source path: %#v", got[0])
	}
	if got[0].SourcePath == "" {
		t.Fatal("master skill should keep source path internally for lazy loading")
	}
}
