// Package app is the Wails application backend.
// All exported methods on App are callable from the TypeScript frontend.
package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mauler/internal/agent"
	"mauler/internal/llm"
	"mauler/internal/llm/backends"
	"mauler/internal/sessionstore"
	"mauler/internal/settings"
	"mauler/internal/tools"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails application struct.
type App struct {
	ctx context.Context

	mu       sync.Mutex
	cfg      *settings.Settings
	profiles *settings.ProfilesFile

	history  *agent.History
	rollback *agent.Rollback
	registry *tools.Registry

	loadMu sync.Mutex // serialises model-load/unload calls; never held alongside mu

	agentRunning bool
	cancelAgent  context.CancelFunc
	confirmCh    chan bool // non-nil when awaiting confirmation
	stopReason   string
	stopDetail   string

	artifactRunning bool
	cancelArtifact  context.CancelFunc

	autonomous bool

	loadedModelKey string
	currentMode    string
	autoAgents     bool
	modeOverride   string

	// cached loaded-context length; keyed by model load key so it auto-invalidates
	// on model change. Avoids an HTTP /props (or LM Studio) query every agent turn.
	ctxLimitKey string
	ctxLimitVal int

	// terminal shell session (at most one active at a time)
	shellMu   sync.Mutex
	shellSess *shellSession
}

// New creates a new App with settings loaded from disk.
func New() *App {
	cfg, _ := settings.Load()
	profiles, _ := settings.LoadProfiles()
	ensureActiveProfile(cfg, profiles)

	active := activeProfile(cfg, profiles)
	history := agent.NewHistory(active.CtxTokens)

	app := &App{
		cfg:        cfg,
		profiles:   profiles,
		history:    history,
		rollback:   &agent.Rollback{},
		registry:   tools.New(),
		autoAgents: true,
	}
	app.registerAppTools()
	tools.SetConfigSnapshot(cfg.Tools)
	return app
}

// syncToolConfig pushes the current tool-relevant settings into the tools package so
// shell/protected-path tools read from memory instead of re-loading settings.toml on
// every call. Call after any in-memory settings mutation. Assumes a.mu is not required
// (a.cfg pointer is stable; callers already hold the lock where needed).
func (a *App) syncToolConfig() {
	tools.SetConfigSnapshot(a.cfg.Tools)
}

// cloneToolsConfigRefs deep-copies the map/slice fields a run reads so that in-place
// mutations from the UI thread (e.g. ApplySafetyPreset writing EnabledTools) cannot
// race with the running agent's snapshot.
func cloneToolsConfigRefs(t *settings.ToolsConfig) {
	if t.EnabledTools != nil {
		m := make(map[string]bool, len(t.EnabledTools))
		for k, v := range t.EnabledTools {
			m[k] = v
		}
		t.EnabledTools = m
	}
	if t.Toolsets != nil {
		m := make(map[string][]string, len(t.Toolsets))
		for k, v := range t.Toolsets {
			cp := make([]string, len(v))
			copy(cp, v)
			m[k] = cp
		}
		t.Toolsets = m
	}
	if t.ProtectedPaths != nil {
		cp := make([]string, len(t.ProtectedPaths))
		copy(cp, t.ProtectedPaths)
		t.ProtectedPaths = cp
	}
	if t.SafeRules != nil {
		cp := make([]settings.ToolSafeRule, len(t.SafeRules))
		copy(cp, t.SafeRules)
		t.SafeRules = cp
	}
}

// OnStartup is called by Wails when the window is ready.
func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx
	// Apply saved working directory here (not in New) so that
	// Wails binding generation — which runs the binary without a window —
	// does not chdir away from the project root and panic on missing wails.json.
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()
	configureWorkingDir(&cfg)
}

// OnDomReady is called when the frontend DOM is ready.
func (a *App) OnDomReady(_ context.Context) {}

// OnShutdown is called before the app exits.
func (a *App) OnShutdown(_ context.Context) {
	a.mu.Lock()
	if a.cancelAgent != nil {
		a.cancelAgent()
	}
	a.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Settings & profiles
// ---------------------------------------------------------------------------

// GetSettings returns the current settings.
func (a *App) GetSettings() settings.Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return *a.cfg
}

// UpdateSettings saves new settings and applies them.
func (a *App) UpdateSettings(cfg settings.Settings) error {
	var workspaceChanged bool
	requestedWorkspace := strings.TrimSpace(cfg.Context.WorkspaceDir)
	if requestedWorkspace != "" {
		abs, err := filepath.Abs(tools.NormalizeHostPath(requestedWorkspace))
		if err != nil {
			return err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Errorf("workspace_dir: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("workspace_dir: %s is not a directory", abs)
		}
		oldWD, _ := os.Getwd()
		if !sameFilesystemPath(oldWD, abs) {
			a.mu.Lock()
			if a.agentRunning {
				a.mu.Unlock()
				return fmt.Errorf("cannot change workspace while an agent run is active")
			}
			a.mu.Unlock()
			if err := os.Chdir(abs); err != nil {
				return err
			}
			workspaceChanged = true
		}
		cfg.Context.WorkspaceDir = filepath.ToSlash(abs)
	}
	cfg.Context.OpenFolders = normaliseAppWorkspaceFolders(cfg.Context.OpenFolders, cfg.Context.WorkspaceDir)
	cfg.Context.Lab.Target = strings.TrimSpace(cfg.Context.Lab.Target)
	cfg.Context.Lab.VPNInterface = strings.TrimSpace(cfg.Context.Lab.VPNInterface)
	cfg.Context.Lab.LatestArtifact = filepath.ToSlash(strings.TrimSpace(cfg.Context.Lab.LatestArtifact))
	if err := settings.Save(&cfg); err != nil {
		return err
	}
	a.mu.Lock()
	previousKey := modelLoadKey(activeProfile(a.cfg, a.profiles))
	*a.cfg = cfg
	a.syncToolConfig()
	active := activeProfile(a.cfg, a.profiles)
	a.history.SetBudget(active.CtxTokens)
	if workspaceChanged {
		a.history.Clear()
		a.rollback.Clear()
	}
	if modelLoadKey(active) != previousKey {
		a.loadedModelKey = ""
	}
	a.mu.Unlock()
	if workspaceChanged && a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:workspace_changed", cfg.Context.WorkspaceDir)
	}
	return nil
}

// GetProfiles returns all profiles.
func (a *App) GetProfiles() settings.ProfilesFile {
	a.mu.Lock()
	defer a.mu.Unlock()
	return *a.profiles
}

// UpdateProfiles saves new profiles and applies them.
func (a *App) UpdateProfiles(pf settings.ProfilesFile) error {
	removeRemoteProfiles(&pf)
	if err := settings.SaveProfiles(&pf); err != nil {
		return err
	}
	a.mu.Lock()
	*a.profiles = pf
	active := activeProfile(a.cfg, a.profiles)
	a.history.SetBudget(active.CtxTokens)
	if modelLoadKey(active) != a.loadedModelKey {
		a.loadedModelKey = ""
	}
	a.mu.Unlock()
	return nil
}

// SwitchProfile changes the active profile by name.
func (a *App) SwitchProfile(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p, ok := a.profiles.Profiles[name]; ok {
		a.cfg.ActiveProfile = name
		a.history.SetBudget(p.CtxTokens)
		active := applyProvider(p, a.profiles)
		if modelLoadKey(active) != a.loadedModelKey {
			a.loadedModelKey = ""
		}
		return settings.Save(a.cfg)
	}
	return fmt.Errorf("profile %q not found", name)
}

// GetProfileNames returns the list of available profile names.
func (a *App) GetProfileNames() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	names := make([]string, 0, len(a.profiles.Profiles))
	for k, profile := range a.profiles.Profiles {
		if strings.TrimSpace(profile.ModelID) != "" {
			names = append(names, k)
		}
	}
	return names
}

// UseProfile saves the current settings/profiles and switches to the selected profile.
func (a *App) UseProfile(name string, cfg settings.Settings, pf settings.ProfilesFile) error {
	profile, ok := pf.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	if _, ok := pf.Providers[profile.Provider]; !ok {
		return fmt.Errorf("provider %q not found", profile.Provider)
	}
	removeRemoteProfiles(&pf)
	cfg.ActiveProfile = name
	if err := settings.Save(&cfg); err != nil {
		return err
	}
	if err := settings.SaveProfiles(&pf); err != nil {
		return err
	}
	a.mu.Lock()
	*a.cfg = cfg
	*a.profiles = pf
	a.syncToolConfig()
	active := applyProvider(profile, a.profiles)
	a.history.SetBudget(active.CtxTokens)
	if modelLoadKey(active) != a.loadedModelKey {
		a.loadedModelKey = ""
	}
	a.mu.Unlock()
	return nil
}

// HistoryStats is the payload returned to the frontend status bar.
type HistoryStats struct {
	TokenCount  int     `json:"token_count"`
	Budget      int     `json:"budget"`
	Fraction    float64 `json:"fraction"`
	RollbackLen int     `json:"rollback_len"`
}

type LabStatus struct {
	AgentRoot      string                     `json:"agent_root"`
	ShellBackend   string                     `json:"shell_backend"`
	ShellDistro    string                     `json:"shell_distro"`
	ShellUser      string                     `json:"shell_user"`
	Target         string                     `json:"target"`
	VPNInterface   string                     `json:"vpn_interface"`
	LatestArtifact string                     `json:"latest_artifact"`
	OpenFolders    []settings.WorkspaceFolder `json:"open_folders"`
}

// MaintenanceResult is returned by one-click local runtime recovery actions.
type MaintenanceResult struct {
	Summary string   `json:"summary"`
	Lines   []string `json:"lines"`
}

// GetHistoryStats returns token usage info for the status bar.
func (a *App) GetHistoryStats() HistoryStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return HistoryStats{
		TokenCount:  a.history.TokenCount(),
		Budget:      a.history.Budget(),
		Fraction:    a.history.UsageFraction(),
		RollbackLen: a.rollback.Len(),
	}
}

// ClearHistory resets the conversation.
func (a *App) ClearHistory() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.history.Clear()
	a.rollback.Clear()
}

// SessionChatMessage is the UI-safe representation returned when loading sessions.
type SessionChatMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

