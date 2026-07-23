package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mauler/internal/settings"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func doctorTestClient(handler http.HandlerFunc) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := &doctorResponseRecorder{header: http.Header{}, status: http.StatusOK}
		handler(rec, r)
		return &http.Response{
			StatusCode: rec.status,
			Header:     rec.header,
			Body:       io.NopCloser(strings.NewReader(rec.body.String())),
			Request:    r,
		}, nil
	})}
}

type doctorResponseRecorder struct {
	header http.Header
	status int
	body   strings.Builder
}

func (r *doctorResponseRecorder) Header() http.Header { return r.header }

func (r *doctorResponseRecorder) Write(data []byte) (int, error) {
	return r.body.Write(data)
}

func (r *doctorResponseRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
}

func TestFetchLMStudioModelInfoReadsCapabilitiesAndLoadedContext(t *testing.T) {
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"models": [{
				"key": "qwen/qwen3.6-27b",
				"selected_variant": "qwen/qwen3.6-27b@UD-Q4_K_XL",
				"capabilities": ["tool_use", "reasoning"],
				"reasoning": {"type": "qwen3"},
				"loaded_instances": [{
					"id": "qwen/qwen3.6-27b:1",
					"config": {"context_length": 32768}
				}]
			}]
		}`))
	})

	model, found, err := fetchLMStudioModelInfoWithClient("http://doctor.test/v1", "qwen/qwen3.6-27b", client)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected model to be found")
	}
	if model.MaxLoadedContext() != 32768 {
		t.Fatalf("MaxLoadedContext = %d, want 32768", model.MaxLoadedContext())
	}
	if !hasCapability(model.Capabilities, "tool", "function") {
		t.Fatalf("expected tool capability: %#v", model.Capabilities)
	}
}

func TestLMStudioCapabilityChecksWarnWhenToolsMissing(t *testing.T) {
	var checks []DoctorCheck
	add := func(c DoctorCheck) { checks = append(checks, c) }

	addLMStudioCapabilityChecks(add, lmStudioDoctorModel{
		Key:          "qwen",
		Capabilities: []string{"completion", "reasoning"},
	}, settings.Profile{Thinking: true})

	var toolWarn, reasoningInfo bool
	for _, check := range checks {
		if check.Name == "LM Studio tool capability" && check.Status == "warn" && strings.Contains(check.Message, "does not report") {
			toolWarn = true
		}
		if check.Name == "LM Studio reasoning metadata" && check.Status == "info" {
			reasoningInfo = true
		}
	}
	if !toolWarn {
		t.Fatalf("expected missing tool capability warning: %#v", checks)
	}
	if !reasoningInfo {
		t.Fatalf("expected missing reasoning metadata info: %#v", checks)
	}
}

func TestFetchLlamacppBuiltinToolsDetectsDangerousServerTools(t *testing.T) {
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"server": {
				"tools": ["read", "grep_search"],
				"experimental": {"exec_shell_command": true}
			},
			"default_generation_settings": {
				"params": {"chat_format": "chatml-function-calling"}
			}
		}`))
	})

	tools, err := fetchLlamacppBuiltinToolsWithClient("http://doctor.test/v1", client)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(tools, ",")
	for _, want := range []string{"exec_shell_command", "read", "grep_search"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in detected tools %q", want, got)
		}
	}
}

func TestFetchLlamacppContextPrefersSlots(t *testing.T) {
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slots":
			_, _ = w.Write([]byte(`[{"id":0,"n_ctx":40960}]`))
		case "/props":
			_, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":8192}}`))
		default:
			http.NotFound(w, r)
		}
	})

	got, err := fetchLlamacppContextWithClient("http://doctor.test/v1", client)
	if err != nil {
		t.Fatal(err)
	}
	if got != 40960 {
		t.Fatalf("context = %d, want 40960", got)
	}
}

