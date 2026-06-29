package app

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

//go:embed testdata/agent_eval/*.json
var agentEvalFS embed.FS

var agentEvalMu sync.Mutex

type AgentEvalScenario struct {
	Name             string            `json:"name"`
	Prompt           string            `json:"prompt"`
	Workspace        map[string]string `json:"workspace"`
	Mode             string            `json:"mode"`
	MaxToolCalls     int               `json:"max_tool_calls"`
	ExpectFiles      map[string]string `json:"expect_files"`
	ExpectStatus     string            `json:"expect_status"`
	ForbidSubstr     []string          `json:"forbid_substr"`
	MaxAutoContinues int               `json:"max_auto_continues"`
}

type AgentEvalResult struct {
	Name          string `json:"name"`
	Pass          bool   `json:"pass"`
	Status        string `json:"status"`
	ToolCalls     int    `json:"tool_calls"`
	AutoContinues int    `json:"auto_continues"`
	Truncations   int    `json:"truncations"`
	ToolErrors    int    `json:"tool_errors"`
	DurationMs    int64  `json:"duration_ms"`
	FailReason    string `json:"fail_reason,omitempty"`
}

type AgentEvalReport struct {
	Results   []AgentEvalResult `json:"results"`
	PassCount int               `json:"pass_count"`
	Total     int               `json:"total"`
	Profile   string            `json:"profile"`
}

func (a *App) RunAgentEval(profileName string) AgentEvalReport {
	scenarios, err := loadAgentEvalScenarios()
	if err != nil {
		return AgentEvalReport{
			Profile: strings.TrimSpace(profileName),
			Results: []AgentEvalResult{{
				Name:       "load-scenarios",
				Pass:       false,
				Status:     "error",
				FailReason: err.Error(),
			}},
			Total: 1,
		}
	}
	return a.runAgentEvalScenarios(profileName, scenarios)
}

func (a *App) runAgentEvalScenarios(profileName string, scenarios []AgentEvalScenario) AgentEvalReport {
	agentEvalMu.Lock()
	defer agentEvalMu.Unlock()

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	if a != nil {
		a.mu.Lock()
		if a.cfg != nil {
			cfg = *a.cfg
			cloneToolsConfigRefs(&cfg.Tools)
		}
		if a.profiles != nil {
			profiles = *a.profiles
		}
		a.mu.Unlock()
	}
	if strings.TrimSpace(profileName) != "" {
		cfg.ActiveProfile = strings.TrimSpace(profileName)
	}
	profile := activeProfile(&cfg, &profiles)
	cfg.Logging.Enabled = false
	cfg.Memory.Enabled = false
	cfg.Skills.Enabled = false
	cfg.Context.WorkspaceDir = ""
	if cfg.Tools.EnabledTools == nil {
		cfg.Tools.EnabledTools = settings.DefaultSettings().Tools.EnabledTools
	}

	report := AgentEvalReport{Profile: cfg.ActiveProfile, Total: len(scenarios)}
	for _, scenario := range scenarios {
		result := runOneAgentEvalScenario(cfg, profiles, profile, scenario)
		if result.Pass {
			report.PassCount++
		}
		report.Results = append(report.Results, result)
	}
	return report
}

