package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"mauler/internal/settings"
	"mauler/internal/tools"
)

// CreateWorkspaceProject lets Chat create a durable project without leaving
// the conversation surface. The picker selects a parent; Mauler creates one
// new child directory and then uses the ordinary authoritative workspace
// switch path so history, tools, todos, browser ownership and epochs cannot
// leak from the previous project.
func (a *App) CreateWorkspaceProject(name, defaultParent string) (string, error) {
	label, err := validateWorkspaceProjectName(name)
	if err != nil {
		return "", err
	}
	if a.ctx == nil {
		return "", fmt.Errorf("app is not ready")
	}
	if strings.TrimSpace(defaultParent) == "" {
		defaultParent = filepath.Dir(tools.NormalizeHostPath(a.GetWorkingDir()))
	}
	defaultParent = tools.NormalizeHostPath(defaultParent)
	if info, statErr := os.Stat(defaultParent); statErr != nil || !info.IsDir() {
		defaultParent = filepath.Dir(tools.NormalizeHostPath(a.GetWorkingDir()))
	}
	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            fmt.Sprintf("Choose where to create %s", label),
		DefaultDirectory: defaultParent,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(parent) == "" {
		return "", nil
	}
	return a.createWorkspaceProjectAt(parent, label)
}

func (a *App) createWorkspaceProjectAt(parent, name string) (string, error) {
	label, err := validateWorkspaceProjectName(name)
	if err != nil {
		return "", err
	}
	parent, err = filepath.Abs(tools.NormalizeHostPath(strings.TrimSpace(parent)))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return "", fmt.Errorf("project parent: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project parent is not a directory: %s", parent)
	}
	target := filepath.Join(parent, label)
	if err := os.Mkdir(target, 0o750); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("a folder named %q already exists in %s", label, filepath.ToSlash(parent))
		}
		return "", fmt.Errorf("create project folder: %w", err)
	}
	if err := a.SetWorkingDir(target); err != nil {
		_ = os.Remove(target) // succeeds only while the newly created folder is empty
		return "", err
	}

	a.mu.Lock()
	id := uniqueWorkspaceProjectID(label, a.cfg.Context.LabProfiles)
	profile := settings.LabProfile{
		ID: id, Name: label, WorkspaceDir: filepath.ToSlash(target),
		OpsProfile: "pentesting", EvidencePolicy: "research_assisted", AccessPreference: "auto",
	}
	a.cfg.Context.LabProfiles = append(a.cfg.Context.LabProfiles, profile)
	a.cfg.Context.ActiveLabProfile = id
	a.cfg.Context.Lab = settings.LabContext{
		ID: id, Name: label, OpsProfile: profile.OpsProfile,
		EvidencePolicy: profile.EvidencePolicy, AccessPreference: profile.AccessPreference,
	}
	cfg := *a.cfg
	a.mu.Unlock()
	if err := settings.Save(&cfg); err != nil {
		return "", fmt.Errorf("save new project: %w", err)
	}
	if a.ctx != nil {
		a.emit("mauler:workspace_folders_changed", cfg.Context.OpenFolders)
	}
	return filepath.ToSlash(target), nil
}

func validateWorkspaceProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("project name is required")
	}
	if len([]rune(name)) > 96 {
		return "", fmt.Errorf("project name must be 96 characters or fewer")
	}
	if name == "." || name == ".." || strings.TrimRight(name, " .") != name {
		return "", fmt.Errorf("project name cannot end with a dot or space")
	}
	if strings.ContainsAny(name, `\/:*?"<>|`) {
		return "", fmt.Errorf("project name cannot contain path separators or Windows-reserved characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("project name cannot contain control characters")
		}
	}
	reserved := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch reserved {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "", fmt.Errorf("project name %q is reserved by Windows", name)
	}
	return name, nil
}

func uniqueWorkspaceProjectID(name string, profiles []settings.LabProfile) string {
	base := slugify(name)
	if base == "" {
		base = "project"
	}
	used := map[string]bool{}
	for _, profile := range profiles {
		used[strings.ToLower(strings.TrimSpace(profile.ID))] = true
	}
	id := base
	for suffix := 2; used[strings.ToLower(id)]; suffix++ {
		id = fmt.Sprintf("%s-%d", base, suffix)
	}
	return id
}
