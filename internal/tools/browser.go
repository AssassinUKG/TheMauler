package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	cdptarget "github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

var sharedBrowser = &browserSession{}

type BrowserRuntimeStatus struct {
	Available  bool   `json:"available"`
	Active     bool   `json:"active"`
	Visible    bool   `json:"visible"`
	Paused     bool   `json:"paused"`
	Binary     string `json:"binary"`
	Owner      string `json:"owner,omitempty"`
	State      string `json:"state"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	LastAction string `json:"last_action,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	ActiveTab  string `json:"active_tab,omitempty"`
	TabCount   int    `json:"tab_count,omitempty"`
}

func GetBrowserRuntimeStatus() BrowserRuntimeStatus {
	return sharedBrowser.status("")
}

func GetBrowserWorkflowStatus(owner string) BrowserRuntimeStatus {
	return sharedBrowser.status(strings.TrimSpace(owner))
}

func detectBrowserBinary() string {
	for _, candidate := range []string{
		"chrome", "chrome.exe", "msedge", "msedge.exe",
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

type browserSessionOwnerKey struct{}
type browserWorkspaceRootKey struct{}
type browserHandoffObserverKey struct{}

// BrowserHandoffObserver lets the application publish a run-owned takeover
// card without coupling the tools package to Wails or descriptive run state.
type BrowserHandoffObserver func(BrowserRuntimeStatus)

// WithBrowserSessionOwner scopes native browser cookies/page state to one
// Mauler conversation workflow. The owner is controller-generated, never
// model-provided.
func WithBrowserSessionOwner(ctx context.Context, owner string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, browserSessionOwnerKey{}, strings.TrimSpace(owner))
}

// WithBrowserWorkspaceRoot scopes browser-created and browser-supplied files to
// the authoritative Mauler workspace rather than the process working directory.
func WithBrowserWorkspaceRoot(ctx context.Context, root string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, browserWorkspaceRootKey{}, strings.TrimSpace(root))
}

func WithBrowserHandoffObserver(ctx context.Context, observer BrowserHandoffObserver) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, browserHandoffObserverKey{}, observer)
}

func browserWorkspaceRoot(ctx context.Context) string {
	if ctx != nil {
		if root, ok := ctx.Value(browserWorkspaceRootKey{}).(string); ok && strings.TrimSpace(root) != "" {
			return strings.TrimSpace(root)
		}
	}
	root, _ := os.Getwd()
	return root
}

func notifyBrowserHandoff(ctx context.Context, status BrowserRuntimeStatus) {
	if ctx == nil {
		return
	}
	if observer, ok := ctx.Value(browserHandoffObserverKey{}).(BrowserHandoffObserver); ok && observer != nil {
		observer(status)
	}
}

func browserSessionOwner(ctx context.Context) string {
	if ctx != nil {
		if owner, ok := ctx.Value(browserSessionOwnerKey{}).(string); ok && strings.TrimSpace(owner) != "" {
			return strings.TrimSpace(owner)
		}
	}
	return "legacy"
}

func (s *browserSession) status(owner string) BrowserRuntimeStatus {
	binary := detectBrowserBinary()
	status := BrowserRuntimeStatus{Available: binary != "", Binary: binary, State: "idle"}
	s.mu.Lock()
	status.Active = s.ctx != nil && s.ctx.Err() == nil
	status.Visible = s.visible
	status.Paused = s.paused
	status.Owner = s.owner
	status.URL = s.url
	status.Title = s.title
	status.LastAction = s.lastAction
	status.LastError = s.lastError
	status.UpdatedAt = s.updatedAt
	status.ActiveTab = s.currentTab
	status.TabCount = len(s.tabs)
	if s.state != "" {
		status.State = s.state
	}
	s.mu.Unlock()
	if owner != "" && status.Active && status.Owner != "" && status.Owner != owner {
		status.State = "owned_elsewhere"
	}
	return status
}

type browserSession struct {
	mu           sync.Mutex
	actionMu     sync.Mutex
	allocCtx     context.Context
	allocCancel  context.CancelFunc
	ctx          context.Context
	rootCtx      context.Context
	cancel       context.CancelFunc
	owner        string
	visible      bool
	paused       bool
	state        string
	url          string
	title        string
	lastAction   string
	lastError    string
	updatedAt    string
	stateChanged chan struct{}
	elements     map[string]string
	tabs         map[string]cdptarget.ID
	tabContexts  map[string]context.Context
	tabCancels   map[string]context.CancelFunc
	currentTab   string
	nextTab      int
}

func (s *browserSession) signalLocked() {
	if s.stateChanged != nil {
		close(s.stateChanged)
	}
	s.stateChanged = make(chan struct{})
}

func (s *browserSession) clearElementsLocked() {
	s.elements = nil
}

