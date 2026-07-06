package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/settings"
)

// start_listener starts the user-configured reverse-shell listener (Environment
// ListenerBackend/ListenerCommand, e.g. Windows `ncat.exe -lvp {port}`) in the
// shared terminal, so the agent can catch AND interact with the caught shell in
// one place. The listener intentionally blocks the terminal waiting for a
// connection — the agent then fires the payload through a NON-terminal channel
// (webshell / one-shot shell) and watches the terminal with terminal_read.
//
// This fixes the observed bug where the agent fired a reverse-shell payload with
// no listener running (per the configured Windows PowerShell ncat backend), so
// the payload connected to nothing and hung.

type startListenerTool struct{ app *App }

type startListenerArgs struct {
	Port    int    `json:"port"`
	Command string `json:"command"`
}

func (t *startListenerTool) Name() string      { return "start_listener" }
func (t *startListenerTool) Destructive() bool { return true }

func (t *startListenerTool) Description() string {
	return "Start the configured reverse-shell listener (Settings → Environment: listener backend/command, e.g. Windows ncat.exe) in the live terminal, ready to catch a callback. " +
		"Use this BEFORE firing any reverse-shell payload. It returns immediately; the terminal then waits for the connection. " +
		"Trigger the payload through http_probe, shell, or the webshell path (NOT terminal_send - the terminal is busy listening), then call terminal_read to catch and terminal_send to drive the shell. " +
		"LHOST and the listener command are filled in from your Environment settings."
}

