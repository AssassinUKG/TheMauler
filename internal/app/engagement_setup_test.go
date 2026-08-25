package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/packlibrary"
	"mauler/internal/settings"
)

func TestGetEngagementSetupPreviewBuildsLockedScopeAndCandidates(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"notes", "scans", "screenshots", "src"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		filepath.Join(root, "notes", "target.md"):           "# target",
		filepath.Join(root, "scans", "nmap.xml"):            "<nmaprun/>",
		filepath.Join(root, "screenshots", "landing.png"):   "png",
		filepath.Join(root, "src", "not-a-candidate.go"):    "package main",
		filepath.Join(root, "node_modules", "ignored.json"): "{}",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	cfg.Context.WorkspaceDir = root
	cfg.Context.Lab = settings.LabContext{
		ID: "example", Name: "Example target", Target: "https://example.com", Hostname: "example.com",
		VPNInterface: "Ethernet 3",
	}
	app := &App{cfg: &cfg, profiles: &profiles}

	preview := app.GetEngagementSetupPreview()
	if !preview.CanCreate || !preview.CanStart {
		t.Fatalf("preview readiness create=%v start=%v checks=%#v", preview.CanCreate, preview.CanStart, preview.Checks)
	}
	if len(preview.Scope) != 2 || preview.Scope[0] != "https://example.com" || preview.Scope[1] != "example.com" {
		t.Fatalf("scope = %#v", preview.Scope)
	}
	if len(preview.Workflows) != 1 || preview.Workflows[0].ID != "webapp-simple" || preview.Workflows[0].CheckCount != 20 {
		t.Fatalf("workflows = %#v", preview.Workflows)
	}
	paths := make([]string, 0, len(preview.CandidateArtifacts))
	for _, artifact := range preview.CandidateArtifacts {
		paths = append(paths, artifact.Path)
	}
	joined := strings.Join(paths, "\n")
	for _, want := range []string{"notes/target.md", "scans/nmap.xml", "screenshots/landing.png"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("candidate paths missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "not-a-candidate.go") || strings.Contains(joined, "node_modules") {
		t.Fatalf("candidate discovery included ignored files: %s", joined)
	}
}

func TestGetEngagementSetupPreviewBlocksMissingTarget(t *testing.T) {
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	cfg.Context.WorkspaceDir = t.TempDir()
	cfg.Context.Lab = settings.LabContext{Name: "Missing target"}
	app := &App{cfg: &cfg, profiles: &profiles}

	preview := app.GetEngagementSetupPreview()
	if preview.CanCreate || len(preview.Scope) != 0 {
		t.Fatalf("missing target should block creation: %#v", preview)
	}
	var targetCheck *EngagementSetupCheck
	for i := range preview.Checks {
		if preview.Checks[i].ID == "target" {
			targetCheck = &preview.Checks[i]
			break
		}
	}
	if targetCheck == nil || targetCheck.Status != "blocked" || !targetCheck.Blocking {
		t.Fatalf("target readiness check = %#v", targetCheck)
	}
}

func TestGetEngagementSetupPreviewUsesCompleteStructuredScope(t *testing.T) {
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	cfg.Context.WorkspaceDir = t.TempDir()
	cfg.Context.Lab = settings.LabContext{ID: "client", Name: "Client", ScopeTargets: []settings.LabScopeTarget{
		{Value: "13.134.229.195", Kind: "ip", Environment: "external"},
		{Value: "3.9.20.132", Kind: "ip", Environment: "external"},
		{Value: "10.20.0.0/16", Kind: "cidr", Environment: "internal"},
		{Value: "10.20.10.5", Kind: "ip", Environment: "internal", Excluded: true},
	}}
	app := &App{cfg: &cfg, profiles: &profiles}
	preview := app.GetEngagementSetupPreview()
	want := "13.134.229.195,3.9.20.132,10.20.0.0/16,!10.20.10.5"
	if got := strings.Join(preview.Scope, ","); got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
	if preview.Target != "13.134.229.195" {
		t.Fatalf("structured scope primary target = %q", preview.Target)
	}
	if !preview.CanCreate {
		t.Fatalf("structured allowed scope did not enable create: %#v", preview.Checks)
	}
}

func TestCheckEngagementTargetHTTPAndOutOfScopeRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	cfg.Context.Lab = settings.LabContext{Name: "Fixture", Target: server.URL}
	app := &App{cfg: &cfg, profiles: &profiles}

	probe := app.CheckEngagementTarget()
	if probe.Status != "ready" || probe.HTTPStatus != http.StatusNoContent || probe.ScopeMatch != server.URL {
		t.Fatalf("HTTP probe = %#v", probe)
	}

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://outside.invalid/escape", http.StatusFound)
	}))
	defer redirect.Close()
	cfg.Context.Lab.Target = redirect.URL
	probe = app.CheckEngagementTarget()
	if probe.Status != "warning" || !strings.Contains(probe.Detail, "redirect blocked by locked scope") {
		t.Fatalf("out-of-scope redirect probe = %#v", probe)
	}
}

func TestPackLibraryCloneAppearsInGuidedSetupAndArchiveRemovesIt(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	library, err := packlibrary.New(filepath.Join(root, "personal-packs"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	cfg.Context.WorkspaceDir = workspace
	app := &App{cfg: &cfg, profiles: &profiles, packs: library, suppressEvents: true}
	snapshot, err := app.ListPackLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var source packlibrary.Summary
	for _, item := range snapshot.Packs {
		if item.ID == "webapp-simple" {
			source = item
			break
		}
	}
	if source.Key == "" {
		t.Fatal("built-in source pack was not listed")
	}
	cloned, err := app.ClonePack(packlibrary.CloneInput{
		SourceKey: source.Key, Scope: packlibrary.ScopeProject, ID: "guided-local", Name: "Guided Local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !setupHasWorkflow(app.GetEngagementSetupPreview(), cloned.ID) {
		t.Fatal("cloned pack did not join guided engagement setup")
	}
	if _, err := app.SetPackArchived(cloned.Key, true); err != nil {
		t.Fatal(err)
	}
	if setupHasWorkflow(app.GetEngagementSetupPreview(), cloned.ID) {
		t.Fatal("archived pack remained selectable in guided engagement setup")
	}
}

func setupHasWorkflow(preview EngagementSetupPreview, id string) bool {
	for _, workflow := range preview.Workflows {
		if workflow.ID == id {
			return true
		}
	}
	return false
}
