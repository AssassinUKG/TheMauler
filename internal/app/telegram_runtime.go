package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mauler/internal/channelbus"
	"mauler/internal/ledger"
	"mauler/internal/settings"
	"mauler/internal/telegram"
)

type telegramAPI interface {
	GetMe(ctx context.Context) (telegram.User, error)
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]telegram.Update, error)
	DeleteWebhook(ctx context.Context, dropPendingUpdates bool) error
	SendMessage(ctx context.Context, chatID int64, text string) (int64, error)
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
	GetFile(ctx context.Context, fileID string) (telegram.File, error)
	DownloadFile(ctx context.Context, filePath string) ([]byte, error)
}

type telegramRuntime struct {
	app    *App
	client telegramAPI
	cfg    settings.TelegramConfig

	mu             sync.Mutex
	offset         int64
	lastProgress   map[int64]time.Time
	progressChats  map[int64]bool
	progressChatID int64
	lastPoll       time.Time
	lastUpdate     time.Time
	lastMessage    string
	lastReject     string
	seenMessages   map[string]bool
	pollErrors     int
	updateCount    int
}

func (a *App) restartTelegramRuntime(cfg settings.TelegramConfig) {
	a.telegramMu.Lock()
	if a.telegramCancel != nil {
		a.telegramCancel()
	}
	a.telegramCancel = nil
	a.telegramRunning = false
	a.telegramService = nil
	a.telegramStatus = "disabled"
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
		a.telegramMu.Unlock()
		return
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	runtime := &telegramRuntime{
		app:           a,
		client:        telegram.NewClient(cfg.Token),
		cfg:           cfg,
		lastProgress:  map[int64]time.Time{},
		progressChats: map[int64]bool{},
		seenMessages:  map[string]bool{},
		offset:        a.loadTelegramOffset(cfg),
	}
	a.telegramCancel = cancel
	a.telegramRunning = true
	a.telegramStatus = "starting"
	a.telegramService = runtime
	a.telegramMu.Unlock()
	go runtime.run(runCtx)
}

func (a *App) stopTelegramRuntimeLocked() {
	a.telegramMu.Lock()
	defer a.telegramMu.Unlock()
	if a.telegramCancel != nil {
		a.telegramCancel()
	}
	a.telegramCancel = nil
	a.telegramRunning = false
	a.telegramService = nil
	if a.telegramStatus != "disabled" {
		a.telegramStatus = "stopped"
	}
}

func (a *App) telegramRuntimeSnapshot() (running bool, status string) {
	a.telegramMu.Lock()
	defer a.telegramMu.Unlock()
	return a.telegramRunning, a.telegramStatus
}

