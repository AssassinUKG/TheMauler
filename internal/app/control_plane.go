package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/controlplane"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

var errControlPlanRequired = fmt.Errorf("control plane requires an accepted plan before completion")

func (a *App) initializeRunControlPlane(run *TaskRun, cfg *settings.Settings, mode AgentMode, autonomous bool) error {
	if run == nil || cfg == nil {
		return fmt.Errorf("run and settings are required")
	}
	if run.Contract != nil || run.Control != nil {
		if run.Contract == nil || run.Control == nil {
			return fmt.Errorf("checkpoint contains an incomplete control-plane snapshot")
		}
		if err := run.Contract.Validate(); err != nil {
			return fmt.Errorf("task contract: %w", err)
		}
		if err := run.Control.Validate(); err != nil {
			return fmt.Errorf("control state: %w", err)
		}
		if run.Contract.RunID != run.ID || run.Control.ContractDigest != run.Contract.Digest {
			return fmt.Errorf("control-plane snapshot does not belong to run %s", run.ID)
		}
		run.addEvent("control_resume", "Restored task contract and control phase", controlPlaneEventDetail(*run.Control, *run.Control))
		return nil
	}

	contract, err := buildTaskContract(*run, *cfg, mode, autonomous)
	if err != nil {
		return err
	}
	state, err := controlplane.NewMachineState(contract, time.Now())
	if err != nil {
		return err
	}
	run.Contract = &contract
	run.Control = &state
	run.addEvent("task_contract", "Validated and sealed task contract", fmt.Sprintf("revision=%d digest=%s risk=%s plan_required=%t blocking_checks=%s", contract.Revision, contract.Digest, contract.Risk, contract.PlanRequired, strings.Join(contract.BlockingCheckIDs(), ",")))
	if err := a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventContractValidated, Detail: "validated task contract"}); err != nil {
		return err
	}
	if !contract.PlanRequired {
		if err := a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventPlanAccepted, Detail: "contract permits direct action without a separate plan"}); err != nil {
			return err
		}
	}
	return nil
}

func buildTaskContract(run TaskRun, cfg settings.Settings, mode AgentMode, autonomous bool) (controlplane.TaskContract, error) {
	workspace := strings.TrimSpace(cfg.Context.WorkspaceDir)
	if workspace == "" {
		workspace = mustGetwd()
	}
	absWorkspace, err := filepath.Abs(filepath.FromSlash(workspace))
	if err != nil {
		return controlplane.TaskContract{}, fmt.Errorf("resolve workspace root: %w", err)
	}

	concrete := promptImpliesConcreteDeliverable(run.Prompt) && !promptLooksReadOnly(run.Prompt)
	planRequired := cfg.Agents.RequirePlan && concrete
	risk := controlplane.RiskLow
	if concrete {
		risk = controlplane.RiskMedium
	}
	lowerPrompt := strings.ToLower(run.Prompt)
	if hasAny(lowerPrompt, "delete ", "remove ", "credential", "exploit", "reverse shell", "privilege escalation") {
		risk = controlplane.RiskHigh
	}

	var deliverables []controlplane.Deliverable
	var checks []controlplane.AcceptanceCheck
	var requiredEvidence []string
	var mutationRules []controlplane.PathRule
	constraints := []string{"Do not mutate outside the authoritative workspace or an explicitly approved scope."}
	if concrete {
		deliverables = append(deliverables, controlplane.Deliverable{
			ID: "primary", Description: strings.TrimSpace(run.Prompt), Kind: "workspace_change",
		})
		mutationRules = append(mutationRules, controlplane.PathRule{Root: absWorkspace, Access: "write"})
		checks = append(checks, controlplane.AcceptanceCheck{
			ID: "mutation_postcondition", Description: "The workspace-change deliverable has at least one content-addressed final file mutation.",
			Verifier: "mutation_verifier", Blocking: true, EvidenceKinds: []string{"file_sha256"},
		})
		requiredEvidence = append(requiredEvidence, "file_sha256")
		if len(detectVerifyCommands(cfg)) > 0 {
			checks = append(checks, controlplane.AcceptanceCheck{
				ID: "project_verification", Description: "Deterministic project build/test checks pass after the final mutation.",
				Verifier: "verify_gate", Blocking: true, EvidenceKinds: []string{"command_verdict"},
			})
			requiredEvidence = append(requiredEvidence, "command_verdict")
		}
	}
	for _, protected := range cfg.Tools.ProtectedPaths {
		constraints = append(constraints, "Protected resource must not be mutated: "+protected)
	}
	approvalPolicy := "exact_normalized_action"
	if autonomous {
		approvalPolicy = "tool_policy_and_scope_gates"
	}
	return controlplane.NewTaskContract(controlplane.ContractInput{
		RunID:               run.ID,
		Objective:           run.Prompt,
		WorkspaceRoot:       absWorkspace,
		Deliverables:        deliverables,
		Constraints:         constraints,
		ProtectedResources:  cfg.Tools.ProtectedPaths,
		AllowedMutations:    mutationRules,
		AcceptanceChecks:    checks,
		RequiredEvidence:    requiredEvidence,
		Risk:                risk,
		InstructionRevision: 1,
		PlanRequired:        planRequired,
		Budgets: controlplane.RunBudgets{
			MaxToolCalls: cfg.Agents.MaxToolCalls, MaxRunSeconds: cfg.Agents.MaxRunSeconds,
		},
		ApprovalPolicy:   approvalPolicy,
		CompletionPolicy: "evidence_owned",
		CreatedAt:        time.Now(),
	})
}

