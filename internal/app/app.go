// Package app is the Wails application backend.
// All exported methods on App are callable from the TypeScript frontend.
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mauler/internal/agent"
	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/llm/backends"
	"mauler/internal/sessionstore"
	"mauler/internal/settings"
	"mauler/internal/store"
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

	pty "github.com/aymanbagabas/go-pty"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails application struct.
type App struct {
	ctx context.Context
	// suppressEvents is used by headless harnesses/tests that execute app logic
	// without a Wails lifecycle context.
	suppressEvents bool

	mu       sync.Mutex
	cfg      *settings.Settings
	profiles *settings.ProfilesFile

	history  *agent.History
	rollback *agent.Rollback
	registry *tools.Registry
	ledger   *ledger.Ledger
	db       *sql.DB

	// contextWindow is the model's full loaded context (tokens). history.Budget()
	// is this minus the output reserve; we keep the window so the status bar can
	// show all parts (used / usable budget / reserved / window).
	contextWindow int

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
	shellOpenMu sync.Mutex
	shellMu     sync.Mutex
	shellSess   *shellSession

	// background jobs launched in the shared terminal (id -> job), so the agent
	// can fire a long scan and poll it instead of blocking.
	bgMu      sync.Mutex
	bgJobs    map[string]*bgJob
	bgCounter int

	// Auto-speculative (MTP) decoding state. specPlan is the last resolved plan
	// surfaced to the UI; specOverrides pins per-profile choices ("on"/"off");
	// specGuard records per-profile stability-guard trips; specTruncStreak counts
	// consecutive MTP-suspect truncations toward tripping that guard.
	specMu          sync.Mutex
	specPlan        SpecPlan
	specOverrides   map[string]string
	specGuard       map[string]string
	specTruncStreak int
}

// bgJob is one detached command running in the shared terminal session, with its
// output redirected to a logfile and its PID written to a pidfile.
type bgJob struct {
	id         string
	command    string
	log        string
	pidfile    string
	started    time.Time
	lastPoll   time.Time
	pollCount  int
	lastState  string
	lastOutput string
}

// New creates a new App with settings loaded from disk.
func New() *App {
	cfg, _ := settings.Load()
	profiles, _ := settings.LoadProfiles()
	ensureActiveProfile(cfg, profiles)
	runLedger, _ := ledger.NewDefault()
	db, _ := store.OpenDefault()
	if runLedger != nil && db != nil {
		runLedger.AttachDB(db)
		_, _ = runLedger.BackfillDBFromJSONL()
	}
	if db != nil {
		tools.SetTodoDB(db)
		_, _ = tools.MigrateTodosJSONToDB(db)
		setMemoryDB(db)
		_, _ = migrateMemoryJSONToDB(db)
	}

	active := activeProfile(cfg, profiles)
	history := agent.NewHistory(active.CtxTokens)

	app := &App{
		cfg:        cfg,
		profiles:   profiles,
		history:    history,
		rollback:   &agent.Rollback{},
		registry:   tools.New(),
		ledger:     runLedger,
		db:         db,
		autoAgents: true,
		bgJobs:     make(map[string]*bgJob),
	}
	app.registerAppTools()
	tools.SetConfigSnapshot(cfg.Tools)
	if db != nil {
		_ = migrateTaskRunsJSONToDB(db)
	}
	return app
}

// syncToolConfig pushes the current tool-relevant settings into the tools package so
// shell/protected-path tools read from memory instead of re-loading settings.toml on
// every call. Call after any in-memory settings mutation. Assumes a.mu is not required
// (a.cfg pointer is stable; callers already hold the lock where needed).
func (a *App) syncToolConfig() {
	tools.SetConfigSnapshot(a.cfg.Tools)
}

func (a *App) emit(event string, data ...interface{}) {
	if a == nil || a.suppressEvents || a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, event, data...)
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
	a.wireBackgroundJobObserver()
	tools.SweepStaleJobLogs() // clear orphan job logfiles left by a prior crash
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
	db := a.db
	a.db = nil
	a.mu.Unlock()
	tools.SetTodoDB(nil)
	setMemoryDB(nil)
	if db != nil {
		_ = db.Close()
	}
}

func (a *App) sessionStore() *sessionstore.Store {
	if a == nil || a.db == nil {
		return nil
	}
	return sessionstore.NewStore(a.db)
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
		a.emit("mauler:workspace_changed", cfg.Context.WorkspaceDir)
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
	p, ok := a.profiles.Profiles[name]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("profile %q not found", name)
	}
	a.cfg.ActiveProfile = name
	a.history.SetBudget(p.CtxTokens)
	active := applyProvider(p, a.profiles)
	if modelLoadKey(active) != a.loadedModelKey {
		a.loadedModelKey = ""
	}
	err := settings.Save(a.cfg)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	// Resolve MTP/speculative decoding for the newly active model. Best-effort and
	// off the lock: a probe failure degrades to name/registry signals, never blocks.
	a.autoApplySpec(name, active)
	return nil
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
	a.autoApplySpec(name, active)
	return nil
}

// HistoryStats is the payload returned to the frontend status bar.
type HistoryStats struct {
	TokenCount  int     `json:"token_count"`
	Budget      int     `json:"budget"`
	Fraction    float64 `json:"fraction"`
	RollbackLen int     `json:"rollback_len"`
	// Window is the model's full context; Reserve is the output headroom held
	// back from it (Window - Budget). The status bar shows all three parts.
	Window  int `json:"window"`
	Reserve int `json:"reserve"`
}

type LabStatus struct {
	AgentRoot      string                     `json:"agent_root"`
	ShellBackend   string                     `json:"shell_backend"`
	ShellDistro    string                     `json:"shell_distro"`
	ShellUser      string                     `json:"shell_user"`
	Target         string                     `json:"target"`
	VPNInterface   string                     `json:"vpn_interface"`
	LatestArtifact string                     `json:"latest_artifact"`
	OpsProfile     string                     `json:"ops_profile"`
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
	budget := a.history.Budget()
	window := a.contextWindow
	if window < budget {
		window = budget // not yet recorded — show budget as the window
	}
	return HistoryStats{
		TokenCount:  a.history.TokenCount(),
		Budget:      budget,
		Fraction:    a.history.UsageFraction(),
		RollbackLen: a.rollback.Len(),
		Window:      window,
		Reserve:     window - budget,
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
	if store := a.sessionStore(); store != nil {
		return store.StoreSession(name, workspaceScope(), model, toSessionStoreMessages(msgs))
	}
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
		if store := a.sessionStore(); store != nil {
			return store.DeleteSession(name, workspaceScope())
		}
		return sessionstore.DeleteDefaultSession(name, workspaceScope())
	}
	if err != nil {
		return err
	}
	if store := a.sessionStore(); store != nil {
		return store.DeleteSession(name, workspaceScope())
	}
	return sessionstore.DeleteDefaultSession(name, workspaceScope())
}

func (a *App) SearchSessionRecall(query string, limit int) ([]sessionstore.SearchResult, error) {
	if store := a.sessionStore(); store != nil {
		return store.Search(query, limit)
	}
	return sessionstore.SearchDefault(query, limit)
}

