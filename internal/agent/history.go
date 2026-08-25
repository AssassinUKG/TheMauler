// Package agent provides conversation history management and rollback.
package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
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

type MicrocompactStats struct {
	Compacted    int
	BeforeTokens int
	AfterTokens  int
}

type RepairAction struct {
	Phase  int
	Action string
	Index  int
	Detail string
}

func (a RepairAction) String() string {
	if a.Detail == "" {
		return fmt.Sprintf("phase=%d action=%s index=%d", a.Phase, a.Action, a.Index)
	}
	return fmt.Sprintf("phase=%d action=%s index=%d detail=%s", a.Phase, a.Action, a.Index, a.Detail)
}

const RepairPlaceholder = "[internal repair: previous empty message omitted; continue the current task.]"

func IsRepairPlaceholder(text string) bool {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	return trimmed == RepairPlaceholder ||
		strings.Contains(lower, "message content repaired") ||
		strings.Contains(lower, "internal repair: previous empty message omitted")
}

func (h *History) RepairStructure() []RepairAction {
	repaired, actions := RepairMessages(h.messages)
	if len(actions) == 0 {
		return nil
	}
	h.messages = repaired
	h.recount()
	return actions
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
			// Preserve compact results that carry extracted evidence (hashes, creds,
			// leak markers, flags). Blind SQLi / enumeration accumulates many small
			// outputs; clearing them mid-extraction makes the model re-fetch what it
			// already had, which is exactly the churn we want to avoid. Large results
			// are still clearable — they bloat context and are re-fetchable.
			if isCompactEvidence(messageContentText(msg)) {
				continue
			}
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

// MicrocompactThinking replaces older assistant thinking blocks with a small
// marker. This is cheaper than summarizing history and preserves visible task
// output, tool calls, and tool-result pairing.
func (h *History) MicrocompactThinking(keepRecentAssistant int) MicrocompactStats {
	stats := MicrocompactStats{BeforeTokens: h.tokenCount}
	if keepRecentAssistant < 0 {
		keepRecentAssistant = 0
	}
	assistantIndexes := make([]int, 0)
	for i, msg := range h.messages {
		if msg.Role == llm.RoleAssistant && (strings.TrimSpace(msg.ReasoningContent) != "" || hasThinkingTrace(messageContentText(msg))) {
			assistantIndexes = append(assistantIndexes, i)
		}
	}
	compactUntil := len(assistantIndexes) - keepRecentAssistant
	if compactUntil <= 0 {
		stats.AfterTokens = h.tokenCount
		return stats
	}
	for _, idx := range assistantIndexes[:compactUntil] {
		msg := &h.messages[idx]
		compacted := false
		if strings.TrimSpace(msg.ReasoningContent) != "" {
			msg.ReasoningContent = ""
			compacted = true
		}
		if text, ok := msg.Content.(string); ok {
			updated := stripThinkingTrace(text)
			if updated != text {
				msg.Content = updated
				compacted = true
			}
		}
		if compacted {
			stats.Compacted++
		}
	}
	if stats.Compacted > 0 {
		h.recount()
	}
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
	if m.ReasoningContent != "" {
		n += len(m.ReasoningContent)/4 + 4
	}
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

// evidenceKeepMaxChars bounds how large a "compact evidence" result may be and
// still be preserved from clearing. Extraction outputs are tiny; large bodies that
// merely mention a keyword are not the accumulative-state case and stay clearable.
const evidenceKeepMaxChars = 1200

var evidencePatterns = []*regexp.Regexp{
	regexp.MustCompile(`~[^~\n]{2,}~`),                    // EXTRACTVALUE/XPath leak markers
	regexp.MustCompile(`(?i)pass(word|wd)?\s*[:=]`),       // password: / passwd=
	regexp.MustCompile(`(?i)BEGIN [A-Z0-9 ]*PRIVATE KEY`), // private keys
	regexp.MustCompile(`(?i)\b(flag|htb|root|user)\{`),    // CTF/HTB flags
	regexp.MustCompile(`\b[a-f0-9]{16,64}\b`),             // hash-like hex (md5/sha/ntlm), bounded so it can't match long hex blobs
	regexp.MustCompile(`(?i)uid=\d+\([^)]+\)\s+gid=`),     // id(1) output
	regexp.MustCompile(`(?i)(api[_-]?key|secret|token)\s*[:=]\s*\S`),
}

// isCompactEvidence reports whether a small tool result carries extracted evidence
// worth keeping in context across clears/compaction.
func isCompactEvidence(text string) bool {
	if len(text) == 0 || len(text) > evidenceKeepMaxChars {
		return false
	}
	for _, re := range evidencePatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
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

func hasThinkingTrace(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<think>") && strings.Contains(lower, "</think>")
}

func stripThinkingTrace(text string) string {
	for {
		lower := strings.ToLower(text)
		start := strings.Index(lower, "<think>")
		if start < 0 {
			return text
		}
		end := strings.Index(lower[start:], "</think>")
		if end < 0 {
			return text
		}
		end += start + len("</think>")
		text = strings.TrimSpace(text[:start]) + "\n[old thinking trace microcompacted]\n" + strings.TrimSpace(text[end:])
	}
}

func sanitizeCompactedMessages(messages []llm.Message) []llm.Message {
	repaired, _ := RepairMessages(messages)
	return repaired
}

func RepairMessages(messages []llm.Message) ([]llm.Message, []RepairAction) {
	valid := make([]llm.Message, 0, len(messages))
	actions := make([]RepairAction, 0)
	for i, msg := range messages {
		if !validRole(msg.Role) {
			actions = append(actions, RepairAction{Phase: 1, Action: "drop_invalid_role", Index: i, Detail: msg.Role})
			continue
		}
		valid = append(valid, msg)
	}

	withoutLeadingAssistant := make([]llm.Message, 0, len(valid))
	seenUser := false
	for i, msg := range valid {
		if msg.Role == llm.RoleUser {
			seenUser = true
		}
		if !seenUser && msg.Role == llm.RoleAssistant {
			actions = append(actions, RepairAction{Phase: 2, Action: "drop_leading_assistant", Index: i})
			continue
		}
		withoutLeadingAssistant = append(withoutLeadingAssistant, msg)
	}

	merged := make([]llm.Message, 0, len(withoutLeadingAssistant))
	for i, msg := range withoutLeadingAssistant {
		if msg.Role == llm.RoleUser && len(merged) > 0 && merged[len(merged)-1].Role == llm.RoleUser {
			prev := &merged[len(merged)-1]
			prev.Content = mergeMessageContent(*prev, msg)
			actions = append(actions, RepairAction{Phase: 3, Action: "merge_consecutive_user", Index: i})
			continue
		}
		if msg.Role == llm.RoleSystem && len(merged) > 0 && merged[len(merged)-1].Role == llm.RoleSystem {
			prev := &merged[len(merged)-1]
			prev.Content = mergeMessageContent(*prev, msg)
			actions = append(actions, RepairAction{Phase: 3, Action: "merge_consecutive_system", Index: i})
			continue
		}
		merged = append(merged, msg)
	}

	out := make([]llm.Message, 0, len(merged))
	pendingToolIDs := map[string]bool{}
	for i := 0; i < len(merged); i++ {
		msg := merged[i]
		if (msg.Role == llm.RoleAssistant || msg.Role == llm.RoleUser) && isEmptyMessageContent(msg) {
			msg.Content = RepairPlaceholder
			actions = append(actions, RepairAction{Phase: 6, Action: "fill_empty_content", Index: i, Detail: msg.Role})
		}
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			resultIDs := followingToolResultIDs(merged, i+1)
			kept := make([]llm.ToolCallDef, 0, len(msg.ToolCalls))
			trailing := i == len(merged)-1
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" && resultIDs[tc.ID] {
					kept = append(kept, tc)
					continue
				}
				if trailing {
					actions = append(actions, RepairAction{Phase: 7, Action: "strip_trailing_tool_call", Index: i, Detail: firstNonEmpty(tc.ID, tc.Function.Name)})
				} else {
					actions = append(actions, RepairAction{Phase: 4, Action: "strip_unmatched_tool_call", Index: i, Detail: firstNonEmpty(tc.ID, tc.Function.Name)})
				}
			}
			if len(kept) == 0 {
				msg.ToolCalls = nil
				if messageContentText(msg) == RepairPlaceholder {
					msg.Content = "[Tool calls were removed because their results are unavailable.]"
				}
			} else {
				msg.ToolCalls = kept
			}
			pendingToolIDs = map[string]bool{}
			for _, tc := range msg.ToolCalls {
				pendingToolIDs[tc.ID] = true
			}
			out = append(out, msg)
			continue
		}
		if msg.Role == llm.RoleTool {
			if msg.ToolCallID == "" || !pendingToolIDs[msg.ToolCallID] {
				actions = append(actions, RepairAction{Phase: 5, Action: "drop_orphaned_tool_result", Index: i, Detail: msg.ToolCallID})
				continue
			}
			out = append(out, msg)
			delete(pendingToolIDs, msg.ToolCallID)
			continue
		}
		pendingToolIDs = map[string]bool{}
		out = append(out, msg)
	}
	return out, actions
}

func validRole(role string) bool {
	switch role {
	case llm.RoleSystem, llm.RoleUser, llm.RoleAssistant, llm.RoleTool:
		return true
	default:
		return false
	}
}

func mergeMessageContent(a, b llm.Message) string {
	left := strings.TrimSpace(messageContentText(a))
	right := strings.TrimSpace(messageContentText(b))
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + "\n" + right
}

func isEmptyMessageContent(msg llm.Message) bool {
	return strings.TrimSpace(messageContentText(msg)) == ""
}

func followingToolResultIDs(messages []llm.Message, start int) map[string]bool {
	ids := map[string]bool{}
	for i := start; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != llm.RoleTool {
			break
		}
		if msg.ToolCallID != "" {
			ids[msg.ToolCallID] = true
		}
	}
	return ids
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "-"
}
