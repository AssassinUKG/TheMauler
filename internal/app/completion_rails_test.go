package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestExtractGoalFeaturesDropsStopwords(t *testing.T) {
	got := extractGoalFeatures("Please add a min and max helper to the package.")
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "please") || strings.Contains(joined, "the") ||
		strings.Contains(joined, "package") || strings.Contains(joined, "helper") {
		t.Fatalf("features kept stopwords: %#v", got)
	}
	for _, want := range []string{"min", "max"} {
		if !containsFeature(got, want) {
			t.Fatalf("features missing %q: %#v", want, got)
		}
	}
}

func TestExtractGoalFeaturesIgnoresExecutionInstructions(t *testing.T) {
	got := extractGoalFeatures("Add a min and a max helper to mathutil.go using the edit tool, not shell redirection. Then finish only after both helpers are present and the Go project builds.")
	for _, unwanted := range []string{"using", "edit", "tool", "shell", "redirection", "finish", "both", "present", "project", "helper"} {
		if containsFeature(got, unwanted) {
			t.Fatalf("instruction word %q was treated as a requested feature: %#v", unwanted, got)
		}
	}
	for _, want := range []string{"min", "max"} {
		if !containsFeature(got, want) {
			t.Fatalf("features missing %q: %#v", want, got)
		}
	}
}

func TestCompletionRailsSpecCoveragePasses(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.WriteFile("math.go", []byte("package math\n// helpers\nfunc min(){}\nfunc max(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	run := startTaskRun("add min and max helpers", "Builder", "profile", "model")
	run.Summary = "Added min and max helpers."
	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"math.go"}`, Result: "updated min and max helpers"})

	verdicts := runCompletionRails(&run, &cfg, nil)

	if len(verdicts) == 0 {
		t.Fatal("expected completion verdicts")
	}
	for _, verdict := range verdicts {
		if verdict.Status != "pass" {
			t.Fatalf("verdict = %#v, want pass", verdict)
		}
	}
}

func TestCompletionRailsSpecCoverageFails(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("add min and max helpers", "Builder", "profile", "model")
	run.Summary = "Added min helper."
	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"math.go"}`, Result: "updated min helper"})

	verdicts := runCompletionRails(&run, &cfg, nil)

	var found bool
	for _, verdict := range verdicts {
		if verdict.Status == "fail" {
			found = true
			if !verdict.Blocking || !strings.Contains(strings.Join(verdict.Improvements, "\n"), "max") {
				t.Fatalf("failure verdict not useful/blocking: %#v", verdict)
			}
		}
	}
	if !found {
		t.Fatalf("expected coverage failure, got %#v", verdicts)
	}
}

