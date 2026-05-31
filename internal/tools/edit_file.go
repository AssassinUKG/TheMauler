package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EditFile replaces an exact string in a file with a new string.
// The old_string must appear exactly once - the model is told to provide
// enough surrounding context to make it unique.
type EditFile struct{}

func (t *EditFile) Name() string      { return "edit_file" }
func (t *EditFile) Destructive() bool { return true }

func (t *EditFile) Description() string {
	return `Replace an exact string in a file. old_string must appear exactly once in the file.
Include enough surrounding lines to make old_string unique if the target text is repeated.
new_string replaces old_string in full - include all the lines you want to keep.
Always call read_file first to confirm the current content before editing.`
}

func (t *EditFile) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":       {"type": "string", "description": "File to edit"},
    "old_string": {"type": "string", "description": "Exact text to replace (must be unique in the file)"},
    "new_string": {"type": "string", "description": "Text to insert in place of old_string"}
  },
  "required": ["path", "old_string", "new_string"],
  "additionalProperties": false
}`)
}

type editFileParams struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (t *EditFile) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p editFileParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("edit_file: bad params: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("edit_file: path is required")
	}
	if p.OldString == "" {
		return "", fmt.Errorf("edit_file: old_string is required")
	}
	p.Path = NormalizeHostPath(p.Path)

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: read %s: %w", p.Path, err)
	}
	content := string(data)

	count := strings.Count(content, p.OldString)
	switch count {
	case 0:
		return "", fmt.Errorf("edit_file: old_string not found in %s - check the exact text including whitespace", p.Path)
	case 1:
		// exactly one match — proceed
	default:
		return "", fmt.Errorf("edit_file: old_string matches %d locations in %s - provide more surrounding context to make it unique", count, p.Path)
	}

	newContent := strings.Replace(content, p.OldString, p.NewString, 1)
	if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("edit_file: write %s: %w", p.Path, err)
	}

	// Report a compact diff summary
	oldLines := strings.Count(p.OldString, "\n") + 1
	newLines := strings.Count(p.NewString, "\n") + 1
	return fmt.Sprintf("edited %s: replaced %d line(s) with %d line(s)", p.Path, oldLines, newLines), nil
}
