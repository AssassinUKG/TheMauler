package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/repoindex"
	"mauler/internal/settings"
)

const repositoryIndexUIOmissionLimit = 200

// RepositoryIndexOmission is an explicit non-indexed file or directory notice.
// It intentionally carries metadata only, never repository source text.
type RepositoryIndexOmission struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// RepositoryIndexStatus is the operator-facing truth for the active workspace
// generation. Active is true only for an immutable, fully committed scan.
type RepositoryIndexStatus struct {
	Available          bool                             `json:"available"`
	Active             bool                             `json:"active"`
	Workspace          string                           `json:"workspace"`
	GenerationID       string                           `json:"generation_id,omitempty"`
	ManifestDigest     string                           `json:"manifest_digest,omitempty"`
	PolicyDigest       string                           `json:"policy_digest,omitempty"`
	Status             string                           `json:"status"`
	Complete           bool                             `json:"complete"`
	FilesSeen          int                              `json:"files_seen"`
	FilesIndexed       int                              `json:"files_indexed"`
	BytesRead          int64                            `json:"bytes_read"`
	ChunkCount         int                              `json:"chunk_count"`
	OmissionCount      int                              `json:"omission_count"`
	OmissionsTruncated bool                             `json:"omissions_truncated"`
	Omissions          []RepositoryIndexOmission        `json:"omissions"`
	Error              string                           `json:"error,omitempty"`
	StartedAt          string                           `json:"started_at,omitempty"`
	CompletedAt        string                           `json:"completed_at,omitempty"`
	Indexing           bool                             `json:"indexing"`
	CanCancel          bool                             `json:"can_cancel"`
	CurrentPath        string                           `json:"current_path,omitempty"`
	ProgressFilesSeen  int                              `json:"progress_files_seen"`
	ProgressIndexed    int                              `json:"progress_files_indexed"`
	ProgressBytesRead  int64                            `json:"progress_bytes_read"`
	ProgressChunkCount int                              `json:"progress_chunk_count"`
	Sources            []settings.RepositoryIndexSource `json:"sources"`
	RefreshMode        string                           `json:"refresh_mode,omitempty"`
	FilesReused        int                              `json:"files_reused"`
	FilesChanged       int                              `json:"files_changed"`
	FilesDeleted       int                              `json:"files_deleted"`
	WatchEnabled       bool                             `json:"watch_enabled"`
	WatchState         string                           `json:"watch_state,omitempty"`
	WatchError         string                           `json:"watch_error,omitempty"`
	WatchLastCheck     string                           `json:"watch_last_check,omitempty"`
	Health             string                           `json:"health"`
	HealthDetail       string                           `json:"health_detail"`
}

func (a *App) repositoryIndexRuntimeStatus(workspace string) (RepositoryIndexStatus, bool) {
	if a == nil {
		return RepositoryIndexStatus{}, false
	}
	a.repositoryIndexMu.Lock()
	defer a.repositoryIndexMu.Unlock()
	status := a.repositoryIndexRuntime
	return status, status.Indexing && sameFilesystemPath(status.Workspace, workspace)
}

func (a *App) repositoryIndexStatus(ctx context.Context) (RepositoryIndexStatus, error) {
	workspace := ""
	if a != nil {
		workspace = filepath.ToSlash(filepath.Clean(a.GetWorkingDir()))
	}
	if runtimeStatus, ok := a.repositoryIndexRuntimeStatus(workspace); ok {
		a.applyRepositoryIndexWatchStatus(&runtimeStatus, workspace)
		applyRepositoryIndexHealth(&runtimeStatus)
		return runtimeStatus, nil
	}
	status, err := a.repositoryIndexStatusFromDatabase(ctx, workspace)
	a.applyRepositoryIndexWatchStatus(&status, workspace)
	applyRepositoryIndexHealth(&status)
	return status, err
}