func (a *App) telegramRuntimeDiagnostics() map[string]string {
	out := map[string]string{}
	a.telegramMu.Lock()
	svc := a.telegramService
	out["telegram_running"] = fmt.Sprintf("%v", a.telegramRunning)
	out["telegram_status"] = a.telegramStatus
	a.telegramMu.Unlock()
	if svc == nil {
		return out
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	out["telegram_offset"] = fmt.Sprintf("%d", svc.offset)
	out["telegram_updates_seen"] = fmt.Sprintf("%d", svc.updateCount)
	out["telegram_poll_errors"] = fmt.Sprintf("%d", svc.pollErrors)
	out["telegram_bot"] = firstNonEmpty(svc.cfg.BotUsername, "-")
	out["telegram_allow_from"] = strings.Join(svc.cfg.AllowFrom, ", ")
	if !svc.lastPoll.IsZero() {
		out["telegram_last_poll"] = svc.lastPoll.Format("15:04:05")
	}
	if !svc.lastUpdate.IsZero() {
		out["telegram_last_update"] = svc.lastUpdate.Format("15:04:05")
	}
	if svc.lastMessage != "" {
		out["telegram_last_message"] = svc.lastMessage
	}
	if svc.lastReject != "" {
		out["telegram_last_reject"] = svc.lastReject
	}
	return out
}

func (a *App) telegramNotifyRunState(state, detail string) {
	a.telegramMu.Lock()
	svc := a.telegramService
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	svc.notifyRunState(context.Background(), state, detail)
}

func (a *App) sendQueuedChannelReply(env channelbus.Envelope, status, text string) {
	if strings.TrimSpace(text) == "" || env.Source != "telegram" {
		return
	}
	chatIDText := ""
	if env.Metadata != nil {
		chatIDText = strings.TrimSpace(env.Metadata["chat_id"])
	}
	if chatIDText == "" {
		return
	}
	chatID, err := strconv.ParseInt(chatIDText, 10, 64)
	if err != nil {
		return
	}
	a.telegramMu.Lock()
	svc := a.telegramService
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = svc.client.SendMessage(ctx, chatID, text)
	detail := ""
	sendStatus := "sent"
	if err != nil {
		sendStatus = "error"
		detail = err.Error()
	}
	if a.ledger != nil {
		_, _ = a.ledger.Record(ledger.Event{
			Kind:    "telegram_send",
			Source:  "telegram",
			Status:  sendStatus,
			Message: truncateRunes(text, 240),
			Detail:  truncateRunes(detail, 1000),
			Metadata: map[string]string{
				"chat_id":         chatIDText,
				"response_status": status,
				"queued_reply":    "true",
			},
		})
	}
}

func (a *App) SendTelegramMessage(chatIDText string, text string) (string, error) {
	chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDText), 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid Telegram chat id %q", chatIDText)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("message is empty")
	}
	a.telegramMu.Lock()
	svc := a.telegramService
	a.telegramMu.Unlock()
	if svc == nil {
		return "", fmt.Errorf("Telegram runtime is not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	messageID, err := svc.client.SendMessage(ctx, chatID, text)
	status := "sent"
	detail := ""
	if err != nil {
		status = "error"
		detail = err.Error()
	}
	if a.ledger != nil {
		_, _ = a.ledger.Record(ledger.Event{
			Kind:    "telegram_send",
			Source:  "telegram",
			Status:  status,
			Message: truncateRunes(text, 240),
			Detail:  truncateRunes(detail, 1000),
			Metadata: map[string]string{
				"chat_id":             strconv.FormatInt(chatID, 10),
				"telegram_message_id": strconv.FormatInt(messageID, 10),
				"manual_send":         "true",
			},
		})
	}
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(messageID, 10), nil
}

func (a *App) DeleteTelegramMessage(chatIDText string, messageIDText string) error {
	chatID, err := strconv.ParseInt(strings.TrimSpace(chatIDText), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Telegram chat id %q", chatIDText)
	}
	messageID, err := strconv.ParseInt(strings.TrimSpace(messageIDText), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Telegram message id %q", messageIDText)
	}
	a.telegramMu.Lock()
	svc := a.telegramService
	a.telegramMu.Unlock()
	if svc == nil {
		return fmt.Errorf("Telegram runtime is not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = svc.client.DeleteMessage(ctx, chatID, messageID)
	status := "deleted"
	detail := ""
	if err != nil {
		status = "error"
		detail = err.Error()
	}
	if a.ledger != nil {
		_, _ = a.ledger.Record(ledger.Event{
			Kind:   "telegram_delete",
			Source: "telegram",
			Status: status,
			Detail: truncateRunes(detail, 1000),
			Metadata: map[string]string{
				"chat_id":             strconv.FormatInt(chatID, 10),
				"telegram_message_id": strconv.FormatInt(messageID, 10),
			},
		})
	}
	return err
}

func (r *telegramRuntime) run(ctx context.Context) {
	dropPending := r.currentOffset() == 0
	if err := r.client.DeleteWebhook(ctx, dropPending); err != nil {
		r.setStatus("webhook cleanup failed: " + err.Error())
	}
	me, err := r.client.GetMe(ctx)
	if err != nil {
		r.setStatus("error: " + err.Error())
		return
	}
	if strings.TrimSpace(r.cfg.BotUsername) == "" {
		r.cfg.BotUsername = me.Username
	}
	r.setStatus("connected @" + firstNonEmpty(me.Username, r.cfg.BotUsername))
	for {
		select {
		case <-ctx.Done():
			r.setStatus("stopped")
			return
		default:
		}
		updates, err := r.client.GetUpdates(ctx, r.currentOffset(), 25)
		r.recordPoll()
		if err != nil {
			r.recordPollError()
			r.setStatus("poll error: " + err.Error())
			if !sleepTelegram(ctx, 3*time.Second) {
				return
			}
			continue
		}
		r.setStatus("connected @" + firstNonEmpty(me.Username, r.cfg.BotUsername))
		for _, update := range updates {
			r.recordUpdate(update)
			r.advanceOffset(update.UpdateID + 1)
			if update.Message == nil {
				continue
			}
			r.handleMessage(ctx, *update.Message)
		}
	}
}

func (r *telegramRuntime) handleMessage(ctx context.Context, msg telegram.Message) {
	if r.alreadySawMessage(msg) {
		r.recordReject("duplicate message skipped: " + describeTelegramMessage(msg))
		return
	}
	r.recordTelegramRuntimeEvent("telegram_message_in", "received", msg, "", "")
	if !r.allowed(msg) {
		r.recordReject("blocked by allow-list: " + describeTelegramMessage(msg))
		return
	}
	text, attachments := r.messageTextAndAttachments(ctx, msg)
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		r.recordReject("empty message: " + describeTelegramMessage(msg))
		return
	}
	if !r.shouldHandle(msg, text) {
		r.recordReject("ignored missing mention: " + describeTelegramMessage(msg))
		return
	}
	text = r.stripMention(text)
	r.recordMessage(describeTelegramMessage(msg))
	env := channelbus.Envelope{
		Source:      "telegram",
		SessionID:   fmt.Sprintf("telegram:%d", msg.Chat.ID),
		UserID:      telegramUserID(msg),
		Username:    telegramUsername(msg),
		Text:        text,
		Attachments: attachments,
		Metadata: map[string]string{
			"chat_id":    strconv.FormatInt(msg.Chat.ID, 10),
			"message_id": strconv.FormatInt(msg.MessageID, 10),
			"chat_type":  msg.Chat.Type,
		},
	}
	resp, err := r.app.DispatchChannelMessage(env)
	if err != nil {
		r.sendTelegramReply(ctx, msg, "Telegram route failed: "+err.Error(), "route_error")
		return
	}
	if resp.RunStarted && r.cfg.SendProgress {
		r.trackProgressChat(msg.Chat.ID)
	}
	reply := resp.Message
	if resp.Queued && resp.QueueID != "" {
		reply += "\n\nqueue: " + resp.QueueID
	}
	if strings.TrimSpace(reply) != "" {
		r.sendTelegramReply(ctx, msg, reply, resp.Status)
	}
}

func (r *telegramRuntime) sendTelegramReply(ctx context.Context, msg telegram.Message, text string, status string) {
	start := time.Now()
	messageID, err := r.client.SendMessage(ctx, msg.Chat.ID, text)
	duration := time.Since(start)
	if err != nil {
		r.recordTelegramSendEvent(msg, status, "error", duration, 0, text, err.Error())
		return
	}
	r.recordTelegramSendEvent(msg, status, "sent", duration, messageID, text, "")
}

func (r *telegramRuntime) messageTextAndAttachments(ctx context.Context, msg telegram.Message) (string, []channelbus.Attachment) {
	text := strings.TrimSpace(msg.Text)
	var attachments []channelbus.Attachment
	if msg.Voice != nil {
		path, transcript := r.downloadAndTranscribe(ctx, msg.Voice.FileID, "voice.ogg", msg.Voice.MimeType)
		attachments = append(attachments, channelbus.Attachment{Kind: "voice", FileID: msg.Voice.FileID, ContentType: msg.Voice.MimeType, Path: path, Text: transcript})
		if text == "" {
			text = transcript
		}
	}
	if msg.Audio != nil {
		name := firstNonEmpty(msg.Audio.FileName, "audio.ogg")
		path, transcript := r.downloadAndTranscribe(ctx, msg.Audio.FileID, name, msg.Audio.MimeType)
		attachments = append(attachments, channelbus.Attachment{Kind: "audio", FileID: msg.Audio.FileID, FileName: name, ContentType: msg.Audio.MimeType, Path: path, Text: transcript})
		if text == "" {
			text = transcript
		}
	}
	return text, attachments
}

func (r *telegramRuntime) downloadAndTranscribe(ctx context.Context, fileID, fallbackName, mimeType string) (string, string) {
	file, err := r.client.GetFile(ctx, fileID)
	if err != nil {
		return "", "Voice received, but Telegram file lookup failed: " + err.Error()
	}
	data, err := r.client.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return "", "Voice received, but Telegram download failed: " + err.Error()
	}
	path := r.saveTelegramAudio(fileID, fallbackName, data)
	transcript := r.transcribeAudio(ctx, data, filepath.Base(path), mimeType)
	if strings.TrimSpace(transcript) == "" {
		transcript = "Voice message saved for review: " + path
	}
	return path, transcript
}

func (r *telegramRuntime) saveTelegramAudio(fileID, fallbackName string, data []byte) string {
	dir, err := settings.ConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	audioDir := filepath.Join(dir, "telegram", "audio")
	_ = os.MkdirAll(audioDir, 0o755)
	name := sanitizeFilename(firstNonEmpty(fileID, fallbackName))
	if filepath.Ext(name) == "" {
		name += filepath.Ext(fallbackName)
	}
	path := filepath.Join(audioDir, name)
	_ = os.WriteFile(path, data, 0o600)
	return filepath.ToSlash(path)
}

func (r *telegramRuntime) transcribeAudio(ctx context.Context, data []byte, filename, mimeType string) string {
	mode := strings.ToLower(strings.TrimSpace(r.cfg.TranscriptionMode))
	if mode == "" || mode == "disabled" {
		return ""
	}
	if strings.TrimSpace(r.cfg.TranscriptionURL) == "" {
		return ""
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return ""
	}
	if _, err := part.Write(data); err != nil {
		return ""
	}
	if mimeType != "" {
		_ = writer.WriteField("mime_type", mimeType)
	}
	_ = writer.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.TranscriptionURL, &body)
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}
	dataOut, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var parsed struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(dataOut, &parsed) == nil && strings.TrimSpace(parsed.Text) != "" {
		return strings.TrimSpace(parsed.Text)
	}
	return strings.TrimSpace(string(dataOut))
}