func (a *App) applyControlEvent(run *TaskRun, event controlplane.Event) error {
	if run == nil || run.Control == nil {
		return fmt.Errorf("control state is unavailable")
	}
	previous := *run.Control
	next, err := previous.Apply(event)
	if err != nil {
		run.addEvent("control_transition_denied", string(event.Kind), fmt.Sprintf("phase=%s error=%v", previous.Phase, err))
		return err
	}
	run.Control = &next
	run.addEvent("control_transition", string(event.Kind), controlPlaneEventDetail(previous, next))
	a.persistControlRun(*run)
	if a != nil && a.ctx != nil {
		a.emit("mauler:control_phase", map[string]any{
			"id": run.ID, "phase": next.Phase, "revision": next.Revision,
			"contract_revision": next.ContractRevision, "detail": strings.TrimSpace(event.Detail),
		})
	}
	return nil
}

func (a *App) persistControlRun(run TaskRun) {
	if a == nil || a.db == nil || run.Contract == nil || run.Control == nil {
		return
	}
	_ = saveTaskRunDB(a.db, run, nil)
}

func controlPlaneEventDetail(previous, next controlplane.MachineState) string {
	return fmt.Sprintf("from=%s to=%s machine_revision=%d contract_revision=%d contract_digest=%s", previous.Phase, next.Phase, next.Revision, next.ContractRevision, next.ContractDigest)
}

func (a *App) prepareControlledTool(run *TaskRun, tc llm.ToolCallDef) (bool, string) {
	if run == nil || run.Control == nil {
		return false, "control state is unavailable"
	}
	decision := run.Control.CheckTool(tc.Function.Name)
	if !decision.Controlled {
		return false, ""
	}
	if run.Control.Phase == controlplane.PhaseRepairing {
		if err := a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventRepairReady, Detail: "model selected a targeted repair action"}); err != nil {
			return true, err.Error()
		}
		decision = run.Control.CheckTool(tc.Function.Name)
	}
	if !decision.Allowed {
		detail := decision.Reason
		if run.Control.Phase == controlplane.PhasePlanning && run.Control.PlanRequired && !run.Control.PlanAccepted {
			detail += ". Create the run plan with todo_write action=replace and non-empty items, then retry the action."
		}
		run.addEvent("control_tool_denied", tc.Function.Name, detail)
		return true, detail
	}
	if run.Control.Phase == controlplane.PhaseActing {
		if err := a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventActionStarted, Detail: tc.Function.Name}); err != nil {
			return true, err.Error()
		}
	}
	return true, ""
}

