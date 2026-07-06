package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mauler/internal/settings"
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
		"path":       filepath.Join(t.TempDir(), "missing", "file.txt"),
		"old_string": "x",
		"new_string": "y",
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestApplyExactEditKeepsHostAndWSLRoutesIdentical(t *testing.T) {
	got, oldLines, newLines, err := applyExactEdit("a\nold\nz\n", "old\n", "new\nline\n", "/tmp/example.py")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nnew\nline\nz\n" {
		t.Fatalf("content = %q", got)
	}
	if oldLines != 2 || newLines != 3 {
		t.Fatalf("line counts = %d/%d, want 2/3", oldLines, newLines)
	}
}

func TestEditFileWSLAbsolutePathRoutesToWSL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("WSL edit routing is Windows-specific")
	}
	cfg := settings.DefaultSettings().Tools
	cfg.ShellBackend = "wsl"
	SetConfigSnapshot(cfg)
	t.Cleanup(ResetConfigSnapshot)
	if !ShouldUseWSLForPath("/tmp/mauler_edit_route_probe.txt") {
		t.Skip("WSL backend is not active for Linux-absolute paths")
	}
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		t.Skip("wsl.exe not available")
	}

	name := "mauler_edit_file_test_" + strings.ReplaceAll(filepath.Base(t.TempDir()), "\\", "_") + ".txt"
	wslPath := "/tmp/" + name
	seed := "alpha\nold block\nomega\n"
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer setupCancel()
	createCmd := exec.CommandContext(setupCtx, "wsl.exe", "--", "bash", "-lc", "printf '%s' "+shellQuote(seed)+" > "+shellQuote(wslPath))
	if out, err := createCmd.CombinedOutput(); err != nil {
		if setupCtx.Err() != nil {
			t.Skipf("WSL not usable for test setup: %v", setupCtx.Err())
		}
		t.Skipf("WSL not usable for test setup: %v: %s", err, strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = exec.CommandContext(cleanupCtx, "wsl.exe", "--", "bash", "-lc", "rm -f -- "+shellQuote(wslPath)).Run()
	})

	out, err := runEditFile(t, map[string]any{
		"path":       wslPath,
		"old_string": "old block",
		"new_string": "new block",
	})
	if err != nil {
		t.Fatalf("edit_file should edit WSL-internal /tmp path, got: %v", err)
	}
	if !strings.Contains(out, "(WSL)") {
		t.Fatalf("result should identify WSL edit route: %q", out)
	}
	data, err := ReadFileViaWSL(wslPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\nnew block\nomega\n" {
		t.Fatalf("WSL file content = %q", data)
	}
}
