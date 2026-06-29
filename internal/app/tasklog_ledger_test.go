package app

import (
	"path/filepath"
	"testing"

	"mauler/internal/ledger"
)

func TestTaskRunMirrorsCoreEventsToLedger(t *testing.T) {
	l := ledger.New(filepath.Join(t.TempDir(), "run-ledger.jsonl"))
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.attachLedger(l)

	run.addEvent("start", "Run started", "mode=Builder")
	run.setState("thinking", "requesting model output")
	run.addTool("shell", `{"command":"pwd"}`, "/tmp/work", "done", 42)
	run.stop("user_stop", "cancelled by user")
	run.finish("stopped", "Stopped by user")

	events, err := l.List(0)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d ledger events, want 5: %#v", len(events), events)
	}
	if events[0].Kind != "run_finish" || events[0].Status != "stopped" {
		t.Fatalf("newest event should be run_finish stopped: %#v", events[0])
	}
	if events[1].Kind != "run_stop" || events[1].Status != "user_stop" {
		t.Fatalf("second event should be run_stop user_stop: %#v", events[1])
	}

	var sawTool, sawState bool
	for _, event := range events {
		if event.RunID != run.ID {
			t.Fatalf("event missing run id %q: %#v", run.ID, event)
		}
		if event.Kind == "tool_result" && event.Tool == "shell" && event.DurationMs == 42 {
			sawTool = true
		}
		if event.Kind == "state" && event.State == "thinking" {
			sawState = true
		}
	}
	if !sawTool {
		t.Fatalf("missing mirrored shell tool event: %#v", events)
	}
	if !sawState {
		t.Fatalf("missing mirrored state event: %#v", events)
	}
}
