package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func compactArgs(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// Read is the compact model-facing file/document reader. It delegates to the
// existing specialised readers while keeping one decision surface.
type Read struct{}

func (t *Read) Name() string      { return "read" }
func (t *Read) Destructive() bool { return false }
func (t *Read) Description() string {
	return "Read file contents by path, many paths, PDF text, line range, chunk, or outline mode. Find files with glob; search contents with grep."
}
func (t *Read) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "paths": {"type": "array", "items": {"type": "string"}, "maxItems": 20},
    "mode": {"type": "string", "enum": ["full", "outline", "chunk", "pdf"]},
    "offset": {"type": "integer"},
    "limit": {"type": "integer"},
    "chunk_index": {"type": "integer"},
    "chunk_size_lines": {"type": "integer"},
    "max_items": {"type": "integer"},
    "start_page": {"type": "integer"},
    "end_page": {"type": "integer"},
    "max_chars": {"type": "integer"}
  },
  "additionalProperties": false
}`)
}
func (t *Read) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Path           string   `json:"path"`
		Paths          []string `json:"paths"`
		Mode           string   `json:"mode"`
		Offset         int      `json:"offset"`
		Limit          int      `json:"limit"`
		ChunkIndex     int      `json:"chunk_index"`
		ChunkSizeLines int      `json:"chunk_size_lines"`
		MaxItems       int      `json:"max_items"`
		StartPage      int      `json:"start_page"`
		EndPage        int      `json:"end_page"`
		MaxChars       int      `json:"max_chars"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("read: bad params: %w", err)
	}
	if len(p.Paths) > 0 {
		paths, err := cleanCompactPathArgs(p.Paths)
		if err != nil {
			return "", err
		}
		return (&ReadMany{}).Run(ctx, compactArgs(map[string]any{"paths": paths}))
	}
	path, err := cleanCompactPathArg(p.Path)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("read: path or paths is required")
	}
	mode := strings.ToLower(strings.TrimSpace(p.Mode))
	if mode == "" {
		mode = "full"
	}
	if mode == "pdf" || strings.EqualFold(filepath.Ext(path), ".pdf") {
		return (&ReadPDF{}).Run(ctx, compactArgs(map[string]any{"path": path, "start_page": p.StartPage, "end_page": p.EndPage, "max_chars": p.MaxChars}))
	}
	switch mode {
	case "outline":
		return (&FileOutline{}).Run(ctx, compactArgs(map[string]any{"path": path, "max_items": p.MaxItems}))
	case "chunk":
		return (&ReadChunks{}).Run(ctx, compactArgs(map[string]any{"path": path, "chunk_index": p.ChunkIndex, "chunk_size_lines": p.ChunkSizeLines}))
	case "full":
		start, end := p.Offset, 0
		if start <= 0 {
			start = 1
		}
		if p.Limit > 0 {
			end = start + p.Limit - 1
		}
		return (&ReadFile{}).Run(ctx, compactArgs(map[string]any{"path": path, "start_line": start, "end_line": end}))
	default:
		return "", fmt.Errorf("read: invalid mode %q", p.Mode)
	}
}

func cleanCompactPathArg(path string) (string, error) {
	return cleanCompactPathArgFor("read", path)
}

func cleanCompactPathArgFor(tool, path string) (string, error) {
	path = strings.TrimSpace(path)
	for _, tag := range []string{"</path>", "</file>", "</filename>", "</target>", "</dir>", "</directory>", "</folder>", "</name>"} {
		for strings.HasSuffix(strings.ToLower(path), tag) {
			path = strings.TrimSpace(path[:len(path)-len(tag)])
		}
	}
	if strings.ContainsAny(path, "<>") {
		if strings.TrimSpace(tool) == "" {
			tool = "tool"
		}
		return "", fmt.Errorf("%s: path contains malformed tool markup; retry with only the filesystem path", tool)
	}
	return path, nil
}

func cleanCompactPathArgs(paths []string) ([]string, error) {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		p, err := cleanCompactPathArg(path)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(p) != "" {
			cleaned = append(cleaned, p)
		}
	}
	return cleaned, nil
}

type Write struct{}

func (t *Write) Name() string      { return "write" }
func (t *Write) Destructive() bool { return true }
func (t *Write) Description() string {
	return "Create a new file, fully replace a file, or append content. Continue chunked output with append=true; after appending, intentional replacement requires overwrite=true. For partial edits use edit; for multi-hunk changes use apply_patch."
}
func (t *Write) Schema() json.RawMessage { return (&WriteFile{}).Schema() }
func (t *Write) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	return (&WriteFile{}).Run(ctx, raw)
}

