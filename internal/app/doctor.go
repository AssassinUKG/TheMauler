package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"mauler/internal/runtimeprofile"
	"mauler/internal/settings"
	"mauler/internal/tools"
)

// DoctorResult is the full health report returned to the frontend.
type DoctorResult struct {
	Checks []DoctorCheck `json:"checks"`
	Score  int           `json:"score"` // 0-100
	Grade  string        `json:"grade"` // OK | WARN | FAIL
}

// DoctorCheck is a single health check item.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok | warn | fail | info
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// RunDoctor performs all health checks and returns the report.
func (a *App) RunDoctor() DoctorResult {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()
	profiles, _ := settings.LoadProfiles()

	var checks []DoctorCheck
	add := func(c DoctorCheck) { checks = append(checks, c) }

	// ── 1. Active provider reachability ──────────────────────────────────────
	activeProfile := profiles.Profiles[cfg.ActiveProfile]
	if strings.TrimSpace(cfg.ActiveProfile) == "" {
		add(DoctorCheck{
			Name:    "Active profile",
			Status:  "fail",
			Message: "No active profile is configured",
			Detail:  "Choose a profile in the status bar or Settings > Profiles.",
		})
	} else if strings.TrimSpace(activeProfile.ModelID) == "" {
		add(DoctorCheck{
			Name:    "Active profile",
			Status:  "fail",
			Message: fmt.Sprintf("Active profile %q is missing or has no model_id", cfg.ActiveProfile),
			Detail:  "Check profiles.toml or select a valid profile in the status bar.",
		})
	} else {
		add(DoctorCheck{
			Name:    "Active profile",
			Status:  "ok",
			Message: fmt.Sprintf("%s -> %s", cfg.ActiveProfile, activeProfile.ModelID),
		})
	}
	providerName := activeProfile.Provider
	provider, hasProvider := profiles.Providers[providerName]
	if hasProvider {
		activeProfile = applyProvider(activeProfile, profiles)
	}
	if !hasProvider {
		add(DoctorCheck{
			Name:    "Active provider",
			Status:  "fail",
			Message: fmt.Sprintf("Profile %q references provider %q which does not exist in profiles.toml", cfg.ActiveProfile, providerName),
		})
	} else {
		if providerHost := providerHostLabel(provider.BaseURL); providerHost != "" && providerHost != "localhost" && providerHost != "127.0.0.1" {
			add(DoctorCheck{
				Name:    "Provider host",
				Status:  "info",
				Message: fmt.Sprintf("Active provider uses %s", providerHost),
				Detail:  "If InferenceBridge is running on this same Windows machine, localhost/127.0.0.1 is usually less fragile than a LAN IP.",
			})
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client, err := buildClient(activeProfile)
		if err != nil {
			add(DoctorCheck{Name: "Active provider", Status: "fail", Message: err.Error()})
		} else if err := client.Ping(ctx); err != nil {
			add(DoctorCheck{
				Name:    "Active provider",
				Status:  "fail",
				Message: fmt.Sprintf("%s (%s) is unreachable", provider.Name, provider.BaseURL),
				Detail:  err.Error(),
			})
		} else {
			add(DoctorCheck{
				Name:    "Active provider",
				Status:  "ok",
				Message: fmt.Sprintf("%s (%s) is reachable", provider.Name, provider.BaseURL),
			})
		}
	}

	// ── 2. llama.cpp version ─────────────────────────────────────────────────
	if hasProvider && provider.Backend == "llamacpp" {
		versionOK, versionMsg, versionDetail := checkLlamacppVersion(provider.BaseURL)
		add(DoctorCheck{
			Name:    "llama.cpp version",
			Status:  versionOK,
			Message: versionMsg,
			Detail:  versionDetail,
		})
	} else if hasProvider && provider.Backend == "lmstudio" {
		add(DoctorCheck{
			Name:    "llama.cpp version",
			Status:  "info",
			Message: "Using LM Studio — llama.cpp version check not applicable",
		})
	}

	if hasProvider && provider.Backend == "llamacpp" {
		chatFormat, caps, err := fetchLlamacppChatFormat(provider.BaseURL)
		if err != nil {
			add(DoctorCheck{
				Name:    "Tool-call format",
				Status:  "warn",
				Message: "Could not read llama.cpp chat format",
				Detail:  err.Error(),
			})
		} else if strings.EqualFold(chatFormat, "Content-only") {
			detail := "TheMauler will repair common native tool text, but a server-side OpenAI tool parser is more reliable."
			if caps.SupportsTools || caps.SupportsToolCalls {
				detail = "The active template advertises tool support, but the backend reports Content-only output. InferenceBridge/llama.cpp is applying Jinja but not converting native tool text into OpenAI tool_calls, so TheMauler must repair streamed text."
			}
			add(DoctorCheck{
				Name:    "Tool-call format",
				Status:  "warn",
				Message: "Backend reports chat_format=Content-only; native tool_calls may be emitted as text",
				Detail:  detail,
			})
		} else {
			add(DoctorCheck{
				Name:    "Tool-call format",
				Status:  "ok",
				Message: fmt.Sprintf("chat_format=%s", chatFormat),
			})
		}

		builtinTools, err := fetchLlamacppBuiltinTools(provider.BaseURL)
		if err != nil {
			add(DoctorCheck{
				Name:    "llama.cpp server tools",
				Status:  "info",
				Message: "Could not inspect llama.cpp server-side built-in tools",
				Detail:  err.Error(),
			})
		} else if len(builtinTools) > 0 {
			add(DoctorCheck{
				Name:    "llama.cpp server tools",
				Status:  "warn",
				Message: "llama.cpp server-side built-in tools appear enabled: " + strings.Join(builtinTools, ", "),
				Detail:  "Keep TheMauler's own gated tools as the authority. Server-side file/shell tools can bypass confirmations, rollback, and task logs.",
			})
		} else {
			add(DoctorCheck{
				Name:    "llama.cpp server tools",
				Status:  "ok",
				Message: "No dangerous server-side built-in tools detected",
			})
		}
	}

	if hasProvider && provider.Backend == "llamacpp" {
		template, err := fetchActiveModelTemplate(provider.BaseURL, activeProfile.ModelID)
		if err != nil {
			add(DoctorCheck{
				Name:    "Model template",
				Status:  "warn",
				Message: "Could not read active model template metadata",
				Detail:  err.Error(),
			})
		} else if strings.Contains(strings.ToLower(template.Source), "fallback") {
			add(DoctorCheck{
				Name:    "Model template",
				Status:  "warn",
				Message: fmt.Sprintf("Active model uses fallback template: %s", template.Source),
				Detail:  "Fallback templates are a common cause of content-only or malformed tool calls. Prefer a model/server setup with a tool-use template.",
			})
		} else if template.Source != "" {
			add(DoctorCheck{
				Name:    "Model template",
				Status:  "ok",
				Message: fmt.Sprintf("template_source=%s", template.Source),
			})
		}
	} else if hasProvider && provider.Backend == "lmstudio" {
		model, found, err := fetchLMStudioModelInfo(provider.BaseURL, activeProfile.ModelID)
		if err != nil {
			add(DoctorCheck{
				Name:    "LM Studio model metadata",
				Status:  "warn",
				Message: "Could not read LM Studio native model metadata",
				Detail:  err.Error(),
			})
		} else if !found {
			add(DoctorCheck{
				Name:    "LM Studio model metadata",
				Status:  "warn",
				Message: fmt.Sprintf("Could not find active model %q in LM Studio native model list", activeProfile.ModelID),
			})
		} else {
			addLMStudioCapabilityChecks(add, model, activeProfile)
		}
	}

	// ── 3. Context window match ───────────────────────────────────────────────
	if hasProvider && activeProfile.CtxTokens > 0 {
		if hasProvider && provider.Backend == "llamacpp" {
			actualCtx, err := fetchLlamacppContext(provider.BaseURL)
			if err != nil {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "warn",
					Message: "Could not read active context size from llama.cpp",
					Detail:  err.Error(),
				})
			} else if actualCtx > 0 && actualCtx < activeProfile.CtxTokens {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "warn",
					Message: fmt.Sprintf("Profile requests %d tokens but server reports %d — compaction threshold may fire too early", activeProfile.CtxTokens, actualCtx),
					Detail:  "Lower ctx_tokens in the profile or restart llama.cpp with a larger --ctx-size",
				})
			} else if actualCtx > activeProfile.CtxTokens*2 {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "warn",
					Message: fmt.Sprintf("Profile is %d tokens but server is running %d tokens", activeProfile.CtxTokens, actualCtx),
					Detail:  "InferenceBridge/llama.cpp appears to have launched model-default context. On a 24 GB RTX 3090, restart the backend with an explicit 32768 ctx to avoid excessive KV cache use.",
				})
			} else {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "ok",
					Message: fmt.Sprintf("Profile: %d tokens  Server: %d tokens", activeProfile.CtxTokens, actualCtx),
				})
			}
		} else if provider.Backend == "lmstudio" {
			model, found, err := fetchLMStudioModelInfo(provider.BaseURL, activeProfile.ModelID)
			if err != nil {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "warn",
					Message: "Could not read LM Studio loaded context",
					Detail:  err.Error(),
				})
			} else if !found {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "info",
					Message: fmt.Sprintf("Profile ctx: %d tokens; no matching LM Studio loaded model metadata found", activeProfile.CtxTokens),
				})
			} else if actualCtx := model.MaxLoadedContext(); actualCtx > 0 && actualCtx < activeProfile.CtxTokens {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "warn",
					Message: fmt.Sprintf("Profile requests %d tokens but LM Studio reports %d", activeProfile.CtxTokens, actualCtx),
					Detail:  "Reload the model with the profile context length or lower ctx_tokens.",
				})
			} else if actualCtx := model.MaxLoadedContext(); actualCtx > activeProfile.CtxTokens*2 {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "warn",
					Message: fmt.Sprintf("Profile is %d tokens but LM Studio reports %d", activeProfile.CtxTokens, actualCtx),
					Detail:  "A very large loaded context can waste VRAM on a 24 GB RTX 3090.",
				})
			} else if actualCtx := model.MaxLoadedContext(); actualCtx > 0 {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "ok",
					Message: fmt.Sprintf("Profile: %d tokens  LM Studio: %d tokens", activeProfile.CtxTokens, actualCtx),
				})
			} else {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "info",
					Message: fmt.Sprintf("Profile ctx: %d tokens; LM Studio did not report loaded context length", activeProfile.CtxTokens),
				})
			}
		}
	}

	// ── 4. Thinking mode + no-think threshold ────────────────────────────────
	addRuntimeProfileChecks(add, activeProfile)
	addProfileIdentityChecks(add, cfg.ActiveProfile, activeProfile)
	addAgentPresetBudgetChecks(add, cfg, activeProfile)
	addProfileSanityChecks(add, activeProfile)

	if activeProfile.Thinking {
		threshold := cfg.Agents.NoThinkAfterToolCalls
		if threshold <= 0 {
			threshold = 3
		}
		add(DoctorCheck{
			Name:    "Thinking mode",
			Status:  "ok",
			Message: fmt.Sprintf("Enabled — thinking disabled automatically after %d tool calls per turn (Qwen3 tool-call collision fix)", threshold),
		})
		if hasProvider && provider.Backend != "llamacpp" {
			add(DoctorCheck{
				Name:    "Thinking mode backend",
				Status:  "warn",
				Message: "Thinking mode is on but the active backend is not llama.cpp — chat_template_kwargs will be silently ignored",
				Detail:  "Switch to a llamacpp provider or disable thinking for this profile",
			})
		}
	} else {
		add(DoctorCheck{
			Name:    "Thinking mode",
			Status:  "info",
			Message: "Disabled for active profile — tool calling will be most reliable in this mode",
		})
	}

	// ── 5. MTP speculative decoding ───────────────────────────────────────────
	if activeProfile.SpecType != "" {
		add(DoctorCheck{
			Name:    "MTP speculative decoding",
			Status:  "ok",
			Message: fmt.Sprintf("Enabled: spec_type=%s draft_n_max=%d — expect 1.4–2.2× faster generation", activeProfile.SpecType, activeProfile.SpecDraftNMax),
		})
	} else {
		add(DoctorCheck{
			Name:    "MTP speculative decoding",
			Status:  "info",
			Message: "Disabled — set spec_type=draft-mtp in the profile for 1.4–2.2× faster generation (llama.cpp b9180+ only)",
		})
	}

	// ── 6. Shell backend ─────────────────────────────────────────────────────
	addRuntimeLockChecks(add)

	shellBackend := cfg.Tools.ShellBackend
	if shellBackend == "" {
		shellBackend = "auto"
	}
	if runtime.GOOS == "windows" {
		if shellBackend == "bash" {
			add(DoctorCheck{
				Name:    "Shell backend",
				Status:  "warn",
				Message: "Shell backend is set to 'bash' on Windows — this will fail unless Git Bash or WSL is in PATH",
				Detail:  "Change to 'auto' (PowerShell) or 'wsl' for WSL bash",
			})
		} else {
			add(DoctorCheck{
				Name:    "Shell backend",
				Status:  "ok",
				Message: fmt.Sprintf("Shell backend: %s (Windows)", shellBackend),
			})
		}
	} else {
		add(DoctorCheck{
			Name:    "Shell backend",
			Status:  "ok",
			Message: fmt.Sprintf("Shell backend: %s (%s)", shellBackend, runtime.GOOS),
		})
	}

	if cfg.Agents.OfflineOnly {
		add(DoctorCheck{
			Name:    "Access preset",
			Status:  "warn",
			Message: fmt.Sprintf("Offline mode is active with toolset=%s", cfg.Tools.ActiveToolset),
			Detail:  "Local file/shell tools can still run if enabled, but web, fetch, and browser tools are intentionally blocked even if the user asks to look online.",
		})
	} else {
		add(DoctorCheck{
			Name:    "Access preset",
			Status:  "ok",
			Message: fmt.Sprintf("toolset=%s", cfg.Tools.ActiveToolset),
		})
	}
	addToolAccessChecks(add, cfg)

	// ── 7. Memory DB ─────────────────────────────────────────────────────────
	if path, err := memoryPath(); err != nil {
		add(DoctorCheck{Name: "Memory DB", Status: "fail", Message: err.Error()})
	} else if _, err := os.Stat(path); os.IsNotExist(err) {
		add(DoctorCheck{Name: "Memory DB", Status: "info", Message: "memory.json does not exist yet — will be created on first save"})
	} else if err != nil {
		add(DoctorCheck{Name: "Memory DB", Status: "fail", Message: err.Error()})
	} else {
		entries, err := loadMemory()
		if err != nil {
			add(DoctorCheck{Name: "Memory DB", Status: "warn", Message: "memory.json exists but could not be parsed", Detail: err.Error()})
		} else {
			add(DoctorCheck{Name: "Memory DB", Status: "ok", Message: fmt.Sprintf("%d memory entries", len(entries))})
		}
	}

	// ── 8. Session recall DB ─────────────────────────────────────────────────
	cfgDir, err := settings.ConfigDir()
	if err != nil {
		add(DoctorCheck{Name: "Session recall DB", Status: "fail", Message: err.Error()})
	} else {
		dbPath := filepath.Join(cfgDir, "state.db")
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			add(DoctorCheck{Name: "Session recall DB", Status: "info", Message: "state.db does not exist yet — created on first session save"})
		} else {
			add(DoctorCheck{Name: "Session recall DB", Status: "ok", Message: dbPath})
		}
	}

	// ── 9. Skills directory ──────────────────────────────────────────────────
	if dir, err := skillsDir(); err != nil {
		add(DoctorCheck{Name: "Skills dir", Status: "fail", Message: err.Error()})
	} else if _, err := os.Stat(dir); os.IsNotExist(err) {
		add(DoctorCheck{Name: "Skills dir", Status: "info", Message: "No skills yet — skills dir will be created when you save the first skill"})
	} else {
		skillList, _ := loadSkills()
		add(DoctorCheck{Name: "Skills dir", Status: "ok", Message: fmt.Sprintf("%d skills in %s", len(skillList), dir)})
	}

	// ── 10. USER.md ──────────────────────────────────────────────────────────
	if up := loadUserProfile(); up == "" {
		add(DoctorCheck{
			Name:    "User profile",
			Status:  "info",
			Message: "USER.md not set — create one in the Memory tab so the agent learns your preferences",
		})
	} else {
		words := len(strings.Fields(up))
		add(DoctorCheck{Name: "User profile", Status: "ok", Message: fmt.Sprintf("USER.md: %d words", words)})
	}

	// ── 11. Web search ───────────────────────────────────────────────────────
	addWebEngineChecks(add, cfg)

	// ── Score ────────────────────────────────────────────────────────────────
	ok, warns, fails := 0, 0, 0
	for _, c := range checks {
		switch c.Status {
		case "ok":
			ok++
		case "warn":
			warns++
		case "fail":
			fails++
		}
	}
	total := ok + warns + fails
	score := 100
	if total > 0 {
		score = (ok*100 + warns*50) / total
	}
	grade := "OK"
	if fails > 0 {
		grade = "FAIL"
	} else if warns > 0 {
		grade = "WARN"
	}

	return DoctorResult{Checks: checks, Score: score, Grade: grade}
}

