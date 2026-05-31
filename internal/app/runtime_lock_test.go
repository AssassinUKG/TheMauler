package app

import (
	"testing"

	"mauler/internal/settings"
)

func TestBuildRuntimeLockCapturesAdapterAndLaunchSignature(t *testing.T) {
	profile := settings.Profile{
		Name:          "qwen3.6-mtp",
		Provider:      "llamacpp-local",
		Backend:       "llamacpp",
		BaseURL:       "http://localhost:8080/v1",
		ModelID:       "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf",
		CtxTokens:     32768,
		Thinking:      true,
		PreserveThink: true,
		SpecType:      "draft-mtp",
		SpecDraftNMax: 2,
	}

	lock := buildRuntimeLock(profile)
	if lock.Adapter != "qwen36" || lock.ToolProtocol != "native-openai" {
		t.Fatalf("unexpected runtime lock adapter/protocol: %#v", lock)
	}
	if lock.ModelHash == "" || lock.LaunchSignature == "" {
		t.Fatalf("runtime lock should include stable identity fields: %#v", lock)
	}
	if lock.SpecType != "draft-mtp" || lock.SpecDraftNMax != 2 {
		t.Fatalf("runtime lock lost MTP launch fields: %#v", lock)
	}
}
