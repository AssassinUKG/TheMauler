# TheMauler — Agent Reliability Roadmap

**Created:** 2026-06-22
**Owner doc:** this is the live source of truth for agent-loop reliability work. Companion analysis: [agent-loop-research-report.md](agent-loop-research-report.md).
**Audience:** an engineer or AI agent picking this up cold. Every item below is self-contained — files, signatures, algorithm, wiring anchors, tests, and acceptance criteria are spelled out so you can implement without re-deriving context.

> Conventions used below
> - File anchors like `app.go:2023` are point-in-time; line numbers drift. Always `grep` the named symbol (e.g. `func (a *App) runAgentLoop`) to relocate it before editing.
> - The agent loop lives in `internal/app/app.go`, function `runAgentLoop(ctx, firstMsg, profile, cfg, autonomous, mode, memories, skills, run TaskRun)` (currently ~`app.go:2023`), label `agentLoop:` (~`app.go:2186`).
> - All new Go must keep `go build ./...`, `go vet ./...`, and `go test ./...` green. Add tests next to the code (`*_test.go`, package `app`/`settings`/etc.).
> - Match surrounding style: table-driven tests, comments explain *why* not *what*, no new deps without reason.

---

## Status legend
- ✅ **Done** — landed and tested.
- 🔨 **Spec ready** — not started; full spec below.
- 🧪 **Held** — needs a live backend to validate before enabling.

---

## Already done (reference)

| Item | Status | Where |
|---|---|---|
| Generic duplicate read-only tool detector | ✅ | `internal/app/agent_loop_stability.go` `repeatedIdenticalReadBlock`, registered in `preToolRecoveryRules`; escalation in `repeatedPreToolRecoveryIgnored` |
| `tool_choice=required` on inspection/ops first turn | ✅ | `toolChoiceFor` + empty-defs guard in `toolDefsAndChoiceForTurn` |
| Model tool-calling-tier guardrail (doctor) | ✅ | `doctor.go` `addModelTierCheck` + `modelParamBillions` |
| llama.cpp agent launch-flag advisory (doctor) | ✅ | `doctor.go` `addLlamacppAgentFlagAdvisory` |
| Auto-lint after edits (`go vet`/`py_compile`/`bash -n`) | ✅ (pre-existing) | `mutation_verifier.go` `appendLint`→`lintFile` |
| SSE quirk handling, inline-tool repair, force-no-think, truncation→chunked-write, compaction + memory re-injection, redaction, rollback | ✅ (pre-existing) | `internal/llm/stream.go`, `internal/app/app.go` |

---

# Tier 1 — do these first (make every future change safe + measurable)

## R1. Agentic-loop eval harness ✅

**Status 2026-06-22:** implemented in `internal/app/agent_eval.go` with bundled scenario specs under `internal/app/testdata/agent_eval/`, a Wails-exported `RunAgentEval(profileName string) AgentEvalReport`, deterministic mock-client smoke coverage, scorer tests deriving metrics from `TaskRun` events/tools, and a Benchmark-page "Agent Eval" action that runs the live suite against the selected profile. The production loop now returns the finished `TaskRun` for harness use, while normal callers ignore the return.

**Goal.** Run a small fixed suite of *multi-step* agent tasks against a profile/config and score completion / spin-out / recovery — so any config or prompt change can be regression-tested instead of changed blind.

**Why.** `internal/app/benchmark.go` only scores single-shot prompts (JSON adherence, throughput, one tool call). There is no measurement of the *loop*. This is the highest-leverage item: it gates everything else.

**Files.**
- New: `internal/app/agent_eval.go`, `internal/app/agent_eval_test.go`
- New: `internal/app/testdata/agent_eval/*.json` (scenario specs)
- Touch: `internal/app/app.go` (Wails-exported method), `frontend/src/components/BenchmarkPage.tsx` (add an "Agent Eval" tab — optional, can ship headless first)

