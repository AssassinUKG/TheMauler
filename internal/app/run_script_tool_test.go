package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/controlplane"
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

func TestRunScriptInnerWriteUsesFinalizedArtifactScope(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("python not available on PATH")
	}
	root := t.TempDir()
	restoreWorkingDir(t)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "report.md")
	if err := os.WriteFile(path, []byte("validated\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	contract, err := controlplane.NewTaskContract(controlplane.ContractInput{
		RunID: "current", Objective: "change report", WorkspaceRoot: root,
		ProtectedArtifacts: []controlplane.ArtifactBoundary{{Path: path, SHA256: strings.Repeat("a", 64), SourceRunID: "prior", Generation: 5}},
		Risk:               controlplane.RiskMedium, InstructionRevision: 1, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	run := &TaskRun{ID: "current", Mode: "Builder", Contract: &contract, Control: &controlplane.MachineState{Version: 1, ContractDigest: contract.Digest, ContractRevision: 1, Phase: controlplane.PhaseActing, Revision: 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	app := New()
	app.suppressEvents = true
	ctx := withRunControlContext(context.Background(), run)
	out, err := app.runPythonToolScript(ctx, `write("report.md", "silently replaced\n")`, 15, 5)
	if err == nil || !strings.Contains(out, "explicit Fixer") {
		t.Fatalf("run_script bypass was not blocked: err=%v\n%s", err, out)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != "validated\n" {
		t.Fatalf("finalized bytes changed: read_err=%v content=%q", readErr, string(data))
	}
}
