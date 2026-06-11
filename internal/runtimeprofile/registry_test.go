package runtimeprofile

import (
	"testing"

	"mauler/internal/settings"
)

func TestMatchQwen36Profile(t *testing.T) {
	rp, ok := Match(settings.Profile{Name: "qwen3.6-think", ModelID: "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf"})
	if !ok {
		t.Fatalf("expected qwen runtime profile match")
	}
	if rp.Adapter != "qwen36" || !rp.Supports.Tools || !rp.Supports.Thinking || !rp.Supports.MTP {
		t.Fatalf("unexpected qwen runtime profile: %#v", rp)
	}
}

func TestMatchGemma4Profile(t *testing.T) {
	rp, ok := Match(settings.Profile{Name: "gemma4-tool-test", ModelID: "gemma-4-31B-it-uncensored-heretic-Q4_K_S.gguf"})
	if !ok {
		t.Fatalf("expected gemma runtime profile match")
	}
	if rp.Adapter != "gemma4" || rp.ToolProtocol != "repair-text" || rp.Supports.Thinking {
		t.Fatalf("unexpected gemma runtime profile: %#v", rp)
	}
}

func TestMatchGemma426BA4BQATProfilePrefersQATRuntime(t *testing.T) {
	rp, ok := Match(settings.Profile{Name: "gemma4-26b-a4b-qat", ModelID: "gemma-4-26B-A4B-it-QAT-Q4_0.gguf"})
	if !ok {
		t.Fatalf("expected gemma 4 26b-a4b QAT runtime profile match")
	}
	if rp.Name != "gemma4-26b-a4b-qat" || rp.Quant != "QAT-Q4_0" || rp.RecommendedCtx != 49152 || rp.Supports.MTP {
		t.Fatalf("unexpected gemma 4 26b-a4b QAT runtime profile: %#v", rp)
	}
	if rp.Defaults.Temperature != 1.0 || rp.Defaults.TopP != 0.95 || rp.Defaults.TopK != 64 || rp.Defaults.MinP != 0.05 || rp.Defaults.RepeatPenalty != 1.0 {
		t.Fatalf("gemma 4 26b-a4b QAT defaults = %#v, want live InferenceBridge temp/top_p/top_k/min_p/repeat_penalty", rp.Defaults)
	}
}

func TestMatchGemma426BA4BQATProfileFromHyphenatedModelID(t *testing.T) {
	rp, ok := Match(settings.Profile{Name: "local", ModelID: "google/gemma-4-26B-A4B-it-qat-q4_0-gguf"})
	if !ok {
		t.Fatalf("expected hyphenated gemma 4 26b-a4b QAT runtime profile match")
	}
	if rp.Name != "gemma4-26b-a4b-qat" {
		t.Fatalf("matched %q, want gemma4-26b-a4b-qat", rp.Name)
	}
}

func TestLooksMTPModelRequiresMTPMarker(t *testing.T) {
	if LooksMTPModel(settings.Profile{Name: "qwen3.6-think", ModelID: "Qwen3.6-27B-UD-Q4_K_XL.gguf"}) {
		t.Fatalf("plain qwen profile should not look like an MTP artifact")
	}
	if !LooksMTPModel(settings.Profile{Name: "qwen3.6-mtp", ModelID: "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf"}) {
		t.Fatalf("MTP model id should be detected")
	}
}
