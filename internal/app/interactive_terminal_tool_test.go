package app

import (
	"strings"
	"testing"
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
		{name: "unknown control errors", args: terminalSendArgs{Control: "x"}, wantErr: true},
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
}

func TestFormatTerminalScreenEmpty(t *testing.T) {
	got := formatTerminalScreen(newTerminalScrollback(0), 10)
	if !strings.Contains(got, "terminal idle") {
		t.Fatalf("empty screen should report idle, got %q", got)
	}
}
