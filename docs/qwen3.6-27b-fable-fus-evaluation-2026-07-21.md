# Qwen3.6 27B Fable/Fus evaluation (2026-07-21)

## Subject

- Profile: `qwen3.6-nothink`
- Provider: `inference-bridge`
- Model: `Qwen3.6-27B-Fable-Fus-711-UnHeretic-NM-DAU-NEO-MAX-NEO-Q4_K_M.gguf`
- Requested context: 35,000 tokens
- Backend-reported context: 35,072 tokens
- Thinking: off
- MTP/speculative decoding: off; the model name does not advertise an MTP draft model
- Hardware: RTX 3090 24 GB

The persistent local default was not changed by this evaluation.

## Direct benchmark

Mauler Bench > Advanced Suite completed all four live scenarios successfully.

| Scenario | Result | Measurement |
|---|---:|---|
| Text speed | pass | 33.0 tok/s, 141 completion tokens |
| Coding | pass | 33.4 tok/s, 59 completion tokens |
| JSON discipline | pass | valid JSON, 33.7 tok/s |
| Tool call | pass | one native structured tool call, zero repaired calls, 32.9 tok/s |

Overall result: score 90, 33.3 tok/s average, 447 prompt tokens, 256 completion tokens,
58.7 seconds total. The backend loaded the requested context without silently falling back to a
smaller window.

The status-bar model-list health poll briefly timed out while the GPU was serving benchmark calls.
`GET /v1/models` responded normally immediately afterward, so this was a concurrent health-poll
timeout rather than a lost backend or failed model load.

## Deterministic context-quality suite

The selected Qwen profile passed the model-independent context envelope:

- pass^5
- 7/7 fixtures
- 105/105 attempts
- hostile-content guard: pass
- model calls: 0

This validates Mauler's canonical routing, compact-tool, source, budget, and hostile-content
fixtures under the selected profile/workspace configuration. It does not measure model reasoning.

## Production agent evaluation

Run ID: `agent-eval-20260721-205255`.

The single full production-path pass was not healthy:

| Metric | Result |
|---|---:|
| Passed fixtures | 2/12 |
| pass power | not pass^1 |
| Unsupported completion rate | 0.0% |
| Duplicate-action rate | 10.2% |
| Tool-error rate | 16.3% |
| Recovery success rate | 0.0% |
| Average tool calls | 4.08 |
| Average fixture duration | 85.4 s |
| Policy violations/blocks | 6 |
| Human interventions | 0 |

Passed:

- `read-and-summarize`
- `verify-blocks-compile-error`

Failed or stopped:

- `chunked-write`: loop circuit breaker; the expected final chunk was missing.
- `compact-arg-repair`: controller rejected an illegal verification transition after a successful read.
- `completion-covers-both-asks`: incomplete artifact plus an illegal verification transition.
- `duplicate-read-guard`: stopped by the documentation-missing completion heuristic.
- `edit-then-verify`: review phase did not complete.
- `grep-then-edit`: review phase did not complete.
- `off-target-ip-guard`: stopped by the documentation-missing completion heuristic.
- `redirect-loop-guard`: four tool errors and an illegal verification transition.
- `stop-cleanly-on-budget`: two policy violations and an illegal verification transition.
- `terminal-routing-discipline`: one policy violation and an illegal verification transition.

The persisted report is in the user's Mauler state directory as `agent-eval-runs.json`.

## Interpretation

The model itself is viable for local no-thinking chat, coding, JSON, and basic native tool-call
generation at roughly 33 tok/s. The 35K profile is VRAM-safe and its tool-call wire format is clean.

The 2/12 agent result is not evidence that every failure belongs to the model. Five stopped results
used `control_verification_illegal`, and two more were rejected by the documentation-missing
heuristic. Those are strong signs that the new control-plane/completion gates and the older agent
fixtures are not yet aligned. The chunked-write duplication and redirect-loop tool errors are still
genuine model/runtime reliability concerns.

Do not run the five-repeat suite yet: one pass took about 17 minutes and failed the single-run gate.
First align read-only completion, documentation intent, review transitions, and verification
transitions with the harness; then rerun one pass. Only run x5 after the single pass is clean or the
remaining failures are explicitly classified as accepted fixture limitations.

## Current recommendation

- Keep `qwen3.6-nothink` for fast local interactive work and bounded tool calls.
- Do not treat this build/model combination as approved for unattended high-autonomy runs yet.
- Keep the exact 35K setting for now; it loaded as 35,072 and left very little VRAM margin.
- Retest the same GGUF after the control-plane/harness compatibility fixes so model quality is not
  conflated with controller policy failures.

## Same-machine Gemma 4 comparison

The same four-scenario Advanced Suite was run against the installed Gemma profiles on 2026-07-21.
This is an apples-to-apples generation/protocol comparison, not a claim about the full Unrestricted
agent loop.

| Profile | Load/context | Score | Avg generation | JSON | Structured tool call |
|---|---:|---:|---:|---:|---:|
| `qwen3.6-nothink` | 35,072 actual | **90** | **33.3 tok/s** | pass | pass, no repair |
| `gemma4-26b-a4b-qat` | 40,192 actual | 55 | 4.3 tok/s | **fail** | pass, no repair |
| `gemma4-31b` | not loaded | not scored | not scored | not scored | not scored |

Gemma 4 26B-A4B passed text, coding, and one structured tool call, but returned invalid JSON in the
JSON-discipline case. Its four scenario rates were 6.1, 7.0, 2.6, and 1.7 tok/s. Qwen passed all
four at 32.9-33.7 tok/s. Both suites took about 58 seconds end-to-end because load/first-token
overhead dominates this tiny suite; the reported steady generation rate is still materially
different.

The 31B result is not a quality failure. InferenceBridge returned `Model not found` for
`gemma-4-31B-it-uncensored-heretic-Q4_K_S.gguf`, so that stale profile cannot be compared until its
model id is pointed at an installed artifact.

Practical verdict: this Qwen is the strongest of the locally testable models for interactive Mauler
work. It is about 7.7x faster than the tested Gemma 26B average and was more disciplined on JSON.
Gemma 26B remains usable for prose/manual second opinions and did demonstrate structured tool-call
ability, but it is not the preferred local agent model from this evidence. The Qwen full
Unrestricted result remains 2/12 and therefore unapproved for unattended autonomy; no equivalent
Gemma full-agent run was spent because the current control-plane/fixture mismatch would make that
comparison misleading.
