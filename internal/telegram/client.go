package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	token       string
	baseURL     string
	fileBaseURL string
	httpClient  *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token:       strings.TrimSpace(token),
		baseURL:     "https://api.telegram.org",
		fileBaseURL: "https://api.telegram.org/file",
		httpClient:  &http.Client{Timeout: 35 * time.Second},
	}
}

func (c *Client) WithBaseURL(baseURL string) *Client {
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

func (c *Client) WithFileBaseURL(baseURL string) *Client {
	c.fileBaseURL = strings.TrimRight(baseURL, "/")
	return c
}

func (c *Client) WithHTTPClient(client *http.Client) *Client {
	if client != nil {
		c.httpClient = client
	}
	return c
}

func (c *Client) methodURL(method string) string {
	return strings.TrimRight(c.baseURL, "/") + "/bot" + c.token + "/" + strings.TrimLeft(method, "/")
}

func (c *Client) fileURL(path string) string {
	return strings.TrimRight(c.fileBaseURL, "/") + "/bot" + c.token + "/" + strings.TrimLeft(path, "/")
}

func (c *Client) GetMe(ctx context.Context) (User, error) {
	var out APIResponse[User]
	if err := c.doJSON(ctx, http.MethodGet, "getMe", nil, &out); err != nil {
		return User{}, err
	}
	if !out.OK {
		return User{}, fmt.Errorf("getMe failed: %s", out.Description)
	}
	return out.Result, nil
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	if timeoutSec <= 0 {
		timeoutSec = 25
	}
	payload := map[string]any{
		"offset":          offset,
		"timeout":         timeoutSec,
		"allowed_updates": []string{"message", "callback_query", "channel_post"},
	}
	var out APIResponse[[]Update]
	if err := c.doJSON(ctx, http.MethodPost, "getUpdates", payload, &out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("getUpdates failed: %s", out.Description)
	}
	return out.Result, nil
}

func (c *Client) DeleteWebhook(ctx context.Context, dropPendingUpdates bool) error {
	payload := map[string]any{"drop_pending_updates": dropPendingUpdates}
	var out APIResponse[json.RawMessage]
	if err := c.doJSON(ctx, http.MethodPost, "deleteWebhook", payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("deleteWebhook failed: %s", out.Description)
	}
	return nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) (int64, error) {
	return c.SendMessageWithOptions(ctx, chatID, text, MessageOptions{})
}

func (c *Client) EditMessage(ctx context.Context, chatID, messageID int64, text string) error {
	return c.EditMessageWithOptions(ctx, chatID, messageID, text, MessageOptions{})
}

func (c *Client) DeleteMessage(ctx context.Context, chatID, messageID int64) error {
	payload := map[string]any{"chat_id": chatID, "message_id": messageID}
	var out APIResponse[json.RawMessage]
	if err := c.doJSON(ctx, http.MethodPost, "deleteMessage", payload, &out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("deleteMessage failed: %s", out.Description)
	}
	return nil
}

func (c *Client) GetFile(ctx context.Context, fileID string) (File, error) {
	payload := map[string]any{"file_id": fileID}
	var out APIResponse[File]
	if err := c.doJSON(ctx, http.MethodPost, "getFile", payload, &out); err != nil {
		return File{}, err
	}
	if !out.OK {
		return File{}, fmt.Errorf("getFile failed: %s", out.Description)
	}
	return out.Result, nil
}

func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.fileURL(filePath), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, c.transportError("downloadFile", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("download file HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) SendVoice(ctx context.Context, chatID int64, data []byte, filename, mimeType string) error {
	return c.sendMultipartFile(ctx, "sendVoice", "voice", chatID, data, filename, mimeType)
}

func (c *Client) SendAudio(ctx context.Context, chatID int64, data []byte, filename, mimeType string) error {
	return c.sendMultipartFile(ctx, "sendAudio", "audio", chatID, data, filename, mimeType)
}

func (c *Client) sendMessageChunk(ctx context.Context, chatID int64, text string) (int64, error) {
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	var out APIResponse[SentMessage]
	if err := c.doJSON(ctx, http.MethodPost, "sendMessage", payload, &out); err != nil {
		return 0, err
	}
	if !out.OK {
		return 0, fmt.Errorf("sendMessage failed: %s", out.Description)
	}
	return out.Result.MessageID, nil
}

func (c *Client) doJSON(ctx context.Context, method, apiMethod string, payload any, out any) error {
	return c.requestJSON(ctx, method, apiMethod, payload, out)
}

func (c *Client) sendMultipartFile(ctx context.Context, apiMethod, field string, chatID int64, data []byte, filename, mimeType string) error {
	return c.sendMultipartFileOptions(ctx, apiMethod, field, chatID, data, filename, mimeType, MessageOptions{})
}

func (c *Client) sendMultipartFileOptions(ctx context.Context, apiMethod, field string, chatID int64, data []byte, filename, mimeType string, options MessageOptions) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", fmt.Sprintf("%d", chatID))
	if options.ThreadID != 0 {
		_ = writer.WriteField("message_thread_id", fmt.Sprintf("%d", options.ThreadID))
	}
	if options.ReplyToMessageID != 0 {
		reply, _ := json.Marshal(map[string]any{"message_id": options.ReplyToMessageID, "allow_sending_without_reply": true})
		_ = writer.WriteField("reply_parameters", string(reply))
	}
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if mimeType != "" {
		_ = writer.WriteField("mime_type", mimeType)
	}
	if err := writer.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(apiMethod), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.transportError(apiMethod, err)
	}
	defer resp.Body.Close()
	var out APIResponse[json.RawMessage]
	dataOut, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil || len(dataOut) > 2*1024*1024 {
		return &APIError{Method: apiMethod, Description: "unconfirmed multipart response", Uncertain: true}
	}
	if err := json.Unmarshal(dataOut, &out); err != nil {
		return &APIError{Method: apiMethod, Description: "unconfirmed malformed multipart response", Uncertain: true}
	}
	if resp.StatusCode >= 400 || !out.OK {
		code := out.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &APIError{Method: apiMethod, Code: code, Description: c.redact(out.Description), Uncertain: code >= 500}
	}
	return nil
}