func (r *telegramRuntime) allowed(msg telegram.Message) bool {
	if len(r.cfg.AllowFrom) == 0 {
		return true
	}
	userID := telegramUserID(msg)
	username := strings.ToLower(strings.TrimPrefix(telegramUsername(msg), "@"))
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	for _, allow := range r.cfg.AllowFrom {
		allow = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(allow, "@")))
		if allow == "" {
			continue
		}
		if allow == userID || allow == chatID || allow == username {
			return true
		}
	}
	return false
}

func (r *telegramRuntime) shouldHandle(msg telegram.Message, text string) bool {
	if !r.cfg.RequireMention {
		return true
	}
	if msg.Chat.Type == "private" || strings.HasPrefix(strings.TrimSpace(text), "/") {
		return true
	}
	bot := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(r.cfg.BotUsername), "@"))
	if bot == "" {
		return false
	}
	return strings.Contains(strings.ToLower(text), "@"+bot)
}

func (r *telegramRuntime) stripMention(text string) string {
	bot := strings.TrimPrefix(strings.TrimSpace(r.cfg.BotUsername), "@")
	if bot == "" {
		return strings.TrimSpace(text)
	}
	replacer := strings.NewReplacer("@"+bot, "", "@"+strings.ToLower(bot), "")
	return strings.TrimSpace(replacer.Replace(text))
}

