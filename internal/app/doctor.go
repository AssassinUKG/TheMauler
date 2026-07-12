package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
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

	// -- 1. Active provider reachability --------------------------------------
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
		addInferenceBridgePortConfigCheck(add, provider)
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

	// -- 2. llama.cpp version -------------------------------------------------
	if hasProvider && provider.Backend == "llamacpp" {
		versionOK, versionMsg, versionDetail := checkLlamacppVersion(provider.BaseURL)
		add(DoctorCheck{
			Name:    "llama.cpp version",
			Status:  versionOK,
			Message: versionMsg,
			Detail:  versionDetail,
		})
		addInferenceBridgeProgressCheck(add, provider.BaseURL)
		addInferenceBridgeAgentEndpointCheck(add, provider)
	} else if hasProvider && provider.Backend == "lmstudio" {
		add(DoctorCheck{
			Name:    "llama.cpp version",
			Status:  "info",
			Message: "Using LM Studio - llama.cpp version check not applicable",
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

	// -- 3. Context window match -----------------------------------------------
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
					Status:  "fail",
					Message: fmt.Sprintf("Profile requests %d tokens but backend actually loaded %d", activeProfile.CtxTokens, actualCtx),
					Detail:  "The agent run will be blocked until InferenceBridge/llama.cpp reloads this model with the profile context. TheMauler no longer silently shrinks to the smaller backend window.",
				})
			} else if actualCtx > activeProfile.CtxTokens*2 {
				add(DoctorCheck{
					Name:    "Context window",
					Status:  "info",
					Message: fmt.Sprintf("Profile is %d tokens but server is running %d tokens", activeProfile.CtxTokens, actualCtx),
					Detail:  "Backend context is larger than requested. This is allowed; TheMauler will keep the profile as the working budget unless you choose a larger profile.",
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

	addGPUVRAMDoctorCheck(add, activeProfile)

	// -- 4. Thinking mode + no-think threshold --------------------------------
	addRuntimeProfileChecks(add, activeProfile)
	addProfileIdentityChecks(add, cfg.ActiveProfile, activeProfile)
	addAgentPresetBudgetChecks(add, cfg, activeProfile)
	addSharedBackendSubagentCheck(add, cfg, activeProfile)
	addProfileSanityChecks(add, activeProfile)
	addModelTierCheck(add, activeProfile)
	if hasProvider && provider.Backend == "llamacpp" {
		addLlamacppLaunchAssertions(add, provider.BaseURL, activeProfile)
		addLlamacppAgentFlagAdvisory(add)
	}

	if activeProfile.Thinking {
		threshold := cfg.Agents.NoThinkAfterToolCalls
		if threshold <= 0 {
			threshold = 2
		}
		add(DoctorCheck{
			Name:    "Thinking mode",
			Status:  "ok",
			Message: fmt.Sprintf("Enabled - thinking disabled automatically after %d tool calls per turn (Qwen3 tool-call collision fix)", threshold),
		})
		if hasProvider && provider.Backend != "llamacpp" {
			add(DoctorCheck{
				Name:    "Thinking mode backend",
				Status:  "warn",
				Message: "Thinking mode is on but the active backend is not llama.cpp - chat_template_kwargs will be silently ignored",
				Detail:  "Switch to a llamacpp provider or disable thinking for this profile",
			})
		}
	} else {
		add(DoctorCheck{
			Name:    "Thinking mode",
			Status:  "info",
			Message: "Disabled for active profile - tool calling will be most reliable in this mode",
		})
	}

	// -- 5. MTP speculative decoding -------------------------------------------
	if activeProfile.SpecType != "" {
		add(DoctorCheck{
			Name:    "MTP speculative decoding",
			Status:  "ok",
			Message: fmt.Sprintf("Enabled: spec_type=%s draft_n_max=%d - expect 1.4-2.2x faster generation", activeProfile.SpecType, activeProfile.SpecDraftNMax),
		})
	} else {
		add(DoctorCheck{
			Name:    "MTP speculative decoding",
			Status:  "info",
			Message: "Disabled - set spec_type=draft-mtp in the profile for 1.4-2.2x faster generation (llama.cpp b9180+ only)",
		})
	}

	// -- 6. Shell backend -----------------------------------------------------
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
				Message: "Shell backend is set to 'bash' on Windows - this will fail unless Git Bash or WSL is in PATH",
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
	addShellNetworkBoundaryCheck(add, cfg, shellBackend)
	addSharedTerminalDoctorCheck(add, a.GetSharedTerminalState())
	addToolingSmokeChecks(add)

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

	// -- 7. Memory DB ---------------------------------------------------------
	if entries, err := loadMemory(); err != nil {
		add(DoctorCheck{Name: "Memory DB", Status: "warn", Message: "SQLite memory store could not be read", Detail: err.Error()})
	} else {
		cfgDir, err := settings.ConfigDir()
		if err != nil {
			add(DoctorCheck{Name: "Memory DB", Status: "fail", Message: err.Error()})
		} else {
			add(DoctorCheck{Name: "Memory DB", Status: "ok", Message: fmt.Sprintf("%d memory entries in SQLite state DB", len(entries)), Detail: filepath.Join(cfgDir, "state.db")})
		}
	}

	// -- 8. Session recall DB -------------------------------------------------
	cfgDir, err := settings.ConfigDir()
	if err != nil {
		add(DoctorCheck{Name: "Session recall DB", Status: "fail", Message: err.Error()})
	} else {
		dbPath := filepath.Join(cfgDir, "state.db")
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			add(DoctorCheck{Name: "Session recall DB", Status: "info", Message: "state.db does not exist yet - created on first session save"})
		} else {
			add(DoctorCheck{Name: "Session recall DB", Status: "ok", Message: dbPath})
		}
	}

	// -- 9. Skills directory --------------------------------------------------
	if dir, err := skillsDir(); err != nil {
		add(DoctorCheck{Name: "Skills dir", Status: "fail", Message: err.Error()})
	} else if _, err := os.Stat(dir); os.IsNotExist(err) {
		add(DoctorCheck{Name: "Skills dir", Status: "info", Message: "No skills yet - skills dir will be created when you save the first skill"})
	} else {
		skillList, _ := loadSkills()
		add(DoctorCheck{Name: "Skills dir", Status: "ok", Message: fmt.Sprintf("%d skills in %s", len(skillList), dir)})
	}
	addMasterSkillDoctorChecks(add)

	// -- 10. USER.md ----------------------------------------------------------
	if up := loadUserProfile(); up == "" {
		add(DoctorCheck{
			Name:    "User profile",
			Status:  "info",
			Message: "USER.md not set - create one in the Memory tab so the agent learns your preferences",
		})
	} else {
		words := len(strings.Fields(up))
		add(DoctorCheck{Name: "User profile", Status: "ok", Message: fmt.Sprintf("USER.md: %d words", words)})
	}

	// -- 11. Web search -------------------------------------------------------
	addWebEngineChecks(add, cfg)

	// -- Score ----------------------------------------------------------------
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

func addMasterSkillDoctorChecks(add func(DoctorCheck)) {
	skill, err := loadSkill("master")
	if err != nil {
		if os.IsNotExist(err) {
			add(DoctorCheck{
				Name:    "Master skill",
				Status:  "info",
				Message: "No master skill registered",
				Detail:  "Register one only if you want TheMauler to use a larger methodology/reference library through skill mode=view.",
			})
			return
		}
		add(DoctorCheck{Name: "Master skill", Status: "warn", Message: "Could not read registered master skill", Detail: err.Error()})
		return
	}
	if strings.TrimSpace(skill.SourcePath) == "" {
		add(DoctorCheck{
			Name:    "Master skill",
			Status:  "warn",
			Message: "Master skill has no source_path, so skill mode=view cannot lazy-load the external methodology",
			Detail:  "Re-register the master source or add source_path to the master skill frontmatter.",
		})
		return
	}
	source := tools.NormalizeHostPath(strings.TrimSpace(skill.SourcePath))
	info, err := os.Stat(source)
	if err != nil {
		add(DoctorCheck{
			Name:    "Master skill source",
			Status:  "warn",
			Message: "Registered master skill source path is not reachable",
			Detail:  err.Error(),
		})
		return
	}
	if !masterSkillWrapperHasAdapter(skill.Body) {
		add(DoctorCheck{
			Name:    "Master skill adapter",
			Status:  "warn",
			Message: "Master wrapper is missing TheMauler/local-LLM adapter guidance",
			Detail:  "Re-register the master source or update master.md so local models use focused skill mode=view queries instead of treating the source as a replacement system prompt.",
		})
	} else {
		add(DoctorCheck{
			Name:    "Master skill adapter",
			Status:  "ok",
			Message: "TheMauler/local-LLM adapter guidance is present",
		})
	}
	stats := scanMasterSkillSource(source, info)
	if stats.markdownFiles == 0 {
		add(DoctorCheck{
			Name:    "Master skill source",
			Status:  "warn",
			Message: "Registered master source contains no markdown files",
			Detail:  source,
		})
		return
	}
	status := "ok"
	message := fmt.Sprintf("%d markdown files, %s total, lazy skill mode=view outline/query mode available", stats.markdownFiles, humanBytes(stats.totalBytes))
	if stats.totalBytes > 2*1024*1024 || stats.markdownFiles > 50 {
		status = "info"
		message = fmt.Sprintf("Large master source: %d markdown files, %s total; use focused skill mode=view queries", stats.markdownFiles, humanBytes(stats.totalBytes))
	}
	add(DoctorCheck{
		Name:    "Master skill source",
		Status:  status,
		Message: message,
		Detail:  "Default skill mode=view returns an outline; focused queries rank methodology sections first.",
	})
	if len(stats.loadAllWarnings) > 0 {
		add(DoctorCheck{
			Name:    "Master skill load-all language",
			Status:  "warn",
			Message: "External source contains instructions that may encourage broad loading",
			Detail:  strings.Join(stats.loadAllWarnings, "\n"),
		})
	}
}

func masterSkillWrapperHasAdapter(body string) bool {
	lower := strings.ToLower(body)
	for _, phrase := range []string{
		"themauler's system prompt",
		"focused query",
		"terminal_send",
		"evidence policy",
	} {
		if !strings.Contains(lower, strings.ToLower(phrase)) {
			return false
		}
	}
	return true
}

type masterSkillSourceStats struct {
	markdownFiles   int
	totalBytes      int64
	loadAllWarnings []string
}

func scanMasterSkillSource(source string, info os.FileInfo) masterSkillSourceStats {
	var stats masterSkillSourceStats
	scanFile := func(path string, fileInfo os.FileInfo) {
		if fileInfo == nil || fileInfo.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return
		}
		stats.markdownFiles++
		stats.totalBytes += fileInfo.Size()
		if len(stats.loadAllWarnings) >= 5 || fileInfo.Size() > 512*1024 {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if phrase := firstMasterLoadAllPhrase(string(data)); phrase != "" {
			label := filepath.Base(path)
			if rel, err := filepath.Rel(source, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				label = filepath.ToSlash(rel)
			}
			stats.loadAllWarnings = append(stats.loadAllWarnings, label+": "+phrase)
		}
	}
	if !info.IsDir() {
		scanFile(source, info)
		return stats
	}
	_ = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != source && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		fileInfo, err := d.Info()
		if err != nil {
			return nil
		}
		scanFile(path, fileInfo)
		return nil
	})
	return stats
}

