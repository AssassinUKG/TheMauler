package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/sessionstore"
	"mauler/internal/settings"
)

type RunCheckpoint struct {
	RunID    string        `json:"run_id"`
	Prompt   string        `json:"prompt"`
	Mode     string        `json:"mode"`
	Profile  string        `json:"profile"`
	Messages []llm.Message `json:"messages"`
	Run      TaskRun       `json:"run"`
	SavedAt  string        `json:"saved_at"`
}

func (a *App) ListResumableRuns() ([]RunCheckpoint, error) {
	store := a.sessionStore()
	if store == nil {
		return nil, nil
	}
	records, err := store.ListCheckpoints()
	if err != nil {
		return nil, err
	}
	out := make([]RunCheckpoint, 0, len(records))
	for _, record := range records {
		cp, err := checkpointFromRecord(record)
		if err != nil {
			continue
		}
		out = append(out, cp)
	}
	return out, nil
}

func (a *App) ResumeRun(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run_id is required")
	}
	a.mu.Lock()
	if a.agentRunning {
		a.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	a.mu.Unlock()

	store := a.sessionStore()
	if store == nil {
		return fmt.Errorf("session store is unavailable")
	}
	record, ok, err := store.LoadCheckpoint(runID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("run checkpoint %q not found", runID)
	}
	cp, err := checkpointFromRecord(record)
	if err != nil {
		return err
	}

	a.mu.Lock()
	cfg := *a.cfg
	cloneToolsConfigRefs(&cfg.Tools)
	profiles := *a.profiles
	autonomous := a.autonomous
	a.agentRunning = true
	a.mu.Unlock()

	if strings.TrimSpace(cp.Profile) != "" {
		cfg.ActiveProfile = cp.Profile
	}
	profile := activeProfile(&cfg, &profiles)
	mode := applyPresetToMode(baseMode(cp.Mode), cfg.Agents.Presets)
	agentCtx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancelAgent = cancel
	a.stopReason = ""
	a.stopDetail = ""
	a.history = agent.NewHistory(profile.CtxTokens)
	a.history.Replace(cp.Messages)
	a.currentMode = mode.Name
	a.mu.Unlock()
	a.emit("mauler:agent_mode", mode.Name)

	run := cp.Run
	if strings.TrimSpace(run.ID) == "" {
		run = startTaskRun(cp.Prompt, mode.Name, cfg.ActiveProfile, profile.ModelID)
	}
	run.addEvent("resume", "Resuming run from checkpoint", cp.SavedAt)
	go a.runAgentLoop(agentCtx, llm.Message{}, profile, &cfg, autonomous, mode, nil, nil, run)
	return nil
}

func (a *App) maybeCheckpoint(run TaskRun, cfg settings.Settings, every int) {
	if every <= 0 || len(run.Tools) == 0 || len(run.Tools)%every != 0 {
		return
	}
	a.saveRunCheckpoint(run, cfg)
}

func (a *App) saveRunCheckpoint(run TaskRun, cfg settings.Settings) {
	store := a.sessionStore()
	if store == nil || strings.TrimSpace(run.ID) == "" {
		return
	}
	a.mu.Lock()
	messages := a.history.Messages()
	a.mu.Unlock()
	cp := RunCheckpoint{
		RunID:    run.ID,
		Prompt:   run.Prompt,
		Mode:     run.Mode,
		Profile:  cfg.ActiveProfile,
		Messages: messages,
		Run:      run,
		SavedAt:  time.Now().Format(time.RFC3339),
	}
	payload, err := json.Marshal(cp)
	if err != nil {
		run.addEvent("checkpoint_error", "Could not encode run checkpoint", err.Error())
		return
	}
	if err := store.SaveCheckpoint(sessionstore.CheckpointRecord{
		RunID:   cp.RunID,
		Prompt:  cp.Prompt,
		Mode:    cp.Mode,
		Profile: cp.Profile,
		Payload: string(payload),
		SavedAt: cp.SavedAt,
	}); err != nil {
		run.addEvent("checkpoint_error", "Could not save run checkpoint", err.Error())
	}
}

func (a *App) deleteRunCheckpoint(runID string) {
	if store := a.sessionStore(); store != nil {
		_ = store.DeleteCheckpoint(runID)
	}
}

func checkpointFromRecord(record sessionstore.CheckpointRecord) (RunCheckpoint, error) {
	var cp RunCheckpoint
	if err := json.Unmarshal([]byte(record.Payload), &cp); err != nil {
		return RunCheckpoint{}, err
	}
	if cp.RunID == "" {
		cp.RunID = record.RunID
	}
	if cp.Prompt == "" {
		cp.Prompt = record.Prompt
	}
	if cp.Mode == "" {
		cp.Mode = record.Mode
	}
	if cp.Profile == "" {
		cp.Profile = record.Profile
	}
	if cp.SavedAt == "" {
		cp.SavedAt = record.SavedAt
	}
	return cp, nil
}