func (a *App) recordControlledToolOutcome(run *TaskRun, controlled bool, tc llm.ToolCallDef, runErr error) {
	if run == nil || run.Control == nil {
		return
	}
	if !controlled {
		if runErr == nil && acceptedPlanCall(tc) && run.Control.Phase == controlplane.PhasePlanning {
			_ = a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventPlanAccepted, Detail: "todo_write replaced the active plan with non-empty steps"})
		}
		return
	}
	if run.Control.Phase != controlplane.PhaseActing {
		return
	}
	if runErr != nil {
		_ = a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventActionFailed, Detail: fmt.Sprintf("%s: %v", tc.Function.Name, runErr)})
		return
	}
	if isWriteTool(strings.ToLower(strings.TrimSpace(tc.Function.Name))) {
		if id, path := mutationEvidenceIDForCall(tc); id != "" {
			run.addEvent("control_evidence", id, path)
		}
	}
	if err := a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventActionSucceeded, Detail: tc.Function.Name}); err != nil {
		return
	}
	_ = a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventObservationRecorded, Detail: "tool result recorded in TaskRun and RunLedger"})
}

func acceptedPlanCall(tc llm.ToolCallDef) bool {
	if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), "todo_write") {
		return false
	}
	var input struct {
		Action string            `json:"action"`
		Items  []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(tc.Function.Arguments, &input); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(input.Action), "replace") && len(input.Items) > 0
}

func (a *App) enterControlApproval(run *TaskRun, tool string) error {
	if run == nil || run.Control == nil {
		return fmt.Errorf("control state is unavailable")
	}
	return a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventApprovalRequired, Detail: tool})
}

func (a *App) leaveControlApproval(run *TaskRun, approved bool, tool string) error {
	kind := controlplane.EventApprovalDenied
	if approved {
		kind = controlplane.EventApprovalGranted
	}
	return a.applyControlEvent(run, controlplane.Event{Kind: kind, Detail: tool})
}

func controlPlanePrompt(run TaskRun) string {
	if run.Contract == nil || run.Control == nil {
		return ""
	}
	missing := run.Control.MissingBlockingChecks()
	allowed := "read context or create the plan"
	switch run.Control.Phase {
	case controlplane.PhaseActing:
		allowed = "select and execute one in-scope tool action"
	case controlplane.PhaseRepairing:
		allowed = "select one targeted repair action; completion is illegal"
	case controlplane.PhaseVerifying:
		allowed = "wait for controller-owned verification"
	}
	return fmt.Sprintf("[control_plane]\ncontract_revision: %d\ncontract_digest: %s\nphase: %s\nplan_required: %t\nplan_accepted: %t\nrisk: %s\nobjective: %s\nallowed_next: %s\nblocking_checks: %s\nThe Go control plane is authoritative. Assistant prose is not completion evidence.",
		run.Contract.Revision, run.Contract.Digest, run.Control.Phase, run.Contract.PlanRequired, run.Control.PlanAccepted,
		run.Contract.Risk, run.Contract.Objective, allowed, strings.Join(missing, ", "))
}

func constrainControlPlanningTools(run TaskRun, defs []llm.ToolDef, choice string) ([]llm.ToolDef, string, bool) {
	if run.Control == nil || run.Control.Phase != controlplane.PhasePlanning || !run.Control.PlanRequired || run.Control.PlanAccepted {
		return defs, choice, false
	}
	constrained := filterToolDefsByName(defs,
		"todo_write", "read", "glob", "grep", "read_pdf", "session_search", "memory", "skill", "read_tool_result", "evidence_bundle",
	)
	if len(constrained) == 0 {
		return defs, choice, false
	}
	return constrained, "required", len(constrained) != len(defs) || choice != "required"
}

func controlSatisfiedChecks(run TaskRun, verdicts []VerifyVerdict) map[string][]string {
	satisfied := map[string][]string{}
	if run.Contract == nil {
		return satisfied
	}
	for _, check := range run.Contract.AcceptanceChecks {
		if !check.Blocking {
			continue
		}
		switch check.ID {
		case "mutation_postcondition":
			if evidence := mutationEvidenceIDs(run); len(evidence) > 0 {
				satisfied[check.ID] = evidence
			}
		case "project_verification":
			if evidence := verificationEvidenceIDs(verdicts); len(evidence) > 0 && len(blockingReviewVerdicts(verdicts)) == 0 {
				satisfied[check.ID] = evidence
			}
		}
	}
	return satisfied
}

func mutationEvidenceIDs(run TaskRun) []string {
	seen := map[string]bool{}
	var out []string
	for _, event := range run.Events {
		if event.Kind != "control_evidence" || !strings.HasPrefix(event.Message, "file_sha256:") || seen[event.Message] {
			continue
		}
		currentID, _ := mutationEvidenceIDForPath(event.Detail)
		if currentID != event.Message {
			continue
		}
		seen[event.Message] = true
		out = append(out, event.Message)
	}
	return out
}

