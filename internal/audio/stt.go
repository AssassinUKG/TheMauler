package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type STTStatus struct {
	State          string    `json:"state"`
	PID            int       `json:"pid"`
	Backend        string    `json:"backend"`
	Model          string    `json:"model"`
	LastDurationMS int64     `json:"last_duration_ms"`
	LastAudioMS    int64     `json:"last_audio_ms"`
	LastSuccess    time.Time `json:"last_success"`
	LastError      string    `json:"last_error"`
}

type whisperWorkerRequest struct {
	Path string `json:"path,omitempty"`
}

type whisperWorkerResponse struct {
	OK         bool   `json:"ok"`
	Ready      bool   `json:"ready,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Error      string `json:"error,omitempty"`
	Code       string `json:"code,omitempty"`
	Model      string `json:"model,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	AudioMS    int64  `json:"audio_ms,omitempty"`
}

type whisperWorker struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *json.Decoder
	python string
}

var globalWhisperWorker whisperWorker
var sttRuntime struct {
	sync.Mutex
	status STTStatus
}

func setSTTState(state string, update func(*STTStatus)) {
	sttRuntime.Lock()
	defer sttRuntime.Unlock()
	sttRuntime.status.State = state
	sttRuntime.status.Backend = "openai-whisper-worker"
	if update != nil {
		update(&sttRuntime.status)
	}
}

func WhisperStatus() STTStatus {
	sttRuntime.Lock()
	defer sttRuntime.Unlock()
	status := sttRuntime.status
	if status.State == "" {
		status.State = "stopped"
	}
	return status
}

func ResolveWhisperPython() string {
	python, _ := ProbeWhisperPython()
	return python
}

// ProbeWhisperPython verifies the full Whisper import rather than merely finding
// a Python executable. Keeping the import error is important: dependency
// conflicts otherwise look like a missing worker in the UI.
func ProbeWhisperPython() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MAULER_STT_PYTHON")); override != "" {
		if path, err := exec.LookPath(override); err == nil {
			return probeWhisperCandidate(path)
		}
	}
	seen := make(map[string]struct{})
	failures := make([]string, 0, 3)
	for _, candidate := range []string{os.Getenv("MAULER_KOKORO_PYTHON"), "python", "py", "python3"} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, probeErr := probeWhisperCandidate(path); probeErr == nil {
			return path, nil
		} else {
			failures = append(failures, probeErr.Error())
		}
	}
	if len(failures) == 0 {
		return "", fmt.Errorf("Whisper dependency check failed: no Python executable was found")
	}
	return "", fmt.Errorf("Whisper dependency check failed: %s", strings.Join(failures, "; "))
}

func probeWhisperCandidate(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-c", "import whisper")
	hideChildProcessWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return path, nil
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s: import timed out", filepath.Base(path))
	}
	detail := lastNonEmptyLine(string(output))
	if detail == "" {
		detail = err.Error()
	}
	if len(detail) > 360 {
		detail = detail[:357] + "..."
	}
	return "", fmt.Errorf("%s: %s", filepath.Base(path), detail)
}

func lastNonEmptyLine(value string) string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

func WarmWhisper(python string) {
	go func() {
		globalWhisperWorker.mu.Lock()
		defer globalWhisperWorker.mu.Unlock()
		if err := globalWhisperWorker.ensureStarted(python); err != nil {
			setSTTState("error", func(s *STTStatus) { s.LastError = err.Error() })
		}
	}()
}

func RestartWhisper(python string) {
	globalWhisperWorker.mu.Lock()
	globalWhisperWorker.stop()
	globalWhisperWorker.mu.Unlock()
	WarmWhisper(python)
}

func StopWhisper() {
	globalWhisperWorker.mu.Lock()
	globalWhisperWorker.stop()
	globalWhisperWorker.mu.Unlock()
}

func TranscribeWhisper(ctx context.Context, path, python string) (string, error) {
	return globalWhisperWorker.transcribe(ctx, python, path)
}

func IsSpeechRejected(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "recording is too short") || strings.Contains(message, "no speech detected")
}