// ChatAttachment is a user-provided text/file attachment from the chat composer.
type ChatAttachment struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	MIME      string `json:"mime,omitempty"`
	Content   string `json:"content,omitempty"`
	Path      string `json:"path,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// SaveSession writes the current conversation history to disk.
func (a *App) SaveSession(name string) error {
	name, err := cleanSessionName(name)
	if err != nil {
		return err
	}
	a.mu.Lock()
	msgs := a.history.Messages()
	cfg := *a.cfg
	profiles := *a.profiles
	a.mu.Unlock()
	data, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		return err
	}
	dir, err := sessionsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o640); err != nil {
		return err
	}
	model := activeProfile(&cfg, &profiles).ModelID
	return sessionstore.StoreDefaultSession(name, workspaceScope(), model, toSessionStoreMessages(msgs))
}

// autoSave silently overwrites the _autosave session after each agent run.
func (a *App) autoSave() {
	_ = a.SaveSession("_autosave")
}

// LoadSession restores a saved conversation and returns chat messages for the UI.
func (a *App) LoadSession(name string) ([]SessionChatMessage, error) {
	name, err := cleanSessionName(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(mustSessionsDir(), name+".json"))
	if err != nil {
		return nil, err
	}
	var msgs []llm.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.history.Replace(msgs)
	a.rollback.Clear()
	a.mu.Unlock()
	return toSessionChatMessages(msgs), nil
}

// ListSessions returns saved session names without the .json extension.
func (a *App) ListSessions() ([]string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), ".json"))
	}
	return names, nil
}

// DeleteSession removes a saved session file.
func (a *App) DeleteSession(name string) error {
	name, err := cleanSessionName(name)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(mustSessionsDir(), name+".json"))
	if os.IsNotExist(err) {
		return sessionstore.DeleteDefaultSession(name, workspaceScope())
	}
	if err != nil {
		return err
	}
	return sessionstore.DeleteDefaultSession(name, workspaceScope())
}

func (a *App) SearchSessionRecall(query string, limit int) ([]sessionstore.SearchResult, error) {
	return sessionstore.SearchDefault(query, limit)
}

func (a *App) ClearSessionRecall() error {
	return sessionstore.ClearDefault()
}

func (a *App) ReindexSessionRecall() (int, error) {
	dir, err := sessionsDir()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	a.mu.Lock()
	cfg := *a.cfg
	profiles := *a.profiles
	a.mu.Unlock()
	model := activeProfile(&cfg, &profiles).ModelID
	scope := workspaceScope()
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return count, err
		}
		var msgs []llm.Message
		if err := json.Unmarshal(data, &msgs); err != nil {
			return count, err
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if err := sessionstore.StoreDefaultSession(name, scope, model, toSessionStoreMessages(msgs)); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (a *App) ListTodos() ([]tools.TodoItem, error) {
	return tools.LoadTodos()
}

func (a *App) ClearTodos() error {
	return tools.SaveTodos([]tools.TodoItem{})
}

// ---------------------------------------------------------------------------
// Agent messaging
// ---------------------------------------------------------------------------

// SendMessage sends a user message and starts the agent loop in a goroutine.
// Images is a slice of base64-encoded data URIs (e.g. "data:image/png;base64,...").
// Attachments are text/file payloads from pasted text or dropped files.
func (a *App) SendMessage(text string, images []string, attachments []ChatAttachment) error {
	a.mu.Lock()
	if a.agentRunning {
		a.mu.Unlock()
		return fmt.Errorf("agent is already running")
	}
	a.agentRunning = true
	cfg := *a.cfg
	cloneToolsConfigRefs(&cfg.Tools)
	profiles := *a.profiles
	autonomous := a.autonomous
	autoAgents := a.autoAgents
	a.mu.Unlock()

	profile := activeProfile(&cfg, &profiles)
	messageText := composeUserTextWithAttachments(text, attachments)
	mode := selectAgentMode(messageText, cfg)
	if !autoAgents {
		mode = manualAgentMode()
	}
	applyAgentPreset(&cfg, &profiles, mode, &profile, &autonomous)
	a.mu.Lock()
	a.currentMode = mode.Name
	a.mu.Unlock()
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:agent_mode", mode.Name)
	}

	var userMsg llm.Message
	if len(images) > 0 {
		blocks := []llm.ContentBlock{{Type: "text", Text: messageText}}
		for _, img := range images {
			b64, mediaType, ok := parseDataURI(img)
			if !ok {
				continue
			}
			blocks = append(blocks, llm.ContentBlock{
				Type: "image_url",
				ImageURL: &llm.ImageURL{
					URL:    "data:" + mediaType + ";base64," + b64,
					Detail: "auto",
				},
			})
		}
		userMsg = llm.Message{Role: llm.RoleUser, Content: blocks}
	} else {
		userMsg = llm.NewTextMessage(llm.RoleUser, messageText)
	}

	agentCtx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancelAgent = cancel
	a.stopReason = ""
	a.stopDetail = ""
	a.mu.Unlock()

	memories := relevantMemory(cfg.Memory, messageText)
	skills := relevantSkills(cfg.Skills, messageText)
	run := startTaskRun(messageText, mode.Name, cfg.ActiveProfile, profile.ModelID)

	go a.runAgentLoop(agentCtx, userMsg, profile, &cfg, autonomous, mode, memories, skills, run)
	return nil
}

func composeUserTextWithAttachments(text string, attachments []ChatAttachment) string {
	text = strings.TrimSpace(text)
	if len(attachments) == 0 {
		return text
	}
	var sb strings.Builder
	if text != "" {
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Attached context from the user:\n")
	for i, att := range attachments {
		name := strings.TrimSpace(att.Name)
		if name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		kind := strings.TrimSpace(att.Kind)
		if kind == "" {
			kind = "file"
		}
		fmt.Fprintf(&sb, "\n--- Attachment %d: %s (%s) ---\n", i+1, name, kind)
		if att.Path != "" {
			fmt.Fprintf(&sb, "Path: %s\n", att.Path)
		} else {
			sb.WriteString("Source: inline chat attachment. This is not a filesystem path; do not call read_file on the attachment name.\n")
		}
		if att.MIME != "" {
			fmt.Fprintf(&sb, "MIME: %s\n", att.MIME)
		}
		if att.Size > 0 {
			fmt.Fprintf(&sb, "Size: %d bytes\n", att.Size)
		}
		content := strings.TrimRight(att.Content, "\r\n")
		if content != "" {
			sb.WriteString(content)
			if !strings.HasSuffix(content, "\n") {
				sb.WriteByte('\n')
			}
		}
		if att.Truncated {
			sb.WriteString("[attachment truncated by composer]\n")
		}
	}
	return sb.String()
}

// GetAgentMode returns the current auto-selected agent mode.
func (a *App) GetAgentMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.currentMode == "" {
		return "Auto"
	}
	return a.currentMode
}

// SetAutoAgents toggles automatic agent mode routing.
func (a *App) SetAutoAgents(enabled bool) {
	a.mu.Lock()
	a.autoAgents = enabled
	if !enabled {
		a.currentMode = "Manual"
	}
	mode := a.currentMode
	a.mu.Unlock()
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:agent_mode", mode)
	}
}

// GetAutoAgents returns whether automatic agent routing is enabled.
func (a *App) GetAutoAgents() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.autoAgents
}

func (a *App) SetAgentModeOverride(mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "Auto"
	}
	a.mu.Lock()
	a.cfg.Agents.ModeOverride = mode
	a.currentMode = mode
	cfg := *a.cfg
	a.mu.Unlock()
	if err := settings.Save(&cfg); err != nil {
		return err
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:agent_mode", mode)
	}
	return nil
}

func (a *App) ApplySafetyPreset(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "unrestricted":
		a.cfg.Agents.OfflineOnly = false
		a.cfg.Agents.DefaultAutonomy = "full"
		a.autonomous = true
		a.cfg.Tools.Enabled = true
		a.cfg.Tools.ActiveToolset = "unrestricted"
		a.cfg.Tools.ConfirmReads = false
		a.cfg.Tools.ConfirmWrites = false
		a.cfg.Tools.ConfirmExec = false
		a.cfg.Tools.ProtectedPaths = nil
		defaults := settings.DefaultSettings()
		if a.cfg.Tools.EnabledTools == nil {
			a.cfg.Tools.EnabledTools = map[string]bool{}
		}
		for tool, enabled := range defaults.Tools.EnabledTools {
			a.cfg.Tools.EnabledTools[tool] = enabled
		}
	case "offline":
		a.cfg.Agents.OfflineOnly = true
		a.cfg.Agents.DefaultAutonomy = "balanced"
		a.autonomous = false
		a.cfg.Tools.Enabled = true
		a.cfg.Tools.ActiveToolset = "offline"
		a.cfg.Tools.ConfirmWrites = true
		a.cfg.Tools.ConfirmExec = true
		a.cfg.Tools.WebEngine = "auto"
		if a.cfg.Tools.EnabledTools == nil {
			a.cfg.Tools.EnabledTools = map[string]bool{}
		}
		// Re-enable core file tools (offline only blocks network tools)
		for _, tool := range []string{"read_file", "read_many", "read_pdf", "write_file", "edit_file", "glob", "grep", "shell", "bash"} {
			a.cfg.Tools.EnabledTools[tool] = true
		}
		for _, tool := range []string{"web_search", "fetch_url", "browser_open", "browser_snapshot", "browser_click", "browser_type", "browser_extract", "browser_screenshot", "browser_close", "browser_agent"} {
			a.cfg.Tools.EnabledTools[tool] = false
		}
	case "balanced":
		a.cfg.Agents.OfflineOnly = false
		a.cfg.Agents.DefaultAutonomy = "balanced"
		a.autonomous = false
		a.cfg.Tools.Enabled = true
		a.cfg.Tools.ActiveToolset = "balanced"
		a.cfg.Tools.ConfirmWrites = true
		a.cfg.Tools.ConfirmExec = true
		defaults := settings.DefaultSettings()
		if a.cfg.Tools.EnabledTools == nil {
			a.cfg.Tools.EnabledTools = map[string]bool{}
		}
		// Re-enable all core tools; restore web tools to their defaults
		for _, tool := range []string{"read_file", "read_many", "read_pdf", "write_file", "edit_file", "glob", "grep", "shell", "bash"} {
			a.cfg.Tools.EnabledTools[tool] = true
		}
		for _, tool := range []string{"web_search", "fetch_url", "browser_open", "browser_snapshot", "browser_click", "browser_type", "browser_extract", "browser_screenshot", "browser_close", "browser_agent"} {
			a.cfg.Tools.EnabledTools[tool] = defaults.Tools.EnabledTools[tool]
		}
	default:
		return fmt.Errorf("unknown safety preset %q", name)
	}
	a.syncToolConfig()
	return settings.Save(a.cfg)
}

// SetAutonomous toggles runtime autonomous mode.
func (a *App) SetAutonomous(enabled bool) {
	a.mu.Lock()
	a.autonomous = enabled
	a.mu.Unlock()
}

// GetAutonomous returns the current runtime autonomous mode.
func (a *App) GetAutonomous() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.autonomous
}

// StopAgent cancels the running agent.
func (a *App) StopAgent() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelAgent != nil {
		a.stopReason = "user_stopped"
		a.stopDetail = "The user stopped the current run."
		a.cancelAgent()
	}
}

// RespondConfirm unblocks the confirm gate (true = allow, false = deny).
// confirmCh is cleared under the lock before the send so that a second call
// from a rapid double-click cannot block forever on a full buffered channel.
func (a *App) RespondConfirm(allow bool) {
	a.mu.Lock()
	ch := a.confirmCh
	a.confirmCh = nil // prevent double-send
	a.mu.Unlock()
	if ch != nil {
		ch <- allow
	}
}

// AddToolSafeRule stores an exact tool-call approval so future matching calls
// can run without pausing for confirmation.
func (a *App) AddToolSafeRule(toolName, input string) error {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return fmt.Errorf("tool name is required")
	}
	hash := safeToolInputHash(input)
	if hash == "" {
		return fmt.Errorf("tool input is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Tools.SafeRules == nil {
		a.cfg.Tools.SafeRules = []settings.ToolSafeRule{}
	}
	for _, rule := range a.cfg.Tools.SafeRules {
		if rule.Tool == toolName && rule.InputHash == hash {
			return settings.Save(a.cfg)
		}
	}
	label := input
	if len(label) > 160 {
		label = label[:160] + "..."
	}
	a.cfg.Tools.SafeRules = append(a.cfg.Tools.SafeRules, settings.ToolSafeRule{
		ID:        fmt.Sprintf("safe-%d", time.Now().UnixNano()),
		Tool:      toolName,
		InputHash: hash,
		Label:     label,
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	return settings.Save(a.cfg)
}

// ---------------------------------------------------------------------------
// Rollback
// ---------------------------------------------------------------------------

// Undo reverses the most recent file mutation.
func (a *App) Undo() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	msg, ok := a.rollback.Pop()
	if !ok {
		return "nothing to undo"
	}
	return msg
}

// RollbackDepth returns how many operations can be undone.
func (a *App) RollbackDepth() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rollback.Len()
}

// ---------------------------------------------------------------------------
// File browsing
// ---------------------------------------------------------------------------

// FileNode is a lightweight tree node returned to the frontend.
type FileNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	IsDir    bool       `json:"isDir"`
	Children []FileNode `json:"children,omitempty"`
}

// GetFileTree returns a recursive directory listing.
// Depth is capped at 6 to avoid huge payloads.
func (a *App) GetFileTree(dir string) ([]FileNode, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	return buildTree(dir, 0, 6)
}

func buildTree(dir string, depth, maxDepth int) ([]FileNode, error) {
	if depth >= maxDepth {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var nodes []FileNode
	for _, e := range entries {
		if shouldSkip(e.Name()) {
			continue
		}
		node := FileNode{
			Name:  e.Name(),
			Path:  filepath.ToSlash(filepath.Join(dir, e.Name())),
			IsDir: e.IsDir(),
		}
		if e.IsDir() {
			children, _ := buildTree(filepath.Join(dir, e.Name()), depth+1, maxDepth)
			node.Children = children
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true,
	".cache": true, "vendor": true, "dist": true, "build": true,
}

func shouldSkip(name string) bool { return skipDirs[name] }

// ReadFileContent returns the content of a file as a string.
func (a *App) ReadFileContent(path string) (string, error) {
	path = tools.NormalizeHostPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveFileContent writes the content of an existing or new file.
func (a *App) SaveFileContent(path, content string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	path = tools.NormalizeHostPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		_ = a.rollback.Push(agent.OpWrite, path)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// GetWorkingDir returns the current working directory.
func (a *App) GetWorkingDir() string {
	wd, _ := os.Getwd()
	return filepath.ToSlash(wd)
}

// SetWorkingDir changes the working directory for the agent and file tree.
func (a *App) SetWorkingDir(dir string) error {
	a.mu.Lock()
	if a.agentRunning {
		a.mu.Unlock()
		return fmt.Errorf("cannot change workspace while an agent run is active")
	}
	a.mu.Unlock()

	dir = tools.NormalizeHostPath(dir)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}
	oldWD, _ := os.Getwd()
	if err := os.Chdir(abs); err != nil {
		return err
	}
	changed := !sameFilesystemPath(oldWD, abs)
	a.mu.Lock()
	a.cfg.Context.WorkspaceDir = filepath.ToSlash(abs)
	a.cfg.Context.OpenFolders = mergeWorkspaceFolders(a.cfg.Context.OpenFolders, settings.WorkspaceFolder{
		Path: filepath.ToSlash(abs),
		Name: filepath.Base(abs),
		Role: "root",
	})
	cfg := *a.cfg
	if changed {
		a.history.Clear()
		a.rollback.Clear()
	}
	a.mu.Unlock()
	_ = settings.Save(&cfg)
	if changed && a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:workspace_changed", filepath.ToSlash(abs))
	}
	return nil
}

func (a *App) ListWorkspaceFolders() []settings.WorkspaceFolder {
	a.mu.Lock()
	defer a.mu.Unlock()
	return normaliseAppWorkspaceFolders(a.cfg.Context.OpenFolders, a.cfg.Context.WorkspaceDir)
}

func (a *App) AddWorkspaceFolder(path, role string) ([]settings.WorkspaceFolder, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return a.ListWorkspaceFolders(), nil
	}
	abs, err := filepath.Abs(tools.NormalizeHostPath(path))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	a.mu.Lock()
	a.cfg.Context.OpenFolders = mergeWorkspaceFolders(a.cfg.Context.OpenFolders, settings.WorkspaceFolder{
		Path: filepath.ToSlash(abs),
		Name: filepath.Base(abs),
		Role: strings.TrimSpace(role),
	})
	a.cfg.Context.OpenFolders = normaliseAppWorkspaceFolders(a.cfg.Context.OpenFolders, a.cfg.Context.WorkspaceDir)
	cfg := *a.cfg
	a.mu.Unlock()
	if err := settings.Save(&cfg); err != nil {
		return nil, err
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:workspace_folders_changed", cfg.Context.OpenFolders)
	}
	return cfg.Context.OpenFolders, nil
}

func (a *App) RemoveWorkspaceFolder(path string) ([]settings.WorkspaceFolder, error) {
	path = filepath.ToSlash(tools.NormalizeHostPath(strings.TrimSpace(path)))
	a.mu.Lock()
	agentRoot := filepath.ToSlash(a.cfg.Context.WorkspaceDir)
	var next []settings.WorkspaceFolder
	for _, folder := range a.cfg.Context.OpenFolders {
		if sameFilesystemPath(folder.Path, path) {
			continue
		}
		next = append(next, folder)
	}
	if agentRoot != "" {
		next = mergeWorkspaceFolders(next, settings.WorkspaceFolder{Path: agentRoot, Name: filepath.Base(agentRoot), Role: "root"})
	}
	a.cfg.Context.OpenFolders = normaliseAppWorkspaceFolders(next, a.cfg.Context.WorkspaceDir)
	cfg := *a.cfg
	a.mu.Unlock()
	if err := settings.Save(&cfg); err != nil {
		return nil, err
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:workspace_folders_changed", cfg.Context.OpenFolders)
	}
	return cfg.Context.OpenFolders, nil
}

func (a *App) SelectWorkspaceFolder(defaultDir string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app is not ready")
	}
	if defaultDir == "" {
		defaultDir = a.GetWorkingDir()
	}
	defaultDir = tools.NormalizeHostPath(defaultDir)
	if info, err := os.Stat(defaultDir); err == nil && !info.IsDir() {
		defaultDir = filepath.Dir(defaultDir)
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Add folder to Explorer",
		DefaultDirectory: defaultDir,
	})
}

func (a *App) UpdateLabContext(target, vpnInterface, latestArtifact string) (LabStatus, error) {
	a.mu.Lock()
	a.cfg.Context.Lab.Target = strings.TrimSpace(target)
	a.cfg.Context.Lab.VPNInterface = strings.TrimSpace(vpnInterface)
	a.cfg.Context.Lab.LatestArtifact = filepath.ToSlash(strings.TrimSpace(latestArtifact))
	cfg := *a.cfg
	a.mu.Unlock()
	if err := settings.Save(&cfg); err != nil {
		return LabStatus{}, err
	}
	return a.GetLabStatus(), nil
}

func (a *App) GetLabStatus() LabStatus {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()
	wd, _ := os.Getwd()
	latest := strings.TrimSpace(cfg.Context.Lab.LatestArtifact)
	if latest == "" {
		latest = latestWorkspaceArtifact(normaliseAppWorkspaceFolders(cfg.Context.OpenFolders, cfg.Context.WorkspaceDir))
	}
	return LabStatus{
		AgentRoot:      filepath.ToSlash(wd),
		ShellBackend:   cfg.Tools.ShellBackend,
		ShellDistro:    cfg.Tools.ShellDistro,
		ShellUser:      cfg.Tools.ShellUser,
		Target:         cfg.Context.Lab.Target,
		VPNInterface:   cfg.Context.Lab.VPNInterface,
		LatestArtifact: filepath.ToSlash(latest),
		OpenFolders:    normaliseAppWorkspaceFolders(cfg.Context.OpenFolders, filepath.ToSlash(wd)),
	}
}

func (a *App) ScaffoldWorkspaceFolders(root string, names []string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = a.GetWorkingDir()
	}
	abs, err := filepath.Abs(tools.NormalizeHostPath(root))
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	var created []string
	for _, name := range names {
		name = strings.Trim(strings.TrimSpace(name), `/\`)
		if name == "" || strings.Contains(name, "..") {
			continue
		}
		path := filepath.Join(abs, filepath.FromSlash(name))
		if err := os.MkdirAll(path, 0o755); err != nil {
			return created, err
		}
		created = append(created, filepath.ToSlash(path))
	}
	return created, nil
}

// PickSaveFilePath opens a native save-file dialog and returns the chosen path,
// or "" if the user cancelled. Used by the frontend to save a reply to disk.
func (a *App) PickSaveFilePath(defaultName string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app is not ready")
	}
	if defaultName == "" {
		defaultName = "response.md"
	}
	return wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Save reply to file",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Markdown (*.md)", Pattern: "*.md"},
			{DisplayName: "Text (*.txt)", Pattern: "*.txt"},
			{DisplayName: "All files (*.*)", Pattern: "*.*"},
		},
	})
}

// SelectWorkingDir opens the native directory picker and updates the workspace.
func (a *App) SelectWorkingDir(defaultDir string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app is not ready")
	}
	if defaultDir == "" {
		defaultDir = a.GetWorkingDir()
	}
	selected, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Select working directory",
		DefaultDirectory: defaultDir,
	})
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", nil
	}
	if err := a.SetWorkingDir(selected); err != nil {
		return "", err
	}
	return a.GetWorkingDir(), nil
}

// SelectProjectInstructionFile opens a native file picker for a MAULER/master skill file.
func (a *App) SelectProjectInstructionFile(defaultPath string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app is not ready")
	}
	defaultDir := strings.TrimSpace(defaultPath)
	if defaultDir == "" {
		defaultDir = a.GetWorkingDir()
	}
	defaultDir = tools.NormalizeHostPath(defaultDir)
	if info, err := os.Stat(defaultDir); err == nil && !info.IsDir() {
		defaultDir = filepath.Dir(defaultDir)
	}
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Select project workflow file",
		DefaultDirectory: defaultDir,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Markdown (*.md)", Pattern: "*.md"},
			{DisplayName: "All files (*.*)", Pattern: "*.*"},
		},
	})
}

// SelectProjectInstructionDirectory opens a native directory picker for a folder
// of Markdown workflow/instruction files.
func (a *App) SelectProjectInstructionDirectory(defaultPath string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app is not ready")
	}
	defaultDir := strings.TrimSpace(defaultPath)
	if defaultDir == "" {
		defaultDir = a.GetWorkingDir()
	}
	defaultDir = tools.NormalizeHostPath(defaultDir)
	if info, err := os.Stat(defaultDir); err == nil && !info.IsDir() {
		defaultDir = filepath.Dir(defaultDir)
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Select project workflow folder",
		DefaultDirectory: defaultDir,
	})
}

// UseProjectInstructionFile registers an explicit workflow/instruction source
// as the "master" skill and injects it into the current chat for this turn.
func (a *App) UseProjectInstructionFile(path string) (settings.Settings, error) {
	a.mu.Lock()
	cfg := *a.cfg
	a.mu.Unlock()

	normalized := ""
	prompt := ""
	var err error
	if strings.TrimSpace(path) == "" {
		if err := deleteMasterSkillSource(); err != nil {
			return cfg, err
		}
	} else {
		var skill Skill
		skill, prompt, err = saveMasterSkillSource(path)
		if err != nil {
			return cfg, err
		}
		normalized = skill.SourcePath
	}
	if isMasterSkillSourceName(cfg.Context.MAULERMDPath) {
		cfg.Context.MAULERMDPath = ""
	}
	if err := settings.Save(&cfg); err != nil {
		return cfg, err
	}

	a.mu.Lock()
	*a.cfg = cfg
	if prompt != "" {
		a.history.Append(llm.Message{Role: llm.RoleSystem, Content: prompt})
	}
	a.mu.Unlock()

	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:project_instructions_changed", normalized)
	}
	return cfg, nil
}

// GetProjectInstructionsSummary returns the instruction sources currently active.
func (a *App) GetProjectInstructionsSummary() string {
	a.mu.Lock()
	cfg := a.cfg.Context
	a.mu.Unlock()
	summary := instructionDocsSummary(cfg)
	if skill, err := loadSkill("master"); err == nil && strings.TrimSpace(skill.SourcePath) != "" {
		if summary == "" || summary == "no project instruction files loaded" {
			summary = "no project instruction files loaded"
		}
		summary += "\n\nmaster skill: registered for lazy use (" + filepath.Base(tools.NormalizeHostPath(skill.SourcePath)) + ")"
	}
	return summary
}

// RenameFile renames or moves a file or directory.
func (a *App) RenameFile(oldPath, newPath string) error {
	oldPath = tools.NormalizeHostPath(oldPath)
	newPath = tools.NormalizeHostPath(newPath)
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}

// DeleteFile removes a file or directory (recursive for dirs).
func (a *App) DeleteFile(path string) error {
	path = tools.NormalizeHostPath(path)
	return os.RemoveAll(path)
}

// CreateFile creates an empty file (and any missing parent directories).
func (a *App) CreateFile(path string) error {
	path = tools.NormalizeHostPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// CreateDir creates a directory and any missing parents.
func (a *App) CreateDir(path string) error {
	path = tools.NormalizeHostPath(path)
	return os.MkdirAll(path, 0o755)
}

// GetHomeDir returns the current user's home directory.
func (a *App) GetHomeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.ToSlash(home)
}

// EncodeFileBase64 reads a file and returns base64 (for image display in chat).
func (a *App) EncodeFileBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// ---------------------------------------------------------------------------
// Artifact runner
// ---------------------------------------------------------------------------

// RunArtifact executes code from the artifact pane and streams stdout/stderr.
func (a *App) RunArtifact(lang, code string) error {
	a.mu.Lock()
	if a.artifactRunning {
		a.mu.Unlock()
		return fmt.Errorf("artifact is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.artifactRunning = true
	a.cancelArtifact = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.artifactRunning = false
			a.cancelArtifact = nil
			a.mu.Unlock()
			wailsruntime.EventsEmit(a.ctx, "mauler:artifact_done")
		}()

		wailsruntime.EventsEmit(a.ctx, "mauler:artifact_output", "")
		cmd, err := artifactCommand(ctx, lang, code)
		if err != nil {
			wailsruntime.EventsEmit(a.ctx, "mauler:artifact_output", err.Error()+"\n")
			return
		}
		writer := &artifactWriter{ctx: a.ctx}
		cmd.Stdout = writer
		cmd.Stderr = writer
		if err := cmd.Run(); err != nil && ctx.Err() == nil {
			wailsruntime.EventsEmit(a.ctx, "mauler:artifact_output", fmt.Sprintf("\n%s\n", err))
		}
	}()

	return nil
}

// StopArtifact cancels the running artifact process.
func (a *App) StopArtifact() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelArtifact != nil {
		a.cancelArtifact()
	}
}

type artifactWriter struct {
	ctx context.Context
}

func (w *artifactWriter) Write(p []byte) (int, error) {
	wailsruntime.EventsEmit(w.ctx, "mauler:artifact_output", string(p))
	return len(p), nil
}

func artifactCommand(ctx context.Context, lang, code string) (*exec.Cmd, error) {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "bash", "sh", "shell":
		name := "bash"
		if _, err := exec.LookPath(name); err != nil {
			if runtime.GOOS == "windows" {
				return nil, fmt.Errorf("bash is not available on PATH")
			}
			name = "sh"
		}
		return exec.CommandContext(ctx, name, "-c", code), nil
	case "powershell", "pwsh", "ps1":
		name := "pwsh"
		if _, err := exec.LookPath(name); err != nil {
			name = "powershell"
		}
		return exec.CommandContext(ctx, name, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", code), nil
	case "python", "python3", "py":
		name := "python"
		if _, err := exec.LookPath(name); err != nil {
			name = "python3"
		}
		return exec.CommandContext(ctx, name, "-c", code), nil
	case "javascript", "js", "node", "typescript", "ts":
		return exec.CommandContext(ctx, "node", "-e", code), nil
	default:
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
}

// ---------------------------------------------------------------------------
// Ping / connectivity
// ---------------------------------------------------------------------------

// Ping tests connectivity to the active profile's backend.
func (a *App) Ping() string {
	a.mu.Lock()
	cfg := *a.cfg
	profiles := *a.profiles
	a.mu.Unlock()

	profile := activeProfile(&cfg, &profiles)
	client, err := buildClient(profile)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		return fmt.Sprintf("unreachable: %v", err)
	}
	return "ok"
}

// ListModels returns the model list from the active backend.
func (a *App) ListModels() ([]string, error) {
	a.mu.Lock()
	cfg := *a.cfg
	profiles := *a.profiles
	a.mu.Unlock()

	profile := activeProfile(&cfg, &profiles)
	client, err := buildClient(profile)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.Models(ctx)
}

// PingProvider tests connectivity to a provider currently being edited.
func (a *App) PingProvider(provider settings.Provider) string {
	client, err := buildClient(settings.Profile{
		Backend:   provider.Backend,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	})
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		return fmt.Sprintf("unreachable: %v", err)
	}
	return "ok"
}

// ListModelsForProvider lists models from a provider currently being edited.
func (a *App) ListModelsForProvider(provider settings.Provider) ([]string, error) {
	client, err := buildClient(settings.Profile{
		Backend:   provider.Backend,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.Models(ctx)
}

// ListWSLDistros returns installed WSL distribution names.
func (a *App) ListWSLDistros() ([]string, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	out, err := exec.Command("wsl.exe", "-l", "-v").CombinedOutput()
	cleaned := cleanWSLOutput(string(out))
	if err != nil {
		return nil, fmt.Errorf("list WSL distros: %w: %s", err, strings.TrimSpace(cleaned))
	}
	return parseWSLDistros(cleaned), nil
}

// KillLocalInferenceServers stops stale InferenceBridge-managed llama.cpp
// processes that can keep VRAM/ports wedged after a failed load.
func (a *App) KillLocalInferenceServers() (MaintenanceResult, error) {
	if runtime.GOOS != "windows" {
		return MaintenanceResult{}, fmt.Errorf("local inference cleanup is currently implemented for Windows")
	}
	targets := []string{
		"llama-server.exe",
		"InferenceBridge.exe",
		"inference-bridge.exe",
	}
	var lines []string
	killed := 0
	for _, target := range targets {
		cmd := exec.Command("taskkill.exe", "/F", "/T", "/IM", target)
		out, err := cmd.CombinedOutput()
		text := strings.TrimSpace(cleanWSLOutput(string(out)))
		if text == "" {
			text = errString(err)
		}
		if err != nil {
			if strings.Contains(strings.ToLower(text), "not found") || strings.Contains(strings.ToLower(text), "no running instance") {
				lines = append(lines, fmt.Sprintf("%s: not running", target))
				continue
			}
			lines = append(lines, fmt.Sprintf("%s: %s", target, text))
			continue
		}
		killed++
		lines = append(lines, fmt.Sprintf("%s: stopped", target))
	}
	summary := fmt.Sprintf("Stopped %d inference process group(s)", killed)
	if killed == 0 {
		summary = "No matching inference processes were running"
	}
	return MaintenanceResult{Summary: summary, Lines: lines}, nil
}

// RestartWSL shuts down all WSL distributions so hung network/shell state can
// restart cleanly on the next WSL command.
func (a *App) RestartWSL() (MaintenanceResult, error) {
	if runtime.GOOS != "windows" {
		return MaintenanceResult{}, fmt.Errorf("WSL restart is only available on Windows")
	}
	cmd := exec.Command("wsl.exe", "--shutdown")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(cleanWSLOutput(string(out)))
	if err != nil {
		if text == "" {
			text = errString(err)
		}
		return MaintenanceResult{Summary: "WSL shutdown failed", Lines: []string{text}}, err
	}
	lines := []string{"wsl.exe --shutdown completed"}
	if text != "" {
		lines = append(lines, text)
	}
	return MaintenanceResult{Summary: "WSL stopped; it will restart on next use", Lines: lines}, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func cleanWSLOutput(text string) string {
	return strings.ReplaceAll(text, "\x00", "")
}

func parseWSLDistros(text string) []string {
	lines := strings.Split(cleanWSLOutput(text), "\n")
	var out []string
	seen := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if line == "" || strings.HasPrefix(strings.ToUpper(line), "NAME ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal agent loop
// ---------------------------------------------------------------------------

func (a *App) runAgentLoop(ctx context.Context, firstMsg llm.Message, profile settings.Profile, cfg *settings.Settings, autonomous bool, mode AgentMode, memories []MemoryEntry, skills []Skill, run TaskRun) {
	var finalSummary string
	var finalStatus = "done"
	run.addEvent("start", "Run started", fmt.Sprintf("mode=%s profile=%s model=%s autonomous=%t", mode.Name, cfg.ActiveProfile, profile.ModelID, autonomous))
	a.setRunState(&run, "planning", "Initial prompt accepted and run context created.")
	defer func() {
		if ctx.Err() != nil {
			finalStatus = "stopped"
			a.mu.Lock()
			reason := a.stopReason
			detail := a.stopDetail
			a.mu.Unlock()
			if reason == "" {
				reason = "context_canceled"
				detail = "The run context was cancelled before the agent finished."
			}
			run.stop(reason, detail)
			run.addEvent("stop", reason, detail)
		}
		if finalStatus == "done" && isBlockingStopReason(run.StopReason) {
			finalStatus = "stopped"
			run.addEvent("stop", "Run ended with unresolved blocking stop reason", run.StopDetail)
		}
		if finalStatus == "done" && requiresLivingDocUpdate(run.Prompt) && !runHasFileMutation(run) {
			finalStatus = "stopped"
			detail := "The user asked for a README/writeup/docs update, but the run completed without write_file or edit_file. Update the requested document before marking the task done."
			run.stopTerminal("documentation_missing", detail)
			run.addEvent("blocked", "Documentation update missing", detail)
		}
		if finalStatus == "done" {
			a.setRunState(&run, "done", "Run completed successfully.")
			run.addEvent("finish", "Run completed", "")
		} else if finalStatus == "error" && run.StopReason != "" {
			a.setRunState(&run, "failed", run.StopDetail)
			run.addEvent("finish", "Run ended with error", run.StopDetail)
		} else if finalStatus == "stopped" {
			a.setRunState(&run, "blocked", run.StopDetail)
		}
		run.finish(finalStatus, finalSummary)
		_ = saveTaskRun(run, &cfg.Logging)
		_ = a.saveRunMilestoneMemory(&run)
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "mauler:task_run", run)
			if suggestion := buildLearningSuggestion(&run); suggestion != nil {
				wailsruntime.EventsEmit(a.ctx, "mauler:suggest_learning", suggestion)
			}
		}
		a.mu.Lock()
		a.agentRunning = false
		a.cancelAgent = nil
		a.stopReason = ""
		a.stopDetail = ""
		a.mu.Unlock()
		wailsruntime.EventsEmit(a.ctx, "mauler:stream_done")
		a.autoSave()
	}()

	// Append system prompt on first turn
	a.mu.Lock()
	if a.history.TokenCount() == 0 {
		a.history.Append(llm.NewTextMessage(llm.RoleSystem, buildSystemPrompt(*cfg, mode, memories, skills)))
	}
	a.history.Append(firstMsg)
	a.mu.Unlock()

	wailsruntime.EventsEmit(a.ctx, "mauler:stream_start")

	client, err := buildClient(profile)
	if err != nil {
		finalStatus = "error"
		finalSummary = err.Error()
		run.stopTerminal("client_error", err.Error())
		run.addEvent("error", "Client setup failed", err.Error())
		wailsruntime.EventsEmit(a.ctx, "mauler:stream_error", err.Error())
		return
	}
	a.setRunState(&run, "model_loading", modelLoadKey(profile))
	if err := a.ensureModelLoaded(ctx, client, profile, func(attempt int, err error) {
		detail := fmt.Sprintf("attempt=%d error=%v", attempt, err)
		run.addEvent("retry", "Retrying model load", detail)
		a.setRunState(&run, "recovering", detail)
	}); err != nil {
		finalStatus = "error"
		finalSummary = err.Error()
		run.stopTerminal("model_load_error", err.Error())
		run.addEvent("error", "Model load failed", err.Error())
		wailsruntime.EventsEmit(a.ctx, "mauler:stream_error", err.Error())
		return
	}
	run.addEvent("model", "Model ready", modelLoadKey(profile))
	if err := saveRuntimeLockSnapshot(profile); err != nil {
		run.addEvent("runtime_lock", "Could not save runtime lock", err.Error())
	} else {
		lock := buildRuntimeLock(profile)
		if data, err := json.Marshal(lock); err == nil {
			run.addEvent("runtime_lock", "Saved runtime lock", string(data))
		}
	}
	if applyWorkingContextBudget(a, mode.ContextBudget, profile.CtxTokens) {
		run.addEvent("context_budget", "Applied agent working context budget", fmt.Sprintf("budget=%d profile_ctx=%d", mode.ContextBudget, profile.CtxTokens))
	}
	firstUserText := messageText(firstMsg) // for toolChoiceFor heuristic and research budgets
	budget := newTaskBudget(cfg.Tools, firstUserText)
	autoContinues := 0
	noToolContinues := 0           // consecutive auto-continues where the model made zero tool calls
	malformedToolContinues := 0    // consecutive retries caused by unparsed inline tool markup
	totalToolCallsMade := 0        // cumulative tool calls across all turns this task
	preOutputInferenceRetries := 0 // one-shot retry for backend failures before any model output
	toolBudgetSummaryRequested := false
	docRecoveryRequested := false
	docRecoveryPromptSent := false
	const maxAutoContinues = 8
	const maxMalformedToolContinues = 2
	logCfg := cfg.Logging
	var respBuf strings.Builder // accumulated response text for log_responses

agentLoop:
	for {
		if ctx.Err() != nil {
			return
		}

		toolBudgetExhausted := agentToolBudgetExhausted(cfg.Agents, len(run.Tools))
		toolDefs, toolChoice := toolDefsAndChoiceForTurn(a.registry, cfg.Tools, firstUserText, autoContinues, totalToolCallsMade)
		if toolBudgetExhausted {
			toolDefs = nil
			toolChoice = "none"
			if !toolBudgetSummaryRequested {
				toolBudgetSummaryRequested = true
				prompt := agentToolBudgetSummaryPrompt(cfg.Agents.MaxToolCalls)
				run.addEvent("continue", "Tool budget exhausted; requesting final text-only summary", prompt)
				a.setRunState(&run, "blocked", fmt.Sprintf("tool budget exhausted (%d calls)", cfg.Agents.MaxToolCalls))
				a.mu.Lock()
				a.history.Append(llm.NewTextMessage(llm.RoleUser, prompt))
				a.mu.Unlock()
			}
		}
		if docRecoveryRequested && requiresLivingDocUpdate(run.Prompt) && !runHasFileMutation(run) && !toolBudgetExhausted {
			toolDefs = filterToolDefsByName(toolDefs, "read_file", "write_file", "edit_file", "glob", "grep")
			toolChoice = "auto"
			if !docRecoveryPromptSent {
				docRecoveryPromptSent = true
				prompt := documentationRecoveryPrompt(run.Prompt, run.StopReason, run.StopDetail)
				run.addEvent("continue", "Forcing documentation recovery turn", prompt)
				a.setRunState(&run, "editing", "Documentation update required before continuing.")
				a.mu.Lock()
				a.history.Append(llm.NewTextMessage(llm.RoleUser, prompt))
				a.mu.Unlock()
			}
		}
		a.mu.Lock()
		needsCompact := a.history.NeedsCompactionWithReserve(cfg.Context.CompactionAt, compactionReserveTokens(toolDefs))
		a.mu.Unlock()
		if needsCompact {
			a.mu.Lock()
			cleared := a.history.ClearOldToolResults(4)
			needsCompact = a.history.NeedsCompactionWithReserve(cfg.Context.CompactionAt, compactionReserveTokens(toolDefs))
			a.mu.Unlock()
			if cleared.Cleared > 0 {
				run.addEvent("context_clear", "Cleared stale tool results", fmt.Sprintf("cleared=%d\nbefore_estimated_tokens=%d\nafter_estimated_tokens=%d", cleared.Cleared, cleared.BeforeTokens, cleared.AfterTokens))
			}
			if needsCompact {
				if compacted := a.doCompact(ctx, client, profile); compacted != nil {
					run.addEvent("compaction", compacted.Message(), compacted.Detail())
				}
			}
		}

		a.mu.Lock()
		msgs := a.history.Messages()
		a.mu.Unlock()
		if compacted := a.ensureRequestContextRoom(ctx, client, profile, cfg, toolDefs, &run); compacted != nil {
			run.addEvent("compaction", compacted.Message(), compacted.Detail())
			a.mu.Lock()
			msgs = a.history.Messages()
			a.mu.Unlock()
		}

		// After the configured threshold of tool calls, disable thinking for the
		// remainder of the run. Qwen3 tends to place tool calls inside the <think>
		// block when thinking is on and the context is heavy with prior tool results,
		// which causes grammar-triggered early termination.
		noThinkThreshold := cfg.Agents.NoThinkAfterToolCalls
		if noThinkThreshold <= 0 {
			noThinkThreshold = 3
		}
		forceNoThink := profile.Thinking && (totalToolCallsMade >= noThinkThreshold || noToolContinues > 0)
		req := buildChatRequest(profile, msgs, toolDefs, toolChoice, forceNoThink, shouldUseCodingParams(firstUserText, mode))
		a.setRunState(&run, "thinking", fmt.Sprintf("tool_choice=%s messages=%d tools=%d no_think=%v", toolChoice, len(msgs), len(toolDefs), forceNoThink))
		if len(toolDefs) > 0 {
			run.addEvent("tool_protocol_request", "Chat request tool protocol", fmt.Sprintf("client=%s model=%s tool_choice=%s tools=%d thinking=%v preserve_thinking=%v force_no_think=%v", client.Name(), profile.ModelID, toolChoice, len(toolDefs), req.EnableThinking, req.PreserveThinking, forceNoThink))
		}
		a.recordBackendRuntimeMismatch(ctx, client, profile, &run)

		ch, err := client.Chat(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if preOutputInferenceRetries < 1 && isRecoverableInferenceFailure(err.Error()) {
				preOutputInferenceRetries++
				run.addEvent("inference_retry", "Retrying chat request after pre-output backend failure", err.Error())
				if !sleepBeforeInferenceRetry(ctx) {
					return
				}
				continue agentLoop
			}
			finalStatus = "error"
			finalSummary = err.Error()
			run.stopTerminal("chat_error", err.Error())
			run.addEvent("error", "Chat request failed", err.Error())
			wailsruntime.EventsEmit(a.ctx, "mauler:stream_error", err.Error())
			return
		}

		var rawTextBuf strings.Builder
		emittedVisibleText := ""
		var thinkBuf strings.Builder
		var toolCalls []llm.ToolCallDef
		var usage *llm.Usage
		var wasTruncated bool

		for delta := range ch {
			if delta.Error != nil {
				if ctx.Err() != nil {
					return
				}
				if preOutputInferenceRetries < 1 && rawTextBuf.Len() == 0 && thinkBuf.Len() == 0 && len(toolCalls) == 0 && isRecoverableInferenceFailure(delta.Error.Error()) {
					preOutputInferenceRetries++
					run.addEvent("inference_retry", "Retrying stream after pre-output backend failure", delta.Error.Error())
					if !sleepBeforeInferenceRetry(ctx) {
						return
					}
					continue agentLoop
				}
				finalStatus = "error"
				finalSummary = delta.Error.Error()
				run.stopTerminal("stream_error", delta.Error.Error())
				run.addEvent("error", "Stream failed", delta.Error.Error())
				wailsruntime.EventsEmit(a.ctx, "mauler:stream_error", delta.Error.Error())
				return
			}
			if delta.Truncated {
				wasTruncated = true
				run.addEvent("truncated", "Model hit token limit", "finish_reason=length")
			}
			if delta.Thinking != "" {
				thinkBuf.WriteString(delta.Thinking)
				wailsruntime.EventsEmit(a.ctx, "mauler:thinking", delta.Thinking)
			}
			if delta.Content != "" {
				rawTextBuf.WriteString(delta.Content)
				visible := sanitizeVisibleModelText(rawTextBuf.String())
				switch {
				case visible == emittedVisibleText:
				case strings.HasPrefix(visible, emittedVisibleText):
					chunk := visible[len(emittedVisibleText):]
					emittedVisibleText = visible
					if chunk != "" {
						wailsruntime.EventsEmit(a.ctx, "mauler:delta", chunk)
					}
				default:
					emittedVisibleText = visible
					wailsruntime.EventsEmit(a.ctx, "mauler:stream_replace", visible)
				}
			}
			if len(delta.ToolCalls) > 0 {
				toolCalls = append(toolCalls, delta.ToolCalls...)
			}
			if delta.Usage != nil {
				usage = delta.Usage
			}
		}

		rawText := rawTextBuf.String()
		textBuf := strings.Builder{}
		textBuf.WriteString(sanitizeVisibleModelText(rawText))
		nativeToolCallCount := len(toolCalls)
		if nativeToolCallCount > 0 {
			run.addEvent("tool_protocol_native", "Backend returned structured tool_calls", toolProtocolDebugDetail(rawText, textBuf.String(), toolCalls, toolDefs))
		}

		repairText := rawText
		if strings.TrimSpace(repairText) == "" {
			repairText = textBuf.String()
		}
		if len(toolCalls) == 0 && strings.TrimSpace(repairText) != "" {
			repairDefs := toolDefs
			if len(repairDefs) == 0 && containsInlineToolMarkup(repairText) && toolChoice != "none" && !toolBudgetExhausted {
				repairDefs = a.registry.ToEnabledToolDefs(settings.EffectiveEnabledTools(cfg.Tools))
			}
			if repaired := parseInlineToolMarkup(repairText, repairDefs); len(repaired) > 0 {
				toolCalls = repaired
				textBuf.Reset()
				if prefix := visibleTextBeforeInlineToolMarkup(repairText); prefix != "" {
					textBuf.WriteString(prefix)
				}
				a.setRunState(&run, "recovering", "Model emitted tool markup as text; converting to structured tool calls.")
				run.addEvent("tool_protocol_repair", "Converted inline tool markup", toolProtocolDebugDetail(rawText, textBuf.String(), repaired, repairDefs))
				wailsruntime.EventsEmit(a.ctx, "mauler:tool_protocol_repair")
			}
		}
		if toolBudgetExhausted && len(toolCalls) > 0 {
			run.addEvent("blocked", "Ignored tool calls after agent tool budget was exhausted", toolProtocolDebugDetail(rawText, textBuf.String(), toolCalls, nil))
			toolCalls = nil
		}
		unrepairedToolMarkup := len(toolCalls) == 0 && containsInlineToolMarkup(repairText) && !toolBudgetExhausted && toolChoice != "none"
		if unrepairedToolMarkup {
			a.setRunState(&run, "recovering", "Model emitted tool markup text that could not be converted.")
			run.addEvent("tool_protocol_unrepaired", "Could not convert inline tool markup", toolProtocolDebugDetail(rawText, textBuf.String(), nil, toolDefs))
			wailsruntime.EventsEmit(a.ctx, "mauler:stream_replace", "")
		}

		// Normalize shell commands (HTML-unescape operators) BEFORE storing in history,
		// so the model never re-reads its own escaped commands and re-learns the
		// &amp;amp; escalation pattern. This also cleans what the UI shows and what the
		// executor receives (the per-call normalize below is then a no-op).
		for i := range toolCalls {
			toolCalls[i] = normalizeToolCallArguments(toolCalls[i])
		}

		visibleText := strings.TrimSpace(textBuf.String())
		a.mu.Lock()
		if !unrepairedToolMarkup && (visibleText != "" || len(toolCalls) > 0) {
			msg := llm.NewTextMessage(llm.RoleAssistant, textBuf.String())
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			a.history.Append(msg)
		}
		if usage != nil {
			a.history.SetExactCount(usage.PromptTokens + usage.CompletionTokens)
		}
		a.mu.Unlock()
		if !unrepairedToolMarkup {
			finalSummary = visibleText
		}
		if !unrepairedToolMarkup && logCfg.LogResponses && visibleText != "" {
			if respBuf.Len() > 0 {
				respBuf.WriteString("\n\n---\n\n")
			}
			respBuf.WriteString(textBuf.String())
			run.Response = trimRunText(respBuf.String())
		}
		// Emit the full thinking block so the UI can attach it to the message
		if thinkBuf.Len() > 0 {
			wailsruntime.EventsEmit(a.ctx, "mauler:thinking_done", thinkBuf.String())
		}
		if usage != nil {
			run.setTokens(usage.PromptTokens, usage.CompletionTokens)
			wailsruntime.EventsEmit(a.ctx, "mauler:usage", map[string]int{
				"prompt_tokens":     usage.PromptTokens,
				"completion_tokens": usage.CompletionTokens,
			})
		}

		if len(toolCalls) == 0 {
			text := strings.TrimSpace(textBuf.String())

			if unrepairedToolMarkup && autoContinues < maxAutoContinues && malformedToolContinues < maxMalformedToolContinues {
				autoContinues++
				noToolContinues++
				malformedToolContinues++
				if !sleepBeforeAutoContinue(ctx, autoContinues) {
					return
				}
				prompt := buildMalformedToolMarkupPrompt(repairText, toolDefs)
				run.addEvent("continue", fmt.Sprintf("Auto-continue %d/%d: malformed inline tool markup (%d/%d)", autoContinues, maxAutoContinues, malformedToolContinues, maxMalformedToolContinues), prompt)
				continueMsg := llm.NewTextMessage(llm.RoleUser, prompt)
				a.mu.Lock()
				a.history.Append(continueMsg)
				a.mu.Unlock()
				continue
			}
			if unrepairedToolMarkup {
				finalStatus = "stopped"
				detail := fmt.Sprintf("The model emitted inline tool markup that TheMauler could not convert after %d repair retries. Try again with a stricter tool-compatible profile/template, or inspect the Logs tab for the raw markup.", malformedToolContinues)
				run.stop("tool_protocol_unrepaired", detail)
				a.setRunState(&run, "blocked", detail)
				run.addEvent("stop", "Malformed tool markup retry limit reached", detail)
				return
			}

			// Thinking-only response: the model finished its <think> block and then
			// emitted finish_reason=stop with zero content and zero tool calls.
			// This is a common Qwen3 pattern when the intended output (e.g. a large
			// file write) is too large to fit within max_tokens in one shot.
			if text == "" && autoContinues < maxAutoContinues {
				stateMsg := "Model produced no visible content or tool call."
				if thinkBuf.Len() > 0 {
					stateMsg = "Thinking-only response produced no content or tool call."
				}
				a.setRunState(&run, "recovering", stateMsg)
				autoContinues++
				noToolContinues++
				if !sleepBeforeAutoContinue(ctx, autoContinues) {
					return
				}
				var prompt string
				thinkingText := strings.TrimSpace(thinkBuf.String())
				if looksAboutToAct(thinkingText) {
					prompt = buildDirectivePrompt(thinkingText)
				} else if needsInspectionTool(firstUserText) {
					prompt = "You produced no visible output and made no tool call. " +
						"The user explicitly asked you to inspect the repository/codebase/files. " +
						"Call an inspection tool RIGHT NOW, preferably glob with pattern \"**/*\" in the current workspace, then read files that actually appear in the result. " +
						"Do not explain. Your next response must be a tool call."
				} else if noToolContinues >= 2 {
					// Second+ thinking-only failure: the output is almost certainly too
					// large for one tool call. Force the model to chunk the work.
					prompt = "You have thought about this multiple times but still produced no tool call. " +
						"The content is likely too large to fit in a single response given the current max_tokens budget. " +
						"SOLUTION — write it in chunks:\n" +
						"1. Call write_file NOW with only the FIRST SECTION of the content (first 80-120 lines or first major heading block). Do NOT try to include everything.\n" +
						"2. For each remaining section, call write_file again with append=true to add it to the end of the file.\n" +
						"Start immediately — call write_file with the first chunk right now. No explanation."
				} else {
					prompt = "You completed your reasoning but produced no output and made no tool calls. " +
						"If the output is large, write only the FIRST SECTION now using write_file, then append the rest in follow-up calls (write_file with append=true). " +
						"Make a tool call immediately — do not explain."
				}
				run.addEvent("continue", fmt.Sprintf("Auto-continue %d/%d: empty output (noToolContinues=%d)", autoContinues, maxAutoContinues, noToolContinues), prompt)
				continueMsg := llm.NewTextMessage(llm.RoleUser, prompt)
				a.mu.Lock()
				a.history.Append(continueMsg)
				a.mu.Unlock()
				continue
			}

			// finish_reason == "length": model was hard-cut by max_tokens.
			// This also fires when the stream parser detected truncated tool-call
			// JSON (finish_reason was "tool_calls" but arguments were invalid).
			if wasTruncated && autoContinues < maxAutoContinues {
				a.setRunState(&run, "recovering", "Model response hit token limit.")
				autoContinues++
				noToolContinues++
				if !sleepBeforeAutoContinue(ctx, autoContinues) {
					return
				}
				var prompt string
				// text is empty when the model was cut off in the middle of
				// generating a tool call — give a targeted chunking directive.
				if text == "" {
					prompt = "Your tool call was cut off by the token limit before the arguments were complete. " +
						"The file you are trying to write is too large for a single response.\n\n" +
						"SOLUTION — write the file in sections:\n" +
						"1. Call write_file NOW with only the FIRST SECTION (first 60-80 lines). Keep it short.\n" +
						"2. For every remaining section call write_file again with append=true.\n" +
						"Do NOT try to include the whole file in one call. Make the first write_file call right now."
				} else if noToolContinues >= 2 || looksAboutToAct(text) {
					prompt = buildDirectivePrompt(text)
				} else {
					tail := text
					if len(tail) > 400 {
						tail = tail[len(tail)-400:]
					}
					prompt = fmt.Sprintf(
						"Your response was cut off by the token limit. Continue from exactly where you left off. Your last output ended with:\n\n%s",
						tail,
					)
				}
				run.addEvent("continue", fmt.Sprintf("Auto-continue %d/%d after truncation", autoContinues, maxAutoContinues), prompt)
				continueMsg := llm.NewTextMessage(llm.RoleUser, prompt)
				a.mu.Lock()
				a.history.Append(continueMsg)
				a.mu.Unlock()
				continue
			}

			// Auto-continue if the model looks like it stopped mid-task naturally
			// or if it narrated an immediate tool action without making one.
			aboutToAct := looksAboutToAct(text)
			if autoContinues < maxAutoContinues && (looksIncomplete(text) || aboutToAct) {
				a.setRunState(&run, "recovering", "Model appeared incomplete or narrated a tool action without making one.")
				autoContinues++
				noToolContinues++
				if !sleepBeforeAutoContinue(ctx, autoContinues) {
					return
				}
				var prompt string
				if aboutToAct || noToolContinues >= 2 {
					prompt = buildDirectivePrompt(text)
				} else {
					tail := text
					if len(tail) > 400 {
						tail = tail[len(tail)-400:]
					}
					prompt = fmt.Sprintf(
						"You stopped generating mid-task. Continue from exactly where you left off — do NOT re-read files you already processed. Your last output ended with:\n\n%s",
						tail,
					)
				}
				reason := "incomplete output"
				if aboutToAct {
					reason = "said it would act but made no tool call"
				}
				run.addEvent("continue", fmt.Sprintf("Auto-continue %d/%d: %s", autoContinues, maxAutoContinues, reason), prompt)
				continueMsg := llm.NewTextMessage(llm.RoleUser, prompt)
				a.mu.Lock()
				a.history.Append(continueMsg)
				a.mu.Unlock()
				continue
			}
			if autoContinues >= maxAutoContinues && (wasTruncated || looksIncomplete(text) || aboutToAct) {
				finalStatus = "stopped"
				detail := "The model still appeared incomplete after the auto-continue limit. Raise max output tokens/context or continue manually from the last message."
				run.stop("auto_continue_exhausted", detail)
				a.setRunState(&run, "blocked", detail)
				run.addEvent("stop", "Auto-continue limit reached", detail)
			}
			if text == "" && finalStatus != "stopped" {
				finalStatus = "stopped"
				detail := "The model returned no visible content and no tool calls."
				run.stop("empty_model_response", detail)
				a.setRunState(&run, "blocked", detail)
				run.addEvent("stop", "Empty model response", detail)
			}
			return
		}
		// Model made tool calls this round — reset the narration-without-acting streak
		// and record the cumulative count so toolChoiceFor stays "auto" for all
		// subsequent turns.
		noToolContinues = 0
		malformedToolContinues = 0
		totalToolCallsMade += len(toolCalls)

		logInput := func(s string) string {
			if logCfg.LogToolInputs {
				return s
			}
			return ""
		}
		logResult := func(s string) string {
			if logCfg.LogToolResults {
				return s
			}
			return ""
		}
		var toolResultMsgs []llm.Message
		for _, tc := range toolCalls {
			tc = normalizeToolCallArguments(tc)
			a.setRunState(&run, stateForTool(tc.Function.Name), tc.Function.Name)
			if cfg.Agents.MaxToolCalls > 0 && len(run.Tools) >= cfg.Agents.MaxToolCalls {
				result := fmt.Sprintf("agent tool-call budget exhausted (%d calls). Stop tools now, summarize progress, and ask before continuing.", cfg.Agents.MaxToolCalls)
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, result))
				run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(result), "blocked", 0)
				run.stop("tool_budget_exhausted", result)
				a.setRunState(&run, "blocked", result)
				run.addEvent("blocked", "Agent tool-call budget exhausted", result)
				wailsruntime.EventsEmit(a.ctx, "mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": result,
				})
				continue
			}
			run.addEvent("tool_call", tc.Function.Name, logInput(string(tc.Function.Arguments)))
			wailsruntime.EventsEmit(a.ctx, "mauler:tool_call", map[string]string{
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": string(tc.Function.Arguments),
			})

			tool, isKnown := a.registry.Get(tc.Function.Name)
			if !cfg.Tools.Enabled || !toolEnabled(settings.EffectiveEnabledTools(cfg.Tools), tc.Function.Name) {
				result := fmt.Sprintf("tool %q is disabled in settings", tc.Function.Name)
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, result))
				run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(result), "disabled", 0)
				run.stop("tool_disabled", result)
				a.setRunState(&run, "blocked", result)
				run.addEvent("blocked", "Tool disabled", result)
				wailsruntime.EventsEmit(a.ctx, "mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": result,
				})
				continue
			}
			if isKnown && shouldConfirmTool(tool, cfg, tc) && !autonomous {
				confirmed := a.awaitConfirm(ctx, tc)
				if !confirmed {
					result := "user denied this operation"
					toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, result))
					run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(result), "denied", 0)
					run.stop("tool_denied", fmt.Sprintf("%s was denied by the user.", tc.Function.Name))
					a.setRunState(&run, "blocked", tc.Function.Name+" was denied by the user.")
					run.addEvent("denied", "Tool confirmation denied", tc.Function.Name)
					wailsruntime.EventsEmit(a.ctx, "mauler:tool_result", map[string]string{
						"id": tc.ID, "name": tc.Function.Name, "result": result,
					})
					continue
				}
			}

			if blocked := budget.before(tc.Function.Name); blocked != "" {
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, blocked))
				run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(blocked), "blocked", 0)
				stopReason := stopReasonForBudgetBlock(tc.Function.Name, blocked)
				run.stop(stopReason, blocked)
				a.setRunState(&run, "blocked", blocked)
				run.addEvent("blocked", "Tool budget blocked call", blocked)
				if requiresLivingDocUpdate(run.Prompt) && !runHasFileMutation(run) && isBlockingStopReason(stopReason) {
					docRecoveryRequested = true
				}
				wailsruntime.EventsEmit(a.ctx, "mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": blocked,
				})
				continue
			}

			if isKnown && isWriteTool(tc.Function.Name) {
				if snapPath := extractPath(tc); snapPath != "" {
					snapPath = tools.NormalizeHostPath(snapPath)
					_ = a.rollback.Push(agent.OpWrite, snapPath)
				}
			}

			toolStart := time.Now()
			var result string
			var runErr error
			if isShellTool(tc.Function.Name) &&
				shouldUseSharedTerminal(cfg.Tools, tc.Function.Name) &&
				shellRequestsSession(tc.Function.Arguments) {
				// Persistence explicitly requested (interactive/reverse shell, persistent
				// cd/env) — use the live shared terminal.
				result, runErr = a.runSharedTerminalShell(ctx, tc.Function.Name, tc.Function.Arguments, cfg.Tools.BashTimeout)
				if errors.Is(runErr, errSharedTerminalUnsupported) {
					result, runErr = a.runIsolatedShellEcho(ctx, tc, cfg.Tools.BashTimeout)
				}
			} else if isShellTool(tc.Function.Name) {
				// Default: isolated per-command exec — deterministic, cannot hang the
				// session — mirrored to the visible terminal so the user still sees it.
				result, runErr = a.runIsolatedShellEcho(ctx, tc, cfg.Tools.BashTimeout)
			} else {
				result, runErr = a.registry.Run(ctx, tc)
			}
			toolDurMs := time.Since(toolStart).Milliseconds()
			if runErr != nil {
				result = toolErrorResult(result, runErr)
				if isMalformedToolArgsError(runErr) {
					result += "\nRecovery: the tool arguments were malformed JSON, usually because the model emitted an incomplete function call. Retry the same tool with complete valid JSON arguments only."
				}
			} else if isWriteTool(tc.Function.Name) {
				if verification := verifyMutationResult(tc); verification != "" {
					result = result + "\n" + verification
				}
			}
			if guarded, findings := guardToolResult(tc.Function.Name, result, cfg.Tools.RedactSecrets); len(findings) > 0 {
				result = guarded
				run.addEvent("guardrail", "Tool result guardrail applied", fmt.Sprintf("%s: %s", tc.Function.Name, strings.Join(findings, ", ")))
			}
			budget.after(tc.Function.Name, result, runErr)
			historyResult := result
			if isShellTool(tc.Function.Name) {
				historyResult = summarizeShellResultForContext(result, cfg.Tools.MaxToolResultChars)
			}
			toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, historyResult))
			status := "done"
			if runErr != nil {
				status = "error"
				a.setRunState(&run, "recovering", fmt.Sprintf("%s failed: %v", tc.Function.Name, runErr))
				if isMalformedToolArgsError(runErr) {
					run.addEvent("tool_error", "Malformed tool arguments", fmt.Sprintf("%s: %v", tc.Function.Name, runErr))
				} else {
					run.addEvent("tool_error", "Recoverable tool failure", fmt.Sprintf("%s: %v", tc.Function.Name, runErr))
				}
			}
			run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(result), status, toolDurMs)
			wailsruntime.EventsEmit(a.ctx, "mauler:tool_result", map[string]string{
				"id": tc.ID, "name": tc.Function.Name, "result": result,
			})
		}

		a.mu.Lock()
		maxChars := cfg.Tools.MaxToolResultChars
		for _, m := range toolResultMsgs {
			if maxChars > 0 {
				if s, ok := m.Content.(string); ok {
					m.Content = truncateToolResult(s, maxChars)
				}
			}
			a.history.Append(m)
		}
		a.mu.Unlock()
		finalSummary = textBuf.String()
	}
}

// awaitConfirm blocks until the user responds or context is cancelled.
func (a *App) awaitConfirm(ctx context.Context, tc llm.ToolCallDef) bool {
	ch := make(chan bool, 1)
	a.mu.Lock()
	a.confirmCh = ch
	a.mu.Unlock()

	wailsruntime.EventsEmit(a.ctx, "mauler:confirm", map[string]string{
		"id":    tc.ID,
		"name":  tc.Function.Name,
		"input": string(tc.Function.Arguments),
	})

	defer func() {
		a.mu.Lock()
		a.confirmCh = nil
		a.mu.Unlock()
	}()

	select {
	case allow := <-ch:
		return allow
	case <-ctx.Done():
		return false
	}
}

type compactionResult struct {
	BeforeMessages  int
	AfterMessages   int
	BeforeTokens    int
	AfterTokens     int
	OmittedMessages int
	Fallback        bool
	Error           string
}

func (r compactionResult) Message() string {
	source := "LLM summary"
	if r.Fallback {
		source = "fallback summary"
	}
	return fmt.Sprintf("Context compacted with %s: %d -> %d messages, %d -> %d estimated tokens", source, r.BeforeMessages, r.AfterMessages, r.BeforeTokens, r.AfterTokens)
}

func (r compactionResult) Detail() string {
	parts := []string{
		fmt.Sprintf("omitted_messages=%d", r.OmittedMessages),
		fmt.Sprintf("before_messages=%d", r.BeforeMessages),
		fmt.Sprintf("after_messages=%d", r.AfterMessages),
		fmt.Sprintf("before_estimated_tokens=%d", r.BeforeTokens),
		fmt.Sprintf("after_estimated_tokens=%d", r.AfterTokens),
	}
	if r.Error != "" {
		parts = append(parts, "summary_error="+r.Error)
	}
	return strings.Join(parts, "\n")
}

func (a *App) ensureRequestContextRoom(ctx context.Context, client llm.Client, profile settings.Profile, cfg *settings.Settings, toolDefs []llm.ToolDef, run *TaskRun) *compactionResult {
	limit := a.requestContextLimit(ctx, client, profile)
	if limit <= 0 {
		return nil
	}

	a.mu.Lock()
	msgs := a.history.Messages()
	beforeMessages := len(msgs)
	beforeTokens := a.history.TokenCount()
	a.mu.Unlock()

	estimated := estimateChatPromptTokens(msgs, toolDefs)
	if estimated+contextOverflowMargin(limit) < limit {
		return nil
	}

	a.mu.Lock()
	cleared := a.history.ClearOldToolResults(0)
	msgs = a.history.Messages()
	a.mu.Unlock()
	if cleared.Cleared > 0 && run != nil {
		run.addEvent("context_clear", "Cleared all stale tool results before request overflow", fmt.Sprintf("cleared=%d\nbefore_estimated_tokens=%d\nafter_estimated_tokens=%d\nrequest_estimate=%d\ncontext_limit=%d", cleared.Cleared, cleared.BeforeTokens, cleared.AfterTokens, estimated, limit))
	}
	estimated = estimateChatPromptTokens(msgs, toolDefs)
	if estimated+contextOverflowMargin(limit) < limit {
		return &compactionResult{
			BeforeMessages:  beforeMessages,
			AfterMessages:   len(msgs),
			BeforeTokens:    beforeTokens,
			AfterTokens:     cleared.AfterTokens,
			OmittedMessages: 0,
			Fallback:        true,
		}
	}

	omitted := 0
	summary := ""
	for _, keep := range []struct {
		first int
		last  int
	}{
		{first: 1, last: 4},
		{first: 1, last: 2},
		{first: 0, last: 2},
		{first: 0, last: 1},
	} {
		summaryMsgs := compactionSummaryMessages(msgs, keep.first, keep.last)
		if len(summaryMsgs) == 0 {
			continue
		}
		summary = deterministicCompactionSummary(summaryMsgs, fmt.Errorf("preflight context estimate %d plus safety margin would exceed loaded context %d", estimated, limit))
		a.mu.Lock()
		a.history.Compact(summary, keep.first, keep.last)
		msgs = a.history.Messages()
		a.mu.Unlock()
		omitted += len(summaryMsgs)
		estimated = estimateChatPromptTokens(msgs, toolDefs)
		if estimated+contextOverflowMargin(limit) < limit {
			break
		}
	}
	a.mu.Lock()
	afterMessages := len(a.history.Messages())
	afterTokens := a.history.TokenCount()
	a.mu.Unlock()
	if summary != "" && a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:compact", summary)
	}
	if summary != "" {
		a.rememberCompactionSummary(summary)
	}
	if omitted == 0 {
		return nil
	}
	return &compactionResult{
		BeforeMessages:  beforeMessages,
		AfterMessages:   afterMessages,
		BeforeTokens:    beforeTokens,
		AfterTokens:     afterTokens,
		OmittedMessages: omitted,
		Fallback:        true,
		Error:           "preflight context overflow avoided",
	}
}

// doCompact summarises old history.
func (a *App) doCompact(ctx context.Context, client llm.Client, profile settings.Profile) *compactionResult {
	a.mu.Lock()
	msgs := a.history.Messages()
	beforeTokens := a.history.TokenCount()
	a.mu.Unlock()

	summaryMsgs := compactionSummaryMessages(msgs, 2, 8)
	if len(summaryMsgs) == 0 {
		return nil
	}
	req := llm.Request{
		Messages:    append(summaryMsgs, llm.NewTextMessage(llm.RoleUser, structuredCompactionPrompt())),
		MaxTokens:   512,
		Temperature: 0.3,
	}

	var sb strings.Builder
	var compactErr error
	usedFallback := false
	if limit := a.requestContextLimit(ctx, client, profile); limit > 0 && estimateChatPromptTokens(req.Messages, nil)+contextOverflowMargin(limit) >= limit {
		usedFallback = true
		compactErr = fmt.Errorf("compaction summary request would exceed loaded context %d", limit)
	} else if ch, err := client.Chat(ctx, req); err != nil {
		compactErr = err
	} else {
		for delta := range ch {
			if delta.Error != nil {
				compactErr = delta.Error
				break
			}
			sb.WriteString(delta.Content)
		}
	}

	summary := sb.String()
	if summary == "" {
		usedFallback = true
		if compactErr != nil {
			summary = deterministicCompactionSummary(summaryMsgs, compactErr)
		} else {
			summary = deterministicCompactionSummary(summaryMsgs, nil)
		}
	}

	a.mu.Lock()
	a.history.Compact(summary, 2, 8)
	afterMessages := len(a.history.Messages())
	afterTokens := a.history.TokenCount()
	a.mu.Unlock()
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:compact", summary)
	}
	a.rememberCompactionSummary(summary)
	result := &compactionResult{
		BeforeMessages:  len(msgs),
		AfterMessages:   afterMessages,
		BeforeTokens:    beforeTokens,
		AfterTokens:     afterTokens,
		OmittedMessages: len(summaryMsgs),
		Fallback:        usedFallback,
	}
	if compactErr != nil {
		result.Error = compactErr.Error()
	}
	return result
}

func (a *App) rememberCompactionSummary(summary string) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	a.mu.Lock()
	enabled := a.cfg != nil && a.cfg.Memory.Enabled
	a.mu.Unlock()
	if !enabled {
		return
	}
	_, _ = a.SaveMemoryEntry(MemoryEntry{
		Scope:      workspaceScope(),
		Title:      "Compacted session state",
		Content:    summary,
		Tags:       []string{"compaction", "session-state"},
		Kind:       "decision",
		Importance: 4,
	})
}

func structuredCompactionPrompt() string {
	return `Compact only the messages above into this exact Markdown structure. Keep it terse but high fidelity. Preserve user requirements, constraints, decisions, file paths, commands, tool failures, partial writes, and the next recovery step. Do not invent facts.

