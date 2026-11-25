package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

func TestRichFormattingSplitsBalancedUnicodeWithoutLosingText(t *testing.T) {
	original := strings.Repeat("🙂 & <tag> 'quoted' \"double\"\n", 900)
	encoded := "<pre>" + html.EscapeString(original) + "</pre>"
	chunks := SplitTelegramHTML(encoded)
	if len(chunks) < 3 {
		t.Fatalf("expected multipart: %d", len(chunks))
	}
	var joined strings.Builder
	tags := regexp.MustCompile(`<[^>]*>`)
	for _, part := range chunks {
		if _, err := splitBalancedHTML(part, telegramChunkLimit); err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(part) {
			t.Fatal("split a Unicode character")
		}
		plain := html.UnescapeString(tags.ReplaceAllString(part, ""))
		if len(utf16.Encode([]rune(plain))) > telegramChunkLimit {
			t.Fatal("exceeded Telegram UTF-16 budget")
		}
		joined.WriteString(plain)
	}
	if joined.String() != original {
		t.Fatal("long reply lost or duplicated content")
	}
	formatted := TelegramHTMLFromMarkdownish("# News\n[Publisher](https://example.test/news?a=1&b=2) **reported** `x < y`\n<script>bad</script>\n[Bad](https://password@example.test/)")
	if !strings.Contains(formatted, "<b>News</b>") || !strings.Contains(formatted, `<a href="https://example.test/news?a=1&amp;b=2">Publisher</a>`) || !strings.Contains(formatted, "<code>x &lt; y</code>") || strings.Contains(formatted, "<script>") || strings.Contains(formatted, `<a href="https://password@`) {
		t.Fatalf("unsafe or incomplete formatting: %s", formatted)
	}
	if _, err := splitBalancedHTML(formatted, telegramChunkLimit); err != nil {
		t.Fatal(err)
	}
}

func TestSendNeverRetriesAmbiguousAcceptanceAndRedactsToken(t *testing.T) {
	for _, kind := range []string{"disconnect", "server_error", "malformed", "missing_ack"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch kind {
				case "disconnect":
					c, _, _ := w.(http.Hijacker).Hijack()
					_ = c.Close()
				case "server_error":
					w.WriteHeader(502)
					_, _ = io.WriteString(w, `{"ok":false,"error_code":502,"description":"botprivate-token failed"}`)
				case "malformed":
					_, _ = io.WriteString(w, "not a Bot API acknowledgement")
				case "missing_ack":
					_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
				}
			}))
			defer server.Close()
			client := NewClient("private-token").WithBaseURL(server.URL)
			_, err := client.SendMessage(context.Background(), 1, "One result")
			if !IsUncertain(err) || calls.Load() != 1 {
				t.Fatalf("err=%v calls=%d", err, calls.Load())
			}
			if strings.Contains(err.Error(), "private-token") {
				t.Fatal("token leaked into error")
			}
		})
	}
}

func TestExplicitFloodControlRetryIsBoundedAndCancellable(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":12}}`)
	}))
	defer server.Close()
	client := NewClient("test").WithBaseURL(server.URL)
	if id, err := client.SendMessage(context.Background(), 1, "Result"); err != nil || id != 12 || calls.Load() != 2 {
		t.Fatalf("id=%d err=%v calls=%d", id, err, calls.Load())
	}
	calls.Store(0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := client.SendMessage(ctx, 1, "Result")
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || !IsUncertain(err) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestMessageOptionsAndExactMarkdownDownload(t *testing.T) {
	original := []byte("# Full report\n\n" + strings.Repeat("🙂 original & complete\n", 200))
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendDocument"):
			if err := r.ParseMultipartForm(128 * 1024); err != nil {
				t.Error(err)
				return
			}
			file, header, err := r.FormFile("document")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if string(data) != string(original) || header.Filename != "mauler-report.md" || r.FormValue("message_thread_id") != "8" {
				t.Error("download bytes or destination changed")
			}
		default:
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["message_thread_id"] != float64(8) || payload["reply_parameters"] == nil || payload["reply_markup"] == nil {
				t.Errorf("missing thread/reply/buttons: %v", payload)
			}
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":12}}`)
	}))
	defer server.Close()
	client := NewClient("test").WithBaseURL(server.URL)
	options := MessageOptions{ThreadID: 8, ReplyToMessageID: 3, ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Full", CallbackData: "nonce"}}}}}
	if _, err := client.SendMessageWithOptions(context.Background(), 1, "Preview", options); err != nil {
		t.Fatal(err)
	}
	if err := client.SendDocument(context.Background(), 1, original, "mauler-report.md", options); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
