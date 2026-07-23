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
var processStateMu sync.Mutex

type AgentEvalScenario struct {
	Name                       string              `json:"name"`
	Prompt                     string              `json:"prompt"`
	Workspace                  map[string]string   `json:"workspace"`
	Mode                       string              `json:"mode"`
	MaxToolCalls               int                 `json:"max_tool_calls"`
	ExpectFiles                map[string]string   `json:"expect_files"`
	ExpectFilesAny             map[string][]string `json:"expect_files_any,omitempty"`
	ExpectFilesCaseInsensitive bool                `json:"expect_files_case_insensitive,omitempty"`
	ExpectStatus               string              `json:"expect_status"`
	ForbidSubstr               []string            `json:"forbid_substr"`
	ForbidTools                []string            `json:"forbid_tools,omitempty"`
	ForbidToolInputSubstr      []string            `json:"forbid_tool_input_substr,omitempty"`
	MaxAutoContinues           int                 `json:"max_auto_continues"`
	CompletionBlocking         *bool               `json:"completion_blocking,omitempty"`
	ReviewerPass               *bool               `json:"reviewer_pass,omitempty"`
	RuntimeVerifier            string              `json:"runtime_verifier,omitempty"`
}

type AgentEvalResult struct {
	Name               string          `json:"name"`
	Attempt            int             `json:"attempt,omitempty"`
	Pass               bool            `json:"pass"`
	ArtifactPass       bool            `json:"artifact_pass"`
	HygienePass        bool            `json:"hygiene_pass"`
	StatusPass         bool            `json:"status_pass"`
	Status             string          `json:"status"`
	ToolCalls          int             `json:"tool_calls"`
	ToolSuccessRate    int             `json:"tool_success_rate"`
	AutoContinues      int             `json:"auto_continues"`
	Truncations        int             `json:"truncations"`
	ToolErrors         int             `json:"tool_errors"`
	RepeatedToolInputs int             `json:"repeated_tool_inputs"`
	RepeatedSkips      int             `json:"repeated_skips"`
	RepeatToolRate     int             `json:"repeat_tool_rate"`
	VerifierPrompts    int             `json:"verifier_prompts"`
	MaxRoutedTools     int             `json:"max_routed_tools"`
	PromptWarnings     int             `json:"prompt_warnings"`
	StabilityScore     int             `json:"stability_score"`
	FalseDone          bool            `json:"false_done"`
	PolicyViolations   int             `json:"policy_violations"`
	HumanInterventions int             `json:"human_interventions"`
	RecoveryEvents     int             `json:"recovery_events"`
	Recovered          bool            `json:"recovered"`
	DurationMs         int64           `json:"duration_ms"`
	FailReason         string          `json:"fail_reason,omitempty"`
	RuntimePass        bool            `json:"runtime_pass,omitempty"`
	DesktopScreenshot  string          `json:"desktop_screenshot,omitempty"`
	MobileScreenshot   string          `json:"mobile_screenshot,omitempty"`
	RuntimeFailures    []string        `json:"runtime_failures,omitempty"`
	ModelID            string          `json:"model_id,omitempty"`
	Provider           string          `json:"provider,omitempty"`
	ContextTokens      int             `json:"context_tokens,omitempty"`
	Seed               int64           `json:"seed,omitempty"`
	ArtifactHash       string          `json:"artifact_hash,omitempty"`
	VerifierVersion    string          `json:"verifier_version,omitempty"`
	StopReason         string          `json:"stop_reason,omitempty"`
	ToolTrace          []TaskToolEvent `json:"tool_trace,omitempty"`
	ResponseExcerpt    string          `json:"response_excerpt,omitempty"`
	EventTrace         []TaskRunEvent  `json:"event_trace,omitempty"`
}

