package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeHostPathWindowsForms(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific normalization")
	}
	tests := map[string]string{
		"/mnt/c/Users/richa/project": `C:\Users\richa\project`,
		"/c/Users/richa/project":     `C:\Users\richa\project`,
	}
	for input, want := range tests {
		if got := NormalizeHostPath(input); got != want {
			t.Fatalf("NormalizeHostPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWindowsPathToWSL(t *testing.T) {
	got := WindowsPathToWSL(`C:\Users\richa\Desktop\TheMauler`)
	want := "/mnt/c/Users/richa/Desktop/TheMauler"
	if got != want {
		t.Fatalf("WindowsPathToWSL = %q, want %q", got, want)
	}
}

func TestMissingReadFileIncludesWorkspaceHint(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "idea.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	_, err := (&ReadFile{}).Run(context.Background(), json.RawMessage(`{"path":"main.go"}`))
	if err == nil {
		t.Fatal("expected missing path error")
	}
	msg := err.Error()
	for _, want := range []string{"Current workspace:", filepath.ToSlash(project), "idea.md", "glob"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing path hint lacks %q:\n%s", want, msg)
		}
	}
}

func TestReadManyMissingPathIncludesOneWorkspaceHint(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "app.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	out, err := (&ReadMany{}).Run(context.Background(), json.RawMessage(`{"paths":["main.go","wails.json"]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# main.go  ERROR:", "# wails.json  ERROR:", "Workspace note:", filepath.ToSlash(project), "app.py"} {
		if !strings.Contains(out, want) {
			t.Fatalf("read_many output lacks %q:\n%s", want, out)
		}
	}
	if count := strings.Count(out, "Workspace note:"); count != 1 {
		t.Fatalf("workspace note count = %d, want 1:\n%s", count, out)
	}
}

func restoreWorkingDir(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	})
}
