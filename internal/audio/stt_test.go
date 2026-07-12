package audio

import (
	"errors"
	"strings"
	"testing"
)

func TestIsSpeechRejected(t *testing.T) {
	for _, message := range []string{"recording is too short", "whisper worker failed: no speech detected"} {
		if !IsSpeechRejected(errors.New(message)) {
			t.Fatalf("expected %q to be a speech rejection", message)
		}
	}
	if IsSpeechRejected(errors.New("worker pipe closed")) {
		t.Fatal("worker infrastructure errors must remain eligible for CLI fallback")
	}
}

func TestWhisperWorkerScriptLoadsOnceAndRejectsSilence(t *testing.T) {
	for _, required := range []string{
		`model = whisper.load_model(MODEL_NAME, device="cpu")`,
		`"code": "too_short"`,
		`"code": "silence"`,
		`model.transcribe(samples`,
	} {
		if !strings.Contains(whisperWorkerScript, required) {
			t.Fatalf("worker script missing %q", required)
		}
	}
	if strings.Count(whisperWorkerScript, "whisper.load_model") != 1 {
		t.Fatal("Whisper model should be loaded exactly once outside the request loop")
	}
}
