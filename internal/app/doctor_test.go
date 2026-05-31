package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mauler/internal/settings"
)

func TestFetchLMStudioModelInfoReadsCapabilitiesAndLoadedContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer server.Close()

	model, found, err := fetchLMStudioModelInfo(server.URL+"/v1", "qwen/qwen3.6-27b")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer server.Close()

	tools, err := fetchLlamacppBuiltinTools(server.URL + "/v1")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"default_generation_settings": {
				"params": {
					"chat_format": "chatml-function-calling",
					"parse_tool_calls": true
				}
			}
		}`))
	}))
	defer server.Close()

	tools, err := fetchLlamacppBuiltinTools(server.URL + "/v1")
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
