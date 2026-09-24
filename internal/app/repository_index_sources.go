package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"mauler/internal/repoindex"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

// GetRepositoryIndexSources returns the explicit read-only corpus roots for
// the current workspace. The workspace itself is always implicit.
func (a *App) GetRepositoryIndexSources() []settings.RepositoryIndexSource {
	return a.repositoryIndexSourcesFor(a.GetWorkingDir())
}

// SelectRepositoryIndexFolder adds one operator-selected folder to the
// current workspace index policy without widening workspace/tool authority.
func (a *App) SelectRepositoryIndexFolder() (RepositoryIndexStatus, error) {
	if a.ctx == nil {
		return RepositoryIndexStatus{}, fmt.Errorf("app is not ready")
	}
	selected, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Add read-only folder to workspace knowledge", DefaultDirectory: a.GetWorkingDir(),
	})
	if err != nil {
		return RepositoryIndexStatus{}, err
	}
	if strings.TrimSpace(selected) == "" {
		return a.GetRepositoryIndexStatus()
	}
	if err := a.addRepositoryIndexSources([]string{selected}); err != nil {
		return RepositoryIndexStatus{}, err
	}
	return a.GetRepositoryIndexStatus()
}

// SelectRepositoryIndexFiles adds exact operator-selected files to the index.
func (a *App) SelectRepositoryIndexFiles() (RepositoryIndexStatus, error) {
	if a.ctx == nil {
		return RepositoryIndexStatus{}, fmt.Errorf("app is not ready")
	}
	selected, err := wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Add read-only files to workspace knowledge", DefaultDirectory: a.GetWorkingDir(),
		Filters: []wailsruntime.FileFilter{{DisplayName: "All files (*.*)", Pattern: "*.*"}},
	})
	if err != nil {
		return RepositoryIndexStatus{}, err
	}
	if len(selected) == 0 {
		return a.GetRepositoryIndexStatus()
	}
	if err := a.addRepositoryIndexSources(selected); err != nil {
		return RepositoryIndexStatus{}, err
	}
	return a.GetRepositoryIndexStatus()
}

func (a *App) RemoveRepositoryIndexSource(path string) (RepositoryIndexStatus, error) {
	if err := a.updateRepositoryIndexSources([]string{path}, false); err != nil {
		return RepositoryIndexStatus{}, err
	}
	return a.GetRepositoryIndexStatus()
}

func (a *App) addRepositoryIndexSources(paths []string) error {
	return a.updateRepositoryIndexSources(paths, true)
}

func (a *App) updateRepositoryIndexSources(paths []string, add bool) error {
	workspace := filepath.ToSlash(filepath.Clean(a.GetWorkingDir()))
	if runtimeStatus, active := a.repositoryIndexRuntimeStatus(workspace); active {
		return fmt.Errorf("cannot change index sources while repository indexing is %s; cancel the scan first", runtimeStatus.Status)
	}
	changes := make(map[string]settings.RepositoryIndexSource, len(paths))
	for _, raw := range paths {
		var path string
		var source settings.RepositoryIndexSource
		var err error
		if add {
			path, source, err = validateRepositoryIndexSource(raw)
		} else {
			path = filepath.ToSlash(filepath.Clean(tools.NormalizeHostPath(strings.TrimSpace(raw))))
			if path == "." || path == "" {
				err = fmt.Errorf("repository index source is empty")
			}
		}
		if err != nil {
			return err
		}
		if add && pathWithinRoot(tools.NormalizeHostPath(workspace), tools.NormalizeHostPath(path)) {
			continue
		}
		changes[strings.ToLower(path)] = source
	}
	if len(changes) == 0 {
		return nil
	}

	a.mu.Lock()
	before := append([]settings.RepositoryIndexSourceSet(nil), a.cfg.Context.RepositoryIndexSourceSets...)
	sets := make([]settings.RepositoryIndexSourceSet, 0, len(before)+1)
	current := map[string]settings.RepositoryIndexSource{}
	watch := false
	for _, set := range before {
		if sameFilesystemPath(set.Workspace, workspace) {
			watch = set.Watch
			for _, source := range set.Sources {
				current[strings.ToLower(filepath.ToSlash(filepath.Clean(source.Path)))] = source
			}
			continue
		}
		sets = append(sets, set)
	}
	for key, source := range changes {
		if add {
			current[key] = source
		} else {
			delete(current, key)
		}
	}
	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 || watch {
		sources := make([]settings.RepositoryIndexSource, 0, len(keys))
		for _, key := range keys {
			sources = append(sources, current[key])
		}
		sets = append(sets, settings.RepositoryIndexSourceSet{Workspace: workspace, Sources: sources, Watch: watch})
	}
	a.cfg.Context.RepositoryIndexSourceSets = sets
	cfg := *a.cfg
	a.mu.Unlock()
	if err := settings.Save(&cfg); err != nil {
		a.mu.Lock()
		a.cfg.Context.RepositoryIndexSourceSets = before
		a.mu.Unlock()
		return fmt.Errorf("save repository index sources: %w", err)
	}
	if a.ctx != nil {
		a.emit("mauler:repository_index_sources_changed", a.repositoryIndexSourcesFor(workspace))
		a.restartRepositoryIndexWatcher()
	}
	return nil
}

func validateRepositoryIndexSource(raw string) (string, settings.RepositoryIndexSource, error) {
	path := tools.NormalizeHostPath(strings.TrimSpace(raw))
	if path == "" {
		return "", settings.RepositoryIndexSource{}, fmt.Errorf("repository index source is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", settings.RepositoryIndexSource{}, fmt.Errorf("resolve repository index source: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", settings.RepositoryIndexSource{}, fmt.Errorf("inspect repository index source: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", settings.RepositoryIndexSource{}, fmt.Errorf("inspect repository index source: %w", err)
	}
	kind := "file"
	if info.IsDir() {
		kind = "folder"
	} else if !info.Mode().IsRegular() {
		return "", settings.RepositoryIndexSource{}, fmt.Errorf("repository index source is not a regular file or folder: %s", canonical)
	}
	canonical = filepath.ToSlash(filepath.Clean(canonical))
	return canonical, settings.RepositoryIndexSource{Path: canonical, Kind: kind}, nil
}

func (a *App) repositoryIndexSourcesFor(workspace string) []settings.RepositoryIndexSource {
	workspace = filepath.ToSlash(filepath.Clean(workspace))
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, set := range a.cfg.Context.RepositoryIndexSourceSets {
		if sameFilesystemPath(set.Workspace, workspace) {
			return append([]settings.RepositoryIndexSource(nil), set.Sources...)
		}
	}
	return []settings.RepositoryIndexSource{}
}

func (a *App) repositoryIndexWatchEnabledFor(workspace string) bool {
	if a == nil || a.cfg == nil {
		return false
	}
	workspace = filepath.ToSlash(filepath.Clean(workspace))
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, set := range a.cfg.Context.RepositoryIndexSourceSets {
		if sameFilesystemPath(set.Workspace, workspace) {
			return set.Watch
		}
	}
	return false
}

func (a *App) repositoryIndexPolicy(workspace string) (repoindex.IndexPolicy, []settings.RepositoryIndexSource) {
	workspace = filepath.Clean(tools.NormalizeHostPath(workspace))
	sources := a.repositoryIndexSourcesFor(filepath.ToSlash(workspace))
	roots := []string{workspace}
	for _, source := range sources {
		roots = append(roots, filepath.Clean(tools.NormalizeHostPath(source.Path)))
	}
	return repoindex.DefaultPolicy(roots...), sources
}