func applyRepositoryIndexHealth(status *RepositoryIndexStatus) {
	if status == nil {
		return
	}
	switch {
	case !status.Available:
		status.Health = "unavailable"
		status.HealthDetail = firstNonEmpty(status.Error, "Repository index storage is unavailable.")
	case status.Indexing:
		status.Health = "indexing"
		if status.Active {
			status.HealthDetail = "Building a replacement generation; the previous complete generation remains searchable."
		} else {
			status.HealthDetail = "Building the first complete generation for this workspace."
		}
	case !status.Active:
		status.Health = "not_indexed"
		status.HealthDetail = "No complete generation exists for the current workspace and selected sources."
	case status.WatchState == "error":
		status.Health = "attention"
		status.HealthDetail = firstNonEmpty(status.WatchError, "The active generation is readable, but Watch needs attention.")
	case !status.Complete || strings.TrimSpace(status.Status) != "complete":
		status.Health = "attention"
		status.HealthDetail = "The active generation is not reported as complete."
	case status.OmissionCount > 0:
		status.Health = "attention"
		status.HealthDetail = fmt.Sprintf("Search is ready; review %d explicit non-indexed entr%s.", status.OmissionCount, plural(status.OmissionCount, "y", "ies"))
	default:
		status.Health = "healthy"
		status.HealthDetail = "The complete active generation is searchable and has no reported omissions."
	}
}

func appOperationContext(a *App) context.Context {
	if a != nil && a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// GetRepositoryIndexStatus reports the complete generation for the current
// authoritative workspace. It never falls back to a previous workspace.
func (a *App) GetRepositoryIndexStatus() (RepositoryIndexStatus, error) {
	return a.repositoryIndexStatus(appOperationContext(a))
}

// PreviewRepositorySplitReview returns a metadata-only view of deterministic,
// read-only review shards for the current active generation. Exact chunk IDs
// remain backend-owned until a bounded child contract is dispatched.
func (a *App) PreviewRepositorySplitReview(shardCount int) (repoindex.SplitReviewPreview, error) {
	if a == nil || a.db == nil {
		return repoindex.SplitReviewPreview{}, fmt.Errorf("repository index database is unavailable")
	}
	ctx := appOperationContext(a)
	workspace := filepath.ToSlash(filepath.Clean(a.GetWorkingDir()))
	if workspace == "." || strings.TrimSpace(workspace) == "" {
		return repoindex.SplitReviewPreview{}, fmt.Errorf("current workspace is unavailable")
	}
	policy, _ := a.repositoryIndexPolicy(workspace)
	index, err := repoindex.NewStore(a.db)
	if err != nil {
		return repoindex.SplitReviewPreview{}, err
	}
	active, err := index.ActiveGeneration(ctx, policy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repoindex.SplitReviewPreview{}, fmt.Errorf("index this workspace before preparing a split review")
		}
		return repoindex.SplitReviewPreview{}, fmt.Errorf("repository split review: %w", err)
	}
	plan, err := index.BuildSplitReviewPlan(ctx, active.ID, shardCount)
	if err != nil {
		return repoindex.SplitReviewPreview{}, fmt.Errorf("repository split review: %w", err)
	}
	return plan.Preview(), nil
}

// indexWorkspaceRepository owns scan concurrency, cancellation, progress, and
// immutable ledger evidence for both the UI and model-facing memory tool.
func (a *App) indexWorkspaceRepository(parent context.Context, progressSink repoindex.ProgressSink) (repoindex.IndexResult, string, error) {
	return a.runWorkspaceRepositoryIndex(parent, progressSink, false)
}

func (a *App) refreshWorkspaceRepository(parent context.Context, progressSink repoindex.ProgressSink) (repoindex.IndexResult, string, error) {
	return a.runWorkspaceRepositoryIndex(parent, progressSink, true)
}

