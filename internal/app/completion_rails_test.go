package app

import (
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

	verdicts := runCompletionRails(&run, &cfg)

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

	verdicts := runCompletionRails(&run, &cfg)

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

	verdicts := runCompletionRails(&run, &cfg)

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

	verdicts := runCompletionRails(&run, &cfg)

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

	verdicts := runCompletionRails(&run, &cfg)

	if len(verdicts) == 0 || !strings.Contains(verdicts[0].Evidence, "min: file math.go") || !strings.Contains(verdicts[0].Evidence, "max: file math.go") {
		t.Fatalf("expected per-feature file evidence, got %#v", verdicts)
	}
}

func TestCompletionRailsDeliverableExistsFail(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	run := startTaskRun("write a report.md", "Builder", "profile", "model")
	run.Summary = "Discussed the report."

	verdicts := runCompletionRails(&run, &cfg)

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

	verdicts := runCompletionRails(&run, &cfg)

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

	verdicts := runCompletionRails(&run, &cfg)

	for _, verdict := range verdicts {
		if verdict.Status != "pass" {
			t.Fatalf("file evidence should satisfy coverage: %#v", verdict)
		}
	}
}

func containsFeature(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
