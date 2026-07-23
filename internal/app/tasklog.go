package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"mauler/internal/controlplane"
	"mauler/internal/ledger"
	"mauler/internal/settings"
)

type TaskRun struct {
	ID                 string                     `json:"id"`
	Prompt             string                     `json:"prompt"`
	Mode               string                     `json:"mode"`
	Profile            string                     `json:"profile"`
	Model              string                     `json:"model,omitempty"`
	ContextPacketClass string                     `json:"context_packet_class,omitempty"`
	ClaimantID         string                     `json:"claimant_id,omitempty"`
	ClaimantAlias      string                     `json:"claimant_alias,omitempty"`
	Origin             string                     `json:"origin,omitempty"`
	Contract           *controlplane.TaskContract `json:"contract,omitempty"`
	Control            *controlplane.MachineState `json:"control,omitempty"`
	Status             string                     `json:"status"`
	State              string                     `json:"state,omitempty"`
	StopReason         string                     `json:"stop_reason,omitempty"`
	StopDetail         string                     `json:"stop_detail,omitempty"`
	StartedAt          string                     `json:"started_at"`
	EndedAt            string                     `json:"ended_at,omitempty"`
	DurationMs         int64                      `json:"duration_ms,omitempty"`
	PromptTokens       int                        `json:"prompt_tokens,omitempty"`
	CompletionTokens   int                        `json:"completion_tokens,omitempty"`
	TotalTokens        int                        `json:"total_tokens,omitempty"`
	Summary            string                     `json:"summary,omitempty"`
	Response           string                     `json:"response,omitempty"`
	Tools              []TaskToolEvent            `json:"tools,omitempty"`
	Events             []TaskRunEvent             `json:"events,omitempty"`

	startMs              int64 // not serialised; used to compute DurationMs
	ledger               *ledger.Ledger
	memoryConflictEvents map[string]int
}

var taskRunSequence atomic.Uint64

type TaskRunEvent struct {
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Detail    string `json:"detail,omitempty"`
}

