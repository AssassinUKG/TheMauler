package app

import (
	"encoding/json"
	"strings"
	"testing"

	"mauler/internal/llm"
)

func shellTC(cmd string) llm.ToolCallDef {
	args, _ := json.Marshal(map[string]string{"command": cmd})
	return llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: args}}
}

func shellToolEvent(cmd string) TaskToolEvent {
	args, _ := json.Marshal(map[string]string{"command": cmd})
	return TaskToolEvent{Name: "shell", Input: string(args), Status: "done"}
}

func TestShellCommandFamilyGroupsByEndpoint(t *testing.T) {
	a := shellCommandFamily(`curl -s "http://connected.htb/admin/ajax.php?x=1' AND EXTRACTVALUE(1,1)"`)
	b := shellCommandFamily(`curl -s "http://connected.htb/admin/ajax.php?x=2' AND SUBSTRING(pw,5,9)"`)
	if a == "" || a != b {
		t.Fatalf("same endpoint different payload should share family: %q vs %q", a, b)
	}
	if got := shellCommandFamily("ls -la /tmp"); got != "" {
		t.Fatalf("non-URL command should have no family, got %q", got)
	}
	c := shellCommandFamily(`curl http://other.htb/login`)
	if c == a {
		t.Fatal("different endpoints must not share a family")
	}
}

func TestShellCommandStormHintFiresAtThreshold(t *testing.T) {
	run := startTaskRun("x", "Ops", "default", "model")
	cmd := func(i int) string {
		return "curl -s \"http://connected.htb/admin/ajax.php?p=" + strings.Repeat("a", i) + "\""
	}
	// Below threshold (current call would be the 9th): no hint.
	for i := 0; i < shellStormThreshold-2; i++ {
		run.Tools = append(run.Tools, shellToolEvent(cmd(i)))
	}
	if h := shellCommandStormHint(run, shellTC(cmd(99))); h != "" {
		t.Fatalf("hint fired below threshold: %q", h)
	}
	// One more prior makes the current call the threshold-th: hint fires.
	run.Tools = append(run.Tools, shellToolEvent(cmd(100)))
	h := shellCommandStormHint(run, shellTC(cmd(200)))
	if h == "" || !strings.Contains(h, "similar") {
		t.Fatalf("expected storm hint at threshold, got %q", h)
	}
	if !strings.Contains(h, "save it once with curl") || !strings.Contains(h, "inspect the saved file locally") {
		t.Fatalf("storm hint should steer curl loops to saved artifacts, got %q", h)
	}
}

func TestCurlFilterRecoveryHintSavesResponseBeforeInspecting(t *testing.T) {
	h := commandSpecificRecoveryHint(`curl -sk "http://connected.htb/admin/config.php?display=epm_advanced" 2>&1 | grep -iE "fwbrand|endpoint"`)
	for _, want := range []string{"curl capture hint", "save the full HTTP response to a file", "inspect the saved file"} {
		if !strings.Contains(h, want) {
			t.Fatalf("curl recovery hint missing %q: %q", want, h)
		}
	}
}

func TestMissingPathShellRecoveryAnchorsWorkspace(t *testing.T) {
	result := appendShellCommandRecoveryHints("ls: cannot access '/root/HTB_writeups/scripts/': No such file or directory\n[wsl exit 2, 100ms]", "ls /root/HTB_writeups/scripts/")
	for _, want := range []string{"Workspace path hint", "Do not retry guessed absolute paths", "find . -maxdepth 3"} {
		if !strings.Contains(result, want) {
			t.Fatalf("missing path recovery hint lacks %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, "Use /root/HTB_writeups") {
		t.Fatalf("hint should not reinforce the bad /root path:\n%s", result)
	}
}

func TestShellCommandStormHintPointsAtWrittenScript(t *testing.T) {
	run := startTaskRun("x", "Ops", "default", "model")
	wargs, _ := json.Marshal(map[string]string{"path": "/tmp/scripts/freepbx_cve.py", "content": "..."})
	run.Tools = append(run.Tools, TaskToolEvent{Name: "write", Input: string(wargs), Status: "done"})
	for i := 0; i < shellStormThreshold-1; i++ {
		run.Tools = append(run.Tools, shellToolEvent("curl -s http://connected.htb/admin/ajax.php?p="+strings.Repeat("a", i)))
	}
	h := shellCommandStormHint(run, shellTC("curl -s http://connected.htb/admin/ajax.php?p=z"))
	if !strings.Contains(h, "freepbx_cve.py") {
		t.Fatalf("storm hint should point at the written script, got %q", h)
	}
}

func TestLargestShellFamilyCounts(t *testing.T) {
	run := startTaskRun("x", "Ops", "default", "model")
	for i := 0; i < 12; i++ {
		run.Tools = append(run.Tools, shellToolEvent("curl http://t.htb/a?x="+strings.Repeat("b", i)))
	}
	run.Tools = append(run.Tools, shellToolEvent("ls -la"))
	fam, n := largestShellFamily(&run)
	if n != 12 || !strings.Contains(fam, "t.htb/a") {
		t.Fatalf("largestShellFamily = %q,%d; want 12 against t.htb/a", fam, n)
	}
}
