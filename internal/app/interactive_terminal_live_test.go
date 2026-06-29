package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"mauler/internal/settings"
)

// TestInteractiveTerminalLive drives terminal_send / terminal_read against a REAL
// WSL bash -i session. It is gated behind MAULER_LIVE_TERMINAL_TEST=1 so it never
// runs in the normal suite (it spawns an actual shell). Run with:
//
//	MAULER_LIVE_TERMINAL_TEST=1 go test ./internal/app -run TestInteractiveTerminalLive -v
func TestInteractiveTerminalLive(t *testing.T) {
	if os.Getenv("MAULER_LIVE_TERMINAL_TEST") != "1" {
		t.Skip("set MAULER_LIVE_TERMINAL_TEST=1 to run the live WSL terminal test")
	}

	app := &App{cfg: &settings.Settings{}}
	app.cfg.Tools.ShellBackend = "wsl"
	app.cfg.Tools.ShellDistro = "kali-linux"

	send := &terminalSendTool{app: app}
	read := &terminalReadTool{app: app}
	ctx := context.Background()

	t.Cleanup(func() {
		app.shellMu.Lock()
		sess := app.shellSess
		app.shellMu.Unlock()
		if sess != nil {
			sess.cancel()
		}
	})

	sendCmd := func(t *testing.T, args string) string {
		t.Helper()
		out, err := send.Run(ctx, json.RawMessage(args))
		if err != nil {
			t.Fatalf("terminal_send(%s) error: %v", args, err)
		}
		t.Logf("send %s ->\n%s", args, out)
		return out
	}
	readScreen := func(t *testing.T, args string) string {
		t.Helper()
		out, err := read.Run(ctx, json.RawMessage(args))
		if err != nil {
			t.Fatalf("terminal_read(%s) error: %v", args, err)
		}
		t.Logf("read %s ->\n%s", args, out)
		return out
	}

	// 1. Capture: a one-shot command's output lands in the live screen snapshot.
	//    Poll to absorb cold WSL boot latency on the very first command.
	sendCmd(t, `{"keys":"echo MARKER_$((40+2))","wait_ms":2000,"lines":50}`)
	out := ""
	for i := 0; i < 12; i++ {
		out = readScreen(t, `{"lines":50,"wait_ms":1000}`)
		if strings.Contains(out, "MARKER_42") {
			break
		}
	}
	if !strings.Contains(out, "MARKER_42") {
		t.Fatalf("expected MARKER_42 in captured output, got:\n%s", out)
	}

	// 2. Non-blocking partial reads: a 5-tick loop (1s apart) must NOT block the
	//    send call to completion — an early read sees a first tick, not the last.
	sendCmd(t, `{"keys":"for i in 1 2 3 4 5; do echo TICK$i; sleep 1; done","wait_ms":1500}`)
	partial := readScreen(t, `{"lines":50}`)
	if !strings.Contains(partial, "TICK1") {
		t.Fatalf("expected an early TICK1 to stream in, got:\n%s", partial)
	}
	if strings.Contains(partial, "TICK5") {
		t.Fatalf("send/read blocked to completion (saw TICK5 too early):\n%s", partial)
	}
	// Let it finish so the loop doesn't bleed into the next step.
	final := readScreen(t, `{"lines":50,"wait_ms":5000}`)
	if !strings.Contains(final, "TICK5") {
		t.Fatalf("expected TICK5 after waiting for the loop to finish, got:\n%s", final)
	}

	// 3. Ctrl-C recovery: interrupt a long sleep, then prove the SAME session is
	//    still alive and responsive by running another command in it.
	sendCmd(t, `{"keys":"sleep 30","wait_ms":600}`)
	sendCmd(t, `{"control":"c","wait_ms":600}`)
	time.Sleep(300 * time.Millisecond)
	recovered := sendCmd(t, `{"keys":"echo AFTER_INTERRUPT_OK","wait_ms":1500,"lines":50}`)
	if !strings.Contains(recovered, "AFTER_INTERRUPT_OK") {
		t.Fatalf("session did not survive Ctrl-C / stayed wedged, got:\n%s", recovered)
	}
}
