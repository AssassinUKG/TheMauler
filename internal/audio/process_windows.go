//go:build windows

package audio

import (
	"os/exec"
	"syscall"
)

// hideChildProcessWindow keeps audio helpers (notably the persistent Kokoro
// Python worker) in the background instead of opening a Windows Terminal tab.
func hideChildProcessWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
