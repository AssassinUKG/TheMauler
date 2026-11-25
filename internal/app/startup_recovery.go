package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mauler/internal/controlplane"
	"mauler/internal/settings"
)

func (a *App) ownNativeRuntime() error {
	if a.runtimeOwnerErr != nil {
		return a.runtimeOwnerErr
	}
	if a.runtimeOwner != nil {
		return nil
	}
	owner, err := acquireNativeRuntimeOwner()
	if err != nil {
		return err
	}
	a.runtimeOwner = owner
	return nil
}

func acquireNativeRuntimeOwner() (*os.File, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	owner, err := acquireRuntimeOwnerFile(filepath.Join(dir, "runtime-owner.lock"))
	if err != nil {
		return nil, fmt.Errorf("Mauler's data directory is already in use or cannot be locked: %w", err)
	}
	return owner, nil
}

// Called only after acquiring the OS-released process ownership lock. A new
// native app cannot classify a still-live sibling's work as interrupted.
func (a *App) reconcileInterruptedNativeRuns() error {
	if a.runtimeOwner == nil || a.db == nil {
		return fmt.Errorf("owned durable runtime is unavailable")
	}
	runs, err := a.listTaskRuns()
	if err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, run := range runs {
		if run.Status != "running" {
			continue
		}
		run.Status, run.State = "stopped", "interrupted"
		run.StopReason = "process_interrupted"
		run.StopDetail = "The prior application process exited before committing a terminal handoff. No action was replayed; inspect Automatic recovery."
		run.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
		run.addEvent("process_interrupted", "Prior native process ended before terminal commit", "Recovery requires explicit admission; no model or tool was started.")
		if run.Control != nil && !run.Control.Terminal() {
			next, err := run.Control.Apply(controlplane.Event{Kind: controlplane.EventCancelled, Detail: run.StopDetail})
			if err != nil {
				return err
			}
			run.Control = &next
		}
		if err := replaceTaskRunTx(tx, run); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *App) joinNativeRunForShutdown() bool {
	a.mu.Lock()
	a.shuttingDown = true
	cancel, done := a.cancelAgent, a.agentDone
	if cancel != nil {
		a.stopReason = "application_shutdown"
		a.stopDetail = "Mauler is shutting down."
		cancel()
	}
	a.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return false
		}
	}
	return true
}
