package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
)

const maxTextReplyContinuations = 3
const textReplyContinuationInstruction = "[text_reply_continuation] The previous answer hit its token limit. Continue exactly from its last sentence without restarting, repeating earlier paragraphs, or adding a new preamble. Finish the original requested answer. Text only; no tools, files, commands, or external actions."
const postToolReplyRetryInstruction = "[post_tool_reply_retry] Write the complete user-facing answer to my original request now. Use the tool results already in this conversation and report failed or unverified work accurately. Tool execution for this turn has ended. Leave any remaining todo entries as they are. Return the final answer as plain response text with no tool calls or tool markup."

// Replay only the final model turn. The tool observations and IDs are already
// in req.Messages, so they must remain intact in the recovery request.
func postToolReplyRetryRequest(req llm.Request, usage *llm.Usage, contextWindow int) (llm.Request, bool) {
	if contextWindow <= 0 || req.MaxTokens <= 0 {
		return req, false
	}
	firstLimit := req.MaxTokens
	if usage != nil {
		firstLimit = max(firstLimit, usage.CompletionTokens)
	}
	req.Tools, req.ToolChoice = nil, "none"
	req.JSONSchema, req.Grammar = nil, ""
	req.Messages = append(append([]llm.Message(nil), req.Messages...), llm.NewTextMessage(llm.RoleUser, postToolReplyRetryInstruction))
	promptTokens := buildPromptBudgetSnapshot(req.Messages, nil, contextWindow).TotalTokens
	if usage != nil {
		promptTokens = max(promptTokens, usage.PromptTokens)
	}
	headroom := contextWindow - promptTokens - 1024
	if firstLimit >= min(headroom, directAnswerMaxTokens) {
		return req, false
	}
	req.MaxTokens = min(max(firstLimit*2, firstLimit+512), headroom, directAnswerMaxTokens)
	return req, true
}

func collectPostToolReply(ctx context.Context, client llm.Client, req llm.Request, observe func(llm.Delta)) (string, *llm.Usage, error) {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	deltas, err := client.Chat(ctx, req)
	if err != nil {
		return "", nil, err
	}
	var content strings.Builder
	var usage *llm.Usage
	var done, cutoff, proposedTool bool
	for {
		select {
		case <-ctx.Done():
			return "", usage, ctx.Err()
		case delta, ok := <-deltas:
			if !ok {
				if !done || cutoff || proposedTool || strings.TrimSpace(content.String()) == "" {
					return "", usage, fmt.Errorf("the final reply retry did not return complete text")
				}
				return content.String(), usage, nil
			}
			observe(delta)
			if delta.Error != nil {
				return "", usage, delta.Error
			}
			content.WriteString(delta.Content)
			done = done || delta.Done
			cutoff = cutoff || delta.Truncated
			proposedTool = proposedTool || len(delta.ToolCalls) != 0
			if delta.Usage != nil {
				usage = delta.Usage
			}
		}
	}
}

type textReplyContinuation struct {
	prefix   string
	last     string
	attempts int
}

func joinTextReply(prefix, next string) string {
	if strings.TrimSpace(prefix) == "" {
		return strings.TrimSpace(next)
	}
	if strings.TrimSpace(next) == "" {
		return strings.TrimSpace(prefix)
	}
	if strings.HasSuffix(strings.TrimSpace(prefix), strings.TrimSpace(next)) {
		return strings.TrimSpace(prefix)
	}
	return strings.TrimSpace(prefix) + "\n\n" + strings.TrimSpace(next)
}

func canContinueTextReply(run TaskRun, direct bool) bool {
	return direct && len(run.Tools) == 0 && run.Contract != nil &&
		len(run.Contract.Deliverables) == 0 && len(run.Contract.RequiredEvidence) == 0 &&
		!directModeCannotCompleteTask(taskRoutingPrompt(run)) && run.StopReason == ""
}

func (c *textReplyContinuation) advance(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || text == c.last || c.attempts >= maxTextReplyContinuations {
		return false
	}
	c.prefix = joinTextReply(c.prefix, text)
	c.last = text
	c.attempts++
	return true
}

// A large maximum does not force a long answer. Explicit long-form requests get
// up to the existing 8K response reserve, even on a short-answer profile. Keep
// the actual loaded context authoritative and never increase a tool-call limit.
func applyTextReplyResponseBudget(req *llm.Request, prompt string, direct bool, contextWindow int) (bool, error) {
	if req == nil || !direct || len(req.Tools) != 0 || req.ToolChoice != "none" {
		return false, nil
	}
	before := req.MaxTokens
	if requestsLongFormReply(prompt) && contextWindow > 0 && req.MaxTokens < directAnswerMaxTokens {
		req.MaxTokens = directAnswerMaxTokens
	}
	if req.MaxTokens <= 0 || req.MaxTokens > directAnswerMaxTokens {
		req.MaxTokens = directAnswerMaxTokens
	}
	if contextWindow > 0 {
		headroom := contextWindow - buildPromptBudgetSnapshot(req.Messages, nil, contextWindow).TotalTokens - 1024
		if headroom < 256 {
			return false, fmt.Errorf("not enough context remains for a complete response; compact or start a new chat")
		}
		if req.MaxTokens > headroom {
			req.MaxTokens = headroom
		}
	}
	return req.MaxTokens != before, nil
}

// Collapse only the exact continuation this run owns. Saved Chat and the live
// UI then retain one whole answer, not a final fragment or duplicate copies.
// Caller holds App.mu. Other history and operator messages are never removed.
func appendTextReplyHistory(history *agent.History, msg llm.Message, prefix string) {
	if prefix != "" {
		messages := history.Messages()
		n := len(messages)
		if n >= 2 && messages[n-1].Role == llm.RoleSystem &&
			messageText(messages[n-1]) == textReplyContinuationInstruction &&
			messages[n-2].Role == llm.RoleAssistant && strings.TrimSpace(messageText(messages[n-2])) == prefix {
			history.Replace(append(messages[:n-2], msg))
			return
		}
	}
	history.Append(msg)
}
