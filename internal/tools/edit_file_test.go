package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func runEditFile(t *testing.T, params any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return (&EditFile{}).Run(context.Background(), raw)
}

func TestEditFileBasicReplacement(t *testing.T) {
	path := writeTemp(t, "foo bar baz")
	_, err := runEditFile(t, map[string]any{
		"path":       path,
		"old_string": "bar",
		"new_string": "REPLACED",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "foo REPLACED baz" {
		t.Fatalf("unexpected content: %q", data)
	}
}

func TestEditFileMultiLineReplacement(t *testing.T) {
	path := writeTemp(t, "func foo() {\n\treturn 1\n}\n")
	_, err := runEditFile(t, map[string]any{
		"path":       path,
		"old_string": "\treturn 1\n",
		"new_string": "\treturn 42\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "return 42") {
		t.Fatalf("replacement not applied: %q", data)
	}
}

func TestEditFileOldStringNotFound(t *testing.T) {
	path := writeTemp(t, "hello world")
	_, err := runEditFile(t, map[string]any{
		"path":       path,
		"old_string": "nothere",
		"new_string": "x",
	})
	if err == nil {
		t.Fatal("expected error when old_string absent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEditFileAmbiguousOldStringError(t *testing.T) {
	path := writeTemp(t, "abc abc abc")
	_, err := runEditFile(t, map[string]any{
		"path":       path,
		"old_string": "abc",
		"new_string": "xyz",
	})
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "3 locations") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEditFileReplacesOnlyFirstOccurrenceWhenUnique(t *testing.T) {
	path := writeTemp(t, "keep AAA end\n")
	_, err := runEditFile(t, map[string]any{
		"path":       path,
		"old_string": "AAA",
		"new_string": "BBB",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "keep BBB end\n" {
		t.Fatalf("unexpected: %q", data)
	}
}

func TestEditFileMissingPathError(t *testing.T) {
	_, err := runEditFile(t, map[string]any{"path": "", "old_string": "x", "new_string": "y"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestEditFileMissingOldStringError(t *testing.T) {
	path := writeTemp(t, "something")
	_, err := runEditFile(t, map[string]any{"path": path, "old_string": "", "new_string": "y"})
	if err == nil {
		t.Fatal("expected error for empty old_string")
	}
}

func TestEditFileReportsLineCounts(t *testing.T) {
	path := writeTemp(t, "line1\nline2\nline3\n")
	out, err := runEditFile(t, map[string]any{
		"path":       path,
		"old_string": "line2\n",
		"new_string": "newA\nnewB\nnewC\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	// "line2\n" has 1 newline → strings.Count + 1 = 2 lines
	// "newA\nnewB\nnewC\n" has 3 newlines → 4 lines
	if !strings.Contains(out, "2 line(s)") || !strings.Contains(out, "4 line(s)") {
		t.Fatalf("expected line counts in result: %q", out)
	}
}

func TestEditFileNotFoundOnDisk(t *testing.T) {
	_, err := runEditFile(t, map[string]any{
		"path":       "/nonexistent/path/file.txt",
		"old_string": "x",
		"new_string": "y",
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
