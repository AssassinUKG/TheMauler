package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/audio"
	"mauler/internal/channelbus"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/store"
	"mauler/internal/telegram"
)

type fakeTelegramAPI struct {
	me          telegram.User
	sent        []string
	edits       []string
	voices      [][]byte
	deleted     []string
	files       map[string]telegram.File
	downloads   map[string][]byte
	deleteDrops []bool
}

func (f *fakeTelegramAPI) GetMe(ctx context.Context) (telegram.User, error) {
	return f.me, nil
}

func (f *fakeTelegramAPI) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]telegram.Update, error) {
	return nil, nil
}

func (f *fakeTelegramAPI) DeleteWebhook(ctx context.Context, dropPendingUpdates bool) error {
	f.deleteDrops = append(f.deleteDrops, dropPendingUpdates)
	return nil
}

func (f *fakeTelegramAPI) SendMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	f.sent = append(f.sent, text)
	return int64(len(f.sent)), nil
}

func (f *fakeTelegramAPI) EditMessage(ctx context.Context, chatID, messageID int64, text string) error {
	f.edits = append(f.edits, fmt.Sprintf("%d:%d:%s", chatID, messageID, text))
	return nil
}

func (f *fakeTelegramAPI) SendVoice(ctx context.Context, chatID int64, data []byte, filename, mimeType string) error {
	f.voices = append(f.voices, append([]byte(nil), data...))
	return nil
}

func (f *fakeTelegramAPI) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	f.deleted = append(f.deleted, fmt.Sprintf("%d:%d", chatID, messageID))
	return nil
}

func (f *fakeTelegramAPI) GetFile(ctx context.Context, fileID string) (telegram.File, error) {
	if f.files != nil {
		if file, ok := f.files[fileID]; ok {
			return file, nil
		}
	}
	return telegram.File{FileID: fileID, FilePath: fileID + ".ogg"}, nil
}

func (f *fakeTelegramAPI) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	if f.downloads != nil {
		if data, ok := f.downloads[filePath]; ok {
			return data, nil
		}
	}
	return []byte("voice-bytes"), nil
}

func newTelegramRuntimeForTest(t *testing.T, cfg settings.TelegramConfig, fake *fakeTelegramAPI) *telegramRuntime {
	t.Helper()
	t.Setenv("MAULER_CONFIG_DIR", t.TempDir())
	defaults := settings.DefaultSettings()
	app := &App{
		suppressEvents: true,
		cfg:            &defaults,
		channelQueue:   channelbus.NewQueue(),
	}
	return &telegramRuntime{
		app:            app,
		client:         fake,
		cfg:            cfg,
		lastProgress:   map[int64]time.Time{},
		progressIDs:    map[int64]int64{},
		progressChats:  map[int64]bool{},
		progressTasks:  map[int64]string{},
		progressStarts: map[int64]time.Time{},
		progressStates: map[int64]string{},
		progressDetail: map[int64]string{},
	}
}

func TestTelegramRuntimeStatusCommandRoutesThroughChannelBus(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{BotUsername: "TheMaulerBot", RequireMention: true}, fake)

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Text:      "/status",
	})

	if len(fake.sent) != 1 {
		t.Fatalf("expected one Telegram reply, got %d", len(fake.sent))
	}
	if !strings.Contains(fake.sent[0], "Status") || !strings.Contains(fake.sent[0], "queued work") {
		t.Fatalf("status reply missing expected content: %q", fake.sent[0])
	}
}

func TestTelegramRuntimeAllowListBlocksUnknownSender(t *testing.T) {
	fake := &fakeTelegramAPI{}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{AllowFrom: []string{"12345"}}, fake)

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 999, Username: "intruder"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Text:      "/status",
	})

	if len(fake.sent) != 0 {
		t.Fatalf("allow-list blocked sender should not receive replies: %#v", fake.sent)
	}
}

func TestTelegramRuntimeRequiresMentionOutsidePrivateChats(t *testing.T) {
	fake := &fakeTelegramAPI{}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{BotUsername: "TheMaulerBot", RequireMention: true}, fake)

	group := telegram.Chat{ID: -100, Type: "group"}
	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      group,
		Text:      "status please",
	})
	if len(fake.sent) != 0 {
		t.Fatalf("unmentioned group message should be ignored: %#v", fake.sent)
	}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 2,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      group,
		Text:      "@TheMaulerBot /status",
	})
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0], "Status") {
		t.Fatalf("mentioned group status should be handled, got: %#v", fake.sent)
	}
}

