package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mauler/internal/agent"
	"mauler/internal/channelbus"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/store"
	"mauler/internal/tools"
)

func restoreWorkingDir(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	})
}

func TestDispatchChannelSideChatDoesNotStartRun(t *testing.T) {
	client := &sideChatRecordingClient{reply: "llm side chat reply"}
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		profiles:     &settings.ProfilesFile{Profiles: map[string]settings.Profile{"test": {ModelID: "fake", Backend: "fake", Thinking: true, PreserveThink: true}}},
		channelQueue: channelbus.NewQueue(),
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "what is the current status?",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if resp.Lane != channelbus.LaneSideChat || resp.Status != "chat" {
		t.Fatalf("expected read-only side chat response, got %+v", resp)
	}
	if !strings.Contains(resp.Message, "llm side chat reply") {
		t.Fatalf("side chat did not use LLM path: %q", resp.Message)
	}
	if app.agentRunning {
		t.Fatal("side chat started an agent run")
	}
	if got := len(app.ListChannelWorkQueue()); got != 0 {
		t.Fatalf("side chat queued work, got %d items", got)
	}
	if client.lastReq.ToolChoice != "none" || client.lastReq.EnableThinking || client.lastReq.PreserveThinking || client.lastReq.ReasoningEffort != "none" {
		t.Fatalf("side chat should force no-tool/no-thinking request, got tool_choice=%q thinking=%v preserve=%v effort=%q",
			client.lastReq.ToolChoice, client.lastReq.EnableThinking, client.lastReq.PreserveThinking, client.lastReq.ReasoningEffort)
	}
	prompt := fmt.Sprintf("%#v", client.lastReq.Messages)
	if !strings.Contains(prompt, "local Windows machine") || strings.Contains(strings.ToLower(prompt), "cloud-hosted") && !strings.Contains(prompt, "Do not claim") {
		t.Fatalf("side chat prompt should anchor MaulBot as local, got %s", prompt)
	}
	if !strings.Contains(prompt, "synthesize your final text into a Telegram voice note") {
		t.Fatalf("side chat prompt should advertise Telegram voice transport, got %s", prompt)
	}
}

type sideChatEchoClient struct{}

func (sideChatEchoClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	ch <- llm.Delta{Content: "llm side chat reply"}
	close(ch)
	return ch, nil
}

func (sideChatEchoClient) Models(ctx context.Context) ([]string, error) { return []string{"fake"}, nil }
func (sideChatEchoClient) Ping(ctx context.Context) error               { return nil }
func (sideChatEchoClient) Name() string                                 { return "fake-side-chat" }

type sideChatRecordingClient struct {
	reply   string
	lastReq llm.Request
}

func (c *sideChatRecordingClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	c.lastReq = req
	ch := make(chan llm.Delta, 1)
	if c.reply != "" {
		ch <- llm.Delta{Content: c.reply}
	}
	close(ch)
	return ch, nil
}

func (c *sideChatRecordingClient) Models(ctx context.Context) ([]string, error) {
	return []string{"fake"}, nil
}
func (c *sideChatRecordingClient) Ping(ctx context.Context) error { return nil }
func (c *sideChatRecordingClient) Name() string                   { return "fake-side-chat-recording" }

type sideChatSequenceClient struct {
	replies []string
	calls   int
}

func (c *sideChatSequenceClient) Chat(ctx context.Context, req llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	reply := ""
	if c.calls < len(c.replies) {
		reply = c.replies[c.calls]
	}
	c.calls++
	if reply != "" {
		ch <- llm.Delta{Content: reply}
	}
	close(ch)
	return ch, nil
}

func (c *sideChatSequenceClient) Models(ctx context.Context) ([]string, error) {
	return []string{"fake"}, nil
}
func (c *sideChatSequenceClient) Ping(ctx context.Context) error { return nil }
func (c *sideChatSequenceClient) Name() string                   { return "fake-side-chat-sequence" }

func TestDispatchChannelSideChatSuppressesToolCallText(t *testing.T) {
	client := &sideChatSequenceClient{replies: []string{
		`run_command(command="powershell -ExecutionPolicy Bypass -File setup.ps1")`,
		`{"name":"run_command","args":{"command":"powershell -ExecutionPolicy Bypass -File setup.ps1"}}`,
	}}
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		profiles:     &settings.ProfilesFile{Profiles: map[string]settings.Profile{"test": {ModelID: "fake", Backend: "fake"}}},
		channelQueue: channelbus.NewQueue(),
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "Can you tell me what you can do?",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if strings.Contains(resp.Message, "run_command") || strings.Contains(resp.Message, `"name"`) {
		t.Fatalf("side chat leaked tool-call text: %q", resp.Message)
	}
	if !strings.Contains(resp.Message, "/cmd") {
		t.Fatalf("side chat tool leak fallback should point to /cmd, got %q", resp.Message)
	}
}

func TestDispatchChannelVoiceSideChatRetriesCannotVoiceClaim(t *testing.T) {
	client := &sideChatSequenceClient{replies: []string{
		"I can't send voice messages, but I'm ready to chat via text.",
		"Loud and clear. What should we do next?",
	}}
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		profiles:     &settings.ProfilesFile{Profiles: map[string]settings.Profile{"test": {ModelID: "fake", Backend: "fake"}}},
		channelQueue: channelbus.NewQueue(),
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:      "telegram",
		SessionID:   "telegram:direct:1",
		Text:        "can you hear me",
		Attachments: []channelbus.Attachment{{Kind: "voice", ContentType: "audio/ogg", Text: "can you hear me"}},
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("expected retry after cannot-voice claim, got %d calls", client.calls)
	}
	if strings.Contains(strings.ToLower(resp.Message), "can't send voice") || strings.Contains(strings.ToLower(resp.Message), "via text") {
		t.Fatalf("voice disclaimer should have been retried away, got %q", resp.Message)
	}
	if !strings.Contains(resp.Message, "Loud and clear") {
		t.Fatalf("expected retry reply, got %q", resp.Message)
	}
}

func TestDispatchChannelSideChatEmptyModelFallsBackToStatus(t *testing.T) {
	client := &sideChatRecordingClient{}
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test", Context: settings.ContextConfig{WorkspaceDir: "C:/workspace"}},
		profiles:     &settings.ProfilesFile{Profiles: map[string]settings.Profile{"test": {ModelID: "fake", Backend: "fake", Thinking: true}}},
		channelQueue: channelbus.NewQueue(),
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "What projects do we have on the go?",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if resp.Lane != channelbus.LaneSideChat || resp.Status != "chat" {
		t.Fatalf("expected side chat fallback response, got %+v", resp)
	}
	if strings.Contains(resp.Message, "I heard you, but the model returned an empty reply") {
		t.Fatalf("old empty-reply fallback leaked through: %q", resp.Message)
	}
	if !strings.Contains(resp.Message, "Mauler state") || !strings.Contains(resp.Message, "workspace: C:/workspace") {
		t.Fatalf("fallback should include useful status, got %q", resp.Message)
	}
}

func TestDispatchChannelRunQueuesWhenProjectBusy(t *testing.T) {
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		channelQueue: channelbus.NewQueue(),
		agentRunning: true,
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "/cmd enumerate the target",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if resp.Lane != channelbus.LaneWork || !resp.Queued || resp.Status != "queued_busy" {
		t.Fatalf("expected busy queued work response, got %+v", resp)
	}
	queue := app.ListChannelWorkQueue()
	if len(queue) != 1 {
		t.Fatalf("expected one queued work item, got %d", len(queue))
	}
	if queue[0].Route.Command != "cmd" {
		t.Fatalf("queued wrong route: %+v", queue[0].Route)
	}
}

func TestDispatchChannelSideChatQueuesWhenProjectBusy(t *testing.T) {
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		channelQueue: channelbus.NewQueue(),
		agentRunning: true,
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "Can you add memories?",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if resp.Lane != channelbus.LaneWork || !resp.Queued || resp.Status != "queued_busy" {
		t.Fatalf("expected memory request to queue as work while busy, got %+v", resp)
	}
	queue := app.ListChannelWorkQueue()
	if len(queue) != 1 || queue[0].Route.Command != "cmd" {
		t.Fatalf("unexpected queued item: %+v", queue)
	}
}

func TestDispatchChannelPlainSideChatQueuesWhenProjectBusy(t *testing.T) {
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		channelQueue: channelbus.NewQueue(),
		agentRunning: true,
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "what did you find so far?",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if resp.Lane != channelbus.LaneSideChat || !resp.Queued || resp.Status != "queued_busy" {
		t.Fatalf("expected side chat to queue while busy, got %+v", resp)
	}
	queue := app.ListChannelWorkQueue()
	if len(queue) != 1 || queue[0].Route.Lane != channelbus.LaneSideChat {
		t.Fatalf("unexpected queued side chat: %+v", queue)
	}
}

func TestDispatchChannelQuickActionQueuesWhenProjectBusy(t *testing.T) {
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		channelQueue: channelbus.NewQueue(),
		agentRunning: true,
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "Can you open a tmux terminal session in WSL Kali for me?",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if resp.Lane != channelbus.LaneQuick || !resp.Queued || resp.Status != "queued_busy" {
		t.Fatalf("expected busy queued quick action, got %+v", resp)
	}
	queue := app.ListChannelWorkQueue()
	if len(queue) != 1 || queue[0].Route.Command != "quick_terminal" {
		t.Fatalf("unexpected quick action queue: %+v", queue)
	}
}

func TestDispatchChannelRunQueuesToDBWhenProjectBusy(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := &App{
		cfg:          &settings.Settings{ActiveProfile: "test"},
		db:           db,
		channelQueue: channelbus.NewPersistentQueue(db),
		agentRunning: true,
	}
	resp, err := app.DispatchChannelMessage(ChannelEnvelope{
		Source:    "telegram",
		SessionID: "telegram:direct:1",
		Text:      "/ops continue the box",
	})
	if err != nil {
		t.Fatalf("DispatchChannelMessage returned error: %v", err)
	}
	if !resp.Queued || resp.QueueID == "" {
		t.Fatalf("expected queued response with id, got %+v", resp)
	}
	reloaded := channelbus.NewPersistentQueue(db).List()
	if len(reloaded) != 1 {
		t.Fatalf("expected persisted queued item, got %d", len(reloaded))
	}
	if reloaded[0].Route.Command != "cmd" || reloaded[0].Status != "queued" {
		t.Fatalf("unexpected persisted route: %+v", reloaded[0])
	}
}

func TestApplyTelegramWorkDefaultsUsesUnrestrictedAutonomy(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.ActiveProfile = "qwen3.6-think"
	cfg.Tools.ActiveToolset = "balanced"
	cfg.Tools.ConfirmExec = true
	cfg.Tools.ConfirmWrites = true
	cfg.Telegram.DefaultProfile = "qwen3.6-nothink"
	cfg.Telegram.DefaultMode = "Auto"
	cfg.Telegram.DefaultToolset = "unrestricted"
	app := &App{
		cfg: &cfg,
		profiles: &settings.ProfilesFile{Profiles: map[string]settings.Profile{
			"qwen3.6-think":   {ModelID: "think"},
			"qwen3.6-nothink": {ModelID: "nothink"},
		}},
	}
	app.applyTelegramWorkDefaults()
	if !app.autonomous {
		t.Fatal("telegram unrestricted default should enable autonomous runs")
	}
	if app.cfg.ActiveProfile != "qwen3.6-nothink" || app.cfg.Agents.ModeOverride != "Auto" || app.cfg.Tools.ActiveToolset != "unrestricted" {
		t.Fatalf("telegram defaults not applied: profile=%s mode=%s toolset=%s", app.cfg.ActiveProfile, app.cfg.Agents.ModeOverride, app.cfg.Tools.ActiveToolset)
	}
	if app.cfg.Tools.ConfirmExec || app.cfg.Tools.ConfirmWrites {
		t.Fatalf("unrestricted telegram runs should not retain confirmation prompts: %#v", app.cfg.Tools)
	}
}

func TestQuickTerminalCommandBuildsTmuxSession(t *testing.T) {
	command, label := quickTerminalCommand("Can you open a tmux terminal session in WSL Kali for me and open on my desktop?")
	if command != "tmux new-session -A -s kali" || label != "tmux session kali" {
		t.Fatalf("unexpected quick terminal command=%q label=%q", command, label)
	}
	command, _ = quickTerminalCommand("open a tmux session named HTB-Box_1")
	if command != "tmux new-session -A -s htb-box_1" {
		t.Fatalf("named tmux session not sanitised as expected: %q", command)
	}
}

func TestBuildChatRequestUsesAllActiveProfileGenerationSettings(t *testing.T) {
	profile := settings.Profile{
		Thinking:      true,
		PreserveThink: true,
		ThinkGeneral: settings.GenerationParams{
			Temperature:     0.61,
			TopP:            0.91,
			TopK:            33,
			MinP:            0.07,
			PresencePenalty: 1.25,
			MaxTokens:       7777,
			Seed:            12345,
		},
		NoThink: settings.GenerationParams{
			Temperature: 0.11,
			MaxTokens:   222,
			Seed:        1,
		},
	}
	msgs := []llm.Message{llm.NewTextMessage(llm.RoleUser, "hello")}

	req := buildChatRequest(profile, msgs, nil, "", false, false, "medium")

	if req.MaxTokens != 7777 {
		t.Fatalf("MaxTokens = %d, want 7777", req.MaxTokens)
	}
	if req.Temperature != 0.61 || req.TopP != 0.91 || req.TopK != 33 || req.MinP != 0.07 || req.PresencePenalty != 1.25 {
		t.Fatalf("sampling params not copied correctly: %#v", req)
	}
	if req.Seed != 12345 {
		t.Fatalf("Seed = %d, want 12345", req.Seed)
	}
	if !req.EnableThinking || !req.PreserveThinking {
		t.Fatalf("thinking flags not copied: %#v", req)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content != "hello" {
		t.Fatalf("messages not copied: %#v", req.Messages)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("tools not copied: %#v", req.Tools)
	}
}

func TestBuildChatRequestDisablesThinkingForToolTurns(t *testing.T) {
	profile := settings.Profile{
		Thinking:      true,
		PreserveThink: true,
		ThinkGeneral:  settings.GenerationParams{Temperature: 0.61, MaxTokens: 7777},
		NoThink:       settings.GenerationParams{Temperature: 0.22, MaxTokens: 3333},
	}
	tool := llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:       "read",
			Parameters: json.RawMessage(`{"type":"object"}`),
		},
	}

	req := buildChatRequest(profile, nil, []llm.ToolDef{tool}, "auto", true, false, "medium")

	if req.EnableThinking || req.PreserveThinking {
		t.Fatalf("tool turns must force no-thinking: %#v", req)
	}
	if req.MaxTokens != 3333 || req.Temperature != 0.22 {
		t.Fatalf("tool turns should use no-thinking params: %#v", req)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read" {
		t.Fatalf("tools not copied: %#v", req.Tools)
	}
}

func TestBuildChatRequestUsesNoThinkSettingsWhenThinkingDisabled(t *testing.T) {
	profile := settings.Profile{
		Thinking: false,
		ThinkGeneral: settings.GenerationParams{
			Temperature: 1.0,
			MaxTokens:   8192,
			Seed:        99,
		},
		NoThink: settings.GenerationParams{
			Temperature:     0.44,
			TopP:            0.8,
			TopK:            20,
			MinP:            0.02,
			PresencePenalty: 0.5,
			MaxTokens:       2048,
			Seed:            88,
		},
	}

	req := buildChatRequest(profile, nil, nil, "", false, false, "medium")

	if req.MaxTokens != 2048 || req.Temperature != 0.44 || req.Seed != 88 {
		t.Fatalf("nothinking params not selected: %#v", req)
	}
	if req.EnableThinking {
		t.Fatalf("EnableThinking = true, want false")
	}
}

func TestBuildChatRequestConstrainsSingleRequiredToolForRepairProtocol(t *testing.T) {
	profile := settings.Profile{
		Name:     "gemma4-26b-a4b-qat",
		ModelID:  "Gemma4-26B-A4B-QAT-Q4_K_M.gguf",
		Thinking: false,
		NoThink:  settings.GenerationParams{MaxTokens: 2048},
	}
	tool := llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:       "read",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		},
	}

	req := buildChatRequest(profile, nil, []llm.ToolDef{tool}, "required", false, false, "medium")
	if string(req.JSONSchema) == "" || !strings.Contains(string(req.JSONSchema), `"path"`) {
		t.Fatalf("Gemma repair/mixed profile should constrain single required tool args: %#v", req)
	}

	secondTool := llm.ToolDef{Type: "function", Function: llm.ToolFunctionDef{Name: "glob", Parameters: json.RawMessage(`{"type":"object"}`)}}
	req = buildChatRequest(profile, nil, []llm.ToolDef{tool, secondTool}, "required", false, false, "medium")
	if req.JSONSchema != nil {
		t.Fatalf("multi-tool request should not use a single argument schema: %#v", req)
	}
}

func TestBuildChatRequestDoesNotConstrainNativeOpenAIToolProtocol(t *testing.T) {
	profile := settings.Profile{
		Name:         "qwen3.6-think",
		ModelID:      "Qwen3.6-27B-MTP-UD-Q4_K_XL.gguf",
		Thinking:     true,
		ThinkGeneral: settings.GenerationParams{MaxTokens: 2048},
	}
	tool := llm.ToolDef{
		Type: "function",
		Function: llm.ToolFunctionDef{
			Name:       "read",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		},
	}

	req := buildChatRequest(profile, nil, []llm.ToolDef{tool}, "required", false, false, "medium")
	if req.JSONSchema != nil {
		t.Fatalf("native OpenAI tool protocol should rely on backend tool_calls, not response_format schema: %#v", req)
	}
}

func TestBuildChatRequestUsesCodingSettingsForNoThinkCodeTasks(t *testing.T) {
	profile := settings.Profile{
		Thinking: false,
		ThinkCoding: settings.GenerationParams{
			Temperature: 0.6,
			MaxTokens:   16384,
			Seed:        22,
		},
		NoThink: settings.GenerationParams{
			Temperature: 0.44,
			MaxTokens:   8192,
			Seed:        88,
		},
	}

	req := buildChatRequest(profile, nil, nil, "", false, true, "medium")

	if req.EnableThinking {
		t.Fatalf("EnableThinking = true, want false")
	}
	if req.MaxTokens != 16384 || req.Temperature != 0.6 || req.Seed != 22 {
		t.Fatalf("coding params not selected for no-thinking code task: %#v", req)
	}
}

