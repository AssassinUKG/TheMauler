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
	services := []ServiceHealth{{
		ID: "audio", Name: "Audio / Voice", Status: audioHealth.Overall,
		Summary: fmt.Sprintf("TTS %s · STT %s · worker %s", firstNonEmpty(audioHealth.ActualTTS, audioHealth.ConfiguredTTS), readiness(audioHealth.STTReady), audioHealth.WorkerState),
		Detail:  audioHealth.LastError, UpdatedAt: now,
		Metadata: map[string]string{"voice": audioHealth.Voice, "worker_pid": strconv.Itoa(audioHealth.WorkerPID)},
	}}

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
		browserStatus = "active"
	}
	services = append(services, ServiceHealth{ID: "browser", Name: "Browser automation", Status: browserStatus, Summary: firstNonEmpty(browser.Binary, "Chrome or Edge not detected"), UpdatedAt: now})

	sessions := a.ListAgentSessions()
	activeSessions := 0
	for _, session := range sessions {
		if session.State != "dead" && session.State != "closed" {
			activeSessions++
		}
	}
	services = append(services, ServiceHealth{ID: "listeners", Name: "Listeners / shells", Status: activeCountStatus(activeSessions), Summary: fmt.Sprintf("%d active · %d tracked", activeSessions, len(sessions)), UpdatedAt: now})

	a.bgMu.Lock()
	jobs := len(a.bgJobs)
	a.bgMu.Unlock()
	services = append(services, ServiceHealth{ID: "jobs", Name: "Background jobs", Status: activeCountStatus(jobs), Summary: fmt.Sprintf("%d running", jobs), UpdatedAt: now})
	return services
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