func TestTelegramRuntimeVoiceMessageDownloadsAndRoutes(t *testing.T) {
	client := &sideChatRecordingClient{reply: "llm side chat reply"}
	oldBuilder := buildClientForAgent
	oldTTS := synthesizeTelegramVoice
	oldLocalWhisper := runLocalWhisperTranscription
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	synthesizeTelegramVoice = func(ctx context.Context, text string, opts audio.TTSOptions) ([]byte, error) {
		return []byte("voice"), nil
	}
	runLocalWhisperTranscription = func(ctx context.Context, path string) string {
		return "spoken hello from telegram"
	}
	t.Cleanup(func() {
		buildClientForAgent = oldBuilder
		synthesizeTelegramVoice = oldTTS
		runLocalWhisperTranscription = oldLocalWhisper
	})
	stt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected transcription method: %s", r.Method)
		}
		if strings.TrimSpace(r.Header.Get("Content-Type")) == "" {
			t.Fatal("missing transcription content-type")
		}
		_, _ = w.Write([]byte(`{"text":"spoken hello from telegram"}`))
	}))
	t.Cleanup(stt.Close)
	fake := &fakeTelegramAPI{
		files:     map[string]telegram.File{"voice-1": {FileID: "voice-1", FilePath: "voice/file.ogg"}},
		downloads: map[string][]byte{"voice/file.ogg": []byte("ogg-data")},
	}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{TranscriptionMode: "local", TranscriptionURL: stt.URL}, fake)
	rt.app.profiles = &settings.ProfilesFile{Profiles: map[string]settings.Profile{"qwen3.6-nothink": {ModelID: "fake", Backend: "fake"}}}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Voice:     &telegram.Voice{FileID: "voice-1", MimeType: "audio/ogg"},
	})

	if len(fake.sent) != 0 {
		t.Fatalf("voice chat should send voice-only when synthesis succeeds, got text replies: %#v", fake.sent)
	}
	if !strings.Contains(fmt.Sprintf("%#v", client.lastReq.Messages), "spoken hello from telegram") {
		t.Fatalf("transcript was not sent to side-chat model: %#v", client.lastReq.Messages)
	}
	if len(fake.voices) != 1 || string(fake.voices[0]) != "voice" {
		t.Fatalf("expected voice reply for voice message, got %#v", fake.voices)
	}
}

func TestTelegramRuntimeVoiceMessageWithoutTranscriptionURLRepliesWithNotice(t *testing.T) {
	calledModel := false
	oldBuilder := buildClientForAgent
	oldLocalWhisper := runLocalWhisperTranscription
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		calledModel = true
		return sideChatEchoClient{}, nil
	}
	runLocalWhisperTranscription = func(ctx context.Context, path string) string { return "" }
	t.Cleanup(func() {
		buildClientForAgent = oldBuilder
		runLocalWhisperTranscription = oldLocalWhisper
	})
	fake := &fakeTelegramAPI{
		files:     map[string]telegram.File{"voice-1": {FileID: "voice-1", FilePath: "voice/file.ogg"}},
		downloads: map[string][]byte{"voice/file.ogg": []byte("ogg-data")},
	}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{TranscriptionMode: "local"}, fake)
	rt.app.profiles = &settings.ProfilesFile{Profiles: map[string]settings.Profile{"qwen3.6-nothink": {ModelID: "fake", Backend: "fake"}}}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Voice:     &telegram.Voice{FileID: "voice-1", MimeType: "audio/ogg"},
	})

	if calledModel {
		t.Fatal("voice transcription notice should not be routed through the model")
	}
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0], "local Whisper transcription failed") {
		t.Fatalf("expected direct transcription notice, got %#v", fake.sent)
	}
}

func TestTelegramRuntimePhotoMessageDownloadsAndRoutes(t *testing.T) {
	fake := &fakeTelegramAPI{
		files:     map[string]telegram.File{"photo-large": {FileID: "photo-large", FilePath: "photos/photo.jpg"}},
		downloads: map[string][]byte{"photos/photo.jpg": []byte("jpeg-data")},
	}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{BotUsername: "TheMaulerBot", RequireMention: true}, fake)

	text, attachments := rt.messageTextAndAttachments(context.Background(), telegram.Message{
		MessageID: 7,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Caption:   "@TheMaulerBot test image",
		Photo: []telegram.PhotoSize{
			{FileID: "photo-small", Width: 90, Height: 90},
			{FileID: "photo-large", Width: 1280, Height: 720},
		},
	})

	if !strings.Contains(text, "test image") {
		t.Fatalf("caption was not preserved: %q", text)
	}
	if len(attachments) != 1 {
		t.Fatalf("expected one image attachment, got %#v", attachments)
	}
	att := attachments[0]
	if att.Kind != "image" || att.FileID != "photo-large" || att.ContentType != "image/jpeg" || att.Path == "" {
		t.Fatalf("bad image attachment: %#v", att)
	}
	data, err := os.ReadFile(filepath.FromSlash(att.Path))
	if err != nil || string(data) != "jpeg-data" {
		t.Fatalf("saved image mismatch data=%q err=%v", string(data), err)
	}
}

