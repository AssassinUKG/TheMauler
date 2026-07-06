package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanCompactPathArgStripsClosingToolTag(t *testing.T) {
	got, err := cleanCompactPathArg(`C:/Users/richa/Documents/HTB_writeups/scans</path>`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `C:/Users/richa/Documents/HTB_writeups/scans` {
		t.Fatalf("path=%q", got)
	}
}

func TestCleanCompactPathArgRejectsMarkup(t *testing.T) {
	if _, err := cleanCompactPathArg(`<path>C:/bad`); err == nil {
		t.Fatal("expected malformed path markup to be rejected")
	}
}

func TestCleanCompactPathArgsStripsClosingTagsInArrays(t *testing.T) {
	got, err := cleanCompactPathArgs([]string{`notes/a.txt</path>`, `notes/b.txt</file>`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != `notes/a.txt,notes/b.txt` {
		t.Fatalf("paths=%#v", got)
	}
}

func TestWriteFileStripsClosingPathTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	raw, _ := json.Marshal(map[string]any{
		"path":    path + "</path>",
		"content": "ok",
	})
	if _, err := (&WriteFile{}).Run(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Fatalf("content=%q", data)
	}
}

func TestEditFileStripsClosingPathTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"path":       path + "</file>",
		"old_string": "before",
		"new_string": "after",
	})
	if _, err := (&EditFile{}).Run(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "after" {
		t.Fatalf("content=%q", data)
	}
}

func TestWriteFileRejectsInlinePathMarkup(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"path":    `<path>C:/bad`,
		"content": "bad",
	})
	_, err := (&WriteFile{}).Run(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "malformed tool markup") {
		t.Fatalf("expected malformed markup error, got %v", err)
	}
}