func (s *browserSession) ensure(parent context.Context, owner string, visible bool) (context.Context, error) {
	if parent == nil {
		parent = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil && s.ctx.Err() == nil {
		if s.owner != "" && s.owner != owner {
			return nil, fmt.Errorf("browser session is owned by another Mauler workflow; stop it before starting a different conversation so cookies and credentials cannot mix")
		}
		if visible && !s.visible {
			return nil, fmt.Errorf("current browser session is headless; stop it and open a visible session before human takeover")
		}
		return s.ctx, nil
	}
	binary := detectBrowserBinary()
	if binary == "" {
		s.state = "unavailable"
		s.lastError = "Chrome or Edge executable was not found"
		s.updatedAt = time.Now().Format(time.RFC3339)
		return nil, fmt.Errorf("browser executable missing: install Google Chrome or Microsoft Edge")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(binary),
		chromedp.Flag("headless", !visible),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
	)
	s.allocCtx, s.allocCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	s.ctx, s.cancel = chromedp.NewContext(s.allocCtx)
	s.owner = owner
	s.visible = visible
	s.paused = false
	s.state = "starting"
	s.lastError = ""
	s.updatedAt = time.Now().Format(time.RFC3339)
	s.clearElementsLocked()
	s.signalLocked()
	if parent.Err() != nil {
		s.closeLocked()
		return nil, parent.Err()
	}
	// Initialise the browser and target against the persistent session context.
	// If the first navigation did this through its per-action timeout, cancelling
	// that action would also tear down Chromium and every later action would see
	// only context.Canceled.
	if err := chromedp.Run(s.ctx); err != nil {
		s.state = "failed"
		s.lastError = err.Error()
		s.updatedAt = time.Now().Format(time.RFC3339)
		s.closeLocked()
		return nil, fmt.Errorf("start browser session: %w", err)
	}
	s.rootCtx = s.ctx
	s.tabs = map[string]cdptarget.ID{}
	s.tabContexts = map[string]context.Context{}
	s.tabCancels = map[string]context.CancelFunc{}
	if browserContext := chromedp.FromContext(s.ctx); browserContext != nil && browserContext.Target != nil {
		s.tabs["t1"] = browserContext.Target.TargetID
		s.tabContexts["t1"] = s.ctx
		s.currentTab = "t1"
		s.nextTab = 2
	}
	return s.ctx, nil
}

func (s *browserSession) close() {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
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
	s.rootCtx = nil
	s.cancel = nil
	s.allocCtx = nil
	s.allocCancel = nil
	s.owner = ""
	s.visible = false
	s.paused = false
	s.state = "idle"
	s.url = ""
	s.title = ""
	s.lastAction = "close"
	s.lastError = ""
	s.updatedAt = time.Now().Format(time.RFC3339)
	s.clearElementsLocked()
	s.tabs = nil
	s.tabContexts = nil
	s.tabCancels = nil
	s.currentTab = ""
	s.nextTab = 0
	s.signalLocked()
}

func runBrowser(parent context.Context, actions ...chromedp.Action) error {
	_, err := sharedBrowser.run(parent, "browser action", false, actions...)
	return err
}

func (s *browserSession) run(parent context.Context, action string, visible bool, actions ...chromedp.Action) (BrowserRuntimeStatus, error) {
	s.actionMu.Lock()
	defer s.actionMu.Unlock()
	owner := browserSessionOwner(parent)
	ctx, err := s.ensure(parent, owner, visible)
	if err != nil {
		return s.status(owner), err
	}
	s.mu.Lock()
	if s.paused {
		s.mu.Unlock()
		return s.status(owner), fmt.Errorf("browser workflow is paused for user takeover; choose ‘I’ve completed this—continue’ before automated interaction resumes")
	}
	s.state = "running"
	s.lastAction = action
	s.lastError = ""
	s.updatedAt = time.Now().Format(time.RFC3339)
	s.signalLocked()
	s.mu.Unlock()
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	stopParentCancel := context.AfterFunc(parent, cancel)
	defer stopParentCancel()
	var title, loc string
	actions = append(actions, chromedp.Title(&title), chromedp.Location(&loc))
	err = chromedp.Run(runCtx, actions...)
	s.mu.Lock()
	s.title = title
	s.url = loc
	s.updatedAt = time.Now().Format(time.RFC3339)
	if err != nil {
		s.state = "failed"
		s.lastError = err.Error()
	} else {
		s.state = "ready"
	}
	s.signalLocked()
	s.mu.Unlock()
	return s.status(owner), err
}

func PauseBrowserWorkflow(owner string, takeover bool) (BrowserRuntimeStatus, error) {
	sharedBrowser.actionMu.Lock()
	defer sharedBrowser.actionMu.Unlock()
	sharedBrowser.mu.Lock()
	defer sharedBrowser.mu.Unlock()
	if sharedBrowser.ctx == nil || sharedBrowser.ctx.Err() != nil {
		return sharedBrowser.statusLocked(), fmt.Errorf("no active browser workflow to pause")
	}
	if strings.TrimSpace(owner) != "" && sharedBrowser.owner != owner {
		return sharedBrowser.statusLocked(), fmt.Errorf("browser session belongs to another workflow")
	}
	if takeover && !sharedBrowser.visible {
		return sharedBrowser.statusLocked(), fmt.Errorf("human takeover needs a visible browser; stop this headless session and open a visible one")
	}
	sharedBrowser.paused = true
	sharedBrowser.state = "waiting_user"
	if takeover {
		sharedBrowser.lastAction = "takeover"
	} else {
		sharedBrowser.lastAction = "pause"
	}
	sharedBrowser.updatedAt = time.Now().Format(time.RFC3339)
	sharedBrowser.signalLocked()
	return sharedBrowser.statusLocked(), nil
}

func ResumeBrowserWorkflow(owner string) (BrowserRuntimeStatus, error) {
	sharedBrowser.actionMu.Lock()
	defer sharedBrowser.actionMu.Unlock()
	sharedBrowser.mu.Lock()
	defer sharedBrowser.mu.Unlock()
	if sharedBrowser.ctx == nil || sharedBrowser.ctx.Err() != nil {
		return sharedBrowser.statusLocked(), fmt.Errorf("no active browser workflow to resume")
	}
	if strings.TrimSpace(owner) != "" && sharedBrowser.owner != owner {
		return sharedBrowser.statusLocked(), fmt.Errorf("browser session belongs to another workflow")
	}
	sharedBrowser.paused = false
	sharedBrowser.state = "ready"
	sharedBrowser.lastAction = "resume"
	sharedBrowser.updatedAt = time.Now().Format(time.RFC3339)
	sharedBrowser.signalLocked()
	return sharedBrowser.statusLocked(), nil
}

// WaitForBrowserWorkflowResume blocks the model-facing takeover tool while the
// UI remains responsive. It returns only after the same owner resumes, closes,
// or cancels the run, preventing an unattended model from racing past MFA or a
// CAPTCHA.
func WaitForBrowserWorkflowResume(ctx context.Context, owner string) (BrowserRuntimeStatus, error) {
	owner = strings.TrimSpace(owner)
	for {
		sharedBrowser.mu.Lock()
		status := sharedBrowser.statusLocked()
		changed := sharedBrowser.stateChanged
		active := sharedBrowser.ctx != nil && sharedBrowser.ctx.Err() == nil
		owned := owner == "" || sharedBrowser.owner == owner
		paused := sharedBrowser.paused
		sharedBrowser.mu.Unlock()
		if !active {
			return status, fmt.Errorf("browser workflow was closed during user takeover")
		}
		if !owned {
			return status, fmt.Errorf("browser session belongs to another workflow")
		}
		if !paused {
			return status, nil
		}
		if changed == nil {
			changed = make(chan struct{})
		}
		select {
		case <-ctx.Done():
			return GetBrowserWorkflowStatus(owner), ctx.Err()
		case <-changed:
		}
	}
}

func CloseBrowserWorkflow(owner string) (BrowserRuntimeStatus, error) {
	sharedBrowser.actionMu.Lock()
	defer sharedBrowser.actionMu.Unlock()
	sharedBrowser.mu.Lock()
	defer sharedBrowser.mu.Unlock()
	if strings.TrimSpace(owner) != "" && sharedBrowser.owner != "" && sharedBrowser.owner != owner {
		return sharedBrowser.statusLocked(), fmt.Errorf("browser session belongs to another workflow")
	}
	sharedBrowser.closeLocked()
	return sharedBrowser.statusLocked(), nil
}

func (s *browserSession) statusLocked() BrowserRuntimeStatus {
	binary := detectBrowserBinary()
	state := s.state
	if state == "" {
		state = "idle"
	}
	return BrowserRuntimeStatus{
		Available: binary != "", Active: s.ctx != nil && s.ctx.Err() == nil,
		Visible: s.visible, Paused: s.paused, Binary: binary, Owner: s.owner,
		State: state, URL: s.url, Title: s.title, LastAction: s.lastAction,
		LastError: s.lastError, UpdatedAt: s.updatedAt,
		ActiveTab: s.currentTab, TabCount: len(s.tabs),
	}
}

type BrowserTabInfo struct {
	Ref     string `json:"ref"`
	Current bool   `json:"current"`
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
}

func browserActionContext(parent, session context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(session, 45*time.Second)
	if parent != nil {
		stop := context.AfterFunc(parent, cancel)
		return ctx, func() { stop(); cancel() }
	}
	return ctx, cancel
}

// BrowserNewTab opens a second target inside the same conversation-owned
// browser allocation and switches automation to it.
type BrowserNewTab struct{}

func (t *BrowserNewTab) Name() string      { return "browser_new_tab" }
func (t *BrowserNewTab) Destructive() bool { return false }
func (t *BrowserNewTab) Description() string {
	return "Open and switch to a new conversation-owned browser tab. Tab refs are local to the active browser workflow."
}
func (t *BrowserNewTab) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)
}
func (t *BrowserNewTab) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_new_tab: bad params: %w", err)
	}
	u := strings.TrimSpace(p.URL)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("browser_new_tab: url must be http or https")
	}
	sharedBrowser.actionMu.Lock()
	defer sharedBrowser.actionMu.Unlock()
	owner := browserSessionOwner(ctx)
	if _, err := sharedBrowser.ensure(ctx, owner, false); err != nil {
		return "", err
	}
	sharedBrowser.mu.Lock()
	if sharedBrowser.paused {
		sharedBrowser.mu.Unlock()
		return "", fmt.Errorf("browser workflow is paused for user takeover")
	}
	root := sharedBrowser.rootCtx
	ref := fmt.Sprintf("t%d", sharedBrowser.nextTab)
	sharedBrowser.nextTab++
	sharedBrowser.mu.Unlock()
	runCtx, cancel := browserActionContext(ctx, root)
	defer cancel()
	var targetID cdptarget.ID
	if err := chromedp.Run(runCtx, chromedp.ActionFunc(func(actionCtx context.Context) error {
		var createErr error
		targetID, createErr = cdptarget.CreateTarget(u).Do(actionCtx)
		return createErr
	})); err != nil {
		return "", err
	}
	tabCtx, tabCancel := chromedp.NewContext(root, chromedp.WithTargetID(targetID))
	if err := chromedp.Run(tabCtx, chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		tabCancel()
		_ = chromedp.Run(root, chromedp.ActionFunc(func(actionCtx context.Context) error { return cdptarget.CloseTarget(targetID).Do(actionCtx) }))
		return "", err
	}
	var title, loc string
	_ = chromedp.Run(tabCtx, chromedp.Title(&title), chromedp.Location(&loc))
	sharedBrowser.mu.Lock()
	sharedBrowser.tabs[ref] = targetID
	sharedBrowser.tabContexts[ref] = tabCtx
	sharedBrowser.tabCancels[ref] = tabCancel
	sharedBrowser.currentTab = ref
	sharedBrowser.ctx = tabCtx
	sharedBrowser.title = title
	sharedBrowser.url = loc
	sharedBrowser.lastAction = "new_tab"
	sharedBrowser.state = "ready"
	sharedBrowser.clearElementsLocked()
	sharedBrowser.updatedAt = time.Now().Format(time.RFC3339)
	sharedBrowser.signalLocked()
	sharedBrowser.mu.Unlock()
	return fmt.Sprintf("opened tab %s\nurl: %s\ntitle: %s\nSnapshot this tab before interaction.", ref, loc, title), nil
}

