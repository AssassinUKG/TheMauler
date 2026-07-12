package tools

import (
	"reflect"
	"testing"

	"mauler/internal/settings"
)

func TestSwapConfigSnapshotRestoresPreviousSnapshot(t *testing.T) {
	ResetConfigSnapshot()
	t.Cleanup(ResetConfigSnapshot)
	previous := settings.ToolsConfig{ShellBackend: "powershell", ShellDistro: "one", ShellUser: "before", ProtectedPaths: []string{"before"}}
	SetConfigSnapshot(previous)
	restore := SwapConfigSnapshot(settings.ToolsConfig{ShellBackend: "wsl", ShellDistro: "two", ShellUser: "during", ProtectedPaths: []string{"during"}})
	if configuredShellBackend() != "wsl" || configuredShellDistro() != "two" || configuredShellUser() != "during" {
		t.Fatal("temporary tool snapshot was not installed")
	}
	restore()
	restore()
	if configuredShellBackend() != "powershell" || configuredShellDistro() != "one" || configuredShellUser() != "before" {
		t.Fatal("previous tool snapshot was not restored")
	}
	if !reflect.DeepEqual(configuredProtectedPaths(), []string{"before"}) {
		t.Fatalf("protected paths were not restored: %#v", configuredProtectedPaths())
	}
}
