package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// WriteFile overwrites (or creates) a file with new content.
// When append is true, content is appended to the existing file instead.
type WriteFile struct{}

func (t *WriteFile) Name() string      { return "write_file" }
func (t *WriteFile) Destructive() bool { return true }

func (t *WriteFile) Description() string {
	return "Write content to a file, overwriting it entirely. " +
		"Set append=true to add content to the end of an existing file instead of overwriting. " +
		"After appending to a path in an agent run, set overwrite=true only when intentionally replacing the accumulated file. " +
		"Creates the file and any missing parent directories if they don't exist."
}

func (t *WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":    {"type": "string", "description": "Destination file path"},
    "content": {"type": "string", "description": "Full file content to write"},
    "append":  {"type": "boolean", "description": "If true, append content to the end of the file instead of overwriting (default false)"},
    "overwrite": {"type": "boolean", "description": "Explicitly allow replacing a path already appended to during this run. Leave false when continuing chunked output (default false)"}
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`)
}

type writeFileParams struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Append    bool   `json:"append"`
	Overwrite bool   `json:"overwrite"`
}

func (t *WriteFile) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p writeFileParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("write_file: bad params: %w", err)
	}
	cleanPath, err := cleanCompactPathArgFor("write_file", p.Path)
	if err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	p.Path = cleanPath
	if p.Path == "" {
		return "", fmt.Errorf("write_file: path is required")
	}
	rawPath := strings.TrimSpace(p.Path)
	p.Path = NormalizeHostPath(p.Path)
	if err := rejectProtectedMutationPath(p.Path); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	guarded, note := guardFileContent(rawPath, p.Content)
	p.Content = guarded
	if shouldWriteViaWSL(rawPath) {
		out, err := writeFileViaWSL(rawPath, p.Content, p.Append)
		return withGuardNote(out, note), err
	}

	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return "", fmt.Errorf("write_file: mkdir: %w", err)
	}

	if p.Append {
		f, err := os.OpenFile(p.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return "", fmt.Errorf("write_file: open for append: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString(p.Content); err != nil {
			return "", fmt.Errorf("write_file: append: %w", err)
		}
		lines := countLines(p.Content)
		return withGuardNote(fmt.Sprintf("appended %d lines to %s", lines, p.Path), note), nil
	}

	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	lines := countLines(p.Content)
	return withGuardNote(fmt.Sprintf("wrote %s (%d lines)", p.Path, lines), note), nil
}

func withGuardNote(result, note string) string {
	if note == "" {
		return result
	}
	return result + "\n" + note
}

// ShouldUseWSLForPath reports whether a path should be read or written inside
// WSL rather than on the Windows host — i.e. a Linux-absolute path on a
// WSL-backed Windows host. Mutation verification uses this to mirror the routing
// write_file already does, so it doesn't os.Stat a /tmp/... path that only
// exists inside the WSL filesystem.
func ShouldUseWSLForPath(path string) bool { return shouldWriteViaWSL(path) }

// ReadFileViaWSL returns the contents of a WSL-internal file by running `cat`
// inside the active distro. Used to verify a file written via writeFileViaWSL.
func ReadFileViaWSL(path string) ([]byte, error) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("invalid WSL path %q", path)
	}
	args := []string{}
	if distro := activeWSLDistro(); distro != "" {
		args = append(args, "-d", distro)
	}
	args = append(args, "--", "bash", "-lc", fmt.Sprintf("cat -- %s", shellQuote(path)))
	cmd := exec.Command("wsl.exe", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return out, nil
}

func shouldWriteViaWSL(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	if detectShellBackend("") != "wsl" {
		return false
	}
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if !strings.HasPrefix(path, "/") {
		return false
	}
	if wslMountRE.MatchString(path) || msysPathRE.MatchString(path) {
		return false
	}
	return true
}

func writeFileViaWSL(path, content string, appendMode bool) (string, error) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("write_file: invalid WSL path %q", path)
	}
	op := ">"
	action := "wrote"
	if appendMode {
		op = ">>"
		action = "appended"
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	script := fmt.Sprintf("mkdir -p -- %s && cat %s %s", shellQuote(dir), op, shellQuote(path))
	if dir == "." || dir == "/" {
		script = fmt.Sprintf("cat %s %s", op, shellQuote(path))
	}
	args := []string{}
	if distro := activeWSLDistro(); distro != "" {
		args = append(args, "-d", distro)
	}
	args = append(args, "--", "bash", "-lc", script)
	cmd := exec.Command("wsl.exe", args...)
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			return "", fmt.Errorf("write_file: wsl write: %w: %s", err, detail)
		}
		return "", fmt.Errorf("write_file: wsl write: %w", err)
	}
	lines := countLines(content)
	return fmt.Sprintf("%s %s (%d lines, WSL)", action, path, lines), nil
}

func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
