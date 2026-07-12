# Agent-Loop Stability Hardening — Codex Implementation Plan

**Date:** 2026-07-07
**Status:** Implemented and locally verified 2026-07-08
**Scope:** `internal/app` (agent loop + loop metrics). No architecture change; three surgical hardening tasks.

**Audit update 2026-07-10:** outcome-loop hardening remains implemented, but the broader stability ring is not race-clean. `go test -race ./internal/app ./internal/tools` found confirmed terminal-session and `run_script` output races. These are tracked as MAULER-AR-001 and MAULER-AR-002 in `agentic-reliability-issues-2026-07.md` and take priority over additional loop heuristics.

> **Read this first — honest framing.** Mauler's stability ring is already mature. This plan does **not** rebuild it. A full inventory of what already exists is in §1 so Codex does not duplicate anything. The real gap (§2) is narrow and specific: **loop detection is input-based, not outcome-based.** Three tasks close that gap. This supersedes the "port checklist" in `stable-agent-harness-architecture-2026-07.md`, which was written before reading the code and wrongly assumed the ring was missing.

---

## 1. Current state — what already exists (DO NOT re-implement)

Verified in `internal/app/app.go` (loop starts `agentLoop:` at ~line 2502) and neighbours:

| Capability | Where | Notes |
|---|---|---|
| Per-turn tool scoping (phase + terminal-state aware) | `toolDefsAndChoiceForTurnWithState`, `opsPhaseForTaskWithState` | Narrows tool defs each turn; do not touch. |
| Loop circuit breaker (inject corrective prompt → pause) | `decideLoopCircuitBreaker`, `buildLoopMetrics`, `loopCircuitBreakerPrompt` (`loop_metrics.go`) | State machine: `inject` → raise effort + system nudge; `pause` if health stays critical; `reset` on recovery. |
| Repeated-read + repeated-script detection | `repeatedIdenticalReadBlock`, `idempotentReadKey` (`agent_loop_stability.go`), `mostRepeatedScriptInvocation` | **Read/script-specific** — key limitation this plan addresses. |
| Salvage leaked tool calls from text | `parseInlineToolMarkup`, `parseConstrainedToolArgsContent`, `containsInlineToolMarkup` | Falls back to full registry when scope empty (`app.go` ~2884). Exceeds HelixClaw. |
| Reject hallucinated tool results | `containsHallucinatedToolResult`, `protocolFailure` gate | Assistant turn is dropped (not appended) on protocol failure. |
| GBNF tool-envelope constraint for non-native models | `llm.ToolCallGrammar`, `cfg.Tools.ToolGrammarConstraint` | Constrained decoding for local GGUFs. |
| Session/transcript repair before send | `a.history.RepairStructure()` (`app.go` ~2705), logged to ledger | Every turn; pairs orphan tool_use/results. |
| Result-feedback discipline | `newToolResultMsg` appended for **every** call — including blocked/cached/denied/error paths (`app.go` ~3262–3503) | Errors always become tool results; do not touch. |
| Thinking suppression on tool turns | `forceNoThink` (`app.go` ~2738) | Same fix HelixClaw uses for Qwen-class models. |
| Multi-stage context compaction | `NeedsCompactionWithReserve`, `ClearOldToolResults`, `applyMicrocompactStage`, `doCompact`, `ensureRequestContextRoom` | Exceeds HelixClaw. |
| MTP/speculative-decode instability fallback | `noteSpecTurn` | Off-topic for this plan. |
| Tool + time budgets, inference retries | `agentToolBudgetExhausted`, `agentTimeBudgetExhausted`, `preOutputInferenceRetries` | Off-topic. |

