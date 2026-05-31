//go:build windows

package app

import (
	"os/exec"
	"syscall"
)

// hideShellWindow prevents the spawned shell from creating a visible console
// window on Windows.  CREATE_NO_WINDOW (0x08000000) suppresses the window
// entirely; HideWindow is an additional belt-and-suspenders flag.
func hideShellWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