**Data model.**
```go
// AgentEvalScenario is one scripted task plus deterministic success checks.
type AgentEvalScenario struct {
    Name        string            // "edit-then-verify"
    Prompt      string            // the user message that starts the run
    Workspace   map[string]string // files seeded into a temp dir before the run (path->content)
    Mode        string            // AgentMode name, e.g. "Builder"
    MaxToolCalls int              // hard cap for this scenario
    // Success checks — ALL must pass:
    ExpectFiles   map[string]string // path -> substring that must be present after the run
    ExpectStatus  string            // required final run status, e.g. "done"
    ForbidSubstr  []string          // substrings that must NOT appear in any tool result (e.g. secret leak)
    MaxAutoContinues int            // fail if the loop auto-continued more than this (spin-out signal)
}

type AgentEvalResult struct {
    Name          string `json:"name"`
    Pass          bool   `json:"pass"`
    Status        string `json:"status"`
    ToolCalls     int    `json:"tool_calls"`
    AutoContinues int    `json:"auto_continues"`
    Truncations   int    `json:"truncations"`
    ToolErrors    int    `json:"tool_errors"`
    DurationMs    int64  `json:"duration_ms"`
    FailReason    string `json:"fail_reason,omitempty"`
}

type AgentEvalReport struct {
    Results   []AgentEvalResult `json:"results"`
    PassCount int               `json:"pass_count"`
    Total     int               `json:"total"`
    Profile   string            `json:"profile"`
}
```

**Algorithm.**
1. For each scenario: create a temp workspace dir, write `Workspace` files, `os.Chdir` into it (save/restore CWD; guard with a mutex — the app is single-active-run).
2. Build a fresh `TaskRun` and call the existing run entry with `autonomous=true` so no confirm prompts block. Reuse the real `runAgentLoop` path — do **not** fork a second loop, or the eval stops reflecting production behavior.
3. To make runs scorable without the Wails event bus, thread an optional sink: add a field `evalSink *evalCounters` to the run or pass via context; increment counters where the loop already emits events (`run.addEvent("truncated", …)`, `"tool_error"`, the `autoContinues++` sites, `totalToolCallsMade`). Simplest: after the run, derive counts from `run.Events`/`run.Tools` (already recorded) instead of a live sink — prefer this, it needs zero loop changes.
4. Score: `Pass = Status==ExpectStatus && all ExpectFiles satisfied && no ForbidSubstr hit && AutoContinues<=MaxAutoContinues`.
5. Return `AgentEvalReport`.

**Seed scenarios (ship at least these 6).**
- `read-and-summarize` — seed 2 files; prompt "summarize what these files do"; expect status done, ≤2 auto-continues.
- `edit-then-verify` — seed a `.go` file with a bug; prompt "fix the compile error"; expect file contains the fix substring, lint `[ok]` in a tool result.
- `chunked-write` — prompt "write a 200-line file X"; expect file exists + ≥150 lines (exercises truncation→append recovery).
- `duplicate-read-guard` — craft a prompt that tempts re-reading; assert the run does NOT exceed N tool calls (exercises R-done `repeatedIdenticalReadBlock`).
- `grep-then-edit` — find a symbol, edit its definition; expect edited substring.
- `stop-cleanly-on-budget` — set `MaxToolCalls=3`; expect status `done`/`stopped` with a final summary, never an empty response.

**Wiring.** Export `func (a *App) RunAgentEval(profileName string) AgentEvalReport` for the frontend; also a `go test` driver `TestAgentEval_Smoke` that runs scenarios whose checks are backend-independent (file ops with a mock client) so CI passes without a live model. Mock client: implement `llm.Client` returning a scripted `Delta` sequence (see `internal/llm/stream_test.go` for delta shapes).

**Tests.** `TestAgentEvalScoring` (table-driven: given a fake `TaskRun`, the scorer returns the right pass/fail), `TestAgentEval_Smoke` (mock-client end-to-end for `edit-then-verify`).

**Acceptance.** `go test ./internal/app/ -run AgentEval` passes; `RunAgentEval` returns a populated report against a live profile; adding the harness changes no production-loop behavior.

**Effort.** L (1–2 days). Biggest payoff.

---

## R2. `settings.Validate()` — clamp on load, not just warn ✅

**Status 2026-06-22:** implemented in `internal/settings/validate.go`, wired into `Load()` and `LoadProfiles()`, with regression tests for compaction, max tokens vs context, sampler clamps, unknown-provider reporting, and idempotence.

