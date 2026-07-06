package telegram

import (
	"html"
	"strings"
)

const telegramChunkLimit = 3800

func TelegramHTMLFromMarkdownish(text string) string {
	var out strings.Builder
	var inFence bool
	var code strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inFence {
				out.WriteString("<pre>")
				out.WriteString(html.EscapeString(strings.TrimRight(code.String(), "\n")))
				out.WriteString("</pre>\n")
				code.Reset()
				inFence = false
			} else {
				inFence = true
			}
			continue
		}
		if inFence {
			code.WriteString(line)
			code.WriteByte('\n')
			continue
		}
		escaped := html.EscapeString(line)
		escaped = convertPairs(escaped, "**", "<b>", "</b>")
		escaped = convertPairs(escaped, "`", "<code>", "</code>")
		out.WriteString(escaped)
		out.WriteByte('\n')
	}
	if inFence && code.Len() > 0 {
		out.WriteString("<pre>")
		out.WriteString(html.EscapeString(strings.TrimRight(code.String(), "\n")))
		out.WriteString("</pre>\n")
	}
	return strings.TrimSpace(out.String())
}

func SplitTelegramHTML(text string) []string {
	if len([]rune(text)) <= telegramChunkLimit {
		if text == "" {
			return []string{""}
		}
		return []string{text}
	}
	var chunks []string
	var current strings.Builder
	for _, line := range strings.Split(text, "\n") {
		lineLen := len([]rune(line)) + 1
		if current.Len() > 0 && len([]rune(current.String()))+lineLen > telegramChunkLimit {
			chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
			current.Reset()
		}
		if lineLen > telegramChunkLimit {
			for _, ch := range line {
				if len([]rune(current.String())) >= telegramChunkLimit {
					chunks = append(chunks, current.String())
					current.Reset()
				}
				current.WriteRune(ch)
			}
			current.WriteByte('\n')
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	if strings.TrimSpace(current.String()) != "" {
		chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
	}
	return chunks
}

func convertPairs(text, marker, open, close string) string {
	var out strings.Builder
	rest := text
	isOpen := true
	for {
		idx := strings.Index(rest, marker)
		if idx < 0 {
			break
		}
		out.WriteString(rest[:idx])
		if isOpen {
			out.WriteString(open)
		} else {
			out.WriteString(close)
		}
		isOpen = !isOpen
		rest = rest[idx+len(marker):]
	}
	out.WriteString(rest)
	if !isOpen {
		out.WriteString(close)
	}
	return out.String()
}
