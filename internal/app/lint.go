package app

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mauler/internal/tools"
)

// lintFile runs a fast, language-appropriate check on the given file path and
// returns a one-line summary plus any error output. Returns ("", nil) for file
// types we don't check. Never returns a Go error - failures become informational
// strings that get appended to the tool result so the model can self-correct.
func lintFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return lintGo(path)
	case ".py":
		return lintPython(path)
	case ".sh":
		return lintShell(path)
	default:
		return ""
	}
}

func lintGo(path string) string {
	dir := filepath.Dir(path)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "vet", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	out = bytes.TrimSpace(out)

	if err == nil {
		return "[ok] go vet passed"
	}
	if len(out) == 0 {
		return fmt.Sprintf("[fail] go vet failed: %v", err)
	}
	msg := string(out)
	if len(msg) > 600 {
		msg = msg[:600] + "\n... (truncated)"
	}
	return fmt.Sprintf("[fail] go vet errors:\n%s", msg)
}

func lintPython(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	binary := ""
	for _, name := range []string{"python3", "python"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if out, err := exec.Command(p, "-c", "import sys; sys.exit(0)").CombinedOutput(); err == nil && len(out) == 0 {
			binary = p
			break
		}
	}
	if binary == "" {
		return ""
	}

	cmd := exec.CommandContext(ctx, binary, "-m", "py_compile", path)
	out, err := cmd.CombinedOutput()
	out = bytes.TrimSpace(out)
	if err == nil {
		return "[ok] python syntax OK"
	}
	msg := string(out)
	if len(msg) > 400 {
		msg = msg[:400] + "\n... (truncated)"
	}
	return fmt.Sprintf("[fail] python syntax error:\n%s", msg)
}

func lintShell(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := exec.LookPath("bash"); err != nil {
		return ""
	}
	candidates := []string{path}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.ToSlash(path), tools.WindowsPathToWSL(path))
	}
	var lastOut []byte
	for _, candidate := range candidates {
		cmd := exec.CommandContext(ctx, "bash", "-n", candidate)
		out, err := cmd.CombinedOutput()
		out = bytes.TrimSpace(out)
		if err == nil {
			return "[ok] bash -n syntax OK"
		}
		lastOut = out
		if !strings.Contains(strings.ToLower(string(out)), "no such file") {
			break
		}
	}
	return fmt.Sprintf("[fail] bash syntax error:\n%s", string(lastOut))
}
