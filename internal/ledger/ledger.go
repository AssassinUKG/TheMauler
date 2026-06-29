package ledger

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mauler/internal/settings"
)

// Event is the canonical record for agent activity. Task logs, live UI,
// memory extraction, reflections, skills, and evidence views should derive from
// this shape instead of each subsystem inventing a partial log.
type Event struct {
	ID         string            `json:"id"`
	RunID      string            `json:"run_id,omitempty"`
	Kind       string            `json:"kind"`
	Source     string            `json:"source,omitempty"`
	Tool       string            `json:"tool,omitempty"`
	Status     string            `json:"status,omitempty"`
	State      string            `json:"state,omitempty"`
	Message    string            `json:"message,omitempty"`
	Detail     string            `json:"detail,omitempty"`
	Input      string            `json:"input,omitempty"`
	Output     string            `json:"output,omitempty"`
	Error      string            `json:"error,omitempty"`
	DurationMs int64             `json:"duration_ms,omitempty"`
	Files      []string          `json:"files,omitempty"`
	Artifacts  []string          `json:"artifacts,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timestamp  string            `json:"timestamp"`
}

type Ledger struct {
	mu   sync.Mutex
	path string
	db   *sql.DB
}

func NewDefault() (*Ledger, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return New(path), nil
}

func New(path string) *Ledger {
	return &Ledger{path: path}
}

func (l *Ledger) AttachDB(db *sql.DB) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.db = db
	l.mu.Unlock()
}

func DefaultPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "run-ledger.jsonl"), nil
}

func (l *Ledger) Record(event Event) (Event, error) {
	if l == nil {
		return normaliseEvent(event), nil
	}
	event = normaliseEvent(event)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o750); err != nil {
		return event, err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return event, err
	}
	defer f.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return event, err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return event, err
	}
	if l.db != nil {
		if err := recordEventDB(l.db, event); err != nil {
			return event, err
		}
	}
	return event, nil
}

func recordEventDB(db *sql.DB, event Event) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	res, err := tx.Exec(`
insert into ledger_events (
  id, run_id, kind, source, tool, status, state, message, detail,
  input, output, error, duration_ms, timestamp
) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.RunID, event.Kind, event.Source, event.Tool, event.Status, event.State, event.Message, event.Detail,
		event.Input, event.Output, event.Error, event.DurationMs, event.Timestamp)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	eventPK, err := res.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, file := range event.Files {
		if _, err := tx.Exec(`insert into ledger_event_files (event_pk, path) values (?, ?)`, eventPK, file); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for _, artifact := range event.Artifacts {
		if _, err := tx.Exec(`insert into ledger_event_artifacts (event_pk, path) values (?, ?)`, eventPK, artifact); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for key, value := range event.Metadata {
		if _, err := tx.Exec(`insert into ledger_event_metadata (event_pk, key, value) values (?, ?, ?)`, eventPK, key, value); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (l *Ledger) List(limit int) ([]Event, error) {
	if l == nil {
		return nil, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.db != nil {
		return l.listDB(limit)
	}
	return readEvents(l.path, limit)
}

func (l *Ledger) listDB(limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := l.db.Query(`
select event_pk, id, run_id, kind, source, tool, status, state, message, detail,
       input, output, error, duration_ms, timestamp
from ledger_events
order by timestamp desc, event_pk desc
limit ?`, limit)
	if err != nil {
		return nil, err
	}
	type rowEvent struct {
		pk    int64
		event Event
	}
	var rowEvents []rowEvent
	for rows.Next() {
		var item rowEvent
		if err := rows.Scan(&item.pk, &item.event.ID, &item.event.RunID, &item.event.Kind, &item.event.Source,
			&item.event.Tool, &item.event.Status, &item.event.State, &item.event.Message, &item.event.Detail,
			&item.event.Input, &item.event.Output, &item.event.Error, &item.event.DurationMs, &item.event.Timestamp); err != nil {
			_ = rows.Close()
			return nil, err
		}
		rowEvents = append(rowEvents, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(rowEvents))
	for _, item := range rowEvents {
		files, err := ledgerEventPaths(l.db, "ledger_event_files", item.pk)
		if err != nil {
			return nil, err
		}
		artifacts, err := ledgerEventPaths(l.db, "ledger_event_artifacts", item.pk)
		if err != nil {
			return nil, err
		}
		metadata, err := ledgerEventMetadata(l.db, item.pk)
		if err != nil {
			return nil, err
		}
		item.event.Files = files
		item.event.Artifacts = artifacts
		item.event.Metadata = metadata
		events = append(events, item.event)
	}
	return events, nil
}

func ledgerEventPaths(db *sql.DB, table string, eventPK int64) ([]string, error) {
	rows, err := db.Query(`select path from `+table+` where event_pk = ? order by rowid`, eventPK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func ledgerEventMetadata(db *sql.DB, eventPK int64) (map[string]string, error) {
	rows, err := db.Query(`select key, value from ledger_event_metadata where event_pk = ? order by rowid`, eventPK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	meta := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		meta[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(meta) == 0 {
		return nil, nil
	}
	return meta, nil
}

func (l *Ledger) Clear() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(l.path, nil, 0o640); err != nil {
		return err
	}
	if l.db != nil {
		if err := clearLedgerDB(l.db); err != nil {
			return err
		}
	}
	return nil
}

func (l *Ledger) Prune(match func(Event) bool) (int, error) {
	if l == nil || match == nil {
		return 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	events, err := readEventsChronological(l.path)
	if err != nil {
		return 0, err
	}
	kept := make([]Event, 0, len(events))
	removed := 0
	for _, event := range events {
		if match(event) {
			removed++
			continue
		}
		kept = append(kept, event)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o750); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, event := range kept {
		if err := enc.Encode(event); err != nil {
			return 0, err
		}
	}
	if l.db != nil {
		if err := clearLedgerDB(l.db); err != nil {
			return 0, err
		}
		for _, event := range kept {
			if err := recordEventDB(l.db, event); err != nil {
				return 0, err
			}
		}
	}
	return removed, nil
}

func (l *Ledger) BackfillDBFromJSONL() (int, error) {
	if l == nil || l.db == nil {
		return 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var count int
	if err := l.db.QueryRow(`select count(*) from ledger_events`).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	events, err := readEventsChronological(l.path)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		if err := recordEventDB(l.db, event); err != nil {
			return 0, err
		}
	}
	return len(events), nil
}

func clearLedgerDB(db *sql.DB) error {
	_, err := db.Exec(`delete from ledger_events`)
	return err
}

func readEvents(path string, limit int) ([]Event, error) {
	events, err := readEventsChronological(path)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func readEventsChronological(path string) ([]Event, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func normaliseEvent(event Event) Event {
	now := time.Now().UTC()
	if strings.TrimSpace(event.Timestamp) == "" {
		event.Timestamp = now.Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(event.ID) == "" {
		event.ID = fmt.Sprintf("evt-%d", now.UnixNano())
	}
	event.Kind = clean(event.Kind)
	if event.Kind == "" {
		event.Kind = "event"
	}
	event.RunID = clean(event.RunID)
	event.Source = clean(event.Source)
	event.Tool = clean(event.Tool)
	event.Status = clean(event.Status)
	event.State = clean(event.State)
	event.Message = trim(clean(event.Message), 2000)
	event.Detail = trim(clean(event.Detail), 8000)
	event.Input = trim(clean(event.Input), 8000)
	event.Output = trim(clean(event.Output), 12000)
	event.Error = trim(clean(event.Error), 4000)
	event.Files = cleanSlice(event.Files)
	event.Artifacts = cleanSlice(event.Artifacts)
	if len(event.Metadata) == 0 {
		event.Metadata = nil
	} else {
		next := map[string]string{}
		for k, v := range event.Metadata {
			k = clean(k)
			if k == "" {
				continue
			}
			next[k] = trim(clean(v), 1000)
		}
		event.Metadata = next
	}
	return event
}

func clean(text string) string {
	return strings.TrimSpace(strings.ToValidUTF8(text, "\uFFFD"))
}

func trim(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "\n..."
}

func cleanSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = clean(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
