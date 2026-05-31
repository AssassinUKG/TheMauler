package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func withTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
}

func TestTodoLifecycle(t *testing.T) {
	withTempHome(t)
	create := &TodoCreate{}
	out, err := create.Run(context.Background(), json.RawMessage(`{"items":["Inspect code","Implement planner","Run tests"]}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out == "" {
		t.Fatalf("expected formatted todo output")
	}
	items, err := LoadTodos()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(items) != 3 || items[0].Status != "in_progress" || items[1].Status != "pending" {
		t.Fatalf("unexpected created todos: %#v", items)
	}

	done := &TodoDone{}
	if _, err := done.Run(context.Background(), json.RawMessage(`{"id":"todo-1","detail":"inspected"}`)); err != nil {
		t.Fatalf("done: %v", err)
	}
	blocked := &TodoBlocked{}
	if _, err := blocked.Run(context.Background(), json.RawMessage(`{"id":"todo-2","reason":"waiting for input"}`)); err != nil {
		t.Fatalf("blocked: %v", err)
	}
	items, _ = LoadTodos()
	if items[0].Status != "done" || items[1].Status != "blocked" || items[1].Detail != "waiting for input" {
		t.Fatalf("unexpected updated todos: %#v", items)
	}
}

func TestTodoClear(t *testing.T) {
	withTempHome(t)
	if err := SaveTodos([]TodoItem{{ID: "todo-1", Text: "x", Status: "pending"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	clear := &TodoClear{}
	if _, err := clear.Run(context.Background(), nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	items, err := LoadTodos()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected cleared todos, got %#v", items)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "mauler", "todos.json")); err != nil {
		t.Fatalf("expected todo file to exist: %v", err)
	}
}

func TestRegistryIncludesTodoTools(t *testing.T) {
	registry := New()
	for _, name := range []string{"todo_create", "todo_update", "todo_done", "todo_blocked", "todo_list", "todo_clear"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("registry missing %s", name)
		}
	}
}
