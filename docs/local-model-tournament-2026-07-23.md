# Local model tournament results — 2026-07-23

> **Measurement correction:** the original matrix UI averaged text and tool-call throughput under a
> column labelled `Text tok/s`. When InferenceBridge omitted llama.cpp `timings`, it also fell back
> to completion tokens divided by the complete cold request, so model switching and prompt
> processing could make a 120 tok/s decoder appear to run at 3–5 tok/s. Those legacy Gemma speed
> rows are retained below as historical results but are not valid decode-speed comparisons.
>
> Current Model Lab performs one excluded warm-up, then three measured text passes. It reports
> backend decode tok/s, warm end-to-end tok/s, timing source, load time, and warm TTFT separately.
> Tool-call and mini-loop latency never contribute to the headline text speed.

## Decision

There are three different winners:

1. **Best proven daily model:** `Qwen3.6-27B-UD-Q4_K_XL.gguf`. Its four-scenario Advanced Suite
   remains the strongest complete evidence: 90/100 at 54.8 tok/s, a clean native tool call, and a
   passing mini loop.
2. **Best proven explicitly unrestricted model:** 
   `Huihui-Qwen3.6-27B-abliterated-ggml-model-Q4_K.gguf`. It scored 90/100 at 51.5 tok/s in the
   Advanced Suite and passed the mini loop. It is the safer unrestricted quality choice than the
   newly tested small models.
3. **Fastest model with backend-reported decode timing:** `Qwen3.5-4B-Q4_K_M.gguf`. With its corrected Qwen3.5 template it produced
   125.4 matrix tok/s (127.3 tok/s in the text-only case), a clean structured tool call, and a
   passing mini loop. Its loop completed only 66% of attempted tool steps, so it is a fast helper,
   not yet the unattended default.

`Qwythos-9B-Claude-Mythos-5-1M-Q5_K_M.gguf` is fast and interesting, but not reliable enough yet.
With its corrected exact template it produced 83.5 matrix tok/s and a clean direct tool call, then
failed the read/write/verify loop at 40% tool success without creating the required file.

Gemma 4 12B improved after its template was repaired: the direct tool call passed and its mini loop
passed at score 50 / 80% tool success. Its old 3.5 matrix tok/s figure included cold/prompt time and
must be rerun with the repaired timing contract before making a throughput judgment.

## How to read current speed figures

All rows used InferenceBridge on the same RTX 3090 and requested 16,384 context tokens.

- **Decode tok/s** is shown only when InferenceBridge/llama.cpp returns authoritative timing
  telemetry. It excludes prompt processing and model load and is the closest match to the speed
  shown inside InferenceBridge Chat.
- **Warm E2E tok/s** is completion tokens divided by the complete measured request wall time. It
  includes prompt processing but excludes the separate model-load request and excluded warm-up.
- **Source** says `decode`, `E2E`, `est. E2E`, `mixed`, or `unknown`; never compare two rows without
  checking this field.
- **Load** is the model/context switch request. **Warm-up TTFT** is retained in run details but
  excluded from headline speed. **TTFT** is the average across the three measured text runs.
- **Tool** and **Loop** remain correctness/reliability evidence. Their latency is never averaged into
  text throughput.
- A direct tool pass proves that the server returned one structured call without repair. It does not
  prove that a multi-step agent loop will finish.
- A one-run mini loop is a smoke test, not pass^5 reliability evidence.

## Corrected-template rerun (legacy timing contract)

These rows were rerun after Mauler was rebuilt with exact Qwen3.5, Qwythos, and small-Gemma templates.

| Model | Matrix tok/s | Text tok/s | TTFT | Direct tool | Mini loop | Verdict |
|---|---:|---:|---:|---|---|---|
| Qwen3.5 4B Q4_K_M | **125.4** | **127.3** | 26.6 s | pass, 0 repairs | pass, score 24, 66%, 95 s | Fastest; needs deeper quality and repeated reliability tests |
| Qwythos 9B Q5_K_M | 83.5 | 82.9 | 33.8 s | pass, 0 repairs | **fail**, score 0, 40%, 197 s | Promising text/reasoning speed, not a dependable agent yet |
| Gemma 4 12B Q4_K_M | 3.5 | 5.3 | 26.8 s | pass, 0 repairs | pass, **score 50**, 80%, 99 s | Better loop than Qwen 4B in this run, but unusably slow |

