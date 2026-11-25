package telegram

import (
	"html"
	"net/url"
	"regexp"
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
		heading := strings.TrimLeft(line, "#")
		isHeading := len(line)-len(heading) > 0 && len(line)-len(heading) <= 6 && strings.HasPrefix(heading, " ")
		if isHeading {
			line = strings.TrimSpace(heading)
		}
		escaped := telegramInline(line)
		if isHeading {
			escaped = "<b>" + escaped + "</b>"
		}
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
	chunks, err := splitBalancedHTML(text, telegramChunkLimit)
	if err != nil {
		chunks, _ = splitBalancedHTML(html.EscapeString(text), telegramChunkLimit)
	}
	if len(chunks) == 0 {
		return []string{""}
	}
	return chunks
}

var telegramLinkRE = regexp.MustCompile(`\[([^\]\n]{1,250})\]\((https?://[^\s)]+)\)`)

func telegramProse(text string) string {
	escaped := html.EscapeString(text)
	escaped = telegramLinkRE.ReplaceAllStringFunc(escaped, func(match string) string {
		parts := telegramLinkRE.FindStringSubmatch(match)
		link := html.UnescapeString(parts[2])
		u, err := url.Parse(link)
		if err != nil || u.Host == "" || u.User != nil || len(link) > 4096 {
			return match
		}
		return `<a href="` + html.EscapeString(link) + `">` + parts[1] + `</a>`
	})
	return convertPairs(escaped, "**", "<b>", "</b>")
}

func telegramInline(text string) string {
	var out strings.Builder
	for {
		start := strings.IndexByte(text, '`')
		if start < 0 {
			out.WriteString(telegramProse(text))
			break
		}
		end := strings.IndexByte(text[start+1:], '`')
		if end < 0 {
			out.WriteString(telegramProse(text))
			break
		}
		end += start + 1
		out.WriteString(telegramProse(text[:start]))
		out.WriteString("<code>" + html.EscapeString(text[start+1:end]) + "</code>")
		text = text[end+1:]
	}
	return out.String()
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
