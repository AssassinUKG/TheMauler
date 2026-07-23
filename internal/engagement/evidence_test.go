package engagement

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVulnerableCheckRequiresCurrentRunRawEvidence(t *testing.T) {
	workflow, checklist := evidenceTestBundle()
	now := time.Date(2026, 7, 13, 18, 0, 0, 0, time.UTC)
	state, err := NewState("eng-evidence", "Evidence", workflow, checklist, []string{"target.test"}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	settleWork(t, state, workflow, checklist, WorkRef{Kind: WorkStep, PhaseID: "checks", ID: "setup"}, "agent", StatusDone, "Checklist prepared.", now)
	ref := WorkRef{Kind: WorkGlobalCheck, ID: "headers"}
	claimed, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent"}, time.Minute, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := state.RecordWork(workflow, ref, "agent", StatusVulnerable, "A repeatable unsafe response was observed.", claimed.Revision, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.FinishWork(workflow, checklist, ref, "agent", recorded.Revision, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "needs 1 raw evidence") {
		t.Fatalf("finish without evidence = %v", err)
	}
	_, err = state.AddEvidence(workflow, Evidence{
		ID: "agent-note", Work: ref, SourceKind: EvidenceHTTPCapture, LedgerEventID: "event-note",
		AgentComposed: true, Description: "Agent summary only.",
	}, "agent", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.FinishWork(workflow, checklist, ref, "agent", recorded.Revision, now.Add(3*time.Second)); err == nil {
		t.Fatal("agent-composed evidence must not satisfy completion")
	}
	_, err = state.AddEvidence(workflow, Evidence{
		ID: "raw-capture", Work: ref, SourceKind: EvidenceHTTPCapture, LedgerEventID: "event-raw",
		SHA256: strings.Repeat("a", 64), Size: 128, Description: "Raw HTTP response captured by http_probe.",
	}, "agent", now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.FinishWork(workflow, checklist, ref, "agent", recorded.Revision, now.Add(4*time.Second)); err != nil {
		t.Fatalf("finish with raw evidence: %v", err)
	}
}

func TestFindingConfirmationRequiresReproductionRawEvidenceAndScreenshotOrWaiver(t *testing.T) {
	workflow, checklist := evidenceTestBundle()
	now := time.Date(2026, 7, 13, 19, 0, 0, 0, time.UTC)
	state, err := NewState("eng-finding", "Finding", workflow, checklist, []string{"target.test"}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	settleWork(t, state, workflow, checklist, WorkRef{Kind: WorkStep, PhaseID: "checks", ID: "setup"}, "agent", StatusDone, "Checklist prepared.", now)
	ref := WorkRef{Kind: WorkGlobalCheck, ID: "headers"}
	if _, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent"}, time.Minute, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.AddEvidence(workflow, Evidence{ID: "raw", Work: ref, SourceKind: EvidenceHTTPCapture, LedgerEventID: "event", Description: "Raw response."}, "agent", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	finding, err := state.UpsertFinding(workflow, Finding{
		ID: "finding-1", Work: ref, Title: "Unsafe headers", Severity: "high", EvidenceIDs: []string{"raw"},
	}, "agent", 0, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.ConfirmFinding(workflow, finding.ID, "agent", finding.Revision, "", false, now.Add(3*time.Second)); err == nil || !strings.Contains(err.Error(), "reproducible") {
		t.Fatalf("confirmation without reproduction = %v", err)
	}
	finding.Reproduction = "Send GET / and inspect the returned security headers."
	finding, err = state.UpsertFinding(workflow, finding, "agent", finding.Revision, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.ConfirmFinding(workflow, finding.ID, "agent", finding.Revision, "", false, now.Add(4*time.Second)); err == nil || !strings.Contains(err.Error(), "screenshot") {
		t.Fatalf("high confirmation without screenshot = %v", err)
	}
	confirmed, err := state.ConfirmFinding(workflow, finding.ID, "agent", finding.Revision, "Operator verified the raw capture; screenshot is not useful for a header-only issue.", true, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.State != FindingConfirmed || confirmed.OperatorWaiver == "" {
		t.Fatalf("confirmed = %#v", confirmed)
	}
}

func TestLockedScopeMatchesStructuredTargetsAndRejectsEscapes(t *testing.T) {
	tests := []struct {
		scope   []string
		target  string
		allowed bool
	}{
		{[]string{"target.htb"}, "https://target.htb/admin", true},
		{[]string{"https://target.htb/app"}, "https://target.htb/app/users", true},
		{[]string{"https://target.htb/app"}, "https://target.htb/admin", false},
		{[]string{"10.10.0.0/16"}, "http://10.10.12.3:8080/", true},
		{[]string{"target.htb:8443"}, "https://target.htb:443/", false},
		{[]string{"target.htb"}, "/relative", true},
		{[]string{"target.htb"}, "https://evil.test/", false},
	}
	for _, tc := range tests {
		decision := CheckTargetScope(tc.scope, tc.target)
		if decision.Allowed != tc.allowed {
			t.Errorf("scope=%v target=%q decision=%#v", tc.scope, tc.target, decision)
		}
	}
	workflow, checklist := evidenceTestBundle()
	state, err := NewState("eng-scope", "Scope", workflow, checklist, []string{"target.htb"}, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.AddEndpoint(checklist, EndpointInput{ID: "bad", URL: "https://evil.test/"}, time.Now()); err == nil {
		t.Fatal("out-of-scope endpoint was accepted")
	}
	if _, err := state.AddEndpoint(checklist, EndpointInput{ID: "good", URL: "https://target.htb/api"}, time.Now()); err != nil {
		t.Fatalf("in-scope endpoint rejected: %v", err)
	}
}

func TestEvidenceRequiresClaim(t *testing.T) {
	workflow, checklist := evidenceTestBundle()
	state, err := NewState("eng-claim", "Claim", workflow, checklist, nil, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, err = state.AddEvidence(workflow, Evidence{ID: "e", Work: WorkRef{Kind: WorkStep, PhaseID: "checks", ID: "setup"}, SourceKind: EvidenceLedgerEvent, LedgerEventID: "event", Description: "raw"}, "agent", time.Now())
	if !errors.Is(err, ErrClaimRequired) {
		t.Fatalf("add evidence without claim = %v", err)
	}
}

func evidenceTestBundle() (WorkflowDefinition, ChecklistDefinition) {
	workflow := WorkflowDefinition{ID: "workflow", Name: "Workflow", Checklist: "checklist", Phases: []PhaseDefinition{{
		ID: "checks", Name: "Checks", Kind: "checklist", Steps: []StepDefinition{{Check: "setup", Title: "Setup"}, {Check: "summary", Title: "Summary"}},
	}}}
	checklist := ChecklistDefinition{ID: "checklist", Name: "Checklist", Items: []CheckDefinition{{
		ID: "headers", Title: "Headers", Scope: "global", Evidence: CheckEvidencePolicy{RequiredKinds: []string{EvidenceHTTPCapture}, Minimum: 1, Reproduce: true},
	}}}
	return workflow, checklist
}
