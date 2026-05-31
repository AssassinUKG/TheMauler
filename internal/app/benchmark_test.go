package app

import (
	"testing"

	"mauler/internal/settings"
)

func TestRecommendProfileSettingsEnablesMTPOnlyForMTPQwen(t *testing.T) {
	profile := settings.Profile{
		Name:      "qwen3.6-mtp",
		ModelID:   "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf",
		CtxTokens: 32768,
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.SpecType != "draft-mtp" || rec.SpecDraftNMax != 2 {
		t.Fatalf("expected conservative MTP recommendation, got spec_type=%q n=%d", rec.SpecType, rec.SpecDraftNMax)
	}
	if !rec.Thinking || !rec.PreserveThink {
		t.Fatalf("qwen recommendation should enable thinking flags: %#v", rec)
	}
}

func TestRecommendProfileSettingsClearsMTPForPlainQwen(t *testing.T) {
	profile := settings.Profile{
		Name:          "qwen3.6-think",
		ModelID:       "Qwen3.6-27B-UD-Q4_K_XL.gguf",
		CtxTokens:     32768,
		SpecType:      "draft-mtp",
		SpecDraftNMax: 3,
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.SpecType != "" || rec.SpecDraftNMax != 0 {
		t.Fatalf("plain qwen should not keep MTP enabled: %#v", rec)
	}
}

func TestRecommendProfileSettingsDisablesThinkingForGemma(t *testing.T) {
	profile := settings.Profile{
		Name:          "gemma4-test",
		ModelID:       "gemma-4-31B-it-Q4_K_S.gguf",
		CtxTokens:     65000,
		Thinking:      true,
		PreserveThink: true,
		SpecType:      "draft-mtp",
		SpecDraftNMax: 2,
	}

	rec, _ := recommendProfileSettings(profile)
	if rec.Thinking || rec.PreserveThink || rec.SpecType != "" || rec.SpecDraftNMax != 0 {
		t.Fatalf("gemma recommendation should disable thinking/MTP: %#v", rec)
	}
}
