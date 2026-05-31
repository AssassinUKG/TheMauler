package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile overwrites (or creates) a file with new content.
// When append is true, content is appended to the existing file instead.
type WriteFile struct{}

func (t *WriteFile) Name() string      { return "write_file" }
func (t *WriteFile) Destructive() bool { return true }

func (t *WriteFile) Description() string {
	return "Write content to a file, overwriting it entirely. " +
		"Set append=true to add content to the end of an existing file instead of overwriting. " +
		"Creates the file and any missing parent directories if they don't exist."
}

func (t *WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":    {"type": "string", "description": "Destination file path"},
    "content": {"type": "string", "description": "Full file content to write"},
    "append":  {"type": "boolean", "description": "If true, append content to the end of the file instead of overwriting (default false)"}
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`)
}

type writeFileParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
}

func (t *WriteFile) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p writeFileParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("write_file: bad params: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("write_file: path is required")
	}
	p.Path = NormalizeHostPath(p.Path)

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
		return fmt.Sprintf("appended %d lines to %s", lines, p.Path), nil
	}

	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	lines := countLines(p.Content)
	return fmt.Sprintf("wrote %s (%d lines)", p.Path, lines), nil
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