func TestBuildChatRequestUsesCodingSettingsForCodeTasks(t *testing.T) {
	profile := settings.Profile{
		Thinking: true,
		ThinkGeneral: settings.GenerationParams{
			Temperature: 1.0,
			MaxTokens:   4096,
			Seed:        11,
		},
		ThinkCoding: settings.GenerationParams{
			Temperature: 0.6,
			MaxTokens:   8192,
			Seed:        22,
		},
	}

	req := buildChatRequest(profile, nil, nil, "", false, shouldUseCodingParams("please write the full PowerShell script", AgentMode{Name: "Manual"}), "medium")

	if req.MaxTokens != 8192 || req.Temperature != 0.6 || req.Seed != 22 {
		t.Fatalf("coding params not selected for script request: %#v", req)
	}
}

func TestEnsureModelLoadedOnlyLoadsOncePerModelKey(t *testing.T) {
	app := &App{}
	client := &countingLoader{}
	profile := settings.Profile{
		Backend:   "lmstudio",
		BaseURL:   "http://localhost:1234/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 1 {
		t.Fatalf("loads = %d, want 1", client.loads)
	}

	profile.CtxTokens = 4096
	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 2 {
		t.Fatalf("loads after context change = %d, want 2", client.loads)
	}
}

func TestEnsureModelLoadedPreventsDoubleLoadUnderConcurrentCalls(t *testing.T) {
	app := &App{}
	client := &countingLoader{}
	profile := settings.Profile{
		Backend:   "lmstudio",
		BaseURL:   "http://localhost:1234/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}

	const goroutines = 10
	errs := make(chan error, goroutines)
	start := make(chan struct{})

	for range goroutines {
		go func() {
			<-start
			errs <- app.ensureModelLoaded(context.Background(), client, profile)
		}()
	}
	close(start)

	for range goroutines {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	if client.loads != 1 {
		t.Fatalf("concurrent calls caused %d loads, want exactly 1", client.loads)
	}
}

func TestEnsureModelLoadedReusesBackendContextAboveProfile(t *testing.T) {
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 153088}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 0 {
		t.Fatalf("loads = %d, want cached model reused when backend context is larger", client.loads)
	}
}

func TestEnsureModelLoadedReusesBackendContextWithoutLocalCache(t *testing.T) {
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 45000,
	}
	app := &App{history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 54016}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 0 {
		t.Fatalf("loads = %d, want already-loaded backend reused without local cache", client.loads)
	}
	if got := app.loadedModelKey; got != modelLoadKey(profile) {
		t.Fatalf("loadedModelKey = %q, want %q", got, modelLoadKey(profile))
	}
}

func TestEnsureModelLoadedReusesSameRuntimeForLowerContextRequest(t *testing.T) {
	loadedProfile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	lowerProfile := loadedProfile
	lowerProfile.CtxTokens = 24576
	app := &App{loadedModelKey: modelLoadKey(loadedProfile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 32768}

	if err := app.ensureModelLoaded(context.Background(), client, lowerProfile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 0 {
		t.Fatalf("loads = %d, want lower context request to reuse larger loaded runtime", client.loads)
	}
}

func TestModelLoadKeySameRuntimeIgnoresOnlyContext(t *testing.T) {
	loadedProfile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1/",
		ModelID:   "qwen",
		CtxTokens: 32768,
		APIKeyEnv: "KEY",
	}
	requestedProfile := loadedProfile
	requestedProfile.BaseURL = "http://127.0.0.1:8802/v1"
	requestedProfile.CtxTokens = 24576

	if !modelLoadKeySameRuntime(requestedProfile, modelLoadKey(loadedProfile)) {
		t.Fatal("same backend/model/api key should match even when requested context is lower")
	}

	requestedProfile.ModelID = "other"
	if modelLoadKeySameRuntime(requestedProfile, modelLoadKey(loadedProfile)) {
		t.Fatal("different model should not match the loaded runtime")
	}
}

func TestEnsureModelLoadedReloadsWhenBackendContextBelowProfile(t *testing.T) {
	profile := settings.Profile{
		Name:      "gemma4-31b",
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "gemma",
		CtxTokens: 120000,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 32768, actualAfterLoad: 120000}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 1 {
		t.Fatalf("loads = %d, want reload when backend context is too small", client.loads)
	}
}

func TestEnsureModelLoadedFailsWhenBackendContextStillBelowProfile(t *testing.T) {
	profile := settings.Profile{
		Name:      "qwen3.6-nothink",
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 54000,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 8192, actualAfterLoad: 8192}

	err := app.ensureModelLoaded(context.Background(), client, profile)
	if err == nil {
		t.Fatal("expected context shortfall error")
	}
	if !strings.Contains(err.Error(), "backend context shortfall") || !strings.Contains(err.Error(), "54000") || !strings.Contains(err.Error(), "8192") {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.loads != 1 {
		t.Fatalf("loads = %d, want one reload attempt before failing", client.loads)
	}
	if app.loadedModelKey != "" {
		t.Fatalf("loadedModelKey = %q, want cleared after context shortfall", app.loadedModelKey)
	}
}

func TestEnsureModelLoadedKeepsCacheWhenBackendContextMatchesProfile(t *testing.T) {
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 32768}

	if err := app.ensureModelLoaded(context.Background(), client, profile); err != nil {
		t.Fatal(err)
	}
	if client.loads != 0 {
		t.Fatalf("loads = %d, want cached model to be reused", client.loads)
	}
}

func TestEnsureModelLoadedRetriesTransientLoadFailure(t *testing.T) {
	withFastModelLoadRetry(t)
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{history: agent.NewHistory(32768)}
	client := &countingLoader{failuresBeforeSuccess: 1, loadErr: errors.New("bridge starting")}
	retries := 0

	if err := app.ensureModelLoaded(context.Background(), client, profile, func(int, error) {
		retries++
	}); err != nil {
		t.Fatal(err)
	}
	if client.loads != 2 {
		t.Fatalf("loads = %d, want retry after transient failure", client.loads)
	}
	if retries != 1 {
		t.Fatalf("retries = %d, want 1", retries)
	}
	if app.loadedModelKey != modelLoadKey(profile) {
		t.Fatalf("loadedModelKey was not cached after successful retry")
	}
}

func TestEnsureModelLoadedReportsRepeatedLoadFailure(t *testing.T) {
	withFastModelLoadRetry(t)
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{loadedModelKey: modelLoadKey(profile), history: agent.NewHistory(32768)}
	client := &countingLoader{actualContext: 24576, failuresBeforeSuccess: 99, loadErr: errors.New("health timeout")}
	retries := 0

	err := app.ensureModelLoaded(context.Background(), client, profile, func(int, error) {
		retries++
	})
	if err == nil {
		t.Fatal("expected repeated load failure")
	}
	if !strings.Contains(err.Error(), "model load failed after 3 attempts") {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.loads != 3 {
		t.Fatalf("loads = %d, want 3 attempts", client.loads)
	}
	if retries != 2 {
		t.Fatalf("retries = %d, want callbacks before attempts 2 and 3", retries)
	}
	if app.loadedModelKey != "" {
		t.Fatalf("loadedModelKey = %q, want cleared after failed reload", app.loadedModelKey)
	}
}

func TestRecordBackendRuntimeMismatchAddsWarningForSmallerBackend(t *testing.T) {
	run := startTaskRun("prompt", "Auto", "profile", "qwen")
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{}
	client := &countingLoader{actualContext: 24576}

	app.recordBackendRuntimeMismatch(context.Background(), client, profile, &run)

	if len(run.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(run.Events))
	}
	if run.Events[0].Kind != "backend_runtime_changed" || !strings.Contains(run.Events[0].Detail, "expected_ctx=32768 actual_ctx=24576") {
		t.Fatalf("unexpected event: %#v", run.Events[0])
	}
}

func TestRecordBackendRuntimeMismatchAddsFailForSevereShortfall(t *testing.T) {
	run := startTaskRun("prompt", "Auto", "profile", "qwen")
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 40000,
	}
	app := &App{}
	client := &countingLoader{actualContext: 8192}

	app.recordBackendRuntimeMismatch(context.Background(), client, profile, &run)

	if len(run.Events) != 1 || !strings.Contains(run.Events[0].Detail, "severity=fail") {
		t.Fatalf("expected fail severity for severe context shortfall, got %#v", run.Events)
	}
}

func TestRecordBackendRuntimeMismatchSkipsMatchingBackend(t *testing.T) {
	run := startTaskRun("prompt", "Auto", "profile", "qwen")
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{}
	client := &countingLoader{actualContext: 32768}

	app.recordBackendRuntimeMismatch(context.Background(), client, profile, &run)

	if len(run.Events) != 0 {
		t.Fatalf("events = %#v, want none for matching backend context", run.Events)
	}
}

func TestRecordBackendRuntimeMismatchAddsInfoForLargerBackend(t *testing.T) {
	run := startTaskRun("prompt", "Auto", "profile", "qwen")
	profile := settings.Profile{
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "qwen",
		CtxTokens: 32768,
	}
	app := &App{}
	client := &countingLoader{actualContext: 65536}

	app.recordBackendRuntimeMismatch(context.Background(), client, profile, &run)

	if len(run.Events) != 1 || !strings.Contains(run.Events[0].Detail, "severity=info") {
		t.Fatalf("expected info event for larger backend context, got %#v", run.Events)
	}

	app.recordBackendRuntimeMismatch(context.Background(), client, profile, &run)
	if len(run.Events) != 1 {
		t.Fatalf("larger backend context info should only be recorded once, got %#v", run.Events)
	}
}

func TestShouldConfirmToolSeparatesShellExecFromWrites(t *testing.T) {
	cfg := &settings.Settings{}
	cfg.Tools.ConfirmExec = true
	cfg.Tools.ConfirmWrites = false

	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(`{"command":"go test ./..."}`)}}
	if !shouldConfirmTool(&namedDestructiveTool{name: "shell"}, cfg, tc) {
		t.Fatalf("shell should respect confirm_exec")
	}
	tc.Function.Name = "write"
	if shouldConfirmTool(&namedDestructiveTool{name: "write"}, cfg, tc) {
		t.Fatalf("write should not confirm when confirm_writes is false")
	}
}

func TestShouldConfirmToolSkipsSafeListedExactInput(t *testing.T) {
	input := `{"command":"go test ./..."}`
	cfg := &settings.Settings{}
	cfg.Tools.ConfirmExec = true
	cfg.Tools.SafeRules = []settings.ToolSafeRule{{
		Tool:      "shell",
		InputHash: safeToolInputHash(input),
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(input)}}

	if shouldConfirmTool(&namedDestructiveTool{name: "shell"}, cfg, tc) {
		t.Fatalf("safe-listed exact tool input should not prompt")
	}

	tc.Function.Arguments = json.RawMessage(`{"command":"go vet ./..."}`)
	if !shouldConfirmTool(&namedDestructiveTool{name: "shell"}, cfg, tc) {
		t.Fatalf("different tool input should still prompt")
	}
}

func TestComposeUserTextWithAttachments(t *testing.T) {
	got := composeUserTextWithAttachments("summarise this", []ChatAttachment{{
		Name:      "Pasted text.txt",
		Kind:      "document",
		MIME:      "text/plain",
		Content:   "b1 - response - 2",
		Truncated: true,
	}})

	for _, want := range []string{"summarise this", "Attached context from the user", "Pasted text.txt", "inline chat attachment", "do not call read", "b1 - response - 2", "attachment truncated"} {
		if !strings.Contains(got, want) {
			t.Fatalf("composed attachment text missing %q:\n%s", want, got)
		}
	}
}

func TestSetWorkingDirAppliesWorkspaceAndClearsRunContext(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "idea.md"), []byte("image scrubber"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "old.txt")
	if err := os.WriteFile(snapshot, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	rb := &agent.Rollback{}
	if err := rb.Push(agent.OpWrite, snapshot); err != nil {
		t.Fatal(err)
	}
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: rb,
		registry: tools.New(),
	}
	app.history.Append(llm.NewTextMessage(llm.RoleSystem, "old workspace"))
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "what is this repo?"))

	if err := app.SetWorkingDir(project); err != nil {
		t.Fatal(err)
	}

	if got, want := app.GetWorkingDir(), filepath.ToSlash(project); got != want {
		t.Fatalf("working dir = %q, want %q", got, want)
	}
	if app.history.TokenCount() != 0 {
		t.Fatalf("history token count = %d, want cleared", app.history.TokenCount())
	}
	if app.rollback.Len() != 0 {
		t.Fatalf("rollback len = %d, want cleared", app.rollback.Len())
	}
	saved, err := settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Context.WorkspaceDir != filepath.ToSlash(project) {
		t.Fatalf("saved workspace = %q, want %q", saved.Context.WorkspaceDir, filepath.ToSlash(project))
	}
}

func TestOpenWorkspaceFolderDoesNotChangeAgentRootOrClearContext(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	agentRoot := t.TempDir()
	browseOnly := t.TempDir()
	restoreWorkingDir(t)
	if err := os.Chdir(agentRoot); err != nil {
		t.Fatal(err)
	}

	cfg := settings.DefaultSettings()
	cfg.Context.WorkspaceDir = filepath.ToSlash(agentRoot)
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: &agent.Rollback{},
		registry: tools.New(),
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "keep this context"))

	folders, err := app.AddWorkspaceFolder(browseOnly, "reference")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := app.GetWorkingDir(), filepath.ToSlash(agentRoot); got != want {
		t.Fatalf("agent root changed to %q, want %q", got, want)
	}
	if app.history.TokenCount() == 0 {
		t.Fatal("adding a browse-only folder should not clear chat context")
	}
	if len(folders) != 2 {
		t.Fatalf("folders = %#v, want agent root plus browse-only folder", folders)
	}
	if folders[1].Path != filepath.ToSlash(browseOnly) || folders[1].Role != "reference" {
		t.Fatalf("browse folder not saved correctly: %#v", folders)
	}
}

func TestNormaliseWorkspaceFoldersPrunesMovedFolders(t *testing.T) {
	agentRoot := t.TempDir()
	browseOnly := t.TempDir()
	missing := filepath.Join(t.TempDir(), "moved-away")

	folders := normaliseAppWorkspaceFolders([]settings.WorkspaceFolder{
		{Path: missing, Name: "old", Role: "folder"},
		{Path: browseOnly, Name: "browse", Role: "folder"},
	}, agentRoot)

	if len(folders) != 2 {
		t.Fatalf("folders = %#v, want agent root plus existing browse folder", folders)
	}
	for _, folder := range folders {
		if sameFilesystemPath(folder.Path, missing) {
			t.Fatalf("missing moved folder was not pruned: %#v", folders)
		}
	}
}

func TestUpdateSettingsAppliesWorkspaceDir(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "app.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)

	cfg := settings.DefaultSettings()
	profiles := settings.DefaultProfiles()
	app := &App{
		cfg:      &cfg,
		profiles: &profiles,
		history:  agent.NewHistory(4096),
		rollback: &agent.Rollback{},
		registry: tools.New(),
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "stale chat"))

	next := cfg
	next.Context.WorkspaceDir = project
	if err := app.UpdateSettings(next); err != nil {
		t.Fatal(err)
	}

	if got, want := app.GetWorkingDir(), filepath.ToSlash(project); got != want {
		t.Fatalf("working dir = %q, want %q", got, want)
	}
	if app.cfg.Context.WorkspaceDir != filepath.ToSlash(project) {
		t.Fatalf("app cfg workspace = %q, want %q", app.cfg.Context.WorkspaceDir, filepath.ToSlash(project))
	}
	if app.history.TokenCount() != 0 {
		t.Fatalf("history token count = %d, want cleared", app.history.TokenCount())
	}
}

