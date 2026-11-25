package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mauler/internal/controlplane"
	"mauler/internal/settings"
)

func TestExactLiteralFileContractUsesOriginalBytesAndNativeWitness(t *testing.T) {
	for _, appendMode := range []bool{false, true} {
		root := t.TempDir()
		t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
		prefix, suffix := "original café prefix\n", "requested λ suffix\n"
		path := filepath.Join(root, "artifact.txt")
		prompt := "Create artifact.txt containing exactly the text represented by this JSON string: " + strconv.Quote(suffix)
		expected := suffix
		if appendMode {
			if err := os.WriteFile(path, []byte(prefix), 0600); err != nil {
				t.Fatal(err)
			}
			prompt = "Append exactly the text represented by this JSON string to artifact.txt, preserving every existing byte: " + strconv.Quote(suffix)
			expected = prefix + suffix
		}
		cfg := settings.DefaultSettings()
		cfg.Context.WorkspaceDir = root
		run := startTaskRun(prompt, "Builder", "local", "model")
		app := &App{}
		if err := app.initializeRunControlPlane(&run, &cfg, baseMode("Builder"), true); err != nil {
			t.Fatal(err)
		}
		if err := app.applyControlEvent(&run, controlplane.Event{Kind: controlplane.EventPlanAccepted, Detail: "fixture admitted plan"}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("tampered"), 0600); err != nil {
			t.Fatal(err)
		}
		wrongID, _ := mutationEvidenceIDForPath(path)
		run.addEvent("control_evidence", wrongID, path)
		if err := app.requestControlVerification(&run); err != nil {
			t.Fatal(err)
		}
		if err := app.passControlVerification(&run, nil); err == nil {
			t.Fatal("wrong bytes acquired terminal authority")
		}
		if err := os.WriteFile(path, []byte(expected), 0600); err != nil {
			t.Fatal(err)
		}
		if err := app.passControlVerification(&run, nil); err == nil {
			t.Fatal("unwitnessed matching bytes acquired terminal authority")
		}
		id, _ := mutationEvidenceIDForPath(path)
		run.addEvent("control_evidence", id, path)
		original := run.Contract.AcceptanceChecks[len(run.Contract.AcceptanceChecks)-1].ExactFile.SHA256
		run.Contract.AcceptanceChecks[len(run.Contract.AcceptanceChecks)-1].ExactFile.SHA256 = strings.Repeat("0", 64)
		if app.passControlVerification(&run, nil) == nil {
			t.Fatal("changed sealed expectation acquired completion authority")
		}
		run.Contract.AcceptanceChecks[len(run.Contract.AcceptanceChecks)-1].ExactFile.SHA256 = original
		verdicts, exclusive := exactFileCompletionVerdicts(run)
		if !exclusive || len(verdicts) != 1 || len(blockingReviewVerdicts(verdicts)) != 0 {
			t.Fatalf("exact current-byte proof rejected: %#v", verdicts)
		}
		if err := app.passControlVerification(&run, verdicts); err != nil {
			t.Fatal(err)
		}
		if run.Control.Phase != controlplane.PhaseComplete {
			t.Fatal("verified file was not complete")
		}
		sealed := run.Contract.AcceptanceChecks[len(run.Contract.AcceptanceChecks)-1].ExactFile
		if sealed == nil || sealed.Size != int64(len(expected)) {
			t.Fatal("exact expectation did not capture original prefix")
		}
		sealed.SHA256 = "00"
		if run.Contract.Validate() == nil {
			t.Fatal("changed expectation did not revoke sealed contract")
		}
	}
}

func TestExactLiteralFileRejectsMalformedScopeAndKeepsMixedGoalRails(t *testing.T) {
	root := t.TempDir()
	for _, prompt := range []string{
		"Create ../outside.txt containing exactly the text represented by this JSON string: \"text\"",
		"Create artifact.txt containing exactly the text represented by this JSON string: malformed",
	} {
		if _, err := compileExactFileExpectation(prompt, root); err == nil {
			t.Errorf("invalid literal contract accepted: %q", prompt)
		}
	}
	exact, err := compileExactFileExpectation("Create artifact.txt containing exactly the text represented by this JSON string: \"text\". Also implement a login API.", root)
	if err != nil || exact == nil || exact.Exclusive {
		t.Fatal("mixed goal bypassed ordinary semantic checks")
	}
	if exact, err := compileExactFileExpectationForAction("Create artifact.txt containing exactly the text represented by this JSON string: \"text\"", root, false); err != nil || exact != nil {
		t.Fatal("no-tool contract gained file requirements")
	}
}

func TestExhaustedBudgetCannotSkipBlockingCompletionRails(t *testing.T) {
	withTempWorkingDir(t)
	cfg := settings.DefaultSettings()
	cfg.Agents.ReviewLoop.VerifyGate = false
	cfg.Agents.ReviewLoop.CompletionRails = true
	cfg.Agents.ReviewLoop.CompletionBlocking = true
	cfg.Agents.ReviewLoop.ReviewerPass = false
	run := startTaskRun("Create a missing-artifact feature", "Builder", "local", "model")
	cycles := 0
	decision := (&App{}).runReviewPhase(context.Background(), &run, settings.Profile{}, &cfg, baseMode("Builder"), true, &cycles, reviewPhaseLimits{budgetsExhausted: true})
	if decision.StopReason != "review_incomplete" || len(blockingReviewVerdicts(decision.Verdicts)) == 0 {
		t.Fatalf("budget exhaustion erased required evidence: %#v", decision)
	}
}
