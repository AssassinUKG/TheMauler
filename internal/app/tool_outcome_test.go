package app

import (
	"errors"
	"testing"
)

func TestClassifyToolExecutionOutcome(t *testing.T) {
	tests := []struct {
		name   string
		result string
		err    error
		status string
	}{
		{name: "done", result: "[shell_result state=done backend=wsl]\ncontract:\n  exit: 0", status: "done"},
		{name: "started", result: "[shell_result state=started backend=wsl]", status: "running"},
		{name: "shared started", result: "Started background job terminal-1: nmap -p- host\nPoll after 1s with {\"job\":\"terminal-1\"}", status: "running"},
		{name: "running", result: "[background job isolated-1: running, 3s elapsed]", status: "running"},
		{name: "state denied", result: "[tool_state_machine state=busy allowed=false recommended=terminal_read]", status: "blocked"},
		{name: "endpoint unverified", result: "No such file or directory\n[verifier_required:http_endpoint] verify it", status: "inconclusive"},
		{name: "go error", err: errors.New("boom"), status: "error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyToolExecutionOutcome(tc.result, tc.err)
			if got.Status != tc.status {
				t.Fatalf("status = %q, want %q: %#v", got.Status, tc.status, got)
			}
		})
	}
}
