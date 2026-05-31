package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func runReadFile(t *testing.T, params any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	return (&ReadFile{}).Run(context.Background(), raw)
}

func TestReadFileFullContent(t *testing.T) {
	path := writeTemp(t, "alpha\nbeta\ngamma\n")
	out, err := runReadFile(t, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "gamma") {
		t.Fatalf("missing content: %q", out)
	}
}

func TestReadFileLineNumbers(t *testing.T) {
	path := writeTemp(t, "one\ntwo\nthree\n")
	out, err := runReadFile(t, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "   1\t") {
		t.Fatalf("expected line numbers: %q", out)
	}
}

func TestReadFileStartEndLine(t *testing.T) {
	path := writeTemp(t, "L1\nL2\nL3\nL4\nL5\n")
	out, err := runReadFile(t, map[string]any{"path": path, "start_line": 2, "end_line": 3})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "L1") || !strings.Contains(out, "L2") || !strings.Contains(out, "L3") || strings.Contains(out, "L4") {
		t.Fatalf("unexpected range result: %q", out)
	}
}

func TestReadFileStartLineOnly(t *testing.T) {
	path := writeTemp(t, "A\nB\nC\n")
	out, err := runReadFile(t, map[string]any{"path": path, "start_line": 2})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "   1\tA") || !strings.Contains(out, "B") || !strings.Contains(out, "C") {
		t.Fatalf("unexpected: %q", out)
	}
}

func TestReadFileInvalidRangeError(t *testing.T) {
	path := writeTemp(t, "X\nY\n")
	_, err := runReadFile(t, map[string]any{"path": path, "start_line": 5, "end_line": 3})
	if err == nil {
		t.Fatal("expected error for start > end")
	}
}

func TestReadFileMissingPathError(t *testing.T) {
	_, err := runReadFile(t, map[string]any{"path": ""})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestReadFileNotFound(t *testing.T) {
	_, err := runReadFile(t, map[string]any{"path": filepath.Join(t.TempDir(), "nope.txt")})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFileClampEndBeyondFileLength(t *testing.T) {
	path := writeTemp(t, "only\ntwo\n")
	out, err := runReadFile(t, map[string]any{"path": path, "end_line": 9999})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "only") || !strings.Contains(out, "two") {
		t.Fatalf("unexpected: %q", out)
	}
}
