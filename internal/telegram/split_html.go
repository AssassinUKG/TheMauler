package telegram

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Port of Jade's restricted, balanced HTML splitter. Limits apply to visible
// UTF-16 units; markup, entities and supplementary characters are never cut.
func splitBalancedHTML(text string, limit int) ([]string, error) {
	type tag struct{ name, open string }
	var stack []tag
	var chunks []string
	var current strings.Builder
	units := 0
	for len(text) > 0 {
		token, cost := "", 0
		switch text[0] {
		case '<':
			end := strings.IndexByte(text, '>')
			if end < 0 {
				return nil, fmt.Errorf("unterminated Telegram HTML tag")
			}
			token = text[:end+1]
			if strings.HasPrefix(token, "</") {
				name := token[2 : len(token)-1]
				if len(stack) == 0 || stack[len(stack)-1].name != name {
					return nil, fmt.Errorf("unbalanced Telegram HTML")
				}
				stack = stack[:len(stack)-1]
			} else {
				name := ""
				switch token {
				case "<b>", "<i>", "<code>", "<pre>":
					name = token[1 : len(token)-1]
				default:
					if strings.HasPrefix(token, `<a href="`) && strings.HasSuffix(token, `">`) {
						href := token[9 : len(token)-2]
						if len(href) <= 4096 && !strings.ContainsAny(href, "<>\"") && (strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "http://")) {
							name = "a"
						}
					}
				}
				if name == "" || len(stack) >= 16 {
					return nil, fmt.Errorf("unsupported Telegram HTML")
				}
				for _, parent := range stack {
					if parent.name == "code" || parent.name == "pre" || name == "code" || name == "pre" || name == "a" && parent.name == "a" {
						return nil, fmt.Errorf("invalid Telegram HTML nesting")
					}
				}
				stack = append(stack, tag{name, token})
			}
		case '&':
			end := strings.IndexByte(text, ';')
			if end < 0 {
				return nil, fmt.Errorf("unterminated Telegram HTML entity")
			}
			token = text[:end+1]
			switch token {
			case "&amp;", "&lt;", "&gt;", "&quot;", "&#34;", "&#39;":
				cost = 1
			default:
				return nil, fmt.Errorf("unsupported Telegram HTML entity")
			}
		default:
			r, size := utf8.DecodeRuneInString(text)
			token = text[:size]
			cost = utf16.RuneLen(r)
		}
		if cost > 0 && units+cost > limit {
			for index := len(stack) - 1; index >= 0; index-- {
				current.WriteString("</" + stack[index].name + ">")
			}
			chunks = append(chunks, current.String())
			current.Reset()
			units = 0
			for _, opened := range stack {
				current.WriteString(opened.open)
			}
		}
		current.WriteString(token)
		units += cost
		text = text[len(token):]
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unclosed Telegram HTML tag")
	}
	if units > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks, nil
}
