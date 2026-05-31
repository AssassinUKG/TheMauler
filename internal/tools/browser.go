package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

var sharedBrowser = &browserSession{}

type browserSession struct {
	mu          sync.Mutex
	allocCtx    context.Context
	allocCancel context.CancelFunc
	ctx         context.Context
	cancel      context.CancelFunc
}

func (s *browserSession) ensure(parent context.Context) (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil && s.ctx.Err() == nil {
		return s.ctx, nil
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
	)
	s.allocCtx, s.allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	s.ctx, s.cancel = chromedp.NewContext(s.allocCtx)
	if parent.Err() != nil {
		s.closeLocked()
		return nil, parent.Err()
	}
	return s.ctx, nil
}

func (s *browserSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

func (s *browserSession) closeLocked() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}
	s.ctx = nil
	s.cancel = nil
	s.allocCtx = nil
	s.allocCancel = nil
}

func runBrowser(parent context.Context, actions ...chromedp.Action) error {
	ctx, err := sharedBrowser.ensure(parent)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return chromedp.Run(runCtx, actions...)
}

// BrowserOpen opens a page in the local browser automation session.
type BrowserOpen struct{}

func (t *BrowserOpen) Name() string      { return "browser_open" }
func (t *BrowserOpen) Destructive() bool { return false }
func (t *BrowserOpen) Description() string {
	return "Open a URL in a local headless browser when web_search/fetch_url are insufficient, such as JS-heavy pages, forms, or GitHub/doc navigation."
}
func (t *BrowserOpen) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "HTTP or HTTPS URL to open"}
  },
  "required": ["url"],
  "additionalProperties": false
}`)
}
func (t *BrowserOpen) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_open: bad params: %w", err)
	}
	u := strings.TrimSpace(p.URL)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("browser_open: url must be http or https")
	}
	if err := runBrowser(ctx, chromedp.Navigate(u), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		return "", err
	}
	return "opened " + u, nil
}

// BrowserSnapshot returns the current page title, URL, and visible text.
type BrowserSnapshot struct{}

func (t *BrowserSnapshot) Name() string      { return "browser_snapshot" }
func (t *BrowserSnapshot) Destructive() bool { return false }
func (t *BrowserSnapshot) Description() string {
	return "Return the current browser page title, URL, and visible body text. Use this before choosing selectors to click/type."
}
func (t *BrowserSnapshot) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "max_chars": {"type": "integer", "description": "Maximum visible text characters, default 8000, max 20000"}
  },
  "additionalProperties": false
}`)
}
func (t *BrowserSnapshot) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		MaxChars int `json:"max_chars"`
	}
	_ = json.Unmarshal(raw, &p)
	maxChars := p.MaxChars
	if maxChars <= 0 {
		maxChars = 8000
	}
	if maxChars > 20000 {
		maxChars = 20000
	}
	var title, loc, body string
	if err := runBrowser(ctx, chromedp.Title(&title), chromedp.Location(&loc), chromedp.Text("body", &body, chromedp.ByQuery)); err != nil {
		return "", err
	}
	body = cleanText(body)
	if len(body) > maxChars {
		body = body[:maxChars] + "\n[truncated]"
	}
	return fmt.Sprintf("# Browser snapshot\nTitle: %s\nURL: %s\n\n%s", title, loc, body), nil
}

// BrowserClick clicks an element matching a CSS selector.
type BrowserClick struct{}

