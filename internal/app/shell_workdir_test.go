package app

import (
	"path/filepath"
	"testing"
)

func TestExistingShellWorkingDirFallsBackWhenSavedWorkspaceWasDeleted(t *testing.T) {
	fallback := t.TempDir()
	missing := filepath.Join(t.TempDir(), "deleted-workspace")

	if got := existingShellWorkingDir(missing, fallback); got != fallback {
		t.Fatalf("working dir = %q, want fallback %q", got, fallback)
	}
}

func TestExistingShellWorkingDirKeepsAvailableWorkspace(t *testing.T) {
	preferred := t.TempDir()
	fallback := t.TempDir()

	if got := existingShellWorkingDir(preferred, fallback); got != preferred {
		t.Fatalf("working dir = %q, want preferred %q", got, preferred)
	}
}
