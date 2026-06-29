package app

import (
	"io"
	"net/http"
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
				"tools": ["read_file", "grep_search"],
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
	for _, want := range []string{"exec_shell_command", "read_file", "grep_search"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in detected tools %q", want, got)
		}
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
	cfg.Tools.ActiveToolset = "balanced"
	cfg.Tools.EnabledTools["subagent_research"] = true

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

func TestShellNetworkBoundaryCheckWarnsForWindowsHostToolsWithWSL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific host/WSL boundary check")
	}
	var checks []DoctorCheck
	cfg := settings.DefaultSettings()
	cfg.Tools.ShellBackend = "wsl"
	cfg.Tools.ShellDistro = "kali-linux"
	cfg.Tools.ActiveToolset = "balanced"

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