func firstMasterLoadAllPhrase(content string) string {
	lower := strings.ToLower(content)
	for _, phrase := range []string{
		"read this whole document",
		"read the whole document",
		"loaded at boot",
		"load unconditionally",
		"always load",
		"must load",
		"direct load",
	} {
		if strings.Contains(lower, phrase) {
			return phrase
		}
	}
	return ""
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
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
			return "warn", "Cannot reach llama.cpp /health - is the server running?", err2.Error()
		}
		defer resp2.Body.Close()
		return "info", "llama.cpp is running (version unknown - /props not available)", ""
	}
	defer resp.Body.Close()
	// We can't parse the full response without JSON parsing, but getting a 200 is enough.
	return "ok", "llama.cpp is running (/props available - likely b9180+ for MTP support)", ""
}

func severeContextUndersize(requested, actual int) bool {
	if requested <= 0 || actual <= 0 {
		return false
	}
	return actual < requested/2 || (requested >= 32000 && actual <= 8192)
}

type gpuVRAMInfo struct {
	Name     string
	TotalMiB int
}

func addGPUVRAMDoctorCheck(add func(DoctorCheck), profile settings.Profile) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil {
		add(DoctorCheck{
			Name:    "GPU VRAM",
			Status:  "info",
			Message: "Could not read NVIDIA VRAM from nvidia-smi",
			Detail:  err.Error(),
		})
		return
	}
	gpus := parseNvidiaSMIVRAM(string(out))
	if len(gpus) == 0 {
		add(DoctorCheck{
			Name:    "GPU VRAM",
			Status:  "info",
			Message: "nvidia-smi did not report GPU VRAM",
		})
		return
	}
	var total int
	var parts []string
	for _, gpu := range gpus {
		total += gpu.TotalMiB
		parts = append(parts, fmt.Sprintf("%s: %d MiB", gpu.Name, gpu.TotalMiB))
	}
	estimate, ok := estimateProfileVRAMMiB(profile)
	status := "ok"
	message := fmt.Sprintf("Detected %d MiB total NVIDIA VRAM", total)
	if ok {
		message = fmt.Sprintf("Detected %d MiB VRAM; active profile estimate is %d MiB", total, estimate)
		parts = append(parts, fmt.Sprintf("active profile estimate: %d MiB (%s @ %d ctx)", estimate, profile.ModelID, profile.CtxTokens))
		if estimate > total {
			status = "warn"
			message = fmt.Sprintf("Active profile may not fit detected VRAM: estimate %d MiB > %d MiB", estimate, total)
		} else if total-estimate < 2048 {
			status = "warn"
			message = fmt.Sprintf("Active profile has tight VRAM headroom: estimate %d MiB of %d MiB", estimate, total)
		}
	}
	add(DoctorCheck{
		Name:    "GPU VRAM",
		Status:  status,
		Message: message,
		Detail:  strings.Join(parts, "\n"),
	})
}