## Objective
- ...

## User Requirements
- ...

## Decisions
- ...

## Files And Symbols
- ...

## Commands And Results
- ...

## Open Tasks
- ...

## Risks And Failures
- ...

## Next Action
- ...`
}

func compactionReserveTokens(toolDefs []llm.ToolDef) int {
	reserve := 4096
	for _, def := range toolDefs {
		reserve += len(def.Function.Name)/4 + len(def.Function.Description)/4 + len(def.Function.Parameters)/4 + 20
	}
	if reserve > 14000 {
		return 14000
	}
	return reserve
}

func (a *App) requestContextLimit(ctx context.Context, client llm.Client, profile settings.Profile) int {
	limit := profile.CtxTokens
	cq, ok := client.(interface {
		ActualContextLength(context.Context) int
	})
	if !ok {
		return limit
	}
	key := modelLoadKey(profile)
	a.mu.Lock()
	if a.ctxLimitKey == key && a.ctxLimitVal > 0 {
		cached := a.ctxLimitVal
		a.mu.Unlock()
		return cached
	}
	a.mu.Unlock()

	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	actual := cq.ActualContextLength(qctx)
	cancel()
	if actual <= 0 {
		// Transient query failure — fall back to the profile budget but don't cache,
		// so a later turn can pick up the real loaded context.
		return limit
	}
	a.mu.Lock()
	a.ctxLimitKey = key
	a.ctxLimitVal = actual
	a.mu.Unlock()
	return actual
}

func contextOverflowMargin(limit int) int {
	if limit <= 0 {
		return 0
	}
	margin := limit / 12
	if margin < 2048 {
		margin = 2048
	}
	if margin > 6144 {
		margin = 6144
	}
	return margin
}

func estimateChatPromptTokens(msgs []llm.Message, toolDefs []llm.ToolDef) int {
	chars := 0
	for _, msg := range msgs {
		chars += len(msg.Role) + len(msg.Name) + len(msg.ToolCallID) + 24
		chars += len(messageText(msg))
		if len(msg.ToolCalls) > 0 {
			if data, err := json.Marshal(msg.ToolCalls); err == nil {
				chars += len(data)
			}
		}
	}
	if len(toolDefs) > 0 {
		if data, err := json.Marshal(toolDefs); err == nil {
			chars += len(data)
		}
	}
	// Use a deliberately conservative chars/token ratio. llama.cpp applies the
	// final chat template and tool grammar after our in-memory history estimate,
	// and local models can reject requests that miss n_ctx by only a few tokens.
	return chars/3 + 512
}

func compactionSummaryMessages(msgs []llm.Message, keepFirst, keepLast int) []llm.Message {
	if len(msgs) <= keepFirst+keepLast+1 {
		return nil
	}
	i := 0
	for i < len(msgs) && msgs[i].Role == llm.RoleSystem {
		i++
	}
	start := i + keepFirst
	if start > len(msgs) {
		start = len(msgs)
	}
	end := len(msgs) - keepLast
	if end < start {
		end = start
	}
	out := make([]llm.Message, 0, end-start)
	for _, msg := range msgs[start:end] {
		out = append(out, compactMessageForSummary(msg))
	}
	return out
}

func compactMessageForSummary(msg llm.Message) llm.Message {
	msg.Content = truncateRunes(messageText(msg), 2400)
	if len(msg.ToolCalls) > 0 {
		calls := make([]llm.ToolCallDef, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			if len(tc.Function.Arguments) > 1200 {
				preview, _ := json.Marshal(map[string]any{
					"truncated": true,
					"preview":   truncateRunes(string(tc.Function.Arguments), 1200),
				})
				tc.Function.Arguments = json.RawMessage(preview)
			}
			calls = append(calls, tc)
		}
		msg.ToolCalls = calls
	}
	return msg
}

func deterministicCompactionSummary(msgs []llm.Message, compactErr error) string {
	var sb strings.Builder
	if compactErr != nil {
		sb.WriteString("LLM compaction failed: " + compactErr.Error() + "\n")
	}
	sb.WriteString("## Objective\n")
	sb.WriteString("- Continue the current user task using the surviving recent context.\n\n")
	sb.WriteString("## User Requirements\n")
	sb.WriteString("- See preserved user messages below.\n\n")
	sb.WriteString("## Decisions\n")
	sb.WriteString("- No structured decisions were recovered by fallback compaction.\n\n")
	sb.WriteString("## Files And Symbols\n")
	sb.WriteString("- See file paths and tool outputs in recent omitted activity.\n\n")
	sb.WriteString("## Commands And Results\n")
	sb.WriteString("- See shell/tool entries in recent omitted activity.\n\n")
	sb.WriteString("## Open Tasks\n")
	sb.WriteString("- Resume from the latest surviving messages and avoid repeating cleared work unless needed.\n\n")
	sb.WriteString("## Risks And Failures\n")
	if compactErr != nil {
		sb.WriteString("- LLM compaction failed; this fallback is lossy.\n\n")
	} else {
		sb.WriteString("- Fallback compaction is lossy.\n\n")
	}
	sb.WriteString("## Next Action\n")
	sb.WriteString("- Inspect the latest surviving messages, then continue the task.\n\n")
	sb.WriteString("## Recent Omitted Activity\n")
	start := 0
	if len(msgs) > 12 {
		start = len(msgs) - 12
	}
	for _, msg := range msgs[start:] {
		text := strings.TrimSpace(messageText(msg))
		if len(msg.ToolCalls) > 0 {
			names := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			text = "tool calls: " + strings.Join(names, ", ") + " " + text
		}
		if msg.Role == llm.RoleTool && msg.Name != "" {
			text = msg.Name + ": " + text
		}
		text = truncateRunes(strings.Join(strings.Fields(text), " "), 500)
		if text == "" {
			continue
		}
		sb.WriteString("- " + msg.Role + ": " + text + "\n")
	}
	return sb.String()
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "... [truncated]"
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newToolResultMsg(id, name, result string) llm.Message {
	return llm.Message{
		Role:       llm.RoleTool,
		Content:    result,
		ToolCallID: id,
		Name:       name,
	}
}

func toolErrorResult(result string, err error) string {
	result = strings.TrimRight(result, "\n\r ")
	if result == "" {
		return fmt.Sprintf("error: %v", err)
	}
	return result + "\nerror: " + err.Error()
}

func (a *App) setRunState(run *TaskRun, state, detail string) {
	if run == nil {
		return
	}
	prev := run.State
	run.setState(state, detail)
	if run.State == prev || a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "mauler:run_state", map[string]string{
		"id":     run.ID,
		"state":  run.State,
		"detail": strings.TrimSpace(detail),
	})
}

// truncateToolResult caps tool output before it enters the conversation history.
// The model only needs the key information; extremely long outputs bloat context fast.
func truncateToolResult(result string, maxChars int) string {
	if maxChars <= 0 || len(result) <= maxChars {
		return result
	}
	keep := maxChars - 120
	if keep < 80 {
		keep = 80
	}
	half := keep / 2
	head := result[:half]
	tail := result[len(result)-half:]
	return fmt.Sprintf("%s\n\n[... %d chars truncated for context ...]\n\n%s",
		head, len(result)-keep, tail)
}

func isShellTool(name string) bool {
	return name == "shell" || name == "bash"
}

func normalizeToolCallArguments(tc llm.ToolCallDef) llm.ToolCallDef {
	if !isShellTool(tc.Function.Name) {
		return tc
	}
	var args map[string]interface{}
	if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
		return tc
	}
	command, ok := args["command"].(string)
	if !ok {
		return tc
	}
	normalized := tools.NormalizeShellCommandText(command)
	if normalized == command {
		return tc
	}
	args["command"] = normalized
	raw, err := marshalToolArgsNoHTMLEscape(args)
	if err != nil {
		return tc
	}
	tc.Function.Arguments = raw
	return tc
}

func marshalToolArgsNoHTMLEscape(args map[string]interface{}) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(args); err != nil {
		return nil, err
	}
	return json.RawMessage(strings.TrimSpace(buf.String())), nil
}

func shouldUseSharedTerminal(cfg settings.ToolsConfig, toolName string) bool {
	return isShellTool(toolName) && strings.EqualFold(strings.TrimSpace(cfg.ShellMode), "shared_terminal")
}

// shellRequestsSession reports whether a shell tool call opted into the persistent
// shared terminal via session=true. Default (absent/false) routes to isolated exec.
func shellRequestsSession(raw json.RawMessage) bool {
	var p struct {
		Session bool `json:"session"`
	}
	_ = json.Unmarshal(raw, &p)
	return p.Session
}

func shellCommandAndTimeout(raw json.RawMessage) (string, int) {
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	_ = json.Unmarshal(raw, &p)
	return strings.TrimSpace(p.Command), p.Timeout
}

// runIsolatedShellEcho runs a shell command in an isolated shell (via the registry)
// and mirrors the command + output to the visible terminal pane, so isolated exec
// keeps the live-terminal visibility users had under shared_terminal mode.
func (a *App) runIsolatedShellEcho(ctx context.Context, tc llm.ToolCallDef, defaultTimeout int) (string, error) {
	cmd, reqTimeout := shellCommandAndTimeout(tc.Function.Arguments)
	timeout := defaultTimeout
	if reqTimeout > 0 {
		timeout = reqTimeout
	}
	runID := sharedTerminalRunID()
	emit := a.ctx != nil && cmd != ""
	if emit {
		wailsruntime.EventsEmit(a.ctx, "mauler:terminal_command_start", map[string]string{
			"id": runID, "session": "isolated", "command": cmd, "timeout": strconv.Itoa(timeout),
		})
	}
	result, err := a.registry.Run(ctx, tc)
	if emit {
		lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
		if len(lines) > 500 {
			lines = append(lines[:500], "[... output truncated in terminal view; full result in Logs ...]")
		}
		for _, line := range lines {
			wailsruntime.EventsEmit(a.ctx, "mauler:shell_output", map[string]string{
				"id": runID, "data": line, "stream": "stdout",
			})
		}
		exit := "0"
		if err != nil {
			exit = "1"
		}
		wailsruntime.EventsEmit(a.ctx, "mauler:terminal_command_done", map[string]string{
			"id": runID, "session": "isolated", "exit_code": exit,
		})
	}
	return result, err
}

func summarizeShellResultForContext(result string, maxChars int) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return result
	}
	capChars := maxChars
	if capChars <= 0 || capChars > 5000 {
		capChars = 5000
	}
	if len(result) <= capChars {
		return result
	}
	lines := strings.Split(result, "\n")
	var interesting []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if trimmed == "" {
			continue
		}
		if strings.Contains(lower, "open") ||
			strings.Contains(lower, "filtered") ||
			strings.Contains(lower, "vulner") ||
			strings.Contains(lower, "found") ||
			strings.Contains(lower, "http") ||
			strings.Contains(lower, "service") ||
			strings.Contains(lower, "login") ||
			strings.Contains(lower, "error") ||
			strings.Contains(lower, "warning") ||
			strings.Contains(lower, "timed out") ||
			strings.Contains(lower, "exit ") {
			interesting = append(interesting, trimmed)
			if len(interesting) >= 60 {
				break
			}
		}
	}
	headLines := firstNonEmptyLines(lines, 30)
	tailLines := lastNonEmptyLines(lines, 30)
	var sb strings.Builder
	sb.WriteString("[shell output summarized for model context; full output is kept in the run log/activity]\n")
	sb.WriteString(fmt.Sprintf("Original output: %d chars, %d lines.\n", len(result), len(lines)))
	if len(interesting) > 0 {
		sb.WriteString("\nLikely important lines:\n")
		for _, line := range interesting {
			sb.WriteString(line + "\n")
		}
	}
	sb.WriteString("\nOutput head:\n")
	sb.WriteString(strings.Join(headLines, "\n"))
	sb.WriteString("\n\nOutput tail:\n")
	sb.WriteString(strings.Join(tailLines, "\n"))
	out := sb.String()
	if len(out) > capChars {
		return truncateToolResult(out, capChars)
	}
	return out
}

func firstNonEmptyLines(lines []string, limit int) []string {
	var out []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func lastNonEmptyLines(lines []string, limit int) []string {
	var out []string
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		out = append([]string{lines[i]}, out...)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func shouldConfirmTool(tool tools.Tool, cfg *settings.Settings, tc llm.ToolCallDef) bool {
	if !tool.Destructive() {
		return false
	}
	if toolSafeListed(cfg.Tools.SafeRules, tc.Function.Name, string(tc.Function.Arguments)) {
		return false
	}
	switch tool.Name() {
	case "shell", "bash":
		return cfg.Tools.ConfirmExec
	default:
		return cfg.Tools.ConfirmWrites
	}
}

func toolSafeListed(rules []settings.ToolSafeRule, toolName, input string) bool {
	hash := safeToolInputHash(input)
	for _, rule := range rules {
		if rule.Tool == toolName && rule.InputHash == hash {
			return true
		}
	}
	return false
}

func safeToolInputHash(input string) string {
	input = normaliseToolInput(input)
	if strings.TrimSpace(input) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func normaliseToolInput(input string) string {
	var value any
	if err := json.Unmarshal([]byte(input), &value); err != nil {
		return strings.TrimSpace(input)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(input)
	}
	return string(data)
}

func stopReasonForBudgetBlock(toolName, message string) string {
	switch {
	case strings.Contains(message, "web research stopped"):
		return "web_research_failed"
	case strings.Contains(message, "web_search budget exhausted"):
		return "search_budget_exhausted"
	case strings.Contains(message, "fetch_url budget exhausted"):
		return "fetch_budget_exhausted"
	case strings.Contains(message, "browser automation budget exhausted"):
		return "browser_budget_exhausted"
	default:
		return "tool_blocked_" + toolName
	}
}

func isBlockingStopReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "search_budget_exhausted",
		"fetch_budget_exhausted",
		"browser_budget_exhausted",
		"web_research_failed",
		"tool_budget_exhausted",
		"tool_denied",
		"tool_disabled":
		return true
	default:
		return false
	}
}

func requiresLivingDocUpdate(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, marker := range []string{
		"update the doc",
		"update doc",
		"update documentation",
		"document",
		"writeup",
		"write up",
		"readme",
		"notes",
		"report",
		".md",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func runHasFileMutation(run TaskRun) bool {
	for _, tool := range run.Tools {
		if tool.Status != "done" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(tool.Name))
		if name == "write_file" || name == "edit_file" {
			return true
		}
	}
	return false
}

type taskBudget struct {
	maxSearches       int
	maxFetches        int
	maxFailedFetches  int
	maxBrowserActions int
	searches          int
	fetches           int
	failedWeb         int
	browserActions    int
}

func newTaskBudget(cfg settings.ToolsConfig, taskText ...string) *taskBudget {
	b := &taskBudget{
		maxSearches:       cfg.MaxSearches,
		maxFetches:        cfg.MaxFetches,
		maxFailedFetches:  cfg.MaxFailedFetches,
		maxBrowserActions: cfg.MaxBrowserActions,
	}
	if b.maxSearches <= 0 {
		b.maxSearches = 8
	}
	if b.maxFetches <= 0 {
		b.maxFetches = 12
	}
	if b.maxFailedFetches <= 0 {
		b.maxFailedFetches = 5
	}
	if b.maxBrowserActions <= 0 {
		b.maxBrowserActions = 35
	}
	if len(taskText) > 0 && isExploitResearchTask(strings.Join(taskText, " ")) {
		b.maxSearches = max(b.maxSearches, 16)
		b.maxFetches = max(b.maxFetches, 24)
		b.maxFailedFetches = max(b.maxFailedFetches, 8)
		b.maxBrowserActions = max(b.maxBrowserActions, 60)
	}
	return b
}

func isExploitResearchTask(text string) bool {
	lower := strings.ToLower(text)
	signals := []string{
		"exploit", "exploits", "cve-", "poc", "proof of concept", "vulnerability", "vuln",
		"rce", "lfi", "sqli", "xss", "ssrf", "auth bypass", "privilege escalation",
		"exploit-db", "packet storm", "rapid7", "metasploit", "nuclei", "wpscan",
		"hackthebox", "hack the box", "htb", "ctf", "freepbx", "searchsploit",
		"get user", "get root", "root flag", "user flag", "privesc", "priv esc",
	}
	for _, signal := range signals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func (b *taskBudget) before(name string) string {
	if isWebTool(name) && b.failedWeb >= b.maxFailedFetches {
		return fmt.Sprintf("web research stopped after %d failed/no-result web attempts. Report uncertainty, cite the best evidence already gathered, and ask for a narrower target if needed.", b.failedWeb)
	}
	switch name {
	case "web_search":
		if b.searches >= b.maxSearches {
			return fmt.Sprintf("web_search budget exhausted (%d searches). Stop searching and summarize uncertainty from the sources already gathered.", b.maxSearches)
		}
		b.searches++
	case "fetch_url":
		if b.fetches >= b.maxFetches {
			return fmt.Sprintf("fetch_url budget exhausted (%d fetches). Stop fetching and summarize uncertainty from the sources already gathered.", b.maxFetches)
		}
		b.fetches++
	default:
		if strings.HasPrefix(name, "browser_") && name != "browser_close" {
			if b.browserActions >= b.maxBrowserActions {
				return fmt.Sprintf("browser automation budget exhausted (%d actions). Stop browsing and summarize what is known.", b.maxBrowserActions)
			}
			b.browserActions++
		}
	}
	return ""
}

func (b *taskBudget) after(name, result string, runErr error) {
	if !isWebTool(name) {
		return
	}
	lower := strings.ToLower(strings.TrimSpace(result))
	if runErr != nil ||
		strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "no results") ||
		strings.Contains(lower, "http 404") ||
		strings.Contains(lower, "http 403") ||
		strings.Contains(lower, "http 429") {
		b.failedWeb++
	}
}

func isWebTool(name string) bool {
	return name == "web_search" || name == "fetch_url"
}

func stateForTool(name string) string {
	switch {
	case name == "web_search" || name == "fetch_url" || strings.HasPrefix(name, "browser_"):
		return "researching"
	case name == "read_file" || name == "read_many" || name == "read_pdf" || name == "glob" || name == "grep" || name == "session_search":
		return "reading"
	case name == "write_file" || name == "edit_file":
		return "editing"
	case name == "shell" || name == "bash":
		return "testing"
	case strings.HasPrefix(name, "todo_"):
		return "planning"
	default:
		return "using_tools"
	}
}

func isMalformedToolArgsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "bad params") ||
		strings.Contains(msg, "unexpected end of json input") ||
		strings.Contains(msg, "invalid character") ||
		strings.Contains(msg, "cannot unmarshal")
}

func toolEnabled(enabled map[string]bool, name string) bool {
	if enabled == nil {
		return true
	}
	if name == "bash" {
		if shellEnabled, ok := enabled["shell"]; ok {
			return shellEnabled
		}
	}
	if on, ok := enabled[name]; ok {
		return on
	}
	return true
}

// AgentMode is the first-pass auto-agent routing result.
type AgentMode struct {
	Name          string
	Description   string
	Instructions  string
	ContextBudget int
}

func classifyAgentMode(text string) AgentMode {
	lower := strings.ToLower(text)
	switch {
	case hasAny(lower, "review", "audit", "risks", "regression", "security", "code quality"):
		return AgentMode{
			Name:         "Reviewer",
			Description:  "Find bugs, risks, regressions, and missing tests.",
			Instructions: "Work like a strict code reviewer. Lead with concrete findings, inspect relevant files before changing behavior, and prefer small verified fixes when asked to apply changes.",
		}
	case hasAny(lower, "research", "look up", "search", "latest", "news", "web", "source", "github"):
		return AgentMode{
			Name:         "Researcher",
			Description:  "Search, fetch, compare sources, and synthesize.",
			Instructions: "Use web_search and fetch_url when current or external information matters. Cite source URLs in tool-backed summaries and avoid unnecessary file writes.",
		}
	case hasAny(lower, "bug", "fix", "error", "failing", "broken", "doesn't work", "does not work", "issue", "crash"):
		return AgentMode{
			Name:         "Fixer",
			Description:  "Diagnose failures and patch them.",
			Instructions: "Reproduce or inspect the failure first, identify the smallest likely cause, patch narrowly, and run focused verification.",
		}
	case hasAny(lower, "build", "add", "implement", "create", "make", "wire", "continue", "carry on", "write", "edit", "patch", "change", "update"):
		return AgentMode{
			Name:         "Builder",
			Description:  "Implement features and verify them.",
			Instructions: "Make the requested change end to end. Follow existing project patterns, keep edits scoped, and run relevant tests/builds.",
		}
	case hasAny(lower, "plan", "design", "architecture", "approach", "what should", "roadmap"):
		return AgentMode{
			Name:         "Planner",
			Description:  "Plan architecture and next steps.",
			Instructions: "Prefer read-only analysis, outline tradeoffs, and only edit files when the user asks to implement.",
		}
	default:
		return AgentMode{
			Name:         "Auto",
			Description:  "General coding agent.",
			Instructions: "Choose the right working style for the task, inspect before editing, and verify changes when possible.",
		}
	}
}

func manualAgentMode() AgentMode {
	return AgentMode{
		Name:         "Manual",
		Description:  "Auto agents are disabled.",
		Instructions: "Use the base TheMauler behavior without task-specific mode routing.",
	}
}

func shouldUseCodingParams(text string, mode AgentMode) bool {
	switch strings.ToLower(strings.TrimSpace(mode.Name)) {
	case "builder", "fixer", "reviewer":
		return true
	}
	lower := strings.ToLower(text)
	return hasAny(lower,
		"script", "code", "function", "class", "module", "component",
		"powershell", "bash", "shell", "python", "typescript", "javascript",
		"html", "css", "json", "yaml", "sql", "go ", "golang",
		"write_file", "edit_file", "full file", "complete file",
		".ps1", ".sh", ".py", ".ts", ".tsx", ".js", ".jsx", ".go",
	)
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func activeProfile(cfg *settings.Settings, pf *settings.ProfilesFile) settings.Profile {
	if p, ok := pf.Profiles[cfg.ActiveProfile]; ok {
		return applyProvider(p, pf)
	}
	for _, p := range pf.Profiles {
		return applyProvider(p, pf)
	}
	return settings.Profile{}
}

func ensureActiveProfile(cfg *settings.Settings, pf *settings.ProfilesFile) {
	if profile, ok := pf.Profiles[cfg.ActiveProfile]; ok && strings.TrimSpace(profile.ModelID) != "" {
		return
	}
	for name, profile := range pf.Profiles {
		if strings.TrimSpace(profile.ModelID) == "" {
			continue
		}
		cfg.ActiveProfile = name
		_ = settings.Save(cfg)
		return
	}
}

func applyProvider(profile settings.Profile, pf *settings.ProfilesFile) settings.Profile {
	if provider, ok := pf.Providers[profile.Provider]; ok {
		profile.Backend = provider.Backend
		profile.BaseURL = provider.BaseURL
		profile.APIKeyEnv = provider.APIKeyEnv
	}
	return profile
}

func buildClient(p settings.Profile) (llm.Client, error) {
	switch p.Backend {
	case "llamacpp":
		return backends.NewLlamacpp(p), nil
	case "lmstudio":
		return backends.NewLMStudio(p), nil
	case "openai-compatible", "openai", "sglang", "vllm":
		return backends.NewOpenAICompatible(p), nil
	default:
		return backends.NewLMStudio(p), nil
	}
}

func (a *App) ensureModelLoaded(ctx context.Context, client llm.Client, profile settings.Profile, onRetry ...func(int, error)) error {
	loader, ok := client.(interface{ LoadModel(context.Context) error })
	if !ok {
		return nil
	}
	key := modelLoadKey(profile)

	// loadMu serialises the check-then-load so two concurrent goroutines cannot
	// both observe loadedModelKey == "" and both trigger a load simultaneously.
	a.loadMu.Lock()
	defer a.loadMu.Unlock()

	type contextQuerier interface {
		ActualContextLength(context.Context) int
	}

	a.mu.Lock()
	alreadyLoaded := key != "" && key == a.loadedModelKey
	sameLoadedModel := key != "" && modelLoadKeySameRuntime(profile, a.loadedModelKey)
	a.mu.Unlock()

	if (alreadyLoaded || sameLoadedModel) && profile.CtxTokens > 0 {
		if cq, ok2 := client.(contextQuerier); ok2 {
			qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			actual := cq.ActualContextLength(qctx)
			cancel()
			if actual > 0 && actual >= profile.CtxTokens {
				alreadyLoaded = true
				a.mu.Lock()
				a.loadedModelKey = key
				a.mu.Unlock()
			} else if actual > 0 && actual < profile.CtxTokens {
				alreadyLoaded = false
			}
		}
	}

	if !alreadyLoaded {
		if err := a.loadModelWithRetry(ctx, loader, key, onRetry...); err != nil {
			return err
		}
		a.mu.Lock()
		a.loadedModelKey = key
		a.mu.Unlock()
	}

	// Sync the history budget to the actual context length reported by the backend.
	// This catches cases where LM Studio loaded the model with a different context than
	// the profile specifies (e.g. manual eject+reload, GPU memory limits, or the model
	// was already running when TheMauler started).
	if cq, ok2 := client.(contextQuerier); ok2 {
		qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		actual := cq.ActualContextLength(qctx)
		cancel()
		if actual > 0 {
			effective := profile.CtxTokens
			if effective <= 0 || actual < effective {
				effective = actual
			}
			if effective > 0 {
				a.mu.Lock()
				changed := a.history.Budget() != effective
				a.history.SetBudget(effective)
				a.mu.Unlock()
				if changed && a.ctx != nil {
					wailsruntime.EventsEmit(a.ctx, "mauler:budget_updated", effective)
				}
			}
		}
	}

	return nil
}

var (
	modelLoadAttempts       = 3
	modelLoadAttemptTimeout = 2 * time.Minute
	modelLoadRetryDelays    = []time.Duration{2 * time.Second, 5 * time.Second}
)

func (a *App) loadModelWithRetry(ctx context.Context, loader interface{ LoadModel(context.Context) error }, key string, onRetry ...func(int, error)) error {
	var lastErr error

	for attempt := 1; attempt <= modelLoadAttempts; attempt++ {
		loadCtx, cancel := context.WithTimeout(ctx, modelLoadAttemptTimeout)
		err := loader.LoadModel(loadCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		a.clearLoadedModelKey(key)
		if ctxErr := ctx.Err(); ctxErr != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctxErr, context.Canceled) {
				return err
			}
			return fmt.Errorf("model load canceled after attempt %d: %w", attempt, ctxErr)
		}
		if attempt == modelLoadAttempts {
			break
		}
		for _, cb := range onRetry {
			if cb != nil {
				cb(attempt, err)
			}
		}
		delay := time.Duration(0)
		if attempt-1 < len(modelLoadRetryDelays) {
			delay = modelLoadRetryDelays[attempt-1]
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return fmt.Errorf("model load canceled before retry %d: %w", attempt+1, ctx.Err())
		}
	}

	return fmt.Errorf("model load failed after %d attempts: %w", modelLoadAttempts, lastErr)
}

func (a *App) clearLoadedModelKey(key string) {
	a.mu.Lock()
	if a.loadedModelKey == key {
		a.loadedModelKey = ""
	}
	a.mu.Unlock()
}

func modelLoadKey(profile settings.Profile) string {
	return strings.Join([]string{
		profile.Backend,
		strings.TrimRight(profile.BaseURL, "/"),
		profile.ModelID,
		fmt.Sprintf("%d", profile.CtxTokens),
		profile.APIKeyEnv,
	}, "\x00")
}

func modelLoadKeySameRuntime(profile settings.Profile, loadedKey string) bool {
	parts := strings.Split(loadedKey, "\x00")
	if len(parts) != 5 {
		return false
	}
	return parts[0] == profile.Backend &&
		parts[1] == strings.TrimRight(profile.BaseURL, "/") &&
		parts[2] == profile.ModelID &&
		parts[4] == profile.APIKeyEnv
}

func (a *App) recordBackendRuntimeMismatch(ctx context.Context, client llm.Client, profile settings.Profile, run *TaskRun) {
	if profile.CtxTokens <= 0 || run == nil {
		return
	}
	cq, ok := client.(interface {
		ActualContextLength(context.Context) int
	})
	if !ok {
		return
	}
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	actual := cq.ActualContextLength(qctx)
	cancel()
	if actual <= 0 || actual == profile.CtxTokens {
		return
	}
	severity := "info"
	if actual < profile.CtxTokens {
		severity = "warn"
	}
	run.addEvent(
		"backend_runtime_changed",
		"Backend runtime differs from active profile",
		fmt.Sprintf("severity=%s expected_ctx=%d actual_ctx=%d model=%s backend=%s base_url=%s", severity, profile.CtxTokens, actual, profile.ModelID, profile.Backend, profile.BaseURL),
	)
}

func buildChatRequest(profile settings.Profile, msgs []llm.Message, toolDefs []llm.ToolDef, toolChoice string, forceNoThink bool, coding bool) llm.Request {
	params := profile.ActiveParams(coding)
	if coding && !profile.Thinking && profile.ThinkCoding.MaxTokens > 0 {
		params = profile.ThinkCoding
	}
	enableThinking := profile.Thinking
	preserveThinking := profile.PreserveThink
	if forceNoThink {
		enableThinking = false
		preserveThinking = false
	}
	return llm.Request{
		Messages:         msgs,
		Tools:            toolDefs,
		ToolChoice:       toolChoice,
		MaxTokens:        params.MaxTokens,
		Temperature:      params.Temperature,
		TopP:             params.TopP,
		TopK:             params.TopK,
		MinP:             params.MinP,
		PresencePenalty:  params.PresencePenalty,
		Seed:             params.Seed,
		EnableThinking:   enableThinking,
		PreserveThinking: preserveThinking,
		SpecType:         profile.SpecType,
		SpecDraftNMax:    profile.SpecDraftNMax,
	}
}

func configureWorkingDir(cfg *settings.Settings) {
	if cfg.Context.WorkspaceDir != "" {
		if info, err := os.Stat(cfg.Context.WorkspaceDir); err == nil && info.IsDir() {
			_ = os.Chdir(cfg.Context.WorkspaceDir)
			return
		}
	}
	if root := discoverWorkspaceRoot(); root != "" {
		_ = os.Chdir(root)
	}
}

func discoverWorkspaceRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		for _, marker := range []string{"wails.json", "AGENTS.md", "go.mod"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func removeRemoteProfiles(pf *settings.ProfilesFile) {
	for name, profile := range pf.Profiles {
		if profile.Backend == "anthropic" || name == "claude-sonnet" {
			delete(pf.Profiles, name)
		}
	}
	for name, provider := range pf.Providers {
		if provider.Backend == "anthropic" {
			delete(pf.Providers, name)
		}
	}
}

func sessionsDir() (string, error) {
	dir, err := settings.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions"), nil
}

func mustSessionsDir() string {
	dir, _ := sessionsDir()
	return dir
}

var sessionNameRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ---------------------------------------------------------------------------
// Terminal / shell session
// ---------------------------------------------------------------------------

// shellSession holds the state of one live interactive shell process.
type shellSession struct {
	id     string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	output chan terminalOutput
	runMu  sync.Mutex
}

type terminalOutput struct {
	data   string
	stream string
}

// Terminal output is rendered as plain text in the frontend, not by a full VT
// emulator. Strip title updates, colour/control escapes, and stray C0 controls
// before events reach React.
var (
	oscEscape  = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b[()][A-Za-z0-9]|\x1b[=>]`)
)