func (t *startListenerTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "port": {"type": "integer", "description": "Listen port (default 4444). Use the same port as your payload's LPORT."},
    "command": {"type": "string", "description": "Optional override for the full listener command. Defaults to the configured listener_command with {port} substituted."}
  },
  "additionalProperties": false
}`)
}

func (t *startListenerTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args startListenerArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("start_listener: bad params: %w", err)
	}
	port := args.Port
	if port <= 0 || port > 65535 {
		port = 4444
	}

	t.app.mu.Lock()
	cfg := *t.app.cfg
	t.app.mu.Unlock()

	invocation, lhost, err := buildListenerInvocation(cfg, port, args.Command)
	if err != nil {
		return "", fmt.Errorf("start_listener: %w", err)
	}

	sess, err := t.app.ensureShellSession()
	if err != nil {
		return "", fmt.Errorf("start_listener: %w", err)
	}
	state := sharedTerminalStateSnapshot(sess)
	decision := adviseStartListener(state)
	if !decision.Allowed {
		return formatToolExecutionBlock(decision, state), nil
	}
	if _, err := io.WriteString(sess.input, invocation+"\r"); err != nil {
		return "", fmt.Errorf("start_listener: write to terminal: %w", err)
	}
	t.app.recordLedger(ledger.Event{
		Kind:    "shell_input",
		Source:  "terminal",
		Status:  "sent",
		Message: sess.id,
		Input:   invocation,
		Metadata: map[string]string{
			"tool":  "start_listener",
			"port":  fmt.Sprintf("%d", port),
			"lhost": lhost,
		},
	})
	t.app.upsertAgentSession(AgentSession{
		ID:              fmt.Sprintf("listener:%d", port),
		Kind:            "listener",
		State:           "listening",
		Port:            port,
		Lhost:           lhost,
		Command:         invocation,
		TerminalSession: sess.id,
		LastEvidence:    "listener command sent and waiting for callback",
	})

	select {
	case <-ctx.Done():
	case <-time.After(800 * time.Millisecond):
	}

	lhostMsg := lhost
	if lhostMsg == "" {
		lhostMsg = "<set LHOST: no VPN interface selected>"
	}
	header := fmt.Sprintf("[start_listener lhost=%s port=%d]\nListener started in the terminal and is now waiting for a connection. Do NOT fire the payload with terminal_send (the terminal is busy listening). Trigger the reverse shell via http_probe, shell, or the webshell path using LHOST=%s LPORT=%d, then call terminal_read to catch it and terminal_send to interact.\n%s",
		lhostMsg, port, lhostMsg, port, formatTerminalScreen(sess.scroll, 0))
	header = fmt.Sprintf("[start_listener lhost=%s port=%d]\ncontract:\n  state: listening\n  next_tool: http_probe_or_shell\n  evidence: listener started in shared terminal\n  do_not_repeat: do not start another listener on this terminal/port; trigger the callback through http_probe, shell, or webshell, then terminal_read\n%s",
		lhostMsg, port, strings.TrimPrefix(header, fmt.Sprintf("[start_listener lhost=%s port=%d]\n", lhostMsg, port)))
	return header, nil
}

// buildListenerInvocation resolves the listener command from Environment settings,
// substitutes the port, and wraps it for the configured backend. Returns the
// command to type into the shared (WSL/Kali) terminal and the resolved LHOST.
func buildListenerInvocation(cfg settings.Settings, port int, override string) (string, string, error) {
	env := cfg.Environment
	tmpl := strings.TrimSpace(override)
	if tmpl == "" {
		tmpl = strings.TrimSpace(env.ListenerCommand)
	}
	if tmpl == "" {
		tmpl = "ncat.exe -lvp {port}"
	}
	cmd := strings.ReplaceAll(tmpl, "{port}", fmt.Sprintf("%d", port))
	lhost := resolveLHOST(cfg)

	backend := strings.ToLower(strings.TrimSpace(env.ListenerBackend))
	switch backend {
	case "manual":
		return "", lhost, fmt.Errorf("listener backend is 'manual'; start the listener yourself, then trigger the payload")
	case "windows_powershell", "powershell", "windows":
		// The shared terminal is WSL/Kali; reach the Windows listener via interop.
		// Skip the wrapper if the command already targets PowerShell explicitly.
		if strings.Contains(strings.ToLower(cmd), "powershell") {
			return cmd, lhost, nil
		}
		return fmt.Sprintf(`powershell.exe -NoProfile -Command %s`, powershellQuote(cmd)), lhost, nil
	default:
		// ai_shell / wsl / bash: run directly in the terminal.
		return cmd, lhost, nil
	}
}

func resolveLHOST(cfg settings.Settings) string {
	if strings.EqualFold(strings.TrimSpace(cfg.Environment.LHOSTSource), "manual") {
		if m := strings.TrimSpace(cfg.Environment.ManualLHOST); m != "" {
			return m
		}
	}
	if info, ok := selectedVPNInfo(cfg); ok && strings.TrimSpace(info.IP) != "" {
		return strings.TrimSpace(info.IP)
	}
	return strings.TrimSpace(cfg.Environment.ManualLHOST)
}

// powershellQuote wraps a command string as a single double-quoted PowerShell
// argument, escaping embedded double quotes.
func powershellQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// looksLikeReverseShellTrigger reports whether a command fires a reverse-shell
// callback (as opposed to starting a listener). Used to nudge the model to start
// the listener first.
func looksLikeReverseShellTrigger(command string) bool {
	lower := strings.ToLower(command)
	if looksLikeInteractiveListenerCommand(command) {
		return false // that's the listener, not the trigger
	}
	if strings.Contains(lower, "--lhost") || strings.Contains(lower, "--lport") {
		return true
	}
	if strings.Contains(lower, "/dev/tcp/") {
		return true
	}
	if (strings.Contains(lower, "nc ") || strings.Contains(lower, "ncat ")) && strings.Contains(lower, " -e ") {
		return true
	}
	if strings.Contains(lower, "msfvenom") && strings.Contains(lower, "reverse") {
		return true
	}
	return false
}

// reverseShellFiredWithoutListener reports whether the run fired a reverse-shell
// trigger without having started a listener first.
func reverseShellFiredWithoutListener(run TaskRun) bool {
	if runStartedListener(run) {
		return false
	}
	for _, tool := range run.Tools {
		if isShellTool(tool.Name) || tool.Name == "terminal_send" {
			if looksLikeReverseShellTrigger(shellCommandFromToolArgs(json.RawMessage(tool.Input))) {
				return true
			}
		}
	}
	return false
}

// runStartedListener reports whether a listener was started earlier in this run,
// either via start_listener or an interactive listener command.
func runStartedListener(run TaskRun) bool {
	for _, tool := range run.Tools {
		if tool.Name == "start_listener" {
			return true
		}
		if isShellTool(tool.Name) || tool.Name == "terminal_send" {
			if looksLikeInteractiveListenerCommand(shellCommandFromToolArgs(json.RawMessage(tool.Input))) {
				return true
			}
		}
	}
	return false
}