The Qwythos failure was concrete: the run stopped rather than reaching `done`, and `summary.txt` was
missing. It had no repeated tool inputs and no false completion; this was failure to finish the work,
not a duplicate-action loop.

## Initial eight-model matrix (legacy timing contract)

This batch exposed the template-selection defect. The 26B Gemmas matched their correct family. The
small Gemmas and Qwen3.5-derived models initially inherited a Qwen3.6 profile name, so their loop
figures are retained as historical evidence but marked superseded where a corrected rerun exists.

| Model | Matrix tok/s | Text tok/s | Direct tool | Mini loop | Template status |
|---|---:|---:|---|---|---|
| Qwen3.5 4B Q4_K_M | 122.5 | 128.0 | pass | pass, score 66, 100% | superseded by corrected rerun |
| Qwythos 9B Q5_K_M | 85.2 | 85.2 | pass | pass, score 19, 66% | superseded by corrected rerun |
| Gemma 4 E4B Q4_K_M | 3.7 | 6.2 | pass | **fail**, score 0, 66%; policy violations | provisional; corrected rerun pending |
| Gemma 4 12B Q4_K_M | 3.2 | 4.8 | pass | **fail**, score 0, 20% | superseded by corrected rerun |
| SuperGemma 4 26B uncensored fast-v2 Q4_K_M | 3.2 | 4.9 | pass | pass, score 39, 100% | correct Gemma 26B template |
| Gemma 4 E4B Q8_0 | 3.2 | 5.1 | pass | pass, score 74, 100% | provisional; corrected rerun pending |
| Gemma 4 26B heretic UD Q4_K_XL | 3.1 | 4.6 | pass | pass, score 33, 83% | correct Gemma 26B template |
| Gemma 4 26B q4_0 heretic Q4_K_M | 3.1 | 4.7 | pass | pass, score 56, 100% | correct Gemma 26B template |

The corrected E4B Q4/Q8 batch remains pending. The desktop UI detected user input after the first
corrected batch, so automation stopped rather than competing with the user for the mouse and
keyboard. Until those two rows are rerun, do not interpret the E4B Q8 score 74 as a reliable win.

## Existing Qwen tournament baseline

The earlier fair six-model Qwen matrix used the same 16K shape, followed by a four-scenario Advanced
Suite for its finalists.

| Model | Matrix tok/s | Direct tool | Mini loop | Deeper evidence |
|---|---:|---|---|---|
| Qwen3.6 27B Unsloth UD-Q4_K_XL | 51.3 | pass | pass, score 43, 83% | **90/100, 54.8 tok/s Advanced Suite** |
| Huihui Qwen3.6 27B abliterated Q4_K | **52.6** | pass | pass, score 43, 83% | **90/100, 51.5 tok/s Advanced Suite** |
| Tess-4 27B Q4_K_M | 34.2 | pass | pass, score 43, 83% | matrix only |
| Qwen3.6 27B HauhauCS Aggressive Q4_K_P | 33.2 | pass | pass, score 36, 83% | matrix only |
| Qwen3.6 27B Fable/Fus Q4_K_M | 33.2 | pass | **fail**, score 24, 83% | matrix only |
| Qwen3.6 35B-A3B heretic Native-MTP Q4_K_M | 10.9 | **fail** | **fail**, score 0, 50% | not recommended |

See `qwen-local-model-tournament-2026-07-21.md` for scenario-level finalist results and the
grammar-constrained caveat.

## Per-model tuning and next action

### Qwen3.5 4B Q4_K_M

- Use the embedded Qwen3.5 Jinja template with `enable_thinking=false`.
- General no-thinking defaults: temperature 0.7, top-p 0.8, top-k 20, min-p 0, presence penalty
  1.5, repeat penalty 1.0.
- The filename has no MTP marker, so speculative MTP stays off.
- Next: run the four-scenario Advanced Suite, then the repaired 12-fixture Agent Eval once. Do not
  run x5 unless the 12 fixtures first pass cleanly.

### Qwythos 9B Q5_K_M

- Use the exact Qwythos/Qwen3.5 template: temperature 0.6, top-p 0.95, top-k 20, repeat penalty 1.05.
- The installed filename is the normal non-MTP artifact. Do not enable draft-MTP for it.
- Confirm that the local file is from the current fixed-v3 replacement release; old exports had
  degeneration and export issues.
