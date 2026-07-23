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
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"mauler/internal/audio"
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
	EditMessage(ctx context.Context, chatID, messageID int64, text string) error
	SendVoice(ctx context.Context, chatID int64, data []byte, filename, mimeType string) error
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
	progressIDs    map[int64]int64
	progressChats  map[int64]bool
	progressTasks  map[int64]string
	progressStarts map[int64]time.Time
	progressChatID int64
	lastPoll       time.Time
	lastUpdate     time.Time
	lastMessage    string
	lastReject     string
	seenMessages   map[string]bool
	voiceChats     map[int64]bool
	ttsVoice       map[int64]string
	ttsSpeed       map[int64]float64
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
		app:            a,
		client:         telegram.NewClient(cfg.Token),
		cfg:            cfg,
		lastProgress:   map[int64]time.Time{},
		progressIDs:    map[int64]int64{},
		progressChats:  map[int64]bool{},
		progressTasks:  map[int64]string{},
		progressStarts: map[int64]time.Time{},
		seenMessages:   map[string]bool{},
		voiceChats:     map[int64]bool{},
		ttsVoice:       map[int64]string{},
		ttsSpeed:       map[int64]float64{},
		offset:         a.loadTelegramOffset(cfg),
	}
	a.telegramCancel = cancel
	a.telegramRunning = true
	a.telegramStatus = "starting"
	a.telegramService = runtime
	a.telegramMu.Unlock()
	go runtime.run(runCtx)
	go runtime.prewarmVoiceReplies(runCtx)
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

func (a *App) telegramNotifyRunTerminalState(state, detail string) {
	a.telegramMu.Lock()
	svc := a.telegramService
	a.telegramMu.Unlock()
	if svc == nil {
		return
	}
	svc.notifyRunTerminalState(context.Background(), state, detail)
}

func (a *App) trackQueuedTelegramRun(env channelbus.Envelope, task string) (int64, bool) {
	if env.Source != "telegram" || env.Metadata == nil {
		return 0, false
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(env.Metadata["chat_id"]), 10, 64)
	if err != nil {
		return 0, false
	}
	a.telegramMu.Lock()
	svc := a.telegramService
	a.telegramMu.Unlock()
	if svc == nil {
		return 0, false
	}
	svc.trackProgressChat(chatID, task)
	return chatID, true
}

