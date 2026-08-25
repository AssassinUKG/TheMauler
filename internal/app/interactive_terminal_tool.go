package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mauler/internal/ledger"
	"mauler/internal/tools"
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
	terminalReadNowMs        = 0
	terminalPollInterval     = 25 * time.Millisecond
	maxTerminalWaitMs        = 10000
)

// ---- terminal command path --------------------------------------------------

type terminalRunTool struct{ app *App }

type terminalRunArgs struct {
	Command string `json:"command"`
	WaitMs  *int   `json:"wait_ms"`
	WaitFor string `json:"wait_for"`
	Lines   int    `json:"lines"`
}

func (t *terminalRunTool) Name() string      { return "terminal_send" }
func (t *terminalRunTool) Destructive() bool { return true }

func (t *terminalRunTool) Description() string {
	return "Run a command in the live shared terminal like a human would: type it, press Enter, return as soon as first output appears, and never wait for command completion. " +
		"Use terminal_send with command only when that command belongs inside the live terminal session: SSH shells, connected reverse/bind shells, msfconsole, REPLs, prompts, listeners, and long-running commands you need to watch/interact with. " +
		"For independent HTTP/webshell/curl/wget checks, use http_probe when possible or shell when exact flags/pipelines are required; do not type them into a connected terminal. " +
		"Follow up with terminal_read to keep watching or terminal_send to answer prompts, send Ctrl-C, or control the session. " +
		"Use shell for short deterministic one-shot commands where exact output/exit code matters. Do not use terminal_send command to fire a reverse-shell payload while the terminal is already busy listening; use http_probe/shell/webshell for that trigger."
}

