package app

import (
	"strings"
	"testing"
)

func TestSessionServiceHealthDoesNotCallReadyTerminalAnActiveListener(t *testing.T) {
	health := sessionServiceHealth([]AgentSession{{
		ID: "terminal:1", Kind: "terminal", State: "ready", TerminalSession: "shell-1",
	}}, "now")
	if health.Status != "idle" {
		t.Fatalf("ready terminal status = %q, want idle", health.Status)
	}
	for _, want := range []string{"0 listeners", "0 shells", "1 idle"} {
		if !strings.Contains(health.Summary, want) {
			t.Fatalf("summary %q does not contain %q", health.Summary, want)
		}
	}
	if got := health.Metadata["session_1"]; got != "shared terminal · ready (idle)" {
		t.Fatalf("idle session detail = %q", got)
	}
}

func TestSessionServiceHealthShowsListenerAddressAndConnectedIdentity(t *testing.T) {
	health := sessionServiceHealth([]AgentSession{
		{ID: "listener:4444", Kind: "listener", State: "listening", Lhost: "10.10.14.2", Port: 4444},
		{ID: "shell:1", Kind: "shell", State: "connected", User: "root", Hostname: "target"},
	}, "now")
	if health.Status != "active" || !strings.Contains(health.Summary, "1 listener") || !strings.Contains(health.Summary, "1 shell") {
		t.Fatalf("active session health = %+v", health)
	}
	if got := health.Metadata["session_1"]; got != "listener · 10.10.14.2:4444 · listening" {
		t.Fatalf("listener detail = %q", got)
	}
	if got := health.Metadata["session_2"]; got != "shell · root@target · connected" {
		t.Fatalf("shell detail = %q", got)
	}
}

func TestAudioServiceHealthNamesTheFailingWorker(t *testing.T) {
	health := audioServiceHealth(AudioHealth{
		Overall: "error", ActualTTS: "kokoro", Voice: "af_heart", WorkerState: "ready", WorkerPID: 10,
		STTEngine: "whisper", STTReady: false, STTWorkerState: "error", STTModel: "tiny.en",
		STTLastError: "Numba needs NumPy 2.3 or less",
	}, "now")
	if !strings.Contains(health.Summary, "TTS kokoro (ready)") || !strings.Contains(health.Summary, "STT whisper (error)") {
		t.Fatalf("audio summary = %q", health.Summary)
	}
	if !strings.Contains(health.Detail, "STT: Numba needs NumPy") {
		t.Fatalf("audio detail = %q", health.Detail)
	}
}