func (a *App) untrackQueuedTelegramRun(chatID int64) {
	if chatID == 0 {
		return
	}
	a.telegramMu.Lock()
	svc := a.telegramService
	a.telegramMu.Unlock()
	if svc != nil {
		svc.untrackProgressChat(chatID)
	}
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
	r.updateVoiceChatMode(msg, attachments)
	if telegramVoiceNotice(text, attachments) {
		r.sendTelegramReply(ctx, msg, text, "voice_notice")
		return
	}
	if !r.shouldHandle(msg, text) {
		r.recordReject("ignored missing mention: " + describeTelegramMessage(msg))
		return
	}
	text = r.stripMention(text)
	if handled := r.handleVoiceCommand(ctx, msg, text); handled {
		return
	}
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
	route := channelbus.RouteEnvelope(env)
	pretracked := false
	if route.Lane == channelbus.LaneWork && !r.app.isAgentRunning() {
		r.trackProgressChat(msg.Chat.ID, route.Argument)
		pretracked = true
	}
	resp, err := r.app.DispatchChannelMessage(env)
	if err != nil {
		if pretracked {
			r.untrackProgressChat(msg.Chat.ID)
		}
		r.sendTelegramReply(ctx, msg, "Telegram route failed: "+err.Error(), "route_error")
		return
	}
	if resp.RunStarted {
		if !pretracked {
			task := route.Argument
			if resp.Data != nil && strings.TrimSpace(resp.Data["task"]) != "" {
				task = resp.Data["task"]
			}
			r.trackProgressChat(msg.Chat.ID, task)
		}
	} else if pretracked {
		r.untrackProgressChat(msg.Chat.ID)
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
	if r.shouldSendVoiceReply(msg, status) {
		if r.sendTelegramVoiceReply(ctx, msg, text, status) {
			return
		}
	}
	start := time.Now()
	messageID, err := r.client.SendMessage(ctx, msg.Chat.ID, text)
	duration := time.Since(start)
	if err != nil {
		r.recordTelegramSendEvent(msg, status, "error", duration, 0, text, err.Error())
		return
	}
	r.recordTelegramSendEvent(msg, status, "sent", duration, messageID, text, "")
}

func (r *telegramRuntime) shouldSendVoiceReply(msg telegram.Message, status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "" && status != "chat" {
		return false
	}
	if status == "voice_notice" || status == "voice_config" || strings.Contains(status, "error") {
		return false
	}
	mode := strings.ToLower(strings.TrimSpace(r.cfg.VoiceReplies))
	switch mode {
	case "always":
		return true
	case "on_voice", "":
		return msg.Voice != nil || msg.Audio != nil || r.isVoiceChat(msg.Chat.ID)
	default:
		return false
	}
}

func (r *telegramRuntime) sendTelegramVoiceReply(ctx context.Context, msg telegram.Message, text string, status string) bool {
	start := time.Now()
	data, err := synthesizeTelegramVoice(ctx, text, r.telegramTTSOptions(msg.Chat.ID))
	duration := time.Since(start)
	if err != nil {
		r.recordTelegramRuntimeEvent("telegram_voice_reply", "error", msg, "Voice reply synthesis failed", err.Error())
		r.sendTelegramVoiceFailureNotice(ctx, msg, "Voice reply failed: "+telegramAudioSetupHint(err.Error()))
		return true
	}
	if err := r.client.SendVoice(ctx, msg.Chat.ID, data, "mauler-reply.ogg", "audio/ogg"); err != nil {
		r.recordTelegramRuntimeEvent("telegram_voice_reply", "error", msg, "Voice reply send failed", err.Error())
		r.sendTelegramVoiceFailureNotice(ctx, msg, "Voice reply could not be sent to Telegram: "+err.Error())
		return true
	}
	r.recordTelegramSendEvent(msg, status, "voice_sent", duration, 0, text, "voice reply sent")
	return true
}

func (r *telegramRuntime) sendTelegramVoiceFailureNotice(ctx context.Context, msg telegram.Message, text string) {
	messageID, err := r.client.SendMessage(ctx, msg.Chat.ID, text)
	if err != nil {
		r.recordTelegramSendEvent(msg, "voice_error", "error", 0, 0, text, err.Error())
		return
	}
	r.recordTelegramSendEvent(msg, "voice_error", "sent", 0, messageID, text, "")
}

func (r *telegramRuntime) messageTextAndAttachments(ctx context.Context, msg telegram.Message) (string, []channelbus.Attachment) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Caption)
	}
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
	if photo := largestTelegramPhoto(msg.Photo); photo != nil {
		path, contentType, err := r.downloadTelegramAttachment(ctx, photo.FileID, "photo.jpg", "image/jpeg", "images")
		note := "Telegram image received."
		if err != nil {
			note = "Telegram image received, but download failed: " + err.Error()
		}
		attachments = append(attachments, channelbus.Attachment{Kind: "image", FileID: photo.FileID, FileName: filepath.Base(path), ContentType: contentType, Path: path, Text: note})
		if text == "" {
			text = note
		}
	}
	if msg.Document != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(msg.Document.MimeType)), "image/") {
		name := firstNonEmpty(msg.Document.FileName, "image")
		path, contentType, err := r.downloadTelegramAttachment(ctx, msg.Document.FileID, name, msg.Document.MimeType, "images")
		note := "Telegram image document received."
		if err != nil {
			note = "Telegram image document received, but download failed: " + err.Error()
		}
		attachments = append(attachments, channelbus.Attachment{Kind: "image", FileID: msg.Document.FileID, FileName: name, ContentType: contentType, Path: path, Text: note})
		if text == "" {
			text = note
		}
	}
	return text, attachments
}

func (r *telegramRuntime) handleVoiceCommand(ctx context.Context, msg telegram.Message, text string) bool {
	if !strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return false
	}
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	switch cmd {
	case "voices":
		r.sendTelegramReply(ctx, msg, "Available Kokoro voices:\n"+strings.Join(audio.KokoroVoices(), ", ")+"\n\nUse /voice <id> and /speed <0.8-1.4>.", "voice_config")
		return true
	case "voice":
		if len(fields) < 2 {
			voice, speed := r.voiceSettings(msg.Chat.ID)
			r.sendTelegramReply(ctx, msg, fmt.Sprintf("Current voice: %s\nCurrent speed: %.2fx\n\nUse /voices to list options, /voice <id>, or /speed <0.8-1.4>.", voice, speed), "voice_config")
			return true
		}
		voice := strings.TrimSpace(fields[1])
		if !audio.IsKokoroVoice(voice) {
			r.sendTelegramReply(ctx, msg, "Unknown Kokoro voice: "+voice+"\nUse /voices to list supported voices.", "voice_config")
			return true
		}
		r.mu.Lock()
		if r.ttsVoice == nil {
			r.ttsVoice = map[int64]string{}
		}
		r.ttsVoice[msg.Chat.ID] = voice
		r.mu.Unlock()
		r.sendTelegramReply(ctx, msg, "Voice set to "+voice+".", "voice_config")
		return true
	case "speed":
		if len(fields) < 2 {
			_, speed := r.voiceSettings(msg.Chat.ID)
			r.sendTelegramReply(ctx, msg, fmt.Sprintf("Current voice speed: %.2fx\nUse /speed <0.8-1.4>. Normal speech is /speed 1.0.", speed), "voice_config")
			return true
		}
		speed, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || speed < 0.8 || speed > 1.4 {
			r.sendTelegramReply(ctx, msg, "Speed must be a number from 0.8 to 1.4. Normal speech is /speed 1.0.", "voice_config")
			return true
		}
		r.mu.Lock()
		if r.ttsSpeed == nil {
			r.ttsSpeed = map[int64]float64{}
		}
		r.ttsSpeed[msg.Chat.ID] = speed
		r.mu.Unlock()
		r.sendTelegramReply(ctx, msg, fmt.Sprintf("Voice speed set to %.2fx.", speed), "voice_config")
		return true
	default:
		return false
	}
}