func TestFetchLlamacppContextReadsWrappedSlots(t *testing.T) {
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slots":
			_, _ = w.Write([]byte(`{"value":[{"id":0,"n_ctx":40192}]}`))
		case "/props":
			_, _ = w.Write([]byte(`{"default_generation_settings":{"n_ctx":8192}}`))
		default:
			http.NotFound(w, r)
		}
	})

	got, err := fetchLlamacppContextWithClient("http://doctor.test/v1", client)
	if err != nil {
		t.Fatal(err)
	}
	if got != 40192 {
		t.Fatalf("context = %d, want 40192", got)
	}
}

func TestFetchLlamacppContextFallsBackToInferenceBridgeStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slots":
			http.NotFound(w, r)
		case "/props":
			_, _ = w.Write([]byte(`{"default_generation_settings":{}}`))
		case "/v1/models/stats":
			_, _ = w.Write([]byte(`{"model":{"state":"Loaded","actual_ctx_tokens":65536}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := fetchLlamacppContext(server.URL + "/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 65536 {
		t.Fatalf("context = %d, want 65536", got)
	}
}

func TestInferenceBridgeAgentEndpointCheckAcceptsExistingRoutes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/reliability/agent-action/validate":
			_, _ = w.Write([]byte(`{"valid":true}`))
		case "/v1/messages":
			http.Error(w, `{"error":"missing max_tokens"}`, http.StatusBadRequest)
		case "/v1/embeddings":
			http.Error(w, `{"error":"no embedding model loaded"}`, http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	})

	check := probeInferenceBridgeAgentEndpoints(settings.Provider{
		Name:    "ib",
		Backend: "llamacpp",
		BaseURL: "http://127.0.0.1:8800/v1",
	}, doctorTestClient(handler))
	if check.Status != "ok" {
		t.Fatalf("status = %q, message = %q, detail = %q", check.Status, check.Message, check.Detail)
	}
	if !strings.Contains(check.Detail, "Anthropic /messages: ok HTTP 400") {
		t.Fatalf("expected messages route probe in detail, got %q", check.Detail)
	}
}

func TestInferenceBridgeAgentEndpointCheckWarnsOnMissingRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/reliability/agent-action/validate":
			_, _ = w.Write([]byte(`{"valid":true}`))
		case "/v1/messages":
			http.Error(w, `{"error":"missing max_tokens"}`, http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	check := probeInferenceBridgeAgentEndpoints(settings.Provider{
		Name:    "ib",
		Backend: "llamacpp",
		BaseURL: "http://127.0.0.1:8800/v1",
	}, doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Scheme = "http"
		r.URL.Host = "127.0.0.1:8800"
		server.Config.Handler.ServeHTTP(w, r)
	}))
	if check.Status != "warn" {
		t.Fatalf("status = %q, message = %q, detail = %q", check.Status, check.Message, check.Detail)
	}
	if !strings.Contains(check.Message, "OpenAI /embeddings") {
		t.Fatalf("expected missing embeddings route in message, got %q", check.Message)
	}
}

func TestSevereContextUndersizeFlagsEightKForFortyKProfile(t *testing.T) {
	if !severeContextUndersize(40000, 8192) {
		t.Fatal("40k requested with 8k actual must be a severe undersize")
	}
	if severeContextUndersize(40000, 32768) {
		t.Fatal("32k actual for 40k requested should be a warning, not a hard fail")
	}
}

func TestParseNvidiaSMIVRAM(t *testing.T) {
	gpus := parseNvidiaSMIVRAM("NVIDIA GeForce RTX 3090, 24576\nNVIDIA RTX 6000 Ada, 49140 MiB\n")
	if len(gpus) != 2 {
		t.Fatalf("gpus = %#v, want 2 parsed entries", gpus)
	}
	if gpus[0].Name != "NVIDIA GeForce RTX 3090" || gpus[0].TotalMiB != 24576 {
		t.Fatalf("first gpu = %#v", gpus[0])
	}
	if gpus[1].Name != "NVIDIA RTX 6000 Ada" || gpus[1].TotalMiB != 49140 {
		t.Fatalf("second gpu = %#v", gpus[1])
	}
}

func TestEstimateProfileVRAMFlagsVeryLargeContext(t *testing.T) {
	profile := settings.Profile{
		ModelID:   "gemma-4-31B-it-uncensored-heretic-Q4_K_S.gguf",
		CtxTokens: 120000,
	}
	huge, ok := estimateProfileVRAMMiB(profile)
	if !ok {
		t.Fatal("expected VRAM estimate")
	}
	profile.CtxTokens = 40000
	moderate, ok := estimateProfileVRAMMiB(profile)
	if !ok {
		t.Fatal("expected VRAM estimate")
	}
	if huge <= 24576 {
		t.Fatalf("120k context estimate = %d MiB, want over 24576 MiB", huge)
	}
	if moderate >= huge {
		t.Fatalf("40k estimate = %d MiB, huge estimate = %d MiB", moderate, huge)
	}
}

func TestSharedTerminalDoctorWarnsWhenBusy(t *testing.T) {
	var checks []DoctorCheck
	addSharedTerminalDoctorCheck(func(c DoctorCheck) { checks = append(checks, c) }, TerminalStateSnapshot{
		State:   "listener",
		Summary: "Shared terminal appears to be running a listener",
		Lines:   []string{"nc listening on 4444"},
	})
	if !hasDoctorCheck(checks, "Shared terminal state", "warn", "listener") {
		t.Fatalf("expected listener warning, got %#v", checks)
	}
	if !strings.Contains(checks[0].Detail, "terminal_read") {
		t.Fatalf("expected terminal guidance, got %q", checks[0].Detail)
	}
}

func TestFetchLlamacppBuiltinToolsIgnoresOrdinaryProps(t *testing.T) {
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"default_generation_settings": {
				"params": {
					"chat_format": "chatml-function-calling",
					"parse_tool_calls": true
				}
			}
		}`))
	})

	tools, err := fetchLlamacppBuiltinToolsWithClient("http://doctor.test/v1", client)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("detected tools = %#v, want none", tools)
	}
}