**Goal.** Normalize/clamp dangerous config at load time so a bad profile can't silently destabilize runs. Doctor *warns*; this *enforces*.

**Why.** `internal/settings/load.go` already has scattered normalization (~`load.go:102` MaxToolCalls, `load.go:115` CompactionAt). Consolidate into one validated path and extend coverage.

**Files.** New: `internal/settings/validate.go`, `internal/settings/validate_test.go`. Touch: `internal/settings/load.go` (call `Validate` at the end of load), keep existing per-field fixups or move them in.

**Signature.**
```go
// Validate clamps out-of-range fields to safe values and returns a list of
// human-readable adjustments (for logging / a doctor surface). It never errors —
// a usable config is always produced.
func (s *Settings) Validate() []string
```

**Rules (clamp + record each change).**
- `Context.CompactionAt`: if `<=0 || >=1` → `0.85`. (A 0 or 1 disables/never-fires compaction → runaway context.)
- `Agents.MaxToolCalls`: if `<=0` → default (200); if `>2000` → 2000.
- Per `Profile`: for each generation preset (`ThinkGeneral`, `ThinkCoding`, `NoThink`, `ThinkCoding`): if `MaxTokens <= 0` → 2048; if `CtxTokens>0 && MaxTokens > CtxTokens/2` → clamp to `CtxTokens/2` (prevents the truncation-storm the loop currently has to recover from).
- `Temperature`: clamp to `[0, 2]`. `TopP`: clamp to `(0, 1]`. `TopK`: if `<0` → 0. `MinP`: clamp `[0,1]`.
- `CtxTokens`: if `<=0` → 8192.
- Profile referencing an unknown provider: leave (doctor reports), but record it.

**Wiring.** At the end of `settings.Load()` (or wherever the `*Settings` is finalized), call `adjustments := cfg.Validate()` and, if non-empty, log them (`fmt.Fprintf(os.Stderr, …)`) and stash on the App so doctor can show "config auto-corrected: …". Do the same for `LoadProfiles()` per profile.

**Tests.** `TestValidateClampsCompaction`, `TestValidateClampsMaxTokensToHalfCtx`, `TestValidateClampsSamplers`, `TestValidateIsIdempotent` (running twice yields no further adjustments).

**Acceptance.** Loading a deliberately broken settings/profiles file yields a usable, clamped config plus a non-empty adjustments list; second `Validate()` returns empty.

**Effort.** S (half day).

---

## R3. Run checkpoint + resume ✅

**Status 2026-06-22:** implemented with SQLite-backed `run_checkpoints` storage, typed app checkpoints containing history plus `TaskRun`, periodic in-loop checkpointing every 4 tool results, clean-exit deletion, and Wails bindings `ListResumableRuns` / `ResumeRun`.

**Goal.** Persist `(history + run state)` periodically during a run so a crash/close mid-task can resume instead of losing everything.

**Why.** Runs are ephemeral today. Long HTB/research runs are exactly the ones worth protecting. 12-Factor #6 (launch/pause/resume).

**Files.** Touch: `internal/sessionstore/store.go` (add checkpoint table/API), `internal/app/app.go` (write checkpoint in-loop; resume entry), `internal/agent/history.go` (need a serializable snapshot — likely already `Messages() []llm.Message`). New: `internal/app/run_checkpoint.go`, `_test.go`.

**Data.**
```go
type RunCheckpoint struct {
    RunID     string          `json:"run_id"`
    Prompt    string          `json:"prompt"`
    Mode      string          `json:"mode"`
    Profile   string          `json:"profile"`
    Messages  []llm.Message   `json:"messages"`   // full history snapshot
    Run       TaskRun         `json:"run"`        // counters, events, tools
    SavedAt   string          `json:"saved_at"`   // RFC3339, passed in (no Date.now in scripts; use time.Now() here — Go is fine)
}
```