func (a *App) runWorkspaceRepositoryIndex(parent context.Context, progressSink repoindex.ProgressSink, incremental bool) (repoindex.IndexResult, string, error) {
	if a == nil || a.db == nil {
		return repoindex.IndexResult{}, "", fmt.Errorf("repository index database is unavailable")
	}
	processStateMu.Lock()
	root := filepath.ToSlash(filepath.Clean(a.GetWorkingDir()))
	if root == "." || strings.TrimSpace(root) == "" {
		processStateMu.Unlock()
		return repoindex.IndexResult{}, "", fmt.Errorf("current workspace is unavailable")
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.repositoryReviewMu.Lock()
	reviewRunning := a.repositoryReviewRuntime.CanCancel
	a.repositoryReviewMu.Unlock()
	if reviewRunning {
		processStateMu.Unlock()
		cancel()
		return repoindex.IndexResult{}, root, fmt.Errorf("repository indexing cannot start while split review is active; cancel the review first")
	}

	a.repositoryIndexMu.Lock()
	if a.repositoryIndexRuntime.Indexing {
		activeWorkspace := a.repositoryIndexRuntime.Workspace
		a.repositoryIndexMu.Unlock()
		processStateMu.Unlock()
		cancel()
		return repoindex.IndexResult{}, root, fmt.Errorf("repository indexing is already active for %s", activeWorkspace)
	}
	a.repositoryIndexRunID++
	runID := a.repositoryIndexRunID
	a.repositoryIndexCancel = cancel
	a.repositoryIndexDone = make(chan struct{})
	policy, sources := a.repositoryIndexPolicy(root)
	refreshMode := "full"
	if incremental {
		refreshMode = "incremental"
	}
	a.repositoryIndexRuntime = RepositoryIndexStatus{
		Available: true, Workspace: root, Status: "starting", Indexing: true, CanCancel: true,
		Omissions: []RepositoryIndexOmission{}, Sources: sources, RefreshMode: refreshMode, StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	a.repositoryIndexMu.Unlock()
	processStateMu.Unlock()
	defer cancel()

	// Snapshot the previous active generation before opening the replacement
	// transaction. This preserves useful provenance while status polling stays
	// independent from SQLite's single-writer connection.
	if previous, err := a.repositoryIndexStatusFromDatabase(ctx, root); err == nil {
		a.repositoryIndexMu.Lock()
		if a.repositoryIndexRunID == runID && a.repositoryIndexRuntime.Indexing {
			startedAt := a.repositoryIndexRuntime.StartedAt
			a.repositoryIndexRuntime = previous
			a.repositoryIndexRuntime.Indexing = true
			a.repositoryIndexRuntime.CanCancel = true
			a.repositoryIndexRuntime.Status = "scanning"
			a.repositoryIndexRuntime.StartedAt = startedAt
		}
		a.repositoryIndexMu.Unlock()
	}

	index, err := repoindex.NewStore(a.db)
	if err != nil {
		a.finishRepositoryIndexRun(runID, err)
		return repoindex.IndexResult{}, root, err
	}
	started := time.Now()
	startKind, startMessage := "repo_index_start", "Indexing operator-selected workspace knowledge"
	if incremental {
		startKind, startMessage = "repo_index_refresh_start", "Refreshing changed workspace knowledge"
	}
	a.recordLedger(ledger.Event{Kind: startKind, Source: "memory", Tool: "memory", Status: "running", Message: startMessage, Files: append([]string(nil), policy.Roots...)})
	progress := func(progress repoindex.Progress) {
		a.repositoryIndexMu.Lock()
		if a.repositoryIndexRunID == runID && a.repositoryIndexRuntime.Indexing {
			a.repositoryIndexRuntime.Status = "scanning"
			a.repositoryIndexRuntime.CurrentPath = progress.CurrentPath
			a.repositoryIndexRuntime.ProgressFilesSeen = progress.FilesSeen
			a.repositoryIndexRuntime.ProgressIndexed = progress.FilesIndexed
			a.repositoryIndexRuntime.ProgressBytesRead = progress.BytesRead
			a.repositoryIndexRuntime.ProgressChunkCount = progress.Chunks
		}
		a.repositoryIndexMu.Unlock()
		if progressSink != nil {
			progressSink(progress)
		}
	}
	var result repoindex.IndexResult
	var indexErr error
	if incremental {
		result, indexErr = index.RefreshWithProgress(ctx, policy, progress)
	} else {
		result, indexErr = index.IndexWithProgress(ctx, policy, progress)
	}
	if indexErr != nil {
		ledgerStatus := "failed"
		if errors.Is(indexErr, context.Canceled) || errors.Is(indexErr, context.DeadlineExceeded) {
			ledgerStatus = "cancelled"
		}
		failureKind := "repo_index_failed"
		if incremental {
			failureKind = "repo_index_refresh_failed"
		}
		a.recordLedger(ledger.Event{
			Kind: failureKind, Source: "memory", Tool: "memory", Status: ledgerStatus,
			Message: "Repository index was not activated", Error: indexErr.Error(), DurationMs: time.Since(started).Milliseconds(),
			Files: []string{root}, Metadata: repoIndexMetadata(result.GenerationID, result.Manifest),
		})
		a.finishRepositoryIndexRun(runID, indexErr)
		return result, root, indexErr
	}
	completeKind := "repo_index_complete"
	if incremental {
		completeKind = "repo_index_refresh_complete"
	}
	a.recordLedger(ledger.Event{
		Kind: completeKind, Source: "memory", Tool: "memory", Status: "ok",
		Message: "Activated immutable repository index generation", DurationMs: time.Since(started).Milliseconds(),
		Files: []string{root}, Artifacts: []string{"repo-index://" + result.GenerationID}, Metadata: repoIndexMetadata(result.GenerationID, result.Manifest),
	})
	a.finishRepositoryIndexRun(runID, nil)
	return result, root, nil
}

func (a *App) finishRepositoryIndexRun(runID uint64, cause error) {
	a.repositoryIndexMu.Lock()
	defer a.repositoryIndexMu.Unlock()
	if a.repositoryIndexRunID != runID {
		return
	}
	status := "complete"
	if cause != nil {
		status = "failed"
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			status = "cancelled"
		}
		a.repositoryIndexRuntime.Error = cause.Error()
	}
	a.repositoryIndexRuntime.Status = status
	a.repositoryIndexRuntime.Indexing = false
	a.repositoryIndexRuntime.CanCancel = false
	a.repositoryIndexRuntime.CurrentPath = ""
	a.repositoryIndexCancel = nil
	if a.repositoryIndexDone != nil {
		close(a.repositoryIndexDone)
		a.repositoryIndexDone = nil
	}
}

func (a *App) repositoryIndexStatusFromDatabase(ctx context.Context, workspace string) (RepositoryIndexStatus, error) {
	policy, sources := a.repositoryIndexPolicy(workspace)
	base := RepositoryIndexStatus{Available: a != nil && a.db != nil, Workspace: workspace, Status: "not_indexed", Omissions: []RepositoryIndexOmission{}, Sources: sources}
	if !base.Available {
		base.Status = "unavailable"
		base.Error = "repository index database is unavailable"
		return base, nil
	}
	index, err := repoindex.NewStore(a.db)
	if err != nil {
		return base, err
	}
	generation, err := index.ActiveGeneration(ctx, policy)
	if err != nil {
		if err == sql.ErrNoRows {
			return base, nil
		}
		return base, fmt.Errorf("repository index status: %w", err)
	}
	manifest, err := index.Manifest(ctx, generation.ID)
	if err != nil {
		return base, fmt.Errorf("repository index manifest: %w", err)
	}
	base.Active = generation.Complete && generation.Status == "complete" && manifest.Complete
	base.GenerationID = generation.ID
	base.ManifestDigest = generation.ManifestDigest
	base.PolicyDigest = generation.PolicyDigest
	base.Status = generation.Status
	base.Complete = generation.Complete && manifest.Complete
	base.FilesSeen = generation.FilesSeen
	base.FilesIndexed = generation.FilesIndexed
	base.BytesRead = generation.BytesRead
	base.ChunkCount = generation.ChunkCount
	base.Error = generation.Error
	base.StartedAt = generation.StartedAt
	base.CompletedAt = generation.CompletedAt
	base.RefreshMode = manifest.RefreshMode
	base.FilesReused = manifest.FilesReused
	base.FilesChanged = manifest.FilesChanged
	base.FilesDeleted = manifest.FilesDeleted
	for _, entry := range manifest.Entries {
		if entry.Status == repoindex.StatusIndexed {
			continue
		}
		base.OmissionCount++
		if len(base.Omissions) < repositoryIndexUIOmissionLimit {
			base.Omissions = append(base.Omissions, RepositoryIndexOmission{Path: entry.Path, Status: entry.Status, Detail: entry.Detail, Size: entry.Size})
		}
	}
	for _, notice := range manifest.Notices {
		base.OmissionCount++
		if len(base.Omissions) < repositoryIndexUIOmissionLimit {
			base.Omissions = append(base.Omissions, RepositoryIndexOmission{Path: notice.Path, Status: notice.Status, Detail: notice.Detail})
		}
	}
	base.OmissionsTruncated = base.OmissionCount > len(base.Omissions)
	return base, nil
}

// IndexWorkspaceRepository streams and atomically activates a new immutable
// generation for the current authoritative workspace.
func (a *App) IndexWorkspaceRepository() (RepositoryIndexStatus, error) {
	ctx := appOperationContext(a)
	if _, _, err := a.indexWorkspaceRepository(ctx, nil); err != nil {
		status, statusErr := a.repositoryIndexStatus(context.Background())
		if statusErr == nil {
			status.Error = err.Error()
			return status, err
		}
		return RepositoryIndexStatus{}, err
	}
	return a.repositoryIndexStatus(ctx)
}

// RefreshWorkspaceRepositoryIndex verifies hashes and transactionally replaces
// only changed/deleted content while reusing exact prior chunks.
func (a *App) RefreshWorkspaceRepositoryIndex() (RepositoryIndexStatus, error) {
	ctx := appOperationContext(a)
	if _, _, err := a.refreshWorkspaceRepository(ctx, nil); err != nil {
		status, statusErr := a.repositoryIndexStatus(context.Background())
		if statusErr == nil {
			status.Error = err.Error()
			return status, err
		}
		return RepositoryIndexStatus{}, err
	}
	return a.repositoryIndexStatus(ctx)
}

// CancelWorkspaceRepositoryIndex requests cancellation without waiting for
// rollback/failed-generation bookkeeping to finish.
func (a *App) CancelWorkspaceRepositoryIndex() (RepositoryIndexStatus, error) {
	workspace := filepath.ToSlash(filepath.Clean(a.GetWorkingDir()))
	a.repositoryIndexMu.Lock()
	if !a.repositoryIndexRuntime.Indexing || !sameFilesystemPath(a.repositoryIndexRuntime.Workspace, workspace) {
		a.repositoryIndexMu.Unlock()
		return a.repositoryIndexStatus(appOperationContext(a))
	}
	cancel := a.repositoryIndexCancel
	a.repositoryIndexRuntime.Status = "cancelling"
	a.repositoryIndexRuntime.CanCancel = false
	status := a.repositoryIndexRuntime
	a.repositoryIndexMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.applyRepositoryIndexWatchStatus(&status, workspace)
	applyRepositoryIndexHealth(&status)
	return status, nil
}

func repositoryIndexStatusLabel(status RepositoryIndexStatus) string {
	if !status.Available {
		return "unavailable"
	}
	if status.Indexing {
		return "indexing"
	}
	if !status.Active {
		return "not indexed"
	}
	return strings.TrimSpace(status.Status) + " (" + strconv.Itoa(status.FilesIndexed) + " files)"
}