func (w *whisperWorker) transcribe(ctx context.Context, python, path string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensureStarted(python); err != nil {
		w.stop()
		return "", err
	}
	setSTTState("transcribing", nil)
	started := time.Now()
	line, _ := json.Marshal(whisperWorkerRequest{Path: path})
	type result struct {
		resp whisperWorkerResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if _, err := w.stdin.Write(append(line, '\n')); err != nil {
			ch <- result{err: err}
			return
		}
		var resp whisperWorkerResponse
		if err := w.stdout.Decode(&resp); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{resp: resp}
	}()
	select {
	case <-ctx.Done():
		w.stop()
		setSTTState("error", func(s *STTStatus) { s.LastError = ctx.Err().Error() })
		return "", ctx.Err()
	case res := <-ch:
		elapsed := time.Since(started).Milliseconds()
		if res.err != nil {
			w.stop()
			setSTTState("error", func(s *STTStatus) { s.LastError = res.err.Error(); s.LastDurationMS = elapsed })
			return "", res.err
		}
		if !res.resp.OK {
			err := fmt.Errorf("whisper worker failed: %s", res.resp.Error)
			setSTTState("ready", func(s *STTStatus) {
				if res.resp.Code == "too_short" || res.resp.Code == "silence" {
					s.LastError = ""
				} else {
					s.LastError = err.Error()
				}
				s.LastDurationMS = elapsed
				s.LastAudioMS = res.resp.AudioMS
			})
			return "", err
		}
		setSTTState("ready", func(s *STTStatus) {
			s.LastError = ""
			s.LastDurationMS = elapsed
			s.LastAudioMS = res.resp.AudioMS
			s.LastSuccess = time.Now()
		})
		return strings.TrimSpace(res.resp.Transcript), nil
	}
}

func (w *whisperWorker) ensureStarted(python string) error {
	python = strings.TrimSpace(python)
	if python == "" {
		var err error
		python, err = ProbeWhisperPython()
		if err != nil {
			return err
		}
	}
	if w.cmd != nil && w.cmd.Process != nil && w.python == python {
		return nil
	}
	w.stop()
	setSTTState("loading", func(s *STTStatus) { s.LastError = ""; s.Model = "tiny.en" })
	script, err := ensureWhisperWorkerScript()
	if err != nil {
		return err
	}
	cmd := exec.Command(python, "-u", script)
	hideChildProcessWindow(cmd)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start whisper worker: %w", err)
	}
	decoder := json.NewDecoder(stdout)
	type readyResult struct {
		resp whisperWorkerResponse
		err  error
	}
	readyCh := make(chan readyResult, 1)
	go func() {
		var resp whisperWorkerResponse
		err := decoder.Decode(&resp)
		readyCh <- readyResult{resp: resp, err: err}
	}()
	select {
	case result := <-readyCh:
		if result.err != nil || !result.resp.OK || !result.resp.Ready {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			return fmt.Errorf("load whisper worker: %v %s %s", result.err, result.resp.Error, strings.TrimSpace(stderr.String()))
		}
	case <-time.After(90 * time.Second):
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return fmt.Errorf("loading Whisper tiny.en timed out")
	}
	w.cmd, w.stdin, w.stdout, w.python = cmd, stdin, decoder, python
	setSTTState("ready", func(s *STTStatus) { s.PID = cmd.Process.Pid; s.Model = "tiny.en"; s.LastError = "" })
	return nil
}

func (w *whisperWorker) stop() {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_, _ = w.cmd.Process.Wait()
	}
	w.cmd, w.stdin, w.stdout, w.python = nil, nil, nil, ""
	setSTTState("stopped", func(s *STTStatus) { s.PID = 0 })
}

func ensureWhisperWorkerScript() (string, error) {
	dir := filepath.Join(os.TempDir(), "mauler-audio")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "whisper_worker.py")
	if err := os.WriteFile(path, []byte(whisperWorkerScript), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

const whisperWorkerScript = `import json, math, sys, time, traceback
import numpy as np
import whisper

MODEL_NAME = "tiny.en"
model = whisper.load_model(MODEL_NAME, device="cpu")
print(json.dumps({"ok": True, "ready": True, "model": MODEL_NAME}), flush=True)

for line in sys.stdin:
    started = time.perf_counter()
    try:
        req = json.loads(line)
        path = (req.get("path") or "").strip()
        if not path:
            raise ValueError("empty audio path")
        samples = whisper.load_audio(path)
        audio_ms = int((len(samples) / 16000.0) * 1000)
        if audio_ms < 350:
            print(json.dumps({"ok": False, "code": "too_short", "error": "recording is too short", "audio_ms": audio_ms}), flush=True)
            continue
        rms = float(np.sqrt(np.mean(np.square(samples)))) if len(samples) else 0.0
        peak = float(np.max(np.abs(samples))) if len(samples) else 0.0
        if rms < 0.0008 and peak < 0.01:
            print(json.dumps({"ok": False, "code": "silence", "error": "no speech detected", "audio_ms": audio_ms}), flush=True)
            continue
        result = model.transcribe(samples, language="en", task="transcribe", fp16=False, condition_on_previous_text=False)
        text = (result.get("text") or "").strip()
        if not text:
            raise RuntimeError("speech transcription produced no text")
        print(json.dumps({"ok": True, "transcript": text, "model": MODEL_NAME, "audio_ms": audio_ms,
                          "duration_ms": int((time.perf_counter() - started) * 1000)}), flush=True)
    except Exception as exc:
        print(json.dumps({"ok": False, "error": str(exc), "duration_ms": int((time.perf_counter() - started) * 1000),
                          "trace": traceback.format_exc(limit=3)}), flush=True)
`