func TestBuildSystemPromptIncludesAuthoritativeWorkspace(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "idea.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "app.py"), []byte("print('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreWorkingDir(t)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	prompt := buildSystemPrompt(settings.DefaultSettings(), AgentMode{}, nil, nil)
	for _, want := range []string{
		"Current workspace context (authoritative for this run)",
		filepath.ToSlash(project),
		"idea.md",
		"app.py",
		"ignore stale project names",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestStopReasonForBudgetBlock(t *testing.T) {
	tests := map[string]string{
		"web research stopped after 2 failed/no-result web attempts": "web_research_failed",
		"web_search budget exhausted (4 searches)":                   "search_budget_exhausted",
		"fetch_url budget exhausted (6 fetches)":                     "fetch_budget_exhausted",
		"browser automation budget exhausted (20 actions)":           "browser_budget_exhausted",
	}
	for msg, want := range tests {
		if got := stopReasonForBudgetBlock("web_search", msg); got != want {
			t.Fatalf("stopReasonForBudgetBlock(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestBlockingStopReasonKeepsBudgetRunFromCleanDone(t *testing.T) {
	if !isBlockingStopReason("search_budget_exhausted") {
		t.Fatal("search budget exhaustion should be a blocking stop reason")
	}
	if isBlockingStopReason("tool_error") {
		t.Fatal("recoverable tool errors should not force blocked final status")
	}
}

func TestRequiresLivingDocUpdateAndMutationDetection(t *testing.T) {
	if !requiresLivingDocUpdate("complete the writeup as you go in Connected.md") {
		t.Fatal("expected writeup prompt to require doc mutation")
	}
	run := startTaskRun("complete writeup", "Builder", "profile", "model")
	if runHasFileMutation(run) {
		t.Fatal("empty run should not have file mutation")
	}
	run.addTool("edit", `{"path":"Connected.md"}`, "ok", "done", 1)
	if !runHasFileMutation(run) {
		t.Fatal("edit success should count as a file mutation")
	}
}

func TestDocumentationRecoveryFiltersToFileTools(t *testing.T) {
	defs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "shell"}},
		{Function: llm.ToolFunctionDef{Name: "web_search"}},
		{Function: llm.ToolFunctionDef{Name: "read"}},
		{Function: llm.ToolFunctionDef{Name: "edit"}},
	}
	got := filterToolDefsByName(defs, "read", "write", "edit", "glob", "grep")
	names := make([]string, 0, len(got))
	for _, def := range got {
		names = append(names, def.Function.Name)
	}
	if strings.Join(names, ",") != "read,edit" {
		t.Fatalf("filtered tools = %#v", names)
	}
	prompt := documentationRecoveryPrompt("update the writeup", "search_budget_exhausted", "web_search budget exhausted")
	if !strings.Contains(prompt, "write or edit") || strings.Contains(prompt, "perform more web") && !strings.Contains(prompt, "Do not perform more web") {
		t.Fatalf("unexpected recovery prompt: %s", prompt)
	}
}

func TestTaskRunKeepsFirstStopReason(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.stop("tool_denied", "denied")
	run.stop("tool_error", "later")
	run.finish("done", "summary")

	if run.StopReason != "tool_denied" || run.StopDetail != "denied" {
		t.Fatalf("unexpected stop fields: %#v", run)
	}
}

func TestTaskRunTerminalStopOverridesRecoverableStopReason(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.stop("tool_disabled", "tool \"web_search\" is disabled in settings")
	run.stopTerminal("chat_error", "HTTP 500: backend completion failed")
	run.finish("error", "HTTP 500: backend completion failed")

	if run.StopReason != "chat_error" || run.StopDetail != "HTTP 500: backend completion failed" {
		t.Fatalf("terminal stop did not override stale recoverable stop: %#v", run)
	}
}

func TestFinalStoppedRunStateDistinguishesUserStopFromBlocked(t *testing.T) {
	if got := finalStoppedRunState("user_stopped"); got != "stopped" {
		t.Fatalf("user stop state = %q, want stopped", got)
	}
	if got := finalStoppedRunState("web_research_failed"); got != "blocked" {
		t.Fatalf("budget block state = %q, want blocked", got)
	}
}

func TestRecoverableInferenceFailureClassifier(t *testing.T) {
	if !isRecoverableInferenceFailure(`HTTP 500: {"error":{"message":"Inference failed: error sending request for url (http://127.0.0.1:20688/completion)"}}`) {
		t.Fatal("expected backend HTTP 500 request failure to be recoverable")
	}
	if !isRecoverableInferenceFailure(`Post "http://127.0.0.1:8802/v1/chat/completions": dial tcp 127.0.0.1:8802: connectex: A connection attempt failed because the connected party did not properly respond after a period of time, or established connection failed because connected host has failed to respond.`) {
		t.Fatal("expected Windows connectex bridge failure to be recoverable")
	}
	if !isRecoverableInferenceFailure(`Post "http://127.0.0.1:8802/v1/chat/completions": readfrom tcp 127.0.0.1:1366->127.0.0.1:8802: write tcp 127.0.0.1:1366->127.0.0.1:8802: wsasend: An existing connection was forcibly closed by the remote host.`) {
		t.Fatal("expected Windows wsasend bridge failure to be recoverable")
	}
	if isRecoverableInferenceFailure(`tool "web_search" is disabled in settings`) {
		t.Fatal("disabled tools should not be treated as recoverable inference failures")
	}
}

func TestTaskRunEventsCaptureTimeline(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.addEvent("continue", "Auto-continue 1/4", strings.Repeat("x", 2500))

	if len(run.Events) != 1 {
		t.Fatalf("expected one event, got %d", len(run.Events))
	}
	if run.Events[0].Kind != "continue" || run.Events[0].Message != "Auto-continue 1/4" {
		t.Fatalf("unexpected event: %#v", run.Events[0])
	}
	if len(run.Events[0].Detail) > 2005 || !strings.HasSuffix(run.Events[0].Detail, "\n...") {
		t.Fatalf("event detail was not trimmed: len=%d suffix=%q", len(run.Events[0].Detail), run.Events[0].Detail[len(run.Events[0].Detail)-4:])
	}
}

func TestTaskRunStateTransitionsAreLogged(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	run.setState("reading", "read")
	run.setState("reading", "read again")
	run.setState("editing", "edit")

	if run.State != "editing" {
		t.Fatalf("state = %q, want editing", run.State)
	}
	stateEvents := 0
	for _, event := range run.Events {
		if event.Kind == "state" {
			stateEvents++
		}
	}
	if stateEvents != 2 {
		t.Fatalf("duplicate state should not log extra events, got %d state events: %#v", stateEvents, run.Events)
	}
}

func TestToolChoiceDisablesToolsForSmallTalkButNotShortTasks(t *testing.T) {
	for _, text := range []string{"hello", "hi there", "thanks", "good morning"} {
		if !looksConversational(text) {
			t.Fatalf("%q should be conversational", text)
		}
		if got := toolChoiceFor(text, 0, 0); got != "none" {
			t.Fatalf("toolChoiceFor(%q) = %q, want none", text, got)
		}
	}

	// Action tasks with no explicit inspection/operational intent leave the choice
	// to the model (auto) on the first turn.
	for _, text := range []string{"fix bug", "run tests", "search for config"} {
		if looksConversational(text) {
			t.Fatalf("%q should be treated as a tool-capable task", text)
		}
		if got := toolChoiceFor(text, 0, 0); got != "auto" {
			t.Fatalf("toolChoiceFor(%q) = %q, want auto", text, got)
		}
	}

	// Explicit repository-inspection intent forces a tool call on the opening turn
	// so the model cannot narrate instead of acting.
	for _, text := range []string{"read README.md", "list files"} {
		if looksConversational(text) {
			t.Fatalf("%q should be treated as a tool-capable task", text)
		}
		if got := toolChoiceFor(text, 0, 0); got != "required" {
			t.Fatalf("toolChoiceFor(%q) = %q, want required", text, got)
		}
	}
}

func TestToolDefsOmittedForConversationalTurn(t *testing.T) {
	registry := tools.New()
	defs, choice := toolDefsAndChoiceForTurn(registry, settings.DefaultSettings().Tools, "hello", 0, 0)
	if choice != "none" {
		t.Fatalf("choice = %q, want none", choice)
	}
	if len(defs) != 0 {
		t.Fatalf("conversational turn should not expose tool schemas, got %d", len(defs))
	}

	defs, choice = toolDefsAndChoiceForTurn(registry, settings.DefaultSettings().Tools, "fix bug", 0, 0)
	if choice != "auto" || len(defs) == 0 {
		t.Fatalf("task turn should expose tools, choice=%q defs=%d", choice, len(defs))
	}
}

func TestToolDefsKeepStrictShellForHTBWSLTasksInBalancedMode(t *testing.T) {
	registry := tools.New()
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "balanced"
	prompt := "Resume HTB Connected against target IP 10.129.12.172. Use WSL sudo and run nmap before exploitation."

	defs, choice := toolDefsAndChoiceForTurn(registry, cfg, prompt, 0, 0)
	if choice != "required" {
		t.Fatalf("choice = %q, want required", choice)
	}
	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	if !seen["shell"] {
		t.Fatalf("HTB/WSL task should expose direct shell tool, got %#v", seen)
	}
	if seen["bash"] || seen["run_script"] {
		t.Fatalf("HTB/WSL first turn should not expose shell aliases/specialists, got %#v", seen)
	}
	for _, blocked := range []string{"web_search", "fetch_url", "browser", "task"} {
		if seen[blocked] {
			t.Fatalf("HTB/WSL task should not expose host-side research tool %s for shell work", blocked)
		}
	}
}

func TestToolDefsKeepOpsToolsSlimForHTBWSLTasksInUnrestrictedMode(t *testing.T) {
	app := &App{registry: tools.New()}
	app.registerAppTools()
	registry := app.registry
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"
	prompt := "Resume HTB Connected against target IP 10.129.12.172. Use WSL sudo and run nmap before exploitation."

	defs, choice := toolDefsAndChoiceForTurn(registry, cfg, prompt, 0, 0)
	if choice != "required" {
		t.Fatalf("choice = %q, want required", choice)
	}
	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	for _, want := range []string{"shell", "terminal_send", "terminal_read", "start_listener", "http_probe"} {
		if !seen[want] {
			t.Fatalf("unrestricted HTB/WSL task should keep %s available, got %#v", want, seen)
		}
	}
	for _, notWant := range []string{"bash", "run_script"} {
		if seen[notWant] {
			t.Fatalf("unrestricted HTB/WSL first turn should not include shell specialist %s, got %#v", notWant, seen)
		}
	}
	for _, notWant := range []string{"web_search", "fetch_url", "browser"} {
		if seen[notWant] {
			t.Fatalf("unrestricted HTB/WSL task should not inject %s unless requested, got %#v", notWant, seen)
		}
	}
	if len(defs) > 18 {
		t.Fatalf("ops tool routing should stay compact, got %d tools: %#v", len(defs), seen)
	}
}

func TestToolRouterNarrowsCodingTaskTools(t *testing.T) {
	registry := tools.New()
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	defs, choice := toolDefsAndChoiceForTurn(registry, cfg, "fix the frontend build error and run tests", 0, 0)
	if choice != "auto" {
		t.Fatalf("choice = %q, want auto", choice)
	}
	seen := toolDefNameSet(defs)
	for _, want := range []string{"read", "grep", "write", "edit", "shell"} {
		if !seen[want] {
			t.Fatalf("coding task should expose %s, got %#v", want, seen)
		}
	}
	for _, notWant := range []string{"browser"} {
		if seen[notWant] {
			t.Fatalf("coding task should not expose browser interaction tool %s by default, got %#v", notWant, seen)
		}
	}
}

func TestToolRouterKeepsReadOnlyInspectionToolUsingButSlim(t *testing.T) {
	registry := tools.New()
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	defs, choice := toolDefsAndChoiceForTurn(registry, cfg, "Inspect this repo, find where model-call telemetry is recorded, then summarize the files involved. Do not edit anything.", 0, 0)
	if choice != "required" {
		t.Fatalf("choice = %q, want required", choice)
	}
	seen := toolDefNameSet(defs)
	for _, want := range []string{"glob", "grep", "read"} {
		if !seen[want] {
			t.Fatalf("inspection task should expose %s, got %#v", want, seen)
		}
	}
	for _, notWant := range []string{"write", "edit", "shell", "bash", "terminal_send", "browser", "web_search"} {
		if seen[notWant] {
			t.Fatalf("read-only inspection task should not expose %s by default, got %#v", notWant, seen)
		}
	}
}

func TestToolRouterNarrowsResearchTaskTools(t *testing.T) {
	registry := tools.New()
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "unrestricted"

	defs, choice := toolDefsAndChoiceForTurn(registry, cfg, "research current Qwen structured output docs online", 0, 0)
	if choice != "auto" {
		t.Fatalf("choice = %q, want auto", choice)
	}
	seen := toolDefNameSet(defs)
	for _, want := range []string{"web_search", "fetch_url"} {
		if !seen[want] {
			t.Fatalf("research task should expose %s, got %#v", want, seen)
		}
	}
	for _, notWant := range []string{"write", "edit", "terminal_send", "start_listener"} {
		if seen[notWant] {
			t.Fatalf("research task should not expose %s by default, got %#v", notWant, seen)
		}
	}
}

func toolDefNameSet(defs []llm.ToolDef) map[string]bool {
	seen := map[string]bool{}
	for _, def := range defs {
		seen[def.Function.Name] = true
	}
	return seen
}

func TestToolErrorResultPreservesCapturedOutput(t *testing.T) {
	got := toolErrorResult("[stderr]\nInvoke-WebRequest failed\n[powershell exit 1, 10ms]", errors.New("exit code 1"))
	if !strings.Contains(got, "Invoke-WebRequest failed") || !strings.Contains(got, "error: exit code 1") {
		t.Fatalf("tool error result lost captured output: %q", got)
	}
	if got := toolErrorResult("", errors.New("exit code 1")); got != "error: exit code 1" {
		t.Fatalf("empty output fallback = %q", got)
	}
}

func TestNeedsInspectionToolSkipsOpsTargetPrompts(t *testing.T) {
	prompt := "HTB Red Team Operator / Navigator Framework against target IP 10.129.245.100 with WSL/Kali"
	if needsInspectionTool(prompt) {
		t.Fatalf("ops target prompt should not force repository inspection")
	}
	if !needsOperationalTool(prompt) {
		t.Fatalf("ops target prompt should use operational recovery")
	}
}

func TestStateForTool(t *testing.T) {
	tests := map[string]string{
		"web_search":     "researching",
		"browser":        "researching",
		"read":           "reading",
		"session_search": "reading",
		"edit":           "editing",
		"shell":          "testing",
		"unknown_tool":   "using_tools",
	}
	for tool, want := range tests {
		if got := stateForTool(tool); got != want {
			t.Fatalf("stateForTool(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestMalformedToolArgsErrorDetection(t *testing.T) {
	err := errors.New("write: bad params: unexpected end of JSON input")
	if !isMalformedToolArgsError(err) {
		t.Fatalf("expected malformed JSON tool error to be detected")
	}
	if isMalformedToolArgsError(errors.New("write: permission denied")) {
		t.Fatalf("permission denied should not be classified as malformed JSON")
	}
}

func TestSaveTaskRunHonoursDisabledLogging(t *testing.T) {
	run := startTaskRun("prompt", "Builder", "profile", "model")
	if err := saveTaskRun(run, &settings.LoggingConfig{Enabled: false}); err != nil {
		t.Fatalf("disabled logging should be a no-op: %v", err)
	}
}

func TestAutoContinueDelay(t *testing.T) {
	if autoContinueDelay(0) != 0 {
		t.Fatalf("attempt 0 should not delay")
	}
	if autoContinueDelay(1) != 500*time.Millisecond {
		t.Fatalf("attempt 1 delay = %s, want 500ms", autoContinueDelay(1))
	}
}

func TestLooksAboutToActCatchesQwenTruncationIntent(t *testing.T) {
	text := "The updated structure is clear. Right - let me write the updated document now."
	if !looksAboutToAct(text) {
		t.Fatalf("expected Qwen-style action intent to be detected")
	}
}

func TestLooksAboutToActCatchesRepairIntent(t *testing.T) {
	text := "I need to fix the app.py - it got corrupted. Let me rewrite it cleanly, then build out the frontend files in sequence."
	if !looksAboutToAct(text) {
		t.Fatalf("expected repair/rewrite intent to be detected")
	}
}

func TestLooksAboutToActCatchesFollowAndEnumerateIntent(t *testing.T) {
	text := "Good -- HTTP port 80 redirects to `/admin`. Let me follow the redirect and enumerate the FreePBX admin panel."
	if !looksAboutToAct(text) {
		t.Fatalf("expected follow/enumerate intent to be detected")
	}
	if !looksIncomplete(text) {
		t.Fatalf("follow/enumerate intent should auto-continue instead of finishing")
	}
}

func TestLooksAboutToActCatchesTerminalEnterIntent(t *testing.T) {
	text := "The shared terminal is in a busy state. I'll send an Enter to clear it and then run the hosts update and connectivity check."
	if !looksAboutToAct(text) {
		t.Fatalf("terminal Enter intent should auto-continue instead of finishing")
	}
	prompt := buildDirectivePrompt(text)
	if !strings.Contains(prompt, "terminal_send") || !strings.Contains(prompt, `"key":"enter"`) {
		t.Fatalf("terminal directive should force terminal_send Enter: %s", prompt)
	}
}

func TestLooksAboutToActIgnoresOrdinaryFinishedText(t *testing.T) {
	text := "The build passed and the document was updated."
	if looksAboutToAct(text) {
		t.Fatalf("ordinary finished text should not look about to act")
	}
}

func TestBuildDirectivePromptRequiresToolCall(t *testing.T) {
	prompt := buildDirectivePrompt("Right - let me write the updated document now.")
	if !strings.Contains(prompt, "Call write or edit RIGHT NOW") || !strings.Contains(prompt, "tool call") {
		t.Fatalf("directive prompt is not forceful enough: %s", prompt)
	}
}

func TestAgentToolBudgetSummaryPromptForbidsToolMarkup(t *testing.T) {
	if !agentToolBudgetExhausted(settings.AgentsConfig{MaxToolCalls: 40}, 40) {
		t.Fatalf("expected budget to be exhausted at the configured cap")
	}
	if agentToolBudgetExhausted(settings.AgentsConfig{MaxToolCalls: 40}, 39) {
		t.Fatalf("budget should not be exhausted before the configured cap")
	}
	prompt := agentToolBudgetSummaryPrompt(40)
	for _, want := range []string{
		"Agent tool-call budget is exhausted (40 calls)",
		"Do not emit tool calls or tool markup",
		"final progress summary",
		"ask the user before continuing",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("budget summary prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestAgentTimeBudgetSummaryPromptForbidsToolMarkup(t *testing.T) {
	started := time.Unix(100, 0)
	if !agentTimeBudgetExhausted(settings.AgentsConfig{MaxRunSeconds: 30}, started, started.Add(31*time.Second)) {
		t.Fatalf("expected time budget to be exhausted after configured seconds")
	}
	if agentTimeBudgetExhausted(settings.AgentsConfig{MaxRunSeconds: 30}, started, started.Add(30*time.Second)) {
		t.Fatalf("time budget should not be exhausted at the exact configured second")
	}
	if agentTimeBudgetExhausted(settings.AgentsConfig{MaxRunSeconds: 0}, started, started.Add(24*time.Hour)) {
		t.Fatalf("zero max_run_seconds should be unlimited")
	}
	prompt := agentTimeBudgetSummaryPrompt(30)
	for _, want := range []string{
		"Agent wall-clock time budget is exhausted (30 seconds)",
		"Do not emit tool calls or tool markup",
		"final progress summary",
		"ask the user before continuing",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("time budget summary prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSharedTerminalWrapperUsesMarkers(t *testing.T) {
	start, donePrefix, wrapped := sharedTerminalWrapper(`printf 'ok\n'`, "abc123")
	if start != "__MAULER_START_abc123__" {
		t.Fatalf("start marker = %q", start)
	}
	if donePrefix != "__MAULER_DONE_abc123:" {
		t.Fatalf("done prefix = %q", donePrefix)
	}
	// The wrapped command must build markers from a variable so the echoed
	// command never carries a literal marker — that's what stops wrapped echo
	// fragments from being misparsed as a real START/DONE/CWD line.
	for _, banned := range []string{"__MAULER_START_", "__MAULER_DONE_", "__MAULER_CWD_"} {
		if strings.Contains(wrapped, banned) {
			t.Fatalf("wrapped command leaks literal marker %q (echo could be misparsed): %s", banned, wrapped)
		}
	}
	for _, want := range []string{"M=__MA''ULER_", "${M}START_abc123__", "${M}DONE_abc123:", "${M}CWD_abc123__", "printf 'ok\\n'", "status=$?"} {
		if !strings.Contains(wrapped, want) {
			t.Fatalf("wrapped command missing %q: %s", want, wrapped)
		}
	}
}

func TestContainsShellHeredoc(t *testing.T) {
	cases := []string{
		"cat <<EOF\nhello\nEOF",
		"sudo tee -a /etc/hosts <<EOF\n10.1.1.1 test\nEOF",
		"printf x | tee /tmp/x; cat <<-'DONE'\nhello\nDONE",
	}
	for _, command := range cases {
		if !containsShellHeredoc(command) {
			t.Fatalf("expected heredoc detection for %q", command)
		}
	}
	for _, command := range []string{
		"printf '%s\\n' '10.1.1.1 test' | sudo -n tee -a /etc/hosts",
		"grep '<<EOF' notes.txt",
	} {
		if containsShellHeredoc(command) {
			t.Fatalf("unexpected heredoc detection for %q", command)
		}
	}
}

func TestDecorateShellStartErrorExplainsWSLUnexpectedFailure(t *testing.T) {
	err := decorateShellStartError("wsl", "kali-linux", "root", errors.New("Catastrophic failure\nError code: Wsl/Service/E_UNEXPECTED"))
	if err == nil {
		t.Fatal("expected decorated error")
	}
	text := err.Error()
	for _, want := range []string{"WSL failed to start", "kali-linux", "root", "Wsl/Service/E_UNEXPECTED", "wsl --shutdown", "Restart WSL"} {
		if !strings.Contains(text, want) {
			t.Fatalf("decorated error missing %q:\n%s", want, text)
		}
	}
}

func TestStripTrailingBackgroundOperator(t *testing.T) {
	got, ok := stripTrailingBackgroundOperator(`sudo nmap -sV -p- 10.0.0.1 &`)
	if !ok || got != `sudo nmap -sV -p- 10.0.0.1` {
		t.Fatalf("strip background = %q, %v", got, ok)
	}
	if got, ok := stripTrailingBackgroundOperator(`printf '%s\n' "a & b"`); ok || got == "" {
		t.Fatalf("quoted ampersand should not be stripped: %q, %v", got, ok)
	}
	if _, ok := stripTrailingBackgroundOperator(`echo a &&`); ok {
		t.Fatal("logical && should not be treated as a background operator")
	}
}

func TestShellAssignmentJobID(t *testing.T) {
	if got := shellAssignmentJobID(`job="j12"`); got != "j12" {
		t.Fatalf("job assignment id = %q, want j12", got)
	}
	if got := shellAssignmentJobID(`echo job="j12"`); got != "" {
		t.Fatalf("non-assignment should not poll job: %q", got)
	}
}

func TestBackgroundJobPollBackoff(t *testing.T) {
	if got := backgroundJobPollInterval(0); got != time.Second {
		t.Fatalf("first interval = %s, want 1s", got)
	}
	if got := backgroundJobPollInterval(1); got != 2*time.Second {
		t.Fatalf("second interval = %s, want 2s", got)
	}
	if got := backgroundJobPollInterval(8); got != 30*time.Second {
		t.Fatalf("later interval = %s, want 30s", got)
	}
	now := time.Now()
	job := &bgJob{id: "j1", started: now.Add(-10 * time.Second), lastPoll: now.Add(-500 * time.Millisecond), pollCount: 0, lastState: "running"}
	wait, tooEarly := backgroundJobPollWait(job, now)
	if !tooEarly || wait <= 0 {
		t.Fatalf("expected early poll wait, got wait=%s tooEarly=%v", wait, tooEarly)
	}
	msg := formatBackgroundJobTooEarly(job, wait)
	if !strings.Contains(msg, "poll skipped: too early") || !strings.Contains(msg, `"job":"j1"`) {
		t.Fatalf("unexpected early poll message: %q", msg)
	}
}

func TestRepeatedShellFailureBlock(t *testing.T) {
	tc := llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "shell",
			Arguments: json.RawMessage(`{"command":"sudo nmap -sV -sC -p- --min-rate 1000 -oN /tmp/connected_nmap.txt 10.129.245.100 &"}`),
		},
	}
	run := TaskRun{
		Tools: []TaskToolEvent{
			{Name: "shell", Status: "error", Input: `{"command":"sudo nmap -sV -sC -p- --min-rate 1000 -oN /tmp/connected_nmap.txt 10.129.245.100"}`},
			{Name: "shell", Status: "error", Input: `{"command":"sudo nmap -sV -sC -p- --min-rate 1000 -oN /tmp/connected_nmap.txt 10.129.245.100 &"}`},
		},
	}
	if got := repeatedShellFailureBlock(run, tc); !strings.Contains(got, "Repeated shell command blocked") {
		t.Fatalf("expected repeat block, got %q", got)
	}
}

func TestRepeatedShellFailureBlockMentionsUsefulPriorEvidence(t *testing.T) {
	tc := llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "shell",
			Arguments: json.RawMessage(`{"command":"jq -r '.results[] | select(.status >= 200 and .status < 400) | \"\\(.status) \\(.input.FUZZ) \\(.url)\"' web_fuzz_results.json"}`),
		},
	}
	run := TaskRun{
		Tools: []TaskToolEvent{
			{Name: "shell", Status: "done", Input: `{"command":"cat fuzz_results.txt | jq -r '.results[] | select(.status >= 200 and .status < 400) | \"\\(.status) \\(.input.FUZZ) \\(.url)\"'"}`, Result: "302  http://connected.htb/\n301 admin http://connected.htb/admin\n200 robots.txt http://connected.htb/robots.txt\n[shared_terminal/wsl exit 0, 14ms]"},
			{Name: "shell", Status: "error", Input: `{"command":"jq -r '.results[] | select(.status >= 200 and .status < 400) | \"\\(.status) \\(.input.FUZZ) \\(.url)\"' web_fuzz_results.json"}`},
			{Name: "shell", Status: "error", Input: `{"command":"jq -r '.results[] | select(.status >= 200 and .status < 400) | \"\\(.status) \\(.input.FUZZ) \\(.url)\"' web_fuzz_results.json"}`},
		},
	}
	got := repeatedShellFailureBlock(run, tc)
	for _, want := range []string{"previous successful evidence", "robots.txt", "ffuf filename hint"} {
		if !strings.Contains(got, want) {
			t.Fatalf("repeat block missing %q: %s", want, got)
		}
	}
}

func TestRepeatedShellEmptyOutputBlock(t *testing.T) {
	command := `curl -s http://connected.htb/admin/ | head -n 100`
	tc := llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "shell",
			Arguments: json.RawMessage(`{"command":"curl -s http://connected.htb/admin/ | head -n 100"}`),
		},
	}
	run := TaskRun{
		Tools: []TaskToolEvent{
			{Name: "shell", Status: "done", Input: `{"command":"` + command + `"}`, Result: "[shared_terminal/wsl exit 0, 62ms]\ncwd: /mnt/c/Users/richa/Documents/HTB_writeups"},
			{Name: "shell", Status: "done", Input: `{"command":"` + command + `"}`, Result: "[shared_terminal/wsl exit 0, 64ms]\ncwd: /mnt/c/Users/richa/Documents/HTB_writeups\n[empty shell output: command exited successfully but produced no stdout/stderr.]"},
		},
	}
	got := repeatedShellEmptyOutputBlock(run, tc)
	if !strings.Contains(got, "empty successful results") {
		t.Fatalf("expected empty-output repeat block, got %q", got)
	}
}

func TestRepeatedShellSameResultBlock(t *testing.T) {
	command := `curl -s --max-time 10 "http://connected.htb/admin/ajax.php?x=sqli" 2>&1 | grep -o "syntax error: '[^']*'"`
	inputBytes, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	tc := llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "shell",
			Arguments: json.RawMessage(inputBytes),
		},
	}
	run := TaskRun{
		Tools: []TaskToolEvent{
			{Name: "shell", Status: "done", Input: string(inputBytes), Result: "syntax error: '~da43a~'\n\n[shared_terminal/wsl exit 0, 211ms]\ncwd: /mnt/c/Users/richa/Documents/HTB_writeups"},
			{Name: "shell", Status: "done", Input: string(inputBytes), Result: "syntax error: '~da43a~'\n\n[shared_terminal/wsl exit 0, 201ms]\ncwd: /mnt/c/Users/richa/Documents/HTB_writeups"},
		},
	}
	got := repeatedShellSameResultBlock(run, tc)
	if !strings.Contains(got, "identical successful results") || !strings.Contains(got, "~da43a~") {
		t.Fatalf("expected same-result repeat block, got %q", got)
	}
}

func TestEmptyShellOutputResultIgnoresMetadata(t *testing.T) {
	if !isEmptyShellOutputResult("[shared_terminal/wsl exit 0, 62ms]\ncwd: /tmp/x") {
		t.Fatal("metadata-only shell result should be empty")
	}
	if isEmptyShellOutputResult("HTTP/1.1 301 Moved Permanently\n[shared_terminal/wsl exit 0, 62ms]") {
		t.Fatal("result with response body/header should not be empty")
	}
}

func TestBackendPromptTokensNeedCompaction(t *testing.T) {
	if !backendPromptTokensNeedCompaction(27000, 32000, 0.85) {
		t.Fatal("backend usage over capped threshold should compact")
	}
	if backendPromptTokensNeedCompaction(12000, 32000, 0.85) {
		t.Fatal("low backend usage should not compact")
	}
}

func TestShouldRequestRecoveryReport(t *testing.T) {
	if !shouldRequestRecoveryReport(TaskRun{StopReason: "repeated_same_tool_result"}, false) {
		t.Fatal("blocking tool stop should request a recovery report")
	}
	if shouldRequestRecoveryReport(TaskRun{StopReason: "repeated_same_tool_result"}, true) {
		t.Fatal("recovery report should only be requested once")
	}
	if shouldRequestRecoveryReport(TaskRun{StopReason: "user_stopped"}, false) {
		t.Fatal("user stops should not trigger automatic recovery")
	}
	if shouldRequestRecoveryReport(TaskRun{StopReason: "tool_budget_exhausted"}, false) {
		t.Fatal("tool budget has its own text-only summary path")
	}
	if shouldRequestRecoveryReport(TaskRun{}, false) {
		t.Fatal("missing stop reason should not trigger recovery")
	}
}

func TestRecoveryReportPromptIncludesRecentToolEvidence(t *testing.T) {
	prompt := recoveryReportPrompt(TaskRun{
		StopReason: "repeated_same_tool_result",
		StopDetail: "same curl result repeated",
		Tools: []TaskToolEvent{
			{Name: "shell", Status: "blocked", Input: `{"command":"curl -s http://connected.htb/admin/"}`, Result: "syntax error: '~da43a~'"},
		},
	})
	for _, want := range []string{"Recovery mode", "Do not call tools", "repeated_same_tool_result", "curl -s", "~da43a~", "Safest next action"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("recovery prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSharedTerminalParserIgnoresEchoAndWrapping(t *testing.T) {
	start, donePrefix, _ := sharedTerminalWrapper("id", "99")
	p := newSharedTerminalParser(start, donePrefix, "99")

	// Lines that arrive before the real START output (the echoed command, possibly
	// wrapped into fragments). With variable markers these carry no literal marker,
	// so none should be captured or end the command.
	echoFragments := []string{
		`M=__MA''ULER_; set -o pipefail 2>/dev/null || true; printf '%s\n' "${M}START_99`,
		`__"; { id; }; status=$?; printf '%s\n' "${M}CWD_99__$PWD"; printf '%s%s\n' "${M}DONE_99:" "$status"`,
	}
	for _, frag := range echoFragments {
		if done, _ := p.feed(terminalOutput{data: frag, stream: "stdout"}); done {
			t.Fatalf("echo fragment wrongly treated as done: %q", frag)
		}
	}
	if len(p.out) != 0 {
		t.Fatalf("echo captured as output: %#v", p.out)
	}

	// Defense-in-depth: even a literal DONE marker with a non-numeric status
	// (the old crash: status parsed as a quote) is ignored, not fatal.
	if done, _ := p.feed(terminalOutput{data: `__MAULER_DONE_99:' "$status"`, stream: "stdout"}); done {
		t.Fatal("garbage DONE status should be ignored, not completed")
	}

	// Real marker OUTPUT lines drive the result.
	p.feed(terminalOutput{data: "__MAULER_START_99__", stream: "stdout"})
	p.feed(terminalOutput{data: "uid=0(root) gid=0(root)", stream: "stdout"})
	p.feed(terminalOutput{data: "__MAULER_CWD_99__/root/loot", stream: "stdout"})
	done, code := p.feed(terminalOutput{data: "__MAULER_DONE_99:0", stream: "stdout"})
	if !done || code != 0 {
		t.Fatalf("real DONE not parsed: done=%v code=%d", done, code)
	}
	if len(p.out) != 1 || p.out[0].data != "uid=0(root) gid=0(root)" {
		t.Fatalf("captured output = %#v", p.out)
	}
	if p.cwd != "/root/loot" {
		t.Fatalf("cwd = %q", p.cwd)
	}
}

func TestSharedTerminalCWDExtractAndStrip(t *testing.T) {
	// Extraction from the cwd marker line.
	cwd, ok := sharedTerminalCWD("__MAULER_CWD_abc123__/home/kali/loot", "abc123")
	if !ok || cwd != "/home/kali/loot" {
		t.Fatalf("cwd extract = %q ok=%v", cwd, ok)
	}
	if _, ok := sharedTerminalCWD("regular output line", "abc123"); ok {
		t.Fatal("non-cwd line should not parse as cwd")
	}

	// The UI filter drops the whole cwd line (path and all), even with a space.
	var f uiMarkerFilter
	got := string(f.feed([]byte("real output\n__MAULER_CWD_abc123__/home/My Files\nmore\n")))
	got += string(f.flush())
	if got != "real output\nmore\n" {
		t.Fatalf("cwd line not stripped from UI: %q", got)
	}

	// A cwd marker split across reads is still dropped.
	f = uiMarkerFilter{}
	got = string(f.feed([]byte("x\n__MAULER_CWD_ab")))
	got += string(f.feed([]byte("c123__/tmp\ny\n")))
	got += string(f.flush())
	if got != "x\ny\n" {
		t.Fatalf("split cwd line not stripped: %q", got)
	}
}

func TestTerminalOutputRecordsBuffersPartialLines(t *testing.T) {
	records, pending := terminalOutputRecords("__MAULER_DO", "stdout", false)
	if len(records) != 0 || pending != "__MAULER_DO" {
		t.Fatalf("first partial = records %#v pending %q", records, pending)
	}
	records, pending = terminalOutputRecords(pending+"NE_abc123:0\n", "stdout", false)
	if pending != "" {
		t.Fatalf("pending after newline = %q", pending)
	}
	if len(records) != 1 || records[0].data != "__MAULER_DONE_abc123:0" {
		t.Fatalf("records = %#v", records)
	}
}

func TestUIMarkerFilterStripsMarkers(t *testing.T) {
	var f uiMarkerFilter
	// START on its own line is removed completely (no blank line left behind).
	got := string(f.feed([]byte("hello\n__MAULER_START_123__\nworld\n")))
	got += string(f.flush())
	if got != "hello\nworld\n" {
		t.Fatalf("start strip = %q", got)
	}

	// DONE attached to output keeps the output and its newline.
	f = uiMarkerFilter{}
	got = string(f.feed([]byte("200 OK__MAULER_DONE_123:0\n$ ")))
	got += string(f.flush())
	if got != "200 OK\n$ " {
		t.Fatalf("done strip = %q", got)
	}

	// A marker split across two reads is still removed.
	f = uiMarkerFilter{}
	got = string(f.feed([]byte("out__MAULER_DO")))
	got += string(f.feed([]byte("NE_123:7\nmore")))
	got += string(f.flush())
	if got != "out\nmore" {
		t.Fatalf("split marker strip = %q", got)
	}

	// Interactive output with no newline is passed through immediately (not held).
	f = uiMarkerFilter{}
	if got = string(f.feed([]byte("password: "))); got != "password: " {
		t.Fatalf("interactive passthrough = %q", got)
	}

	// A false "__MAULER_" that is not a real marker passes through.
	f = uiMarkerFilter{}
	got = string(f.feed([]byte("__MAULER_NOPE here")))
	got += string(f.flush())
	if got != "__MAULER_NOPE here" {
		t.Fatalf("false marker = %q", got)
	}

	// The echoed wrapper command (both markers on one line) is dropped whole.
	f = uiMarkerFilter{}
	echo := "set -o pipefail 2>/dev/null || true; printf '%s\\n' '__MAULER_START_9__'; { id; }; status=$?; printf '%s%s\\n' '__MAULER_DONE_9:' \"$status\"\n"
	got = string(f.feed([]byte(echo)))
	got += string(f.flush())
	if got != "" {
		t.Fatalf("wrapper echo not dropped: %q", got)
	}

	// The variable-marker wrapper echo carries no literal __MAULER_, so it must be
	// recognised by its assignment signature and dropped — including when it lands
	// as a trailing partial line before its newline arrives (the leak seen live).
	f = uiMarkerFilter{}
	varEcho := `M=__MA''ULER_; set -o pipefail 2>/dev/null || true; printf '%s\n' "${M}START_123__"; { curl -sL http://x ; }; status=$?; printf '%s%s\n' "${M}DONE_123:" "$status"`
	if got = string(f.feed([]byte(varEcho))); got != "" {
		t.Fatalf("variable wrapper echo leaked before newline: %q", got)
	}
	got = string(f.feed([]byte("\n403\n")))
	got += string(f.flush())
	if got != "403\n" {
		t.Fatalf("variable wrapper echo not dropped (want just the 403 output): %q", got)
	}

	// The live leak: the prompt was already emitted as a partial line, then the
	// wrapper echo arrived on that same prompt line. The filter should clear the
	// prompt row and suppress the wrapper, not stream the wrapper text.
	f = uiMarkerFilter{}
	got = string(f.feed([]byte("root@HomePc:/tmp$ ")))
	got += string(f.feed([]byte(`M=__MA''ULER_; set -o pipefail; printf '%s\n' "${M}START_456__"; { id; }; status=$?; printf '%s%s\n' "${M}DONE_456:" "$status"`)))
	got += string(f.feed([]byte("\nuid=0(root)\n")))
	got += string(f.flush())
	if strings.Contains(got, "M=__MA") || strings.Contains(got, "START_456") || !strings.Contains(got, "\r\x1b[2K") || !strings.Contains(got, "uid=0(root)\n") {
		t.Fatalf("prompt-prefixed wrapper echo was not suppressed correctly: %q", got)
	}

	f = uiMarkerFilter{}
	got = ""
	for _, chunk := range []string{
		"M", "=__", "MA", "''", "UL", "ER_; set -o pipefail; printf '%s\\n' \"${M}START_789__\"; { grep connected /etc/hosts; }; status=$?; printf '%s%s\\n' \"${M}DONE_789:\" \"$status\"",
		"\n10.129.245.100 connected.htb\n",
	} {
		got += string(f.feed([]byte(chunk)))
	}
	got += string(f.flush())
	if strings.Contains(got, "M=__MA") || strings.Contains(got, "START_789") || strings.Contains(got, "DONE_789") {
		t.Fatalf("chunked wrapper echo leaked to UI: %q", got)
	}
	if !strings.Contains(got, "10.129.245.100 connected.htb\n") {
		t.Fatalf("command output was lost: %q", got)
	}
}

// TestUIMarkerFilterSuppressesTruncatedWrapperEcho reproduces the live leak: a
// racy `stty -echo` ate a *variable-length prefix* of the `M=__MA”ULER_`
// assignment, so the echoed wrapper arrived as `MA”ULER_; …` or even `LER_; …`
// with no `M=__` at all. markerEchoSig (which needs the full prefix) then missed
// it and the wrapper leaked. The truncation-proof signatures (${M}, pipefail)
// must catch every truncation, on its own prompt row, and erase the row.
func TestUIMarkerFilterSuppressesTruncatedWrapperEcho(t *testing.T) {
	// Exact suffixes observed live (block 1/3: "MA''ULER_;", block 2: "LER_;").
	truncations := []string{
		`MA''ULER_`,
		`A''ULER_`,
		`''ULER_`,
		`ULER_`,
		`LER_`,
	}
	for _, pre := range truncations {
		var f uiMarkerFilter
		echo := pre + `; set -o pipefail 2>/dev/null || true; printf '%s\n' "${M}START_111__"; { grep connected.htb /etc/hosts || true; }; status=$?; printf '%s\n' "${M}CWD_111__$PWD"; printf '%s%s\n' "${M}DONE_111:" "$status"`
		got := string(f.feed([]byte(echo)))
		got += string(f.feed([]byte("\n10.129.245.100 connected.htb\n")))
		got += string(f.flush())
		if strings.Contains(got, "pipefail") || strings.Contains(got, "${M}") ||
			strings.Contains(got, "ULER_") || strings.Contains(got, "printf") {
			t.Fatalf("truncated wrapper echo (prefix %q) leaked to UI: %q", pre, got)
		}
		if !strings.Contains(got, "10.129.245.100 connected.htb\n") {
			t.Fatalf("command output lost for prefix %q: %q", pre, got)
		}
	}

	// Same truncation, but the prompt was already streamed on the row first: the
	// row must be cleared (\r\x1b[2K) and the wrapper text never shown.
	var f uiMarkerFilter
	got := string(f.feed([]byte("root@HomePc:.../HTB_writeups$ ")))
	got += string(f.feed([]byte(`LER_; set -o pipefail 2>/dev/null || true; printf '%s\n' "${M}START_222__"; { id; }; status=$?; printf '%s%s\n' "${M}DONE_222:" "$status"`)))
	got += string(f.feed([]byte("\nuid=0(root)\n")))
	got += string(f.flush())
	if strings.Contains(got, "pipefail") || strings.Contains(got, "${M}") || strings.Contains(got, "ULER_") {
		t.Fatalf("prompt-prefixed truncated wrapper echo leaked: %q", got)
	}
	if !strings.Contains(got, "\r\x1b[2K") || !strings.Contains(got, "uid=0(root)\n") {
		t.Fatalf("prompt row not cleared or output lost: %q", got)
	}

	// When xterm wraps a very long echoed wrapper, the final visual row can arrive
	// as only the status-print tail. Suppress that tail too.
	f = uiMarkerFilter{}
	got = string(f.feed([]byte(`1571575200:" "$status"`)))
	got += string(f.feed([]byte("\nreal output\n")))
	got += string(f.flush())
	if strings.Contains(got, "$status") || !strings.Contains(got, "real output\n") {
		t.Fatalf("wrapped status tail leaked or output was lost: %q", got)
	}
}

// TestUIMarkerFilterStripsRecoverySentinel covers the interrupt-recovery path:
// the echoed recovery command (which contains `stty echo …`) is suppressed via
// the stty signature, and the printed `__MAULER_RECOVER_<id>__` sentinel output
// is stripped so the user never sees Mauler's recovery plumbing.
func TestUIMarkerFilterStripsRecoverySentinel(t *testing.T) {
	var f uiMarkerFilter
	// Echoed recovery command line (carries the stty echo signature) + the sentinel
	// output line that the shell prints, followed by a real prompt.
	got := string(f.feed([]byte("stty echo 2>/dev/null || true; printf '%s\\n' '__MAULER_RECOVER_77__'\n")))
	got += string(f.feed([]byte("__MAULER_RECOVER_77__\n")))
	got += string(f.feed([]byte("root@HomePc:/tmp$ ")))
	got += string(f.flush())
	if strings.Contains(got, "__MAULER_RECOVER_") || strings.Contains(got, "stty echo") {
		t.Fatalf("recovery plumbing leaked to UI: %q", got)
	}
	if !strings.Contains(got, "root@HomePc:/tmp$ ") {
		t.Fatalf("prompt after recovery was lost: %q", got)
	}

	// The sentinel split across reads is still fully stripped.
	f = uiMarkerFilter{}
	got = string(f.feed([]byte("__MAULER_REC")))
	got += string(f.feed([]byte("OVER_88__\nback\n")))
	got += string(f.flush())
	if got != "back\n" {
		t.Fatalf("split recovery sentinel not stripped: %q", got)
	}
}

func TestSplitSharedTerminalDoneHandlesMarkerAttachedToOutput(t *testing.T) {
	donePrefix := "__MAULER_DONE_abc123:"
	status, preDone, ok := splitSharedTerminalDone("200 http://connected.htb/admin/config.php__MAULER_DONE_abc123:0", donePrefix)
	if !ok {
		t.Fatal("expected done marker to be detected inside output line")
	}
	if status != "0" {
		t.Fatalf("status = %q, want 0", status)
	}
	if preDone != "200 http://connected.htb/admin/config.php" {
		t.Fatalf("preDone = %q", preDone)
	}
}

func TestParseSharedTerminalExitCodeRejectsBadMarker(t *testing.T) {
	if code, err := parseSharedTerminalExitCode("130"); err != nil || code != 130 {
		t.Fatalf("valid exit code = %d, %v", code, err)
	}
	if code, err := parseSharedTerminalExitCode("not-a-code"); err == nil || code != -1 {
		t.Fatalf("invalid exit code = %d, %v; want -1 and error", code, err)
	}
}

func TestSharedTerminalBackendResolutionMatchesRuntime(t *testing.T) {
	wantAuto := "bash"
	wantAutoSupported := true
	if runtime.GOOS == "windows" {
		wantAuto = "powershell"
		wantAutoSupported = false
	}
	if got := resolveSharedTerminalBackend("auto"); got != wantAuto {
		t.Fatalf("auto backend resolved to %q, want %q", got, wantAuto)
	}
	if got := sharedTerminalSupportsBackend("auto"); got != wantAutoSupported {
		t.Fatalf("auto backend support = %v, want %v", got, wantAutoSupported)
	}
	if !sharedTerminalSupportsBackend("bash") || !sharedTerminalSupportsBackend("wsl") {
		t.Fatal("bash and wsl should support shared terminal")
	}
	for _, backend := range []string{"powershell", "pwsh", "cmd"} {
		if sharedTerminalSupportsBackend(backend) {
			t.Fatalf("%s should not use bash-only shared terminal wrapper", backend)
		}
	}
}

func TestFormatSharedTerminalResultFiltersWrapperEcho(t *testing.T) {
	lines := []terminalOutput{
		{stream: "stderr", data: "root@host:/tmp$ printf '%s\\n' '__MAULER_START_abc123__'; { grep x; }; status=$?; printf '%s%s\\n' '__MAULER_DONE_abc123:' \"$status\""},
		{stream: "stdout", data: "real output"},
	}
	got := formatSharedTerminalResult(lines, "wsl", 0, time.Second)
	if strings.Contains(got, "__MAULER_START_") || strings.Contains(got, "__MAULER_DONE_") || strings.Contains(got, "printf") {
		t.Fatalf("wrapper echo leaked into result:\n%s", got)
	}
	if !strings.Contains(got, "real output") {
		t.Fatalf("real output missing:\n%s", got)
	}
	for _, want := range []string{"[shell_result state=done backend=shared_terminal/wsl]", "contract:", "state: done", "exit: 0", "next_tool: proceed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("shared terminal result missing %q:\n%s", want, got)
		}
	}
	withCWD := withSharedTerminalCWD(got, "/tmp/work")
	if !strings.Contains(withCWD, "cwd: /tmp/work") || strings.Contains(withCWD, "cwd: unknown") {
		t.Fatalf("cwd was not injected into contract:\n%s", withCWD)
	}
}

func TestNormalizeToolCallArgumentsDecodesShellEntities(t *testing.T) {
	tc := llm.ToolCallDef{
		ID:   "call-1",
		Type: "function",
		Function: llm.FunctionCall{
			Name:      "shell",
			Arguments: json.RawMessage(`{"command":"curl -sk \"https://10.129.13.198/conn.php?cmd=id\" 2&gt;/dev/null","timeout":15}`),
		},
	}

	got := normalizeToolCallArguments(tc)
	args := string(got.Function.Arguments)
	if strings.Contains(args, "&gt;") || !strings.Contains(args, `2>/dev/null`) {
		t.Fatalf("shell tool args were not normalized: %s", args)
	}
}

func TestNormalizeToolCallArgumentsRepairsLegacyShellAndTerminalArgs(t *testing.T) {
	shellCall := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"bash":"grep connected /etc/hosts 2&gt;&amp;1"}`),
	}}
	gotShell := normalizeToolCallArguments(shellCall)
	shellArgs := string(gotShell.Function.Arguments)
	if !strings.Contains(shellArgs, `"command":"grep connected /etc/hosts 2>&1"`) || strings.Contains(shellArgs, `"bash"`) {
		t.Fatalf("legacy shell args were not canonicalized: %s", shellArgs)
	}

	terminalCall := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "terminal_send",
		Arguments: json.RawMessage(`{"data":"ping -c 2 connected.htb 2&gt;&amp;1","id":"shell-verify"}`),
	}}
	gotTerminal := normalizeToolCallArguments(terminalCall)
	terminalArgs := string(gotTerminal.Function.Arguments)
	if !strings.Contains(terminalArgs, `"command":"ping -c 2 connected.htb 2>&1"`) || strings.Contains(terminalArgs, `"data"`) {
		t.Fatalf("legacy terminal args were not canonicalized: %s", terminalArgs)
	}
}

func TestNormalizeToolCallArgumentsRepairsReadPathMarkup(t *testing.T) {
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "read",
		Arguments: json.RawMessage(`{"path":"C:/Users/richa/Documents/HTB_writeups/scans</path>"}`),
	}}
	got := normalizeToolCallArguments(tc)
	args := string(got.Function.Arguments)
	if strings.Contains(args, "</path>") || !strings.Contains(args, `"path":"C:/Users/richa/Documents/HTB_writeups/scans"`) {
		t.Fatalf("read path markup was not canonicalized: %s", args)
	}
}

func TestNormalizeToolCallArgumentsRepairsWriteEmbeddedContentMarkup(t *testing.T) {
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "write",
		Arguments: json.RawMessage("{\"path\":\"C:/Users/richa/Documents/HTB_writeups/JPHut/index.html</path>\\n<parameter=content><!DOCTYPE html>\\n<html>hut</html>\"}"),
	}}
	got := normalizeToolCallArguments(tc)
	args := string(got.Function.Arguments)
	if strings.Contains(args, "</path>") || strings.Contains(args, "<parameter=content>") {
		t.Fatalf("write embedded content markup was not removed: %s", args)
	}
	if !strings.Contains(args, `"path":"C:/Users/richa/Documents/HTB_writeups/JPHut/index.html"`) {
		t.Fatalf("write path was not repaired: %s", args)
	}
	if !strings.Contains(args, `"content":"<!DOCTYPE html>\n<html>hut</html>"`) {
		t.Fatalf("write content was not repaired: %s", args)
	}
}

func TestBuildProjectResumePromptIncludesExistingWriteupAndEvidence(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "Connected.md"), []byte("# Connected\n\nVerified webshell and privesc notes."), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "scans"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scans", "nmap_initial.txt"), []byte("80/tcp open http"), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg := settings.DefaultSettings()
	cfg.Context.Lab.Name = "Connected"
	cfg.Context.Lab.Target = "10.129.26.26"
	cfg.Context.Lab.Hostname = "connected.htb"
	cfg.Context.Lab.AccessPreference = "webshell"

	prompt := buildProjectResumePrompt(cfg)
	for _, want := range []string{
		"Project resume packet",
		"target=10.129.26.26",
		"Connected.md",
		"scans/nmap_initial.txt",
		"Verified webshell",
		"before rescanning or re-exploiting",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("resume prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestApplyWorkingContextBudgetTracksModelContext(t *testing.T) {
	a := &App{history: agent.NewHistory(32768)}
	// 64k model with a stale 32k mode preset: budget should follow the model
	// (minus the response reserve), not be pinned at 32768.
	applyWorkingContextBudget(a, 32768, 65536)
	if got := a.history.Budget(); got != 65536-workingContextOutputReserve {
		t.Fatalf("64k model budget = %d, want %d", got, 65536-workingContextOutputReserve)
	}
	// Smaller model: budget scales down with the real context.
	applyWorkingContextBudget(a, 32768, 30000)
	if got := a.history.Budget(); got != 30000-workingContextOutputReserve {
		t.Fatalf("30k model budget = %d, want %d", got, 30000-workingContextOutputReserve)
	}
}

func TestShouldUseSharedTerminal(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ShellMode = "shared_terminal"
	if !shouldUseSharedTerminal(cfg, "shell") {
		t.Fatal("expected shell to use shared terminal")
	}
	if shouldUseSharedTerminal(cfg, "read") {
		t.Fatal("non-shell tools should not use shared terminal")
	}
	cfg.ShellMode = "isolated"
	if shouldUseSharedTerminal(cfg, "shell") {
		t.Fatal("isolated shell mode should not use shared terminal")
	}
}

func TestSharedTerminalReadyForWrappedCommandRequiresPrompt(t *testing.T) {
	busy := &shellSession{id: "busy", scroll: newTerminalScrollback(20)}
	busy.scroll.append("Ncat: Listening on :::4444")
	busy.scroll.append("Ncat: Listening on 0.0.0.0:4444")
	if sharedTerminalReadyForWrappedCommand(busy) {
		t.Fatal("listener output without a shell prompt should be treated as busy")
	}

	idle := &shellSession{id: "idle", scroll: newTerminalScrollback(20)}
	idle.scroll.append("uid=0(root) gid=0(root)")
	idle.scroll.append("root@kali:~/HTB_writeups#")
	if !sharedTerminalReadyForWrappedCommand(idle) {
		t.Fatal("terminal tail ending at a shell prompt should be ready")
	}
}

func TestResolvedToolTimeoutUsesDefaultAndOverride(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.BashTimeout = 150
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "shell", Arguments: json.RawMessage(`{"command":"id"}`)}}
	if got := resolvedToolTimeout(cfg, tc); got != 150 {
		t.Fatalf("default shell timeout = %d, want 150", got)
	}

	tc.Function.Arguments = json.RawMessage(`{"command":"id","timeout":45}`)
	if got := resolvedToolTimeout(cfg, tc); got != 45 {
		t.Fatalf("explicit shell timeout = %d, want 45", got)
	}

	tc.Function.Name = "read"
	if got := resolvedToolTimeout(cfg, tc); got != 0 {
		t.Fatalf("non-shell timeout = %d, want 0", got)
	}
}

func TestAppendShellRecoveryHintsForFFUFFuzzError(t *testing.T) {
	result := "Keyword FUZZ defined, but not found in headers, method, URL or POST data."
	got := appendShellRecoveryHints(result)
	if !strings.Contains(got, "http://boxname.htb/admin/FUZZ") {
		t.Fatalf("ffuf recovery hint missing:\n%s", got)
	}
}

func TestAppendShellRecoveryHintsForGobusterLengthFlag(t *testing.T) {
	result := "Incorrect Usage: flag provided but not defined: -length\nNAME:\n   gobuster dir"
	got := appendShellRecoveryHints(result)
	if !strings.Contains(got, "--exclude-length") {
		t.Fatalf("gobuster recovery hint missing:\n%s", got)
	}
}

func TestRecoverBenignShellPipelineCloseForScannerHead(t *testing.T) {
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"command":"ffuf -u https://connected.htb/FUZZ -w words.txt 2>&1 | head -30"}`),
	}}
	result := "ffuf hit\n[shared_terminal/wsl exit 141, 2s]"
	got, ok := recoverBenignShellPipelineClose(tc, result, errors.New("exit code 141"))
	if !ok {
		t.Fatal("expected scanner | head exit 141 to be recovered")
	}
	if !strings.Contains(got, "SIGPIPE") || !strings.Contains(got, "Treat the shown output as evidence") {
		t.Fatalf("missing pipeline-close recovery hint:\n%s", got)
	}
}