func (t *terminalRunTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "Command line to type into the live shared terminal and execute."},
    "wait_ms": {"type": "integer", "description": "Maximum milliseconds to wait. With wait_for=output, returns once output appears. With wait_for=prompt, returns once the shell prompt marker is back. Default 700, max 10000."},
    "wait_for": {"type": "string", "enum": ["output", "prompt"], "description": "output returns after first visible change (default). prompt waits until shell integration reports the prompt is back and includes exit/cwd when available."},
    "lines": {"type": "integer", "description": "Trailing screen lines to return. Default 60, max 200."}
  },
  "required": ["command"],
  "additionalProperties": false
}`)
}

func (t *terminalRunTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args terminalRunArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("terminal_send: bad command params: %w", err)
	}
	command := strings.TrimRight(args.Command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("terminal_send: command is required")
	}
	waitFor := strings.ToLower(strings.TrimSpace(args.WaitFor))
	if waitFor == "" {
		waitFor = "output"
	}
	if waitFor != "output" && waitFor != "prompt" {
		return "", fmt.Errorf("terminal_send: invalid wait_for %q (want output or prompt)", args.WaitFor)
	}
	// Normalize exactly like the shell tool: un-HTML-escape operators (local models
	// emit 2>&amp;&gt;1), reject protected-path mutations, apply scan hygiene. Without
	// this, terminal command mode typed raw entities straight into the shell.
	prepared, err := tools.PrepareShellCommand(command)
	if err != nil {
		return "", fmt.Errorf("terminal_send: %w", err)
	}
	command = prepared
	t.app.mu.Lock()
	toolCfg := t.app.cfg.Tools
	t.app.mu.Unlock()
	if decision, routed := terminalWSLHostCommandDecision(toolCfg, t.app.GetSharedTerminalState(), command); routed {
		return formatToolExecutionBlock(decision, t.app.GetSharedTerminalState()), nil
	}
	sess, err := t.app.ensureShellSession()
	if err != nil {
		return "", fmt.Errorf("terminal_send: %w", err)
	}
	state := sharedTerminalStateSnapshot(sess)
	t.app.updateAgentSessionsFromTerminalState(state)
	decision := adviseTerminalRun(state, command)
	if !decision.Allowed {
		return formatToolExecutionBlock(decision, state), nil
	}
	runID := sharedTerminalRunID()
	startedAt := time.Now()
	if t.app.ctx != nil {
		t.app.emit("mauler:terminal_command_start", map[string]string{
			"id": runID, "session": sess.id, "command": command, "timeout": fmt.Sprintf("%.1f", clampRunWaitMs(args.WaitMs).Seconds()), "tool": "terminal_send",
		})
	}
	resetShellSessionPromptState(sess)
	beforeLen := 0
	if sess.scroll != nil {
		beforeLen = sess.scroll.length()
	}
	beforeGen := terminalScreenGeneration(sess)
	payload := command + "\r"
	if _, err := io.WriteString(sess.input, payload); err != nil {
		return "", fmt.Errorf("terminal_send: write to terminal: %w", err)
	}
	t.app.recordLedger(ledger.Event{
		Kind:    "shell_input",
		Source:  "terminal",
		Status:  "sent",
		Message: sess.id,
		Input:   command,
		Metadata: map[string]string{
			"interactive": "true",
			"tool":        "terminal_send",
			"bytes":       fmt.Sprintf("%d", len(payload)),
		},
	})

	if waitFor == "prompt" {
		waitForTerminalPrompt(ctx, sess, clampRunWaitMs(args.WaitMs))
	} else {
		waitForTerminalOutput(ctx, sess, beforeLen, beforeGen, clampRunWaitMs(args.WaitMs))
	}
	result := formatTerminalRunResult(sess, command, beforeLen, args.Lines)
	snap := sharedTerminalStateSnapshot(sess)
	maybeInjectRemoteShellIntegration(sess, snap.Lines)
	t.app.updateAgentSessionsFromTerminalState(snap)
	if t.app.ctx != nil {
		t.app.emit("mauler:terminal_command_done", map[string]string{
			"id": runID, "session": sess.id, "exit_code": "live", "duration_ms": fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()), "tool": "terminal_send", "result": result,
		})
	}
	return result, nil
}

func clampRunWaitMs(ms *int) time.Duration {
	if ms == nil {
		v := 700
		return time.Duration(v) * time.Millisecond
	}
	return clampWaitMs(ms)
}

func waitForTerminalOutput(ctx context.Context, sess *shellSession, beforeLen int, beforeGen uint64, maxWait time.Duration) {
	if maxWait <= 0 || sess == nil {
		return
	}
	if terminalOutputChanged(sess, beforeLen, beforeGen) {
		return
	}
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	ticker := time.NewTicker(terminalPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			return
		case <-ticker.C:
			if terminalOutputChanged(sess, beforeLen, beforeGen) {
				return
			}
		}
	}
}

func waitForTerminalPrompt(ctx context.Context, sess *shellSession, maxWait time.Duration) {
	if maxWait <= 0 || sess == nil {
		return
	}
	if terminalSessionPromptReady(sess) {
		return
	}
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	ticker := time.NewTicker(terminalPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			return
		case <-ticker.C:
			if terminalSessionPromptReady(sess) {
				return
			}
		}
	}
}

func terminalOutputChanged(sess *shellSession, beforeLen int, beforeGen uint64) bool {
	if sess == nil {
		return false
	}
	if sess.scroll != nil && sess.scroll.length() > beforeLen {
		return true
	}
	return terminalScreenGeneration(sess) != beforeGen
}

func terminalScreenGeneration(sess *shellSession) uint64 {
	if sess == nil || sess.screen == nil {
		return 0
	}
	return sess.screen.generationValue()
}

func formatTerminalRunResult(sess *shellSession, command string, beforeLen int, lines int) string {
	if lines <= 0 {
		lines = 60
	}
	if lines > maxTerminalReadLines {
		lines = maxTerminalReadLines
	}
	newLines := cleanTerminalLines(sess.scroll.since(beforeLen, lines))
	state := classifyTerminalStateForSession(sess, command, newLines, beforeLen, sess.scroll.length())
	commandOutput := formatTerminalCommandOutput(sess, beforeLen, lines)
	hint := terminalStateHint(state)
	failLine := ""
	if exit, failed := terminalCommandFailure(sess, state); failed {
		// Deterministic error detection off the shell-integration exit code: surface
		// it as a structured field and steer the next step, so the model does not
		// mis-read a failed command as success from ambiguous output.
		failLine = fmt.Sprintf("\n  command_failed: exit=%d", exit)
		hint = fmt.Sprintf("the command FAILED (exit=%d). Diagnose the failure class (network/DNS, path/URL, auth, payload, or command syntax) before retrying or claiming success/failure. Do not report success.", exit)
	}
	return fmt.Sprintf("[terminal_send session=%s state=%s]\ncontract:\n  state: %s\n  exit: %s\n  cwd: %s\n  next_tool: %s%s\n  evidence: %s\n  do_not_repeat: do not rerun the same command if this output already answered the check\ncommand: %s\nnext: %s\n%s",
		sess.id, state, state, terminalExitLabel(sess), terminalCWDLabel(sess), nextToolForTerminalState(state), failLine, truncateRunes(hint, 240), command, hint, commandOutput)
}

func formatTerminalCommandOutput(sess *shellSession, beforeLen int, lines int) string {
	if sess == nil || sess.scroll == nil {
		return "[terminal command output unavailable]"
	}
	if lines <= 0 {
		lines = 60
	}
	if lines > maxTerminalReadLines {
		lines = maxTerminalReadLines
	}
	delta := cleanTerminalLines(sess.scroll.since(beforeLen, lines))
	if len(delta) > 0 {
		return fmt.Sprintf("[terminal command output - %d new line(s)]\n%s", len(delta), strings.Join(delta, "\n"))
	}
	if terminalSessionPromptReady(sess) {
		return "[terminal command output - no new complete lines; prompt is back]"
	}
	return "[terminal command output - no new complete lines yet; watch with terminal_read only if the state says running/no_output_yet]"
}

// terminalCommandFailure reports the shell-integration exit code when the last
// command has genuinely finished (a completion marker returned) with a non-zero
// status. It only fires for finished commands so a still-running command's stale
// exit is never reported as a failure.
func terminalCommandFailure(sess *shellSession, state string) (int, bool) {
	snapshot := sess.stateSnapshot()
	if !snapshot.hasLastExit || state != "prompt_or_idle" {
		return 0, false
	}
	if snapshot.lastExit == 0 {
		return 0, false
	}
	return snapshot.lastExit, true
}

func terminalStateHint(state string) string {
	switch state {
	case "no_output_yet":
		return "no output yet. Call terminal_read ONCE with a short wait. If it stays empty and no shell prompt appears, the command is likely blocked waiting for input or a listener — do NOT keep polling; investigate instead."
	case "interactive_prompt":
		return "the command is waiting for input — answer it with terminal_send"
	case "prompt_or_idle":
		return "the previous command has FINISHED (a shell prompt is back). Proceed to the next step. Do NOT call terminal_read again to wait for this command."
	case "running":
		return "command is still running; call terminal_read to watch, or terminal_send {control:\"c\"} to interrupt"
	default:
		return "output is ready; proceed, or terminal_send if interaction is needed"
	}
}

func nextToolForTerminalState(state string) string {
	switch state {
	case "interactive_prompt":
		return "terminal_send"
	case "running", "no_output_yet":
		return "terminal_read"
	case "prompt_or_idle", "output_ready":
		return "proceed"
	default:
		return "terminal_read"
	}
}

// classifyTerminalState decides what the model should do next from a terminal
// snapshot. Order matters: a waiting input-prompt and a returned shell prompt
// (command finished) are detected BEFORE the "no new output" case, so a finished
// command is never mislabeled "still waiting" — the bug that made the model loop
// terminal_read on a command that had already completed.
func classifyTerminalState(command string, tail []string, beforeLen, afterLen int) string {
	joined := strings.TrimSpace(strings.Join(tail, "\n"))
	if terminalTailHasInteractivePrompt(tail) {
		return "interactive_prompt"
	}
	if endsWithShellPrompt(tail) {
		return "prompt_or_idle"
	}
	if afterLen <= beforeLen || joined == "" {
		return "no_output_yet"
	}
	if commandLooksLongRunning(command) {
		return "running"
	}
	return "output_ready"
}

func classifyTerminalStateForSession(sess *shellSession, command string, tail []string, beforeLen, afterLen int) string {
	// When this session emits shell-integration marks, the marker protocol is the
	// source of truth for running-vs-finished — no 30s expiry and no long-running
	// string guessing. Only trust it for the local shell or a remote session we have
	// injected markers into (remoteConnected without integration has no marks).
	state := sess.stateSnapshot()
	if state.sawPromptMarker && (!state.remoteConnected || state.remoteIntegrated) {
		if state.awaitingCommand && !state.promptReady {
			// A program can prompt for input without the shell prompt returning.
			if terminalTailHasInteractivePrompt(tail) {
				return "interactive_prompt"
			}
			return "running"
		}
		if state.promptReady {
			return "prompt_or_idle"
		}
	}
	if terminalSessionPromptReady(sess) {
		return "prompt_or_idle"
	}
	return classifyTerminalState(command, tail, beforeLen, afterLen)
}

func terminalSessionPromptReady(sess *shellSession) bool {
	state := sess.stateSnapshot()
	return state.promptReady && !state.lastDoneAt.IsZero() && time.Since(state.lastDoneAt) < 30*time.Second
}

func terminalExitLabel(sess *shellSession) string {
	state := sess.stateSnapshot()
	if !state.hasLastExit {
		return "unknown"
	}
	return fmt.Sprintf("%d", state.lastExit)
}

func terminalCWDLabel(sess *shellSession) string {
	state := sess.stateSnapshot()
	if strings.TrimSpace(state.lastCWD) == "" {
		return "unknown"
	}
	return state.lastCWD
}

func terminalTailHasInteractivePrompt(tail []string) bool {
	checked := 0
	for i := len(tail) - 1; i >= 0 && checked < 6; i-- {
		line := strings.TrimSpace(tail[i])
		if line == "" || tools.IsScanProgressLine(line) || isLowSignalArtLineLocal(line) {
			continue
		}
		checked++
		lower := strings.ToLower(line)
		if strings.Contains(lower, "type=\"password\"") || strings.Contains(lower, "type='password'") || strings.Contains(lower, "<input") {
			continue
		}
		if containsAny(lower, "password:", "passphrase", "[y/n]", "(y/n)", "are you sure", "continue?", "login:", "username:") {
			return true
		}
	}
	return false
}

// endsWithShellPrompt reports whether the last meaningful line of the snapshot is
// a shell prompt (ends in $ or #), i.e. the command has returned control. Skips
// blank and replacement-char banner-art lines so the prompt under a banner is
// still found.
func endsWithShellPrompt(tail []string) bool {
	for i := len(tail) - 1; i >= 0; i-- {
		line := strings.TrimRight(tail[i], " \t")
		if strings.TrimSpace(line) == "" || tools.IsScanProgressLine(line) {
			continue
		}
		if isLowSignalArtLineLocal(line) {
			continue
		}
		last := line[len(line)-1]
		return last == '$' || last == '#'
	}
	return false
}

// isLowSignalArtLineLocal mirrors the tools-package art heuristic for the prompt
// scan (a line that is overwhelmingly '?' replacement art carries no prompt).
func isLowSignalArtLineLocal(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 16 {
		return false
	}
	nonSpace, q := 0, 0
	for _, r := range t {
		if r == ' ' || r == '\t' {
			continue
		}
		nonSpace++
		if r == '?' {
			q++
		}
	}
	return nonSpace > 0 && q*100/nonSpace >= 80
}

func commandLooksLongRunning(command string) bool {
	lower := strings.ToLower(command)
	return containsAny(lower,
		"nmap ", "ffuf ", "gobuster ", "feroxbuster ", "wfuzz ", "hydra ", "sqlmap ",
		"nc ", "ncat ", "socat ", "msfconsole", "ssh ", "python ", "python3 ", "exploit",
		"reverse", "listener", "tail -f", "watch ")
}

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

var namedTerminalKeys = map[string]string{
	"up":        "\x1b[A",
	"down":      "\x1b[B",
	"right":     "\x1b[C",
	"left":      "\x1b[D",
	"home":      "\x1b[H",
	"end":       "\x1b[F",
	"pageup":    "\x1b[5~",
	"pagedown":  "\x1b[6~",
	"delete":    "\x1b[3~",
	"enter":     "\r",
	"return":    "\r",
	"backspace": "\x7f",
	"tab":       "\t",
	"esc":       "\x1b",
	"escape":    "\x1b",
}

// ---- terminal_send ----------------------------------------------------------

type terminalSendTool struct{ app *App }

type terminalSendArgs struct {
	Command     string   `json:"command"`
	Data        string   `json:"data"`
	Text        string   `json:"text"`
	Input       string   `json:"input"`
	Keys        string   `json:"keys"`
	Key         string   `json:"key"`
	KeySequence []string `json:"key_sequence"`
	ID          string   `json:"id"`
	Enter       *bool    `json:"enter"`
	Control     string   `json:"control"`
	WaitMs      *int     `json:"wait_ms"`
	WaitFor     string   `json:"wait_for"`
	Lines       int      `json:"lines"`
}

func (t *terminalSendTool) Name() string      { return "terminal_send" }
func (t *terminalSendTool) Destructive() bool { return true }

func (t *terminalSendTool) Description() string {
	return "Type keystrokes into the live shared terminal and return immediately (no waiting for a command to finish). " +
		"Use this — together with terminal_read — to drive interactive or long-running sessions you want to watch and steer in real time: " +
		"a reverse/ssh shell, msfconsole, a python REPL, a [y/N] prompt, or a live terminal job you may interrupt early. " +
		"Put a command in `command` only when that command belongs in the live terminal session. For independent HTTP/webshell/curl/wget checks, use http_probe (or shell when exact curl flags/pipelines are required) instead of typing them into a connected terminal. " +
		"For any other one-shot command whose full output you just want back in a single turn, use the `shell` tool instead. " +
		"Send a command line in `keys` (Enter is pressed by default). Set `control` to send a control key — c=Ctrl-C/interrupt, d=Ctrl-D, z, l, u, esc, tab. " +
		"After sending, this returns as soon as new output appears, or at wait_ms as a maximum (default 600); call terminal_read again to keep watching."
}

func (t *terminalSendTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "Command line to type and execute in the live shared terminal."},
    "keys": {"type": "string", "description": "Text/keystrokes to type into the live terminal, e.g. \"nmap -sV 10.10.10.5\". Newlines are sent as typed."},
    "key": {"type": "string", "description": "One named key to send: up, down, left, right, home, end, pageup, pagedown, delete, enter, backspace, tab, esc, ctrl-a..ctrl-z."},
    "key_sequence": {"type": "array", "description": "Interactive key/text sequence only, e.g. [\"/\", \"needle\", \"enter\"] for less search. Do not put a whole shell command here; use command or keys.", "items": {"type": "string"}},
    "enter": {"type": "boolean", "description": "Press Enter after the keys. Default true (skipped automatically if keys already ends with a newline, or when only a control key is sent)."},
    "control": {"type": "string", "description": "Send a single control key instead of/after the text: c (Ctrl-C / interrupt), d (Ctrl-D / EOF), z, l (clear), u (clear line), esc, tab."},
    "wait_ms": {"type": "integer", "description": "Maximum ms to wait for new output before returning the latest screen. Returns immediately once output appears. Default 600, max 10000."},
    "wait_for": {"type": "string", "enum": ["output", "prompt"], "description": "With command, output returns after first visible change (default). prompt waits for the prompt marker and exit/cwd when available."},
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
	normalizeTerminalSendArgs(&args)
	if strings.TrimSpace(args.Command) != "" {
		runArgs := terminalRunArgs{Command: args.Command, WaitMs: args.WaitMs, WaitFor: args.WaitFor, Lines: args.Lines}
		return (&terminalRunTool{app: t.app}).Run(ctx, compactAppArgs(runArgs))
	}
	// Un-HTML-escape operators in typed text (local models emit &amp;/&gt;), same as
	// the shell tool. Control keys are untouched.
	if args.Keys != "" {
		args.Keys = tools.NormalizeShellCommandText(args.Keys)
		if tools.HasResidualShellHTMLEntity(args.Keys) {
			return "", fmt.Errorf("terminal_send: keys still contain malformed HTML-escaped shell operators after decoding; retry with literal operators like &, >, <, |, and \"")
		}
	}
	for i, key := range args.KeySequence {
		if isTerminalControlCKey(key) || isNamedTerminalControlKey(key) {
			continue
		}
		decoded := tools.NormalizeShellCommandText(key)
		if tools.HasResidualShellHTMLEntity(decoded) {
			return "", fmt.Errorf("terminal_send: key_sequence[%d] still contains malformed HTML-escaped shell operators after decoding; retry with literal operators like &, >, <, |, and \"", i)
		}
		args.KeySequence[i] = decoded
	}
	if strings.TrimSpace(args.Keys) != "" && strings.TrimSpace(args.Control) == "" && len(args.KeySequence) == 0 {
		t.app.mu.Lock()
		toolCfg := t.app.cfg.Tools
		t.app.mu.Unlock()
		state := t.app.GetSharedTerminalState()
		if decision, routed := terminalWSLHostCommandDecision(toolCfg, state, args.Keys); routed {
			return formatToolExecutionBlock(decision, state), nil
		}
	}

	payload, err := buildTerminalSendPayload(args)
	if err != nil {
		return "", fmt.Errorf("terminal_send: %w", err)
	}
	if payload == "" {
		if args.Lines > 0 || args.WaitMs != nil {
			lines := args.Lines
			if lines <= 0 {
				lines = defaultTerminalReadLines
			}
			return "", fmt.Errorf("terminal_send: no keys/control were provided. You likely meant terminal_read with {\"lines\":%d,\"wait_ms\":%d}; call terminal_read instead of terminal_send to inspect the screen", lines, clampWaitMs(args.WaitMs).Milliseconds())
		}
		return "", fmt.Errorf("terminal_send: provide keys and/or control")
	}

	sess, err := t.app.ensureShellSession()
	if err != nil {
		return "", fmt.Errorf("terminal_send: %w", err)
	}
	state := sharedTerminalStateSnapshot(sess)
	t.app.updateAgentSessionsFromTerminalState(state)
	decision := adviseTerminalSend(state, args)
	if !decision.Allowed {
		return formatToolExecutionBlock(decision, state), nil
	}
	runID := sharedTerminalRunID()
	startedAt := time.Now()
	action := strings.TrimSpace(args.Keys)
	if action == "" && strings.TrimSpace(args.Control) != "" {
		action = "Ctrl-" + strings.ToUpper(strings.TrimSpace(args.Control))
	}
	if t.app.ctx != nil {
		t.app.emit("mauler:terminal_command_start", map[string]string{
			"id": runID, "session": sess.id, "command": action, "timeout": fmt.Sprintf("%.1f", clampWaitMs(args.WaitMs).Seconds()), "tool": "terminal_send",
		})
	}
	resetShellSessionPromptState(sess)
	beforeLen := 0
	if sess.scroll != nil {
		beforeLen = sess.scroll.length()
	}
	beforeGen := terminalScreenGeneration(sess)
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

	waitForTerminalOutput(ctx, sess, beforeLen, beforeGen, clampWaitMs(args.WaitMs))
	result := formatTerminalScreenForSession(sess, args.Lines)
	t.app.updateAgentSessionsFromTerminalState(sharedTerminalStateSnapshot(sess))
	if t.app.ctx != nil {
		t.app.emit("mauler:terminal_command_done", map[string]string{
			"id": runID, "session": sess.id, "exit_code": "sent", "duration_ms": fmt.Sprintf("%d", time.Since(startedAt).Milliseconds()), "tool": "terminal_send", "result": result,
		})
	}
	return result, nil
}

func compactAppArgs(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// buildTerminalSendPayload turns the tool args into the exact byte string to
// write to the PTY: the typed text, an optional Enter, and an optional control
// key. Pure (no I/O) so it is unit-testable without a live terminal.
func buildTerminalSendPayload(args terminalSendArgs) (string, error) {
	var sb strings.Builder
	sb.WriteString(args.Keys)
	if strings.TrimSpace(args.Key) != "" {
		seq, err := terminalKeyPayload(args.Key, false)
		if err != nil {
			return "", err
		}
		sb.WriteString(seq)
	}
	for _, key := range args.KeySequence {
		seq, err := terminalKeyPayload(key, true)
		if err != nil {
			return "", err
		}
		sb.WriteString(seq)
	}

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

func normalizeTerminalSendArgs(args *terminalSendArgs) {
	if args == nil {
		return
	}
	if strings.TrimSpace(args.Command) == "" {
		args.Command = firstNonEmptyTerminalArg(args.Data, args.Text, args.Input, terminalDataFromID(args.ID))
	}
	if strings.TrimSpace(args.Keys) != "" {
		candidate := strings.ToLower(strings.TrimSpace(args.Keys))
		if _, ok := namedTerminalKeys[candidate]; ok && !looksLikeTerminalCommandText(args.Keys) {
			args.Key = candidate
			args.Keys = ""
		}
		return
	}
	if strings.TrimSpace(args.Key) != "" || strings.TrimSpace(args.Control) != "" {
		return
	}
	if len(args.KeySequence) != 1 {
		return
	}
	candidate := strings.TrimSpace(args.KeySequence[0])
	if candidate == "" {
		return
	}
	if _, ok := namedTerminalKeys[strings.ToLower(candidate)]; ok {
		return
	}
	if strings.HasPrefix(strings.ToLower(candidate), "ctrl-") {
		return
	}
	if !looksLikeTerminalCommandText(candidate) {
		return
	}
	args.Keys = tools.NormalizeShellCommandText(candidate)
	args.KeySequence = nil
}

func firstNonEmptyTerminalArg(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func terminalDataFromID(id string) string {
	const marker = "<parameter=data>"
	idx := strings.Index(strings.ToLower(id), marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(id[idx+len(marker):])
}

func terminalSendHasInterrupt(args terminalSendArgs) bool {
	if isTerminalControlCControl(args.Control) || isTerminalControlCKey(args.Key) {
		return true
	}
	for _, key := range args.KeySequence {
		if isTerminalControlCKey(key) {
			return true
		}
	}
	return false
}

func terminalSendIsPureEnter(args terminalSendArgs) bool {
	if strings.TrimSpace(args.Keys) != "" || strings.TrimSpace(args.Control) != "" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(args.Key))
	if key == "enter" || key == "return" {
		return len(args.KeySequence) == 0
	}
	if len(args.KeySequence) != 1 {
		return false
	}
	seq := strings.ToLower(strings.TrimSpace(args.KeySequence[0]))
	return seq == "enter" || seq == "return"
}

func isTerminalControlCControl(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return lower == "c" || lower == "ctrl-c" || lower == "ctrl+c" || lower == "^c"
}

func isTerminalControlCKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return lower == "ctrl-c" || lower == "ctrl+c" || lower == "^c"
}

func isNamedTerminalControlKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return strings.HasPrefix(lower, "ctrl-") || strings.HasPrefix(lower, "ctrl+") || strings.HasPrefix(lower, "^")
}

func looksLikeTerminalCommandText(text string) bool {
	lower := strings.TrimSpace(strings.ToLower(text))
	if lower == "" || !strings.ContainsAny(lower, " \t") {
		return false
	}
	commandPrefixes := []string{
		"tmux ", "screen ", "bash ", "sh ", "zsh ", "fish ", "powershell", "pwsh ",
		"cmd ", "wsl ", "ssh ", "scp ", "nc ", "ncat ", "socat ", "python ", "python3 ",
		"perl ", "ruby ", "go ", "npm ", "node ", "nmap ", "curl ", "wget ", "ffuf ",
		"feroxbuster ", "gobuster ", "sqlmap ", "msfconsole ", "cd ", "ls ", "cat ",
		"grep ", "find ", "mkdir ", "touch ", "echo ", "printf ", "sudo ", "docker ",
	}
	for _, prefix := range commandPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.ContainsAny(lower, "/\\|;&>$<`")
}

