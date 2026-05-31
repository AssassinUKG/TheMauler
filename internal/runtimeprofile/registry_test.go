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

func TestLooksMTPModelRequiresMTPMarker(t *testing.T) {
	if LooksMTPModel(settings.Profile{Name: "qwen3.6-think", ModelID: "Qwen3.6-27B-UD-Q4_K_XL.gguf"}) {
		t.Fatalf("plain qwen profile should not look like an MTP artifact")
	}
	if !LooksMTPModel(settings.Profile{Name: "qwen3.6-mtp", ModelID: "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf"}) {
		t.Fatalf("MTP model id should be detected")
	}
}