func TestTelegramRuntimeVoiceMessageUsesLocalWhisperFallback(t *testing.T) {
	client := &sideChatRecordingClient{reply: "heard local voice"}
	oldBuilder := buildClientForAgent
	oldLocalWhisper := runLocalWhisperTranscription
	oldTTS := synthesizeTelegramVoice
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	runLocalWhisperTranscription = func(ctx context.Context, path string) string {
		if strings.TrimSpace(path) == "" {
			t.Fatal("local whisper received empty audio path")
		}
		return "local whisper transcript"
	}
	synthesizeTelegramVoice = func(ctx context.Context, text string, opts audio.TTSOptions) ([]byte, error) {
		if !strings.Contains(text, "heard local voice") {
			t.Fatalf("voice reply synthesized wrong text: %q", text)
		}
		return []byte("ogg-voice"), nil
	}
	t.Cleanup(func() {
		buildClientForAgent = oldBuilder
		runLocalWhisperTranscription = oldLocalWhisper
		synthesizeTelegramVoice = oldTTS
	})
	fake := &fakeTelegramAPI{
		files:     map[string]telegram.File{"voice-1": {FileID: "voice-1", FilePath: "voice/file.ogg"}},
		downloads: map[string][]byte{"voice/file.ogg": []byte("ogg-data")},
	}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{TranscriptionMode: "local"}, fake)
	rt.app.profiles = &settings.ProfilesFile{Profiles: map[string]settings.Profile{"qwen3.6-nothink": {ModelID: "fake", Backend: "fake"}}}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Voice:     &telegram.Voice{FileID: "voice-1", MimeType: "audio/ogg"},
	})

	if len(fake.sent) != 0 {
		t.Fatalf("voice chat should send voice-only when synthesis succeeds, got text replies: %#v", fake.sent)
	}
	if !strings.Contains(fmt.Sprintf("%#v", client.lastReq.Messages), "local whisper transcript") {
		t.Fatalf("local transcript was not sent to side-chat model: %#v", client.lastReq.Messages)
	}
	if len(fake.voices) != 1 || string(fake.voices[0]) != "ogg-voice" {
		t.Fatalf("expected one voice reply, got %#v", fake.voices)
	}
}

func TestTelegramRuntimeVoiceReplyFailureSendsSetupNotice(t *testing.T) {
	client := &sideChatRecordingClient{reply: "heard you"}
	oldBuilder := buildClientForAgent
	oldLocalWhisper := runLocalWhisperTranscription
	oldTTS := synthesizeTelegramVoice
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	runLocalWhisperTranscription = func(ctx context.Context, path string) string {
		return "voice transcript"
	}
	synthesizeTelegramVoice = func(ctx context.Context, text string, opts audio.TTSOptions) ([]byte, error) {
		return nil, fmt.Errorf("kokoro missing")
	}
	t.Cleanup(func() {
		buildClientForAgent = oldBuilder
		runLocalWhisperTranscription = oldLocalWhisper
		synthesizeTelegramVoice = oldTTS
	})
	fake := &fakeTelegramAPI{
		files:     map[string]telegram.File{"voice-1": {FileID: "voice-1", FilePath: "voice/file.ogg"}},
		downloads: map[string][]byte{"voice/file.ogg": []byte("ogg-data")},
	}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{TranscriptionMode: "local"}, fake)
	rt.app.profiles = &settings.ProfilesFile{Profiles: map[string]settings.Profile{"qwen3.6-nothink": {ModelID: "fake", Backend: "fake"}}}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Voice:     &telegram.Voice{FileID: "voice-1", MimeType: "audio/ogg"},
	})

	if len(fake.sent) != 1 {
		t.Fatalf("expected voice setup notice, got %#v", fake.sent)
	}
	if !strings.Contains(fake.sent[0], "Voice reply failed") || !strings.Contains(fake.sent[0], "setup.ps1") {
		t.Fatalf("expected voice failure setup notice, got %#v", fake.sent)
	}
	if len(fake.voices) != 0 {
		t.Fatalf("voice send should not be attempted after synthesis failure, got %#v", fake.voices)
	}
}