func terminalKeyPayload(key string, allowLiteral bool) (string, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", nil
	}
	lower := strings.ToLower(trimmed)
	if seq, ok := namedTerminalKeys[lower]; ok {
		return seq, nil
	}
	if (strings.HasPrefix(lower, "ctrl-") || strings.HasPrefix(lower, "ctrl+")) && len([]rune(lower)) == len("ctrl-a") {
		r := []rune(lower)[5]
		if r >= 'a' && r <= 'z' {
			return string(byte(r) & 0x1f), nil
		}
	}
	// In key_sequence, plain strings are useful for things like less search:
	// ["/", "needle", "enter"]. A single unknown key should be called out.
	if allowLiteral || len([]rune(trimmed)) == 1 || strings.ContainsAny(trimmed, " \t/.-_:=") {
		return trimmed, nil
	}
	return "", fmt.Errorf("unknown key %q (use arrows/page keys/enter/backspace/tab/esc/ctrl-a..ctrl-z, or literal text in key_sequence)", key)
}

// ---- terminal_read ----------------------------------------------------------

type terminalReadTool struct{ app *App }

type terminalReadArgs struct {
	Lines      int    `json:"lines"`
	WaitMs     *int   `json:"wait_ms"`
	Mode       string `json:"mode"`
	Grep       string `json:"grep"`
	SaveResult bool   `json:"save_result"`
}