// BrowserTabs returns stable controller-issued tab refs without exposing CDP IDs.
type BrowserTabs struct{}

func (t *BrowserTabs) Name() string      { return "browser_tabs" }
func (t *BrowserTabs) Destructive() bool { return false }
func (t *BrowserTabs) Description() string {
	return "List conversation-owned browser tabs and their stable refs."
}
func (t *BrowserTabs) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t *BrowserTabs) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	sharedBrowser.actionMu.Lock()
	defer sharedBrowser.actionMu.Unlock()
	owner := browserSessionOwner(ctx)
	sharedBrowser.mu.Lock()
	if sharedBrowser.rootCtx == nil || sharedBrowser.owner != owner {
		sharedBrowser.mu.Unlock()
		return "", fmt.Errorf("no active browser workflow for this conversation")
	}
	root := sharedBrowser.rootCtx
	refs := make(map[cdptarget.ID]string, len(sharedBrowser.tabs))
	current := sharedBrowser.currentTab
	for ref, id := range sharedBrowser.tabs {
		refs[id] = ref
	}
	sharedBrowser.mu.Unlock()
	infos, err := chromedp.Targets(root)
	if err != nil {
		return "", err
	}
	tabs := make([]BrowserTabInfo, 0, len(refs))
	for _, info := range infos {
		if ref := refs[info.TargetID]; ref != "" {
			tabs = append(tabs, BrowserTabInfo{Ref: ref, Current: ref == current, Title: info.Title, URL: info.URL})
		}
	}
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].Ref < tabs[j].Ref })
	encoded, _ := json.MarshalIndent(tabs, "", "  ")
	return string(encoded), nil
}

type BrowserSwitchTab struct{}