func (r *telegramRuntime) updateVoiceChatMode(msg telegram.Message, attachments []channelbus.Attachment) {
	hasVoice := telegramHasAudioAttachment(attachments)
	hasTypedText := strings.TrimSpace(msg.Text) != ""
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.voiceChats == nil {
		r.voiceChats = map[int64]bool{}
	}
	if hasVoice {
		r.voiceChats[msg.Chat.ID] = true
		return
	}
	if hasTypedText {
		delete(r.voiceChats, msg.Chat.ID)
	}
}

func (r *telegramRuntime) isVoiceChat(chatID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.voiceChats != nil && r.voiceChats[chatID]
}

func (r *telegramRuntime) downloadAndTranscribe(ctx context.Context, fileID, fallbackName, mimeType string) (string, string) {
	path, _, data, err := r.downloadTelegramFile(ctx, fileID, fallbackName, mimeType, "audio")
	if err != nil {
		return "", "Voice received, but Telegram download failed: " + err.Error()
	}
	transcript := r.transcribeAudio(ctx, data, path, filepath.Base(path), mimeType)
	if strings.TrimSpace(transcript) == "" {
		transcript = telegramVoiceTranscriptionNotice(r.cfg, path)
	}
	return path, transcript
}

func (r *telegramRuntime) saveTelegramAudio(fileID, fallbackName string, data []byte) string {
	path, _ := r.saveTelegramDownloadedFile(fileID, fallbackName, data, "audio")
	return path
}

func (r *telegramRuntime) downloadTelegramAttachment(ctx context.Context, fileID, fallbackName, mimeType, folder string) (string, string, error) {
	path, contentType, _, err := r.downloadTelegramFile(ctx, fileID, fallbackName, mimeType, folder)
	return path, contentType, err
}

func (r *telegramRuntime) downloadTelegramFile(ctx context.Context, fileID, fallbackName, mimeType, folder string) (string, string, []byte, error) {
	file, err := r.client.GetFile(ctx, fileID)
	if err != nil {
		return "", mimeType, nil, fmt.Errorf("file lookup failed: %w", err)
	}
	data, err := r.client.DownloadFile(ctx, file.FilePath)
	if err != nil {
		return "", mimeType, nil, err
	}
	name := fallbackName
	if filepath.Ext(name) == "" {
		if ext := filepath.Ext(file.FilePath); ext != "" {
			name += ext
		}
	}
	path, contentType := r.saveTelegramDownloadedFile(fileID, name, data, folder)
	if strings.TrimSpace(mimeType) != "" {
		contentType = mimeType
	}
	return path, contentType, data, nil
}

func (r *telegramRuntime) saveTelegramDownloadedFile(fileID, fallbackName string, data []byte, folder string) (string, string) {
	dir, err := settings.ConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	folder = sanitizeFilename(firstNonEmpty(folder, "files"))
	downloadDir := filepath.Join(dir, "telegram", folder)
	_ = os.MkdirAll(downloadDir, 0o755)
	name := sanitizeFilename(firstNonEmpty(fileID, fallbackName))
	if filepath.Ext(name) == "" {
		name += filepath.Ext(fallbackName)
	}
	path := filepath.Join(downloadDir, name)
	_ = os.WriteFile(path, data, 0o600)
	path = filepath.ToSlash(path)
	return path, telegramContentTypeFromName(path)
}