func TestTelegramRuntimeVoiceModeSticksUntilTypedText(t *testing.T) {
	client := &sideChatRecordingClient{reply: "reply text"}
	oldBuilder := buildClientForAgent
	oldLocalWhisper := runLocalWhisperTranscription
	oldTTS := synthesizeTelegramVoice
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return client, nil
	}
	runLocalWhisperTranscription = func(ctx context.Context, path string) string {
		return "voice transcript"
	}
	synthesizeTelegramVoice = func(ctx context.Context, text string, opts audio.TTSOptions) ([]byte, error) {
		return []byte("ogg-voice"), nil
	}
	t.Cleanup(func() {
		buildClientForAgent = oldBuilder
		runLocalWhisperTranscription = oldLocalWhisper
		synthesizeTelegramVoice = oldTTS
	})
	fake := &fakeTelegramAPI{
		files:     map[string]telegram.File{"voice-1": {FileID: "voice-1", FilePath: "voice/file.ogg"}},
		downloads: map[string][]byte{"voice/file.ogg": []byte("ogg-data")},
	}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{TranscriptionMode: "local"}, fake)
	rt.app.profiles = &settings.ProfilesFile{Profiles: map[string]settings.Profile{"qwen3.6-nothink": {ModelID: "fake", Backend: "fake"}}}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Voice:     &telegram.Voice{FileID: "voice-1", MimeType: "audio/ogg"},
	})
	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 2,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Voice:     &telegram.Voice{FileID: "voice-1", MimeType: "audio/ogg"},
	})
	if len(fake.sent) != 0 || len(fake.voices) != 2 {
		t.Fatalf("voice session should produce two voice-only replies, sent=%#v voices=%#v", fake.sent, fake.voices)
	}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 3,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Text:      "back to typing",
	})
	if len(fake.voices) != 2 {
		t.Fatalf("typed text should exit voice mode, got extra voices=%#v", fake.voices)
	}
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0], "reply text") {
		t.Fatalf("typed text should receive text reply, got %#v", fake.sent)
	}
}

func TestTelegramOffsetPersistsPerBotToken(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := settings.TelegramConfig{Token: "123:abc"}
	app := &App{db: db}

	if got := app.loadTelegramOffset(cfg); got != 0 {
		t.Fatalf("new offset = %d, want 0", got)
	}
	app.saveTelegramOffset(cfg, 42)
	if got := app.loadTelegramOffset(cfg); got != 42 {
		t.Fatalf("saved offset = %d, want 42", got)
	}
	if got := app.loadTelegramOffset(settings.TelegramConfig{Token: "other"}); got != 0 {
		t.Fatalf("offset should be token-scoped, got %d", got)
	}
}

func TestTelegramRuntimeSkipsDuplicateMessageInSession(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{BotUsername: "TheMaulerBot", RequireMention: true}, fake)

	msg := telegram.Message{
		MessageID: 10,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Text:      "/status",
	}
	rt.handleMessage(context.Background(), msg)
	rt.handleMessage(context.Background(), msg)

	if len(fake.sent) != 1 {
		t.Fatalf("duplicate Telegram message should only be answered once, got %d replies: %#v", len(fake.sent), fake.sent)
	}
}

func TestTelegramRunProgressMessageFormatsModelLoad(t *testing.T) {
	detail := strings.Join([]string{
		"llamacpp",
		"http://127.0.0.1:8800/v1",
		"Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_M.gguf",
		"45000",
		"",
	}, "\x00")

	got := formatTelegramRunProgressMessage("model_loading", detail)
	for _, want := range []string{
		"\u23f3 Mauler is working",
		"**Stage:** Loading the model",
		"Backend: llamacpp",
		"Model: Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_M.gguf",
		"Context: 45000 tokens",
		"Endpoint: http://127.0.0.1:8800/v1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("progress message missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x00") || strings.Contains(got, "Run model_loading") {
		t.Fatalf("progress message leaked raw internals: %q", got)
	}
}

