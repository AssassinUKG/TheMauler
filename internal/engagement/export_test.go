package engagement

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appstore "mauler/internal/store"
)

func TestEngagementExportImportPreservesStateAndClearsEphemeralClaim(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalog, err := LoadEmbeddedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	db1, err := appstore.Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()
	service1 := NewService(db1, catalog, nil)
	now := time.Date(2026, 7, 13, 21, 0, 0, 0, time.UTC)
	record, err := service1.Create(ctx, CreateInput{
		ID: "eng-portable", Name: "Portable", Workspace: root, WorkflowID: "webapp-simple", Scope: []string{"target.test"}, ScopeLocked: true,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := service1.AddEndpoint(ctx, record.State.ID, EndpointInput{ID: "login", Method: "GET", URL: "/login"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.ID != "login" {
		t.Fatalf("endpoint = %#v", endpoint)
	}
	if _, err := service1.SetNotes(ctx, record.State.ID, "# Risk model\n\nAuthentication and profile changes are the main trust boundary.", record.State.NotesRevision, now); err != nil {
		t.Fatal(err)
	}
	next, err := service1.Now(ctx, record.State.ID, now)
	if err != nil || next.Work == nil {
		t.Fatalf("next=%#v err=%v", next, err)
	}
	if _, err := service1.Claim(ctx, record.State.ID, next.Work.Ref, Claimant{ID: "channel:telegram:test"}, time.Minute, now); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "capture.txt")
	if err := os.WriteFile(artifact, []byte("raw response"), 0o600); err != nil {
		t.Fatal(err)
	}
	evidence, err := service1.AddEvidence(ctx, EvidenceInput{
		EngagementID: record.State.ID, Work: next.Work.Ref, Claimant: Claimant{ID: "channel:telegram:test"},
		SourceKind: EvidenceArtifact, Path: artifact, Description: "Raw response capture.", OperatorTrusted: true,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service1.UpsertFinding(ctx, FindingInput{
		EngagementID: record.State.ID, Work: next.Work.Ref, Claimant: Claimant{ID: "channel:telegram:test"},
		Title: "Portable draft", Severity: "low", Reproduction: "Repeat the request.", EvidenceIDs: []string{evidence.ID},
	}, now); err != nil {
		t.Fatal(err)
	}
	claimedState, err := service1.Get(ctx, record.State.ID)
	if err != nil {
		t.Fatal(err)
	}
	claimedWork := claimedState.State.Steps[next.Work.Ref.Key()]
	if claimedWork == nil {
		t.Fatal("claimed work missing before export")
	}
	if _, err := service1.RecordResult(ctx, ObservationInput{
		EngagementID: record.State.ID, Ref: next.Work.Ref, ClaimantID: "channel:telegram:test",
		Status: StatusDone, Observation: "Reachability was captured before exporting.", ExpectedRevision: claimedWork.Revision,
	}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	exported, err := service1.ExportJSON(ctx, record.State.ID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported, `"schema_version": 1`) || strings.Contains(exported, filepath.ToSlash(root)+"/capture.txt") {
		t.Fatalf("export is not portable:\n%s", exported)
	}

	db2, err := appstore.Open(filepath.Join(t.TempDir(), "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	service2 := NewService(db2, catalog, nil)
	imported, err := service2.ImportJSON(ctx, exported, root, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if imported.State.ID != record.State.ID || len(imported.State.EvidenceOrder) != 1 || len(imported.State.FindingOrder) != 1 || imported.State.Endpoints["login"] == nil || !strings.Contains(imported.State.Notes, "Risk model") {
		t.Fatalf("imported state = %#v", imported.State)
	}
	work := imported.State.Steps[next.Work.Ref.Key()]
	if work == nil || work.Claim != nil || work.Status != StatusPending {
		t.Fatalf("ephemeral claim was not cleared: %#v", work)
	}
	if imported.State.Evidence[evidence.ID].Path != "capture.txt" || imported.Workflow.Digest == "" || imported.Checklist.Digest == "" {
		t.Fatalf("imported provenance = %#v", imported.State.Evidence[evidence.ID])
	}
	if _, err := service2.ImportJSON(ctx, exported, root, now); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate import = %v", err)
	}
}

func TestEngagementImportRejectsTamperedPinnedDefinitionAndPathEscape(t *testing.T) {
	ctx := context.Background()
	catalog, _ := LoadEmbeddedCatalog()
	db, err := appstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, catalog, nil)
	record, err := service.Create(ctx, CreateInput{ID: "eng-tamper", Name: "Tamper", Workspace: t.TempDir(), WorkflowID: "webapp-simple", Scope: []string{"target.test"}, ScopeLocked: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := service.ExportJSON(ctx, record.State.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatal(err)
	}
	recordObject := envelope["record"].(map[string]any)
	workflow := recordObject["workflow"].(map[string]any)
	workflow["name"] = "Tampered"
	tampered, _ := json.Marshal(envelope)
	otherDB, _ := appstore.Open(filepath.Join(t.TempDir(), "other.db"))
	defer otherDB.Close()
	other := NewService(otherDB, catalog, nil)
	if _, err := other.ImportJSON(ctx, string(tampered), t.TempDir(), time.Now()); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered import = %v", err)
	}
	var portable ExportEnvelope
	if err := json.Unmarshal([]byte(raw), &portable); err != nil {
		t.Fatal(err)
	}
	var ref WorkRef
	for _, work := range portable.Record.State.Steps {
		if work != nil {
			ref = work.Ref
			break
		}
	}
	portable.Record.State.Evidence["escape"] = &Evidence{
		ID: "escape", Work: ref, SourceKind: EvidenceArtifact, Path: "../outside.txt", Description: "invalid path", CreatedAt: time.Now(),
	}
	portable.Record.State.EvidenceOrder = append(portable.Record.State.EvidenceOrder, "escape")
	escaped, _ := json.Marshal(portable)
	if _, err := other.ImportJSON(ctx, string(escaped), t.TempDir(), time.Now()); err == nil || !strings.Contains(err.Error(), "escapes the workspace") {
		t.Fatalf("path escape import = %v", err)
	}
}
