package engagement

import (
	"errors"
	"testing"
	"time"
)

func TestStateRequiresClaimAndEnforcesOrderedPhaseProgress(t *testing.T) {
	workflow := testWorkflow("step", RunCount{})
	checklist := testChecklist()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	state, err := NewState("eng-1", "Test", workflow, checklist, []string{"target.htb", "target.htb"}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Scope) != 1 || !state.ScopeLocked {
		t.Fatalf("scope = %#v locked=%v", state.Scope, state.ScopeLocked)
	}

	first := WorkRef{Kind: WorkStep, PhaseID: "phase-one", ID: "first"}
	if _, err := state.RecordWork(workflow, first, "agent-1", StatusDone, "tested it", 1, now); !errors.Is(err, ErrClaimRequired) {
		t.Fatalf("record without claim = %v, want ErrClaimRequired", err)
	}
	if _, err := state.Advance(workflow, now); !errors.Is(err, ErrPhaseBlocked) {
		t.Fatalf("advance incomplete phase = %v, want ErrPhaseBlocked", err)
	}

	next, err := state.Now(workflow, now)
	if err != nil {
		t.Fatal(err)
	}
	if next.Action != "claim" || next.Work == nil || next.Work.Ref != first {
		t.Fatalf("first next action = %#v", next)
	}
	settleWork(t, state, workflow, checklist, first, "agent-1", StatusDone, "Reachability confirmed.", now)

	second := WorkRef{Kind: WorkStep, PhaseID: "phase-one", ID: "second"}
	next, err = state.Now(workflow, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.Work == nil || next.Work.Ref != second {
		t.Fatalf("second next action = %#v", next)
	}
	settleWork(t, state, workflow, checklist, second, "agent-1", StatusSkipped, "No additional work was applicable.", now.Add(2*time.Second))

	next, err = state.Now(workflow, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.Action != "advance" || !next.PhaseComplete {
		t.Fatalf("phase completion = %#v", next)
	}
	phase, err := state.Advance(workflow, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if phase != "phase-two" {
		t.Fatalf("advanced phase = %q, want phase-two", phase)
	}
}

func TestStateRejectsStaleRevisionAndConcurrentClaim(t *testing.T) {
	workflow := testWorkflow("step", RunCount{})
	checklist := testChecklist()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	state, err := NewState("eng-1", "Test", workflow, checklist, nil, true, now)
	if err != nil {
		t.Fatal(err)
	}
	ref := WorkRef{Kind: WorkStep, PhaseID: "phase-one", ID: "first"}
	claimed, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent-1", Alias: "main"}, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent-2"}, time.Minute, now); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("second claim = %v, want ErrAlreadyClaimed", err)
	}
	if _, err := state.RecordWork(workflow, ref, "agent-1", StatusDone, "evidence recorded", claimed.Revision-1, now); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale write = %v, want ErrRevisionConflict", err)
	}
}

func TestClaimHeartbeatPreservesWorkRevisionAndReleaseRequeuesObservedWork(t *testing.T) {
	workflow := testWorkflow("step", RunCount{})
	checklist := testChecklist()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	state, err := NewState("eng-heartbeat", "Heartbeat", workflow, checklist, nil, true, now)
	if err != nil {
		t.Fatal(err)
	}
	ref := WorkRef{Kind: WorkStep, PhaseID: "phase-one", ID: "first"}
	claimed, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent-1", Alias: "main"}, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	stateRevision := state.Revision
	heartbeat, changed, err := state.HeartbeatClaimant("agent-1", time.Minute, now.Add(30*time.Second))
	if err != nil || !changed {
		t.Fatalf("heartbeat=%#v changed=%v err=%v", heartbeat, changed, err)
	}
	if heartbeat.Revision != claimed.Revision || state.Revision != stateRevision+1 {
		t.Fatalf("heartbeat revisions work=%d want=%d state=%d want=%d", heartbeat.Revision, claimed.Revision, state.Revision, stateRevision+1)
	}
	if heartbeat.Claim == nil || !heartbeat.Claim.HeartbeatAt.Equal(now.Add(30*time.Second)) || !heartbeat.Claim.LeaseUntil.Equal(now.Add(90*time.Second)) {
		t.Fatalf("heartbeat lease = %#v", heartbeat.Claim)
	}
	recorded, err := state.RecordWork(workflow, ref, "agent-1", StatusDone, "Reachability recorded before a clean stop.", claimed.Revision, now.Add(40*time.Second))
	if err != nil {
		t.Fatalf("heartbeat made the claim revision stale: %v", err)
	}
	released, changed, err := state.ReleaseClaimant("agent-1", now.Add(45*time.Second))
	if err != nil || !changed {
		t.Fatalf("release=%#v changed=%v err=%v", released, changed, err)
	}
	if released.Claim != nil || released.Status != StatusPending || released.Finished || released.Revision != recorded.Revision+1 {
		t.Fatalf("released work = %#v", released)
	}
	if _, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent-2"}, time.Minute, now.Add(46*time.Second)); err != nil {
		t.Fatalf("released observed work could not be reclaimed: %v", err)
	}
}