func (r *telegramRuntime) notifyRunState(ctx context.Context, state, detail string) {
	if !r.cfg.SendProgress {
		return
	}
	r.mu.Lock()
	interval := time.Duration(r.cfg.ProgressIntervalS) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	chats := make([]int64, 0, len(r.progressChats))
	for chatID := range r.progressChats {
		if time.Since(r.lastProgress[chatID]) < interval && state != "done" && state != "failed" && state != "blocked" && state != "stopped" {
			continue
		}
		r.lastProgress[chatID] = time.Now()
		chats = append(chats, chatID)
	}
	if state == "done" || state == "failed" || state == "blocked" || state == "stopped" {
		r.progressChats = map[int64]bool{}
	}
	r.mu.Unlock()
	if len(chats) == 0 {
		return
	}
	msg := "Run " + state
	if strings.TrimSpace(detail) != "" {
		msg += "\n" + truncateRunes(detail, 600)
	}
	for _, chatID := range chats {
		_, _ = r.client.SendMessage(ctx, chatID, msg)
	}
}

func (r *telegramRuntime) trackProgressChat(chatID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progressChats[chatID] = true
}

func (r *telegramRuntime) setStatus(status string) {
	r.app.telegramMu.Lock()
	r.app.telegramStatus = status
	if strings.HasPrefix(status, "stopped") || strings.HasPrefix(status, "error:") {
		r.app.telegramRunning = false
	}
	r.app.telegramMu.Unlock()
}

