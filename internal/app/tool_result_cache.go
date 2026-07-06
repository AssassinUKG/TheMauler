package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"mauler/internal/llm"
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
	if isShellTool(name) {
		command := shellCommandFromToolArgs(tc.Function.Arguments)
		key := repeatShellCommandKey(command)
		if key == "" {
			return ""
		}
		return "shell\x00" + key
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
	if isShellTool(name) {
		command := shellCommandFromToolArgs(raw)
		key := repeatShellCommandKey(command)
		if key == "" {
			return ""
		}
		return "shell\x00" + key
	}
	return ""
}
