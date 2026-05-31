package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mauler/internal/settings"
)

type TodoItem struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type todoFile struct {
	Items []TodoItem `json:"items"`
}

type TodoCreate struct{}
type TodoUpdate struct{}
type TodoDone struct{}
type TodoBlocked struct{}
type TodoList struct{}
type TodoClear struct{}

func (t *TodoCreate) Name() string      { return "todo_create" }
func (t *TodoCreate) Destructive() bool { return false }
func (t *TodoCreate) Description() string {
	return "Create or replace the active task plan. Use at the start of multi-step work so progress is visible and trackable."
}
func (t *TodoCreate) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "items": {
      "type": "array",
      "description": "Ordered checklist items for this task.",
      "items": {"type": "string"}
    }
  },
  "required": ["items"],
  "additionalProperties": false
}`)
}
func (t *TodoCreate) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("todo_create: bad params: %w", err)
	}
	items := make([]TodoItem, 0, len(p.Items))
	now := time.Now().Format(time.RFC3339)
	for i, text := range p.Items {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		status := "pending"
		if i == 0 {
			status = "in_progress"
		}
		items = append(items, TodoItem{
			ID:        fmt.Sprintf("todo-%d", i+1),
			Text:      text,
			Status:    status,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if len(items) == 0 {
		return "", fmt.Errorf("todo_create: at least one item is required")
	}
	if err := saveTodos(items); err != nil {
		return "", err
	}
	return formatTodos(items), nil
}

func (t *TodoUpdate) Name() string      { return "todo_update" }
func (t *TodoUpdate) Destructive() bool { return false }
func (t *TodoUpdate) Description() string {
	return "Update a task plan item status or detail. Use before moving between major phases."
}
func (t *TodoUpdate) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Todo id, for example todo-1."},
    "status": {"type": "string", "enum": ["pending", "in_progress", "done", "blocked"], "description": "New item status."},
    "detail": {"type": "string", "description": "Short progress note or blocker detail."}
  },
  "required": ["id", "status"],
  "additionalProperties": false
}`)
}
func (t *TodoUpdate) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("todo_update: bad params: %w", err)
	}
	items, err := updateTodo(p.ID, p.Status, p.Detail)
	if err != nil {
		return "", err
	}
	return formatTodos(items), nil
}

func (t *TodoDone) Name() string      { return "todo_done" }
func (t *TodoDone) Destructive() bool { return false }
func (t *TodoDone) Description() string {
	return "Mark a task plan item done, optionally with a short result note."
}
func (t *TodoDone) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "detail": {"type": "string"}
  },
  "required": ["id"],
  "additionalProperties": false
}`)
}
func (t *TodoDone) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		ID     string `json:"id"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("todo_done: bad params: %w", err)
	}
	items, err := updateTodo(p.ID, "done", p.Detail)
	if err != nil {
		return "", err
	}
	return formatTodos(items), nil
}

func (t *TodoBlocked) Name() string      { return "todo_blocked" }
func (t *TodoBlocked) Destructive() bool { return false }
func (t *TodoBlocked) Description() string {
	return "Mark a task plan item blocked with the reason or permission needed."
}
func (t *TodoBlocked) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "id": {"type": "string"},
    "reason": {"type": "string", "description": "Why this item is blocked."}
  },
  "required": ["id", "reason"],
  "additionalProperties": false
}`)
}
func (t *TodoBlocked) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("todo_blocked: bad params: %w", err)
	}
	items, err := updateTodo(p.ID, "blocked", p.Reason)
	if err != nil {
		return "", err
	}
	return formatTodos(items), nil
}

func (t *TodoList) Name() string      { return "todo_list" }
func (t *TodoList) Destructive() bool { return false }
func (t *TodoList) Description() string {
	return "List the current active task plan."
}
func (t *TodoList) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t *TodoList) Run(_ context.Context, _ json.RawMessage) (string, error) {
	items, err := LoadTodos()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "No active task plan.", nil
	}
	return formatTodos(items), nil
}

func (t *TodoClear) Name() string      { return "todo_clear" }
func (t *TodoClear) Destructive() bool { return false }
func (t *TodoClear) Description() string {
	return "Clear the active task plan after work is complete or abandoned."
}
func (t *TodoClear) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t *TodoClear) Run(_ context.Context, _ json.RawMessage) (string, error) {
	if err := SaveTodos([]TodoItem{}); err != nil {
		return "", err
	}
	return "cleared active task plan", nil
}

func LoadTodos() ([]TodoItem, error) {
	path, err := todoPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []TodoItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	var file todoFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	sort.SliceStable(file.Items, func(i, j int) bool { return file.Items[i].ID < file.Items[j].ID })
	return file.Items, nil
}

func SaveTodos(items []TodoItem) error {
	return saveTodos(items)
}

func saveTodos(items []TodoItem) error {
	path, err := todoPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(todoFile{Items: items}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o640)
}

func updateTodo(id, status, detail string) ([]TodoItem, error) {
	id = strings.TrimSpace(id)
	status = normaliseTodoStatus(status)
	if id == "" {
		return nil, fmt.Errorf("todo_update: id is required")
	}
	items, err := LoadTodos()
	if err != nil {
		return nil, err
	}
	now := time.Now().Format(time.RFC3339)
	for i := range items {
		if items[i].ID == id {
			items[i].Status = status
			items[i].Detail = strings.TrimSpace(detail)
			items[i].UpdatedAt = now
			return items, saveTodos(items)
		}
	}
	return nil, fmt.Errorf("todo_update: %s not found", id)
}

func normaliseTodoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending", "in_progress", "done", "blocked":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "pending"
	}
}

func todoPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "todos.json"), nil
}

func formatTodos(items []TodoItem) string {
	if len(items) == 0 {
		return "No active task plan."
	}
	var sb strings.Builder
	sb.WriteString("Active task plan:\n")
	for _, item := range items {
		fmt.Fprintf(&sb, "- [%s] %s: %s", item.Status, item.ID, item.Text)
		if strings.TrimSpace(item.Detail) != "" {
			fmt.Fprintf(&sb, " — %s", strings.TrimSpace(item.Detail))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