func (r *telegramRuntime) recordPoll() {
	r.mu.Lock()
	r.lastPoll = time.Now()
	r.mu.Unlock()
}

func (r *telegramRuntime) recordPollError() {
	r.mu.Lock()
	r.pollErrors++
	r.mu.Unlock()
	if r.app != nil && r.app.ledger != nil {
		_, _ = r.app.ledger.Record(ledger.Event{
			Kind:    "telegram_poll_error",
			Source:  "telegram",
			Status:  "error",
			Message: "Telegram polling failed",
		})
	}
}

func (r *telegramRuntime) recordUpdate(update telegram.Update) {
	r.mu.Lock()
	r.updateCount++
	r.lastUpdate = time.Now()
	r.mu.Unlock()
}

func (r *telegramRuntime) recordMessage(summary string) {
	r.mu.Lock()
	r.lastMessage = truncateRunes(summary, 220)
	r.mu.Unlock()
}

func (r *telegramRuntime) recordReject(summary string) {
	r.mu.Lock()
	r.lastReject = truncateRunes(summary, 260)
	r.mu.Unlock()
	if r.app != nil && r.app.ledger != nil {
		_, _ = r.app.ledger.Record(ledger.Event{
			Kind:    "telegram_reject",
			Source:  "telegram",
			Status:  "rejected",
			Message: truncateRunes(summary, 240),
			Detail:  truncateRunes(summary, 1000),
		})
	}
}

func (r *telegramRuntime) recordTelegramRuntimeEvent(kind, status string, msg telegram.Message, text string, detail string) {
	if r == nil || r.app == nil || r.app.ledger == nil {
		return
	}
	if strings.TrimSpace(text) == "" {
		text = describeTelegramMessage(msg)
	}
	metadata := map[string]string{
		"chat_id":           strconv.FormatInt(msg.Chat.ID, 10),
		"source_message_id": strconv.FormatInt(msg.MessageID, 10),
		"chat_type":         msg.Chat.Type,
	}
	if msg.From != nil {
		metadata["from_id"] = strconv.FormatInt(msg.From.ID, 10)
		if msg.From.Username != "" {
			metadata["username"] = msg.From.Username
		}
	}
	_, _ = r.app.ledger.Record(ledger.Event{
		Kind:     kind,
		Source:   "telegram",
		Status:   status,
		Message:  truncateRunes(text, 240),
		Detail:   truncateRunes(detail, 1000),
		Metadata: metadata,
	})
}

