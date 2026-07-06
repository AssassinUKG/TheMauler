package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"mauler/internal/channelbus"
	"mauler/internal/ledger"
	"mauler/internal/llm"
	"mauler/internal/settings"
)

type ChannelEnvelope = channelbus.Envelope
type ChannelAttachment = channelbus.Attachment
type ChannelRoute = channelbus.Route
type ChannelResponse = channelbus.Response
type ChannelWorkItem = channelbus.WorkItem

func (a *App) DispatchChannelMessage(env ChannelEnvelope) (ChannelResponse, error) {
	env = channelbus.NormalizeEnvelope(env)
	route := channelbus.RouteEnvelope(env)
	a.recordChannelEvent("channel_message_in", env, route, "")
	switch route.Lane {
	case channelbus.LaneSideChat:
		if a.isAgentRunning() {
			item := a.ensureChannelQueue().Enqueue(env, route)
			resp := channelbus.Response{
				Lane:    route.Lane,
				Status:  "queued_busy",
				Message: "Project run is active, so I queued this side-chat message and will answer it as soon as the run is idle.",
				Queued:  true,
				QueueID: item.ID,
			}
			a.recordChannelEvent("channel_message_out", env, route, resp.Message)
			return resp, nil
		}
		resp := a.handleChannelSideChat(env, route)
		a.recordChannelEvent("channel_message_out", env, route, resp.Message)
		return resp, nil
	case channelbus.LaneControl:
		resp, err := a.handleChannelControl(env, route)
		if err != nil {
			a.recordChannelEvent("channel_error", env, route, err.Error())
			return resp, err
		}
		a.recordChannelEvent("channel_message_out", env, route, resp.Message)
		return resp, nil
	case channelbus.LaneQuick:
		resp, err := a.handleChannelQuickAction(env, route)
		if err != nil {
			a.recordChannelEvent("channel_error", env, route, err.Error())
			return resp, err
		}
		a.recordChannelEvent("channel_message_out", env, route, resp.Message)
		return resp, nil
	case channelbus.LaneWork:
		resp, err := a.handleChannelWork(env, route)
		if err != nil {
			a.recordChannelEvent("channel_error", env, route, err.Error())
			return resp, err
		}
		a.recordChannelEvent("channel_message_out", env, route, resp.Message)
		return resp, nil
	case channelbus.LaneInterrupt, channelbus.LaneNote:
		item := a.ensureChannelQueue().Enqueue(env, route)
		resp := channelbus.Response{
			Lane:    route.Lane,
			Status:  "queued",
			Message: fmt.Sprintf("Queued %s for the active run. It will not interrupt unless explicitly consumed.", route.Lane),
			Queued:  true,
			QueueID: item.ID,
		}
		a.recordChannelEvent("channel_message_out", env, route, resp.Message)
		return resp, nil
	default:
		resp := channelbus.Response{Lane: route.Lane, Status: "unknown_command", Message: "Unknown remote command. Use /help for available commands."}
		a.recordChannelEvent("channel_message_out", env, route, resp.Message)
		return resp, nil
	}
}

func (a *App) handleChannelQuickAction(env channelbus.Envelope, route channelbus.Route) (channelbus.Response, error) {
	a.mu.Lock()
	running := a.agentRunning
	a.mu.Unlock()
	if running {
		item := a.ensureChannelQueue().Enqueue(env, route)
		return channelbus.Response{
			Lane:    route.Lane,
			Status:  "queued_busy",
			Message: "Agent run is active, so this quick action was queued instead of typing over it.",
			Queued:  true,
			QueueID: item.ID,
		}, nil
	}
	switch route.Command {
	case "quick_terminal":
		command, label := quickTerminalCommand(route.Argument)
		if command == "" {
			return channelbus.Response{Lane: route.Lane, Status: "unsupported", Message: "I recognised a quick terminal request, but do not have a deterministic command for it yet. Use /run <task> for the full agent."}, nil
		}
		raw, _ := json.Marshal(terminalRunArgs{Command: command, WaitFor: "output", Lines: 60})
		result, err := (&terminalRunTool{app: a}).Run(context.Background(), raw)
		if err != nil {
			return channelbus.Response{Lane: route.Lane, Status: "error", Message: "Quick terminal action failed: " + err.Error()}, nil
		}
		return channelbus.Response{
			Lane:    route.Lane,
			Status:  "done",
			Message: fmt.Sprintf("Quick terminal action: %s\ncommand: %s\n\n%s", label, command, truncateRunes(result, 1800)),
			Data: map[string]string{
				"command": command,
				"label":   label,
			},
		}, nil
	default:
		return channelbus.Response{Lane: route.Lane, Status: "unsupported", Message: "Unknown quick action. Use /run <task> for full agent work."}, nil
	}
}