- Start at 65,536 context. The advertised one-million-token context is an architecture capability,
  not a sensible 24 GB VRAM default.
- Next: diagnose why the corrected mini loop stopped without writing the file before spending time
  on a full Advanced Suite.

### Gemma 4 12B Q4_K_M

- Use the embedded Gemma 4 Jinja template and native function-call format.
- Use Google's current defaults: temperature 1.0, top-p 0.95, top-k 64, min-p 0, repeat penalty 1.0.
- No-thinking remains the everyday agent default; an explicit thinking profile is now supported.
- The template repair fixed the loop result. The old 3–5 tok/s result was a cold wall-clock
  fallback, not decode throughput. Rerun it under the repaired timing contract before judging speed.

### Gemma 4 E4B Q4_K_M

- Use the new exact E4B template with the same Google 1.0 / 0.95 / 64 defaults.
- Q4_K_M is the sensible first choice on 24 GB because it leaves more KV-cache headroom.
- Its old loop had three policy violations and exceeded the auto-continue allowance under the wrong
  template. It must be rerun before use.

### Gemma 4 E4B Q8_0

- Use the same exact E4B template as Q4_K_M.
- The old score-74 loop is interesting but provisional. Q8 was slower than Q4 in the initial text
  case and consumes materially more VRAM.
- Keep Q8 only if repeated corrected-template tests show a quality or reliability gain.

### SuperGemma 4 26B uncensored fast-v2 Q4_K_M

- Keep the embedded Gemma 4 template, no-thinking, and the existing 26B mixed/native tool safety net.
- InferenceBridge showed about 120 decode tok/s at 16K while its OpenAI-compatible SSE response
  omitted the `timings` object. A separate warm API probe produced 73 tokens in 1.01 seconds
  (about 72 warm E2E tok/s).
- Its old 3.2/4.9 tok/s Matrix result included cold startup and tool latency and is invalid as a
  decode-speed judgment. Retest it with the repaired warm/repeated UI.

### Gemma 4 26B heretic UD Q4_K_XL

- Its direct tool and loop passed, but 3.1 matrix tok/s is far behind the Qwen 27B models.
- Do not use Q4_K_XL here merely because the winning Qwen uses that quant family; model architecture
  and runtime support dominate the result.

### Gemma 4 26B q4_0 heretic Q4_K_M

- It produced the best valid 26B-Gemma loop in this batch (score 56 / 100% tool success) but only
  3.1 matrix tok/s.
- It is a possible slow review/reasoning specialist, not a daily interactive model.

## Template repair delivered

Mauler now:

- matches the actual selected model id before a borrowed profile name;
- has exact code-owned templates for Gemma 4 12B and E4B;
- has exact Qwen3.5 4B and Qwythos 9B templates;
- preserves current embedded-GGUF Jinja rather than forcing a generic chat format;
- records current thinking, tool-protocol, sampling, context, and MTP metadata; and
- includes focused matcher/recommendation tests.

## Benchmark and Chat latency repair delivered

Mauler now:

- performs one excluded warm-up followed by three measured text passes;
- records backend decode and warm end-to-end throughput independently;
- labels wall-clock/estimated fallbacks instead of presenting them as decoder speed;
- keeps load, warm-up, prompt, decode, TTFT, tool, and mini-loop timing separate;
- creates and activates a named local Chat profile only when `Use` is clicked on a Matrix row; and
- provides a desktop Fast Chat lane with no project documents, memory, tools, control-plane packet,
  planning pass, or reviewer pass. Full Project Agent behavior is unchanged.

Production build and repository tests passed on 2026-07-23. The rebuilt executable is
`build/bin/TheMauler.exe`.

## Sources

- [Google Gemma 4 12B model card](https://huggingface.co/google/gemma-4-12B-it)
- [Google Gemma 4 E4B generation config](https://huggingface.co/google/gemma-4-E4B-it/blob/main/generation_config.json)
- [Google Gemma 4 function calling](https://ai.google.dev/gemma/docs/capabilities/text/function-calling-gemma4)
- [Qwen Qwen3.5 4B model card](https://huggingface.co/Qwen/Qwen3.5-4B)
- [Qwythos 9B GGUF model card](https://huggingface.co/empero-ai/Qwythos-9B-Claude-Mythos-5-1M-GGUF)

“Unrestricted” describes refusal tuning. It does not prove intelligence, tool reliability, or safe
unattended operation; those require local benchmark and pass^k evidence.
