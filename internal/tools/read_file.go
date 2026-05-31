package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ReadFile reads a file, optionally restricted to a line range.
type ReadFile struct{}

func (t *ReadFile) Name() string      { return "read_file" }
func (t *ReadFile) Destructive() bool { return false }

func (t *ReadFile) Description() string {
	return "Read the contents of a file. Optionally restrict to a range of lines (1-indexed)."
}

func (t *ReadFile) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":       {"type": "string",  "description": "File path to read"},
    "start_line": {"type": "integer", "description": "First line to read (1-indexed, inclusive). Omit to read from the beginning."},
    "end_line":   {"type": "integer", "description": "Last line to read (1-indexed, inclusive). Omit to read to the end."}
  },
  "required": ["path"],
  "additionalProperties": false
}`)
}

type readFileParams struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func (t *ReadFile) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p readFileParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("read_file: bad params: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("read_file: path is required")
	}
	p.Path = NormalizeHostPath(p.Path)

	data, err := os.ReadFile(p.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("read_file: %w\n%s", err, MissingPathHint())
		}
		return "", fmt.Errorf("read_file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	total := len(lines)

	start := 1
	end := total
	if p.StartLine > 0 {
		start = p.StartLine
	}
	if p.EndLine > 0 {
		end = p.EndLine
	}

	// Clamp
	if start < 1 {
		start = 1
	}
	if end > total {
		end = total
	}
	if start > end {
		return "", fmt.Errorf("read_file: start_line %d > end_line %d", start, end)
	}

	// Build output with line numbers
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s  (lines %d-%d of %d)\n", p.Path, start, end, total)
	for i, line := range lines[start-1 : end] {
		fmt.Fprintf(&sb, "%4d\t%s\n", start+i, line)
	}
	return sb.String(), nil
}