func parseNvidiaSMIVRAM(out string) []gpuVRAMInfo {
	var gpus []gpuVRAMInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		memText := strings.TrimSpace(parts[1])
		memText = strings.TrimSuffix(strings.TrimSpace(memText), "MiB")
		total, err := strconv.Atoi(strings.TrimSpace(memText))
		if err != nil || total <= 0 {
			continue
		}
		gpus = append(gpus, gpuVRAMInfo{Name: name, TotalMiB: total})
	}
	return gpus
}

func estimateProfileVRAMMiB(profile settings.Profile) (int, bool) {
	paramsB, ok := modelParamsB(profile.ModelID)
	if !ok || profile.CtxTokens <= 0 {
		return 0, false
	}
	quantBytes := 0.60
	switch modelQuantTag(profile.ModelID) {
	case "q2_k", "ud-q2_k_xl":
		quantBytes = 0.36
	case "q3_k_s", "q3_k_m", "q3_k_l":
		quantBytes = 0.48
	case "q4_0", "q4_1", "q4_k_s", "q4_k_m", "ud-q4_k_xl":
		quantBytes = 0.58
	case "q5_0", "q5_1", "q5_k_s", "q5_k_m":
		quantBytes = 0.70
	case "q6_k":
		quantBytes = 0.82
	case "q8_0":
		quantBytes = 1.05
	}
	modelMiB := paramsB * 1000 * quantBytes
	kvPerTokenMiB := 0.11
	if paramsB < 15 {
		kvPerTokenMiB = 0.06
	} else if paramsB < 25 {
		kvPerTokenMiB = 0.08
	}
	kvMiB := float64(profile.CtxTokens) * kvPerTokenMiB
	const overheadMiB = 1536
	return int(modelMiB + kvMiB + overheadMiB), true
}

