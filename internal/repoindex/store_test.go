package repoindex

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	maulerstore "mauler/internal/store"
)

func TestStoreIndexSearchAndImmutableGenerationReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "service.go")
	writeFixture(t, path, []byte("package service\n// SentinelAlpha documents the first generation.\n"))
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy(root)
	first, err := index.Index(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	active, err := index.ActiveGeneration(context.Background(), policy)
	if err != nil || active.ID != first.GenerationID || !active.Complete {
		t.Fatalf("first active = %#v err=%v", active, err)
	}
	hits, err := index.Search(context.Background(), first.GenerationID, "SentinelAlpha", 10)
	if err != nil || len(hits) != 1 || hits[0].Path != "service.go" || hits[0].FileSHA256 == "" || hits[0].TrustLabel != TrustLabel {
		t.Fatalf("first search = %#v err=%v", hits, err)
	}

	writeFixture(t, path, []byte("package service\n// SentinelBeta documents the replacement generation.\n"))
	second, err := index.Index(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if second.GenerationID == first.GenerationID || second.Manifest.Digest == first.Manifest.Digest {
		t.Fatalf("replacement did not create fresh immutable generation: %#v %#v", first, second)
	}
	active, err = index.ActiveGeneration(context.Background(), policy)
	if err != nil || active.ID != second.GenerationID {
		t.Fatalf("replacement active = %#v err=%v", active, err)
	}
	oldHits, err := index.Search(context.Background(), first.GenerationID, "SentinelAlpha", 10)
	if err != nil || len(oldHits) != 1 {
		t.Fatalf("old generation lost = %#v err=%v", oldHits, err)
	}
	newHits, err := index.Search(context.Background(), second.GenerationID, "SentinelBeta", 10)
	if err != nil || len(newHits) != 1 || newHits[0].FileSHA256 == oldHits[0].FileSHA256 {
		t.Fatalf("new generation search = %#v err=%v", newHits, err)
	}
}

func TestIncrementalRefreshReusesVerifiedChunksAndReplacesChangedFiles(t *testing.T) {
	root := t.TempDir()
	stable := filepath.Join(root, "stable.txt")
	changed := filepath.Join(root, "changed.txt")
	writeFixture(t, stable, []byte("StableNeedle\n"))
	writeFixture(t, changed, []byte("AlphaNeedle\n"))
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	policy := DefaultPolicy(root)
	first, err := index.Index(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}

	second, err := index.Refresh(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if second.GenerationID == first.GenerationID || second.Manifest.RefreshMode != "incremental" || second.Manifest.FilesReused != 2 || second.Manifest.FilesChanged != 0 || second.Manifest.FilesDeleted != 0 {
		t.Fatalf("unchanged refresh = %#v", second)
	}
	if second.Manifest.Digest != first.Manifest.Digest {
		t.Fatalf("unchanged content changed manifest digest: %s != %s", second.Manifest.Digest, first.Manifest.Digest)
	}
	hits, err := index.Search(context.Background(), second.GenerationID, "StableNeedle", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("reused search = %#v err=%v", hits, err)
	}

	info, err := os.Stat(changed)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, changed, []byte("OmegaNeedle\n")) // same byte length
	if err := os.Chtimes(changed, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	third, err := index.Refresh(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if third.Manifest.FilesReused != 1 || third.Manifest.FilesChanged != 1 || third.Manifest.FilesDeleted != 0 {
		t.Fatalf("changed refresh = %#v", third.Manifest)
	}
	oldHits, _ := index.Search(context.Background(), third.GenerationID, "AlphaNeedle", 5)
	newHits, _ := index.Search(context.Background(), third.GenerationID, "OmegaNeedle", 5)
	if len(oldHits) != 0 || len(newHits) != 1 {
		t.Fatalf("changed content old=%#v new=%#v", oldHits, newHits)
	}
	oldGenerationHits, _ := index.Search(context.Background(), first.GenerationID, "AlphaNeedle", 5)
	if len(oldGenerationHits) != 1 {
		t.Fatalf("prior immutable generation lost: %#v", oldGenerationHits)
	}
}

func TestIncrementalRefreshTracksDeletionAndWatchMetadata(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	remove := filepath.Join(root, "remove.txt")
	writeFixture(t, keep, []byte("keep\n"))
	writeFixture(t, remove, []byte("remove\n"))
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	policy := DefaultPolicy(root)
	if _, err := index.Index(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if changed, err := index.HasChanges(context.Background(), policy); err != nil || changed {
		t.Fatalf("unchanged metadata changed=%t err=%v", changed, err)
	}
	if err := os.Remove(remove); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, "new.txt"), []byte("new\n"))
	if changed, err := index.HasChanges(context.Background(), policy); err != nil || !changed {
		t.Fatalf("changed metadata changed=%t err=%v", changed, err)
	}
	result, err := index.Refresh(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.FilesReused != 1 || result.Manifest.FilesChanged != 1 || result.Manifest.FilesDeleted != 1 {
		t.Fatalf("deletion refresh = %#v", result.Manifest)
	}
	if changed, err := index.HasChanges(context.Background(), policy); err != nil || changed {
		t.Fatalf("post-refresh metadata changed=%t err=%v", changed, err)
	}
}

func TestFailedReplacementDoesNotDisplaceActiveGeneration(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "notes.txt"), []byte("stable active content\n"))
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	policy := DefaultPolicy(root)
	first, err := index.Index(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failed, err := index.Index(ctx, policy)
	if !errors.Is(err, context.Canceled) || failed.GenerationID == "" {
		t.Fatalf("cancelled replacement = %#v err=%v", failed, err)
	}
	active, err := index.ActiveGeneration(context.Background(), policy)
	if err != nil || active.ID != first.GenerationID {
		t.Fatalf("cancel displaced active generation: %#v err=%v", active, err)
	}
	failedStatus, err := index.Generation(context.Background(), failed.GenerationID)
	if err != nil || failedStatus.Status != "cancelled" || failedStatus.Complete {
		t.Fatalf("failed status = %#v err=%v", failedStatus, err)
	}
}

func TestProgressCancellationDoesNotDisplaceActiveGeneration(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "notes.txt"), []byte("stable active content\n"))
	db, err := maulerstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	policy := DefaultPolicy(root)
	first, err := index.Index(context.Background(), policy)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, "replacement.txt"), []byte("replacement content\n"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callbackCount := 0
	failed, err := index.IndexWithProgress(ctx, policy, func(progress Progress) {
		callbackCount++
		if progress.FilesSeen > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || callbackCount == 0 || failed.Manifest.Complete {
		t.Fatalf("progress cancellation = callbacks %d result %#v err=%v", callbackCount, failed, err)
	}
	active, err := index.ActiveGeneration(context.Background(), policy)
	if err != nil || active.ID != first.GenerationID {
		t.Fatalf("progress cancellation displaced active generation: %#v err=%v", active, err)
	}
}

func TestStoreRequiresMigratedSchema(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bare.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index, _ := NewStore(db)
	_, err = index.Index(context.Background(), DefaultPolicy(t.TempDir()))
	if err == nil {
		t.Fatal("bare database unexpectedly accepted repository index")
	}
}

func TestFTSQueryTreatsInputAsTerms(t *testing.T) {
	if got := ftsQuery(`alpha OR beta* "gamma"`); got != `"alpha" AND "OR" AND "beta" AND "gamma"` {
		t.Fatalf("fts query = %q", got)
	}
}
