//go:build !windows

package app

import (
	"os/exec"

	pty "github.com/aymanbagabas/go-pty"
)

// hideShellWindow is a no-op on non-Windows platforms.
func hideShellWindow(_ *exec.Cmd) {}

func hidePtyShellWindow(_ *pty.Cmd) {}