func quickTerminalCommand(text string) (command, label string) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if !strings.Contains(lower, "tmux") {
		return "", ""
	}
	session := quickTmuxSessionName(text)
	if session == "" {
		session = "mauler"
	}
	return "tmux new-session -A -s " + session, "tmux session " + session
}

var tmuxSessionNameRE = regexp.MustCompile(`(?i)(?:session\s+(?:called|named)\s+|named\s+)([a-z0-9_.-]{2,32})`)

func quickTmuxSessionName(text string) string {
	m := tmuxSessionNameRE.FindStringSubmatch(text)
	if len(m) > 1 {
		return sanitizeTmuxSessionName(m[1])
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "htb"):
		return "htb"
	case strings.Contains(lower, "kali"):
		return "kali"
	default:
		return "mauler"
	}
}

func sanitizeTmuxSessionName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			sb.WriteRune(r)
		}
	}
	return strings.Trim(sb.String(), ".-_")
}

func (a *App) ListChannelWorkQueue() []ChannelWorkItem {
	return a.ensureChannelQueue().ListActive()
}

func (a *App) GetChannelBusStatus() map[string]string {
	a.mu.Lock()
	running := a.agentRunning
	mode := a.currentMode
	profile := ""
	workspace := ""
	if a.cfg != nil {
		profile = a.cfg.ActiveProfile
		workspace = a.cfg.Context.WorkspaceDir
	}
	a.mu.Unlock()
	queue := a.ensureChannelQueue().ListActive()
	out := map[string]string{
		"agent_running": fmt.Sprintf("%v", running),
		"mode":          mode,
		"profile":       profile,
		"workspace":     workspace,
		"queued_work":   fmt.Sprintf("%d", len(queue)),
	}
	for key, value := range a.telegramRuntimeDiagnostics() {
		out[key] = value
	}
	return out
}

func (a *App) handleChannelSideChat(env channelbus.Envelope, route channelbus.Route) channelbus.Response {
	reply, err := a.runChannelSideChat(env, route)
	if err != nil {
		reply = "I received that, but the side-chat model call failed: " + err.Error() + "\n\nUse /run <task> if you want me to start an unrestricted project agent run."
	}
	return channelbus.Response{
		Lane:    route.Lane,
		Status:  "chat",
		Message: reply,
		Data: map[string]string{
			"session_id": env.SessionID,
			"reason":     route.Reason,
		},
	}
}

func (a *App) runChannelSideChat(env channelbus.Envelope, route channelbus.Route) (string, error) {
	text := strings.TrimSpace(route.Argument)
	if text == "" {
		text = strings.TrimSpace(env.Text)
	}
	if text == "" {
		return "I received an empty message. Send text, voice with transcription configured, or use /run <task>.", nil
	}
	cfg, pf, profile, err := a.sideChatProfile()
	if err != nil {
		return "", err
	}
	client, err := buildClientForAgent(profile)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Now()
	a.recordChannelSideChatModelEvent("channel_sidechat_model_start", env, route, profile, 0, 0, "")
	if err := a.ensureModelLoaded(ctx, client, profile); err != nil {
		a.recordChannelSideChatModelEvent("channel_sidechat_model_error", env, route, profile, time.Since(start), 0, err.Error())
		return "", err
	}
	sessionID := strings.TrimSpace(env.SessionID)
	if sessionID == "" {
		sessionID = env.Source + ":default"
	}
	msgs := a.sideChatMessages(sessionID, env, text, cfg, pf)
	reply, err := a.runSideChatCompletion(ctx, client, profile, msgs)
	if err != nil {
		a.recordChannelSideChatModelEvent("channel_sidechat_model_error", env, route, profile, time.Since(start), 0, err.Error())
		return "", err
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		retryMsgs := []llm.Message{
			llm.NewTextMessage(llm.RoleSystem, "Reply directly in one or two short sentences. Do not use tools. Do not output hidden thinking."),
			llm.NewTextMessage(llm.RoleUser, text),
		}
		reply, err = a.runSideChatCompletion(ctx, client, profile, retryMsgs)
		if err != nil {
			a.recordChannelSideChatModelEvent("channel_sidechat_model_error", env, route, profile, time.Since(start), 0, err.Error())
			return "", err
		}
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		reply = a.sideChatFallbackReply(text)
		a.recordChannelSideChatModelEvent("channel_sidechat_model_empty", env, route, profile, time.Since(start), 0, "empty visible assistant response after retry")
	}
	a.recordChannelSideChatModelEvent("channel_sidechat_model_done", env, route, profile, time.Since(start), len(reply), reply)
	a.appendSideChatTurn(sessionID, text, reply)
	return reply, nil
}