func TestRecoverBenignShellPipelineCloseForCurlHeadExit23(t *testing.T) {
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"command":"curl -s http://connected.htb/admin/config.php | head -n 50"}`),
	}}
	result := "<html>FreePBX</html>\n[shared_terminal/wsl exit 23, 200ms]"
	got, ok := recoverBenignShellPipelineClose(tc, result, errors.New("exit code 23"))
	if !ok {
		t.Fatal("expected curl | head exit 23 to be recovered")
	}
	if !strings.Contains(got, "curl exit 23") || !strings.Contains(got, "Treat the shown output as evidence") {
		t.Fatalf("missing curl/head recovery hint:\n%s", got)
	}
}

func TestAppendShellCommandRecoveryHintsForFfufGrepJQNulls(t *testing.T) {
	got := appendShellCommandRecoveryHints("null null null\n[shared_terminal/wsl exit 0, 20ms]", `grep -E '"status":[[:space:]]*(200|301|302)' web_fuzz_results.txt | jq -r '. | "\(.status) \(.input.FUZZ) \(.url)"'`)
	if !strings.Contains(got, "Do not repeat this grep|jq pipeline") || !strings.Contains(got, ".results[]") {
		t.Fatalf("missing ffuf jq recovery hint:\n%s", got)
	}
}

func TestSanitizeTerminalLineStripsPromptControls(t *testing.T) {
	raw := "\x1b]0;root@HomePc: /mnt/c/Users/richa/Desktop/TheMauler\x07" +
		"\x1b[01;32mroot@HomePc\x1b[00m:\x1b[01;34m/mnt/c/Users/richa/Desktop/TheMauler\x1b[00m$ \x07"
	got := sanitizeTerminalLine(raw)
	if strings.Contains(got, "]0;") || strings.Contains(got, "\x1b") || strings.Contains(got, "\x07") {
		t.Fatalf("terminal controls leaked through: %q", got)
	}
	if got != "root@HomePc:/mnt/c/Users/richa/Desktop/TheMauler$ " {
		t.Fatalf("sanitizeTerminalLine = %q", got)
	}
}

func TestMaulerBashInteractiveArgsUsesAsciiPrompt(t *testing.T) {
	args := strings.Join(maulerBashInteractiveArgs(), " ")
	for _, want := range []string{"--rcfile", "test -f ~/.bashrc && . ~/.bashrc", "PROMPT_COMMAND=", "PROMPT_DIRTRIM=3", "PS1='\\\\u@\\\\h:\\\\w\\\\$ '"} {
		if !strings.Contains(args, want) {
			t.Fatalf("mauler bash args missing %q: %s", want, args)
		}
	}
}

func TestContextOverflowMarginLeavesRoomNearLoadedContext(t *testing.T) {
	if got := contextOverflowMargin(40960); got < 3000 {
		t.Fatalf("margin for 40k context too small: %d", got)
	}
}

func TestEnsureRequestContextRoomCompactsBeforeHardOverflow(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Context.CompactionAt = 0.95
	profile := settings.Profile{Name: "qwen", CtxTokens: 40960}
	app := &App{history: agent.NewHistory(profile.CtxTokens)}
	app.history.Append(llm.NewTextMessage(llm.RoleSystem, "system"))
	app.history.Append(llm.NewTextMessage(llm.RoleUser, strings.Repeat("u", 24000)))
	app.history.Append(llm.NewTextMessage(llm.RoleAssistant, strings.Repeat("a", 24000)))
	app.history.Append(llm.NewTextMessage(llm.RoleUser, strings.Repeat("b", 24000)))
	app.history.Append(llm.NewTextMessage(llm.RoleAssistant, strings.Repeat("c", 24000)))
	app.history.Append(llm.NewTextMessage(llm.RoleUser, strings.Repeat("d", 24000)))
	app.history.Append(llm.NewTextMessage(llm.RoleAssistant, strings.Repeat("e", 24000)))
	before := app.history.TokenCount()

	client := &countingLoader{actualContext: 40960}
	result := app.ensureRequestContextRoom(context.Background(), client, profile, &cfg, nil, nil)

	if result == nil {
		t.Fatal("expected preflight compaction before hard context overflow")
	}
	if app.history.TokenCount() >= before {
		t.Fatalf("expected preflight compaction to shrink history: before=%d after=%d", before, app.history.TokenCount())
	}
	if estimateChatPromptTokens(app.history.Messages(), nil)+contextOverflowMargin(40960) >= 40960 {
		t.Fatalf("history still estimates too close to context after preflight compaction")
	}
}

func TestSanitizeVisibleModelTextRemovesGemmaChannelTokens(t *testing.T) {
	got := sanitizeVisibleModelText("<|channel|>thought <channel|>Hello! I am TheMauler.")
	if got != "Hello! I am TheMauler." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextRemovesMalformedGemmaChannelTokens(t *testing.T) {
	got := sanitizeVisibleModelText("<|channel>thought <channel|>Hello! I am TheMauler.")
	if got != "Hello! I am TheMauler." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextRemovesGemmaChannelThoughtBlock(t *testing.T) {
	got := sanitizeVisibleModelText("<|channel>thought scratchpad <channel|>Hello! I am TheMauler.")
	if got != "Hello! I am TheMauler." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextDropsFakeSystemTail(t *testing.T) {
	got := sanitizeVisibleModelText(`Hello<end_of_turn> <start_of_turn>system {"stdout":"fake"}`)
	if got != "Hello" {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextHandlesSplitChannelLeakAfterAccumulation(t *testing.T) {
	raw := "<|chan" + "nel|>thought <channel|>Hello."
	got := sanitizeVisibleModelText(raw)
	if got != "Hello." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextRemovesBareThoughtPrefix(t *testing.T) {
	got := sanitizeVisibleModelText("thoughtIt is currently 2026-05-30.")
	if got != "It is currently 2026-05-30." {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSanitizeVisibleModelTextDropsMalformedThinkLeak(t *testing.T) {
	if got := sanitizeVisibleModelText("<think>*"); got != "" {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
	if got := sanitizeVisibleModelText("<think>hidden</think>\nVisible answer"); got != "Visible answer" {
		t.Fatalf("unexpected sanitized text: %q", got)
	}
}

func TestSplitModelTextForDisplayExtractsLiteralThinkBlocks(t *testing.T) {
	visible, thinking := splitModelTextForDisplay("<think>\nplan quietly\n</think>\nFinal answer")
	if strings.TrimSpace(visible) != "Final answer" {
		t.Fatalf("visible = %q", visible)
	}
	if thinking != "plan quietly" {
		t.Fatalf("thinking = %q", thinking)
	}
}

func TestSplitModelTextForDisplayStreamsOpenThinkBlock(t *testing.T) {
	visible, thinking := splitModelTextForDisplay("prefix\n<think>\nchecking evidence")
	if strings.TrimSpace(visible) != "prefix" {
		t.Fatalf("visible = %q", visible)
	}
	if thinking != "checking evidence" {
		t.Fatalf("thinking = %q", thinking)
	}
}

func TestSanitizeVisibleModelTextStripsRepairMarkers(t *testing.T) {
	for _, input := range []string{
		"[message content repaired: empty content]",
		"[internal repair: previous empty message omitted; continue the current task.]",
	} {
		if got := sanitizeVisibleModelText(input); got != "" {
			t.Fatalf("expected repair-only marker to be hidden for %q, got %q", input, got)
		}
	}
	if got := sanitizeVisibleModelText("before\n[message content repaired: empty content]\nafter"); got != "before\nafter" {
		t.Fatalf("expected marker line to be removed, got %q", got)
	}
}

func TestInvalidDoneReasonRejectsPlannerErrorWithJunkSummary(t *testing.T) {
	run := startTaskRun("jsut start the pkan", "Auto", "qwen3.6-nothink", "qwen")
	run.addTool("todo_write", `{"action":"replace"}`, "Active task plan", "done", 1)
	run.addTool("todo_write", `{"action":"update","id":"connected_privesc"}`, "error: todo_update: connected_privesc not found", "error", 1)

	reason := invalidDoneReason(run, "*")
	if !strings.Contains(reason, "Final assistant message was not meaningful") {
		t.Fatalf("unexpected invalid reason: %q", reason)
	}
}

func TestInvalidDoneReasonRejectsExecutionTaskWithOnlyPlannerTools(t *testing.T) {
	run := startTaskRun("carry on hacking connected.htb and start the plan", "Auto", "qwen3.6-nothink", "qwen")
	run.addTool("todo_write", `{"action":"replace"}`, "Active task plan", "done", 1)

	reason := invalidDoneReason(run, "Plan is ready.")
	if !strings.Contains(reason, "only updated the plan") {
		t.Fatalf("unexpected invalid reason: %q", reason)
	}
}

func TestInvalidDoneReasonRejectsActionIntentFinalSummary(t *testing.T) {
	run := startTaskRun("continue target 10.129.26.26", "Auto", "qwen3.6-nothink", "qwen")
	run.addTool("terminal_send", `{"command":"id && whoami && hostname"}`, "uid=0(root)", "done", 1)

	reason := invalidDoneReason(run, "The terminal is in a busy state. I need to send Ctrl+C to break any stuck process and get back to a clean prompt, then proceed with the task.")
	if !strings.Contains(reason, "describes a next action") {
		t.Fatalf("unexpected invalid reason: %q", reason)
	}
}

func TestParseInlineToolMarkupRepairsLocalModelToolText(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "shell"}},
		{Function: llm.ToolFunctionDef{Name: "session_search"}},
	}
	text := `<shell><command>ls -la</command></shell>
