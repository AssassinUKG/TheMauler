package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"mauler/internal/ledger"
)

// The interactive terminal tools (terminal_send / terminal_read) expose the
// always-on shared PTY as tmux-style primitives: send keystrokes and return
// immediately, then snapshot the live screen on demand. Unlike the blocking
// `shell` tool — which writes a marker-wrapped command and blocks until the
// command finishes or times out — these never wait for completion, so the model
// can drive long-running, streaming, or prompt-driven sessions (msfconsole, ssh,
// a python REPL, nmap it interrupts early) and watch them like a real terminal.

const (
	defaultTerminalReadLines = 40
	maxTerminalReadLines     = 200
	defaultTerminalWaitMs    = 600
	maxTerminalWaitMs        = 10000
)

// controlKeys maps friendly names to the raw control bytes a terminal expects.
var controlKeys = map[string]string{
	"c":   "\x03", // Ctrl-C / SIGINT — interrupt the running command
	"d":   "\x04", // Ctrl-D / EOF
	"z":   "\x1a", // Ctrl-Z / suspend
	"l":   "\x0c", // Ctrl-L / clear
	"u":   "\x15", // Ctrl-U / clear line
	"esc": "\x1b",
	"tab": "\t",
}

// ---- terminal_send ----------------------------------------------------------

type terminalSendTool struct{ app *App }

type terminalSendArgs struct {
	Keys    string `json:"keys"`
	Enter   *bool  `json:"enter"`
	Control string `json:"control"`
	WaitMs  *int   `json:"wait_ms"`
	Lines   int    `json:"lines"`
}

func (t *terminalSendTool) Name() string      { return "terminal_send" }
func (t *terminalSendTool) Destructive() bool { return true }

func (t *terminalSendTool) Description() string {
	return "Type keystrokes into the live shared terminal and return immediately (no waiting for a command to finish). " +
		"Use this — together with terminal_read — to drive interactive or long-running sessions you want to watch and steer in real time: " +
		"a reverse/ssh shell, msfconsole, a python REPL, a [y/N] prompt, or a scan you may interrupt early. " +
		"For a one-shot command whose full output you just want back in a single turn, use the `shell` tool instead. " +
		"Send a command line in `keys` (Enter is pressed by default). Set `control` to send a control key — c=Ctrl-C/interrupt, d=Ctrl-D, z, l, u, esc, tab. " +
		"After sending, this waits wait_ms (default 600) and returns the latest lines of the screen, so you usually see the first output right away; call terminal_read again to keep watching."
}