func modelParamsB(modelID string) (float64, bool) {
	re := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*b`)
	match := re.FindStringSubmatch(modelID)
	if len(match) != 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(match[1], 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func addSharedTerminalDoctorCheck(add func(DoctorCheck), state TerminalStateSnapshot) {
	status := "ok"
	detail := strings.Join(state.Lines, "\n")
	switch strings.TrimSpace(state.State) {
	case "", "missing", "closed":
		status = "info"
	case "ready":
		status = "ok"
	case "listener", "running", "interactive_prompt", "busy":
		status = "warn"
		if detail != "" {
			detail += "\n"
		}
		detail += "Use Terminal Recover for stale busy state, terminal_read/terminal_send for intentional listeners/prompts, or start long jobs with background=true."
	default:
		status = "info"
	}
	message := state.Summary
	if strings.TrimSpace(message) == "" {
		message = "Shared terminal state: " + firstNonEmpty(state.State, "unknown")
	}
	add(DoctorCheck{
		Name:    "Shared terminal state",
		Status:  status,
		Message: message,
		Detail:  detail,
	})
}

func addToolingSmokeChecks(add func(DoctorCheck)) {
	var failures []string

	sess := &shellSession{scroll: newTerminalScrollback(20), screen: newTerminalScreen(80, 10)}
	updateShellSessionOSCState(sess, []byte("\x1b]133;D;0\x07\x1b]133;P;cwd=/tmp/mauler\x07\x1b]133;A\x07"))
	if !terminalSessionPromptReady(sess) || terminalExitLabel(sess) != "0" || terminalCWDLabel(sess) != "/tmp/mauler" {
		failures = append(failures, "OSC-133 prompt/exit/cwd parser did not update terminal session state")
	}

	sess.scroll.append("22/tcp open ssh")
	sess.scroll.append("80/tcp open http Apache")
	search, matches := formatTerminalSearchView(sess, 10, "history", "Apache")
	if matches != 1 || !strings.Contains(search, "80/tcp") {
		failures = append(failures, "terminal history grep did not return expected match")
	}

	shellResult := formatSharedTerminalResult([]terminalOutput{{stream: "stdout", data: "ok"}}, "wsl", 0, time.Millisecond)
	if !strings.Contains(shellResult, "[shell_result state=done") || !strings.Contains(shellResult, "exit: 0") {
		failures = append(failures, "shared shell result contract missing state/exit")
	}

	runScript := formatRunScriptResult("done", 1, "read", "", "", "ok", "", "", 5, 10)
	if !strings.Contains(runScript, "[run_script_result state=done]") || !strings.Contains(runScript, "inner_tool_calls: 1") {
		failures = append(failures, "run_script result contract missing state/inner_tool_calls")
	}

	if len(failures) > 0 {
		add(DoctorCheck{
			Name:    "Tooling smoke tests",
			Status:  "fail",
			Message: fmt.Sprintf("%d tooling reliability checks failed", len(failures)),
			Detail:  strings.Join(failures, "\n"),
		})
		return
	}
	add(DoctorCheck{
		Name:    "Tooling smoke tests",
		Status:  "ok",
		Message: "Terminal markers, terminal grep, shell contracts, and run_script contracts parse correctly",
	})
}

// fetchLlamacppContext reads the active llama.cpp context size from /slots,
// /props, InferenceBridge's /v1/models/stats, then /v1/health KV cache metadata.
func fetchLlamacppContext(baseURL string) (int, error) {
	return fetchLlamacppContextWithClient(baseURL, &http.Client{Timeout: 3 * time.Second})
}

func fetchLlamacppContextWithClient(baseURL string, client *http.Client) (int, error) {
	base := strings.TrimSuffix(baseURL, "/v1")
	if ctx, err := fetchLlamacppSlotsContextWithClient(base, client); err == nil && ctx > 0 {
		return ctx, nil
	}
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
	if ctx, err := fetchInferenceBridgeStatsContextWithClient(base, client); err == nil && ctx > 0 {
		return ctx, nil
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

func fetchLlamacppSlotsContextWithClient(base string, client *http.Client) (int, error) {
	resp, err := client.Get(base + "/slots")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("slots HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	type llamaSlot struct {
		NCtx int `json:"n_ctx"`
	}
	var slots []llamaSlot
	if err := json.Unmarshal(data, &slots); err == nil {
		for _, slot := range slots {
			if slot.NCtx > 0 {
				return slot.NCtx, nil
			}
		}
	}
	var wrapped struct {
		Value []llamaSlot `json:"value"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return 0, err
	}
	for _, slot := range wrapped.Value {
		if slot.NCtx > 0 {
			return slot.NCtx, nil
		}
	}
	return 0, nil
}

func fetchInferenceBridgeStatsContextWithClient(base string, client *http.Client) (int, error) {
	resp, err := client.Get(base + "/v1/models/stats")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("stats HTTP %d", resp.StatusCode)
	}
	var stats any
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return 0, err
	}
	for _, key := range []string{"actual_ctx_tokens", "actual_ctx", "context_size", "ctx_tokens", "ctx_size", "n_ctx", "num_ctx"} {
		if value := findNumericJSONKey(stats, key); value > 0 {
			return value, nil
		}
	}
	return 0, nil
}

type inferenceBridgeProgress struct {
	State string
	Stage string
	Ready bool
	Raw   string
}

func addInferenceBridgeProgressCheck(add func(DoctorCheck), baseURL string) {
	base := strings.TrimSuffix(baseURL, "/v1")
	progress, err := fetchInferenceBridgeProgressWithClient(base, &http.Client{Timeout: 3 * time.Second})
	if err != nil {
		add(DoctorCheck{
			Name:    "InferenceBridge progress state",
			Status:  "info",
			Message: "Could not read /v1/models/stats progress state",
			Detail:  err.Error(),
		})
		return
	}
	state := strings.ToLower(strings.TrimSpace(progress.State))
	stage := strings.ToLower(strings.TrimSpace(progress.Stage))
	if progress.Ready && (state == "loading" || state == "resolving" || stage == "loading" || stage == "resolving") {
		add(DoctorCheck{
			Name:    "InferenceBridge progress state",
			Status:  "warn",
			Message: fmt.Sprintf("Health looks ready but stats still reports state=%q stage=%q", progress.State, progress.Stage),
			Detail:  "This looks like stale progress state in InferenceBridge, not a model/runtime failure. Reset the progress state after a successful load so UI/Doctor does not show Loading forever.\n" + progress.Raw,
		})
		return
	}
	if progress.State != "" || progress.Stage != "" {
		add(DoctorCheck{
			Name:    "InferenceBridge progress state",
			Status:  "ok",
			Message: fmt.Sprintf("state=%q stage=%q ready=%v", progress.State, progress.Stage, progress.Ready),
		})
	}
}

