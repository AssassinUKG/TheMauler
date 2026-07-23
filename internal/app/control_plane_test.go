package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mauler/internal/controlplane"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/store"
)

func TestRunControlPlaneBlocksMutationUntilPlanAndRequiresFileEvidence(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	run := startTaskRun("update helper.go with a greeting", "Builder", "profile", "model")
	app := &App{}
	if err := app.initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Builder"}, true); err != nil {
		t.Fatal(err)
	}
	if run.Contract == nil || run.Control == nil || run.Control.Phase != controlplane.PhasePlanning || !run.Contract.PlanRequired {
		t.Fatalf("initial control state = contract=%#v state=%#v", run.Contract, run.Control)
	}

	writeCall := llm.ToolCallDef{Function: llm.FunctionCall{Name: "write", Arguments: json.RawMessage(`{"path":"helper.go","content":"package helper"}`)}}
	if controlled, blocked := app.prepareControlledTool(&run, writeCall); !controlled || blocked == "" {
		t.Fatalf("write before plan = controlled %t block %q", controlled, blocked)
	}

	planCall := llm.ToolCallDef{Function: llm.FunctionCall{Name: "todo_write", Arguments: json.RawMessage(`{"action":"replace","items":["Update helper","Verify result"]}`)}}
	app.recordControlledToolOutcome(&run, false, planCall, nil)
	if run.Control.Phase != controlplane.PhaseActing || !run.Control.PlanAccepted {
		t.Fatalf("plan did not enter acting: %#v", run.Control)
	}
	if err := os.WriteFile(filepath.Join(root, "helper.go"), []byte("package helper\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	controlled, blocked := app.prepareControlledTool(&run, writeCall)
	if !controlled || blocked != "" {
		t.Fatalf("planned write = controlled %t block %q", controlled, blocked)
	}
	app.recordControlledToolOutcome(&run, controlled, writeCall, nil)
	if run.Control.Phase != controlplane.PhaseActing {
		t.Fatalf("observed tool should return to acting: %s", run.Control.Phase)
	}
	if err := app.requestControlVerification(&run); err != nil {
		t.Fatal(err)
	}
	if err := app.passControlVerification(&run, nil); err != nil {
		t.Fatal(err)
	}
	if run.Control.Phase != controlplane.PhaseComplete || len(run.Control.SatisfiedChecks["mutation_postcondition"]) != 1 {
		t.Fatalf("completed control state = %#v", run.Control)
	}
}

func TestRunControlPlanePersistsAtInitialization(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	run := startTaskRun("inspect the workspace", "Researcher", "profile", "model")
	app := &App{db: db}
	if err := app.initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Researcher"}, true); err != nil {
		t.Fatal(err)
	}
	runs, err := loadTaskRunsDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Contract == nil || runs[0].Control == nil || runs[0].Control.Phase != controlplane.PhaseActing {
		t.Fatalf("persisted run = %#v", runs)
	}
}

func TestRunControlPlaneAddsProjectVerificationWhenDetected(t *testing.T) {
	root := t.TempDir()
	previous, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	run := startTaskRun("fix the fixture", "Fixer", "profile", "model")
	if err := (&App{}).initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Fixer"}, true); err != nil {
		t.Fatal(err)
	}
	if !controlNeedsProjectVerification(run) {
		t.Fatalf("contract checks = %#v", run.Contract.AcceptanceChecks)
	}
	if prompt := controlPlanePrompt(run); prompt == "" || !containsAll(prompt, "contract_digest", "phase: planning", "Assistant prose is not completion evidence") {
		t.Fatalf("control prompt = %q", prompt)
	}
}

