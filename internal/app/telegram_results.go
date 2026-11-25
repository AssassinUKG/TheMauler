package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"mauler/internal/telegram"
)

// Presentation only. No callback may dispatch a model, command or task.
type telegramRichAPI interface {
	SendMessageWithOptions(context.Context, int64, string, telegram.MessageOptions) (int64, error)
	EditMessageWithOptions(context.Context, int64, int64, string, telegram.MessageOptions) error
	AnswerCallbackQuery(context.Context, string, string) error
	SendDocument(context.Context, int64, []byte, string, telegram.MessageOptions) error
}

type telegramResultEntry struct {
	owner         telegram.Message
	messageID     int64
	text, sources string
	expires       time.Time
	used          map[string]bool
}

type telegramResultStore struct {
	mu      sync.Mutex
	entries map[string]*telegramResultEntry
	bytes   int
}

func (s *telegramResultStore) prepare(owner telegram.Message, text, sources string, now time.Time) (string, string, telegram.MessageOptions) {
	options := telegram.MessageOptions{ThreadID: owner.MessageThreadID, ReplyToMessageID: owner.MessageID}
	if owner.From == nil || owner.From.ID == 0 || owner.Chat.ID == 0 || len(text) > 64*1024 || strings.TrimSpace(text) == "" {
		return text, "", options
	}
	long := len([]rune(text)) > 1800
	if !long && sources == "" {
		return text, "", options
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[string]*telegramResultEntry{}
	}
	for key, entry := range s.entries {
		if !now.Before(entry.expires) {
			s.bytes -= len(entry.text) + len(entry.sources)
			delete(s.entries, key)
		}
	}
	if len(s.entries) >= 128 || s.bytes+len(text)+len(sources) > 8*1024*1024 {
		return text, "", options
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return text, "", options
	}
	key := hex.EncodeToString(nonce[:])
	s.entries[key] = &telegramResultEntry{owner: owner, text: text, sources: sources, expires: now.Add(15 * time.Minute), used: map[string]bool{}}
	s.bytes += len(text) + len(sources)
	var buttons []telegram.InlineKeyboardButton
	add := func(label, view string) {
		buttons = append(buttons, telegram.InlineKeyboardButton{Text: label, CallbackData: "mm:" + key + ":" + view})
	}
	if long {
		add("Full report", "full")
		add("Download .md", "download")
	}
	if sources != "" {
		add("Sources", "sources")
	}
	options.ReplyMarkup = &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{buttons}}
	if !long {
		return text, key, options
	}
	prose := strings.SplitN(text, "```", 2)[0]
	runes := []rune(prose)
	if len(runes) > 1200 {
		runes = runes[:1200]
	}
	excerpt := strings.TrimSpace(string(runes))
	if end := strings.LastIndex(excerpt, "\n\n"); end > len(excerpt)/3 {
		excerpt = excerpt[:end]
	}
	if excerpt == "" {
		excerpt = "The complete answer includes code or a long report."
	}
	return "**Reply preview**\n\n" + excerpt + "\n\n…\n\nFull report shows the complete answer; Download saves the original Markdown. Buttons expire after 15 minutes or restart.", key, options
}

func (s *telegramResultStore) bind(key string, id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[key]; entry != nil && id > 0 {
		entry.messageID = id
	}
}
func (s *telegramResultStore) discard(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[key]; entry != nil {
		s.bytes -= len(entry.text) + len(entry.sources)
		delete(s.entries, key)
	}
}

func (s *telegramResultStore) take(query telegram.CallbackQuery, now time.Time) (telegramResultEntry, string, bool) {
	parts := strings.Split(query.Data, ":")
	if len(parts) != 3 || parts[0] != "mm" {
		return telegramResultEntry{}, "", false
	}
	view := parts[2]
	if view != "full" && view != "sources" && view != "download" {
		return telegramResultEntry{}, "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[parts[1]]
	if entry == nil || query.Message == nil || !now.Before(entry.expires) || entry.owner.From == nil || entry.messageID == 0 || query.From.ID != entry.owner.From.ID || query.Message.Chat.ID != entry.owner.Chat.ID || query.Message.MessageThreadID != entry.owner.MessageThreadID || query.Message.MessageID != entry.messageID || entry.used[view] {
		return telegramResultEntry{}, "", false
	}
	if view == "sources" && entry.sources == "" {
		return telegramResultEntry{}, "", false
	}
	entry.used[view] = true
	return *entry, view, true
}

func (r *telegramRuntime) handleResultCallback(ctx context.Context, query telegram.CallbackQuery) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, ok := r.client.(telegramRichAPI)
	if !ok {
		return
	}
	accepted := false
	if query.Message != nil {
		message := *query.Message
		message.From = &query.From
		accepted = r.allowed(message)
	}
	if !accepted {
		_ = client.AnswerCallbackQuery(ctx, query.ID, "That result is unavailable for this account.")
		return
	}
	entry, view, ok := r.results.take(query, time.Now())
	if !ok {
		_ = client.AnswerCallbackQuery(ctx, query.ID, "That button expired, was already used, or belongs to another request.")
		return
	}
	_ = client.AnswerCallbackQuery(ctx, query.ID, "Opening retained result…")
	var err error
	if view == "download" {
		err = client.SendDocument(ctx, entry.owner.Chat.ID, []byte(entry.text), "mauler-report.md", telegram.MessageOptions{ThreadID: entry.owner.MessageThreadID, ReplyToMessageID: entry.messageID})
	} else {
		text := entry.text
		if view == "sources" {
			text = entry.sources
		}
		_, err = client.SendMessageWithOptions(ctx, entry.owner.Chat.ID, text, telegram.MessageOptions{ThreadID: entry.owner.MessageThreadID, ReplyToMessageID: entry.messageID})
	}
	if err != nil && r.app != nil {
		r.recordTelegramSendEvent(entry.owner, "result_"+view, "error", 0, entry.messageID, "", err.Error())
	}
}