func (r *telegramRuntime) transcribeAudio(ctx context.Context, data []byte, path, filename, mimeType string) string {
	mode := strings.ToLower(strings.TrimSpace(r.cfg.TranscriptionMode))
	if mode == "" || mode == "disabled" {
		return ""
	}
	if strings.TrimSpace(r.cfg.TranscriptionURL) == "" {
		if mode == "local" {
			return runLocalWhisperTranscription(ctx, path)
		}
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
	resp, err := telegramTranscriptionHTTPClient(r.cfg.TranscriptionURL).Do(req)
	if err != nil {
		if mode == "local" {
			return runLocalWhisperTranscription(ctx, path)
		}
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if mode == "local" {
			return runLocalWhisperTranscription(ctx, path)
		}
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

func telegramTranscriptionHTTPClient(rawURL string) *http.Client {
	host := ""
	if parsed, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		host = parsed.Hostname()
	}
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return &http.Client{
			Timeout: 35 * time.Second,
			Transport: &http.Transport{
				Proxy: nil,
			},
		}
	}
	return http.DefaultClient
}

var runLocalWhisperTranscription = persistentLocalWhisperTranscription
var synthesizeTelegramVoice = defaultSynthesizeTelegramVoice

func persistentLocalWhisperTranscription(ctx context.Context, path string) string {
	transcript, err := audio.TranscribeWhisper(ctx, path, "")
	if err == nil && strings.TrimSpace(transcript) != "" {
		return strings.TrimSpace(transcript)
	}
	if audio.IsSpeechRejected(err) {
		return ""
	}
	return defaultLocalWhisperTranscription(ctx, path)
}

func defaultLocalWhisperTranscription(ctx context.Context, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	command, args, ok := resolveWhisperCommand()
	if !ok {
		return ""
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	outDir, err := os.MkdirTemp("", "mauler-whisper-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(outDir)
	whisperArgs := append(args, path,
		"--model", "tiny.en",
		"--language", "en",
		"--task", "transcribe",
		"--fp16", "False",
		"--output_format", "txt",
		"--output_dir", outDir,
	)
	cmd := exec.CommandContext(runCtx, command, whisperArgs...)
	hideShellWindow(cmd)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	if _, err := cmd.CombinedOutput(); err != nil {
		return ""
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".txt"
	data, err := os.ReadFile(filepath.Join(outDir, base))
	if err != nil {
		matches, _ := filepath.Glob(filepath.Join(outDir, "*.txt"))
		if len(matches) == 0 {
			return ""
		}
		data, err = os.ReadFile(matches[0])
		if err != nil {
			return ""
		}
	}
	return strings.TrimSpace(string(data))
}

func resolveWhisperCommand() (string, []string, bool) {
	if override := strings.TrimSpace(os.Getenv("MAULER_WHISPER_COMMAND")); override != "" {
		return override, nil, true
	}
	if path, err := exec.LookPath("whisper"); err == nil {
		return path, nil, true
	}
	for _, candidate := range []string{os.Getenv("MAULER_STT_PYTHON"), os.Getenv("MAULER_KOKORO_PYTHON"), "python", "py", "python3"} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, path, "-c", "import whisper")
		hideShellWindow(cmd)
		cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
		err = cmd.Run()
		cancel()
		if err == nil {
			return path, []string{"-m", "whisper"}, true
		}
	}
	return "", nil, false
}

func defaultSynthesizeTelegramVoice(ctx context.Context, text string, opts audio.TTSOptions) ([]byte, error) {
	text = cleanTelegramVoiceText(text)
	if text == "" {
		return nil, fmt.Errorf("empty voice text")
	}
	result, err := audio.SynthesizeVoiceNote(ctx, text, opts)
	if err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (r *telegramRuntime) telegramTTSOptions(chatID int64) audio.TTSOptions {
	voice, speed := r.voiceSettings(chatID)
	return audio.TTSOptions{
		Engine:       firstNonEmpty(os.Getenv("MAULER_TTS_ENGINE"), "auto"),
		Voice:        voice,
		KokoroPython: os.Getenv("MAULER_KOKORO_PYTHON"),
		PiperPath:    os.Getenv("MAULER_PIPER_PATH"),
		PiperModel:   os.Getenv("MAULER_PIPER_MODEL"),
		PiperConfig:  os.Getenv("MAULER_PIPER_CONFIG"),
		DataDirs: []string{
			maulerTTSPath(),
			helixClawTTSPath(),
		},
		MaxChars: 900,
		Speed:    speed,
	}
}

func (r *telegramRuntime) prewarmVoiceReplies(ctx context.Context) {
	mode := strings.ToLower(strings.TrimSpace(r.cfg.VoiceReplies))
	if mode == "" || mode == "off" || mode == "never" || mode == "disabled" {
		return
	}
	warmCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, _ = synthesizeTelegramVoice(warmCtx, "Ready.", r.telegramTTSOptions(0))
}

func (r *telegramRuntime) voiceSettings(chatID int64) (string, float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	voice := firstNonEmpty(os.Getenv("MAULER_TTS_VOICE"), os.Getenv("MAULER_KOKORO_VOICE"), audio.DefaultKokoroVoice)
	if r.ttsVoice != nil && strings.TrimSpace(r.ttsVoice[chatID]) != "" {
		voice = strings.TrimSpace(r.ttsVoice[chatID])
	}
	speed := 1.0
	if raw := strings.TrimSpace(os.Getenv("MAULER_TTS_SPEED")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed >= 0.8 && parsed <= 1.4 {
			speed = parsed
		}
	}
	if r.ttsSpeed != nil && r.ttsSpeed[chatID] >= 0.8 && r.ttsSpeed[chatID] <= 1.4 {
		speed = r.ttsSpeed[chatID]
	}
	return voice, speed
}

func resolvePiperVoiceInstall() (piperPath, modelPath, configPath string, err error) {
	piperPath = firstExistingPath(
		os.Getenv("MAULER_PIPER_PATH"),
		maulerTTSPath("piper", executableName("piper")),
		helixClawTTSPath("piper", executableName("piper")),
	)
	if piperPath == "" {
		if path, lookErr := exec.LookPath("piper"); lookErr == nil {
			piperPath = path
		}
	}
	if piperPath == "" {
		return "", "", "", fmt.Errorf("piper.exe not found; set MAULER_PIPER_PATH or install Piper under the Mauler/HelixClaw tts directory")
	}

	voice := strings.TrimSpace(os.Getenv("MAULER_PIPER_VOICE"))
	if voice == "" {
		voice = "en_GB-jenny_dioco-medium"
	}
	modelPath = firstExistingPath(
		os.Getenv("MAULER_PIPER_MODEL"),
		maulerTTSPath("voices", voice+".onnx"),
		helixClawTTSPath("voices", voice+".onnx"),
		helixClawTTSPath("voices", "en_US-lessac-medium.onnx"),
	)
	configPath = firstExistingPath(
		os.Getenv("MAULER_PIPER_CONFIG"),
		maulerTTSPath("voices", voice+".onnx.json"),
		helixClawTTSPath("voices", voice+".onnx.json"),
		helixClawTTSPath("voices", "en_US-lessac-medium.onnx.json"),
	)
	if modelPath == "" || configPath == "" {
		return "", "", "", fmt.Errorf("Piper voice model/config not found; set MAULER_PIPER_MODEL and MAULER_PIPER_CONFIG")
	}
	return piperPath, modelPath, configPath, nil
}

func executableName(name string) string {
	if strings.HasSuffix(strings.ToLower(name), ".exe") || runtime.GOOS != "windows" {
		return name
	}
	return name + ".exe"
}

func maulerTTSPath(parts ...string) string {
	dir, err := settings.ConfigDir()
	if err != nil {
		return ""
	}
	all := append([]string{dir, "tts"}, parts...)
	return filepath.Join(all...)
}

func helixClawTTSPath(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	all := append([]string{home, ".HelixClaw", "tts"}, parts...)
	return filepath.Join(all...)
}

func firstExistingPath(paths ...string) string {
	for _, path := range paths {
		path = strings.Trim(strings.TrimSpace(path), `"`)
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func cleanTelegramVoiceText(text string) string {
	text = sanitizeVisibleModelText(text)
	replacer := strings.NewReplacer(
		"**", "",
		"__", "",
		"`", "",
		"#", "",
		">", "",
		"*", "",
		"[", "",
		"]", "",
		"(", "",
		")", "",
	)
	text = replacer.Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	return truncateRunes(strings.TrimSpace(text), 900)
}

func telegramVoiceNotice(text string, attachments []channelbus.Attachment) bool {
	if len(attachments) == 0 {
		return false
	}
	if !telegramHasAudioAttachment(attachments) {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(text), "Voice message received, but ")
}

func telegramHasAudioAttachment(attachments []channelbus.Attachment) bool {
	for _, att := range attachments {
		kind := strings.ToLower(strings.TrimSpace(att.Kind))
		ct := strings.ToLower(strings.TrimSpace(att.ContentType))
		if kind == "voice" || kind == "audio" || strings.Contains(ct, "audio/") || strings.Contains(ct, "ogg") || strings.Contains(ct, "opus") {
			return true
		}
	}
	return false
}

func largestTelegramPhoto(photos []telegram.PhotoSize) *telegram.PhotoSize {
	if len(photos) == 0 {
		return nil
	}
	best := photos[0]
	bestScore := best.FileSize
	if bestScore <= 0 {
		bestScore = int64(best.Width * best.Height)
	}
	for _, photo := range photos[1:] {
		score := photo.FileSize
		if score <= 0 {
			score = int64(photo.Width * photo.Height)
		}
		if score > bestScore {
			best = photo
			bestScore = score
		}
	}
	return &best
}

func telegramContentTypeFromName(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return "application/octet-stream"
	}
	if ct := mime.TypeByExtension(ext); strings.TrimSpace(ct) != "" {
		return ct
	}
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".ogg", ".oga", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	default:
		return "application/octet-stream"
	}
}

func telegramVoiceTranscriptionNotice(cfg settings.TelegramConfig, path string) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.TranscriptionMode))
	switch {
	case mode == "" || mode == "disabled":
		return "Voice message received, but transcription is disabled. I saved the audio locally at:\n" + path
	case mode == "local" && strings.TrimSpace(cfg.TranscriptionURL) == "":
		return "Voice message received, but local Whisper transcription failed or Whisper is unavailable. Run .\\setup.ps1 -Auto or install with `python -m pip install openai-whisper`; I saved the audio locally at:\n" + path
	case strings.TrimSpace(cfg.TranscriptionURL) == "":
		return "Voice message received, but transcription is not configured yet. Set Telegram transcription_url to a local/OpenAI-compatible STT endpoint, or set transcription_mode=disabled. I saved the audio locally at:\n" + path
	default:
		return "Voice message received, but transcription returned no text. I saved the audio locally at:\n" + path
	}
}

