package app

import (
	"bytes"
	"strings"
	"testing"

	"mauler/internal/llm"
)

// --- Item 1: deterministic error detection off the OSC-133 exit code ---------

func TestTerminalCommandFailureOnlyOnFinishedNonZero(t *testing.T) {
	one := 1
	zero := 0
	cases := []struct {
		name     string
		exit     *int
		state    string
		wantExit int
		wantFail bool
	}{
		{"finished non-zero fails", &one, "prompt_or_idle", 1, true},
		{"finished zero passes", &zero, "prompt_or_idle", 0, false},
		{"running non-zero not reported", &one, "running", 0, false},
		{"no exit code", nil, "prompt_or_idle", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := &shellSession{lastExit: tc.exit}
			exit, failed := terminalCommandFailure(sess, tc.state)
			if failed != tc.wantFail || exit != tc.wantExit {
				t.Fatalf("terminalCommandFailure = (%d,%v), want (%d,%v)", exit, failed, tc.wantExit, tc.wantFail)
			}
		})
	}
}

func TestFormatTerminalRunResultReportsFailure(t *testing.T) {
	sess := &shellSession{id: "s1", scroll: newTerminalScrollback(10), screen: newTerminalScreen(80, 5)}
	sess.scroll.append("curl: (7) Failed to connect")
	one := 1
	sess.sawPromptMarker = true
	sess.promptReady = true
	sess.awaitingCommand = false
	sess.lastExit = &one
	out := formatTerminalRunResult(sess, "curl -s http://x", 0, 20)
	if !strings.Contains(out, "command_failed: exit=1") {
		t.Fatalf("expected command_failed field, got:\n%s", out)
	}
}

func TestCriticalVerifierHintFlagsCommandExit(t *testing.T) {
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "terminal_send"}}
	hint := criticalVerifierHint(tc, "[terminal_send session=s1 state=prompt_or_idle]\n  command_failed: exit=2\n")
	if !strings.Contains(hint, "verifier_required:command_exit") {
		t.Fatalf("expected command_exit verifier hint, got %q", hint)
	}
}

// --- Item 2: shell-integration marks are authoritative for run/finish --------

func TestClassifyTerminalStateTrustsMarks(t *testing.T) {
	// Command in flight with marks: authoritative "running", no long-running guess.
	running := &shellSession{sawPromptMarker: true, awaitingCommand: true}
	if got := classifyTerminalStateForSession(running, "sleep 1", nil, 0, 0); got != "running" {
		t.Fatalf("in-flight marked command: got %q, want running", got)
	}
	// Completion marker returned: authoritative "prompt_or_idle".
	done := &shellSession{sawPromptMarker: true, promptReady: true}
	if got := classifyTerminalStateForSession(done, "ls", nil, 0, 0); got != "prompt_or_idle" {
		t.Fatalf("finished marked command: got %q, want prompt_or_idle", got)
	}
	// Remote-connected without injected markers: no marks to trust, fall back to
	// the tail heuristic (empty tail with no growth => no_output_yet).
	remote := &shellSession{sawPromptMarker: true, awaitingCommand: true, remoteConnected: true}
	if got := classifyTerminalStateForSession(remote, "id", nil, 0, 0); got == "running" {
		t.Fatalf("un-integrated remote must not be trusted as authoritative running")
	}
}

// --- Item 3: OSC marker protocol + remote propagation ------------------------

func TestOSCLocalDoneClearsRemoteSession(t *testing.T) {
	sess := &shellSession{remoteConnected: true, remoteIntegrated: true, awaitingCommand: true}
	applyShellSessionOSCPayload(sess, "D;0")
	if sess.remoteConnected || sess.remoteIntegrated {
		t.Fatal("local done marker must clear remote-session flags (back at local prompt)")
	}
	if !sess.promptReady || sess.awaitingCommand || !sess.sawPromptMarker {
		t.Fatal("local done marker must mark finished + integration-capable")
	}
	if sess.lastExit == nil || *sess.lastExit != 0 {
		t.Fatal("local done marker must record exit code")
	}
}

func TestOSCRemoteDoneKeepsSessionConnected(t *testing.T) {
	sess := &shellSession{remoteConnected: true, awaitingCommand: true}
	applyShellSessionOSCPayload(sess, "R;7")
	if !sess.remoteConnected {
		t.Fatal("remote done marker must NOT clear the connected flag")
	}
	if !sess.remoteIntegrated {
		t.Fatal("remote done marker implies the remote is integrated")
	}
	if sess.awaitingCommand || sess.lastExit == nil || *sess.lastExit != 7 {
		t.Fatal("remote done marker must finish the command and record exit")
	}
	// An integrated remote's marks are now authoritative for classification.
	if got := classifyTerminalStateForSession(sess, "id", nil, 0, 0); got != "prompt_or_idle" {
		t.Fatalf("integrated remote finished: got %q, want prompt_or_idle", got)
	}
}

func TestOSCRemoteCwd(t *testing.T) {
	sess := &shellSession{}
	applyShellSessionOSCPayload(sess, "Q;cwd=/root/loot")
	if sess.lastCWD != "/root/loot" {
		t.Fatalf("remote cwd marker: got %q", sess.lastCWD)
	}
}

func TestResetPromptStateMarksAwaiting(t *testing.T) {
	one := 1
	sess := &shellSession{promptReady: true, lastExit: &one}
	resetShellSessionPromptState(sess)
	if sess.promptReady || sess.lastExit != nil || !sess.awaitingCommand {
		t.Fatal("reset must clear prompt/exit and mark a command in flight")
	}
}

func TestMaybeInjectRemoteShellIntegration(t *testing.T) {
	prompt := []string{"root@target:/tmp#"}

	// No-op when not connected.
	buf := &bytes.Buffer{}
	s := &shellSession{input: buf}
	maybeInjectRemoteShellIntegration(s, prompt)
	if buf.Len() != 0 || s.remoteIntegrated {
		t.Fatal("must not inject when not remote-connected")
	}

	// No-op when connected but no shell prompt visible.
	buf.Reset()
	s = &shellSession{input: buf, remoteConnected: true}
	maybeInjectRemoteShellIntegration(s, []string{"streaming output..."})
	if buf.Len() != 0 || s.remoteIntegrated {
		t.Fatal("must not inject without a visible shell prompt")
	}

	// Fires once when connected + prompt visible.
	buf.Reset()
	s = &shellSession{input: buf, remoteConnected: true}
	maybeInjectRemoteShellIntegration(s, prompt)
	if !s.remoteIntegrated || !strings.Contains(buf.String(), "133;R;") {
		t.Fatalf("expected remote marker hook injection, got %q", buf.String())
	}

	// No re-inject once integrated.
	buf.Reset()
	maybeInjectRemoteShellIntegration(s, prompt)
	if buf.Len() != 0 {
		t.Fatal("must not re-inject when already integrated")
	}
}