<session_search><query>project build complete</query><limit>10</limit></session_search>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Function.Name != "shell" || !strings.Contains(string(calls[0].Function.Arguments), `"command":"ls -la"`) {
		t.Fatalf("bad shell repair: %#v", calls[0])
	}
	if calls[1].Function.Name != "session_search" || !strings.Contains(string(calls[1].Function.Arguments), `"limit":10`) {
		t.Fatalf("bad session_search repair: %#v", calls[1])
	}
}

func TestParseInlineToolMarkupRepairsFunctionStyleToolText(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
		{Function: llm.ToolFunctionDef{Name: "read"}},
		{Function: llm.ToolFunctionDef{Name: "shell"}},
	}
	text := `I'll inspect the workspace now.

glob("**/*.go")
read('AGENTS.md')
shell(` + "`" + `go test ./...` + "`" + `)`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3: %#v", len(calls), calls)
	}
	got := map[string]string{}
	for _, call := range calls {
		got[call.Function.Name] = string(call.Function.Arguments)
	}
	if !strings.Contains(got["glob"], `"pattern":"**/*.go"`) {
		t.Fatalf("bad glob repair: %s", got["glob"])
	}
	if !strings.Contains(got["read"], `"path":"AGENTS.md"`) {
		t.Fatalf("bad read repair: %s", got["read"])
	}
	if !strings.Contains(got["shell"], `"command":"go test ./..."`) {
		t.Fatalf("bad shell repair: %s", got["shell"])
	}
}

