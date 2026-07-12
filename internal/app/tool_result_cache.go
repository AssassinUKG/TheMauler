package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/tools"
)

var cachedToolResultIDPattern = regexp.MustCompile(`result_id="?([A-Za-z0-9._/\-]+)"?`)

func cachedToolResultForCall(run TaskRun, tc llm.ToolCallDef) string {
	key := cacheableToolCallKey(tc)
	if key == "" {
		return ""
	}
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if strings.EqualFold(strings.TrimSpace(tool.Status), "error") || strings.EqualFold(strings.TrimSpace(tool.Status), "blocked") {
			continue
		}
		if cacheableStoredToolKey(tool) != key {
			continue
		}
		result := strings.TrimSpace(tool.Result)
		if result == "" {
			result = "[cached tool result was empty]"
		}
		resultID := cachedToolResultID(result)
		nextTool := "proceed"
		if resultID != "" {
			nextTool = "read_tool_result"
		}
		return fmt.Sprintf("[cached_tool_result]\ncontract:\n  state: cached\n  tool: %s\n  source_status: %s\n  result_id: %s\n  next_tool: %s\n  do_not_repeat: exact input already ran in this run; use cached evidence unless a meaningful input changes\n  cache_key: %s\n\n%s",
			tc.Function.Name,
			strings.TrimSpace(tool.Status),
			cacheContractValue(resultID),
			nextTool,
			cacheContractValue(key),
			truncateRunes(result, 1800))
	}
	return ""
}

func cachedToolResultID(result string) string {
	match := cachedToolResultIDPattern.FindStringSubmatch(result)
	if len(match) < 2 {
		return ""
	}
	return strings.Trim(match[1], `"'.,;)]}`)
}

func repeatedCachedToolCall(run TaskRun, tc llm.ToolCallDef) bool {
	key := cacheableToolCallKey(tc)
	if key == "" {
		return false
	}
	for _, tool := range run.Tools {
		status := strings.ToLower(strings.TrimSpace(tool.Status))
		if status != "cached" && status != "skipped" {
			continue
		}
		if cacheableStoredToolKey(tool) == key {
			return true
		}
	}
	return false
}

func cachedEmptyGlobResultForCall(run TaskRun, tc llm.ToolCallDef) string {
	if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), "glob") {
		return ""
	}
	key := globCallKey(tc.Function.Arguments)
	if key == "" {
		return ""
	}
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if !strings.EqualFold(strings.TrimSpace(tool.Name), "glob") || !strings.EqualFold(strings.TrimSpace(tool.Status), "done") {
			continue
		}
		if globCallKey(json.RawMessage(tool.Input)) != key {
			continue
		}
		result := strings.TrimSpace(tool.Result)
		if !strings.Contains(result, "state: empty") && !strings.Contains(result, "matches: 0") {
			continue
		}
		return "[empty_glob_cached]\ncontract:\n  state: cached_empty\n  tool: glob\n  next_tool: proceed\n  do_not_repeat: this glob pattern already returned zero matches in this run; use the empty result unless files were created or the pattern/dir changes meaningfully\n  cache_key: " + cacheContractValue(key) + "\n\n" + truncateRunes(result, 1200)
	}
	return ""
}

func globCallKey(raw json.RawMessage) string {
	var args struct {
		Pattern string `json:"pattern"`
		Dir     string `json:"dir"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return ""
	}
	dir := strings.TrimSpace(args.Dir)
	if dir == "" {
		dir = "."
	}
	dir = strings.TrimRight(filepath.ToSlash(tools.NormalizeHostPath(dir)), "/")
	if dir == "" {
		dir = "."
	}
	return strings.ToLower(dir + "\x00" + pattern)
}

func cacheContractValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func cacheableToolCallKey(tc llm.ToolCallDef) string {
	name := strings.TrimSpace(tc.Function.Name)
	if name == "" || isWriteTool(name) || isReasoningEffortTool(name) {
		return ""
	}
	if key := idempotentReadKey(name, tc.Function.Arguments); key != "" {
		return key
	}
	return ""
}

func cacheableStoredToolKey(tool TaskToolEvent) string {
	name := strings.TrimSpace(tool.Name)
	if name == "" || isWriteTool(name) || isReasoningEffortTool(name) {
		return ""
	}
	raw := json.RawMessage(tool.Input)
	if key := idempotentReadKey(name, raw); key != "" {
		return key
	}
	return ""
}
