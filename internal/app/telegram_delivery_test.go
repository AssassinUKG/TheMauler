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

	"mauler/internal/settings"
	"mauler/internal/telegram"
)

func TestTelegramUncertainProgressDoesNotCreateDuplicateCards(t *testing.T) {
	var sends int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends++
		w.WriteHeader(502)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":502,"description":"unconfirmed"}`)
	}))
	defer server.Close()
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: true}, &fakeTelegramAPI{})
	rt.client = telegram.NewClient("fixture").WithBaseURL(server.URL)
	rt.trackProgressChat(42, "news")
	rt.sendTelegramProgressUpdate(context.Background(), 42, "working", "Reading headlines")
	rt.sendTelegramProgressUpdate(context.Background(), 42, "working", "Still reading headlines")
	if sends != 1 {
		t.Fatalf("uncertain initial status was blindly resent: %d", sends)
	}
}

func TestTelegramUncertainEditDoesNotResendFinalAnswer(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.URL.Path)
		w.WriteHeader(502)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":502,"description":"unconfirmed"}`)
	}))
	defer server.Close()
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: true}, &fakeTelegramAPI{})
	rt.client = telegram.NewClient("fixture").WithBaseURL(server.URL)
	owner := resultOwner()
	if _, err := rt.sendResult(context.Background(), owner, strings.Repeat("complete ", 400), "", 99); !telegram.IsUncertain(err) {
		t.Fatal(err)
	}
	if len(methods) != 1 || !strings.HasSuffix(methods[0], "/editMessageText") {
		t.Fatal(methods)
	}
}

func TestTelegramOwnedFinalAnswerEditsSingleCardAndRetiresHeartbeat(t *testing.T) {
	var methods []string
	var final string
	var hasButtons bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		methods = append(methods, method)
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if method == "editMessageText" {
			final, _ = payload["text"].(string)
			hasButtons = payload["reply_markup"] != nil
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":99}}`)
	}))
	defer server.Close()
	rt := newTelegramRuntimeForTest(t, settings.TelegramConfig{SendProgress: true}, &fakeTelegramAPI{})
	rt.client = telegram.NewClient("fixture").WithBaseURL(server.URL)
	owner := resultOwner()
	rt.trackProgressChat(owner.Chat.ID, "latest news")
	rt.progressOwners = map[int64]telegram.Message{owner.Chat.ID: owner}
	rt.sendTelegramProgressUpdate(context.Background(), owner.Chat.ID, "working", "Reading dated publisher headlines")
	rt.notifyRunState(context.Background(), "done", strings.Repeat("Actual dated headline with source link.\n", 100))
	rt.sendTelegramProgressUpdate(context.Background(), owner.Chat.ID, "working", "Stale heartbeat")
	if strings.Join(methods, ",") != "sendMessage,editMessageText" || !hasButtons || !strings.Contains(final, "Reply preview") || strings.Contains(final, "Task:") {
		t.Fatalf("methods=%v final=%s", methods, final)
	}
	if len(rt.progressOwners) != 0 || len(rt.progressChats) != 0 {
		t.Fatal("terminal result retained active progress ownership")
	}
}