func (r *telegramRuntime) recordTelegramSendEvent(msg telegram.Message, responseStatus, sendStatus string, duration time.Duration, telegramMessageID int64, text string, detail string) {
	if r == nil || r.app == nil || r.app.ledger == nil {
		return
	}
	metadata := map[string]string{
		"chat_id":             strconv.FormatInt(msg.Chat.ID, 10),
		"source_message_id":   strconv.FormatInt(msg.MessageID, 10),
		"response_status":     responseStatus,
		"telegram_message_id": strconv.FormatInt(telegramMessageID, 10),
		"reply_chars":         fmt.Sprintf("%d", len(text)),
	}
	if msg.From != nil {
		metadata["from_id"] = strconv.FormatInt(msg.From.ID, 10)
		if msg.From.Username != "" {
			metadata["username"] = msg.From.Username
		}
	}
	if duration > 0 {
		metadata["duration_ms"] = fmt.Sprintf("%d", duration.Milliseconds())
	}
	_, _ = r.app.ledger.Record(ledger.Event{
		Kind:       "telegram_send",
		Source:     "telegram",
		Status:     sendStatus,
		Message:    truncateRunes(text, 240),
		Detail:     truncateRunes(detail, 1000),
		DurationMs: duration.Milliseconds(),
		Metadata:   metadata,
	})
}

func describeTelegramMessage(msg telegram.Message) string {
	parts := []string{
		"chat=" + strconv.FormatInt(msg.Chat.ID, 10),
		"type=" + firstNonEmpty(msg.Chat.Type, "-"),
	}
	if msg.From != nil {
		parts = append(parts, "from_id="+strconv.FormatInt(msg.From.ID, 10))
		if msg.From.Username != "" {
			parts = append(parts, "from=@"+msg.From.Username)
		}
	}
	if strings.TrimSpace(msg.Text) != "" {
		parts = append(parts, "text="+truncateRunes(strings.TrimSpace(msg.Text), 80))
	}
	if msg.Voice != nil {
		parts = append(parts, "voice="+msg.Voice.FileID)
	}
	if msg.Audio != nil {
		parts = append(parts, "audio="+msg.Audio.FileID)
	}
	return strings.Join(parts, " ")
}

func (r *telegramRuntime) currentOffset() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.offset
}

func (r *telegramRuntime) advanceOffset(offset int64) {
	r.mu.Lock()
	if offset > r.offset {
		r.offset = offset
		go r.app.saveTelegramOffset(r.cfg, offset)
	}
	r.mu.Unlock()
}

func (r *telegramRuntime) alreadySawMessage(msg telegram.Message) bool {
	key := strconv.FormatInt(msg.Chat.ID, 10) + ":" + strconv.FormatInt(msg.MessageID, 10)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seenMessages == nil {
		r.seenMessages = map[string]bool{}
	}
	if r.seenMessages[key] {
		return true
	}
	r.seenMessages[key] = true
	return false
}

func (a *App) loadTelegramOffset(cfg settings.TelegramConfig) int64 {
	if a == nil || a.db == nil {
		return 0
	}
	var raw string
	err := a.db.QueryRow(`select value from app_state where key = ?`, telegramOffsetStateKey(cfg)).Scan(&raw)
	if err == sql.ErrNoRows || err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (a *App) saveTelegramOffset(cfg settings.TelegramConfig, offset int64) {
	if a == nil || a.db == nil || offset <= 0 {
		return
	}
	_, _ = a.db.Exec(`
insert into app_state (key, value, updated_at)
values (?, ?, ?)
on conflict(key) do update set value=excluded.value, updated_at=excluded.updated_at
`, telegramOffsetStateKey(cfg), strconv.FormatInt(offset, 10), time.Now().Format(time.RFC3339Nano))
}

func telegramOffsetStateKey(cfg settings.TelegramConfig) string {
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return "telegram.offset.default"
	}
	sum := sha256.Sum256([]byte(token))
	return "telegram.offset." + hex.EncodeToString(sum[:])[:16]
}

func telegramUserID(msg telegram.Message) string {
	if msg.From == nil {
		return ""
	}
	return strconv.FormatInt(msg.From.ID, 10)
}

func telegramUsername(msg telegram.Message) string {
	if msg.From == nil {
		return ""
	}
	return msg.From.Username
}

func sleepTelegram(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "telegram-audio"
	}
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(name)
}