func fetchInferenceBridgeProgressWithClient(base string, client *http.Client) (inferenceBridgeProgress, error) {
	resp, err := client.Get(base + "/v1/models/stats")
	if err != nil {
		return inferenceBridgeProgress{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return inferenceBridgeProgress{}, fmt.Errorf("stats HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return inferenceBridgeProgress{}, err
	}
	var stats any
	if err := json.Unmarshal(data, &stats); err != nil {
		return inferenceBridgeProgress{}, err
	}
	progress := inferenceBridgeProgress{
		State: firstJSONTextKey(stats, "state", "status"),
		Stage: firstJSONTextKey(stats, "stage", "phase"),
		Ready: findBoolJSONKey(stats, "ready") || strings.EqualFold(firstJSONTextKey(stats, "health", "server_state"), "ok"),
		Raw:   truncateRunes(string(data), 1200),
	}
	if strings.Contains(strings.ToLower(string(data)), `"ok"`) || strings.Contains(strings.ToLower(string(data)), `"healthy"`) {
		progress.Ready = true
	}
	return progress, nil
}

func addInferenceBridgeAgentEndpointCheck(add func(DoctorCheck), provider settings.Provider) {
	check := probeInferenceBridgeAgentEndpoints(provider, &http.Client{Timeout: 4 * time.Second})
	if check.Status != "" {
		add(check)
	}
}

func probeInferenceBridgeAgentEndpoints(provider settings.Provider, client *http.Client) DoctorCheck {
	if provider.Backend != "llamacpp" {
		return DoctorCheck{}
	}
	base := strings.TrimRight(provider.BaseURL, "/")
	if base == "" {
		return DoctorCheck{}
	}
	if !isLikelyInferenceBridgeBaseURL(base) {
		runtimeProbe := probeProviderJSONRoute(client, provider, "/runtime/doctor", nil)
		if runtimeProbe.Err != nil || runtimeProbe.StatusCode == http.StatusNotFound || runtimeProbe.StatusCode == http.StatusMethodNotAllowed {
			return DoctorCheck{}
		}
	}

	validatorBody := map[string]any{
		"think_tag_style": "qwen",
		"text":            `{"step_id":"doctor","role":"worker","goal":"probe InferenceBridge agent validator","action":"read","arguments":{"path":"AGENTS.md"},"expected_outcome":"file can be inspected","success_check":"validator accepts the action shape","confidence":0.9,"next_step":"continue"}`,
	}
	validator := probeProviderJSONRoute(client, provider, "/reliability/agent-action/validate", validatorBody)
	messages := probeProviderJSONRoute(client, provider, "/messages", map[string]any{})
	embeddings := probeProviderJSONRoute(client, provider, "/embeddings", map[string]any{})

	var missing []string
	var details []string
	addProbeDetail := func(name string, probe doctorRouteProbe) {
		status := "ok"
		if !probe.Exists() {
			status = "missing"
			missing = append(missing, name)
		}
		detail := fmt.Sprintf("%s: %s HTTP %d", name, status, probe.StatusCode)
		if probe.Err != nil {
			detail += " (" + probe.Err.Error() + ")"
		}
		if probe.Body != "" {
			detail += " " + probe.Body
		}
		details = append(details, detail)
	}
	addProbeDetail("agent-action validator", validator)
	addProbeDetail("Anthropic /messages", messages)
	addProbeDetail("OpenAI /embeddings", embeddings)

	if len(missing) > 0 {
		return DoctorCheck{
			Name:    "InferenceBridge agent endpoints",
			Status:  "warn",
			Message: "Some InferenceBridge agent API endpoints are missing: " + strings.Join(missing, ", "),
			Detail:  strings.Join(details, "\n"),
		}
	}
	return DoctorCheck{
		Name:    "InferenceBridge agent endpoints",
		Status:  "ok",
		Message: "Agent validator, Anthropic /messages, and /embeddings routes are reachable",
		Detail:  strings.Join(details, "\n"),
	}
}

type doctorRouteProbe struct {
	StatusCode int
	Body       string
	Err        error
}

func (p doctorRouteProbe) Exists() bool {
	if p.Err != nil {
		return false
	}
	return p.StatusCode > 0 && p.StatusCode != http.StatusNotFound && p.StatusCode != http.StatusMethodNotAllowed
}

func probeProviderJSONRoute(client *http.Client, provider settings.Provider, path string, body any) doctorRouteProbe {
	rawURL := strings.TrimRight(provider.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(string(data)))
	if err != nil {
		return doctorRouteProbe{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if key := providerAPIKey(provider); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return doctorRouteProbe{Err: err}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 800))
	return doctorRouteProbe{
		StatusCode: resp.StatusCode,
		Body:       truncateRunes(strings.TrimSpace(string(respBody)), 300),
	}
}

func isLikelyInferenceBridgeBaseURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return parsed.Port() == "8800" || strings.Contains(host, "inferencebridge") || strings.Contains(host, "inference-bridge")
}

func providerAPIKey(provider settings.Provider) string {
	envName := strings.TrimSpace(provider.APIKeyEnv)
	if envName == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func firstJSONTextKey(value any, keys ...string) string {
	for _, key := range keys {
		if text := findTextJSONKey(value, key); text != "" {
			return text
		}
	}
	return ""
}

func findNumericJSONKey(value any, key string) int {
	switch typed := value.(type) {
	case map[string]any:
		for k, child := range typed {
			if strings.EqualFold(k, key) {
				switch n := child.(type) {
				case float64:
					return int(n)
				case int:
					return n
				case json.Number:
					if i, err := n.Int64(); err == nil {
						return int(i)
					}
				}
			}
			if found := findNumericJSONKey(child, key); found > 0 {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findNumericJSONKey(child, key); found > 0 {
				return found
			}
		}
	}
	return 0
}

func findTextJSONKey(value any, key string) string {
	switch typed := value.(type) {
	case map[string]any:
		for k, child := range typed {
			if strings.EqualFold(k, key) {
				if s, ok := child.(string); ok {
					return s
				}
			}
			if text := findTextJSONKey(child, key); text != "" {
				return text
			}
		}
	case []any:
		for _, child := range typed {
			if text := findTextJSONKey(child, key); text != "" {
				return text
			}
		}
	}
	return ""
}

func findBoolJSONKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for k, child := range typed {
			if strings.EqualFold(k, key) {
				if b, ok := child.(bool); ok {
					return b
				}
			}
			if findBoolJSONKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if findBoolJSONKey(child, key) {
				return true
			}
		}
	}
	return false
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
	return fetchLlamacppBuiltinToolsWithClient(baseURL, &http.Client{Timeout: 3 * time.Second})
}

func fetchLlamacppBuiltinToolsWithClient(baseURL string, client *http.Client) ([]string, error) {
	base := strings.TrimSuffix(baseURL, "/v1")
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
		"read",
		"write",
		"edit",
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

func fetchLlamacppPropsAny(baseURL string, client *http.Client) (any, error) {
	base := strings.TrimSuffix(baseURL, "/v1")
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
	return props, nil
}

type llamaLaunchSignals struct {
	HasJinja       bool
	ReasoningDeep  bool
	FlashAttention bool
	Speculative    bool
	Text           string
}

func llamaLaunchSignalsFromProps(props any) llamaLaunchSignals {
	var entries []string
	collectJSONEntries("", props, &entries)
	text := strings.ToLower(strings.Join(entries, "\n"))
	return llamaLaunchSignals{
		HasJinja:       strings.Contains(text, "--jinja") || hasBoolishJSONSignal(entries, "jinja", true),
		ReasoningDeep:  strings.Contains(text, "--reasoning-format deepseek") || strings.Contains(text, "reasoning_format=deepseek") || strings.Contains(text, "reasoning-format=deepseek"),
		FlashAttention: strings.Contains(text, "--flash-attn on") || strings.Contains(text, "--flash-attn true") || hasBoolishJSONSignal(entries, "flash", true),
		Speculative:    hasSpeculativeJSONSignal(entries),
		Text:           text,
	}
}

func collectJSONEntries(prefix string, value any, out *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			collectJSONEntries(next, child, out)
		}
	case []any:
		for _, child := range typed {
			collectJSONEntries(prefix, child, out)
		}
	case string:
		*out = append(*out, strings.ToLower(prefix+"="+typed))
	case bool:
		if typed {
			*out = append(*out, strings.ToLower(prefix+"=true"))
		} else {
			*out = append(*out, strings.ToLower(prefix+"=false"))
		}
	case float64:
		*out = append(*out, fmt.Sprintf("%s=%g", strings.ToLower(prefix), typed))
	case nil:
		*out = append(*out, strings.ToLower(prefix+"=null"))
	default:
		*out = append(*out, strings.ToLower(fmt.Sprintf("%s=%v", prefix, typed)))
	}
}