func maulerBashInteractiveArgs() []string {
	// Source the user's bashrc for PATH/aliases, then override the prompt bits
	// that commonly emit OSC title sequences and Unicode-heavy Kali prompts, and
	// force a non-interactive, no-pager environment. Pagers (git/systemctl/less/man)
	// and credential prompts otherwise block the shared session until it times out,
	// which is the main reason the agent finds the shared terminal "problematic".
	rc := "exec bash --rcfile <(printf '%s\\n' " +
		"'test -f ~/.bashrc && . ~/.bashrc' " +
		"'PROMPT_COMMAND=' " +
		"'PROMPT_DIRTRIM=3' " +
		"'export PAGER=cat GIT_PAGER=cat SYSTEMD_PAGER=cat MANPAGER=cat LESS=FRX' " +
		"'export DEBIAN_FRONTEND=noninteractive GIT_TERMINAL_PROMPT=0 PIP_DISABLE_PIP_VERSION_CHECK=1' " +
		"'export PYTHONUNBUFFERED=1' " +
		"\"PS1='\\\\u@\\\\h:\\\\w\\\\$ '\") -i"
	return []string{"-lc", rc}
}

// OpenShell starts a new interactive shell and returns its session ID.
// Any previously open shell is closed first.  Output is streamed to the
// frontend via "mauler:shell_output" events; exit is signalled by
// "mauler:shell_exit".
func (a *App) OpenShell() (string, error) {
	a.shellMu.Lock()
	defer a.shellMu.Unlock()

	// Kill any existing session so we don't leak processes.
	if a.shellSess != nil {
		a.shellSess.cancel()
		a.shellSess = nil
	}

	a.mu.Lock()
	backend := a.cfg.Tools.ShellBackend
	distro := strings.TrimSpace(a.cfg.Tools.ShellDistro)
	user := strings.TrimSpace(a.cfg.Tools.ShellUser)
	cwd, _ := os.Getwd()
	if dir := a.cfg.Context.WorkspaceDir; dir != "" {
		cwd = dir
	}
	a.mu.Unlock()

	var shellCmd string
	var shellArgs []string
	switch backend {
	case "powershell":
		shellCmd = "powershell.exe"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", "-"}
	case "cmd":
		shellCmd = "cmd.exe"
	case "bash":
		shellCmd = "bash"
		shellArgs = maulerBashInteractiveArgs()
	case "wsl":
		shellCmd = "wsl.exe"
		if distro != "" {
			shellArgs = append(shellArgs, "-d", distro)
		}
		if user != "" {
			shellArgs = append(shellArgs, "--user", user)
		}
		if wslDir := tools.WindowsPathToWSL(cwd); wslDir != "" {
			shellArgs = append(shellArgs, "--cd", wslDir)
		}
		shellArgs = append(shellArgs, "--", "bash")
		shellArgs = append(shellArgs, maulerBashInteractiveArgs()...)
	default: // auto
		if runtime.GOOS == "windows" {
			shellCmd = "powershell.exe"
			shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", "-"}
		} else {
			shellCmd = "bash"
			shellArgs = maulerBashInteractiveArgs()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, shellCmd, shellArgs...)
	if backend != "wsl" {
		cmd.Dir = cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("shell stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("shell stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return "", fmt.Errorf("shell stderr pipe: %w", err)
	}

	// Hide the console window before starting (Windows: CREATE_NO_WINDOW).
	hideShellWindow(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		return "", fmt.Errorf("shell start: %w", err)
	}

	id := fmt.Sprintf("shell-%d", time.Now().UnixMilli())
	sess := &shellSession{id: id, cmd: cmd, stdin: stdin, cancel: cancel, output: make(chan terminalOutput, 4096)}
	a.shellSess = sess

	go a.pipeShellOutput(id, stdout, "stdout")
	go a.pipeShellOutput(id, stderr, "stderr")
	go func() {
		_ = cmd.Wait()
		cancel()
		a.shellMu.Lock()
		if a.shellSess != nil && a.shellSess.id == id {
			a.shellSess = nil
		}
		a.shellMu.Unlock()
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "mauler:shell_exit", map[string]string{"id": id})
		}
	}()

	return id, nil
}

