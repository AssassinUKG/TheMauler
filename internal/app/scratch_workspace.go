package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/settings"
	"mauler/internal/tools"
)

const scratchWorkspaceReviewAfter = 7 * 24 * time.Hour

// ScratchWorkspaceStatus is UI-safe metadata for an explicitly attached
// conversation workspace. ReviewAfter is advisory: Mauler never deletes the
// directory or its evidence automatically.
type ScratchWorkspaceStatus struct {
	Active            bool   `json:"active"`
	Exists            bool   `json:"exists"`
	ReviewDue         bool   `json:"review_due"`
	Name              string `json:"name,omitempty"`
	Path              string `json:"path,omitempty"`
	CreatedUnix       int64  `json:"created_unix,omitempty"`
	ReviewAfterUnix   int64  `json:"review_after_unix,omitempty"`
	RetentionPolicy   string `json:"retention_policy"`
	PromotionEligible bool   `json:"promotion_eligible"`
}

type scratchWorkspaceMarker struct {
	Version         int    `json:"version"`
	Name            string `json:"name"`
	CreatedUnix     int64  `json:"created_unix"`
	ReviewAfterUnix int64  `json:"review_after_unix"`
	Promoted        bool   `json:"promoted"`
}

// CreateScratchWorkspace explicitly creates and attaches a private workspace
// to the current conversation without erasing its history. It is persistent on
// disk and is only temporary in the sense that it is not yet a saved project.
func (a *App) CreateScratchWorkspace(name string) (ScratchWorkspaceStatus, error) {
	processStateMu.Lock()
	defer processStateMu.Unlock()
	originalWorkingDir, err := os.Getwd()
	if err != nil {
		return ScratchWorkspaceStatus{}, err
	}

	a.mu.Lock()
	if a.agentRunning || a.evalRunning {
		a.mu.Unlock()
		return ScratchWorkspaceStatus{}, fmt.Errorf("cannot attach a scratch workspace while an agent run or eval is active")
	}
	a.mu.Unlock()

	label := strings.TrimSpace(name)
	if label == "" {
		label = "Scratch chat"
	}
	slug := slugify(label)
	if slug == "" {
		slug = "chat"
	}
	configDir, err := settings.ConfigDir()
	if err != nil {
		return ScratchWorkspaceStatus{}, err
	}
	created := time.Now().UTC()
	root := filepath.Join(configDir, "scratch-workspaces", fmt.Sprintf("%s-%06d-%s", created.Format("20060102-150405"), created.Nanosecond()/1000, slug))
	if err := os.MkdirAll(filepath.Join(root, ".mauler"), 0o750); err != nil {
		return ScratchWorkspaceStatus{}, err
	}
	marker := scratchWorkspaceMarker{
		Version: 1, Name: label, CreatedUnix: created.Unix(),
		ReviewAfterUnix: created.Add(scratchWorkspaceReviewAfter).Unix(),
	}
	if err := writeScratchWorkspaceMarker(root, marker); err != nil {
		return ScratchWorkspaceStatus{}, err
	}
	if err := os.Chdir(root); err != nil {
		return ScratchWorkspaceStatus{}, err
	}

	a.mu.Lock()
	if a.agentRunning || a.evalRunning {
		a.mu.Unlock()
		_ = os.Chdir(originalWorkingDir)
		return ScratchWorkspaceStatus{}, fmt.Errorf("cannot attach a scratch workspace while an agent run or eval is active")
	}
	previousCfg := *a.cfg
	a.cfg.Context.WorkspacePreferences = upsertWorkspaceAgentPreference(
		a.cfg.Context.WorkspacePreferences,
		a.cfg.Context.WorkspaceDir,
		a.cfg.Agents.ModeOverride,
	)
	normalized := filepath.ToSlash(root)
	a.cfg.Context.WorkspaceDir = normalized
	a.cfg.Context.OpenFolders = []settings.WorkspaceFolder{{Path: normalized, Name: label, Role: "root"}}
	a.cfg.Context.ScratchWorkspaceDir = normalized
	a.cfg.Context.ScratchWorkspaceName = label
	a.cfg.Context.ScratchWorkspaceCreatedUnix = marker.CreatedUnix
	a.cfg.Context.ScratchWorkspaceReviewUnix = marker.ReviewAfterUnix
	// A prior project's authorised target must not leak into an unattached chat.
	labID := firstNonEmpty(a.cfg.Context.ActiveLabProfile, a.cfg.Context.Lab.ID, "default")
	a.cfg.Context.Lab = settings.LabContext{ID: labID, Name: label, EvidencePolicy: "research_assisted", AccessPreference: "auto"}
	cfg := *a.cfg
	a.rollback.Clear()
	a.advanceConversationEpochLocked()
	previousBrowserOwner := a.rotateBrowserWorkflowOwnerLocked()
	a.mu.Unlock()

	if err := settings.Save(&cfg); err != nil {
		_ = os.Chdir(originalWorkingDir)
		a.mu.Lock()
		*a.cfg = previousCfg
		a.mu.Unlock()
		return ScratchWorkspaceStatus{}, err
	}
	_, _ = tools.CloseBrowserWorkflow(previousBrowserOwner)
	_ = a.ClearTodos()
	status := a.GetScratchWorkspaceStatus()
	if a.ctx != nil {
		a.emit("mauler:workspace_attached", normalized)
		a.emit("mauler:scratch_workspace", status)
	}
	_ = a.refreshEngagementCatalog()
	return status, nil
}

