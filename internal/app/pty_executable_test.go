package app

import (
	"context"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

func TestPTYExecutableResolvesBeforeWorkspaceDirectory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows ConPTY regression")
	}
	shell, err := resolvePTYExecutable("powershell.exe")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(shell) {
		t.Fatalf("not absolute: %q", shell)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	terminal, err := pty.New()
	if err != nil {
		t.Fatal(err)
	}
	closeTerminal := sync.OnceFunc(func() { _ = terminal.Close() })
	defer closeTerminal()
	cmd := terminal.Command(shell, "-NoProfile", "-NonInteractive", "-Command", "[Console]::Write('MAULER_PTY_PATH_OK')")
	cmd.Dir = t.TempDir()
	hidePtyShellWindow(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	read := make(chan string, 1)
	go func() { bytes, _ := io.ReadAll(terminal); read <- string(bytes) }()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	closeTerminal()
	select {
	case output := <-read:
		if !strings.Contains(output, "MAULER_PTY_PATH_OK") {
			t.Fatalf("missing marker: %q", output)
		}
	case <-ctx.Done():
		t.Fatal("PTY reader did not join")
	}
}

func TestPTYExecutableRejectsMissingProgram(t *testing.T) {
	if _, err := resolvePTYExecutable("mauler-no-such-shell-34687919"); err == nil {
		t.Fatal("missing executable admitted")
	}
}