func (t *terminalReadTool) Name() string      { return "terminal_read" }
func (t *terminalReadTool) Destructive() bool { return false }

func (t *terminalReadTool) Description() string {
	return "Return the latest output of the live shared terminal right now, without sending anything — the agent's way to 'look at the screen'. " +
		"Call it repeatedly only while a command is still running or a prompt is waiting. Search history with grep instead of rerunning commands; mode=history without grep is not a raw scrollback dump. " +
		"By default this snapshots immediately. Set wait_ms only when waiting for long-running output; it returns sooner if new output appears."
}

func (t *terminalReadTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "lines": {"type": "integer", "description": "Trailing screen lines to return. Default 40, max 200."},
    "wait_ms": {"type": "integer", "description": "Maximum ms to wait for new output before snapshotting. Omit for instant read. Max 10000."},
    "mode": {"type": "string", "enum": ["screen", "history"], "description": "screen returns the current visible terminal snapshot (default). history is for grep/search; without grep it returns a short current-screen reminder, not full scrollback."},
    "grep": {"type": "string", "description": "Optional regex or literal substring to search in the selected terminal view/history. Use this to find evidence in prior output instead of rerunning a command."},
    "save_result": {"type": "boolean", "description": "When true, save the returned terminal view/search output as an offloaded tool result and include result_id for read_tool_result."}
  },
  "additionalProperties": false
}`)
}

func (t *terminalReadTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var args terminalReadArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("terminal_read: bad params: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(args.Mode))
	if mode == "" {
		mode = "screen"
	}
	if mode != "screen" && mode != "history" {
		return "", fmt.Errorf("terminal_read: invalid mode %q (want screen or history)", args.Mode)
	}
	grep := strings.TrimSpace(args.Grep)

	t.app.shellMu.Lock()
	sess := t.app.shellSess
	t.app.shellMu.Unlock()
	if sess == nil {
		return "No live terminal session yet. Use terminal_send (or the shell tool) to start one.", nil
	}

	beforeLen := 0
	if sess.scroll != nil {
		beforeLen = sess.scroll.length()
	}
	beforeGen := terminalScreenGeneration(sess)
	waitForTerminalOutput(ctx, sess, beforeLen, beforeGen, clampReadWaitMs(args.WaitMs))
	// Report completion state so a poll tells the model whether the command
	// finished (prompt back) instead of leaving it to guess and loop on waits.
	lines := args.Lines
	if lines <= 0 {
		lines = defaultTerminalReadLines
	}
	tail := cleanTerminalLines(sess.scroll.tail(lines))
	// Refresh nested-session state, then best-effort install remote markers so a
	// connected session's running/finished/exit state becomes authoritative too.
	sharedTerminalStateSnapshot(sess)
	maybeInjectRemoteShellIntegration(sess, tail)
	state := classifyTerminalStateForSession(sess, "", tail, 0, sess.scroll.length())
	view := formatTerminalView(sess, args.Lines, mode)
	if mode == "history" && grep == "" {
		view = formatTerminalHistoryNoGrepView(sess, args.Lines)
	}
	matchCount := 0
	if grep != "" {
		view, matchCount = formatTerminalSearchView(sess, args.Lines, mode, grep)
		args.SaveResult = true
	}
	handle := ""
	if args.SaveResult {
		if saved, err := t.app.saveToolResult(sharedTerminalRunID(), "terminal_read", view); err == nil {
			handle = saved
		}
	}
	result := fmt.Sprintf("[terminal_read state=%s mode=%s]\ncontract:\n  state: %s\n  mode: %s\n  grep: %s\n  matches: %d\n  result_id: %s\n  exit: %s\n  cwd: %s\n  next_tool: %s\n  evidence: %s\n  do_not_repeat: do not keep polling if state is prompt_or_idle or output_ready; search/read saved output before rerunning commands\nnext: %s\n%s",
		state, mode, state, mode, terminalReadGrepLabel(grep), matchCount, terminalReadHandleLabel(handle), terminalExitLabel(sess), terminalCWDLabel(sess), nextToolForTerminalState(state), truncateRunes(terminalStateHint(state), 220), terminalStateHint(state), view)
	if handle != "" {
		result += fmt.Sprintf("\n\n[terminal output saved: result_id=%s. Use read_tool_result with this result_id for larger slices; do not rerun the command just to inspect prior output.]", handle)
	}
	t.app.updateAgentSessionsFromTerminalState(sharedTerminalStateSnapshot(sess))
	if t.app.ctx != nil {
		t.app.emit("mauler:terminal_command_done", map[string]string{
			"id": sharedTerminalRunID(), "session": sess.id, "exit_code": state, "duration_ms": "0", "tool": "terminal_read", "result": result,
		})
	}
	return result, nil
}

func terminalReadGrepLabel(grep string) string {
	if strings.TrimSpace(grep) == "" {
		return "-"
	}
	return strconv.Quote(grep)
}

func terminalReadHandleLabel(handle string) string {
	if strings.TrimSpace(handle) == "" {
		return "-"
	}
	return handle
}

func formatTerminalView(sess *shellSession, lines int, mode string) string {
	if strings.EqualFold(mode, "history") {
		return formatTerminalHistory(sessScroll(sess), lines)
	}
	return formatTerminalScreenForSession(sess, lines)
}

func formatTerminalHistoryNoGrepView(sess *shellSession, lines int) string {
	if lines <= 0 || lines > 20 {
		lines = 20
	}
	screen := formatTerminalScreenForSession(sess, lines)
	return "[terminal history not dumped]\nmode=history is reserved for grep/search evidence. Use {\"mode\":\"history\",\"grep\":\"...\"} to find prior output, or use mode=screen to inspect the live terminal.\n" + screen
}

func formatTerminalSearchView(sess *shellSession, lines int, mode, grep string) (string, int) {
	if lines <= 0 {
		lines = defaultTerminalReadLines
	}
	if lines > maxTerminalReadLines {
		lines = maxTerminalReadLines
	}
	var matches []string
	if strings.EqualFold(mode, "screen") {
		matches = searchTerminalLines(renderedTerminalScreenLines(sess, maxTerminalReadLines), grep, lines)
	} else {
		matches = cleanTerminalLines(sessScroll(sess).search(grep, lines))
	}
	if len(matches) == 0 {
		return fmt.Sprintf("[terminal %s search - no matches for %q]", mode, grep), 0
	}
	return fmt.Sprintf("[terminal %s search - %d match(es) for %q]\n%s", mode, len(matches), grep, strings.Join(matches, "\n")), len(matches)
}

func renderedTerminalScreenLines(sess *shellSession, lines int) []string {
	if sess == nil || sess.screen == nil {
		return cleanTerminalLines(sessScroll(sess).tail(lines))
	}
	return cleanTerminalLines(sess.screen.render(lines))
}

func searchTerminalLines(lines []string, pattern string, limit int) []string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}
	if limit <= 0 || limit > maxTerminalReadLines {
		limit = maxTerminalReadLines
	}
	var out []string
	for i, line := range lines {
		matched, err := regexp.MatchString(pattern, line)
		if err != nil {
			matched = strings.Contains(strings.ToLower(line), strings.ToLower(pattern))
		}
		if !matched {
			continue
		}
		out = append(out, fmt.Sprintf("%d: %s", i+1, line))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func sessScroll(sess *shellSession) *terminalScrollback {
	if sess == nil {
		return nil
	}
	return sess.scroll
}

func formatTerminalHistory(scroll *terminalScrollback, lines int) string {
	if lines <= 0 {
		lines = defaultTerminalReadLines
	}
	if lines > maxTerminalReadLines {
		lines = maxTerminalReadLines
	}
	tail := cleanTerminalLines(scroll.tail(lines))
	if len(tail) == 0 {
		return "[terminal history empty]"
	}
	return fmt.Sprintf("[terminal history - last %d of %d line(s)]\n%s", len(tail), scroll.length(), strings.Join(tail, "\n"))
}

func formatTerminalScreenForSession(sess *shellSession, lines int) string {
	if sess != nil && sess.screen != nil {
		if lines <= 0 {
			lines = defaultTerminalReadLines
		}
		if lines > maxTerminalReadLines {
			lines = maxTerminalReadLines
		}
		rendered := cleanTerminalLines(sess.screen.render(lines))
		if len(rendered) > 0 {
			return fmt.Sprintf("[live terminal screen - %d rendered line(s)]\n%s", len(rendered), strings.Join(rendered, "\n"))
		}
	}
	return formatTerminalScreen(sessScroll(sess), lines)
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

func clampReadWaitMs(ms *int) time.Duration {
	if ms == nil {
		return terminalReadNowMs
	}
	return clampWaitMs(ms)
}

func formatTerminalScreen(scroll *terminalScrollback, lines int) string {
	if lines <= 0 {
		lines = defaultTerminalReadLines
	}
	if lines > maxTerminalReadLines {
		lines = maxTerminalReadLines
	}
	tail := cleanTerminalLines(scroll.tail(lines))
	if len(tail) == 0 {
		return "[terminal idle — no output captured yet. Call terminal_read again to keep polling, or terminal_send to type a command.]"
	}
	total := scroll.length()
	header := fmt.Sprintf("[live terminal — last %d of %d line(s)]", len(tail), total)
	return header + "\n" + strings.Join(tail, "\n")
}

// cleanTerminalLines collapses carriage-return progress and drops scan progress
// noise from a scrollback snapshot, so the interactive terminal tools surface
// findings (not ffuf/gobuster banners and ":: Progress:" spam) — the same
// hygiene the blocking shell path gets via tools.CleanCommandOutput.
func cleanTerminalLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	prev := ""
	for _, ln := range lines {
		ln = tools.CollapseLineCarriageReturns(ln)
		if tools.IsScanProgressLine(ln) {
			continue
		}
		if ln != "" && ln == prev {
			continue
		}
		out = append(out, ln)
		if strings.TrimSpace(ln) != "" {
			prev = ln
		}
	}
	return out
}