func (a *App) runSideChatCompletion(ctx context.Context, client llm.Client, profile settings.Profile, msgs []llm.Message) (string, error) {
	params := profile.NoThink
	if params.MaxTokens <= 0 {
		params = profile.ActiveParams(false)
	}
	req := llm.Request{
		Messages:         msgs,
		MaxTokens:        sideChatMaxTokens(profile),
		Temperature:      params.Temperature,
		TopP:             params.TopP,
		TopK:             params.TopK,
		MinP:             params.MinP,
		PresencePenalty:  params.PresencePenalty,
		Seed:             params.Seed,
		ToolChoice:       "none",
		EnableThinking:   false,
		PreserveThinking: false,
		ReasoningEffort:  "none",
	}
	stream, err := client.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for delta := range stream {
		if delta.Error != nil {
			return strings.TrimSpace(sb.String()), delta.Error
		}
		sb.WriteString(delta.Content)
	}
	return strings.TrimSpace(sb.String()), nil
}

func (a *App) sideChatProfile() (*settings.Settings, *settings.ProfilesFile, settings.Profile, error) {
	a.mu.Lock()
	cfg := a.cfg
	pf := a.profiles
	a.mu.Unlock()
	if cfg == nil {
		loaded, err := settings.Load()
		if err != nil {
			return nil, nil, settings.Profile{}, err
		}
		cfg = loaded
	}
	if pf == nil {
		loaded, err := settings.LoadProfiles()
		if err != nil {
			return nil, nil, settings.Profile{}, err
		}
		pf = loaded
	}
	ensureActiveProfile(cfg, pf)
	profile := activeProfile(cfg, pf)
	if strings.TrimSpace(profile.ModelID) == "" {
		return nil, nil, settings.Profile{}, fmt.Errorf("no active model profile configured")
	}
	return cfg, pf, profile, nil
}

func (a *App) sideChatMessages(sessionID string, env channelbus.Envelope, text string, cfg *settings.Settings, pf *settings.ProfilesFile) []llm.Message {
	system := "You are TheMauler's Telegram side-chat assistant. Reply conversationally and directly. " +
		"Keep this chat separate from the active project run. Do not claim to have run tools or changed files in side chat. " +
		"You may answer questions about current app/project status using the status packet below. " +
		"If the user wants real work, tell them to use /run <task>, /stop, /status, /facts, /plan, or /terminal. " +
		"Unrestricted project runs are allowed when the user uses /run; side chat itself is no-tool text chat."
	if cfg != nil {
		system += " Active profile: " + cfg.ActiveProfile + "."
	}
	system += "\n\nCurrent status packet:\n" + a.remoteStatusSummary()
	_ = pf
	msgs := []llm.Message{llm.NewTextMessage(llm.RoleSystem, system)}
	a.sideChatMu.Lock()
	history := append([]llm.Message(nil), a.sideChatHistories[sessionID]...)
	a.sideChatMu.Unlock()
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	msgs = append(msgs, history...)
	user := text
	if env.Username != "" || env.UserID != "" {
		user = fmt.Sprintf("Telegram user %s%s says:\n%s", env.Username, env.UserID, text)
	}
	msgs = append(msgs, llm.NewTextMessage(llm.RoleUser, user))
	return msgs
}

func (a *App) appendSideChatTurn(sessionID, userText, assistantText string) {
	a.sideChatMu.Lock()
	defer a.sideChatMu.Unlock()
	if a.sideChatHistories == nil {
		a.sideChatHistories = map[string][]llm.Message{}
	}
	history := a.sideChatHistories[sessionID]
	history = append(history, llm.NewTextMessage(llm.RoleUser, userText), llm.NewTextMessage(llm.RoleAssistant, assistantText))
	if len(history) > 16 {
		history = history[len(history)-16:]
	}
	a.sideChatHistories[sessionID] = history
}

