# llama.cpp upstream review - 2026-07-21

## Outcome

InferenceBridge has already installed llama.cpp `b10075` (`76f46ad29`), the current upstream
release on 2026-07-21. The runtime status reports that binary at
`%LOCALAPPDATA%\InferenceBridge\bin\llama-server.exe`. The last recorded generation used
`b10069` before the update; the next model load will use `b10075`.

Review baseline: the previous local runtime review ended at `b9842` on 2026-06-29.

Primary sources:

- [llama.cpp b10075 release](https://github.com/ggml-org/llama.cpp/releases/tag/b10075)
- [previous b9842 baseline](https://github.com/ggml-org/llama.cpp/releases/tag/b9842)
- [current llama-server API documentation](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)
- [official REST API change log](https://github.com/ggml-org/llama.cpp/issues/9291)

## Compatibility issue fixed in Mauler

Current llama.cpp Chat Completions streams finish in this order:

1. A choice with `finish_reason`.
2. A separate `choices: []` event containing authoritative `usage` and top-level `timings`.
3. `data: [DONE]`.

Mauler returned as soon as it saw `finish_reason`, so it silently discarded the final usage and
timing event. This affected both ordinary text and tool-call turns. It did not normally lose the
assistant text, but it did lose authoritative token counts, cache counts, prompt throughput, and
decode throughput.

The SSE parser now reads through `[DONE]`, preserves trailing usage/timings, flushes accumulated
tool calls after the stream terminus, and retains truncation state. Model-call ledger events and
benchmarks use llama.cpp's decode rate when present instead of estimating it from total wall time.
Regression fixtures cover both normal and tool-call stream ordering.

Doctor now reads InferenceBridge `/v1/runtime/status` and reports the exact installed build. Direct
llama.cpp providers retain `/props` and `/health` fallbacks.

## Upstream changes since b9842

| Upstream change | Value to Mauler | Decision |
| --- | --- | --- |
| [Resumable SSE replay](https://github.com/ggml-org/llama.cpp/pull/23226) | Can recover a dropped local stream without rerunning the entire model turn. | High-value joint integration, guarded by delta deduplication and tool-call idempotency. |
| [SSE keepalive pings](https://github.com/ggml-org/llama.cpp/pull/25241) | Prevents slow prefill from looking like a dead connection. | Forward after InferenceBridge accepts `sse_ping_interval`; no separate Mauler UI is needed initially. |
| [Responses streaming timings/progress](https://github.com/ggml-org/llama.cpp/pull/25348) | Better telemetry on `/v1/responses`. | Do not migrate Mauler's proven agent loop just for this; Chat Completions now supplies the required timings. |
| [Per-request reasoning budget](https://github.com/ggml-org/llama.cpp/pull/23116) | Bounds expensive deep-thinking turns and allows a zero-budget skip. | Optional profile/run control after InferenceBridge forwards it. Tool turns remain no-thinking. |
| [Live reasoning control](https://github.com/ggml-org/llama.cpp/pull/23971) | Can change or stop reasoning during an active request. | Later UI enhancement, not part of the core local-agent path. |
| [Speculative acceptance metrics](https://github.com/ggml-org/llama.cpp/pull/24536) | Gives evidence for choosing MTP/draft parameters rather than guessing. | Add to benchmark/calibration after InferenceBridge exposes safe structured metrics. |
| [Strict prompt-cache RAM limit](https://github.com/ggml-org/llama.cpp/pull/25070) and [user-message checkpoints](https://github.com/ggml-org/llama.cpp/pull/24176) | Improves cache lifecycle and bounded RAM use. | Runtime-owned; benefit automatically. Surface only if a cache tuning UI is later justified. |
| [Multimodal text-only slot save/restore](https://github.com/ggml-org/llama.cpp/pull/25076) | Makes slot persistence safer after multimodal requests. | Runtime-owned; no Mauler protocol change now. |
| [DFlash/EAGLE3 sidecar auto-download](https://github.com/ggml-org/llama.cpp/pull/25811) | Reduces setup friction for compatible speculative models. | Optional InferenceBridge preset; benchmark on the RTX 3090 before enabling. |

The current server API also documents exact input-token count routes:
`/v1/chat/completions/input_tokens`, `/v1/responses/input_tokens`, and
`/v1/messages/count_tokens`. These are more reliable than Mauler's character-based context estimate.

## Priorities and ownership

### P0 - completed in this review

- Mauler: retain trailing llama.cpp usage and timing events.
- Mauler: record cached prompt tokens and backend prompt/decode throughput in model-call evidence.
- Mauler: use authoritative decode throughput in benchmark results.
- Mauler: show the exact InferenceBridge-managed llama.cpp build in Doctor.

### P1 - next

1. **Exact token preflight (Mauler).** Before compaction or a large send, call
   `/v1/chat/completions/input_tokens` using the actual composed request. Use the existing estimate
   only when the endpoint is unsupported or unavailable. Cache the result for the immutable prompt
   packet so preflight does not add repeated work.
2. **Replayable streams (Mauler + InferenceBridge).** InferenceBridge must forward conversation
   identifiers and replay/stop routes. Mauler must persist the conversation ID, resume from the last
   acknowledged event, deduplicate deltas, and never execute a replayed tool call twice. Ship behind
   a provider capability flag and failure-injection tests.

### P2 - useful, not blocking

- Mauler + InferenceBridge: forward and persist `reasoning_budget_tokens`; optionally add a
  run-level **Skip reasoning** action for explicitly selected thinking profiles.
- InferenceBridge: expose llama.cpp speculative acceptance metrics without making the raw metrics
  endpoint broadly reachable. Mauler: consume them in Benchmark LLM and profile recommendations.
- InferenceBridge: evaluate EAGLE3 or DFlash presets for Qwen3.5/3.6. Keep them opt-in until repeated
  quality and speed tests pass on 24 GB VRAM.
- InferenceBridge: parse the same final stream timing event into its own runtime-status generation
  metrics; the current status can report token counts while leaving prompt/decode rates null.

## Deliberately not adopted

- **llama.cpp built-in file/shell tools:** upstream documents these as internal. They would bypass
  Mauler's confirmations, scope policy, rollback, RunLedger, and evidence checks. Mauler remains the
  sole tool authority.
- **Router/multi-model mode:** one large local model on a 24 GB RTX 3090 is better managed through
  InferenceBridge's existing load lifecycle. Router mode would add VRAM churn without improving the
  current workflow.
- **Immediate switch to `/v1/responses`:** Mauler's Chat Completions agent path already handles
  streaming tools, cancellation, usage, and timings. A protocol migration needs a concrete feature
  or reliability win and its own fixture suite.
- **Unbounded reasoning by default:** local chat/coding/tool turns stay on the no-thinking profile.
  Frontier/cloud or local thinking remains an explicit per-task choice.

## Acceptance checks for the next slice

- Exact preflight agrees with the server tokenizer and compaction occurs before a context overflow.
- Unsupported token-count endpoints fall back cleanly without blocking a run.
- Reconnected streams produce byte-for-byte equivalent visible text without duplicate tool calls.
- Every resumed or stopped stream is traceable in RunLedger.
- Speculative recommendations cite observed acceptance and repeated-run performance, not a presumed
  speed multiplier.
