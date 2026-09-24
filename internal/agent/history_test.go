package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestHistoryCountsToolCallArguments(t *testing.T) {
	h := NewHistory(1000)
	args := strings.Repeat("x", 4000)
	h.Append(llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCallDef{{
			ID:   "call-1",
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "write_file",
				Arguments: json.RawMessage(`{"content":"` + args + `"}`),
			},
		}},
	})

	if h.TokenCount() < 900 {
		t.Fatalf("tool call arguments were undercounted: %d", h.TokenCount())
	}
}

func TestNeedsCompactionWithReserveUsesEffectiveBudget(t *testing.T) {
	h := NewHistory(1000)
	h.Append(llm.NewTextMessage(llm.RoleUser, strings.Repeat("x", 2000)))

	if !h.NeedsCompactionWithReserve(0.85, 500) {
		t.Fatalf("expected compaction once overhead reserve lowers effective budget")
	}
}

func TestCompactDropsOrphanedToolResultAtBoundary(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.NewTextMessage(llm.RoleSystem, "system"))
	h.Append(llm.NewTextMessage(llm.RoleUser, "first"))
	h.Append(llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCallDef{{
			ID:       "call-1",
			Type:     "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
		}},
	})
	h.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "call-1", Name: "read_file", Content: "orphan if kept alone"})
	h.Append(llm.NewTextMessage(llm.RoleUser, "tail"))

	h.Compact("summary", 1, 2)

	for i, msg := range h.Messages() {
		if msg.Role == llm.RoleTool && (i == 0 || len(h.Messages()[i-1].ToolCalls) == 0) {
			t.Fatalf("compact kept orphaned tool message at %d: %#v", i, h.Messages())
		}
	}
}

func TestRepairMessagesDropsInvalidRole(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "system"),
		llm.NewTextMessage("developer", "bad"),
		llm.NewTextMessage(llm.RoleUser, "task"),
	})

	if len(msgs) != 2 {
		t.Fatalf("expected invalid role to be dropped, got %#v", msgs)
	}
	if len(actions) != 1 || actions[0].Action != "drop_invalid_role" {
		t.Fatalf("expected drop_invalid_role action, got %#v", actions)
	}
}

func TestRepairMessagesDropsLeadingAssistant(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "system"),
		llm.NewTextMessage(llm.RoleAssistant, "stale answer"),
		llm.NewTextMessage(llm.RoleUser, "task"),
		llm.NewTextMessage(llm.RoleAssistant, "ok"),
	})

	if len(msgs) != 3 || msgs[1].Role != llm.RoleUser {
		t.Fatalf("expected leading assistant to be dropped, got %#v", msgs)
	}
	if !hasRepairAction(actions, "drop_leading_assistant") {
		t.Fatalf("expected drop_leading_assistant action, got %#v", actions)
	}
}

func TestRepairMessagesMergesConsecutiveUserMessages(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "first"),
		llm.NewTextMessage(llm.RoleUser, "second"),
	})

	if len(msgs) != 1 || messageContentText(msgs[0]) != "first\nsecond" {
		t.Fatalf("expected merged user messages, got %#v", msgs)
	}
	if !hasRepairAction(actions, "merge_consecutive_user") {
		t.Fatalf("expected merge_consecutive_user action, got %#v", actions)
	}
}

func TestRepairMessagesMergesConsecutiveSystemMessages(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		llm.NewTextMessage(llm.RoleAssistant, "initial answer"),
		llm.NewTextMessage(llm.RoleSystem, "first controller repair"),
		llm.NewTextMessage(llm.RoleSystem, "second controller repair"),
	})

	if len(msgs) != 3 || msgs[2].Role != llm.RoleSystem ||
		messageContentText(msgs[2]) != "first controller repair\nsecond controller repair" {
		t.Fatalf("expected merged controller messages, got %#v", msgs)
	}
	if !hasRepairAction(actions, "merge_consecutive_system") {
		t.Fatalf("expected merge_consecutive_system action, got %#v", actions)
	}
}

func TestRepairMessagesMergesAssistantTextButPreservesStructuredContent(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: "text", Text: "first"}, {Type: "image_url", ImageURL: &llm.ImageURL{URL: "data:image/png;base64,abc"}}}},
		llm.NewTextMessage(llm.RoleAssistant, "second"),
	})
	if len(msgs) != 2 || !hasRepairAction(actions, "merge_consecutive_assistant") {
		t.Fatalf("assistant messages were not merged: msgs=%#v actions=%#v", msgs, actions)
	}
	blocks, ok := msgs[1].Content.([]llm.ContentBlock)
	if !ok || len(blocks) != 3 || blocks[1].ImageURL == nil || blocks[1].ImageURL.URL == "" {
		t.Fatalf("structured assistant content was not preserved: %#v", msgs[1].Content)
	}
}

