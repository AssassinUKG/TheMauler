package app

import (
	"context"
	"fmt"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/settings"
)

const maxEscalationsPerRun = 2

type escalationAttempt struct {
	Text      string
	ToolCalls []llm.ToolCallDef
}

func (a *App) tryEscalation(ctx context.Context, cfg *settings.Settings, currentProfile settings.Profile, run *TaskRun, reason, detail string, toolDefs []llm.ToolDef, coding bool, used *int) (escalationAttempt, bool) {
	if cfg == nil || run == nil || used == nil {
		return escalationAttempt{}, false
	}
	name := strings.TrimSpace(cfg.Agents.EscalationProfile)
	if name == "" || *used >= maxEscalationsPerRun {
		return escalationAttempt{}, false
	}
	a.mu.Lock()
	profiles := a.profiles
	a.mu.Unlock()
	if profiles == nil {
		return escalationAttempt{}, false
	}
	profile, ok := profiles.Profiles[name]
	if !ok || strings.TrimSpace(profile.ModelID) == "" {
		run.addEvent("escalation", "Escalation profile unavailable", fmt.Sprintf("profile=%s", name))
		return escalationAttempt{}, false
	}
	profile = applyProvider(profile, profiles)
	client, err := buildClientForAgent(profile)
	if err != nil {
		run.addEvent("escalation", "Escalation client setup failed", err.Error())
		return escalationAttempt{}, false
	}
	if err := a.ensureModelLoaded(ctx, client, profile); err != nil {
		run.addEvent("escalation", "Escalation model load failed", err.Error())
		return escalationAttempt{}, false
	}

	a.mu.Lock()
	msgs := a.history.Messages()
	a.mu.Unlock()
	prompt := escalationPrompt(run.Prompt, reason, detail, currentProfile, profile)
	msgs = append(msgs, llm.NewTextMessage(llm.RoleUser, prompt))
	req := buildChatRequest(profile, msgs, toolDefs, "auto", false, coding, "high")
	if req.MaxTokens <= 0 || req.MaxTokens > 4096 {
		req.MaxTokens = 4096
	}

	(*used)++
	run.addEvent("escalation", fmt.Sprintf("Escalating hard step to %s (%d/%d)", name, *used, maxEscalationsPerRun), prompt)
	ch, err := client.Chat(ctx, req)
	if err != nil {
		run.addEvent("escalation", "Escalation chat failed", err.Error())
		return escalationAttempt{}, false
	}
	var out strings.Builder
	var calls []llm.ToolCallDef
	for delta := range ch {
		if delta.Error != nil {
			run.addEvent("escalation", "Escalation stream failed", delta.Error.Error())
			return escalationAttempt{}, false
		}
		out.WriteString(delta.Content)
		calls = append(calls, delta.ToolCalls...)
	}
	text := strings.TrimSpace(out.String())
	for i := range calls {
		calls[i] = normalizeToolCallArguments(calls[i])
	}
	if text == "" && len(calls) == 0 {
		run.addEvent("escalation", "Escalation returned no action", "")
		return escalationAttempt{}, false
	}
	msg := llm.NewTextMessage(llm.RoleAssistant, text)
	if len(calls) > 0 {
		msg.ToolCalls = calls
	}
	a.mu.Lock()
	a.history.Append(msg)
	a.mu.Unlock()
	run.addEvent("escalation", "Escalation produced a recovery step", fmt.Sprintf("text_chars=%d tool_calls=%d", len(text), len(calls)))
	return escalationAttempt{Text: text, ToolCalls: calls}, true
}

func escalationPrompt(originalTask, reason, detail string, current, escalation settings.Profile) string {
	return fmt.Sprintf(
		"Frontier escalation for one hard agent step.\n"+
			"Original task: %s\n"+
			"Local profile that got stuck: %s / %s\n"+
			"Escalation profile: %s / %s\n"+
			"Hard-stop reason: %s\n"+
			"Detail:\n%s\n\n"+
			"Return exactly one recovery step. If a tool call is needed, emit one valid structured tool call with complete JSON arguments. If no tool call is needed, write the concise next instruction/result. Do not restart the whole task.",
		strings.TrimSpace(originalTask),
		current.Name, current.ModelID,
		escalation.Name, escalation.ModelID,
		strings.TrimSpace(reason),
		strings.TrimSpace(detail),
	)
}
