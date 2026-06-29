package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/ledger"
)

type fileChangesTool struct{ app *App }

func (t *fileChangesTool) Name() string      { return "file_changes" }
func (t *fileChangesTool) Destructive() bool { return false }

func (t *fileChangesTool) Description() string {
	return "List recent files created or modified by agent write/edit tools from the RunLedger. Use before cleanup to identify temporary files the agent created and existing files it modified."
}

func (t *fileChangesTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "status": {"type": "string", "enum": ["all", "created", "modified"], "description": "Filter file changes by status/action (default all)"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum file-change events to return (default 30)"}
  }
}`)
}

type fileChangesArgs struct {
	Status string `json:"status"`
	Limit  int    `json:"limit"`
}

func (t *fileChangesTool) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var args fileChangesArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("file_changes: bad params: %w", err)
		}
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	status := strings.ToLower(strings.TrimSpace(args.Status))
	if status == "" {
		status = "all"
	}
	events, err := t.app.ListLedgerEvents(500)
	if err != nil {
		return "", fmt.Errorf("file_changes: list ledger: %w", err)
	}
	changes := filterFileChangeEvents(events, status, limit)
	t.app.recordLedger(ledger.Event{
		Kind:    "file_change_query",
		Source:  "tool",
		Tool:    "file_changes",
		Status:  "ok",
		Message: fmt.Sprintf("returned %d change%s", len(changes), plural(len(changes), "", "s")),
		Metadata: map[string]string{
			"filter": status,
			"limit":  fmt.Sprintf("%d", limit),
		},
	})
	if len(changes) == 0 {
		return "No matching file-change records found.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Recent file changes (%s):\n", status)
	for _, event := range changes {
		action := firstNonEmpty(event.Metadata["action"], event.Status)
		path := ""
		if len(event.Files) > 0 {
			path = event.Files[0]
		}
		cleanup := event.Metadata["cleanup_hint"]
		if cleanup != "" {
			cleanup = " cleanup=" + cleanup
		}
		fmt.Fprintf(&sb, "- %s %s [%s; %s]%s\n", action, path, event.Tool, event.Timestamp, cleanup)
		if event.Metadata["after_size"] != "" || event.Metadata["after_sha256"] != "" {
			fmt.Fprintf(&sb, "  after_size=%s after_sha256=%s\n", event.Metadata["after_size"], event.Metadata["after_sha256"])
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func filterFileChangeEvents(events []ledger.Event, status string, limit int) []ledger.Event {
	out := make([]ledger.Event, 0, limit)
	for _, event := range events {
		if event.Kind != "file_change" {
			continue
		}
		action := strings.ToLower(firstNonEmpty(event.Metadata["action"], event.Status))
		if status != "all" && action != status {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out
}