func (t *BrowserSwitchTab) Name() string      { return "browser_switch_tab" }
func (t *BrowserSwitchTab) Destructive() bool { return false }
func (t *BrowserSwitchTab) Description() string {
	return "Switch automation to a stable tab ref returned by browser tabs, then snapshot before interaction."
}
func (t *BrowserSwitchTab) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"tab_ref":{"type":"string"}},"required":["tab_ref"],"additionalProperties":false}`)
}
func (t *BrowserSwitchTab) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		TabRef string `json:"tab_ref"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_switch_tab: bad params: %w", err)
	}
	ref := strings.TrimSpace(p.TabRef)
	sharedBrowser.actionMu.Lock()
	defer sharedBrowser.actionMu.Unlock()
	owner := browserSessionOwner(ctx)
	sharedBrowser.mu.Lock()
	if sharedBrowser.owner != owner || sharedBrowser.rootCtx == nil {
		sharedBrowser.mu.Unlock()
		return "", fmt.Errorf("no active browser workflow for this conversation")
	}
	selectedCtx := sharedBrowser.tabContexts[ref]
	if sharedBrowser.tabs[ref] == "" || selectedCtx == nil {
		sharedBrowser.mu.Unlock()
		return "", fmt.Errorf("stale browser tab reference %q", ref)
	}
	sharedBrowser.ctx = selectedCtx
	sharedBrowser.currentTab = ref
	sharedBrowser.clearElementsLocked()
	sharedBrowser.mu.Unlock()
	var title, loc string
	runCtx, cancel := browserActionContext(ctx, selectedCtx)
	defer cancel()
	if err := chromedp.Run(runCtx, chromedp.Title(&title), chromedp.Location(&loc)); err != nil {
		return "", err
	}
	sharedBrowser.mu.Lock()
	sharedBrowser.title = title
	sharedBrowser.url = loc
	sharedBrowser.lastAction = "switch_tab"
	sharedBrowser.state = "ready"
	sharedBrowser.updatedAt = time.Now().Format(time.RFC3339)
	sharedBrowser.signalLocked()
	sharedBrowser.mu.Unlock()
	return fmt.Sprintf("switched to %s\nurl: %s\ntitle: %s\nSnapshot before interaction.", ref, loc, title), nil
}

type BrowserCloseTab struct{}

func (t *BrowserCloseTab) Name() string      { return "browser_close_tab" }
func (t *BrowserCloseTab) Destructive() bool { return false }
func (t *BrowserCloseTab) Description() string {
	return "Close a secondary browser tab by stable ref. The primary t1 tab is closed only by stopping the whole browser workflow."
}
func (t *BrowserCloseTab) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"tab_ref":{"type":"string"}},"required":["tab_ref"],"additionalProperties":false}`)
}
func (t *BrowserCloseTab) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		TabRef string `json:"tab_ref"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_close_tab: bad params: %w", err)
	}
	ref := strings.TrimSpace(p.TabRef)
	if ref == "t1" {
		return "", fmt.Errorf("primary tab t1 is workflow-owned; use browser close to stop the whole session")
	}
	sharedBrowser.actionMu.Lock()
	defer sharedBrowser.actionMu.Unlock()
	owner := browserSessionOwner(ctx)
	sharedBrowser.mu.Lock()
	if sharedBrowser.owner != owner || sharedBrowser.rootCtx == nil {
		sharedBrowser.mu.Unlock()
		return "", fmt.Errorf("no active browser workflow for this conversation")
	}
	targetID := sharedBrowser.tabs[ref]
	tabCtx := sharedBrowser.tabContexts[ref]
	cancelTab := sharedBrowser.tabCancels[ref]
	root := sharedBrowser.rootCtx
	if targetID == "" || tabCtx == nil || cancelTab == nil {
		sharedBrowser.mu.Unlock()
		return "", fmt.Errorf("stale browser tab reference %q", ref)
	}
	delete(sharedBrowser.tabs, ref)
	delete(sharedBrowser.tabContexts, ref)
	delete(sharedBrowser.tabCancels, ref)
	if sharedBrowser.currentTab == ref {
		sharedBrowser.currentTab = "t1"
		sharedBrowser.ctx = root
	}
	sharedBrowser.clearElementsLocked()
	sharedBrowser.lastAction = "close_tab"
	sharedBrowser.updatedAt = time.Now().Format(time.RFC3339)
	sharedBrowser.signalLocked()
	sharedBrowser.mu.Unlock()
	if err := chromedp.Cancel(tabCtx); err != nil && !errors.Is(err, context.Canceled) {
		cancelTab()
		return "", fmt.Errorf("close browser tab %s: %w", ref, err)
	}
	cancelTab()
	return "closed tab " + ref, nil
}

type BrowserCheckpointInfo struct {
	Name                 string `json:"name"`
	URL                  string `json:"url"`
	Title                string `json:"title,omitempty"`
	Visible              bool   `json:"visible"`
	CreatedAt            string `json:"created_at"`
	CredentialsPersisted bool   `json:"credentials_persisted"`
	CookiesPersisted     bool   `json:"cookies_persisted"`
	Version              int    `json:"version"`
}

func safeBrowserCheckpointName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return "", fmt.Errorf("checkpoint name must contain 1 to 64 letters, numbers, dashes, or underscores")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return "", fmt.Errorf("checkpoint name must contain only letters, numbers, dashes, or underscores")
		}
	}
	return name, nil
}

func browserCheckpointDir(ctx context.Context) (string, error) {
	root, err := filepath.Abs(NormalizeHostPath(browserWorkspaceRoot(ctx)))
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("browser workspace is unavailable")
	}
	dir := filepath.Join(root, ".mauler", "browser-checkpoints")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func checkpointSafeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("checkpoint requires a current HTTP or HTTPS page")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String(), nil
}

func SaveBrowserCheckpoint(ctx context.Context, name string) (BrowserCheckpointInfo, error) {
	name, err := safeBrowserCheckpointName(name)
	if err != nil {
		return BrowserCheckpointInfo{}, err
	}
	status := GetBrowserWorkflowStatus(browserSessionOwner(ctx))
	if !status.Active || status.State == "owned_elsewhere" {
		return BrowserCheckpointInfo{}, fmt.Errorf("no active browser workflow for this conversation")
	}
	safeURL, err := checkpointSafeURL(status.URL)
	if err != nil {
		return BrowserCheckpointInfo{}, err
	}
	checkpoint := BrowserCheckpointInfo{
		Name: name, URL: safeURL, Title: status.Title, Visible: status.Visible,
		CreatedAt: time.Now().Format(time.RFC3339), Version: 1,
		CredentialsPersisted: false, CookiesPersisted: false,
	}
	dir, err := browserCheckpointDir(ctx)
	if err != nil {
		return BrowserCheckpointInfo{}, err
	}
	encoded, _ := json.MarshalIndent(checkpoint, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, name+".json"), encoded, 0o600); err != nil {
		return BrowserCheckpointInfo{}, err
	}
	return checkpoint, nil
}

