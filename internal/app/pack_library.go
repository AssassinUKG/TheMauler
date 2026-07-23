package app

import (
	"fmt"
	"strings"

	"mauler/internal/engagement"
	"mauler/internal/packlibrary"
)

func (a *App) ListPackLibrary() (packlibrary.Snapshot, error) {
	if a == nil || a.packs == nil {
		return packlibrary.Snapshot{}, fmt.Errorf("Pack Library is unavailable")
	}
	return a.packs.List(workspaceScope())
}

func (a *App) ClonePack(input packlibrary.CloneInput) (packlibrary.Summary, error) {
	if a == nil || a.packs == nil {
		return packlibrary.Summary{}, fmt.Errorf("Pack Library is unavailable")
	}
	summary, err := a.packs.Clone(workspaceScope(), input)
	if err != nil {
		return packlibrary.Summary{}, err
	}
	if err := a.refreshEngagementCatalog(); err != nil {
		return packlibrary.Summary{}, err
	}
	a.emit("mauler:pack_library_changed", map[string]any{"key": summary.Key, "action": "clone"})
	return summary, nil
}

func (a *App) ImportPackJSON(scope, raw string) (packlibrary.Summary, error) {
	if a == nil || a.packs == nil {
		return packlibrary.Summary{}, fmt.Errorf("Pack Library is unavailable")
	}
	if strings.TrimSpace(raw) == "" {
		return packlibrary.Summary{}, fmt.Errorf("pack JSON is required")
	}
	summary, err := a.packs.Import(workspaceScope(), scope, raw)
	if err != nil {
		return packlibrary.Summary{}, err
	}
	if err := a.refreshEngagementCatalog(); err != nil {
		return packlibrary.Summary{}, err
	}
	a.emit("mauler:pack_library_changed", map[string]any{"key": summary.Key, "action": "import"})
	return summary, nil
}

func (a *App) ExportPackJSON(key string) (string, error) {
	if a == nil || a.packs == nil {
		return "", fmt.Errorf("Pack Library is unavailable")
	}
	return a.packs.Export(workspaceScope(), key)
}

func (a *App) SetPackArchived(key string, archived bool) (packlibrary.Summary, error) {
	if a == nil || a.packs == nil {
		return packlibrary.Summary{}, fmt.Errorf("Pack Library is unavailable")
	}
	summary, err := a.packs.SetArchived(workspaceScope(), key, archived)
	if err != nil {
		return packlibrary.Summary{}, err
	}
	if err := a.refreshEngagementCatalog(); err != nil {
		return packlibrary.Summary{}, err
	}
	a.emit("mauler:pack_library_changed", map[string]any{"key": summary.Key, "action": "archive", "archived": archived})
	return summary, nil
}

func (a *App) engagementCatalog() (engagement.Catalog, error) {
	if a != nil && a.packs != nil {
		return a.packs.Catalog(workspaceScope())
	}
	return engagement.LoadEmbeddedCatalog()
}

func (a *App) refreshEngagementCatalog() error {
	if a == nil || a.engagements == nil {
		return nil
	}
	catalog, err := a.engagementCatalog()
	if err != nil {
		return err
	}
	a.engagements.SetCatalog(catalog)
	return nil
}