// checkLlamacppVersion probes /health or /props to guess the server version.
func checkLlamacppVersion(baseURL string) (status, message, detail string) {
	base := strings.TrimSuffix(baseURL, "/v1")
	client := &http.Client{Timeout: 4 * time.Second}
	// Try /props endpoint (newer llama.cpp servers expose build info here)
	resp, err := client.Get(base + "/props")
	if err != nil {
		// Fallback: just check /health
		resp2, err2 := client.Get(base + "/health")
		if err2 != nil {
			return "warn", "Cannot reach llama.cpp /health — is the server running?", err2.Error()
		}
		defer resp2.Body.Close()
		return "info", "llama.cpp is running (version unknown — /props not available)", ""
	}
	defer resp.Body.Close()
	// We can't parse the full response without JSON parsing, but getting a 200 is enough.
	return "ok", "llama.cpp is running (/props available — likely b9180+ for MTP support)", ""
}

// fetchLlamacppContext reads the active llama.cpp context size from /props,
// falling back to InferenceBridge's /v1/health KV cache metadata.
func fetchLlamacppContext(baseURL string) (int, error) {
	base := strings.TrimSuffix(baseURL, "/v1")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + "/props")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var props struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&props); err != nil {
		return 0, err
	}
	if props.DefaultGenerationSettings.NCtx > 0 {
		return props.DefaultGenerationSettings.NCtx, nil
	}
	resp, err = client.Get(base + "/v1/health")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var health struct {
		KVCache struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"kv_cache"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return 0, err
	}
	return health.KVCache.TotalTokens, nil
}