func ListBrowserCheckpoints(ctx context.Context) ([]BrowserCheckpointInfo, error) {
	dir, err := browserCheckpointDir(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]BrowserCheckpointInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			continue
		}
		var checkpoint BrowserCheckpointInfo
		if json.Unmarshal(raw, &checkpoint) == nil && checkpoint.Version == 1 && checkpoint.Name != "" {
			out = append(out, checkpoint)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func ResumeBrowserCheckpoint(ctx context.Context, name string) (BrowserCheckpointInfo, error) {
	name, err := safeBrowserCheckpointName(name)
	if err != nil {
		return BrowserCheckpointInfo{}, err
	}
	dir, err := browserCheckpointDir(ctx)
	if err != nil {
		return BrowserCheckpointInfo{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return BrowserCheckpointInfo{}, fmt.Errorf("read browser checkpoint: %w", err)
	}
	var checkpoint BrowserCheckpointInfo
	if err := json.Unmarshal(raw, &checkpoint); err != nil || checkpoint.Version != 1 {
		return BrowserCheckpointInfo{}, fmt.Errorf("browser checkpoint is invalid or unsupported")
	}
	checkpoint.URL, err = checkpointSafeURL(checkpoint.URL)
	if err != nil {
		return BrowserCheckpointInfo{}, err
	}
	args, _ := json.Marshal(map[string]any{"url": checkpoint.URL, "visible": checkpoint.Visible})
	if _, err := (&BrowserOpen{}).Run(ctx, args); err != nil {
		return BrowserCheckpointInfo{}, err
	}
	return checkpoint, nil
}

type BrowserCheckpoint struct{}

func (t *BrowserCheckpoint) Name() string      { return "browser_checkpoint" }
func (t *BrowserCheckpoint) Destructive() bool { return false }
func (t *BrowserCheckpoint) Description() string {
	return "Save a named browser continuation checkpoint containing only a sanitized URL, title, and visibility. Credentials, cookies, form values, URL queries, and fragments are never persisted."
}
func (t *BrowserCheckpoint) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`)
}
func (t *BrowserCheckpoint) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_checkpoint: bad params: %w", err)
	}
	checkpoint, err := SaveBrowserCheckpoint(ctx, p.Name)
	if err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(checkpoint)
	return "[browser_checkpoint]\n" + string(encoded), nil
}

type BrowserResumeCheckpoint struct{}

func (t *BrowserResumeCheckpoint) Name() string      { return "browser_resume_checkpoint" }
func (t *BrowserResumeCheckpoint) Destructive() bool { return false }
func (t *BrowserResumeCheckpoint) Description() string {
	return "Resume a named metadata-only browser checkpoint. This reopens its sanitized URL; it does not restore credentials, cookies, form values, query tokens, or fragments."
}
func (t *BrowserResumeCheckpoint) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`)
}
func (t *BrowserResumeCheckpoint) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_resume_checkpoint: bad params: %w", err)
	}
	checkpoint, err := ResumeBrowserCheckpoint(ctx, p.Name)
	if err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(checkpoint)
	return "[browser_checkpoint_resumed]\n" + string(encoded) + "\nSnapshot before interaction; authentication may need to be completed again.", nil
}

type BrowserPageElement struct {
	Ref         string `json:"ref"`
	Tag         string `json:"tag"`
	Type        string `json:"type,omitempty"`
	Name        string `json:"name,omitempty"`
	Text        string `json:"text,omitempty"`
	Href        string `json:"href,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
}

func (s *browserSession) setElements(owner string, elements []BrowserPageElement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil || s.owner != owner {
		return fmt.Errorf("browser session expired while recording page references")
	}
	s.elements = make(map[string]string, len(elements))
	for _, element := range elements {
		if element.Ref != "" {
			s.elements[element.Ref] = `[data-mauler-ref="` + element.Ref + `"]`
		}
	}
	return nil
}

func (s *browserSession) resolveSelector(ctx context.Context, ref, selector string) (string, error) {
	ref = strings.TrimSpace(ref)
	selector = strings.TrimSpace(selector)
	if ref != "" && selector != "" {
		return "", fmt.Errorf("provide one page element ref or CSS selector, not both")
	}
	if ref == "" {
		if selector == "" {
			return "", fmt.Errorf("page element ref or selector is required")
		}
		return selector, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil || s.ctx.Err() != nil || s.owner != browserSessionOwner(ctx) {
		return "", fmt.Errorf("browser session expired; open and snapshot the page again")
	}
	resolved := s.elements[ref]
	if resolved == "" {
		return "", fmt.Errorf("stale browser element reference %q; snapshot the page again", ref)
	}
	return resolved, nil
}

const browserElementsScript = `(() => {
  document.querySelectorAll('[data-mauler-ref]').forEach((el) => el.removeAttribute('data-mauler-ref'));
  const candidates = Array.from(document.querySelectorAll('a,button,input,select,textarea,[role="button"],[contenteditable="true"]'));
  const visible = candidates.filter((el) => {
    const style = window.getComputedStyle(el);
    const rect = el.getBoundingClientRect();
    return style.visibility !== 'hidden' && style.display !== 'none' && rect.width > 0 && rect.height > 0;
  }).slice(0, 100);
  const trim = (value, max) => String(value || '').replace(/\s+/g, ' ').trim().slice(0, max);
  return visible.map((el, index) => {
    const ref = 'e' + (index + 1);
    el.setAttribute('data-mauler-ref', ref);
    return {
      ref,
      tag: el.tagName.toLowerCase(),
      type: trim(el.getAttribute('type'), 40),
      name: trim(el.getAttribute('aria-label') || el.getAttribute('name') || el.id, 120),
      text: trim(el.innerText || el.textContent, 160),
      href: el.tagName.toLowerCase() === 'a' ? trim(el.href, 500) : '',
      placeholder: trim(el.getAttribute('placeholder'), 160),
      required: !!el.required,
      disabled: !!el.disabled
    };
  });
})()`

// BrowserOpen opens a page in the local browser automation session.
type BrowserOpen struct{}

func (t *BrowserOpen) Name() string      { return "browser_open" }
func (t *BrowserOpen) Destructive() bool { return false }
func (t *BrowserOpen) Description() string {
	return "Open a URL in the persistent native browser session. Set visible=true whenever the user asks to open, show, or launch the browser, and for login, signup, CAPTCHA, MFA, verification, payment, or any workflow that may need user takeover."
}
func (t *BrowserOpen) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "HTTP or HTTPS URL to open"},
    "visible": {"type": "boolean", "description": "Show the browser window for user takeover"}
  },
  "required": ["url"],
  "additionalProperties": false
}`)
}
func (t *BrowserOpen) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		URL     string `json:"url"`
		Visible bool   `json:"visible"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_open: bad params: %w", err)
	}
	u := strings.TrimSpace(p.URL)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("browser_open: url must be http or https")
	}
	sharedBrowser.mu.Lock()
	sharedBrowser.clearElementsLocked()
	sharedBrowser.mu.Unlock()
	status, err := sharedBrowser.run(ctx, "open", p.Visible, chromedp.Navigate(u), chromedp.WaitReady("body", chromedp.ByQuery))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("opened %s\nstate: %s\nvisible: %t\nurl: %s\ntitle: %s", u, status.State, status.Visible, status.URL, status.Title), nil
}

// BrowserSnapshot returns the current page title, URL, and visible text.
type BrowserSnapshot struct{}

func (t *BrowserSnapshot) Name() string      { return "browser_snapshot" }
func (t *BrowserSnapshot) Destructive() bool { return false }
func (t *BrowserSnapshot) Description() string {
	return "Return the current browser page title, URL, visible body text, and controller-generated page element refs. Prefer refs such as e1 over brittle CSS selectors and snapshot again after navigation or DOM changes."
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
	var elements []BrowserPageElement
	if _, err := sharedBrowser.run(ctx, "snapshot", false, chromedp.Title(&title), chromedp.Location(&loc), chromedp.Text("body", &body, chromedp.ByQuery), chromedp.Evaluate(browserElementsScript, &elements)); err != nil {
		return "", err
	}
	if err := sharedBrowser.setElements(browserSessionOwner(ctx), elements); err != nil {
		return "", err
	}
	body = cleanText(body)
	if len(body) > maxChars {
		body = body[:maxChars] + "\n[truncated]"
	}
	encoded, _ := json.MarshalIndent(elements, "", "  ")
	return fmt.Sprintf("# Browser snapshot\nTitle: %s\nURL: %s\n\n%s\n\n## Interactive elements\n%s", title, loc, body, encoded), nil
}

// BrowserClick clicks an element matching a CSS selector.
type BrowserClick struct{}

func (t *BrowserClick) Name() string      { return "browser_click" }
func (t *BrowserClick) Destructive() bool { return false }
func (t *BrowserClick) Description() string {
	return "Click a visible element by stable page ref from the latest snapshot, or by CSS selector as a fallback. Observe after clicking and never blindly repeat a submit."
}
func (t *BrowserClick) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
	"ref": {"type": "string", "description": "Stable element ref from the latest browser snapshot, for example e3"},
    "selector": {"type": "string", "description": "CSS selector to click"}
  },
  "additionalProperties": false
}`)
}
func (t *BrowserClick) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Ref      string `json:"ref"`
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_click: bad params: %w", err)
	}
	selector, err := sharedBrowser.resolveSelector(ctx, p.Ref, p.Selector)
	if err != nil {
		return "", fmt.Errorf("browser_click: %w", err)
	}
	status, err := sharedBrowser.run(ctx, "click", false, chromedp.Click(selector, chromedp.ByQuery), chromedp.Sleep(500*time.Millisecond))
	if err != nil {
		return "", err
	}
	sharedBrowser.mu.Lock()
	sharedBrowser.clearElementsLocked()
	sharedBrowser.mu.Unlock()
	label := strings.TrimSpace(p.Ref)
	if label == "" {
		label = selector
	}
	return fmt.Sprintf("clicked %s\nstate: %s\nurl: %s\ntitle: %s\nElement refs are now stale. Snapshot the page before the next action and never repeat a submit without observing.", label, status.State, status.URL, status.Title), nil
}

