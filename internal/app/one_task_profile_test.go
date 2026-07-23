package app

import (
	"reflect"
	"testing"

	"mauler/internal/settings"
)

func TestOneTaskCloudProfilesDoNotAppearAsDefaultChoices(t *testing.T) {
	profiles := &settings.ProfilesFile{
		Providers: map[string]settings.Provider{
			"local": {
				Name:    "local",
				Backend: "openai-compatible",
				BaseURL: "http://127.0.0.1:8800/v1",
			},
			"openrouter": {
				Name:      "openrouter",
				Backend:   "openai-compatible",
				BaseURL:   "https://openrouter.ai/api/v1",
				APIKeyEnv: "OPENROUTER_API_KEY",
			},
		},
		Profiles: map[string]settings.Profile{
			"local-fast": {Provider: "local", ModelID: "qwen-local"},
			"cloud-deep": {Provider: "openrouter", ModelID: "vendor/frontier"},
		},
	}
	app := &App{profiles: profiles}

	if got := app.GetProfileNames(); !reflect.DeepEqual(got, []string{"local-fast"}) {
		t.Fatalf("default profile choices = %v, want local profiles only", got)
	}
	if !isOneTaskCloudProfile(profiles.Profiles["cloud-deep"], profiles) {
		t.Fatal("OpenRouter profile was not classified as a one-task cloud profile")
	}
	app.cfg = &settings.Settings{ActiveProfile: "local-fast"}
	if err := app.SwitchProfile("cloud-deep"); err == nil {
		t.Fatal("cloud profile was allowed to replace the persistent local default")
	}
	if app.cfg.ActiveProfile != "local-fast" {
		t.Fatalf("active profile changed to %q", app.cfg.ActiveProfile)
	}
}

func TestUnknownOneTaskProfileDoesNotLeaveAgentRunning(t *testing.T) {
	app := &App{
		cfg: &settings.Settings{ActiveProfile: "local-fast"},
		profiles: &settings.ProfilesFile{
			Profiles: map[string]settings.Profile{
				"local-fast": {Provider: "local", ModelID: "qwen-local"},
			},
		},
	}

	err := app.SendMessageWithProfile("hello", nil, nil, "missing-cloud-profile")
	if err == nil {
		t.Fatal("expected an invalid one-task profile error")
	}
	if app.agentRunning {
		t.Fatal("invalid one-task profile left the agent marked as running")
	}
}
