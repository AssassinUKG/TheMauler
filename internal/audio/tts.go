package audio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	DefaultKokoroVoice = "af_heart"
	DefaultPiperVoice  = "en_GB-jenny_dioco-medium"
)

var kokoroVoices = []string{
	"af_heart", "af_alloy", "af_aoede", "af_bella", "af_jessica", "af_kore", "af_nicole", "af_nova", "af_river", "af_sarah", "af_sky",
	"am_adam", "am_echo", "am_eric", "am_fenrir", "am_liam", "am_michael", "am_onyx", "am_puck",
	"bf_alice", "bf_emma", "bf_isabella", "bf_lily",
	"bm_daniel", "bm_fable", "bm_george", "bm_lewis",
}

func KokoroVoices() []string {
	out := make([]string, len(kokoroVoices))
	copy(out, kokoroVoices)
	return out
}

func IsKokoroVoice(voice string) bool {
	voice = strings.ToLower(strings.TrimSpace(voice))
	for _, candidate := range kokoroVoices {
		if voice == candidate {
			return true
		}
	}
	return false
}

type TTSOptions struct {
	Engine       string
	Voice        string
	Speed        float64
	MaxChars     int
	DataDirs     []string
	KokoroPython string
	PiperPath    string
	PiperModel   string
	PiperConfig  string
}

type Audio struct {
	Data     []byte
	MIMEType string
	FileExt  string
	Engine   string
	Voice    string
}

type RuntimeStatus struct {
	WorkerState string    `json:"worker_state"`
	WorkerPID   int       `json:"worker_pid"`
	LastEngine  string    `json:"last_engine"`
	LastVoice   string    `json:"last_voice"`
	LastError   string    `json:"last_error"`
	LastSuccess time.Time `json:"last_success"`
}

var runtimeHealth struct {
	sync.Mutex
	lastEngine  string
	lastVoice   string
	lastError   string
	lastSuccess time.Time
}

func recordSynthesis(audio Audio, err error) {
	runtimeHealth.Lock()
	defer runtimeHealth.Unlock()
	if err != nil {
		runtimeHealth.lastError = err.Error()
		return
	}
	runtimeHealth.lastEngine = audio.Engine
	runtimeHealth.lastVoice = audio.Voice
	runtimeHealth.lastError = ""
	runtimeHealth.lastSuccess = time.Now()
}

func Status() RuntimeStatus {
	status := RuntimeStatus{WorkerState: "stopped"}
	globalKokoroWorker.mu.Lock()
	if globalKokoroWorker.cmd != nil && globalKokoroWorker.cmd.Process != nil {
		status.WorkerState = "ready"
		status.WorkerPID = globalKokoroWorker.cmd.Process.Pid
	}
	globalKokoroWorker.mu.Unlock()
	runtimeHealth.Lock()
	status.LastEngine = runtimeHealth.lastEngine
	status.LastVoice = runtimeHealth.lastVoice
	status.LastError = runtimeHealth.lastError
	status.LastSuccess = runtimeHealth.lastSuccess
	runtimeHealth.Unlock()
	return status
}

func RestartWorker() {
	globalKokoroWorker.mu.Lock()
	globalKokoroWorker.stop()
	globalKokoroWorker.mu.Unlock()
}

func ShutdownWorkers() {
	globalKokoroWorker.mu.Lock()
	globalKokoroWorker.stop()
	globalKokoroWorker.mu.Unlock()
	globalWhisperWorker.mu.Lock()
	globalWhisperWorker.stop()
	globalWhisperWorker.mu.Unlock()
}

func Synthesize(ctx context.Context, text string, opts TTSOptions) (result Audio, resultErr error) {
	defer func() { recordSynthesis(result, resultErr) }()
	text = cleanForTTS(text, opts.MaxChars)
	if text == "" {
		return Audio{}, fmt.Errorf("empty voice text")
	}
	engine := strings.ToLower(strings.TrimSpace(opts.Engine))
	if engine == "" {
		engine = "auto"
	}
	switch engine {
	case "kokoro", "auto":
		audio, err := synthesizeKokoro(ctx, text, opts)
		if err == nil || engine == "kokoro" {
			return audio, err
		}
		piperAudio, piperErr := synthesizePiper(ctx, text, opts)
		if piperErr != nil {
			return Audio{}, fmt.Errorf("kokoro failed: %v; piper fallback failed: %w", err, piperErr)
		}
		return piperAudio, nil
	case "piper":
		return synthesizePiper(ctx, text, opts)
	default:
		return Audio{}, fmt.Errorf("unsupported TTS engine %q", opts.Engine)
	}
}

