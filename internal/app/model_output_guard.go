package app

import (
	"strings"
	"unicode/utf8"

	"mauler/internal/llm"
)

const (
	promptEchoRoleUser = 1 << iota
	promptEchoRoleAssistant
)

// sanitizeVisibleModelOutput is the final trust boundary between model text and
// user-visible/persisted assistant prose. Some local backends can occasionally
// decode part of their input prompt after a valid answer. Chat-template tokens
// are not guaranteed to survive that failure, so matching only template
// sentinels is insufficient: detect the code-owned prompt packets and exact
// conversational anchors that were sent for this request.
func sanitizeVisibleModelOutput(text string, promptMessages []llm.Message) (string, bool) {
	visible := sanitizeVisibleModelText(text)
	if visible == "" || len(promptMessages) == 0 {
		return visible, false
	}

	if boundary, ok := firstInternalPromptEchoBoundary(visible, promptMessages); ok {
		prefix := trimTrailingPromptEchoLines(visible[:boundary], promptMessages)
		return strings.TrimRight(prefix, " \t\r\n"), true
	}
	if boundary := firstConversationPromptEchoBoundary(visible, promptMessages); boundary >= 0 {
		return strings.TrimRight(visible[:boundary], " \t\r\n"), true
	}
	return visible, false
}

