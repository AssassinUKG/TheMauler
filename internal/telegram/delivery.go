package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Uncertain means acceptance was not confirmed. A caller must not blindly
// resend a whole message or recreate a status card after such a failure.
type APIError struct {
	Method, Description string
	Code                int
	Uncertain           bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Telegram %s failed (%d): %s", e.Method, e.Code, e.Description)
}
func IsUncertain(err error) bool {
	var api *APIError
	return err != nil && (!errors.As(err, &api) || api.Uncertain)
}
func IsNotModified(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.Code == 400 && strings.Contains(strings.ToLower(api.Description), "message is not modified")
}
func IsMissingMessage(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.Code == 400 && strings.Contains(strings.ToLower(api.Description), "message to edit not found")
}
func (c *Client) redact(text string) string {
	if c.token != "" {
		text = strings.ReplaceAll(text, c.token, "[redacted]")
	}
	if len(text) > 1024 {
		text = text[:1024]
	}
	return text
}
func (c *Client) transportError(method string, err error) error {
	return &APIError{Method: method, Description: c.redact(err.Error()), Uncertain: true}
}

func (c *Client) requestJSON(ctx context.Context, method, apiMethod string, payload any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.methodURL(apiMethod), body)
		if err != nil {
			return c.transportError(apiMethod, err)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := c.httpClient.Do(req)
		if err != nil {
			return c.transportError(apiMethod, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
		_ = response.Body.Close()
		if readErr != nil {
			return c.transportError(apiMethod, readErr)
		}
		if len(raw) > 2*1024*1024 {
			return &APIError{Method: apiMethod, Description: "response exceeded byte limit", Uncertain: true}
		}
		var envelope APIResponse[json.RawMessage]
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return c.transportError(apiMethod, fmt.Errorf("unconfirmed malformed Bot API response"))
		}
		if response.StatusCode == http.StatusTooManyRequests || envelope.ErrorCode == 429 {
			wait := envelope.Parameters.RetryAfter
			if attempt < 2 && wait > 0 && wait <= 30 {
				timer := time.NewTimer(time.Duration(wait) * time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return c.transportError(apiMethod, ctx.Err())
				case <-timer.C:
					continue
				}
			}
		}
		if response.StatusCode >= 400 || !envelope.OK {
			code := envelope.ErrorCode
			if code == 0 {
				code = response.StatusCode
			}
			return &APIError{Method: apiMethod, Code: code, Description: c.redact(envelope.Description), Uncertain: code >= 500}
		}
		return json.Unmarshal(raw, out)
	}
	return &APIError{Method: apiMethod, Code: 429, Description: "flood-control retry limit reached"}
}

func messagePayload(chatID int64, text string, options MessageOptions) map[string]any {
	payload := map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	if options.ThreadID != 0 {
		payload["message_thread_id"] = options.ThreadID
	}
	if options.ReplyToMessageID != 0 {
		payload["reply_parameters"] = map[string]any{"message_id": options.ReplyToMessageID, "allow_sending_without_reply": true}
	}
	if options.ReplyMarkup != nil {
		payload["reply_markup"] = options.ReplyMarkup
	}
	return payload
}

func (c *Client) SendMessageWithOptions(ctx context.Context, chatID int64, text string, options MessageOptions) (int64, error) {
	chunks := SplitTelegramHTML(TelegramHTMLFromMarkdownish(text))
	var first int64
	for index, chunk := range chunks {
		partOptions := options
		if index > 0 {
			partOptions.ReplyMarkup = nil
		}
		var out APIResponse[SentMessage]
		if err := c.doJSON(ctx, http.MethodPost, "sendMessage", messagePayload(chatID, chunk, partOptions), &out); err != nil {
			return first, err
		}
		if out.Result.MessageID <= 0 {
			return first, &APIError{Method: "sendMessage", Description: "message acknowledgement missing", Uncertain: true}
		}
		if index == 0 {
			first = out.Result.MessageID
		}
	}
	return first, nil
}

func (c *Client) EditMessageWithOptions(ctx context.Context, chatID, messageID int64, text string, options MessageOptions) error {
	chunks := SplitTelegramHTML(TelegramHTMLFromMarkdownish(text))
	if len(chunks) != 1 {
		return &APIError{Method: "editMessageText", Code: 400, Description: "cannot edit one message into multiple parts"}
	}
	payload := messagePayload(chatID, chunks[0], options)
	payload["message_id"] = messageID
	delete(payload, "reply_parameters")
	delete(payload, "message_thread_id")
	if options.ReplyMarkup == nil {
		payload["reply_markup"] = InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}
	}
	var out APIResponse[json.RawMessage]
	return c.doJSON(ctx, http.MethodPost, "editMessageText", payload, &out)
}

func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string) error {
	var out APIResponse[json.RawMessage]
	return c.doJSON(ctx, http.MethodPost, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, &out)
}
func (c *Client) SendDocument(ctx context.Context, chatID int64, data []byte, name string, options MessageOptions) error {
	if len(data) > 64*1024 {
		return fmt.Errorf("result document exceeds byte limit")
	}
	return c.sendMultipartFileOptions(ctx, "sendDocument", "document", chatID, data, name, "text/markdown", options)
}
