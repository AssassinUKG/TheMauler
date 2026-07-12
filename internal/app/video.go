package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// VideoIngest is the result of decoding a pasted or dropped video clip into
// material a vision-capable model can actually consume: a set of sampled
// keyframes (returned as image data URIs, reusing the existing image path) plus
// an optional transcript of the audio track.
type VideoIngest struct {
	Frames     []string `json:"frames"`     // "data:image/jpeg;base64,..." keyframes, chronological
	Transcript string   `json:"transcript"` // audio transcript, may be empty
	Duration   float64  `json:"duration"`   // seconds, 0 when unknown
	FrameCount int      `json:"frameCount"`
	Note       string   `json:"note"` // human-readable summary + any warnings
}

// IngestVideo decodes a base64 data URI (e.g. a pasted clip) into keyframes and
// an optional transcript. filename is used only to pick a sensible extension.
func (a *App) IngestVideo(dataURI, filename string) (VideoIngest, error) {
	b64, mediaType, ok := parseDataURI(dataURI)
	if !ok {
		return VideoIngest{}, fmt.Errorf("not a data URI")
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return VideoIngest{}, fmt.Errorf("decode video: %w", err)
	}
	tmp, err := os.CreateTemp("", "mauler-video-*"+videoExt(filename, mediaType))
	if err != nil {
		return VideoIngest{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return VideoIngest{}, err
	}
	tmp.Close()
	return a.ingestVideoFile(tmp.Name())
}

// IngestVideoPath decodes a video already on disk (e.g. a dropped file path).
func (a *App) IngestVideoPath(path string) (VideoIngest, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return VideoIngest{}, fmt.Errorf("empty path")
	}
	if _, err := os.Stat(path); err != nil {
		return VideoIngest{}, fmt.Errorf("video not found: %w", err)
	}
	return a.ingestVideoFile(path)
}

func (a *App) ingestVideoFile(path string) (VideoIngest, error) {
	a.mu.Lock()
	cfg := a.cfg.Image
	a.mu.Unlock()

	if !cfg.VideoEnabled {
		return VideoIngest{}, fmt.Errorf("video ingestion is disabled in settings")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return VideoIngest{}, fmt.Errorf("ffmpeg not found in PATH — install it to analyze video (winget install ffmpeg)")
	}

	maxFrames := cfg.VideoMaxFrames
	if maxFrames <= 0 {
		maxFrames = 8
	}
	if maxFrames > 32 {
		maxFrames = 32
	}
	frameWidth := cfg.VideoFrameWidth
	if frameWidth <= 0 {
		frameWidth = 768
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	duration := probeVideoDuration(ctx, path)

	frames, err := extractVideoFrames(ctx, path, maxFrames, frameWidth, duration)
	if err != nil {
		return VideoIngest{}, err
	}
	if len(frames) == 0 {
		return VideoIngest{}, fmt.Errorf("no frames could be extracted from the video")
	}

	transcript := ""
	if cfg.VideoTranscribe {
		transcript = transcribeVideoAudio(ctx, path)
	}

	res := VideoIngest{
		Frames:     frames,
		Transcript: transcript,
		Duration:   duration,
		FrameCount: len(frames),
		Note:       videoIngestNote(len(frames), duration, transcript),
	}
	return res, nil
}

// probeVideoDuration returns the clip length in seconds, or 0 when ffprobe is
// unavailable or the probe fails.
func probeVideoDuration(ctx context.Context, path string) float64 {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return 0
	}
	out, err := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// extractVideoFrames samples up to maxFrames keyframes evenly across the clip,
// scaled to frameWidth, and returns them as JPEG data URIs in chronological
// order.
func extractVideoFrames(ctx context.Context, path string, maxFrames, frameWidth int, duration float64) ([]string, error) {
	outDir, err := os.MkdirTemp("", "mauler-frames-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(outDir)

	// Even sampling: aim for maxFrames across the whole duration. When the
	// duration is unknown, fall back to one frame every two seconds.
	var fps string
	if duration > 0.001 {
		fps = fmt.Sprintf("%.6f", float64(maxFrames)/duration)
	} else {
		fps = "0.5"
	}
	vf := fmt.Sprintf("fps=%s,scale=%d:-2:flags=lanczos", fps, frameWidth)
	pattern := filepath.Join(outDir, "frame_%04d.jpg")

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", path,
		"-vf", vf,
		"-frames:v", strconv.Itoa(maxFrames),
		"-q:v", "3",
		pattern,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg frame extraction failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	matches, _ := filepath.Glob(filepath.Join(outDir, "frame_*.jpg"))
	sort.Strings(matches)
	frames := make([]string, 0, len(matches))
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil || len(data) == 0 {
			continue
		}
		frames = append(frames, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(data))
	}
	return frames, nil
}

// transcribeVideoAudio extracts the audio track to a 16 kHz mono WAV and runs
// local whisper transcription. Returns "" when the clip has no audio, ffmpeg
// fails, or whisper is not installed — audio is best-effort.
func transcribeVideoAudio(ctx context.Context, path string) string {
	dir, err := os.MkdirTemp("", "mauler-vaudio-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	wav := filepath.Join(dir, "audio.wav")
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", path,
		"-vn", "-ac", "1", "-ar", "16000",
		wav,
	)
	if err := cmd.Run(); err != nil {
		return ""
	}
	info, err := os.Stat(wav)
	if err != nil || info.Size() < 1024 {
		return ""
	}
	return runLocalWhisperTranscription(ctx, wav)
}

func videoIngestNote(frameCount int, duration float64, transcript string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Attached video sampled into %d keyframe%s", frameCount, plural(frameCount, "", "s"))
	if duration > 0 {
		fmt.Fprintf(&b, " across %.0fs", duration)
	}
	b.WriteString(".")
	if strings.TrimSpace(transcript) != "" {
		b.WriteString(" Audio transcript included.")
	}
	return b.String()
}

func videoExt(filename, mediaType string) string {
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename))); ext != "" {
		return ext
	}
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "video/mp4", "video/quicktime":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/x-matroska":
		return ".mkv"
	case "video/x-msvideo":
		return ".avi"
	default:
		return ".mp4"
	}
}
