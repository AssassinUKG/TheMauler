package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	wslMountRE = regexp.MustCompile(`^/mnt/([a-zA-Z])(?:/(.*))?$`)
	msysPathRE = regexp.MustCompile(`^/([a-zA-Z])(?:/(.*))?$`)
	winDriveRE = regexp.MustCompile(`^([a-zA-Z]):[\\/]*(.*)$`)
)

// NormalizeHostPath converts common Windows/WSL path forms to the path format
// usable by the current Go process.
func NormalizeHostPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if runtime.GOOS == "windows" {
		if m := wslMountRE.FindStringSubmatch(path); m != nil {
			return strings.ToUpper(m[1]) + ":\\" + slashToBackslash(m[2])
		}
		if m := msysPathRE.FindStringSubmatch(path); m != nil {
			return strings.ToUpper(m[1]) + ":\\" + slashToBackslash(m[2])
		}
		return filepath.Clean(path)
	}
	if isWSL() {
		if m := winDriveRE.FindStringSubmatch(path); m != nil {
			rest := strings.ReplaceAll(m[2], "\\", "/")
			drive := strings.ToLower(m[1])
			if rest == "" {
				return "/mnt/" + drive
			}
			return "/mnt/" + drive + "/" + rest
		}
	}
	return filepath.Clean(path)
}

func WindowsPathToWSL(path string) string {
	path = NormalizeHostPath(path)
	if m := winDriveRE.FindStringSubmatch(path); m != nil {
		rest := strings.ReplaceAll(m[2], "\\", "/")
		drive := strings.ToLower(m[1])
		if rest == "" {
			return "/mnt/" + drive
		}
		return "/mnt/" + drive + "/" + rest
	}
	return strings.ReplaceAll(path, "\\", "/")
}

func slashToBackslash(path string) string {
	if path == "" {
		return ""
	}
	return strings.ReplaceAll(path, "/", "\\")
}

func CurrentWorkingDir() string {
	wd, _ := os.Getwd()
	return filepath.ToSlash(wd)
}

func MissingPathHint() string {
	wd := CurrentWorkingDir()
	entries := TopLevelEntries(20)
	if len(entries) == 0 {
		return "Current workspace: " + wd + ". The requested path does not exist here. Use glob with pattern \"**/*\" to discover valid paths before retrying."
	}
	return "Current workspace: " + wd + ". Top-level entries: " + strings.Join(entries, ", ") + ". The requested path does not exist here. Use glob with pattern \"**/*\" or read one of these files before retrying."
}

func TopLevelEntries(limit int) []string {
	if limit <= 0 {
		return nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		return nil
	}
	out := make([]string, 0, min(len(entries), limit))
	for _, entry := range entries {
		name := entry.Name()
		if name == ".git" || name == "node_modules" || name == "dist" || name == "build" || name == "target" {
			continue
		}
		if entry.IsDir() {
			name += "/"
		}
		out = append(out, name)
		if len(out) >= limit {
			break
		}
	}
	return out
}
