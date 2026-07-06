package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mauler/internal/ledger"
	"mauler/internal/tools"
)

const defaultProgressPath = ".mauler/progress.md"

type progressTool struct{ app *App }

type progressToolArgs struct {
	Action  string `json:"action"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Section string `json:"section"`
}

func (t *progressTool) Name() string { return "progress" }

func (t *progressTool) Description() string {
	return "Read or update the durable task progress artifact. Default path is .mauler/progress.md. Use this before compaction, before long background work, at resume, and near run completion."
}

func (t *progressTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "action": {"type": "string", "enum": ["read", "update", "append"], "description": "read returns the progress artifact; update replaces it; append adds content under a section heading."},
    "path": {"type": "string", "description": "Optional progress file path. Defaults to .mauler/progress.md."},
    "content": {"type": "string", "description": "Content for update or append."},
    "section": {"type": "string", "description": "Optional heading for append, e.g. Next Steps or Verified."}
  },
  "required": ["action"]
}`)
}

func (t *progressTool) Destructive() bool { return true }

func (t *progressTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args progressToolArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("progress: bad params: %w", err)
	}
	path := progressPath(args.Path)
	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "read":
		return readProgress(path)
	case "update":
		return t.writeProgress(path, ensureProgressShape(args.Content), false)
	case "append":
		return t.writeProgress(path, progressAppendBlock(args.Section, args.Content), true)
	default:
		return "", fmt.Errorf("progress: action must be read, update, or append")
	}
}

func progressPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultProgressPath
	}
	return tools.NormalizeHostPath(path)
}

func readProgress(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return fmt.Sprintf("No progress artifact exists at %s yet. Create it with progress action=update.", filepath.ToSlash(path)), nil
	}
	if err != nil {
		return "", fmt.Errorf("progress: read %s: %w", path, err)
	}
	return fmt.Sprintf("# %s\n\n%s", filepath.ToSlash(path), strings.TrimSpace(string(data))), nil
}

func (t *progressTool) writeProgress(path, content string, appendMode bool) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("progress: content is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("progress: mkdir: %w", err)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return "", fmt.Errorf("progress: open: %w", err)
	}
	defer f.Close()
	if appendMode {
		if _, err := f.WriteString("\n"); err != nil {
			return "", err
		}
	}
	if _, err := f.WriteString(strings.TrimRight(content, "\r\n") + "\n"); err != nil {
		return "", fmt.Errorf("progress: write: %w", err)
	}
	t.app.recordLedger(ledger.Event{
		Kind:    "progress_update",
		Source:  "progress",
		Tool:    "progress",
		Status:  "done",
		Message: filepath.ToSlash(path),
		Output:  truncateRunes(content, 4000),
	})
	action := "updated"
	if appendMode {
		action = "appended"
	}
	return fmt.Sprintf("progress %s: %s", action, filepath.ToSlash(path)), nil
}

func progressAppendBlock(section, content string) string {
	section = strings.TrimSpace(section)
	content = strings.TrimSpace(content)
	if section == "" {
		section = "Update"
	}
	return fmt.Sprintf("## %s\n\n%s\n", section, content)
}

func ensureProgressShape(content string) string {
	content = strings.TrimSpace(content)
	if content != "" {
		return content + "\n"
	}
	return `# Progress

## Objective

## Current State

## Decisions

## Files Touched

## Commands / Artifacts

## Verified

## Next Steps

## Open Questions
`
}