type chatTemplateCaps struct {
	SupportsTools     bool `json:"supports_tools"`
	SupportsToolCalls bool `json:"supports_tool_calls"`
}

func fetchLlamacppChatFormat(baseURL string) (string, chatTemplateCaps, error) {
	base := strings.TrimSuffix(baseURL, "/v1")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + "/props")
	if err != nil {
		return "", chatTemplateCaps{}, err
	}
	defer resp.Body.Close()
	var props struct {
		DefaultGenerationSettings struct {
			Params struct {
				ChatFormat string `json:"chat_format"`
			} `json:"params"`
		} `json:"default_generation_settings"`
		ChatTemplateCaps chatTemplateCaps `json:"chat_template_caps"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&props); err != nil {
		return "", chatTemplateCaps{}, err
	}
	if strings.TrimSpace(props.DefaultGenerationSettings.Params.ChatFormat) == "" {
		return "unknown", props.ChatTemplateCaps, nil
	}
	return props.DefaultGenerationSettings.Params.ChatFormat, props.ChatTemplateCaps, nil
}

func fetchLlamacppBuiltinTools(baseURL string) ([]string, error) {
	base := strings.TrimSuffix(baseURL, "/v1")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(base + "/props")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("props HTTP %d", resp.StatusCode)
	}
	var props any
	if err := json.NewDecoder(resp.Body).Decode(&props); err != nil {
		return nil, err
	}
	found := map[string]bool{}
	collectBuiltinToolNames(props, found)
	return sortedBuiltinToolNames(found), nil
}

func collectBuiltinToolNames(value any, found map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			markBuiltinToolName(key, found)
			collectBuiltinToolNames(child, found)
		}
	case []any:
		for _, child := range v {
			collectBuiltinToolNames(child, found)
		}
	case string:
		markBuiltinToolName(v, found)
	}
}

func markBuiltinToolName(text string, found map[string]bool) {
	lower := strings.ToLower(text)
	for _, name := range dangerousLlamaServerTools() {
		if lower == name || strings.Contains(lower, name) {
			found[name] = true
		}
	}
}

func dangerousLlamaServerTools() []string {
	return []string{
		"exec_shell_command",
		"read_file",
		"write_file",
		"edit_file",
		"grep_search",
		"file_search",
	}
}

func sortedBuiltinToolNames(found map[string]bool) []string {
	var out []string
	for _, name := range dangerousLlamaServerTools() {
		if found[name] {
			out = append(out, name)
		}
	}
	return out
}

type modelTemplateInfo struct {
	Source string
	Mode   string
}

type lmStudioDoctorModel struct {
	Key             string          `json:"key"`
	SelectedVariant string          `json:"selected_variant"`
	Capabilities    []string        `json:"capabilities"`
	Reasoning       json.RawMessage `json:"reasoning"`
	LoadedInstances []struct {
		ID     string
		Config struct {
			ContextLength int `json:"context_length"`
		} `json:"config"`
	} `json:"loaded_instances"`
}

func (m lmStudioDoctorModel) MaxLoadedContext() int {
	maxCtx := 0
	for _, instance := range m.LoadedInstances {
		if instance.Config.ContextLength > maxCtx {
			maxCtx = instance.Config.ContextLength
		}
	}
	return maxCtx
}

func fetchLMStudioModelInfo(baseURL, modelID string) (lmStudioDoctorModel, bool, error) {
	nativeBase := strings.TrimSuffix(baseURL, "/v1")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(nativeBase + "/api/v1/models")
	if err != nil {
		return lmStudioDoctorModel{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return lmStudioDoctorModel{}, false, fmt.Errorf("LM Studio native models HTTP %d", resp.StatusCode)
	}
	var listing struct {
		Models []lmStudioDoctorModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return lmStudioDoctorModel{}, false, err
	}
	if len(listing.Models) == 0 {
		return lmStudioDoctorModel{}, false, nil
	}
	want := normalizeDoctorModelID(modelID)
	var firstLoaded *lmStudioDoctorModel
	for i := range listing.Models {
		model := &listing.Models[i]
		if len(model.LoadedInstances) > 0 && firstLoaded == nil {
			firstLoaded = model
		}
		if want == "" {
			continue
		}
		if normalizeDoctorModelID(model.Key) == want || normalizeDoctorModelID(model.SelectedVariant) == want {
			return *model, true, nil
		}
	}
	if want == "" && firstLoaded != nil {
		return *firstLoaded, true, nil
	}
	if firstLoaded != nil && len(firstLoaded.LoadedInstances) > 0 {
		return *firstLoaded, true, nil
	}
	return lmStudioDoctorModel{}, false, nil
}

func addLMStudioCapabilityChecks(add func(DoctorCheck), model lmStudioDoctorModel, profile settings.Profile) {
	if len(model.Capabilities) == 0 {
		add(DoctorCheck{
			Name:    "LM Studio tool capability",
			Status:  "info",
			Message: "LM Studio did not report model capabilities",
			Detail:  "Upgrade LM Studio if tool/reasoning capability metadata is missing from /api/v1/models.",
		})
	} else if hasCapability(model.Capabilities, "tool", "function") {
		add(DoctorCheck{
			Name:    "LM Studio tool capability",
			Status:  "ok",
			Message: "Loaded model reports tool/function capability",
		})
	} else {
		add(DoctorCheck{
			Name:    "LM Studio tool capability",
			Status:  "warn",
			Message: "Loaded model does not report tool/function capability",
			Detail:  "Recent LM Studio builds fixed several Qwen/GLM tool-call parsers; update LM Studio or use llama.cpp with a tool-aware template if tool calls degrade.",
		})
	}

	reasoning := strings.TrimSpace(string(model.Reasoning))
	if profile.Thinking {
		if reasoning != "" && reasoning != "null" && reasoning != "{}" {
			add(DoctorCheck{
				Name:    "LM Studio reasoning metadata",
				Status:  "ok",
				Message: "LM Studio reports reasoning metadata for the loaded model",
			})
		} else {
			add(DoctorCheck{
				Name:    "LM Studio reasoning metadata",
				Status:  "info",
				Message: "LM Studio did not report reasoning metadata for the loaded model",
				Detail:  "Thinking may still work through LM Studio UI settings, but TheMauler cannot verify it from native metadata.",
			})
		}
	}
}

func hasCapability(capabilities []string, needles ...string) bool {
	for _, capability := range capabilities {
		lower := strings.ToLower(capability)
		for _, needle := range needles {
			if strings.Contains(lower, needle) {
				return true
			}
		}
	}
	return false
}

func normalizeDoctorModelID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	id = strings.TrimSuffix(id, "/")
	if i := strings.Index(id, "@"); i >= 0 {
		id = id[:i]
	}
	return id
}

func fetchActiveModelTemplate(baseURL, modelID string) (modelTemplateInfo, error) {
	base := strings.TrimRight(baseURL, "/")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(base + "/models")
	if err != nil {
		return modelTemplateInfo{}, err
	}
	defer resp.Body.Close()
	var listing struct {
		Data []struct {
			ID             string `json:"id"`
			State          string `json:"state"`
			Active         bool   `json:"active"`
			TemplateSource string `json:"template_source"`
			TemplateMode   string `json:"template_mode"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return modelTemplateInfo{}, err
	}
	var loaded modelTemplateInfo
	for _, item := range listing.Data {
		info := modelTemplateInfo{Source: item.TemplateSource, Mode: item.TemplateMode}
		if item.ID == modelID || item.Active {
			return info, nil
		}
		if loaded.Source == "" && item.State == "loaded" {
			loaded = info
		}
	}
	return loaded, nil
}