type AgentEvalReport struct {
	Results                   []AgentEvalResult `json:"results"`
	PassCount                 int               `json:"pass_count"`
	Total                     int               `json:"total"`
	Profile                   string            `json:"profile"`
	ID                        string            `json:"id,omitempty"`
	CreatedAt                 string            `json:"created_at,omitempty"`
	Repeats                   int               `json:"repeats"`
	FixtureCount              int               `json:"fixture_count"`
	FixturePassCount          int               `json:"fixture_pass_count"`
	PassPower                 string            `json:"pass_power"`
	FullPass                  bool              `json:"full_pass"`
	UnsupportedCompletionRate float64           `json:"unsupported_completion_rate"`
	DuplicateActionRate       float64           `json:"duplicate_action_rate"`
	ToolErrorRate             float64           `json:"tool_error_rate"`
	RecoverySuccessRate       float64           `json:"recovery_success_rate"`
	AverageToolCalls          float64           `json:"average_tool_calls"`
	AverageDurationMs         int64             `json:"average_duration_ms"`
	PolicyViolations          int               `json:"policy_violations"`
	HumanInterventions        int               `json:"human_interventions"`
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
	report := a.runAgentEvalScenarios(profileName, scenarios)
	_ = saveAgentEvalReport(report)
	return report
}

// RunAgentEvalRepeated runs the production agent-loop suite repeatedly so
// single-run luck is reported separately from pass^k reliability.
func (a *App) RunAgentEvalRepeated(profileName string, repeats int) AgentEvalReport {
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
			Total:   1,
			Repeats: normalizeAgentEvalRepeats(repeats),
		}
	}
	report := a.runAgentEvalScenariosRepeated(profileName, scenarios, repeats)
	_ = saveAgentEvalReport(report)
	return report
}

func (a *App) RunJHUTAgentEval(profileName string) AgentEvalReport {
	home, _ := os.UserHomeDir()
	root := strings.TrimSpace(os.Getenv("MAULER_BENCH_ROOT"))
	if root == "" {
		root = filepath.Join(home, "Documents", "MaulerBench")
	}
	promptPath := filepath.Join(root, "jhut-threejs", "MAULER.md")
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		return AgentEvalReport{Profile: profileName, Total: 1, Results: []AgentEvalResult{{Name: "jhut-browser-e2e", Status: "error", FailReason: "read shared JHUT prompt: " + err.Error()}}}
	}
	scenario := AgentEvalScenario{Name: "jhut-browser-e2e", Prompt: string(prompt), Workspace: map[string]string{"MAULER.md": string(prompt)}, Mode: "Builder", MaxToolCalls: 40, ExpectFiles: map[string]string{"jhut.html": ""}, ExpectStatus: "done", MaxAutoContinues: 8, RuntimeVerifier: "jhut"}
	report := a.runAgentEvalScenarios(profileName, []AgentEvalScenario{scenario})
	_ = saveAgentEvalReport(report)
	return report
}

func (a *App) runAgentEvalScenarios(profileName string, scenarios []AgentEvalScenario) AgentEvalReport {
	return a.runAgentEvalScenariosRepeated(profileName, scenarios, 1)
}

func (a *App) runAgentEvalScenariosRepeated(profileName string, scenarios []AgentEvalScenario, repeats int) AgentEvalReport {
	repeats = normalizeAgentEvalRepeats(repeats)
	agentEvalMu.Lock()
	defer agentEvalMu.Unlock()
	processStateMu.Lock()
	defer processStateMu.Unlock()
	if err := a.beginAgentEval(); err != nil {
		return agentEvalPreflightFailure(profileName, err)
	}
	defer a.endAgentEval()

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
	cfg = canonicalAgentEvalSettings(cfg)
	profile := activeProfile(&cfg, &profiles)
	cfg.Logging.Enabled = false
	cfg.Memory.Enabled = false
	cfg.Skills.Enabled = false
	cfg.Context.WorkspaceDir = ""
	if cfg.Tools.EnabledTools == nil {
		cfg.Tools.EnabledTools = settings.DefaultSettings().Tools.EnabledTools
	}

	report := AgentEvalReport{
		ID:           fmt.Sprintf("agent-eval-%s", time.Now().Format("20060102-150405")),
		CreatedAt:    time.Now().Format(time.RFC3339),
		Profile:      cfg.ActiveProfile,
		Repeats:      repeats,
		FixtureCount: len(scenarios),
		Total:        len(scenarios) * repeats,
	}
	fixturePasses := make(map[string]int, len(scenarios))
	for attempt := 1; attempt <= repeats; attempt++ {
		for _, scenario := range scenarios {
			result := runOneAgentEvalScenario(cfg, profiles, profile, scenario)
			result.Attempt = attempt
			if result.Pass {
				report.PassCount++
				fixturePasses[scenario.Name]++
			}
			report.Results = append(report.Results, result)
		}
	}
	for _, scenario := range scenarios {
		if fixturePasses[scenario.Name] == repeats {
			report.FixturePassCount++
		}
	}
	populateAgentEvalReportMetrics(&report)
	return report
}