func firstInternalPromptEchoBoundary(text string, promptMessages []llm.Message) (int, bool) {
	markers := activeInternalPromptMarkers(promptMessages)
	if len(markers) == 0 {
		return 0, false
	}
	for start := 0; start <= len(text); {
		candidate := strings.TrimLeft(text[start:], " \t")
		candidate = strings.ToLower(strings.ReplaceAll(candidate, "\r\n", "\n"))
		for _, marker := range markers {
			if strings.HasPrefix(candidate, marker) ||
				(len(candidate) >= 4 && strings.HasPrefix(marker, candidate)) {
				return start, true
			}
		}
		next := strings.IndexByte(text[start:], '\n')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return 0, false
}

func activeInternalPromptMarkers(messages []llm.Message) []string {
	systemMarkers := []string{
		"[control_plane]\ncontract_revision:",
		"the user selected ",
		"you are themauler, an expert ai coding assistant.",
		"conversation mode — direct:",
	}
	attachmentMarkers := []string{
		"attached context from the user:\nsafety boundary: the request above is authoritative.",
		"safety boundary: the request above is authoritative.",
	}
	active := make(map[string]bool, len(systemMarkers)+len(attachmentMarkers))
	for _, message := range messages {
		content := strings.ToLower(messageText(message))
		if message.Role == llm.RoleSystem {
			for _, marker := range systemMarkers {
				if strings.Contains(content, marker) {
					active[marker] = true
				}
			}
		}
		if message.Role == llm.RoleUser {
			_, legacyAttachments := parseLegacyChatAttachments(messageText(message))
			if len(message.Attachments) > 0 || len(legacyAttachments) > 0 {
				for _, marker := range attachmentMarkers {
					if strings.Contains(content, marker) {
						active[marker] = true
					}
				}
			}
		}
	}
	markers := make([]string, 0, len(active))
	for _, marker := range append(systemMarkers, attachmentMarkers...) {
		if active[marker] {
			markers = append(markers, marker)
		}
	}
	return markers
}

func firstConversationPromptEchoBoundary(text string, messages []llm.Message) int {
	anchors := promptEchoAnchors(messages)
	if len(anchors) == 0 {
		return -1
	}
	normalizedText := text
	runStart := -1
	runRoles := 0
	for start := 0; start <= len(normalizedText); {
		end := strings.IndexByte(normalizedText[start:], '\n')
		if end < 0 {
			end = len(normalizedText)
		} else {
			end += start
		}
		line := normalizePromptEchoLine(normalizedText[start:end])
		roles, matched := anchors[line]
		if line == "" {
			// Empty separators do not break a sequence of echoed prompt turns.
		} else if matched {
			if runStart < 0 {
				runStart = start
				runRoles = 0
			}
			runRoles |= roles
			if roles == promptEchoRoleAssistant && utf8.RuneCountInString(line) >= 12 &&
				len(strings.TrimSpace(normalizedText[:start])) >= 32 {
				return start
			}
			if runRoles == promptEchoRoleUser|promptEchoRoleAssistant {
				return runStart
			}
		} else if len(line) >= 3 && len(strings.TrimSpace(normalizedText[:start])) >= 32 &&
			isAssistantPromptEchoPrefix(line, anchors) {
			// Hold a partially streamed prior assistant turn before it reaches
			// Chat. If later tokens diverge, the full line is released normally.
			return start
		} else {
			runStart = -1
			runRoles = 0
		}
		if end >= len(normalizedText) {
			break
		}
		start = end + 1
	}
	return -1
}

func isAssistantPromptEchoPrefix(line string, anchors map[string]int) bool {
	for anchor, roles := range anchors {
		if roles == promptEchoRoleAssistant && strings.HasPrefix(anchor, line) {
			return true
		}
	}
	return false
}

func trimTrailingPromptEchoLines(text string, messages []llm.Message) string {
	anchors := promptEchoAnchors(messages)
	if len(anchors) == 0 {
		return text
	}
	cut := len(text)
	for {
		end := cut
		for end > 0 {
			switch text[end-1] {
			case ' ', '\t', '\r', '\n':
				end--
			default:
				goto trimmed
			}
		}
	trimmed:
		if end == 0 {
			break
		}
		start := strings.LastIndexByte(text[:end], '\n') + 1
		line := normalizePromptEchoLine(text[start:end])
		if _, ok := anchors[line]; !ok {
			break
		}
		cut = start
	}
	return text[:cut]
}

func promptEchoAnchors(messages []llm.Message) map[string]int {
	anchors := make(map[string]int)
	for _, message := range messages {
		role := 0
		switch message.Role {
		case llm.RoleUser:
			role = promptEchoRoleUser
		case llm.RoleAssistant:
			role = promptEchoRoleAssistant
		default:
			continue
		}
		text := messageText(message)
		if message.Role == llm.RoleUser {
			if strings.TrimSpace(message.DisplayContent) != "" {
				text = message.DisplayContent
			} else if display, _ := parseLegacyChatAttachments(text); strings.TrimSpace(display) != "" {
				text = display
			}
		} else {
			text = sanitizeVisibleModelText(text)
		}
		for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			normalized := normalizePromptEchoLine(line)
			if !usePromptEchoAnchor(normalized) {
				continue
			}
			anchors[normalized] |= role
			break // one bounded conversational anchor per message
		}
	}
	// A line used by both roles is ambiguous and cannot prove an echoed turn.
	for line, roles := range anchors {
		if roles == promptEchoRoleUser|promptEchoRoleAssistant {
			delete(anchors, line)
		}
	}
	return anchors
}

func normalizePromptEchoLine(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func usePromptEchoAnchor(text string) bool {
	if utf8.RuneCountInString(text) < 6 || utf8.RuneCountInString(text) > 280 {
		return false
	}
	if strings.HasPrefix(text, "```") || strings.HasPrefix(text, "[control_plane]") ||
		strings.HasPrefix(text, "attached context from the user:") {
		return false
	}
	return true
}

// scrubAssistantPromptEchoes repairs contaminated saved/in-memory transcripts
// without rewriting their source file. A later normal save persists the clean
// assistant content through the existing conversation lifecycle.
func scrubAssistantPromptEchoes(messages []llm.Message) ([]llm.Message, int) {
	out := append([]llm.Message(nil), messages...)
	repaired := 0
	for i := range out {
		if out[i].Role != llm.RoleAssistant {
			continue
		}
		cleaned, suppressed := sanitizeVisibleModelOutput(messageText(out[i]), out[:i])
		if !suppressed {
			continue
		}
		out[i].Content = cleaned
		if out[i].DisplayContent != "" {
			out[i].DisplayContent = cleaned
		}
		repaired++
	}
	return out, repaired
}
