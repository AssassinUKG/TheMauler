package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runReadMany(t *testing.T, params any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return (&ReadMany{}).Run(context.Background(), raw)
}

func TestReadManyReadsMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("content A"), 0o644)
	_ = os.WriteFile(b, []byte("content B"), 0o644)

	out, err := runReadMany(t, map[string]any{"paths": []string{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "content A") || !strings.Contains(out, "content B") {
		t.Fatalf("missing content: %q", out)
	}
}

func TestReadManyIncludesLineNumbers(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "nums.txt")
	_ = os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0o644)

	out, err := runReadMany(t, map[string]any{"paths": []string{f}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "   1\t") || !strings.Contains(out, "   3\t") {
		t.Fatalf("expected line numbers: %q", out)
	}
}

func TestReadManyInlineErrorForMissingFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	bad := filepath.Join(dir, "missing.txt")
	_ = os.WriteFile(good, []byte("ok"), 0o644)

	out, err := runReadMany(t, map[string]any{"paths": []string{good, bad}})
	if err != nil {
		t.Fatal("expected no Go error, got: " + err.Error())
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("good file content missing: %q", out)
	}
	if !strings.Contains(out, "ERROR") {
		t.Fatalf("expected inline error for missing file: %q", out)
	}
}

func TestReadManyEmptyPathsError(t *testing.T) {
	_, err := runReadMany(t, map[string]any{"paths": []string{}})
	if err == nil {
		t.Fatal("expected error for empty paths")
	}
}

func TestReadManyTruncatesAt20Files(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 25)
	for i := range paths {
		p := filepath.Join(dir, strings.Repeat("a", i+1)+".txt")
		_ = os.WriteFile(p, []byte("x"), 0o644)
		paths[i] = p
	}
	// Should silently truncate to 20 without error
	out, err := runReadMany(t, map[string]any{"paths": paths})
	if err != nil {
		t.Fatal(err)
	}
	// Count "# " headers to see how many files were read
	count := strings.Count(out, "# ")
	if count != 20 {
		t.Fatalf("expected 20 files read, got %d", count)
	}
}

func TestReadManyHeaderIncludesPath(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "named.go")
	_ = os.WriteFile(f, []byte("package main\n"), 0o644)

	out, err := runReadMany(t, map[string]any{"paths": []string{f}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "named.go") {
		t.Fatalf("expected filename in header: %q", out)
	}
}