func SynthesizeVoiceNote(ctx context.Context, text string, opts TTSOptions) (Audio, error) {
	audio, err := Synthesize(ctx, text, opts)
	if err != nil {
		return Audio{}, err
	}
	if audio.MIMEType == "audio/ogg" {
		return audio, nil
	}
	ogg, err := wavToOggOpus(ctx, audio.Data)
	if err != nil {
		return Audio{}, err
	}
	audio.Data = ogg
	audio.MIMEType = "audio/ogg"
	audio.FileExt = "ogg"
	return audio, nil
}

func synthesizeKokoro(ctx context.Context, text string, opts TTSOptions) (Audio, error) {
	if strings.TrimSpace(os.Getenv("MAULER_KOKORO_WORKER")) != "0" {
		if audio, err := synthesizeKokoroWorker(ctx, text, opts); err == nil {
			return audio, nil
		}
	}
	return synthesizeKokoroCLI(ctx, text, opts)
}

func synthesizeKokoroCLI(ctx context.Context, text string, opts TTSOptions) (Audio, error) {
	python := strings.TrimSpace(firstNonEmpty(opts.KokoroPython, os.Getenv("MAULER_KOKORO_PYTHON")))
	if python == "" {
		python = firstCommand("python", "py", "python3")
	}
	if python == "" {
		return Audio{}, fmt.Errorf("python not found; install Python and `pip install kokoro soundfile`, or set MAULER_TTS_ENGINE=piper")
	}
	voice := strings.TrimSpace(firstNonEmpty(opts.Voice, os.Getenv("MAULER_KOKORO_VOICE"), DefaultKokoroVoice))
	speed := opts.Speed
	if speed <= 0 {
		speed = 1
	}
	dir, err := os.MkdirTemp("", "mauler-kokoro-*")
	if err != nil {
		return Audio{}, err
	}
	defer os.RemoveAll(dir)
	wavPath := filepath.Join(dir, "reply.wav")
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, python,
		"-m", "kokoro",
		"--text", text,
		"--output-file", wavPath,
		"--voice", voice,
		"--speed", fmt.Sprintf("%.3f", speed),
	)
	hideChildProcessWindow(cmd)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	if out, err := cmd.CombinedOutput(); err != nil {
		return Audio{}, fmt.Errorf("kokoro tts failed: %v: %s", err, truncate(string(out), 800))
	}
	data, err := os.ReadFile(wavPath)
	if err != nil {
		return Audio{}, err
	}
	if len(data) < 128 {
		return Audio{}, fmt.Errorf("kokoro produced empty audio (%d bytes)", len(data))
	}
	return Audio{Data: data, MIMEType: "audio/wav", FileExt: "wav", Engine: "kokoro", Voice: voice}, nil
}

type kokoroWorkerRequest struct {
	Text  string  `json:"text"`
	Voice string  `json:"voice"`
	Speed float64 `json:"speed"`
}

type kokoroWorkerResponse struct {
	OK    bool   `json:"ok"`
	WAV   string `json:"wav,omitempty"`
	Error string `json:"error,omitempty"`
	Voice string `json:"voice,omitempty"`
}

type kokoroWorker struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *json.Decoder
	python string
	script string
}

var globalKokoroWorker kokoroWorker

func synthesizeKokoroWorker(ctx context.Context, text string, opts TTSOptions) (Audio, error) {
	python := strings.TrimSpace(firstNonEmpty(opts.KokoroPython, os.Getenv("MAULER_KOKORO_PYTHON")))
	if python == "" {
		python = firstCommand("python", "py", "python3")
	}
	if python == "" {
		return Audio{}, fmt.Errorf("python not found")
	}
	voice := strings.TrimSpace(firstNonEmpty(opts.Voice, os.Getenv("MAULER_KOKORO_VOICE"), DefaultKokoroVoice))
	speed := opts.Speed
	if speed <= 0 {
		speed = 1
	}
	return globalKokoroWorker.synthesize(ctx, python, text, voice, speed)
}