func (a *App) sideChatFallbackReply(text string) string {
	lower := strings.ToLower(text)
	status := a.remoteStatusSummary()
	if strings.Contains(lower, "project") ||
		strings.Contains(lower, "status") ||
		strings.Contains(lower, "running") ||
		strings.Contains(lower, "queue") ||
		(strings.Contains(lower, "what") && strings.Contains(lower, "go")) {
		return "The local side-chat model returned no visible text, but I still have Mauler state:\n\n" + status + "\n\nUse /run <task> if you want me to start or queue project work."
	}
	return "I received that, but the local side-chat model returned no visible text. Telegram is still connected; use /run <task> for project work, or ask again and I will retry the side chat."
}

func sideChatMaxTokens(profile settings.Profile) int {
	params := profile.ActiveParams(false)
	if params.MaxTokens > 0 && params.MaxTokens < 900 {
		return params.MaxTokens
	}
	return 700
}

func (a *App) handleChannelControl(env channelbus.Envelope, route channelbus.Route) (channelbus.Response, error) {
	switch route.Command {
	case "status":
		return channelbus.Response{Lane: route.Lane, Status: "ok", Message: a.remoteStatusSummary()}, nil
	case "facts":
		return channelbus.Response{Lane: route.Lane, Status: "ok", Message: a.remoteFactsSummary()}, nil
	case "terminal", "terminal_read":
		state := a.GetSharedTerminalState()
		return channelbus.Response{Lane: route.Lane, Status: "ok", Message: formatRemoteTerminalState(state)}, nil
	case "stop":
		a.StopAgent()
		return channelbus.Response{Lane: route.Lane, Status: "stopping", Message: "Stop requested for the active project run."}, nil
	case "help":
		return channelbus.Response{Lane: route.Lane, Status: "ok", Message: remoteHelpText()}, nil
	case "plan", "logs", "brain":
		return channelbus.Response{Lane: route.Lane, Status: "ok", Message: a.remoteRecentRunSummary(route.Command)}, nil
	case "terminal_send":
		item := a.ensureChannelQueue().Enqueue(env, route)
		return channelbus.Response{Lane: route.Lane, Status: "queued", Message: "Queued terminal_send for explicit operator review/dispatch.", Queued: true, QueueID: item.ID}, nil
	default:
		return channelbus.Response{Lane: route.Lane, Status: "not_implemented", Message: "Command is routed cleanly but not implemented yet: /" + route.Command}, nil
	}
}

func (a *App) handleChannelWork(env channelbus.Envelope, route channelbus.Route) (channelbus.Response, error) {
	prompt := strings.TrimSpace(route.Argument)
	if prompt == "" {
		prompt = strings.TrimSpace(env.Text)
	}
	a.mu.Lock()
	running := a.agentRunning
	a.mu.Unlock()
	if running {
		item := a.ensureChannelQueue().Enqueue(env, route)
		return channelbus.Response{
			Lane:    route.Lane,
			Status:  "queued_busy",
			Message: "Project run is active, so this work request was queued instead of interrupting it.",
			Queued:  true,
			QueueID: item.ID,
		}, nil
	}
	if prompt == "" {
		return channelbus.Response{Lane: route.Lane, Status: "empty", Message: "No work prompt supplied."}, nil
	}
	a.applyTelegramWorkDefaults()
	if err := a.SendMessage(prompt, nil, nil); err != nil {
		item := a.ensureChannelQueue().Enqueue(env, route)
		return channelbus.Response{
			Lane:    route.Lane,
			Status:  "queued_after_start_error",
			Message: "Could not start the run immediately, so the request was queued: " + err.Error(),
			Queued:  true,
			QueueID: item.ID,
		}, nil
	}
	return channelbus.Response{
		Lane:       route.Lane,
		Status:     "started",
		Message:    "Started project run from remote channel.",
		RunStarted: true,
	}, nil
}

