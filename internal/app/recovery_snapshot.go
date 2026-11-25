package app

import (
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

// This uses the existing checkpoint/TaskRun tables in one SQLite transaction.
// A successful pre-effect commit is required before the native executor runs.
func (a *App) checkpointBeforeExternalWork(run *TaskRun, cfg settings.Settings) error {
	if a == nil || a.db == nil {
		if a != nil && a.ctx != nil && !a.suppressEvents {
			return fmt.Errorf("durable recovery store is unavailable")
		}
		return nil // model-free/headless fixtures have no native process to recover.
	}
	if err := validateCompletionContract(run); err != nil {
		return err
	}
	a.mu.Lock()
	messages := a.history.Messages()
	name := a.activeConversationName
	a.mu.Unlock()
	cp := RunCheckpoint{RunID: run.ID, ConversationName: name, ConversationMode: normalizeConversationMode(run.conversationMode), Prompt: run.Prompt, Mode: run.Mode, Profile: cfg.ActiveProfile, Messages: messages, Run: *run, SavedAt: time.Now().UTC().Format(time.RFC3339Nano), RecoveryVersion: 1}
	payload, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceTaskRunTx(tx, *run); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO run_checkpoints (run_id,prompt,mode,profile,payload,saved_at) VALUES (?,?,?,?,?,?) ON CONFLICT(run_id) DO UPDATE SET prompt=excluded.prompt,mode=excluded.mode,profile=excluded.profile,payload=excluded.payload,saved_at=excluded.saved_at`, cp.RunID, cp.Prompt, cp.Mode, cp.Profile, string(payload), cp.SavedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func recoveryToolMayHaveEffects(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "glob", "grep", "read_tool_result", "code_outline", "find_symbol", "todo_write":
		return false
	default:
		return true // unknown, shell, network, helper and mutation tools fail closed.
	}
}

func (a *App) checkpointToolIntent(run *TaskRun, cfg settings.Settings, call llm.ToolCallDef) error {
	if !recoveryToolMayHaveEffects(call.Function.Name) {
		return nil
	}
	hash := sha256.Sum256(call.Function.Arguments)
	run.addEvent("recovery_effect_intent", call.Function.Name, "call="+call.ID+" args_sha256="+hex.EncodeToString(hash[:]))
	return a.checkpointBeforeExternalWork(run, cfg)
}

func checkpointResumeBlock(cp RunCheckpoint) string {
	if !cp.Explicit && cp.RecoveryVersion != 1 {
		return "This legacy snapshot has no pre-action recovery proof. Review its files and start a new explicitly scoped task instead."
	}
	if cp.Run.Control != nil && cp.Run.Control.Phase == controlplane.PhaseComplete {
		return "This snapshot already reached verification completion. Open the saved conversation; do not replay its actions."
	}
	for _, event := range cp.Run.Events {
		if event.Kind == "recovery_effect_intent" {
			return "An interrupted side-effecting action may already have run. Automatic replay is blocked to avoid duplicate changes; review the retained evidence and start a new explicitly scoped repair or verification task."
		}
	}
	for _, tool := range cp.Run.Tools {
		if recoveryToolMayHaveEffects(tool.Name) && tool.Status != "blocked" && tool.Status != "denied" && tool.Status != "disabled" {
			return "This checkpoint contains prior side effects. Reconcile them before starting new work; replay is not safe."
		}
	}
	for _, message := range cp.Messages {
		for _, call := range message.ToolCalls {
			if recoveryToolMayHaveEffects(call.Function.Name) {
				return "The saved conversation includes side-effecting tool calls. Review them before explicitly starting new work."
			}
		}
	}
	return ""
}

func (a *App) checkpointResumeReadiness(cp RunCheckpoint) string {
	if reason := checkpointResumeBlock(cp); reason != "" {
		return reason
	}
	if cp.Explicit {
		return ""
	}
	rows, err := a.listTaskRuns()
	if err != nil {
		return "The original durable run record could not be read; automatic replay is not safe."
	}
	for _, row := range rows {
		if row.ID != cp.RunID {
			continue
		}
		if row.Generation != cp.Run.Generation || row.Contract == nil || cp.Run.Contract == nil || row.Contract.Digest != cp.Run.Contract.Digest {
			return "The original execution contract has changed; automatic replay is not safe."
		}
		if row.Control != nil && row.Control.Phase == controlplane.PhaseComplete {
			return "The original run already passed verification. Review the retained answer/files instead of replaying it."
		}
		return checkpointResumeBlock(RunCheckpoint{Run: row, Messages: cp.Messages, RecoveryVersion: 1})
	}
	return "The original durable run record is unavailable. Review retained evidence and start a new explicitly scoped task."
}

func prepareCheckpointRun(cp RunCheckpoint, cfg settings.Settings, mode, model string) (TaskRun, error) {
	run := resumedTaskRun(cp, mode, cfg.ActiveProfile, model)
	if cp.Explicit && cp.Run.Contract == nil {
		return run, nil
	}
	if cp.Run.Contract == nil || cp.Run.Control == nil || cp.RunID != cp.Run.ID {
		return TaskRun{}, fmt.Errorf("checkpoint execution identity is unavailable")
	}
	if err := validateCompletionContract(&cp.Run); err != nil {
		return TaskRun{}, err
	}
	if !sameFilesystemPath(cp.Run.Contract.WorkspaceRoot, cfg.Context.WorkspaceDir) {
		return TaskRun{}, fmt.Errorf("select the checkpoint's original workspace before recovery")
	}
	contract, err := controlplane.ResumeTaskContract(*cp.Run.Contract, run.ID, time.Now())
	if err != nil {
		return TaskRun{}, err
	}
	state, err := controlplane.NewMachineState(contract, time.Now())
	if err != nil {
		return TaskRun{}, err
	}
	state, err = state.Apply(controlplane.Event{Kind: controlplane.EventContractValidated, Detail: "recovered original task contract"})
	if err != nil {
		return TaskRun{}, err
	}
	if cp.Run.Control.PlanAccepted {
		state, err = state.Apply(controlplane.Event{Kind: controlplane.EventPlanAccepted, Detail: "original admitted plan retained; no action was started"})
	}
	if err != nil {
		return TaskRun{}, err
	}
	run.Contract, run.Control = &contract, &state
	return run, nil
}

func (a *App) commitRecoveryTerminal(run TaskRun, retire bool) error {
	if a.db == nil {
		return a.saveTaskRun(run, nil)
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceTaskRunTx(tx, run); err != nil {
		return err
	}
	if retire {
		if _, err := tx.Exec(`DELETE FROM run_checkpoints WHERE run_id=?`, run.ID); err != nil {
			return err
		}
		if run.ParentRunID != "" && !run.persistentCheckpoint {
			if _, err := tx.Exec(`DELETE FROM run_checkpoints WHERE run_id=?`, run.ParentRunID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (a *App) GetActiveConversationName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.activeConversationName
}

func (a *App) recoveryConversationName(cp RunCheckpoint) (string, error) {
	a.sessionLibraryMu.Lock()
	defer a.sessionLibraryMu.Unlock()
	base, err := cleanSessionName("Recovered-" + firstNonEmpty(cp.ConversationName, cp.Name, cp.RunID))
	if err != nil {
		return "", err
	}
	if len(base) > 70 {
		base = base[:70]
	}
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	for index := 1; index <= 1000; index++ {
		name := base
		if index > 1 {
			name = fmt.Sprintf("%s-%d", base, index)
		}
		if _, err := os.Stat(filepath.Join(dir, name+".json")); os.IsNotExist(err) {
			return name, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("recovery conversation namespace exhausted")
}