**Algorithm.**
1. Add `func (s *Store) SaveCheckpoint(cp RunCheckpoint) error` and `LoadCheckpoint(runID string) (RunCheckpoint, bool, error)` and `DeleteCheckpoint(runID string)` backed by a `run_checkpoints` SQLite table (single row per run id, upsert).
2. In `runAgentLoop`, after each tool-execution batch (end of the `agentLoop` body, near `finalSummary = textBuf.String()`), call a throttled `a.maybeCheckpoint(run, every=4 tool calls)`. Cheap: gate on `totalToolCallsMade % 4 == 0`.
3. On clean terminal exit (`return` paths that set a final status) call `DeleteCheckpoint(run.ID)` so only *interrupted* runs linger.
4. Resume: `func (a *App) ResumeRun(runID string) error` — load checkpoint, rebuild history (`a.history.Replace(cp.Messages)` — add that method if missing), re-enter `runAgentLoop` with the restored `TaskRun`. Surface unfinished checkpoints on startup via a Wails method `ListResumableRuns() []RunCheckpoint`.

**Tests.** `TestCheckpointRoundTrip` (save→load equal), `TestCheckpointDeletedOnCleanExit`, `TestMaybeCheckpointThrottles`.

**Acceptance.** Kill the app mid-run; on restart `ListResumableRuns` shows it; `ResumeRun` continues from the last checkpoint without re-running completed tool calls.

**Effort.** M (1 day).

---

## R4. Global wall-clock task budget ✅

**Status 2026-06-22:** implemented as `agents.max_run_seconds` (default 1800, `0` unlimited), validated for negative values, surfaced in Settings, and wired into `runAgentLoop` as a clean text-only final-summary turn.

**Goal.** A maximum elapsed-time per task that triggers the existing graceful "summarize and stop" path. Closes the last runaway vector (token + tool-count budgets already exist; time does not).

**Files.** Touch: `internal/app/app.go` (`runAgentLoop` top + loop head), `internal/settings/model.go` + `defaults.go` (`AgentsConfig.MaxRunSeconds int`). Optionally extend `taskBudget` (`app.go:4333`).

**Algorithm.**
1. Add `AgentsConfig.MaxRunSeconds` (default e.g. `1800` = 30 min; `0` = unlimited). Add to `defaults.go` and `Validate()` clamp (`<0 → 0`).
2. In `runAgentLoop`, capture `startedAt := time.Now()` before the `for` loop.
3. At the top of `agentLoop` (right after the `ctx.Err()` check ~`app.go:2188`), if `cfg.Agents.MaxRunSeconds>0 && time.Since(startedAt) > dur`, set a flag `timeBudgetExhausted` and route exactly like the existing `toolBudgetExhausted` branch: strip tools, `tool_choice="none"`, inject the final-summary prompt (reuse `agentToolBudgetSummaryPrompt` or a sibling `agentTimeBudgetSummaryPrompt`). Do **not** hard-kill mid-tool — let it produce a clean summary.

**Tests.** `TestTimeBudgetTriggersSummary` (inject a fake clock or set `MaxRunSeconds=0`-vs-tiny and assert the summary branch is taken). Prefer injecting `startedAt`/`now` via a small helper so the test is deterministic.

**Acceptance.** A run exceeding `MaxRunSeconds` stops with a final text summary and status `stopped`, never an abrupt empty return.

**Effort.** S (half day).

---

# Tier 2 — strong hedges

## R5. Sub-agent for read-heavy sub-tasks ✅

**Status 2026-06-22:** implemented as `subagent_explore` using the existing bounded subagent runner, with a dedicated read-only `explore` toolset, default tool exposure, UI risk labels, and a main-system-prompt nudge to delegate large codebase mapping/read-heavy inspection into the subagent.

**Goal.** Offload big read/exploration sub-tasks (codebase mapping, recon enumeration) to a fresh-context sub-agent that returns only the conclusion, keeping the main loop's context out of the ">40% dumb zone" (12-Factor #10 — the #1 anti-spin-out lever).

**Why.** `internal/app/subagents.go` already runs sub-agents (`subagent_research`). Extend the pattern to a general `subagent_explore` and/or make the loop *prefer* delegating large inspections.

**Files.** Touch: `internal/app/subagents.go` (add/extend a tool), `internal/tools/registry.go` (register), system prompt (guidance to delegate).