func (t *terminalSendTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "keys": {"type": "string", "description": "Text/keystrokes to type into the live terminal, e.g. \"nmap -sV 10.10.10.5\". Newlines are sent as typed."},
    "enter": {"type": "boolean", "description": "Press Enter after the keys. Default true (skipped automatically if keys already ends with a newline, or when only a control key is sent)."},
    "control": {"type": "string", "description": "Send a single control key instead of/after the text: c (Ctrl-C / interrupt), d (Ctrl-D / EOF), z, l (clear), u (clear line), esc, tab."},
    "wait_ms": {"type": "integer", "description": "After sending, wait this many ms then return the latest screen output. Default 600, max 10000. Use a larger wait to let output appear before reading."},
    "lines": {"type": "integer", "description": "Trailing screen lines to return after the wait. Default 40, max 200."}
  },
  "additionalProperties": false
}`)
}

func (t *terminalSendTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args terminalSendArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("terminal_send: bad params: %w", err)
	}

	payload, err := buildTerminalSendPayload(args)
	if err != nil {
		return "", fmt.Errorf("terminal_send: %w", err)
	}
	if payload == "" {
		return "", fmt.Errorf("terminal_send: provide keys and/or control")
	}

	sess, err := t.app.ensureShellSession()
	if err != nil {
		return "", fmt.Errorf("terminal_send: %w", err)
	}
	if _, err := io.WriteString(sess.input, payload); err != nil {
		return "", fmt.Errorf("terminal_send: write to terminal: %w", err)
	}
	t.app.recordLedger(ledger.Event{
		Kind:     "shell_input",
		Source:   "terminal",
		Status:   "sent",
		Message:  sess.id,
		Input:    strings.TrimRight(args.Keys, "\r\n"),
		Metadata: map[string]string{"interactive": "true", "bytes": fmt.Sprintf("%d", len(payload))},
	})

	wait := clampWaitMs(args.WaitMs)
	if wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return formatTerminalScreen(sess.scroll, args.Lines), nil
}

// buildTerminalSendPayload turns the tool args into the exact byte string to
// write to the PTY: the typed text, an optional Enter, and an optional control
// key. Pure (no I/O) so it is unit-testable without a live terminal.
func buildTerminalSendPayload(args terminalSendArgs) (string, error) {
	var sb strings.Builder
	sb.WriteString(args.Keys)

	appendEnter := true
	if args.Enter != nil {
		appendEnter = *args.Enter
	}
	hasKeys := args.Keys != ""
	endsWithNewline := strings.HasSuffix(args.Keys, "\n") || strings.HasSuffix(args.Keys, "\r")
	if appendEnter && hasKeys && !endsWithNewline {
		sb.WriteString("\r")
	}

	if c := strings.TrimSpace(strings.ToLower(args.Control)); c != "" {
		seq, ok := controlKeys[c]
		if !ok {
			return "", fmt.Errorf("unknown control key %q (use c, d, z, l, u, esc, tab)", args.Control)
		}
		sb.WriteString(seq)
	}
	return sb.String(), nil
}

// ---- terminal_read ----------------------------------------------------------

type terminalReadTool struct{ app *App }

type terminalReadArgs struct {
	Lines  int  `json:"lines"`
	WaitMs *int `json:"wait_ms"`
}

func (t *terminalReadTool) Name() string      { return "terminal_read" }
func (t *terminalReadTool) Destructive() bool { return false }

func (t *terminalReadTool) Description() string {
	return "Return the latest output of the live shared terminal right now, without sending anything — the agent's way to 'look at the screen'. " +
		"Call it repeatedly to watch a long-running or interactive command stream (after terminal_send). " +
		"Optionally wait a moment first with wait_ms to let more output arrive."
}

func (t *terminalReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "lines": {"type": "integer", "description": "Trailing screen lines to return. Default 40, max 200."},
    "wait_ms": {"type": "integer", "description": "Wait this many ms before snapshotting, to let more output arrive. Default 0, max 10000."}
  },
  "additionalProperties": false
}`)
}

func (t *terminalReadTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args terminalReadArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("terminal_read: bad params: %w", err)
	}

	t.app.shellMu.Lock()
	sess := t.app.shellSess
	t.app.shellMu.Unlock()
	if sess == nil {
		return "No live terminal session yet. Use terminal_send (or the shell tool) to start one.", nil
	}

	if wait := clampWaitMs(args.WaitMs); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return formatTerminalScreen(sess.scroll, args.Lines), nil
}

// ---- shared helpers ---------------------------------------------------------

func clampWaitMs(ms *int) time.Duration {
	v := defaultTerminalWaitMs
	if ms != nil {
		v = *ms
	}
	if v < 0 {
		v = 0
	}
	if v > maxTerminalWaitMs {
		v = maxTerminalWaitMs
	}
	return time.Duration(v) * time.Millisecond
}

func formatTerminalScreen(scroll *terminalScrollback, lines int) string {
	if lines <= 0 {
		lines = defaultTerminalReadLines
	}
	if lines > maxTerminalReadLines {
		lines = maxTerminalReadLines
	}
	tail := scroll.tail(lines)
	if len(tail) == 0 {
		return "[terminal idle — no output captured yet. Call terminal_read again to keep polling, or terminal_send to type a command.]"
	}
	total := scroll.length()
	header := fmt.Sprintf("[live terminal — last %d of %d line(s)]", len(tail), total)
	return header + "\n" + strings.Join(tail, "\n")
}
