//go:build !windows

package app

import "os/exec"

// hideShellWindow is a no-op on non-Windows platforms.
func hideShellWindow(_ *exec.Cmd) {}
