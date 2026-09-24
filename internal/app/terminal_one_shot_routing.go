package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"mauler/internal/llm"
)

var independentHTTPCLICommandRE = regexp.MustCompile(`(?i)(?:^|[\s;&|()])(?:sudo\s+)?(?:timeout\s+\S+\s+)?(?:curl|wget)\s`)

// isIndependentHTTPCLICommand identifies deterministic HTTP commands which do
// not belong in the live shared terminal. A hung endpoint otherwise owns the
// PTY, and a small model can spend its recovery budget polling or pressing
// Ctrl-C until the loop circuit breaker stops the run.
func isIndependentHTTPCLICommand(command string) bool {
	return independentHTTPCLICommandRE.MatchString(strings.TrimSpace(command))
}

// routeTerminalHTTPCommandToShell turns a model's mistaken terminal_send curl
// or wget call into the isolated one-shot shell call it intended. The rewrite
// happens before the assistant tool-call message is persisted, so the tool-call
// name, tool result, policy checks, ledger, and evidence all remain consistent.
func routeTerminalHTTPCommandToShell(tc llm.ToolCallDef, toolDefs []llm.ToolDef) (llm.ToolCallDef, string, bool) {
	if !strings.EqualFold(strings.TrimSpace(tc.Function.Name), "terminal_send") || !toolCallAdvertised(toolDefs, "shell") {
		return tc, "", false
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil || !isIndependentHTTPCLICommand(args.Command) {
		return tc, "", false
	}
	raw, err := marshalToolArgsNoHTMLEscape(map[string]any{
		"command": strings.TrimSpace(args.Command),
		"backend": "wsl",
		"timeout": 30,
	})
	if err != nil {
		return tc, "", false
	}
	tc.Function.Name = "shell"
	tc.Function.Arguments = raw
	return tc, fmt.Sprintf("independent HTTP command moved from the live terminal to an isolated WSL one-shot with a 30s wall-clock timeout: %s", truncateLine(args.Command, 180)), true
}
