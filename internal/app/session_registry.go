package app

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"mauler/internal/ledger"
)

type AgentSession struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	State           string `json:"state"`
	Port            int    `json:"port,omitempty"`
	Lhost           string `json:"lhost,omitempty"`
	Command         string `json:"command,omitempty"`
	User            string `json:"user,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	LastEvidence    string `json:"last_evidence,omitempty"`
	TerminalSession string `json:"terminal_session,omitempty"`
}

func (a *App) upsertAgentSession(next AgentSession) AgentSession {
	if a == nil {
		return next
	}
	next.ID = strings.TrimSpace(next.ID)
	if next.ID == "" {
		next.ID = fmt.Sprintf("%s:%d", firstNonEmpty(next.Kind, "session"), time.Now().UnixNano())
	}
	now := time.Now().Format(time.RFC3339)
	a.sessionMu.Lock()
	if a.agentSessions == nil {
		a.agentSessions = make(map[string]AgentSession)
	}
	prev := a.agentSessions[next.ID]
	if next.Kind == "" {
		next.Kind = prev.Kind
	}
	if next.State == "" {
		next.State = prev.State
	}
	if next.StartedAt == "" {
		next.StartedAt = firstNonEmpty(prev.StartedAt, now)
	}
	next.UpdatedAt = now
	if next.Port == 0 {
		next.Port = prev.Port
	}
	if next.Lhost == "" {
		next.Lhost = prev.Lhost
	}
	if next.Command == "" {
		next.Command = prev.Command
	}
	if next.User == "" {
		next.User = prev.User
	}
	if next.Hostname == "" {
		next.Hostname = prev.Hostname
	}
	if next.LastEvidence == "" {
		next.LastEvidence = prev.LastEvidence
	}
	if next.TerminalSession == "" {
		next.TerminalSession = prev.TerminalSession
	}
	a.agentSessions[next.ID] = next
	changed := prev.State != next.State || prev.LastEvidence != next.LastEvidence || prev.ID == ""
	a.sessionMu.Unlock()
	if changed {
		a.recordAgentSessionEvent(next)
	}
	return next
}

func (a *App) recordAgentSessionEvent(session AgentSession) {
	if a == nil {
		return
	}
	meta := map[string]string{
		"id":      session.ID,
		"kind":    session.Kind,
		"session": session.TerminalSession,
	}
	if session.Port > 0 {
		meta["port"] = fmt.Sprintf("%d", session.Port)
	}
	if session.Lhost != "" {
		meta["lhost"] = session.Lhost
	}
	a.recordLedger(ledger.Event{
		Kind:     "session_state",
		Source:   "terminal",
		Status:   session.State,
		State:    session.State,
		Message:  session.ID,
		Detail:   session.LastEvidence,
		Metadata: meta,
	})
}

func (a *App) ListAgentSessions() []AgentSession {
	if a == nil {
		return []AgentSession{}
	}
	a.refreshAgentSessionsFromSharedTerminal()
	a.sessionMu.Lock()
	out := make([]AgentSession, 0, len(a.agentSessions))
	for _, session := range a.agentSessions {
		out = append(out, session)
	}
	a.sessionMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

func (a *App) refreshAgentSessionsFromSharedTerminal() {
	if a == nil {
		return
	}
	a.shellMu.Lock()
	sess := a.shellSess
	a.shellMu.Unlock()
	a.updateAgentSessionsFromTerminalState(sharedTerminalStateSnapshot(sess))
}

func (a *App) updateAgentSessionsFromTerminalState(snapshot TerminalStateSnapshot) {
	if a == nil || snapshot.State == "" {
		return
	}
	var candidates []AgentSession
	a.sessionMu.Lock()
	for _, session := range a.agentSessions {
		if session.TerminalSession == "" || session.TerminalSession == snapshot.Session {
			candidates = append(candidates, session)
		}
	}
	a.sessionMu.Unlock()
	if len(candidates) == 0 && snapshot.Session != "" && (snapshot.State == "connected" || snapshot.State == "listener" || snapshot.State == "busy" || snapshot.State == "running") {
		candidates = append(candidates, AgentSession{
			ID:              "terminal:" + snapshot.Session,
			Kind:            "terminal",
			TerminalSession: snapshot.Session,
		})
	}
	for _, session := range candidates {
		state := sessionStateFromTerminal(snapshot.State, session.Kind)
		if state == "" {
			continue
		}
		user, host := extractSessionIdentity(snapshot.Lines)
		session.State = state
		session.TerminalSession = firstNonEmpty(session.TerminalSession, snapshot.Session)
		session.User = firstNonEmpty(user, session.User)
		session.Hostname = firstNonEmpty(host, session.Hostname)
		session.LastEvidence = snapshot.Summary
		a.upsertAgentSession(session)
	}
}

func sessionStateFromTerminal(terminalState, kind string) string {
	switch strings.TrimSpace(terminalState) {
	case "connected":
		return "connected"
	case "listener":
		return "listening"
	case "interactive_prompt":
		return "busy"
	case "running":
		if kind == "listener" {
			return "listening"
		}
		return "busy"
	case "ready":
		if kind == "listener" {
			return "ready"
		}
		return "ready"
	case "closed":
		return "dead"
	case "busy":
		return "busy"
	default:
		return ""
	}
}

var (
	sessionUIDRe      = regexp.MustCompile(`uid=\d+\(([^)]+)\)`)
	sessionHostnameRe = regexp.MustCompile(`(?m)^(?:hostname\s*)?([A-Za-z0-9._-]{2,})$`)
)

func extractSessionIdentity(lines []string) (string, string) {
	var user, host string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if user == "" {
			if m := sessionUIDRe.FindStringSubmatch(trimmed); len(m) == 2 {
				user = m[1]
			} else if strings.EqualFold(trimmed, "root") || strings.EqualFold(trimmed, "asterisk") || strings.EqualFold(trimmed, "www-data") {
				user = trimmed
			}
		}
		lower := strings.ToLower(trimmed)
		if host == "" && !strings.ContainsAny(trimmed, " /\\<>|;&=$") && !strings.Contains(lower, "uid=") {
			if m := sessionHostnameRe.FindStringSubmatch(trimmed); len(m) == 2 && !strings.Contains(lower, "ncat") && !strings.Contains(lower, "listening") {
				host = m[1]
			}
		}
	}
	return user, host
}
