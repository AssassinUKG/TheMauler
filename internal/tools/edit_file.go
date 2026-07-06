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
	cleanPath, err := cleanCompactPathArgFor("edit_file", p.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	p.Path = cleanPath
	if p.Path == "" {
		return "", fmt.Errorf("edit_file: path is required")
	}
	if p.OldString == "" {
		return "", fmt.Errorf("edit_file: old_string is required")
	}

	rawPath := strings.TrimSpace(p.Path)
	p.Path = NormalizeHostPath(p.Path)
	if err := rejectProtectedMutationPath(p.Path); err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	if shouldWriteViaWSL(rawPath) {
		return editFileViaWSL(rawPath, p.OldString, p.NewString)
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: read %s: %w", p.Path, err)
	}
	newContent, oldLines, newLines, err := applyExactEdit(string(data), p.OldString, p.NewString, p.Path)
	if err != nil {
		return "", err
	}

	newContent, note := guardFileContent(p.Path, newContent)
	if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
		return "", fmt.Errorf("edit_file: write %s: %w", p.Path, err)
	}

	return withGuardNote(fmt.Sprintf("edited %s: replaced %d line(s) with %d line(s)", p.Path, oldLines, newLines), note), nil
}

func applyExactEdit(content, oldString, newString, displayPath string) (string, int, int, error) {
	count := strings.Count(content, oldString)
	switch count {
	case 0:
		return "", 0, 0, fmt.Errorf("edit_file: old_string not found in %s - check the exact text including whitespace", displayPath)
	case 1:
		// exactly one match - proceed
	default:
		return "", 0, 0, fmt.Errorf("edit_file: old_string matches %d locations in %s - provide more surrounding context to make it unique", count, displayPath)
	}
	oldLines := strings.Count(oldString, "\n") + 1
	newLines := strings.Count(newString, "\n") + 1
	return strings.Replace(content, oldString, newString, 1), oldLines, newLines, nil
}

func editFileViaWSL(path, oldString, newString string) (string, error) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	data, err := ReadFileViaWSL(path)
	if err != nil {
		return "", fmt.Errorf("edit_file: read %s (WSL): %w", path, err)
	}
	newContent, oldLines, newLines, err := applyExactEdit(string(data), oldString, newString, path)
	if err != nil {
		return "", err
	}
	newContent, note := guardFileContent(path, newContent)
	if _, err := writeFileViaWSL(path, newContent, false); err != nil {
		return "", fmt.Errorf("edit_file: write %s (WSL): %w", path, err)
	}
	return withGuardNote(fmt.Sprintf("edited %s (WSL): replaced %d line(s) with %d line(s)", path, oldLines, newLines), note), nil
}
