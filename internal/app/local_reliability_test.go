package app

// Opt-in local-model qualification. Uses real transport/agent/tools/control
// code with isolated state, not the normal desktop or a mock model.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

var qualificationTools = map[string]bool{
	"read": true, "write": true, "edit": true, "glob": true, "grep": true, "todo_write": true,
}

type localQualificationRuntime struct {
	base, key, model string
	context          int
	http             *http.Client
}

func (r *localQualificationRuntime) get(path string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, r.base+"/"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.key)
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local %s status %d", path, resp.StatusCode)
	}
	var value map[string]any
	// Bridge status includes a bounded request-history view and can exceed 1 MiB
	// after repeated qualification. Never persist that payload; inspect metadata
	// only and keep an explicit finite response budget.
	err = json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(&value)
	if err != nil {
		return nil, fmt.Errorf("decode bounded local %s response: %w", path, err)
	}
	return value, nil
}

func (r *localQualificationRuntime) idle() bool {
	value, err := r.get("runtime/status")
	return err == nil && value["active_requests"] == float64(0) && value["queued_requests"] == float64(0)
}

func (r *localQualificationRuntime) unchangedAndIdle() bool {
	models, err := r.get("models")
	if err != nil {
		return false
	}
	loaded, same := 0, false
	for _, item := range models["data"].([]any) {
		model := item.(map[string]any)
		if model["active"] == true && model["state"] == "loaded" {
			loaded++
			same = model["id"] == r.model
		}
	}
	ctx, err := r.get("context/status")
	return err == nil && loaded == 1 && same && ctx["total_tokens"] == float64(r.context) && r.idle()
}

// Deliberately exposes only Client, never LoadModel/ForceLoadModel. Preflight
// verifies the already-loaded model/context. Qualification cannot swap models.
type localQualificationClient struct {
	llm.Client
	calls                 atomic.Int64
	started, output       chan struct{}
	startOnce, outputOnce sync.Once
	wg                    sync.WaitGroup
}