type Edit struct{}

func (t *Edit) Name() string      { return "edit" }
func (t *Edit) Destructive() bool { return true }
func (t *Edit) Description() string {
	return "Replace one exact string in a file. Use read first to confirm current content; use apply_patch for multiple hunks or files."
}
func (t *Edit) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "old": {"type": "string"},
    "new": {"type": "string"},
    "replace_all": {"type": "boolean"}
  },
  "required": ["path", "old", "new"],
  "additionalProperties": false
}`)
}
func (t *Edit) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("edit: bad params: %w", err)
	}
	if p.ReplaceAll {
		return "", fmt.Errorf("edit: replace_all is not supported by exact-edit backend yet; provide unique old text or use apply_patch")
	}
	return (&EditFile{}).Run(ctx, compactArgs(map[string]any{"path": p.Path, "old_string": p.Old, "new_string": p.New}))
}

type SQLite struct{}

func (t *SQLite) Name() string      { return "sqlite" }
func (t *SQLite) Destructive() bool { return false }
func (t *SQLite) Description() string {
	return "Inspect SQLite schema or run one read-only SELECT/WITH query via mode schema or query."
}
func (t *SQLite) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "mode": {"type": "string", "enum": ["schema", "query"]},
    "path": {"type": "string"},
    "include_columns": {"type": "boolean"},
    "query": {"type": "string"},
    "limit": {"type": "integer"}
  },
  "required": ["mode"],
  "additionalProperties": false
}`)
}
func (t *SQLite) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Mode           string `json:"mode"`
		Path           string `json:"path"`
		IncludeColumns *bool  `json:"include_columns"`
		Query          string `json:"query"`
		Limit          int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("sqlite: bad params: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case "schema":
		return (&SQLiteSchema{}).Run(ctx, compactArgs(map[string]any{"path": p.Path, "include_columns": p.IncludeColumns}))
	case "query":
		return (&SQLiteQuery{}).Run(ctx, compactArgs(map[string]any{"path": p.Path, "query": p.Query, "limit": p.Limit}))
	default:
		return "", fmt.Errorf("sqlite: mode must be schema or query")
	}
}

type Skill struct{}

func (t *Skill) Name() string      { return "skill" }
func (t *Skill) Destructive() bool { return false }
func (t *Skill) Description() string {
	return "List saved skills or read one skill via mode list or view."
}
func (t *Skill) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "mode": {"type": "string", "enum": ["list", "view"]},
    "filter": {"type": "string"},
    "name": {"type": "string"},
    "query": {"type": "string"},
    "max_bytes": {"type": "integer"}
  },
  "required": ["mode"],
  "additionalProperties": false
}`)
}
func (t *Skill) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Mode     string `json:"mode"`
		Filter   string `json:"filter"`
		Name     string `json:"name"`
		Query    string `json:"query"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("skill: bad params: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(p.Mode)) {
	case "list":
		return (&SkillsList{}).Run(ctx, compactArgs(map[string]any{"filter": p.Filter}))
	case "view":
		return (&SkillView{}).Run(ctx, compactArgs(map[string]any{"name": p.Name, "query": p.Query, "max_bytes": p.MaxBytes}))
	default:
		return "", fmt.Errorf("skill: mode must be list or view")
	}
}

type TodoWrite struct{}

func (t *TodoWrite) Name() string      { return "todo_write" }
func (t *TodoWrite) Destructive() bool { return false }
func (t *TodoWrite) Description() string {
	return "Create, replace, update, complete, block, list, or clear the active task plan in one tool."
}
func (t *TodoWrite) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["replace", "update", "done", "blocked", "list", "clear"]},
    "items": {"type": "array", "items": {"type": "string"}},
    "id": {"type": "string"},
    "status": {"type": "string", "enum": ["pending", "in_progress", "done", "blocked"]},
    "detail": {"type": "string"},
    "reason": {"type": "string"}
  },
  "required": ["action"],
  "additionalProperties": false
}`)
}
func (t *TodoWrite) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Action string          `json:"action"`
		Items  json.RawMessage `json:"items"`
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Detail string          `json:"detail"`
		Reason string          `json:"reason"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("todo_write: bad params: %w", err)
	}
	items, err := parseTodoWriteItems(p.Items)
	if err != nil {
		return "", err
	}
	id := normaliseCompactTodoID(p.ID)
	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "replace":
		return (&TodoCreate{}).Run(ctx, compactArgs(map[string]any{"items": items}))
	case "update":
		id = resolveCompactTodoID(id, p.Detail)
		return (&TodoUpdate{}).Run(ctx, compactArgs(map[string]any{"id": id, "status": p.Status, "detail": p.Detail}))
	case "done":
		id = resolveCompactTodoID(id, p.Detail)
		return (&TodoDone{}).Run(ctx, compactArgs(map[string]any{"id": id, "detail": p.Detail}))
	case "blocked":
		id = resolveCompactTodoID(id, p.Reason)
		return (&TodoBlocked{}).Run(ctx, compactArgs(map[string]any{"id": id, "reason": p.Reason}))
	case "list":
		return (&TodoList{}).Run(ctx, compactArgs(map[string]any{}))
	case "clear":
		return (&TodoClear{}).Run(ctx, compactArgs(map[string]any{}))
	default:
		return "", fmt.Errorf("todo_write: invalid action %q", p.Action)
	}
}

