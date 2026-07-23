package app

import (
	"encoding/base64"
	"testing"
)

func TestAudioUsesKokoroForAutoAndExplicitKokoro(t *testing.T) {
	for _, engine := range []string{"", "auto", "AUTO", "kokoro"} {
		if !audioUsesKokoro(engine) {
			t.Fatalf("audioUsesKokoro(%q) = false", engine)
		}
	}
	for _, engine := range []string{"piper", "disabled"} {
		if audioUsesKokoro(engine) {
			t.Fatalf("audioUsesKokoro(%q) = true", engine)
		}
	}
}

func TestDecodeAudioDataURI(t *testing.T) {
	payload := []byte("voice-data")
	mimeType, decoded, err := decodeAudioDataURI("data:audio/webm;base64," + base64.StdEncoding.EncodeToString(payload))
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "audio/webm" || string(decoded) != string(payload) {
		t.Fatalf("unexpected decode: mime=%q data=%q", mimeType, decoded)
	}
}

func TestDecodeAudioDataURIRejectsNonAudio(t *testing.T) {
	if _, _, err := decodeAudioDataURI("data:text/plain;base64,dm9pY2U="); err == nil {
		t.Fatal("expected non-audio data URI to be rejected")
	}
}

func TestAudioExtension(t *testing.T) {
	tests := map[string]string{
		"audio/ogg":  ".ogg",
		"audio/wav":  ".wav",
		"audio/mp4":  ".m4a",
		"audio/webm": ".webm",
	}
	for mimeType, want := range tests {
		if got := audioExtension(mimeType); got != want {
			t.Fatalf("audioExtension(%q)=%q, want %q", mimeType, got, want)
		}
	}
}

func TestAudioOverallDistinguishesCurrentFailureFromLastRequestFailure(t *testing.T) {
	if got := audioOverall(true, true, "ready", "", "error", false, "dependency conflict"); got != "error" {
		t.Fatalf("current worker error = %q, want error", got)
	}
	if got := audioOverall(true, true, "ready", "last playback failed", "ready", true, ""); got != "degraded" {
		t.Fatalf("recovered worker with prior request failure = %q, want degraded", got)
	}
	if got := audioOverall(true, true, "ready", "", "ready", true, ""); got != "ready" {
		t.Fatalf("ready workers = %q, want ready", got)
	}
}
