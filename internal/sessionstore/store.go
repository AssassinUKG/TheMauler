package sessionstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"mauler/internal/store"
)

type Message struct {
	Role      string
	Content   string
	ToolName  string
	ToolCalls string
}

type SearchResult struct {
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name"`
	MessageID   int64  `json:"message_id"`
	Role        string `json:"role"`
	Content     string `json:"content"`
	ToolName    string `json:"tool_name,omitempty"`
	Rank        string `json:"rank,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type CheckpointRecord struct {
	RunID   string `json:"run_id"`
	Prompt  string `json:"prompt"`
	Mode    string `json:"mode"`
	Profile string `json:"profile"`
	Payload string `json:"payload"`
	SavedAt string `json:"saved_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func DefaultPath() (string, error) {
	return store.DefaultPath()
}

func StoreDefaultSession(name, scope, model string, messages []Message) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return StoreSession(path, name, scope, model, messages)
}

func SearchDefault(query string, limit int) ([]SearchResult, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Search(path, query, limit)
}

func DeleteDefaultSession(name, scope string) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return DeleteSession(path, name, scope)
}

func RenameDefaultSession(oldName, newName, scope string) error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return RenameSession(path, oldName, newName, scope)
}

func ClearDefault() error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return Clear(path)
}

func StoreSession(dbPath, name, scope, model string, messages []Message) error {
	db, err := open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return NewStore(db).StoreSession(name, scope, model, messages)
}

func (s *Store) StoreSession(name, scope, model string, messages []Message) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("session name is required")
	}

	sessionID := sessionID(scope, name)
	now := time.Now().Format(time.RFC3339)
	return withTx(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM messages_fts WHERE rowid IN (SELECT id FROM messages WHERE session_id = ?)`, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO sessions (id, name, scope, model, updated_at, message_count) VALUES (?, ?, ?, ?, ?, ?)`,
			sessionID, name, scope, model, now, len(messages),
		); err != nil {
			return err
		}
		for i, msg := range messages {
			content := strings.TrimSpace(msg.Content)
			toolCalls := strings.TrimSpace(msg.ToolCalls)
			toolName := strings.TrimSpace(msg.ToolName)
			res, err := tx.Exec(
				`INSERT INTO messages (session_id, idx, role, content, tool_name, tool_calls, timestamp) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				sessionID, i, msg.Role, content, toolName, toolCalls, now,
			)
			if err != nil {
				return err
			}
			msgID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			indexText := strings.TrimSpace(content + " " + toolName + " " + toolCalls)
			if indexText == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO messages_fts(rowid, content) VALUES (?, ?)`, msgID, indexText); err != nil {
				return err
			}
		}
		return nil
	})
}

func RenameSession(dbPath, oldName, newName, scope string) error {
	db, err := open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return NewStore(db).RenameSession(oldName, newName, scope)
}

// RenameSession changes a saved conversation's display name and identity while
// preserving the message row IDs used by the full-text index. A missing source
// row is not an error because legacy JSON sessions may predate recall indexing.
func (s *Store) RenameSession(oldName, newName, scope string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("old and new session names are required")
	}
	oldID := sessionID(scope, oldName)
	newID := sessionID(scope, newName)
	if oldID == newID {
		return nil
	}

	return withTx(s.db, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM sessions WHERE id = ?`, newID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			return fmt.Errorf("session %q already exists", newName)
		}

		result, err := tx.Exec(`
INSERT INTO sessions (id, name, scope, model, updated_at, message_count)
SELECT ?, ?, scope, model, updated_at, message_count
FROM sessions
WHERE id = ?`, newID, newName, oldID)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows == 0 {
			return nil
		}
		if _, err := tx.Exec(`UPDATE messages SET session_id = ? WHERE session_id = ?`, newID, oldID); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM sessions WHERE id = ?`, oldID)
		return err
	})
}

func DeleteSession(dbPath, name, scope string) error {
	db, err := open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return NewStore(db).DeleteSession(name, scope)
}

