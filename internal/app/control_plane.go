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
	"mauler/internal/tools"
)

var errControlPlanRequired = fmt.Errorf("control plane requires an accepted plan before completion")

type runControlContextKey struct{}

func withRunControlContext(ctx context.Context, run *TaskRun) context.Context {
	if ctx == nil || run == nil {
		return ctx
	}
	return context.WithValue(ctx, runControlContextKey{}, run)
}

func runControlFromContext(ctx context.Context) *TaskRun {
	if ctx == nil {
		return nil
	}
	run, _ := ctx.Value(runControlContextKey{}).(*TaskRun)
	return run
}

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

	contract, err := a.buildTaskContract(*run, *cfg, mode, autonomous)
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

func (a *App) buildTaskContract(run TaskRun, cfg settings.Settings, mode AgentMode, autonomous bool) (controlplane.TaskContract, error) {
	workspace := strings.TrimSpace(cfg.Context.WorkspaceDir)
	if workspace == "" {
		workspace = mustGetwd()
	}
	absWorkspace, err := filepath.Abs(filepath.FromSlash(workspace))
	if err != nil {
		return controlplane.TaskContract{}, fmt.Errorf("resolve workspace root: %w", err)
	}

	// An explicit no-tool instruction is an answer-only contract. Even if the
	// requested prose contains mutation-shaped words such as "add a closing
	// line", it cannot authorize or require a workspace mutation.
	concrete := promptImpliesConcreteDeliverable(run.Prompt) &&
		!promptLooksReadOnly(run.Prompt) &&
		!explicitlyForbidsToolUse(run.Prompt)
	planRequired := cfg.Agents.RequirePlan && concrete
	risk := controlplane.RiskLow
	if concrete {
		risk = controlplane.RiskMedium
	}
	lowerPrompt := strings.ToLower(run.Prompt)
	if concrete && hasAny(lowerPrompt, "delete ", "remove ", "credential", "exploit", "reverse shell", "privilege escalation") {
		risk = controlplane.RiskHigh
	}

	var deliverables []controlplane.Deliverable
	var checks []controlplane.AcceptanceCheck
	var requiredEvidence []string
	var mutationRules []controlplane.PathRule
	constraints := []string{"Do not mutate outside the authoritative workspace or an explicitly approved scope."}
	protectedArtifacts := a.finalizedArtifactBoundaries(absWorkspace)
	repairScope := repairScopeForPrompt(run.Prompt, mode.Name, absWorkspace, protectedArtifacts)
	if len(protectedArtifacts) > 0 {
		constraints = append(constraints, "Earlier finalized artifacts are immutable unless this is an explicit Fixer run whose repair scope names the exact file.")
	}
	if strings.EqualFold(strings.TrimSpace(mode.Name), "Fixer") && len(protectedArtifacts) > 0 && len(repairScope) == 0 {
		constraints = append(constraints, "No finalized artifact is in repair scope. Name the exact handed-off file before changing it.")
	}
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
		ProtectedArtifacts:  protectedArtifacts,
		RepairScope:         repairScope,
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
		a.emitRun(run, "mauler:control_phase", map[string]any{
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
	if detail := guardFinalizedArtifactMutation(*run, tc); detail != "" {
		run.addEvent("control_artifact_scope_denied", tc.Function.Name, detail)
		return true, detail
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
		if id, path := mutationEvidenceIDForCall(run, tc); id != "" {
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
	repairScope := "none"
	if len(run.Contract.RepairScope) > 0 {
		repairScope = strings.Join(run.Contract.RepairScope, ", ")
	}
	return fmt.Sprintf("[control_plane]\ncontract_revision: %d\ncontract_digest: %s\nphase: %s\nplan_required: %t\nplan_accepted: %t\nrisk: %s\nobjective: %s\nallowed_next: %s\nblocking_checks: %s\nprotected_artifacts: %d\nrepair_scope: %s\nThe Go control plane is authoritative. Assistant prose is not completion evidence.",
		run.Contract.Revision, run.Contract.Digest, run.Control.Phase, run.Contract.PlanRequired, run.Control.PlanAccepted,
		run.Contract.Risk, run.Contract.Objective, allowed, strings.Join(missing, ", "), len(run.Contract.ProtectedArtifacts), repairScope)
}

func (a *App) finalizedArtifactBoundaries(workspace string) []controlplane.ArtifactBoundary {
	if a == nil || a.db == nil {
		return nil
	}
	runs, err := loadTaskRunsDB(a.db)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var boundaries []controlplane.ArtifactBoundary
	for _, prior := range runs {
		for _, artifact := range prior.FinalizedArtifacts {
			path, ok := artifactPathInWorkspace(workspace, artifact.Path)
			if !ok {
				continue
			}
			key := strings.ToLower(filepath.Clean(path))
			if seen[key] || artifact.RunID == "" || artifact.Generation == 0 || len(artifact.SHA256) != 64 {
				continue
			}
			seen[key] = true
			boundaries = append(boundaries, controlplane.ArtifactBoundary{
				Path: path, SHA256: strings.ToLower(artifact.SHA256), SourceRunID: artifact.RunID, Generation: artifact.Generation,
			})
		}
	}
	return boundaries
}

func artifactPathInWorkspace(workspace, supplied string) (string, bool) {
	workspace, err := filepath.Abs(tools.NormalizeHostPath(strings.TrimSpace(workspace)))
	if err != nil || workspace == "" {
		return "", false
	}
	path := tools.NormalizeHostPath(strings.TrimSpace(supplied))
	if path == "" || tools.ShouldUseWSLForPath(supplied) {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(workspace, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Clean(path), true
}

func repairScopeForPrompt(prompt, mode, workspace string, boundaries []controlplane.ArtifactBoundary) []string {
	if !strings.EqualFold(strings.TrimSpace(mode), "Fixer") {
		return nil
	}
	prompt = strings.ToLower(filepath.ToSlash(strings.TrimSpace(prompt)))
	var scope []string
	for _, boundary := range boundaries {
		path := filepath.ToSlash(boundary.Path)
		rel, _ := filepath.Rel(workspace, boundary.Path)
		candidates := []string{strings.ToLower(path), strings.ToLower(filepath.ToSlash(rel)), strings.ToLower(filepath.Base(path))}
		for _, candidate := range candidates {
			if candidate != "" && candidate != "." && strings.Contains(prompt, candidate) {
				scope = append(scope, boundary.Path)
				break
			}
		}
	}
	return scope
}

func guardFinalizedArtifactMutation(run TaskRun, tc llm.ToolCallDef) string {
	if run.Contract == nil || len(run.Contract.ProtectedArtifacts) == 0 {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(tc.Function.Name))
	if isWriteTool(name) {
		path, ok := artifactPathInWorkspace(run.Contract.WorkspaceRoot, pathFromToolInput(string(tc.Function.Arguments)))
		if !ok {
			return ""
		}
		if boundary, protected := protectedArtifactAtPath(run.Contract.ProtectedArtifacts, path); protected {
			return finalizedArtifactMutationBlock(run, boundary)
		}
		return ""
	}
	if name == "shell" || name == "terminal_send" {
		command := shellCommandFromToolArgs(tc.Function.Arguments)
		for _, boundary := range run.Contract.ProtectedArtifacts {
			if shellMutatesFinalizedArtifact(command, run.Contract.WorkspaceRoot, boundary.Path) {
				return finalizedArtifactMutationBlock(run, boundary)
			}
		}
	}
	if name == "run_script" {
		code := runScriptCodeFromToolArgs(tc.Function.Arguments)
		for _, boundary := range run.Contract.ProtectedArtifacts {
			if scriptMutatesFinalizedArtifact(code, run.Contract.WorkspaceRoot, boundary.Path) {
				return finalizedArtifactMutationBlock(run, boundary)
			}
		}
	}
	return ""
}

func protectedArtifactAtPath(boundaries []controlplane.ArtifactBoundary, path string) (controlplane.ArtifactBoundary, bool) {
	path = strings.ToLower(filepath.Clean(path))
	for _, boundary := range boundaries {
		if strings.ToLower(filepath.Clean(boundary.Path)) == path {
			return boundary, true
		}
	}
	return controlplane.ArtifactBoundary{}, false
}

func finalizedArtifactMutationBlock(run TaskRun, boundary controlplane.ArtifactBoundary) string {
	if strings.EqualFold(strings.TrimSpace(run.Mode), "Fixer") {
		for _, allowed := range run.Contract.RepairScope {
			if strings.EqualFold(filepath.Clean(allowed), filepath.Clean(boundary.Path)) {
				return ""
			}
		}
		return fmt.Sprintf("finalized artifact %s is not in this Fixer run's repair scope; name the exact file in the user request before changing it", filepath.ToSlash(boundary.Path))
	}
	return fmt.Sprintf("finalized artifact %s was handed off by run %s generation %d; start an explicit Fixer request naming this file before changing it", filepath.ToSlash(boundary.Path), boundary.SourceRunID, boundary.Generation)
}

func shellMutatesFinalizedArtifact(command, workspace, artifact string) bool {
	lower := strings.ToLower(filepath.ToSlash(command))
	if strings.TrimSpace(lower) == "" {
		return false
	}
	rel, _ := filepath.Rel(workspace, artifact)
	candidates := []string{strings.ToLower(filepath.ToSlash(artifact)), strings.ToLower(filepath.ToSlash(rel)), strings.ToLower(filepath.Base(artifact))}
	references := false
	for _, candidate := range candidates {
		if candidate != "" && candidate != "." && strings.Contains(lower, candidate) {
			references = true
			break
		}
	}
	if !references {
		return false
	}
	for _, marker := range []string{"rm ", "remove-item", "del ", "erase ", "mv ", "move-item", "cp ", "copy-item", "sed -i", "perl -pi", "truncate ", "touch ", "tee ", "set-content", "add-content", "out-file", "gofmt -w", "rustfmt "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, candidate := range candidates {
		if candidate != "" && (strings.Contains(lower, "> "+candidate) || strings.Contains(lower, ">"+candidate)) {
			return true
		}
	}
	return false
}

func runScriptCodeFromToolArgs(raw json.RawMessage) string {
	var input struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return ""
	}
	return input.Code
}

func scriptMutatesFinalizedArtifact(code, workspace, artifact string) bool {
	lower := strings.ToLower(filepath.ToSlash(code))
	if strings.TrimSpace(lower) == "" {
		return false
	}
	rel, _ := filepath.Rel(workspace, artifact)
	candidates := []string{strings.ToLower(filepath.ToSlash(artifact)), strings.ToLower(filepath.ToSlash(rel)), strings.ToLower(filepath.Base(artifact))}
	references := false
	for _, candidate := range candidates {
		if candidate != "" && candidate != "." && strings.Contains(lower, candidate) {
			references = true
			break
		}
	}
	if !references {
		return false
	}
	// run_script is an orchestration surface. Direct filesystem/process access
	// would bypass the inner Mauler tool calls and their control-plane checks, so
	// any such access to a sealed artifact must enter an explicit Fixer scope.
	for _, marker := range []string{
		"write(", "edit(", "open(", ".write_text(", ".write_bytes(",
		"os.remove", "os.unlink", "os.rename", "os.replace", "shutil.", "subprocess.",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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

func mutationEvidenceIDForCall(run *TaskRun, tc llm.ToolCallDef) (string, string) {
	path := pathFromToolInput(string(tc.Function.Arguments))
	if run != nil && run.Contract != nil {
		if canonical, ok := artifactPathInWorkspace(run.Contract.WorkspaceRoot, path); ok {
			path = canonical
		}
	}
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
	satisfied := controlSatisfiedChecks(*run, verdicts)
	if err := a.applyControlEvent(run, controlplane.Event{
		Kind: controlplane.EventVerificationPassed, Detail: "all blocking acceptance checks have immutable evidence ids",
		SatisfiedChecks: satisfied,
	}); err != nil {
		return err
	}
	run.FinalizedArtifacts = sealFinalizedArtifacts(*run, satisfied)
	a.persistControlRun(*run)
	return nil
}

func (a *App) failControlVerification(run *TaskRun, verdicts []VerifyVerdict, detail string) error {
	return a.applyControlEvent(run, controlplane.Event{
		Kind: controlplane.EventVerificationFailed, Detail: detail,
		SatisfiedChecks: controlSatisfiedChecks(*run, verdicts),
	})
}

func sealFinalizedArtifacts(run TaskRun, satisfied map[string][]string) []FinalizedArtifact {
	verifiers := append([]string(nil), satisfied["project_verification"]...)
	var sealed []FinalizedArtifact
	for _, evidenceID := range satisfied["mutation_postcondition"] {
		path, digest, ok := parseFileEvidenceID(evidenceID)
		if !ok {
			continue
		}
		info, err := os.Stat(filepath.FromSlash(path))
		if err != nil || info.IsDir() {
			continue
		}
		sealed = append(sealed, FinalizedArtifact{
			Path: path, SHA256: digest, Size: info.Size(), RunID: run.ID,
			Generation: run.Generation, ConversationEpoch: run.ConversationEpoch,
			EvidenceID: evidenceID, VerifierEvidenceIDs: verifiers,
			FinalizedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Fresh:       true, Freshness: "fresh", CurrentSHA256: digest,
		})
	}
	return sealed
}

func parseFileEvidenceID(id string) (path, digest string, ok bool) {
	const prefix = "file_sha256:"
	if !strings.HasPrefix(id, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, prefix)
	separator := strings.LastIndex(rest, ":")
	if separator <= 0 || separator == len(rest)-1 {
		return "", "", false
	}
	path, digest = rest[:separator], rest[separator+1:]
	if len(digest) != 64 {
		return "", "", false
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", "", false
	}
	return path, strings.ToLower(digest), true
}

func refreshFinalizedArtifactFreshness(artifacts []FinalizedArtifact) []FinalizedArtifact {
	for i := range artifacts {
		artifacts[i].Fresh = false
		artifacts[i].Freshness = "missing"
		artifacts[i].CurrentSHA256 = ""
		id, _ := mutationEvidenceIDForPath(artifacts[i].Path)
		_, digest, ok := parseFileEvidenceID(id)
		if !ok {
			continue
		}
		artifacts[i].CurrentSHA256 = digest
		if strings.EqualFold(digest, artifacts[i].SHA256) {
			artifacts[i].Fresh = true
			artifacts[i].Freshness = "fresh"
		} else {
			artifacts[i].Freshness = "changed"
		}
	}
	return artifacts
}