func (a *App) applyTelegramWorkDefaults() {
	if a == nil || a.cfg == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	tg := a.cfg.Telegram
	if profile := strings.TrimSpace(tg.DefaultProfile); profile != "" && a.profiles != nil {
		if _, ok := a.profiles.Profiles[profile]; ok {
			a.cfg.ActiveProfile = profile
		}
	}
	if mode := strings.TrimSpace(tg.DefaultMode); mode != "" {
		a.cfg.Agents.ModeOverride = mode
		a.currentMode = mode
	}
	if toolset := strings.TrimSpace(tg.DefaultToolset); toolset != "" {
		a.cfg.Tools.Enabled = true
		a.cfg.Tools.ActiveToolset = toolset
		if strings.EqualFold(toolset, "unrestricted") {
			a.autonomous = true
			a.cfg.Agents.OfflineOnly = false
			a.cfg.Agents.DefaultAutonomy = "full"
			a.cfg.Tools.ConfirmReads = false
			a.cfg.Tools.ConfirmWrites = false
			a.cfg.Tools.ConfirmExec = false
		}
		a.syncToolConfig()
	}
}

func (a *App) ensureChannelQueue() *channelbus.Queue {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.channelQueue == nil {
		a.channelQueue = channelbus.NewPersistentQueue(a.db)
	}
	return a.channelQueue
}

func (a *App) isAgentRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.agentRunning
}

func (a *App) drainChannelQueueAsync() {
	if a == nil {
		return
	}
	a.channelDrainMu.Lock()
	if a.channelDrainRunning {
		a.channelDrainMu.Unlock()
		return
	}
	a.channelDrainRunning = true
	a.channelDrainMu.Unlock()
	go func() {
		defer func() {
			a.channelDrainMu.Lock()
			a.channelDrainRunning = false
			a.channelDrainMu.Unlock()
		}()
		a.drainChannelQueue()
	}()
}

func (a *App) drainChannelQueue() {
	for {
		if a.isAgentRunning() {
			return
		}
		item, ok := a.ensureChannelQueue().PopNext()
		if !ok {
			return
		}
		resp, err := a.dispatchQueuedChannelItem(item)
		status := "done"
		if err != nil {
			status = "failed"
			resp = channelbus.Response{Lane: item.Route.Lane, Status: "error", Message: "Queued remote item failed: " + err.Error()}
		} else if resp.Status != "" {
			status = resp.Status
			if resp.RunStarted {
				status = "started"
			}
		}
		a.ensureChannelQueue().Mark(item.ID, status)
		if strings.TrimSpace(resp.Message) != "" {
			a.sendQueuedChannelReply(item.Envelope, resp.Status, resp.Message)
		}
		a.recordChannelEvent("channel_queue_dispatch", item.Envelope, item.Route, resp.Message)
		if resp.RunStarted || a.isAgentRunning() {
			return
		}
	}
}

func (a *App) dispatchQueuedChannelItem(item channelbus.WorkItem) (channelbus.Response, error) {
	env := item.Envelope
	route := item.Route
	switch route.Lane {
	case channelbus.LaneSideChat:
		resp := a.handleChannelSideChat(env, route)
		return resp, nil
	case channelbus.LaneQuick:
		return a.handleChannelQuickAction(env, route)
	case channelbus.LaneWork:
		return a.handleChannelWork(env, route)
	case channelbus.LaneControl:
		return a.handleChannelControl(env, route)
	case channelbus.LaneInterrupt, channelbus.LaneNote:
		return channelbus.Response{
			Lane:    route.Lane,
			Status:  "queued_note",
			Message: "Queued message kept for the project run:\n" + firstNonEmpty(route.Argument, env.Text),
		}, nil
	default:
		return channelbus.Response{Lane: route.Lane, Status: "unsupported", Message: "Queued remote item has no dispatcher: " + string(route.Lane)}, nil
	}
}

func (a *App) remoteStatusSummary() string {
	a.mu.Lock()
	running := a.agentRunning
	mode := strings.TrimSpace(a.currentMode)
	profile := ""
	workspace := ""
	if a.cfg != nil {
		profile = a.cfg.ActiveProfile
		workspace = firstNonEmpty(a.cfg.Context.WorkspaceDir, mustGetwd())
	}
	a.mu.Unlock()
	state := a.GetSharedTerminalState()
	queueLen := len(a.ensureChannelQueue().ListActive())
	return fmt.Sprintf("Status\n- running: %v\n- mode: %s\n- profile: %s\n- workspace: %s\n- terminal: %s\n- queued work: %d",
		running, firstNonEmpty(mode, "idle"), firstNonEmpty(profile, "unknown"), firstNonEmpty(workspace, "unknown"), firstNonEmpty(state.State, "unknown"), queueLen)
}