func TestProfileIdentityWarnsOnFamilyMismatch(t *testing.T) {
	var checks []DoctorCheck
	add := func(c DoctorCheck) { checks = append(checks, c) }

	addProfileIdentityChecks(add, "qwen3.6-think", settings.Profile{
		ModelID: "gemma-4-31B-it-uncensored-heretic-Q4_K_S.gguf",
	})

	if len(checks) != 1 || checks[0].Name != "Profile identity" || checks[0].Status != "warn" {
		t.Fatalf("expected profile identity warning, got %#v", checks)
	}
}

func TestAgentPresetBudgetCheckDocumentsWorkingBudgetOnly(t *testing.T) {
	var checks []DoctorCheck
	add := func(c DoctorCheck) { checks = append(checks, c) }
	cfg := settings.DefaultSettings()
	cfg.Agents.Presets = map[string]settings.AgentModePreset{
		"Builder": {Enabled: true, ContextBudget: 32768},
	}

	addAgentPresetBudgetChecks(add, cfg, settings.Profile{CtxTokens: 120000})

	if len(checks) != 1 || checks[0].Name != "Agent context budgets" || checks[0].Status != "info" {
		t.Fatalf("expected context budget info, got %#v", checks)
	}
	if !strings.Contains(checks[0].Detail, "not backend model loading") {
		t.Fatalf("detail should explain launch context separation: %#v", checks[0])
	}
}

func TestSharedBackendSubagentCheckWarnsWhenEnabled(t *testing.T) {
	var checks []DoctorCheck
	add := func(c DoctorCheck) { checks = append(checks, c) }
	cfg := settings.DefaultSettings()
	cfg.Tools.Enabled = true
	cfg.Tools.ActiveToolset = "unrestricted"
	cfg.Tools.EnabledTools["task"] = true

	addSharedBackendSubagentCheck(add, cfg, settings.Profile{Backend: "llamacpp"})

	if len(checks) != 1 || checks[0].Name != "Subagent backend isolation" || checks[0].Status != "info" {
		t.Fatalf("expected shared backend info check, got %#v", checks)
	}
	if !strings.Contains(checks[0].Detail, "lower-context subagent requests are now reused") {
		t.Fatalf("detail should mention lower-context reuse: %#v", checks[0])
	}
}