func providerHostLabel(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func addRuntimeProfileChecks(add func(DoctorCheck), profile settings.Profile) {
	rp, ok := runtimeprofile.Match(profile)
	if !ok {
		add(DoctorCheck{
			Name:    "Runtime registry",
			Status:  "info",
			Message: "No built-in runtime profile matched the active model",
			Detail:  "TheMauler will use generic OpenAI-compatible behavior. Add a runtime profile before relying on model-specific routing or eval gates.",
		})
		return
	}
	add(DoctorCheck{
		Name:    "Runtime registry",
		Status:  "ok",
		Message: fmt.Sprintf("%s adapter=%s tool_protocol=%s", rp.Name, rp.Adapter, rp.ToolProtocol),
	})
	if profile.Thinking && !rp.Supports.Thinking {
		add(DoctorCheck{
			Name:    "Runtime adapter",
			Status:  "warn",
			Message: fmt.Sprintf("%s is not marked as thinking-capable but this profile has thinking=true", rp.Name),
			Detail:  "Disable thinking for this profile unless the specific backend/template exposes a known reasoning channel.",
		})
	}
	if profile.SpecType != "" && !rp.Supports.MTP {
		add(DoctorCheck{
			Name:    "MTP compatibility",
			Status:  "warn",
			Message: fmt.Sprintf("%s is not marked as MTP-capable but spec_type=%s is enabled", rp.Name, profile.SpecType),
			Detail:  "Disable draft-mtp or switch to a known MTP GGUF.",
		})
	} else if profile.SpecType != "" && !runtimeprofile.LooksMTPModel(profile) {
		add(DoctorCheck{
			Name:    "MTP compatibility",
			Status:  "warn",
			Message: "Profile enables draft-mtp but model_id/name does not include MTP",
			Detail:  "Use an MTP GGUF artifact for draft-mtp. Normal Qwen/Gemma GGUFs should leave spec_type empty.",
		})
	} else if profile.SpecType == "" && rp.Supports.MTP && runtimeprofile.LooksMTPModel(profile) {
		add(DoctorCheck{
			Name:    "MTP compatibility",
			Status:  "info",
			Message: "Model looks MTP-capable but draft-mtp is disabled",
			Detail:  "For recent llama.cpp builds, try spec_type=draft-mtp and spec_draft_n_max=2 or 3, then benchmark.",
		})
	}
	if rp.RecommendedCtx > 0 && profile.CtxTokens > rp.RecommendedCtx*2 {
		add(DoctorCheck{
			Name:    "Runtime context profile",
			Status:  "warn",
			Message: fmt.Sprintf("%s recommends around %d ctx, profile requests %d", rp.Name, rp.RecommendedCtx, profile.CtxTokens),
			Detail:  "Large contexts are allowed, but verify VRAM/KV cache and latency before treating this profile as stable.",
		})
	}
}

func addRuntimeLockChecks(add func(DoctorCheck)) {
	cfgDir, err := settings.ConfigDir()
	if err != nil {
		add(DoctorCheck{Name: "Runtime lock", Status: "warn", Message: "Could not locate config directory", Detail: err.Error()})
		return
	}
	path := filepath.Join(cfgDir, "runtime-lock.json")
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		add(DoctorCheck{
			Name:    "Runtime lock",
			Status:  "info",
			Message: "No runtime-lock.json yet",
			Detail:  "The next successful agent run will write the active model/profile/backend launch snapshot.",
		})
		return
	}
	if err != nil {
		add(DoctorCheck{Name: "Runtime lock", Status: "warn", Message: "Could not inspect runtime-lock.json", Detail: err.Error()})
		return
	}
	add(DoctorCheck{
		Name:    "Runtime lock",
		Status:  "ok",
		Message: fmt.Sprintf("Last runtime snapshot: %s", info.ModTime().Format(time.RFC3339)),
		Detail:  path,
	})
}

