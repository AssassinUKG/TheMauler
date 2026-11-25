package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mauler/internal/settings"
	"mauler/internal/telegram"
)

func resultOwner() telegram.Message {
	return telegram.Message{MessageID: 3, MessageThreadID: 8, From: &telegram.User{ID: 100}, Chat: telegram.Chat{ID: -200, Type: "supergroup"}}
}

func TestTelegramResultOwnershipExpiryAndOneShotViews(t *testing.T) {
	var store telegramResultStore
	now := time.Now()
	original := strings.Repeat("Complete result 🙂\n", 200)
	preview, key, options := store.prepare(resultOwner(), original, "Observed source", now)
	if key == "" || !strings.Contains(preview, "Reply preview") || len(options.ReplyMarkup.InlineKeyboard[0]) != 3 {
		t.Fatal("missing result presentation")
	}
	owner := resultOwner()
	owner.MessageID = 99
	query := telegram.CallbackQuery{From: *owner.From, Message: &owner, Data: "mm:" + key + ":full"}
	if _, _, ok := store.take(query, now); ok {
		t.Fatal("unacknowledged message granted access")
	}
	store.bind(key, 99)
	for _, mutate := range []func(*telegram.CallbackQuery){
		func(q *telegram.CallbackQuery) { q.From.ID++ },
		func(q *telegram.CallbackQuery) { q.Message.Chat.ID++ },
		func(q *telegram.CallbackQuery) { q.Message.MessageThreadID++ },
		func(q *telegram.CallbackQuery) { q.Message.MessageID++ },
		func(q *telegram.CallbackQuery) { q.Message = nil },
		func(q *telegram.CallbackQuery) { q.Data = "mm:" + key + ":execute" },
	} {
		copy := query
		message := *query.Message
		copy.Message = &message
		mutate(&copy)
		if _, _, ok := store.take(copy, now); ok {
			t.Fatal("foreign result callback admitted")
		}
	}
	entry, view, ok := store.take(query, now)
	if !ok || view != "full" || entry.text != original {
		t.Fatal("wrong retained result")
	}
	if _, _, ok := store.take(query, now); ok {
		t.Fatal("replayed callback")
	}
	query.Data = "mm:" + key + ":download"
	if _, _, ok := store.take(query, now); !ok {
		t.Fatal("other read-only view incorrectly consumed")
	}
	query.Data = "mm:" + key + ":sources"
	if _, _, ok := store.take(query, now.Add(16*time.Minute)); ok {
		t.Fatal("expired callback")
	}
}

func TestTelegramPreviewNeverHidesAnUnavailableFullResult(t *testing.T) {
	var store telegramResultStore
	for _, owner := range []telegram.Message{{}, resultOwner()} {
		text := strings.Repeat("large", 15000)
		preview, key, options := store.prepare(owner, text, "", time.Now())
		if preview != text || key != "" || options.ReplyMarkup != nil {
			t.Fatal("unavailable full result was hidden")
		}
	}
	owner := resultOwner()
	for i := 0; i < 128; i++ {
		_, key, _ := store.prepare(owner, strings.Repeat("x", 1900), "", time.Now())
		if key == "" {
			t.Fatal(i)
		}
	}
	full := strings.Repeat("bounded but uncached", 200)
	preview, key, _ := store.prepare(owner, full, "", time.Now())
	if preview != full || key != "" {
		t.Fatal("full cache caused lossy preview")
	}
}

func TestTelegramResultsUseRealTransportAndNeverDispatchWork(t *testing.T) {
	original := "# Complete answer\n\n" + strings.Repeat("Dated headlines 🙂 & original Markdown.\n", 100)
	var methods []string
	var button string
	var fullBody string
	var document []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		methods = append(methods, method)
		if method == "sendDocument" {
			if err := r.ParseMultipartForm(128 * 1024); err != nil {
				t.Error(err)
				return
			}
			f, _, err := r.FormFile("document")
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			document, _ = io.ReadAll(f)
		} else {
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if method == "sendMessage" {
				if markup, ok := payload["reply_markup"].(map[string]any); ok {
					button = markup["inline_keyboard"].([]any)[0].([]any)[0].(map[string]any)["callback_data"].(string)
				} else {
					part, _ := payload["text"].(string)
					fullBody += part
				}
			}
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":99}}`)
	}))
	defer server.Close()
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{AllowFrom: []string{"100"}}, &fakeTelegramAPI{})
	rt.client = telegram.NewClient("fixture").WithBaseURL(server.URL)
	owner := resultOwner()
	if _, err := rt.sendResult(context.Background(), owner, original, "Observed publisher source", 0); err != nil {
		t.Fatal(err)
	}
	message := owner
	message.MessageID = 99
	query := telegram.CallbackQuery{ID: "callback", From: *owner.From, Message: &message, Data: button}
	rt.handleResultCallback(context.Background(), query)
	if !strings.Contains(fullBody, "Dated headlines 🙂 &amp; original Markdown.") || !strings.Contains(fullBody, "Complete answer") {
		t.Fatal("full result missing")
	}
	query.Data = strings.TrimSuffix(button, ":full") + ":download"
	rt.handleResultCallback(context.Background(), query)
	if string(document) != original {
		t.Fatal("download did not retain exact original")
	}
	if strings.Join(methods, ",") != "sendMessage,answerCallbackQuery,sendMessage,sendMessage,answerCallbackQuery,sendDocument" {
		t.Fatal(methods)
	}
}

func TestTelegramObservedSourcesAreToolOwnedAndTruthfullyLabelled(t *testing.T) {
	run := TaskRun{Tools: []TaskToolEvent{
		{Name: "web_search", Status: "done", Result: "1. Publisher\nURL: https://news.example/story#x\nSnippet: Reported"},
		{Name: "web_search", Status: "failed", Result: "URL: https://news.example/failed"},
		{Name: "fetch_url", Status: "done", Input: `{"url":"https://news.example/page"}`},
		{Name: "fetch_url", Status: "done", Input: `{"url":"https://news.example/private?api_key=hidden"}`},
	}}
	sources := telegramObservedSources(run)
	if !strings.Contains(sources, "Search result — article not read") || !strings.Contains(sources, "not independent verification") || strings.Contains(sources, "hidden") || strings.Contains(sources, "/failed") {
		t.Fatal(sources)
	}
}
