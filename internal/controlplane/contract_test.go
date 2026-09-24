package controlplane

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testContract(t *testing.T, checks ...AcceptanceCheck) TaskContract {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspace")
	contract, err := NewTaskContract(ContractInput{
		RunID:               "task-1",
		Objective:           "update the helper and run tests",
		WorkspaceRoot:       root,
		Deliverables:        []Deliverable{{ID: "primary", Description: "updated helper", Kind: "file_change"}},
		AllowedMutations:    []PathRule{{Root: root, Access: "write"}},
		AcceptanceChecks:    checks,
		Risk:                RiskMedium,
		InstructionRevision: 1,
		PlanRequired:        true,
		Budgets:             RunBudgets{MaxToolCalls: 20, MaxRunSeconds: 60},
		CreatedAt:           time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func TestTaskContractIsSealedAndValidated(t *testing.T) {
	contract := testContract(t, AcceptanceCheck{
		ID: "tests", Description: "tests pass", Verifier: "verify_gate", Blocking: true, EvidenceKinds: []string{"command_exit"},
	})
	if !strings.HasPrefix(contract.Digest, "sha256:") {
		t.Fatalf("digest = %q", contract.Digest)
	}
	if err := contract.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := contract.BlockingCheckIDs(); len(got) != 1 || got[0] != "tests" {
		t.Fatalf("blocking checks = %#v", got)
	}
}

func TestTaskContractRejectsTamperingAndDuplicateChecks(t *testing.T) {
	contract := testContract(t)
	contract.Objective = "different objective"
	if err := contract.Validate(); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered contract error = %v", err)
	}

	root := t.TempDir()
	_, err := NewTaskContract(ContractInput{
		RunID:         "task-duplicate",
		Objective:     "do work",
		WorkspaceRoot: root,
		AcceptanceChecks: []AcceptanceCheck{
			{ID: "same", Description: "one", Verifier: "v", Blocking: true},
			{ID: "same", Description: "two", Verifier: "v", Blocking: true},
		},
		CreatedAt: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate acceptance check") {
		t.Fatalf("duplicate check error = %v", err)
	}
}

func TestReviseTaskContractLinksInstructionHistory(t *testing.T) {
	current := testContract(t)
	next, err := ReviseTaskContract(current, "update only the tests", 2, time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != 2 || next.InstructionRevision != 2 || next.ParentDigest != current.Digest || next.Digest == current.Digest {
		t.Fatalf("bad revision: %#v", next)
	}
	if _, err := ReviseTaskContract(next, "stale correction", 2, time.Now()); err == nil {
		t.Fatal("expected stale instruction revision to fail")
	}
}

func TestTaskContractRepairScopeMustReferenceProtectedArtifact(t *testing.T) {
	root := t.TempDir()
	artifact := filepath.Join(root, "report.md")
	_, err := NewTaskContract(ContractInput{
		RunID: "repair", Objective: "fix report.md", WorkspaceRoot: root,
		ProtectedArtifacts: []ArtifactBoundary{{Path: artifact, SHA256: strings.Repeat("a", 64), SourceRunID: "prior", Generation: 1}},
		RepairScope:        []string{filepath.Join(root, "other.md")},
		Risk:               RiskMedium, InstructionRevision: 1, CreatedAt: time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "not a protected finalized artifact") {
		t.Fatalf("repair scope validation error = %v", err)
	}
}