func addProfileIdentityChecks(add func(DoctorCheck), profileName string, profile settings.Profile) {
	nameFamily := modelFamily(profileName)
	modelFamily := modelFamily(profile.ModelID)
	if nameFamily == "" || modelFamily == "" || nameFamily == modelFamily {
		return
	}
	add(DoctorCheck{
		Name:    "Profile identity",
		Status:  "warn",
		Message: fmt.Sprintf("Profile name suggests %s but model_id is %s", nameFamily, profile.ModelID),
		Detail:  "This is allowed for testing, but it makes logs and status labels misleading. Rename the profile or switch model_id to match the profile family.",
	})
}

func modelFamily(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "gemma"):
		return "Gemma"
	case strings.Contains(lower, "qwen"):
		return "Qwen"
	case strings.Contains(lower, "glm"):
		return "GLM"
	case strings.Contains(lower, "llama"):
		return "Llama"
	case strings.Contains(lower, "mistral"):
		return "Mistral"
	default:
		return ""
	}
}

func addAgentPresetBudgetChecks(add func(DoctorCheck), cfg settings.Settings, profile settings.Profile) {
	if profile.CtxTokens <= 0 {
		return
	}
	var lower []string
	for name, preset := range cfg.Agents.Presets {
		if !preset.Enabled || preset.ContextBudget <= 0 || preset.ContextBudget >= profile.CtxTokens {
			continue
		}
		lower = append(lower, fmt.Sprintf("%s=%d", name, preset.ContextBudget))
	}
	if len(lower) == 0 {
		add(DoctorCheck{
			Name:    "Agent context budgets",
			Status:  "ok",
			Message: fmt.Sprintf("No enabled preset is below profile ctx_tokens=%d", profile.CtxTokens),
		})
		return
	}
	sort.Strings(lower)
	add(DoctorCheck{
		Name:    "Agent context budgets",
		Status:  "info",
		Message: fmt.Sprintf("Some presets use smaller working budgets than profile ctx_tokens=%d", profile.CtxTokens),
		Detail:  "These budgets now limit only chat/history compaction, not backend model loading: " + strings.Join(lower, ", "),
	})
}