**Spec.**
- Add a `subagent_explore` tool: input `{task: string, paths?: []string}`. It spawns a sub-run with a read-only toolset (`read_file`, `read_many`, `glob`, `grep`), a tight `MaxToolCalls` (e.g. 20) and a tight context budget, with a system instruction "explore and return a structured findings summary; do not edit." Returns only the final text.
- The sub-agent shares the active backend (per `addSharedBackendSubagentCheck` in doctor — same-model reuse is already handled).
- Prompt nudge: in the agent system prompt, add "For large inspection/enumeration tasks, call subagent_explore and work from its summary rather than reading many files into your own context."

**Tests.** `TestSubagentExploreReadOnly` (the spawned toolset excludes write/edit/shell), `TestSubagentExploreReturnsSummary` (mock client).

**Acceptance.** `subagent_explore` runs a bounded read-only sub-run and returns a summary string; main-loop context growth for a "map the repo" task is materially lower than doing it inline (measure via R1 harness `ToolCalls`/token counts).

**Effort.** M (1 day).

---

## R6. Frontier escalation fallback (+ finish the Anthropic backend) ✅

**Status 2026-06-22:** implemented. Anthropic Messages API backend now supports streaming text, tool-use parsing, tool-result conversion, usage/truncation handling, and tests. Frontier escalation is opt-in via `agents.escalation_profile`, capped at two attempts per run, and only fires for hard recovery points: unrepaired malformed tool markup or truncation auto-continue exhaustion. If the escalation response emits a tool call, the main loop executes that single recovery step and then returns control to the local profile.

**Goal.** When the local model spins out on a *specific hard step* (truncation loop, repeated tool failure, malformed-markup limit), escalate that one step to a stronger model, then hand control back to local. Also: the Anthropic backend is still a stub (`internal/llm/backends/anthropic.go`) — implement it so an escalation target exists.

**Files.** Implement `internal/llm/backends/anthropic.go` (Messages API, SSE `content_block_delta` / `input_json_delta`, auth `X-API-Key` from `os.Getenv(profile.APIKeyEnv)` — follow `openaicompat.go` as the reference `Client`). Touch: `internal/app/app.go` to add an escalation hook; `settings` to add `Agents.EscalationProfile string` (name of a profile to escalate to; empty = disabled).

**Escalation trigger (be conservative).** Only when the loop is *already* in a hard-stop branch it would otherwise give up on: `malformedToolContinues >= maxMalformedToolContinues`, or `autoContinues >= maxAutoContinues` with `wasTruncated`. At that point, if `EscalationProfile` is set, build a one-shot request to the escalation client with the current history + a focused instruction, take its single response/tool-call, append it, reset the relevant counter once, and continue the local loop. Cap escalations per run (e.g. 2) to bound cost.

**Tests.** `TestAnthropicSSEParsing` (table-driven over recorded event chunks), `TestEscalationFiresOnlyAtHardStop`, `TestEscalationCappedPerRun`.

**Acceptance.** Anthropic backend streams text + tool calls through the same `llm.Client` interface; with `EscalationProfile` set, a run that would dead-end on truncation/malformed markup instead gets one frontier-assisted step and recovers; with it empty, behavior is unchanged.

**Effort.** L (Anthropic backend ~1 day; escalation hook ~half day).

---

## R7. Grammar-constrained tool arguments (llama.cpp) 🧪

**Status 2026-06-22:** live-validation probe implemented, feature still held. The Benchmarks page has a "Grammar Probe" action wired to `RunGrammarToolArgsProbe(profileName)`, which sends one required dummy tool call with that tool's parameter schema as `response_format` and reports whether the backend preserves a structured `tool_calls` envelope with valid arguments. Do not enable `grammar_tool_args` behavior until this probe passes repeatedly on the intended llama.cpp profile.

**Goal.** Force valid tool-call JSON at generation time instead of repairing it after — eliminates the malformed-args class.

**Why held.** `llm.Request.JSONSchema` plumbing exists, but llama.cpp's `json_schema`/grammar constrains *message content*, not the tool-call envelope, and interacts with `tool_choice`. Shipping it blind risks breaking all tool calling. **Must be validated against a live llama.cpp server before enabling.**