func TestExpiredClaimReturnsWorkToPending(t *testing.T) {
	workflow := testWorkflow("step", RunCount{})
	checklist := testChecklist()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	state, err := NewState("eng-1", "Test", workflow, checklist, nil, true, now)
	if err != nil {
		t.Fatal(err)
	}
	ref := WorkRef{Kind: WorkStep, PhaseID: "phase-one", ID: "first"}
	if _, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent-1"}, time.Minute, now); err != nil {
		t.Fatal(err)
	}
	claimed, err := state.ClaimWork(workflow, ref, Claimant{ID: "agent-2"}, time.Minute, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("claim after expiry: %v", err)
	}
	if claimed.Claim == nil || claimed.Claim.Claimant.ID != "agent-2" || claimed.Status != StatusFocused {
		t.Fatalf("replacement claim = %#v", claimed)
	}
}

func TestRepeatRunPreservesObservationHistory(t *testing.T) {
	workflow := testWorkflow("step", RunCount{Count: 2})
	checklist := testChecklist()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	state, err := NewState("eng-1", "Test", workflow, checklist, nil, true, now)
	if err != nil {
		t.Fatal(err)
	}
	ref := WorkRef{Kind: WorkStep, PhaseID: "phase-one", ID: "first"}
	result := settleWork(t, state, workflow, checklist, ref, "agent-1", StatusDone, "First pass completed.", now)
	if !result.RunAgain || result.NextRun != 2 || result.Work.Status != StatusPending {
		t.Fatalf("first finish = %#v", result)
	}
	result = settleWork(t, state, workflow, checklist, ref, "agent-1", StatusDone, "Second deeper pass completed.", now.Add(time.Second))
	if result.RunAgain || !result.Work.Finished || result.Work.RunsCompleted != 2 {
		t.Fatalf("second finish = %#v", result)
	}
	if len(result.Work.Observations) != 2 {
		t.Fatalf("observation history = %#v", result.Work.Observations)
	}
}