func addProfileSanityChecks(add func(DoctorCheck), profile settings.Profile) {
	maxTokens := maxGenerationTokens(profile.ThinkGeneral, profile.ThinkCoding, profile.NoThink)
	if profile.CtxTokens > 0 && maxTokens > profile.CtxTokens/2 {
		add(DoctorCheck{
			Name:    "Profile max output",
			Status:  "warn",
			Message: fmt.Sprintf("A generation preset allows %d output tokens with only %d ctx_tokens", maxTokens, profile.CtxTokens),
			Detail:  "Keep max_tokens comfortably below the context window. For Qwen3.6-27B on a 3090, 4096-8192 is a safer range for normal work.",
		})
	} else if maxTokens > 16384 {
		add(DoctorCheck{
			Name:    "Profile max output",
			Status:  "warn",
			Message: fmt.Sprintf("A generation preset allows %d output tokens", maxTokens),
			Detail:  "Very large max_tokens can cause long stalls or context pressure. 4096-8192 is usually enough for agent work.",
		})
	} else {
		add(DoctorCheck{
			Name:    "Profile max output",
			Status:  "ok",
			Message: fmt.Sprintf("Largest max_tokens preset: %d", maxTokens),
		})
	}

	model := strings.ToLower(profile.ModelID)
	if strings.Contains(model, "qwen3.6") && strings.Contains(model, "27b") && strings.Contains(model, "q4_k_m") {
		add(DoctorCheck{
			Name:    "Model quant",
			Status:  "info",
			Message: "Active Qwen3.6-27B model is Q4_K_M",
			Detail:  "Project default guidance is UD-Q4_K_XL for the RTX 3090. Q4_K_M may run, but verify VRAM/headroom before using large context.",
		})
	}
	if strings.Contains(model, "q6_k") {
		add(DoctorCheck{
			Name:    "Model quant",
			Status:  "warn",
			Message: "Q6_K is not safe for Qwen3.6-27B at 32K on a 24 GB RTX 3090",
			Detail:  "Use UD-Q4_K_XL instead; Q6_K can OOM once KV cache grows.",
		})
	}
}

