package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/sessionstore"
)

type SessionSearch struct{}

func (t *SessionSearch) Name() string      { return "session_search" }
func (t *SessionSearch) Destructive() bool { return false }

func (t *SessionSearch) Description() string {
	return "Search prior saved/autosaved chat sessions with local full-text search. Use this to recall previous decisions, fixes, errors, and user preferences without rereading project files."
}

func (t *SessionSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Keywords to search for in prior chat messages and tool calls."},
    "limit": {"type": "integer", "description": "Maximum matches to return, default 10, max 50."}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
}

type sessionSearchParams struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (t *SessionSearch) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p sessionSearchParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("session_search: bad params: %w", err)
	}
	results, err := sessionstore.SearchDefault(p.Query, p.Limit)
	if err != nil {
		return "", fmt.Errorf("session_search: %w", err)
	}
	if len(results) == 0 {
		return fmt.Sprintf("No prior session messages matched %q.", strings.TrimSpace(p.Query)), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Session search results for %q:\n", strings.TrimSpace(p.Query))
	for i, result := range results {
		fmt.Fprintf(&sb, "\n%d. session=%s role=%s updated=%s\n", i+1, result.SessionName, result.Role, result.UpdatedAt)
		if result.ToolName != "" {
			fmt.Fprintf(&sb, "   tool=%s\n", result.ToolName)
		}
		for _, line := range strings.Split(result.Content, "\n") {
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(&sb, "   %s\n", line)
			}
		}
	}
	return sb.String(), nil
}