func TestSharedBackendSubagentCheckSkipsNonLlamaCpp(t *testing.T) {
	var checks []DoctorCheck
	addSharedBackendSubagentCheck(func(c DoctorCheck) { checks = append(checks, c) }, settings.DefaultSettings(), settings.Profile{Backend: "lmstudio"})

	if len(checks) != 0 {
		t.Fatalf("non-llamacpp provider should not emit shared backend check: %#v", checks)
	}
}

func TestLlamaLaunchSignalsFromProps(t *testing.T) {
	props := map[string]any{
		"launch_args": "--jinja --reasoning-format deepseek --flash-attn on",
		"default_generation_settings": map[string]any{
			"params": map[string]any{
				"cache_type_k": "q8_0",
			},
		},
		"speculative": map[string]any{
			"spec_type": "draft-mtp",
		},
	}
	got := llamaLaunchSignalsFromProps(props)
	if !got.HasJinja || !got.ReasoningDeep || !got.FlashAttention || !got.Speculative {
		t.Fatalf("missing expected launch signals: %#v", got)
	}
}

func TestLlamaLaunchSignalsIgnoresDisabledSpeculative(t *testing.T) {
	props := map[string]any{
		"use_jinja":  true,
		"flash_attn": true,
		"speculative": map[string]any{
			"spec_type": false,
			"draft_n":   float64(0),
		},
	}
	got := llamaLaunchSignalsFromProps(props)
	if !got.HasJinja || !got.FlashAttention {
		t.Fatalf("expected jinja/flash signals: %#v", got)
	}
	if got.Speculative {
		t.Fatalf("disabled speculative settings should not warn: %#v", got)
	}
}

func TestModelQuantTag(t *testing.T) {
	cases := map[string]string{
		"Qwen3.6-35B-A3B-Q4_K_S.gguf":     "q4_k_s",
		"qwen3.6-27b-UD-Q4_K_XL.gguf":     "ud-q4_k_xl",
		"/models/gemma-3-12b-q3_k_m.gguf": "q3_k_m",
		"no-quant-name":                   "",
	}
	for input, want := range cases {
		if got := modelQuantTag(input); got != want {
			t.Fatalf("modelQuantTag(%q)=%q want %q", input, got, want)
		}
	}
}

func TestLlamacppLaunchAssertionsWarnOnSpeculative(t *testing.T) {
	var checks []DoctorCheck
	props := map[string]any{
		"launch_args": "--jinja --reasoning-format deepseek --flash-attn on",
		"speculative": map[string]any{
			"spec_type": "draft-mtp",
		},
	}
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(props)
	})

	addLlamacppLaunchAssertionsWithClient(func(c DoctorCheck) { checks = append(checks, c) }, "http://doctor.test/v1", settings.Profile{ModelID: "qwen3.6"}, client)

	if !hasDoctorCheck(checks, "llama.cpp speculative decoding", "warn", "Speculative/draft decoding signal detected") {
		t.Fatalf("expected speculative warning, got %#v", checks)
	}
	if !hasDoctorCheck(checks, "llama.cpp Jinja", "ok", "Jinja/template signal detected") {
		t.Fatalf("expected jinja ok, got %#v", checks)
	}
}

func TestCheckLlamacppVersionUsesInferenceBridgeExactBuild(t *testing.T) {
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runtime/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"server_version": "b10075",
			"server_path":    `C:\runtime\llama-server.exe`,
		})
	})

	status, message, detail := checkLlamacppVersionWithClient("http://doctor.test/v1", client)
	if status != "ok" || message != "llama.cpp b10075 via InferenceBridge" {
		t.Fatalf("version check = %q, %q, %q", status, message, detail)
	}
	if !strings.Contains(detail, "llama-server.exe") {
		t.Fatalf("version detail = %q", detail)
	}
}

func TestCheckLlamacppVersionFallsBackToProps(t *testing.T) {
	client := doctorTestClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runtime/status":
			http.NotFound(w, r)
		case "/props":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})

	status, message, _ := checkLlamacppVersionWithClient("http://doctor.test/v1", client)
	if status != "ok" || !strings.Contains(message, "/props available") {
		t.Fatalf("version check = %q, %q", status, message)
	}
}

