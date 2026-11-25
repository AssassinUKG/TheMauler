package app

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/telegram"
	"mauler/internal/tools"
)

// Credential-safe account check only: no poller, webhook change or chat message.
func TestConfiguredTelegramIdentityReadOnly(t *testing.T) {
	if os.Getenv("MAULER_LIVE_TELEGRAM_IDENTITY") != "1" {
		t.Skip("explicit configured bot identity check")
	}
	cfg, err := settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Telegram.Enabled || strings.TrimSpace(cfg.Telegram.Token) == "" {
		t.Fatal("configured Telegram bot is not enabled")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	identity, err := telegram.NewClient(cfg.Telegram.Token).GetMe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID == 0 {
		t.Fatal("Bot API returned no bot identity")
	}
	t.Log("configured bot authenticated; messages sent=0; webhook/poller changes=0")
}

// Isolated real-model agent path. No production bot or configuration is opened.
func TestLocalTelegramNewsPipeline(t *testing.T) {
	base, key := os.Getenv("MAULER_NEWS_LOCAL_URL"), os.Getenv("MAULER_NEWS_LOCAL_KEY")
	if base == "" {
		t.Skip("explicit already-loaded loopback news qualification")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Path != "/v1" || u.User != nil || u.RawQuery != "" {
		t.Fatal("local-only provider boundary")
	}
	runtime := &localQualificationRuntime{base: base, key: key, http: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if !runtime.idle() {
		t.Fatal("loaded model must be idle")
	}
	status, err := runtime.get("runtime/status")
	if err != nil {
		t.Fatal(err)
	}
	runtime.model, _ = status["model"].(string)
	preview, _ := status["last_launch_preview"].(map[string]any)
	count, _ := preview["context_size"].(float64)
	runtime.context = int(count)
	if runtime.model == "" || runtime.context < 8192 {
		t.Fatal("already-loaded model and context required")
	}
	processStateMu.Lock()
	defer processStateMu.Unlock()
	oldTools := qualificationTools
	qualificationTools = map[string]bool{"web_search": true, "fetch_url": true}
	defer func() { qualificationTools = oldTools }()
	root := t.TempDir()
	app, profile, client, restore := qualificationApp(t, root, runtime)
	defer restore()
	// The older file-only fixture deliberately denies network access. This fixture
	// admits only public news observation, not shell, filesystem or browser actions.
	if err := os.WriteFile(filepath.Join(app.cfg.Context.WorkspaceDir, "AGENTS.md"), []byte("# News qualification\nUse only the available read-only web search/fetch tools for dated public headlines. No file creation, shell, login, messaging, or other external side effects.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app.registry.Register(&tools.WebSearch{})
	app.registry.Register(&tools.FetchURL{})
	prompt := "What’s the latest news headlines today?"
	mode := selectAgentMode(prompt, *app.cfg)
	run := startTaskRun(prompt, mode.Name, profile.Name, profile.ModelID)
	run.Origin = "telegram"
	run.ConversationEpoch = app.currentConversationEpoch()
	run.conversationMode = conversationModeAdaptive
	run.RoutingPrompt = prompt
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	final := app.runAgentLoop(ctx, llm.NewTextMessage(llm.RoleUser, prompt), profile, app.cfg, true, mode, nil, nil, run)
	if !client.joined() {
		t.Fatal("local inference reader did not join")
	}
	if !runtime.unchangedAndIdle() {
		t.Fatal("local model changed or request remained active")
	}
	var observed bool
	for _, tool := range final.Tools {
		if tool.Name == "web_search" && tool.Status == "done" && strings.Contains(tool.Result, "# Publisher headlines") {
			observed = true
		}
	}
	t.Logf("model=%s state=%s stop=%s tools=%d reply=%s", runtime.model, final.Status, final.StopReason, len(final.Tools), final.Response)
	// Jade-style Sources is populated from observations, not from links the model
	// may or may not repeat in its prose. Require that real source evidence here.
	sources := telegramObservedSources(final)
	if !observed || final.Status != "done" || len(final.Response) < 150 || !strings.Contains(sources, "http") || !strings.Contains(sources, "Publisher feed entry") || strings.Contains(strings.ToLower(final.Response), "no tools were available") {
		t.Fatal("exact news prompt did not return a sourced local-model answer")
	}
}
