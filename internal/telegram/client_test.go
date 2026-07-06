package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientGetUpdatesAndSendMessage(t *testing.T) {
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			_ = json.NewEncoder(w).Encode(APIResponse[[]Update]{
				OK: true,
				Result: []Update{{
					UpdateID: 42,
					Message:  &Message{MessageID: 7, Chat: Chat{ID: 123, Type: "private"}, Text: "/status"},
				}},
			})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Fatalf("decode sendMessage: %v", err)
			}
			_ = json.NewEncoder(w).Encode(APIResponse[SentMessage]{OK: true, Result: SentMessage{MessageID: 99, Chat: Chat{ID: 123}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient("TOKEN").WithBaseURL(server.URL)
	updates, err := client.GetUpdates(context.Background(), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Message.Text != "/status" {
		t.Fatalf("unexpected updates: %+v", updates)
	}
	msgID, err := client.SendMessage(context.Background(), 123, "**hello** <world>")
	if err != nil {
		t.Fatal(err)
	}
	if msgID != 99 {
		t.Fatalf("message id = %d", msgID)
	}
	if sent["parse_mode"] != "HTML" || !strings.Contains(sent["text"].(string), "<b>hello</b>") || strings.Contains(sent["text"].(string), "<world>") {
		t.Fatalf("message not formatted safely: %#v", sent)
	}
}

func TestClientDownloadsTelegramFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			_ = json.NewEncoder(w).Encode(APIResponse[File]{OK: true, Result: File{FileID: "voice1", FilePath: "voice/file.ogg"}})
		case strings.HasSuffix(r.URL.Path, "/voice/file.ogg"):
			_, _ = w.Write([]byte("audio-bytes"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient("TOKEN").WithBaseURL(server.URL).WithFileBaseURL(server.URL)
	file, err := client.GetFile(context.Background(), "voice1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.DownloadFile(context.Background(), file.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "audio-bytes" {
		t.Fatalf("download = %q", string(data))
	}
}
