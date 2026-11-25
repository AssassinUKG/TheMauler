package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mauler/internal/settings"
	"mauler/internal/store"
)

func TestCancelWorkspaceRepositoryIndexSignalsActiveRun(t *testing.T) {
	workspace, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	cancelled := false
	a := &App{}
	a.repositoryIndexRuntime = RepositoryIndexStatus{Available: true, Workspace: filepath.ToSlash(workspace), Status: "scanning", Indexing: true, CanCancel: true}
	a.repositoryIndexCancel = func() { cancelled = true }
	status, err := a.CancelWorkspaceRepositoryIndex()
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled || !status.Indexing || status.CanCancel || status.Status != "cancelling" {
		t.Fatalf("cancel status = %#v signalled=%t", status, cancelled)
	}
}

func TestRepositoryIndexRuntimeStatusDoesNotQueryDatabase(t *testing.T) {
	workspace, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{repositoryIndexRuntime: RepositoryIndexStatus{
		Available: true, Workspace: filepath.ToSlash(workspace), Status: "scanning", Indexing: true,
		ProgressFilesSeen: 12, ProgressIndexed: 9,
	}}
	status, err := a.repositoryIndexStatus(context.Background())
	if err != nil || !status.Indexing || status.ProgressFilesSeen != 12 || status.ProgressIndexed != 9 {
		t.Fatalf("runtime status = %#v err=%v", status, err)
	}
}

func TestRepositoryIndexStartingStatusPreservesCommittedGeneration(t *testing.T) {
	startedAt := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	previous := RepositoryIndexStatus{
		Available: true, Active: true, Complete: true, Workspace: "C:/workspace",
		GenerationID: "generation-1", ManifestDigest: "manifest-1", PolicyDigest: "policy-1",
		Status: "complete", FilesSeen: 2, FilesIndexed: 2, ChunkCount: 2, RefreshMode: "full",
	}

	status := repositoryIndexStartingStatus(&previous, "C:/workspace", nil, "incremental", startedAt)
	if !status.Active || !status.Complete || status.GenerationID != previous.GenerationID {
		t.Fatalf("replacement lost committed generation: %#v", status)
	}
	if !status.Indexing || !status.CanCancel || status.Status != "starting" || status.RefreshMode != "incremental" {
		t.Fatalf("replacement run status = %#v", status)
	}
	if status.StartedAt != startedAt.Format(time.RFC3339Nano) {
		t.Fatalf("started_at = %q", status.StartedAt)
	}
}

func TestRepositoryIndexWatcherActivatesIncrementalGeneration(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	restoreWorkingDir(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "initial.txt"), []byte("InitialNeedle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := settings.DefaultSettings()
	a := &App{ctx: ctx, cfg: &cfg, db: db, suppressEvents: true}
	initial, err := a.IndexWorkspaceRepository()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetRepositoryIndexWatch(true); err != nil {
		t.Fatal(err)
	}
	defer func() {
		a.repositoryIndexMu.Lock()
		stop, done := a.repositoryWatchCancel, a.repositoryWatchDone
		a.repositoryIndexMu.Unlock()
		if stop != nil {
			stop()
		}
		if done != nil {
			<-done
		}
	}()
	if err := os.WriteFile(filepath.Join(workspace, "added.txt"), []byte("WatcherNeedle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3*repositoryIndexWatchInterval + 3*time.Second)
	var refreshed RepositoryIndexStatus
	for time.Now().Before(deadline) {
		refreshed, err = a.GetRepositoryIndexStatus()
		if err == nil && refreshed.Active && refreshed.Complete && !refreshed.Indexing &&
			refreshed.GenerationID != "" && refreshed.GenerationID != initial.GenerationID &&
			refreshed.RefreshMode == "incremental" && refreshed.WatchState == "watching" && refreshed.WatchLastCheck != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.GenerationID == initial.GenerationID || refreshed.FilesChanged != 1 || refreshed.FilesReused != 1 {
		t.Fatalf("watch refresh = %#v", refreshed)
	}
	if !refreshed.WatchEnabled || refreshed.WatchState != "watching" || refreshed.WatchLastCheck == "" {
		t.Fatalf("watch health = %#v", refreshed)
	}
}

func TestRepositoryIndexHealthClassification(t *testing.T) {
	tests := []struct {
		name   string
		status RepositoryIndexStatus
		want   string
	}{
		{name: "unavailable", status: RepositoryIndexStatus{}, want: "unavailable"},
		{name: "first index", status: RepositoryIndexStatus{Available: true, Indexing: true}, want: "indexing"},
		{name: "not indexed", status: RepositoryIndexStatus{Available: true}, want: "not_indexed"},
		{name: "watch error", status: RepositoryIndexStatus{Available: true, Active: true, Complete: true, Status: "complete", WatchState: "error", WatchError: "walk failed"}, want: "attention"},
		{name: "omissions", status: RepositoryIndexStatus{Available: true, Active: true, Complete: true, Status: "complete", OmissionCount: 2}, want: "attention"},
		{name: "healthy", status: RepositoryIndexStatus{Available: true, Active: true, Complete: true, Status: "complete"}, want: "healthy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			applyRepositoryIndexHealth(&test.status)
			if test.status.Health != test.want || test.status.HealthDetail == "" {
				t.Fatalf("health = %#v", test.status)
			}
		})
	}
}