type TaskToolEvent struct {
	Name       string `json:"name"`
	Input      string `json:"input,omitempty"`
	Result     string `json:"result,omitempty"`
	Status     string `json:"status"`
	Timestamp  string `json:"timestamp"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

func (a *App) ListTaskRuns() ([]TaskRun, error) {
	return a.listTaskRuns()
}

func (a *App) ClearTaskRuns() error {
	return a.clearTaskRuns()
}

func (a *App) ExportTaskRunsJSON() (string, error) {
	runs, err := a.listTaskRuns()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) ImportTaskRunsJSON(raw string) (int, error) {
	var runs []TaskRun
	if err := json.Unmarshal([]byte(raw), &runs); err != nil {
		return 0, err
	}
	if a != nil && a.db != nil {
		return len(runs), replaceTaskRunsDB(a.db, runs)
	}
	return len(runs), saveTaskRuns(runs)
}

func startTaskRun(prompt, mode, profile, model string) TaskRun {
	now := time.Now()
	return TaskRun{
		ID: "task-" + strings.ReplaceAll(now.Format(time.RFC3339Nano), ":", "-") +
			"-" + strconv.FormatUint(taskRunSequence.Add(1), 36),
		Prompt:    strings.TrimSpace(prompt),
		Mode:      mode,
		Profile:   profile,
		Model:     model,
		Status:    "running",
		State:     "planning",
		StartedAt: now.Format(time.RFC3339),
		startMs:   now.UnixMilli(),
	}
}

func (r *TaskRun) attachLedger(l *ledger.Ledger) {
	r.ledger = l
}

func (r *TaskRun) addTool(name, input, result, status string, durationMs int64) {
	event := TaskToolEvent{
		Name:       name,
		Input:      input,
		Result:     trimRunText(result),
		Status:     status,
		Timestamp:  time.Now().Format(time.RFC3339),
		DurationMs: durationMs,
	}
	r.Tools = append(r.Tools, event)
	r.recordLedger(ledger.Event{
		Kind:       "tool_result",
		Source:     "tool",
		Tool:       event.Name,
		Status:     event.Status,
		Input:      event.Input,
		Output:     event.Result,
		DurationMs: event.DurationMs,
		Timestamp:  event.Timestamp,
	})
}

func (r *TaskRun) addEvent(kind, message, detail string) {
	event := TaskRunEvent{
		Kind:      strings.TrimSpace(kind),
		Message:   strings.TrimSpace(message),
		Timestamp: time.Now().Format(time.RFC3339),
		Detail:    trimRunText(detail),
	}
	r.Events = append(r.Events, event)
	ledgerEvent := ledger.Event{
		Kind:      event.Kind,
		Source:    "task_run",
		Message:   event.Message,
		Detail:    event.Detail,
		Timestamp: event.Timestamp,
	}
	if event.Kind == "state" {
		ledgerEvent.State = event.Message
	}
	r.recordLedger(ledgerEvent)
}

func (r *TaskRun) addMemoryConflictEvent(message, detail string) {
	if r == nil {
		return
	}
	key := strings.TrimSpace(detail)
	if key == "" {
		key = strings.TrimSpace(message)
	}
	if r.memoryConflictEvents == nil {
		r.memoryConflictEvents = map[string]int{}
	}
	if r.memoryConflictEvents[key] > 0 {
		r.memoryConflictEvents[key]++
		return
	}
	if len(r.memoryConflictEvents) >= 3 {
		r.memoryConflictEvents[key]++
		return
	}
	r.memoryConflictEvents[key] = 1
	r.addEvent("memory_conflict", message, detail)
}

func (r *TaskRun) hasEvent(kind, detailContains string) bool {
	if r == nil {
		return false
	}
	for _, event := range r.Events {
		if event.Kind != kind {
			continue
		}
		if detailContains == "" || strings.Contains(event.Detail, detailContains) {
			return true
		}
	}
	return false
}

func (r *TaskRun) setState(state, detail string) {
	state = strings.TrimSpace(state)
	if state == "" || r.State == state {
		return
	}
	r.State = state
	r.addEvent("state", state, detail)
}

func (r *TaskRun) setTokens(prompt, completion int) {
	r.PromptTokens = prompt
	r.CompletionTokens = completion
	r.TotalTokens = prompt + completion
}

func (r *TaskRun) finish(status, summary string) {
	r.Status = status
	r.Summary = trimRunText(summary)
	now := time.Now()
	r.EndedAt = now.Format(time.RFC3339)
	if r.startMs > 0 {
		r.DurationMs = now.UnixMilli() - r.startMs
	}
	r.recordLedger(ledger.Event{
		Kind:       "run_finish",
		Source:     "task_run",
		Status:     r.Status,
		State:      r.State,
		Message:    r.Summary,
		DurationMs: r.DurationMs,
		Timestamp:  r.EndedAt,
	})
}

func (r *TaskRun) stop(reason, detail string) {
	if r.StopReason == "" {
		r.StopReason = strings.TrimSpace(reason)
		r.StopDetail = trimRunText(detail)
		r.recordLedger(ledger.Event{
			Kind:      "run_stop",
			Source:    "task_run",
			Status:    r.StopReason,
			State:     r.State,
			Message:   r.StopReason,
			Detail:    r.StopDetail,
			Timestamp: time.Now().Format(time.RFC3339),
		})
	}
}

func (r *TaskRun) stopTerminal(reason, detail string) {
	r.StopReason = strings.TrimSpace(reason)
	r.StopDetail = trimRunText(detail)
	r.recordLedger(ledger.Event{
		Kind:      "run_stop",
		Source:    "task_run",
		Status:    r.StopReason,
		State:     r.State,
		Message:   r.StopReason,
		Detail:    r.StopDetail,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func (r *TaskRun) recordLedger(event ledger.Event) {
	if r == nil || r.ledger == nil {
		return
	}
	event.RunID = r.ID
	if event.Timestamp == "" {
		event.Timestamp = time.Now().Format(time.RFC3339)
	}
	_, _ = r.ledger.Record(event)
}

func saveTaskRun(run TaskRun, cfg *settings.LoggingConfig) error {
	if cfg != nil && !cfg.Enabled {
		return nil
	}
	runs, err := loadTaskRuns()
	if err != nil {
		return err
	}
	next := []TaskRun{run}
	for _, existing := range runs {
		if existing.ID != run.ID {
			next = append(next, existing)
		}
	}
	maxRuns := 100
	if cfg != nil && cfg.MaxRuns > 0 {
		maxRuns = cfg.MaxRuns
	}
	if len(next) > maxRuns {
		next = next[:maxRuns]
	}
	return saveTaskRuns(next)
}

func loadTaskRuns() ([]TaskRun, error) {
	path, err := taskRunsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []TaskRun{}, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []TaskRun
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, err
	}
	if runs == nil {
		return []TaskRun{}, nil
	}
	return runs, nil
}

func saveTaskRuns(runs []TaskRun) error {
	path, err := taskRunsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o640)
}

func taskRunsPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "task-runs.json"), nil
}

func trimRunText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 2000 {
		return text[:2000] + "\n..."
	}
	return text
}
