package app

import (
	"testing"

	"mauler/internal/settings"
)

func TestSpecCalibrationKey_StableAndCtxAware(t *testing.T) {
	a := settings.Profile{ModelID: "Qwen3.6-27B-MTP-UD-Q4_K_XL", CtxTokens: 32768}
	b := settings.Profile{ModelID: "Qwen3.6-27B-MTP-UD-Q4_K_XL", CtxTokens: 32768}
	c := settings.Profile{ModelID: "Qwen3.6-27B-MTP-UD-Q4_K_XL", CtxTokens: 16384}
	if specCalibrationKey(a) != specCalibrationKey(b) {
		t.Fatalf("same model+ctx must share a key")
	}
	if specCalibrationKey(a) == specCalibrationKey(c) {
		t.Fatalf("different ctx must produce different keys")
	}
}

func TestPickBestSample_PicksFastestEnabled(t *testing.T) {
	samples := []SpecCalibrationSample{
		{N: 0, TokPerSec: 40}, // baseline ignored by picker
		{N: 1, TokPerSec: 55},
		{N: 2, TokPerSec: 70},
		{N: 3, TokPerSec: 64},
		{N: 4, Note: "failed"}, // no tok/s — skipped
	}
	best, ok := pickBestSample(samples)
	if !ok || best.N != 2 || best.TokPerSec != 70 {
		t.Fatalf("expected best n=2 @70, got %+v ok=%v", best, ok)
	}
	if got := baselineTokPerSec(samples); got != 40 {
		t.Fatalf("expected baseline 40, got %v", got)
	}
}

func TestPickBestSample_NoEnabledSamples(t *testing.T) {
	if _, ok := pickBestSample([]SpecCalibrationSample{{N: 0, TokPerSec: 40}}); ok {
		t.Fatalf("baseline-only set has no enabled best")
	}
	if _, ok := pickBestSample(nil); ok {
		t.Fatalf("empty set has no best")
	}
}
