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
