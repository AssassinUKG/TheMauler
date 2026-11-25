//go:build !windows

package mcpclient

import "os/exec"

func hideChildWindow(_ *exec.Cmd) {}
