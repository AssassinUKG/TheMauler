package app

import (
	"fmt"
	"strings"

	"mauler/internal/llm"
	"mauler/internal/tools"
)

func initialRunPromptText(firstMsg llm.Message, run TaskRun) string {
	if firstMsg.Role != "" {
		if text := strings.TrimSpace(messageText(firstMsg)); text != "" {
			return text
		}
	}
	if text := strings.TrimSpace(run.Prompt); text != "" {
		return text
	}
	if text := strings.TrimSpace(run.Summary); text != "" {
		return text
	}
	return ""
}

func (a *App) appendGoalReminder(run *TaskRun) {
	if a == nil || run == nil {
		return
	}
	reminder := goalReminderPrompt(*run)
	if strings.TrimSpace(reminder) == "" {
		return
	}
	a.mu.Lock()
	a.history.Append(llm.NewTextMessage(llm.RoleSystem, reminder))
	a.mu.Unlock()
	run.addEvent("goal_reminder", "Re-injected original task after context drop", reminder)
}

func goalReminderPrompt(run TaskRun) string {
	task := strings.TrimSpace(run.Prompt)
	if task == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Original task reminder: ")
	sb.WriteString(task)
	if todos, err := tools.LoadTodos(); err == nil && len(todos) > 0 {
		sb.WriteString("\nCurrent plan state:")
		limit := len(todos)
		if limit > 8 {
			limit = 8
		}
		for i := 0; i < limit; i++ {
			item := todos[i]
			fmt.Fprintf(&sb, "\n- [%s] %s", item.Status, item.Text)
		}
	}
	sb.WriteString("\nContinue from the latest verified state. Do not restart or repeat completed tool calls unless the previous result is unavailable.")
	return sb.String()
}