func TestRequestControlVerificationConcludesRepairWithoutWaivingChecks(t *testing.T) {
	cfg := settings.DefaultSettings()
	run := startTaskRun("inspect the workspace", "Researcher", "profile", "model")
	app := &App{}
	if err := app.initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Researcher"}, true); err != nil {
		t.Fatal(err)
	}
	if err := app.applyControlEvent(&run, controlplane.Event{Kind: controlplane.EventActionFailed, Detail: "optional read missed"}); err != nil {
		t.Fatal(err)
	}
	if run.Control.Phase != controlplane.PhaseRepairing {
		t.Fatalf("phase after tool failure = %s", run.Control.Phase)
	}
	if err := app.requestControlVerification(&run); err != nil {
		t.Fatal(err)
	}
	if run.Control.Phase != controlplane.PhaseVerifying || run.Control.LastEvent != controlplane.EventVerificationRequested {
		t.Fatalf("control after repair completion = %#v", run.Control)
	}
}

func TestRequestControlVerificationRequiresPlanWithoutTerminalTransition(t *testing.T) {
	cfg := settings.DefaultSettings()
	run := startTaskRun("update helper.go", "Builder", "profile", "model")
	app := &App{}
	if err := app.initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Builder"}, true); err != nil {
		t.Fatal(err)
	}
	before := *run.Control
	if err := app.requestControlVerification(&run); !errors.Is(err, errControlPlanRequired) {
		t.Fatalf("verification error = %v, want plan-required sentinel", err)
	}
	if run.Control.Phase != before.Phase || run.Control.Revision != before.Revision {
		t.Fatalf("rejected completion mutated control state: before=%#v after=%#v", before, run.Control)
	}
}

func TestControlPlanningScopeHidesMutationToolsUntilPlanAccepted(t *testing.T) {
	cfg := settings.DefaultSettings()
	run := startTaskRun("update helper.go", "Builder", "profile", "model")
	app := &App{}
	if err := app.initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Builder"}, true); err != nil {
		t.Fatal(err)
	}
	defs := []llm.ToolDef{
		{Type: "function", Function: llm.ToolFunctionDef{Name: "read"}},
		{Type: "function", Function: llm.ToolFunctionDef{Name: "todo_write"}},
		{Type: "function", Function: llm.ToolFunctionDef{Name: "edit"}},
		{Type: "function", Function: llm.ToolFunctionDef{Name: "shell"}},
	}
	got, choice, changed := constrainControlPlanningTools(run, defs, "auto")
	if !changed || choice != "required" {
		t.Fatalf("planning scope = changed %t choice %q", changed, choice)
	}
	if names := toolProtocolToolNames(got); names != "read, todo_write" {
		t.Fatalf("planning tools = %q, want read, todo_write", names)
	}
}

func TestControlCanVerifyAtToolBudgetAfterEvidenceProducingAction(t *testing.T) {
	run := TaskRun{Control: &controlplane.MachineState{Phase: controlplane.PhaseObserving, PlanRequired: true, PlanAccepted: true}}
	if !controlCanVerifyAtToolBudget(run) {
		t.Fatal("accepted observing run should retain controller verification at the tool budget")
	}
	run.Control.Phase = controlplane.PhasePlanning
	if controlCanVerifyAtToolBudget(run) {
		t.Fatal("planning run must not bypass the tool budget into verification")
	}
	run.Control.Phase = controlplane.PhaseActing
	run.Control.PlanAccepted = false
	if controlCanVerifyAtToolBudget(run) {
		t.Fatal("unaccepted required plan must not verify at the tool budget")
	}
}

func TestMutationEvidenceRejectsStaleFileHash(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "artifact.txt")
	if err := os.WriteFile(path, []byte("verified"), 0o640); err != nil {
		t.Fatal(err)
	}
	id, evidencePath := mutationEvidenceIDForPath(path)
	run := TaskRun{Events: []TaskRunEvent{{Kind: "control_evidence", Message: id, Detail: evidencePath}}}
	if got := mutationEvidenceIDs(run); len(got) != 1 {
		t.Fatalf("fresh evidence = %#v", got)
	}
	if err := os.WriteFile(path, []byte("changed after evidence"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got := mutationEvidenceIDs(run); len(got) != 0 {
		t.Fatalf("stale evidence remained valid: %#v", got)
	}
}

func containsAll(text string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(text, value) {
			return false
		}
	}
	return true
}