func (s *Store) DeleteSession(name, scope string) error {
	id := sessionID(scope, name)
	return withTx(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM messages_fts WHERE rowid IN (SELECT id FROM messages WHERE session_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id)
		return err
	})
}

func Clear(dbPath string) error {
	db, err := open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return NewStore(db).Clear()
}

func (s *Store) Clear() error {
	return withTx(s.db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM messages_fts`); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM messages`); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM sessions`)
		return err
	})
}

func Search(dbPath, query string, limit int) ([]SearchResult, error) {
	db, err := open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return NewStore(db).Search(query, limit)
}

func (s *Store) Search(query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	match := ftsQuery(query)
	rows, err := s.db.Query(`
SELECT m.id, m.session_id, s.name, m.role, m.content, m.tool_name, s.updated_at, bm25(messages_fts) AS rank
FROM messages_fts
JOIN messages m ON m.id = messages_fts.rowid
JOIN sessions s ON s.id = m.session_id
WHERE messages_fts MATCH ?
ORDER BY rank, s.updated_at DESC
LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var result SearchResult
		var rank float64
		if err := rows.Scan(&result.MessageID, &result.SessionID, &result.SessionName, &result.Role, &result.Content, &result.ToolName, &result.UpdatedAt, &rank); err != nil {
			return nil, err
		}
		result.Content = trimResult(result.Content)
		result.Rank = fmt.Sprintf("%.4f", rank)
		out = append(out, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) SaveCheckpoint(record CheckpointRecord) error {
	record.RunID = strings.TrimSpace(record.RunID)
	record.SavedAt = strings.TrimSpace(record.SavedAt)
	if record.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if strings.TrimSpace(record.Payload) == "" {
		return fmt.Errorf("checkpoint payload is required")
	}
	if record.SavedAt == "" {
		record.SavedAt = time.Now().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`
INSERT INTO run_checkpoints (run_id, prompt, mode, profile, payload, saved_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
  prompt=excluded.prompt,
  mode=excluded.mode,
  profile=excluded.profile,
  payload=excluded.payload,
  saved_at=excluded.saved_at`,
		record.RunID, record.Prompt, record.Mode, record.Profile, record.Payload, record.SavedAt)
	return err
}

func (s *Store) LoadCheckpoint(runID string) (CheckpointRecord, bool, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return CheckpointRecord{}, false, nil
	}
	var record CheckpointRecord
	err := s.db.QueryRow(`
SELECT run_id, prompt, mode, profile, payload, saved_at
FROM run_checkpoints
WHERE run_id = ?`, runID).Scan(&record.RunID, &record.Prompt, &record.Mode, &record.Profile, &record.Payload, &record.SavedAt)
	if err == sql.ErrNoRows {
		return CheckpointRecord{}, false, nil
	}
	if err != nil {
		return CheckpointRecord{}, false, err
	}
	return record, true, nil
}

func (s *Store) ListCheckpoints() ([]CheckpointRecord, error) {
	rows, err := s.db.Query(`
SELECT run_id, prompt, mode, profile, payload, saved_at
FROM run_checkpoints
ORDER BY saved_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []CheckpointRecord
	for rows.Next() {
		var record CheckpointRecord
		if err := rows.Scan(&record.RunID, &record.Prompt, &record.Mode, &record.Profile, &record.Payload, &record.SavedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Store) DeleteCheckpoint(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM run_checkpoints WHERE run_id = ?`, runID)
	return err
}

func open(path string) (*sql.DB, error) {
	return store.Open(path)
}

func withTx(db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func sessionID(scope, name string) string {
	if strings.TrimSpace(scope) == "" {
		return name
	}
	return scope + "::" + name
}

func ftsQuery(query string) string {
	terms := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' && r != '.'
	})
	uniq := map[string]bool{}
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.Trim(term, `"`)
		if len(term) < 2 || uniq[term] {
			continue
		}
		uniq[term] = true
		out = append(out, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	}
	return strings.Join(out, " AND ")
}

func trimResult(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 600 {
		return text[:600] + "\n..."
	}
	return text
}

func MarshalToolCalls(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
