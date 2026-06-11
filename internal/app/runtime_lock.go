package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
)

// RuntimeLock records the local runtime facts that most often explain
// "worked yesterday" regressions: model identity, profile context, adapter,
// tool protocol, and launch-affecting flags.
type RuntimeLock struct {
	UpdatedAt       string `json:"updated_at"`
	ProfileName     string `json:"profile_name"`
	ProviderName    string `json:"provider_name"`
	Backend         string `json:"backend"`
	BaseURL         string `json:"base_url"`
	ModelID         string `json:"model_id"`
	ModelHash       string `json:"model_hash"`
	Adapter         string `json:"adapter"`
	ToolProtocol    string `json:"tool_protocol"`
	CtxTokens       int    `json:"ctx_tokens"`
	Thinking        bool   `json:"thinking"`
	PreserveThink   bool   `json:"preserve_thinking"`
	SpecType        string `json:"spec_type,omitempty"`
	SpecDraftNMax   int    `json:"spec_draft_n_max,omitempty"`
	SpecDraftModel  string `json:"spec_draft_model,omitempty"`
	LaunchSignature string `json:"launch_signature"`
}

func saveRuntimeLockSnapshot(profile settings.Profile) error {
	cfgDir, err := settings.ConfigDir()
	if err != nil {
		return err
	}
	lock := buildRuntimeLock(profile)
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfgDir, "runtime-lock.json"), data, 0o600)
}

func buildRuntimeLock(profile settings.Profile) RuntimeLock {
	adapter := "unknown"
	toolProtocol := "unknown"
	if rp, ok := runtimeprofile.Match(profile); ok {
		adapter = rp.Adapter
		toolProtocol = rp.ToolProtocol
	}
	sigParts := []string{
		profile.Name,
		profile.Provider,
		profile.Backend,
		profile.BaseURL,
		profile.ModelID,
		profile.MMProj,
		profile.SpecType,
		profile.SpecDraftModel,
	}
	sum := sha256.Sum256([]byte(strings.Join(sigParts, "\x00")))
	return RuntimeLock{
		UpdatedAt:       time.Now().Format(time.RFC3339),
		ProfileName:     profile.Name,
		ProviderName:    profile.Provider,
		Backend:         profile.Backend,
		BaseURL:         profile.BaseURL,
		ModelID:         profile.ModelID,
		ModelHash:       hex.EncodeToString(sum[:])[:16],
		Adapter:         adapter,
		ToolProtocol:    toolProtocol,
		CtxTokens:       profile.CtxTokens,
		Thinking:        profile.Thinking,
		PreserveThink:   profile.PreserveThink,
		SpecType:        profile.SpecType,
		SpecDraftNMax:   profile.SpecDraftNMax,
		SpecDraftModel:  profile.SpecDraftModel,
		LaunchSignature: strings.Join(sigParts, " | "),
	}
}