func (w *kokoroWorker) synthesize(ctx context.Context, python, text, voice string, speed float64) (Audio, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ensureStarted(python); err != nil {
		w.stop()
		return Audio{}, err
	}
	req := kokoroWorkerRequest{Text: text, Voice: voice, Speed: speed}
	line, err := json.Marshal(req)
	if err != nil {
		return Audio{}, err
	}
	type result struct {
		resp kokoroWorkerResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if _, err := w.stdin.Write(append(line, '\n')); err != nil {
			ch <- result{err: err}
			return
		}
		var resp kokoroWorkerResponse
		if err := w.stdout.Decode(&resp); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{resp: resp}
	}()
	select {
	case <-ctx.Done():
		w.stop()
		return Audio{}, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			w.stop()
			return Audio{}, res.err
		}
		if !res.resp.OK {
			return Audio{}, fmt.Errorf("kokoro worker failed: %s", res.resp.Error)
		}
		data, err := base64.StdEncoding.DecodeString(res.resp.WAV)
		if err != nil {
			w.stop()
			return Audio{}, err
		}
		if len(data) < 128 {
			return Audio{}, fmt.Errorf("kokoro worker produced empty audio (%d bytes)", len(data))
		}
		return Audio{Data: data, MIMEType: "audio/wav", FileExt: "wav", Engine: "kokoro", Voice: firstNonEmpty(res.resp.Voice, voice)}, nil
	}
}

func (w *kokoroWorker) ensureStarted(python string) error {
	if w.cmd != nil && w.cmd.Process != nil && w.python == python {
		return nil
	}
	w.stop()
	script, err := ensureKokoroWorkerScript()
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
		return fmt.Errorf("start kokoro worker: %w", err)
	}
	w.cmd = cmd
	w.stdin = stdin
	w.stdout = json.NewDecoder(stdout)
	w.python = python
	w.script = script
	return nil
}

func (w *kokoroWorker) stop() {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_, _ = w.cmd.Process.Wait()
	}
	w.cmd = nil
	w.stdin = nil
	w.stdout = nil
	w.python = ""
}

func ensureKokoroWorkerScript() (string, error) {
	dir := filepath.Join(os.TempDir(), "mauler-audio")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "kokoro_worker.py")
	if err := os.WriteFile(path, []byte(kokoroWorkerScript), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

const kokoroWorkerScript = `import base64, io, json, sys, traceback
import numpy as np
import soundfile as sf
from kokoro import KPipeline

pipelines = {}

def lang_for_voice(voice):
    voice = (voice or "af_heart").strip()
    if voice.startswith("b"):
        return "b"
    return "a"

def get_pipeline(voice):
    lang = lang_for_voice(voice)
    if lang not in pipelines:
        pipelines[lang] = KPipeline(lang_code=lang, repo_id="hexgrad/Kokoro-82M")
    return pipelines[lang]

def tensor_to_numpy(audio):
    if hasattr(audio, "detach"):
        audio = audio.detach().cpu().numpy()
    return np.asarray(audio, dtype=np.float32)

for line in sys.stdin:
    try:
        req = json.loads(line)
        text = (req.get("text") or "").strip()
        voice = (req.get("voice") or "af_heart").strip()
        speed = float(req.get("speed") or 1.0)
        if not text:
            raise ValueError("empty text")
        parts = []
        for result in get_pipeline(voice)(text, voice=voice, speed=speed):
            parts.append(tensor_to_numpy(result.audio))
        if not parts:
            raise RuntimeError("no audio generated")
        audio = np.concatenate(parts) if len(parts) > 1 else parts[0]
        buf = io.BytesIO()
        sf.write(buf, audio, 24000, format="WAV")
        wav = base64.b64encode(buf.getvalue()).decode("ascii")
        print(json.dumps({"ok": True, "wav": wav, "voice": voice}), flush=True)
    except Exception as exc:
        print(json.dumps({"ok": False, "error": str(exc), "trace": traceback.format_exc(limit=3)}), flush=True)
`

func synthesizePiper(ctx context.Context, text string, opts TTSOptions) (Audio, error) {
	piper, model, config, err := resolvePiperInstall(opts)
	if err != nil {
		return Audio{}, err
	}
	dir, err := os.MkdirTemp("", "mauler-piper-*")
	if err != nil {
		return Audio{}, err
	}
	defer os.RemoveAll(dir)
	wavPath := filepath.Join(dir, "reply.wav")
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, piper, "--model", model, "--config", config, "--output_file", wavPath)
	hideChildProcessWindow(cmd)
	cmd.Stdin = strings.NewReader(text)
	cmd.Dir = filepath.Dir(piper)
	if out, err := cmd.CombinedOutput(); err != nil {
		return Audio{}, fmt.Errorf("piper tts failed: %v: %s", err, truncate(string(out), 800))
	}
	data, err := os.ReadFile(wavPath)
	if err != nil {
		return Audio{}, err
	}
	if len(data) < 128 {
		return Audio{}, fmt.Errorf("piper produced empty audio (%d bytes)", len(data))
	}
	return Audio{Data: data, MIMEType: "audio/wav", FileExt: "wav", Engine: "piper", Voice: piperVoice(opts)}, nil
}

