package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGrep(t *testing.T, params any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return (&Grep{}).Run(context.Background(), raw)
}

func makeGrepTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"alpha.go":       "package main\n\nfunc Hello() string {\n\treturn \"hello world\"\n}\n",
		"beta.go":        "package main\n\nfunc Goodbye() string {\n\treturn \"goodbye\"\n}\n",
		"notes.txt":      "This is a NOTE about something important.\nnote: lowercase too.\n",
		"sub/gamma.go":   "package sub\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n",
		"skip.png":       "\x89PNG\r\n", // binary — should be skipped
	}
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestGrepFindsPattern(t *testing.T) {
	root := makeGrepTree(t)
	out, err := runGrep(t, map[string]any{"pattern": "Hello", "path": root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha.go") || !strings.Contains(out, "Hello") {
		t.Fatalf("expected match in alpha.go: %q", out)
	}
}

func TestGrepCaseInsensitive(t *testing.T) {
	root := makeGrepTree(t)
	out, err := runGrep(t, map[string]any{"pattern": "NOTE", "path": root, "case_sensitive": false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NOTE") || !strings.Contains(out, "note:") {
		t.Fatalf("expected both NOTE and note: %q", out)
	}
}

func TestGrepCaseSensitiveNoFalsePositive(t *testing.T) {
	root := makeGrepTree(t)
	out, err := runGrep(t, map[string]any{"pattern": "NOTE", "path": root, "case_sensitive": true})
	if err != nil {
		t.Fatal(err)
	}
	// "note:" (lowercase) should not appear
	if strings.Contains(out, "note:") {
		t.Fatalf("lowercase note should not match case-sensitive NOTE: %q", out)
	}
}

func TestGrepGlobFilter(t *testing.T) {
	root := makeGrepTree(t)
	out, err := runGrep(t, map[string]any{"pattern": "func", "path": root, "glob": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "notes.txt") {
		t.Fatalf("txt file should be excluded by glob: %q", out)
	}
	if !strings.Contains(out, "alpha.go") {
		t.Fatalf("alpha.go should match: %q", out)
	}
}

func TestGrepNoMatch(t *testing.T) {
	root := makeGrepTree(t)
	out, err := runGrep(t, map[string]any{"pattern": "ZZZNOMATCH", "path": root})
	if err != nil {
		t.Fatal(err)
	}
	if out != "no matches" {
		t.Fatalf("expected no matches, got %q", out)
	}
}

func TestGrepMissingPatternError(t *testing.T) {
	_, err := runGrep(t, map[string]any{"pattern": ""})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestGrepInvalidRegexError(t *testing.T) {
	_, err := runGrep(t, map[string]any{"pattern": "[invalid"})
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestGrepSkipsBinaryFiles(t *testing.T) {
	root := makeGrepTree(t)
	// PNG file exists in the tree with content \x89PNG
	out, err := runGrep(t, map[string]any{"pattern": "PNG", "path": root})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "skip.png") {
		t.Fatalf("binary file should be skipped: %q", out)
	}
}

func TestGrepSearchesSubdirectories(t *testing.T) {
	root := makeGrepTree(t)
	out, err := runGrep(t, map[string]any{"pattern": "Add", "path": root})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gamma.go") {
		t.Fatalf("expected match in sub/gamma.go: %q", out)
	}
}

func TestGrepLineNumbersIncluded(t *testing.T) {
	root := makeGrepTree(t)
	out, err := runGrep(t, map[string]any{"pattern": "hello world", "path": root})
	if err != nil {
		t.Fatal(err)
	}
	// Output format is "file:lineno: text"
	if !strings.Contains(out, ":4:") {
		t.Fatalf("expected line number 4: %q", out)
	}
}

func TestIsBinaryExt(t *testing.T) {
	for _, ext := range []string{".png", ".jpg", ".pdf", ".exe", ".zip", ".so", ".pyc"} {
		if !isBinaryExt(ext) {
			t.Errorf("%s should be binary", ext)
		}
	}
	for _, ext := range []string{".go", ".ts", ".txt", ".json", ".md"} {
		if isBinaryExt(ext) {
			t.Errorf("%s should not be binary", ext)
		}
	}
}