// GetScratchWorkspaceStatus reports the active conversation scratch without
// enumerating or deleting any expired directory.
func (a *App) GetScratchWorkspaceStatus() ScratchWorkspaceStatus {
	status := ScratchWorkspaceStatus{RetentionPolicy: "review only; files are never deleted automatically"}
	if a == nil || a.cfg == nil {
		return status
	}
	a.mu.Lock()
	root := filepath.ToSlash(strings.TrimSpace(a.cfg.Context.ScratchWorkspaceDir))
	status.Name = strings.TrimSpace(a.cfg.Context.ScratchWorkspaceName)
	status.CreatedUnix = a.cfg.Context.ScratchWorkspaceCreatedUnix
	status.ReviewAfterUnix = a.cfg.Context.ScratchWorkspaceReviewUnix
	current := filepath.ToSlash(strings.TrimSpace(a.cfg.Context.WorkspaceDir))
	a.mu.Unlock()
	if root == "" {
		return status
	}
	status.Path = root
	status.Active = sameFilesystemPath(root, current)
	info, err := os.Stat(tools.NormalizeHostPath(root))
	status.Exists = err == nil && info.IsDir()
	status.ReviewDue = status.ReviewAfterUnix > 0 && time.Now().Unix() >= status.ReviewAfterUnix
	status.PromotionEligible = status.Active && status.Exists
	return status
}

// PromoteScratchWorkspace registers the existing scratch directory as a
// durable project. No file move or deletion occurs.
func (a *App) PromoteScratchWorkspace(name string) (ScratchWorkspaceStatus, error) {
	processStateMu.Lock()
	defer processStateMu.Unlock()

	a.mu.Lock()
	if a.agentRunning || a.evalRunning {
		a.mu.Unlock()
		return ScratchWorkspaceStatus{}, fmt.Errorf("cannot promote a scratch workspace while an agent run or eval is active")
	}
	root := filepath.ToSlash(strings.TrimSpace(a.cfg.Context.ScratchWorkspaceDir))
	current := filepath.ToSlash(strings.TrimSpace(a.cfg.Context.WorkspaceDir))
	if root == "" || !sameFilesystemPath(root, current) {
		a.mu.Unlock()
		return ScratchWorkspaceStatus{}, fmt.Errorf("the active conversation does not have a scratch workspace")
	}
	previousCfg := *a.cfg
	label := strings.TrimSpace(name)
	if label == "" {
		label = firstNonEmpty(a.cfg.Context.ScratchWorkspaceName, "Saved workspace")
	}
	baseID := slugify(label)
	if baseID == "" {
		baseID = "workspace"
	}
	id := baseID
	used := map[string]bool{}
	for _, profile := range a.cfg.Context.LabProfiles {
		used[strings.ToLower(strings.TrimSpace(profile.ID))] = true
	}
	for suffix := 2; used[strings.ToLower(id)]; suffix++ {
		id = fmt.Sprintf("%s-%d", baseID, suffix)
	}
	profile := settings.LabProfile{
		ID: id, Name: label, WorkspaceDir: root,
		EvidencePolicy:   firstNonEmpty(a.cfg.Context.Lab.EvidencePolicy, "research_assisted"),
		AccessPreference: firstNonEmpty(a.cfg.Context.Lab.AccessPreference, "auto"),
	}
	a.cfg.Context.LabProfiles = append(a.cfg.Context.LabProfiles, profile)
	a.cfg.Context.ActiveLabProfile = id
	a.cfg.Context.Lab = settings.LabContext{
		ID: id, Name: label, EvidencePolicy: profile.EvidencePolicy,
		AccessPreference: profile.AccessPreference,
	}
	a.cfg.Context.ScratchWorkspaceDir = ""
	a.cfg.Context.ScratchWorkspaceName = ""
	a.cfg.Context.ScratchWorkspaceCreatedUnix = 0
	a.cfg.Context.ScratchWorkspaceReviewUnix = 0
	cfg := *a.cfg
	a.mu.Unlock()

	if err := settings.Save(&cfg); err != nil {
		a.mu.Lock()
		*a.cfg = previousCfg
		a.mu.Unlock()
		return ScratchWorkspaceStatus{}, err
	}
	if marker, err := readScratchWorkspaceMarker(root); err == nil {
		marker.Promoted = true
		marker.Name = label
		_ = writeScratchWorkspaceMarker(root, marker)
	}
	status := ScratchWorkspaceStatus{
		Exists: true, Name: label, Path: root,
		RetentionPolicy: "durable workspace; files remain in place",
	}
	if a.ctx != nil {
		a.emit("mauler:scratch_workspace", status)
		a.emit("mauler:workspace_folders_changed", cfg.Context.OpenFolders)
	}
	return status, nil
}

func scratchWorkspaceMarkerPath(root string) string {
	return filepath.Join(tools.NormalizeHostPath(root), ".mauler", "scratch-workspace.json")
}

func writeScratchWorkspaceMarker(root string, marker scratchWorkspaceMarker) error {
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(scratchWorkspaceMarkerPath(root), data, 0o640)
}

func readScratchWorkspaceMarker(root string) (scratchWorkspaceMarker, error) {
	var marker scratchWorkspaceMarker
	data, err := os.ReadFile(scratchWorkspaceMarkerPath(root))
	if err != nil {
		return marker, err
	}
	err = json.Unmarshal(data, &marker)
	return marker, err
}