func resolvePiperInstall(opts TTSOptions) (piperPath, modelPath, configPath string, err error) {
	piperPath = firstExistingFile(append([]string{
		opts.PiperPath,
		os.Getenv("MAULER_PIPER_PATH"),
	}, joinDataDirs(opts.DataDirs, "piper", executableName("piper"))...)...)
	if piperPath == "" {
		piperPath = firstCommand("piper")
	}
	if piperPath == "" {
		return "", "", "", fmt.Errorf("piper executable not found; set MAULER_PIPER_PATH or MAULER_TTS_ENGINE=kokoro")
	}
	voice := piperVoice(opts)
	modelPath = firstExistingFile(append([]string{
		opts.PiperModel,
		os.Getenv("MAULER_PIPER_MODEL"),
	}, joinDataDirs(opts.DataDirs, "voices", voice+".onnx")...)...)
	configPath = firstExistingFile(append([]string{
		opts.PiperConfig,
		os.Getenv("MAULER_PIPER_CONFIG"),
	}, joinDataDirs(opts.DataDirs, "voices", voice+".onnx.json")...)...)
	if modelPath == "" || configPath == "" {
		return "", "", "", fmt.Errorf("Piper voice model/config not found; set MAULER_PIPER_MODEL and MAULER_PIPER_CONFIG")
	}
	return piperPath, modelPath, configPath, nil
}

func wavToOggOpus(ctx context.Context, wav []byte) ([]byte, error) {
	ffmpeg := firstCommand("ffmpeg")
	if ffmpeg == "" {
		return nil, fmt.Errorf("ffmpeg not found in PATH")
	}
	dir, err := os.MkdirTemp("", "mauler-voice-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	wavPath := filepath.Join(dir, "in.wav")
	oggPath := filepath.Join(dir, "out.ogg")
	if err := os.WriteFile(wavPath, wav, 0o644); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, ffmpeg, "-y", "-hide_banner", "-loglevel", "error", "-i", wavPath, "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", oggPath)
	hideChildProcessWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg voice conversion failed: %v: %s", err, truncate(string(out), 800))
	}
	data, err := os.ReadFile(oggPath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("voice conversion produced empty file")
	}
	return data, nil
}

func cleanForTTS(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars > 0 && len([]rune(text)) > maxChars {
		runes := []rune(text)
		text = string(runes[:maxChars])
		if idx := strings.LastIndexAny(text, ".!?;\n"); idx > maxChars/2 {
			text = text[:idx+1]
		}
	}
	return strings.TrimSpace(text)
}

func piperVoice(opts TTSOptions) string {
	return firstNonEmpty(opts.Voice, os.Getenv("MAULER_PIPER_VOICE"), DefaultPiperVoice)
}

func joinDataDirs(dirs []string, parts ...string) []string {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		all := append([]string{dir}, parts...)
		out = append(out, filepath.Join(all...))
	}
	return out
}

func firstExistingFile(paths ...string) string {
	for _, path := range paths {
		path = strings.Trim(strings.TrimSpace(path), `"`)
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func firstCommand(names ...string) string {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func executableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncate(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	var b bytes.Buffer
	b.WriteString(string(runes[:limit]))
	b.WriteString("...")
	return b.String()
}
