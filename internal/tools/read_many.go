package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ReadMany reads several files in a single tool call, reducing round-trips
// when the model needs to inspect multiple files before acting.
type ReadMany struct{}

func (t *ReadMany) Name() string      { return "read_many" }
func (t *ReadMany) Destructive() bool { return false }

func (t *ReadMany) Description() string {
	return "Read multiple files in one call and return all their contents. " +
		"Use this instead of calling read_file repeatedly when you need to inspect " +
		"several files — it saves round-trips and keeps context coherent. " +
		"Maximum 20 files per call."
}

func (t *ReadMany) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "paths": {
      "type": "array",
      "items": {"type": "string"},
      "description": "List of file paths to read (max 20)",
      "maxItems": 20
    }
  },
  "required": ["paths"],
  "additionalProperties": false
}`)
}

type readManyParams struct {
	Paths []string `json:"paths"`
}

func (t *ReadMany) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p readManyParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("read_many: bad params: %w", err)
	}
	if len(p.Paths) == 0 {
		return "", fmt.Errorf("read_many: paths must not be empty")
	}
	if len(p.Paths) > 20 {
		p.Paths = p.Paths[:20]
	}

	var sb strings.Builder
	missing := false
	for _, path := range p.Paths {
		path = NormalizeHostPath(path)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				missing = true
			}
			fmt.Fprintf(&sb, "# %s  ERROR: %v\n\n", path, err)
			continue
		}
		lines := strings.Split(string(data), "\n")
		fmt.Fprintf(&sb, "# %s  (%d lines)\n", path, len(lines))
		for i, line := range lines {
			fmt.Fprintf(&sb, "%4d\t%s\n", i+1, line)
		}
		sb.WriteByte('\n')
	}
	if missing {
		fmt.Fprintf(&sb, "Workspace note: %s\n", MissingPathHint())
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