func (a *App) remoteFactsSummary() string {
	events, err := a.ListLedgerEvents(300)
	if err != nil {
		return "Facts unavailable: " + err.Error()
	}
	facts := strings.TrimSpace(buildRunFactsPrompt(events))
	if facts == "" {
		return "No pinned run facts yet."
	}
	return facts
}

func (a *App) remoteRecentRunSummary(kind string) string {
	runs, err := a.ListTaskRuns()
	if err != nil {
		return "Run summary unavailable: " + err.Error()
	}
	if len(runs) == 0 {
		return "No task runs yet."
	}
	run := runs[0]
	switch kind {
	case "plan":
		var lines []string
		for _, event := range run.Events {
			if strings.Contains(event.Kind, "todo") || event.Kind == "progress_update" || event.Kind == "progress" {
				lines = append(lines, "- "+event.Message)
			}
		}
		if len(lines) == 0 {
			return "No plan/progress events found for latest run."
		}
		return "Latest plan/progress\n" + strings.Join(lines, "\n")
	case "logs", "brain":
		return fmt.Sprintf("Latest run\n- status: %s\n- state: %s\n- stop: %s\n- summary: %s", run.Status, run.State, run.StopReason, run.Summary)
	default:
		return fmt.Sprintf("Latest run: %s (%s)", run.ID, run.Status)
	}
}

func formatRemoteTerminalState(state TerminalStateSnapshot) string {
	var sb strings.Builder
	sb.WriteString("Terminal\n")
	sb.WriteString("- state: " + firstNonEmpty(state.State, "unknown") + "\n")
	if state.Summary != "" {
		sb.WriteString("- summary: " + state.Summary + "\n")
	}
	if len(state.Lines) > 0 {
		sb.WriteString("\n")
		sb.WriteString(strings.Join(state.Lines, "\n"))
	}
	return sb.String()
}

func remoteHelpText() string {
	return strings.Join([]string{
		"Remote commands:",
		"/status - active run, profile, terminal, queue",
		"/facts - current pinned run facts",
		"/run <task> - start or queue project work",
		"/stop - stop active project run",
		"/terminal_read - read shared terminal state",
		"/plan - latest plan/progress",
		"/logs - latest run summary",
		"/help - this help",
		"",
		"Plain messages are side chat and do not interrupt the project run.",
	}, "\n")
}

func (a *App) recordChannelEvent(kind string, env channelbus.Envelope, route channelbus.Route, detail string) {
	if a == nil || a.ledger == nil {
		return
	}
	metadata := map[string]string{
		"source":     env.Source,
		"session_id": env.SessionID,
		"lane":       string(route.Lane),
		"command":    route.Command,
	}
	if env.Username != "" {
		metadata["username"] = env.Username
	}
	_, _ = a.ledger.Record(ledger.Event{
		Kind:     kind,
		Source:   "channelbus",
		Status:   string(route.Lane),
		Message:  truncateRunes(env.Text, 240),
		Detail:   truncateRunes(detail, 1000),
		Metadata: metadata,
	})
}

func (a *App) recordChannelSideChatModelEvent(kind string, env channelbus.Envelope, route channelbus.Route, profile settings.Profile, duration time.Duration, replyChars int, detail string) {
	if a == nil || a.ledger == nil {
		return
	}
	status := "running"
	if strings.Contains(kind, "done") {
		status = "done"
	} else if strings.Contains(kind, "error") {
		status = "error"
	}
	metadata := map[string]string{
		"source":      env.Source,
		"session_id":  env.SessionID,
		"lane":        string(route.Lane),
		"command":     route.Command,
		"profile":     profile.Name,
		"model":       profile.ModelID,
		"backend":     profile.Backend,
		"reply_chars": fmt.Sprintf("%d", replyChars),
	}
	if duration > 0 {
		metadata["duration_ms"] = fmt.Sprintf("%d", duration.Milliseconds())
	}
	if env.Username != "" {
		metadata["username"] = env.Username
	}
	_, _ = a.ledger.Record(ledger.Event{
		Kind:       kind,
		Source:     "channelbus",
		Status:     status,
		Message:    truncateRunes(env.Text, 240),
		Detail:     truncateRunes(detail, 1200),
		DurationMs: duration.Milliseconds(),
		Metadata:   metadata,
	})
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}