**Plan.**
1. On a live server, test: with a single tool and `tool_choice="required"`, does passing that tool's parameter schema via the backend's grammar produce a valid `tool_calls` entry, or does it corrupt the envelope? Document the finding.
2. If safe only in the single-tool/`required` case, gate it exactly there in `buildChatRequest`/`toolDefsAndChoiceForTurn`: when `toolChoice=="required"` and `len(toolDefs)==1`, attach `JSONSchema = toolDefs[0].Function.Parameters`. Otherwise leave unset.
3. Add a profile flag `Profile.GrammarToolArgs bool` (default false) so it's opt-in until proven.

**Tests.** Backend-level test with a recorded server response confirming the constrained path yields a valid tool call; unit test that the schema is only attached under the gated condition.

**Acceptance.** With the flag on and the gated condition met, malformed-tool-arg recoveries drop to ~0 in the R1 harness, with no regression in multi-tool turns (flag off path unchanged).

**Effort.** M once a live server is available; do not merge enabled-by-default without that validation.

---

# Tier 3 — hygiene / observability

## R8. Plan-then-execute via the todo tool ✅

**Status 2026-06-22:** system prompt now explicitly asks substantial multi-step tasks to first call `todo_create` with a concise 3-8 step plan and maintain it with todo update/done/blocked tools.
**Goal.** For large multi-step tasks, have the model emit a short plan as todo items and drive the loop against it, re-anchoring on each step.
**Files.** `internal/tools/todo.go` (exists — confirm it's registered and the loop reads it), system prompt. Add a nudge: for tasks the heuristics flag as multi-step (reuse `needsInspectionTool`/`needsOperationalTool`/length), instruct "first write a 3–8 step plan with the todo tool, then execute and check off each step."
**Acceptance.** Multi-step tasks show a todo list in the run; the model references remaining steps after compaction. **Effort.** S.

## R9. Periodic goal re-injection ✅

**Status 2026-06-22:** implemented goal reminders after context drops/compaction. The reminder includes the original task and current todo state when available, and resume now preserves first-turn heuristics by falling back to the saved run prompt when `firstMsg` is empty.
**Goal.** Prevent goal drift in long runs: every K turns (or right after a compaction), re-inject a compact "Original task: … / Remaining: …" system message.
**Files.** `internal/app/app.go` loop head; reuse the `contextDropped`/compaction sites (~`app.go:2278`) — when `contextDropped`, also append a one-line goal reminder built from `run.Prompt` + current todo state.
**Acceptance.** After a mid-run compaction, the next request contains the original objective verbatim. **Effort.** S.

## R10. Loop-health telemetry ✅

**Status 2026-06-22:** implemented `LoopMetrics` aggregation from `TaskRun` tools/events and records a `loop_metrics` task-run/ledger event at run finish with tool calls, auto-continues, truncations, tool errors, compactions, context clears, stop reason, duration, and token counts.
**Goal.** Aggregate per-run reliability metrics so regressions are visible: spin-out rate (auto-continues/run), truncation rate, tool-error rate, compaction count, avg tool calls to completion.
**Files.** New `internal/app/loop_metrics.go` deriving from `run.Events`/`run.Tools` at run end; persist to the ledger/sessionstore; surface on `LiveOpsPage`/`LogsPage`. These events already exist (`"truncated"`, `"tool_error"`, `"compaction"`, `autoContinues`), so this is pure aggregation — no loop changes.
**Acceptance.** A metrics summary is recorded per run and viewable; feeds the R1 harness's pass/fail context. **Effort.** S–M.

---

## Suggested execution order
1. **R2** (Validate) — half day, removes a whole class of instability immediately.
2. **R1** (eval harness) — the measurement backbone everything else is graded against.
3. **R4** (wall-clock budget) — half day, closes the last runaway vector.
4. **R3** (checkpoint/resume) — durability for long runs.
5. **R10** (telemetry) — cheap, makes R1's results trend over time.
6. **R5** (subagent_explore) — biggest context-hygiene win for long runs.
7. **R6** (Anthropic + escalation) — frontier safety net.
8. **R8 / R9** — drift/plan hygiene.
9. **R7** (grammar tool args) — once a live llama.cpp server is on hand to validate.

## Definition of "still good and reliable"
All ✅ items remain green, R1 eval suite passes on the active profile, R2 guarantees no out-of-range config reaches the loop, and R3/R4 mean no run can run away or be lost. That is the bar.