func runOneAgentEvalScenario(cfg settings.Settings, profiles settings.ProfilesFile, profile settings.Profile, scenario AgentEvalScenario) AgentEvalResult {
	start := time.Now()
	result := AgentEvalResult{Name: scenario.Name}
	if strings.TrimSpace(scenario.Name) == "" {
		result.Name = "unnamed"
	}

	workspace, err := os.MkdirTemp("", "mauler-agent-eval-*")
	if err != nil {
		result.Status = "error"
		result.FailReason = err.Error()
		return result
	}
	defer os.RemoveAll(workspace)

	oldWD, err := os.Getwd()
	if err != nil {
		result.Status = "error"
		result.FailReason = err.Error()
		return result
	}
	defer os.Chdir(oldWD)
	if err := seedAgentEvalWorkspace(workspace, scenario.Workspace); err != nil {
		result.Status = "error"
		result.FailReason = err.Error()
		return result
	}
	if err := os.Chdir(workspace); err != nil {
		result.Status = "error"
		result.FailReason = err.Error()
		return result
	}

	runApp := &App{
		cfg:            &cfg,
		profiles:       &profiles,
		suppressEvents: true,
		history:        agent.NewHistory(profile.CtxTokens),
		rollback:       &agent.Rollback{},
		registry:       tools.New(),
		autoAgents:     true,
		bgJobs:         make(map[string]*bgJob),
		contextWindow:  profile.CtxTokens,
	}
	runApp.registerAppTools()
	tools.SetConfigSnapshot(cfg.Tools)

	if scenario.MaxToolCalls > 0 {
		cfg.Agents.MaxToolCalls = scenario.MaxToolCalls
	}
	modeName := firstNonEmpty(scenario.Mode, "Builder")
	mode := applyPresetToMode(baseMode(modeName), cfg.Agents.Presets)
	userMsg := llm.NewTextMessage(llm.RoleUser, scenario.Prompt)
	run := startTaskRun(scenario.Prompt, mode.Name, cfg.ActiveProfile, profile.ModelID)
	finished := runApp.runAgentLoop(context.Background(), userMsg, profile, &cfg, true, mode, nil, nil, run)
	result.DurationMs = time.Since(start).Milliseconds()
	scoreAgentEvalResult(&result, finished, workspace, scenario)
	return result
}

func loadAgentEvalScenarios() ([]AgentEvalScenario, error) {
	entries, err := agentEvalFS.ReadDir("testdata/agent_eval")
	if err != nil {
		return nil, err
	}
	var scenarios []AgentEvalScenario
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := agentEvalFS.ReadFile(filepath.ToSlash(filepath.Join("testdata/agent_eval", entry.Name())))
		if err != nil {
			return nil, err
		}
		var scenario AgentEvalScenario
		if err := json.Unmarshal(data, &scenario); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios, nil
}

func seedAgentEvalWorkspace(root string, files map[string]string) error {
	for name, content := range files {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
			return fmt.Errorf("unsafe scenario path %q", name)
		}
		path := filepath.Join(root, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			return err
		}
	}
	return nil
}

func scoreAgentEvalResult(result *AgentEvalResult, run TaskRun, workspace string, scenario AgentEvalScenario) {
	result.Status = run.Status
	result.ToolCalls = len(run.Tools)
	result.AutoContinues = countRunEvents(run.Events, "continue")
	result.Truncations = countRunEvents(run.Events, "truncated")
	result.ToolErrors = countRunEvents(run.Events, "tool_error")
	result.DurationMs = run.DurationMs

	var failures []string
	if want := strings.TrimSpace(scenario.ExpectStatus); want != "" && run.Status != want {
		failures = append(failures, fmt.Sprintf("status=%q want %q", run.Status, want))
	}
	for rel, wantSubstr := range scenario.ExpectFiles {
		data, err := os.ReadFile(filepath.Join(workspace, filepath.Clean(filepath.FromSlash(rel))))
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s missing: %v", rel, err))
			continue
		}
		if !strings.Contains(string(data), wantSubstr) {
			failures = append(failures, fmt.Sprintf("%s missing expected substring %q", rel, wantSubstr))
		}
	}
	for _, forbidden := range scenario.ForbidSubstr {
		if forbidden == "" {
			continue
		}
		for _, tool := range run.Tools {
			if strings.Contains(tool.Result, forbidden) {
				failures = append(failures, fmt.Sprintf("tool result leaked forbidden substring %q", forbidden))
				break
			}
		}
	}
	if scenario.MaxAutoContinues >= 0 && result.AutoContinues > scenario.MaxAutoContinues {
		failures = append(failures, fmt.Sprintf("auto_continues=%d > %d", result.AutoContinues, scenario.MaxAutoContinues))
	}
	result.Pass = len(failures) == 0
	result.FailReason = strings.Join(failures, "; ")
}

func countRunEvents(events []TaskRunEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}
