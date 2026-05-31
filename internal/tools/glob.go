package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// Glob finds files matching a pattern.
type Glob struct{}

func (t *Glob) Name() string      { return "glob" }
func (t *Glob) Destructive() bool { return false }

func (t *Glob) Description() string {
	return "Find files matching a glob pattern. " +
		"Supports ** for recursive matching (e.g. '**/*.go'). " +
		"Results are sorted and limited to 200 matches."
}

func (t *Glob) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Glob pattern, e.g. '**/*.go' or 'src/**/*.ts'"},
    "dir":     {"type": "string", "description": "Root directory to search from (default: current directory)"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
}

type globParams struct {
	Pattern string `json:"pattern"`
	Dir     string `json:"dir"`
}

func (t *Glob) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p globParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("glob: bad params: %w", err)
	}
	if p.Pattern == "" {
		return "", fmt.Errorf("glob: pattern is required")
	}

	root := p.Dir
	if root == "" {
		root = "."
	}
	root = NormalizeHostPath(root)

	const maxResults = 200
	var matches []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() && shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			matched, _ := filepath.Match(p.Pattern, rel)
			if !matched {
				// also try matching just the base name for simple patterns
				matched, _ = filepath.Match(p.Pattern, d.Name())
			}
			// for ** patterns, do a simple suffix check
			if !matched && strings.Contains(p.Pattern, "**") {
				matched = globDoublestar(p.Pattern, filepath.ToSlash(rel))
			}
			if matched {
				matches = append(matches, filepath.ToSlash(rel))
				if len(matches) >= maxResults {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob: walk: %w", err)
	}

	if len(matches) == 0 {
		return "no files matched", nil
	}

	var sb strings.Builder
	for _, m := range matches {
		sb.WriteString(m)
		sb.WriteByte('\n')
	}
	if len(matches) == maxResults {
		sb.WriteString(fmt.Sprintf("... (limited to %d results)", maxResults))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// globDoublestar does a simple ** expansion: split on ** and check prefix/suffix.
func globDoublestar(pattern, path string) bool {
	parts := strings.SplitN(pattern, "**", 2)
	if len(parts) != 2 {
		return false
	}
	prefix := filepath.ToSlash(parts[0])
	suffix := filepath.ToSlash(parts[1])
	suffix = strings.TrimPrefix(suffix, "/")

	if prefix != "" && !strings.HasPrefix(path, prefix) {
		return false
	}
	if suffix != "" {
		matched, _ := filepath.Match(suffix, filepath.Base(path))
		if !matched {
			// check if path ends with the suffix pattern
			matched, _ = filepath.Match("*/"+suffix, path)
			if !matched {
				return strings.HasSuffix(path, strings.TrimPrefix(suffix, "*"))
			}
		}
		return matched
	}
	return true
}

func shouldSkipDir(name string) bool {
	skip := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".cache": true, "vendor": true, "dist": true, "build": true,
		".next": true, "target": true,
	}
	return skip[name]
}
