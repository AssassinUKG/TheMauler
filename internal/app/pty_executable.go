package app

import (
	"os/exec"
	"path/filepath"
)

func resolvePTYExecutable(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}