func canonicalAgentEvalSettings(cfg settings.Settings) settings.Settings {
	defaults := settings.DefaultSettings()
	// Agent Eval is a model/runtime comparison, not a test of whichever
	// confirmations, custom presets, explicit verify command, or tool toggle the
	// operator happened to use in the previous chat. Keep endpoint/shell
	// environment settings, but normalize the operational envelope.
	cfg.Agents = defaults.Agents
	cfg.Agents.MaxRunSeconds = 0
	cfg.Agents.ReviewLoop.ReviewerPass = false
	cfg.Agents.ReviewLoop.VerifyCommands = nil
	cfg.Tools.Enabled = true
	cfg.Tools.ActiveToolset = "unrestricted"
	cfg.Tools.ConfirmReads = false
	cfg.Tools.ConfirmWrites = false
	cfg.Tools.ConfirmExec = false
	cfg.Tools.EnabledTools = defaults.Tools.EnabledTools
	cfg.Tools.Toolsets = defaults.Tools.Toolsets
	cloneToolsConfigRefs(&cfg.Tools)
	return cfg
}

func normalizeAgentEvalRepeats(repeats int) int {
	if repeats <= 0 {
		return 1
	}
	if repeats > 10 {
		return 10
	}
	return repeats
}

func populateAgentEvalReportMetrics(report *AgentEvalReport) {
	if report == nil {
		return
	}
	if report.Repeats <= 0 {
		report.Repeats = 1
	}
	if report.FixtureCount <= 0 && report.Repeats > 0 {
		report.FixtureCount = report.Total / report.Repeats
	}
	falseDone := 0
	totalTools := 0
	totalDuplicates := 0
	totalToolErrors := 0
	totalDuration := int64(0)
	recoveryAttempts := 0
	recoverySuccesses := 0
	for _, result := range report.Results {
		if result.FalseDone {
			falseDone++
		}
		totalTools += result.ToolCalls
		totalDuplicates += result.RepeatedToolInputs + result.RepeatedSkips
		totalToolErrors += result.ToolErrors
		totalDuration += result.DurationMs
		report.PolicyViolations += result.PolicyViolations
		report.HumanInterventions += result.HumanInterventions
		if result.RecoveryEvents > 0 {
			recoveryAttempts++
			if result.Recovered {
				recoverySuccesses++
			}
		}
	}
	if report.Total > 0 {
		report.UnsupportedCompletionRate = float64(falseDone) * 100 / float64(report.Total)
		report.AverageToolCalls = float64(totalTools) / float64(report.Total)
		report.AverageDurationMs = totalDuration / int64(report.Total)
	}
	if totalTools > 0 {
		report.DuplicateActionRate = float64(totalDuplicates) * 100 / float64(totalTools)
		report.ToolErrorRate = float64(totalToolErrors) * 100 / float64(totalTools)
	}
	if recoveryAttempts > 0 {
		report.RecoverySuccessRate = float64(recoverySuccesses) * 100 / float64(recoveryAttempts)
	} else {
		report.RecoverySuccessRate = 100
	}
	report.FullPass = report.PassCount == report.Total && report.FixturePassCount == report.FixtureCount && report.PolicyViolations == 0 && report.HumanInterventions == 0 && falseDone == 0
	if report.FullPass {
		report.PassPower = fmt.Sprintf("pass^%d", report.Repeats)
	} else {
		report.PassPower = fmt.Sprintf("not pass^%d", report.Repeats)
	}
}