// BrowserType types into an element matching a CSS selector.
type BrowserType struct{}

func (t *BrowserType) Name() string      { return "browser_type" }
func (t *BrowserType) Destructive() bool { return false }
func (t *BrowserType) Description() string {
	return "Clear and type text into a stable page ref from the latest snapshot, or a CSS selector fallback, optionally submitting with Enter. Typed text is never echoed."
}
func (t *BrowserType) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
	"ref": {"type": "string", "description": "Stable element ref from the latest browser snapshot"},
    "selector": {"type": "string", "description": "CSS selector to type into"},
    "text": {"type": "string", "description": "Text to type"},
    "submit": {"type": "boolean", "description": "Press Enter after typing"}
  },
	"required": ["text"],
  "additionalProperties": false
}`)
}
func (t *BrowserType) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Ref      string `json:"ref"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Submit   bool   `json:"submit"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_type: bad params: %w", err)
	}
	selector, err := sharedBrowser.resolveSelector(ctx, p.Ref, p.Selector)
	if err != nil {
		return "", fmt.Errorf("browser_type: %w", err)
	}
	selectorJSON, _ := json.Marshal(selector)
	clearExpression := fmt.Sprintf(`(() => {
  const element = document.querySelector(%s);
  if (!element) throw new Error("input selector not found");
  element.focus();
  element.value = "";
  element.dispatchEvent(new Event("input", { bubbles: true }));
})()`, selectorJSON)
	actions := []chromedp.Action{
		chromedp.Evaluate(clearExpression, nil),
		chromedp.SendKeys(selector, p.Text, chromedp.ByQuery),
	}
	if p.Submit {
		actions = append(actions, chromedp.SendKeys(selector, "\n", chromedp.ByQuery))
	}
	actions = append(actions, chromedp.Sleep(500*time.Millisecond))
	status, err := sharedBrowser.run(ctx, "type", false, actions...)
	if err != nil {
		return "", err
	}
	if p.Submit {
		sharedBrowser.mu.Lock()
		sharedBrowser.clearElementsLocked()
		sharedBrowser.mu.Unlock()
	}
	label := strings.TrimSpace(p.Ref)
	if label == "" {
		label = selector
	}
	return fmt.Sprintf("typed into %s\nstate: %s\nurl: %s\nText was not echoed to protect credentials.", label, status.State, status.URL), nil
}

// BrowserExtract extracts text from a CSS selector, or body when omitted.
type BrowserExtract struct{}

func (t *BrowserExtract) Name() string      { return "browser_extract" }
func (t *BrowserExtract) Destructive() bool { return false }
func (t *BrowserExtract) Description() string {
	return "Extract visible text from a stable page ref or CSS selector on the current browser page, or body when both are omitted."
}
func (t *BrowserExtract) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
	"ref": {"type": "string", "description": "Stable element ref from the latest browser snapshot"},
    "selector": {"type": "string", "description": "CSS selector to extract from, default body"},
    "max_chars": {"type": "integer", "description": "Maximum characters, default 12000, max 30000"}
  },
  "additionalProperties": false
}`)
}
func (t *BrowserExtract) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Ref      string `json:"ref"`
		Selector string `json:"selector"`
		MaxChars int    `json:"max_chars"`
	}
	_ = json.Unmarshal(raw, &p)
	selector := strings.TrimSpace(p.Selector)
	if strings.TrimSpace(p.Ref) != "" || selector != "" {
		var err error
		selector, err = sharedBrowser.resolveSelector(ctx, p.Ref, selector)
		if err != nil {
			return "", fmt.Errorf("browser_extract: %w", err)
		}
	} else {
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
	if _, err := sharedBrowser.run(ctx, "extract", false, chromedp.Text(selector, &text, chromedp.ByQuery)); err != nil {
		return "", err
	}
	text = cleanText(text)
	if len(text) > maxChars {
		text = text[:maxChars] + "\n[truncated]"
	}
	return fmt.Sprintf("# Extracted %s\n\n%s", selector, text), nil
}

