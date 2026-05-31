package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Grep searches for a regex pattern in files.
type Grep struct{}

func (t *Grep) Name() string      { return "grep" }
func (t *Grep) Destructive() bool { return false }

func (t *Grep) Description() string {
	return "Search for a regex pattern across files. " +
		"Returns matching lines with file path and line number. " +
		"Limited to 100 matches."
}

func (t *Grep) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern":        {"type": "string", "description": "Regular expression to search for"},
    "path":           {"type": "string", "description": "File or directory to search (default: current directory)"},
    "glob":           {"type": "string", "description": "Only search files matching this glob, e.g. '*.go'"},
    "case_sensitive": {"type": "boolean", "description": "Case-sensitive match (default true)"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
}

type grepParams struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path"`
	Glob          string `json:"glob"`
	CaseSensitive *bool  `json:"case_sensitive"`
}

const maxGrepResults = 100

func (t *Grep) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p grepParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("grep: bad params: %w", err)
	}
	if p.Pattern == "" {
		return "", fmt.Errorf("grep: pattern is required")
	}

	// Build regex
	reStr := p.Pattern
	caseSensitive := true
	if p.CaseSensitive != nil && !*p.CaseSensitive {
		caseSensitive = false
	}
	if !caseSensitive {
		reStr = "(?i)" + reStr
	}
	re, err := regexp.Compile(reStr)
	if err != nil {
		return "", fmt.Errorf("grep: invalid pattern %q: %w", p.Pattern, err)
	}

	searchPath := p.Path
	if searchPath == "" {
		searchPath = "."
	}
	searchPath = NormalizeHostPath(searchPath)

	type match struct {
		file string
		line int
		text string
	}
	var matches []match

	err = filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// apply glob filter
		if p.Glob != "" {
			ok, _ := filepath.Match(p.Glob, d.Name())
			if !ok {
				return nil
			}
		}
		// skip binary-looking files
		if isBinaryExt(filepath.Ext(path)) {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, match{
					file: filepath.ToSlash(path),
					line: lineNum,
					text: strings.TrimSpace(line),
				})
				if len(matches) >= maxGrepResults {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("grep: walk: %w", err)
	}

	if len(matches) == 0 {
		return "no matches", nil
	}

	var sb strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&sb, "%s:%d: %s\n", m.file, m.line, m.text)
	}
	if len(matches) == maxGrepResults {
		sb.WriteString(fmt.Sprintf("... (limited to %d results)", maxGrepResults))
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

func isBinaryExt(ext string) bool {
	binary := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bin": true,
		".exe": true, ".so": true, ".dll": true, ".dylib": true,
		".wasm": true, ".pyc": true,
	}
	return binary[strings.ToLower(ext)]
}