// pipeShellOutput reads lines from r and emits them as mauler:shell_output events.
func (a *App) pipeShellOutput(id string, r io.Reader, stream string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := sanitizeTerminalLine(decodeTerminalOutput(scanner.Bytes()))
		a.shellMu.Lock()
		sess := a.shellSess
		if sess != nil && sess.id == id {
			select {
			case sess.output <- terminalOutput{data: line, stream: stream}:
			default:
			}
		}
		a.shellMu.Unlock()
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "mauler:shell_output", map[string]string{
				"id":     id,
				"data":   line,
				"stream": stream,
			})
		}
	}
}

func sanitizeTerminalLine(s string) string {
	s = oscEscape.ReplaceAllString(s, "")
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimRight(s, "\r")
}

func decodeTerminalOutput(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	zeroOdd := 0
	pairs := len(data) / 2
	if pairs >= 4 {
		for i := 0; i+1 < len(data); i += 2 {
			if data[i+1] == 0 {
				zeroOdd++
			}
		}
		if zeroOdd*100/pairs >= 45 {
			u16 := make([]uint16, 0, pairs)
			for i := 0; i+1 < len(data); i += 2 {
				u16 = append(u16, uint16(data[i])|uint16(data[i+1])<<8)
			}
			return string(utf16.Decode(u16))
		}
	}
	return strings.ToValidUTF8(string(data), "?")
}