func TestRepairMessagesStripsToolCallsWithoutResults(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCallDef{testToolCall("call-1", "read")}},
		llm.NewTextMessage(llm.RoleAssistant, "continued without a tool result"),
	})

	if len(msgs) != 3 {
		t.Fatalf("expected assistant message to remain as repaired text, got %#v", msgs)
	}
	if len(msgs[1].ToolCalls) != 0 {
		t.Fatalf("expected unmatched tool call to be stripped, got %#v", msgs[1].ToolCalls)
	}
	if !strings.Contains(messageContentText(msgs[1]), "Tool calls were removed") {
		t.Fatalf("expected replacement content, got %q", messageContentText(msgs[1]))
	}
	if !hasRepairAction(actions, "strip_unmatched_tool_call") {
		t.Fatalf("expected strip_unmatched_tool_call action, got %#v", actions)
	}
}

func TestRepairMessagesDropsOrphanToolResult(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		{Role: llm.RoleTool, ToolCallID: "missing", Name: "read", Content: "orphan"},
	})

	if len(msgs) != 1 || msgs[0].Role != llm.RoleUser {
		t.Fatalf("expected orphan tool result to be dropped, got %#v", msgs)
	}
	if !hasRepairAction(actions, "drop_orphaned_tool_result") {
		t.Fatalf("expected drop_orphaned_tool_result action, got %#v", actions)
	}
}

func TestRepairMessagesFillsEmptyUserAndAssistantContent(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, ""),
		llm.NewTextMessage(llm.RoleAssistant, " "),
	})

	if messageContentText(msgs[0]) != RepairPlaceholder || messageContentText(msgs[1]) != RepairPlaceholder {
		t.Fatalf("expected placeholders, got %#v", msgs)
	}
	if countRepairActions(actions, "fill_empty_content") != 2 {
		t.Fatalf("expected two fill_empty_content actions, got %#v", actions)
	}
}

func TestRepairMessagesKeepsMatchedMultipleToolResults(t *testing.T) {
	input := []llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		{Role: llm.RoleAssistant, Content: "running tools", ToolCalls: []llm.ToolCallDef{
			testToolCall("call-1", "read"),
			testToolCall("call-2", "grep"),
		}},
		{Role: llm.RoleTool, ToolCallID: "call-1", Name: "read", Content: "read output"},
		{Role: llm.RoleTool, ToolCallID: "call-2", Name: "grep", Content: "grep output"},
		llm.NewTextMessage(llm.RoleAssistant, "done"),
	}
	msgs, actions := RepairMessages(input)

	if len(actions) != 0 {
		t.Fatalf("expected clean multi-tool transcript to need no repair, got %#v", actions)
	}
	if len(msgs) != len(input) || len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("expected all matched tool calls/results to remain, got %#v", msgs)
	}
}

func TestRepairMessagesStripsTrailingToolCalls(t *testing.T) {
	msgs, actions := RepairMessages([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCallDef{testToolCall("call-1", "read")}},
	})

	if len(msgs) != 2 {
		t.Fatalf("expected assistant message to remain as repaired text, got %#v", msgs)
	}
	if len(msgs[1].ToolCalls) != 0 {
		t.Fatalf("expected trailing tool call to be stripped, got %#v", msgs[1].ToolCalls)
	}
	if !hasRepairAction(actions, "strip_trailing_tool_call") {
		t.Fatalf("expected strip_trailing_tool_call action, got %#v", actions)
	}
}

func TestRepairMessagesCollapsesOnlyExactControllerContinuation(t *testing.T) {
	prompt := "Your response was cut off by the token limit. Continue from exactly where you left off."
	msgs, report := RepairMessagesReport([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "real task"),
		llm.NewTextMessage(llm.RoleAssistant, "partial"),
		llm.NewTextMessage(llm.RoleUser, prompt),
		llm.NewTextMessage(llm.RoleUser, prompt),
	})
	if !report.Valid || !hasRepairAction(report.Actions, "collapse_stale_continuation") {
		t.Fatalf("exact continuation was not collapsed: %#v", report)
	}
	if len(msgs) != 3 {
		t.Fatalf("repaired message count = %d, want 3: %#v", len(msgs), msgs)
	}
}

