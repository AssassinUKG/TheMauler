package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"mauler/internal/settings"

	_ "modernc.org/sqlite"
)

type SQLiteSchema struct{}

func (t *SQLiteSchema) Name() string      { return "sqlite_schema" }
func (t *SQLiteSchema) Destructive() bool { return false }

func (t *SQLiteSchema) Description() string {
	return "Inspect a local SQLite database schema read-only. Defaults to TheMauler's state.db when path is omitted."
}

func (t *SQLiteSchema) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Optional SQLite database path. Defaults to TheMauler's state.db."},
    "include_columns": {"type": "boolean", "description": "Include PRAGMA table_info columns for each table/view. Default true."}
  },
  "additionalProperties": false
}`)
}

type sqliteSchemaParams struct {
	Path           string `json:"path"`
	IncludeColumns *bool  `json:"include_columns"`
}

func (t *SQLiteSchema) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p sqliteSchemaParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", fmt.Errorf("sqlite_schema: bad params: %w", err)
		}
	}
	db, path, err := openSQLiteReadOnly(p.Path)
	if err != nil {
		return "", fmt.Errorf("sqlite_schema: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `select type, name, sql from sqlite_schema where name not like 'sqlite_%' order by type, name`)
	if err != nil {
		return "", fmt.Errorf("sqlite_schema: %w", err)
	}
	defer rows.Close()

	includeColumns := p.IncludeColumns == nil || *p.IncludeColumns
	var sb strings.Builder
	fmt.Fprintf(&sb, "SQLite schema: %s\n", path)
	count := 0
	for rows.Next() {
		var typ, name string
		var ddl sql.NullString
		if err := rows.Scan(&typ, &name, &ddl); err != nil {
			return "", fmt.Errorf("sqlite_schema: %w", err)
		}
		count++
		fmt.Fprintf(&sb, "\n%s %s\n", typ, name)
		if ddl.Valid && strings.TrimSpace(ddl.String) != "" {
			fmt.Fprintf(&sb, "  ddl: %s\n", compactWhitespace(ddl.String))
		}
		if includeColumns && (typ == "table" || typ == "view") {
			cols, err := sqliteTableColumns(ctx, db, name)
			if err != nil {
				fmt.Fprintf(&sb, "  columns: error: %v\n", err)
			} else if len(cols) > 0 {
				fmt.Fprintf(&sb, "  columns: %s\n", strings.Join(cols, ", "))
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("sqlite_schema: %w", err)
	}
	if count == 0 {
		fmt.Fprintf(&sb, "\nNo user tables or views found.\n")
	}
	return sb.String(), nil
}

type SQLiteQuery struct{}

func (t *SQLiteQuery) Name() string      { return "sqlite_query" }
func (t *SQLiteQuery) Destructive() bool { return false }

func (t *SQLiteQuery) Description() string {
	return "Run a read-only SELECT/WITH query against a local SQLite database. Defaults to TheMauler's state.db when path is omitted."
}

func (t *SQLiteQuery) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Read-only SELECT or WITH query. Multiple statements and writes are rejected."},
    "path": {"type": "string", "description": "Optional SQLite database path. Defaults to TheMauler's state.db."},
    "limit": {"type": "integer", "description": "Maximum rows to print. Default 50, max 200."}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
}

type sqliteQueryParams struct {
	Query string `json:"query"`
	Path  string `json:"path"`
	Limit int    `json:"limit"`
}

func (t *SQLiteQuery) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p sqliteQueryParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("sqlite_query: bad params: %w", err)
	}
	query, err := normalizeReadOnlySQLiteQuery(p.Query)
	if err != nil {
		return "", fmt.Errorf("sqlite_query: %w", err)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	db, path, err := openSQLiteReadOnly(p.Path)
	if err != nil {
		return "", fmt.Errorf("sqlite_query: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("sqlite_query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("sqlite_query: %w", err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "SQLite query: %s\n", path)
	fmt.Fprintf(&sb, "Columns: %s\n", strings.Join(cols, " | "))

	values := make([]any, len(cols))
	scan := make([]any, len(cols))
	for i := range values {
		scan[i] = &values[i]
	}
	rowCount := 0
	truncated := false
	for rows.Next() {
		if rowCount >= limit {
			truncated = true
			break
		}
		if err := rows.Scan(scan...); err != nil {
			return "", fmt.Errorf("sqlite_query: %w", err)
		}
		rowCount++
		fmt.Fprintf(&sb, "\n%d.", rowCount)
		for i, col := range cols {
			fmt.Fprintf(&sb, " %s=%s", col, sqliteValueString(values[i]))
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("sqlite_query: %w", err)
	}
	if rowCount == 0 {
		fmt.Fprintf(&sb, "\nNo rows.\n")
	} else if truncated {
		fmt.Fprintf(&sb, "\n\nStopped at limit=%d.\n", limit)
	}
	return sb.String(), nil
}

func openSQLiteReadOnly(path string) (*sql.DB, string, error) {
	if strings.TrimSpace(path) == "" {
		dir, err := settings.ConfigDir()
		if err != nil {
			return nil, "", err
		}
		path = filepath.Join(dir, "state.db")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, "", err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, "", err
	}
	return db, abs, nil
}

func normalizeReadOnlySQLiteQuery(query string) (string, error) {
	q := strings.TrimSpace(query)
	q = strings.TrimSuffix(q, ";")
	if strings.Contains(q, ";") {
		return "", fmt.Errorf("multiple SQL statements are not allowed")
	}
	lower := strings.ToLower(strings.TrimSpace(q))
	if !(strings.HasPrefix(lower, "select ") || strings.HasPrefix(lower, "with ")) {
		return "", fmt.Errorf("only SELECT/WITH queries are allowed")
	}
	return q, nil
}

func sqliteTableColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "pragma table_info("+quoteSQLiteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		label := name
		if typ != "" {
			label += " " + typ
		}
		if pk > 0 {
			label += " primary key"
		}
		cols = append(cols, label)
	}
	return cols, rows.Err()
}

func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func compactWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func sqliteValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(v)
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}
