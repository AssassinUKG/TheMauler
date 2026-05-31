package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type FileOutline struct{}

func (t *FileOutline) Name() string      { return "file_outline" }
func (t *FileOutline) Destructive() bool { return false }

func (t *FileOutline) Description() string {
	return "Return a compact outline of a source or Markdown file without reading the full contents. Use this before reading large files."
}

func (t *FileOutline) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path to outline"},
    "max_items": {"type": "integer", "description": "Maximum outline entries to return (default 120, max 300)"}
  },
  "required": ["path"],
  "additionalProperties": false
}`)
}

type fileOutlineParams struct {
	Path     string `json:"path"`
	MaxItems int    `json:"max_items"`
}

func (t *FileOutline) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p fileOutlineParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("file_outline: bad params: %w", err)
	}
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("file_outline: path is required")
	}
	maxItems := p.MaxItems
	if maxItems <= 0 {
		maxItems = 120
	}
	if maxItems > 300 {
		maxItems = 300
	}
	path := NormalizeHostPath(p.Path)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file_outline: %w\n%s", err, MissingPathHint())
		}
		return "", fmt.Errorf("file_outline: %w", err)
	}
	defer file.Close()
	info, _ := file.Stat()
	var entries []string
	lineCount := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lineCount++
		if len(entries) >= maxItems {
			continue
		}
		if entry := outlineEntry(path, lineCount, scanner.Text()); entry != "" {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("file_outline: scan: %w", err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n", filepath.ToSlash(path))
	if info != nil {
		fmt.Fprintf(&sb, "size_bytes: %d\n", info.Size())
	}
	fmt.Fprintf(&sb, "lines: %d\n", lineCount)
	if len(entries) == 0 {
		sb.WriteString("outline: no obvious symbols/headings/imports found\n")
	} else {
		sb.WriteString("outline:\n")
		for _, entry := range entries {
			sb.WriteString(entry + "\n")
		}
		if len(entries) >= maxItems {
			fmt.Fprintf(&sb, "... limited to %d entries\n", maxItems)
		}
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

type ReadChunks struct{}

func (t *ReadChunks) Name() string      { return "read_chunks" }
func (t *ReadChunks) Destructive() bool { return false }

func (t *ReadChunks) Description() string {
	return "Read one bounded chunk of a large file by chunk index. Use file_outline first to choose the right chunk."
}

func (t *ReadChunks) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path to read"},
    "chunk_index": {"type": "integer", "description": "1-based chunk index to read"},
    "chunk_size_lines": {"type": "integer", "description": "Lines per chunk (default 200, max 800)"}
  },
  "required": ["path", "chunk_index"],
  "additionalProperties": false
}`)
}

type readChunksParams struct {
	Path           string `json:"path"`
	ChunkIndex     int    `json:"chunk_index"`
	ChunkSizeLines int    `json:"chunk_size_lines"`
}

func (t *ReadChunks) Run(_ context.Context, raw json.RawMessage) (string, error) {
	var p readChunksParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("read_chunks: bad params: %w", err)
	}
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("read_chunks: path is required")
	}
	if p.ChunkIndex <= 0 {
		return "", fmt.Errorf("read_chunks: chunk_index must be 1 or greater")
	}
	chunkSize := p.ChunkSizeLines
	if chunkSize <= 0 {
		chunkSize = 200
	}
	if chunkSize > 800 {
		chunkSize = 800
	}
	path := NormalizeHostPath(p.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("read_chunks: %w\n%s", err, MissingPathHint())
		}
		return "", fmt.Errorf("read_chunks: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	total := len(lines)
	totalChunks := (total + chunkSize - 1) / chunkSize
	if p.ChunkIndex > totalChunks {
		return "", fmt.Errorf("read_chunks: chunk_index %d > total_chunks %d", p.ChunkIndex, totalChunks)
	}
	start := (p.ChunkIndex-1)*chunkSize + 1
	end := start + chunkSize - 1
	if end > total {
		end = total
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s  chunk %d/%d (lines %d-%d of %d)\n", filepath.ToSlash(path), p.ChunkIndex, totalChunks, start, end, total)
	for i, line := range lines[start-1 : end] {
		fmt.Fprintf(&sb, "%4d\t%s\n", start+i, line)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

var outlinePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*func\s+(?:\([^)]+\)\s*)?[A-Za-z_][A-Za-z0-9_]*\s*\(`),
	regexp.MustCompile(`^\s*type\s+[A-Za-z_][A-Za-z0-9_]*\s+(?:struct|interface)\b`),
	regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+[A-Za-z_$][A-Za-z0-9_$]*\s*\(`),
	regexp.MustCompile(`^\s*(?:export\s+)?(?:class|interface|type)\s+[A-Za-z_$][A-Za-z0-9_$]*\b`),
	regexp.MustCompile(`^\s*(?:const|let|var)\s+[A-Za-z_$][A-Za-z0-9_$]*\s*=\s*(?:async\s*)?\(`),
	regexp.MustCompile(`^\s*def\s+[A-Za-z_][A-Za-z0-9_]*\s*\(`),
	regexp.MustCompile(`^\s*class\s+[A-Za-z_][A-Za-z0-9_]*\b`),
	regexp.MustCompile(`^\s*#{1,6}\s+\S`),
	regexp.MustCompile(`^\s*import\s+`),
	regexp.MustCompile(`^\s*from\s+\S+\s+import\s+`),
}

func outlineEntry(path string, line int, text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(trimmed) > 240 {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, pattern := range outlinePatterns {
		if pattern.MatchString(text) {
			if (ext == ".go" || ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx") && strings.HasPrefix(trimmed, "import ") {
				return fmt.Sprintf("- %d: %s", line, trimmed)
			}
			return fmt.Sprintf("- %d: %s", line, trimmed)
		}
	}
	return ""
}
