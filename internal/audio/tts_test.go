package audio

import (
	"errors"
	"strings"
	"testing"
)

func TestRestartWorkerClearsLatchedErrorAndReportsStopped(t *testing.T) {
	recordSynthesis(Audio{}, errors.New("old kokoro failure"))
	if status := Status(); status.LastError == "" {
		t.Fatal("test setup did not latch a synthesis error")
	}
	RestartWorker()
	status := Status()
	if status.LastError != "" {
		t.Fatalf("restart retained stale error %q", status.LastError)
	}
	if status.WorkerState != "stopped" || status.WorkerPID != 0 {
		t.Fatalf("restart status = %+v", status)
	}
}

func TestKokoroWorkerProtocolIncludesWarmReadyHandshake(t *testing.T) {
	for _, want := range []string{`get_pipeline(warm_voice)`, `"ready": True`} {
		if !strings.Contains(kokoroWorkerScript, want) {
			t.Fatalf("worker script is missing startup handshake %q", want)
		}
	}
}
