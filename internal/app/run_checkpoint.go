package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/sessionstore"
	"mauler/internal/settings"
)

type RunCheckpoint struct {
	RunID            string               `json:"run_id"`
	Name             string               `json:"name,omitempty"`
	ConversationName string               `json:"conversation_name,omitempty"`
	ConversationMode string               `json:"conversation_mode,omitempty"`
	Explicit         bool                 `json:"explicit,omitempty"`
	Prompt           string               `json:"prompt"`
	Mode             string               `json:"mode"`
	Profile          string               `json:"profile"`
	Messages         []llm.Message        `json:"messages"`
	ChatMessages     []SessionChatMessage `json:"chat_messages,omitempty"`
	Run              TaskRun              `json:"run"`
	SavedAt          string               `json:"saved_at"`
}

func (a *App) ListResumableRuns() ([]RunCheckpoint, error) {
	store := a.sessionStore()
	if store == nil {
		return []RunCheckpoint{}, nil
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

// SaveConversationCheckpoint creates an operator-named, reusable snapshot of
// the current conversation. It uses the same immutable run-checkpoint store as
// automatic crash recovery, but is never consumed merely because it resumes.
func (a *App) SaveConversationCheckpoint(name, conversationName string) (RunCheckpoint, error) {
	name, err := validateCheckpointName(name)
	if err != nil {
		return RunCheckpoint{}, err
	}
	conversationName = strings.TrimSpace(conversationName)
	if len([]rune(conversationName)) > 120 {
		return RunCheckpoint{}, fmt.Errorf("conversation name is too long")
	}
	for _, r := range conversationName {
		if unicode.IsControl(r) {
			return RunCheckpoint{}, fmt.Errorf("conversation name contains control characters")
		}
	}

	a.mu.Lock()
	if a.agentRunning || a.evalRunning {
		a.mu.Unlock()
		return RunCheckpoint{}, fmt.Errorf("cannot checkpoint while an agent run or eval is active")
	}
	if a.history == nil || a.cfg == nil || a.profiles == nil {
		a.mu.Unlock()
		return RunCheckpoint{}, fmt.Errorf("conversation state is unavailable")
	}
	messages := a.history.Messages()
	cfg := *a.cfg
	profiles := *a.profiles
	modeName := firstNonEmpty(strings.TrimSpace(a.currentMode), "Auto")
	conversationMode := normalizeConversationMode(a.conversationMode)
	conversationEpoch := a.currentConversationEpoch()
	a.mu.Unlock()
	if len(messages) == 0 {
		return RunCheckpoint{}, fmt.Errorf("the current conversation is empty")
	}
	prompt := checkpointResumePrompt(messages)
	if prompt == "" {
		return RunCheckpoint{}, fmt.Errorf("the conversation has no user task to resume")
	}
	existingCheckpoints, err := a.ListResumableRuns()
	if err != nil {
		return RunCheckpoint{}, err
	}
	for _, existing := range existingCheckpoints {
		if existing.Explicit && strings.EqualFold(existing.Name, name) {
			return RunCheckpoint{}, fmt.Errorf("checkpoint %q already exists", name)
		}
	}

	profile := activeProfile(&cfg, &profiles)
	run := startTaskRun(prompt, modeName, cfg.ActiveProfile, profile.ModelID)
	run.Status = "paused"
	run.State = "checkpointed"
	run.Origin = "desktop"
	run.ConversationEpoch = conversationEpoch
	run.conversationMode = conversationMode
	cp := RunCheckpoint{
		RunID: run.ID, Name: name, ConversationName: conversationName,
		ConversationMode: conversationMode, Explicit: true,
		Prompt: prompt, Mode: modeName, Profile: cfg.ActiveProfile,
		Messages: messages, ChatMessages: toSessionChatMessages(messages), Run: run,
		SavedAt: time.Now().Format(time.RFC3339),
	}
	if err := a.persistRunCheckpoint(cp); err != nil {
		return RunCheckpoint{}, err
	}
	return cp, nil
}

func validateCheckpointName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("checkpoint name is required")
	}
	if len([]rune(name)) > 80 {
		return "", fmt.Errorf("checkpoint name must be 80 characters or fewer")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("checkpoint name contains control characters")
		}
	}
	return name, nil
}