func TestParseInlineToolMarkupRepairsQwenToolCallTemplate(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
		{Function: llm.ToolFunctionDef{Name: "read"}},
	}
	text := `<tool_call>
<function=glob>
<parameter=pattern>
**/*.go
</parameter>
</function>
</tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "glob" || !strings.Contains(string(calls[0].Function.Arguments), `"pattern":"**/*.go"`) {
		t.Fatalf("bad Qwen tool-call repair: %#v", calls[0])
	}
}

func TestParseInlineToolMarkupRepairsHermesJSONToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "read"}},
	}
	text := `<tool_call>
{"name":"read","arguments":{"path":"AGENTS.md","start_line":1,"end_line":20}}
</tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "read" || !strings.Contains(args, `"path":"AGENTS.md"`) || !strings.Contains(args, `"end_line":20`) {
		t.Fatalf("bad Hermes tool-call repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsNamedParametersToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_write"}},
		{Function: llm.ToolFunctionDef{Name: "read"}},
	}
	text := `<tool_call name="todo_write">
<parameters>
{
  "action": "replace",
  "items": [
    "Explore project structure and read existing files",
    "Fix identified issues in the code"
  ]
}
</parameters>
</tool_call>
<tool_call name="read">
<parameters>{"paths":["main.go","AGENTS.md"]}</parameters>
</tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "todo_write" || !strings.Contains(string(calls[0].Function.Arguments), "Explore project structure") {
		t.Fatalf("bad todo_write repair: %#v args=%s", calls[0], calls[0].Function.Arguments)
	}
	if calls[1].Function.Name != "read" || !strings.Contains(string(calls[1].Function.Arguments), `"paths":["`) {
		t.Fatalf("bad read repair: %#v args=%s", calls[1], calls[1].Function.Arguments)
	}
}

func TestParseInlineToolMarkupRepairsSelfClosingParametersAttribute(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "read",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		}},
	}
	text := `<|channel>thought
<channel|><tool_call name="read" parameters={"path": "idea.md"} />`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "read" || !strings.Contains(args, `"path":"idea.md"`) {
		t.Fatalf("bad self-closing repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsQuotedParametersAttributeWithRawQuotes(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "read",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		}},
	}
	text := `<|channel>thought <channel|><tool_call name="read" parameters="{ "path": "idea.md" }"/>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "read" || !strings.Contains(args, `"path":"idea.md"`) {
		t.Fatalf("bad quoted parameters repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsEscapedQuotedParametersAttribute(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "todo_write",
			Parameters: json.RawMessage(`{"type":"object","required":["action","items"],"properties":{"action":{"type":"string"},"items":{"type":"array","items":{"type":"string"}}}}`),
		}},
	}
	text := `<tool_call name="todo_write" parameters="{\"action\":\"replace\",\"items\":[\"Inspect existing app.py\",\"Test the application\"]}"/>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_write" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "Test the application") {
		t.Fatalf("bad escaped parameters repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupDropsRepairedCallMissingRequiredArgs(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{
			Name:       "read",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		}},
	}
	text := `<tool_call name="read" />`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 0 {
		t.Fatalf("expected invalid repaired call to be dropped, got %#v", calls)
	}
}

func TestParseInlineToolMarkupInfersMalformedQwenToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
		{Function: llm.ToolFunctionDef{Name: "grep"}},
	}
	text := "```\n\n```\n\n<tool_call>\n{\"pattern\":\"**/*.go\"}"

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "glob" || !strings.Contains(string(calls[0].Function.Arguments), `"pattern":"**/*.go"`) {
		t.Fatalf("bad malformed Qwen repair: %#v", calls[0])
	}
}

func TestParseInlineToolMarkupRepairsFencedToolInputJSON(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "glob"}},
	}
	text := "```json\n{\n  \"tool\": \"glob\",\n  \"input\": {\n    \"pattern\": \"**/*.go\"\n  }\n}\n```"

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "glob" || !strings.Contains(string(calls[0].Function.Arguments), `"pattern":"**/*.go"`) {
		t.Fatalf("bad fenced JSON repair: %#v", calls[0])
	}
}

func TestParseInlineToolMarkupRepairsGemmaPipeToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "web_search"}},
	}
	text := `<tool_call>call:web_search|limit:3,query:'<|'>top three car wash places near Bradley Stoke Bristol UK<|'}></tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "web_search" || !strings.Contains(args, `"limit":3`) || !strings.Contains(args, "Bradley Stoke Bristol UK") {
		t.Fatalf("bad Gemma pipe repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsGemmaTodoArrayPipeToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_write"}},
	}
	text := `thought<tool_call>call:todo_write|action:"replace",items:["Inspect current settings and profiles in the repository","Research optimal settings/parameters for Gemma4(LLM)","Compare research with current configuration","Suggest specific updates to settings/profiles"]<tool_call>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_write" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "Gemma4") {
		t.Fatalf("bad Gemma todo repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsGemmaTodoArrayBraceToolCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_write"}},
	}
	text := `thought<|tool_call>call:todo_write{action:"replace",items:[
    "Inspect current settings and profiles in the repository",
    "Research optimal settings/parameters for Gemma4(LLM)",
    "Compare research with current configuration",
    "Suggest specific updates to settings/profiles"
]}<tool_call|>`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_write" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "Gemma4") {
		t.Fatalf("bad Gemma todo brace repair: %#v args=%s", calls[0], args)
	}
}

func TestParseInlineToolMarkupRepairsGemmaFencedFunctionArray(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "todo_write"}},
	}
	text := "<|channel>thought\n<channel|>```json\n[\n  {\n    \"function\": \"todo_write\",\n    \"parameters\": {\n      \"action\": \"replace\",\n      \"items\": [\n        \"inspect settings\",\n        \"run tests\"\n      ]\n    }\n  }\n]\n```"

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if calls[0].Function.Name != "todo_write" || !strings.Contains(args, `"items":["`) || !strings.Contains(args, "run tests") {
		t.Fatalf("bad Gemma fenced function repair: %#v args=%s", calls[0], args)
	}
}

func TestContainsInlineToolMarkupDetectsGemmaPipeToolCall(t *testing.T) {
	if !containsInlineToolMarkup(`<tool_call>call:web_search|query:'x'</tool_call>`) {
		t.Fatalf("expected Gemma-style tool markup to be detected")
	}
}

func TestContainsHallucinatedToolResultDetectsFakeSystemTail(t *testing.T) {
	if !containsHallucinatedToolResult(`<end_of_turn> <start_of_turn>system {"stdout":"fake"}`) {
		t.Fatalf("expected fake system tool result tail to be detected")
	}
	if !containsHallucinatedToolResult(`<tool_result>{"stdout":"fake"}</tool_result>`) {
		t.Fatalf("expected tool_result text to be detected")
	}
	if containsHallucinatedToolResult(`The JSON shape is {"stdout":"text","stderr":"text"}.`) {
		t.Fatalf("plain explanatory JSON should not be treated as a hallucinated tool result")
	}
}

func TestSingleMentionedToolDefsNarrowsMalformedMarkup(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "shell"}},
		{Function: llm.ToolFunctionDef{Name: "read"}},
	}
	got := singleMentionedToolDefs(`<call:shell command="ls -la" />`, toolDefs)
	if len(got) != 1 || got[0].Function.Name != "shell" {
		t.Fatalf("expected shell to be narrowed, got %#v", got)
	}
	if got := singleMentionedToolDefs(`<call:shell command="x" /><tool_call name="read" parameters={"path":"x"}/>`, toolDefs); len(got) != 0 {
		t.Fatalf("multiple mentioned tools should not narrow: %#v", got)
	}
}

func TestParseConstrainedToolArgsContentConvertsPlainArgsJSON(t *testing.T) {
	toolDef := llm.ToolDef{Function: llm.ToolFunctionDef{
		Name:       "read",
		Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
	}}
	calls := parseConstrainedToolArgsContent(`{"path":"AGENTS.md"}`, toolDef)
	if len(calls) != 1 || calls[0].Function.Name != "read" || !strings.Contains(string(calls[0].Function.Arguments), `"path":"AGENTS.md"`) {
		t.Fatalf("bad constrained args repair: %#v", calls)
	}
	if calls := parseConstrainedToolArgsContent(`{"limit":10}`, toolDef); len(calls) != 0 {
		t.Fatalf("missing required args should not convert: %#v", calls)
	}
}

func TestParseConstrainedToolArgsContentConvertsFencedArgsJSON(t *testing.T) {
	toolDef := llm.ToolDef{Function: llm.ToolFunctionDef{
		Name:       "shell",
		Parameters: json.RawMessage(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"}}}`),
	}}
	calls := parseConstrainedToolArgsContent("```json\n{\"command\":\"ls -la\"}\n```", toolDef)
	if len(calls) != 1 || calls[0].Function.Name != "shell" || !strings.Contains(string(calls[0].Function.Arguments), `"command":"ls -la"`) {
		t.Fatalf("bad fenced constrained args repair: %#v", calls)
	}
}

func TestParseInlineToolMarkupRepairsGemmaAngleCall(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "shell"}},
	}
	text := `<|channel>thought <channel|><call:shell command="ls -R" /> <end_of_turn> <start_of_turn>system {"stdout":"fake"}`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	if calls[0].Function.Name != "shell" || !strings.Contains(string(calls[0].Function.Arguments), `"command":"ls -R"`) {
		t.Fatalf("bad Gemma angle call repair: %#v args=%s", calls[0], calls[0].Function.Arguments)
	}
}

func TestParseInlineToolMarkupRepairsGemmaBraceQuoteSentinels(t *testing.T) {
	toolDefs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "shell"}},
	}
	text := `<|channel>call:shell{command:<|"|>curl -X POST http://connected.htb/admin/ajax.php -d "action=getbrandmodel&id=1' AND 1=1--" -H "Content-Type: application/x-www-form-urlencoded"<|"|>}`

	calls := parseInlineToolMarkup(text, toolDefs)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %#v", len(calls), calls)
	}
	args := string(calls[0].Function.Arguments)
	if !strings.Contains(args, `curl -X POST http://connected.htb/admin/ajax.php`) || strings.Contains(args, `"command":"<"`) {
		t.Fatalf("bad Gemma brace sentinel repair: %#v args=%s", calls[0], args)
	}
}

