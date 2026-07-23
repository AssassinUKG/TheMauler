package app

import (
	"path/filepath"
	"strings"

	"mauler/internal/settings"
)

func workspaceAgentPreference(preferences []settings.WorkspacePreference, path string) string {
	for _, preference := range preferences {
		if sameFilesystemPath(preference.Path, path) {
			return strings.TrimSpace(preference.AgentMode)
		}
	}
	return ""
}

func upsertWorkspaceAgentPreference(preferences []settings.WorkspacePreference, path, mode string) []settings.WorkspacePreference {
	path = filepath.ToSlash(strings.TrimSpace(path))
	mode = strings.TrimSpace(mode)
	if path == "" || mode == "" {
		return preferences
	}
	result := append([]settings.WorkspacePreference(nil), preferences...)
	for i := range result {
		if sameFilesystemPath(result[i].Path, path) {
			result[i].Path = path
			result[i].AgentMode = mode
			return result
		}
	}
	return append(result, settings.WorkspacePreference{Path: path, AgentMode: mode})
}

func modeForActivatedWorkspace(preferences []settings.WorkspacePreference, path string) string {
	if mode := workspaceAgentPreference(preferences, path); mode != "" {
		return mode
	}
	return "Auto"
}