func TestRepairMessagesDeduplicatesOnlySameIDAndPayload(t *testing.T) {
	duplicate := testToolCall("call-1", "read")
	distinctParallel := testToolCall("call-2", "read")
	msgs, report := RepairMessagesReport([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		{Role: llm.RoleAssistant, Content: "reading", ToolCalls: []llm.ToolCallDef{duplicate, duplicate, distinctParallel}},
		{Role: llm.RoleTool, ToolCallID: "call-1", Name: "read", Content: "one"},
		{Role: llm.RoleTool, ToolCallID: "call-2", Name: "read", Content: "two"},
	})
	if !report.Valid || !hasRepairAction(report.Actions, "deduplicate_exact_tool_call") {
		t.Fatalf("duplicate tool call was not reported: %#v", report)
	}
	if len(msgs) != 4 || len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("distinct parallel work was not preserved: %#v", msgs)
	}
}

func TestRepairMessagesReportsRejectedWithoutUserInstruction(t *testing.T) {
	_, report := RepairMessagesReport([]llm.Message{
		llm.NewTextMessage(llm.RoleAssistant, "stale"),
	})
	if report.Valid || report.Status != "rejected" || !strings.Contains(report.Diagnostic, "no usable message") {
		t.Fatalf("missing-user session report = %#v", report)
	}
}

func TestRepairMessagesMarksEmptyToolResultWithoutInventingEvidence(t *testing.T) {
	msgs, report := RepairMessagesReport([]llm.Message{
		llm.NewTextMessage(llm.RoleUser, "task"),
		{Role: llm.RoleAssistant, Content: "checking", ToolCalls: []llm.ToolCallDef{testToolCall("call-1", "read")}},
		{Role: llm.RoleTool, ToolCallID: "call-1", Name: "read", Content: ""},
	})
	if !report.Valid || !hasRepairAction(report.Actions, "fill_empty_tool_result") {
		t.Fatalf("empty tool result was not repaired: %#v", report)
	}
	if got := messageContentText(msgs[2]); !strings.Contains(got, "no evidence was produced") {
		t.Fatalf("empty tool marker overclaimed evidence: %q", got)
	}
}

func TestRepairMessagesNoopOnCleanHistory(t *testing.T) {
	input := []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, "system"),
		llm.NewTextMessage(llm.RoleUser, "task"),
		llm.NewTextMessage(llm.RoleAssistant, "done"),
	}
	msgs, actions := RepairMessages(input)

	if len(actions) != 0 {
		t.Fatalf("expected no repair actions, got %#v", actions)
	}
	if len(msgs) != len(input) {
		t.Fatalf("expected clean transcript to stay same length, got %#v", msgs)
	}
	for i := range input {
		if msgs[i].Role != input[i].Role || messageContentText(msgs[i]) != messageContentText(input[i]) {
			t.Fatalf("clean transcript changed at %d: got %#v want %#v", i, msgs[i], input[i])
		}
	}
}

func TestRepairStructureRecountsAfterRepair(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.NewTextMessage(llm.RoleUser, "task"))
	h.Append(llm.NewTextMessage(llm.RoleUser, "second"))
	before := h.TokenCount()

	actions := h.RepairStructure()
	if len(actions) == 0 {
		t.Fatalf("expected repair actions")
	}
	if len(h.Messages()) != 1 {
		t.Fatalf("expected history to be repaired in place, got %#v", h.Messages())
	}
	if h.TokenCount() <= 0 || h.TokenCount() > before+10 {
		t.Fatalf("expected token count to be recounted, before=%d after=%d", before, h.TokenCount())
	}
}

func TestClearOldToolResultsKeepsRecentAndShrinksContext(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.NewTextMessage(llm.RoleSystem, "system"))
	for i := 1; i <= 3; i++ {
		h.Append(llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: "call",
			Name:       "read_file",
			Content:    strings.Repeat(string(rune('a'+i)), 2000),
		})
	}
	before := h.TokenCount()
	stats := h.ClearOldToolResults(1)
	if stats.Cleared != 2 {
		t.Fatalf("expected 2 cleared tool results, got %d", stats.Cleared)
	}
	if h.TokenCount() >= before {
		t.Fatalf("expected context to shrink: before=%d after=%d", before, h.TokenCount())
	}
	msgs := h.Messages()
	if !strings.Contains(messageContentText(msgs[1]), "Tool result cleared") {
		t.Fatalf("old tool result was not replaced: %#v", msgs[1].Content)
	}
	if strings.Contains(messageContentText(msgs[3]), "Tool result cleared") {
		t.Fatalf("most recent tool result should be retained")
	}
}

func testToolCall(id, name string) llm.ToolCallDef {
	return llm.ToolCallDef{
		ID:   id,
		Type: "function",
		Function: llm.FunctionCall{
			Name:      name,
			Arguments: json.RawMessage(`{}`),
		},
	}
}