func mutationEvidenceIDForCall(tc llm.ToolCallDef) (string, string) {
	path := pathFromToolInput(string(tc.Function.Arguments))
	return mutationEvidenceIDForPath(path)
}

func mutationEvidenceIDForPath(path string) (string, string) {
	if path == "" {
		return "", ""
	}
	path = filepath.Clean(filepath.FromSlash(path))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	sum := sha256.Sum256(data)
	return "file_sha256:" + filepath.ToSlash(path) + ":" + hex.EncodeToString(sum[:]), filepath.ToSlash(path)
}

func verificationEvidenceIDs(verdicts []VerifyVerdict) []string {
	var out []string
	for _, verdict := range verdicts {
		if !strings.EqualFold(strings.TrimSpace(verdict.Status), "pass") {
			continue
		}
		gate := strings.ToLower(strings.TrimSpace(verdict.Gate))
		if gate != "build" && gate != "test" && gate != "verify" && gate != "lint" {
			continue
		}
		data, _ := json.Marshal(verdict)
		sum := sha256.Sum256(data)
		out = append(out, "command_verdict:"+gate+":"+hex.EncodeToString(sum[:]))
	}
	return out
}

func controlNeedsProjectVerification(run TaskRun) bool {
	if run.Contract == nil {
		return false
	}
	for _, check := range run.Contract.AcceptanceChecks {
		if check.Blocking && check.ID == "project_verification" {
			return true
		}
	}
	return false
}

func (a *App) ensureControlVerification(ctx context.Context, run *TaskRun, cfg *settings.Settings, verdicts []VerifyVerdict) []VerifyVerdict {
	if run == nil || cfg == nil || !controlNeedsProjectVerification(*run) || len(verificationEvidenceIDs(verdicts)) > 0 {
		return verdicts
	}
	controlVerdicts := a.runVerifyGate(ctx, run, cfg)
	if len(controlVerdicts) > 0 {
		recordReviewGateEvent(run, -1, controlVerdicts)
		verdicts = append(verdicts, controlVerdicts...)
	}
	return verdicts
}

func (a *App) requestControlVerification(run *TaskRun) error {
	if run == nil || run.Control == nil {
		return fmt.Errorf("control state is unavailable")
	}
	switch run.Control.Phase {
	case controlplane.PhasePlanning:
		if run.Control.PlanRequired && !run.Control.PlanAccepted {
			return errControlPlanRequired
		}
	case controlplane.PhaseRepairing:
		if err := a.applyControlEvent(run, controlplane.Event{
			Kind:   controlplane.EventRepairConcluded,
			Detail: "model concluded the repair attempt; controller will re-run all blocking checks",
		}); err != nil {
			return err
		}
	case controlplane.PhaseObserving:
		if err := a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventObservationRecorded, Detail: "final tool observation recorded"}); err != nil {
			return err
		}
	}
	return a.applyControlEvent(run, controlplane.Event{Kind: controlplane.EventVerificationRequested, Detail: "model proposed completion; controller started acceptance checks"})
}

func controlCanVerifyAtToolBudget(run TaskRun) bool {
	if run.Control == nil || run.Control.Terminal() {
		return false
	}
	if run.Control.PlanRequired && !run.Control.PlanAccepted {
		return false
	}
	switch run.Control.Phase {
	case controlplane.PhaseActing, controlplane.PhaseObserving, controlplane.PhaseRepairing:
		return true
	default:
		return false
	}
}

func (a *App) passControlVerification(run *TaskRun, verdicts []VerifyVerdict) error {
	return a.applyControlEvent(run, controlplane.Event{
		Kind: controlplane.EventVerificationPassed, Detail: "all blocking acceptance checks have immutable evidence ids",
		SatisfiedChecks: controlSatisfiedChecks(*run, verdicts),
	})
}

func (a *App) failControlVerification(run *TaskRun, verdicts []VerifyVerdict, detail string) error {
	return a.applyControlEvent(run, controlplane.Event{
		Kind: controlplane.EventVerificationFailed, Detail: detail,
		SatisfiedChecks: controlSatisfiedChecks(*run, verdicts),
	})
}