func TestShellNetworkBoundaryCheckWarnsForWindowsHostToolsWithWSL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific host/WSL boundary check")
	}
	var checks []DoctorCheck
	cfg := settings.DefaultSettings()
	cfg.Tools.ShellBackend = "wsl"
	cfg.Tools.ShellDistro = "kali-linux"
	cfg.Tools.ActiveToolset = "unrestricted"
	cfg.Tools.EnabledTools["fetch_url"] = true
	cfg.Tools.EnabledTools["browser"] = true

	addShellNetworkBoundaryCheck(func(c DoctorCheck) { checks = append(checks, c) }, cfg, "wsl")

	if len(checks) != 1 || checks[0].Name != "WSL/Kali target routing" || checks[0].Status != "warn" {
		t.Fatalf("expected WSL/Kali routing warning, got %#v", checks)
	}
	for _, want := range []string{"browser/fetch tools run from Windows", "kali-linux", "Use the local-code/offline toolset"} {
		if !strings.Contains(checks[0].Message+" "+checks[0].Detail, want) {
			t.Fatalf("expected check to mention %q, got %#v", want, checks[0])
		}
	}
}

func TestMasterSkillDoctorWarnsWhenAdapterMissingAndLoadAllPresent(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	sourceDir := t.TempDir()
	mustWrite(t, filepath.Join(sourceDir, "master_skill.md"), `# Master

You will read this whole document first.

## Recon

Use the methodology.
`)
	_, err := saveSkill(Skill{
		Name:        "master",
		Description: "Old master wrapper",
		Version:     "1.0.0",
		Tags:        []string{"master"},
		SourcePath:  filepath.ToSlash(sourceDir),
		Body:        "External master skill source.",
	})
	if err != nil {
		t.Fatal(err)
	}
	var checks []DoctorCheck
	addMasterSkillDoctorChecks(func(c DoctorCheck) { checks = append(checks, c) })

	if !hasDoctorCheck(checks, "Master skill adapter", "warn", "missing TheMauler/local-LLM adapter") {
		t.Fatalf("expected adapter warning, got %#v", checks)
	}
	if !hasDoctorCheck(checks, "Master skill load-all language", "warn", "read this whole document") {
		t.Fatalf("expected load-all warning, got %#v", checks)
	}
}

func TestMasterSkillDoctorAcceptsAdapterAndReportsLargeSource(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	sourceDir := t.TempDir()
	for i := 0; i < 51; i++ {
		mustWrite(t, filepath.Join(sourceDir, fmt.Sprintf("doc-%02d.md", i)), "# Doc\n\nSmall methodology note.")
	}
	_, err := saveSkill(Skill{
		Name:        "master",
		Description: "Adapter master wrapper",
		Version:     "1.0.0",
		Tags:        []string{"master"},
		SourcePath:  filepath.ToSlash(sourceDir),
		Body:        masterSkillAdapterBody(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var checks []DoctorCheck
	addMasterSkillDoctorChecks(func(c DoctorCheck) { checks = append(checks, c) })

	if !hasDoctorCheck(checks, "Master skill adapter", "ok", "adapter guidance is present") {
		t.Fatalf("expected adapter ok, got %#v", checks)
	}
	if !hasDoctorCheck(checks, "Master skill source", "info", "Large master source") {
		t.Fatalf("expected large source info, got %#v", checks)
	}
}

func hasDoctorCheck(checks []DoctorCheck, name, status, contains string) bool {
	for _, check := range checks {
		if check.Name != name || check.Status != status {
			continue
		}
		if strings.Contains(check.Message+" "+check.Detail, contains) {
			return true
		}
	}
	return false
}

func TestReadInferenceBridgeConfigPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inference-bridge.toml")
	if err := os.WriteFile(path, []byte(`
[models]
default_context = 8192

[server]
host = "127.0.0.1"
port = 8802
`), 0o600); err != nil {
		t.Fatal(err)
	}

	port, ok := readInferenceBridgeConfigPort(path)
	if !ok || port != "8802" {
		t.Fatalf("port=%q ok=%v, want 8802 true", port, ok)
	}
}