func telegramAudioSetupHint(detail string) string {
	detail = strings.TrimSpace(detail)
	base := "run .\\setup.ps1 -Auto, or install Python packages with `python -m pip install kokoro soundfile openai-whisper` and make sure ffmpeg is on PATH"
	if detail == "" {
		return base
	}
	return detail + ". To enable voice chat, " + base
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
	r.notifyRunStateWithTerminal(ctx, state, detail, isRemoteRunDeliveryTerminalState(state))
}

func (r *telegramRuntime) notifyRunTerminalState(ctx context.Context, state, detail string) {
	r.notifyRunStateWithTerminal(ctx, state, detail, true)
}

func (r *telegramRuntime) notifyRunStateWithTerminal(ctx context.Context, state, detail string, terminal bool) {
	if !r.cfg.SendProgress && !terminal {
		return
	}
	r.mu.Lock()
	interval := time.Duration(r.cfg.ProgressIntervalS) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	type progressTarget struct {
		chatID  int64
		task    string
		elapsed time.Duration
	}
	targets := make([]progressTarget, 0, len(r.progressChats))
	for chatID := range r.progressChats {
		if time.Since(r.lastProgress[chatID]) < interval && !terminal {
			continue
		}
		r.lastProgress[chatID] = time.Now()
		elapsed := time.Duration(0)
		if started := r.progressStarts[chatID]; !started.IsZero() {
			elapsed = time.Since(started)
		}
		targets = append(targets, progressTarget{chatID: chatID, task: r.progressTasks[chatID], elapsed: elapsed})
	}
	if terminal {
		r.progressChats = map[int64]bool{}
		r.progressTasks = map[int64]string{}
		r.progressStarts = map[int64]time.Time{}
	}
	r.mu.Unlock()
	if len(targets) == 0 {
		return
	}
	for _, target := range targets {
		msg := formatTelegramRunProgressMessageForTask(state, detail, target.task, target.elapsed)
		r.sendTelegramProgressUpdateWithTerminal(ctx, target.chatID, state, msg, terminal)
	}
}