func (t *BrowserClick) Name() string      { return "browser_click" }
func (t *BrowserClick) Destructive() bool { return false }
func (t *BrowserClick) Description() string {
	return "Click a visible element by CSS selector in the current browser page."
}
func (t *BrowserClick) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "selector": {"type": "string", "description": "CSS selector to click"}
  },
  "required": ["selector"],
  "additionalProperties": false
}`)
}
func (t *BrowserClick) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_click: bad params: %w", err)
	}
	selector := strings.TrimSpace(p.Selector)
	if selector == "" {
		return "", fmt.Errorf("browser_click: selector is required")
	}
	if err := runBrowser(ctx, chromedp.Click(selector, chromedp.ByQuery), chromedp.Sleep(500*time.Millisecond)); err != nil {
		return "", err
	}
	return "clicked " + selector, nil
}

// BrowserType types into an element matching a CSS selector.
type BrowserType struct{}

func (t *BrowserType) Name() string      { return "browser_type" }
func (t *BrowserType) Destructive() bool { return false }
func (t *BrowserType) Description() string {
	return "Clear and type text into a CSS selector in the current browser page, optionally submitting with Enter."
}
func (t *BrowserType) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "selector": {"type": "string", "description": "CSS selector to type into"},
    "text": {"type": "string", "description": "Text to type"},
    "submit": {"type": "boolean", "description": "Press Enter after typing"}
  },
  "required": ["selector", "text"],
  "additionalProperties": false
}`)
}
func (t *BrowserType) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Submit   bool   `json:"submit"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_type: bad params: %w", err)
	}
	selector := strings.TrimSpace(p.Selector)
	if selector == "" {
		return "", fmt.Errorf("browser_type: selector is required")
	}
	actions := []chromedp.Action{
		chromedp.SetValue(selector, "", chromedp.ByQuery),
		chromedp.SendKeys(selector, p.Text, chromedp.ByQuery),
	}
	if p.Submit {
		actions = append(actions, chromedp.SendKeys(selector, "\n", chromedp.ByQuery))
	}
	actions = append(actions, chromedp.Sleep(500*time.Millisecond))
	if err := runBrowser(ctx, actions...); err != nil {
		return "", err
	}
	return "typed into " + selector, nil
}

// BrowserExtract extracts text from a CSS selector, or body when omitted.
type BrowserExtract struct{}

func (t *BrowserExtract) Name() string      { return "browser_extract" }
func (t *BrowserExtract) Destructive() bool { return false }
func (t *BrowserExtract) Description() string {
	return "Extract visible text from a CSS selector on the current browser page, or body when selector is omitted."
}
func (t *BrowserExtract) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "selector": {"type": "string", "description": "CSS selector to extract from, default body"},
    "max_chars": {"type": "integer", "description": "Maximum characters, default 12000, max 30000"}
  },
  "additionalProperties": false
}`)
}
func (t *BrowserExtract) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Selector string `json:"selector"`
		MaxChars int    `json:"max_chars"`
	}
	_ = json.Unmarshal(raw, &p)
	selector := strings.TrimSpace(p.Selector)
	if selector == "" {
		selector = "body"
	}
	maxChars := p.MaxChars
	if maxChars <= 0 {
		maxChars = 12000
	}
	if maxChars > 30000 {
		maxChars = 30000
	}
	var text string
	if err := runBrowser(ctx, chromedp.Text(selector, &text, chromedp.ByQuery)); err != nil {
		return "", err
	}
	text = cleanText(text)
	if len(text) > maxChars {
		text = text[:maxChars] + "\n[truncated]"
	}
	return fmt.Sprintf("# Extracted %s\n\n%s", selector, text), nil
}

// BrowserScreenshot saves a full-page screenshot and returns the path.
type BrowserScreenshot struct{}

func (t *BrowserScreenshot) Name() string      { return "browser_screenshot" }
func (t *BrowserScreenshot) Destructive() bool { return false }
func (t *BrowserScreenshot) Description() string {
	return "Save a full-page browser screenshot to a temporary PNG and return its local path."
}
func (t *BrowserScreenshot) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`)
}
func (t *BrowserScreenshot) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var buf []byte
	if err := runBrowser(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("themauler-browser-%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return "", err
	}
	return "screenshot saved: " + path, nil
}

// BrowserClose closes the browser automation session.
type BrowserClose struct{}

func (t *BrowserClose) Name() string      { return "browser_close" }
func (t *BrowserClose) Destructive() bool { return false }
func (t *BrowserClose) Description() string {
	return "Close the current browser automation session."
}
func (t *BrowserClose) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`)
}
func (t *BrowserClose) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	sharedBrowser.close()
	return "browser closed", nil
}