func hasBoolishJSONSignal(entries []string, keyNeedle string, wantOn bool) bool {
	keyNeedle = strings.ToLower(keyNeedle)
	for _, entry := range entries {
		entry = strings.ToLower(entry)
		if !strings.Contains(entry, keyNeedle) {
			continue
		}
		if wantOn && (strings.HasSuffix(entry, "=true") || strings.HasSuffix(entry, "=on") || strings.HasSuffix(entry, "=1") || strings.Contains(entry, " "+keyNeedle)) {
			return true
		}
		if !wantOn && (strings.HasSuffix(entry, "=false") || strings.HasSuffix(entry, "=off") || strings.HasSuffix(entry, "=0")) {
			return true
		}
	}
	return false
}

func hasSpeculativeJSONSignal(entries []string) bool {
	for _, entry := range entries {
		lower := strings.ToLower(entry)
		if !(strings.Contains(lower, "spec") || strings.Contains(lower, "draft")) {
			continue
		}
		if strings.Contains(lower, "speculative") || strings.Contains(lower, "spec_type") || strings.Contains(lower, "draft_model") || strings.Contains(lower, "draft_n") {
			if strings.HasSuffix(lower, "=false") || strings.HasSuffix(lower, "=off") || strings.HasSuffix(lower, "=0") || strings.HasSuffix(lower, "=null") {
				continue
			}
			return true
		}
	}
	return false
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
	return fetchLMStudioModelInfoWithClient(baseURL, modelID, &http.Client{Timeout: 5 * time.Second})
}

