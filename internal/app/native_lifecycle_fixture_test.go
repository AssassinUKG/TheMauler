package app

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"mauler/internal/settings"
)

// Operator-only preparation. The executable under test still uses its normal
// Wails entry points; this helper supplies isolated settings, never UI actions.
func TestPrepareNativeLifecycleFixture(t *testing.T) {
	root, key := os.Getenv("MAULER_NATIVE_TEST_ROOT"), os.Getenv("MAULER_NATIVE_LOCAL_KEY")
	if root == "" || key == "" {
		t.Skip("explicit disposable root and ephemeral local key required")
	}
	if !filepath.IsAbs(root) {
		t.Fatal("absolute root required")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("fresh root required")
	}
	runtime := &localQualificationRuntime{base: "http://127.0.0.1:8804/v1", key: key, http: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	models, err := runtime.get("models")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range models["data"].([]any) {
		model := row.(map[string]any)
		if model["active"] == true && model["state"] == "loaded" {
			if runtime.model != "" {
				t.Fatal("only one loaded model required")
			}
			runtime.model = model["id"].(string)
		}
	}
	ctx, err := runtime.get("context/status")
	if err != nil {
		t.Fatal(err)
	}
	runtime.context = int(ctx["total_tokens"].(float64))
	if runtime.model == "" || runtime.context < 4096 || !runtime.idle() {
		t.Fatal("loaded idle local model/context required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(root, "config")
	t.Setenv("MAULER_CONFIG_DIR", configDir)
	cfg := canonicalAgentEvalSettings(settings.DefaultSettings())
	cfg.ActiveProfile = "native-local-qualification"
	cfg.Context.WorkspaceDir = filepath.Join(root, "workspace-a")
	cfg.Context.MAULERMDPath = "AGENTS.md"
	cfg.Context.OpenFolders = []settings.WorkspaceFolder{{Path: cfg.Context.WorkspaceDir, Name: "Native workspace A", Role: "root"}, {Path: filepath.Join(root, "workspace-b"), Name: "Native workspace B", Role: "folder"}}
	cfg.UI.TerminalDefaultOpen = false
	for _, name := range []string{"workspace-a", "workspace-b"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Native lifecycle fixture\nComplete only the requested task in this disposable folder. No shell, browser, remote service or cloud actions.\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sentinel.txt"), []byte(name+" native fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "append-fixture.txt"), []byte("original native prefix Ω\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Agents.OfflineOnly = true
	cfg.Agents.EscalationProfile = ""
	cfg.Agents.MaxRunSeconds, cfg.Agents.MaxToolCalls = 120, 16
	cfg.Agents.ThinkingMode, cfg.Agents.ReasoningEffort = "off", "none"
	cfg.Agents.ReviewLoop.OnlyAutonomous = false
	cfg.Audio.Enabled, cfg.Telegram.Enabled = false, false
	cfg.Memory.Enabled, cfg.Memory.AutoInject, cfg.Memory.DisableAutoDistill = false, false, true
	cfg.Skills.Enabled, cfg.Skills.AutoInject = false, false
	cfg.Tools.EnabledTools = map[string]bool{}
	names := []string{"read", "write", "edit", "glob", "grep", "todo_write"}
	for _, name := range names {
		cfg.Tools.EnabledTools[name] = true
	}
	cfg.Tools.Toolsets = map[string][]string{"qualification": names, "unrestricted": names}
	cfg.Tools.ActiveToolset = "qualification"
	for name, preset := range cfg.Agents.Presets {
		preset.Profile = ""
		preset.Toolset = "qualification"
		cfg.Agents.Presets[name] = preset
	}
	profile := settings.Profile{Name: cfg.ActiveProfile, Provider: "native-local", Backend: "llamacpp", BaseURL: runtime.base, APIKeyEnv: "MAULER_NATIVE_LOCAL_KEY", ModelID: runtime.model, CtxTokens: runtime.context, NoThink: settings.GenerationParams{Temperature: 0.1, TopP: 0.95, TopK: 20, MaxTokens: 2048, Seed: -1}}
	profiles := settings.ProfilesFile{Profiles: map[string]settings.Profile{profile.Name: profile}, Providers: map[string]settings.Provider{"native-local": {Name: "native-local", Backend: "llamacpp", BaseURL: runtime.base, APIKeyEnv: "MAULER_NATIVE_LOCAL_KEY"}}}
	if err := settings.Save(&cfg); err != nil {
		t.Fatal(err)
	}
	if err := settings.SaveProfiles(&profiles); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.MarshalIndent(map[string]any{"root": root, "config": configDir, "workspaceA": cfg.Context.WorkspaceDir, "workspaceB": filepath.Join(root, "workspace-b"), "model": runtime.model, "contextTokens": runtime.context, "localOnly": true, "credentialsPersisted": false}, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "fixture.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("Prepared isolated native fixture; no model calls, loading or persistent credentials.")
}

// Read-only disk observation; never calls New, OnStartup, a provider or UI.
func TestInspectNativeLifecycleFixture(t *testing.T) {
	root, tag := os.Getenv("MAULER_NATIVE_TEST_ROOT"), os.Getenv("MAULER_NATIVE_OBSERVATION")
	if root == "" || tag == "" {
		t.Skip("explicit fixture and fresh observation required")
	}
	if !filepath.IsAbs(root) || !regexp.MustCompile(`^[a-z0-9-]{1,80}$`).MatchString(tag) {
		t.Fatal("invalid fixture observation")
	}
	if _, err := os.Stat(filepath.Join(root, "fixture.json")); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(root, "config", "state.db"))+"?mode=ro&_pragma=busy_timeout(1000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	app := &App{db: db}
	runs, err := app.listTaskRuns()
	if err != nil {
		t.Fatal(err)
	}
	checkpoints, err := app.ListResumableRuns()
	if err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	for _, run := range runs {
		phase := ""
		if run.Control != nil {
			phase = string(run.Control.Phase)
		}
		rows = append(rows, map[string]any{"id": run.ID, "generation": run.Generation, "parent": run.ParentRunID, "status": run.Status, "state": run.State, "stopReason": run.StopReason, "phase": phase, "tools": len(run.Tools), "response": run.Response})
	}
	data, err := json.MarshalIndent(map[string]any{"schemaVersion": "mauler-native-disk-observation-v1", "tag": tag, "runs": rows, "checkpoints": checkpoints}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.OpenFile(filepath.Join(root, tag+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("Observed %d runs and %d checkpoints; read-only, no model calls.", len(runs), len(checkpoints))
}
