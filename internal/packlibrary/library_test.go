package packlibrary

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/engagement"
)

func TestLibraryCloneArchiveAndRestore(t *testing.T) {
	root := t.TempDir()
	library, err := New(filepath.Join(root, "personal"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o750); err != nil {
		t.Fatal(err)
	}

	initial, err := library.List(workspace)
	if err != nil {
		t.Fatal(err)
	}
	builtin := findSummary(t, initial, func(item Summary) bool { return item.BuiltIn && item.ID == "webapp-simple" })
	if !builtin.Active || builtin.Archived || builtin.CheckCount != 20 {
		t.Fatalf("unexpected built-in summary: %+v", builtin)
	}
	for _, issue := range builtin.QualityIssues {
		if issue.Field == "source" {
			t.Fatalf("embedded curated source was reported as unapproved: %+v", issue)
		}
	}

	cloned, err := library.Clone(workspace, CloneInput{
		SourceKey: builtin.Key, Scope: ScopePersonal, ID: "webapp-local", Name: "Local Web App",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cloned.BuiltIn || cloned.Scope != ScopePersonal || cloned.Trust != engagement.PackTrustLocal || !cloned.Active {
		t.Fatalf("unexpected clone summary: %+v", cloned)
	}
	if _, err := os.Stat(filepath.FromSlash(cloned.Path)); err != nil {
		t.Fatalf("clone file not written: %v", err)
	}

	catalog, err := library.Catalog(workspace)
	if err != nil {
		t.Fatal(err)
	}
	workflow, checklist, err := catalog.Bundle("webapp-local")
	if err != nil {
		t.Fatal(err)
	}
	if workflow.Checklist != "webapp-local-checks" || len(checklist.Items) != 20 || checklist.Source.Trust != engagement.PackTrustLocal {
		t.Fatalf("unexpected cloned bundle: workflow=%+v checklist=%+v", workflow, checklist)
	}
	if original, _, err := catalog.Bundle("webapp-simple"); err != nil || original.Name != "Web App (Simple)" {
		t.Fatalf("built-in changed after clone: workflow=%+v err=%v", original, err)
	}

	archived, err := library.SetArchived(workspace, cloned.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.Active {
		t.Fatalf("archive state not reflected: %+v", archived)
	}
	catalog, err = library.Catalog(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Bundle("webapp-local"); err == nil {
		t.Fatal("archived pack remained in active catalog")
	}

	restored, err := library.SetArchived(workspace, cloned.Key, false)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Archived || !restored.Active {
		t.Fatalf("restore state not reflected: %+v", restored)
	}
}

func TestLibraryProjectScopeAndPortableImport(t *testing.T) {
	root := t.TempDir()
	library, err := New(filepath.Join(root, "personal"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	otherWorkspace := filepath.Join(root, "other")
	for _, path := range []string{workspace, otherWorkspace} {
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	initial, err := library.List(workspace)
	if err != nil {
		t.Fatal(err)
	}
	builtin := findSummary(t, initial, func(item Summary) bool { return item.ID == "webapp-simple" })
	project, err := library.Clone(workspace, CloneInput{
		SourceKey: builtin.Key, Scope: ScopeProject, ID: "wordpress-local", Name: "WordPress Local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.ToSlash(project.Path), "/.mauler/engagement-packs/") {
		t.Fatalf("project pack stored outside workspace: %s", project.Path)
	}
	other, err := library.List(otherWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range other.Packs {
		if item.ID == project.ID {
			t.Fatalf("project pack leaked into another workspace: %+v", item)
		}
	}

	exported, err := library.Export(workspace, project.Key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(filepath.Join(root, "second-personal"))
	if err != nil {
		t.Fatal(err)
	}
	imported, err := second.Import(otherWorkspace, ScopePersonal, exported)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID != "wordpress-local" || imported.Scope != ScopePersonal || !imported.Active {
		t.Fatalf("unexpected imported summary: %+v", imported)
	}
	if _, err := second.Import(otherWorkspace, ScopePersonal, exported); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate import was not rejected: %v", err)
	}
}

func TestLibraryRejectsUnsafeIdentityAndBuiltInArchive(t *testing.T) {
	library, err := New(filepath.Join(t.TempDir(), "personal"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := library.List("")
	if err != nil {
		t.Fatal(err)
	}
	builtin := findSummary(t, snapshot, func(item Summary) bool { return item.BuiltIn })
	if _, err := library.Clone("", CloneInput{SourceKey: builtin.Key, Scope: ScopePersonal, ID: "../escape", Name: "Bad"}); err == nil {
		t.Fatal("unsafe clone id was accepted")
	}
	if _, err := library.SetArchived("", builtin.Key, true); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("built-in archive was not rejected: %v", err)
	}
}

func TestLibraryQuarantinesMalformedPackWithoutBlockingCatalog(t *testing.T) {
	root := t.TempDir()
	personalRoot := filepath.Join(root, "personal")
	library, err := New(personalRoot)
	if err != nil {
		t.Fatal(err)
	}
	brokenDir := filepath.Join(personalRoot, "broken")
	if err := os.MkdirAll(brokenDir, 0o750); err != nil {
		t.Fatal(err)
	}
	brokenPath := filepath.Join(brokenDir, "0.1.0.pack.json")
	if err := os.WriteFile(brokenPath, []byte(`{"schema_version":`), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := library.List("")
	if err != nil {
		t.Fatal(err)
	}
	invalid := findSummary(t, snapshot, func(item Summary) bool { return !item.Valid })
	if invalid.Active || invalid.ValidationError == "" || snapshot.InvalidCount != 1 {
		t.Fatalf("malformed pack was not quarantined: snapshot=%+v invalid=%+v", snapshot, invalid)
	}
	catalog, err := library.Catalog("")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Bundle("webapp-simple"); err != nil {
		t.Fatalf("malformed local pack blocked built-in catalog: %v", err)
	}
	if _, err := library.Export("", invalid.Key); err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("quarantined pack export was not rejected: %v", err)
	}
	archived, err := library.SetArchived("", invalid.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.Valid {
		t.Fatalf("quarantined pack could not be archived safely: %+v", archived)
	}
}

func TestLibraryRejectsChecklistOwnershipCollision(t *testing.T) {
	library, err := New(filepath.Join(t.TempDir(), "personal"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := library.List("")
	if err != nil {
		t.Fatal(err)
	}
	builtin := findSummary(t, snapshot, func(item Summary) bool { return item.ID == "webapp-simple" })
	raw, err := library.Export("", builtin.Key)
	if err != nil {
		t.Fatal(err)
	}
	var bundle Bundle
	if err := json.Unmarshal([]byte(raw), &bundle); err != nil {
		t.Fatal(err)
	}
	bundle.ID = "collision-local"
	bundle.Name = "Collision Local"
	bundle.Version = "0.1.0"
	bundle.Workflow.ID = bundle.ID
	bundle.Workflow.Name = bundle.Name
	bundle.Workflow.Version = bundle.Version
	bundle.Checklist.Version = bundle.Version
	bundle.Workflow.Source = engagement.DefinitionSource{ID: "local-collision", Repository: "mauler://pack-library", Ref: bundle.Version, License: "MIT", Trust: engagement.PackTrustLocal}
	bundle.Checklist.Source = bundle.Workflow.Source
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Import("", ScopePersonal, string(encoded)); err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("checklist ownership collision was accepted: %v", err)
	}
}

func findSummary(t *testing.T, snapshot Snapshot, predicate func(Summary) bool) Summary {
	t.Helper()
	for _, item := range snapshot.Packs {
		if predicate(item) {
			return item
		}
	}
	t.Fatalf("matching pack not found in %+v", snapshot.Packs)
	return Summary{}
}
