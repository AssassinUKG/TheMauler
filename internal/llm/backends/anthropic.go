package backends

import (
	"context"
	"fmt"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"os"
)

// Anthropic is a stub llm.Client for the Anthropic Messages API.
// Full implementation comes in Phase 8 (Polish).
type Anthropic struct {
	apiKey  string
	modelID string
}

// NewAnthropic creates an Anthropic client using the API key from the profile's
// api_key_env environment variable.
func NewAnthropic(p settings.Profile) llm.Client {
	key := os.Getenv(p.APIKeyEnv)
	return &Anthropic{apiKey: key, modelID: p.ModelID}
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) Ping(ctx context.Context) error {
	if a.apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	return nil // skip live ping for stub
}

func (a *Anthropic) Models(ctx context.Context) ([]string, error) {
	return []string{a.modelID}, nil
}

func (a *Anthropic) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	go func() {
		defer close(ch)
		ch <- llm.Delta{
			Content: "[Anthropic backend not yet implemented — coming in Phase 8]",
			Done:    true,
		}
	}()
	return ch, nil
}
