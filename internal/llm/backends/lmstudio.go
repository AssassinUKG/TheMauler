package backends

import (
	"mauler/internal/llm"
	"mauler/internal/settings"
	"os"
)

// NewLMStudio creates a llm.Client for an LM Studio local server.
// chat_template_kwargs are NOT sent — LM Studio manages thinking via its own UI.
// Default base URL: http://localhost:1234/v1
func NewLMStudio(p settings.Profile) llm.Client {
	baseURL := p.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:1234/v1"
	}
	apiKey := ""
	if p.APIKeyEnv != "" {
		apiKey = os.Getenv(p.APIKeyEnv)
	}
	return newOpenAICompat("lmstudio", baseURL, p.ModelID, p.CtxTokens, apiKey, false)
}
