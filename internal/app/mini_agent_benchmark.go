package app

import (
	"strings"

	"mauler/internal/settings"
)

func (a *App) RunMiniAgentLoopBenchmark(profile settings.Profile, provider settings.Provider) AgentEvalResult {
	profile.Backend = provider.Backend
	profile.BaseURL = provider.BaseURL
	profile.APIKeyEnv = provider.APIKeyEnv
	profile.Name = firstNonEmpty(profile.Name, "matrix-mini-loop")

	cfg := settings.DefaultSettings()
	cfg.ActiveProfile = profile.Name
	cfg.Agents.MaxToolCalls = 6
	cfg.Agents.MaxRunSeconds = 180
	cfg.Logging.Enabled = false
	cfg.Memory.Enabled = false
	cfg.Skills.Enabled = false
	profiles := settings.ProfilesFile{
		Providers: map[string]settings.Provider{
			provider.Name: provider,
			"matrix-provider": {
				Name:      "matrix-provider",
				Backend:   provider.Backend,
				BaseURL:   provider.BaseURL,
				APIKeyEnv: provider.APIKeyEnv,
			},
		},
		Profiles: map[string]settings.Profile{
			profile.Name: profile,
		},
	}
	if strings.TrimSpace(profile.Provider) == "" {
		profile.Provider = firstNonEmpty(provider.Name, "matrix-provider")
		profiles.Profiles[profile.Name] = profile
	}

	return runOneAgentEvalScenario(cfg, profiles, profile, AgentEvalScenario{
		Name:   "Mini agent loop",
		Prompt: "Read notes.txt, then create summary.txt with a one-sentence summary. Verify the file exists before finishing.",
		Workspace: map[string]string{
			"notes.txt": "TheMauler is a Windows desktop agent workbench with local LLM support, tool calls, benchmarks, logs, and memory.",
		},
		Mode:             "Builder",
		MaxToolCalls:     6,
		ExpectFiles:      map[string]string{"summary.txt": "TheMauler"},
		ExpectStatus:     "done",
		MaxAutoContinues: 2,
	})
}
