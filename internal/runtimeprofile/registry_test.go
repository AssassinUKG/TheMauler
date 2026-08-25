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

func TestMatchQwen38OfficialAndUncensoredProfiles(t *testing.T) {
	for _, modelID := range []string{
		"Qwen3.8-27B-Q4_K_M.gguf",
		"Qwen3.8-27B-Uncensored-Q4_K_M.gguf",
	} {
		rp, ok := Match(settings.Profile{Name: "local-import", ModelID: modelID})
		if !ok {
			t.Fatalf("expected Qwen3.8 runtime profile match for %q", modelID)
		}
		if rp.Name != "qwen3.8-27b" || rp.Adapter != "qwen38" || rp.ToolProtocol != "native-openai" {
			t.Fatalf("unexpected Qwen3.8 runtime profile: %#v", rp)
		}
		if !rp.Supports.Tools || !rp.Supports.Thinking || !rp.Supports.MTP || !rp.RequiresJinja {
			t.Fatalf("missing Qwen3.8 capability metadata: %#v", rp)
		}
		if rp.Defaults.Temperature != 0.7 || rp.Defaults.TopP != 0.8 || rp.Defaults.TopK != 20 || rp.Defaults.PresencePenalty != 1.5 || rp.Defaults.RepeatPenalty != 1.0 {
			t.Fatalf("Qwen3.8 no-thinking defaults = %#v", rp.Defaults)
		}
		if rp.RecommendedCtx != 35000 || rp.KVTypeK != "f16" || rp.KVTypeV != "f16" {
			t.Fatalf("Qwen3.8 local runtime defaults = %#v", rp)
		}
	}
}

func TestMatchInstalledQwen36VariantTemplates(t *testing.T) {
	tests := []struct {
		modelID      string
		wantName     string
		wantRepo     string
		wantQuant    string
		wantCtx      int
		wantProtocol string
	}{
		{
			modelID:      "Qwen3.6-27B-UD-Q4_K_XL.gguf",
			wantName:     "qwen3.6-27b-unsloth-ud-q4-k-xl",
			wantRepo:     "unsloth/Qwen3.6-27B-GGUF",
			wantQuant:    "UD-Q4_K_XL",
			wantCtx:      35000,
			wantProtocol: "native-openai",
		},
		{
			modelID:      "Huihui-Qwen3.6-27B-abliterated-ggml-model-Q4_K.gguf",
			wantName:     "qwen3.6-27b-huihui-abliterated-mtp",
			wantRepo:     "huihui-ai/Huihui-Qwen3.6-27B-abliterated-MTP-GGUF",
			wantQuant:    "Q4_K",
			wantCtx:      35000,
			wantProtocol: "native-openai",
		},
		{
			modelID:      "Qwen3.6-27B-Uncensored-HauhauCS-Aggressive-Q4_K_P.gguf",
			wantName:     "qwen3.6-27b-hauhaucs-aggressive",
			wantRepo:     "HauhauCS/Qwen3.6-27B-Uncensored-HauhauCS-Aggressive",
			wantQuant:    "Q4_K_P",
			wantCtx:      40000,
			wantProtocol: "native-openai",
		},
		{
			modelID:      "Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_M.gguf",
			wantName:     "qwen3.6-35b-a3b-heretic-native-mtp",
			wantRepo:     "llmfan46/Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved-GGUF",
			wantQuant:    "Q4_K_M",
			wantCtx:      16384,
			wantProtocol: "repair-text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			rp, ok := Match(settings.Profile{Name: "local-import", ModelID: tt.modelID})
			if !ok {
				t.Fatalf("expected runtime profile match for %q", tt.modelID)
			}
			if rp.Name != tt.wantName || rp.HuggingFaceRepo != tt.wantRepo || rp.Quant != tt.wantQuant || rp.RecommendedCtx != tt.wantCtx || rp.ToolProtocol != tt.wantProtocol {
				t.Fatalf("runtime profile = %#v", rp)
			}
			if rp.Defaults.Temperature != 0.2 || rp.Defaults.TopP != 0.95 || rp.Defaults.TopK != 20 || rp.Defaults.RepeatPenalty != 1.05 {
				t.Fatalf("sampling defaults = %#v", rp.Defaults)
			}
		})
	}
}

