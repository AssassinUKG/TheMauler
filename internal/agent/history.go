// Package agent provides conversation history management and rollback.
package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/llm"
)

// History manages the conversation message list and tracks token usage.
type History struct {
	messages   []llm.Message
	tokenCount int
	budget     int // ctx_tokens from active profile
}

// NewHistory creates a History with the given context budget.
func NewHistory(budget int) *History {
	return &History{budget: budget}
}

// Append adds a message and updates the estimated token count.
func (h *History) Append(m llm.Message) {
	h.messages = append(h.messages, m)
	h.tokenCount += estimateTokens(m)
}

// Messages returns a copy of the current message slice.
func (h *History) Messages() []llm.Message {
	out := make([]llm.Message, len(h.messages))
	copy(out, h.messages)
	return out
}

// Replace swaps the history with a saved message list and recounts tokens.
func (h *History) Replace(messages []llm.Message) {
	h.messages = make([]llm.Message, len(messages))
	copy(h.messages, messages)
	h.tokenCount = 0
	for _, m := range h.messages {
		h.tokenCount += estimateTokens(m)
	}
}

// TokenCount returns the current estimated token usage.
func (h *History) TokenCount() int { return h.tokenCount }

// Budget returns the configured context budget.
func (h *History) Budget() int { return h.budget }

// SetBudget updates the context window limit (e.g. on profile switch).
func (h *History) SetBudget(b int) { h.budget = b }

// SetExactCount updates the token count from a backend-reported value.
func (h *History) SetExactCount(n int) { h.tokenCount = n }

// UsageFraction returns the fraction of the budget currently used.
func (h *History) UsageFraction() float64 {
	if h.budget <= 0 {
		return 0
	}
	return float64(h.tokenCount) / float64(h.budget)
}

// NeedsCompaction reports whether the threshold has been exceeded.
func (h *History) NeedsCompaction(threshold float64) bool {
	return h.UsageFraction() >= threshold
}

// NeedsCompactionWithReserve reports whether history has exceeded the threshold
// after reserving room for system prompts, tool schemas, and the next reply.
func (h *History) NeedsCompactionWithReserve(threshold float64, reserveTokens int) bool {
	if h.budget <= 0 {
		return false
	}
	if threshold <= 0 {
		threshold = 0.85
	}
	effectiveBudget := h.budget - reserveTokens
	if effectiveBudget < h.budget/2 {
		effectiveBudget = h.budget / 2
	}
	if effectiveBudget <= 0 {
		return false
	}
	return h.tokenCount >= int(float64(effectiveBudget)*threshold)
}

type ToolClearStats struct {
	Cleared      int
	BeforeTokens int
	AfterTokens  int
}

// ClearOldToolResults replaces stale, re-fetchable tool payloads with compact
// placeholders while preserving the tool message and call pairing.
func (h *History) ClearOldToolResults(keepRecent int) ToolClearStats {
	stats := ToolClearStats{BeforeTokens: h.tokenCount}
	if keepRecent < 0 {
		keepRecent = 0
	}
	toolIndexes := make([]int, 0)
	for i, msg := range h.messages {
		if msg.Role == llm.RoleTool && clearableToolResult(msg.Name) && !isClearedToolResult(msg) {
			toolIndexes = append(toolIndexes, i)
		}
	}
	clearUntil := len(toolIndexes) - keepRecent
	if clearUntil <= 0 {
		stats.AfterTokens = h.tokenCount
		return stats
	}
	for _, idx := range toolIndexes[:clearUntil] {
		msg := &h.messages[idx]
		text := messageContentText(*msg)
		sum := sha256.Sum256([]byte(text))
		msg.Content = fmt.Sprintf("[Tool result cleared for context: %s, original_chars=%d, sha256=%x. Re-run the tool if the raw result is needed.]", msg.Name, len(text), sum[:8])
		stats.Cleared++
	}
	h.recount()
	stats.AfterTokens = h.tokenCount
	return stats
}