// ShellInput writes a line of text to the active shell's stdin.
func (a *App) ShellInput(id, text string) error {
	a.shellMu.Lock()
	sess := a.shellSess
	a.shellMu.Unlock()
	if sess == nil || sess.id != id {
		return fmt.Errorf("no active shell session %q", id)
	}
	_, err := fmt.Fprintln(sess.stdin, text)
	return err
}

func (a *App) runSharedTerminalShell(ctx context.Context, toolName string, raw json.RawMessage, defaultTimeout int) (string, error) {
	var p struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("%s: bad params: %w", toolName, err)
	}
	command, err := tools.PrepareShellCommand(p.Command)
	if err != nil {
		return "", fmt.Errorf("%s: %w", toolName, err)
	}
	// A persistent shell hangs forever on a sudo password prompt, poisoning every
	// later command in the session. Make sudo non-interactive so it fails fast instead.
	command = tools.ForceNonInteractiveSudo(command)
	timeoutSecs := defaultTimeout
	if timeoutSecs <= 0 {
		timeoutSecs = 120
	}
	if p.Timeout > 0 && p.Timeout <= 300 {
		timeoutSecs = p.Timeout
	}
	a.mu.Lock()
	backend := a.cfg.Tools.ShellBackend
	a.mu.Unlock()
	if !sharedTerminalSupportsBackend(backend) {
		return "", errSharedTerminalUnsupported
	}
	sess, err := a.ensureShellSession()
	if err != nil {
		return "", err
	}
	sess.runMu.Lock()
	defer sess.runMu.Unlock()
	drainTerminalOutput(sess.output)

	runID := sharedTerminalRunID()
	startMarker, donePrefix, wrapped := sharedTerminalWrapper(command, runID)
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "mauler:terminal_command_start", map[string]string{
			"id": runID, "session": sess.id, "command": command, "timeout": strconv.Itoa(timeoutSecs),
		})
	}
	if _, err := fmt.Fprintln(sess.stdin, wrapped); err != nil {
		return "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()
	started := false
	var out []terminalOutput
	startedAt := time.Now()
	for {
		select {
		case line := <-sess.output:
			trimmed := strings.TrimSpace(line.data)
			if isSharedTerminalWrapperEcho(line.data) {
				if strings.Contains(line.data, startMarker) {
					started = true
				}
				continue
			}
			if strings.Contains(trimmed, startMarker) {
				started = true
				continue
			}
			if statusText, preDone, ok := splitSharedTerminalDone(line.data, donePrefix); ok {
				if started && strings.TrimSpace(preDone) != "" {
					out = append(out, terminalOutput{data: strings.TrimSpace(preDone), stream: line.stream})
				}
				codeText := strings.TrimSpace(statusText)
				exitCode, _ := strconv.Atoi(codeText)
				result := formatSharedTerminalResult(out, backend, exitCode, time.Since(startedAt).Round(time.Millisecond))
				if a.ctx != nil {
					wailsruntime.EventsEmit(a.ctx, "mauler:terminal_command_done", map[string]string{
						"id": runID, "session": sess.id, "exit_code": strconv.Itoa(exitCode),
					})
				}
				if exitCode != 0 {
					return result, fmt.Errorf("exit code %d", exitCode)
				}
				return result, nil
			}
			if started {
				out = append(out, line)
				if len(out) > 2000 {
					out = out[len(out)-2000:]
				}
			}
		case <-runCtx.Done():
			a.shellMu.Lock()
			if a.shellSess != nil && a.shellSess.id == sess.id {
				a.shellSess.cancel()
				a.shellSess = nil
			}
			a.shellMu.Unlock()
			result := formatSharedTerminalResult(out, backend, -1, time.Since(startedAt).Round(time.Millisecond))
			if runCtx.Err() == context.DeadlineExceeded {
				return result + fmt.Sprintf("\n[shared_terminal timed out after %ds]", timeoutSecs), fmt.Errorf("shared terminal: timed out after %ds", timeoutSecs)
			}
			return result + "\n[shared_terminal cancelled]", fmt.Errorf("shared terminal: cancelled")
		}
	}
}

var errSharedTerminalUnsupported = errors.New("shared terminal is only supported for bash/wsl shell backends")

func sharedTerminalSupportsBackend(backend string) bool {
	backend = strings.TrimSpace(strings.ToLower(backend))
	return backend == "wsl" || backend == "bash" || backend == ""
}

func (a *App) ensureShellSession() (*shellSession, error) {
	a.shellMu.Lock()
	sess := a.shellSess
	a.shellMu.Unlock()
	if sess != nil {
		return sess, nil
	}
	if _, err := a.OpenShell(); err != nil {
		return nil, err
	}
	a.shellMu.Lock()
	defer a.shellMu.Unlock()
	if a.shellSess == nil {
		return nil, fmt.Errorf("shared terminal did not start")
	}
	return a.shellSess, nil
}

func drainTerminalOutput(ch <-chan terminalOutput) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func sharedTerminalRunID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func sharedTerminalWrapper(command, runID string) (string, string, string) {
	start := "__MAULER_START_" + runID + "__"
	donePrefix := "__MAULER_DONE_" + runID + ":"
	wrapped := fmt.Sprintf("set -o pipefail 2>/dev/null || true; printf '%%s\\n' %s; { %s; }; status=$?; printf '%%s%%s\\n' %s \"$status\"",
		terminalShellQuote(start), command, terminalShellQuote(donePrefix))
	return start, donePrefix, wrapped
}

func terminalShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func isSharedTerminalWrapperEcho(line string) bool {
	return strings.Contains(line, "__MAULER_START_") && strings.Contains(line, "__MAULER_DONE_") && strings.Contains(line, "printf")
}

func splitSharedTerminalDone(line, donePrefix string) (statusText, preDone string, ok bool) {
	idx := strings.Index(line, donePrefix)
	if idx < 0 {
		return "", "", false
	}
	rest := strings.TrimSpace(line[idx+len(donePrefix):])
	fields := strings.Fields(rest)
	if len(fields) > 0 {
		rest = fields[0]
	}
	return rest, line[:idx], true
}

func formatSharedTerminalResult(lines []terminalOutput, backend string, exitCode int, elapsed time.Duration) string {
	var sb strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line.data) == "" {
			continue
		}
		if isSharedTerminalWrapperEcho(line.data) || strings.Contains(line.data, "__MAULER_START_") || strings.Contains(line.data, "__MAULER_DONE_") {
			continue
		}
		if line.stream == "stderr" {
			sb.WriteString("[stderr] ")
		}
		sb.WriteString(line.data)
		sb.WriteString("\n")
	}
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}
	if exitCode >= 0 {
		sb.WriteString(fmt.Sprintf("[shared_terminal/%s exit %d, %s]", backend, exitCode, elapsed))
	} else {
		sb.WriteString(fmt.Sprintf("[shared_terminal/%s stopped, %s]", backend, elapsed))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ShellClose terminates the shell session identified by id.
func (a *App) ShellClose(id string) error {
	a.shellMu.Lock()
	defer a.shellMu.Unlock()
	if a.shellSess == nil || a.shellSess.id != id {
		return nil
	}
	a.shellSess.cancel()
	a.shellSess = nil
	return nil
}

func cleanSessionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = sessionNameRE.ReplaceAllString(name, "-")
	name = strings.Trim(name, ".-_")
	if name == "" {
		return "", fmt.Errorf("session name is required")
	}
	return name, nil
}

func toSessionChatMessages(msgs []llm.Message) []SessionChatMessage {
	out := []SessionChatMessage{}
	for _, msg := range msgs {
		role := msg.Role
		if role == llm.RoleSystem && len(out) == 0 {
			continue
		}
		if role == llm.RoleTool {
			role = "tool_result"
		}
		out = append(out, SessionChatMessage{
			Role:    role,
			Content: messageText(msg),
			Images:  messageImages(msg),
		})
	}
	return out
}

func messageText(msg llm.Message) string {
	switch content := msg.Content.(type) {
	case string:
		return content
	case []llm.ContentBlock:
		var sb strings.Builder
		for _, block := range content {
			if block.Type == "text" {
				sb.WriteString(block.Text)
			}
		}
		return sb.String()
	case []interface{}:
		var sb strings.Builder
		for _, item := range content {
			if m, ok := item.(map[string]interface{}); ok {
				if m["type"] == "text" {
					if text, ok := m["text"].(string); ok {
						sb.WriteString(text)
					}
				}
			}
		}
		return sb.String()
	default:
		data, _ := json.Marshal(content)
		return string(data)
	}
}

func messageImages(msg llm.Message) []string {
	images := []string{}
	switch content := msg.Content.(type) {
	case []llm.ContentBlock:
		for _, block := range content {
			if block.Type == "image_url" && block.ImageURL != nil && block.ImageURL.URL != "" {
				images = append(images, block.ImageURL.URL)
			}
		}
	case []interface{}:
		for _, item := range content {
			m, ok := item.(map[string]interface{})
			if !ok || m["type"] != "image_url" {
				continue
			}
			imageURL, ok := m["image_url"].(map[string]interface{})
			if !ok {
				continue
			}
			if url, ok := imageURL["url"].(string); ok && url != "" {
				images = append(images, url)
			}
		}
	}
	return images
}

func toSessionStoreMessages(msgs []llm.Message) []sessionstore.Message {
	out := make([]sessionstore.Message, 0, len(msgs))
	for _, msg := range msgs {
		text := strings.TrimSpace(messageText(msg))
		if len(messageImages(msg)) > 0 {
			if text != "" {
				text += "\n"
			}
			text += "[image attachment]"
		}
		out = append(out, sessionstore.Message{
			Role:      msg.Role,
			Content:   text,
			ToolName:  msg.Name,
			ToolCalls: sessionstore.MarshalToolCalls(msg.ToolCalls),
		})
	}
	return out
}

func buildSystemPrompt(cfg settings.Settings, mode AgentMode, memories []MemoryEntry, skills []Skill) string {
	var sb strings.Builder
	sb.WriteString("You are TheMauler, an expert AI coding assistant. ")
	sb.WriteString("Current date: " + time.Now().Format("2006-01-02") + ". ")
	if mode.Name != "" {
		sb.WriteString("Auto agent mode: " + mode.Name + " - " + mode.Description + " ")
		sb.WriteString(mode.Instructions + " ")
	}
	// Core behaviour rules — stated before tool guidance so they have highest priority.
	sb.WriteString("IMPORTANT RULES: " +
		"(1) Only use tools when they are genuinely required to answer — if you already know the answer, reply directly without any tool calls. " +
		"(2) When your answer is complete, STOP. Do NOT offer to run additional tool calls, do NOT ask if the user wants to search for more, do NOT suggest follow-up tool use. " +
		"(3) Never mention the availability of tools in a conversational reply. " +
		"(4) If you say you will find, inspect, read, search, fetch, write, create, update, or run something, your very next action must be the appropriate tool call — not more narration. ")
	sb.WriteString(formatEnabledToolSummary(cfg.Tools))
	sb.WriteString("For multi-step implementation, debugging, research, or review tasks, create and maintain a visible plan with todo_create/todo_update/todo_done/todo_blocked. Keep it concise and update it as phases change. ")
	sb.WriteString("If the user asks to update a README, writeup, notes file, report, or documentation as work progresses, treat that file as a living artifact: write a concise update after each major verified milestone instead of leaving all documentation until the end. ")
	sb.WriteString("Use session_search when the user asks about prior work, past decisions, remembered fixes, or anything likely discussed in an earlier chat. ")
	sb.WriteString("Use skills_list at the start of a complex task to see if a relevant procedural skill exists, then skill_view to read its full instructions. ")
	sb.WriteString("Prefer glob/grep/file_outline/read_chunks/read_file/read_many/read_pdf for file discovery and inspection instead of shell. Use file_outline before reading large files, then read_chunks or read_file line ranges for only the needed sections. Use read_pdf for local PDF documents the user wants analysed. ")
	if workspace := buildWorkspaceContextPrompt(); workspace != "" {
		sb.WriteString(workspace)
	}
	sb.WriteString("The shell tool is platform-aware: Windows uses PowerShell by default, Linux and WSL use bash by default. Use syntax and paths appropriate to the active shell; on Windows PowerShell do not use bash-only constructs like /dev/null or complex bash pipelines. ")
	if strings.EqualFold(cfg.Tools.ShellBackend, "wsl") {
		if distro := strings.TrimSpace(cfg.Tools.ShellDistro); distro != "" {
			sb.WriteString("The active shell backend is WSL distro " + distro + "; run Linux/Kali commands directly with bash syntax and Linux paths. Do not prefix shell commands with wsl or wsl.exe because the shell tool is already inside that distro. ")
		} else {
			sb.WriteString("The active shell backend is the default WSL distro; run Linux commands directly with bash syntax and Linux paths. Do not prefix shell commands with wsl or wsl.exe because the shell tool is already inside WSL. ")
		}
		sb.WriteString("For HTB/CTF enumeration, use realistic shell timeouts: 120-300 seconds for nmap, gobuster, ffuf, hydra, and similar scans. Do not pipe long-running scans through head because it can terminate the scan early and hide the real exit status; write scan output to a file with -oA/-oN/-oG or tee, then tail/grep the saved file afterward. If a scan times out, retry narrower or with a larger timeout and continue from partial output instead of stopping. Sudo is allowed when the user supplied the WSL/Kali sudo password, but it must be non-interactive: pass the password through stdin with sudo -S and a bounded timeout. For privileged one-line writes such as /etc/hosts, prefer printf piped into sudo -S tee -a instead of nested sudo bash -c quoting. ")
	}
	if len(cfg.Tools.ProtectedPaths) > 0 {
		sb.WriteString("Never edit, delete, move, overwrite, chmod/chown, or otherwise mutate these protected paths: " + strings.Join(cfg.Tools.ProtectedPaths, "; ") + ". ")
	}
	sb.WriteString(fmt.Sprintf("Web research is budgeted per task: at most %d searches, %d fetches, and %d failed/no-result web attempts. ", cfg.Tools.MaxSearches, cfg.Tools.MaxFetches, cfg.Tools.MaxFailedFetches))
	sb.WriteString("For exploit, CVE, PoC, CTF/HTB, or service-version research, the runtime may grant a larger web budget; fan out across vendor advisories, NVD/CVE records, GitHub PoCs, Exploit-DB/Rapid7/Packet Storm, and relevant issue/forum reports before concluding none exists. ")
	sb.WriteString("Rank sources as official docs first, then GitHub/repo docs, package docs, blogs/community posts, and random mirrors last. ")
	sb.WriteString("If repeated searches/fetches fail or return only mirrors, stop searching and state the uncertainty instead of spiraling. ")
	sb.WriteString("When web_search/fetch_url are insufficient for JavaScript-heavy pages or forms, use browser_open/browser_snapshot/browser_click/browser_type/browser_extract/browser_screenshot within the browser action budget. ")
	sb.WriteString("When using web_search for current events, include today's year/date in the query and fetch promising high-ranked sources with fetch_url. ")
	sb.WriteString("If a tool is blocked, disabled, denied, exhausted by budget, or returns repeated errors, stop the loop and clearly report what stopped you, what you already tried, and the exact next permission or input needed. ")
	if cfg.Agents.RequirePlan {
		sb.WriteString("For substantial multi-step tasks, state a concise plan before acting and update it when the route changes. ")
	}
	sb.WriteString("Always think step by step. Prefer targeted edits over rewrites.")
	if len(skills) > 0 {
		sb.WriteString("\n\nRelevant procedural skills:\n")
		for _, s := range skills {
			sb.WriteString("\n### Skill: " + s.Name + "\n")
			if s.Description != "" {
				sb.WriteString("**When to use:** " + s.Description + "\n")
			}
			if strings.TrimSpace(s.SourcePath) != "" {
				sb.WriteString("External source: " + s.SourcePath + "\n")
				sb.WriteString("Load lazily with skill_view. Pass a focused query when only a section is needed.\n")
			} else if s.Body != "" {
				body := strings.TrimSpace(s.Body)
				const maxInlineSkillChars = 6000
				if len(body) > maxInlineSkillChars {
					body = body[:maxInlineSkillChars] + "\n\n[Skill truncated in prompt. Use skill_view for the full instructions.]"
				}
				sb.WriteString(body + "\n")
			}
		}
	}
	if len(memories) > 0 {
		sb.WriteString("\n\nRelevant project memory:\n")
		for _, memory := range memories {
			sb.WriteString("- ")
			if memory.Title != "" {
				sb.WriteString(memory.Title + ": ")
			}
			sb.WriteString(strings.TrimSpace(memory.Content))
			meta := []string{}
			if memory.Kind != "" && memory.Kind != "note" {
				meta = append(meta, "kind="+memory.Kind)
			}
			if memory.Importance > 0 {
				meta = append(meta, fmt.Sprintf("importance=%d", memory.Importance))
			}
			if len(memory.Tags) > 0 {
				meta = append(meta, "tags="+strings.Join(memory.Tags, ","))
			}
			if len(meta) > 0 {
				sb.WriteString(" [" + strings.Join(meta, "; ") + "]")
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString(buildProjectInstructionsPrompt(cfg.Context))
	if userProfile := loadUserProfile(); userProfile != "" {
		sb.WriteString("\n\nUser profile:\n")
		sb.WriteString(userProfile)
	}
	return sb.String()
}

func formatEnabledToolSummary(cfg settings.ToolsConfig) string {
	if !cfg.Enabled {
		return "No tools are enabled for this run; answer directly and say what permission is needed if implementation is required. "
	}
	effective := settings.EffectiveEnabledTools(cfg)
	has := func(name string) bool { return toolEnabled(effective, name) }
	var groups []string
	if has("read_file") || has("read_many") || has("read_chunks") || has("file_outline") || has("read_pdf") {
		groups = append(groups, "read and inspect files")
	}
	if has("write_file") || has("edit_file") {
		groups = append(groups, "write and edit files")
	}
	if has("shell") || has("bash") {
		groups = append(groups, "run shell commands")
	}
	if has("glob") || has("grep") {
		groups = append(groups, "search local files with glob and grep")
	}
	if has("web_search") || has("fetch_url") {
		groups = append(groups, "search and fetch the web")
	}
	if has("browser_open") || has("browser_snapshot") || has("browser_click") || has("browser_type") || has("browser_extract") || has("browser_screenshot") {
		groups = append(groups, "use browser automation")
	}
	if len(groups) == 0 {
		return "No usable tools are enabled for this run; answer directly and say what permission is needed if implementation is required. "
	}
	return fmt.Sprintf("Enabled tools for this run let you %s. If the user asks you to implement, patch, write, edit, or run something and those tools are enabled, use a tool call instead of telling the user to copy/paste code. ", strings.Join(groups, ", "))
}

func buildWorkspaceContextPrompt() string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return ""
	}
	wd = filepath.ToSlash(wd)
	cfg, _ := settings.Load()
	var sb strings.Builder
	sb.WriteString("\n\nCurrent workspace context (authoritative for this run):\n")
	sb.WriteString("- Agent root: " + wd + "\n")
	if entries := workspaceTopLevelEntries(wd, 40); len(entries) > 0 {
		sb.WriteString("- Top-level entries: " + strings.Join(entries, ", ") + "\n")
	}
	if cfg != nil {
		folders := normaliseAppWorkspaceFolders(cfg.Context.OpenFolders, wd)
		var extras []string
		for _, folder := range folders {
			if sameFilesystemPath(folder.Path, wd) {
				continue
			}
			label := folder.Path
			if folder.Role != "" && folder.Role != "folder" {
				label += " (" + folder.Role + ")"
			}
			extras = append(extras, label)
			if len(extras) >= 8 {
				break
			}
		}
		if len(extras) > 0 {
			sb.WriteString("- Additional open folders for browsing/reference: " + strings.Join(extras, "; ") + "\n")
		}
		var lab []string
		if cfg.Context.Lab.Target != "" {
			lab = append(lab, "target="+cfg.Context.Lab.Target)
		}
		if cfg.Context.Lab.VPNInterface != "" {
			lab = append(lab, "vpn/interface="+cfg.Context.Lab.VPNInterface)
		}
		if cfg.Context.Lab.LatestArtifact != "" {
			lab = append(lab, "latest_artifact="+cfg.Context.Lab.LatestArtifact)
		}
		if len(lab) > 0 {
			sb.WriteString("- Lab/run context: " + strings.Join(lab, "; ") + "\n")
		}
	}
	sb.WriteString("All relative file paths in tool calls resolve from this root. ")
	sb.WriteString("Open folders are for browsing/reference only unless the user or tool call uses an absolute path. ")
	sb.WriteString("The user may switch projects between chats; ignore stale project names, memories, or prior file paths that conflict with this root and its entries. ")
	sb.WriteString("Before reading assumed project files, discover what actually exists with glob/grep or use the files shown above. ")
	sb.WriteString("If a file read reports that a path does not exist, adapt to the current workspace instead of retrying paths from another project.\n")
	return sb.String()
}

func workspaceTopLevelEntries(root string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := make([]string, 0, min(len(entries), limit))
	for _, entry := range entries {
		name := entry.Name()
		if shouldSkip(name) {
			continue
		}
		if entry.IsDir() {
			name += "/"
		}
		out = append(out, name)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normaliseAppWorkspaceFolders(folders []settings.WorkspaceFolder, agentRoot string) []settings.WorkspaceFolder {
	seen := map[string]bool{}
	var out []settings.WorkspaceFolder
	add := func(folder settings.WorkspaceFolder) {
		folder.Path = filepath.ToSlash(strings.TrimSpace(folder.Path))
		folder.Name = strings.TrimSpace(folder.Name)
		folder.Role = strings.TrimSpace(folder.Role)
		if folder.Role == "" {
			folder.Role = "folder"
		}
		if folder.Path == "" {
			return
		}
		if !workspaceFolderExists(folder.Path) {
			return
		}
		key := strings.ToLower(filepath.ToSlash(tools.NormalizeHostPath(folder.Path)))
		if seen[key] {
			return
		}
		if folder.Name == "" {
			folder.Name = filepath.Base(folder.Path)
		}
		seen[key] = true
		out = append(out, folder)
	}
	if strings.TrimSpace(agentRoot) != "" {
		root := filepath.ToSlash(agentRoot)
		add(settings.WorkspaceFolder{Path: root, Name: filepath.Base(root), Role: "root"})
	}
	for _, folder := range folders {
		add(folder)
	}
	return out
}

func workspaceFolderExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(tools.NormalizeHostPath(path))
	return err == nil && info.IsDir()
}

func mergeWorkspaceFolders(folders []settings.WorkspaceFolder, folder settings.WorkspaceFolder) []settings.WorkspaceFolder {
	folder.Path = filepath.ToSlash(strings.TrimSpace(folder.Path))
	if folder.Path == "" {
		return folders
	}
	var out []settings.WorkspaceFolder
	replaced := false
	for _, existing := range folders {
		if sameFilesystemPath(existing.Path, folder.Path) {
			if folder.Name == "" {
				folder.Name = existing.Name
			}
			if folder.Role == "" {
				folder.Role = existing.Role
			}
			out = append(out, folder)
			replaced = true
			continue
		}
		out = append(out, existing)
	}
	if !replaced {
		out = append(out, folder)
	}
	return out
}

func latestWorkspaceArtifact(folders []settings.WorkspaceFolder) string {
	var newestPath string
	var newestTime time.Time
	for _, folder := range folders {
		if folder.Path == "" {
			continue
		}
		role := strings.ToLower(folder.Role)
		name := strings.ToLower(filepath.Base(folder.Path))
		if role != "scans" && role != "loot" && name != "scans" && name != "loot" {
			continue
		}
		_ = filepath.WalkDir(tools.NormalizeHostPath(folder.Path), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.ModTime().After(newestTime) {
				newestTime = info.ModTime()
				newestPath = path
			}
			return nil
		})
	}
	return filepath.ToSlash(newestPath)
}

func sameFilesystemPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	cleanA := filepath.Clean(a)
	cleanB := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(cleanA, cleanB)
	}
	return cleanA == cleanB
}

