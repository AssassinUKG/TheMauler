package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

type reviewerJSONVerdict struct {
	Verdict  string   `json:"verdict"`
	Severity string   `json:"severity"`
	Items    []string `json:"items"`
}

var reviewerVerdictSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "verdict":{"type":"string","enum":["approve","request_changes"]},
    "severity":{"type":"string","enum":["blocking","advisory"]},
    "items":{"type":"array","items":{"type":"string"}}
  },
  "required":["verdict","severity","items"]
}`)

// runReviewerPass runs a same-profile, fresh-context, read-only reviewer pass.
// Infrastructure/protocol failures are retried once, then remain explicitly
// inconclusive so unavailable review cannot strengthen a completion verdict.
func (a *App) runReviewerPass(ctx context.Context, run *TaskRun, profile settings.Profile, cfg *settings.Settings) VerifyVerdict {
	reviewProfile := profile
	if sibling, ok := a.thinkingSiblingProfile(profile); ok {
		reviewProfile = sibling
	}
	var verdict VerifyVerdict
	for attempt := 1; attempt <= 2; attempt++ {
		verdict = a.runReviewerPassOnce(ctx, run, reviewProfile, cfg)
		if verdict.Status != "inconclusive" || ctx.Err() != nil {
			return verdict
		}
	}
	verdict.Summary = strings.TrimSpace(verdict.Summary + " Reviewer remained inconclusive after 2 attempts.")
	return verdict
}

func (a *App) runReviewerPassOnce(ctx context.Context, run *TaskRun, profile settings.Profile, cfg *settings.Settings) VerifyVerdict {
	if run == nil || cfg == nil {
		return reviewerInconclusiveVerdict("reviewer unavailable: missing run or config")
	}
	client, err := buildClientForAgent(profile)
	if err != nil {
		return reviewerInconclusiveVerdict("reviewer client failed: " + err.Error())
	}
	if a != nil {
		if err := a.ensureModelLoaded(ctx, client, profile); err != nil {
			return reviewerInconclusiveVerdict("reviewer model load failed: " + err.Error())
		}
	}

	registry := reviewerRegistry(a)
	toolDefs := reviewerReadOnlyToolDefs(registry, cfg)
	maxTools := cfg.Agents.ReviewLoop.ReviewerMaxTools
	if maxTools <= 0 {
		maxTools = 15
	}
	packet := buildReviewPacket(*run)
	msgs := []llm.Message{
		llm.NewTextMessage(llm.RoleSystem, reviewerSystemPrompt(maxTools)),
		llm.NewTextMessage(llm.RoleUser, packet),
	}
	toolsUsed := 0
	for turn := 0; turn < 4; turn++ {
		reqToolDefs := toolDefs
		toolChoice := "auto"
		if toolsUsed >= maxTools {
			reqToolDefs = nil
			toolChoice = "none"
		}
		forceNoThink := toolChoice != "none"
		effort := "low"
		if toolChoice == "none" {
			effort = "high"
		}
		req := buildChatRequest(profile, msgs, reqToolDefs, toolChoice, forceNoThink, toolChoice != "none", effort)
		if toolChoice == "none" {
			req.JSONSchema = reviewerVerdictSchema
		}
		req.MaxTokens = clampReviewerMaxTokens(req.MaxTokens)
		ch, err := client.Chat(ctx, req)
		if err != nil {
			return reviewerInconclusiveVerdict("reviewer request failed: " + err.Error())
		}
		text, calls, err := collectReviewerTurn(ch)
		if err != nil {
			return reviewerInconclusiveVerdict("reviewer stream failed: " + err.Error())
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: text, ToolCalls: calls})
		if len(calls) == 0 {
			if toolChoice == "none" {
				return reviewerVerdictFromText(text)
			}
			msgs = append(msgs, llm.NewTextMessage(llm.RoleUser, "Return the JSON reviewer verdict now. Do not call tools."))
			toolsUsed = maxTools
			continue
		}
		for _, call := range calls {
			if toolsUsed >= maxTools {
				msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, "reviewer tool budget exhausted; return JSON verdict now."))
				continue
			}
			toolsUsed++
			if !reviewerToolAllowed(call.Function.Name) {
				msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, "blocked: reviewer pass is read-only and cannot use this tool."))
				continue
			}
			result, runErr := registry.Run(ctx, call)
			if runErr != nil {
				result = toolErrorResult(result, runErr)
			}
			if len(result) > 4000 {
				result = result[:1000] + "\n...\n" + result[len(result)-3000:]
			}
			msgs = append(msgs, newToolResultMsg(call.ID, call.Function.Name, result))
		}
	}
	return reviewerInconclusiveVerdict("reviewer turn budget exhausted without a verdict")
}

func reviewerRegistry(a *App) *tools.Registry {
	if a != nil && a.registry != nil {
		return a.registry
	}
	r := tools.New()
	if a != nil {
		r.Register(&readToolResultTool{app: a})
	}
	return r
}

func reviewerReadOnlyToolDefs(registry *tools.Registry, cfg *settings.Settings) []llm.ToolDef {
	allowed := map[string]bool{"read": true, "glob": true, "grep": true, "read_tool_result": true}
	enabled := map[string]bool{}
	if cfg != nil {
		enabled = settings.EffectiveEnabledTools(cfg.Tools)
	}
	return registry.ToEnabledToolDefsFor(enabled, allowed)
}

func reviewerToolAllowed(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "glob", "grep", "read_tool_result":
		return true
	default:
		return false
	}
}

func reviewerSystemPrompt(maxTools int) string {
	return fmt.Sprintf("You are a strict reviewer. You did not write this change. Review it against the objective for correctness, missed edge cases, safety, and whether it truly satisfies the ask. You may read files to verify, but you must not edit or run shell commands. Use at most %d read-only tool calls. Return only JSON matching the schema. If approving, include the top residual risk in items.", maxTools)
}

func buildReviewPacket(run TaskRun) string {
	var sb strings.Builder
	sb.WriteString("Objective:\n")
	sb.WriteString(run.Prompt)
	sb.WriteString("\n\nFinal summary:\n")
	sb.WriteString(firstNonEmpty(run.Summary, run.Response, "(none)"))
	sb.WriteString("\n\nTouched files and evidence:\n")
	seen := map[string]bool{}
	for _, tool := range run.Tools {
		if !strings.EqualFold(tool.Status, "done") || !isWriteTool(tool.Name) {
			continue
		}
		path := pathFromToolInput(tool.Input)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		fmt.Fprintf(&sb, "- %s via %s\n", path, tool.Name)
		if content := compactFileContentForReview(path); content != "" {
			sb.WriteString(content)
			sb.WriteString("\n")
		}
	}
	if len(seen) == 0 {
		sb.WriteString("- No file mutations recorded.\n")
	}
	if len(run.Tools) > 0 {
		sb.WriteString("\nTool trail:\n")
		for _, tool := range run.Tools {
			fmt.Fprintf(&sb, "- %s status=%s input=%s result=%s\n", tool.Name, tool.Status, truncateForReview(tool.Input, 300), truncateForReview(tool.Result, 500))
		}
	}
	packet := sb.String()
	if len(packet) > 16000 {
		return packet[:4000] + "\n...\n" + packet[len(packet)-12000:]
	}
	return packet
}

func compactFileContentForReview(path string) string {
	data, err := os.ReadFile(filepath.Clean(filepath.FromSlash(path)))
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > 3000 {
		content = content[:1200] + "\n...\n" + content[len(content)-1800:]
	}
	return "```text\n" + content + "\n```"
}

func truncateForReview(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func clampReviewerMaxTokens(max int) int {
	if max <= 0 || max > 1200 {
		return 1200
	}
	if max < 256 {
		return 256
	}
	return max
}

func collectReviewerTurn(ch <-chan llm.Delta) (string, []llm.ToolCallDef, error) {
	var text strings.Builder
	var calls []llm.ToolCallDef
	for delta := range ch {
		if delta.Error != nil {
			return "", nil, delta.Error
		}
		text.WriteString(delta.Content)
		if len(delta.ToolCalls) > 0 {
			calls = append(calls, delta.ToolCalls...)
		}
	}
	return text.String(), calls, nil
}

func reviewerVerdictFromText(text string) VerifyVerdict {
	var verdict reviewerJSONVerdict
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &verdict); err != nil {
		return reviewerInconclusiveVerdict("reviewer returned malformed JSON verdict")
	}
	verdict.Verdict = strings.ToLower(strings.TrimSpace(verdict.Verdict))
	verdict.Severity = strings.ToLower(strings.TrimSpace(verdict.Severity))
	if verdict.Verdict != "approve" && verdict.Verdict != "request_changes" {
		return reviewerInconclusiveVerdict("reviewer returned an unsupported verdict value")
	}
	if verdict.Severity != "blocking" && verdict.Severity != "advisory" {
		return reviewerInconclusiveVerdict("reviewer returned an unsupported severity value")
	}
	if verdict.Verdict == "approve" {
		return VerifyVerdict{
			Gate:         "reviewer",
			Status:       "pass",
			Summary:      "Reviewer approved the deliverable.",
			Improvements: cleanReviewerItems(verdict.Items),
		}
	}
	blocking := verdict.Severity == "blocking"
	return VerifyVerdict{
		Gate:         "reviewer",
		Status:       "fail",
		Blocking:     blocking,
		Summary:      "Reviewer requested changes.",
		Improvements: cleanReviewerItems(verdict.Items),
	}
}

func reviewerApproveVerdict(summary string) VerifyVerdict {
	return VerifyVerdict{Gate: "reviewer", Status: "pass", Blocking: false, Summary: summary}
}

func reviewerInconclusiveVerdict(summary string) VerifyVerdict {
	return VerifyVerdict{
		Gate:         "reviewer",
		Status:       "inconclusive",
		Blocking:     true,
		Summary:      summary,
		Improvements: []string{"Retry the reviewer when the active profile is available and can return the required JSON verdict."},
	}
}

func cleanReviewerItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.Trim(text, "`")
		text = strings.TrimPrefix(strings.TrimSpace(text), "json")
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end >= start {
		return text[start : end+1]
	}
	return text
}