func (r *telegramRuntime) sendResult(ctx context.Context, owner telegram.Message, text, sources string, existingID int64) (int64, error) {
	client, ok := r.client.(telegramRichAPI)
	if !ok {
		return r.client.SendMessage(ctx, owner.Chat.ID, text)
	}
	preview, key, options := r.results.prepare(owner, text, sources, time.Now())
	var id int64
	var err error
	if existingID > 0 && len(telegram.SplitTelegramHTML(telegram.TelegramHTMLFromMarkdownish(preview))) == 1 {
		err = client.EditMessageWithOptions(ctx, owner.Chat.ID, existingID, preview, options)
		id = existingID
		if telegram.IsNotModified(err) {
			err = nil
		}
		if telegram.IsMissingMessage(err) {
			id, err = client.SendMessageWithOptions(ctx, owner.Chat.ID, preview, options)
		}
	} else {
		id, err = client.SendMessageWithOptions(ctx, owner.Chat.ID, preview, options)
	}
	if err == nil {
		r.results.bind(key, id)
	} else {
		r.results.discard(key)
	}
	return id, err
}

func (r *telegramRuntime) trackProgressEnvelope(env ChannelEnvelope, task string) {
	message, ok := progressOwnerFromEnvelope(env)
	if !ok {
		return
	}
	r.trackProgressChat(message.Chat.ID, task)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.progressOwners == nil {
		r.progressOwners = map[int64]telegram.Message{}
	}
	if r.progressClaimants == nil {
		r.progressClaimants = map[int64]string{}
	}
	r.progressOwners[message.Chat.ID] = message
	claimant, _, _ := channelRunClaimant(env)
	r.progressClaimants[message.Chat.ID] = claimant
}

func telegramSessionID(chatID, threadID int64) string {
	session := fmt.Sprintf("telegram:%d", chatID)
	if threadID != 0 {
		session += fmt.Sprintf(":thread:%d", threadID)
	}
	return session
}

func (a *App) rememberTelegramRunSources(run TaskRun) {
	if run.Origin != "telegram" {
		return
	}
	a.telegramMu.Lock()
	runtime := a.telegramService
	a.telegramMu.Unlock()
	if runtime == nil {
		return
	}
	sources := telegramObservedSources(run)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.progressSources == nil {
		runtime.progressSources = map[int64]string{}
	}
	for chat, claimant := range runtime.progressClaimants {
		if claimant != "" && claimant == run.ClaimantID {
			runtime.progressSources[chat] = sources
		}
	}
}

func progressOwnerFromEnvelope(env ChannelEnvelope) (telegram.Message, bool) {
	chatID, e := strconv.ParseInt(env.Metadata["chat_id"], 10, 64)
	if e != nil {
		return telegram.Message{}, false
	}
	userID, e := strconv.ParseInt(env.UserID, 10, 64)
	if e != nil {
		return telegram.Message{}, false
	}
	messageID, _ := strconv.ParseInt(env.Metadata["message_id"], 10, 64)
	threadID, _ := strconv.ParseInt(env.Metadata["message_thread_id"], 10, 64)
	return telegram.Message{MessageID: messageID, MessageThreadID: threadID, From: &telegram.User{ID: userID}, Chat: telegram.Chat{ID: chatID, Type: env.Metadata["chat_type"]}}, true
}

var observedURLLine = regexp.MustCompile(`(?m)^\s*URL:\s*(https?://\S+)`)

func telegramObservedSources(run TaskRun) string {
	var entries []string
	seen := map[string]bool{}
	add := func(raw, label string) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || len(raw) > 2048 || u.Scheme != "http" && u.Scheme != "https" {
			return
		}
		for key := range u.Query() {
			if strings.Contains(strings.ToLower(key), "key") || strings.Contains(strings.ToLower(key), "token") || strings.Contains(strings.ToLower(key), "secret") || strings.Contains(strings.ToLower(key), "password") {
				return
			}
		}
		u.Fragment = ""
		value := u.String()
		if !seen[value] && len(entries) < 12 {
			seen[value] = true
			entries = append(entries, fmt.Sprintf("%d. %s\n%s", len(entries)+1, label, value))
		}
	}
	for _, tool := range run.Tools {
		if tool.Status != "done" {
			continue
		}
		if tool.Name == "web_search" {
			label := "Search result — article not read"
			if strings.HasPrefix(tool.Result, "# Publisher headlines\n") {
				label = "Publisher feed entry — article body not read"
			}
			for _, match := range observedURLLine.FindAllStringSubmatch(tool.Result, 12) {
				add(match[1], label)
			}
		}
		if tool.Name == "fetch_url" || tool.Name == "http_probe" {
			var input map[string]any
			if json.Unmarshal([]byte(tool.Input), &input) == nil {
				if raw, ok := input["url"].(string); ok {
					add(raw, "Page requested by the web tool — not independent verification")
				}
			}
		}
	}
	if len(entries) == 0 {
		return ""
	}
	return "**Sources observed in this run**\n\n" + strings.Join(entries, "\n\n") + "\n\nSearch results and requested pages are not independent verification of every claim."
}