func parseDataURI(uri string) (b64 string, mediaType string, ok bool) {
	if !strings.HasPrefix(uri, "data:") {
		return "", "", false
	}
	rest := uri[5:]
	semi := strings.Index(rest, ";")
	if semi < 0 {
		return "", "", false
	}
	mediaType = rest[:semi]
	rest = rest[semi+1:]
	if !strings.HasPrefix(rest, "base64,") {
		return "", "", false
	}
	return rest[7:], mediaType, true
}

// looksIncomplete returns true when the model appears to have stopped mid-task
// rather than finishing naturally — e.g., it was narrating its next step when
// it hit max_tokens, or it explicitly said it would do more.
func looksIncomplete(text string) bool {
	if text == "" {
		return false
	}
	// A response that looks fully finished should never trigger an auto-continue,
	// even if it happens to contain a phrase from the lists below.
	if looksFinished(text) {
		return false
	}
	lower := strings.ToLower(text)
	// Ends with a colon (about to start something) or ellipsis
	last := text[len(text)-1]
	if last == ':' || strings.HasSuffix(text, "...") {
		return true
	}
	// Phrases that appear near the end of the response (last 300 chars).
	// NOTE: kept narrow on purpose — broad phrases like "here is the", "first,",
	// "starting with" fired on fully-complete answers and caused unwanted continues.
	suffixPhrases := []string{
		// "let me …" action starters
		"let me begin", "let me start", "let me proceed",
		"let me now", "let me create", "let me write", "let me build",
		"let me update", "let me add", "let me find", "let me explore",
		"let me check", "let me inspect", "let me read", "let me search",
		"let me fetch", "let me look", "let me try", "let me run",
		"let me first", "let me next",
		// "now …" continuations
		"now let me", "now i'll", "now i will", "now create", "now write",
		"now build", "now update", "now find", "now explore", "now check",
		"now inspect", "now read", "now search", "now fetch", "now run",
		// "i'll / i will …" intent
		"i'll start", "i will start", "i'll now", "i will now",
		"i'll begin", "i will begin", "i'll proceed", "i will proceed",
		"i'll first", "i will first", "i'll next", "i will next",
		"next i'll", "next i will", "next let me",
		"i'll apply", "i will apply", "applying the", "making the changes",
		"writing the", "creating the", "updating the", "building the",
		// Narrow transition signals only
		"first, i'll", "first, i will", "first, let me", "first i'll", "first let me",
		"to do this,", "to accomplish", "to complete",
	}
	tail := lower[max(0, len(lower)-300):]
	for _, p := range suffixPhrases {
		if strings.Contains(tail, p) {
			return true
		}
	}
	// Short responses (≤ 350 chars) that contain future-intent language anywhere
	// are almost certainly a plan without action — treat as incomplete.
	if len(lower) <= 350 {
		globalPhrases := []string{
			"i'll start by", "i will start by", "i'll begin by", "i will begin by",
			"first i'll", "first i will", "first, i'll", "first, i will",
			"let me first", "let me start by", "let me begin by",
			"my plan", "here's my plan", "here is my plan",
			"i'll proceed", "i will proceed", "then i'll", "then i will",
		}
		for _, p := range globalPhrases {
			if strings.Contains(lower, p) {
				return true
			}
		}
	}
	return false
}

func sleepBeforeAutoContinue(ctx context.Context, attempt int) bool {
	delay := autoContinueDelay(attempt)
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func sleepBeforeInferenceRetry(ctx context.Context) bool {
	timer := time.NewTimer(750 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func isRecoverableInferenceFailure(message string) bool {
	msg := strings.ToLower(message)
	if strings.Contains(msg, "tool \"") && strings.Contains(msg, "disabled") {
		return false
	}
	for _, marker := range []string{
		"inference failed",
		"http 500",
		"http 502",
		"http 503",
		"http 504",
		"error sending request",
		"connectex",
		"connection attempt failed",
		"connection reset",
		"connection refused",
		"actively refused",
		"did not properly respond",
		"failed to respond",
		"no connection could be made",
		"unable to connect",
		"unexpected eof",
		"server closed",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func autoContinueDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	return 500 * time.Millisecond
}

// looksAboutToAct returns true when the model's last text strongly suggests it
// was about to call a tool (write, create, run, etc.) before being cut off.
// Used to decide whether to send a directive prompt vs. a soft continue.
func looksAboutToAct(text string) bool {
	lower := strings.ToLower(text)
	tail := lower
	if len(tail) > 300 {
		tail = tail[len(tail)-300:]
	}
	actPhrases := []string{
		"let me write", "i'll write", "i will write", "now write",
		"let me create", "i'll create", "now create",
		"let me run", "i'll run", "now run",
		"let me apply", "i'll apply", "now apply",
		"let me update", "i'll update", "now update",
		"let me fix", "i'll fix", "i will fix", "now fix",
		"let me rewrite", "i'll rewrite", "i will rewrite", "now rewrite",
		"let me repair", "i'll repair", "i will repair", "now repair",
		"let me find", "i'll find", "now find",
		"let me discover", "i'll discover", "now discover",
		"let me enumerate", "i'll enumerate", "now enumerate",
		"let me test", "i'll test", "now test",
		"let me verify", "i'll verify", "now verify",
		"let me explore", "i'll explore", "now explore",
		"let me check", "i'll check", "now check",
		"let me inspect", "i'll inspect", "now inspect",
		"let me read", "i'll read", "now read",
		"let me look", "i'll look", "now look",
		"let me search", "i'll search", "now search",
		"let me fetch", "i'll fetch", "now fetch",
		"let me begin", "let me start", "let me proceed", "let me first",
		"i'll start", "i'll begin", "i'll proceed", "i will start", "i will begin",
		"i need to fix", "i need to rewrite", "i need to repair",
		"right — let me", "right - let me", "right, let me",
		// Narrow intent phrases only — removed "here we go/here's the/here is the/
		// starting with/beginning with/first," because they all fire on completed answers.
		"first, i'll", "first, i will", "first, let me",
	}
	for _, p := range actPhrases {
		if strings.Contains(tail, p) {
			return true
		}
	}
	return false
}

// buildDirectivePrompt is used when the model has narrated its intent multiple
// times without calling any tools. It strips the soft "continue" framing and
// demands an immediate tool call.
func buildDirectivePrompt(lastText string) string {
	lower := strings.ToLower(lastText)

	// Find the last sentence where the model described what it was about to do.
	intentPhrases := []string{
		"let me write", "let me create", "let me build", "let me update", "let me add",
		"let me fix", "let me rewrite", "let me repair",
		"let me find", "let me explore", "let me check", "let me inspect", "let me read", "let me search", "let me fetch",
		"let me look", "let me discover", "let me enumerate", "let me test", "let me verify", "let me start", "let me begin",
		"right — let me", "right - let me", "right, let me",
		"now i'll write", "now i'll create", "now i'll fix", "now i'll rewrite", "i'll now write", "i will write", "i will now",
		"now write", "now create", "now fix", "now rewrite", "now find", "now discover", "now enumerate", "now test", "now verify", "now explore", "now check", "now inspect",
		"i'll write", "i'll create", "i'll fix", "i'll rewrite", "i'll repair", "i'll find", "i'll explore", "i'll check", "i'll inspect",
		"i need to fix", "i need to rewrite", "i need to repair",
	}
	intent := ""
	for _, p := range intentPhrases {
		if idx := strings.LastIndex(lower, p); idx >= 0 {
			snip := strings.TrimSpace(lastText[idx:])
			if len(snip) > 200 {
				snip = snip[:200]
			}
			intent = snip
			break
		}
	}

	if intent != "" {
		if isInspectionIntent(intent) {
			return fmt.Sprintf(
				"You have stated your intent (%q) but have not called any tools. "+
					"Do NOT write any more explanatory text. "+
					"Call the appropriate inspection/research tool RIGHT NOW: glob, grep, read_file, read_many, read_pdf, shell, web_search, or fetch_url. "+
					"The tool call must be your very next action.",
				intent,
			)
		}
		return fmt.Sprintf(
			"You have stated your intent (%q) but have not called any tools. "+
				"Do NOT write any more explanatory text. "+
				"Call write_file or edit_file RIGHT NOW with the actual file content. "+
				"The tool call must be your very next action.",
			intent,
		)
	}
	return "You have been describing what you will do without calling any tools. " +
		"Stop narrating. Call the appropriate tool (write_file, edit_file, shell, etc.) immediately — " +
		"your next response must contain a tool call, not text."
}

func buildMalformedToolMarkupPrompt(rawText string, toolDefs []llm.ToolDef) string {
	names := enabledToolNames(toolDefs)
	tail := strings.TrimSpace(rawText)
	if len(tail) > 600 {
		tail = tail[len(tail)-600:]
	}
	return fmt.Sprintf(
		"You emitted malformed or backend-native tool markup that TheMauler could not convert into a real tool call. "+
			"Do not repeat the malformed markup and do not explain. Make exactly one valid tool call now using one of the enabled tools: %s.\n\n"+
			"Malformed tail for reference:\n%s",
		strings.Join(names, ", "),
		tail,
	)
}

func visibleTextBeforeInlineToolMarkup(text string) string {
	cut := len(text)
	for _, marker := range []string{
		"<|tool_call>", "<tool_call>", "<call:", "<function=", "<parameters>",
		"<|channel>", "<|channel|>", "<channel>",
	} {
		if idx := strings.Index(strings.ToLower(text), strings.ToLower(marker)); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	return strings.TrimSpace(sanitizeVisibleModelText(text[:cut]))
}

func enabledToolNames(toolDefs []llm.ToolDef) []string {
	names := make([]string, 0, len(toolDefs))
	for _, def := range toolDefs {
		if def.Function.Name != "" {
			names = append(names, def.Function.Name)
		}
	}
	sort.Strings(names)
	return names
}

func toolProtocolDebugDetail(rawText, visibleText string, calls []llm.ToolCallDef, toolDefs []llm.ToolDef) string {
	var sb strings.Builder
	if len(calls) > 0 {
		names := make([]string, 0, len(calls))
		for _, call := range calls {
			names = append(names, call.Function.Name)
		}
		fmt.Fprintf(&sb, "converted_calls=%d names=%s\n", len(calls), strings.Join(names, ","))
	} else {
		fmt.Fprintf(&sb, "converted_calls=0\n")
	}
	fmt.Fprintf(&sb, "enabled_tools=%d\n", len(toolDefs))
	fmt.Fprintf(&sb, "markers=%s\n", strings.Join(toolProtocolMarkers(rawText), ","))
	fmt.Fprintf(&sb, "raw_tail=%s\n", tailForLog(rawText, 900))
	if strings.TrimSpace(visibleText) != "" {
		fmt.Fprintf(&sb, "visible_tail=%s\n", tailForLog(visibleText, 500))
	}
	return sb.String()
}

func toolProtocolMarkers(text string) []string {
	lower := strings.ToLower(text)
	candidates := []string{
		"<tool_call", "<|tool_call", "</tool_call>", "<parameters", "<function=", "<parameter=",
		"<call:", "call:", "tool_name", "tool_argument", "```json", "<|channel", "<channel|>",
	}
	var out []string
	for _, marker := range candidates {
		if strings.Contains(lower, marker) {
			out = append(out, marker)
		}
	}
	return out
}

func tailForLog(text string, max int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
	if len(text) > max {
		text = text[len(text)-max:]
	}
	return text
}

func sanitizeVisibleModelText(text string) string {
	if text == "" {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	lowerTrimmed := strings.ToLower(trimmed)
	for _, prefix := range []string{"thought<", "analysis<", "final<"} {
		if strings.HasPrefix(lowerTrimmed, prefix) {
			text = trimmed[len(strings.TrimSuffix(prefix, "<")):]
			break
		}
	}
	if idx := firstChatTemplateBoundary(text); idx >= 0 {
		text = text[:idx]
	}
	text = stripGemmaChannelBlocks(text)
	for _, pair := range [][2]string{
		{"<|channel|>thought <channel|>", ""},
		{"<|channel>thought <channel|>", ""},
		{"<|channel|>analysis <channel|>", ""},
		{"<|channel>analysis <channel|>", ""},
		{"<|channel|>final <channel|>", ""},
		{"<|channel>final <channel|>", ""},
		{"<|channel|>", ""},
		{"<|channel>", ""},
		{"<|message|>", ""},
		{"<|message>", ""},
		{"<|start|>", ""},
		{"<|start>", ""},
		{"<|end|>", ""},
		{"<|end>", ""},
		{"<|end_of_turn|>", ""},
		{"<|end_of_turn>", ""},
		{"<start_of_turn>", ""},
		{"<end_of_turn>", ""},
		{"<channel|>", ""},
		{"<channel>", ""},
	} {
		text = strings.ReplaceAll(text, pair[0], pair[1])
	}
	channelRe := regexp.MustCompile(`(?i)<\|?channel\|?>?\s*(thought|analysis|final|commentary)\s*`)
	text = channelRe.ReplaceAllString(text, "")
	text = stripBareChannelPrefix(text)
	return strings.TrimLeft(text, " \t\r\n")
}

func stripGemmaChannelBlocks(text string) string {
	for _, open := range []string{"<|channel>thought", "<|channel|>thought", "<|channel>analysis", "<|channel|>analysis"} {
		for {
			start := strings.Index(text, open)
			if start < 0 {
				break
			}
			afterOpen := start + len(open)
			relEnd := strings.Index(text[afterOpen:], "<channel|>")
			if relEnd < 0 {
				text = text[:start]
				break
			}
			closeEnd := afterOpen + relEnd + len("<channel|>")
			text = text[:start] + text[closeEnd:]
		}
	}
	return text
}

func stripBareChannelPrefix(text string) string {
	trimmedLeft := strings.TrimLeft(text, " \t\r\n")
	lower := strings.ToLower(trimmedLeft)
	for _, prefix := range []string{"thought", "analysis", "final", "commentary"} {
		if !strings.HasPrefix(lower, prefix) || len(trimmedLeft) == len(prefix) {
			continue
		}
		next := rune(trimmedLeft[len(prefix)])
		if (next >= 'A' && next <= 'Z') || next == '.' || next == '!' || next == '?' || next == ':' || next == ',' || next == ' ' || next == '\t' || next == '\r' || next == '\n' {
			return trimmedLeft[len(prefix):]
		}
	}
	return text
}

func firstChatTemplateBoundary(text string) int {
	lower := strings.ToLower(text)
	best := -1
	for _, marker := range []string{
		"<end_of_turn>",
		"<|end_of_turn|>",
		"<|end_of_turn>",
		"<start_of_turn>system",
		"<|start|>system",
		"<|start>system",
	} {
		if idx := strings.Index(lower, marker); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

func parseInlineToolMarkup(text string, toolDefs []llm.ToolDef) []llm.ToolCallDef {
	allowed := map[string]bool{}
	for _, def := range toolDefs {
		if def.Function.Name != "" {
			allowed[def.Function.Name] = true
		}
	}
	if len(allowed) == 0 {
		return nil
	}

	var calls []llm.ToolCallDef
	for _, call := range extractNamedToolCallTags(text, allowed, len(calls)) {
		calls = append(calls, call)
	}
	for _, call := range extractQwenToolCallBlocks(text, allowed, len(calls)) {
		calls = append(calls, call)
	}
	for _, call := range extractBraceToolCalls(text, allowed, len(calls)) {
		calls = append(calls, call)
	}
	for _, call := range extractPipeToolCalls(text, allowed, len(calls)) {
		calls = append(calls, call)
	}
	for _, call := range extractAngleCallTags(text, allowed, len(calls)) {
		calls = append(calls, call)
	}
	for _, call := range extractJSONToolObjects(text, allowed, len(calls)) {
		calls = append(calls, call)
	}
	for _, tag := range extractInlineTags(text) {
		name := strings.TrimSpace(tag.Name)
		if !allowed[name] {
			continue
		}
		args := inlineToolArgs(name, tag.Body)
		if len(args) == 0 {
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			continue
		}
		calls = append(calls, llm.ToolCallDef{
			ID:   fmt.Sprintf("inline_%d", len(calls)+1),
			Type: "function",
			Function: llm.FunctionCall{
				Name:      name,
				Arguments: raw,
			},
		})
	}
	for _, call := range extractInlineFunctionCalls(text, allowed, len(calls)) {
		calls = append(calls, call)
	}
	return validInlineToolCalls(calls, toolDefs)
}

func validInlineToolCalls(calls []llm.ToolCallDef, toolDefs []llm.ToolDef) []llm.ToolCallDef {
	required := map[string][]string{}
	for _, def := range toolDefs {
		var schema struct {
			Required []string `json:"required"`
		}
		if len(def.Function.Parameters) > 0 && json.Unmarshal(def.Function.Parameters, &schema) == nil {
			required[def.Function.Name] = schema.Required
		}
	}
	out := make([]llm.ToolCallDef, 0, len(calls))
	for _, call := range calls {
		req := required[call.Function.Name]
		if len(req) == 0 {
			out = append(out, call)
			continue
		}
		var args map[string]interface{}
		if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
			continue
		}
		ok := true
		for _, key := range req {
			val, exists := args[key]
			if !exists || emptyInlineArgValue(val) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, call)
		}
	}
	return out
}

func emptyInlineArgValue(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	default:
		return false
	}
}

func containsInlineToolMarkup(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<|tool_call") ||
		strings.Contains(lower, "<call:") ||
		regexp.MustCompile(`(?is)\bcall\s*:\s*[a-zA-Z_][a-zA-Z0-9_]*`).MatchString(text)
}

func extractNamedToolCallTags(text string, allowed map[string]bool, offset int) []llm.ToolCallDef {
	re := regexp.MustCompile(`(?is)<tool_call\b([^>]*)>(.*?)(?:</tool_call>|$)`)
	matches := re.FindAllStringSubmatch(text, -1)
	var calls []llm.ToolCallDef
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := namedToolCallAttr(match[1])
		if name == "" || !allowed[name] {
			continue
		}
		args := namedToolCallArgs(match[1], match[2])
		if args == nil {
			args = map[string]interface{}{}
		}
		raw, err := json.Marshal(args)
		if err != nil {
			continue
		}
		calls = append(calls, llm.ToolCallDef{
			ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
			Type: "function",
			Function: llm.FunctionCall{
				Name:      name,
				Arguments: raw,
			},
		})
	}
	return calls
}

func namedToolCallAttr(attrs string) string {
	re := regexp.MustCompile(`(?is)\b(?:name|tool|function)\s*=\s*("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s>]+)`)
	match := re.FindStringSubmatch(attrs)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(cleanInlineToolValue(match[1]))
}

func namedToolCallArgs(attrs, body string) map[string]interface{} {
	if args := namedToolCallParametersAttr(attrs); len(args) > 0 {
		return args
	}
	body = strings.TrimSpace(body)
	paramRe := regexp.MustCompile(`(?is)<parameters?\b[^>]*>(.*?)(?:</parameters?>|$)`)
	if match := paramRe.FindStringSubmatch(body); len(match) >= 2 {
		body = strings.TrimSpace(match[1])
	}
	if obj := firstJSONObject(body); len(obj) > 0 {
		return obj
	}
	return qwenParameterArgs(body)
}

func namedToolCallParametersAttr(attrs string) map[string]interface{} {
	idx := strings.Index(strings.ToLower(attrs), "parameters")
	if idx < 0 {
		return nil
	}
	rest := attrs[idx+len("parameters"):]
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "=") {
		return nil
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))
	if args := firstJSONObject(rest); len(args) > 0 {
		return args
	}
	unescapedRest := strings.ReplaceAll(html.UnescapeString(rest), `\"`, `"`)
	if args := firstJSONObject(unescapedRest); len(args) > 0 {
		return args
	}
	if strings.HasPrefix(rest, `"`) || strings.HasPrefix(rest, `'`) {
		quote := rest[0]
		escaped := false
		for i := 1; i < len(rest); i++ {
			if escaped {
				escaped = false
				continue
			}
			if rest[i] == '\\' {
				escaped = true
				continue
			}
			if rest[i] == quote {
				var args map[string]interface{}
				value := html.UnescapeString(rest[1:i])
				if err := json.Unmarshal([]byte(value), &args); err == nil {
					return args
				}
				return nil
			}
		}
		return nil
	}
	return firstJSONObject(rest)
}

func extractQwenToolCallBlocks(text string, allowed map[string]bool, offset int) []llm.ToolCallDef {
	blockRe := regexp.MustCompile(`(?is)<tool_call\b[^>]*>(.*?)(?:</tool_call>|$)`)
	matches := blockRe.FindAllStringSubmatch(text, -1)
	var calls []llm.ToolCallDef
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		block := strings.TrimSpace(match[1])
		before := len(calls)
		for _, parsed := range parseQwenFunctionBlocks(block, allowed) {
			raw, err := json.Marshal(parsed.Args)
			if err != nil {
				continue
			}
			calls = append(calls, llm.ToolCallDef{
				ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
				Type: "function",
				Function: llm.FunctionCall{
					Name:      parsed.Name,
					Arguments: raw,
				},
			})
		}
		if len(calls) > before {
			continue
		}
		if obj := firstJSONObject(block); len(obj) > 0 {
			name, args := normalizeInlineToolObject(obj, allowed)
			if name == "" {
				continue
			}
			raw, err := json.Marshal(args)
			if err != nil {
				continue
			}
			calls = append(calls, llm.ToolCallDef{
				ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
				Type: "function",
				Function: llm.FunctionCall{
					Name:      name,
					Arguments: raw,
				},
			})
		}
	}
	return calls
}

func extractPipeToolCalls(text string, allowed map[string]bool, offset int) []llm.ToolCallDef {
	var candidates []string
	blockRe := regexp.MustCompile(`(?is)<\|?tool_call\|?\b[^>]*>(.*?)(?:</?tool_call\|?>|<\|/?tool_call\|?>|$)`)
	for _, match := range blockRe.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			candidates = append(candidates, match[1])
		}
	}
	if len(candidates) == 0 {
		candidates = append(candidates, text)
	}
	callRe := regexp.MustCompile(`(?is)\bcall\s*:\s*([a-zA-Z_][\w]*)\s*\|([^\r\n]*)`)
	var calls []llm.ToolCallDef
	for _, candidate := range candidates {
		for _, match := range callRe.FindAllStringSubmatch(candidate, -1) {
			if len(match) < 3 {
				continue
			}
			name := strings.TrimSpace(match[1])
			if !allowed[name] {
				continue
			}
			args := pipeToolArgs(match[2])
			if len(args) == 0 {
				continue
			}
			raw, err := json.Marshal(args)
			if err != nil {
				continue
			}
			calls = append(calls, llm.ToolCallDef{
				ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
				Type: "function",
				Function: llm.FunctionCall{
					Name:      name,
					Arguments: raw,
				},
			})
		}
	}
	return calls
}

