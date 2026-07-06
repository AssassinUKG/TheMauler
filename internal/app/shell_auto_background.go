package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"mauler/internal/llm"
)

func autoBackgroundShellCall(tc llm.ToolCallDef) (json.RawMessage, string, bool) {
	if !isShellTool(tc.Function.Name) {
		return nil, "", false
	}
	var args map[string]any
	if len(tc.Function.Arguments) == 0 || json.Unmarshal(tc.Function.Arguments, &args) != nil {
		return nil, "", false
	}
	if strings.TrimSpace(stringFromAny(args["job"])) != "" {
		return nil, "", false
	}
	if bg, ok := args["background"].(bool); ok && bg {
		return nil, "", false
	}
	command := strings.TrimSpace(stringFromAny(args["command"]))
	if command == "" || !shouldAutoBackgroundCommand(command, intFromAny(args["timeout"])) {
		return nil, "", false
	}
	args["background"] = true
	delete(args, "timeout")
	out, err := json.Marshal(args)
	if err != nil {
		return nil, "", false
	}
	return json.RawMessage(out), fmt.Sprintf("command=%q", truncateRunes(command, 220)), true
}

func shouldAutoBackgroundCommand(command string, timeout int) bool {
	lower := strings.ToLower(command)
	if strings.Contains(lower, "background=false") || strings.Contains(lower, "timeout 5") || strings.Contains(lower, "timeout 10") {
		return false
	}
	if strings.Contains(lower, "nmap ") {
		return containsAny(lower, "-p-", "--top-ports", "--script", " -a", "-sv", "-sc")
	}
	for _, name := range []string{"ffuf", "gobuster", "feroxbuster", "wfuzz", "hydra", "sqlmap", "hashcat", "john", "nikto", "dirsearch"} {
		if strings.Contains(lower, name+" ") || strings.HasPrefix(lower, name) {
			return true
		}
	}
	return false
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		var out int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &out)
		return out
	default:
		return 0
	}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
