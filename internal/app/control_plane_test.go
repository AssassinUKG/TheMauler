package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestRunControlPlaneTreatsExplicitNoToolAnswerAsReadOnly(t *testing.T) {
	root := t.TempDir()
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	run := startTaskRun(
		"Return a Markdown table, then add a closing line. Do not use tools.",
		"Auto",
		"profile",
		"model",
	)

	if err := (&App{}).initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Auto"}, true); err != nil {
		t.Fatal(err)
	}
	if run.Contract.PlanRequired || len(run.Contract.Deliverables) != 0 ||
		len(run.Contract.AllowedMutations) != 0 || len(run.Contract.AcceptanceChecks) != 0 {
		t.Fatalf("no-tool answer received a mutation contract: %#v", run.Contract)
	}
	if run.Control.Phase != controlplane.PhaseActing || !run.Control.PlanAccepted {
		t.Fatalf("no-tool answer did not enter direct acting phase: %#v", run.Control)
	}
}

func TestRunControlPlaneTreatsHTTPMethodInventoryAsReadOnly(t *testing.T) {
	root := t.TempDir()
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	run := startTaskRun(
		"how many POST, PUT, PATCH, DELETE endpoints are there and how many should be targeted for 20% coverage?",
		"Auto", "profile", "model",
	)

	if err := (&App{}).initializeRunControlPlane(&run, &cfg, AgentMode{Name: "Auto"}, true); err != nil {
		t.Fatal(err)
	}
	if run.Contract.PlanRequired || len(run.Contract.Deliverables) != 0 || len(run.Contract.AcceptanceChecks) != 0 {
		t.Fatalf("API inventory question received a workspace-mutation contract: %#v", run.Contract)
	}
	if run.Contract.Risk != controlplane.RiskLow || run.Control.Phase != controlplane.PhaseActing {
		t.Fatalf("API inventory control state = contract=%#v control=%#v", run.Contract, run.Control)
	}
	if mode := classifyAgentMode(run.Prompt); mode.Name != "Auto" {
		t.Fatalf("API inventory routed to %q, want read-only Auto", mode.Name)
	}
}

func TestRunControlPlaneTreatsAnswerArtifactsAsReadOnly(t *testing.T) {
	root := t.TempDir()
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	for _, prompt := range []string{
		"Create a concise table of the endpoints in the attached swagger file.",
		"Generate a coverage report from swagger.json and show me the totals.",
		"Create a plan for reviewing this repository.",
		"Tell me what this config file contains and summarise it.",
	} {
		run := startTaskRun(prompt, "Auto", "profile", "model")
		if err := (&App{}).initializeRunControlPlane(&run, &cfg, classifyAgentMode(prompt), true); err != nil {
			t.Fatal(err)
		}
		if run.Contract.PlanRequired || len(run.Contract.Deliverables) != 0 ||
			len(run.Contract.AllowedMutations) != 0 || len(run.Contract.AcceptanceChecks) != 0 {
			t.Fatalf("answer-only prompt received mutation contract: prompt=%q contract=%#v", prompt, run.Contract)
		}
	}
}

