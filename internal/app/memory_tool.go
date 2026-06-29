package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/ledger"
)

// memoryTool gives the model first-class access to durable project memory during a
// run. Without it, memory is read-only and frozen at turn start: the agent can see
// what was injected but cannot pull more or persist what it learns. recall lets it
// query before repeating work; remember lets it capture a reusable lesson, command,
// preference, or target detail so the brain compounds across runs.
type memoryTool struct{ app *App }

func (t *memoryTool) Name() string      { return "memory" }
func (t *memoryTool) Destructive() bool { return false }

func (t *memoryTool) Description() string {
	return "Persistent project memory scoped to the current workspace. " +
		"action=recall searches stored notes, lessons, preferences, and facts by query and returns the most relevant; call it before repeating work to check what is already known. " +
		"action=remember saves a durable entry; call it when you learn something reusable — a failure to avoid, a working command, a user preference, a confirmed target detail. " +
		"Keep entries short and factual; do not store secrets or one-off chatter."
}

func (t *memoryTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "action": {"type": "string", "enum": ["recall", "remember"], "description": "recall to search memory, remember to store a new entry"},
    "query": {"type": "string", "description": "recall: keywords or topic to search for; empty returns the most important/recent entries"},
    "title": {"type": "string", "description": "remember: short title for the entry"},
    "content": {"type": "string", "description": "remember: the fact, lesson, or preference to store"},
    "kind": {"type": "string", "enum": ["note", "preference", "constraint", "fact", "workflow", "decision"], "description": "remember: category of memory"},
    "confidence": {"type": "string", "enum": ["confirmed", "likely", "hypothesis", "stale"], "description": "remember: confidence level; use likely/hypothesis unless live evidence confirmed it"},
    "source": {"type": "string", "enum": ["agent", "tool", "model", "previous_run"], "description": "remember: where this memory came from; defaults to agent"},
    "tags": {"type": "array", "items": {"type": "string"}, "description": "remember: optional tags for retrieval"},
    "importance": {"type": "integer", "minimum": 1, "maximum": 5, "description": "remember: 1-5, higher is injected more readily"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20, "description": "recall: max entries to return (default 6)"}
  },
  "required": ["action"]
}`)
}

type memoryToolArgs struct {
	Action     string   `json:"action"`
	Query      string   `json:"query"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Kind       string   `json:"kind"`
	Confidence string   `json:"confidence"`
	Source     string   `json:"source"`
	Tags       []string `json:"tags"`
	Importance int      `json:"importance"`
	Limit      int      `json:"limit"`
}

func (t *memoryTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args memoryToolArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("memory: bad params: %w", err)
	}
	t.app.mu.Lock()
	memEnabled := t.app.cfg.Memory.Enabled
	t.app.mu.Unlock()
	if !memEnabled {
		return "Memory is disabled in settings (Memory.Enabled=false); recall/remember are unavailable until it is turned on.", nil
	}

	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "recall":
		return t.runRecall(args)
	case "remember":
		return t.runRemember(args)
	default:
		return "", fmt.Errorf("memory: action must be \"recall\" or \"remember\"")
	}
}

func (t *memoryTool) runRecall(args memoryToolArgs) (string, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 6
	}
	entries, err := searchMemory(args.Query, limit)
	if err != nil {
		return "", fmt.Errorf("memory: recall failed: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	t.app.recordLedger(ledger.Event{
		Kind:    "memory_recall",
		Source:  "memory",
		Status:  "ok",
		Message: query,
		Metadata: map[string]string{
			"query":   query,
			"results": fmt.Sprintf("%d", len(entries)),
		},
	})
	if len(entries) == 0 {
		if query == "" {
			return "No memory entries stored for this workspace yet.", nil
		}
		return fmt.Sprintf("No memory entries matched %q.", query), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Recalled %d memory entr%s:\n", len(entries), plural(len(entries), "y", "ies"))
	for _, entry := range entries {
		sb.WriteString("- ")
		if entry.Title != "" {
			sb.WriteString(entry.Title + ": ")
		}
		sb.WriteString(strings.TrimSpace(entry.Content))
		meta := []string{}
		if entry.Kind != "" && entry.Kind != "note" {
			meta = append(meta, "kind="+entry.Kind)
		}
		if entry.Confidence != "" {
			meta = append(meta, "confidence="+normaliseMemoryConfidence(entry.Confidence))
		}
		if entry.Source != "" {
			meta = append(meta, "source="+normaliseMemorySource(entry.Source))
		}
		if entry.Importance > 0 {
			meta = append(meta, fmt.Sprintf("importance=%d", entry.Importance))
		}
		if len(entry.Tags) > 0 {
			meta = append(meta, "tags="+strings.Join(entry.Tags, ","))
		}
		if len(meta) > 0 {
			sb.WriteString(" [" + strings.Join(meta, "; ") + "]")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func (t *memoryTool) runRemember(args memoryToolArgs) (string, error) {
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return "", fmt.Errorf("memory: content is required for remember")
	}
	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = deriveMemoryTitle(content)
	}
	// Tag agent-authored memories so they are distinguishable from user-curated ones.
	tags := append([]string{}, args.Tags...)
	tags = append(tags, "agent")
	entry, err := t.app.SaveMemoryEntry(MemoryEntry{
		Title:      title,
		Content:    content,
		Kind:       args.Kind,
		Confidence: firstNonEmpty(args.Confidence, "likely"),
		Source:     firstNonEmpty(args.Source, "agent"),
		Tags:       tags,
		Importance: args.Importance,
		Scope:      workspaceScope(),
	})
	if err != nil {
		return "", fmt.Errorf("memory: failed to save entry: %w", err)
	}
	return fmt.Sprintf("Saved memory %s [%s]: %s", entry.ID, entry.Kind, entry.Title), nil
}

func deriveMemoryTitle(content string) string {
	line := strings.TrimSpace(content)
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	const max = 60
	if len(line) > max {
		line = strings.TrimSpace(line[:max]) + "…"
	}
	if line == "" {
		return "Note"
	}
	return line
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