func (r *telegramRuntime) sendTelegramProgressUpdate(ctx context.Context, chatID int64, state, text string) {
	r.sendTelegramProgressUpdateWithTerminal(ctx, chatID, state, text, isRemoteRunDeliveryTerminalState(state))
}

func (r *telegramRuntime) sendTelegramProgressUpdateWithTerminal(ctx context.Context, chatID int64, state, text string, terminal bool) {
	start := time.Now()
	r.mu.Lock()
	existingID := r.progressIDs[chatID]
	r.mu.Unlock()
	var messageID int64
	var err error
	sendStatus := "edited"
	if existingID > 0 {
		err = r.client.EditMessage(ctx, chatID, existingID, text)
		messageID = existingID
	}
	if existingID == 0 || err != nil {
		messageID, err = r.client.SendMessage(ctx, chatID, text)
		sendStatus = "sent"
	}
	duration := time.Since(start)
	detail := ""
	if err != nil {
		sendStatus = "error"
		detail = err.Error()
	} else {
		r.mu.Lock()
		if terminal {
			delete(r.progressIDs, chatID)
		} else if messageID > 0 {
			r.progressIDs[chatID] = messageID
		}
		r.mu.Unlock()
	}
	if r.app != nil && r.app.ledger != nil {
		_, _ = r.app.ledger.Record(ledger.Event{
			Kind:       "telegram_progress_send",
			Source:     "telegram",
			Status:     sendStatus,
			Message:    truncateRunes(text, 240),
			Detail:     truncateRunes(detail, 1000),
			DurationMs: duration.Milliseconds(),
			Metadata: map[string]string{
				"chat_id":             strconv.FormatInt(chatID, 10),
				"run_state":           state,
				"telegram_message_id": strconv.FormatInt(messageID, 10),
			},
		})
	}
}

func formatRemoteRunStartMessage(prompt string, cfg *settings.Settings) string {
	var lines []string
	lines = append(lines, "\u25b6 Mauler run started")
	if cfg != nil {
		if cfg.ActiveProfile != "" {
			lines = append(lines, "Model profile: "+cfg.ActiveProfile)
		}
		if cfg.Tools.ActiveToolset != "" {
			lines = append(lines, "Access: "+cfg.Tools.ActiveToolset)
		}
	}
	if trimmed := strings.TrimSpace(prompt); trimmed != "" {
		lines = append(lines, "Task: "+truncateRunes(trimmed, 360))
	}
	if cfg != nil && cfg.Telegram.SendProgress {
		lines = append(lines, "I will keep one status message updated and include the final result.")
	} else {
		lines = append(lines, "I will send the final result when the run finishes.")
	}
	return strings.Join(lines, "\n")
}

func formatTelegramRunProgressMessage(state, detail string) string {
	return formatTelegramRunProgressMessageForTask(state, detail, "", 0)
}

