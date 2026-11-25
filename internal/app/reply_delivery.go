package app

import "mauler/internal/llm"

func requestsLongFormReply(text string) bool {
	return hasAnyWholePhrase(text, "long", "detailed", "in depth", "comprehensive", "words", "paragraphs", "chapters")
}

// Delivery metadata is UI/persistence only; provider encoders select wire fields.
func (a *App) recordReplyDeliveryOutcome(status, reason string) {
	if a == nil || a.history == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	messages := a.history.Messages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser {
			break
		}
		if messages[i].Role == llm.RoleAssistant && messageText(messages[i]) != "" {
			messages[i].DeliveryStatus, messages[i].StopReason = status, reason
			a.history.Replace(messages)
			return
		}
	}
}