func TestChecklistPhaseRoutesSetupThenChecksThenSummary(t *testing.T) {
	workflow := WorkflowDefinition{
		ID: "workflow", Name: "Workflow", Checklist: "checklist",
		Phases: []PhaseDefinition{{
			ID: "checks", Name: "Checks", Kind: "checklist",
			Steps: []StepDefinition{
				{Check: "setup", Title: "Tune checklist"},
				{Check: "work-checks", Title: "Work checks"},
			},
		}},
	}
	checklist := ChecklistDefinition{
		ID: "checklist", Name: "Checklist",
		Items: []CheckDefinition{
			{ID: "headers", Title: "Headers", Scope: "global"},
			{ID: "cookies", Title: "Cookies", Scope: "global"},
			{ID: "idor", Title: "IDOR", Scope: "per_endpoint"},
		},
	}
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	state, err := NewState("eng-1", "Test", workflow, checklist, nil, true, now)
	if err != nil {
		t.Fatal(err)
	}
	setup := WorkRef{Kind: WorkStep, PhaseID: "checks", ID: "setup"}
	settleWork(t, state, workflow, checklist, setup, "agent-1", StatusDone, "Checklist tuned.", now)

	next, err := state.Now(workflow, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.Work == nil || next.Work.Ref.Kind != WorkGlobalCheck || next.Work.Ref.ID != "headers" {
		t.Fatalf("first global check = %#v", next)
	}
	claimed, err := state.ClaimWork(workflow, next.Work.Ref, Claimant{ID: "agent-1"}, time.Minute, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	other := WorkRef{Kind: WorkGlobalCheck, ID: "cookies"}
	if _, err := state.ClaimWork(workflow, other, Claimant{ID: "agent-1"}, time.Minute, now.Add(time.Second)); !errors.Is(err, ErrClaimLimit) {
		t.Fatalf("second simultaneous claim = %v, want ErrClaimLimit", err)
	}
	recorded, err := state.RecordWork(workflow, next.Work.Ref, "agent-1", StatusPassed, "Headers are present.", claimed.Revision, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.FinishWork(workflow, checklist, next.Work.Ref, "agent-1", recorded.Revision, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	settleWork(t, state, workflow, checklist, other, "agent-1", StatusWarning, "Cookie flags need review.", now.Add(2*time.Second))

	next, err = state.Now(workflow, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	summary := WorkRef{Kind: WorkStep, PhaseID: "checks", ID: "work-checks"}
	if next.Work == nil || next.Work.Ref != summary {
		t.Fatalf("summary next = %#v", next)
	}
	settleWork(t, state, workflow, checklist, summary, "agent-1", StatusDone, "All global checks completed.", now.Add(3*time.Second))
	next, err = state.Now(workflow, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.Action != "done" || !next.WorkflowDone {
		t.Fatalf("completed checklist workflow = %#v", next)
	}
}

func TestAvailableWorkFansOutChecksButKeepsGatesSequential(t *testing.T) {
	workflow := WorkflowDefinition{
		ID: "workflow", Name: "Workflow", Checklist: "checklist",
		Phases: []PhaseDefinition{{
			ID: "checks", Name: "Checks", Kind: "checklist",
			Steps: []StepDefinition{
				{Check: "setup", Title: "Tune checklist"},
				{Check: "summary", Title: "Summarise checks"},
			},
		}},
	}
	checklist := ChecklistDefinition{
		ID: "checklist", Name: "Checklist",
		Items: []CheckDefinition{
			{ID: "headers", Title: "Headers", Scope: "global"},
			{ID: "cookies", Title: "Cookies", Scope: "global"},
			{ID: "cors", Title: "CORS", Scope: "global"},
		},
	}
	now := time.Date(2026, 7, 13, 13, 0, 0, 0, time.UTC)
	state, err := NewState("eng-queue", "Queue", workflow, checklist, nil, true, now)
	if err != nil {
		t.Fatal(err)
	}

	queue, err := state.Available(workflow, "", 8, now)
	if err != nil {
		t.Fatal(err)
	}
	if queue.Parallel || len(queue.Items) != 1 || queue.Items[0].Ref.ID != "setup" {
		t.Fatalf("setup queue = %#v", queue)
	}
	setup := WorkRef{Kind: WorkStep, PhaseID: "checks", ID: "setup"}
	settleWork(t, state, workflow, checklist, setup, "desktop:main", StatusDone, "Checklist setup completed.", now)

	queue, err = state.Available(workflow, "", 2, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !queue.Parallel || len(queue.Items) != 2 || queue.Items[0].Ref.ID != "headers" || queue.Items[1].Ref.ID != "cookies" {
		t.Fatalf("parallel queue = %#v", queue)
	}
	headers := queue.Items[0].Ref
	if _, err := state.ClaimWork(workflow, headers, Claimant{ID: "subagent:a"}, time.Minute, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	ownerQueue, err := state.Available(workflow, "subagent:a", 8, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerQueue.Items) != 1 || ownerQueue.Items[0].Ref != headers {
		t.Fatalf("claim owner queue = %#v", ownerQueue)
	}
	otherQueue, err := state.Available(workflow, "subagent:b", 8, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !otherQueue.Parallel || len(otherQueue.Items) != 2 || otherQueue.Items[0].Ref.ID != "cookies" || otherQueue.Items[1].Ref.ID != "cors" || otherQueue.Blocker != "" {
		t.Fatalf("other claimant queue = %#v", otherQueue)
	}
}

func TestEndpointPhaseMaterialisesPerEndpointChecksAndBlocksEmptyCoverage(t *testing.T) {
	workflow := WorkflowDefinition{
		ID: "endpoint-workflow", Name: "Endpoint workflow", Checklist: "endpoint-checks",
		Phases: []PhaseDefinition{{
			ID: "endpoint-testing", Name: "Endpoint testing", Kind: "endpoint",
			Steps: []StepDefinition{
				{Check: "collect", Title: "Collect"},
				{Check: "group", Title: "Group"},
				{Check: "adjust", Title: "Adjust"},
				{Check: "test", Title: "Test"},
			},
		}},
	}
	checklist := ChecklistDefinition{
		ID: "endpoint-checks", Name: "Endpoint checks",
		Items: []CheckDefinition{
			{ID: "access", Title: "Access control", Scope: "per_endpoint"},
			{ID: "injection", Title: "Injection", Scope: "per_endpoint"},
		},
	}
	now := time.Date(2026, 7, 13, 18, 0, 0, 0, time.UTC)
	state, err := NewState("eng-endpoints", "Endpoints", workflow, checklist, nil, true, now)
	if err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"collect", "group", "adjust"} {
		ref := WorkRef{Kind: WorkStep, PhaseID: "endpoint-testing", ID: id}
		settleWork(t, state, workflow, checklist, ref, "agent", StatusDone, "Endpoint setup completed.", now.Add(time.Duration(index)*time.Second))
	}
	if _, err := state.Now(workflow, now.Add(4*time.Second)); !errors.Is(err, ErrPhaseBlocked) {
		t.Fatalf("empty endpoint phase = %v, want ErrPhaseBlocked", err)
	}
	first, err := state.AddEndpoint(checklist, EndpointInput{ID: "login", Method: "post", URL: "/login"}, now.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first.Method != "POST" || len(state.EndpointChecks) != 2 {
		t.Fatalf("first endpoint = %#v checks=%d", first, len(state.EndpointChecks))
	}
	if _, err := state.AddEndpoint(checklist, EndpointInput{ID: "profile", URL: "/profile"}, now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.AddEndpoint(checklist, EndpointInput{ID: "login", URL: "/other"}, now); err == nil {
		t.Fatal("duplicate endpoint unexpectedly succeeded")
	}
	if grouped, err := state.SetEndpointGroup("login", "Authentication", now.Add(7*time.Second)); err != nil || grouped.FeatureGroup != "Authentication" {
		t.Fatalf("group endpoint = %#v err=%v", grouped, err)
	}

	wantOrder := []WorkRef{
		{Kind: WorkEndpointCheck, EndpointID: "login", ID: "access"},
		{Kind: WorkEndpointCheck, EndpointID: "login", ID: "injection"},
		{Kind: WorkEndpointCheck, EndpointID: "profile", ID: "access"},
		{Kind: WorkEndpointCheck, EndpointID: "profile", ID: "injection"},
	}
	for index, want := range wantOrder {
		next, err := state.Now(workflow, now.Add(time.Duration(8+index)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if next.Work == nil || next.Work.Ref != want {
			t.Fatalf("endpoint check %d = %#v, want %#v", index, next, want)
		}
		settleWork(t, state, workflow, checklist, want, "agent", StatusPassed, "The endpoint check passed with a captured response.", now.Add(time.Duration(8+index)*time.Second))
	}
	next, err := state.Now(workflow, now.Add(13*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	summary := WorkRef{Kind: WorkStep, PhaseID: "endpoint-testing", ID: "test"}
	if next.Work == nil || next.Work.Ref != summary {
		t.Fatalf("endpoint summary = %#v", next)
	}
	settleWork(t, state, workflow, checklist, summary, "agent", StatusDone, "All endpoint checks completed.", now.Add(13*time.Second))
	next, err = state.Now(workflow, now.Add(14*time.Second))
	if err != nil || next.Action != "done" || !next.WorkflowDone {
		t.Fatalf("endpoint workflow completion = %#v err=%v", next, err)
	}
}

func settleWork(t *testing.T, state *State, workflow WorkflowDefinition, checklist ChecklistDefinition, ref WorkRef, claimantID, status, observation string, now time.Time) FinishResult {
	t.Helper()
	claimed, err := state.ClaimWork(workflow, ref, Claimant{ID: claimantID}, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := state.RecordWork(workflow, ref, claimantID, status, observation, claimed.Revision, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := state.FinishWork(workflow, checklist, ref, claimantID, recorded.Revision, now)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