func TestRunControlPlaneKeepsMixedAnswerAndMutationRequestsWritable(t *testing.T) {
	root := t.TempDir()
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	for _, prompt := range []string{
		"Summarise the issue, then fix the parser.",
		"Create a coverage report and save it to a file.",
		"Show me the current settings and update the docs.",
	} {
		run := startTaskRun(prompt, "Builder", "profile", "model")
		if err := (&App{}).initializeRunControlPlane(&run, &cfg, classifyAgentMode(prompt), true); err != nil {
			t.Fatal(err)
		}
		if !run.Contract.PlanRequired || len(run.Contract.AllowedMutations) == 0 {
			t.Fatalf("mixed mutation prompt lost write contract: prompt=%q contract=%#v", prompt, run.Contract)
		}
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

func TestSealFinalizedArtifactsBindsRunGenerationAndFreshness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "poc.py")
	if err := os.WriteFile(path, []byte("print('verified')\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	id, evidencePath := mutationEvidenceIDForPath(path)
	run := TaskRun{ID: "task-seal", Generation: 42, ConversationEpoch: 7}
	sealed := sealFinalizedArtifacts(run, map[string][]string{
		"mutation_postcondition": {id},
		"project_verification":   {"command_verdict:test:abc"},
	})
	if len(sealed) != 1 || sealed[0].Path != evidencePath || sealed[0].RunID != run.ID || sealed[0].Generation != run.Generation || sealed[0].ConversationEpoch != run.ConversationEpoch || !sealed[0].Fresh {
		t.Fatalf("sealed artifact = %#v", sealed)
	}
	if len(sealed[0].VerifierEvidenceIDs) != 1 || sealed[0].VerifierEvidenceIDs[0] != "command_verdict:test:abc" {
		t.Fatalf("verifier evidence = %#v", sealed[0].VerifierEvidenceIDs)
	}
	if err := os.WriteFile(path, []byte("print('changed')\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	refreshed := refreshFinalizedArtifactFreshness(sealed)
	if refreshed[0].Fresh || refreshed[0].Freshness != "changed" || refreshed[0].CurrentSHA256 == refreshed[0].SHA256 {
		t.Fatalf("changed artifact freshness = %#v", refreshed[0])
	}
}

func TestFinalizedArtifactRequiresExplicitFixerRepairScope(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	path := filepath.Join(root, "report.md")
	if err := os.WriteFile(path, []byte("validated\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	id, evidencePath := mutationEvidenceIDForPath(path)
	_, digest, ok := parseFileEvidenceID(id)
	if !ok {
		t.Fatalf("parse evidence id %q", id)
	}
	prior := TaskRun{
		ID: "task-prior", Generation: 10, Status: "done", StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
		FinalizedArtifacts: []FinalizedArtifact{{Path: evidencePath, SHA256: digest, RunID: "task-prior", Generation: 10, EvidenceID: id, FinalizedAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}},
	}
	if err := saveTaskRunDB(db, prior, nil); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	app := &App{db: db}

	ordinary := startTaskRun("update report.md with the latest notes", "Builder", "profile", "model")
	if err := app.initializeRunControlPlane(&ordinary, &cfg, AgentMode{Name: "Builder"}, true); err != nil {
		t.Fatal(err)
	}
	app.recordControlledToolOutcome(&ordinary, false, llm.ToolCallDef{Function: llm.FunctionCall{Name: "todo_write", Arguments: json.RawMessage(`{"action":"replace","items":["Update","Verify"]}`)}}, nil)
	writeCall := llm.ToolCallDef{Function: llm.FunctionCall{Name: "write", Arguments: json.RawMessage(`{"path":"report.md","content":"changed"}`)}}
	if controlled, blocked := app.prepareControlledTool(&ordinary, writeCall); !controlled || !strings.Contains(blocked, "explicit Fixer") {
		t.Fatalf("ordinary post-handoff write = controlled %t blocked %q", controlled, blocked)
	}

	repair := startTaskRun("fix report.md and verify the correction", "Fixer", "profile", "model")
	if err := app.initializeRunControlPlane(&repair, &cfg, AgentMode{Name: "Fixer"}, true); err != nil {
		t.Fatal(err)
	}
	if len(repair.Contract.RepairScope) != 1 || !strings.EqualFold(repair.Contract.RepairScope[0], path) {
		t.Fatalf("repair scope = %#v", repair.Contract.RepairScope)
	}
	app.recordControlledToolOutcome(&repair, false, llm.ToolCallDef{Function: llm.FunctionCall{Name: "todo_write", Arguments: json.RawMessage(`{"action":"replace","items":["Repair","Verify"]}`)}}, nil)
	if controlled, blocked := app.prepareControlledTool(&repair, writeCall); !controlled || blocked != "" {
		t.Fatalf("scoped Fixer write = controlled %t blocked %q", controlled, blocked)
	}
}

func TestFinalizedArtifactShellMutationCannotBypassRepairScope(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "poc.py")
	contract, err := controlplane.NewTaskContract(controlplane.ContractInput{
		RunID: "current", Objective: "update something else", WorkspaceRoot: root,
		ProtectedArtifacts: []controlplane.ArtifactBoundary{{Path: path, SHA256: strings.Repeat("a", 64), SourceRunID: "prior", Generation: 9}},
		Risk:               controlplane.RiskMedium, InstructionRevision: 1, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	run := TaskRun{ID: "current", Mode: "Builder", Contract: &contract, Control: &controlplane.MachineState{Version: 1, ContractDigest: contract.Digest, ContractRevision: 1, Phase: controlplane.PhaseActing, Revision: 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	call := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(`{"command":"sed -i 's/old/new/' poc.py"}`)}}
	if controlled, blocked := (&App{}).prepareControlledTool(&run, call); !controlled || !strings.Contains(blocked, "explicit Fixer") {
		t.Fatalf("shell bypass = controlled %t blocked %q", controlled, blocked)
	}
	scriptCall := llm.ToolCallDef{Function: llm.FunctionCall{Name: "run_script", Arguments: json.RawMessage(`{"code":"open('poc.py', 'w').write('changed')"}`)}}
	if controlled, blocked := (&App{}).prepareControlledTool(&run, scriptCall); !controlled || !strings.Contains(blocked, "explicit Fixer") {
		t.Fatalf("run_script bypass = controlled %t blocked %q", controlled, blocked)
	}
	readScript := llm.ToolCallDef{Function: llm.FunctionCall{Name: "run_script", Arguments: json.RawMessage(`{"code":"print(read('poc.py'))"}`)}}
	if controlled, blocked := (&App{}).prepareControlledTool(&run, readScript); controlled || blocked != "" {
		t.Fatalf("read-only run_script = controlled %t blocked %q", controlled, blocked)
	}
}

func TestScopedFinalizedArtifactRepairCreatesFreshHandoffAndKeepsSiblingImmutable(t *testing.T) {
	root := t.TempDir()
	restoreWorkingDir(t)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repairPath := filepath.Join(root, "report.md")
	siblingPath := filepath.Join(root, "poc.py")
	if err := os.WriteFile(repairPath, []byte("validated report\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(siblingPath, []byte("print('validated')\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	repairEvidence, repairEvidencePath := mutationEvidenceIDForPath(repairPath)
	siblingEvidence, siblingEvidencePath := mutationEvidenceIDForPath(siblingPath)
	_, repairDigest, repairOK := parseFileEvidenceID(repairEvidence)
	_, siblingDigest, siblingOK := parseFileEvidenceID(siblingEvidence)
	if !repairOK || !siblingOK {
		t.Fatalf("could not fingerprint fixtures: repair=%q sibling=%q", repairEvidence, siblingEvidence)
	}
	prior := TaskRun{
		ID: "task-handoff-prior", Generation: 40, ConversationEpoch: 6, Status: "done",
		StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
		FinalizedArtifacts: []FinalizedArtifact{
			{Path: repairEvidencePath, SHA256: repairDigest, RunID: "task-handoff-prior", Generation: 40, ConversationEpoch: 6, EvidenceID: repairEvidence},
			{Path: siblingEvidencePath, SHA256: siblingDigest, RunID: "task-handoff-prior", Generation: 40, ConversationEpoch: 6, EvidenceID: siblingEvidence},
		},
	}
	if err := saveTaskRunDB(db, prior, nil); err != nil {
		t.Fatal(err)
	}

	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(root)
	app := &App{db: db, suppressEvents: true}
	repair := startTaskRun("fix report.md and verify the corrected handoff", "Fixer", "profile", "model")
	repair.ConversationEpoch = 7
	if err := app.initializeRunControlPlane(&repair, &cfg, AgentMode{Name: "Fixer"}, true); err != nil {
		t.Fatal(err)
	}
	if len(repair.Contract.RepairScope) != 1 || !strings.EqualFold(repair.Contract.RepairScope[0], repairPath) {
		t.Fatalf("repair scope = %#v, want only %s", repair.Contract.RepairScope, repairPath)
	}
	app.recordControlledToolOutcome(&repair, false, llm.ToolCallDef{Function: llm.FunctionCall{Name: "todo_write", Arguments: json.RawMessage(`{"action":"replace","items":["Repair report.md","Verify corrected bytes"]}`)}}, nil)

	repairCall := llm.ToolCallDef{Function: llm.FunctionCall{Name: "write", Arguments: json.RawMessage(`{"path":"report.md","content":"corrected report\\n"}`)}}
	controlled, blocked := app.prepareControlledTool(&repair, repairCall)
	if !controlled || blocked != "" {
		t.Fatalf("scoped repair was blocked: controlled=%t detail=%q", controlled, blocked)
	}
	siblingCall := llm.ToolCallDef{Function: llm.FunctionCall{Name: "write", Arguments: json.RawMessage(`{"path":"poc.py","content":"silently changed\\n"}`)}}
	if siblingControlled, siblingBlocked := app.prepareControlledTool(&repair, siblingCall); !siblingControlled || !strings.Contains(siblingBlocked, "not in this Fixer run's repair scope") {
		t.Fatalf("unscoped sibling mutation was not blocked: controlled=%t detail=%q", siblingControlled, siblingBlocked)
	}
	if err := os.WriteFile(repairPath, []byte("corrected report\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	app.recordControlledToolOutcome(&repair, controlled, repairCall, nil)
	if err := app.requestControlVerification(&repair); err != nil {
		t.Fatal(err)
	}
	if err := app.passControlVerification(&repair, nil); err != nil {
		t.Fatal(err)
	}

	if len(repair.FinalizedArtifacts) != 1 {
		t.Fatalf("repair handoff = %#v, want exactly one newly finalized artifact", repair.FinalizedArtifacts)
	}
	handoff := repair.FinalizedArtifacts[0]
	if handoff.Path != repairEvidencePath || handoff.RunID != repair.ID || handoff.Generation != repair.Generation || handoff.ConversationEpoch != repair.ConversationEpoch || !handoff.Fresh || handoff.SHA256 == repairDigest {
		t.Fatalf("new repair handoff did not bind corrected bytes: %#v", handoff)
	}
	if siblingBytes, err := os.ReadFile(siblingPath); err != nil || string(siblingBytes) != "print('validated')\n" {
		t.Fatalf("protected sibling changed: err=%v bytes=%q", err, siblingBytes)
	}
	if refreshed := refreshFinalizedArtifactFreshness(repair.FinalizedArtifacts); len(refreshed) != 1 || !refreshed[0].Fresh || refreshed[0].Freshness != "fresh" {
		t.Fatalf("new handoff is not fresh: %#v", refreshed)
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
