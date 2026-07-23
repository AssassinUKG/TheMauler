# Huihui Qwen3.6 27B agent evaluation — 2026-07-21

## Outcome

The active unrestricted local profile completed one full 12-fixture live Agent Eval. It passed
2/12 fixtures, so it is **not certified for unattended agent loops** by this gate.

This result is not a clean model ranking. The earlier `qwen3.6-nothink` UD profile also passed the
same 2/12 fixtures and hit almost the same terminal-reason pattern. That repeatability points to a
shared Agent Eval/control-plane compatibility problem that must be fixed before using this suite to
choose between the two models.

The authoritative report is stored outside the repository at
`C:\Users\richa\.config\mauler\agent-eval-runs.json` under run
`agent-eval-20260721-233538`.

## Evaluated profile

| Setting | Value |
| --- | --- |
| Profile | `qwen3.6-nothink-copy-copy` |
| Model | `Huihui-Qwen3.6-27B-abliterated-ggml-model-Q4_K.gguf` |
| Provider | `inference-bridge` |
| Context | 35,000 tokens |
| Thinking | Off |
| Temperature | 0.2 |
| Top P / Top K / Min P | 0.95 / 20 / 0 |
| Repeat penalty | 1.05 |
| Maximum output | 8,192 tokens |
| Speculative decoding | Native MTP, draft n=2 |

## Metrics

| Metric | Result |
| --- | ---: |
| Fixture passes | 2/12 |
| Unsupported-completion rate | 0% |
| Duplicate-action rate | 12.9% |
| Tool-error rate | 12.9% |
| Recovery success | 0% |
| Average tool calls | 5.17 |
| Policy violations | 8 |
| Average fixture duration | 81.8 seconds |
| Total fixture time | 16 minutes 21 seconds |

Passing fixtures:

- `read-and-summarize`
- `verify-blocks-compile-error`

Terminal failures:

- `control_verification_illegal`: 6
- `review_incomplete`: 2
- `documentation_missing`: 1
- `loop_circuit_breaker`: 1

The zero unsupported-completion rate is important: the agent did not falsely convert these failures
into successful completion claims. It also correctly repaired and verified the compile-error
fixture. However, the policy, repetition, recovery, and completion-state results are not acceptable
for unattended use.

## Comparison with the previous UD run

| Profile | Passes | Passing fixtures | Policy violations | Duplicate actions | Tool errors |
| --- | ---: | --- | ---: | ---: | ---: |
| Huihui Qwen3.6 27B Q4_K | 2/12 | read/summarise; compile repair | 8 | 12.9% | 12.9% |
| Qwen3.6 27B UD Q4_K_XL | 2/12 | read/summarise; compile repair | 6 | 10.2% | 16.3% |

The control plane only accepts `verification_requested` from `acting` or `observing`. The live suite
currently allows some fixtures to propose completion while still planning, and repair/review paths
can propose completion again without first returning from `repairing` to `acting`. Documentation
completion heuristics also interfere with at least one fixture. These are product/harness failures
mixed into the model score.

## Decision

Keep this profile active for supervised unrestricted work with the settings above. Do not promote it
as the unattended-loop winner yet, and do not change sampling or disable MTP on the strength of this
run: the comparison is dominated by the shared control-state failure pattern.

The next valid gate is:

1. Give Agent Eval a canonical control contract for each fixture.
2. Ensure read-only completion can legally enter verification.
3. Ensure review repair returns the machine to `acting` before another completion proposal.
4. Isolate fixture scoring from unrelated documentation heuristics.
5. Persist and reload completed Agent Eval reports in the UI and clear the running state reliably.
6. Rerun one 12-fixture suite; only then run the expensive x5/60-scenario reliability gate.

## Repair checkpoint — 2026-07-22

The control-state, read-only/documentation classification, verification, budget-exhaustion,
canonical-evaluation-policy, and fixture-discipline repairs required by this report have landed.
Focused tests, control-plane and app race tests, and the full production build gate pass.

The rebuilt UI rerun is intentionally deferred while the workstation is in active use. The exact
single-suite and conditional pass^5 procedure, acceptance thresholds, unattended operating envelope,
and resume checklist are recorded in
[unattended-agent-reliability-gate-2026-07-22.md](unattended-agent-reliability-gate-2026-07-22.md).
The previous 2/12 result remains the latest live evidence until that rerun completes; no unattended
certification is implied by the repair tests alone.

## Repaired live-gate checkpoint — 2026-07-23

The rebuilt UI was exercised repeatedly after the control-state repair. Those diagnostic runs
exposed and removed four product/harness defects without weakening the gate:

| Report | Result | Defect exposed |
| --- | ---: | --- |
| `agent-eval-20260723-181041` | 11/12 | The compile-repair scorer rejected a second valid source-level fix. |
| `agent-eval-20260723-183502` | 11/12 | A missing `append=true` could overwrite an accumulated chunked file. |
| `agent-eval-20260723-190139` | 10/12 | Consecutive controller prompts produced an invalid request shape; completion rails also treated process words and a planning-only answer as unfinished work. |
| `agent-eval-20260723-192441` | 11/12 | A successful edit did not invalidate a cached pre-edit read, causing false repair churn. |

The corresponding repairs now accept code-owned alternate valid artifacts, protect accumulated
files with an explicit `overwrite=true` escape hatch, merge consecutive controller messages,
distinguish planning-only completion, narrow completion features, and invalidate cached read/glob
evidence after successful mutations. Focused tests, the full Go suite, vet, app/tools race tests,
frontend production build, Wails production build, and `git diff --check` passed.

The final fresh Gate 1 report is `agent-eval-20260723-195504`. It passed **11/12**:

| Metric | Result |
| --- | ---: |
| Unsupported-completion rate | 0% |
| Duplicate-action rate | 10.5% |
| Tool-error rate | 3.5% |
| Recovery success | 50% |
| Average tool calls | 4.75 |
| Average fixture duration | 68.4 seconds |
| Policy violations | 0 |
| Human interventions | 0 |

The sole failure was `chunked-write`. The first 100 lines were written safely. The model then omitted
`append=true` for the final chunk three times, despite the skip result explicitly instructing it to
retry with `append=true`. Mauler prevented the destructive overwrite on every attempt and the loop
circuit breaker stopped the run; `Line 150` was therefore absent. This is a model-loop reliability
failure, not a reason to weaken the overwrite guard or the fixture.

Gate 1 is therefore **not pass^1** and Agent Eval x5 was correctly not started. Huihui remains a
strong supervised unrestricted profile, but it is not certified for unattended loops by this gate.