func parseTodoWriteItems(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []string
	if err := json.Unmarshal(raw, &items); err == nil {
		return compactTodoItems(items), nil
	}
	var nested [][]string
	if err := json.Unmarshal(raw, &nested); err == nil {
		var flat []string
		for _, group := range nested {
			flat = append(flat, group...)
		}
		return compactTodoItems(flat), nil
	}
	return nil, fmt.Errorf("todo_write: items must be an array of strings")
}

func compactTodoItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func normaliseCompactTodoID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.HasPrefix(strings.ToLower(id), "todo-") {
		return id
	}
	allDigits := true
	for _, r := range id {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return "todo-" + id
	}
	return id
}

func resolveCompactTodoID(id, hint string) string {
	id = normaliseCompactTodoID(id)
	if id == "" || strings.HasPrefix(strings.ToLower(id), "todo-") {
		return id
	}
	items, err := LoadTodos()
	if err != nil || len(items) == 0 {
		return id
	}
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint != "" {
		for _, item := range items {
			text := strings.ToLower(strings.TrimSpace(item.Text + " " + item.Detail))
			if text != "" && (strings.Contains(text, hint) || strings.Contains(hint, strings.ToLower(strings.TrimSpace(item.Text)))) {
				return item.ID
			}
		}
	}
	for _, wantStatus := range []string{"in_progress", "pending", "blocked", "done"} {
		for _, item := range items {
			if strings.EqualFold(item.Status, wantStatus) {
				return item.ID
			}
		}
	}
	return id
}

type Browser struct{}

func (t *Browser) Name() string      { return "browser" }
func (t *Browser) Destructive() bool { return false }
func (t *Browser) Description() string {
	return "Drive the browser with action open, snapshot, click, type, extract, screenshot, close, or agent. Use task type research for heavy multi-step research."
}
func (t *Browser) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["open", "snapshot", "click", "type", "extract", "screenshot", "close", "agent"]},
    "url": {"type": "string"},
    "selector": {"type": "string"},
    "text": {"type": "string"},
    "submit": {"type": "boolean"},
    "max_chars": {"type": "integer"},
    "task": {"type": "string"},
    "timeout_secs": {"type": "integer"}
  },
  "required": ["action"],
  "additionalProperties": false
}`)
}
func (t *Browser) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Action      string `json:"action"`
		URL         string `json:"url"`
		Selector    string `json:"selector"`
		Text        string `json:"text"`
		Submit      bool   `json:"submit"`
		MaxChars    int    `json:"max_chars"`
		Task        string `json:"task"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser: bad params: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(p.Action)) {
	case "open":
		return (&BrowserOpen{}).Run(ctx, compactArgs(map[string]any{"url": p.URL}))
	case "snapshot":
		return (&BrowserSnapshot{}).Run(ctx, compactArgs(map[string]any{"max_chars": p.MaxChars}))
	case "click":
		return (&BrowserClick{}).Run(ctx, compactArgs(map[string]any{"selector": p.Selector}))
	case "type":
		return (&BrowserType{}).Run(ctx, compactArgs(map[string]any{"selector": p.Selector, "text": p.Text, "submit": p.Submit}))
	case "extract":
		return (&BrowserExtract{}).Run(ctx, compactArgs(map[string]any{"selector": p.Selector, "max_chars": p.MaxChars}))
	case "screenshot":
		return (&BrowserScreenshot{}).Run(ctx, compactArgs(map[string]any{}))
	case "close":
		return (&BrowserClose{}).Run(ctx, compactArgs(map[string]any{}))
	case "agent":
		return (&BrowserAgent{TimeoutSecs: 300}).Run(ctx, compactArgs(map[string]any{"task": p.Task, "timeout_secs": p.TimeoutSecs}))
	default:
		return "", fmt.Errorf("browser: invalid action %q", p.Action)
	}
}