func hasRepairAction(actions []RepairAction, action string) bool {
	return countRepairActions(actions, action) > 0
}

func countRepairActions(actions []RepairAction, action string) int {
	count := 0
	for _, got := range actions {
		if got.Action == action {
			count++
		}
	}
	return count
}

func TestClearOldToolResultsPreservesCompactEvidence(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.NewTextMessage(llm.RoleSystem, "system"))
	// An extracted hash leak (compact evidence) followed by two bulky read_file results.
	h.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "c0", Name: "bash", Content: "syntax error: '~da43a91f0c2b4e5f6a7b8c9d~'"})
	for i := 1; i <= 3; i++ {
		h.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "c", Name: "read_file", Content: "non-evidence body " + strings.Repeat("z", 1500)})
	}
	stats := h.ClearOldToolResults(1)
	msgs := h.Messages()
	// The evidence message (index 1) must survive even though it is "old".
	if strings.Contains(messageContentText(msgs[1]), "Tool result cleared") {
		t.Fatalf("compact evidence result was wrongly cleared: %q", messageContentText(msgs[1]))
	}
	if !strings.Contains(messageContentText(msgs[1]), "da43a91f0c2b4e5f6a7b8c9d") {
		t.Fatalf("evidence content lost: %q", messageContentText(msgs[1]))
	}
	// The bulky read_file results should still be cleared.
	if stats.Cleared < 1 {
		t.Fatalf("expected bulky results to clear, cleared=%d", stats.Cleared)
	}
}

func TestMicrocompactThinkingDropsOldThinkBlocks(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.NewTextMessage(llm.RoleSystem, "system"))
	h.Append(llm.NewTextMessage(llm.RoleAssistant, "<think>"+strings.Repeat("hidden ", 200)+"</think>\nVisible result"))
	h.Append(llm.NewTextMessage(llm.RoleAssistant, "<think>recent reasoning</think>\nRecent visible"))

	before := h.TokenCount()
	stats := h.MicrocompactThinking(1)
	if stats.Compacted != 1 {
		t.Fatalf("expected one old thinking trace compacted, got %d", stats.Compacted)
	}
	if h.TokenCount() >= before {
		t.Fatalf("expected token estimate to shrink: before=%d after=%d", before, h.TokenCount())
	}
	msgs := h.Messages()
	if strings.Contains(messageContentText(msgs[1]), "hidden") || !strings.Contains(messageContentText(msgs[1]), "Visible result") {
		t.Fatalf("old thinking trace was not stripped while preserving visible text: %q", messageContentText(msgs[1]))
	}
	if !strings.Contains(messageContentText(msgs[2]), "recent reasoning") {
		t.Fatalf("recent thinking trace should be preserved")
	}
}

func TestMicrocompactThinkingClearsReasoningWithoutBreakingToolCalls(t *testing.T) {
	h := NewHistory(4096)
	h.Append(llm.Message{
		Role:             llm.RoleAssistant,
		Content:          "Inspecting the file.",
		ReasoningContent: strings.Repeat("tool planning ", 80),
		ToolCalls: []llm.ToolCallDef{{
			ID: "call-1", Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
		}},
	})
	before := h.TokenCount()
	stats := h.MicrocompactThinking(0)
	if stats.Compacted != 1 {
		t.Fatalf("expected one reasoning payload compacted, got %#v", stats)
	}
	msgs := h.Messages()
	if msgs[0].ReasoningContent != "" || len(msgs[0].ToolCalls) != 1 || msgs[0].Content != "Inspecting the file." {
		t.Fatalf("reasoning compaction damaged the tool turn: %#v", msgs[0])
	}
	if h.TokenCount() >= before {
		t.Fatalf("reasoning compaction did not reduce token estimate: before=%d after=%d", before, h.TokenCount())
	}
}

func TestIsCompactEvidence(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"syntax error: '~freepbxuser@localhost~'", true},
		{"password: hunter2", true},
		{"5f4dcc3b5aa765d61d8327deb882cf99", true}, // md5-ish hash
		{"HTB{r00ted}", true},
		{"uid=0(root) gid=0(root) groups=0(root)", true},
		{"just some normal command output with no secrets", false},
		{strings.Repeat("a", 3000) + " password: x", false}, // too large to preserve
	}
	for _, c := range cases {
		if got := isCompactEvidence(c.text); got != c.want {
			t.Errorf("isCompactEvidence(%.40q) = %v, want %v", c.text, got, c.want)
		}
	}
}