func TestInvalidInlineShellCommandRejected(t *testing.T) {
	call := llm.ToolCallDef{
		Function: llm.FunctionCall{
			Name:      "shell",
			Arguments: json.RawMessage(`{"command":"<","|>curl -X POST http":"//connected.htb/admin/ajax.php"}`),
		},
	}
	if !invalidInlineToolCall(call) {
		t.Fatalf("malformed repaired shell command should be invalid")
	}
}

func TestToolChoiceTreatsDirectoryQuestionAsTask(t *testing.T) {
	if looksConversational("whats in this directory?") {
		t.Fatalf("directory listing question should expose tools")
	}
	// "what's in this directory" is explicit inspection intent — force the listing
	// tool call on the first turn rather than letting the model narrate.
	if got := toolChoiceFor("whats in this directory?", 0, 0); got != "required" {
		t.Fatalf("toolChoiceFor directory question = %q, want required", got)
	}
}

func TestLooksAboutToActCatchesLookIntent(t *testing.T) {
	text := "Let me look at the project structure and key files to understand what this is about."
	if !looksAboutToAct(text) {
		t.Fatalf("look intent should force an inspection tool directive")
	}
}

func TestLooksAboutToActCatchesFinishedSentenceIntent(t *testing.T) {
	text := "I now have everything I need. Let me create the new README with all the optimization details incorporated."
	if !looksAboutToAct(text) {
		t.Fatalf("finished sentence action intent should still force a tool directive")
	}
	if !strings.Contains(buildDirectivePrompt(text), "tool call") {
		t.Fatalf("directive prompt should require a tool call")
	}
}

func TestLooksAboutToActCatchesInspectionIntent(t *testing.T) {
	text := "Let me explore the project structure more broadly to find the real project README and understand what this project does."
	if !looksAboutToAct(text) {
		t.Fatalf("inspection intent should force a tool directive")
	}
	prompt := buildDirectivePrompt(text)
	if !strings.Contains(prompt, "glob") || !strings.Contains(prompt, "read") {
		t.Fatalf("inspection directive should suggest inspection tools: %s", prompt)
	}
}

func TestThinkingOnlyDiscoveryIntentBuildsInspectionDirective(t *testing.T) {
	text := "Let me start by discovering the current state of the target and checking existing notes."
	if !looksAboutToAct(text) {
		t.Fatalf("thinking-only discovery intent should force a tool directive")
	}
	prompt := buildDirectivePrompt(text)
	if !strings.Contains(prompt, "shell") || !strings.Contains(prompt, "read") {
		t.Fatalf("discovery directive should suggest inspection tools: %s", prompt)
	}
}

func TestVisibleTextBeforeInlineToolMarkupKeepsProgressSummary(t *testing.T) {
	raw := "Good -- the box is running. Let me check known CVEs.\n<|tool_call>call:shell{command:<|\"|>curl http://connected.htb<|\"|>}<tool_call|>"
	got := visibleTextBeforeInlineToolMarkup(raw)
	if !strings.Contains(got, "box is running") || strings.Contains(got, "tool_call") || strings.Contains(got, "curl") {
		t.Fatalf("visibleTextBeforeInlineToolMarkup = %q", got)
	}
}

func TestClassifyAgentMode(t *testing.T) {
	tests := map[string]string{
		"please review this code":                           "Reviewer",
		"latest news about local models":                    "Researcher",
		"fix the failing build error":                       "Fixer",
		"plan the architecture":                             "Planner",
		"implement the settings page":                       "Builder",
		"make a plan and update files":                      "Builder",
		"carry on hacking the HTB box":                      "Auto",
		"carry on hacking the target and get user and root": "Auto",
		"hello there":                                       "Auto",
	}
	for input, want := range tests {
		if got := classifyAgentMode(input).Name; got != want {
			t.Fatalf("classifyAgentMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClassifyAgentModeKeepsOperationalAttackWorkGeneric(t *testing.T) {
	for _, input := range []string{
		"create a foothold payload for the target",
		"verify RCE against 10.129.15.218",
		"exploit FreePBX on connected.htb",
		"start a listener and catch a reverse shell",
	} {
		if got := classifyAgentMode(input).Name; got != "Auto" {
			t.Fatalf("classifyAgentMode(%q) = %q, want Auto", input, got)
		}
	}
	if got := classifyAgentMode("research the latest CVE writeups").Name; got != "Researcher" {
		t.Fatalf("pure web research should stay Researcher, got %q", got)
	}
}

func TestSelectAgentModeDoesNotAutoPromoteHTBWorkspaceToOps(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ModeOverride = "Auto"
	cfg.Context.WorkspaceDir = "C:/Users/richa/Documents/HTB_writeups"
	cfg.Context.OpenFolders = []settings.WorkspaceFolder{{Path: "C:/Users/richa/Documents/HTB_writeups/scans", Role: "scans"}}
	got := selectAgentMode("carry on", cfg)
	if got.Name != "Builder" {
		t.Fatalf("carry on in HTB workspace routed to %q, want Builder", got.Name)
	}

	cfg.Context.WorkspaceDir = "C:/Users/richa/Desktop/TheMauler"
	cfg.Context.OpenFolders = nil
	got = selectAgentMode("implement the UI feature", cfg)
	if got.Name != "Builder" {
		t.Fatalf("codebase implementation routed to %q, want Builder", got.Name)
	}
}

func TestFormatEnabledToolSummaryReflectsEffectiveToolset(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "offline"
	summary := formatEnabledToolSummary(cfg)
	if !strings.Contains(summary, "write and edit files") || !strings.Contains(summary, "run shell commands") {
		t.Fatalf("offline summary should include local code tools: %s", summary)
	}
	if strings.Contains(summary, "search and fetch the web") {
		t.Fatalf("offline summary should not include web tools: %s", summary)
	}

	cfg.ActiveToolset = "safe"
	summary = formatEnabledToolSummary(cfg)
	if strings.Contains(summary, "write and edit files") || strings.Contains(summary, "run shell commands") {
		t.Fatalf("safe summary should not claim write/shell access: %s", summary)
	}
}

func TestManualAgentMode(t *testing.T) {
	mode := manualAgentMode()
	if mode.Name != "Manual" || mode.Instructions == "" {
		t.Fatalf("manual mode not populated: %#v", mode)
	}
}

func TestGetHistoryStatsReturnsConcreteValues(t *testing.T) {
	app := &App{
		history:                 agent.NewHistory(4096),
		rollback:                &agent.Rollback{},
		contextWindow:           8192,
		configuredContextWindow: 32768,
	}
	app.history.Append(llm.NewTextMessage(llm.RoleUser, "hello"))

	stats := app.GetHistoryStats()

	if stats.Budget != 4096 {
		t.Fatalf("Budget = %d, want 4096", stats.Budget)
	}
	if stats.TokenCount == 0 {
		t.Fatalf("TokenCount should be populated: %#v", stats)
	}
	if stats.Window != 8192 || stats.ConfiguredWindow != 32768 {
		t.Fatalf("context windows not populated: %#v", stats)
	}
}

func TestSaveFileContentSnapshotsExistingFileForRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{rollback: &agent.Rollback{}}

	if err := app.SaveFileContent(path, "after"); err != nil {
		t.Fatal(err)
	}
	if app.rollback.Len() != 1 {
		t.Fatalf("rollback depth = %d, want 1", app.rollback.Len())
	}
	msg := app.Undo()
	if !strings.Contains(msg, "restored") {
		t.Fatalf("Undo() = %q, want restored message", msg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "before" {
		t.Fatalf("file content after undo = %q, want before", data)
	}
}

func TestDoCompactCanBeCalledWithoutCallerHoldingLock(t *testing.T) {
	app := &App{history: agent.NewHistory(4096)}
	for i := 0; i < 14; i++ {
		app.history.Append(llm.NewTextMessage(llm.RoleUser, "important details"))
	}

	app.doCompact(context.Background(), &summaryClient{}, settings.Profile{})

	msgs := app.history.Messages()
	joined := ""
	for _, msg := range msgs {
		joined += messageText(msg)
	}
	if !strings.Contains(joined, "summary ok") {
		t.Fatalf("history was not compacted with summary: %#v", msgs)
	}
}

func TestSelectAgentModeOverrideAndPresetInstructions(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ModeOverride = "Reviewer"
	cfg.Agents.Presets["Reviewer"] = settings.AgentModePreset{
		Enabled:      true,
		Instructions: "custom review instructions",
	}

	mode := selectAgentMode("please implement this", cfg)

	if mode.Name != "Reviewer" || mode.Instructions != "custom review instructions" {
		t.Fatalf("override/preset not applied: %#v", mode)
	}
}

func TestApplyAgentPresetOfflineDisablesExternalTools(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.OfflineOnly = true
	profiles := settings.DefaultProfiles()
	profile := activeProfile(&cfg, &profiles)
	autonomous := false

	applyAgentPreset(&cfg, &profiles, AgentMode{Name: "Researcher"}, &profile, &autonomous)

	for _, name := range []string{"web_search", "fetch_url", "browser"} {
		if cfg.Tools.EnabledTools[name] {
			t.Fatalf("%s should be disabled by offline mode", name)
		}
	}
	if cfg.Tools.ActiveToolset != "web-research" {
		t.Fatalf("researcher preset should select web-research toolset, got %q", cfg.Tools.ActiveToolset)
	}
}

func TestApplyAgentPresetKeepsExplicitUnrestrictedToolset(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Tools.ActiveToolset = "unrestricted"
	profiles := settings.DefaultProfiles()
	profile := activeProfile(&cfg, &profiles)
	autonomous := true

	applyAgentPreset(&cfg, &profiles, AgentMode{Name: "Researcher"}, &profile, &autonomous)

	if cfg.Tools.ActiveToolset != "unrestricted" {
		t.Fatalf("explicit unrestricted toolset was downgraded to %q", cfg.Tools.ActiveToolset)
	}
	effective := settings.EffectiveEnabledTools(cfg.Tools)
	if !effective["write"] || !effective["shell"] || !effective["web_search"] {
		t.Fatalf("unrestricted should keep write/shell/web tools enabled: %#v", effective)
	}
}

func TestToolDisabledMessageNamesActiveToolset(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "web-research"

	msg := toolDisabledMessage(cfg, "write")

	if !strings.Contains(msg, "web-research") || !strings.Contains(msg, "Enabled tools now") || !strings.Contains(msg, "unrestricted") {
		t.Fatalf("disabled message should explain toolset cause, got %q", msg)
	}
}

func TestDisabledToolStopsOnlyAfterRepeat(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{{Name: "shell", Status: "disabled"}}}
	if shouldStopForDisabledTool(run, "shell") {
		t.Fatal("first disabled tool call should be recoverable")
	}
	run.Tools = append(run.Tools, TaskToolEvent{Name: "shell", Status: "disabled"})
	if !shouldStopForDisabledTool(run, "shell") {
		t.Fatal("repeated disabled tool call should block")
	}
	run.Tools = append(run.Tools, TaskToolEvent{Name: "web_search", Status: "done"})
	if shouldStopForDisabledTool(run, "shell") {
		t.Fatal("non-disabled progress should reset the disabled repeat check")
	}
}

func TestDisabledToolRecoveryPolicy(t *testing.T) {
	cfg := settings.DefaultSettings().Tools
	cfg.ActiveToolset = "web-research"
	tc := llm.ToolCallDef{Function: llm.FunctionCall{Name: "write"}}

	decision := evaluateDisabledToolRecoveryPolicy(TaskRun{}, cfg, tc)
	if decision.HardStop || decision.RunState != "recovering" || decision.ToolStatus != "disabled" {
		t.Fatalf("first disabled tool should recover, got %#v", decision)
	}

	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "write", Status: "disabled"},
		{Name: "write", Status: "disabled"},
	}}
	decision = evaluateDisabledToolRecoveryPolicy(run, cfg, tc)
	if !decision.HardStop || decision.StopReason != "tool_disabled" || decision.RunState != "blocked" {
		t.Fatalf("repeated disabled tool should block, got %#v", decision)
	}
}

func TestToolCallAdvertised(t *testing.T) {
	defs := []llm.ToolDef{
		{Function: llm.ToolFunctionDef{Name: "http_probe"}},
		{Function: llm.ToolFunctionDef{Name: "shell"}},
	}
	if !toolCallAdvertised(defs, "shell") {
		t.Fatal("expected shell to be advertised")
	}
	if toolCallAdvertised(defs, "terminal_send") {
		t.Fatal("terminal_send should not be advertised")
	}
}

func TestDuplicateFetchURLSkip(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "fetch_url", Status: "done", Input: `{"url":"https://example.com/a/"}`, Result: "old"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "fetch_url",
		Arguments: json.RawMessage(`{"url":"https://example.com/a"}`),
	}}
	got := duplicateFetchURLSkip(run, tc)
	if !strings.Contains(got, "already fetched") || !strings.Contains(got, "different high-quality source") {
		t.Fatalf("duplicate fetch was not skipped with useful guidance: %q", got)
	}

	decision := evaluateSkipRecoveryPolicy(run, tc)
	if decision.ToolStatus != "skipped" || decision.RunState != "recovering" || !strings.Contains(decision.Message, "already fetched") {
		t.Fatalf("duplicate fetch policy did not return skip decision: %#v", decision)
	}
}

func TestPreToolRecoveryPolicyMapsRepeatedShellFailure(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "error", Input: `{"command":"curl http://target/admin"}`},
		{Name: "shell", Status: "blocked", Input: `{"command":"curl http://target/admin"}`},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"command":"curl http://target/admin"}`),
	}}

	decision := evaluatePreToolRecoveryPolicy(run, tc)
	if decision.HardStop || decision.StopReason != "" || decision.ToolStatus != "skipped" || decision.RunState != "recovering" || !strings.Contains(decision.Message, "Repeated shell command blocked") {
		t.Fatalf("unexpected policy decision: %#v", decision)
	}
	if !strings.Contains(decision.Message, "skipped without stopping") {
		t.Fatalf("repeated failure should be recoverable first: %q", decision.Message)
	}
}

func TestPreToolRecoveryPolicyResetsRepeatedShellFailureAfterSuccess(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "error", Input: `{"command":"curl http://target/admin"}`},
		{Name: "shell", Status: "done", Input: `{"command":"curl http://target/admin"}`, Result: "HTTP/1.1 200 OK"},
		{Name: "shell", Status: "error", Input: `{"command":"curl http://target/admin"}`},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(`{"command":"curl http://target/admin"}`),
	}}

	decision := evaluatePreToolRecoveryPolicy(run, tc)
	if decision.HardStop || decision.StopReason != "" {
		t.Fatalf("successful same-command result should reset repeated failure streak: %#v", decision)
	}
}

func TestSharedTerminalFallbackResultIsMarked(t *testing.T) {
	fallback := sharedTerminalFallbackNote(errSharedTerminalBusy, TerminalStateSnapshot{State: "listener", Summary: "Shared terminal appears to be running a listener"})
	result := fallback + "[wsl exit 7, 3.2s]\nerror: exit code 7"
	if !strings.Contains(result, "isolated shell fallback") || !strings.Contains(result, "[wsl exit 7") {
		t.Fatalf("fallback result marker missing: %q", result)
	}
	if !strings.Contains(result, "Shared terminal state: listener") || !strings.Contains(result, "terminal_read/terminal_send") {
		t.Fatalf("fallback result should explain state-aware routing: %q", result)
	}
}

func TestPreToolRecoveryPolicySoftSkipsRepeatedSameResult(t *testing.T) {
	input := `{"command":"sed -n '63,78p' /tmp/cve.py | cat -A"}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "same evidence\n[shared_terminal/wsl exit 0, 12ms]"},
		{Name: "shell", Status: "done", Input: input, Result: "same evidence\n[shared_terminal/wsl exit 0, 10ms]"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(input),
	}}

	decision := evaluatePreToolRecoveryPolicy(run, tc)
	if decision.HardStop || decision.StopReason != "" || decision.ToolStatus != "skipped" || decision.RunState != "recovering" {
		t.Fatalf("repeated same result should be a recoverable skip first: %#v", decision)
	}
	if !strings.Contains(decision.Message, "skipped without stopping") {
		t.Fatalf("missing soft recovery wording: %q", decision.Message)
	}
}

func TestPreToolRecoveryPolicyKeepsRepeatedSuccessfulShellResultsRecovering(t *testing.T) {
	input := `{"command":"sed -n '63,78p' /tmp/cve.py | cat -A"}`
	skipResult := "Repeated shell command blocked after 2 identical successful results\nRecovery: this repeated command was skipped without stopping the run."
	tools := []TaskToolEvent{
		{Name: "shell", Status: "done", Input: input, Result: "same evidence\n[shared_terminal/wsl exit 0, 12ms]"},
		{Name: "shell", Status: "done", Input: input, Result: "same evidence\n[shared_terminal/wsl exit 0, 10ms]"},
	}
	for i := 0; i < repeatedShellRecoverySkipHardStopThreshold; i++ {
		tools = append(tools, TaskToolEvent{Name: "shell", Status: "skipped", Input: input, Result: skipResult})
	}
	run := TaskRun{Tools: tools}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(input),
	}}

	decision := evaluatePreToolRecoveryPolicy(run, tc)
	if decision.HardStop || decision.StopReason != "" || decision.ToolStatus != "skipped" || decision.RunState != "recovering" {
		t.Fatalf("repeated successful shell result should stay recoverable: %#v", decision)
	}
	if !strings.Contains(decision.Message, "Cached result preview") {
		t.Fatalf("repeated successful shell result should include cached evidence: %q", decision.Message)
	}
}

func TestPreToolRecoveryPolicyStillHardStopsRepeatedShellFailuresWhenIgnored(t *testing.T) {
	input := `{"command":"curl -sk https://connected.htb/missing"}`
	skipResult := "Repeated shell command blocked after 2 recent failures\nRecovery: this repeated command was skipped without stopping the run."
	tools := []TaskToolEvent{
		{Name: "shell", Status: "error", Input: input, Result: "curl: (7) Failed to connect"},
		{Name: "shell", Status: "error", Input: input, Result: "curl: (7) Failed to connect"},
	}
	for i := 0; i < repeatedShellRecoverySkipHardStopThreshold; i++ {
		tools = append(tools, TaskToolEvent{Name: "shell", Status: "skipped", Input: input, Result: skipResult})
	}
	run := TaskRun{Tools: tools}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "shell",
		Arguments: json.RawMessage(input),
	}}

	decision := evaluatePreToolRecoveryPolicy(run, tc)
	if !decision.HardStop || decision.StopReason != "repeated_tool_failure" || decision.ToolStatus != "blocked" {
		t.Fatalf("ignored repeated shell failures should still hard stop: %#v", decision)
	}
}

