package backends

import (
	"encoding/json"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"os"
)

// NewLlamacpp creates a llm.Client for a llama.cpp /v1 server.
// It enables chat_template_kwargs so thinking mode can be toggled per-request.
// Default base URL: http://localhost:8080/v1
func NewLlamacpp(p settings.Profile) llm.Client {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080/v1"
	}
	apiKey := ""
	if p.APIKeyEnv != "" {
		apiKey = os.Getenv(p.APIKeyEnv)
	}
	client := newOpenAICompat("llamacpp", baseURL, p.ModelID, p.CtxTokens, apiKey, true)
	kwargs := map[string]interface{}{
		"enable_thinking":   p.Thinking,
		"preserve_thinking": p.PreserveThink,
	}
	if p.SpecType != "" {
		kwargs["spec_type"] = p.SpecType
	}
	if p.SpecDraftNMax > 0 {
		kwargs["spec_draft_n_max"] = p.SpecDraftNMax
	}
	if raw, err := json.Marshal(kwargs); err == nil {
		client.loadKwargsJSON = string(raw)
	}
	return client
}
