package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/engagement"
	"mauler/internal/ledger"
	"mauler/internal/settings"
	appstore "mauler/internal/store"
)

const engagementEvalVerifierVersion = "engagement-native-v1"

// RunEngagementAgentEval is the deterministic, no-model lifecycle scenario in
// the Agent Eval surface. It exercises the same SQLite service, RunLedger
// evidence, scope guard, and portable snapshot path used by live runs.
func (a *App) RunEngagementAgentEval() AgentEvalReport {
	agentEvalMu.Lock()
	defer agentEvalMu.Unlock()
	processStateMu.Lock()
	defer processStateMu.Unlock()
	if err := a.beginAgentEval(); err != nil {
		return agentEvalPreflightFailure("native-go", err)
	}
	defer a.endAgentEval()

	result := runNativeEngagementEval()
	report := AgentEvalReport{
		ID: "engagement-eval-" + time.Now().Format("20060102-150405"), CreatedAt: time.Now().Format(time.RFC3339),
		Profile: "native-go", Total: 1, Results: []AgentEvalResult{result},
	}
	if result.Pass {
		report.PassCount = 1
	}
	_ = saveAgentEvalReport(report)
	return report
}

func runNativeEngagementEval() AgentEvalResult {
	started := time.Now()
	result := AgentEvalResult{
		Name: "engagement-lifecycle-e2e", Status: "running", VerifierVersion: engagementEvalVerifierVersion,
		ToolSuccessRate: 100, StabilityScore: 100,
	}
	fail := func(step string, err error) AgentEvalResult {
		result.Status = "failed"
		result.StatusPass = false
		result.ArtifactPass = false
		result.HygienePass = true
		result.RuntimePass = false
		result.Pass = false
		result.FailReason = step + ": " + err.Error()
		result.RuntimeFailures = []string{result.FailReason}
		result.DurationMs = time.Since(started).Milliseconds()
		return result
	}

	root, err := os.MkdirTemp("", "mauler-engagement-eval-*")
	if err != nil {
		return fail("create workspace", err)
	}
	defer os.RemoveAll(root)
	oldWD, err := os.Getwd()
	if err != nil {
		return fail("read working directory", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()
	if err := os.Chdir(root); err != nil {
		return fail("enter workspace", err)
	}

	catalog, err := engagementEvalCatalog()
	if err != nil {
		return fail("build pinned catalog", err)
	}
	db, err := appstore.Open(filepath.Join(root, "state.db"))
	if err != nil {
		return fail("open source state", err)
	}
	defer db.Close()
	runLedger := ledger.New(filepath.Join(root, "run-ledger.jsonl"))
	runLedger.AttachDB(db)
	service := engagement.NewService(db, catalog, runLedger)
	ctx := context.Background()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	claimant := engagement.Claimant{ID: "eval:desktop:main", Alias: "Native evaluator"}
	record, err := service.Create(ctx, engagement.CreateInput{
		ID: "engagement-eval", Name: "Fixture web app", Workspace: filepath.ToSlash(root), WorkflowID: "eval-web",
		Scope: []string{"fixture.invalid"}, ScopeLocked: true,
	}, now)
	if err != nil {
		return fail("create scoped engagement", err)
	}
	result.ToolCalls++
	next, err := service.Now(ctx, record.State.ID, now.Add(time.Second))
	if err != nil || next.Work == nil || next.Work.Ref.ID != "verify_reachable" {
		return fail("resolve deterministic now", firstEvalError(err, "unexpected first work"))
	}
	claimed, err := service.Claim(ctx, record.State.ID, next.Work.Ref, claimant, time.Minute, now.Add(2*time.Second))
	if err != nil {
		return fail("claim reachability", err)
	}
	result.ToolCalls++
	reachEvidence, err := engagementEvalCapture(ctx, service, runLedger, root, record.State.ID, next.Work.Ref, claimant, "reachability", "HTTP/1.1 200 OK\nServer: fixture\n", now.Add(3*time.Second))
	if err != nil || reachEvidence.SourceKind != engagement.EvidenceHTTPCapture || reachEvidence.AgentComposed {
		return fail("attach raw reachability evidence", firstEvalError(err, "raw capture provenance was not preserved"))
	}
	result.ToolCalls++
	recorded, err := service.RecordResult(ctx, engagement.ObservationInput{
		EngagementID: record.State.ID, Ref: next.Work.Ref, ClaimantID: claimant.ID, Status: engagement.StatusDone,
		Observation: "The scoped fixture returned HTTP 200 and the raw response was captured.", ExpectedRevision: claimed.Revision,
	}, now.Add(4*time.Second))
	if err != nil {
		return fail("observe reachability", err)
	}
	if _, err := service.Finish(ctx, record.State.ID, next.Work.Ref, claimant.ID, recorded.Revision, now.Add(5*time.Second)); err != nil {
		return fail("finish reachability", err)
	}
	result.ToolCalls += 2

	endpoint, err := service.AddEndpoint(ctx, record.State.ID, engagement.EndpointInput{ID: "login", Method: "POST", URL: "/login", Name: "Login"}, now.Add(6*time.Second))
	if err != nil {
		return fail("add endpoint", err)
	}
	if endpoint, err = service.SetEndpointGroup(ctx, record.State.ID, endpoint.ID, "authentication", now.Add(7*time.Second)); err != nil || endpoint.FeatureGroup != "authentication" {
		return fail("group endpoint", firstEvalError(err, "endpoint group did not persist"))
	}
	result.ToolCalls += 2
	if _, err := service.Advance(ctx, record.State.ID, now.Add(8*time.Second)); err != nil {
		return fail("advance to global checks", err)
	}
	result.ToolCalls++
	if err := engagementEvalSettleStep(ctx, service, record.State.ID, claimant, now.Add(9*time.Second)); err != nil {
		return fail("finish global setup", err)
	}
	result.ToolCalls += 3

	globalNext, err := service.Now(ctx, record.State.ID, now.Add(10*time.Second))
	if err != nil || globalNext.Work == nil || globalNext.Work.Ref.Kind != engagement.WorkGlobalCheck {
		return fail("resolve global check", firstEvalError(err, "global check was not next"))
	}
	globalClaim, err := service.Claim(ctx, record.State.ID, globalNext.Work.Ref, claimant, time.Minute, now.Add(11*time.Second))
	if err != nil {
		return fail("claim global check", err)
	}
	result.ToolCalls++
	resumedService := engagement.NewService(db, catalog, runLedger)
	resumed, err := resumedService.Now(ctx, record.State.ID, now.Add(12*time.Second))
	if err != nil || resumed.Work == nil || resumed.Work.Claim == nil || resumed.Work.Claim.Claimant.ID != claimant.ID {
		return fail("resume claimed work from SQLite", firstEvalError(err, "live claim did not resume"))
	}
	service = resumedService

	finding, err := service.UpsertFinding(ctx, engagement.FindingInput{
		EngagementID: record.State.ID, Work: globalNext.Work.Ref, Claimant: claimant, Title: "Missing security headers",
		Severity: "low", Reproduction: "Request the fixture root and inspect the final response headers.",
	}, now.Add(13*time.Second))
	if err != nil {
		return fail("create draft finding", err)
	}
	if _, confirmErr := service.ConfirmFinding(ctx, record.State.ID, finding.ID, claimant.ID, finding.Revision, "", false, now.Add(14*time.Second)); confirmErr == nil || !strings.Contains(confirmErr.Error(), "non-agent-composed evidence") {
		return fail("reject unsupported finding", firstEvalError(confirmErr, "finding confirmation unexpectedly succeeded"))
	}
	result.ToolCalls += 2
	headerEvidence, err := engagementEvalCapture(ctx, service, runLedger, root, record.State.ID, globalNext.Work.Ref, claimant, "headers", "HTTP/1.1 200 OK\nContent-Type: text/html\n", now.Add(15*time.Second))
	if err != nil {
		return fail("attach global-check evidence", err)
	}
	result.ToolCalls++
	finding, err = service.UpsertFinding(ctx, engagement.FindingInput{
		ID: finding.ID, EngagementID: record.State.ID, Work: globalNext.Work.Ref, Claimant: claimant,
		Title: finding.Title, Severity: finding.Severity, Reproduction: finding.Reproduction,
		EvidenceIDs: []string{headerEvidence.ID}, ExpectedRevision: finding.Revision,
	}, now.Add(16*time.Second))
	if err != nil {
		return fail("link finding evidence", err)
	}
	if finding, err = service.ConfirmFinding(ctx, record.State.ID, finding.ID, claimant.ID, finding.Revision, "", false, now.Add(17*time.Second)); err != nil || finding.State != engagement.FindingConfirmed {
		return fail("confirm evidenced finding", firstEvalError(err, "finding was not confirmed"))
	}
	result.ToolCalls += 2
	globalObserved, err := service.RecordResult(ctx, engagement.ObservationInput{
		EngagementID: record.State.ID, Ref: globalNext.Work.Ref, ClaimantID: claimant.ID, Status: engagement.StatusVulnerable,
		Observation: "The captured response omitted the required security headers.", ExpectedRevision: globalClaim.Revision,
	}, now.Add(18*time.Second))
	if err != nil {
		return fail("observe global check", err)
	}
	if _, err := service.Finish(ctx, record.State.ID, globalNext.Work.Ref, claimant.ID, globalObserved.Revision, now.Add(19*time.Second)); err != nil {
		return fail("finish global check", err)
	}
	result.ToolCalls += 2
	if err := engagementEvalSettleStep(ctx, service, record.State.ID, claimant, now.Add(20*time.Second)); err != nil {
		return fail("finish global summary", err)
	}
	if _, err := service.Advance(ctx, record.State.ID, now.Add(21*time.Second)); err != nil {
		return fail("advance to endpoint checks", err)
	}
	result.ToolCalls += 4
	if err := engagementEvalSettleStep(ctx, service, record.State.ID, claimant, now.Add(22*time.Second)); err != nil {
		return fail("finish endpoint setup", err)
	}
	result.ToolCalls += 3

	endpointNext, err := service.Now(ctx, record.State.ID, now.Add(23*time.Second))
	if err != nil || endpointNext.Work == nil || endpointNext.Work.Ref.Kind != engagement.WorkEndpointCheck || endpointNext.Work.Ref.EndpointID != endpoint.ID {
		return fail("resolve endpoint check", firstEvalError(err, "endpoint check was not next"))
	}
	endpointClaim, err := service.Claim(ctx, record.State.ID, endpointNext.Work.Ref, claimant, time.Minute, now.Add(24*time.Second))
	if err != nil {
		return fail("claim endpoint check", err)
	}
	if _, err := engagementEvalCapture(ctx, service, runLedger, root, record.State.ID, endpointNext.Work.Ref, claimant, "endpoint-access", "HTTP/1.1 403 Forbidden\n", now.Add(25*time.Second)); err != nil {
		return fail("attach endpoint evidence", err)
	}
	endpointObserved, err := service.RecordResult(ctx, engagement.ObservationInput{
		EngagementID: record.State.ID, Ref: endpointNext.Work.Ref, ClaimantID: claimant.ID, Status: engagement.StatusPassed,
		Observation: "Unauthenticated access was denied with HTTP 403 in the raw capture.", ExpectedRevision: endpointClaim.Revision,
	}, now.Add(26*time.Second))
	if err != nil {
		return fail("observe endpoint check", err)
	}
	if _, err := service.Finish(ctx, record.State.ID, endpointNext.Work.Ref, claimant.ID, endpointObserved.Revision, now.Add(27*time.Second)); err != nil {
		return fail("finish endpoint check", err)
	}
	result.ToolCalls += 4
	if err := engagementEvalSettleStep(ctx, service, record.State.ID, claimant, now.Add(28*time.Second)); err != nil {
		return fail("finish endpoint summary", err)
	}
	result.ToolCalls += 3
	completed, err := service.Now(ctx, record.State.ID, now.Add(29*time.Second))
	if err != nil || !completed.WorkflowDone || completed.Action != "done" {
		return fail("complete workflow", firstEvalError(err, "workflow did not reach done"))
	}

	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	evalApp := &App{cfg: &cfg, engagements: service, ledger: runLedger, suppressEvents: true}
	if err := evalApp.enforceEngagementScope(ctx, "http_probe", "https://fixture.invalid/login"); err != nil {
		return fail("allow in-scope probe", err)
	}
	if scopeErr := evalApp.enforceEngagementScope(ctx, "http_probe", "https://evil.invalid/"); scopeErr == nil || !strings.Contains(scopeErr.Error(), "locked engagement scope") {
		return fail("reject out-of-scope probe", firstEvalError(scopeErr, "out-of-scope probe unexpectedly succeeded"))
	}
	result.ToolCalls += 2

	exported, err := service.ExportJSON(ctx, record.State.ID, now.Add(30*time.Second))
	if err != nil {
		return fail("export portable snapshot", err)
	}
	result.ArtifactHash = byteHash([]byte(exported))
	importRoot := filepath.Join(root, "imported")
	if err := os.MkdirAll(importRoot, 0o750); err != nil {
		return fail("create import workspace", err)
	}
	importDB, err := appstore.Open(filepath.Join(importRoot, "state.db"))
	if err != nil {
		return fail("open import state", err)
	}
	defer importDB.Close()
	imported, err := engagement.NewService(importDB, catalog, nil).ImportJSON(ctx, exported, filepath.ToSlash(importRoot), now.Add(31*time.Second))
	if err != nil {
		return fail("import portable snapshot", err)
	}
	finalRecord, err := service.Get(ctx, record.State.ID)
	if err != nil {
		return fail("reload final state", err)
	}
	if err := compareEngagementEvalState(finalRecord, imported); err != nil {
		return fail("compare imported state", err)
	}
	result.ToolCalls += 2

	result.Status = "done"
	result.StatusPass = true
	result.ArtifactPass = true
	result.HygienePass = true
	result.RuntimePass = true
	result.Pass = true
	result.DurationMs = time.Since(started).Milliseconds()
	return result
}

func engagementEvalCatalog() (engagement.Catalog, error) {
	checklistInput := engagement.ChecklistDefinition{
		ID: "eval-checks", Name: "Evaluation checks",
		Items: []engagement.CheckDefinition{
			{ID: "security_headers", Title: "Security headers", Scope: "global", Evidence: engagement.CheckEvidencePolicy{RequiredKinds: []string{engagement.EvidenceHTTPCapture}, Minimum: 1, Reproduce: true}},
			{ID: "access_control", Title: "Endpoint access control", Scope: "per_endpoint", Evidence: engagement.CheckEvidencePolicy{RequiredKinds: []string{engagement.EvidenceHTTPCapture}, Minimum: 1, Reproduce: true}},
		},
	}
	workflowInput := engagement.WorkflowDefinition{
		ID: "eval-web", Name: "Evaluation web workflow", Checklist: checklistInput.ID,
		Phases: []engagement.PhaseDefinition{
			{ID: "recon", Name: "Recon", Steps: []engagement.StepDefinition{{ID: "verify_reachable", Title: "Verify reachability"}}},
			{ID: "global", Name: "Global checks", Kind: "checklist", Steps: []engagement.StepDefinition{{ID: "prepare_global", Title: "Prepare globals"}, {ID: "global_summary", Title: "Summarise globals"}}},
			{ID: "endpoints", Name: "Endpoint checks", Kind: "endpoint", Steps: []engagement.StepDefinition{{ID: "prepare_endpoints", Title: "Prepare endpoints"}, {ID: "endpoint_summary", Title: "Summarise endpoints"}}},
		},
	}
	checklistJSON, _ := json.Marshal(checklistInput)
	checklist, err := engagement.ParseChecklist(checklistJSON)
	if err != nil {
		return engagement.Catalog{}, err
	}
	workflowJSON, _ := json.Marshal(workflowInput)
	workflow, err := engagement.ParseWorkflow(workflowJSON)
	if err != nil {
		return engagement.Catalog{}, err
	}
	if err := engagement.ValidateBundle(workflow, checklist); err != nil {
		return engagement.Catalog{}, err
	}
	return engagement.Catalog{
		Workflows:  map[string]engagement.WorkflowDefinition{workflow.ID: workflow},
		Checklists: map[string]engagement.ChecklistDefinition{checklist.ID: checklist},
	}, nil
}

func engagementEvalSettleStep(ctx context.Context, service *engagement.Service, engagementID string, claimant engagement.Claimant, now time.Time) error {
	next, err := service.Now(ctx, engagementID, now)
	if err != nil {
		return err
	}
	if next.Work == nil || next.Work.Ref.Kind != engagement.WorkStep {
		return fmt.Errorf("expected a workflow step, got %#v", next)
	}
	claimed, err := service.Claim(ctx, engagementID, next.Work.Ref, claimant, time.Minute, now)
	if err != nil {
		return err
	}
	recorded, err := service.RecordResult(ctx, engagement.ObservationInput{
		EngagementID: engagementID, Ref: next.Work.Ref, ClaimantID: claimant.ID, Status: engagement.StatusDone,
		Observation: "The native evaluation completed this deterministic gate.", ExpectedRevision: claimed.Revision,
	}, now.Add(time.Millisecond))
	if err != nil {
		return err
	}
	_, err = service.Finish(ctx, engagementID, next.Work.Ref, claimant.ID, recorded.Revision, now.Add(2*time.Millisecond))
	return err
}

func engagementEvalCapture(ctx context.Context, service *engagement.Service, runLedger *ledger.Ledger, root, engagementID string, ref engagement.WorkRef, claimant engagement.Claimant, name, payload string, now time.Time) (engagement.Evidence, error) {
	path := filepath.Join(root, name+".http.txt")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		return engagement.Evidence{}, err
	}
	event, err := runLedger.Record(ledger.Event{
		ID: "eval-http-" + name, RunID: claimant.ID, Kind: "pipeline", Source: "tool", Tool: "http_probe", Status: "done",
		Message: "https://fixture.invalid/" + name, Output: payload, Artifacts: []string{path}, Timestamp: now.Format(time.RFC3339Nano),
	})
	if err != nil {
		return engagement.Evidence{}, err
	}
	return service.AddEvidence(ctx, engagement.EvidenceInput{
		EngagementID: engagementID, Work: ref, Claimant: claimant, SourceKind: engagement.EvidenceHTTPCapture,
		LedgerEventID: event.ID, Description: "Raw fixture HTTP capture for " + name + ".",
	}, now)
}

func compareEngagementEvalState(source, imported engagement.Record) error {
	if source.State == nil || imported.State == nil {
		return fmt.Errorf("source or imported state is nil")
	}
	if source.State.ID != imported.State.ID || source.State.CurrentPhase != imported.State.CurrentPhase {
		return fmt.Errorf("identity/phase drifted: source=%s/%s imported=%s/%s", source.State.ID, source.State.CurrentPhase, imported.State.ID, imported.State.CurrentPhase)
	}
	if len(source.State.Evidence) != len(imported.State.Evidence) || len(source.State.Findings) != len(imported.State.Findings) || len(source.State.Endpoints) != len(imported.State.Endpoints) {
		return fmt.Errorf("collection counts drifted during import")
	}
	for key, work := range source.State.Steps {
		other := imported.State.Steps[key]
		if work == nil || other == nil || work.Status != other.Status || work.Finished != other.Finished || work.RunsCompleted != other.RunsCompleted {
			return fmt.Errorf("step %s drifted during import", key)
		}
	}
	for key, work := range source.State.GlobalChecks {
		other := imported.State.GlobalChecks[key]
		if work == nil || other == nil || work.Status != other.Status || work.Finished != other.Finished {
			return fmt.Errorf("global check %s drifted during import", key)
		}
	}
	for key, work := range source.State.EndpointChecks {
		other := imported.State.EndpointChecks[key]
		if work == nil || other == nil || work.Status != other.Status || work.Finished != other.Finished {
			return fmt.Errorf("endpoint check %s drifted during import", key)
		}
	}
	for _, finding := range imported.State.Findings {
		if finding == nil || finding.State != engagement.FindingConfirmed {
			return fmt.Errorf("confirmed finding state drifted during import")
		}
	}
	return nil
}

func firstEvalError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errors.New(fallback)
}
