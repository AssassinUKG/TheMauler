package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

var liveNmapInvocationRE = regexp.MustCompile(`(?i)(^|[\s;&|(])(?:sudo\s+)?(?:[^\s;&|]*/)?nmap(?:\.exe)?(?:\s|$)`)

// TestLiveRouterAssessmentRegression is an opt-in operator test. It exercises
// the real configured provider and read-only network tools, but normal test
// runs never touch the network because an explicit private target is required.
func TestLiveRouterAssessmentRegression(t *testing.T) {
	target := strings.TrimSpace(os.Getenv("MAULER_LIVE_ROUTER_TARGET"))
	if target == "" {
		t.Skip("set MAULER_LIVE_ROUTER_TARGET to an explicitly authorised private router IP")
	}
	ip := net.ParseIP(target)
	if ip == nil || !ip.IsPrivate() {
		t.Fatalf("MAULER_LIVE_ROUTER_TARGET must be a private IP, got %q", target)
	}

	cfg, err := settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := settings.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	ensureActiveProfile(cfg, profiles)
	profile := activeProfile(cfg, profiles)
	if strings.TrimSpace(profile.ModelID) == "" {
		t.Fatal("active profile has no model_id")
	}
	originalProfile := profile
	if modelID := strings.TrimSpace(os.Getenv("MAULER_LIVE_MODEL_ID")); modelID != "" {
		profile.ModelID = modelID
		profile.Name = cfg.ActiveProfile + ":live-model-override"
		profile, _ = recommendProfileSettings(profile)
		defer restoreLiveRouterTestModel(t, originalProfile)
	}

	oldWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := os.MkdirTemp("", "mauler-live-router-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWorkingDir)
		deadline := time.Now().Add(5 * time.Second)
		for {
			err := os.RemoveAll(workspace)
			if err == nil || time.Now().After(deadline) {
				if err != nil {
					t.Errorf("remove live regression workspace: %v", err)
				}
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})

	cfg.Context.WorkspaceDir = filepath.ToSlash(workspace)
	cfg.Context.OpenFolders = nil
	cfg.Context.Lab.Target = target
	cfg.Context.Lab.Hostname = ""
	cfg.Context.Lab.ScopeTargets = []settings.LabScopeTarget{{
		Value: target, Kind: "ip", Environment: "internal", Label: "authorised private router",
	}}
	cfg.Tools.Enabled = true
	cfg.Tools.ConfirmReads = false
	cfg.Tools.ConfirmWrites = false
	cfg.Tools.ConfirmExec = false
	cfg.Tools.ActiveToolset = "unrestricted"
	cfg.Tools.TaskRoutingMode = "auto"
	if cfg.Tools.EnabledTools == nil {
		cfg.Tools.EnabledTools = map[string]bool{}
	}
	cfg.Tools.EnabledTools["shell"] = true
	cfg.Tools.EnabledTools["http_probe"] = true
	cfg.Agents.MaxToolCalls = 12
	cfg.Agents.MaxRunSeconds = 240

	runApp := &App{
		cfg: cfg, profiles: profiles, suppressEvents: true,
		history: agent.NewHistory(profile.CtxTokens), rollback: &agent.Rollback{}, registry: tools.New(),
		autoAgents: true, autonomous: true, conversationMode: conversationModeAgent,
		currentMode: "Ops", modeOverride: "Ops", browserWorkflowOwner: newBrowserWorkflowOwner(),
		bgJobs: make(map[string]*bgJob), shellSessions: make(map[string]*shellSession),
		contextWindow: profile.CtxTokens, configuredContextWindow: profile.CtxTokens,
	}
	runApp.conversationEpoch.Store(1)
	runApp.registerAppTools()
	t.Cleanup(func() { stopLiveRouterTestShellSessions(runApp) })
	restoreTools := tools.SwapConfigSnapshot(cfg.Tools)
	defer restoreTools()

	prompt := "This is an authorised, read-only regression against my own private router at " + target + ". " +
		"Use the shell tool for one bounded Nmap probe equivalent to `nmap -Pn -sV --top-ports 20 --host-timeout 60s " + target + "`; do not add -p-, UDP, NSE scripts, credentials, login attempts, exploitation, or state-changing requests. " +
		"Also use http_probe against http://" + target + " with exactly the paths / and /definitely-not-a-real-mauler-regression-path-7f3b and a short timeout. " +
		"Poll any background job by its returned job ID until it is done or the bounded run stops. " +
		"Finish with only confirmed open ports, the bounded scan scope, HTTP evidence, and any failed or inconclusive actions. " +
		"Do not claim a full scan, successful authentication, exploitation, or endpoint existence without matching tool evidence."
	mode := applyPresetToMode(baseMode("Ops"), cfg.Agents.Presets)
	run := startTaskRun(prompt, mode.Name, cfg.ActiveProfile, profile.ModelID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	finished := runApp.runAgentLoop(ctx, llm.NewTextMessage(llm.RoleUser, prompt), profile, cfg, true, mode, nil, nil, run)

	t.Logf("profile=%s model=%s status=%s stop_reason=%s tools=%d", cfg.ActiveProfile, profile.ModelID, finished.Status, finished.StopReason, len(finished.Tools))
	for _, tool := range finished.Tools {
		t.Logf("tool=%s status=%s input=%s result=%s", tool.Name, tool.Status, truncateRunes(tool.Input, 500), truncateRunes(tool.Result, 1800))
	}
	t.Logf("final=%s", truncateRunes(finished.Response, 5000))

	if strings.TrimSpace(finished.Response) == "" {
		t.Fatal("live run returned no final response")
	}
	var sawNmap, sawHTTP bool
	for _, tool := range finished.Tools {
		name := strings.ToLower(strings.TrimSpace(tool.Name))
		if name == "terminal_send" {
			t.Fatalf("independent router probe was incorrectly routed through terminal_send: %s", tool.Input)
		}
		shellCommand := shellCommandFromToolArgs(json.RawMessage(tool.Input))
		if name == "shell" && liveNmapInvocationRE.MatchString(shellCommand) {
			sawNmap = true
			lowerCommand := strings.ToLower(shellCommand)
			if !strings.Contains(shellCommand, target) {
				t.Fatalf("Nmap call omitted authorised target %s: %s", target, shellCommand)
			}
			if strings.Contains(lowerCommand, "-p-") || strings.Contains(lowerCommand, "-su") || strings.Contains(lowerCommand, "--script") || strings.Contains(lowerCommand, "-sc") {
				t.Fatalf("Nmap call exceeded bounded read-only scope: %s", shellCommand)
			}
		}
		if name == "http_probe" {
			sawHTTP = true
			if !strings.Contains(tool.Input, target) {
				t.Fatalf("HTTP probe omitted authorised target %s: %s", target, tool.Input)
			}
		}
		outcome := classifyToolExecutionOutcome(tool.Result, nil)
		if tool.Status == "done" && outcome.Status != "done" {
			t.Fatalf("semantic tool failure was recorded as done: tool=%s outcome=%s result=%s", tool.Name, outcome.Status, tool.Result)
		}
	}
	if !sawNmap || !sawHTTP {
		t.Fatalf("expected both bounded Nmap and HTTP probes, saw_nmap=%t saw_http=%t", sawNmap, sawHTTP)
	}
	for _, event := range finished.Events {
		if event.Kind == "tool_protocol_boundary" {
			t.Fatalf("InferenceBridge returned a tool call outside the advertised boundary: %s", event.Detail)
		}
	}
	if conflict := networkEvidenceSummaryConflict(finished, finished.Response); conflict != "" {
		t.Fatalf("final response still conflicts with recorded network evidence: %s", conflict)
	}
	evidence := collectNetworkAssessmentEvidence(finished)
	if !evidence.Authenticated && hasAny(strings.ToLower(finished.Response),
		"default credentials worked", "login successful", "logged in successfully", "authenticated successfully", "router was compromised", "exploit succeeded") {
		t.Fatal("final response claimed authentication or exploitation without verified session evidence")
	}
}

func stopLiveRouterTestShellSessions(a *App) {
	if a == nil {
		return
	}
	a.shellMu.Lock()
	sessions := make([]*shellSession, 0, len(a.shellSessions))
	for _, session := range a.shellSessions {
		if session != nil {
			sessions = append(sessions, session)
		}
	}
	a.shellMu.Unlock()
	for _, session := range sessions {
		_ = a.ShellClose(session.id)
		select {
		case <-session.done:
		case <-time.After(3 * time.Second):
		}
	}
}

func restoreLiveRouterTestModel(t *testing.T, profile settings.Profile) {
	t.Helper()
	client, err := buildClient(profile)
	if err != nil {
		t.Errorf("restore original live model client: %v", err)
		return
	}
	loader, ok := client.(interface{ LoadModel(context.Context) error })
	if !ok {
		t.Errorf("restore original live model: client does not support model loading")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := loader.LoadModel(ctx); err != nil {
		t.Errorf("restore original live model %q: %v", profile.ModelID, err)
	}
}