const maxBrowserUploadBytes int64 = 256 << 20

func workspaceScopedBrowserFile(ctx context.Context, supplied string) (string, string, os.FileInfo, error) {
	root := strings.TrimSpace(browserWorkspaceRoot(ctx))
	if root == "" {
		return "", "", nil, fmt.Errorf("browser workspace root is not configured")
	}
	rootAbs, err := filepath.Abs(NormalizeHostPath(root))
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve browser workspace: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", "", nil, fmt.Errorf("browser workspace is unavailable: %w", err)
	}
	path := NormalizeHostPath(strings.TrimSpace(supplied))
	if path == "" {
		return "", "", nil, fmt.Errorf("file path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootReal, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve file path: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", nil, fmt.Errorf("open workspace file: %w", err)
	}
	rel, err := filepath.Rel(rootReal, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", nil, fmt.Errorf("browser upload is restricted to the active workspace")
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("browser upload path must be a regular file")
	}
	if info.Size() > maxBrowserUploadBytes {
		return "", "", nil, fmt.Errorf("browser upload exceeds the 256 MiB workspace limit")
	}
	return real, filepath.ToSlash(rel), info, nil
}

func browserFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// BrowserUpload chooses a workspace-scoped file in an input[type=file].
type BrowserUpload struct{}

func (t *BrowserUpload) Name() string      { return "browser_upload" }
func (t *BrowserUpload) Destructive() bool { return false }
func (t *BrowserUpload) Description() string {
	return "Select one regular file from the active workspace in a file input using a stable page ref or CSS selector. The file is hashed for evidence; paths outside the workspace and symlink escapes are blocked."
}
func (t *BrowserUpload) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "ref": {"type": "string", "description": "Stable file-input ref from the latest snapshot"},
    "selector": {"type": "string", "description": "CSS selector fallback for input[type=file]"},
    "path": {"type": "string", "description": "Workspace-relative or workspace-contained file path"}
  },
  "required": ["path"],
  "additionalProperties": false
}`)
}
func (t *BrowserUpload) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Ref      string `json:"ref"`
		Selector string `json:"selector"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_upload: bad params: %w", err)
	}
	selector, err := sharedBrowser.resolveSelector(ctx, p.Ref, p.Selector)
	if err != nil {
		return "", fmt.Errorf("browser_upload: %w", err)
	}
	abs, rel, info, err := workspaceScopedBrowserFile(ctx, p.Path)
	if err != nil {
		return "", fmt.Errorf("browser_upload: %w", err)
	}
	hash, err := browserFileSHA256(abs)
	if err != nil {
		return "", fmt.Errorf("browser_upload: hash file: %w", err)
	}
	if _, err := sharedBrowser.run(ctx, "upload", false, chromedp.SetUploadFiles(selector, []string{abs}, chromedp.ByQuery)); err != nil {
		return "", err
	}
	return fmt.Sprintf("[browser_upload_evidence]\nworkspace_file: %s\nsize: %d\nsha256: %s\nFile selected in the page. Observe before any separate submit action.", rel, info.Size(), hash), nil
}

type browserFileStamp struct {
	Size    int64
	ModNano int64
}

func browserDirectoryStamps(dir string) map[string]browserFileStamp {
	stamps := map[string]browserFileStamp{}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if info, err := entry.Info(); err == nil {
			stamps[entry.Name()] = browserFileStamp{Size: info.Size(), ModNano: info.ModTime().UnixNano()}
		}
	}
	return stamps
}

func waitForBrowserDownload(ctx context.Context, dir string, before map[string]browserFileStamp, timeout time.Duration) (string, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	stable := map[string]int{}
	lastSize := map[string]int64{}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("download timed out after the page action; do not repeat it until the page and download folder are observed")
		case <-ticker.C:
			entries, _ := os.ReadDir(dir)
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				if !entry.IsDir() {
					names = append(names, entry.Name())
				}
			}
			sort.Strings(names)
			for _, name := range names {
				if strings.HasSuffix(strings.ToLower(name), ".crdownload") || strings.HasSuffix(strings.ToLower(name), ".tmp") {
					continue
				}
				path := filepath.Join(dir, name)
				info, err := os.Stat(path)
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				old, existed := before[name]
				if existed && old.Size == info.Size() && old.ModNano == info.ModTime().UnixNano() {
					continue
				}
				if lastSize[name] == info.Size() {
					stable[name]++
				} else {
					lastSize[name] = info.Size()
					stable[name] = 0
				}
				if stable[name] >= 1 {
					return path, nil
				}
			}
		}
	}
}

func safeBrowserOwnerDir(owner string) string {
	var b strings.Builder
	for _, r := range owner {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "legacy"
	}
	return b.String()
}

func browserEvidenceDir(ctx context.Context) (string, error) {
	root, err := filepath.Abs(NormalizeHostPath(browserWorkspaceRoot(ctx)))
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("browser workspace is unavailable")
	}
	dir := filepath.Join(root, ".mauler", "browser-downloads", safeBrowserOwnerDir(browserSessionOwner(ctx)))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// BrowserDownload clicks a download control and records the resulting file in
// a workspace-owned evidence directory.
type BrowserDownload struct{}

