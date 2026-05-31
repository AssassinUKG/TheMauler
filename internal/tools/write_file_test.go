package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWriteFile(t *testing.T, params any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return (&WriteFile{}).Run(context.Background(), raw)
}

func TestWriteFileCreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	out, err := runWriteFile(t, map[string]any{"path": path, "content": "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "wrote") {
		t.Fatalf("unexpected result: %q", out)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Fatalf("file content = %q", data)
	}
}

func TestWriteFileOverwritesExistingFile(t *testing.T) {
	path := writeTemp(t, "old content")
	_, err := runWriteFile(t, map[string]any{"path": path, "content": "new content"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Fatalf("expected overwrite, got %q", data)
	}
}

func TestWriteFileCreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "file.txt")
	_, err := runWriteFile(t, map[string]any{"path": path, "content": "nested"})
	if err != nil {
		t.Fatalf("expected parent dirs to be created: %v", err)
	}
}

func TestWriteFileReportsLineCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lines.txt")
	content := "line1\nline2\nline3\n"
	out, err := runWriteFile(t, map[string]any{"path": path, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "4 lines") {
		t.Fatalf("expected line count in result, got %q", out)
	}
}

func TestWriteFileMissingPathError(t *testing.T) {
	_, err := runWriteFile(t, map[string]any{"path": "", "content": "x"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestWriteFileEmptyContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	_, err := runWriteFile(t, map[string]any{"path": path, "content": ""})
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Size() != 0 {
		t.Fatalf("expected empty file, size=%d", info.Size())
	}
}

func TestWriteFileAppendAddsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	_, err := runWriteFile(t, map[string]any{"path": path, "content": "line1\n"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := runWriteFile(t, map[string]any{"path": path, "content": "line2\n", "append": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "appended") {
		t.Fatalf("expected 'appended' in result, got %q", out)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "line1\nline2\n" {
		t.Fatalf("file content = %q, want line1+line2", string(got))
	}
}

func TestWriteFileAppendCreatesFileIfMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.txt")
	_, err := runWriteFile(t, map[string]any{"path": path, "content": "hello\n", "append": true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello\n" {
		t.Fatalf("file content = %q, want hello", string(got))
	}
}

func TestCountLinesSingleLine(t *testing.T) {
	if n := countLines("hello"); n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}
}

func TestCountLinesMultiple(t *testing.T) {
	if n := countLines("a\nb\nc"); n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}

func TestCountLinesTrailingNewline(t *testing.T) {
	if n := countLines("a\nb\n"); n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}
