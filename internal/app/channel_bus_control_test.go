package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestRemoteWorkspaceFileControlsStayInsideActiveProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("box notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scans"), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: &settings.Settings{Context: settings.ContextConfig{WorkspaceDir: filepath.ToSlash(root)}}}

	listing, err := app.remoteListFiles("")
	if err != nil || !strings.Contains(listing, "notes.md") || !strings.Contains(listing, "scans/") {
		t.Fatalf("listing=%q err=%v", listing, err)
	}
	content, err := app.remoteReadWorkspaceFile("notes.md")
	if err != nil || !strings.Contains(content, "box notes") {
		t.Fatalf("content=%q err=%v", content, err)
	}
	if _, err := app.remoteReadWorkspaceFile("../outside.txt"); err == nil {
		t.Fatal("workspace escape should be rejected")
	}
}

func TestRemoteArtifactListingAndProjectSummary(t *testing.T) {
	root := t.TempDir()
	artifactDir := filepath.Join(root, "mauler_artifacts")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "scan.txt"), []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{cfg: &settings.Settings{Context: settings.ContextConfig{
		WorkspaceDir: filepath.ToSlash(root), ActiveLabProfile: "box-1",
		LabProfiles: []settings.LabProfile{{ID: "box-1", Name: "Box One", WorkspaceDir: filepath.ToSlash(root), Target: "10.10.10.10"}},
	}}}
	artifacts, err := app.remoteArtifactSummary("")
	if err != nil || !strings.Contains(artifacts, "mauler_artifacts/scan.txt") {
		t.Fatalf("artifacts=%q err=%v", artifacts, err)
	}
	projects := app.remoteProjectsSummary()
	if !strings.Contains(projects, "box-1 [active]") || !strings.Contains(projects, "10.10.10.10") {
		t.Fatalf("projects=%q", projects)
	}
}