func extractBraceToolCalls(text string, allowed map[string]bool, offset int) []llm.ToolCallDef {
	re := regexp.MustCompile(`(?is)\bcall\s*:\s*([a-zA-Z_][\w]*)\s*(\{.*?\})(?:\s*(?:<tool_call|</tool_call|<\|tool_call|\z))`)
	matches := re.FindAllStringSubmatch(text, -1)
	var calls []llm.ToolCallDef
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if !allowed[name] {
			continue
		}
		args := braceToolArgs(match[2])
		if len(args) == 0 {
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			continue
		}
		calls = append(calls, llm.ToolCallDef{
			ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
			Type: "function",
			Function: llm.FunctionCall{
				Name:      name,
				Arguments: raw,
			},
		})
	}
	return calls
}

func braceToolArgs(text string) map[string]interface{} {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "{")
	text = strings.TrimSuffix(text, "}")
	return pipeToolArgs(text)
}

func extractAngleCallTags(text string, allowed map[string]bool, offset int) []llm.ToolCallDef {
	re := regexp.MustCompile(`(?is)<call:([a-zA-Z_][\w]*)\s+([^>]*?)/?>`)
	matches := re.FindAllStringSubmatch(text, -1)
	var calls []llm.ToolCallDef
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if !allowed[name] {
			continue
		}
		args := angleCallArgs(match[2])
		if len(args) == 0 {
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			continue
		}
		calls = append(calls, llm.ToolCallDef{
			ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
			Type: "function",
			Function: llm.FunctionCall{
				Name:      name,
				Arguments: raw,
			},
		})
	}
	return calls
}

func angleCallArgs(text string) map[string]interface{} {
	re := regexp.MustCompile(`(?is)([a-zA-Z_][\w]*)\s*=\s*("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s/>]+)`)
	matches := re.FindAllStringSubmatch(text, -1)
	args := map[string]interface{}{}
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		key := strings.TrimSpace(match[1])
		val := cleanInlineToolValue(match[2])
		if key == "" || val == "" {
			continue
		}
		switch key {
		case "limit", "timeout", "max_chars", "start_page", "end_page", "start_line", "end_line", "chunk_index", "chunk_size_lines":
			if n, err := strconv.Atoi(val); err == nil {
				args[key] = n
			} else {
				args[key] = val
			}
		default:
			args[key] = val
		}
	}
	return args
}

func pipeToolArgs(text string) map[string]interface{} {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "</tool_call>")
	text = strings.TrimSuffix(text, "<tool_call|>")
	text = strings.Trim(text, " \t\r\n|>")
	if text == "" {
		return nil
	}
	parts := splitPipeArgs(text)
	args := map[string]interface{}{}
	for _, part := range parts {
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.Trim(key, `"'`))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if parsed, ok := parseInlineJSONValue(value); ok {
			args[key] = parsed
			continue
		}
		cleaned := cleanInlineToolValue(value)
		if cleaned == "" {
			continue
		}
		switch key {
		case "limit", "timeout", "max_chars", "start_page", "end_page", "start_line", "end_line", "chunk_index", "chunk_size_lines":
			if n, err := strconv.Atoi(cleaned); err == nil {
				args[key] = n
			} else {
				args[key] = cleaned
			}
		default:
			args[key] = cleaned
		}
	}
	return args
}

func splitPipeArgs(text string) []string {
	var parts []string
	var sb strings.Builder
	quote := rune(0)
	bracketDepth := 0
	braceDepth := 0
	for _, r := range text {
		switch {
		case quote != 0:
			sb.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
			sb.WriteRune(r)
		case r == '[':
			bracketDepth++
			sb.WriteRune(r)
		case r == ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			sb.WriteRune(r)
		case r == '{':
			braceDepth++
			sb.WriteRune(r)
		case r == '}':
			if braceDepth > 0 {
				braceDepth--
			}
			sb.WriteRune(r)
		case r == ',' || r == '|':
			if bracketDepth > 0 || braceDepth > 0 {
				sb.WriteRune(r)
				continue
			}
			if strings.TrimSpace(sb.String()) != "" {
				parts = append(parts, sb.String())
			}
			sb.Reset()
		default:
			sb.WriteRune(r)
		}
	}
	if strings.TrimSpace(sb.String()) != "" {
		parts = append(parts, sb.String())
	}
	return parts
}

func parseInlineJSONValue(value string) (interface{}, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "</tool_call>")
	value = strings.TrimSuffix(value, "<tool_call>")
	value = strings.TrimSuffix(value, "<tool_call|>")
	value = strings.TrimSpace(value)
	if !(strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{")) {
		return nil, false
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(value), &parsed); err == nil {
		return parsed, true
	}
	return nil, false
}

func cleanInlineToolValue(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "<|'>", "")
	value = strings.ReplaceAll(value, "<|'}>", "")
	value = strings.ReplaceAll(value, "<|", "")
	value = strings.ReplaceAll(value, "|>", "")
	value = stripInlineToolTags(value)
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return value
}

func extractJSONToolObjects(text string, allowed map[string]bool, offset int) []llm.ToolCallDef {
	blocks := fencedJSONBlocks(text)
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		blocks = append(blocks, trimmed)
	} else if strings.HasPrefix(trimmed, "[") {
		blocks = append(blocks, trimmed)
	}
	var calls []llm.ToolCallDef
	for _, block := range blocks {
		for _, parsed := range normalizeJSONToolCalls(block, allowed) {
			raw, err := json.Marshal(parsed.Args)
			if err != nil {
				continue
			}
			calls = append(calls, llm.ToolCallDef{
				ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
				Type: "function",
				Function: llm.FunctionCall{
					Name:      parsed.Name,
					Arguments: raw,
				},
			})
		}
	}
	return calls
}

func normalizeJSONToolCalls(text string, allowed map[string]bool) []parsedInlineCall {
	var value interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &value); err != nil {
		if obj := firstJSONObject(text); len(obj) > 0 {
			value = obj
		} else {
			return nil
		}
	}
	var out []parsedInlineCall
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if obj, ok := item.(map[string]interface{}); ok {
				if name, args := normalizeInlineToolObject(obj, allowed); name != "" {
					out = append(out, parsedInlineCall{Name: name, Args: args})
				}
			}
		}
	case map[string]interface{}:
		if name, args := normalizeInlineToolObject(typed, allowed); name != "" {
			out = append(out, parsedInlineCall{Name: name, Args: args})
		}
	}
	return out
}

func fencedJSONBlocks(text string) []string {
	re := regexp.MustCompile("(?is)```(?:json)?\\s*(.*?)```")
	matches := re.FindAllStringSubmatch(text, -1)
	var blocks []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		body := strings.TrimSpace(match[1])
		if strings.Contains(body, "{") {
			blocks = append(blocks, body)
		}
	}
	return blocks
}

func normalizeInlineToolObject(obj map[string]interface{}, allowed map[string]bool) (string, map[string]interface{}) {
	if name := inlineToolObjectName(obj); name != "" && allowed[name] {
		if args := inlineToolObjectArgs(obj); len(args) > 0 {
			return name, args
		}
		return name, map[string]interface{}{}
	}
	name := inferInlineToolName(obj, allowed)
	if name == "" {
		return "", nil
	}
	return name, obj
}

func inlineToolObjectName(obj map[string]interface{}) string {
	for _, key := range []string{"name", "tool", "tool_name", "function", "function_name"} {
		if val, ok := obj[key]; ok {
			name := strings.TrimSpace(fmt.Sprint(val))
			if name != "" {
				return name
			}
		}
	}
	return ""
}

func inlineToolObjectArgs(obj map[string]interface{}) map[string]interface{} {
	for _, key := range []string{"arguments", "parameters", "params", "input", "tool_argument", "tool_arguments"} {
		val, ok := obj[key]
		if !ok {
			continue
		}
		if args, ok := val.(map[string]interface{}); ok {
			return args
		}
		return map[string]interface{}{key: val}
	}
	args := map[string]interface{}{}
	for key, val := range obj {
		switch key {
		case "name", "tool", "tool_name", "function", "function_name":
			continue
		default:
			args[key] = val
		}
	}
	return args
}

type parsedInlineCall struct {
	Name string
	Args map[string]interface{}
}

func parseQwenFunctionBlocks(block string, allowed map[string]bool) []parsedInlineCall {
	fnRe := regexp.MustCompile(`(?is)<function=([a-zA-Z_][\w]*)\b[^>]*>(.*?)(?:</function>|$)`)
	matches := fnRe.FindAllStringSubmatch(block, -1)
	var out []parsedInlineCall
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if !allowed[name] {
			continue
		}
		body := strings.TrimSpace(match[2])
		args := qwenParameterArgs(body)
		if len(args) == 0 {
			args = firstJSONObject(body)
		}
		if len(args) == 0 {
			continue
		}
		out = append(out, parsedInlineCall{Name: name, Args: args})
	}
	return out
}

func qwenParameterArgs(body string) map[string]interface{} {
	paramRe := regexp.MustCompile(`(?is)<parameter=([a-zA-Z_][\w]*)\b[^>]*>(.*?)(?:</parameter>|$)`)
	matches := paramRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	args := map[string]interface{}{}
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		key := strings.TrimSpace(match[1])
		val := html.UnescapeString(strings.TrimSpace(stripInlineToolTags(match[2])))
		if val == "" {
			continue
		}
		switch key {
		case "limit", "timeout", "max_chars", "start_page", "end_page":
			if n, err := strconv.Atoi(val); err == nil {
				args[key] = n
			} else {
				args[key] = val
			}
		default:
			args[key] = val
		}
	}
	return args
}

func firstJSONObject(text string) map[string]interface{} {
	start := strings.Index(text, "{")
	if start < 0 {
		return nil
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(text[start:i+1]), &args); err == nil {
					return args
				}
				return nil
			}
		}
	}
	return nil
}

func inferInlineToolName(args map[string]interface{}, allowed map[string]bool) string {
	if _, ok := args["command"]; ok {
		if allowed["shell"] {
			return "shell"
		}
		if allowed["bash"] {
			return "bash"
		}
	}
	if _, ok := args["query"]; ok && allowed["session_search"] {
		return "session_search"
	}
	if val, ok := args["path"]; ok {
		path := fmt.Sprint(val)
		if strings.HasSuffix(strings.ToLower(path), ".pdf") && allowed["read_pdf"] {
			return "read_pdf"
		}
		if allowed["read_file"] {
			return "read_file"
		}
		if allowed["read_pdf"] {
			return "read_pdf"
		}
	}
	if val, ok := args["pattern"]; ok {
		pattern := fmt.Sprint(val)
		if allowed["glob"] && looksLikeGlobPattern(pattern) {
			return "glob"
		}
		if allowed["grep"] {
			return "grep"
		}
		if allowed["glob"] {
			return "glob"
		}
	}
	return ""
}

func looksLikeGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[]") ||
		strings.Contains(pattern, "/") ||
		strings.Contains(pattern, `\`)
}

func extractInlineFunctionCalls(text string, allowed map[string]bool, offset int) []llm.ToolCallDef {
	var calls []llm.ToolCallDef
	for name := range allowed {
		re := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*\(\s*("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|` + "`" + `(?:\\.|[^` + "`" + `])*` + "`" + `|\{.*?\})\s*\)`)
		for _, match := range re.FindAllStringSubmatch(text, -1) {
			if len(match) < 2 {
				continue
			}
			args := inlineFunctionArgs(name, strings.TrimSpace(match[1]))
			if len(args) == 0 {
				continue
			}
			raw, err := json.Marshal(args)
			if err != nil {
				continue
			}
			calls = append(calls, llm.ToolCallDef{
				ID:   fmt.Sprintf("inline_%d", offset+len(calls)+1),
				Type: "function",
				Function: llm.FunctionCall{
					Name:      name,
					Arguments: raw,
				},
			})
		}
	}
	return calls
}

func inlineFunctionArgs(name, arg string) map[string]interface{} {
	if strings.HasPrefix(arg, "{") {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(arg), &args); err == nil && len(args) > 0 {
			return args
		}
		return nil
	}
	plain, ok := unquoteInlineArg(arg)
	if !ok || strings.TrimSpace(plain) == "" {
		return nil
	}
	switch name {
	case "shell", "bash":
		return map[string]interface{}{"command": plain}
	case "session_search":
		return map[string]interface{}{"query": plain, "limit": 10}
	case "read_file", "read_pdf":
		return map[string]interface{}{"path": plain}
	case "glob":
		return map[string]interface{}{"pattern": plain}
	case "grep":
		return map[string]interface{}{"pattern": plain}
	default:
		return nil
	}
}

func unquoteInlineArg(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < 2 {
		return "", false
	}
	if strings.HasPrefix(arg, "`") && strings.HasSuffix(arg, "`") {
		return strings.TrimSuffix(strings.TrimPrefix(arg, "`"), "`"), true
	}
	if strings.HasPrefix(arg, "'") && strings.HasSuffix(arg, "'") {
		arg = `"` + strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(arg, "'"), "'"), `"`, `\"`) + `"`
	}
	out, err := strconv.Unquote(arg)
	if err != nil {
		return "", false
	}
	return out, true
}

func inlineToolArgs(name, body string) map[string]interface{} {
	args := map[string]interface{}{}
	for _, child := range extractInlineTags(body) {
		key := strings.TrimSpace(child.Name)
		val := html.UnescapeString(strings.TrimSpace(stripInlineToolTags(child.Body)))
		if val == "" {
			continue
		}
		switch key {
		case "limit", "timeout", "max_chars", "start_page", "end_page":
			if n, err := strconv.Atoi(val); err == nil {
				args[key] = n
			}
		default:
			args[key] = val
		}
	}
	if len(args) == 0 {
		plain := html.UnescapeString(strings.TrimSpace(stripInlineToolTags(body)))
		if plain == "" {
			return nil
		}
		switch name {
		case "shell", "bash":
			args["command"] = plain
		case "session_search":
			args["query"] = plain
		case "read_file", "read_pdf":
			args["path"] = plain
		case "glob":
			args["pattern"] = plain
		case "grep":
			args["pattern"] = plain
		default:
			return nil
		}
	}
	if name == "session_search" {
		if _, ok := args["limit"]; !ok {
			args["limit"] = 10
		}
	}
	return args
}

type inlineTag struct {
	Name string
	Body string
}

func extractInlineTags(text string) []inlineTag {
	startRe := regexp.MustCompile(`(?is)<([a-zA-Z_][\w]*)\b[^>]*>`)
	matches := startRe.FindAllStringSubmatchIndex(text, -1)
	var tags []inlineTag
	cursor := 0
	lowerText := strings.ToLower(text)
	for _, match := range matches {
		if len(match) < 4 || match[0] < cursor {
			continue
		}
		name := text[match[2]:match[3]]
		closeTag := "</" + strings.ToLower(name) + ">"
		closeIdx := strings.Index(lowerText[match[1]:], closeTag)
		if closeIdx < 0 {
			continue
		}
		bodyStart := match[1]
		bodyEnd := match[1] + closeIdx
		tags = append(tags, inlineTag{Name: name, Body: text[bodyStart:bodyEnd]})
		cursor = bodyEnd + len(closeTag)
	}
	return tags
}

func stripInlineToolTags(s string) string {
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)
	return strings.TrimSpace(tagRe.ReplaceAllString(s, ""))
}

func isInspectionIntent(text string) bool {
	lower := strings.ToLower(text)
	return hasAny(lower, "find", "explore", "check", "inspect", "read", "search", "fetch", "look", "discover", "enumerate", "test", "verify", "start", "begin")
}

func needsInspectionTool(text string) bool {
	lower := strings.ToLower(text)
	return hasAny(lower,
		"repo", "repository", "codebase", "code base", "project", "workspace",
		"files", "file tree", "directory", "folder",
		"look at", "look into", "inspect", "read", "explore", "what's in", "whats in",
	)
}

// looksFinished returns true when the response appears to be a complete,
// self-contained answer.  Used to short-circuit false positives in
// looksIncomplete / looksAboutToAct — if the model clearly just answered, we
// should not auto-continue even if some phrase in the body happens to match.
func looksFinished(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	// Multi-paragraph response ending with a sentence-final character is almost
	// certainly complete.
	last := lower[len(lower)-1]
	if strings.Count(text, "\n\n") >= 1 && (last == '.' || last == '!' || last == '?') {
		return true
	}
	// Offer-to-help closings are the canonical end of a conversational response.
	closings := []string{
		"what would you like", "what else would you like",
		"is there anything else", "let me know if",
		"feel free to ask", "hope this helps",
		"does this help", "does this answer",
		"what would you like me to",
		"anything else i can", "happy to help",
	}
	for _, p := range closings {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// looksConversational returns true when the user's message is a factual or
// explanatory question that the model should be able to answer directly
// without any tool calls.
func looksConversational(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	normalised := strings.Trim(lower, " \t\r\n.!?,;:")

	// These strongly indicate a task that needs tools; bail out immediately.
	taskPhrases := []string{
		"write ", "create ", "make ", "build ", "implement ", "add ",
		"fix ", "debug ", "update ", "edit ", "change ", "modify ", "refactor ",
		"delete ", "remove ", "rename ", "move ",
		"search for", "find the file", "find file", "look for", "check the",
		"run ", "execute ", "compile ", "test ", "install ", "deploy ",
		"browse ", "fetch ", "download ", "open the", "read the file",
		"read ", "show me the file", "show the file", "list the files", "list files",
		"go online", "search online", "look online", "web search", "search web", "near me", "near ",
		"what's in this directory", "whats in this directory", "what is in this directory",
		"what's in this folder", "whats in this folder", "what is in this folder",
		"current directory", "this directory", "this folder",
	}
	for _, p := range taskPhrases {
		if strings.Contains(lower, p) {
			return false
		}
	}
	taskStarts := []string{"write", "create", "make", "build", "implement", "add", "fix", "debug", "update", "edit", "change", "modify", "refactor", "delete", "remove", "rename", "move", "run", "execute", "compile", "test", "install", "deploy", "browse", "fetch", "download", "read", "list"}
	for _, p := range taskStarts {
		if normalised == p || strings.HasPrefix(normalised, p+" ") {
			return false
		}
	}

	smallTalk := map[string]bool{
		"hello": true, "hi": true, "hey": true, "hey there": true, "hi there": true, "yo": true,
		"thanks": true, "thank you": true, "cheers": true, "ok": true, "okay": true, "cool": true,
		"nice": true, "yep": true, "yeah": true, "nope": true, "lol": true, "good morning": true,
		"good afternoon": true, "good evening": true,
	}
	if smallTalk[normalised] {
		return true
	}

	qaPhrases := []string{
		"what is ", "what are ", "what's ", "what was ", "what were ",
		"who is ", "who are ", "who was ", "who were ",
		"when is ", "when are ", "when was ", "when were ",
		"where is ", "where are ", "where was ",
		"why is ", "why are ", "why was ",
		"how does ", "how do ", "how is ", "how are ",
		"explain ", "describe ", "tell me about", "tell me what",
		"define ", "meaning of", "what does ",
		"do you know ", "do you think ", "can you explain",
		"what do you think", "what do you recommend",
		"is it ", "is there ", "are there ", "is this ",
		"show me how", "help me understand",
		"any tips", "any advice",
		"weather", "temperature", "forecast", "humidity",
		"capital of", "population of", "history of",
	}
	for _, p := range qaPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// toolChoiceFor returns the tool_choice value to send with the next request.
//
//   - "none"  → conversational question on the FIRST turn where no tools have
//     yet been called; tool definitions are still sent for KV-cache efficiency.
//   - "auto"  → model decides (default during any task turn or after tool use).
//
// We deliberately avoid "required" because local models can stall or loop when
// forced to produce a tool call they don't need.
func toolChoiceFor(firstUserText string, autoContinues int, totalToolCallsMade int) string {
	// Mid-task turns: always let the model decide freely.
	if autoContinues > 0 || totalToolCallsMade > 0 {
		return "auto"
	}
	if looksConversational(firstUserText) {
		return "none"
	}
	return "auto"
}

func toolDefsAndChoiceForTurn(registry *tools.Registry, cfg settings.ToolsConfig, firstUserText string, autoContinues int, totalToolCallsMade int) ([]llm.ToolDef, string) {
	toolChoice := toolChoiceFor(firstUserText, autoContinues, totalToolCallsMade)
	if !cfg.Enabled || toolChoice == "none" {
		return nil, toolChoice
	}
	enabled := settings.EffectiveEnabledTools(cfg)
	if looksShellCentricTask(firstUserText) && !explicitWebResearchIntent(firstUserText) {
		enabled = cloneToolEnabledMap(enabled)
		enabled["subagent_research"] = false
	}
	return registry.ToEnabledToolDefs(enabled), toolChoice
}

func filterToolDefsByName(toolDefs []llm.ToolDef, names ...string) []llm.ToolDef {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	out := make([]llm.ToolDef, 0, len(toolDefs))
	for _, def := range toolDefs {
		if allowed[def.Function.Name] {
			out = append(out, def)
		}
	}
	return out
}

func documentationRecoveryPrompt(prompt, stopReason, stopDetail string) string {
	var sb strings.Builder
	sb.WriteString("The user explicitly asked for a README/writeup/docs update as work progresses, but no document has been written yet. ")
	if stopReason != "" {
		sb.WriteString("The run also hit stop reason `" + stopReason + "`. ")
	}
	if stopDetail != "" {
		sb.WriteString("Stop detail: " + truncateRunes(stopDetail, 240) + " ")
	}
	sb.WriteString("Do not perform more web, browser, or shell research now. Use read_file/glob/grep if needed, then call write_file or edit_file to update the requested documentation with the verified facts gathered so far. If the named file is missing, create it in the active workspace using the requested name or the closest existing writeup name from the prompt. Original user request: ")
	sb.WriteString(truncateRunes(prompt, 300))
	return sb.String()
}

func cloneToolEnabledMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func looksShellCentricTask(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"htb", "hackthebox", "hack the box",
		"pentest", "penetration test", "lab target", "target ip", "target url",
		"wsl", "kali", "vpn", "tun0", "sudo", "nmap", "gobuster", "ffuf", "feroxbuster", "dirsearch", "nikto", "searchsploit",
		"user.txt", "root.txt", "foothold", "privilege escalation", "privesc", "recon", "enumerate", "enumeration",
		"dvwa", "command injection", "cmd injection", "sql injection", "sqli", "xss", "lfi", "rfi", "ssrf",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func explicitWebResearchIntent(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "research online") ||
		strings.Contains(lower, "look online") ||
		strings.Contains(lower, "web research") ||
		strings.Contains(lower, "search the web") ||
		strings.Contains(lower, "find writeup") ||
		strings.Contains(lower, "find documentation")
}

func agentToolBudgetExhausted(cfg settings.AgentsConfig, used int) bool {
	return cfg.MaxToolCalls > 0 && used >= cfg.MaxToolCalls
}

func agentToolBudgetSummaryPrompt(maxToolCalls int) string {
	return fmt.Sprintf(
		"Agent tool-call budget is exhausted (%d calls). Do not emit tool calls or tool markup. "+
			"Write a concise final progress summary for the user now: what was attempted, what worked, what failed, current evidence, and the safest next manual step. "+
			"If more work is needed, ask the user before continuing.",
		maxToolCalls,
	)
}

func extractPath(tc llm.ToolCallDef) string {
	raw := string(tc.Function.Arguments)
	for _, key := range []string{`"path"`, `"file"`, `"filename"`} {
		idx := strings.Index(raw, key)
		if idx < 0 {
			continue
		}
		after := raw[idx+len(key):]
		colon := strings.Index(after, ":")
		if colon < 0 {
			continue
		}
		after = strings.TrimSpace(after[colon+1:])
		if len(after) > 0 && after[0] == '"' {
			end := strings.Index(after[1:], `"`)
			if end >= 0 {
				return after[1 : end+1]
			}
		}
	}
	return ""
}

func isWriteTool(name string) bool {
	return name == "write_file" || name == "edit_file"
}
