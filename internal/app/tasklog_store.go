package app

import (
	"database/sql"
	"errors"
	"fmt"

	"mauler/internal/settings"
)

func migrateTaskRunsJSONToDB(db *sql.DB) error {
	if db == nil {
		return nil
	}
	count, err := taskRunCountDB(db)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	runs, err := loadTaskRuns()
	if err != nil {
		return err
	}
	for i := len(runs) - 1; i >= 0; i-- {
		if err := saveTaskRunDB(db, runs[i], nil); err != nil {
			return err
		}
	}
	return nil
}

func taskRunCountDB(db *sql.DB) (int, error) {
	var count int
	if err := db.QueryRow(`select count(*) from task_runs`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (a *App) listTaskRuns() ([]TaskRun, error) {
	if a != nil && a.db != nil {
		return loadTaskRunsDB(a.db)
	}
	return loadTaskRuns()
}

func (a *App) clearTaskRuns() error {
	if a != nil && a.db != nil {
		if err := clearTaskRunsDB(a.db); err != nil {
			return err
		}
		return saveTaskRuns([]TaskRun{})
	}
	return saveTaskRuns([]TaskRun{})
}

func (a *App) saveTaskRun(run TaskRun, cfg *settingsLoggingConfig) error {
	if cfg != nil && !cfg.Enabled() {
		return nil
	}
	if a != nil && a.db != nil {
		return saveTaskRunDB(a.db, run, cfg)
	}
	return saveTaskRun(run, cfg.asSettings())
}

type settingsLoggingConfig struct {
	enabled bool
	maxRuns int
}

func loggingConfigValue(cfg *settings.LoggingConfig) *settingsLoggingConfig {
	if cfg == nil {
		return nil
	}
	return &settingsLoggingConfig{enabled: cfg.Enabled, maxRuns: cfg.MaxRuns}
}

func (c *settingsLoggingConfig) Enabled() bool {
	return c == nil || c.enabled
}

func (c *settingsLoggingConfig) MaxRuns() int {
	if c == nil || c.maxRuns <= 0 {
		return 100
	}
	return c.maxRuns
}

func (c *settingsLoggingConfig) asSettings() *settings.LoggingConfig {
	if c == nil {
		return nil
	}
	return &settings.LoggingConfig{Enabled: c.enabled, MaxRuns: c.maxRuns}
}

func saveTaskRunDB(db *sql.DB, run TaskRun, cfg *settingsLoggingConfig) error {
	if cfg != nil && !cfg.Enabled() {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := replaceTaskRunTx(tx, run); err != nil {
		_ = tx.Rollback()
		return err
	}
	maxRuns := 100
	if cfg != nil {
		maxRuns = cfg.MaxRuns()
	}
	if maxRuns > 0 {
		if _, err := tx.Exec(`delete from task_runs where id in (
select id from task_runs order by started_at desc limit -1 offset ?
)`, maxRuns); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func replaceTaskRunTx(tx *sql.Tx, run TaskRun) error {
	if run.ID == "" {
		return errors.New("task run id is required")
	}
	if _, err := tx.Exec(`delete from task_run_tools where run_id = ?`, run.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from task_run_events where run_id = ?`, run.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
insert into task_runs (
  id, prompt, mode, profile, model, status, state, stop_reason, stop_detail,
  started_at, ended_at, duration_ms, prompt_tokens, completion_tokens,
  total_tokens, summary, response
) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(id) do update set
  prompt=excluded.prompt,
  mode=excluded.mode,
  profile=excluded.profile,
  model=excluded.model,
  status=excluded.status,
  state=excluded.state,
  stop_reason=excluded.stop_reason,
  stop_detail=excluded.stop_detail,
  started_at=excluded.started_at,
  ended_at=excluded.ended_at,
  duration_ms=excluded.duration_ms,
  prompt_tokens=excluded.prompt_tokens,
  completion_tokens=excluded.completion_tokens,
  total_tokens=excluded.total_tokens,
  summary=excluded.summary,
  response=excluded.response
`, run.ID, run.Prompt, run.Mode, run.Profile, run.Model, run.Status, run.State, run.StopReason, run.StopDetail,
		run.StartedAt, run.EndedAt, run.DurationMs, run.PromptTokens, run.CompletionTokens, run.TotalTokens, run.Summary, run.Response); err != nil {
		return err
	}
	for i, tool := range run.Tools {
		if _, err := tx.Exec(`insert into task_run_tools (run_id, idx, name, input, result, status, timestamp, duration_ms) values (?, ?, ?, ?, ?, ?, ?, ?)`,
			run.ID, i, tool.Name, tool.Input, tool.Result, tool.Status, tool.Timestamp, tool.DurationMs); err != nil {
			return err
		}
	}
	for i, event := range run.Events {
		if _, err := tx.Exec(`insert into task_run_events (run_id, idx, kind, message, detail, timestamp) values (?, ?, ?, ?, ?, ?)`,
			run.ID, i, event.Kind, event.Message, event.Detail, event.Timestamp); err != nil {
			return err
		}
	}
	return nil
}

func loadTaskRunsDB(db *sql.DB) ([]TaskRun, error) {
	rows, err := db.Query(`
select id, prompt, mode, profile, model, status, state, stop_reason, stop_detail,
       started_at, ended_at, duration_ms, prompt_tokens, completion_tokens,
       total_tokens, summary, response
from task_runs
order by started_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []TaskRun
	for rows.Next() {
		var run TaskRun
		if err := rows.Scan(&run.ID, &run.Prompt, &run.Mode, &run.Profile, &run.Model, &run.Status, &run.State,
			&run.StopReason, &run.StopDetail, &run.StartedAt, &run.EndedAt, &run.DurationMs, &run.PromptTokens,
			&run.CompletionTokens, &run.TotalTokens, &run.Summary, &run.Response); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range runs {
		tools, err := loadTaskRunToolsDB(db, runs[i].ID)
		if err != nil {
			return nil, err
		}
		events, err := loadTaskRunEventsDB(db, runs[i].ID)
		if err != nil {
			return nil, err
		}
		runs[i].Tools = tools
		runs[i].Events = events
	}
	if runs == nil {
		return []TaskRun{}, nil
	}
	return runs, nil
}

func loadTaskRunToolsDB(db *sql.DB, runID string) ([]TaskToolEvent, error) {
	rows, err := db.Query(`select name, input, result, status, timestamp, duration_ms from task_run_tools where run_id = ? order by idx`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tools []TaskToolEvent
	for rows.Next() {
		var tool TaskToolEvent
		if err := rows.Scan(&tool.Name, &tool.Input, &tool.Result, &tool.Status, &tool.Timestamp, &tool.DurationMs); err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tools == nil {
		return []TaskToolEvent{}, nil
	}
	return tools, nil
}

func loadTaskRunEventsDB(db *sql.DB, runID string) ([]TaskRunEvent, error) {
	rows, err := db.Query(`select kind, message, detail, timestamp from task_run_events where run_id = ? order by idx`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []TaskRunEvent
	for rows.Next() {
		var event TaskRunEvent
		if err := rows.Scan(&event.Kind, &event.Message, &event.Detail, &event.Timestamp); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if events == nil {
		return []TaskRunEvent{}, nil
	}
	return events, nil
}

func clearTaskRunsDB(db *sql.DB) error {
	_, err := db.Exec(`delete from task_runs`)
	if err != nil {
		return fmt.Errorf("clear task runs: %w", err)
	}
	return nil
}

func replaceTaskRunsDB(db *sql.DB, runs []TaskRun) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`delete from task_runs`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, run := range runs {
		if err := replaceTaskRunTx(tx, run); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