func formatTelegramRunProgressMessageForTask(state, detail, task string, elapsed time.Duration) string {
	state = strings.TrimSpace(state)
	detail = strings.TrimSpace(detail)
	if state == "" {
		state = "working"
	}
	lines := []string{remoteRunTitle(state)}
	if task = strings.TrimSpace(task); task != "" {
		lines = append(lines, "Task: "+truncateRunes(task, 360))
	}
	lines = append(lines, "Stage: "+remoteRunPhase(state))
	if model := formatTelegramModelLoadDetail(detail); state == "model_loading" && model != "" {
		lines = append(lines, splitNonEmptyLines(model)...)
	} else if isRemoteRunTerminalState(state) {
		terminalDetail := detail
		if state != "done" {
			terminalDetail = cleanTelegramRunDetail(detail)
		}
		if result := cleanTelegramRunResult(terminalDetail); result != "" {
			label := "Reason:"
			switch state {
			case "done":
				label = "Result:"
			case "failed":
				label = "Error:"
			}
			lines = append(lines, label, result)
		}
	} else if current := telegramRunCurrentAction(state, detail); current != "" {
		lines = append(lines, "Current: "+truncateRunes(current, 700))
	}
	if elapsed > 0 {
		label := "Elapsed: "
		if isRemoteRunTerminalState(state) {
			label = "Finished in: "
		}
		lines = append(lines, label+formatRemoteRunDuration(elapsed))
	}
	return strings.Join(lines, "\n")
}

/*
	switch state {
	case "model_loading":
		lines := []string{"⏳ Model loading"}
		if model := formatTelegramModelLoadDetail(detail); model != "" {
			lines = append(lines, model)
		}
		lines = append(lines, "Status: preparing local inference")
		return strings.Join(lines, "\n")
	case "planning":
		return runStatusMessage("🧭 Planning", "Building the next action plan.", detail)
	case "researching":
		return runStatusMessage("🔎 Researching", "Looking up or collecting evidence.", detail)
	case "reading":
		return runStatusMessage("📖 Reading", "Inspecting files, sessions, or captured output.", detail)
	case "editing":
		return runStatusMessage("✏️ Editing", "Changing local files or artifacts.", detail)
	case "testing":
		return runStatusMessage("🧪 Testing", "Running a local check or command.", detail)
	case "using_tools":
		return runStatusMessage("🛠 Using tool", "Calling an approved local tool.", detail)
	case "thinking":
		return runStatusMessage("💭 Thinking", "Choosing the next step from current evidence.", detail)
	case "done":
		return runStatusMessage("✅ Run done", "Completed successfully.", detail)
	case "failed":
		return runStatusMessage("❌ Run failed", "Stopped with an error.", detail)
	case "blocked":
		return runStatusMessage("⛔ Run paused", "The loop guard blocked repeated unhelpful work.", detail)
	case "stopped":
		return runStatusMessage("⏹ Run stopped", "Stopped by request or shutdown.", detail)
	default:
		title := "• " + strings.ReplaceAll(state, "_", " ")
		return runStatusMessage(title, "Working on the request.", detail)
	}
}

func runStatusMessage(title, summary, detail string) string {
	lines := []string{title}
	if summary != "" {
		lines = append(lines, "Status: "+summary)
	}
	if detail = cleanTelegramRunDetail(detail); detail != "" {
		lines = append(lines, "Detail: "+truncateRunes(detail, 700))
	}
	return strings.Join(lines, "\n")
}

*/

func remoteRunTitle(state string) string {
	switch state {
	case "done":
		return "\u2705 Mauler finished"
	case "failed":
		return "\u274c Mauler failed"
	case "blocked":
		return "\u26d4 Mauler needs attention"
	case "stopped":
		return "\u23f9 Mauler stopped"
	default:
		return "\u23f3 Mauler is working"
	}
}

func remoteRunPhase(state string) string {
	switch state {
	case "model_loading":
		return "Loading the model"
	case "planning":
		return "Planning"
	case "researching":
		return "Researching"
	case "reading":
		return "Reading context"
	case "editing":
		return "Editing files"
	case "testing":
		return "Verifying"
	case "using_tools":
		return "Using a tool"
	case "thinking":
		return "Thinking"
	case "done":
		return "Complete"
	case "failed":
		return "Failed"
	case "blocked":
		return "Blocked by the loop guard"
	case "stopped":
		return "Stopped"
	default:
		words := strings.ReplaceAll(state, "_", " ")
		if words == "" {
			return "Working"
		}
		return strings.ToUpper(words[:1]) + words[1:]
	}
}

func isRemoteRunTerminalState(state string) bool {
	switch strings.TrimSpace(state) {
	case "done", "failed", "blocked", "stopped":
		return true
	default:
		return false
	}
}

func isRemoteRunDeliveryTerminalState(state string) bool {
	switch strings.TrimSpace(state) {
	case "done", "failed", "stopped":
		return true
	default:
		return false
	}
}

