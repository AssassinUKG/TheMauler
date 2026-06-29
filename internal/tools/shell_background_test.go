package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunShellBackgroundDispatch(t *testing.T) {
	if _, handled, _ := runShellBackground(shellParams{Command: "echo hi"}); handled {
		t.Fatal("plain command should not be handled by background dispatcher")
	}
	out, handled, err := runShellBackground(shellParams{Job: "nope-does-not-exist"})
	if !handled {
		t.Fatal("job poll should be handled by background dispatcher")
	}
	if err == nil || !strings.Contains(err.Error(), "no background job") {
		t.Fatalf("unknown job poll = %q, err %v; want a no-such-job error", out, err)
	}
}

func TestBackgroundJobPollIntervalSchedule(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second, 8 * time.Second, 13 * time.Second, 13 * time.Second, 13 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := backgroundJobPollInterval(i); got != w {
			t.Errorf("interval(%d) = %s, want %s", i, got, w)
		}
	}
}

func TestTailFileTruncatesAndKeepsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := tailFile(path, 12000); !strings.Contains(got, "line3") || !strings.Contains(got, "line1") {
		t.Fatalf("full tail = %q, want all lines", got)
	}
	// A small cap slices into the stream; the partial first line is dropped and a
	// truncation marker is prepended.
	got := tailFile(path, 6)
	if strings.Contains(got, "line1") {
		t.Fatalf("capped tail %q should not contain the first line", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("capped tail %q should announce truncation", got)
	}
}

func TestExitCodeFromWaitErr(t *testing.T) {
	if got := exitCodeFromWaitErr(nil); got != 0 {
		t.Fatalf("nil err exit code = %d, want 0", got)
	}
}

// TestBackgroundJobLifecycle launches a real detached command and polls it to
// completion. echo and `exit N` are valid in both bash and PowerShell, so this
// works regardless of the resolved backend.
func TestBackgroundJobLifecycle(t *testing.T) {
	start, _, err := runShellBackground(shellParams{Command: "echo backgroundmarker", Background: true})
	if err != nil {
		t.Fatalf("start background job: %v", err)
	}
	id := extractJobID(t, start)

	text := pollUntilDone(t, id, 15*time.Second)
	if !strings.Contains(text, "backgroundmarker") {
		t.Fatalf("job output %q missing expected marker", text)
	}
	if !strings.Contains(text, "exit 0") {
		t.Fatalf("job output %q missing exit 0", text)
	}
	if _, _, err := runShellBackground(shellParams{Job: id}); err == nil {
		t.Fatal("a finished job should be cleared and error on re-poll")
	}
}

func TestBackgroundJobObserverFires(t *testing.T) {
	var mu sync.Mutex
	var states []string
	OnBackgroundJobUpdate = func(u BackgroundJobUpdate) {
		mu.Lock()
		states = append(states, u.State)
		mu.Unlock()
	}
	t.Cleanup(func() { OnBackgroundJobUpdate = nil })

	start, _, err := runShellBackground(shellParams{Command: "echo observed", Background: true})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := extractJobID(t, start)
	pollUntilDone(t, id, 15*time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(states) == 0 || states[0] != "started" {
		t.Fatalf("expected first observer state 'started', got %v", states)
	}
	sawDone := false
	for _, s := range states {
		if s == "done" {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatalf("observer never saw a done state: %v", states)
	}
}

func TestBackgroundJobCapturesNonZeroExit(t *testing.T) {
	start, _, err := runShellBackground(shellParams{Command: "exit 7", Background: true})
	if err != nil {
		t.Fatalf("start background job: %v", err)
	}
	id := extractJobID(t, start)
	text := pollUntilDone(t, id, 15*time.Second)
	if !strings.Contains(text, "exit 7") {
		t.Fatalf("job output %q should report exit 7", text)
	}
}

func TestReapFinishedJobsClearsDoneAndLog(t *testing.T) {
	start, _, err := runShellBackground(shellParams{Command: "echo reapme", Background: true})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	id := extractJobID(t, start)

	// Wait for the process to actually exit (poll once past the backoff window).
	pollUntilDone(t, id, 15*time.Second)
	// pollUntilDone already cleared it on the done poll, so start a fresh one that
	// finishes but is never polled, to exercise reap directly.
	start2, _, err := runShellBackground(shellParams{Command: "echo reapme2", Background: true})
	if err != nil {
		t.Fatalf("start2: %v", err)
	}
	id2 := extractJobID(t, start2)
	// Give it time to exit without polling it.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		bgJobs.mu.Lock()
		job := bgJobs.jobs[id2]
		bgJobs.mu.Unlock()
		if job == nil {
			break
		}
		job.mu.Lock()
		done := job.done
		logPath := job.logPath
		job.mu.Unlock()
		if done {
			if reaped := reapFinishedShellJobs(); reaped < 1 {
				t.Fatalf("reapFinishedShellJobs returned %d, want >=1", reaped)
			}
			bgJobs.mu.Lock()
			_, stillTracked := bgJobs.jobs[id2]
			bgJobs.mu.Unlock()
			if stillTracked {
				t.Fatal("reaped job still tracked")
			}
			if _, err := os.Stat(logPath); !os.IsNotExist(err) {
				t.Fatalf("reaped job logfile still present: %v", err)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("job never reached done state to reap")
}

func TestSweepStaleJobLogsRemovesOrphans(t *testing.T) {
	orphan := filepath.Join(os.TempDir(), "mauler_job_zz999.log")
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if SweepStaleJobLogs() < 1 {
		t.Fatal("sweep should have removed at least the orphan log")
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan logfile not removed: %v", err)
	}
}

func extractJobID(t *testing.T, start string) string {
	t.Helper()
	const marker = "Started background job "
	idx := strings.Index(start, marker)
	if idx < 0 {
		t.Fatalf("no job id in start output: %q", start)
	}
	rest := start[idx+len(marker):]
	end := strings.IndexAny(rest, " \n")
	if end < 0 {
		t.Fatalf("malformed job id in: %q", start)
	}
	return rest[:end]
}

func pollUntilDone(t *testing.T, id string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, _, err := runShellBackground(shellParams{Job: id})
		if err != nil {
			t.Fatalf("poll job %s: %v", id, err)
		}
		if strings.Contains(out, "too early") {
			time.Sleep(700 * time.Millisecond)
			continue
		}
		if strings.Contains(out, ": done") {
			return out
		}
		time.Sleep(700 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish within %s", id, timeout)
	return ""
}