**Conclusion:** five of the six canonical stability guards are already present and in several cases stronger than the reference. The one genuine weakness is loop detection (guard #2).

---

## 2. The gap — loop detection triggers on repeated INPUT, not repeated OUTCOME

`buildLoopMetrics` → `repeatedToolInputCount` → `normaliseLoopToolInput` counts turns where the **tool input** repeats. `LoopMetrics.LoopStalled()` fires on `RepeatedToolInputs >= 2 || RepeatedSkips >= 2`.

The proven insight from HelixClaw (`actor.rs` outcome-aware loop detection) is that **the loop signal must be the `(call, result)` pair, not the call alone**:

- **False positives today:** legitimately re-issuing the *same input* that returns *new output* — polling a status endpoint, tailing a growing log, paginating — reads as a loop and can trip the breaker / burn a corrective prompt.
- **False negatives today:** the model varies the input trivially (`curl -s X` → `curl -sv X` → `curl -s X --max-time 5`) but gets the **same result** every time — a real loop that `RepeatedToolInputs` misses because the normalized inputs differ.
- **No cyclic detection:** A→B→A→B / A→B→C→A→B→C tool alternation (each call individually fine) is not detected at all.

Three tasks close this. Task 1 is the core; Tasks 2-3 are additive and independent.

**Implementation update 2026-07-08.** Task 1 and Task 2 are implemented in
`internal/app/loop_metrics.go` with table-driven coverage in `loop_metrics_test.go`. Task 3 was
confirmed and tightened: `cachedToolResultForCall` now caches only idempotent read/search style
tools, emits the existing `tool_cache_hit` path from `runAgentLoop`, and excludes non-idempotent
tools such as `shell`, `write`, and `terminal_send`. Shell repeat handling remains in the dedicated
shell repeat guards plus outcome-aware loop metrics.

---

## Task 1 — Add an outcome-repeat signal (`RepeatedIdenticalOutcomes`)

**Goal:** make "same call → same result, N times" the primary loop trigger, alongside the existing input signal.

**Files:** `internal/app/loop_metrics.go` (+ `loop_metrics_test.go`).

**Changes:**
1. Add field to `LoopMetrics` (struct at `loop_metrics.go:11`):
   ```go
   RepeatedIdenticalOutcomes int `json:"repeated_identical_outcomes"`
   ```
2. New helper `repeatedIdenticalOutcomeCount(tools []TaskToolEvent) int`. For each tool event compute an outcome key = `hash(normaliseLoopToolInput(tool) + "\x00" + normaliseToolResult(tool.Result))`. Count events whose outcome key equals a prior event's key. Reuse the existing `.Result` field on `TaskToolEvent` (already read at `loop_metrics.go:125`). Add a small `normaliseToolResult` (trim, collapse whitespace, cap to first N KB, strip volatile tokens like timestamps/`result_id`/hex addresses so cosmetically-different-but-identical outputs still match).
3. Populate it in `buildLoopMetrics` (`loop_metrics.go:32`).
4. Feed it into scoring + stall:
   - `loopStabilityScore`: `score -= m.RepeatedIdenticalOutcomes * 6` (weight it **above** `RepeatedToolInputs * 5`, since a confirmed same-result repeat is a stronger loop signal than input-only).
   - `LoopStalled()` (`loop_metrics.go:188`): add `|| m.RepeatedIdenticalOutcomes >= 2`.
5. Surface it in `Detail()`, `loopCircuitBreakerPrompt`, and `loopCircuitBreakerStopDetail` (append `repeated_identical_outcomes=%d`).

**Acceptance criteria:**
- Same `(input, result)` appearing twice sets `RepeatedIdenticalOutcomes >= 2` and makes `LoopStalled()` true.
- Same input with **different** results across turns does **not** increment `RepeatedIdenticalOutcomes` (polling/pagination is not flagged).
- Different inputs with the **same** normalized result across turns **does** increment it (trivial-variation loop is caught).
- Existing `RepeatedToolInputs` behaviour unchanged.

**Tests (`loop_metrics_test.go`):** table-driven — (a) identical input+result ×2 → stalled; (b) identical input, differing results ×3 → not stalled by the new signal; (c) differing inputs, identical result ×2 → stalled; (d) volatile-token-only differences in result → treated identical.

---

## Task 2 — Ping-pong / cyclic-alternation detection

**Goal:** detect period-2 (A→B→A→B) and period-3 (A→B→C→A→B→C) tool-name cycles that individually-benign calls form.

**Files:** `internal/app/loop_metrics.go` (+ test).

**Changes:**
1. Add `ToolCycleDetected bool` (and optionally `ToolCyclePeriod int`) to `LoopMetrics`.
2. New helper `detectToolCycle(tools []TaskToolEvent) (bool, int)`: take the trailing window (last ~6) of tool **names**; return true if the tail matches a repeated period-2 or period-3 pattern (`[A,B,A,B]` or `[A,B,C,A,B,C]`) with A≠B (and A≠B≠C for period-3).
3. Populate in `buildLoopMetrics`; OR it into `LoopStalled()`; subtract a fixed penalty in `loopStabilityScore` (e.g. `-8` when detected); mention in `Detail()`/prompts.

**Acceptance criteria:**
- `[read, grep, read, grep]` → detected (period 2).
- `[read, grep, glob, read, grep, glob]` → detected (period 3).
- `[read, read, read]` → **not** cycle-detected (that is Task 1 / repeated-input territory, not alternation).
- A non-repeating tail → not detected.

**Tests:** table of tool-name sequences → expected `(detected, period)`.

---

## Task 3 — Generalize the repeated-call cache beyond reads/scripts *(lower priority)*

**Goal:** the existing `repeatedIdenticalReadBlock` (reads) and `mostRepeatedScriptInvocation` (scripts) only cover two tool families. Generalize to any tool: when the model issues an exact `(tool, args)` that already produced a result this run, short-circuit with the cached result and a one-line "you already ran this; here is the prior result" note instead of re-executing.

**Files:** `internal/app/agent_loop_stability.go`, wire-in at the tool-execution site in `app.go` (~line 3400, near the existing `cachedToolResultForCall`).

**Note:** `cachedToolResultForCall(run, tc)` already exists (`app.go` ~3400) — **confirm its current coverage first.** If it already caches all idempotent tools, this task may reduce to widening its tool allowlist rather than new code. Codex must read that function before writing anything here.

**Acceptance criteria:** an exact repeat `(tool,args)` for a read-only/idempotent tool returns the cached result without a second real execution, and emits a `tool_cache_hit` event. Non-idempotent tools (shell/write/terminal_send) are never cached.

---

## Sequencing, risk, verification

- **Order:** Task 1 → Task 2 → (optionally) Task 3. Tasks 1 and 2 are independent additions to `loop_metrics.go` and can land in either order; do them in one PR.
- **Risk:** low and self-contained — all three only *add* signals feeding the existing `decideLoopCircuitBreaker`; they cannot bypass tool scoping, salvage, repair, or budgets. Worst case is an over-eager corrective prompt, which the existing `reset` path already recovers from.
- **Tuning guard:** keep new thresholds conservative (stall at `>= 2`) but land them behind the existing breaker's inject→pause escalation so a single false trip only injects a prompt, never hard-stops.
- **Verify:** `cd TheMauler && go test ./internal/app/...` (existing `loop_metrics_test.go`, `agent_loop_stability_test.go`, `command_storm_test.go` must stay green; add the new cases above). Then a smoke run of a task that legitimately polls/paginates to confirm no false loop-trip.

## Definition of done

- [x] `RepeatedIdenticalOutcomes` implemented, scored above `RepeatedToolInputs`, wired into `LoopStalled()` + breaker prompts, with the four Task-1 test cases green.
- [x] `detectToolCycle` implemented for period-2 and period-3, wired into `LoopStalled()`/score, with tests.
- [x] `cachedToolResultForCall` coverage confirmed; read-only/idempotent tools cache through `tool_cache_hit`, and non-idempotent tools are excluded.
- [x] `go test ./internal/app/...` green.
- [ ] One live polling/pagination smoke run shows no false loop-trip.

Verified 2026-07-08:

```powershell
go test ./internal/agent
go test ./internal/app/...
go test ./...
go vet ./...
npm run --prefix frontend build
.\build.ps1 -SkipTests
```

---

## References
- Mauler loop: `internal/app/app.go` (`agentLoop:` ~2502), `internal/app/loop_metrics.go`, `internal/app/agent_loop_stability.go`
- Proven mechanism source: HelixClaw `crates/helixclaw-agents/src/actor.rs` — outcome-aware loop detection (hash of `(combined_call_hash, result_hash)`; warn@2, hard-stop@3) and ping-pong period-2/3 detection
- Architecture context: `docs/stable-agent-harness-architecture-2026-07.md` (three-ring model), `docs/agent-reliability-roadmap.md`, `docs/helixclaw-parity-comparison-2026-07.md`
