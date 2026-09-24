package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"mauler/internal/repoindex"
	"mauler/internal/settings"
)

const repositoryIndexWatchInterval = 4 * time.Second

// SetRepositoryIndexWatch persists watch mode for the current workspace. The
// watcher polls metadata cheaply; any detected change is then SHA-verified by
// the incremental replacement path before chunks are reused.
func (a *App) SetRepositoryIndexWatch(enabled bool) (RepositoryIndexStatus, error) {
	workspace := filepath.ToSlash(filepath.Clean(a.GetWorkingDir()))
	a.mu.Lock()
	before := append([]settings.RepositoryIndexSourceSet(nil), a.cfg.Context.RepositoryIndexSourceSets...)
	sets := make([]settings.RepositoryIndexSourceSet, 0, len(before)+1)
	var sources []settings.RepositoryIndexSource
	for _, set := range before {
		if sameFilesystemPath(set.Workspace, workspace) {
			sources = append([]settings.RepositoryIndexSource(nil), set.Sources...)
			continue
		}
		sets = append(sets, set)
	}
	if enabled || len(sources) > 0 {
		sets = append(sets, settings.RepositoryIndexSourceSet{Workspace: workspace, Sources: sources, Watch: enabled})
	}
	a.cfg.Context.RepositoryIndexSourceSets = sets
	cfg := *a.cfg
	a.mu.Unlock()
	if err := settings.Save(&cfg); err != nil {
		a.mu.Lock()
		a.cfg.Context.RepositoryIndexSourceSets = before
		a.mu.Unlock()
		return RepositoryIndexStatus{}, fmt.Errorf("save repository watch mode: %w", err)
	}
	a.restartRepositoryIndexWatcher()
	return a.repositoryIndexStatus(appOperationContext(a))
}

func (a *App) restartRepositoryIndexWatcher() {
	workspace := filepath.ToSlash(filepath.Clean(a.GetWorkingDir()))
	enabled := a.repositoryIndexWatchEnabledFor(workspace)
	a.repositoryIndexMu.Lock()
	if a.repositoryWatchCancel != nil {
		a.repositoryWatchCancel()
	}
	a.repositoryWatchID++
	id := a.repositoryWatchID
	a.repositoryWatchRoot = workspace
	a.repositoryWatchError = ""
	a.repositoryWatchChecked = time.Time{}
	if !enabled || a.ctx == nil {
		a.repositoryWatchCancel = nil
		a.repositoryWatchDone = nil
		a.repositoryWatchState = "off"
		a.repositoryIndexMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.repositoryWatchCancel = cancel
	a.repositoryWatchDone = done
	a.repositoryWatchState = "watching"
	a.repositoryIndexMu.Unlock()
	go a.runRepositoryIndexWatcher(ctx, id, workspace, done)
}

func (a *App) runRepositoryIndexWatcher(ctx context.Context, id uint64, workspace string, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(repositoryIndexWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !sameFilesystemPath(a.GetWorkingDir(), workspace) {
				return
			}
			a.repositoryIndexMu.Lock()
			indexing := a.repositoryIndexRuntime.Indexing
			a.repositoryIndexMu.Unlock()
			if indexing {
				a.setRepositoryWatchState(id, "indexing", "", true)
				continue
			}
			policy, _ := a.repositoryIndexPolicy(workspace)
			index, err := repoindex.NewStore(a.db)
			if err != nil {
				a.setRepositoryWatchState(id, "error", err.Error(), true)
				continue
			}
			changed, err := index.HasChanges(ctx, policy)
			if errors.Is(err, sql.ErrNoRows) {
				a.setRepositoryWatchState(id, "awaiting_index", "", true)
				continue
			}
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				a.setRepositoryWatchState(id, "error", err.Error(), true)
				continue
			}
			if !changed {
				a.setRepositoryWatchState(id, "watching", "", true)
				continue
			}
			a.setRepositoryWatchState(id, "refreshing", "", true)
			if _, _, err := a.refreshWorkspaceRepository(ctx, nil); err != nil {
				if ctx.Err() != nil {
					return
				}
				a.setRepositoryWatchState(id, "error", err.Error(), true)
				continue
			}
			a.setRepositoryWatchState(id, "watching", "", true)
			if a.ctx != nil {
				a.emit("mauler:repository_index_watch", workspace)
			}
		}
	}
}

func (a *App) setRepositoryWatchState(id uint64, state, detail string, checked bool) {
	a.repositoryIndexMu.Lock()
	defer a.repositoryIndexMu.Unlock()
	if a.repositoryWatchID != id {
		return
	}
	a.repositoryWatchState = state
	a.repositoryWatchError = detail
	if checked {
		a.repositoryWatchChecked = time.Now().UTC()
	}
}

func (a *App) applyRepositoryIndexWatchStatus(status *RepositoryIndexStatus, workspace string) {
	if status == nil {
		return
	}
	status.WatchEnabled = a.repositoryIndexWatchEnabledFor(workspace)
	a.repositoryIndexMu.Lock()
	defer a.repositoryIndexMu.Unlock()
	if !sameFilesystemPath(a.repositoryWatchRoot, workspace) {
		if status.WatchEnabled {
			status.WatchState = "starting"
		} else {
			status.WatchState = "off"
		}
		return
	}
	status.WatchState = a.repositoryWatchState
	status.WatchError = a.repositoryWatchError
	if !a.repositoryWatchChecked.IsZero() {
		status.WatchLastCheck = a.repositoryWatchChecked.Format(time.RFC3339Nano)
	}
}
