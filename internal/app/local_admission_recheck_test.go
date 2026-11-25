package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// A guard-only audit of retained model results. Does not replay actions,
// publish completions, construct a provider, or make network/model calls.
func TestRecheckRetainedLocalCompletionAdmissions(t *testing.T) {
	path := os.Getenv("MAULER_LOCAL_RECHECK_REPORT")
	output := os.Getenv("MAULER_LOCAL_RECHECK_OUTPUT")
	if path == "" || output == "" {
		t.Skip("explicit retained report/audit output required")
	}
	if !filepath.IsAbs(path) || !filepath.IsAbs(output) {
		t.Fatal("explicit absolute paths required")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var report struct {
		Cases []struct {
			Run      TaskRun
			Followup *TaskRun
			Recovery *TaskRun
		}
	}
	if err := json.NewDecoder(io.LimitReader(file, 32*1024*1024)).Decode(&report); err != nil {
		t.Fatal(err)
	}
	admitted, exact := 0, 0
	for _, row := range report.Cases {
		runs := []TaskRun{row.Run}
		if row.Followup != nil {
			runs = append(runs, *row.Followup)
		}
		if row.Recovery != nil {
			runs = append(runs, *row.Recovery)
		}
		for _, run := range runs {
			if err := validateCompletionContract(&run); err != nil {
				t.Fatalf("retained contract rejected %s: %v", run.ID, err)
			}
			admitted++
			if run.Status == "done" {
				verdicts, _ := exactFileCompletionVerdicts(run)
				if len(blockingReviewVerdicts(verdicts)) > 0 {
					t.Fatalf("retained exact-byte result is stale: %s", run.ID)
				}
				if len(verdicts) > 0 {
					exact++
				}
			}
		}
	}
	if len(report.Cases) != 65 || admitted != 80 || exact != 10 {
		t.Fatalf("unexpected qualification coverage: %d cases, %d admissions, %d exact files", len(report.Cases), admitted, exact)
	}
	data, _ := json.MarshalIndent(map[string]any{"schemaVersion": "mauler-local-admission-audit-v1", "sourceReport": path, "cases": len(report.Cases), "admittedSnapshots": admitted, "exactArtifactsCurrent": exact, "modelCalls": 0, "effectsReplayed": 0, "pass": true}, "", "  ")
	handle, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := handle.Write(data)
	closeErr := handle.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(fmt.Errorf("write audit: %v %v", writeErr, closeErr))
	}
	t.Logf("Retained local evidence: %d current-source admissions, %d current exact artifacts, zero model calls/effect replay.", admitted, exact)
}
