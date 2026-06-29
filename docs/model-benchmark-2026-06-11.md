# Model benchmark — Qwen3.6-27B Q4_K_P on InferenceBridge / RTX 3090 (2026-06-11)

Live tests against the loaded model (`Qwen3.6-27B-Uncensored-HauhauCS-Aggressive-Q4_K_P.gguf`,
`http://127.0.0.1:8802`, **loaded at n_ctx = 32000**, max model ctx 262144).

## Raw results

**Generation throughput:** steady **~35 tok/s** at all sizes (256-out and 900-out runs both
~35). Good for a 27B Q4 on a 3090.

**Prompt processing (input ingestion):**
| input tokens | prompt-eval tok/s |
|---|---|
| 1.2k | ~1040 |
| 4.4k | ~1340 |
| 8.7k | ~2010 |
| 15k  | ~2300 |

Scales up with batch size; ~2000+ t/s on large inputs. A 16k-token file ingests in ~7s.

**Reasoning effort (speed lever):**
| effort | wall (same prompt) | notes |
|---|---|---|
| none   | 7.5s  | fastest, clean output |
| medium | 8.9s  | (default in /v1/models) |
| low    | 17.1s | emitted 2.2k thinking chars, hit max_tokens |
| high   | 17.2s | |

On this *aggressive* finetune, thinking is inconsistent and **eats the output budget**.
`effort=none` (the `nothink` profile) is fastest and cleanest — keep it.

**Large single-shot write:** a ~3000-word doc = **4096 tokens in 117s (~35 t/s), hit the
length cap** → big files must be written in chunks (append), which TheMauler's auto-continue
already does.

## Context-size comparison (reloaded via InferenceBridge `/v1/models/load`)

Reloaded the **same model** at three context windows and re-benchmarked. Loads took ~10s; no
OOM at any size (24GB RTX 3090).

| loaded n_ctx | gen tok/s | prompt-eval tok/s (large input) | notes |
|---|---|---|---|
| 32000 | ~35 | ~2300 (15k in) | original |
| 49152 | ~35.5 | ~2433 (20k in) | no change |
| 65536 | ~35.0 | ~1827 (46k in, ingests in ~25s) | no change, **loads clean** |

**Conclusion: bigger context is essentially free here.** Generation stays ~35 tok/s and
prompt-eval stays GPU-resident (no spill) up to 65536. A near-full 46k-token prompt ingests in
~25s. So there's no reason to stay at 32k — **65536 is now the default** for the nothink/think
profiles (set in `profiles.toml`). TheMauler loads the model at the profile's `ctx_tokens`
automatically (`loadLlamaCppModel` → `/v1/models/load` with `context_size`), so just restart
the app; no manual InferenceBridge relaunch needed. Dial back to 49152 only if you need VRAM
margin for a second concurrent model.

## What this means

### Speed
- **Keep `qwen3.6-nothink` (reasoning effort none).** Confirmed fastest; thinking on this
  model is unreliable and wastes tokens. Use a thinking profile only when you specifically
  want deeper multi-step planning and can eat ~2× latency.
- Generation is the bottleneck (~35 t/s), not prompt ingestion. Latency ≈ output length / 35.
  Keep responses tight; prefer tools/files over long prose.
- For *quick* throwaway tasks, the **Qwen3.5-9B** (also on the bridge, not loaded) would run
  ~2-3× faster generation — worth a fast profile for trivial edits, but keep 27B for hacking.

### Large files
- **Reading is fast but context-expensive at 32k.** Prompt-eval ~2000 t/s means a 16k-token
  file loads in ~7s — but that's *half your 32k window*. At 32k you can't hold a big file +
  the working transcript. **Raise n_ctx to 49–64k** (you already run `qwen3.6-large-code` at
  64k; prompt-eval is fast, so bigger context costs little time). This is the main fix for
  "works with large files."
- **Writing is slow and capped.** ~35 t/s and limited by per-turn `max_tokens`, so a big file
  = several chunked appends. Two tunables help:
  - Raise the agent's **max output tokens** (e.g. 2048–4096) so each chunk is bigger and there
    are fewer round-trips.
  - Expect ~1 min per ~2k tokens written regardless — chunked `write_file(append=true)` is the
    right pattern (already implemented), don't try to one-shot a huge file.

## Recommended settings (applied 2026-06-11)
- Profile: `qwen3.6-nothink` (effort none) for speed; reserve `qwen3.6-think` for hard privesc.
- **`ctx_tokens = 65536`** set on both Qwen profiles in `profiles.toml` — tested clean, no
  speed cost. TheMauler loads the model at this context automatically on next run (no manual
  InferenceBridge relaunch). This is the biggest win for depth + large files.
- **Single-shot output raised to 16384** (nothink `nothinking` + think `general` blocks) so big
  file writes need fewer chunks. Still ~35 t/s, so very large files chunk via
  `write_file(append=true)` as before.
- Sampling is already at the model's live defaults (temp 1.0 / top_p 0.95 / top_k 20 /
  min_p 0.05 / rep 1.0); TheMauler lowers temperature per mode for coding/tools, which is right.
- Verified VRAM-safe on the 24GB 3090 at 65536 (load error null, no KV spill).
