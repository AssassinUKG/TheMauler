package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

const defaultToolResultPreviewChars = 2000

func (a *App) toolResultForContext(runID, toolName, result string, cfg settings.ToolsConfig) string {
	trigger := cfg.MaxToolResultChars
	if trigger <= 0 || len(result) <= trigger {
		return result
	}
	handle, err := a.saveToolResult(runID, toolName, result)
	if err != nil {
		return truncateToolResult(result, trigger)
	}
	previewChars := cfg.ToolResultPreviewChars
	if previewChars <= 0 {
		previewChars = defaultToolResultPreviewChars
	}
	return toolResultPreview(result, handle, previewChars)
}

func (a *App) offloadToolResultMessagesForAggregate(runID string, msgs []llm.Message, cfg settings.ToolsConfig) []llm.Message {
	capChars := cfg.ToolResultAggregateChars
	if capChars <= 0 || len(msgs) == 0 {
		return msgs
	}
	total := 0
	type candidate struct {
		index int
		size  int
	}
	var candidates []candidate
	for i, msg := range msgs {
		content, ok := msg.Content.(string)
		if !ok {
			continue
		}
		size := len([]rune(content))
		total += size
		if size > cfg.ToolResultPreviewChars && !isToolResultOffloadPreview(content) {
			candidates = append(candidates, candidate{index: i, size: size})
		}
	}
	if total <= capChars || len(candidates) == 0 {
		return msgs
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].size > candidates[j].size
	})
	out := append([]llm.Message(nil), msgs...)
	previewChars := cfg.ToolResultPreviewChars
	if previewChars <= 0 {
		previewChars = defaultToolResultPreviewChars
	}
	for _, candidate := range candidates {
		if total <= capChars {
			break
		}
		content, _ := out[candidate.index].Content.(string)
		handle, err := a.saveToolResult(runID, out[candidate.index].Name, content)
		if err != nil {
			continue
		}
		preview := toolResultPreview(content, handle, previewChars)
		out[candidate.index].Content = preview
		total = total - candidate.size + len([]rune(preview))
	}
	return out
}

func (a *App) saveToolResult(runID, toolName, full string) (string, error) {
	runID = safeToolResultPart(firstNonEmpty(runID, "run"))
	sum := sha256.Sum256([]byte(full))
	id := fmt.Sprintf("%s-%d-%s", safeToolResultPart(firstNonEmpty(toolName, "tool")), time.Now().UnixNano(), hex.EncodeToString(sum[:])[:12])
	dir, err := toolResultRunDir(runID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+".txt")
	if err := os.WriteFile(path, []byte(full), 0o640); err != nil {
		return "", err
	}
	return runID + "/" + id, nil
}

func (a *App) loadToolResultSlice(handle string, offset, limit int) (string, bool) {
	runID, id, ok := splitToolResultHandle(handle)
	if !ok {
		return "", false
	}
	dir, err := toolResultRunDir(runID)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".txt"))
	if err != nil {
		return "", false
	}
	runes := []rune(string(data))
	if offset < 0 {
		offset = 0
	}
	if offset > len(runes) {
		offset = len(runes)
	}
	if limit <= 0 || limit > 20000 {
		limit = 4000
	}
	end := offset + limit
	if end > len(runes) {
		end = len(runes)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Tool result %s slice offset=%d limit=%d total_chars=%d\n\n", handle, offset, limit, len(runes))
	sb.WriteString(string(runes[offset:end]))
	if end < len(runes) {
		fmt.Fprintf(&sb, "\n\n[more available: call read_tool_result with result_id=%q offset=%d]", handle, end)
	}
	return sb.String(), true
}

func toolResultPreview(full, handle string, previewChars int) string {
	if previewChars <= 0 {
		previewChars = defaultToolResultPreviewChars
	}
	runes := []rune(full)
	if len(runes) <= previewChars {
		return full
	}
	keep := previewChars - 220
	if keep < 120 {
		keep = 120
	}
	half := keep / 2
	head := string(runes[:half])
	tail := string(runes[len(runes)-half:])
	omitted := len(runes) - (half * 2)
	return fmt.Sprintf("%s\n\n[tool result offloaded: %d chars omitted (the MIDDLE). result_id=%s. Do NOT re-run the command to see more — call read_tool_result with this result_id (and offset/limit) to read the omitted middle, where scan findings usually are.]\n\n%s",
		head, omitted, handle, tail)
}

func isToolResultOffloadPreview(content string) bool {
	return strings.Contains(content, "[tool result offloaded:") && strings.Contains(content, "result_id=")
}

func toolResultRunDir(runID string) (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "run-artifacts", "tool-results", safeToolResultPart(runID)), nil
}

func splitToolResultHandle(handle string) (string, string, bool) {
	parts := strings.Split(strings.TrimSpace(handle), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	runID := safeToolResultPart(parts[0])
	id := safeToolResultPart(parts[1])
	if runID == "" || id == "" {
		return "", "", false
	}
	return runID, id, true
}

var toolResultPartRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func safeToolResultPart(value string) string {
	value = strings.TrimSpace(value)
	value = toolResultPartRE.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if len(value) > 120 {
		value = value[:120]
	}
	return value
}
