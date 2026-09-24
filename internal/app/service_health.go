package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"mauler/internal/tools"
)

type ServiceHealth struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	Summary   string            `json:"summary"`
	Detail    string            `json:"detail,omitempty"`
	UpdatedAt string            `json:"updated_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (a *App) GetServiceHealth() []ServiceHealth {
	now := time.Now().Format(time.RFC3339)
	audioHealth := a.GetAudioHealth()
	services := []ServiceHealth{audioServiceHealth(audioHealth, now)}

	telegram := a.telegramRuntimeDiagnostics()
	telegramStatus := "disabled"
	if telegram["telegram_running"] == "true" {
		telegramStatus = "ready"
	} else if strings.Contains(strings.ToLower(telegram["telegram_status"]), "error") {
		telegramStatus = "error"
	}
	services = append(services, ServiceHealth{ID: "telegram", Name: "Telegram", Status: telegramStatus, Summary: firstNonEmpty(telegram["telegram_status"], "not running"), UpdatedAt: now, Metadata: telegram})

	browser := tools.GetBrowserRuntimeStatus()
	browserStatus := "unavailable"
	if browser.Available {
		browserStatus = "ready"
	}
	if browser.Active {
		browserStatus = firstNonEmpty(browser.State, "active")
	}
	services = append(services, ServiceHealth{
		ID: "browser", Name: "Browser automation", Status: browserStatus,
		Summary:   firstNonEmpty(browser.Title, browser.Binary, "Chrome or Edge not detected"),
		Detail:    browser.LastError,
		UpdatedAt: now,
		Metadata: map[string]string{
			"url":         browser.URL,
			"last_action": browser.LastAction,
			"visible":     strconv.FormatBool(browser.Visible),
			"paused":      strconv.FormatBool(browser.Paused),
		},
	})

	indexStatus, indexErr := a.GetRepositoryIndexStatus()
	indexHealth := "ready"
	indexSummary := "No complete workspace index"
	indexDetail := ""
	if indexErr != nil {
		indexHealth, indexSummary, indexDetail = "error", "Repository index status unavailable", indexErr.Error()
	} else if !indexStatus.Available {
		indexHealth, indexSummary, indexDetail = "unavailable", "Repository index database unavailable", indexStatus.Error
	} else if indexStatus.Indexing {
		indexHealth = "active"
		indexSummary = fmt.Sprintf("Scanning: %d seen, %d indexed", indexStatus.ProgressFilesSeen, indexStatus.ProgressIndexed)
	} else if indexStatus.Active {
		indexSummary = fmt.Sprintf("%d files · %d searchable chunks", indexStatus.FilesIndexed, indexStatus.ChunkCount)
	}
	services = append(services, ServiceHealth{
		ID: "repository_index", Name: "Workspace index", Status: indexHealth, Summary: indexSummary,
		Detail: indexDetail, UpdatedAt: now,
		Metadata: map[string]string{
			"workspace": indexStatus.Workspace, "generation_id": indexStatus.GenerationID,
			"manifest_digest": indexStatus.ManifestDigest, "indexing": strconv.FormatBool(indexStatus.Indexing),
			"files_indexed": strconv.Itoa(indexStatus.FilesIndexed), "progress_files_seen": strconv.Itoa(indexStatus.ProgressFilesSeen),
		},
	})

	sessions := a.ListAgentSessions()
	services = append(services, sessionServiceHealth(sessions, now))

	eventDiagnostics := a.GetRunEventDiagnostics()
	eventStatus := "ready"
	eventSummary := "No stale run events rejected"
	if eventDiagnostics.StaleEventsDropped > 0 {
		eventStatus = "active"
		eventSummary = fmt.Sprintf("%d stale run event(s) safely rejected", eventDiagnostics.StaleEventsDropped)
	}
	services = append(services, ServiceHealth{
		ID: "run_event_ownership", Name: "Conversation event ownership", Status: eventStatus,
		Summary: eventSummary, UpdatedAt: now,
		Metadata: map[string]string{
			"conversation_epoch":   strconv.FormatUint(eventDiagnostics.ConversationEpoch, 10),
			"stale_events_dropped": strconv.FormatUint(eventDiagnostics.StaleEventsDropped, 10),
			"last_event":           eventDiagnostics.LastRejection.Event,
			"last_run_id":          eventDiagnostics.LastRejection.RunID,
			"last_rejected_at":     eventDiagnostics.LastRejection.RejectedAt,
		},
	})

	a.bgMu.Lock()
	jobs := len(a.bgJobs)
	a.bgMu.Unlock()
	services = append(services, ServiceHealth{ID: "jobs", Name: "Background jobs", Status: activeCountStatus(jobs), Summary: fmt.Sprintf("%d running", jobs), UpdatedAt: now})
	return services
}

func audioServiceHealth(health AudioHealth, now string) ServiceHealth {
	ttsEngine := firstNonEmpty(health.ActualTTS, health.ConfiguredTTS, "auto")
	ttsState := firstNonEmpty(health.WorkerState, "stopped")
	sttEngine := firstNonEmpty(health.STTEngine, "whisper")
	sttState := firstNonEmpty(health.STTWorkerState, "stopped")
	if !health.STTReady && sttState != "error" {
		sttState = "missing"
	}
	details := make([]string, 0, 2)
	if health.LastError != "" {
		details = append(details, "TTS: "+health.LastError)
	}
	if health.STTLastError != "" {
		details = append(details, "STT: "+health.STTLastError)
	}
	return ServiceHealth{
		ID: "audio", Name: "Audio / Voice", Status: health.Overall,
		Summary:   fmt.Sprintf("TTS %s (%s) · STT %s (%s)", ttsEngine, ttsState, sttEngine, sttState),
		Detail:    strings.Join(details, " | "),
		UpdatedAt: now,
		Metadata: map[string]string{
			"voice":          health.Voice,
			"tts_state":      ttsState,
			"tts_worker_pid": strconv.Itoa(health.WorkerPID),
			"stt_state":      sttState,
			"stt_model":      health.STTModel,
			"stt_worker_pid": strconv.Itoa(health.STTWorkerPID),
		},
	}
}

func sessionServiceHealth(sessions []AgentSession, now string) ServiceHealth {
	listeners, shells, busy, idle := 0, 0, 0, 0
	metadata := make(map[string]string)
	visible := 0
	for _, session := range sessions {
		switch strings.ToLower(strings.TrimSpace(session.State)) {
		case "listening", "listener":
			listeners++
		case "connected":
			shells++
		case "busy", "running", "interactive_prompt":
			busy++
		case "ready":
			idle++
		}
		if session.State == "dead" || session.State == "closed" || visible >= 4 {
			continue
		}
		visible++
		metadata[fmt.Sprintf("session_%d", visible)] = describeAgentSession(session)
	}
	active := listeners + shells + busy
	summary := fmt.Sprintf("%d %s · %d %s · %d busy · %d idle",
		listeners, pluralWord(listeners, "listener", "listeners"),
		shells, pluralWord(shells, "shell", "shells"), busy, idle)
	if len(sessions) == 0 {
		summary = "No listeners, connected shells, or tracked terminals"
	}
	return ServiceHealth{
		ID: "listeners", Name: "Listeners / shells", Status: activeCountStatus(active),
		Summary: summary, UpdatedAt: now, Metadata: metadata,
	}
}

func describeAgentSession(session AgentSession) string {
	state := firstNonEmpty(strings.ToLower(strings.TrimSpace(session.State)), "unknown")
	if state == "ready" {
		state = "ready (idle)"
	}
	kind := firstNonEmpty(strings.ToLower(strings.TrimSpace(session.Kind)), "session")
	identity := ""
	if session.User != "" || session.Hostname != "" {
		identity = strings.Trim(strings.TrimSpace(session.User)+"@"+strings.TrimSpace(session.Hostname), "@")
	}
	address := strings.TrimSpace(session.Lhost)
	if session.Port > 0 {
		if address == "" {
			address = fmt.Sprintf(":%d", session.Port)
		} else {
			address = fmt.Sprintf("%s:%d", address, session.Port)
		}
	}
	parts := []string{kind}
	if address != "" {
		parts = append(parts, address)
	} else if identity != "" {
		parts = append(parts, identity)
	} else if kind == "terminal" {
		parts[0] = "shared terminal"
	}
	parts = append(parts, state)
	return strings.Join(parts, " · ")
}

func pluralWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func readiness(ok bool) string {
	if ok {
		return "ready"
	}
	return "missing"
}
func activeCountStatus(count int) string {
	if count > 0 {
		return "active"
	}
	return "idle"
}
