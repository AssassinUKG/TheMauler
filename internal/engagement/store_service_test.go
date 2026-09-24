package engagement

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mauler/internal/ledger"
	appstore "mauler/internal/store"
)

type recordingLedger struct {
	events []ledger.Event
}

func TestServiceHashesWorkspaceEvidenceAndPersistsFinding(t *testing.T) {
	root := t.TempDir()
	db, err := appstore.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	catalog, err := LoadEmbeddedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	runLedger := ledger.New(filepath.Join(root, "ledger.jsonl"))
	runLedger.AttachDB(db)
	service := NewService(db, catalog, runLedger)
	now := time.Date(2026, 7, 13, 20, 0, 0, 0, time.UTC)
	record, err := service.Create(context.Background(), CreateInput{
		ID: "eng-proof", Name: "Proof", Workspace: root, WorkflowID: "webapp-simple", Scope: []string{"target.test"}, ScopeLocked: true,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.Now(context.Background(), record.State.ID, now)
	if err != nil || next.Work == nil {
		t.Fatalf("next=%#v err=%v", next, err)
	}
	claimed, err := service.Claim(context.Background(), record.State.ID, next.Work.Ref, Claimant{ID: "agent"}, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "probe.txt")
	if err := os.WriteFile(artifact, []byte("HTTP/1.1 200 OK\nServer: target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	event, err := runLedger.Record(ledger.Event{Kind: "tool_result", Source: "tool", Tool: "http_probe", Output: "captured", Artifacts: []string{artifact}})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := service.AddEvidence(context.Background(), EvidenceInput{
		EngagementID: record.State.ID, Work: next.Work.Ref, Claimant: Claimant{ID: "agent"}, SourceKind: EvidenceScreenshot,
		Path: artifact, Description: "Raw target response.",
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if evidence.AgentComposed || evidence.LedgerEventID != event.ID || len(evidence.SHA256) != 64 || evidence.Size == 0 || evidence.Path != "probe.txt" {
		t.Fatalf("evidence = %#v", evidence)
	}
	if evidence.SourceKind != EvidenceHTTPCapture {
		t.Fatalf("source kind must derive from ledger tool, got %q", evidence.SourceKind)
	}
	finding, err := service.UpsertFinding(context.Background(), FindingInput{
		EngagementID: record.State.ID, Work: next.Work.Ref, Claimant: Claimant{ID: "agent"}, Title: "Header observation",
		Severity: "low", Reproduction: "Request the root path and inspect the response.", EvidenceIDs: []string{evidence.ID},
	}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if finding.State != FindingDraft || finding.Revision != 1 {
		t.Fatalf("finding = %#v", finding)
	}
	loaded, err := service.Get(context.Background(), record.State.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.State.EvidenceOrder) != 1 || len(loaded.State.FindingOrder) != 1 || loaded.State.Steps[next.Work.Ref.Key()].Revision != claimed.Revision {
		t.Fatalf("persisted engagement state = %#v", loaded.State)
	}
	if freshness := loaded.EvidenceFreshness[evidence.ID]; freshness.State != EvidenceFresh || freshness.CurrentSHA256 != evidence.SHA256 {
		t.Fatalf("fresh evidence fingerprint = %#v", freshness)
	}
	if err := os.WriteFile(artifact, []byte("HTTP/1.1 500 Changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = service.Get(context.Background(), record.State.ID)
	if err != nil {
		t.Fatal(err)
	}
	if freshness := loaded.EvidenceFreshness[evidence.ID]; freshness.State != EvidenceStale || freshness.CurrentSHA256 == evidence.SHA256 {
		t.Fatalf("changed evidence fingerprint = %#v", freshness)
	}
	if _, err := service.ConfirmFinding(context.Background(), record.State.ID, finding.ID, "agent", finding.Revision, "", false, now.Add(3*time.Second)); err == nil || !strings.Contains(err.Error(), "attach fresh evidence") {
		t.Fatalf("stale evidence confirmation error = %v", err)
	}
}

func (r *recordingLedger) Record(event ledger.Event) (ledger.Event, error) {
	r.events = append(r.events, event)
	return event, nil
}

func TestServicePersistsPinnedEngagementAndResumesNextAction(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := appstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadEmbeddedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingLedger{}
	service := NewService(db, catalog, recorder)
	now := time.Date(2026, 7, 13, 16, 0, 0, 0, time.UTC)
	record, err := service.Create(context.Background(), CreateInput{
		ID: "eng-persist", Name: "Authorised target", Workspace: `C:\work\target`, WorkflowID: "webapp-simple",
		Scope: []string{"target.test"}, ScopeLocked: true,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	notesRevision, err := service.SetNotes(context.Background(), record.State.ID, "# Target model\n\nAuthenticated users can manage their own profile.", record.State.NotesRevision, now)
	if err != nil {
		t.Fatal(err)
	}
	if notesRevision != 2 {
		t.Fatalf("notes revision = %d", notesRevision)
	}
	if _, err := service.SetNotes(context.Background(), record.State.ID, "stale overwrite", record.State.NotesRevision, now); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale notes update = %v", err)
	}
	if record.Workflow.Digest == "" || record.Checklist.Digest == "" || record.Checklist.Version != "1.1.0" {
		t.Fatalf("pinned record = %#v", record)
	}
	endpoint, err := service.AddEndpoint(context.Background(), record.State.ID, EndpointInput{Method: "get", URL: "/login", Name: "Login"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.ID == "" || endpoint.Method != "GET" {
		t.Fatalf("service endpoint = %#v", endpoint)
	}

	next, err := service.Now(context.Background(), record.State.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if next.Work == nil || next.Work.Ref.ID != "verify_reachable" {
		t.Fatalf("first action = %#v", next)
	}
	claimed, err := service.Claim(context.Background(), record.State.ID, next.Work.Ref, Claimant{ID: "agent-main"}, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := service.RecordResult(context.Background(), ObservationInput{
		EngagementID: record.State.ID, Ref: next.Work.Ref, ClaimantID: "agent-main", Status: StatusDone,
		Observation: "The authorised target responded over HTTPS.", ExpectedRevision: claimed.Revision,
	}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Finish(context.Background(), record.State.ID, next.Work.Ref, "agent-main", observed.Revision, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = appstore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	resumed := NewService(db, catalog, recorder)
	next, err = resumed.Now(context.Background(), record.State.ID, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.Work == nil || next.Work.Ref.ID != "quick_port_scan" {
		t.Fatalf("resumed action = %#v", next)
	}
	summaries, err := resumed.List(context.Background(), `c:\WORK\target`)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].CurrentPhase != "reconnaissance" {
		t.Fatalf("summaries = %#v", summaries)
	}
	loaded, err := resumed.Get(context.Background(), record.State.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State.NotesRevision != 2 || !strings.Contains(loaded.State.Notes, "Target model") {
		t.Fatalf("resumed notes = %#v", loaded.State)
	}
	if len(recorder.events) < 5 || recorder.events[0].Kind != "engagement_create" {
		t.Fatalf("ledger events = %#v", recorder.events)
	}
}

func TestServiceConcurrentHeartbeatRetriesWithoutStalingObservation(t *testing.T) {
	db, err := appstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	catalog, err := LoadEmbeddedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, catalog, nil)
	now := time.Date(2026, 7, 13, 16, 0, 0, 0, time.UTC)
	record, err := service.Create(context.Background(), CreateInput{
		ID: "eng-heartbeat-service", Name: "Heartbeat", Workspace: t.TempDir(), WorkflowID: "webapp-simple",
		Scope: []string{"target.test"}, ScopeLocked: true,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.Now(context.Background(), record.State.ID, now)
	if err != nil || next.Work == nil {
		t.Fatalf("next=%#v err=%v", next, err)
	}
	claimed, err := service.Claim(context.Background(), record.State.ID, next.Work.Ref, Claimant{ID: "agent-main"}, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, heartbeatErr := service.HeartbeatClaimant(context.Background(), record.State.ID, "agent-main", 2*time.Minute, now.Add(30*time.Second))
			errs <- heartbeatErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for heartbeatErr := range errs {
		if heartbeatErr != nil {
			t.Fatalf("concurrent heartbeat: %v", heartbeatErr)
		}
	}
	loaded, err := service.Get(context.Background(), record.State.ID)
	if err != nil {
		t.Fatal(err)
	}
	work := loaded.State.Steps[next.Work.Ref.Key()]
	if work == nil || work.Revision != claimed.Revision || work.Claim == nil || !work.Claim.LeaseUntil.Equal(now.Add(150*time.Second)) {
		t.Fatalf("persisted heartbeat work = %#v", work)
	}
	if _, err := service.RecordResult(context.Background(), ObservationInput{
		EngagementID: record.State.ID, Ref: next.Work.Ref, ClaimantID: "agent-main", Status: StatusDone,
		Observation: "Heartbeat preserved the original work revision.", ExpectedRevision: claimed.Revision,
	}, now.Add(40*time.Second)); err != nil {
		t.Fatalf("observation after heartbeat: %v", err)
	}
}

func TestStoreRejectsStaleStateAndPinnedPackMutation(t *testing.T) {
	db, err := appstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	workflow := testWorkflow("step", RunCount{})
	checklist := testChecklist()
	now := time.Date(2026, 7, 13, 17, 0, 0, 0, time.UTC)
	state, err := NewState("eng-race", "Race", workflow, checklist, []string{"target.test"}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	if err := store.Create(context.Background(), Record{Workspace: `C:\work\race`, Workflow: workflow, Checklist: checklist, State: state}); err != nil {
		t.Fatal(err)
	}
	first, err := store.Load(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := store.Load(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}
	ref := WorkRef{Kind: WorkStep, PhaseID: "phase-one", ID: "first"}
	expected := first.State.Revision
	if _, err := first.State.ClaimWork(first.Workflow, ref, Claimant{ID: "one"}, time.Minute, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), first, expected); err != nil {
		t.Fatal(err)
	}
	staleExpected := stale.State.Revision
	if _, err := stale.State.ClaimWork(stale.Workflow, ref, Claimant{ID: "two"}, time.Minute, now); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), stale, staleExpected); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update = %v, want ErrRevisionConflict", err)
	}

	current, err := store.Load(context.Background(), state.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Checklist.Version = "silently-changed"
	current.State.UpdatedAt = now.Add(time.Second)
	current.State.Revision++
	if err := store.Update(context.Background(), current, current.State.Revision-1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("pack mutation update = %v, want ErrRevisionConflict", err)
	}
}

func TestStoreDeleteReturnsNotFoundForMissingEngagement(t *testing.T) {
	db, err := appstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := NewStore(db).Delete(context.Background(), "missing"); !errors.Is(err, ErrEngagementNotFound) {
		t.Fatalf("delete missing = %v", err)
	}
}
