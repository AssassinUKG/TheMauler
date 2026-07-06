package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func TestCriticalVerifierHints(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		args   string
		result string
		want   string
	}{
		{
			name:   "webshell url needs id pwd",
			tool:   "shell",
			result: "webshell: https://connected.htb/shell.php?cmd=whoami",
			want:   "verifier_required:webshell",
		},
		{
			name:   "uid needs context verification",
			tool:   "terminal_read",
			result: "uid=999(asterisk) gid=1000(asterisk)",
			want:   "verifier_required:shell_user",
		},
		{
			name:   "root needs root verification",
			tool:   "terminal_read",
			result: "uid=0(root) gid=0(root)",
			want:   "verifier_required:root",
		},
		{
			name:   "route failure needs connectivity verification",
			tool:   "shell",
			args:   `{"command":"curl http://connected.htb"}`,
			result: "curl: (7) Failed to connect to connected.htb port 80",
			want:   "verifier_required:target_route",
		},
		{
			name:   "listener needs read verification",
			tool:   "terminal_read",
			result: "contract:\n  state: listening\nListener started",
			want:   "verifier_required:listener",
		},
		{
			name:   "connected session needs live verification",
			tool:   "terminal_read",
			result: "[terminal_read state=connected]\nShared terminal appears connected live session",
			want:   "verifier_required:session",
		},
		{
			name:   "file write needs verification",
			tool:   "write",
			args:   `{"path":"C:/tmp/report.md"}`,
			result: "wrote C:/tmp/report.md",
			want:   "verifier_required:file",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := appendCriticalVerifierHint(llm.ToolCallDef{
				Function: llm.FunctionCall{
					Name:      tc.tool,
					Arguments: json.RawMessage(tc.args),
				},
			}, tc.result)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("missing verifier hint %q:\n%s", tc.want, got)
			}
			if strings.Count(got, "[verifier_required:") != 1 {
				t.Fatalf("expected one verifier hint:\n%s", got)
			}
		})
	}
}

func TestCriticalVerifierHintIgnoresNonExecutionTools(t *testing.T) {
	got := appendCriticalVerifierHint(llm.ToolCallDef{
		Function: llm.FunctionCall{Name: "read"},
	}, "uid=999(asterisk)")
	if strings.Contains(got, "verifier_required") {
		t.Fatalf("read-only tool should not get execution verifier hint:\n%s", got)
	}
}

func TestBuildPendingVerifierPrompt(t *testing.T) {
	prompt := buildPendingVerifierPrompt([]llm.Message{
		llm.NewTextMessage(llm.RoleTool, "ok\n[verifier_required:webshell] verify it"),
		llm.NewTextMessage(llm.RoleTool, "ok\n[verifier_required:root] verify it"),
		llm.NewTextMessage(llm.RoleTool, "duplicate\n[verifier_required:webshell] verify it"),
	})
	for _, want := range []string{"Proper verifier required", "webshell", "root", "evidence-backed verifier", "capture command plus result evidence"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "for: webshell, root.") {
		t.Fatalf("webshell should be deduped in verifier list:\n%s", prompt)
	}
}

func TestEvidencePinSkipsToolStateMachineBlocks(t *testing.T) {
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "terminal_send"}}
	result := "[tool_state_machine state=busy allowed=false recommended=terminal_read]\nuid=0(root) gid=0(root)\n[verifier_required:root] Before reporting root/success, verify with `id; hostname; pwd`."
	if !shouldSkipEvidencePin(tc, result) {
		t.Fatal("state-machine control output should not be pinned as target evidence")
	}
	if pins := extractEvidencePins(result, 6); len(pins) == 0 {
		t.Fatal("test fixture should contain extractable evidence to prove the skip guard matters")
	}
}
