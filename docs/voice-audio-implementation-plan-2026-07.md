# Voice / audio (STT + TTS) implementation plan — 2026-07-02

**Audience:** an AI coding agent (Codex/Claude) implementing natural voice conversation in TheMauler.
**Goal (user's words):** *"speech to text, text to speech and all audio… best one I can get for speed
and talking naturally with my AI."* Target: talk to the agent and hear it talk back, low-latency,
natural, fully local on an RTX 3090.

**Architecture decision (settled): put ALL audio in the Mauler, leave InferenceBridge unchanged.**
Reasoning below. `go build ./... && go test ./internal/...` + `cd frontend && npm run build` to verify.

---

## 1. Recommended stack (best speed + naturalness, local, 2026)

Grounded in current benchmarks (sources at bottom). On a 3090 + English coding/pentest use:

| Role | Primary pick | Why | Fallback |
|---|---|---|---|
| **STT** | **Parakeet TDT 0.6B v3** (NVIDIA) | True streaming (80–1120 ms configurable), ~6.3% WER, **almost never hallucinates during silence** (critical for open-mic), RTFx ~3300. ~1.2 GB VRAM. | **faster-whisper large-v3-turbo** — multilingual (99 langs), 8× faster decode than large-v3, but chunked (not true streaming) and hallucinates in silence. |
| **TTS** | **Kokoro-82M** | Efficiency+naturalness champion: **RTF 0.03 on a 3090** (~33× real-time), ~45 ms first-audio, streaming, 54 voices, <2 GB VRAM, Apache-2.0. Single forward pass (no diffusion). | **Piper** (lowest integration cost, ultra-fast, robotic-ish) for v1; **Chatterbox-Turbo** (~6 GB, most natural + voice cloning, English-only, watermarked) as a quality upgrade. |
| **VAD / endpointing** | **Silero VAD** via `@ricky0123/vad-web` (browser WASM) | Tiny, fast, the standard. Handles speech start/stop + barge-in in the frontend. | Silero VAD in Go (onnx) if you prefer server-side. |

**VRAM budget (24 GB):** 35B-A3B LLM Q4 (~20 GB) + Parakeet 0.6B (~1.2 GB) + Kokoro 82M (<2 GB) fits.
If the LLM is loaded near-full via InferenceBridge, confirm ~3 GB headroom remains (drop LLM ctx or
quant slightly if not). This is the one cross-component risk — everything else is independent.

**Unified engine option (strongly recommended):** **`sherpa-onnx` (k2-fsa)** runs Parakeet/Whisper
STT **and** Kokoro/Piper TTS through **one library with a C API and Go bindings**
(`github.com/k2-fsa/sherpa-onnx-go`), ONNX Runtime + CUDA. This lets TheMauler do all audio
**in-process in Go — no Python sidecar**. Verify current Kokoro/Parakeet model support in the
sherpa-onnx release you pin; if a model isn't yet wired there, use the sidecar path (§3) for it.

---

## 2. Architecture: Mauler-only (not InferenceBridge)

**Do it all in the Mauler.** Audio capture, playback, VAD, STT, and TTS live where the microphone,
speakers, and UI are — the Wails desktop app. The InferenceBridge stays a pure LLM token server and
is not touched.

Why:
- Mic/speaker are **client** concerns; the bridge has no business handling them.
- STT/TTS are **small models** the Mauler can host in-process (sherpa-onnx) or as a supervised local
  sidecar — the same way it already supervises shells and the llama.cpp server.
- Keeps InferenceBridge **single-responsibility and unchanged**, so the shared HelixClaw backend
  carries zero risk from this feature.
- If HelixClaw later wants voice, **promote** the STT/TTS engine to a shared local service then —
  don't couple prematurely.

```
   ┌─────────────────────────── TheMauler (Wails) ───────────────────────────┐
   │  Frontend (WebView2)                     Go backend                       │
   │  mic ─getUserMedia→ AudioWorklet ─PCM→   audio ingest → STT (Parakeet)    │
   │  Silero VAD (@ricky0123/vad-web)         partials/final ↑                 │
   │        │ barge-in                         final transcript → runAgentLoop │
   │  speaker ◀─WebAudio queue◀─PCM chunks◀── TTS (Kokoro) ◀ sentence-chunker ◀│ LLM delta stream
   └──────────────────────────────────────────────────────────────────────────┘
                                                     │ (unchanged)
                                                     ▼  InferenceBridge → llama.cpp (LLM tokens only)
```

---

## 3. The pipeline (this is what makes it feel natural)

1. **Capture:** frontend `getUserMedia` → `AudioWorklet` downsamples to 16 kHz mono PCM → frames to Go.
2. **VAD:** `@ricky0123/vad-web` (Silero) in the frontend flags speech start/stop; only speech frames
   are streamed to Go (saves compute, kills silence-hallucination).
3. **STT:** Go feeds frames to Parakeet streaming; emits **partial** transcripts live (`mauler:stt_partial`)
   and a **final** on VAD endpoint (`mauler:stt_final`).
4. **To the agent:** the final transcript is submitted as the user turn through the **existing**
   `SendMessage` / `runAgentLoop` path — no loop changes needed.
5. **Sentence-streamed TTS (the key latency trick):** hook the existing `mauler:delta` token stream
   into a **sentence/clause chunker**; each completed clause is synthesized immediately, so speech
   starts ~1 sentence after generation begins, not after the whole reply. Audio chunks →
   `mauler:tts_audio` → frontend Web Audio queue plays them back-to-back.
6. **Barge-in:** if VAD detects the user speaking during playback, the frontend stops the audio queue
   and calls the existing `StopAgent` to cancel the in-flight LLM+TTS, then starts a new user turn.
   This is what makes it feel like a conversation instead of walkie-talkie.

Modes: **push-to-talk** (hold a key, simplest, ship first) and **open-mic / hands-free** (VAD-driven,
barge-in enabled). Make it a setting.

---

## 4. Implementation plan (files + anchors)

### Backend (Go)
- **`internal/audio/`** (new package):
  - `stt.go` — STT engine interface + Parakeet/Whisper impl (sherpa-onnx-go, CUDA). `Transcribe(stream)`
    → partial/final events.
  - `tts.go` — TTS engine interface + Kokoro/Piper impl. `Synthesize(text, voice) → PCM chunks`.
  - `sentence_chunker.go` — accumulates LLM deltas, emits complete clauses on `. ? ! ;` / newline
    boundaries with a min-length guard so it doesn't synthesize fragments.
  - `engine_sidecar.go` *(fallback path)* — supervise a local `python -m mauler_audio` FastAPI/websocket
    sidecar (faster-whisper + kokoro-onnx) launched/killed like the shell sessions; use only if
    sherpa-onnx lacks a model you need.
- **Wails bindings** (in `internal/app/app.go`, near the other bindings):
  - `StartVoice(mode string) error`, `StopVoice() error`, `PushAudioFrame(b64pcm string)`,
    `SetVoice(id string)`, `ListVoices() []Voice`.
  - New events: `mauler:stt_partial`, `mauler:stt_final`, `mauler:tts_audio` (b64 PCM), `mauler:tts_done`,
    `mauler:voice_state`.
  - Reuse `StopAgent` for barge-in cancellation; reuse `SendMessage` for the final transcript.
- **Sentence-chunker hook:** where `mauler:delta` is emitted in `runAgentLoop`
  (`app.go` ~2750, the `a.emit("mauler:delta", chunk)` site), tee visible text into the chunker when
  voice output is active. Gate on a run-scoped `voiceActive` flag so text-only runs are unaffected.

### Frontend (React, mirror `TerminalPane.tsx` event-wiring style)
- **`components/VoicePane.tsx`** (or a mic button in `ChatPane`): getUserMedia + AudioWorklet capture,
  `@ricky0123/vad-web` VAD, push-to-talk key + open-mic toggle, live partial-transcript display,
  Web Audio playback queue for `mauler:tts_audio`, barge-in wiring.
- **`worklets/pcm-capture.js`** — AudioWorklet that downsamples to 16 kHz mono and posts PCM frames.
- Wire the new `mauler:*` events in `App.tsx` alongside the existing ones.

### Settings (`internal/settings/model.go` + `defaults.go`)
Add `AudioConfig`:
```
Enabled        bool     // master switch
Mode           string   // "push_to_talk" | "open_mic"
STTEngine      string   // "parakeet" | "whisper_turbo" | "sidecar"
TTSEngine      string   // "kokoro" | "piper" | "chatterbox"
Voice          string   // voice id (Kokoro has 54)
Speed          float64  // TTS rate
InputDevice    string   // "" = default
VADThreshold   float64  // Silero sensitivity
BargeIn        bool     // interrupt playback on user speech
SpeakToolNotes bool     // read short status/tool notes aloud (optional)
```
Add sane defaults + a `Validate()` clamp; surface in `SettingsModal.tsx` as an "Audio / Voice" tab.

---

## 5. Milestones (ship incrementally, each is usable)

- **M1 — working loop (push-to-talk, sidecar).** Python sidecar (faster-whisper large-v3-turbo +
  Kokoro) launched/supervised by the Mauler. Hold-to-talk → transcript → agent → full-reply TTS
  playback. Proves UX in days. *No streaming yet.*
- **M2 — natural streaming.** Sentence-chunked TTS on the delta stream (speech starts mid-generation) +
  live STT partials + open-mic VAD + **barge-in**. This is the "talks naturally" milestone.
- **M3 — in-process + quality.** Replace the sidecar with **sherpa-onnx-go** (Parakeet + Kokoro,
  no Python). Add voice picker, speed control, and Chatterbox as an optional natural/cloning voice.
- **M4 — polish (optional).** Earcons for tool events, "read this file/finding aloud," wake word,
  per-lab voice, and a "hands-free Ops" mode that narrates progress.

---

## 6. Risks & verification

- **VRAM contention** with the LLM — the only cross-component risk (§1). Verify headroom; add a Doctor
  check that STT+TTS+LLM fit, mirroring the context-shortfall check proposed in the settings audit.
- **sherpa-onnx model coverage** — pin a release that ships Parakeet + Kokoro; else keep those on the
  sidecar. Don't block M1/M2 on it.
- **Echo/feedback** in open-mic (speaker → mic) — enable browser AEC (`echoCancellation: true` in
  getUserMedia) and pause capture during playback if AEC is insufficient.
- **Latency target:** end-to-end query→first-spoken-word ~680 ms is documented as achievable with this
  class of stack; treat >1.2 s as a regression to investigate (usually the LLM TTFT, not audio).
- **Test:** unit-test the sentence-chunker (delta stream → clause boundaries) and the STT/TTS engine
  interfaces with recorded fixtures; the audio I/O itself is validated live in `wails dev`.

---

## Sources (2026)
- [Best open-source STT 2026 (benchmarks) — Northflank](https://northflank.com/blog/best-open-source-speech-to-text-stt-model-in-2026-benchmarks)
- [Parakeet vs Whisper 2026 — Local AI Master](https://localaimaster.com/blog/parakeet-vs-whisper)
- [Local STT: Moonshine vs Parakeet vs Whisper — onResonant](https://www.onresonant.com/resources/local-stt-models-2026)
- [Best Local TTS Models 2026 — Local AI Master](https://localaimaster.com/blog/best-local-tts-models)
- [Kokoro vs XTTS vs Chatterbox 2026 — Local AI Master](https://localaimaster.com/blog/kokoro-vs-xtts-vs-chatterbox)
- [Kokoro vs XTTS-v2 low-latency — GIGAGPU](https://gigagpu.com/kokoro-vs-xtts-v2-low-latency-tts/)
- [Streaming TTS: Kokoro, ElevenLabs Turbo, Cartesia, OpenAI — Forasoft](https://www.forasoft.com/learn/ai-for-video-engineering/articles-ai/streaming-tts-kokoro-elevenlabs-turbo-openai-tts)
- [Best Open Source TTS 2026 — Speakeasy](https://www.tryspeakeasy.io/blog/open-source-text-to-speech-2026)