func maxGenerationTokens(params ...settings.GenerationParams) int {
	maxTokens := 0
	for _, p := range params {
		if p.MaxTokens > maxTokens {
			maxTokens = p.MaxTokens
		}
	}
	return maxTokens
}

func addToolAccessChecks(add func(DoctorCheck), cfg settings.Settings) {
	effective := settings.EffectiveEnabledTools(cfg.Tools)
	registry := tools.New()
	available := map[string]bool{}
	for _, tool := range registry.All() {
		available[tool.Name()] = true
	}
	groups := []struct {
		name  string
		tools []string
	}{
		{"Web tools", []string{"web_search", "fetch_url", "subagent_research"}},
		{"Browser tools", []string{"browser_open", "browser_snapshot", "browser_extract", "browser_screenshot"}},
		{"Write tools", []string{"write_file", "edit_file"}},
		{"Shell tools", []string{"shell", "bash"}},
	}
	for _, group := range groups {
		enabled := []string{}
		blocked := []string{}
		missing := []string{}
		for _, name := range group.tools {
			if !available[name] && !strings.HasPrefix(name, "subagent_") {
				missing = append(missing, name)
				continue
			}
			if cfg.Tools.Enabled && toolEnabled(effective, name) {
				enabled = append(enabled, name)
			} else {
				blocked = append(blocked, name)
			}
		}
		status := "ok"
		message := fmt.Sprintf("%s enabled under toolset=%s", strings.Join(enabled, ", "), cfg.Tools.ActiveToolset)
		if len(enabled) == 0 {
			status = "warn"
			message = fmt.Sprintf("All %s are blocked under toolset=%s", strings.ToLower(group.name), cfg.Tools.ActiveToolset)
		} else if len(blocked) > 0 || len(missing) > 0 {
			status = "info"
		}
		detailParts := []string{}
		if !cfg.Tools.Enabled {
			status = "fail"
			message = "Global tool use is disabled"
		}
		if len(blocked) > 0 {
			detailParts = append(detailParts, "blocked: "+strings.Join(blocked, ", "))
		}
		if len(missing) > 0 {
			detailParts = append(detailParts, "not registered: "+strings.Join(missing, ", "))
		}
		if len(enabled) > 0 {
			detailParts = append(detailParts, "enabled: "+strings.Join(enabled, ", "))
		}
		add(DoctorCheck{Name: group.name, Status: status, Message: message, Detail: strings.Join(detailParts, "\n")})
	}
}

