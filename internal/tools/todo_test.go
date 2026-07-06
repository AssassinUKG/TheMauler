package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mauler/internal/store"
)

func withTempHome(t *testing.T) {
	t.Helper()
	SetTodoDB(nil)
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

func TestTodoWriteRepairsNestedItemsAndNumericIDs(t *testing.T) {
	withTempHome(t)
	write := &TodoWrite{}
	if _, err := write.Run(context.Background(), json.RawMessage(`{"action":"replace","items":[["Inspect run","Patch loop"]]}`)); err != nil {
		t.Fatalf("replace nested items: %v", err)
	}
	items, err := LoadTodos()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(items) != 2 || items[0].Text != "Inspect run" {
		t.Fatalf("unexpected todo_write replace result: %#v", items)
	}
	if _, err := write.Run(context.Background(), json.RawMessage(`{"action":"done","id":"1","detail":"checked"}`)); err != nil {
		t.Fatalf("done numeric id: %v", err)
	}
	items, err = LoadTodos()
	if err != nil {
		t.Fatalf("load after done: %v", err)
	}
	if items[0].Status != "done" || items[0].Detail != "checked" {
		t.Fatalf("numeric id was not normalised: %#v", items[0])
	}
}

func TestTodoWriteResolvesPlanAliasToActiveItem(t *testing.T) {
	withTempHome(t)
	write := &TodoWrite{}
	if _, err := write.Run(context.Background(), json.RawMessage(`{"action":"replace","id":"connected_privesc","items":["Update /etc/hosts","Verify webshell"]}`)); err != nil {
		t.Fatalf("replace with plan alias: %v", err)
	}
	if _, err := write.Run(context.Background(), json.RawMessage(`{"action":"update","id":"connected_privesc","status":"in_progress","detail":"Update /etc/hosts"}`)); err != nil {
		t.Fatalf("update via plan alias: %v", err)
	}
	items, err := LoadTodos()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if items[0].ID != "todo-1" || items[0].Status != "in_progress" || items[0].Detail != "Update /etc/hosts" {
		t.Fatalf("plan alias did not resolve to active item: %#v", items)
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
	db, cleanup, err := todoStore()
	if err != nil {
		t.Fatalf("todo store: %v", err)
	}
	defer cleanup()
	count, err := todoCountDB(db)
	if err != nil {
		t.Fatalf("todo count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected sqlite todos to be cleared, got %d", count)
	}
}

func TestTodoMigratesLegacyJSONIntoSQLite(t *testing.T) {
	withTempHome(t)
	legacy := []TodoItem{
		{ID: "todo-1", Text: "legacy", Status: "in_progress", CreatedAt: "2026-06-15T10:00:00Z", UpdatedAt: "2026-06-15T10:00:00Z"},
	}
	if err := saveTodosJSON(legacy); err != nil {
		t.Fatalf("legacy save: %v", err)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	SetTodoDB(db)
	t.Cleanup(func() { SetTodoDB(nil) })

	imported, err := MigrateTodosJSONToDB(db)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported = %d, want 1", imported)
	}
	items, err := LoadTodos()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(items) != 1 || items[0].Text != "legacy" {
		t.Fatalf("unexpected migrated todos: %#v", items)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "mauler", "todos.json")); err != nil {
		t.Fatalf("legacy todo file should remain available: %v", err)
	}
}

func TestRegistryIncludesTodoTools(t *testing.T) {
	registry := New()
	if _, ok := registry.Get("todo_write"); !ok {
		t.Fatalf("registry missing todo_write")
	}
}
