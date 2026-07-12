//go:build !windows

package audio

import "os/exec"

func hideChildProcessWindow(_ *exec.Cmd) {}
