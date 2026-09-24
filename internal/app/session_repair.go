package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/agent"
	"mauler/internal/ledger"
	"mauler/internal/llm"
)

// SessionRepairReport is safe to show in the UI. It describes structural
// changes only and does not expose message bodies or tool-result evidence.
type SessionRepairReport struct {
	Name           string               `json:"name"`
	Status         string               `json:"status"`
	Valid          bool                 `json:"valid"`
	BeforeMessages int                  `json:"before_messages"`
	AfterMessages  int                  `json:"after_messages"`
	Actions        []agent.RepairAction `json:"actions"`
	Diagnostic     string               `json:"diagnostic,omitempty"`
	Applied        bool                 `json:"applied"`
	BackupPath     string               `json:"backup_path,omitempty"`
}

// InspectSessionRepair previews the deterministic repair without changing the
// saved transcript, current conversation, recall index, or task state.
func (a *App) InspectSessionRepair(name string) (SessionRepairReport, error) {
	name, repaired, report, err := loadSessionRepair(name)
	_ = repaired
	if err != nil {
		return SessionRepairReport{}, err
	}
	return appSessionRepairReport(name, report), nil
}

// RepairSession applies a previously previewable deterministic repair. A
// non-JSON backup is retained beside the transcript and therefore never
// appears as another selectable conversation.
func (a *App) RepairSession(name string) (SessionRepairReport, error) {
	a.mu.Lock()
	running := a.agentRunning || a.evalRunning
	a.mu.Unlock()
	if running {
		return SessionRepairReport{}, fmt.Errorf("cannot repair a saved conversation while an agent run or eval is active")
	}

	name, repaired, report, err := loadSessionRepair(name)
	if err != nil {
		return SessionRepairReport{}, err
	}
	result := appSessionRepairReport(name, report)
	if !report.Valid {
		return result, fmt.Errorf("session repair rejected: %s", report.Diagnostic)
	}
	if len(report.Actions) == 0 {
		return result, nil
	}

	dir := mustSessionsDir()
	path := filepath.Join(dir, name+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		return result, err
	}
	backup := filepath.Join(dir, fmt.Sprintf("%s.pre-repair-%s.bak", name, time.Now().UTC().Format("20060102T150405.000000000Z")))
	if err := os.WriteFile(backup, original, 0o640); err != nil {
		return result, fmt.Errorf("write session repair backup: %w", err)
	}
	data, err := json.MarshalIndent(repaired, "", "  ")
	if err != nil {
		return result, fmt.Errorf("encode repaired session: %w", err)
	}
	data = append(data, '\n')
	if err := replaceSessionFile(path, data); err != nil {
		return result, err
	}

	if store := a.sessionStore(); store != nil {
		a.mu.Lock()
		cfg := *a.cfg
		profiles := *a.profiles
		a.mu.Unlock()
		model := activeProfile(&cfg, &profiles).ModelID
		if err := store.StoreSession(name, workspaceScope(), model, toSessionStoreMessages(repaired)); err != nil {
			return result, fmt.Errorf("refresh repaired session recall: %w", err)
		}
	}
	result.Applied = true
	result.BackupPath = backup
	a.recordSessionRepairLedger(name, "applied", report)
	return result, nil
}

func loadSessionRepair(name string) (string, []llm.Message, agent.RepairReport, error) {
	name, err := cleanSessionName(name)
	if err != nil {
		return "", nil, agent.RepairReport{}, err
	}
	data, err := os.ReadFile(filepath.Join(mustSessionsDir(), name+".json"))
	if err != nil {
		return name, nil, agent.RepairReport{}, err
	}
	var messages []llm.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return name, nil, agent.RepairReport{}, fmt.Errorf("decode saved session %q: %w", name, err)
	}
	repaired, report := agent.RepairMessagesReport(messages)
	if report.Valid && !sessionHasUserInstruction(repaired) {
		report.Status = "rejected"
		report.Valid = false
		report.Diagnostic = "saved conversation contains no user instruction after structural repair"
	}
	return name, repaired, report, nil
}

func sessionHasUserInstruction(messages []llm.Message) bool {
	for _, message := range messages {
		if message.Role != llm.RoleUser {
			continue
		}
		switch content := message.Content.(type) {
		case string:
			if strings.TrimSpace(content) != "" && !agent.IsRepairPlaceholder(content) {
				return true
			}
		case []llm.ContentBlock:
			for _, block := range content {
				if strings.TrimSpace(block.Text) != "" || (block.ImageURL != nil && strings.TrimSpace(block.ImageURL.URL) != "") {
					return true
				}
			}
		}
	}
	return false
}

func appSessionRepairReport(name string, report agent.RepairReport) SessionRepairReport {
	actions := report.Actions
	if actions == nil {
		actions = []agent.RepairAction{}
	}
	return SessionRepairReport{
		Name:           name,
		Status:         report.Status,
		Valid:          report.Valid,
		BeforeMessages: report.BeforeMessages,
		AfterMessages:  report.AfterMessages,
		Actions:        actions,
		Diagnostic:     report.Diagnostic,
	}
}

func replaceSessionFile(path string, data []byte) error {
	tmp := path + ".repair.tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return fmt.Errorf("write repaired session: %w", err)
	}
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	// Windows does not replace an existing destination with os.Rename. The
	// original has already been backed up, so fall back to an in-place write
	// without creating a remove/rename window where the session can disappear.
	if err := os.WriteFile(path, data, 0o640); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace repaired session in place: %w", err)
	}
	_ = os.Remove(tmp)
	return nil
}

func (a *App) recordSessionRepairLedger(name, status string, report agent.RepairReport) {
	details := make([]string, 0, len(report.Actions))
	for _, action := range report.Actions {
		details = append(details, action.String())
	}
	a.recordLedger(ledger.Event{
		Kind:    "session_repair",
		Source:  "saved_session",
		Status:  status,
		Message: name,
		Detail:  strings.Join(details, "\n"),
		Metadata: map[string]string{
			"actions":         fmt.Sprintf("%d", len(report.Actions)),
			"before_messages": fmt.Sprintf("%d", report.BeforeMessages),
			"after_messages":  fmt.Sprintf("%d", report.AfterMessages),
		},
	})
}
