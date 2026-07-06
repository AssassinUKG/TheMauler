package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mauler/internal/channelbus"
	"mauler/internal/llm"
	"mauler/internal/settings"
	"mauler/internal/store"
	"mauler/internal/telegram"
)

type fakeTelegramAPI struct {
	me          telegram.User
	sent        []string
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
		app:           app,
		client:        fake,
		cfg:           cfg,
		lastProgress:  map[int64]time.Time{},
		progressChats: map[int64]bool{},
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
	oldBuilder := buildClientForAgent
	buildClientForAgent = func(settings.Profile) (llm.Client, error) {
		return sideChatEchoClient{}, nil
	}
	t.Cleanup(func() { buildClientForAgent = oldBuilder })
	fake := &fakeTelegramAPI{
		files:     map[string]telegram.File{"voice-1": {FileID: "voice-1", FilePath: "voice/file.ogg"}},
		downloads: map[string][]byte{"voice/file.ogg": []byte("ogg-data")},
	}
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{}, fake)
	rt.app.profiles = &settings.ProfilesFile{Profiles: map[string]settings.Profile{"qwen3.6-nothink": {ModelID: "fake", Backend: "fake"}}}

	rt.handleMessage(context.Background(), telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: 100, Username: "rich"},
		Chat:      telegram.Chat{ID: 42, Type: "private"},
		Voice:     &telegram.Voice{FileID: "voice-1", MimeType: "audio/ogg"},
	})

	if len(fake.sent) != 1 {
		t.Fatalf("expected side-chat reply for voice message, got %d", len(fake.sent))
	}
	if !strings.Contains(fake.sent[0], "llm side chat reply") {
		t.Fatalf("voice message was not routed through LLM side chat: %q", fake.sent[0])
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
