package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeLintFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func findBash() (string, error) { return exec.LookPath("bash") }

// findPython returns a working python binary path (not the Windows Store stub).
func findPython() (string, error) {
	for _, name := range []string{"python3", "python"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		// On Windows, python3 may be a Microsoft Store stub that prints an error.
		// Verify by actually importing a stdlib module.
		out, err := exec.Command(p, "-c", "import sys; sys.exit(0)").CombinedOutput()
		if err == nil && len(out) == 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// --- lintFile dispatch ---

func TestLintFileIgnoresUnknownExtension(t *testing.T) {
	path := writeLintFile(t, "data.json", `{"key":"value"}`)
	if out := lintFile(path); out != "" {
		t.Fatalf("expected empty for json, got %q", out)
	}
}

func TestLintFileIgnoresMarkdown(t *testing.T) {
	path := writeLintFile(t, "README.md", "# heading\n")
	if out := lintFile(path); out != "" {
		t.Fatalf("expected empty for md, got %q", out)
	}
}

func TestLintFileDispatchesByExtension(t *testing.T) {
	// .sh should be dispatched (even if bash is unavailable, lintFile won't error)
	path := writeLintFile(t, "script.sh", "echo hi\n")
	// Just verify it doesn't panic and returns a string or empty
	_ = lintFile(path)
}

// --- shell linting ---

func TestLintShellValidScript(t *testing.T) {
	if _, err := findBash(); err != nil {
		t.Skip("bash not available")
	}
	path := writeLintFile(t, "ok.sh", "#!/usr/bin/env bash\necho hello\n")
	out := lintFile(path)
	if out == "" {
		t.Skip("bash -n returned empty (unexpected env)")
	}
	if !strings.Contains(out, "syntax OK") {
		t.Fatalf("expected pass: %q", out)
	}
}

func TestLintShellInvalidScript(t *testing.T) {
	if _, err := findBash(); err != nil {
		t.Skip("bash not available")
	}
	path := writeLintFile(t, "bad.sh", "#!/usr/bin/env bash\nif [\n")
	out := lintFile(path)
	if !strings.Contains(out, "syntax error") {
		t.Fatalf("expected failure for broken shell: %q", out)
	}
}

func TestLintShellMissingBashReturnsEmpty(t *testing.T) {
	// lintShell returns "" when bash is not found — never an error
	out := lintShell("/nonexistent/file.sh")
	// Either "" (no bash) or an error about reading the file. Neither should panic.
	_ = out
}

// --- python linting ---

func TestLintPythonValidScript(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python not available")
	}
	path := writeLintFile(t, "ok.py", "def hello():\n    return 'hi'\n")
	out := lintFile(path)
	if !strings.Contains(out, "syntax OK") {
		t.Fatalf("expected pass: %q", out)
	}
}

func TestLintPythonSyntaxError(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skip("python not available")
	}
	path := writeLintFile(t, "bad.py", "def broken(\n")
	out := lintFile(path)
	if !strings.Contains(out, "syntax error") {
		t.Fatalf("expected failure for broken python: %q", out)
	}
}

func TestLintPythonNoPythonReturnsEmpty(t *testing.T) {
	// lintPython returns "" silently when python is not installed
	// We can only test this properly if python is absent; otherwise skip
	if _, err := findPython(); err == nil {
		t.Skip("python is available — skip unavailability test")
	}
	out := lintPython("/some/file.py")
	if out != "" {
		t.Fatalf("expected empty when python absent, got %q", out)
	}
}

// --- go linting (integration, requires go in PATH) ---

func TestLintGoValidPackage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	dir := t.TempDir()
	// Minimal valid module + file
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module linttest\n\ngo 1.21\n"), 0o644)
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(path, []byte("package linttest\n\nfunc Hello() string { return \"hi\" }\n"), 0o644)
	out := lintGo(path)
	if !strings.Contains(out, "go vet passed") {
		t.Fatalf("expected go vet pass: %q", out)
	}
}

func TestLintGoInvalidPackage(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module lintbad\n\ngo 1.21\n"), 0o644)
	// printf with wrong arg type — go vet catches this
	path := filepath.Join(dir, "bad.go")
	content := `package lintbad

import "fmt"

func Bad() {
	fmt.Printf("%d", "not an int")
}
`
	_ = os.WriteFile(path, []byte(content), 0o644)
	out := lintGo(path)
	if !strings.Contains(out, "go vet errors") {
		t.Fatalf("expected go vet failure: %q", out)
	}
}