func TestCompletionRailsDeliverableExistsPass(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.WriteFile("report.md", []byte("report"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	run := startTaskRun("write a report.md", "Builder", "profile", "model")
	run.Tools = append(run.Tools, TaskToolEvent{Name: "write", Status: "done", Input: `{"path":"report.md"}`, Result: "wrote report.md"})

	verdicts := runCompletionRails(&run, &cfg, nil)

	if len(verdicts) == 0 {
		t.Fatal("expected verdicts")
	}
	for _, verdict := range verdicts {
		if verdict.Status != "pass" {
			t.Fatalf("verdict = %#v, want pass", verdict)
		}
	}
}

func TestCompletionRailsRejectsModelSummaryAsCoverageEvidence(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("add min and max helpers", "Builder", "profile", "model")
	run.Summary = "Added min and max helpers and verified everything."
	run.Response = "Both requested helpers are complete."

	verdicts := runCompletionRails(&run, &cfg, nil)

	var coverage VerifyVerdict
	for _, verdict := range verdicts {
		if verdict.Gate == "completion" && strings.Contains(verdict.Summary, "uncovered") {
			coverage = verdict
			break
		}
	}
	if coverage.Status != "fail" || !coverage.Blocking || !strings.Contains(coverage.Evidence, "min: missing") || !strings.Contains(coverage.Evidence, "max: missing") {
		t.Fatalf("summary claims must not satisfy coverage: %#v", verdicts)
	}
}

func TestCompletionRailsRecordsIndependentEvidencePerFeature(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.WriteFile("math.go", []byte("package math\n// helpers\nfunc min(){}\nfunc max(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	run := startTaskRun("add min and max helpers", "Builder", "profile", "model")
	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"math.go"}`, Result: "model says done"})

	verdicts := runCompletionRails(&run, &cfg, nil)

	if len(verdicts) == 0 || !strings.Contains(verdicts[0].Evidence, "min: file math.go") || !strings.Contains(verdicts[0].Evidence, "max: file math.go") {
		t.Fatalf("expected per-feature file evidence, got %#v", verdicts)
	}
}

func TestCompletionRailsDeliverableExistsFail(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("write a report.md", "Builder", "profile", "model")
	run.Summary = "Discussed the report."

	verdicts := runCompletionRails(&run, &cfg, nil)

	var found bool
	for _, verdict := range verdicts {
		if strings.Contains(verdict.Summary, "Deliverable rail failed") {
			found = true
			if !verdict.Blocking {
				t.Fatalf("deliverable failure should block in blocking mode: %#v", verdict)
			}
		}
	}
	if !found {
		t.Fatalf("expected deliverable failure, got %#v", verdicts)
	}
}

func TestCompletionRailsAdvisoryDoesNotBlock(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = false
	run := startTaskRun("write a report.md", "Builder", "profile", "model")

	verdicts := runCompletionRails(&run, &cfg, nil)

	for _, verdict := range verdicts {
		if verdict.Status == "fail" && verdict.Blocking {
			t.Fatalf("advisory mode should not block: %#v", verdict)
		}
	}
}

func TestCompletionRailsReadsTouchedFileEvidence(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.WriteFile("math.go", []byte("package math\n// helpers\nfunc min(){}\nfunc max(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	run := startTaskRun("add min and max helpers", "Builder", "profile", "model")
	run.Tools = append(run.Tools, TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"math.go"}`, Result: "updated file"})

	verdicts := runCompletionRails(&run, &cfg, nil)

	for _, verdict := range verdicts {
		if verdict.Status != "pass" {
			t.Fatalf("file evidence should satisfy coverage: %#v", verdict)
		}
	}
}

func TestCompletionRailsSuccessfulBuildCoversCompileFixSemantics(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.WriteFile("main.go", []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("Fix the compile error in main.go. Use tools, then verify the file is fixed.", "Fixer", "profile", "model")
	run.Tools = append(run.Tools,
		TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"main.go"}`, Result: "edited main.go"},
		TaskToolEvent{Name: "shell", Status: "done", Input: `{"command":"go build ./..."}`, Result: "[shell_result state=done exit=0]"},
	)

	verdicts := runCompletionRails(&run, &cfg, nil)
	for _, verdict := range verdicts {
		if verdict.Gate != "completion" || !strings.Contains(verdict.Summary, "requested features") {
			continue
		}
		if verdict.Status != "pass" || verdict.Blocking {
			t.Fatalf("successful build should cover compile-fix semantics: %#v", verdict)
		}
		for _, want := range []string{
			"compile: successful verification command",
			"error: successful verification command",
			"fixed: successful verification command",
		} {
			if !strings.Contains(verdict.Evidence, want) {
				t.Fatalf("completion evidence missing %q: %s", want, verdict.Evidence)
			}
		}
		return
	}
	t.Fatalf("completion coverage verdict missing: %#v", verdicts)
}

func TestCompletionRailsFailedBuildDoesNotCoverCompileFixSemantics(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("Fix the compile error and verify it is fixed.", "Fixer", "profile", "model")
	run.Tools = append(run.Tools, TaskToolEvent{
		Name: "shell", Status: "error", Input: `{"command":"go build ./..."}`, Result: "exit status 1",
	})

	verdicts := runCompletionRails(&run, &cfg, nil)
	for _, verdict := range verdicts {
		if verdict.Gate == "completion" && strings.Contains(verdict.Summary, "uncovered") {
			if !verdict.Blocking || !strings.Contains(verdict.Evidence, "compile: missing") || !strings.Contains(verdict.Evidence, "fixed: missing") {
				t.Fatalf("failed build must remain blocking: %#v", verdict)
			}
			return
		}
	}
	t.Fatalf("expected blocking coverage verdict, got %#v", verdicts)
}

func TestCompletionRailsMaskedVerificationCannotCertifyCompileFix(t *testing.T) {
	for _, command := range []string{"go build ./... || true", "go vet ./main.go; echo PASS", "echo 'compile error fixed go build'", "go build ./... | tee build.log"} {
		t.Run(command, func(t *testing.T) {
			cfg := settings.DefaultSettings()
			cfg.Agents.ReviewLoop.CompletionBlocking = true
			run := startTaskRun("Fix the compile error and verify it is fixed.", "Fixer", "profile", "model")
			input, _ := json.Marshal(map[string]string{"command": command})
			run.Tools = append(run.Tools, TaskToolEvent{Name: "shell", Status: "done", Input: string(input), Result: "compile error fixed BUILD_OK"})
			verdicts := runCompletionRails(&run, &cfg, nil)
			if len(verdicts) == 0 || !verdicts[0].Blocking || !strings.Contains(verdicts[0].Evidence, "compile: missing") {
				t.Fatalf("masked check certified completion: %#v", verdicts)
			}
		})
	}
}

func TestCompletionRailsSuccessfulChunkedWriteActionsCoverProcedure(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.MkdirAll("generated", 0o755); err != nil {
		t.Fatal(err)
	}
	var content strings.Builder
	for i := 1; i <= 150; i++ {
		content.WriteString("Line ")
		content.WriteString(fmt.Sprint(i))
		content.WriteByte('\n')
	}
	if err := os.WriteFile("generated/long.txt", []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("Create generated/long.txt containing the lines Line 1 through Line 150, one numbered line per line. First create a short plan with todo_write. Use the model-facing write tool for the first chunk and write with append=true for later chunks; do not overwrite an earlier chunk. Verify that Line 150 is present before finishing.", "Builder", "profile", "model")
	run.Tools = append(run.Tools,
		TaskToolEvent{Name: "todo_write", Status: "done", Input: `{"action":"replace","items":["write first chunk","append second chunk","verify"]}`},
		TaskToolEvent{Name: "write", Status: "done", Input: `{"path":"generated/long.txt","content":"Line 1"}`},
		TaskToolEvent{Name: "write", Status: "done", Input: `{"path":"generated/long.txt","content":"Line 2","append":true}`},
		TaskToolEvent{Name: "shell", Status: "done", Input: `{"command":"wc -l generated/long.txt && tail -n 3 generated/long.txt && head -n 1 generated/long.txt"}`, Result: "150 generated/long.txt\nLine 148\nLine 149\nLine 150\nLine 1"},
	)

	verdicts := runCompletionRails(&run, &cfg, nil)
	for _, verdict := range verdicts {
		if verdict.Gate != "completion" || !strings.Contains(verdict.Summary, "requested features") {
			continue
		}
		if verdict.Status != "pass" || verdict.Blocking {
			t.Fatalf("successful chunked-write actions should cover procedure: %#v", verdict)
		}
		for _, want := range []string{
			"first: successful todo_write action",
			"short: successful todo_write action",
			"plan: successful todo_write action",
			"todo_write: successful todo_write action",
			"model-facing: successful write action",
			"chunk: successful write action",
			"append: successful write action",
			"overwrite: successful write action",
			"verify: successful verification command",
		} {
			if !strings.Contains(verdict.Evidence, want) {
				t.Fatalf("completion evidence missing %q: %s", want, verdict.Evidence)
			}
		}
		return
	}
	t.Fatalf("completion coverage verdict missing: %#v", verdicts)
}

func TestCompletionRailsSuccessfulGrepThenEditActionsCoverProcedure(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.MkdirAll("src", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("src/task.txt", []byte("status = DONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("Find the TODO marker and replace it with DONE. Use grep first, then edit the definition.", "Builder", "profile", "model")
	run.Tools = append(run.Tools,
		TaskToolEvent{Name: "todo_write", Status: "done", Input: `{"action":"replace","items":["grep","edit","verify"]}`},
		TaskToolEvent{Name: "grep", Status: "done", Input: `{"path":"src/task.txt","pattern":"TODO"}`, Result: "src/task.txt:1: status = TODO"},
		TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"src/task.txt","old":"status = TODO","new":"status = DONE"}`},
	)

	verdicts := runCompletionRails(&run, &cfg, nil)
	for _, verdict := range verdicts {
		if verdict.Gate != "completion" || !strings.Contains(verdict.Summary, "requested features") {
			continue
		}
		if verdict.Status != "pass" || verdict.Blocking {
			t.Fatalf("successful grep-then-edit actions should cover procedure: %#v", verdict)
		}
		for _, want := range []string{
			"find: successful grep action",
			"todo: successful grep action",
			"marker: successful grep action",
			"replace: successful edit action",
			"grep: successful grep action",
			"first: successful grep action",
			"definition: successful edit action",
		} {
			if !strings.Contains(verdict.Evidence, want) {
				t.Fatalf("completion evidence missing %q: %s", want, verdict.Evidence)
			}
		}
		return
	}
	t.Fatalf("completion coverage verdict missing: %#v", verdicts)
}

func TestCompletionRailsProceduralEvidenceRemainsFailClosed(t *testing.T) {
	withTempWorkingDir(t)
	if err := os.MkdirAll("src", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("src/task.txt", []byte("status = DONE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("Find the TODO marker and replace it with DONE. Use grep first, then edit the definition.", "Builder", "profile", "model")
	run.Tools = append(run.Tools,
		TaskToolEvent{Name: "edit", Status: "done", Input: `{"path":"src/task.txt","old":"status = TODO","new":"status = DONE"}`},
		TaskToolEvent{Name: "grep", Status: "error", Input: `{"path":"src/task.txt","pattern":"TODO"}`, Result: "no matches"},
	)

	verdicts := runCompletionRails(&run, &cfg, nil)
	for _, verdict := range verdicts {
		if verdict.Gate == "completion" && strings.Contains(verdict.Summary, "uncovered") {
			if !verdict.Blocking || !strings.Contains(verdict.Evidence, "grep: missing") || !strings.Contains(verdict.Evidence, "first: missing") {
				t.Fatalf("failed or out-of-order tools must not satisfy procedure: %#v", verdict)
			}
			return
		}
	}
	t.Fatalf("expected blocking procedural coverage verdict, got %#v", verdicts)
}

func containsFeature(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
