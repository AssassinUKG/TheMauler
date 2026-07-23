package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"mauler/internal/audio"
	"mauler/internal/settings"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type SpeechAudio struct {
	DataURI string `json:"data_uri"`
	Engine  string `json:"engine"`
	Voice   string `json:"voice"`
}

type AudioHealth struct {
	Enabled           bool   `json:"enabled"`
	Overall           string `json:"overall"`
	ConfiguredTTS     string `json:"configured_tts"`
	ActualTTS         string `json:"actual_tts"`
	Voice             string `json:"voice"`
	STTEngine         string `json:"stt_engine"`
	STTReady          bool   `json:"stt_ready"`
	WorkerState       string `json:"worker_state"`
	WorkerPID         int    `json:"worker_pid"`
	LastSuccess       string `json:"last_success"`
	LastError         string `json:"last_error"`
	SpeakReplies      bool   `json:"speak_replies"`
	WorkerHidden      bool   `json:"worker_hidden"`
	STTWorkerState    string `json:"stt_worker_state"`
	STTWorkerPID      int    `json:"stt_worker_pid"`
	STTModel          string `json:"stt_model"`
	STTLastDurationMS int64  `json:"stt_last_duration_ms"`
	STTLastAudioMS    int64  `json:"stt_last_audio_ms"`
	STTLastSuccess    string `json:"stt_last_success"`
	STTLastError      string `json:"stt_last_error"`
}

var audioSTTProbeCache struct {
	sync.Mutex
	ready     bool
	detail    string
	checkedAt time.Time
}

func audioSTTDependencyStatus() (bool, string) {
	audioSTTProbeCache.Lock()
	defer audioSTTProbeCache.Unlock()
	if time.Since(audioSTTProbeCache.checkedAt) < 30*time.Second {
		return audioSTTProbeCache.ready, audioSTTProbeCache.detail
	}
	_, err := audio.ProbeWhisperPython()
	audioSTTProbeCache.ready = err == nil
	audioSTTProbeCache.detail = ""
	if err != nil {
		audioSTTProbeCache.detail = err.Error()
	}
	audioSTTProbeCache.checkedAt = time.Now()
	return audioSTTProbeCache.ready, audioSTTProbeCache.detail
}

func (a *App) GetAudioHealth() AudioHealth {
	a.mu.Lock()
	cfg := settingsAudioSnapshot(a.cfg)
	a.mu.Unlock()
	runtimeStatus := audio.Status()
	sttStatus := audio.WhisperStatus()
	sttReady, sttDependencyError := audioSTTDependencyStatus()
	sttLastError := sttStatus.LastError
	if sttLastError == "" {
		sttLastError = sttDependencyError
	}
	overall := audioOverall(cfg.Enabled, audioUsesKokoro(cfg.TTSEngine), runtimeStatus.WorkerState,
		runtimeStatus.LastError, sttStatus.State, sttReady, sttLastError)
	return AudioHealth{
		Enabled: cfg.Enabled, Overall: overall, ConfiguredTTS: firstNonEmpty(cfg.TTSEngine, "auto"),
		ActualTTS: runtimeStatus.LastEngine, Voice: firstNonEmpty(runtimeStatus.LastVoice, cfg.Voice),
		STTEngine: firstNonEmpty(cfg.STTEngine, "whisper"), STTReady: sttReady,
		WorkerState: runtimeStatus.WorkerState, WorkerPID: runtimeStatus.WorkerPID,
		LastSuccess: runtimeStatus.LastSuccess.Format(time.RFC3339), LastError: runtimeStatus.LastError,
		SpeakReplies: cfg.SpeakReplies, WorkerHidden: true,
		STTWorkerState: sttStatus.State, STTWorkerPID: sttStatus.PID, STTModel: sttStatus.Model,
		STTLastDurationMS: sttStatus.LastDurationMS, STTLastAudioMS: sttStatus.LastAudioMS,
		STTLastSuccess: sttStatus.LastSuccess.Format(time.RFC3339), STTLastError: sttLastError,
	}
}

func audioOverall(enabled, requiresKokoro bool, ttsState, ttsLastError, sttState string, sttReady bool, sttLastError string) string {
	if !enabled {
		return "disabled"
	}
	ttsState = strings.ToLower(strings.TrimSpace(ttsState))
	sttState = strings.ToLower(strings.TrimSpace(sttState))
	if ttsState == "error" || sttState == "error" {
		return "error"
	}
	if ttsState == "loading" || sttState == "loading" {
		return "loading"
	}
	if !sttReady || sttState == "stopped" || (requiresKokoro && ttsState == "stopped") || ttsLastError != "" || sttLastError != "" {
		return "degraded"
	}
	return "ready"
}