func (a *App) ClearSessionRecall() error {
	if store := a.sessionStore(); store != nil {
		return store.Clear()
	}
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
		if store := a.sessionStore(); store != nil {
			if err := store.StoreSession(name, scope, model, toSessionStoreMessages(msgs)); err != nil {
				return count, err
			}
		} else if err := sessionstore.StoreDefaultSession(name, scope, model, toSessionStoreMessages(msgs)); err != nil {
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
	if err := tools.SaveTodos([]tools.TodoItem{}); err != nil {
		return err
	}
	a.recordLedger(ledger.Event{
		Kind:    "planner_event",
		Source:  "todo",
		Tool:    "todo_clear",
		Status:  "cleared",
		Message: "active task plan cleared",
	})
	return nil
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
		a.emit("mauler:agent_mode", mode.Name)
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

	memorySelection := selectRelevantMemory(cfg, messageText)
	if results, err := a.SearchSessionRecall(messageText, 2); err == nil {
		memorySelection.AddSessionRecall(results)
	}
	if events, err := a.ListLedgerEvents(300); err == nil {
		memorySelection.AddEvidencePointers(events, messageText)
	}
	memories := memorySelection.Entries
	skills := relevantSkillsForSettings(cfg.Skills, cfg, messageText)
	run := startTaskRun(messageText, mode.Name, cfg.ActiveProfile, profile.ModelID)
	run.attachLedger(a.ledger)
	if len(memorySelection.Withheld) > 0 {
		run.addEvent("memory_conflict", fmt.Sprintf("Withheld %d conflicting memor%s from prompt injection", len(memorySelection.Withheld), plural(len(memorySelection.Withheld), "y", "ies")), strings.Join(memorySelection.Conflicts, "\n"))
	}
	if len(memorySelection.Plan.Selected) > 0 {
		run.addEvent("memory_retrieval_plan", fmt.Sprintf("Selected %d context item%s for %s prompt packet", len(memorySelection.Plan.Selected), plural(len(memorySelection.Plan.Selected), "", "s"), memorySelection.Plan.Intent), memoryRetrievalPlanDetail(memorySelection.Plan))
	}

	go a.runAgentLoop(agentCtx, userMsg, profile, &cfg, autonomous, mode, memories, skills, run)
	return nil
}

const maxMemoryReinjections = 3

const maxAutoDistilledMemories = 2

// autoDistillLearnings closes the learning loop at run finish: it mines this run's
// ledger for high-importance reflection candidates (repeated failures, blocking
// stops, tool errors) and persists the top few as durable constraint memories,
// so the agent stops repeating the same mistakes across runs. It only saves the
// reflection class (lessons), dedups against existing titles in this workspace,
// caps the count, and tags entries "auto" so the user can review or prune them.
func (a *App) autoDistillLearnings(run *TaskRun, cfg settings.MemoryConfig) {
	if !cfg.Enabled || cfg.DisableAutoDistill || run == nil {
		return
	}
	events, _ := a.ListLedgerEvents(400)
	runEvents := make([]ledger.Event, 0, len(events))
	for _, event := range events {
		if event.RunID == run.ID {
			runEvents = append(runEvents, event)
		}
	}
	// Reflection candidates come from the ledger; the inefficiency lesson below is
	// derived from run.Tools, so we proceed even when the ledger has no events.
	candidates := buildLearningCandidates(runEvents)
	scope := workspaceScope()
	existing, _ := loadMemory()
	existingTitles := map[string]bool{}
	for _, m := range existing {
		if m.Scope == "" || m.Scope == scope {
			existingTitles[strings.ToLower(strings.TrimSpace(m.Title))] = true
		}
	}
	saved := 0
	for _, candidate := range candidates {
		if saved >= maxAutoDistilledMemories {
			break
		}
		if candidate.Type != "reflection" || candidate.Importance < 4 {
			continue
		}
		title := strings.TrimSpace(candidate.Title)
		if title == "" || existingTitles[strings.ToLower(title)] {
			continue
		}
		tags := normaliseTags(append(append([]string{}, candidate.Tags...), "auto", "distilled"))
		if _, err := a.SaveMemoryEntry(MemoryEntry{
			Title:      title,
			Content:    candidate.Content,
			Kind:       firstNonEmpty(candidate.Kind, "constraint"),
			Confidence: "likely",
			Source:     "auto_distill",
			Tags:       tags,
			Importance: candidate.Importance,
			Scope:      scope,
		}); err != nil {
			continue
		}
		existingTitles[strings.ToLower(title)] = true
		saved++
	}
	// Inefficiency lesson: a storm of near-identical commands means the run should
	// have scripted the loop. Capture that even though the commands "succeeded", so
	// the brain learns from slow-but-working runs, not only outright failures.
	if saved < maxAutoDistilledMemories {
		if fam, n := largestShellFamily(run); n >= shellStormThreshold {
			bin := strings.SplitN(fam, "|", 2)[0]
			title := "Script repeated " + bin + " extraction instead of manual per-value requests"
			if !existingTitles[strings.ToLower(title)] {
				content := fmt.Sprintf("This run issued %d near-identical %s requests to the same endpoint to extract or enumerate data one value at a time. Lesson: for blind SQLi, fuzzing, or byte-window dumps, write a single script or bash loop and run it once, and persist extracted values to a file or the memory tool so they survive context compaction.", n, bin)
				if _, err := a.SaveMemoryEntry(MemoryEntry{
					Title:      title,
					Content:    content,
					Kind:       "constraint",
					Confidence: "likely",
					Source:     "auto_distill",
					Tags:       normaliseTags([]string{"auto", "distilled", "inefficiency", "workflow"}),
					Importance: 4,
					Scope:      scope,
				}); err == nil {
					existingTitles[strings.ToLower(title)] = true
					saved++
				}
			}
		}
	}
	if saved > 0 {
		run.addEvent("memory_distill", fmt.Sprintf("Auto-saved %d lesson%s to memory", saved, plural(saved, "", "s")), "")
	}
}

// maybeReinjectMemory re-scores durable memory against recent conversation and
// appends a compact note for entries that became relevant after the initial
// injection. It is bounded (at most maxMemoryReinjections notes per run, a few
// entries each) and deduplicated against everything already injected, so a long
// run picks up newly-relevant memory without re-dumping the whole store.
func (a *App) maybeReinjectMemory(run *TaskRun, cfg settings.Settings, injected map[string]bool, count *int) {
	if !cfg.Memory.Enabled || !cfg.Memory.AutoInject || *count >= maxMemoryReinjections {
		return
	}
	a.mu.Lock()
	window := recentContextText(a.history.Messages(), 6)
	a.mu.Unlock()
	terms := keywordSet(window)
	if len(terms) == 0 {
		return
	}
	candidates, err := searchMemory(window, 6)
	if err != nil {
		return
	}
	filtered, withheld, conflicts := filterMemoryInjectionConflicts(candidates, cfg, window)
	if len(withheld) > 0 && run != nil {
		run.addEvent("memory_conflict", fmt.Sprintf("Withheld %d conflicting memory reinjection candidate%s", len(withheld), plural(len(withheld), "", "s")), strings.Join(conflicts, "\n"))
	}
	var fresh []MemoryEntry
	for _, entry := range filtered {
		if injected[entry.ID] || !memoryHasTermHit(entry, terms) {
			continue
		}
		fresh = append(fresh, entry)
		if len(fresh) >= 3 {
			break
		}
	}
	if len(fresh) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("Additional relevant project memory (surfaced as the task evolved):\n")
	for _, entry := range fresh {
		injected[entry.ID] = true
		sb.WriteString("- ")
		if entry.Title != "" {
			sb.WriteString(entry.Title + ": ")
		}
		sb.WriteString(strings.TrimSpace(entry.Content))
		if entry.Kind != "" && entry.Kind != "note" {
			sb.WriteString(" [kind=" + entry.Kind + "]")
		}
		sb.WriteString("\n")
	}
	note := strings.TrimRight(sb.String(), "\n")
	a.mu.Lock()
	a.history.Append(llm.NewTextMessage(llm.RoleSystem, note))
	a.mu.Unlock()
	*count++
	run.addEvent("memory_reinject", fmt.Sprintf("Re-injected %d memory entr%s", len(fresh), plural(len(fresh), "y", "ies")), note)
}

// recentContextText concatenates the text of the last n non-system messages,
// capped, to use as a relevance query for memory re-injection.
func recentContextText(msgs []llm.Message, n int) string {
	if len(msgs) == 0 || n <= 0 {
		return ""
	}
	start := len(msgs) - n
	if start < 0 {
		start = 0
	}
	var parts []string
	for _, m := range msgs[start:] {
		if m.Role == llm.RoleSystem {
			continue
		}
		if text := strings.TrimSpace(messageText(m)); text != "" {
			parts = append(parts, text)
		}
	}
	out := strings.Join(parts, "\n")
	const maxWindow = 4000
	if len(out) > maxWindow {
		out = out[len(out)-maxWindow:]
	}
	return out
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
		a.emit("mauler:agent_mode", mode)
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
		a.emit("mauler:agent_mode", mode)
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

// InterruptShellTool interrupts only the current shared-terminal tool command.
// Unlike StopAgent, this leaves the agent run alive so the model can receive a
// recoverable shell-tool error and decide what to do next.
func (a *App) InterruptShellTool() {
	a.shellMu.Lock()
	sess := a.shellSess
	a.shellMu.Unlock()
	if sess == nil {
		return
	}
	select {
	case sess.interrupt <- struct{}{}:
	default:
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
		a.emit("mauler:workspace_changed", filepath.ToSlash(abs))
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
		a.emit("mauler:workspace_folders_changed", cfg.Context.OpenFolders)
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
		a.emit("mauler:workspace_folders_changed", cfg.Context.OpenFolders)
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

func (a *App) UpdateLabContext(target, vpnInterface, latestArtifact, opsProfile string) (LabStatus, error) {
	a.mu.Lock()
	a.cfg.Context.Lab.Target = strings.TrimSpace(target)
	a.cfg.Context.Lab.VPNInterface = strings.TrimSpace(vpnInterface)
	a.cfg.Context.Lab.LatestArtifact = filepath.ToSlash(strings.TrimSpace(latestArtifact))
	a.cfg.Context.Lab.OpsProfile = normaliseOpsProfile(opsProfile)
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
		OpsProfile:     normaliseOpsProfile(cfg.Context.Lab.OpsProfile),
		OpenFolders:    normaliseAppWorkspaceFolders(cfg.Context.OpenFolders, filepath.ToSlash(wd)),
	}
}

func normaliseOpsProfile(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "htb", "ctf":
		return "htb"
	default:
		return "pentesting"
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
		a.emit("mauler:project_instructions_changed", normalized)
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
	a.recordLedger(ledger.Event{
		Kind:   "artifact_start",
		Source: "artifact",
		Status: "running",
		Input:  fmt.Sprintf("lang=%s\ncode_chars=%d", strings.TrimSpace(lang), len(code)),
	})

	go func() {
		status := "done"
		detail := ""
		defer func() {
			a.mu.Lock()
			a.artifactRunning = false
			a.cancelArtifact = nil
			a.mu.Unlock()
			if ctx.Err() != nil {
				status = "cancelled"
				detail = ctx.Err().Error()
			}
			a.recordLedger(ledger.Event{
				Kind:    "artifact_done",
				Source:  "artifact",
				Status:  status,
				Detail:  detail,
				Message: strings.TrimSpace(lang),
			})
			a.emit("mauler:artifact_done")
		}()

		a.emit("mauler:artifact_output", "")
		cmd, err := artifactCommand(ctx, lang, code)
		if err != nil {
			status = "error"
			detail = err.Error()
			a.recordLedger(ledger.Event{
				Kind:   "artifact_error",
				Source: "artifact",
				Status: "error",
				Error:  err.Error(),
			})
			a.emit("mauler:artifact_output", err.Error()+"\n")
			return
		}
		writer := &artifactWriter{ctx: a.ctx, app: a}
		cmd.Stdout = writer
		cmd.Stderr = writer
		if err := cmd.Run(); err != nil && ctx.Err() == nil {
			status = "error"
			detail = err.Error()
			a.recordLedger(ledger.Event{
				Kind:   "artifact_error",
				Source: "artifact",
				Status: "error",
				Error:  err.Error(),
			})
			a.emit("mauler:artifact_output", fmt.Sprintf("\n%s\n", err))
		}
	}()

	return nil
}

// StopArtifact cancels the running artifact process.
func (a *App) StopArtifact() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelArtifact != nil {
		a.recordLedger(ledger.Event{
			Kind:    "artifact_stop",
			Source:  "artifact",
			Status:  "requested",
			Message: "user requested artifact stop",
		})
		a.cancelArtifact()
	}
}

type artifactWriter struct {
	ctx context.Context
	app *App
}

func (w *artifactWriter) Write(p []byte) (int, error) {
	wailsruntime.EventsEmit(w.ctx, "mauler:artifact_output", string(p))
	if w.app != nil && len(p) > 0 {
		w.app.recordLedger(ledger.Event{
			Kind:   "artifact_output",
			Source: "artifact",
			Output: string(p),
		})
	}
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
	models, err := client.Models(ctx)
	status := "ok"
	detail := fmt.Sprintf("models=%d", len(models))
	if err != nil {
		status = "error"
		detail = err.Error()
	}
	a.recordLedger(ledger.Event{
		Kind:    "provider_models",
		Source:  "provider",
		Status:  status,
		Message: profile.Backend,
		Detail:  detail,
		Metadata: map[string]string{
			"base_url": profile.BaseURL,
			"model":    profile.ModelID,
		},
	})
	return models, err
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
		result := fmt.Sprintf("unreachable: %v", err)
		a.recordLedger(ledger.Event{
			Kind:     "provider_ping",
			Source:   "provider",
			Status:   "unreachable",
			Message:  provider.Backend,
			Detail:   result,
			Metadata: map[string]string{"base_url": provider.BaseURL},
		})
		return result
	}
	a.recordLedger(ledger.Event{
		Kind:     "provider_ping",
		Source:   "provider",
		Status:   "ok",
		Message:  provider.Backend,
		Metadata: map[string]string{"base_url": provider.BaseURL},
	})
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
	models, err := client.Models(ctx)
	status := "ok"
	detail := fmt.Sprintf("models=%d", len(models))
	if err != nil {
		status = "error"
		detail = err.Error()
	}
	a.recordLedger(ledger.Event{
		Kind:     "provider_models",
		Source:   "provider",
		Status:   status,
		Message:  provider.Backend,
		Detail:   detail,
		Metadata: map[string]string{"base_url": provider.BaseURL},
	})
	return models, err
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

func (a *App) runAgentLoop(ctx context.Context, firstMsg llm.Message, profile settings.Profile, cfg *settings.Settings, autonomous bool, mode AgentMode, memories []MemoryEntry, skills []Skill, run TaskRun) (finalRun TaskRun) {
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
			a.setRunState(&run, finalStoppedRunState(run.StopReason), run.StopDetail)
		}
		if finalStatus == "stopped" && strings.TrimSpace(finalSummary) == "" {
			finalSummary = fallbackStoppedRunSummary(run)
			if strings.TrimSpace(run.Response) == "" {
				run.Response = finalSummary
			}
			if strings.TrimSpace(finalSummary) != "" && a.ctx != nil {
				a.emit("mauler:delta", finalSummary)
			}
		}
		run.addEvent("loop_metrics", "Loop-health metrics", buildLoopMetrics(run).Detail())
		run.finish(finalStatus, finalSummary)
		if ctx.Err() == nil {
			a.deleteRunCheckpoint(run.ID)
		}
		_ = a.saveTaskRun(run, loggingConfigValue(&cfg.Logging))
		if cfg.Memory.Enabled {
			_ = a.saveRunMilestoneMemory(&run)
		}
		a.autoDistillLearnings(&run, cfg.Memory)
		tools.ReapBackgroundShellJobs() // free finished-but-unpolled standalone jobs
		if a.ctx != nil {
			a.emit("mauler:task_run", run)
			if suggestion := buildLearningSuggestion(&run); suggestion != nil {
				a.recordLedger(ledger.Event{
					RunID:   run.ID,
					Kind:    "learning_suggestion",
					Source:  "skills",
					Status:  suggestion.Type,
					Message: suggestion.Title,
					Detail:  suggestion.Reason,
					Output:  suggestion.Template,
				})
				a.emit("mauler:suggest_learning", suggestion)
			}
		}
		a.mu.Lock()
		a.agentRunning = false
		a.cancelAgent = nil
		a.stopReason = ""
		a.stopDetail = ""
		a.mu.Unlock()
		a.emit("mauler:stream_done")
		a.autoSave()
		finalRun = run
	}()

	// Append system prompt on first turn
	a.mu.Lock()
	if a.history.TokenCount() == 0 {
		a.history.Append(llm.NewTextMessage(llm.RoleSystem, buildSystemPrompt(*cfg, mode, memories, skills)))
	}
	if firstMsg.Role != "" {
		a.history.Append(firstMsg)
	}
	a.mu.Unlock()

	a.emit("mauler:stream_start")

	client, err := buildClientForAgent(profile)
	if err != nil {
		finalStatus = "error"
		finalSummary = err.Error()
		run.stopTerminal("client_error", err.Error())
		run.addEvent("error", "Client setup failed", err.Error())
		a.emit("mauler:stream_error", err.Error())
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
		a.emit("mauler:stream_error", err.Error())
		return
	}
	run.addEvent("model", "Model ready", modelLoadKey(profile))
	stopKeepalive := a.startShellKeepalive(cfg)
	defer stopKeepalive()
	if err := saveRuntimeLockSnapshot(profile); err != nil {
		run.addEvent("runtime_lock", "Could not save runtime lock", err.Error())
	} else {
		lock := buildRuntimeLock(profile)
		if data, err := json.Marshal(lock); err == nil {
			run.addEvent("runtime_lock", "Saved runtime lock", string(data))
		}
	}
	if applyWorkingContextBudget(a, mode.ContextBudget, profile.CtxTokens) {
		effectiveBudget := effectiveWorkingContextBudget(mode.ContextBudget, profile.CtxTokens)
		run.addEvent("context_budget", "Applied agent working context budget", fmt.Sprintf("budget=%d preset=%d profile_ctx=%d", effectiveBudget, mode.ContextBudget, profile.CtxTokens))
	}
	firstUserText := initialRunPromptText(firstMsg, run) // for toolChoiceFor heuristic and research budgets
	budget := newTaskBudget(cfg.Tools, firstUserText)
	startedAt := time.Now()
	autoContinues := 0
	noToolContinues := 0           // consecutive auto-continues where the model made zero tool calls
	malformedToolContinues := 0    // consecutive retries caused by unparsed inline tool markup
	totalToolCallsMade := 0        // cumulative tool calls across all turns this task
	preOutputInferenceRetries := 0 // bounded retries for backend failures before any model output
	escalationsUsed := 0
	currentEffort := normaliseReasoningEffort(mode.DefaultEffort)
	if currentEffort == "" {
		currentEffort = defaultReasoningEffortForMode(mode)
	}
	reasoningEffortChanges := 0
	run.addEvent("reasoning_effort", "Initial reasoning effort", currentEffort)
	toolBudgetSummaryRequested := false
	timeBudgetSummaryRequested := false
	docRecoveryRequested := false
	docRecoveryPromptSent := false
	backendUsagePressure := false
	recoveryReportRequested := false
	// Memory injected up front is frozen against the first prompt; as a long run
	// drifts, re-score against recent context and surface newly-relevant entries.
	// Seed with the up-front IDs so we never re-inject what is already in context.
	injectedMemoryIDs := map[string]bool{}
	for _, m := range memories {
		injectedMemoryIDs[m.ID] = true
	}
	memoryReinjections := 0
	persistNudgeSent := false // one-time reminder to save evidence before context is dropped
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
		timeBudgetExhausted := agentTimeBudgetExhausted(cfg.Agents, startedAt, time.Now())
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
		if timeBudgetExhausted {
			toolDefs = nil
			toolChoice = "none"
			if !timeBudgetSummaryRequested {
				timeBudgetSummaryRequested = true
				prompt := agentTimeBudgetSummaryPrompt(cfg.Agents.MaxRunSeconds)
				run.addEvent("continue", "Run time budget exhausted; requesting final text-only summary", prompt)
				a.setRunState(&run, "blocked", fmt.Sprintf("time budget exhausted (%d seconds)", cfg.Agents.MaxRunSeconds))
				a.mu.Lock()
				a.history.Append(llm.NewTextMessage(llm.RoleUser, prompt))
				a.mu.Unlock()
			}
		}
		if docRecoveryRequested && requiresLivingDocUpdate(run.Prompt) && !runHasFileMutation(run) && !toolBudgetExhausted && !timeBudgetExhausted {
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
		if shouldRequestRecoveryReport(run, recoveryReportRequested) {
			recoveryReportRequested = true
			toolDefs = nil
			toolChoice = "none"
			prompt := recoveryReportPrompt(run)
			run.addEvent("recovery", "Requesting final no-tool recovery report", prompt)
			a.setRunState(&run, "recovering", "Generating final recovery report without more tool calls.")
			a.mu.Lock()
			a.history.Append(llm.NewTextMessage(llm.RoleUser, prompt))
			a.mu.Unlock()
		} else if recoveryReportRequested {
			toolDefs = nil
			toolChoice = "none"
		}
		if totalToolCallsMade > 0 {
			a.maybeReinjectMemory(&run, *cfg, injectedMemoryIDs, &memoryReinjections)
		}

		a.mu.Lock()
		needsCompact := a.history.NeedsCompactionWithReserve(cfg.Context.CompactionAt, compactionReserveTokens(toolDefs))
		a.mu.Unlock()
		contextDropped := false
		if backendUsagePressure {
			backendUsagePressure = false
			a.mu.Lock()
			cleared := a.history.ClearOldToolResults(2)
			needsCompact = a.history.NeedsCompactionWithReserve(contextPressureCompactionThreshold(cfg.Context.CompactionAt), compactionReserveTokens(toolDefs))
			a.mu.Unlock()
			if cleared.Cleared > 0 {
				contextDropped = true
				run.addEvent("context_clear", "Cleared stale tool results after backend token pressure", fmt.Sprintf("cleared=%d\nbefore_tokens=%d\nafter_tokens=%d", cleared.Cleared, cleared.BeforeTokens, cleared.AfterTokens))
			}
			if needsCompact {
				if compacted := a.doCompact(ctx, client, profile); compacted != nil {
					contextDropped = true
					run.addEvent("compaction", compacted.Message(), compacted.Detail())
				}
			}
		}
		if needsCompact {
			a.mu.Lock()
			cleared := a.history.ClearOldToolResults(4)
			needsCompact = a.history.NeedsCompactionWithReserve(cfg.Context.CompactionAt, compactionReserveTokens(toolDefs))
			a.mu.Unlock()
			if cleared.Cleared > 0 {
				contextDropped = true
				run.addEvent("context_clear", "Cleared stale tool results", fmt.Sprintf("cleared=%d\nbefore_estimated_tokens=%d\nafter_estimated_tokens=%d", cleared.Cleared, cleared.BeforeTokens, cleared.AfterTokens))
			}
			if needsCompact {
				if compacted := a.doCompact(ctx, client, profile); compacted != nil {
					contextDropped = true
					run.addEvent("compaction", compacted.Message(), compacted.Detail())
				}
			}
		}
		// The first time context is dropped, remind the model to persist anything it
		// is accumulating. Older tool outputs (extracted hashes, creds, found paths)
		// age out as the context fills; without this the model re-fetches what it had.
		if contextDropped && !persistNudgeSent {
			persistNudgeSent = true
			a.mu.Lock()
			a.history.Append(llm.NewTextMessage(llm.RoleSystem, "Context is filling and older tool results are being dropped to make room. Persist anything you are accumulating NOW — extracted hashes/credentials, discovered paths/endpoints, partial findings — by writing them to a file (write_file) or saving them with the memory tool. Do not rely on earlier tool output staying in context, and do not re-run commands whose results you already captured."))
			a.mu.Unlock()
			run.addEvent("persist_nudge", "Reminded model to persist findings before context loss", "")
		}
		if contextDropped {
			a.appendGoalReminder(&run)
		}

		a.mu.Lock()
		msgs := a.history.Messages()
		a.mu.Unlock()
		if compacted := a.ensureRequestContextRoom(ctx, client, profile, cfg, toolDefs, &run); compacted != nil {
			run.addEvent("compaction", compacted.Message(), compacted.Detail())
			a.appendGoalReminder(&run)
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
		req := buildChatRequest(profile, msgs, toolDefs, toolChoice, forceNoThink, shouldUseCodingParams(firstUserText, mode), currentEffort)
		a.setRunState(&run, "thinking", fmt.Sprintf("tool_choice=%s messages=%d tools=%d no_think=%v effort=%s", toolChoice, len(msgs), len(toolDefs), forceNoThink, currentEffort))
		if len(toolDefs) > 0 {
			run.addEvent("tool_protocol_request", "Chat request tool protocol", fmt.Sprintf("client=%s model=%s tool_choice=%s tools=%d thinking=%v preserve_thinking=%v force_no_think=%v reasoning_effort=%s max_tokens=%d", client.Name(), profile.ModelID, toolChoice, len(toolDefs), req.EnableThinking, req.PreserveThinking, forceNoThink, req.ReasoningEffort, req.MaxTokens))
		}
		a.recordBackendRuntimeMismatch(ctx, client, profile, &run)

		ch, err := client.Chat(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if preOutputInferenceRetries < maxPreOutputInferenceRetries && isRecoverableInferenceFailure(err.Error()) {
				preOutputInferenceRetries++
				run.addEvent("inference_retry", fmt.Sprintf("Retrying chat request after pre-output backend failure (%d/%d)", preOutputInferenceRetries, maxPreOutputInferenceRetries), err.Error())
				if !sleepBeforeInferenceRetry(ctx, preOutputInferenceRetries) {
					return
				}
				continue agentLoop
			}
			finalStatus = "error"
			finalSummary = err.Error()
			run.stopTerminal("chat_error", err.Error())
			run.addEvent("error", "Chat request failed", err.Error())
			a.emit("mauler:stream_error", err.Error())
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
				if preOutputInferenceRetries < maxPreOutputInferenceRetries && rawTextBuf.Len() == 0 && thinkBuf.Len() == 0 && len(toolCalls) == 0 && isRecoverableInferenceFailure(delta.Error.Error()) {
					preOutputInferenceRetries++
					run.addEvent("inference_retry", fmt.Sprintf("Retrying stream after pre-output backend failure (%d/%d)", preOutputInferenceRetries, maxPreOutputInferenceRetries), delta.Error.Error())
					if !sleepBeforeInferenceRetry(ctx, preOutputInferenceRetries) {
						return
					}
					continue agentLoop
				}
				finalStatus = "error"
				finalSummary = delta.Error.Error()
				run.stopTerminal("stream_error", delta.Error.Error())
				run.addEvent("error", "Stream failed", delta.Error.Error())
				a.emit("mauler:stream_error", delta.Error.Error())
				return
			}
			if delta.Truncated {
				wasTruncated = true
				run.addEvent("truncated", "Model hit token limit", "finish_reason=length")
			}
			if delta.Thinking != "" && req.EnableThinking {
				thinkBuf.WriteString(delta.Thinking)
				a.emit("mauler:thinking", delta.Thinking)
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
						a.emit("mauler:delta", chunk)
					}
				default:
					emittedVisibleText = visible
					a.emit("mauler:stream_replace", visible)
				}
			}
			if len(delta.ToolCalls) > 0 {
				toolCalls = append(toolCalls, delta.ToolCalls...)
			}
			if delta.Usage != nil {
				usage = delta.Usage
			}
		}

		// Stability guard: while MTP/speculative decoding is on, watch for the
		// speculative-rejection-at-</think> signature — a turn that truncates with
		// thinking present but produces no usable answer or tool call. A clean turn
		// resets the streak; repeats auto-fall back to a stable decode (see
		// noteSpecTurn). Cheap and off the lock.
		if strings.TrimSpace(profile.SpecType) != "" {
			suspect := wasTruncated && req.EnableThinking && thinkBuf.Len() > 0 &&
				len(toolCalls) == 0 && len(strings.TrimSpace(emittedVisibleText)) < 24
			a.noteSpecTurn(cfg.ActiveProfile, suspect)
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
			if len(repairDefs) == 0 && containsInlineToolMarkup(repairText) && toolChoice != "none" && !toolBudgetExhausted && !timeBudgetExhausted {
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
				a.emit("mauler:tool_protocol_repair")
				a.emit("mauler:stream_replace", textBuf.String())
			}
		}
		if toolBudgetExhausted && len(toolCalls) > 0 {
			run.addEvent("blocked", "Ignored tool calls after agent tool budget was exhausted", toolProtocolDebugDetail(rawText, textBuf.String(), toolCalls, nil))
			toolCalls = nil
		}
		if timeBudgetExhausted && len(toolCalls) > 0 {
			run.addEvent("blocked", "Ignored tool calls after agent time budget was exhausted", toolProtocolDebugDetail(rawText, textBuf.String(), toolCalls, nil))
			toolCalls = nil
		}
		if recoveryReportRequested && len(toolCalls) > 0 {
			run.addEvent("blocked", "Ignored tool calls during recovery report", toolProtocolDebugDetail(rawText, textBuf.String(), toolCalls, nil))
			toolCalls = nil
			if strings.TrimSpace(textBuf.String()) == "" {
				textBuf.WriteString(fallbackStoppedRunSummary(run))
			}
		}
		unrepairedToolMarkup := len(toolCalls) == 0 && containsInlineToolMarkup(repairText) && !toolBudgetExhausted && !timeBudgetExhausted && toolChoice != "none"
		if unrepairedToolMarkup {
			a.setRunState(&run, "recovering", "Model emitted tool markup text that could not be converted.")
			run.addEvent("tool_protocol_unrepaired", "Could not convert inline tool markup", toolProtocolDebugDetail(rawText, textBuf.String(), nil, toolDefs))
			a.emit("mauler:stream_replace", "")
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
		if thinkBuf.Len() > 0 && req.EnableThinking {
			a.emit("mauler:thinking_done", thinkBuf.String())
		}
		if usage != nil {
			run.setTokens(usage.PromptTokens, usage.CompletionTokens)
			if backendPromptTokensNeedCompaction(usage.PromptTokens, a.contextWindow, cfg.Context.CompactionAt) {
				backendUsagePressure = true
				run.addEvent("context_pressure", "Backend prompt usage exceeded compaction threshold", fmt.Sprintf("prompt_tokens=%d\ncontext_window=%d\nthreshold=%.2f", usage.PromptTokens, a.contextWindow, cfg.Context.CompactionAt))
			}
			a.emit("mauler:usage", map[string]int{
				"prompt_tokens":     usage.PromptTokens,
				"completion_tokens": usage.CompletionTokens,
			})
		}

		if len(toolCalls) == 0 {
			text := strings.TrimSpace(textBuf.String())

			if recoveryReportRequested {
				finalStatus = "stopped"
				if text == "" {
					finalSummary = fallbackStoppedRunSummary(run)
					if strings.TrimSpace(run.Response) == "" {
						run.Response = finalSummary
					}
				}
				return
			}
			if timeBudgetExhausted {
				finalStatus = "stopped"
				detail := fmt.Sprintf("Agent wall-clock time budget was exhausted after %d seconds.", cfg.Agents.MaxRunSeconds)
				run.stop("time_budget_exhausted", detail)
				run.addEvent("stop", "Time budget exhausted", detail)
				if text == "" {
					finalSummary = fallbackStoppedRunSummary(run)
					if strings.TrimSpace(run.Response) == "" {
						run.Response = finalSummary
					}
				}
				return
			}

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
				detail := fmt.Sprintf("The model emitted inline tool markup that TheMauler could not convert after %d repair retries. Try again with a stricter tool-compatible profile/template, or inspect the Logs tab for the raw markup.", malformedToolContinues)
				if attempt, ok := a.tryEscalation(ctx, cfg, profile, &run, "tool_protocol_unrepaired", detail, toolDefs, shouldUseCodingParams(firstUserText, mode), &escalationsUsed); ok {
					malformedToolContinues = 0
					autoContinues = 0
					if len(attempt.ToolCalls) > 0 {
						toolCalls = attempt.ToolCalls
						textBuf.Reset()
						textBuf.WriteString(attempt.Text)
						goto processToolCalls
					}
					continue
				}
				finalStatus = "stopped"
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
				} else if needsOperationalTool(firstUserText) {
					prompt = "You produced no visible output and made no tool call. " +
						"This is an authorised Ops/HTB target workflow, not a repository-inspection task. " +
						"Continue from the latest live target evidence. If more evidence is needed, make one targeted shell tool call now (curl, nmap, grep/tail a saved artifact, or a Mauler background-job poll using the job field). " +
						"If the latest evidence already shows a block such as HTTP 403 or repeated tool failure, stop tool use and summarize what is known plus the exact next input needed."
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
				if wasTruncated {
					detail := "The model still appeared truncated after the auto-continue limit."
					if attempt, ok := a.tryEscalation(ctx, cfg, profile, &run, "auto_continue_exhausted", detail, toolDefs, shouldUseCodingParams(firstUserText, mode), &escalationsUsed); ok {
						autoContinues = 0
						noToolContinues = 0
						if len(attempt.ToolCalls) > 0 {
							toolCalls = attempt.ToolCalls
							textBuf.Reset()
							textBuf.WriteString(attempt.Text)
							goto processToolCalls
						}
						continue
					}
				}
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
	processToolCalls:
		noToolContinues = 0
		malformedToolContinues = 0
		totalToolCallsMade += countBudgetedToolCalls(toolCalls)

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
			if isReasoningEffortTool(tc.Function.Name) {
				run.addEvent("tool_call", tc.Function.Name, logInput(string(tc.Function.Arguments)))
				a.emit("mauler:tool_call", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "input": string(tc.Function.Arguments),
				})
				result, ok := applyReasoningEffortTool(&currentEffort, &reasoningEffortChanges, tc.Function.Arguments)
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, result))
				status := "done"
				if !ok {
					status = "blocked"
				}
				run.addEvent("reasoning_effort", "Reasoning effort tool call", fmt.Sprintf("status=%s result=%s", status, result))
				a.emit("mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": result,
				})
				continue
			}
			if cfg.Agents.MaxToolCalls > 0 && len(run.Tools) >= cfg.Agents.MaxToolCalls {
				result := fmt.Sprintf("agent tool-call budget exhausted (%d calls). Stop tools now, summarize progress, and ask before continuing.", cfg.Agents.MaxToolCalls)
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, result))
				run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(result), "blocked", 0)
				run.stop("tool_budget_exhausted", result)
				a.setRunState(&run, "blocked", result)
				run.addEvent("blocked", "Agent tool-call budget exhausted", result)
				a.emit("mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": result,
				})
				continue
			}
			run.addEvent("tool_call", tc.Function.Name, logInput(string(tc.Function.Arguments)))
			toolCallPayload := map[string]string{
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": string(tc.Function.Arguments),
			}
			if timeout := resolvedToolTimeout(cfg.Tools, tc); timeout > 0 {
				toolCallPayload["timeout"] = strconv.Itoa(timeout)
			}
			a.emit("mauler:tool_call", toolCallPayload)

			tool, isKnown := a.registry.Get(tc.Function.Name)
			if !cfg.Tools.Enabled || !toolEnabled(settings.EffectiveEnabledTools(cfg.Tools), tc.Function.Name) {
				decision := evaluateDisabledToolRecoveryPolicy(run, cfg.Tools, tc)
				result := decision.Message
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, result))
				run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(result), decision.ToolStatus, 0)
				if decision.HardStop {
					run.stop(decision.StopReason, result)
					a.setRunState(&run, decision.RunState, result)
					run.addEvent("blocked", decision.EventMessage, result)
				} else {
					a.setRunState(&run, decision.RunState, result)
					run.addEvent("tool_error", decision.EventMessage, result)
				}
				a.emit("mauler:tool_result", map[string]string{
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
					a.emit("mauler:tool_result", map[string]string{
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
				a.emit("mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": blocked,
				})
				continue
			}
			if decision := evaluateSkipRecoveryPolicy(run, tc); decision.Message != "" {
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, decision.Message))
				run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(decision.Message), decision.ToolStatus, 0)
				a.setRunState(&run, decision.RunState, decision.Message)
				run.addEvent("tool_skip", decision.EventMessage, decision.Message)
				a.emit("mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": decision.Message,
				})
				continue
			}
			if decision := evaluatePreToolRecoveryPolicy(run, tc); decision.Message != "" {
				toolResultMsgs = append(toolResultMsgs, newToolResultMsg(tc.ID, tc.Function.Name, decision.Message))
				run.addTool(tc.Function.Name, logInput(string(tc.Function.Arguments)), logResult(decision.Message), decision.ToolStatus, 0)
				if decision.HardStop {
					run.stop(decision.StopReason, decision.Message)
				}
				a.setRunState(&run, decision.RunState, decision.Message)
				eventKind := "tool_skip"
				if decision.HardStop {
					eventKind = "blocked"
				}
				run.addEvent(eventKind, decision.EventMessage, decision.Message)
				a.emit("mauler:tool_result", map[string]string{
					"id": tc.ID, "name": tc.Function.Name, "result": decision.Message,
				})
				continue
			}

			var beforeMutation fileChangeSnapshot
			if isKnown && isWriteTool(tc.Function.Name) {
				beforeMutation = snapshotToolTarget(tc)
				if snapPath := extractPath(tc); snapPath != "" {
					snapPath = tools.NormalizeHostPath(snapPath)
					_ = a.rollback.Push(agent.OpWrite, snapPath)
				}
			}

			toolStart := time.Now()
			var result string
			var runErr error
			if shouldUseSharedTerminal(cfg.Tools, tc.Function.Name) {
				result, runErr = a.runSharedTerminalShell(ctx, tc.Function.Name, tc.Function.Arguments, cfg.Tools.BashTimeout)
				if errors.Is(runErr, errSharedTerminalUnsupported) {
					result, runErr = a.registry.Run(ctx, tc)
				}
			} else {
				result, runErr = a.registry.Run(ctx, tc)
			}
			toolDurMs := time.Since(toolStart).Milliseconds()
			if isShellTool(tc.Function.Name) && runErr != nil {
				if adjusted, ok := recoverBenignShellPipelineClose(tc, result, runErr); ok {
					result = adjusted
					runErr = nil
				}
			}
			if runErr != nil {
				result = toolErrorResult(result, runErr)
				if isMalformedToolArgsError(runErr) {
					result += "\nRecovery: the tool arguments were malformed JSON, usually because the model emitted an incomplete function call. Retry the same tool with complete valid JSON arguments only."
				}
			} else if isWriteTool(tc.Function.Name) {
				if verification := verifyMutationResult(tc); verification != "" {
					result = result + "\n" + verification
					a.recordFileChange(run.ID, tc, beforeMutation, verification, toolDurMs)
				}
			}
			if isShellTool(tc.Function.Name) {
				result = appendShellRecoveryHints(result)
				if runErr == nil && isEmptyShellOutputResult(result) {
					result = strings.TrimRight(result, "\r\n") + "\n[empty shell output: command exited successfully but produced no stdout/stderr. Do not repeat the same command; change flags/path or summarize the empty response.]"
				} else if runErr == nil {
					if hint := shellCommandStormHint(run, tc); hint != "" {
						result = strings.TrimRight(result, "\r\n") + hint
						run.addEvent("command_storm", "Nudged model to script a repeated command family", hint)
					}
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
			a.recordCategorizedToolLedger(run.ID, tc, status, result, toolDurMs)
			a.emit("mauler:tool_result", map[string]string{
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
		a.maybeCheckpoint(run, *cfg, 4)
	}
}

// awaitConfirm blocks until the user responds or context is cancelled.
func (a *App) awaitConfirm(ctx context.Context, tc llm.ToolCallDef) bool {
	ch := make(chan bool, 1)
	a.mu.Lock()
	a.confirmCh = ch
	a.mu.Unlock()

	a.emit("mauler:confirm", map[string]string{
		"id":    tc.ID,
		"name":  tc.Function.Name,
		"input": string(tc.Function.Arguments),
	})
	a.recordLedger(ledger.Event{
		Kind:    "confirmation_request",
		Source:  "confirm",
		Tool:    tc.Function.Name,
		Status:  "pending",
		Message: tc.ID,
		Input:   string(tc.Function.Arguments),
	})

	defer func() {
		a.mu.Lock()
		a.confirmCh = nil
		a.mu.Unlock()
	}()

	select {
	case allow := <-ch:
		status := "denied"
		if allow {
			status = "allowed"
		}
		a.recordLedger(ledger.Event{
			Kind:    "confirmation_response",
			Source:  "confirm",
			Tool:    tc.Function.Name,
			Status:  status,
			Message: tc.ID,
			Input:   string(tc.Function.Arguments),
		})
		return allow
	case <-ctx.Done():
		a.recordLedger(ledger.Event{
			Kind:    "confirmation_response",
			Source:  "confirm",
			Tool:    tc.Function.Name,
			Status:  "cancelled",
			Message: tc.ID,
			Input:   string(tc.Function.Arguments),
			Detail:  ctx.Err().Error(),
		})
		return false
	}
}

func finalStoppedRunState(reason string) string {
	switch strings.TrimSpace(reason) {
	case "user_stopped", "context_canceled", "cancelled", "canceled":
		return "stopped"
	default:
		return "blocked"
	}
}

func fallbackStoppedRunSummary(run TaskRun) string {
	reason := firstNonEmpty(run.StopReason, "stopped")
	detail := strings.TrimSpace(run.StopDetail)
	if detail == "" {
		for i := len(run.Events) - 1; i >= 0; i-- {
			event := run.Events[i]
			if strings.TrimSpace(event.Detail) != "" {
				detail = event.Detail
				break
			}
			if strings.TrimSpace(event.Message) != "" {
				detail = event.Message
				break
			}
		}
	}

	var lastTool *TaskToolEvent
	if len(run.Tools) > 0 {
		lastTool = &run.Tools[len(run.Tools)-1]
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Run stopped: %s", strings.ReplaceAll(reason, "_", " "))
	if detail != "" {
		fmt.Fprintf(&sb, "\n\nLatest blocker: %s", truncateLine(detail, 900))
	}
	if lastTool != nil {
		fmt.Fprintf(&sb, "\n\nLast tool: %s", lastTool.Name)
		if strings.TrimSpace(lastTool.Status) != "" {
			fmt.Fprintf(&sb, " (%s)", lastTool.Status)
		}
		if strings.TrimSpace(lastTool.Input) != "" {
			fmt.Fprintf(&sb, "\nInput: %s", truncateLine(lastTool.Input, 500))
		}
		if strings.TrimSpace(lastTool.Result) != "" {
			fmt.Fprintf(&sb, "\nResult: %s", truncateLine(lastTool.Result, 500))
		}
	}
	sb.WriteString("\n\nNo final assistant message was produced before the run stopped, so this summary was generated from the run state.")
	return strings.TrimSpace(sb.String())
}

func shouldRequestRecoveryReport(run TaskRun, alreadyRequested bool) bool {
	if alreadyRequested {
		return false
	}
	reason := strings.TrimSpace(run.StopReason)
	if reason == "" || !isBlockingStopReason(reason) {
		return false
	}
	switch reason {
	case "user_stopped", "context_canceled", "cancelled", "canceled", "tool_budget_exhausted":
		return false
	default:
		return true
	}
}

func recoveryReportPrompt(run TaskRun) string {
	var sb strings.Builder
	sb.WriteString("Recovery mode. Do not call tools. The run hit a blocking tool/problem state, and you have one final text-only turn.\n\n")
	fmt.Fprintf(&sb, "Stop reason: %s\n", firstNonEmpty(run.StopReason, "unknown"))
	if detail := strings.TrimSpace(run.StopDetail); detail != "" {
		fmt.Fprintf(&sb, "Stop detail: %s\n", truncateLine(detail, 1200))
	}
	if len(run.Tools) > 0 {
		sb.WriteString("\nRecent tools:\n")
		start := len(run.Tools) - 5
		if start < 0 {
			start = 0
		}
		for _, tool := range run.Tools[start:] {
			fmt.Fprintf(&sb, "- %s [%s]", firstNonEmpty(tool.Name, "tool"), firstNonEmpty(tool.Status, "unknown"))
			if in := strings.TrimSpace(tool.Input); in != "" {
				fmt.Fprintf(&sb, "\n  input: %s", truncateLine(in, 450))
			}
			if out := strings.TrimSpace(tool.Result); out != "" {
				fmt.Fprintf(&sb, "\n  result: %s", truncateLine(out, 650))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\nWrite a concise recovery report with these headings only:\n")
	sb.WriteString("What failed\nEvidence gathered\nLikely cause\nSafest next action\n")
	sb.WriteString("\nDo not say you will run a command. Do not ask for another tool call. If the next step needs user approval/input, state the exact input needed.")
	return strings.TrimSpace(sb.String())
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
		a.emit("mauler:compact", summary)
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
		a.emit("mauler:compact", summary)
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
	// Stable per-workspace ID so each compaction OVERWRITES the previous session-state
	// memory instead of appending a new one. Otherwise long runs pile up dozens of
	// "Compacted session state" entries that dominate memory injection and crowd the
	// context window — a self-reinforcing pollution loop. Importance is kept modest so
	// real facts/decisions outrank it for the limited injection slots.
	_, _ = a.SaveMemoryEntry(MemoryEntry{
		ID:         "compaction-session-state-" + workspaceScope(),
		Scope:      workspaceScope(),
		Title:      "Compacted session state",
		Content:    summary,
		Tags:       []string{"compaction", "session-state"},
		Kind:       "decision",
		Importance: 2,
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

func backendPromptTokensNeedCompaction(promptTokens, contextWindow int, threshold float64) bool {
	if promptTokens <= 0 || contextWindow <= 0 {
		return false
	}
	if threshold <= 0 {
		threshold = 0.85
	}
	if threshold > 0.82 {
		threshold = 0.82
	}
	return float64(promptTokens) >= float64(contextWindow)*threshold
}

func contextPressureCompactionThreshold(threshold float64) float64 {
	if threshold <= 0 || threshold > 0.75 {
		return 0.75
	}
	return threshold
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
	a.emit("mauler:run_state", map[string]string{
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

func resolvedToolTimeout(cfg settings.ToolsConfig, tc llm.ToolCallDef) int {
	if !isShellTool(tc.Function.Name) {
		return 0
	}
	timeoutSecs := cfg.BashTimeout
	if timeoutSecs <= 0 {
		timeoutSecs = 120
	}
	var p struct {
		Timeout int `json:"timeout"`
	}
	if err := json.Unmarshal(tc.Function.Arguments, &p); err == nil && p.Timeout > 0 && p.Timeout <= 300 {
		timeoutSecs = p.Timeout
	}
	return timeoutSecs
}

type recoveryPolicyDecision struct {
	Message      string
	StopReason   string
	EventMessage string
	ToolStatus   string
	RunState     string
	HardStop     bool
}

type preToolRecoveryRule struct {
	stopReason string
	event      string
	soft       bool
	evaluate   func(TaskRun, llm.ToolCallDef) string
}

var preToolRecoveryRules = []preToolRecoveryRule{
	{
		stopReason: "repeated_tool_failure",
		event:      "Repeated shell command blocked",
		evaluate:   repeatedShellFailureBlock,
	},
	{
		stopReason: "repeated_empty_tool_output",
		event:      "Repeated empty shell output blocked",
		evaluate:   repeatedShellEmptyOutputBlock,
	},
	{
		stopReason: "repeated_same_tool_result",
		event:      "Repeated identical shell result blocked",
		soft:       true,
		evaluate:   repeatedShellSameResultBlock,
	},
	{
		stopReason: "repeated_identical_read",
		event:      "Repeated identical read tool blocked",
		soft:       true,
		evaluate:   repeatedIdenticalReadBlock,
	},
}

func evaluatePreToolRecoveryPolicy(run TaskRun, tc llm.ToolCallDef) recoveryPolicyDecision {
	for _, rule := range preToolRecoveryRules {
		if msg := rule.evaluate(run, tc); msg != "" {
			hardStop := !rule.soft || repeatedPreToolRecoveryIgnored(run, tc)
			status := "blocked"
			state := "blocked"
			event := rule.event
			stopReason := rule.stopReason
			if !hardStop {
				status = "skipped"
				state = "recovering"
				event = strings.Replace(rule.event, "blocked", "returned for recovery", 1)
				stopReason = ""
				msg = msg + "\nRecovery: this repeated command was skipped without stopping the run. Continue from the evidence already shown, choose a different verification/action, or write/update the relevant artifact."
			}
			return recoveryPolicyDecision{
				Message:      msg,
				StopReason:   stopReason,
				EventMessage: event,
				ToolStatus:   status,
				RunState:     state,
				HardStop:     hardStop,
			}
		}
	}
	return recoveryPolicyDecision{}
}

func repeatedPreToolRecoveryIgnored(run TaskRun, tc llm.ToolCallDef) bool {
	// Generic idempotent reads: escalate to a hard stop only after the model has
	// already been handed the duplicate-read nudge once and repeated the same call.
	if key := idempotentReadKey(tc.Function.Name, tc.Function.Arguments); key != "" {
		for i := len(run.Tools) - 1; i >= 0; i-- {
			tool := run.Tools[i]
			if idempotentReadKey(tool.Name, json.RawMessage(tool.Input)) != key {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(tool.Status), "skipped") && strings.Contains(tool.Result, "repeated command was skipped") {
				return true
			}
		}
		return false
	}
	if !isShellTool(tc.Function.Name) {
		return false
	}
	key := repeatShellCommandKey(shellCommandFromToolArgs(tc.Function.Arguments))
	if key == "" {
		return false
	}
	skips := 0
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if !isShellTool(tool.Name) {
			continue
		}
		if repeatShellCommandKey(shellCommandFromToolArgs(json.RawMessage(tool.Input))) != key {
			continue
		}
		if tool.Status == "skipped" && strings.Contains(tool.Result, "repeated command was skipped") {
			skips++
			if skips >= 2 {
				return true
			}
		}
		if tool.Status == "done" || tool.Status == "error" {
			continue
		}
	}
	return false
}

type skipRecoveryRule struct {
	status   string
	state    string
	event    string
	evaluate func(TaskRun, llm.ToolCallDef) string
}

var skipRecoveryRules = []skipRecoveryRule{
	{
		status:   "skipped",
		state:    "recovering",
		event:    "Duplicate fetch_url skipped",
		evaluate: duplicateFetchURLSkip,
	},
}

func evaluateSkipRecoveryPolicy(run TaskRun, tc llm.ToolCallDef) recoveryPolicyDecision {
	for _, rule := range skipRecoveryRules {
		if msg := rule.evaluate(run, tc); msg != "" {
			return recoveryPolicyDecision{
				Message:      msg,
				EventMessage: rule.event,
				ToolStatus:   firstNonEmpty(rule.status, "skipped"),
				RunState:     firstNonEmpty(rule.state, "recovering"),
			}
		}
	}
	return recoveryPolicyDecision{}
}

func evaluateDisabledToolRecoveryPolicy(run TaskRun, cfg settings.ToolsConfig, tc llm.ToolCallDef) recoveryPolicyDecision {
	msg := toolDisabledMessage(cfg, tc.Function.Name)
	decision := recoveryPolicyDecision{
		Message:      msg,
		EventMessage: "Disabled tool call returned to model for recovery",
		ToolStatus:   "disabled",
		RunState:     "recovering",
	}
	if !cfg.Enabled || shouldStopForDisabledTool(run, tc.Function.Name) {
		decision.StopReason = "tool_disabled"
		decision.EventMessage = "Tool disabled"
		decision.RunState = "blocked"
		decision.HardStop = true
	}
	return decision
}

func repeatedShellFailureBlock(run TaskRun, tc llm.ToolCallDef) string {
	if !isShellTool(tc.Function.Name) {
		return ""
	}
	command := shellCommandFromToolArgs(tc.Function.Arguments)
	key := repeatShellCommandKey(command)
	if key == "" {
		return ""
	}
	failures := 0
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if !isShellTool(tool.Name) {
			continue
		}
		if repeatShellCommandKey(shellCommandFromToolArgs(json.RawMessage(tool.Input))) != key {
			continue
		}
		if tool.Status == "error" || tool.Status == "blocked" {
			failures++
			if failures >= 2 {
				return fmt.Sprintf("Repeated shell command blocked after %d recent failures: %s\nRecovery: do not run the same command again. Replan from the latest output, narrow the command, use background=true with a job id for long scans, or ask the user before retrying.", failures, command)
			}
		}
	}
	return ""
}

func repeatedShellEmptyOutputBlock(run TaskRun, tc llm.ToolCallDef) string {
	if !isShellTool(tc.Function.Name) {
		return ""
	}
	command := shellCommandFromToolArgs(tc.Function.Arguments)
	key := repeatShellCommandKey(command)
	if key == "" {
		return ""
	}
	emptySuccesses := 0
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if !isShellTool(tool.Name) {
			continue
		}
		if repeatShellCommandKey(shellCommandFromToolArgs(json.RawMessage(tool.Input))) != key {
			continue
		}
		if tool.Status != "done" {
			continue
		}
		if !isEmptyShellOutputResult(tool.Result) {
			continue
		}
		emptySuccesses++
		if emptySuccesses >= 2 {
			return fmt.Sprintf("Repeated shell command blocked after %d empty successful results: %s\nRecovery: do not run the same command again. The command exits 0 but produces no useful stdout/stderr. Replan: try curl with -i -L -S and no -s, request a different path, inspect saved files, or summarize that the endpoint returned an empty body.", emptySuccesses, command)
		}
	}
	return ""
}

func repeatedShellSameResultBlock(run TaskRun, tc llm.ToolCallDef) string {
	if !isShellTool(tc.Function.Name) {
		return ""
	}
	command := shellCommandFromToolArgs(tc.Function.Arguments)
	key := repeatShellCommandKey(command)
	if key == "" {
		return ""
	}
	var lastResult string
	repeats := 0
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if !isShellTool(tool.Name) {
			continue
		}
		if repeatShellCommandKey(shellCommandFromToolArgs(json.RawMessage(tool.Input))) != key {
			continue
		}
		if tool.Status != "done" {
			continue
		}
		normalized := normalizedShellEvidence(tool.Result)
		if normalized == "" {
			continue
		}
		if lastResult == "" {
			lastResult = normalized
			repeats = 1
			continue
		}
		if normalized != lastResult {
			break
		}
		repeats++
		if repeats >= 2 {
			return fmt.Sprintf("Repeated shell command blocked after %d identical successful results: %s\nRepeated evidence: %s\nRecovery: do not run the same command again. Treat this output as evidence, update the plan, vary the query/parameter/encoding, or summarize the finding before continuing.", repeats, command, truncateLine(lastResult, 500))
		}
	}
	return ""
}

func duplicateFetchURLSkip(run TaskRun, tc llm.ToolCallDef) string {
	if tc.Function.Name != "fetch_url" {
		return ""
	}
	url := fetchURLFromToolArgs(tc.Function.Arguments)
	if url == "" {
		return ""
	}
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if tool.Name != "fetch_url" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(tool.Status), "done") {
			continue
		}
		if sameURL(fetchURLFromToolArgs(json.RawMessage(tool.Input)), url) {
			return fmt.Sprintf("fetch_url skipped: %s was already fetched in this run. Use the previous result, fetch a different high-quality source, or refine the search query with current year/date and product/version terms.", url)
		}
	}
	return ""
}

func fetchURLFromToolArgs(raw json.RawMessage) string {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.URL)
}

func sameURL(a, b string) bool {
	a = strings.TrimRight(strings.ToLower(strings.TrimSpace(a)), "/")
	b = strings.TrimRight(strings.ToLower(strings.TrimSpace(b)), "/")
	return a != "" && a == b
}

const shellStormThreshold = 10

var shellFamilyURLRE = regexp.MustCompile(`https?://[^\s"'|]+`)

// shellCommandFamily returns a coarse "family key" — core binary plus the target
// URL (host+path, query stripped) — so commands that differ only in payload/offset
// group together. That grouping is the signature of a manual extraction or
// enumeration loop (the same curl to the same endpoint with a varying injection).
// Returns "" when there's no URL target, so varied local commands don't false-group.
func shellCommandFamily(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	bin := ""
	for _, f := range strings.Fields(command) {
		if f == "sudo" || strings.HasPrefix(f, "-") {
			continue
		}
		if i := strings.IndexByte(f, '='); i > 0 && !strings.ContainsAny(f[:i], "/\\") {
			continue // env assignment VAR=val
		}
		bin = filepath.Base(f)
		break
	}
	if bin == "" {
		return ""
	}
	u := shellFamilyURLRE.FindString(command)
	if u == "" {
		return ""
	}
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	return bin + "|" + u
}

// shellCommandStormHint appends a soft nudge (not a block) when the agent has run
// many near-identical URL-targeted commands this run — redirecting it to script the
// loop / run a script it already wrote, and to persist extracted values. Fires once
// at the threshold, then every 10, so it never spams.
func shellCommandStormHint(run TaskRun, tc llm.ToolCallDef) string {
	if !isShellTool(tc.Function.Name) {
		return ""
	}
	fam := shellCommandFamily(shellCommandFromToolArgs(tc.Function.Arguments))
	if fam == "" {
		return ""
	}
	count := 1 // the current call (not yet recorded in run.Tools)
	for _, tool := range run.Tools {
		if !isShellTool(tool.Name) {
			continue
		}
		if shellCommandFamily(shellCommandFromToolArgs(json.RawMessage(tool.Input))) == fam {
			count++
		}
	}
	// Fire at the threshold then every 5 calls after. In a real run the family count
	// grows one call at a time, so it reliably lands on each trigger point without
	// nudging on every single command (which would itself add to context pressure).
	if count < shellStormThreshold || (count-shellStormThreshold)%5 != 0 {
		return ""
	}
	bin := strings.SplitN(fam, "|", 2)[0]
	parts := []string{fmt.Sprintf("This is ~the %dth similar %s request to the same endpoint this run.", count, bin)}
	if script := lastScriptArtifact(run); script != "" {
		parts = append(parts, fmt.Sprintf("You already wrote %s — run it to do this in one shot instead of one request per value.", script))
	} else if bin == "curl" {
		parts = append(parts, "For repeated HTTP path/header checks, use http_probe with a paths list so raw output is saved as an artifact and only a compact summary enters context.")
	} else {
		parts = append(parts, "If you're extracting or enumerating, script the loop (one bash loop or a small script) and run it once instead of issuing each request by hand.")
	}
	parts = append(parts, "Save extracted values (hashes, creds, paths) to a file or via the memory tool so they survive context compaction.")
	return "\n[" + strings.Join(parts, " ") + "]"
}

// lastScriptArtifact returns the basename of the most recent script the run wrote,
// or "" if none — used to point the model at a script it already created.
func lastScriptArtifact(run TaskRun) string {
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if tool.Name != "write_file" && tool.Name != "edit_file" {
			continue
		}
		var p struct {
			Path string `json:"path"`
			File string `json:"file"`
		}
		if json.Unmarshal([]byte(tool.Input), &p) != nil {
			continue
		}
		path := p.Path
		if path == "" {
			path = p.File
		}
		lower := strings.ToLower(path)
		for _, ext := range []string{".py", ".sh", ".ps1", ".rb", ".pl"} {
			if strings.HasSuffix(lower, ext) {
				return filepath.Base(path)
			}
		}
	}
	return ""
}

// largestShellFamily returns the most-repeated URL-targeted command family in a run
// and its count, used to distill an inefficiency lesson at run finish.
func largestShellFamily(run *TaskRun) (string, int) {
	counts := map[string]int{}
	best := ""
	bestN := 0
	for _, tool := range run.Tools {
		if !isShellTool(tool.Name) {
			continue
		}
		fam := shellCommandFamily(shellCommandFromToolArgs(json.RawMessage(tool.Input)))
		if fam == "" {
			continue
		}
		counts[fam]++
		if counts[fam] > bestN {
			bestN = counts[fam]
			best = fam
		}
	}
	return best, bestN
}

func normalizedShellEvidence(result string) string {
	lines := strings.Split(strings.TrimSpace(result), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isShellMetadataLine(trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	return strings.ToLower(strings.Join(out, "\n"))
}

func isEmptyShellOutputResult(result string) bool {
	lines := strings.Split(strings.TrimSpace(result), "\n")
	meaningful := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isShellMetadataLine(trimmed) {
			continue
		}
		meaningful++
	}
	return meaningful == 0
}

func isShellMetadataLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "[shared_terminal/") ||
		strings.HasPrefix(lower, "[powershell ") ||
		strings.HasPrefix(lower, "[cmd ") ||
		strings.HasPrefix(lower, "[bash ") ||
		strings.HasPrefix(lower, "[wsl ") ||
		strings.HasPrefix(lower, "[empty shell output:") ||
		strings.HasPrefix(lower, "cwd: ")
}

func shellCommandFromToolArgs(raw json.RawMessage) string {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.Command)
}

func repeatShellCommandKey(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if stripped, ok := stripTrailingBackgroundOperator(command); ok {
		command = stripped
	}
	command = strings.TrimSpace(command)
	command = regexp.MustCompile(`\bsudo\s+-n\s+`).ReplaceAllString(command, "sudo ")
	command = regexp.MustCompile(`\s+`).ReplaceAllString(command, " ")
	return strings.ToLower(command)
}

func appendShellRecoveryHints(result string) string {
	if strings.TrimSpace(result) == "" {
		return result
	}
	lower := strings.ToLower(result)
	var hints []string
	if strings.Contains(lower, "keyword fuzz defined") && strings.Contains(lower, "not found in headers") {
		hints = append(hints, "ffuf syntax hint: ffuf requires the literal word FUZZ in the URL, headers, method, or POST data. For directory fuzzing use a URL such as http://connected.htb/admin/FUZZ, not http://connected.htb/admin/.")
	}
	if strings.Contains(lower, "flag provided but not defined: -length") && strings.Contains(lower, "gobuster dir") {
		hints = append(hints, "gobuster syntax hint: this gobuster build does not support --length. Use --exclude-length <size> when filtering by response length, or omit length filtering and save output to a file for review.")
	}
	if len(hints) == 0 {
		return result
	}
	for _, hint := range hints {
		if strings.Contains(result, hint) {
			continue
		}
		result = strings.TrimRight(result, "\r\n") + "\nRecovery: " + hint
	}
	return result
}

func recoverBenignShellPipelineClose(tc llm.ToolCallDef, result string, runErr error) (string, bool) {
	if runErr == nil || !strings.Contains(strings.ToLower(runErr.Error()), "exit code 141") {
		return result, false
	}
	command := shellCommandFromToolArgs(tc.Function.Arguments)
	if !looksLikeHeadTerminatedPipeline(command) || shellResultHasNoEvidence(result) {
		return result, false
	}
	hint := "Recovery: shell exit 141 is likely SIGPIPE from piping a noisy scanner into head after enough output was captured. Treat the shown output as evidence. Next time, do not pipe long-running scanners through head; write output to a file (-o/-of/-json/tee) or run background=true, then tail/grep the saved file."
	result = strings.TrimRight(result, "\r\n")
	if !strings.Contains(result, hint) {
		result += "\n" + hint
	}
	return result, true
}

func looksLikeHeadTerminatedPipeline(command string) bool {
	lower := strings.ToLower(command)
	if !strings.Contains(lower, "|") || !regexp.MustCompile(`\|\s*head\b`).MatchString(lower) {
		return false
	}
	return hasAny(lower, "ffuf", "gobuster", "feroxbuster", "dirsearch", "nmap", "hydra", "wpscan")
}

func shellResultHasNoEvidence(result string) bool {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isShellMetadataLine(line) || strings.HasPrefix(strings.ToLower(line), "error: exit code") {
			continue
		}
		return false
	}
	return true
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
		"tool_disabled",
		"repeated_tool_failure",
		"repeated_empty_tool_output",
		"repeated_same_tool_result":
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
		b.maxSearches = 16
	}
	if b.maxFetches <= 0 {
		b.maxFetches = 32
	}
	if b.maxFailedFetches <= 0 {
		b.maxFailedFetches = 10
	}
	if b.maxBrowserActions <= 0 {
		b.maxBrowserActions = 80
	}
	if len(taskText) > 0 && isExploitResearchTask(strings.Join(taskText, " ")) {
		b.maxSearches = max(b.maxSearches, 32)
		b.maxFetches = max(b.maxFetches, 48)
		b.maxFailedFetches = max(b.maxFailedFetches, 14)
		b.maxBrowserActions = max(b.maxBrowserActions, 120)
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

func (a *App) recordCategorizedToolLedger(runID string, tc llm.ToolCallDef, status, result string, durationMs int64) {
	kind := ledgerKindForTool(tc.Function.Name)
	if kind == "" {
		return
	}
	a.recordLedger(ledger.Event{
		RunID:      runID,
		Kind:       kind,
		Source:     "tool",
		Tool:       tc.Function.Name,
		Status:     status,
		Message:    tc.ID,
		Input:      string(tc.Function.Arguments),
		Output:     result,
		DurationMs: durationMs,
	})
}

func ledgerKindForTool(name string) string {
	switch {
	case name == "web_search" || name == "fetch_url":
		return "web_research"
	case strings.HasPrefix(name, "browser_"):
		return "browser_action"
	case strings.HasPrefix(name, "todo_"):
		return "planner_event"
	case strings.HasPrefix(name, "subagent_"):
		return "subagent_result"
	default:
		return ""
	}
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

func toolDisabledMessage(cfg settings.ToolsConfig, name string) string {
	if !cfg.Enabled {
		return "tools are disabled globally in settings"
	}
	active := strings.TrimSpace(cfg.ActiveToolset)
	if active == "" {
		active = settings.DefaultSettings().Tools.ActiveToolset
	}
	if !toolsetContains(cfg, active, name) {
		return fmt.Sprintf("tool %q is disabled by active toolset %q. Enabled tools now: %s. Use one of those tools, or ask the user to switch to a toolset that includes %q (for target shell/file work: unrestricted, balanced, local-code, offline, or Ops mode).", name, active, enabledToolListForMessage(settings.EffectiveEnabledTools(cfg)), name)
	}
	if enabled, ok := cfg.EnabledTools[name]; ok && !enabled {
		return fmt.Sprintf("tool %q is disabled by its per-tool toggle under active toolset %q. Enabled tools now: %s. Use one of those tools or ask the user to enable %q.", name, active, enabledToolListForMessage(settings.EffectiveEnabledTools(cfg)), name)
	}
	return fmt.Sprintf("tool %q is disabled in settings under active toolset %q", name, active)
}

func shouldStopForDisabledTool(run TaskRun, name string) bool {
	seen := 0
	for i := len(run.Tools) - 1; i >= 0; i-- {
		tool := run.Tools[i]
		if strings.TrimSpace(tool.Name) == name && strings.EqualFold(strings.TrimSpace(tool.Status), "disabled") {
			seen++
			if seen >= 2 {
				return true
			}
			continue
		}
		break
	}
	return false
}

func enabledToolListForMessage(enabled map[string]bool) string {
	var names []string
	for name, ok := range enabled {
		if ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 16 {
		names = append(names[:16], "...")
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func toolsetContains(cfg settings.ToolsConfig, active, name string) bool {
	if name == "bash" {
		name = "shell"
	}
	toolsets := cfg.Toolsets
	if toolsets == nil {
		toolsets = settings.DefaultSettings().Tools.Toolsets
	}
	tools, ok := toolsets[strings.TrimSpace(active)]
	if !ok || len(tools) == 0 {
		tools = toolsets[settings.DefaultSettings().Tools.ActiveToolset]
	}
	for _, tool := range tools {
		if tool == name || (name == "shell" && tool == "bash") {
			return true
		}
	}
	return false
}

// AgentMode is the first-pass auto-agent routing result.
type AgentMode struct {
	Name          string
	Description   string
	Instructions  string
	ContextBudget int
	DefaultEffort string
}

func classifyAgentMode(text string) AgentMode {
	lower := strings.ToLower(text)
	switch {
	case looksShellCentricTask(lower):
		return AgentMode{
			Name:         "Ops",
			Description:  "Operate inside a lab target with shell-first evidence gathering.",
			Instructions: "Work like a careful HTB/Kali operator. Prefer WSL shell evidence over Windows browser/fetch, verify the target state before choosing an exploit path, keep notes/writeups current with confirmed facts, and avoid copying large public PoCs unless the user explicitly asks for that route.",
		}
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
	case "ops":
		return false
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
	case "anthropic":
		return backends.NewAnthropic(p), nil
	default:
		return backends.NewLMStudio(p), nil
	}
}

var buildClientForAgent = buildClient

func (a *App) ensureModelLoaded(ctx context.Context, client llm.Client, profile settings.Profile, onRetry ...func(int, error)) error {
	loader, ok := client.(interface{ LoadModel(context.Context) error })
	if !ok {
		a.recordLedger(ledger.Event{
			Kind:    "model_load",
			Source:  "provider",
			Status:  "skipped",
			Message: modelLoadKey(profile),
			Detail:  "client does not expose LoadModel",
		})
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
	} else {
		a.recordLedger(ledger.Event{
			Kind:    "model_load",
			Source:  "provider",
			Status:  "reused",
			Message: key,
			Metadata: map[string]string{
				"ctx_tokens": strconv.Itoa(profile.CtxTokens),
			},
		})
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
			window := profile.CtxTokens
			if window <= 0 || actual < window {
				window = actual
			}
			if window > 0 {
				// Hold back the same output reserve as the main path so the two
				// budget setters agree and the status bar's parts stay consistent.
				usable := effectiveWorkingContextBudget(0, window)
				a.mu.Lock()
				changed := a.history.Budget() != usable
				a.history.SetBudget(usable)
				a.contextWindow = window
				a.mu.Unlock()
				if changed && a.ctx != nil {
					a.emit("mauler:budget_updated", usable)
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
		a.recordLedger(ledger.Event{
			Kind:    "model_load",
			Source:  "provider",
			Status:  "attempt",
			Message: key,
			Metadata: map[string]string{
				"attempt": strconv.Itoa(attempt),
				"max":     strconv.Itoa(modelLoadAttempts),
			},
		})
		loadCtx, cancel := context.WithTimeout(ctx, modelLoadAttemptTimeout)
		err := loader.LoadModel(loadCtx)
		cancel()
		if err == nil {
			a.recordLedger(ledger.Event{
				Kind:    "model_load",
				Source:  "provider",
				Status:  "ok",
				Message: key,
				Metadata: map[string]string{
					"attempt": strconv.Itoa(attempt),
				},
			})
			return nil
		}
		lastErr = err
		a.clearLoadedModelKey(key)
		if ctxErr := ctx.Err(); ctxErr != nil {
			if errors.Is(err, context.Canceled) || errors.Is(ctxErr, context.Canceled) {
				a.recordLedger(ledger.Event{
					Kind:    "model_load",
					Source:  "provider",
					Status:  "cancelled",
					Message: key,
					Error:   err.Error(),
				})
				return err
			}
			a.recordLedger(ledger.Event{
				Kind:    "model_load",
				Source:  "provider",
				Status:  "cancelled",
				Message: key,
				Error:   ctxErr.Error(),
			})
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
		a.recordLedger(ledger.Event{
			Kind:    "model_load",
			Source:  "provider",
			Status:  "retry",
			Message: key,
			Error:   err.Error(),
			Metadata: map[string]string{
				"attempt": strconv.Itoa(attempt),
			},
		})
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

	a.recordLedger(ledger.Event{
		Kind:    "model_load",
		Source:  "provider",
		Status:  "error",
		Message: key,
		Error:   fmt.Sprintf("%v", lastErr),
	})
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
	// Backends often round context upward slightly. Record that once per run so
	// diagnostics remain available without polluting every model turn.
	if severity == "info" && run.hasEvent("backend_runtime_changed", "severity=info") {
		return
	}
	run.addEvent(
		"backend_runtime_changed",
		"Backend runtime differs from active profile",
		fmt.Sprintf("severity=%s expected_ctx=%d actual_ctx=%d model=%s backend=%s base_url=%s", severity, profile.CtxTokens, actual, profile.ModelID, profile.Backend, profile.BaseURL),
	)
}

func buildChatRequest(profile settings.Profile, msgs []llm.Message, toolDefs []llm.ToolDef, toolChoice string, forceNoThink bool, coding bool, effort string) llm.Request {
	plan := effortToThinking(effort, profile)
	useCodingParams := coding || plan.coding
	params := profile.ActiveParams(useCodingParams)
	if useCodingParams && !profile.Thinking && profile.ThinkCoding.MaxTokens > 0 {
		params = profile.ThinkCoding
	}
	if plan.maxTokensCap > 0 && (params.MaxTokens <= 0 || params.MaxTokens > plan.maxTokensCap) {
		params.MaxTokens = plan.maxTokensCap
	}
	enableThinking := plan.enableThinking
	preserveThinking := profile.PreserveThink && enableThinking
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
		ReasoningEffort:  normaliseReasoningEffort(effort),
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

// shellSession holds the state of one live interactive shell running on a
// pseudo-terminal (ConPTY on Windows). input writes to the PTY master; cancel
// terminates the process and closes the PTY (idempotent).
type shellSession struct {
	id        string
	input     io.Writer
	pty       pty.Pty
	cancel    func()
	done      chan struct{}
	output    chan terminalOutput
	interrupt chan struct{}
	runMu     sync.Mutex
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
		a.recordLedger(ledger.Event{
			Kind:    "shell_close",
			Source:  "terminal",
			Status:  "replaced",
			Message: a.shellSess.id,
		})
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
		shellArgs = []string{"-NoLogo", "-NoProfile"}
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
			shellArgs = []string{"-NoLogo", "-NoProfile"}
		} else {
			shellCmd = "bash"
			shellArgs = maulerBashInteractiveArgs()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	terminal, err := pty.New()
	if err != nil {
		cancel()
		return "", fmt.Errorf("shell pty: %w", err)
	}
	// Start wide. The agent often runs commands before (or while) the terminal
	// pane is laid out/visible, so the frontend's fit-based ShellResize may not have
	// run yet. A narrow default makes width-aware programs (and command echo) hard-
	// wrap/truncate output to a narrow column, which both mangles what the *model*
	// captures and leaves a dead zone on the right of the visible terminal. 200x50
	// gives plenty of room; ShellResize narrows it later to match the real pane.
	_ = terminal.Resize(200, 50)

	cmd := terminal.CommandContext(ctx, shellCmd, shellArgs...)
	if backend != "wsl" {
		cmd.Dir = cwd
	}

	// Hide the child process window where the platform exposes that knob.
	hidePtyShellWindow(cmd)

	if err := cmd.Start(); err != nil {
		cancel()
		_ = terminal.Close()
		a.recordLedger(ledger.Event{
			Kind:   "shell_start",
			Source: "terminal",
			Status: "error",
			Error:  err.Error(),
			Input:  shellCmd + " " + strings.Join(shellArgs, " "),
		})
		return "", fmt.Errorf("shell start: %w", err)
	}

	id := fmt.Sprintf("shell-%d", time.Now().UnixMilli())
	a.recordLedger(ledger.Event{
		Kind:    "shell_start",
		Source:  "terminal",
		Status:  "running",
		Message: id,
		Input:   shellCmd + " " + strings.Join(shellArgs, " "),
		Metadata: map[string]string{
			"backend": backend,
			"distro":  distro,
			"user":    user,
			"cwd":     filepath.ToSlash(cwd),
		},
	})
	closeOnce := sync.Once{}
	sess := &shellSession{
		id:    id,
		input: terminal,
		pty:   terminal,
		done:  make(chan struct{}),
		cancel: func() {
			closeOnce.Do(func() {
				cancel()
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = terminal.Close()
			})
		},
		output:    make(chan terminalOutput, 4096),
		interrupt: make(chan struct{}, 1),
	}
	a.shellSess = sess

	go a.pipeShellOutput(id, terminal, "stdout")
	go func() {
		_ = cmd.Wait()
		sess.cancel()
		close(sess.done)
		a.shellMu.Lock()
		if a.shellSess != nil && a.shellSess.id == id {
			a.shellSess = nil
		}
		a.shellMu.Unlock()
		a.recordLedger(ledger.Event{
			Kind:    "shell_exit",
			Source:  "terminal",
			Status:  "done",
			Message: id,
		})
		if a.ctx != nil {
			a.emit("mauler:shell_exit", map[string]string{"id": id})
		}
	}()

	return id, nil
}

// pipeShellOutput fans PTY output out to two consumers:
//
//   - The UI gets the *raw* bytes (base64) on "mauler:shell_output" so xterm.js
//     can render colours, cursor moves, \r repaints and full-screen TUIs intact.
//   - The agent's shared-terminal collector gets plain text: complete lines are
//     assembled from the raw stream first, then sanitizeTerminalLine runs per
//     line. Sanitizing only whole lines avoids splitting an escape sequence
//     across a Read() boundary, which used to leak control bytes to the agent.
//
// PTYs combine stdout/stderr, so stream is normally "stdout".
func (a *App) pipeShellOutput(id string, r io.Reader, stream string) {
	buf := make([]byte, 8192)
	var pending string
	var dropped int
	var uiFilter uiMarkerFilter
	emitUI := func(b []byte) {
		if a.ctx != nil && len(b) > 0 {
			a.emit("mauler:shell_output", map[string]string{
				"id":   id,
				"data": base64.StdEncoding.EncodeToString(b),
			})
		}
	}
	for {
		n, err := r.Read(buf)
		if n > 0 {
			// 1. Raw bytes (minus framing markers) to the xterm.js terminal.
			emitUI(uiFilter.feed(buf[:n]))
			// 2. Sanitized, line-framed text for the agent collector.
			a.shellMu.Lock()
			sess := a.shellSess
			if sess != nil && sess.id == id {
				var records []terminalOutput
				records, pending = terminalOutputRecords(pending+decodeTerminalOutput(buf[:n]), stream, false)
				for _, record := range records {
					record.data = sanitizeTerminalLine(record.data)
					if strings.TrimSpace(record.data) == "" {
						continue
					}
					select {
					case sess.output <- record:
					default:
						dropped++
					}
				}
				if dropped > 0 {
					select {
					case sess.output <- terminalOutput{data: fmt.Sprintf("[%d line(s) dropped: shared-terminal buffer full]", dropped), stream: stream}:
						dropped = 0
					default:
					}
				}
			}
			a.shellMu.Unlock()
		}
		if err != nil {
			emitUI(uiFilter.flush())
			if clean := sanitizeTerminalLine(pending); strings.TrimSpace(clean) != "" {
				a.shellMu.Lock()
				sess := a.shellSess
				if sess != nil && sess.id == id {
					select {
					case sess.output <- terminalOutput{data: strings.TrimRight(clean, "\r\n"), stream: stream}:
					default:
					}
				}
				a.shellMu.Unlock()
			}
			break
		}
	}
}

func terminalOutputRecords(chunk, stream string, flush bool) ([]terminalOutput, string) {
	chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
	parts := strings.Split(chunk, "\n")
	remainder := ""
	if !flush && !strings.HasSuffix(chunk, "\n") {
		remainder = parts[len(parts)-1]
		parts = parts[:len(parts)-1]
	}
	out := make([]terminalOutput, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimRight(part, "\r")
		if part == "" {
			continue
		}
		out = append(out, terminalOutput{data: part, stream: stream})
	}
	if flush && remainder != "" {
		out = append(out, terminalOutput{data: strings.TrimRight(remainder, "\r"), stream: stream})
	}
	return out, remainder
}

// uiMarkerFilter removes the shared-terminal framing from the *raw* byte stream
// shown in the xterm.js terminal. The agent still sees the markers on its
// sanitized line channel; this only cleans up what the human sees. It works a
// line at a time:
//
//   - The wrapper-command echo carries both __MAULER_START_ and __MAULER_DONE_
//     on one line and is dropped whole.
//   - A lone START marker line is dropped; a DONE marker attached to real output
//     has only the marker token stripped so the output keeps its line break.
//
// Incomplete trailing lines that contain no marker are emitted immediately, so
// interactive prompts and TUIs stay responsive (nothing is buffered waiting for
// a newline unless it might be a marker line).
type uiMarkerFilter struct {
	line         []byte // bytes of the current, not-yet-terminated line
	emitted      int    // how many bytes of line have already gone to the UI
	suppressLine bool   // wrapper echo detected after a prompt fragment was emitted
}

var (
	markerCommon  = []byte("__MAULER_")
	markerStart   = []byte("__MAULER_START_")
	markerDone    = []byte("__MAULER_DONE_")
	markerCWD     = []byte("__MAULER_CWD_")
	markerRecover = []byte("__MAULER_RECOVER_")
	// markerEchoSig is the literal that prefixes every wrapped command. The echoed
	// command builds markers from a shell variable (so it carries no literal
	// __MAULER_), so this assignment is how the UI recognises and hides the echo.
	markerEchoSig = []byte("M=__MA''ULER_")
	markerSttyOff = []byte("stty -echo 2>/dev/null || true")
	markerSttyOn  = []byte("stty echo 2>/dev/null || true")
	// markerEchoVar / markerEchoPipefail are truncation-proof signatures of the
	// echoed wrapper. markerEchoSig only matches when the `M=__MA''ULER_` prefix
	// survives intact; if anything ever eats that prefix again (a re-introduced
	// stty race, terminal reflow, …) these still match because they sit after the
	// prefix and never appear in real command OUTPUT — the shell expands `${M}` to
	// `__MAULER_`, so a literal `${M}` only exists in an echoed-but-unrun command.
	markerEchoVar      = []byte("${M}")
	markerEchoPipefail = []byte("set -o pipefail 2>/dev/null")
)

// isWrapperEchoLine reports whether a (possibly prefix-truncated) line is the
// echoed shared-terminal wrapper command, using signatures that survive
// truncation. Used by the UI marker filter to suppress/erase the echo.
func isWrapperEchoLine(b []byte) bool {
	return bytes.Contains(b, markerEchoSig) ||
		bytes.Contains(b, markerEchoVar) ||
		bytes.Contains(b, markerEchoPipefail)
}

func (f *uiMarkerFilter) feed(data []byte) []byte {
	f.line = append(f.line, data...)
	var out []byte
	for {
		nl := bytes.IndexByte(f.line, '\n')
		if nl < 0 {
			break
		}
		out = append(out, f.classify(f.line[:nl+1])...)
		f.line = append([]byte(nil), f.line[nl+1:]...)
		f.emitted = 0
		f.suppressLine = false
	}
	if !f.suppressLine && f.emitted > 0 && (isWrapperEchoLine(f.line) || bytes.Contains(f.line, markerSttyOff) || bytes.Contains(f.line, markerSttyOn)) {
		// The prompt can be rendered before the echoed wrapper assignment arrives.
		// Clear that row and suppress the rest of the wrapper echo until newline.
		out = append(out, []byte("\r\x1b[2K")...)
		f.emitted = len(f.line)
		f.suppressLine = true
	}
	// Stream the trailing partial line now unless it might still grow into a
	// marker line, or a wrapper-command echo, that we need to classify on its
	// newline. (The echo carries no literal __MAULER_, so we also gate on its
	// assignment signature, otherwise it would leak before classify can drop it.)
	tail := f.line[f.emitted:]
	if !f.suppressLine && !bytes.Contains(f.line, markerCommon) && !isWrapperEchoLine(f.line) && !bytes.Contains(f.line, markerSttyOff) && !bytes.Contains(f.line, markerSttyOn) && !couldBecomeMarkerEcho(tail) && !couldBecomeLiteralEcho(tail, markerEchoPipefail) && !couldBecomeLiteralEcho(tail, markerSttyOff) && !couldBecomeLiteralEcho(tail, markerSttyOn) && len(f.line) > f.emitted {
		out = append(out, f.line[f.emitted:]...)
		f.emitted = len(f.line)
	}
	return out
}

func (f *uiMarkerFilter) flush() []byte {
	var out []byte
	if len(f.line) > f.emitted {
		out = stripMarkerTokens(f.line[f.emitted:])
	}
	f.line, f.emitted, f.suppressLine = nil, 0, false
	return out
}

// classify handles one complete line (including its trailing newline).
func (f *uiMarkerFilter) classify(full []byte) []byte {
	if f.suppressLine {
		return nil
	}
	if isWrapperEchoLine(full) {
		if f.emitted > 0 {
			return []byte("\r\x1b[2K")
		}
		return nil
	}
	if bytes.Contains(full, markerSttyOff) || bytes.Contains(full, markerSttyOn) {
		if f.emitted > 0 {
			return []byte("\r\x1b[2K")
		}
		return nil
	}
	if f.emitted > 0 {
		// Part of this line already reached the UI; we can't drop it now, but we
		// can still strip any marker tokens from the remainder.
		return stripMarkerTokens(full[f.emitted:])
	}
	if bytes.Contains(full, markerStart) && bytes.Contains(full, markerDone) {
		return nil // wrapper-command echo
	}
	return stripMarkerTokens(full)
}

// stripMarkerTokens removes any __MAULER_START_…__ / __MAULER_DONE_…:<n> tokens
// from a complete byte slice, leaving the surrounding bytes (including newlines)
// intact.
func stripMarkerTokens(b []byte) []byte {
	var out []byte
	for len(b) > 0 {
		i := bytes.Index(b, markerCommon)
		if i < 0 {
			out = append(out, b...)
			break
		}
		out = append(out, b[:i]...)
		b = b[i:]
		if tok, ok, _ := matchMarker(b, true); ok {
			b = b[tok:]
		} else {
			out = append(out, b[0])
			b = b[1:]
		}
	}
	return out
}

// matchMarker inspects a buffer that begins with "__MAULER_". It returns the
// length of a complete marker token to drop (ok), whether more bytes are needed
// to decide (wait), or neither (a false alarm to pass through).
func matchMarker(b []byte, eof bool) (tokenLen int, ok, wait bool) {
	if !eof && (isStrictPrefix(b, markerStart) || isStrictPrefix(b, markerDone) || isStrictPrefix(b, markerCWD) || isStrictPrefix(b, markerRecover)) {
		return 0, false, true
	}
	if bytes.HasPrefix(b, markerCWD) {
		// The cwd marker carries an arbitrary path; it's always printed on its
		// own line, so drop everything through the newline.
		if nl := bytes.IndexByte(b, '\n'); nl >= 0 {
			return nl + 1, true, false
		}
		return 0, false, !eof
	}
	if bytes.HasPrefix(b, markerRecover) {
		// The interrupt-recovery sentinel (__MAULER_RECOVER_<id>__) is internal
		// plumbing printed on its own line; drop the whole line so the user never
		// sees it. (The agent parser already skips any __MAULER_ line.)
		if nl := bytes.IndexByte(b, '\n'); nl >= 0 {
			return nl + 1, true, false
		}
		return 0, false, !eof
	}
	if bytes.HasPrefix(b, markerStart) {
		rest := b[len(markerStart):]
		p := bytes.Index(rest, []byte("__"))
		if p < 0 {
			return 0, false, !eof
		}
		end := len(markerStart) + p + 2
		// START is printed on its own line; swallow the trailing newline too so
		// we don't leave a blank line behind.
		if end < len(b) && b[end] == '\n' {
			end++
		} else if end == len(b) && !eof {
			return 0, false, true
		}
		return end, true, false
	}
	if bytes.HasPrefix(b, markerDone) {
		j := len(markerDone)
		for j < len(b) && isASCIIDigit(b[j]) {
			j++
		}
		if j >= len(b) {
			return 0, false, !eof
		}
		if b[j] != ':' {
			return 0, false, false
		}
		j++
		ds := j
		for j < len(b) && isASCIIDigit(b[j]) {
			j++
		}
		if j == ds {
			return 0, false, j >= len(b) && !eof
		}
		if j >= len(b) && !eof {
			return 0, false, true
		}
		// Keep the byte after the status (usually a newline) so output the DONE
		// marker was attached to keeps its line break.
		return j, true, false
	}
	return 0, false, false
}

func isStrictPrefix(b, lit []byte) bool { return len(b) < len(lit) && bytes.Equal(b, lit[:len(b)]) }

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

func couldBecomeMarkerEcho(b []byte) bool {
	return couldBecomeLiteralEcho(b, markerEchoSig)
}

func couldBecomeLiteralEcho(b, lit []byte) bool {
	if len(b) == 0 {
		return false
	}
	if len(b) <= len(lit) && bytes.Equal(b, lit[:len(b)]) {
		return true
	}
	max := min(len(b), len(lit)-1)
	for n := 1; n <= max; n++ {
		if bytes.Equal(b[len(b)-n:], lit[:n]) {
			return true
		}
	}
	return false
}

func sanitizeTerminalLine(s string) string {
	s = oscEscape.ReplaceAllString(s, "")
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
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

// ShellInput writes raw bytes to the active shell's PTY. The frontend's xterm.js
// onData handler already supplies exactly what a terminal would send — Enter as
// "\r", backspace as "\x7f", arrows/function keys as "\x1b[…", Tab as "\t",
// Ctrl-C as "\x03" — so we forward verbatim without munging line endings.
func (a *App) ShellInput(id, text string) error {
	a.shellMu.Lock()
	sess := a.shellSess
	a.shellMu.Unlock()
	if sess == nil || sess.id != id {
		return fmt.Errorf("no active shell session %q", id)
	}
	a.recordLedger(ledger.Event{
		Kind:    "shell_input",
		Source:  "terminal",
		Status:  "sent",
		Message: id,
		Metadata: map[string]string{
			"bytes": strconv.Itoa(len(text)),
		},
	})
	_, err := io.WriteString(sess.input, text)
	return err
}

// ShellResize adjusts the active PTY dimensions. The frontend sends best-effort
// measurements from the visible pane; failing resize is non-fatal.
func (a *App) ShellResize(id string, cols, rows int) error {
	// Floor the width well above the old 20-col minimum: a collapsed/hidden pane
	// can report a tiny width, which would make the agent's command output hard-wrap
	// to that width — corrupting what the model captures. 80 cols is the standard
	// terminal floor and keeps autonomous output readable even when the human pane
	// is narrow or not laid out.
	if cols < 80 {
		cols = 80
	}
	if rows < 10 {
		rows = 10
	}
	if cols > 300 {
		cols = 300
	}
	if rows > 120 {
		rows = 120
	}
	a.shellMu.Lock()
	sess := a.shellSess
	a.shellMu.Unlock()
	if sess == nil || sess.id != id {
		return nil
	}
	if sess.pty == nil {
		return nil
	}
	return sess.pty.Resize(cols, rows)
}

func (a *App) runSharedTerminalShell(ctx context.Context, toolName string, raw json.RawMessage, defaultTimeout int) (string, error) {
	var p struct {
		Command    string `json:"command"`
		Timeout    int    `json:"timeout"`
		Background bool   `json:"background"`
		Job        string `json:"job"`
		Verbose    bool   `json:"verbose"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", fmt.Errorf("%s: bad params: %w", toolName, err)
	}

	a.mu.Lock()
	backend := resolveSharedTerminalBackend(a.cfg.Tools.ShellBackend)
	a.mu.Unlock()
	if !sharedTerminalSupportsResolvedBackend(backend) {
		return "", errSharedTerminalUnsupported
	}

	// Polling an existing background job needs no command.
	if jobID := strings.TrimSpace(p.Job); jobID != "" {
		sess, err := a.ensureShellSession()
		if err != nil {
			return "", err
		}
		sess.runMu.Lock()
		defer sess.runMu.Unlock()
		drainTerminalOutput(sess.output)
		return a.pollBackgroundJob(sess, jobID, p.Verbose)
	}

	command, err := tools.PrepareShellCommand(p.Command)
	if err != nil {
		return "", fmt.Errorf("%s: %w", toolName, err)
	}
	if jobID := shellAssignmentJobID(command); jobID != "" {
		sess, err := a.ensureShellSession()
		if err != nil {
			return "", err
		}
		sess.runMu.Lock()
		defer sess.runMu.Unlock()
		drainTerminalOutput(sess.output)
		return a.pollBackgroundJob(sess, jobID, false)
	}
	// A persistent shell hangs forever on a sudo password prompt, poisoning every
	// later command in the session. Make sudo non-interactive so it fails fast instead.
	command = tools.ForceNonInteractiveSudo(command)
	if containsShellHeredoc(command) {
		return "shared_terminal does not support heredoc commands from AI tool calls because the wrapper can leave bash waiting at a continuation prompt. Use a printf pipe instead, for example: printf '%s\\n' '10.129.245.100 connected.htb' | sudo -n tee -a /etc/hosts, or write a temporary script/file and run it.", fmt.Errorf("%s: heredoc unsupported in shared terminal", toolName)
	}
	if isShellJobsCommand(command) {
		return "Mauler background jobs are not listed with the shell command `job` or `jobs`. Poll a known background job by calling the shell tool with {\"job\":\"j1\"}; if no job id was returned, start the long command with {\"command\":\"...\",\"background\":true}.", fmt.Errorf("%s: use shell job parameter for Mauler background jobs", toolName)
	}
	if stripped, ok := stripTrailingBackgroundOperator(command); ok {
		command = stripped
		p.Background = true
	}
	timeoutSecs := defaultTimeout
	if timeoutSecs <= 0 {
		timeoutSecs = 120
	}
	if p.Timeout > 0 && p.Timeout <= 300 {
		timeoutSecs = p.Timeout
	}
	sess, err := a.ensureShellSession()
	if err != nil {
		return "", err
	}
	sess.runMu.Lock()
	defer sess.runMu.Unlock()
	drainTerminalOutput(sess.output)

	// Launch a long-running command detached and return immediately with a handle.
	if p.Background {
		return a.startBackgroundJob(sess, command, p.Verbose)
	}

	runID := sharedTerminalRunID()
	startMarker, donePrefix, wrapped := sharedTerminalWrapper(command, runID)
	if a.ctx != nil {
		a.emit("mauler:terminal_command_start", map[string]string{
			"id": runID, "session": sess.id, "command": command, "timeout": strconv.Itoa(timeoutSecs),
		})
	}
	// We deliberately leave terminal echo on: the agent skips the wrapper-command
	// echo via isSharedTerminalWrapperEcho, and the UI's uiMarkerFilter drops that
	// echoed line, so there's no need for the racy stty -echo / stty echo toggle
	// that used to leak "stty -echo" and the wrapper into the visible terminal.
	if _, err := writeSharedTerminalWrappedCommand(sess, wrapped); err != nil {
		return "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()
	parser := newSharedTerminalParser(startMarker, donePrefix, runID)
	startedAt := time.Now()
	for {
		select {
		case line, ok := <-sess.output:
			if !ok {
				result := formatSharedTerminalResult(parser.out, backend, -1, time.Since(startedAt).Round(time.Millisecond))
				if strings.TrimSpace(result) != "" {
					result += "\n"
				}
				return result + "[shared_terminal output closed before command completed]", fmt.Errorf("shared terminal: output closed")
			}
			done, exitCode := parser.feed(line)
			if done {
				result := withSharedTerminalCWD(formatSharedTerminalResult(parser.out, backend, exitCode, time.Since(startedAt).Round(time.Millisecond)), parser.cwd)
				if a.ctx != nil {
					a.emit("mauler:terminal_command_done", map[string]string{
						"id": runID, "session": sess.id, "exit_code": strconv.Itoa(exitCode),
					})
				}
				if exitCode != 0 {
					return result, fmt.Errorf("exit code %d", exitCode)
				}
				return result, nil
			}
		case <-sess.done:
			result := formatSharedTerminalResult(parser.out, backend, -1, time.Since(startedAt).Round(time.Millisecond))
			if strings.TrimSpace(result) != "" {
				result += "\n"
			}
			return result + "[shared_terminal shell exited before command completed]", fmt.Errorf("shared terminal: shell exited")
		case <-sess.interrupt:
			result := formatSharedTerminalResult(parser.out, backend, -1, time.Since(startedAt).Round(time.Millisecond))
			interrupted, interruptResult, interruptErr := a.interruptSharedTerminalRun(sess, backend, parser, startedAt, runID, 2*time.Second)
			if strings.TrimSpace(interruptResult) != "" {
				result = interruptResult
			}
			if interrupted {
				return result + "\n[shared_terminal shell command cancelled by user; sent Ctrl+C, session preserved]", fmt.Errorf("shared terminal: shell command cancelled by user")
			}
			if interruptErr != nil {
				return result + fmt.Sprintf("\n[shared_terminal shell command cancelled by user; %v]", interruptErr), fmt.Errorf("shared terminal: shell command cancelled by user")
			}
			return result + "\n[shared_terminal shell command cancelled by user; killed wedged session]", fmt.Errorf("shared terminal: shell command cancelled by user")
		case <-runCtx.Done():
			result := formatSharedTerminalResult(parser.out, backend, -1, time.Since(startedAt).Round(time.Millisecond))
			if runCtx.Err() == context.DeadlineExceeded {
				interrupted, interruptResult, interruptErr := a.interruptSharedTerminalRun(sess, backend, parser, startedAt, runID, 2*time.Second)
				if strings.TrimSpace(interruptResult) != "" {
					result = interruptResult
				}
				if interrupted {
					return result + fmt.Sprintf("\n[shared_terminal timed out after %ds; sent Ctrl+C, session preserved. For long commands use background=true]", timeoutSecs), fmt.Errorf("shared terminal: timed out after %ds", timeoutSecs)
				}
				if interruptErr != nil {
					return result + fmt.Sprintf("\n[shared_terminal timed out after %ds; %v]", timeoutSecs, interruptErr), fmt.Errorf("shared terminal: timed out after %ds", timeoutSecs)
				}
				return result + fmt.Sprintf("\n[shared_terminal timed out after %ds; killed wedged session]", timeoutSecs), fmt.Errorf("shared terminal: timed out after %ds", timeoutSecs)
			}
			a.killSharedTerminalSession(sess)
			return result + "\n[shared_terminal cancelled]", fmt.Errorf("shared terminal: cancelled")
		}
	}
}

var errSharedTerminalUnsupported = errors.New("shared terminal is only supported for bash/wsl shell backends")

func sharedTerminalSupportsBackend(backend string) bool {
	return sharedTerminalSupportsResolvedBackend(resolveSharedTerminalBackend(backend))
}

func sharedTerminalSupportsResolvedBackend(backend string) bool {
	return backend == "wsl" || backend == "bash"
}

func resolveSharedTerminalBackend(backend string) string {
	backend = strings.TrimSpace(strings.ToLower(backend))
	if backend == "" || backend == "auto" {
		if runtime.GOOS == "windows" {
			return "powershell"
		}
		return "bash"
	}
	if backend == "pwsh" {
		return "powershell"
	}
	return backend
}

func (a *App) ensureShellSession() (*shellSession, error) {
	a.shellOpenMu.Lock()
	defer a.shellOpenMu.Unlock()

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

// interruptSharedTerminalRun sends Ctrl+C to a timed-out command and tries to
// keep the session alive (preserving cwd/env/foothold). Because bash aborts the
// wrapper's command list on SIGINT, the DONE marker usually never prints, so we
// confirm the shell is back at a prompt with a fresh recovery sentinel. Only if
// the shell stays unresponsive after the grace window do we kill it.
func (a *App) interruptSharedTerminalRun(sess *shellSession, backend string, parser *sharedTerminalParser, startedAt time.Time, runID string, grace time.Duration) (bool, string, error) {
	result := func() string {
		return formatSharedTerminalResult(parser.out, backend, -1, time.Since(startedAt).Round(time.Millisecond))
	}
	if _, err := io.WriteString(sess.input, "\x03"); err != nil {
		a.killSharedTerminalSession(sess)
		return false, result(), fmt.Errorf("interrupt failed: %w", err)
	}

	// Give SIGINT a moment to land, then probe for a live prompt. If the shell
	// echoes or runs our probe, it's responsive and the session is preserved.
	time.Sleep(120 * time.Millisecond)
	sentinel := "__MAULER_RECOVER_" + runID + "__"
	if _, err := io.WriteString(sess.input, fmt.Sprintf("stty echo 2>/dev/null || true; printf '%%s\\n' %s\r", terminalShellQuote(sentinel))); err != nil {
		a.killSharedTerminalSession(sess)
		return false, result(), nil
	}

	timer := time.NewTimer(grace)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-sess.output:
			if !ok {
				return false, result(), fmt.Errorf("output closed while interrupting")
			}
			if strings.Contains(line.data, sentinel) {
				return true, result(), nil
			}
			// A real DONE can still arrive if the command trapped SIGINT.
			if done, code := parser.feed(line); done {
				return true, withSharedTerminalCWD(formatSharedTerminalResult(parser.out, backend, code, time.Since(startedAt).Round(time.Millisecond)), parser.cwd), nil
			}
		case <-sess.done:
			return false, result(), fmt.Errorf("shell exited while interrupting")
		case <-timer.C:
			a.killSharedTerminalSession(sess)
			return false, result(), nil
		}
	}
}

// runSharedCommandSimple runs a short command on the shared session and returns
// the captured output lines plus exit code. It reuses the marker protocol but,
// unlike the foreground path, expects the command to finish quickly (job launch
// or a poll), so it has no interrupt/escalation logic. Caller must hold runMu.
func (a *App) runSharedCommandSimple(sess *shellSession, command string, timeout time.Duration) ([]terminalOutput, int, error) {
	drainTerminalOutput(sess.output)
	runID := sharedTerminalRunID()
	startMarker, donePrefix, wrapped := sharedTerminalWrapper(command, runID)
	if _, err := writeSharedTerminalWrappedCommand(sess, wrapped); err != nil {
		return nil, -1, err
	}
	parser := newSharedTerminalParser(startMarker, donePrefix, runID)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-sess.output:
			if !ok {
				return parser.out, -1, fmt.Errorf("shared terminal output closed")
			}
			if done, code := parser.feed(line); done {
				return parser.out, code, nil
			}
		case <-timer.C:
			return parser.out, -1, fmt.Errorf("shared terminal helper timed out after %s", timeout)
		case <-sess.done:
			return parser.out, -1, fmt.Errorf("shared terminal shell exited")
		}
	}
}

// startBackgroundJob launches command detached in the shared session, capturing
// its output to a logfile and PID to a pidfile, then returns a handle the agent
// can poll. The shared session keeps the job's cwd/env, so foothold state is
// preserved across the launch.
func (a *App) startBackgroundJob(sess *shellSession, command string, verbose bool) (string, error) {
	a.bgMu.Lock()
	running := len(a.bgJobs)
	a.bgMu.Unlock()
	if running >= tools.MaxConcurrentBackgroundJobs {
		return "", fmt.Errorf("shell: too many background jobs tracked (%d/%d); poll existing jobs to completion before starting more", running, tools.MaxConcurrentBackgroundJobs)
	}
	a.bgMu.Lock()
	a.bgCounter++
	id := fmt.Sprintf("j%d", a.bgCounter)
	a.bgMu.Unlock()

	logPath := "/tmp/mauler_job_" + id + ".log"
	pidPath := "/tmp/mauler_job_" + id + ".pid"
	launch := fmt.Sprintf("{ %s ; } > %s 2>&1 & echo $! > %s; echo launched %s",
		command, terminalShellQuote(logPath), terminalShellQuote(pidPath), id)

	if _, _, err := a.runSharedCommandSimple(sess, launch, 15*time.Second); err != nil {
		return "", fmt.Errorf("shell: failed to launch background job: %w", err)
	}

	now := time.Now()
	job := &bgJob{
		id:        id,
		command:   command,
		log:       logPath,
		pidfile:   pidPath,
		started:   now,
		lastPoll:  now,
		lastState: "running",
	}
	a.bgMu.Lock()
	a.bgJobs[id] = job
	a.bgMu.Unlock()
	a.emitBackgroundJobUpdate(job, "started", "", backgroundJobPollInterval(0), verbose)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Started background job %s: %s\n", id, command)
	fmt.Fprintf(&sb, "Output is streaming to %s.\n", logPath)
	fmt.Fprintf(&sb, "Poll after %s with {\"job\":\"%s\"}; use {\"job\":\"%s\",\"verbose\":true} for up to 65000 bytes of output.", backgroundJobPollInterval(0), id, id)
	if verbose {
		fmt.Fprintf(&sb, "\n[background job %s verbose] pidfile=%s log=%s backoff=1s,2s,3s,5s,8s,13s,30s", id, pidPath, logPath)
	}
	return sb.String(), nil
}

// pollBackgroundJob reports a background job's running/done state and the tail of
// its output. A finished job is forgotten after this call.
func (a *App) pollBackgroundJob(sess *shellSession, id string, verbose bool) (string, error) {
	a.bgMu.Lock()
	job := a.bgJobs[id]
	if job != nil {
		if wait, ok := backgroundJobPollWait(job, time.Now()); ok {
			result := formatBackgroundJobTooEarly(job, wait)
			a.bgMu.Unlock()
			a.emitBackgroundJobUpdate(job, firstNonEmpty(job.lastState, "running"), result, wait, verbose)
			return result, nil
		}
	}
	a.bgMu.Unlock()
	if job == nil {
		return "", fmt.Errorf("shell: no background job %q (unknown id, or it already finished and was cleared)", id)
	}

	now := time.Now()
	tailBytes := 12000
	if verbose {
		tailBytes = 65000
	}
	poll := fmt.Sprintf("pid=$(cat %s 2>/dev/null); if [ -n \"$pid\" ] && kill -0 \"$pid\" 2>/dev/null; then printf '__JOBSTATE__ running\\n'; else printf '__JOBSTATE__ done\\n'; fi; tail -c %d -- %s 2>/dev/null",
		terminalShellQuote(job.pidfile), tailBytes, terminalShellQuote(job.log))
	out, _, err := a.runSharedCommandSimple(sess, poll, 15*time.Second)
	if err != nil {
		return "", fmt.Errorf("shell: failed to poll job %s: %w", id, err)
	}

	state := "unknown"
	var body []string
	for _, line := range out {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line.data), "__JOBSTATE__"); ok {
			state = strings.TrimSpace(rest)
			continue
		}
		body = append(body, line.data)
	}
	if state == "done" {
		a.bgMu.Lock()
		delete(a.bgJobs, id)
		a.bgMu.Unlock()
	}

	text := strings.TrimRight(strings.Join(body, "\n"), "\n")
	if strings.TrimSpace(text) == "" {
		text = "(no output captured yet)"
	}
	elapsed := time.Since(job.started).Round(time.Second)
	nextPoll := backgroundJobPollInterval(job.pollCount + 1)
	footer := fmt.Sprintf("[background job %s: %s, %s elapsed]", id, state, elapsed)
	if state == "done" {
		footer += " (job cleared)"
	} else {
		footer += fmt.Sprintf(" (next poll in %s; then use {\"job\":\"%s\"})", nextPoll, id)
		if verbose {
			footer += fmt.Sprintf(" [verbose tail=%d bytes log=%s pidfile=%s command=%q]", tailBytes, job.log, job.pidfile, job.command)
		}
	}

	a.bgMu.Lock()
	if live := a.bgJobs[id]; live != nil {
		live.lastPoll = now
		live.pollCount++
		live.lastState = state
		live.lastOutput = text
		job = live
	}
	a.bgMu.Unlock()
	job.lastState = state
	job.lastOutput = text
	a.emitBackgroundJobUpdate(job, state, text, nextPoll, verbose)
	return text + "\n\n" + footer, nil
}

// startShellKeepalive holds the WSL2 distro VM warm for the duration of an agent
// run so commands don't pay the multi-second cold-boot penalty after the VM idles
// out (e.g. while a slow local model thinks between tool calls). It launches one
// detached `sleep` inside the distro and returns a stop func that kills it.
//
// It is a no-op unless we're on Windows with the WSL backend in isolated mode —
// shared_terminal mode already keeps a warm PTY session open, and other backends
// have no VM to keep warm.
func (a *App) startShellKeepalive(cfg *settings.Settings) func() {
	if runtime.GOOS != "windows" || cfg == nil {
		return func() {}
	}
	if resolveSharedTerminalBackend(cfg.Tools.ShellBackend) != "wsl" {
		return func() {}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Tools.ShellMode), "shared_terminal") {
		return func() {}
	}
	args := []string{}
	if distro := strings.TrimSpace(cfg.Tools.ShellDistro); distro != "" {
		args = append(args, "-d", distro)
	}
	if user := strings.TrimSpace(cfg.Tools.ShellUser); user != "" {
		args = append(args, "--user", user)
	}
	args = append(args, "--", "sleep", "86400")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	hideShellWindow(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		return func() {}
	}
	go func() { _ = cmd.Wait() }()
	return cancel
}

// wireBackgroundJobObserver mirrors standalone (isolated/PowerShell-backend)
// background jobs into the Jobs tab. The shared-terminal path emits job updates
// directly; this covers the path that runs jobs outside that session so both show
// up identically in the UI.
func (a *App) wireBackgroundJobObserver() {
	tools.OnBackgroundJobUpdate = func(u tools.BackgroundJobUpdate) {
		if a.ctx == nil {
			return
		}
		payload := map[string]interface{}{
			"id":              u.ID,
			"command":         u.Command,
			"state":           firstNonEmpty(u.State, "running"),
			"log":             u.Log,
			"pidfile":         "",
			"elapsed_sec":     u.ElapsedSec,
			"next_poll_sec":   u.NextPollSec,
			"verbose":         u.Verbose,
			"updated_at_unix": time.Now().UnixMilli(),
		}
		if u.Done {
			payload["exit_code"] = u.ExitCode
		}
		if strings.TrimSpace(u.Output) != "" {
			payload["output"] = trimRunText(u.Output)
		}
		a.emit("mauler:job_update", payload)
	}
}

func (a *App) emitBackgroundJobUpdate(job *bgJob, state, output string, nextPoll time.Duration, verbose bool) {
	if a == nil || a.ctx == nil || job == nil {
		return
	}
	elapsed := time.Since(job.started).Round(time.Second)
	payload := map[string]interface{}{
		"id":              job.id,
		"command":         job.command,
		"state":           firstNonEmpty(state, job.lastState, "running"),
		"log":             job.log,
		"pidfile":         job.pidfile,
		"elapsed_sec":     int(elapsed.Seconds()),
		"next_poll_sec":   int(nextPoll.Round(time.Second).Seconds()),
		"verbose":         verbose,
		"updated_at_unix": time.Now().UnixMilli(),
	}
	if strings.TrimSpace(output) != "" {
		payload["output"] = trimRunText(output)
	} else if strings.TrimSpace(job.lastOutput) != "" {
		payload["output"] = trimRunText(job.lastOutput)
	}
	a.emit("mauler:job_update", payload)
}

func backgroundJobPollInterval(polls int) time.Duration {
	switch {
	case polls <= 0:
		return time.Second
	case polls == 1:
		return 2 * time.Second
	case polls == 2:
		return 3 * time.Second
	case polls == 3:
		return 5 * time.Second
	case polls == 4:
		return 8 * time.Second
	case polls <= 7:
		return 13 * time.Second
	default:
		return 30 * time.Second
	}
}

func backgroundJobPollWait(job *bgJob, now time.Time) (time.Duration, bool) {
	if job == nil || job.lastPoll.IsZero() {
		return 0, false
	}
	interval := backgroundJobPollInterval(job.pollCount)
	elapsed := now.Sub(job.lastPoll)
	if elapsed >= interval {
		return 0, false
	}
	return (interval - elapsed).Round(time.Second), true
}

func formatBackgroundJobTooEarly(job *bgJob, wait time.Duration) string {
	if wait < time.Second {
		wait = time.Second
	}
	elapsed := time.Since(job.started).Round(time.Second)
	state := firstNonEmpty(job.lastState, "running")
	text := strings.TrimSpace(job.lastOutput)
	if text == "" {
		text = "(no new poll; previous output not available yet)"
	}
	return fmt.Sprintf("%s\n\n[background job %s: %s, %s elapsed] poll skipped: too early; wait %s before polling again. Backoff schedule: 1s, 2s, 3s, 5s, 8s, 13s, then 30s. Use {\"job\":\"%s\",\"verbose\":true} when you need a larger output tail.",
		text, job.id, state, elapsed, wait, job.id)
}

func (a *App) killSharedTerminalSession(sess *shellSession) {
	a.shellMu.Lock()
	if a.shellSess != nil && a.shellSess.id == sess.id {
		a.shellSess.cancel()
		a.shellSess = nil
	}
	a.shellMu.Unlock()
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

func containsShellHeredoc(command string) bool {
	for _, line := range strings.Split(command, "\n") {
		for i := 0; i < len(line)-1; i++ {
			if line[i] != '<' || line[i+1] != '<' || shellIndexInQuotes(line, i) {
				continue
			}
			return true
		}
	}
	return false
}

func isShellJobsCommand(command string) bool {
	lower := strings.TrimSpace(strings.ToLower(command))
	return lower == "job" || lower == "jobs" || lower == "jobs -l" || lower == "jobs -p"
}

func shellAssignmentJobID(command string) string {
	trimmed := strings.TrimSpace(command)
	re := regexp.MustCompile(`(?is)^job\s*=\s*['"]?(j[0-9]+)['"]?\s*$`)
	match := re.FindStringSubmatch(trimmed)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func stripTrailingBackgroundOperator(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || !strings.HasSuffix(trimmed, "&") {
		return command, false
	}
	idx := len(trimmed) - 1
	if shellIndexInQuotes(trimmed, idx) {
		return command, false
	}
	prev := idx - 1
	for prev >= 0 && (trimmed[prev] == ' ' || trimmed[prev] == '\t') {
		prev--
	}
	if prev >= 0 && (trimmed[prev] == '&' || trimmed[prev] == '|') {
		return command, false
	}
	stripped := strings.TrimSpace(trimmed[:idx])
	if stripped == "" {
		return command, false
	}
	return stripped, true
}

func shellIndexInQuotes(line string, idx int) bool {
	var quote rune
	escaped := false
	for pos, r := range line {
		if pos >= idx {
			return quote != 0
		}
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
		}
	}
	return quote != 0
}

func sharedTerminalWrapper(command, runID string) (string, string, string) {
	start := "__MAULER_START_" + runID + "__"
	donePrefix := "__MAULER_DONE_" + runID + ":"
	// Build the marker prefix from a shell variable whose *source text* never
	// contains the literal "__MAULER_" (the '' splits it). The echoed command can
	// therefore never carry a literal marker, so even when the terminal wraps the
	// echoed command across rows, no fragment is mistaken for a real
	// START/DONE/CWD line — only the printf OUTPUT (after expansion) carries the
	// literal markers. This is what makes the parser robust to echo wrapping.
	wrapped := fmt.Sprintf("M=__MA''ULER_; set -o pipefail 2>/dev/null || true; printf '%%s\\n' \"${M}START_%s__\"; { %s; }; status=$?; printf '%%s\\n' \"${M}CWD_%s__$PWD\"; printf '%%s%%s\\n' \"${M}DONE_%s:\" \"$status\"",
		runID, command, runID, runID)
	return start, donePrefix, wrapped
}

func writeSharedTerminalWrappedCommand(sess *shellSession, wrapped string) (int, error) {
	// Leave terminal echo ON and let the filters hide the plumbing (this matches
	// the design note in runSharedTerminalShell). We must NOT prepend
	// `stty -echo` here: the TTY line discipline echoes the wrapper bytes that
	// arrive in the same write burst *before* the shell executes `stty -echo`, so
	// echo-off only kicks in partway through the wrapper assignment. That ate a
	// variable-length prefix of `M=__MA''ULER_` (leaving `MA''ULER_;`, `LER_;`, …),
	// which no longer matched markerEchoSig and leaked into the visible terminal.
	// With echo left on, the full `M=__MA''ULER_…` line is echoed intact and the
	// uiMarkerFilter drops it; the agent-side parser ignores it via the START gate.
	return io.WriteString(sess.input, wrapped+"\r")
}

// sharedTerminalParser classifies the lines that come back while a wrapped
// command runs in the shared shell. Because the wrapper builds markers from a
// shell variable, a literal __MAULER_ marker appears only in real printf OUTPUT,
// never in the echoed command — so the echoed command (even if wrapped across
// rows) flips nothing and is naturally ignored, since `started` only turns on at
// the real START output line.
type sharedTerminalParser struct {
	startMarker string
	donePrefix  string
	runID       string
	started     bool
	out         []terminalOutput
	cwd         string
}

func newSharedTerminalParser(startMarker, donePrefix, runID string) *sharedTerminalParser {
	return &sharedTerminalParser{startMarker: startMarker, donePrefix: donePrefix, runID: runID}
}

// feed processes one channel line, returning done=true with the exit code only
// when the real numeric DONE marker arrives.
func (p *sharedTerminalParser) feed(line terminalOutput) (done bool, exitCode int) {
	data := line.data
	if strings.Contains(data, p.startMarker) {
		p.started = true
		return false, 0
	}
	if cwd, ok := sharedTerminalCWD(data, p.runID); ok {
		p.cwd = cwd
		return false, 0
	}
	if statusText, preDone, ok := splitSharedTerminalDone(data, p.donePrefix); ok {
		code, err := parseSharedTerminalExitCode(statusText)
		if err != nil {
			// Not the real DONE — a stray/echoed fragment. Ignore and keep waiting.
			return false, 0
		}
		if p.started && strings.TrimSpace(preDone) != "" {
			p.out = append(p.out, terminalOutput{data: strings.TrimSpace(preDone), stream: line.stream})
		}
		return true, code
	}
	// Don't capture pre-START echo or any residual marker text.
	if !p.started || strings.Contains(data, "__MAULER_") {
		return false, 0
	}
	p.out = append(p.out, line)
	if len(p.out) > 2000 {
		p.out = p.out[len(p.out)-2000:]
	}
	return false, 0
}

// sharedTerminalCWD extracts the working directory from a __MAULER_CWD_ line, or
// returns "" if the line isn't a cwd marker.
func sharedTerminalCWD(line, runID string) (string, bool) {
	prefix := "__MAULER_CWD_" + runID + "__"
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return "", false
	}
	return strings.TrimRight(line[idx+len(prefix):], "\r\n"), true
}

// withSharedTerminalCWD appends the session's working directory to a result so
// the agent stays oriented after commands that cd around.
func withSharedTerminalCWD(result, cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return result
	}
	return result + "\ncwd: " + cwd
}

func terminalShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func isSharedTerminalWrapperEcho(line string) bool {
	// Truncation-proof signatures (see isWrapperEchoLine): a literal `${M}` or the
	// pipefail body only appear in an echoed-but-unrun wrapper, never in real output.
	if isWrapperEchoLine([]byte(line)) {
		return true
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "stty -echo 2>/dev/null || true" || trimmed == "stty echo 2>/dev/null || true" {
		return true
	}
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

func parseSharedTerminalExitCode(statusText string) (int, error) {
	codeText := strings.TrimSpace(statusText)
	exitCode, err := strconv.Atoi(codeText)
	if err != nil {
		return -1, fmt.Errorf("shared terminal: invalid exit code %q", codeText)
	}
	return exitCode, nil
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
	a.recordLedger(ledger.Event{
		Kind:    "shell_close",
		Source:  "terminal",
		Status:  "requested",
		Message: id,
	})
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
	sb.WriteString("For substantial multi-step implementation, debugging, research, review, or Ops tasks, first call todo_create with a concise 3-8 step plan, then use todo_update/todo_done/todo_blocked as phases change. ")
	sb.WriteString("If the user asks to update a README, writeup, notes file, report, or documentation as work progresses, treat that file as a living artifact: write a concise update after each major verified milestone instead of leaving all documentation until the end. ")
	sb.WriteString("Use session_search when the user asks about prior work, past decisions, remembered fixes, or anything likely discussed in an earlier chat. ")
	sb.WriteString("Use skills_list at the start of a complex task to see if a relevant procedural skill exists, then skill_view to read its full instructions. ")
	sb.WriteString("Use set_reasoning_effort to control thinking depth as the task changes: minimal or low for rote reads, small edits, formatting, and running known commands; medium for normal implementation; high for ambiguous design, debugging, exploitation reasoning, or complex reviews. Do not change effort more than a few times per task. ")
	sb.WriteString("Use http_probe for bounded HTTP header/path probing when you would otherwise run several similar curl commands; it returns a compact summary and artifact path. ")
	if hint := masterSkillRegistryHint(); hint != "" {
		sb.WriteString(hint)
	}
	sb.WriteString("Prefer glob/grep/file_outline/read_chunks/read_file/read_many/read_pdf for file discovery and inspection instead of shell. Use file_outline before reading large files, then read_chunks or read_file line ranges for only the needed sections. Use read_pdf for local PDF documents the user wants analysed. For large codebase mapping, broad enumeration, or read-heavy inspection tasks, call subagent_explore and work from its bounded summary instead of pulling many files into the main context. ")
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
		sb.WriteString("For HTB/Kali target interaction, prefer the shell tool for target HTTP/TCP work because it runs inside WSL/Kali with that distro's /etc/hosts, VPN routing, and tooling. Use curl, nc, nmap, ffuf, gobuster, and similar tools from shell for .htb hosts and lab IPs. fetch_url and browser tools run from the Windows host and may not share WSL DNS, hosts entries, VPN routes, or Kali tools; use them for public internet research or only when the user explicitly needs a visible Windows browser. ")
		sb.WriteString("For HTB/CTF enumeration, use realistic shell timeouts: 120-300 seconds for nmap, gobuster, ffuf, hydra, and similar scans. Do not pipe long-running scans through head because it can terminate the scan early and hide the real exit status; write scan output to a file with -oA/-oN/-oG or tee, then tail/grep the saved file afterward. If a scan times out, retry narrower or with a larger timeout and continue from partial output instead of stopping. For long scans in shared-terminal mode, prefer background=true and poll the returned job id with backoff: wait about 1s, 2s, 3s, 5s, 8s, 13s, then 30s between polls; use verbose=true only when fuller output is needed. Sudo is allowed when the user supplied the WSL/Kali sudo password, but it must be non-interactive: pass the password through stdin with sudo -S and a bounded timeout. For privileged one-line writes such as /etc/hosts, prefer printf piped into sudo -S tee -a instead of nested sudo bash -c quoting. ")
	}
	if strings.EqualFold(mode.Name, "Ops") {
		switch normaliseOpsProfile(cfg.Context.Lab.OpsProfile) {
		case "htb":
			sb.WriteString("Ops profile: HTB/CTF. Assist the authorised box workflow from WSL/Kali: recon, foothold, user, privilege escalation, root, and writeup. Flag discovery is allowed as evidence for the exercise, but still verify live target state before relying on old notes or public PoCs. ")
		default:
			sb.WriteString("Ops profile: Pentesting. Assist authorised attack/testing and evidence capture only. Track assets, services, suspected vulnerabilities, requests/responses, PoC verification, screenshots, impact notes, and report-ready evidence. Do not perform remediation, patching, hardening, or client-system fixes unless the user explicitly changes the task. Do not frame objectives as user/root flags. ")
		}
		sb.WriteString("Ops mode: treat prior run memories, old writeups, CVE names, and public PoCs as hypotheses until live target evidence confirms them. First reconcile the requested target IP/hostname with the writeup and /etc/hosts, then verify services and versions from the target. Do not choose an exploit only because a memory or old note mentions it, and do not write or paste a large public exploit before a small proof check shows the endpoint and parameters match this host. ")
		sb.WriteString("Ops mode: when a CVE, public exploit, or named product vulnerability is central to the task and the user has not explicitly forbidden web research, do one fresh current-source pass before committing to an exploit path: search with the current year/date plus product/version/CVE terms, fetch at least one primary or high-quality source, and compare publication/update dates against the target version. If web tools are unavailable, state that the exploit choice is based only on local evidence/memory. ")
		if masterSkillRegistryHint() != "" {
			sb.WriteString("Ops mode: for complex recon, exploitation, foothold, privilege escalation, or report/evidence tasks, call skill_view with name `master` and a focused query early in the run. Use the relevant workflow steps from that skill instead of repeatedly guessing commands. ")
		}
		sb.WriteString("Ops mode: if the same shell command returns the same response twice, stop repeating it, record the evidence, and vary only one thing or summarize the finding. For repeated HTTP curl probes, prefer http_probe so raw output goes to an artifact and the prompt only gets a summary. ")
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
		sb.WriteString("The plan should be visible through the todo tools, not only prose in chat. ")
	}
	sb.WriteString("Work deliberately, verify important claims with evidence, and prefer targeted edits over rewrites.")
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
		writeMemoryPromptPackets(&sb, memories)
	}
	if cfg.Memory.Enabled {
		sb.WriteString("\n\nYou have a memory tool for this workspace. Call memory with action=recall (a short query) before repeating work to check what is already known, and action=remember to save a reusable lesson, working command, user preference, or confirmed target detail. Only the highest-scoring entries are auto-injected above, so recall when you need more; keep remembered entries short and factual and never store secrets. ")
	}
	sb.WriteString(buildProjectInstructionsPrompt(cfg.Context))
	if userProfile := loadUserProfile(); userProfile != "" {
		sb.WriteString("\n\nUser profile:\n")
		sb.WriteString(userProfile)
	}
	return sb.String()
}

func writeMemoryPromptPackets(sb *strings.Builder, memories []MemoryEntry) {
	type packet struct {
		title string
		items []MemoryEntry
	}
	packets := []packet{
		{title: "Relevant project memory - user preferences and constraints:"},
		{title: "Relevant project memory - confirmed facts and decisions:"},
		{title: "Relevant project memory - previous run recall:"},
		{title: "Relevant project memory - unverified or stale, verify before use:"},
		{title: "Relevant prior-session pointers - compact recall only:"},
		{title: "Relevant evidence pointers - inspect artifacts/files before relying on details:"},
	}
	for _, memory := range memories {
		switch memoryLayer(memory) {
		case "session_recall":
			packets[4].items = append(packets[4].items, memory)
		case "evidence":
			packets[5].items = append(packets[5].items, memory)
		case "unverified":
			packets[3].items = append(packets[3].items, memory)
		case "preferences":
			packets[0].items = append(packets[0].items, memory)
		case "previous_run":
			packets[2].items = append(packets[2].items, memory)
		default:
			packets[1].items = append(packets[1].items, memory)
		}
	}
	for _, packet := range packets {
		if len(packet.items) == 0 {
			continue
		}
		sb.WriteString("\n\n" + packet.title + "\n")
		for _, memory := range packet.items {
			writeMemoryPromptLine(sb, memory)
		}
	}
}

func writeMemoryPromptLine(sb *strings.Builder, memory MemoryEntry) {
	sb.WriteString("- ")
	sb.WriteString(memoryPromptPrefix(memory))
	if memory.Title != "" {
		sb.WriteString(memory.Title + ": ")
	}
	sb.WriteString(strings.TrimSpace(memory.Content))
	meta := []string{}
	if memory.Kind != "" && normaliseMemoryKind(memory.Kind) != "note" {
		meta = append(meta, "kind="+normaliseMemoryKind(memory.Kind))
	}
	if memory.Confidence != "" {
		meta = append(meta, "confidence="+normaliseMemoryConfidence(memory.Confidence))
	}
	if memory.Source != "" {
		meta = append(meta, "source="+normaliseMemorySource(memory.Source))
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

func masterSkillRegistryHint() string {
	skill, err := loadSkill("master")
	if err != nil || strings.TrimSpace(skill.SourcePath) == "" {
		return ""
	}
	return "A master workflow skill is already registered as skill `master`; when the task mentions master_skill, master skill, navigator, methodology, or workflow guidance, call skill_view with name `master` instead of searching the workspace for master_skill.md or master_skills.md. "
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

// maxPreOutputInferenceRetries bounds how many times a chat/stream request is
// retried when it fails before any model output. A managed local bridge can be
// unavailable while it restarts llama-server, waits for a health check, or
// reloads a 27B model. That window can last well over a minute, so ride it out
// instead of killing an otherwise healthy agent run on the first connect reset.
const maxPreOutputInferenceRetries = 15

// sleepBeforeInferenceRetry waits with exponential backoff (1s, 2s, 4s, 8s,
// capped at 15s) before the next attempt, returning false if the run is
// cancelled while waiting.
func sleepBeforeInferenceRetry(ctx context.Context, attempt int) bool {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << (attempt - 1)
	if delay > 15*time.Second {
		delay = 15 * time.Second
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
		"forcibly closed",
		"wsasend",
		"i/o timeout",
		"connection aborted",
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
		if invalidInlineToolCall(call) {
			continue
		}
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

func invalidInlineToolCall(call llm.ToolCallDef) bool {
	var args map[string]interface{}
	if err := json.Unmarshal(call.Function.Arguments, &args); err != nil {
		return true
	}
	if isShellTool(call.Function.Name) {
		if raw, ok := args["command"]; ok {
			command := strings.TrimSpace(fmt.Sprint(raw))
			if command == "" || command == "<" || command == ">" || strings.HasPrefix(command, "<|") {
				return true
			}
		}
		for key := range args {
			if strings.Contains(key, "<|") || strings.Contains(key, "|>") {
				return true
			}
		}
	}
	return false
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
	text = normalizeGemmaInlineToolArgs(text)
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

func normalizeGemmaInlineToolArgs(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	text = strings.ReplaceAll(text, `<|"|>`, `"`)
	text = strings.ReplaceAll(text, `<|'|>`, `'`)
	text = strings.ReplaceAll(text, "<|`|>", "`")
	return text
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
	if isOperationalTargetTask(lower) {
		return false
	}
	return hasAny(lower,
		"repo", "repository", "codebase", "code base", "project", "workspace",
		"files", "file tree", "directory", "folder",
		"look at", "look into", "inspect", "read", "explore", "what's in", "whats in",
	)
}

func needsOperationalTool(text string) bool {
	return isOperationalTargetTask(strings.ToLower(text))
}

func isOperationalTargetTask(lower string) bool {
	return hasAny(lower,
		"htb", "hackthebox", "ctf", "kali", "wsl", "pentest", "penetration test",
		"target ip", "target url", ".htb", "foothold", "privesc", "privilege escalation",
		"user flag", "root flag", "nmap", "ffuf", "gobuster", "burp", "freepbx",
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
	// First turn of a task that clearly needs a tool before anything useful can be
	// said — repository inspection or an operational/HTB target workflow — force a
	// tool call so the model cannot spend the opening turn narrating what it is
	// about to do without doing it. Mirrors the recovery prompts that already
	// special-case these intents (needsInspectionTool / needsOperationalTool).
	if needsInspectionTool(firstUserText) || needsOperationalTool(firstUserText) {
		return "required"
	}
	return "auto"
}

func toolDefsAndChoiceForTurn(registry *tools.Registry, cfg settings.ToolsConfig, firstUserText string, autoContinues int, totalToolCallsMade int) ([]llm.ToolDef, string) {
	toolChoice := toolChoiceFor(firstUserText, autoContinues, totalToolCallsMade)
	if !cfg.Enabled || toolChoice == "none" {
		return nil, toolChoice
	}
	enabled := settings.EffectiveEnabledTools(cfg)
	if shouldHideHostResearchTools(cfg, firstUserText) {
		enabled = cloneToolEnabledMap(enabled)
		for _, name := range []string{
			"web_search", "fetch_url",
			"browser_open", "browser_snapshot", "browser_click", "browser_type", "browser_extract", "browser_screenshot", "browser_close", "browser_agent",
		} {
			enabled[name] = false
		}
		enabled["subagent_research"] = false
	}
	defs := registry.ToEnabledToolDefs(enabled)
	if enabled[reasoningEffortToolName] {
		defs = appendReasoningEffortToolDef(defs)
	}
	// A "required" choice with no tools to call is invalid and strict
	// OpenAI-compatible backends reject it — downgrade to a plain text turn.
	if len(defs) == 0 && toolChoice == "required" {
		toolChoice = "none"
	}
	return defs, toolChoice
}

func shouldHideHostResearchTools(cfg settings.ToolsConfig, firstUserText string) bool {
	if !looksShellCentricTask(firstUserText) || explicitWebResearchIntent(firstUserText) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ActiveToolset)) {
	case "unrestricted", "web-research", "browser":
		return false
	}
	return true
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
	if hasAny(lower,
		"htb", "hackthebox", "hack the box",
		"hack the target", "hacking the target", "target and get user", "get user and root",
		"pentest", "penetration test", "lab target", "target ip", "target url",
		"wsl", "kali", "vpn", "tun0", "sudo", "nmap", "gobuster", "ffuf", "feroxbuster", "dirsearch", "nikto", "searchsploit",
		"user.txt", "root.txt", "user flag", "root flag", "get user", "get root", "foothold", "privilege escalation", "privesc", "recon", "enumerate", "enumeration",
		"dvwa", "command injection", "cmd injection", "sql injection", "sqli", "xss", "lfi", "rfi", "ssrf",
	) {
		return true
	}
	if explicitWebResearchIntent(lower) {
		return false
	}
	return hasOffensiveActionIntent(lower) && !looksCodebaseTask(lower)
}

func hasOffensiveActionIntent(lower string) bool {
	return hasAny(lower,
		"exploit", "exploit chain", "weaponize", "payload", "webshell", "web shell",
		"reverse shell", "bind shell", "rce", "foothold", "privilege escalation", "privesc",
		"get user", "get root", "user flag", "root flag", "shell access", "listener",
	)
}

func looksCodebaseTask(lower string) bool {
	return hasAny(lower,
		"repo", "repository", "codebase", "code base", "project", "workspace",
		"app", "application", "feature", "ui", "frontend", "backend",
		"implement", "patch", "refactor", "test suite", "build error",
	)
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

func agentTimeBudgetExhausted(cfg settings.AgentsConfig, startedAt, now time.Time) bool {
	return cfg.MaxRunSeconds > 0 && now.Sub(startedAt) > time.Duration(cfg.MaxRunSeconds)*time.Second
}

func agentToolBudgetSummaryPrompt(maxToolCalls int) string {
	return fmt.Sprintf(
		"Agent tool-call budget is exhausted (%d calls). Do not emit tool calls or tool markup. "+
			"Write a concise final progress summary for the user now: what was attempted, what worked, what failed, current evidence, and the safest next manual step. "+
			"If more work is needed, ask the user before continuing.",
		maxToolCalls,
	)
}

func agentTimeBudgetSummaryPrompt(maxRunSeconds int) string {
	return fmt.Sprintf(
		"Agent wall-clock time budget is exhausted (%d seconds). Do not emit tool calls or tool markup. "+
			"Write a concise final progress summary for the user now: what was attempted, what worked, what failed, current evidence, and the safest next manual step. "+
			"If more work is needed, ask the user before continuing.",
		maxRunSeconds,
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