func formatRemoteRunDuration(elapsed time.Duration) string {
	if elapsed < time.Second {
		return "<1s"
	}
	seconds := int64(elapsed.Round(time.Second) / time.Second)
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	seconds %= 60
	if seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

func telegramRunCurrentAction(state, detail string) string {
	detail = strings.TrimSpace(detail)
	switch detail {
	case "shell":
		return "Running a shell command"
	case "terminal_send":
		return "Running a command in the shared terminal"
	case "terminal_read":
		return "Reading terminal output"
	case "read", "read_file", "read_pdf":
		return "Reading a file"
	case "glob", "grep", "session_search":
		return "Searching local context"
	case "web_search", "fetch_url", "http_probe":
		return "Collecting scoped web evidence"
	case "browser":
		return "Inspecting the browser"
	case "write", "edit":
		return "Updating a file"
	}
	if state == "thinking" && (strings.Contains(detail, "tool_choice=") || strings.Contains(detail, "messages=")) {
		return "Choosing the next useful action"
	}
	return cleanTelegramRunDetail(detail)
}

func cleanTelegramRunResult(detail string) string {
	detail = strings.TrimSpace(strings.ReplaceAll(detail, "\x00", " "))
	if detail == "" {
		return ""
	}
	var lines []string
	blank := false
	for _, line := range strings.Split(strings.ReplaceAll(detail, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) == "" {
			if len(lines) > 0 && !blank {
				lines = append(lines, "")
				blank = true
			}
			continue
		}
		lines = append(lines, line)
		blank = false
	}
	return truncateRunes(strings.TrimSpace(strings.Join(lines, "\n")), 2800)
}

func telegramRunCompletionDetail(run TaskRun, finalSummary string) string {
	summary := strings.TrimSpace(finalSummary)
	if summary != "" && !isGenericTelegramCompletion(summary) {
		return summary
	}
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if !telegramResultBearingTool(tool.Name) || (tool.Status != "done" && tool.Status != "routed") {
			continue
		}
		if result := cleanTelegramRunResult(tool.Result); result != "" {
			return result
		}
	}
	if summary != "" {
		return summary
	}
	if response := strings.TrimSpace(run.Response); response != "" {
		return response
	}
	return "The run completed successfully, but it did not produce a final text result."
}

func isGenericTelegramCompletion(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = strings.Trim(normalized, " .!`*_#:-")
	switch normalized {
	case "done", "complete", "completed", "finished", "success", "successful",
		"run completed successfully", "task completed successfully", "command completed successfully":
		return true
	}
	if len([]rune(normalized)) <= 180 {
		return strings.Contains(normalized, "completed successfully") ||
			strings.Contains(normalized, "finished successfully") ||
			strings.Contains(normalized, "command succeeded") ||
			strings.Contains(normalized, "ran successfully")
	}
	return false
}

func telegramResultBearingTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "shell", "terminal_send", "terminal_read", "http_probe", "read", "read_pdf", "grep", "sqlite", "browser":
		return true
	default:
		return false
	}
}

func splitNonEmptyLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func cleanTelegramRunDetail(detail string) string {
	detail = strings.TrimSpace(strings.ReplaceAll(detail, "\x00", " "))
	if detail == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"tool_choice=", "tool choice: ",
		"messages=", "messages: ",
		"tools=", "tools: ",
		"no_think=", "no-think: ",
		"effort=", "effort: ",
		"stability_score=", "stability score: ",
		"repeated_tool_inputs=", "repeated tool inputs: ",
		"repeated_identical_outcomes=", "repeated identical outcomes: ",
		"repeated_skips=", "repeated skips: ",
		"tool_errors=", "tool errors: ",
		"tool_cycle_detected=", "tool cycle detected: ",
		"tool_cycle_period=", "tool cycle period: ",
	)
	return strings.Join(strings.Fields(replacer.Replace(detail)), " ")
}

func formatTelegramModelLoadDetail(detail string) string {
	parts := strings.Split(detail, "\x00")
	if len(parts) == 5 {
		var lines []string
		if parts[0] != "" {
			lines = append(lines, "Backend: "+parts[0])
		}
		if parts[2] != "" {
			lines = append(lines, "Model: "+parts[2])
		}
		if parts[3] != "" && parts[3] != "0" {
			lines = append(lines, "Context: "+parts[3]+" tokens")
		}
		if parts[1] != "" {
			lines = append(lines, "Endpoint: "+parts[1])
		}
		return strings.Join(lines, "\n")
	}
	return cleanTelegramRunDetail(detail)
}

func (r *telegramRuntime) trackProgressChat(chatID int64, task string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progressChats[chatID] = true
	r.progressTasks[chatID] = strings.TrimSpace(task)
	r.progressStarts[chatID] = time.Now()
	// Give the acknowledgement message a chance to arrive before the first
	// periodic edit. Terminal states bypass this interval and are never delayed.
	r.lastProgress[chatID] = time.Now()
}

func (r *telegramRuntime) untrackProgressChat(chatID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.progressChats, chatID)
	delete(r.progressTasks, chatID)
	delete(r.progressStarts, chatID)
	delete(r.lastProgress, chatID)
	delete(r.progressIDs, chatID)
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
