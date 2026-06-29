package tools

import (
	"html"
	"path/filepath"
	"regexp"
	"strings"
)

// guardFileContent applies filetype-specific normalization to content the model writes,
// fixing systematic model quirks without touching content types where the same text is
// legitimate. Returns the (possibly fixed) content and a short note describing any change
// (empty when nothing changed). Add new cases per filetype as needed.
func guardFileContent(path, content string) (string, string) {
	switch fileKind(path, content) {
	case "shell":
		if fixed := unescapeShellOperators(content); fixed != content {
			return fixed, "guard: unescaped HTML-encoded shell operators in script"
		}
	}
	return content, ""
}

// fileKind classifies a file by extension, then by shebang as a fallback.
func fileKind(path, content string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh", ".bash", ".zsh":
		return "shell"
	}
	head := strings.TrimSpace(content)
	if len(head) > 80 {
		head = head[:80]
	}
	if strings.HasPrefix(head, "#!") {
		line := head
		if i := strings.IndexByte(line, '\n'); i >= 0 {
			line = line[:i]
		}
		if strings.Contains(line, "bash") || strings.Contains(line, "/sh") || strings.Contains(line, "zsh") {
			return "shell"
		}
	}
	return "text"
}

var shellOperatorEntityRE = regexp.MustCompile(`&(amp|gt|lt|quot);`)

// unescapeShellOperators reverses the model's habit of HTML-escaping shell operators
// (2&gt;&amp;1) in script files, which otherwise makes the saved script fail to run.
// Unescapes to a fixpoint to undo compounded escaping (&amp;amp;...). Only runs when an
// operator entity is actually present.
func unescapeShellOperators(content string) string {
	if !shellOperatorEntityRE.MatchString(content) {
		return content
	}
	for i := 0; i < 24; i++ {
		next := html.UnescapeString(content)
		if next == content {
			break
		}
		content = next
	}
	return content
}
