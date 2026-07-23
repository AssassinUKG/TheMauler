package app

import (
	"testing"

	"mauler/internal/settings"
)

func TestRecommendModelProfileTemplateReturnsCodeOwnedMetadata(t *testing.T) {
	a := &App{}
	got := a.RecommendModelProfileTemplate(settings.Profile{
		Name:      "hf-gemma",
		Provider:  "inference-bridge",
		ModelID:   "HauhauCS/Gemma4-26B-A4B-QAT-Uncensored-HauhauCS-Balanced-MTP",
		CtxTokens: 0,
		NoThink:   settings.GenerationParams{MaxTokens: 8192, Seed: -1},
	})

	if !got.Matched || got.TemplateID != "gemma4-26b-a4b-qat-hauhaucs-balanced" {
		t.Fatalf("template result = %#v", got)
	}
	if got.ChatTemplate != "embedded-gguf-jinja" || !got.RequiresJinja || got.HuggingFaceRepo == "" {
		t.Fatalf("template metadata = %#v", got)
	}
	if got.Profile.Provider != "inference-bridge" || got.Profile.CtxTokens != 40000 {
		t.Fatalf("templated profile = %#v", got.Profile)
	}
}

func TestRecommendModelProfileTemplateLeavesUnknownModelUsable(t *testing.T) {
	a := &App{}
	in := settings.Profile{
		Name:      "unknown",
		Provider:  "inference-bridge",
		ModelID:   "some/new-model.gguf",
		CtxTokens: 32768,
		NoThink:   settings.GenerationParams{Temperature: 0.7, MaxTokens: 4096, Seed: -1},
	}
	got := a.RecommendModelProfileTemplate(in)
	if got.Matched {
		t.Fatalf("unknown model unexpectedly matched: %#v", got)
	}
	if got.Profile.ModelID != in.ModelID || got.Profile.CtxTokens != in.CtxTokens || got.Profile.NoThink.Temperature != in.NoThink.Temperature {
		t.Fatalf("unknown profile changed: got %#v want %#v", got.Profile, in)
	}
}
