package sessionstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mauler/internal/settings"

	_ "modernc.org/sqlite"
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

func DefaultPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
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

func ClearDefault() error {
	path, err := DefaultPath()
	if err != nil {
		return err
	}
	return Clear(path)
}

func StoreSession(dbPath, name, scope, model string, messages []Message) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("session name is required")
	}
	db, err := open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	sessionID := sessionID(scope, name)
	now := time.Now().Format(time.RFC3339)
	return withTx(db, func(tx *sql.Tx) error {
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

func DeleteSession(dbPath, name, scope string) error {
	db, err := open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	id := sessionID(scope, name)
	return withTx(db, func(tx *sql.Tx) error {
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
	return withTx(db, func(tx *sql.Tx) error {
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
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	db, err := open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	match := ftsQuery(query)
	rows, err := db.Query(`
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

func open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_, _ = db.Exec(`PRAGMA journal_mode=DELETE`)
	}
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  scope TEXT,
  model TEXT,
  updated_at TEXT NOT NULL,
  message_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  idx INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT,
  tool_name TEXT,
  tool_calls TEXT,
  timestamp TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, idx);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content);
`)
	return err
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