func (a *App) beginAgentEval() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	if a.evalRunning {
		a.mu.Unlock()
		return fmt.Errorf("agent eval is already running")
	}
	if a.agentRunning || a.artifactRunning {
		a.mu.Unlock()
		return fmt.Errorf("agent eval requires an idle app: a task or artifact is active")
	}
	a.evalRunning = true
	queue := a.channelQueue
	a.mu.Unlock()

	fail := func(err error) error {
		a.endAgentEval()
		return err
	}
	if queue != nil && len(queue.ListActive()) > 0 {
		return fail(fmt.Errorf("agent eval requires an empty channel work queue"))
	}
	a.channelDrainMu.Lock()
	draining := a.channelDrainRunning
	a.channelDrainMu.Unlock()
	if draining {
		return fail(fmt.Errorf("agent eval requires idle channel dispatch"))
	}
	a.bgMu.Lock()
	backgroundJobs := len(a.bgJobs)
	a.bgMu.Unlock()
	if backgroundJobs > 0 {
		return fail(fmt.Errorf("agent eval requires no tracked background terminal jobs"))
	}
	a.shellMu.Lock()
	sessions := make([]*shellSession, 0, len(a.shellSessions))
	for _, session := range a.shellSessions {
		sessions = append(sessions, session)
	}
	a.shellMu.Unlock()
	for _, session := range sessions {
		if session != nil && !session.runMu.TryLock() {
			return fail(fmt.Errorf("agent eval requires idle terminal commands"))
		}
		if session != nil {
			session.runMu.Unlock()
		}
	}
	return nil
}

func (a *App) endAgentEval() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.evalRunning = false
	a.mu.Unlock()
}

func agentEvalPreflightFailure(profileName string, err error) AgentEvalReport {
	return AgentEvalReport{
		Profile: strings.TrimSpace(profileName),
		Total:   1,
		Results: []AgentEvalResult{{
			Name:       "eval-preflight",
			Status:     "blocked",
			FailReason: err.Error(),
		}},
	}
}

func agentEvalReportsPath() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agent-eval-runs.json"), nil
}

func loadAgentEvalReports() ([]AgentEvalReport, error) {
	path, err := agentEvalReportsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []AgentEvalReport{}, nil
	}
	if err != nil {
		return nil, err
	}
	var reports []AgentEvalReport
	if err := json.Unmarshal(data, &reports); err != nil {
		return nil, err
	}
	return reports, nil
}

