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
	if verified := latestVerifiedRunState(run); verified != "" {
		sb.WriteString("\nLatest verified state from this run:")
		sb.WriteString(verified)
	}
	if todos, err := tools.LoadTodos(); err == nil && len(todos) > 0 {
		var active []tools.TodoItem
		for _, item := range todos {
			status := strings.ToLower(strings.TrimSpace(item.Status))
			if status == "done" || status == "completed" {
				continue
			}
			active = append(active, item)
			if len(active) >= 5 {
				break
			}
		}
		if len(active) > 0 {
			sb.WriteString("\nOpen plan items:")
		}
		for i := 0; i < len(active); i++ {
			item := active[i]
			fmt.Fprintf(&sb, "\n- [%s] %s", item.Status, item.Text)
		}
	}
	sb.WriteString("\nContinue from the latest verified state above, even if the older plan is stale. Do not restart or repeat completed tool calls unless the previous result is unavailable.")
	return sb.String()
}

func latestVerifiedRunState(run TaskRun) string {
	var lines []string
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if tool.Status != "done" {
			continue
		}
		switch {
		case isShellTool(tool.Name):
			cmd := shellCommandFromToolArgs([]byte(tool.Input))
			evidence := usefulShellEvidenceSummary(tool.Result)
			if evidence == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("\n- shell: %s => %s", truncateLine(cmd, 120), evidence))
		case strings.HasPrefix(tool.Name, "todo_"):
			continue
		default:
			evidence := truncateLine(tool.Result, 240)
			if evidence == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("\n- %s => %s", tool.Name, evidence))
		}
		if len(lines) >= 5 {
			break
		}
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "")
}
