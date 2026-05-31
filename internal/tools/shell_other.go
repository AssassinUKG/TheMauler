//go:build !windows

package tools

import "os/exec"

func applyHiddenWindow(_ *exec.Cmd) {}