func TestSkipRecoveryPolicySkipsRepeatedTerminalInterrupt(t *testing.T) {
	input := `{"key_sequence":["Ctrl+C","Enter"],"wait_ms":5000}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "terminal_send", Status: "done", Input: input, Result: "screen"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "terminal_send",
		Arguments: json.RawMessage(input),
	}}

	decision := evaluateSkipRecoveryPolicy(run, tc)
	if decision.ToolStatus != "skipped" || decision.RunState != "recovering" || !strings.Contains(decision.Message, "Ctrl-C was already sent once") {
		t.Fatalf("expected repeated Ctrl-C to be skipped for recovery: %#v", decision)
	}
}

func TestSkipRecoveryPolicySkipsRepeatedTerminalEnter(t *testing.T) {
	input := `{"keys":"enter","wait_ms":3000}`
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "terminal_send", Status: "done", Input: input, Result: "screen"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "terminal_send",
		Arguments: json.RawMessage(input),
	}}

	decision := evaluateSkipRecoveryPolicy(run, tc)
	if decision.ToolStatus != "skipped" || decision.RunState != "recovering" || !strings.Contains(decision.Message, "Enter was already sent once") {
		t.Fatalf("expected repeated Enter to be skipped for recovery: %#v", decision)
	}
}

func TestPreToolRecoveryBlocksRepeatedMalformedTerminalSendArgs(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "terminal_send", Status: "error", Result: "error: terminal_send: provide keys and/or control"},
		{Name: "terminal_send", Status: "error", Result: "error: terminal_send: provide keys and/or control"},
	}}
	tc := llm.ToolCallDef{Function: llm.FunctionCall{
		Name:      "terminal_send",
		Arguments: json.RawMessage(`{"data":"ping -c 2 connected.htb 2>&1"}`),
	}}

	decision := evaluatePreToolRecoveryPolicy(run, tc)
	if decision.ToolStatus != "skipped" || decision.RunState != "recovering" || !strings.Contains(decision.Message, "repeated malformed argument errors") {
		t.Fatalf("expected malformed terminal_send args to be skipped for recovery: %#v", decision)
	}
}

func TestAgentPresetContextBudgetDoesNotShrinkLoadContext(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Agents.ModeOverride = "Builder"
	cfg.Agents.Presets["Builder"] = settings.AgentModePreset{
		Enabled:       true,
		ContextBudget: 32768,
	}
	profiles := settings.DefaultProfiles()
	profile := settings.Profile{
		Name:      "gemma4-31b",
		Backend:   "llamacpp",
		BaseURL:   "http://127.0.0.1:8802/v1",
		ModelID:   "gemma-4-31B-it-uncensored-heretic-Q4_K_S.gguf",
		CtxTokens: 120000,
	}
	autonomous := false

	mode := selectAgentMode("build this", cfg)
	applyAgentPreset(&cfg, &profiles, mode, &profile, &autonomous)

	if profile.CtxTokens != 120000 {
		t.Fatalf("profile ctx_tokens = %d, want backend load context preserved", profile.CtxTokens)
	}
	if mode.ContextBudget != 32768 {
		t.Fatalf("mode context budget = %d, want preset working budget", mode.ContextBudget)
	}
}

func TestApplySafetyPresetUnrestrictedEnablesFullAccess(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	cfg.Tools.ConfirmExec = true
	cfg.Tools.ConfirmWrites = true
	cfg.Tools.ProtectedPaths = []string{"C:/protected"}
	cfg.Agents.OfflineOnly = true
	cfg.Tools.EnabledTools["web_search"] = false
	app := &App{cfg: &cfg}

	if err := app.ApplySafetyPreset("unrestricted"); err != nil {
		t.Fatal(err)
	}

	if !app.autonomous || app.cfg.Agents.OfflineOnly || app.cfg.Tools.ConfirmExec || app.cfg.Tools.ConfirmWrites {
		t.Fatalf("unrestricted preset did not enable full access: autonomous=%v cfg=%#v", app.autonomous, app.cfg)
	}
	if !app.cfg.Tools.EnabledTools["web_search"] || !app.cfg.Tools.EnabledTools["shell"] {
		t.Fatalf("unrestricted preset should enable default tools: %#v", app.cfg.Tools.EnabledTools)
	}
	if app.cfg.Tools.ActiveToolset != "unrestricted" {
		t.Fatalf("unrestricted preset should select unrestricted toolset, got %q", app.cfg.Tools.ActiveToolset)
	}
	if len(app.cfg.Tools.ProtectedPaths) != 0 {
		t.Fatalf("unrestricted preset should clear protected paths: %#v", app.cfg.Tools.ProtectedPaths)
	}
}

func TestApplySafetyPresetOfflineSelectsOfflineToolsetAndBlocksBrowserAgent(t *testing.T) {
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	cfg := settings.DefaultSettings()
	app := &App{cfg: &cfg}

	if err := app.ApplySafetyPreset("offline"); err != nil {
		t.Fatal(err)
	}

	if app.autonomous || !app.cfg.Agents.OfflineOnly || app.cfg.Tools.ActiveToolset != "offline" {
		t.Fatalf("offline preset did not set expected state: autonomous=%v cfg=%#v", app.autonomous, app.cfg)
	}
	if app.cfg.Tools.EnabledTools["web_search"] || app.cfg.Tools.EnabledTools["browser"] {
		t.Fatalf("offline preset should disable external/browser-agent tools: %#v", app.cfg.Tools.EnabledTools)
	}
}

func TestBuildSystemPromptInjectsRelevantMemory(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Context.MAULERMDPath = "C:/does/not/exist/MAULER.md"
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Auto"}, []MemoryEntry{{
		Title:      "Provider",
		Content:    "LM Studio runs on the LAN endpoint.",
		Kind:       "preference",
		Importance: 5,
		Tags:       []string{"local"},
	}}, nil)

	if !strings.Contains(prompt, "Relevant project memory") || !strings.Contains(prompt, "LM Studio runs") || !strings.Contains(prompt, "tags=local") {
		t.Fatalf("memory was not injected into prompt: %s", prompt)
	}
}

func TestBuildSystemPromptSplitsMemoryPackets(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Context.MAULERMDPath = "C:/does/not/exist/MAULER.md"
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Auto"}, []MemoryEntry{
		{Title: "Style", Content: "Prefer WSL shell.", Kind: "preference", Confidence: "confirmed", Source: "user"},
		{Title: "Target", Content: "HTTP service observed.", Kind: "fact", Confidence: "confirmed", Source: "tool"},
		{Title: "Old run", Content: "Prior exploit may apply.", Kind: "note", Confidence: "confirmed", Source: "previous_run"},
		{Title: "Guess", Content: "FreePBX path might be present.", Kind: "fact", Confidence: "hypothesis", Source: "model"},
	}, nil)

	wants := []string{
		"Relevant project memory - user preferences and constraints",
		"Relevant project memory - confirmed facts and decisions",
		"Relevant project memory - previous run recall",
		"Relevant project memory - unverified or stale",
		"UNVERIFIED hypothesis: Guess",
	}
	for _, want := range wants {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptOpsTreatsMemoryAsHypothesis(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Context.MAULERMDPath = "C:/does/not/exist/MAULER.md"
	cfg.Tools.ShellBackend = "wsl"
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Ops"}, nil, nil)

	for _, want := range []string{"Ops mode", "hypotheses", "live target evidence", "Do not choose an exploit only because a memory", "fresh current-source pass", "start_listener", "terminal_send"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("Ops prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildSystemPromptKeepsToolRoutingCompact(t *testing.T) {
	cfg := settings.DefaultSettings()
	cfg.Context.MAULERMDPath = "C:/does/not/exist/MAULER.md"
	cfg.Tools.ShellBackend = "wsl"
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Ops"}, nil, nil)

	for _, want := range []string{
		"Tool routing:",
		"terminal_send/terminal_read only for commands that belong inside a live or interactive terminal session",
		"http_probe for independent HTTP/webshell/curl/wget checks",
		"Do not type independent HTTP or webshell probes into a connected terminal",
		"Use terminal_send only for a real live terminal session",
		"trigger the callback through http_probe/shell/webshell",
		"Loop discipline:",
		"Web budgets:",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("compact prompt missing %q: %s", want, prompt)
		}
	}
	for _, notWant := range []string{
		"For interactive, prompt-driven, long-running, or live-watched terminal work",
		"Prefer glob/grep/file_outline/read_chunks/read_file/read_many/read_pdf",
		"terminal tools for live or interactive commands, shell for short deterministic one-shots",
		"prefer shell for target HTTP/TCP",
		"Reverse-shell stable path:",
		"Reverse shell order (do NOT skip)",
		"For exploit, CVE, PoC, CTF/HTB, or service-version research",
	} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("system prompt still contains moved tool prose %q:\n%s", notWant, prompt)
		}
	}
}

func TestBuildSystemPromptIncludesProgressArtifact(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	}()
	if err := os.MkdirAll(filepath.Join(dir, ".mauler"), 0o755); err != nil {
		t.Fatal(err)
	}
	progress := "# Progress\n\n## Objective\n\nFinish the current agent-loop upgrade.\n\n## Next Steps\n\nWire resume context."
	if err := os.WriteFile(filepath.Join(dir, ".mauler", "progress.md"), []byte(progress), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := settings.DefaultSettings()
	cfg.Context.MAULERMDPath = "C:/does/not/exist/MAULER.md"
	prompt := buildSystemPrompt(cfg, AgentMode{Name: "Builder"}, nil, nil)

	if !strings.Contains(prompt, "Workspace progress artifact") || !strings.Contains(prompt, "Finish the current agent-loop upgrade") {
		t.Fatalf("prompt did not include progress artifact:\n%s", prompt)
	}
}

func TestBuildProgressArtifactPromptKeepsLatestTail(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	}()
	if err := os.MkdirAll(filepath.Join(dir, ".mauler"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := "OLD-STALE-OPENING\n" + strings.Repeat("old context line\n", 260)
	latest := "LATEST-RESUME-POINT use terminal_read and continue from the active shell"
	if err := os.WriteFile(filepath.Join(dir, ".mauler", "progress.md"), []byte(old+"\n"+latest), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := buildProgressArtifactPrompt()
	if !strings.Contains(prompt, "latest progress excerpt") || !strings.Contains(prompt, latest) {
		t.Fatalf("progress prompt did not keep latest tail:\n%s", prompt)
	}
	if strings.Contains(prompt, "OLD-STALE-OPENING") {
		t.Fatalf("progress prompt kept stale head:\n%s", prompt)
	}
	if len([]rune(prompt)) > 1300 {
		t.Fatalf("progress prompt too large: %d runes", len([]rune(prompt)))
	}
}

func TestWriteMemoryPromptLineCapsLargeContent(t *testing.T) {
	var sb strings.Builder
	writeMemoryPromptLine(&sb, MemoryEntry{
		Title:      strings.Repeat("Title ", 80),
		Content:    strings.Repeat("large compaction memory body ", 80),
		Kind:       "note",
		Confidence: "confirmed",
		Source:     "previous_run",
		Importance: 5,
		Tags:       []string{"one", "two", "three", "four", "five", "six", "seven", "eight"},
	})

	line := sb.String()
	if len([]rune(line)) > 700 {
		t.Fatalf("memory prompt line too large: %d runes\n%s", len([]rune(line)), line)
	}
	if !strings.Contains(line, "... [truncated]") {
		t.Fatalf("memory prompt line was not visibly truncated:\n%s", line)
	}
	if strings.Contains(line, "seven") || strings.Contains(line, "eight") {
		t.Fatalf("memory prompt line did not cap tags:\n%s", line)
	}
}

func TestMovedRoutingGuidanceLivesInToolDescriptions(t *testing.T) {
	checks := []struct {
		name string
		desc string
		want []string
	}{
		{"terminal_send", (&terminalSendTool{}).Description(), []string{"interactive", "msfconsole", "terminal_read", "http_probe", "one-shot"}},
		{"start_listener", (&startListenerTool{}).Description(), []string{"BEFORE firing any reverse-shell payload", "http_probe, shell, or the webshell path", "terminal_read", "terminal_send"}},
		{"http_probe", (&httpProbeTool{}).Description(), []string{"repeated curl", "raw artifact path", "Inspect the saved artifact"}},
		{"shell", (&tools.Shell{}).Description(), []string{"short deterministic one-shot", "http_probe", "terminal_send", "120-300s", "background=true"}},
		{"read", (&tools.Read{}).Description(), []string{"mode", "outline", "chunk"}},
		{"glob", (&tools.Glob{}).Description(), []string{"Prefer this over shell", "repository discovery"}},
		{"grep", (&tools.Grep{}).Description(), []string{"Prefer this over shell grep", "bounded file:line evidence"}},
		{"web_search", (&tools.WebSearch{}).Description(), []string{"current year/date", "official/vendor/GitHub", "fetch_url"}},
	}
	for _, check := range checks {
		for _, want := range check.want {
			if !strings.Contains(check.desc, want) {
				t.Fatalf("%s description missing %q:\n%s", check.name, want, check.desc)
			}
		}
	}
}

func TestLooksLikeInteractiveListenerCommand(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{`ncat.exe -lvp 4444`, true},
		{`powershell.exe -NoProfile -Command "ncat.exe -lvp 4444"`, true},
		{`rlwrap nc -lvnp 4444`, true},
		{`socat file:` + "`tty`,raw,echo=0 tcp-listen:4444", true},
		{`nc -zv 10.10.10.10 22`, false},
		{`nmap -sV 10.10.10.10`, false},
	}
	for _, tc := range cases {
		if got := looksLikeInteractiveListenerCommand(tc.command); got != tc.want {
			t.Fatalf("looksLikeInteractiveListenerCommand(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

func TestMemoryScoringPrefersTaggedImportantPinnedEntries(t *testing.T) {
	terms := keywordSet("use lm studio local provider")
	plain := MemoryEntry{
		Title:      "Provider",
		Content:    "LM Studio is available.",
		Kind:       "note",
		Importance: 1,
		UpdatedAt:  "2020-01-01T00:00:00Z",
	}
	rich := MemoryEntry{
		Title:      "Local provider preference",
		Content:    "Use the LAN endpoint for LM Studio.",
		Tags:       []string{"lm", "studio", "local", "provider"},
		Kind:       "preference",
		Importance: 5,
		Pinned:     true,
		UpdatedAt:  timeNowRFC3339(),
	}

	if scoreMemory(rich, terms, "provider") <= scoreMemory(plain, terms, "provider") {
		t.Fatalf("rich memory should outrank plain memory")
	}
}

func TestMemoryNormalisation(t *testing.T) {
	tags := normaliseTags([]string{" Local ", "#local", "Qwen", ""})
	if strings.Join(tags, ",") != "local,qwen" {
		t.Fatalf("tags were not normalized/deduped: %#v", tags)
	}
	if normaliseMemoryKind("bad-kind") != "note" {
		t.Fatalf("unknown memory kind should fall back to note")
	}
	if clampInt(9, 1, 5, 3) != 5 || clampInt(0, 1, 5, 3) != 3 {
		t.Fatalf("clampInt returned unexpected values")
	}
}

func TestParseWSLDistrosCleansWindowsOutput(t *testing.T) {
	raw := "\x00 \x00 \x00N\x00A\x00M\x00E\x00 \x00 \x00 \x00 \x00 \x00S\x00T\x00A\x00T\x00E\x00\r\x00\n\x00*\x00 \x00k\x00a\x00l\x00i\x00-\x00l\x00i\x00n\x00u\x00x\x00 \x00 \x00R\x00u\x00n\x00n\x00i\x00n\x00g\x00\r\x00\n\x00 \x00 \x00U\x00b\x00u\x00n\x00t\x00u\x00-\x002\x004\x00.\x000\x004\x00 \x00S\x00t\x00o\x00p\x00p\x00e\x00d\x00\r\x00\n\x00"
	got := parseWSLDistros(raw)
	want := []string{"kali-linux", "Ubuntu-24.04"}
	if len(got) != len(want) {
		t.Fatalf("parseWSLDistros = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseWSLDistros = %#v, want %#v", got, want)
		}
	}
}

func timeNowRFC3339() string {
	return "2099-01-01T00:00:00Z"
}

func withFastModelLoadRetry(t *testing.T) {
	t.Helper()
	oldAttempts := modelLoadAttempts
	oldTimeout := modelLoadAttemptTimeout
	oldDelays := modelLoadRetryDelays
	modelLoadAttempts = 3
	modelLoadAttemptTimeout = 100 * time.Millisecond
	modelLoadRetryDelays = []time.Duration{0, 0}
	t.Cleanup(func() {
		modelLoadAttempts = oldAttempts
		modelLoadAttemptTimeout = oldTimeout
		modelLoadRetryDelays = oldDelays
	})
}

type countingLoader struct {
	loads                 int
	actualContext         int
	actualAfterLoad       int
	failuresBeforeSuccess int
	loadErr               error
}

type summaryClient struct{}

type namedDestructiveTool struct {
	name string
}

func (t *namedDestructiveTool) Name() string                                         { return t.name }
func (t *namedDestructiveTool) Description() string                                  { return "" }
func (t *namedDestructiveTool) Schema() json.RawMessage                              { return nil }
func (t *namedDestructiveTool) Run(context.Context, json.RawMessage) (string, error) { return "", nil }
func (t *namedDestructiveTool) Destructive() bool                                    { return true }

func (c *countingLoader) Chat(context.Context, llm.Request) (<-chan llm.Delta, error) {
	return nil, nil
}

func (c *countingLoader) Models(context.Context) ([]string, error) {
	return nil, nil
}

func (c *countingLoader) Ping(context.Context) error {
	return nil
}

func (c *countingLoader) Name() string {
	return "test"
}

func (c *countingLoader) LoadModel(context.Context) error {
	c.loads++
	if c.failuresBeforeSuccess > 0 {
		c.failuresBeforeSuccess--
		if c.loadErr != nil {
			return c.loadErr
		}
		return errors.New("load failed")
	}
	if c.actualAfterLoad > 0 {
		c.actualContext = c.actualAfterLoad
	} else if c.actualContext > 0 {
		c.actualContext = 32768
	}
	return nil
}

func (c *countingLoader) ActualContextLength(context.Context) int {
	return c.actualContext
}

func (c *summaryClient) Chat(context.Context, llm.Request) (<-chan llm.Delta, error) {
	ch := make(chan llm.Delta, 1)
	ch <- llm.Delta{Content: "summary ok"}
	close(ch)
	return ch, nil
}

func (c *summaryClient) Models(context.Context) ([]string, error) { return nil, nil }
func (c *summaryClient) Ping(context.Context) error               { return nil }
func (c *summaryClient) Name() string                             { return "summary" }