func TestTelegramRunProgressMessageFormatsLoopGuard(t *testing.T) {
	got := formatTelegramRunProgressMessage("blocked", "Loop circuit-breaker paused the run after a corrective prompt because loop-health stayed critical: stability_score=14 repeated_tool_inputs=1 repeated_identical_outcomes=0 repeated_skips=0 tool_errors=6 tool_cycle_detected=false tool_cycle_period=0.")
	for _, want := range []string{
		"\u26d4 Mauler needs attention",
		"**Stage:** Blocked by the loop guard",
		"stability score: 14",
		"repeated tool inputs: 1",
		"tool errors: 6",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("blocked message missing %q:\n%s", want, got)
		}
	}
}

func TestTelegramRunCompletionSendsWhoamiResult(t *testing.T) {
	run := TaskRun{
		Prompt: "whoami",
		Tools: []TaskToolEvent{{
			Name:   "shell",
			Status: "done",
			Result: "root",
		}},
	}
	detail := telegramRunCompletionDetail(run, "Command completed successfully.")
	got := formatTelegramRunProgressMessageForTask("done", detail, run.Prompt, 2*time.Second)
	for _, want := range []string{
		"\u2705 Mauler finished",
		"**Task**\nwhoami",
		"**Result**\nroot",
		"**Finished in:** 2s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("completion message missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Latest - Run completed successfully") {
		t.Fatalf("completion message repeated generic status instead of the result: %q", got)
	}
}

func TestTelegramRunCompletionPrefersMeaningfulFinalAnswer(t *testing.T) {
	run := TaskRun{
		Prompt: "whoami",
		Tools: []TaskToolEvent{{
			Name:   "shell",
			Status: "done",
			Result: "root",
		}},
	}
	detail := telegramRunCompletionDetail(run, "The command reports that the current user is root.")
	if detail != "The command reports that the current user is root." {
		t.Fatalf("meaningful final answer should win over raw tool fallback, got %q", detail)
	}
}

func TestTelegramRunCompletionPreservesMultilineUIAnswer(t *testing.T) {
	uiAnswer := "Current user: root\n\nDirectory contents:\n- AGENTS.md\n- docs/\n- scripts/"
	run := TaskRun{Prompt: "whoami and list the directory"}
	detail := telegramRunCompletionDetail(run, uiAnswer)
	got := formatTelegramRunProgressMessageForTask("done", detail, run.Prompt, 4*time.Second)
	if !strings.Contains(got, "**Result**\n"+uiAnswer) {
		t.Fatalf("multiline UI answer was not preserved in Telegram completion:\n%s", got)
	}
}

func TestTelegramRunProgressShowsTaskAndCurrentAction(t *testing.T) {
	got := formatTelegramRunProgressMessageForTask("testing", "shell", "whoami", 3*time.Second)
	for _, want := range []string{
		"\u23f3 Mauler is working",
		"**Task**\nwhoami",
		"**Stage:** Verifying",
		"**Current:** Running a shell command",
		"**Elapsed:** 3s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("working message missing %q:\n%s", want, got)
		}
	}
}

func TestTelegramProgressUpdateEditsExistingMessage(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: true, ProgressIntervalS: 1}, fake)
	rt.trackProgressChat(42, "test progress editing")

	rt.sendTelegramProgressUpdate(context.Background(), 42, "thinking", "first")
	rt.sendTelegramProgressUpdate(context.Background(), 42, "testing", "second")

	if len(fake.sent) != 1 {
		t.Fatalf("progress should create one status message, got sends: %#v", fake.sent)
	}
	if len(fake.edits) != 1 || !strings.Contains(fake.edits[0], "42:1:second") {
		t.Fatalf("progress should edit first status message, got edits: %#v", fake.edits)
	}
}

func TestTelegramProgressHeartbeatUpdatesDuringQuietRun(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: true, ProgressIntervalS: 1}, fake)
	rt.trackProgressChat(42, "Check the seven-day forecast for Bradley Stoke")
	rt.mu.Lock()
	rt.progressStarts[42] = time.Now().Add(-12 * time.Second)
	rt.lastProgress[42] = time.Now().Add(-2 * time.Second)
	rt.progressStates[42] = "thinking"
	rt.progressDetail[42] = "Waiting for the local model to return its next action."
	rt.mu.Unlock()

	rt.sendDueProgressHeartbeats(context.Background())

	if len(fake.sent) != 1 {
		t.Fatalf("quiet active run should receive a heartbeat card, sends=%#v", fake.sent)
	}
	for _, want := range []string{"Mauler is working", "seven-day forecast", "Thinking", "Waiting for the local model", "Elapsed"} {
		if !strings.Contains(fake.sent[0], want) {
			t.Fatalf("heartbeat missing %q: %s", want, fake.sent[0])
		}
	}

	rt.mu.Lock()
	rt.lastProgress[42] = time.Now().Add(-2 * time.Second)
	rt.progressStates[42] = "testing"
	rt.progressDetail[42] = "shell"
	rt.mu.Unlock()
	rt.sendDueProgressHeartbeats(context.Background())
	if len(fake.sent) != 1 || len(fake.edits) != 1 || !strings.Contains(fake.edits[0], "Running a shell command") {
		t.Fatalf("second heartbeat should edit the same status card: sends=%#v edits=%#v", fake.sent, fake.edits)
	}
}

