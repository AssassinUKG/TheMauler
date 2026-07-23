package controlplane

import (
	"strings"
	"testing"
	"time"
)

func applyEvent(t *testing.T, state MachineState, kind EventKind, checks map[string][]string) MachineState {
	t.Helper()
	next, err := state.Apply(Event{Kind: kind, SatisfiedChecks: checks, At: time.Date(2026, 7, 15, 12, 0, state.Revision, 0, time.UTC)})
	if err != nil {
		t.Fatalf("apply %s from %s: %v", kind, state.Phase, err)
	}
	return next
}

func TestMachineHappyPathRequiresEvidenceOwnedVerification(t *testing.T) {
	contract := testContract(t, AcceptanceCheck{ID: "tests", Description: "tests pass", Verifier: "verify_gate", Blocking: true})
	state, err := NewMachineState(contract, time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	state = applyEvent(t, state, EventContractValidated, nil)
	if decision := state.CheckTool("write"); decision.Allowed || !decision.Controlled {
		t.Fatalf("write before plan should be blocked: %#v", decision)
	}
	state = applyEvent(t, state, EventPlanAccepted, nil)
	state = applyEvent(t, state, EventActionStarted, nil)
	state = applyEvent(t, state, EventActionSucceeded, nil)
	state = applyEvent(t, state, EventObservationRecorded, nil)
	state = applyEvent(t, state, EventVerificationRequested, nil)
	state = applyEvent(t, state, EventVerificationPassed, map[string][]string{"tests": {"ledger:verify-1"}})
	if state.Phase != PhaseComplete || !state.Terminal() {
		t.Fatalf("final state = %#v", state)
	}
}

func TestMachineCannotCompleteAfterFailureOrWithoutBlockingEvidence(t *testing.T) {
	contract := testContract(t, AcceptanceCheck{ID: "tests", Description: "tests pass", Verifier: "verify_gate", Blocking: true})
	state, _ := NewMachineState(contract, time.Now())
	state = applyEvent(t, state, EventContractValidated, nil)
	state = applyEvent(t, state, EventPlanAccepted, nil)
	state = applyEvent(t, state, EventActionStarted, nil)
	state = applyEvent(t, state, EventActionFailed, nil)
	if _, err := state.Apply(Event{Kind: EventVerificationRequested}); err == nil {
		t.Fatal("failed action transitioned directly to verification")
	}
	state = applyEvent(t, state, EventRepairReady, nil)
	state = applyEvent(t, state, EventVerificationRequested, nil)
	if _, err := state.Apply(Event{Kind: EventVerificationPassed}); err == nil || !strings.Contains(err.Error(), "unsatisfied") {
		t.Fatalf("completion without evidence error = %v", err)
	}
	if state.Phase != PhaseVerifying {
		t.Fatalf("illegal transition mutated state: %s", state.Phase)
	}
}

func TestMachineCanConcludeRepairOnlyBeforeFreshVerification(t *testing.T) {
	contract := testContract(t)
	state, _ := NewMachineState(contract, time.Now())
	state = applyEvent(t, state, EventContractValidated, nil)
	state = applyEvent(t, state, EventPlanAccepted, nil)
	state = applyEvent(t, state, EventActionStarted, nil)
	state = applyEvent(t, state, EventActionFailed, nil)
	state = applyEvent(t, state, EventRepairConcluded, nil)
	if state.Phase != PhaseActing {
		t.Fatalf("repair conclusion phase = %s, want acting", state.Phase)
	}
	state = applyEvent(t, state, EventVerificationRequested, nil)
	state = applyEvent(t, state, EventVerificationPassed, nil)
	if state.Phase != PhaseComplete {
		t.Fatalf("fresh verification phase = %s, want complete", state.Phase)
	}
}

func TestMachineApprovalAndContractRevision(t *testing.T) {
	contract := testContract(t)
	state, _ := NewMachineState(contract, time.Now())
	state = applyEvent(t, state, EventContractValidated, nil)
	state = applyEvent(t, state, EventPlanAccepted, nil)
	state = applyEvent(t, state, EventApprovalRequired, nil)
	if state.Phase != PhaseAwaitingApproval || state.ResumePhase != PhaseActing {
		t.Fatalf("approval state = %#v", state)
	}
	state = applyEvent(t, state, EventApprovalGranted, nil)
	if state.Phase != PhaseActing {
		t.Fatalf("approval resume phase = %s", state.Phase)
	}

	revised, err := ReviseTaskContract(contract, "new user correction", 2, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.Rebase(revised, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhasePlanning || state.PlanAccepted || state.ContractDigest != revised.Digest || len(state.SatisfiedChecks) != 0 {
		t.Fatalf("rebase state = %#v", state)
	}
}