func (t *BrowserDownload) Name() string      { return "browser_download" }
func (t *BrowserDownload) Destructive() bool { return false }
func (t *BrowserDownload) Description() string {
	return "Click a download control by stable page ref or CSS selector, save the resulting file under .mauler/browser-downloads, and return path, size, and SHA-256 evidence. Never repeat a timed-out download blindly."
}
func (t *BrowserDownload) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "ref": {"type": "string", "description": "Stable download element ref from the latest snapshot"},
    "selector": {"type": "string", "description": "CSS selector fallback"},
    "timeout_secs": {"type": "integer", "description": "Completion wait, default 20, max 40 seconds"}
  },
  "additionalProperties": false
}`)
}
func (t *BrowserDownload) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Ref         string `json:"ref"`
		Selector    string `json:"selector"`
		TimeoutSecs int    `json:"timeout_secs"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("browser_download: bad params: %w", err)
	}
	selector, err := sharedBrowser.resolveSelector(ctx, p.Ref, p.Selector)
	if err != nil {
		return "", fmt.Errorf("browser_download: %w", err)
	}
	dir, err := browserEvidenceDir(ctx)
	if err != nil {
		return "", fmt.Errorf("browser_download: %w", err)
	}
	timeout := time.Duration(p.TimeoutSecs) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if timeout > 40*time.Second {
		timeout = 40 * time.Second
	}
	before := browserDirectoryStamps(dir)
	var downloaded string
	actions := []chromedp.Action{
		chromedp.ActionFunc(func(actionCtx context.Context) error {
			return cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorAllow).WithDownloadPath(dir).WithEventsEnabled(true).Do(actionCtx)
		}),
		chromedp.Click(selector, chromedp.ByQuery),
		chromedp.ActionFunc(func(actionCtx context.Context) error {
			var waitErr error
			downloaded, waitErr = waitForBrowserDownload(actionCtx, dir, before, timeout)
			return waitErr
		}),
	}
	status, err := sharedBrowser.run(ctx, "download", false, actions...)
	if err != nil {
		return "", err
	}
	sharedBrowser.mu.Lock()
	sharedBrowser.clearElementsLocked()
	sharedBrowser.mu.Unlock()
	info, err := os.Stat(downloaded)
	if err != nil {
		return "", fmt.Errorf("browser_download: downloaded file disappeared: %w", err)
	}
	hash, err := browserFileSHA256(downloaded)
	if err != nil {
		return "", fmt.Errorf("browser_download: hash evidence: %w", err)
	}
	rel, _ := filepath.Rel(browserWorkspaceRoot(ctx), downloaded)
	return fmt.Sprintf("[browser_download_evidence]\nworkspace_file: %s\nsize: %d\nsha256: %s\nurl: %s\nstate: %s", filepath.ToSlash(rel), info.Size(), hash, status.URL, status.State), nil
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
	if _, err := sharedBrowser.run(ctx, "screenshot", false, chromedp.FullScreenshot(&buf, 90)); err != nil {
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
	if _, err := CloseBrowserWorkflow(browserSessionOwner(ctx)); err != nil {
		return "", err
	}
	return "browser closed", nil
}

type BrowserRecovery struct {
	Class        string `json:"class"`
	Retryable    bool   `json:"retryable"`
	ObserveFirst bool   `json:"observe_first"`
	Ambiguous    bool   `json:"ambiguous"`
	Guidance     string `json:"guidance"`
}

// ClassifyBrowserRecovery produces code-owned recovery advice. The model may
// use it to change evidence path, but no class authorises automatic repetition
// of a click, submit, upload, or download.
func ClassifyBrowserRecovery(action string, err error) BrowserRecovery {
	action = strings.ToLower(strings.TrimSpace(action))
	message := ""
	if err != nil {
		message = strings.ToLower(err.Error())
	}
	stateChanging := action == "click" || action == "upload" || action == "download" || action == "type_submit"
	recovery := BrowserRecovery{Class: "unknown", ObserveFirst: true, Ambiguous: stateChanging, Guidance: "Snapshot the current page and inspect browser status before choosing a different evidence path."}
	switch {
	case strings.Contains(message, "restricted to the active workspace") || strings.Contains(message, "symlink") || strings.Contains(message, "256 mib"):
		recovery.Class = "scope_policy"
		recovery.ObserveFirst = false
		recovery.Ambiguous = false
		recovery.Guidance = "Choose an existing regular file inside the active workspace. Do not bypass the workspace boundary."
	case strings.Contains(message, "stale browser element reference") || strings.Contains(message, "could not find node") || strings.Contains(message, "selector") && strings.Contains(message, "not found"):
		recovery.Class = "stale_element"
		recovery.Retryable = !stateChanging
		recovery.Ambiguous = false
		recovery.Guidance = "Take a fresh snapshot and use a newly issued page element ref; do not reuse the stale ref."
	case strings.Contains(message, "stale browser tab reference") || strings.Contains(message, "browser tab") && strings.Contains(message, "not found"):
		recovery.Class = "stale_tab"
		recovery.Retryable = false
		recovery.Ambiguous = false
		recovery.Guidance = "List the current conversation-owned tabs and switch using a newly issued tab ref."
	case strings.Contains(message, "owned by another") || strings.Contains(message, "belongs to another"):
		recovery.Class = "ownership_conflict"
		recovery.ObserveFirst = false
		recovery.Ambiguous = false
		recovery.Guidance = "Stop or finish the other conversation-owned browser session before starting this workflow."
	case strings.Contains(message, "no active browser") || strings.Contains(message, "session expired") || strings.Contains(message, "context canceled"):
		recovery.Class = "expired_session"
		recovery.Retryable = !stateChanging
		recovery.Guidance = "Check status and reopen the intended URL in the same conversation. Never replay a potentially completed page action automatically."
	case strings.Contains(message, "deadline exceeded") || strings.Contains(message, "timed out") || strings.Contains(message, "timeout"):
		recovery.Class = "timeout"
		recovery.Retryable = !stateChanging && (action == "open" || action == "snapshot" || action == "extract" || action == "status")
		recovery.Guidance = "Observe browser and page state first. Retry only an idempotent observation; do not repeat a page action whose outcome is uncertain."
	case strings.Contains(message, "paused for user takeover"):
		recovery.Class = "waiting_user"
		recovery.ObserveFirst = false
		recovery.Ambiguous = false
		recovery.Guidance = "Wait for the user to choose I’ve completed this—continue in Chat."
	case strings.Contains(message, "closed during user takeover"):
		recovery.Class = "user_stopped"
		recovery.ObserveFirst = false
		recovery.Ambiguous = false
		recovery.Guidance = "The user stopped the browser workflow. Do not reopen or repeat the action without a new request."
	}
	return recovery
}

func FormatBrowserRecovery(action string, err error) string {
	recovery := ClassifyBrowserRecovery(action, err)
	encoded, _ := json.Marshal(recovery)
	return "[browser_recovery]\n" + string(encoded)
}