func TestTelegramCompletionIsSentWhenProgressUpdatesAreDisabled(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: false}, fake)
	rt.trackProgressChat(42, "whoami")

	rt.notifyRunState(context.Background(), "done", "root")

	if len(fake.sent) != 1 {
		t.Fatalf("final result should be sent even with progress disabled, got %#v", fake.sent)
	}
	for _, want := range []string{"\u2705 Mauler finished", "**Task**\nwhoami", "**Result**\nroot"} {
		if !strings.Contains(fake.sent[0], want) {
			t.Fatalf("final result missing %q: %q", want, fake.sent[0])
		}
	}
}

func TestTelegramRecoverableBlockedStateDoesNotLoseFinalResult(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: true, ProgressIntervalS: 1}, fake)
	rt.trackProgressChat(42, "whoami")
	rt.lastProgress[42] = time.Now().Add(-2 * time.Second)

	rt.notifyRunState(context.Background(), "blocked", "Waiting for a corrective final pass.")
	rt.notifyRunState(context.Background(), "done", "root")

	if len(fake.sent) != 1 {
		t.Fatalf("recoverable blocked state should keep one editable status message, sends=%#v", fake.sent)
	}
	if len(fake.edits) != 1 || !strings.Contains(fake.edits[0], "**Result**\nroot") {
		t.Fatalf("final result was lost after recoverable blocked state, edits=%#v", fake.edits)
	}
}

func TestTelegramCompletionBridgesResultIntoSameChatFollowUp(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: false}, fake)
	rt.trackProgressChat(42, "Get the 7-day weather forecast for Bradley Stoke, Bristol")

	rt.notifyRunState(context.Background(), "done", "Monday: 18°C and dry.\nTuesday: 17°C with light rain.")

	reply, ok := rt.app.remoteRunFollowUp("telegram:42", "What's the result?")
	if !ok {
		t.Fatal("expected deterministic follow-up result")
	}
	for _, want := range []string{"Latest Mauler result", "7-day weather forecast", "Monday: 18°C", "Tuesday: 17°C"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("follow-up result missing %q: %s", want, reply)
		}
	}
}

func TestTelegramCompletionRejectsRawToolCallAsFinalAnswer(t *testing.T) {
	raw := "<tool_call><function=http_probe><parameter=url>https://example.test</parameter></function></tool_call>"
	detail := telegramRunCompletionDetail(TaskRun{Prompt: "check it"}, raw)
	if sideChatLooksLikeToolCall(detail) || strings.Contains(detail, "https://example.test") {
		t.Fatalf("raw tool request leaked into Telegram completion: %q", detail)
	}
	if !strings.Contains(detail, "rejected") {
		t.Fatalf("expected actionable protocol failure message, got %q", detail)
	}
}

func TestTelegramLongFinalReplacesLiveCardBeforeMultipartSend(t *testing.T) {
	fake := &fakeTelegramAPI{me: telegram.User{Username: "TheMaulerBot"}}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: true, ProgressIntervalS: 1}, fake)
	rt.trackProgressChat(42, "produce a detailed report")
	rt.lastProgress[42] = time.Now().Add(-2 * time.Second)
	rt.notifyRunState(context.Background(), "thinking", "drafting")
	rt.notifyRunState(context.Background(), "done", strings.Repeat("full result line\n", 400))

	if len(fake.deleted) != 1 || fake.deleted[0] != "42:1" {
		t.Fatalf("long terminal result should replace stale progress card, deleted=%#v", fake.deleted)
	}
	if len(fake.sent) != 2 {
		t.Fatalf("fake API should receive live card and complete long result, sends=%d", len(fake.sent))
	}
	if got := strings.Count(fake.sent[1], "full result line"); got != 400 {
		t.Fatalf("long final result was truncated: lines=%d", got)
	}
}