func (a *App) RestartAudioWorker() AudioHealth {
	a.mu.Lock()
	cfg := settingsAudioSnapshot(a.cfg)
	a.mu.Unlock()
	if cfg.Enabled && audioUsesKokoro(cfg.TTSEngine) {
		audio.RestartKokoro(os.Getenv("MAULER_KOKORO_PYTHON"), cfg.Voice)
	} else {
		audio.RestartWorker()
	}
	if cfg.Enabled && strings.EqualFold(firstNonEmpty(cfg.STTEngine, "whisper"), "whisper") {
		audioSTTProbeCache.Lock()
		audioSTTProbeCache.checkedAt = time.Time{}
		audioSTTProbeCache.Unlock()
		audio.RestartWhisper("")
	} else {
		audio.StopWhisper()
	}
	return a.GetAudioHealth()
}

func audioUsesKokoro(engine string) bool {
	engine = strings.ToLower(strings.TrimSpace(engine))
	return engine == "" || engine == "auto" || engine == "kokoro"
}

func (a *App) ListKokoroVoices() []string {
	return audio.KokoroVoices()
}

func (a *App) TranscribeVoiceClip(dataURI string) (string, error) {
	a.mu.Lock()
	cfg := settingsAudioSnapshot(a.cfg)
	a.mu.Unlock()
	if !cfg.Enabled {
		return "", fmt.Errorf("desktop audio is disabled in Settings")
	}
	if cfg.STTEngine != "" && !strings.EqualFold(cfg.STTEngine, "whisper") {
		return "", fmt.Errorf("desktop STT engine %q is not installed yet; select whisper", cfg.STTEngine)
	}
	mimeType, data, err := decodeAudioDataURI(dataURI)
	if err != nil {
		return "", err
	}
	if len(data) > 25*1024*1024 {
		return "", fmt.Errorf("voice clip is too large")
	}
	dir, err := os.MkdirTemp("", "mauler-voice-input-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "speech"+audioExtension(mimeType))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	transcript, workerErr := audio.TranscribeWhisper(runCtx, path, "")
	transcript = strings.TrimSpace(transcript)
	if audio.IsSpeechRejected(workerErr) {
		return "", workerErr
	}
	if transcript == "" {
		// Preserve the one-shot CLI as a compatibility fallback if the persistent
		// worker cannot start or loses its pipe.
		transcript = strings.TrimSpace(defaultLocalWhisperTranscription(runCtx, path))
	}
	if transcript == "" {
		if workerErr != nil {
			return "", workerErr
		}
		return "", fmt.Errorf("speech transcription produced no text")
	}
	return transcript, nil
}

func (a *App) SynthesizeSpeech(text string) (SpeechAudio, error) {
	a.mu.Lock()
	cfg := settingsAudioSnapshot(a.cfg)
	a.mu.Unlock()
	if !cfg.Enabled || !cfg.SpeakReplies {
		return SpeechAudio{}, fmt.Errorf("spoken replies are disabled")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := audio.Synthesize(runCtx, cleanTelegramVoiceText(text), audio.TTSOptions{
		Engine:       cfg.TTSEngine,
		Voice:        cfg.Voice,
		Speed:        cfg.Speed,
		MaxChars:     900,
		KokoroPython: os.Getenv("MAULER_KOKORO_PYTHON"),
		PiperPath:    os.Getenv("MAULER_PIPER_PATH"),
		PiperModel:   os.Getenv("MAULER_PIPER_MODEL"),
		PiperConfig:  os.Getenv("MAULER_PIPER_CONFIG"),
		DataDirs:     []string{maulerTTSPath(), helixClawTTSPath()},
	})
	if err != nil {
		return SpeechAudio{}, err
	}
	return SpeechAudio{
		DataURI: "data:" + result.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(result.Data),
		Engine:  result.Engine,
		Voice:   result.Voice,
	}, nil
}

func settingsAudioSnapshot(cfg *settings.Settings) settings.AudioConfig {
	if cfg == nil {
		return settings.AudioConfig{}
	}
	return cfg.Audio
}

func decodeAudioDataURI(value string) (string, []byte, error) {
	header, payload, ok := strings.Cut(strings.TrimSpace(value), ",")
	if !ok || !strings.HasPrefix(header, "data:audio/") || !strings.HasSuffix(header, ";base64") {
		return "", nil, fmt.Errorf("invalid audio data URI")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, fmt.Errorf("decode voice clip: %w", err)
	}
	mimeType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	return mimeType, data, nil
}

func audioExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mp4", "audio/m4a":
		return ".m4a"
	default:
		return ".webm"
	}
}