func saveAgentEvalReport(report AgentEvalReport) error {
	reports, err := loadAgentEvalReports()
	if err != nil {
		return err
	}
	reports = append([]AgentEvalReport{report}, reports...)
	if len(reports) > 100 {
		reports = reports[:100]
	}
	path, err := agentEvalReportsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (a *App) ClearAgentEvalReports() error {
	path, err := agentEvalReportsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
	defer func() { _ = os.Chdir(oldWD) }()
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
	restoreToolConfig := tools.SwapConfigSnapshot(cfg.Tools)
	defer restoreToolConfig()

	if scenario.MaxToolCalls > 0 {
		cfg.Agents.MaxToolCalls = scenario.MaxToolCalls
	}
	if scenario.CompletionBlocking != nil {
		cfg.Agents.ReviewLoop.CompletionBlocking = *scenario.CompletionBlocking
	}
	if scenario.ReviewerPass != nil {
		cfg.Agents.ReviewLoop.ReviewerPass = *scenario.ReviewerPass
	}
	modeName := firstNonEmpty(scenario.Mode, "Builder")
	mode := applyPresetToMode(baseMode(modeName), cfg.Agents.Presets)
	userMsg := llm.NewTextMessage(llm.RoleUser, scenario.Prompt)
	run := startTaskRun(scenario.Prompt, mode.Name, cfg.ActiveProfile, profile.ModelID)
	finished := runApp.runAgentLoop(context.Background(), userMsg, profile, &cfg, true, mode, nil, nil, run)
	result.DurationMs = time.Since(start).Milliseconds()
	result.ModelID, result.Provider, result.ContextTokens = profile.ModelID, profile.Provider, profile.CtxTokens
	result.Seed = profile.ActiveParams(true).Seed
	result.StopReason = finished.StopReason
	result.ToolTrace = append([]TaskToolEvent(nil), finished.Tools...)
	scoreAgentEvalResult(&result, finished, workspace, scenario)
	if !result.Pass {
		result.ResponseExcerpt = truncateRunes(strings.TrimSpace(finished.Response), 4000)
		startEvent := len(finished.Events) - 24
		if startEvent < 0 {
			startEvent = 0
		}
		result.EventTrace = append([]TaskRunEvent(nil), finished.Events[startEvent:]...)
	}
	if scenario.RuntimeVerifier == "jhut" {
		configDir, _ := settings.ConfigDir()
		evidenceDir := filepath.Join(configDir, "agent-eval-artifacts", fmt.Sprintf("%s-%d", scenario.Name, time.Now().Unix()))
		report, _ := verifyJHUTBrowser(context.Background(), workspace, evidenceDir)
		result.RuntimePass = report.Pass
		result.DesktopScreenshot = report.DesktopScreenshot
		result.MobileScreenshot = report.MobileScreenshot
		result.RuntimeFailures = report.Failures
		result.VerifierVersion = report.VerifierVersion
		if artifact, err := os.ReadFile(filepath.Join(workspace, "jhut.html")); err == nil {
			result.ArtifactHash = byteHash(artifact)
		}
		if !report.Pass {
			result.ArtifactPass = false
			result.Pass = false
			if result.FailReason != "" {
				result.FailReason += "; "
			}
			result.FailReason += "runtime verification: " + strings.Join(report.Failures, ", ")
		}
	}
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
	metrics := buildLoopMetrics(run)
	result.Status = run.Status
	result.ToolCalls = metrics.ToolCalls
	result.ToolSuccessRate = toolSuccessRate(run.Tools)
	result.AutoContinues = metrics.AutoContinues
	result.Truncations = metrics.Truncations
	result.ToolErrors = metrics.ToolErrors
	result.RepeatedToolInputs = metrics.RepeatedToolInputs
	result.RepeatedSkips = metrics.RepeatedSkips
	result.RepeatToolRate = repeatToolRate(metrics)
	result.VerifierPrompts = metrics.VerifierPrompts
	result.MaxRoutedTools = metrics.MaxRoutedTools
	result.PromptWarnings = metrics.PromptWarnings
	result.StabilityScore = metrics.StabilityScore
	result.DurationMs = run.DurationMs

	var statusFailures []string
	var artifactFailures []string
	var hygieneFailures []string
	if want := strings.TrimSpace(scenario.ExpectStatus); want != "" && run.Status != want {
		statusFailures = append(statusFailures, fmt.Sprintf("status=%q want %q", run.Status, want))
	}
	for rel, wantSubstr := range scenario.ExpectFiles {
		data, err := os.ReadFile(filepath.Join(workspace, filepath.Clean(filepath.FromSlash(rel))))
		if err != nil {
			artifactFailures = append(artifactFailures, fmt.Sprintf("%s missing: %v", rel, err))
			continue
		}
		content := string(data)
		matched := strings.Contains(content, wantSubstr)
		if scenario.ExpectFilesCaseInsensitive {
			matched = strings.Contains(strings.ToLower(content), strings.ToLower(wantSubstr))
		}
		if !matched {
			artifactFailures = append(artifactFailures, fmt.Sprintf("%s missing expected substring %q", rel, wantSubstr))
		}
	}
	for rel, acceptedSubstrs := range scenario.ExpectFilesAny {
		data, err := os.ReadFile(filepath.Join(workspace, filepath.Clean(filepath.FromSlash(rel))))
		if err != nil {
			artifactFailures = append(artifactFailures, fmt.Sprintf("%s missing: %v", rel, err))
			continue
		}
		content := string(data)
		if scenario.ExpectFilesCaseInsensitive {
			content = strings.ToLower(content)
		}
		matched := false
		for _, accepted := range acceptedSubstrs {
			if scenario.ExpectFilesCaseInsensitive {
				accepted = strings.ToLower(accepted)
			}
			if strings.Contains(content, accepted) {
				matched = true
				break
			}
		}
		if !matched {
			artifactFailures = append(artifactFailures, fmt.Sprintf("%s missing every accepted substring %q", rel, acceptedSubstrs))
		}
	}
	for _, forbidden := range scenario.ForbidSubstr {
		if forbidden == "" {
			continue
		}
		for _, tool := range run.Tools {
			if strings.Contains(tool.Result, forbidden) {
				artifactFailures = append(artifactFailures, fmt.Sprintf("tool result leaked forbidden substring %q", forbidden))
				break
			}
		}
	}
	forbiddenTools := make(map[string]bool, len(scenario.ForbidTools))
	for _, name := range scenario.ForbidTools {
		forbiddenTools[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for _, tool := range run.Tools {
		if forbiddenTools[strings.ToLower(strings.TrimSpace(tool.Name))] {
			hygieneFailures = append(hygieneFailures, fmt.Sprintf("forbidden tool used: %s", tool.Name))
		}
		for _, forbidden := range scenario.ForbidToolInputSubstr {
			if forbidden != "" && strings.Contains(tool.Input, forbidden) {
				hygieneFailures = append(hygieneFailures, fmt.Sprintf("tool input used forbidden substring %q", forbidden))
				break
			}
		}
	}
	if scenario.MaxAutoContinues >= 0 && result.AutoContinues > scenario.MaxAutoContinues {
		hygieneFailures = append(hygieneFailures, fmt.Sprintf("auto_continues=%d > %d", result.AutoContinues, scenario.MaxAutoContinues))
	}
	if result.MaxRoutedTools > 24 {
		hygieneFailures = append(hygieneFailures, fmt.Sprintf("max_routed_tools=%d > 24", result.MaxRoutedTools))
	}
	result.StatusPass = len(statusFailures) == 0
	result.ArtifactPass = len(artifactFailures) == 0
	result.FalseDone = strings.EqualFold(run.Status, "done") && !result.ArtifactPass
	result.PolicyViolations = countRunEvents(run.Events, "policy_block") + countRunEvents(run.Events, "control_transition_denied")
	result.HumanInterventions = countRunEvents(run.Events, "denied") + countRunEvents(run.Events, "confirmation_required")
	result.RecoveryEvents = countRunEvents(run.Events, "recovery") + countRunEvents(run.Events, "tool_error")
	result.Recovered = result.RecoveryEvents > 0 && strings.EqualFold(run.Status, "done")
	if result.PolicyViolations > 0 {
		hygieneFailures = append(hygieneFailures, fmt.Sprintf("policy_violations=%d", result.PolicyViolations))
	}
	if result.HumanInterventions > 0 {
		hygieneFailures = append(hygieneFailures, fmt.Sprintf("human_interventions=%d", result.HumanInterventions))
	}
	result.HygienePass = len(hygieneFailures) == 0
	failures := append([]string{}, statusFailures...)
	failures = append(failures, artifactFailures...)
	failures = append(failures, hygieneFailures...)
	if result.FalseDone {
		failures = append(failures, "false_done=true")
	}
	result.Pass = result.StatusPass && result.ArtifactPass && result.HygienePass
	result.FailReason = strings.Join(failures, "; ")
}

func toolSuccessRate(tools []TaskToolEvent) int {
	if len(tools) == 0 {
		return 100
	}
	success := 0
	for _, tool := range tools {
		switch strings.ToLower(strings.TrimSpace(tool.Status)) {
		case "done", "ok":
			success++
		case "skipped":
			if strings.Contains(strings.ToLower(tool.Result), "cached") || strings.Contains(strings.ToLower(tool.Result), "repeated") {
				success++
			}
		}
	}
	return (success * 100) / len(tools)
}

func repeatToolRate(metrics LoopMetrics) int {
	if metrics.ToolCalls <= 0 {
		return 0
	}
	repeats := metrics.RepeatedToolInputs + metrics.RepeatedSkips
	return (repeats * 100) / metrics.ToolCalls
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