func TestMatchQwen36MultiQuantRepoUsesGenericTemplate(t *testing.T) {
	rp, ok := Match(settings.Profile{Name: "hf-import", ModelID: "unsloth/Qwen3.6-27B-GGUF"})
	if !ok || rp.Name != "qwen3.6-27b" {
		t.Fatalf("multi-quant repo matched %#v, ok=%v", rp, ok)
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
	if rp.Name != "gemma4-26b-a4b-qat" || rp.Quant != "QAT Q4-class" || rp.RecommendedCtx != 49152 || rp.Supports.MTP {
		t.Fatalf("unexpected gemma 4 26b-a4b QAT runtime profile: %#v", rp)
	}
	if rp.Defaults.Temperature != 1.0 || rp.Defaults.TopP != 0.95 || rp.Defaults.TopK != 64 || rp.Defaults.MinP != 0.05 || rp.Defaults.RepeatPenalty != 1.0 {
		t.Fatalf("gemma 4 26b-a4b QAT defaults = %#v, want live InferenceBridge temp/top_p/top_k/min_p/repeat_penalty", rp.Defaults)
	}
}

func TestMatchHauhauCSGemma426BQATTemplateBeforeGenericQAT(t *testing.T) {
	rp, ok := Match(settings.Profile{
		Name:    "local-import",
		ModelID: "Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced-Q4_K_M.gguf",
	})
	if !ok {
		t.Fatalf("expected HauhauCS Gemma 4 template match")
	}
	if rp.Name != "gemma4-26b-a4b-qat-hauhaucs-balanced" || rp.HuggingFaceRepo == "" {
		t.Fatalf("matched wrong template: %#v", rp)
	}
	if rp.Defaults.Temperature != 0.6 || rp.Defaults.TopP != 0.9 || rp.Defaults.TopK != 64 || rp.Defaults.MinP != 0.05 {
		t.Fatalf("HauhauCS sampling defaults = %#v", rp.Defaults)
	}
	if rp.RecommendedCtx != 40000 || rp.ChatTemplate != "embedded-gguf-jinja" || !rp.RequiresJinja {
		t.Fatalf("HauhauCS runtime defaults = %#v", rp)
	}
}

func TestMatchGenericGemmaQATDoesNotUseVariantOnlyTemplate(t *testing.T) {
	rp, ok := Match(settings.Profile{Name: "local", ModelID: "google/gemma-4-26B-A4B-it-qat-GGUF"})
	if !ok || rp.Name != "gemma4-26b-a4b-qat" {
		t.Fatalf("generic QAT matched %#v, ok=%v", rp, ok)
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

func TestMatchUsesModelIDBeforeBorrowedProfileName(t *testing.T) {
	tests := []struct {
		modelID string
		want    string
	}{
		{"gemma-4-12B-it-Q4_K_M.gguf", "gemma4-12b"},
		{"gemma-4-E4B-it-Q4_K_M.gguf", "gemma4-e4b"},
		{"gemma-4-E4B-it-Q8_0.gguf", "gemma4-e4b"},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			rp, ok := Match(settings.Profile{
				Name:    "qwen3.6-nothink-copy-copy:matrix-row",
				ModelID: tt.modelID,
			})
			if !ok || rp.Name != tt.want {
				t.Fatalf("Match(%q) = %#v, %v; want %q", tt.modelID, rp, ok, tt.want)
			}
			if rp.Adapter != "gemma4" || rp.ToolProtocol != "native-openai" || !rp.Supports.Thinking || !rp.Supports.Tools {
				t.Fatalf("small Gemma runtime metadata = %#v", rp)
			}
			if rp.Defaults.Temperature != 1.0 || rp.Defaults.TopP != 0.95 || rp.Defaults.TopK != 64 {
				t.Fatalf("small Gemma sampling defaults = %#v", rp.Defaults)
			}
		})
	}
}

func TestMatchQwen35LocalTemplates(t *testing.T) {
	tests := []struct {
		modelID string
		want    string
	}{
		{"Qwen3.5-4B-Q4_K_M.gguf", "qwen3.5-4b"},
		{"Qwythos-9B-Claude-Mythos-5-1M-Q5_K_M.gguf", "qwen3.5-qwythos-9b-1m"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			rp, ok := Match(settings.Profile{
				Name:    "qwen3.6-nothink-copy-copy:matrix-row",
				ModelID: tt.modelID,
			})
			if !ok || rp.Name != tt.want {
				t.Fatalf("Match(%q) = %#v, %v; want %q", tt.modelID, rp, ok, tt.want)
			}
			if rp.Adapter != "qwen35" || rp.ToolProtocol != "native-openai" {
				t.Fatalf("Qwen3.5 runtime metadata = %#v", rp)
			}
		})
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