func (c *localQualificationClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	for _, tool := range req.Tools {
		if !qualificationTools[tool.Function.Name] {
			return nil, fmt.Errorf("qualification refuses unscoped advertised tool %q", tool.Function.Name)
		}
	}
	c.calls.Add(1)
	c.startOnce.Do(func() { close(c.started) })
	stream, err := c.Client.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan llm.Delta)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case delta, ok := <-stream:
				if !ok {
					return
				}
				if delta.Content != "" {
					c.outputOnce.Do(func() { close(c.output) })
				}
				select {
				case out <- delta:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (c *localQualificationClient) joined() bool {
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

func qualificationApp(t *testing.T, root string, runtime *localQualificationRuntime) (*App, settings.Profile, *localQualificationClient, func()) {
	t.Helper()
	// Keep the disposable workspace outside the source checkout so its ancestor
	// manifests/AGENTS cannot contaminate the model's fixture context.
	workspace, err := os.MkdirTemp("", "mauler-local-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("# Local qualification fixture\nComplete only the requested bounded task in this folder. No shell, browser, remote target or external service access.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAULER_CONFIG_DIR", filepath.Join(root, "config"))
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAULER_LOCAL_EVAL_KEY", runtime.key)
	cfg := canonicalAgentEvalSettings(settings.DefaultSettings())
	cfg.ActiveProfile = "local-qualification"
	cfg.Context.WorkspaceDir = workspace
	cfg.Context.MAULERMDPath = filepath.Join(workspace, "AGENTS.md")
	cfg.Context.OpenFolders = nil
	cfg.Agents.OfflineOnly = true
	cfg.Agents.EscalationProfile = ""
	cfg.Agents.MaxRunSeconds = 90
	cfg.Agents.MaxToolCalls = 16
	cfg.Agents.ThinkingMode = "off"
	cfg.Agents.ReasoningEffort = "none"
	cfg.Memory.Enabled, cfg.Memory.AutoInject, cfg.Memory.DisableAutoDistill = false, false, true
	cfg.Skills.Enabled, cfg.Skills.AutoInject = false, false
	cfg.Tools.EnabledTools = map[string]bool{}
	names := []string{}
	for name := range qualificationTools {
		cfg.Tools.EnabledTools[name] = true
		names = append(names, name)
	}
	cfg.Tools.Toolsets = map[string][]string{"qualification": names, "unrestricted": names}
	cfg.Tools.ActiveToolset = "qualification"
	for name, preset := range cfg.Agents.Presets {
		preset.Profile = ""
		preset.Toolset = "qualification"
		cfg.Agents.Presets[name] = preset
	}
	profile := settings.Profile{
		Name: cfg.ActiveProfile, Provider: "local-qualification", Backend: "llamacpp", BaseURL: runtime.base,
		APIKeyEnv: "MAULER_LOCAL_EVAL_KEY", ModelID: runtime.model, CtxTokens: runtime.context,
		NoThink: settings.GenerationParams{Temperature: 0.1, TopP: 0.95, TopK: 20, MaxTokens: 2048, Seed: -1},
	}
	profiles := settings.ProfilesFile{Profiles: map[string]settings.Profile{profile.Name: profile}, Providers: map[string]settings.Provider{
		"local-qualification": {Name: "local-qualification", Backend: "llamacpp", BaseURL: runtime.base, APIKeyEnv: "MAULER_LOCAL_EVAL_KEY"},
	}}
	native, err := buildClient(profile)
	if err != nil {
		t.Fatal(err)
	}
	client := &localQualificationClient{Client: native, started: make(chan struct{}), output: make(chan struct{})}
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(selected settings.Profile) (llm.Client, error) {
		if selected.ModelID != runtime.model || selected.BaseURL != runtime.base {
			return nil, fmt.Errorf("qualification refuses provider/model change")
		}
		return client, nil
	}
	a := &App{cfg: &cfg, profiles: &profiles, suppressEvents: true, history: agent.NewHistory(runtime.context),
		rollback: &agent.Rollback{}, registry: tools.New(), autoAgents: true, autonomous: true, conversationMode: conversationModeAdaptive,
		bgJobs: make(map[string]*bgJob), shellSessions: make(map[string]*shellSession), contextWindow: runtime.context, configuredContextWindow: runtime.context}
	a.conversationEpoch.Store(1)
	a.registerAppTools()
	for _, tool := range a.registry.All() {
		if !qualificationTools[tool.Name()] {
			a.registry.Unregister(tool.Name())
		}
	}
	restoreTools := tools.SwapConfigSnapshot(cfg.Tools)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	return a, profile, client, func() { restoreTools(); buildClientForAgent = oldBuilder; _ = os.Chdir(oldWD) }
}

func qualificationRun(a *App, profile settings.Profile, prompt string) (context.CancelFunc, <-chan TaskRun) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	a.mu.Lock()
	a.agentRunning = true
	a.cancelAgent = cancel
	a.stopReason, a.stopDetail = "", ""
	cfg := *a.cfg
	a.mu.Unlock()
	mode := selectAgentMode(prompt, cfg)
	run := startTaskRun(prompt, mode.Name, profile.Name, profile.ModelID)
	run.setAgentDefinition(mode)
	run.Origin = "desktop"
	run.ConversationEpoch = a.currentConversationEpoch()
	run.conversationMode = conversationModeAdaptive
	run.RoutingPrompt = prompt
	done := make(chan TaskRun, 1)
	go func() {
		done <- a.runAgentLoop(ctx, llm.NewTextMessage(llm.RoleUser, prompt), profile, &cfg, true, mode, nil, nil, run)
	}()
	return cancel, done
}

func qualificationResult(done <-chan TaskRun) (TaskRun, bool) {
	select {
	case run := <-done:
		return run, true
	case <-time.After(105 * time.Second):
		return TaskRun{}, false
	}
}

func TestLocalReliabilityQualification(t *testing.T) {
	base, key, output := os.Getenv("MAULER_LOCAL_EVAL_URL"), os.Getenv("MAULER_LOCAL_EVAL_KEY"), os.Getenv("MAULER_LOCAL_EVAL_OUTPUT")
	if base == "" || key == "" || output == "" {
		t.Skip("operator-run local-model qualification requires explicit loopback URL, ephemeral key and isolated output")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || !filepath.IsAbs(output) {
		t.Fatal("invalid local qualification boundary")
	}
	repeats, _ := strconv.Atoi(os.Getenv("MAULER_LOCAL_EVAL_REPEATS"))
	if repeats < 1 || repeats > 5 {
		t.Fatal("explicit repetitions must be 1..5")
	}
	runtime := &localQualificationRuntime{base: strings.TrimRight(base, "/"), key: key, http: &http.Client{
		Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if !runtime.idle() {
		status, statusErr := runtime.get("runtime/status")
		t.Fatalf("local backend must be idle: active=%v queued=%v error=%v", status["active_requests"], status["queued_requests"], statusErr)
	}
	models, err := runtime.get("models")
	if err != nil {
		t.Fatal(err)
	}
	loaded := 0
	for _, item := range models["data"].([]any) {
		model := item.(map[string]any)
		if model["active"] == true && model["state"] == "loaded" {
			runtime.model, _ = model["id"].(string)
			loaded++
		}
	}
	contextState, err := runtime.get("context/status")
	if err != nil {
		t.Fatal(err)
	}
	runtime.context = int(contextState["total_tokens"].(float64))
	if loaded != 1 || runtime.model == "" || runtime.context < 8192 {
		t.Fatal("exactly one already-loaded local model/context required")
	}
	processStateMu.Lock()
	defer processStateMu.Unlock()
	type scenario struct {
		name, prompt                    string
		expects                         []string
		action, write, append, followup bool
		cancelPhase                     string
	}
	scenarios := []scenario{
		{name: "greeting", prompt: "Hello! Please greet me briefly."},
		{name: "simple-answer", prompt: "What is 7 plus 5? Answer briefly.", expects: []string{"12"}},
		{name: "normal-writing", prompt: "Write a short message telling my EHC guild how proud I am that we took a level 9 planet and I was awarded Prince. Just draft the message in chat; no files or tools.", expects: []string{"proud"}},
		{name: "advice-only", prompt: "Explain how I could list files in a folder on Windows. Advice only; do not inspect or access any files."},
		{name: "list-files", prompt: "List the files in my workspace.", expects: []string{"alpha.txt", "beta.txt", "notes.txt"}, action: true},
		{name: "list-paraphrase", prompt: "Can you tell me what's in this folder?", expects: []string{"alpha.txt", "beta.txt", "notes.txt"}, action: true},
		{name: "explicit-file-tools", prompt: "Use your file tools to list the items in the current working folder.", expects: []string{"alpha.txt", "beta.txt", "notes.txt"}, action: true},
		{name: "read-file", prompt: "Read notes.txt and tell me the exact marker stored in it.", action: true},
		{name: "read-followup", prompt: "Read notes.txt and tell me the exact marker stored in it.", action: true, followup: true},
		{name: "write-verify-finalize", write: true},
		{name: "append-verify-finalize", write: true, append: true},
		{name: "cancel-startup", cancelPhase: "startup"},
		{name: "cancel-output", cancelPhase: "output"},
	}
	results := []map[string]any{}
	for _, s := range scenarios {
		for repetition := 1; repetition <= repeats; repetition++ {
			t.Run(fmt.Sprintf("%s-%d", s.name, repetition), func(t *testing.T) {
				root, err := os.MkdirTemp(output, s.name+"-")
				if err != nil {
					t.Fatal(err)
				}
				a, profile, client, restore := qualificationApp(t, root, runtime)
				defer restore()
				marker := fmt.Sprintf("MAULER_NOTE_%d_%d", time.Now().UnixNano(), repetition)
				for name, content := range map[string]string{"alpha.txt": "alpha\n", "beta.txt": "beta\n", "notes.txt": marker + "\n"} {
					if err := os.WriteFile(name, []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
				prompt := s.prompt
				prefix, suffix := "retained prefix: café / 日本語\n", "literal appended suffix: \"alpha\" / λ\n"
				expected := ""
				if s.write {
					expected = fmt.Sprintf("LOCAL_WRITE_%d\nQuoted: \"alpha\"; path: C:\\work\\fixture\nUnicode: café / λ / 日本語\nEND_LOCAL_WRITE_%d\n", repetition, repetition)
					if s.append {
						expected = prefix + suffix
						_ = os.WriteFile("artifact.txt", []byte(prefix), 0600)
						prompt = "Append exactly the text represented by this JSON string to artifact.txt, preserving every existing byte: " + strconv.Quote(suffix) + ". Use append=true, then read back and verify the complete file. Finish the handoff in chat. Do not use shell or create any other deliverable."
					} else {
						prompt = "Create artifact.txt containing exactly the text represented by this JSON string: " + strconv.Quote(expected) + ". Then read back and verify the complete file, and finish the handoff in chat. Do not use shell or create any other deliverable."
					}
				}
				if s.cancelPhase != "" {
					prompt = "Write a continuous numbered list from 1 to 5000, one number per line. No tools, no files, no commentary."
				}
				cancel, done := qualificationRun(a, profile, prompt)
				observed := true
				if s.cancelPhase != "" {
					signal := client.started
					if s.cancelPhase == "output" {
						signal = client.output
					}
					select {
					case <-signal:
					case <-time.After(20 * time.Second):
						observed = false
					}
					active := false
					deadline := time.Now().Add(10 * time.Second)
					for time.Now().Before(deadline) {
						if !runtime.idle() {
							active = true
							break
						}
						time.Sleep(20 * time.Millisecond)
					}
					observed = observed && active
					a.StopAgent()
				}
				run, joined := qualificationResult(done)
				cancel()
				calls := client.calls.Load()
				pass := joined && client.joined() && calls > 0
				if s.cancelPhase == "" {
					pass = pass && strings.TrimSpace(run.Response) != ""
				}
				for _, want := range s.expects {
					pass = pass && strings.Contains(strings.ToLower(run.Response), strings.ToLower(want))
				}
				if s.cancelPhase == "" {
					pass = pass && run.Status == "done"
				}
				if s.action {
					observedFile := false
					for _, tool := range run.Tools {
						observedFile = observedFile || tool.Status == "done" && (tool.Name == "read" || tool.Name == "glob" || tool.Name == "grep")
					}
					pass = pass && observedFile
				}
				if !s.action && !s.write {
					pass = pass && len(run.Tools) == 0
				}
				if strings.HasPrefix(s.name, "read-") {
					pass = pass && strings.Contains(run.Response, marker)
				}
				record := map[string]any{"case": s.name, "repetition": repetition, "modelCalls": calls, "run": run, "evidence": root, "joined": joined}
				if s.followup {
					nextCancel, nextDone := qualificationRun(a, profile, "What did that file say?")
					next, nextJoined := qualificationResult(nextDone)
					nextCancel()
					record["followup"] = next
					pass = pass && nextJoined && next.Status == "done" && strings.Contains(next.Response, marker) && client.calls.Load() > calls
				}
				if s.write {
					actual, readErr := os.ReadFile("artifact.txt")
					record["actualContent"], record["expectedContent"] = string(actual), expected
					readback, wrote := false, false
					for _, tool := range run.Tools {
						if tool.Status == "done" {
							readback = readback || tool.Name == "read" && strings.Contains(tool.Input, "artifact.txt")
							wrote = wrote || tool.Name == "write" && strings.Contains(tool.Input, "artifact.txt")
						}
					}
					pass = pass && readErr == nil && string(actual) == expected && readback && wrote && len(run.FinalizedArtifacts) > 0
					var latestReview []VerifyVerdict
					for _, event := range run.Events {
						if event.Kind == "review_gate" {
							_ = json.Unmarshal([]byte(event.Detail), &latestReview)
						}
					}
					record["blockingReviewRemaining"] = len(blockingReviewVerdicts(latestReview)) > 0
					pass = pass && len(blockingReviewVerdicts(latestReview)) == 0
				}
				if s.cancelPhase != "" {
					deadline := time.Now().Add(10 * time.Second)
					for !runtime.idle() && time.Now().Before(deadline) {
						time.Sleep(30 * time.Millisecond)
					}
					stopped := run.Status == "stopped" && run.StopReason == "user_stopped" && runtime.idle()
					record["idleAfterStop"], record["typedStopAccepted"] = runtime.idle(), stopped
					recoveryMarker := fmt.Sprintf("MAULER_RECOVERED_%d", repetition)
					nextCancel, nextDone := qualificationRun(a, profile, "Reply exactly "+recoveryMarker+" and nothing else.")
					next, nextJoined := qualificationResult(nextDone)
					nextCancel()
					record["stopObserved"], record["recovery"], record["recoveryModelProven"] = observed, next, client.calls.Load() > calls
					record["recoveryJoined"], record["idleAfterRecovery"] = nextJoined, runtime.idle()
					pass = pass && observed && stopped && nextJoined && next.Status == "done" && strings.TrimSpace(next.Response) == recoveryMarker && client.calls.Load() > calls
				}
				pass = pass && client.joined()
				record["pass"] = pass
				results = append(results, record)
				encoded, _ := json.MarshalIndent(record, "", "  ")
				if err := os.WriteFile(filepath.Join(root, "result.json"), encoded, 0600); err != nil {
					t.Fatal(err)
				}
				t.Logf("MAULER_LOCAL %s %d/%d: %t status=%s tools=%d calls=%d", s.name, repetition, repeats, pass, run.Status, len(run.Tools), client.calls.Load())
				if !pass {
					t.Errorf("local case did not meet its evidence contract; inspect %s", root)
				}
			})
		}
	}
	passed := 0
	for _, result := range results {
		if result["pass"] == true {
			passed++
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for !runtime.unchangedAndIdle() && time.Now().Before(deadline) {
		time.Sleep(40 * time.Millisecond)
	}
	report := map[string]any{"schemaVersion": "mauler-local-reliability-v2", "localOnly": true, "productionAppStarted": false,
		"model": runtime.model, "contextTokens": runtime.context, "repeats": repeats, "passed": passed, "total": len(results),
		"modelUnchangedAndIdle": runtime.unchangedAndIdle(), "cases": results,
		"limits": []string{"Headless production agent loop, not native Wails UI acceptance.", "Read-only model discovery; model loading/reconfiguration is deliberately unavailable.", "Isolated canonical evaluation envelope; production settings and secrets are not changed.", "Physical reboot/power loss and browser/remote actions are not covered."},
	}
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(output, "report.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("MAULER_LOCAL report=%s passed=%d/%d", filepath.Join(output, "report.json"), passed, len(results))
	if passed != len(results) || !runtime.unchangedAndIdle() {
		t.Fatalf("inspect retained local qualification evidence: %d/%d", passed, len(results))
	}
}
