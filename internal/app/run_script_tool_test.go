package app

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunScriptCallsRegistryTools(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available on PATH")
	}
	tmp := t.TempDir()
	restoreWorkingDir(t)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.suppressEvents = true

	out, err := app.runPythonToolScript(context.Background(), `
write("out.txt", "hello from run_script\n")
print(read("out.txt"))
`, 15, 5)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"[run_script_result state=done]", "contract:", "inner_tool_calls: 2", "failed_tool: -", "next_tool: proceed", "hello from run_script"} {
		if !strings.Contains(out, want) {
			t.Fatalf("run_script output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "inner_tool_calls=2") || !strings.Contains(out, "hello from run_script") {
		t.Fatalf("unexpected run_script output:\n%s", out)
	}
	data, err := os.ReadFile("out.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello from run_script\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestRunScriptReportsFailedInnerTool(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available on PATH")
	}
	tmp := t.TempDir()
	restoreWorkingDir(t)
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	app := New()
	app.suppressEvents = true

	out, err := app.runPythonToolScript(context.Background(), `read("missing.txt")`, 15, 5)
	if err == nil {
		t.Fatalf("expected run_script failure, got output:\n%s", out)
	}
	for _, want := range []string{"[run_script_result state=error]", "failed_tool: read", "next_tool: inspect failed_tool/error", "missing.txt", "does not exist"} {
		if !strings.Contains(out, want) {
			t.Fatalf("run_script failure output missing %q:\n%s", want, out)
		}
	}
}