func checkpointResumePrompt(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != llm.RoleUser {
			continue
		}
		if text := strings.TrimSpace(messageText(messages[i])); text != "" {
			return text
		}
	}
	return ""
}

func namedCheckpointResumeInstruction(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "Resume the saved conversation from its captured state. Reconcile the existing evidence first and do not repeat completed work unless verification requires it."
	}
	return "Resume the saved conversation from its captured state. Reconcile the existing evidence first and do not repeat completed work unless verification requires it.\n\nOriginal task: " + prompt
}

func (a *App) DeleteResumableRun(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run_id is required")
	}
	store := a.sessionStore()
	if store == nil {
		return fmt.Errorf("session store is unavailable")
	}
	return store.DeleteCheckpoint(runID)
}

func (a *App) ResumeRun(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run_id is required")
	}
	a.mu.Lock()
	if a.agentRunning || a.evalRunning {
		a.mu.Unlock()
		return fmt.Errorf("agent run or eval is already active")
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
	a.advanceConversationEpochLocked()
	conversationEpoch := a.currentConversationEpoch()
	a.history = agent.NewHistory(profile.CtxTokens)
	a.history.Replace(cp.Messages)
	a.currentMode = mode.Name
	a.conversationMode = normalizeConversationMode(cp.ConversationMode)
	a.mu.Unlock()
	a.emit("mauler:agent_mode", mode.Name)

	run := resumedTaskRun(cp, mode.Name, cfg.ActiveProfile, profile.ModelID)
	run.conversationMode = normalizeConversationMode(cp.ConversationMode)
	run.ConversationEpoch = conversationEpoch
	parentRunID := run.ParentRunID
	run.addEvent("resume", "Resuming checkpoint as a new owned run generation", fmt.Sprintf("parent_run_id=%s saved_at=%s", parentRunID, cp.SavedAt))
	firstMsg := llm.Message{}
	if cp.Explicit {
		firstMsg = llm.NewTextMessage(llm.RoleUser, namedCheckpointResumeInstruction(cp.Prompt))
	}
	go a.runAgentLoop(agentCtx, firstMsg, profile, &cfg, autonomous, mode, nil, nil, run)
	return nil
}

func resumedTaskRun(cp RunCheckpoint, mode, profile, model string) TaskRun {
	parentRunID := strings.TrimSpace(cp.Run.ID)
	if parentRunID == "" {
		parentRunID = strings.TrimSpace(cp.RunID)
	}
	run := startTaskRun(cp.Prompt, mode, profile, model)
	run.ParentRunID = parentRunID
	run.Origin = strings.TrimSpace(cp.Run.Origin)
	if run.Origin == "" {
		run.Origin = "desktop"
	}
	run.ClaimantID = cp.Run.ClaimantID
	run.ClaimantAlias = cp.Run.ClaimantAlias
	run.ContextPacketClass = cp.Run.ContextPacketClass
	run.persistentCheckpoint = cp.Explicit
	return run
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
		RunID:            run.ID,
		ConversationMode: normalizeConversationMode(run.conversationMode),
		Prompt:           run.Prompt,
		Mode:             run.Mode,
		Profile:          cfg.ActiveProfile,
		Messages:         messages,
		Run:              run,
		SavedAt:          time.Now().Format(time.RFC3339),
	}
	if err := a.persistRunCheckpoint(cp); err != nil {
		run.addEvent("checkpoint_error", "Could not save run checkpoint", err.Error())
	}
}

func (a *App) persistRunCheckpoint(cp RunCheckpoint) error {
	store := a.sessionStore()
	if store == nil {
		return fmt.Errorf("session store is unavailable")
	}
	persisted := cp
	persisted.ChatMessages = nil
	payload, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("encode run checkpoint: %w", err)
	}
	return store.SaveCheckpoint(sessionstore.CheckpointRecord{
		RunID:   cp.RunID,
		Prompt:  cp.Prompt,
		Mode:    cp.Mode,
		Profile: cp.Profile,
		Payload: string(payload),
		SavedAt: cp.SavedAt,
	})
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
	cp.ConversationMode = normalizeConversationMode(cp.ConversationMode)
	cp.ChatMessages = toSessionChatMessages(cp.Messages)
	return cp, nil
}