func fetchLMStudioModelInfoWithClient(baseURL, modelID string, client *http.Client) (lmStudioDoctorModel, bool, error) {
	nativeBase := strings.TrimSuffix(baseURL, "/v1")
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
	hasDraftModel := strings.TrimSpace(profile.SpecDraftModel) != ""
	if profile.SpecType != "" && hasDraftModel {
		status := "ok"
		detail := "The draft model path will be passed to the llama.cpp load API."
		if _, err := os.Stat(profile.SpecDraftModel); err != nil {
			status = "warn"
			detail = fmt.Sprintf("Draft model path was set but could not be read: %v", err)
		}
		add(DoctorCheck{
			Name:    "MTP compatibility",
			Status:  status,
			Message: fmt.Sprintf("Enabled: spec_type=%s draft_n_max=%d with draft model", profile.SpecType, profile.SpecDraftNMax),
			Detail:  detail,
		})
	} else if profile.SpecType != "" && !rp.Supports.MTP {
		add(DoctorCheck{
			Name:    "MTP compatibility",
			Status:  "warn",
			Message: fmt.Sprintf("%s is not marked as MTP-capable but spec_type=%s is enabled", rp.Name, profile.SpecType),
			Detail:  "Set spec_draft_model to a matching GGUF draft model, or disable draft-mtp.",
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
	if profile.SpecType != "" && !hasDraftModel {
		add(DoctorCheck{
			Name:    "MTP bridge build",
			Status:  "info",
			Message: "Self-MTP is enabled (no separate draft model) - this needs an InferenceBridge build that emits --spec-type without -md",
			Detail:  "Confirm in the bridge log a \"Speculative decoding enabled\" (target=speculative) line and --spec-type in the llama-server args. An older bridge silently ignores self-MTP, so generation runs at normal speed with no error.",
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

func addSharedBackendSubagentCheck(add func(DoctorCheck), cfg settings.Settings, profile settings.Profile) {
	if profile.Backend != "llamacpp" {
		return
	}
	enabled := settings.EffectiveEnabledTools(cfg.Tools)
	var subagents []string
	for name, ok := range enabled {
		if ok && (name == "task" || strings.HasPrefix(name, "subagent_")) {
			subagents = append(subagents, name)
		}
	}
	if len(subagents) == 0 {
		add(DoctorCheck{
			Name:    "Subagent backend isolation",
			Status:  "ok",
			Message: "No subagent tools are enabled for the active toolset",
		})
		return
	}
	sort.Strings(subagents)
	add(DoctorCheck{
		Name:    "Subagent backend isolation",
		Status:  "info",
		Message: "Subagents share the active llama.cpp backend",
		Detail:  "Same-model lower-context subagent requests are now reused instead of reloading down. Enabled subagents: " + strings.Join(subagents, ", "),
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
	if quant := modelQuantTag(profile.ModelID); quant != "" {
		switch {
		case strings.Contains(quant, "q2") || strings.Contains(quant, "q3"):
			add(DoctorCheck{
				Name:    "Model quant reliability",
				Status:  "warn",
				Message: fmt.Sprintf("Active model quant %s is below the reliable agent tier", strings.ToUpper(quant)),
				Detail:  "Very small GGUF quants often reduce tool-call discipline and reasoning reliability. For the RTX 3090, prefer UD-Q4_K_XL for Qwen3.6-class agent profiles.",
			})
		case strings.Contains(quant, "q4_k_s"):
			add(DoctorCheck{
				Name:    "Model quant reliability",
				Status:  "info",
				Message: "Active model uses Q4_K_S",
				Detail:  "Q4_K_S is compact and fast, but it may be less reliable for long agentic tool use than UD-Q4_K_XL/Q4_K_M-class variants. Keep benchmarking TTFT and tool discipline before changing quant.",
			})
		case strings.Contains(quant, "q4"):
			add(DoctorCheck{
				Name:    "Model quant reliability",
				Status:  "ok",
				Message: fmt.Sprintf("Active model quant %s is in the expected 3090-friendly range", strings.ToUpper(quant)),
			})
		}
	}
}

func modelQuantTag(modelID string) string {
	lower := strings.ToLower(modelID)
	known := []string{"ud-q4_k_xl", "q8_0", "q6_k", "q5_k_m", "q5_k_s", "q4_k_xl", "q4_k_m", "q4_k_s", "q4_0", "q3_k_m", "q3_k_s", "q2_k"}
	for _, tag := range known {
		if strings.Contains(lower, tag) {
			return tag
		}
	}
	return ""
}

func addLlamacppLaunchAssertions(add func(DoctorCheck), baseURL string, profile settings.Profile) {
	addLlamacppLaunchAssertionsWithClient(add, baseURL, profile, &http.Client{Timeout: 3 * time.Second})
}

func addLlamacppLaunchAssertionsWithClient(add func(DoctorCheck), baseURL string, profile settings.Profile, client *http.Client) {
	props, err := fetchLlamacppPropsAny(baseURL, client)
	if err != nil {
		add(DoctorCheck{
			Name:    "llama.cpp launch assertions",
			Status:  "info",
			Message: "Could not inspect /props for launch flags",
			Detail:  err.Error(),
		})
		return
	}
	signals := llamaLaunchSignalsFromProps(props)
	if signals.HasJinja {
		add(DoctorCheck{Name: "llama.cpp Jinja", Status: "ok", Message: "Jinja/template signal detected"})
	} else {
		add(DoctorCheck{
			Name:    "llama.cpp Jinja",
			Status:  "info",
			Message: "Could not confirm --jinja/use_jinja from /props",
			Detail:  "If tool calls leak as text or </think> appears in responses, reload through InferenceBridge/llama.cpp with Jinja enabled.",
		})
	}
	if strings.Contains(strings.ToLower(profile.ModelID), "qwen") {
		if signals.ReasoningDeep {
			add(DoctorCheck{Name: "llama.cpp reasoning format", Status: "ok", Message: "DeepSeek/Qwen reasoning-format signal detected"})
		} else {
			add(DoctorCheck{
				Name:    "llama.cpp reasoning format",
				Status:  "info",
				Message: "Could not confirm --reasoning-format deepseek from /props",
				Detail:  "For Qwen3-class thinking models, use the DeepSeek-style reasoning format when the backend supports it so <think> stays structured.",
			})
		}
	}
	if signals.FlashAttention {
		add(DoctorCheck{Name: "llama.cpp flash attention", Status: "ok", Message: "Flash-attention signal detected"})
	} else {
		add(DoctorCheck{
			Name:    "llama.cpp flash attention",
			Status:  "info",
			Message: "Could not confirm flash-attention from /props",
			Detail:  "Flash attention is usually the right default for long local-agent contexts when the backend/model supports it.",
		})
	}
	if signals.Speculative {
		add(DoctorCheck{
			Name:    "llama.cpp speculative decoding",
			Status:  "warn",
			Message: "Speculative/draft decoding signal detected",
			Detail:  "MTP can improve speed, but if you see early stops near </think>, repeated empty turns, or truncation loops, disable speculative decoding and re-test stability.",
		})
	} else {
		add(DoctorCheck{Name: "llama.cpp speculative decoding", Status: "ok", Message: "No speculative/draft decoding signal detected in /props"})
	}
}

// addModelTierCheck warns when the active model is below the parameter tier at
// which local tool calling stays reliable. BFCL V4 shows a sharp cliff: a ~9B
// general model scores ~66%, a 4B ~50%, a 2B ~44% - so multi-step agent runs
// spin out well before chat quality visibly drops. Docker's 21-model agent eval
// makes the same point (a tool-tuned 14B beats a 70B that calls tools poorly):
// size is a floor, not the goal.
func addModelTierCheck(add func(DoctorCheck), profile settings.Profile) {
	b := modelParamBillions(profile.ModelID)
	if b <= 0 {
		return
	}
	switch {
	case b < 4:
		add(DoctorCheck{
			Name:    "Model tool-calling tier",
			Status:  "warn",
			Message: fmt.Sprintf("Active model looks ~%gB - below the reliable tool-calling tier", b),
			Detail:  "Agent tool calling degrades sharply under ~7-9B (BFCL V4: ~9B~66%, 4B~50%, 2B~44%). Small models are fine for chat but spin out on multi-step tool use - prefer a 7B+ tool-tuned model for agent runs.",
		})
	case b < 7:
		add(DoctorCheck{
			Name:    "Model tool-calling tier",
			Status:  "info",
			Message: fmt.Sprintf("Active model is ~%gB - near the lower edge of reliable tool calling", b),
			Detail:  "Below ~7-9B, tool-call reliability starts to drop (BFCL V4). Watch for malformed or looping tool calls; move to a larger tool-tuned model if you see them.",
		})
	default:
		add(DoctorCheck{
			Name:    "Model tool-calling tier",
			Status:  "ok",
			Message: fmt.Sprintf("Active model ~%gB is in the reliable tool-calling tier", b),
		})
	}
}

// addLlamacppAgentFlagAdvisory surfaces the research-backed llama.cpp launch
// flags that make Qwen3-class local models stable as agents. These cannot all
// be read back from /props, so it is an advisory (info) check rather than a
// pass/fail - the chat_format, template, and MTP checks above cover the parts
// that are machine-detectable.
func addLlamacppAgentFlagAdvisory(add func(DoctorCheck)) {
	detail := strings.Join([]string{
		"--jinja - convert native <tool_call> output into OpenAI tool_calls. Without it, tool calls and </think> leak as plain text (TheMauler repairs this, but it is a safety net, not a fix).",
		"--reasoning-format deepseek - Qwen3 uses the same <think>/</think> delimiters as DeepSeek-R1.",
		"Disable speculative/draft decoding if you see truncation or repetition loops - draft rejections at </think> spike the EOS probability and cause early termination.",
		"--presence-penalty up to 2.0 if the model loops inside <think> until it runs out of tokens.",
		"Use the highest quant that fits 24 GB VRAM - UD-Q4_K_XL is the project default for Qwen3.6-27B on the RTX 3090; avoid sub-Q4 quants, which hurt tool-call accuracy.",
		"--reasoning-budget N - cap thinking tokens at generation time (llama.cpp PR #20297) so a runaway <think> can't eat the whole turn.",
	}, "\n")
	add(DoctorCheck{
		Name:    "llama.cpp agent flags",
		Status:  "info",
		Message: "Review backend launch flags for Qwen3 tool-call stability",
		Detail:  detail,
	})
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
		{"Web tools", []string{"web_search", "fetch_url", "task"}},
		{"Browser tools", []string{"browser"}},
		{"Write tools", []string{"write", "edit"}},
		{"Shell tools", []string{"shell"}},
	}
	for _, group := range groups {
		enabled := []string{}
		blocked := []string{}
		missing := []string{}
		for _, name := range group.tools {
			if !available[name] {
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

func addInferenceBridgePortConfigCheck(add func(DoctorCheck), provider settings.Provider) {
	if provider.Backend != "llamacpp" {
		return
	}
	base, err := url.Parse(provider.BaseURL)
	if err != nil || base.Hostname() == "" {
		return
	}
	host := strings.ToLower(base.Hostname())
	if host != "127.0.0.1" && host != "localhost" {
		return
	}
	wantPort := base.Port()
	if wantPort == "" {
		return
	}
	var mismatches []string
	for _, path := range inferenceBridgeConfigCandidates() {
		port, ok := readInferenceBridgeConfigPort(path)
		if !ok || port == "" || port == wantPort {
			continue
		}
		mismatches = append(mismatches, fmt.Sprintf("%s has server.port=%s", path, port))
	}
	if len(mismatches) == 0 {
		return
	}
	add(DoctorCheck{
		Name:    "InferenceBridge port config",
		Status:  "warn",
		Message: fmt.Sprintf("Mauler provider points at %s but an InferenceBridge config uses a different port", provider.BaseURL),
		Detail:  strings.Join(mismatches, "\n") + "\nKeep Mauler profiles.toml and InferenceBridge's active config on the same port, otherwise runs can fail after bridge restarts.",
	})
}

func inferenceBridgeConfigCandidates() []string {
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		clean := filepath.Clean(path)
		key := strings.ToLower(clean)
		if seen[key] {
			return
		}
		seen[key] = true
		paths = append(paths, clean)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		add(filepath.Join(local, "InferenceBridge", "inference-bridge.toml"))
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		add(filepath.Join(appdata, "InferenceBridge", "inference-bridge.toml"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "Documents", "InferenceBridge", "inference-bridge.toml"))
		add(filepath.Join(home, ".config", "InferenceBridge", "inference-bridge.toml"))
	}
	return paths
}

func readInferenceBridgeConfigPort(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	inServer := false
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inServer = strings.EqualFold(strings.Trim(line, "[] "), "server")
			continue
		}
		if !inServer || !strings.HasPrefix(line, "port") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != "port" {
			continue
		}
		port := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		return port, port != ""
	}
	return "", false
}

func addShellNetworkBoundaryCheck(add func(DoctorCheck), cfg settings.Settings, shellBackend string) {
	if runtime.GOOS != "windows" || !strings.EqualFold(strings.TrimSpace(shellBackend), "wsl") {
		return
	}
	effective := settings.EffectiveEnabledTools(cfg.Tools)
	hostSideTools := []string{}
	for _, name := range []string{"fetch_url", "browser"} {
		if effective[name] {
			hostSideTools = append(hostSideTools, name)
		}
	}
	if len(hostSideTools) == 0 {
		add(DoctorCheck{
			Name:    "WSL/Kali target routing",
			Status:  "ok",
			Message: "Target interaction is shell-only from WSL/Kali",
			Detail:  "Browser/fetch tools are disabled by the active toolset or per-tool toggles.",
		})
		return
	}
	distro := strings.TrimSpace(cfg.Tools.ShellDistro)
	if distro == "" {
		distro = "default WSL"
	}
	add(DoctorCheck{
		Name:    "WSL/Kali target routing",
		Status:  "warn",
		Message: fmt.Sprintf("Shell runs in %s, but browser/fetch tools run from Windows", distro),
		Detail:  fmt.Sprintf("For HTB/Kali targets, prefer shell tools such as curl, nmap, ffuf, gobuster, and nc inside WSL. Windows-side tools may not share WSL /etc/hosts, VPN routing, or Kali tooling. Enabled host-side tools: %s. Use the local-code/offline toolset for WSL-only target work, or use IP addresses when a visible Windows browser is required.", strings.Join(hostSideTools, ", ")),
	})
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