// Compact replaces the middle of the history with a summary message.
// keepFirst = number of early turns to always keep (after system)
// keepLast  = number of recent turns to always keep
func (h *History) Compact(summary string, keepFirst, keepLast int) {
	if len(h.messages) <= keepFirst+keepLast+1 {
		return // not enough history to compact
	}

	var kept []llm.Message

	// Preserve leading system message(s)
	i := 0
	for i < len(h.messages) && h.messages[i].Role == llm.RoleSystem {
		kept = append(kept, h.messages[i])
		i++
	}

	// keepFirst user/assistant turns
	end := i + keepFirst
	if end > len(h.messages) {
		end = len(h.messages)
	}
	kept = append(kept, h.messages[i:end]...)

	// Inject summary as an assistant message
	kept = append(kept, llm.NewTextMessage(llm.RoleAssistant,
		"[Context summary — earlier work]\n"+summary))

	// keepLast turns from the end
	tail := len(h.messages) - keepLast
	if tail < end {
		tail = end
	}
	kept = append(kept, h.messages[tail:]...)

	h.messages = sanitizeCompactedMessages(kept)

	h.recount()
}

// Clear resets the history entirely (keeps no messages).
func (h *History) Clear() {
	h.messages = nil
	h.tokenCount = 0
}

func (h *History) recount() {
	h.tokenCount = 0
	for _, m := range h.messages {
		h.tokenCount += estimateTokens(m)
	}
}

// estimateTokens gives a rough token count (~4 chars per token).
func estimateTokens(m llm.Message) int {
	n := len(m.Role)/4 + len(m.Name)/4 + len(m.ToolCallID)/4 + 4
	switch c := m.Content.(type) {
	case string:
		n += len(c)/4 + 4
	case []llm.ContentBlock:
		for _, b := range c {
			n += len(b.Text) / 4
			if b.ImageURL != nil && b.ImageURL.URL != "" {
				n += 85
			}
		}
	default:
		if data, err := json.Marshal(c); err == nil {
			n += len(data)/4 + 4
		}
	}
	if len(m.ToolCalls) > 0 {
		if data, err := json.Marshal(m.ToolCalls); err == nil {
			n += len(data)/4 + 8
		}
	}
	return n
}

func clearableToolResult(name string) bool {
	switch name {
	case "read_file", "read_many", "file_outline", "read_chunks", "read_pdf", "glob", "grep", "session_search", "web_search", "fetch_url", "shell", "bash", "browser_snapshot", "browser_extract":
		return true
	default:
		return false
	}
}

func isClearedToolResult(msg llm.Message) bool {
	text := messageContentText(msg)
	return strings.HasPrefix(text, "[Tool result cleared for context:")
}

func messageContentText(msg llm.Message) string {
	switch c := msg.Content.(type) {
	case string:
		return c
	case []llm.ContentBlock:
		var sb strings.Builder
		for _, b := range c {
			if b.Text != "" {
				sb.WriteString(b.Text)
			}
			if b.ImageURL != nil && b.ImageURL.URL != "" {
				sb.WriteString("[image]")
			}
		}
		return sb.String()
	default:
		data, _ := json.Marshal(c)
		return string(data)
	}
}

func sanitizeCompactedMessages(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	pendingToolIDs := map[string]bool{}
	for _, msg := range messages {
		if msg.Role == llm.RoleTool {
			if msg.ToolCallID != "" && !pendingToolIDs[msg.ToolCallID] {
				continue
			}
			out = append(out, msg)
			delete(pendingToolIDs, msg.ToolCallID)
			continue
		}
		if len(pendingToolIDs) > 0 && len(out) > 0 {
			last := &out[len(out)-1]
			if last.Role == llm.RoleAssistant && len(last.ToolCalls) > 0 {
				last.ToolCalls = nil
				if text, ok := last.Content.(string); ok && text == "" {
					last.Content = "[Tool calls were compacted; results are unavailable.]"
				}
			}
			pendingToolIDs = map[string]bool{}
		}
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			pendingToolIDs = map[string]bool{}
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					pendingToolIDs[tc.ID] = true
				}
			}
		}
		out = append(out, msg)
	}
	if len(pendingToolIDs) > 0 && len(out) > 0 {
		last := &out[len(out)-1]
		if last.Role == llm.RoleAssistant && len(last.ToolCalls) > 0 {
			last.ToolCalls = nil
			if text, ok := last.Content.(string); ok && text == "" {
				last.Content = "[Tool calls were compacted; results are unavailable.]"
			}
		}
	}
	return out
}