func addWebEngineChecks(add func(DoctorCheck), cfg settings.Settings) {
	engine := strings.ToLower(strings.TrimSpace(cfg.Tools.WebEngine))
	if engine == "" || engine == "auto" {
		engine = "duckduckgo"
	}
	switch engine {
	case "duckduckgo", "ddg":
		webCheckCtx, webCancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer webCancel()
		if err := checkWebSearch(webCheckCtx); err != nil {
			add(DoctorCheck{
				Name:    "Web engine",
				Status:  "warn",
				Message: "DuckDuckGo connectivity check failed",
				Detail:  err.Error(),
			})
		} else {
			add(DoctorCheck{Name: "Web engine", Status: "ok", Message: "DuckDuckGo reachable"})
		}
	case "searxng", "searx":
		if strings.TrimSpace(cfg.Tools.WebBaseURL) == "" {
			add(DoctorCheck{
				Name:    "Web engine",
				Status:  "fail",
				Message: "SearXNG selected but tools.web_base_url is empty",
				Detail:  "Set tools.web_base_url to your SearXNG instance, for example http://localhost:8081",
			})
			return
		}
		if err := checkURLReachable(cfg.Tools.WebBaseURL); err != nil {
			add(DoctorCheck{Name: "Web engine", Status: "warn", Message: "SearXNG base URL is not reachable", Detail: err.Error()})
		} else {
			add(DoctorCheck{Name: "Web engine", Status: "ok", Message: "SearXNG reachable at " + cfg.Tools.WebBaseURL})
		}
	case "brave":
		if strings.TrimSpace(cfg.Tools.BraveAPIKey) == "" && strings.TrimSpace(cfg.Tools.WebAPIKeyEnv) == "" {
			add(DoctorCheck{
				Name:    "Web engine",
				Status:  "fail",
				Message: "Brave selected but no API key is configured",
				Detail:  "Set tools.web_api_key_env to an environment variable name or set tools.brave_api_key.",
			})
			return
		}
		if envName := strings.TrimSpace(cfg.Tools.WebAPIKeyEnv); envName != "" && os.Getenv(envName) == "" && strings.TrimSpace(cfg.Tools.BraveAPIKey) == "" {
			add(DoctorCheck{
				Name:    "Web engine",
				Status:  "fail",
				Message: fmt.Sprintf("Brave API key env var %s is not set", envName),
			})
		} else {
			add(DoctorCheck{Name: "Web engine", Status: "ok", Message: "Brave Search credentials configured"})
		}
	default:
		add(DoctorCheck{Name: "Web engine", Status: "fail", Message: "Unsupported web engine: " + cfg.Tools.WebEngine})
	}

	if err := checkURLReachable("https://example.com/"); err != nil {
		add(DoctorCheck{Name: "Fetch URL", Status: "warn", Message: "Basic HTTPS fetch check failed", Detail: err.Error()})
	} else {
		add(DoctorCheck{Name: "Fetch URL", Status: "ok", Message: "Basic HTTPS fetch reachable"})
	}
}

func checkURLReachable(rawURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func checkWebSearch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://duckduckgo.com/", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
