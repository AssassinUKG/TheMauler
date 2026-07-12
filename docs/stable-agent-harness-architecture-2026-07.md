# Stable Agent Harness Architecture — Port Spec

**Date:** 2026-07-07
**Status:** Design / porting spec
**Source of truth:** HelixClaw's `AgentActor` loop (`crates/helixclaw-agents/src/actor.rs`), battle-tested across the CEO → Orchestrator → Worker stack. This doc distills that architecture into a framework-agnostic spec and maps each piece onto TheMauler's Go packages so we can pull the stability layer in.

> **⚠️ CORRECTION (2026-07-07, after reading the code):** This doc's §3/§4 originally assumed Mauler was *missing* the outer stability ring. That was wrong — the loop in `internal/app/app.go` (`agentLoop:` ~2502) already implements five of the six guards (scoping, circuit breaker, salvage, session repair, compaction), several stronger than the reference. **Treat §1–§2 (the three-ring mental model + the loop) as valid, but use `agent-loop-stability-hardening-plan-2026-07.md` — not §4's checklist — for actual work.** That plan has the real gap analysis and Codex tasks.

> **Why this doc exists:** Mauler already has a solid *inner* layer — a tool set with tool-level guards (`internal/tools/filetype_guards.go`, `protected.go`, `paths.go`) and transcript management (`internal/agent/history.go`, `rollback.go`). What it's missing is the **outer stability ring** that wraps the loop: tool scoping per turn, loop/runaway detection, circuit breaking, result-feedback discipline, session integrity, and context compaction. That ring is ~90% of what makes an agent harness *stable* rather than just *functional*. This spec is that ring.

---

## 1. The mental model: three rings

An agent harness is three concentric rings. Get the boundaries right and everything else follows.

```
┌─────────────────────────────────────────────────────────┐
│  OUTER RING — Guards (where STABILITY lives)             │
│  tool scoping · loop detection · circuit breaker ·       │
│  result-feedback · session repair · context compaction   │
│  ┌───────────────────────────────────────────────────┐  │
│  │  MIDDLE RING — The Loop (where AGENCY lives)       │  │
│  │  execute tools · append results · re-invoke ·      │  │
│  │  decide when to stop                               │  │
│  │  ┌─────────────────────────────────────────────┐  │  │
│  │  │  INNER RING — The Model (pure function)      │  │  │
│  │  │  (system, tools, transcript) → text | calls  │  │  │
│  │  │  stateless · no side effects · no memory     │  │  │
│  │  └─────────────────────────────────────────────┘  │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

- **Inner — the model.** A pure function: `(system, tools, transcript) → text | tool_calls`. Stateless. It never runs anything; it *emits a request* (a tool call: name + JSON args). All memory is the transcript, held by the harness and re-sent every turn.
- **Middle — the loop.** Executes the requested tools against the real world, appends the results, re-invokes the model with the enlarged transcript, and decides when to stop. This is where *agency* lives. It is ~20 lines.
- **Outer — the guards.** Everything that keeps the middle ring from spinning out. This is where *stability* lives. It is thousands of lines.

**The core insight:** the model is only as stable as the outer ring. Nearly every real-world agent failure is an outer-ring defect (wrong tool scope, missing salvage, no loop guard) — not the model being dumb. Don't reach for a bigger model to fix an outer-ring bug.

---

## 2. The loop (middle ring)

The entire loop, in essence. Mauler target: the agent-runtime driver (per `docs/go-native-agent-runtime-update-2026-06-30.md`), sitting over `internal/agent` (history) and `internal/tools` (dispatch).

```go
func RunTurn(ctx context.Context, task Task) (Result, error) {
    h := history.New(task)                 // internal/agent: the transcript IS the state
    for iter := 0; iter < maxIterations; iter++ {
        tools := scopeToolsForTurn(h, iter) // OUTER RING guard #1 — narrow the toolset
        resp, err := llm.Generate(ctx, systemPrompt, tools, h.Messages())
        if err != nil { return Result{}, err }
        h.AppendAssistant(resp)             // record the model's turn verbatim

        if resp.StopReason == StopEndTurn { // model is done talking
            return Result{Text: resp.Text}, nil
        }
        calls := resp.ToolCalls
        if len(calls) == 0 {
            calls = salvageToolCallsFromText(resp.Text, tools) // OUTER RING guard #4
        }
        if guard := checkLoopGuards(h, calls); guard.Stop {    // OUTER RING guards #2/#3
            h.AppendSystemNudge(guard.Message)
            if guard.HardStop { return Result{Text: guard.Message}, nil }
            continue
        }
        for _, call := range calls {
            result := executeTool(ctx, call)                    // HARNESS runs it, not the model
            h.AppendToolResult(call.ID, result)                 // OUTER RING guard #5 — always append, errors too
        }
        h = compactIfNeeded(h)                                  // OUTER RING guard #6
    }
    return Result{}, ErrMaxIterations
}
```

Key discipline, independent of any guard:

- **The model never executes anything.** It requests; the harness executes. `executeTool` is the only place side effects happen.
- **Re-send the full transcript every turn.** The API is stateless. `history.Messages()` is the memory.
- **Every `tool_use` gets exactly one `tool_result`.** Including failures (see guard #5). A dropped result desyncs the transcript and the model fabricates to fill the gap.
- **Roles must alternate / pair correctly.** Malformed transcripts are a top cause of instability (see guard #5 / session repair).

---

## 3. The stability ring (outer ring) — six guards

Each guard: the failure it prevents → the mechanism → where it lives in Mauler → the proven HelixClaw implementation to port from.

### Guard 1 — Tool scoping per turn *(highest leverage)*

- **Failure prevented:** a casual/irrelevant turn is handed the full toolset and the model fires a destructive or nonsensical tool (e.g. a greeting triggering a `web_fetch`, or a chat turn running a shell command). Fewer tools in front of the model = fewer wrong turns.
- **Mechanism:** don't expose the whole registry every turn. Compute a *scope* from the turn's intent and phase, and slice the tool definitions down to that scope. Scopes are a small enum: `None` (pure chat/greeting), `ReadOnly` (read/search/web/memory), `WriteFile`, `Validate`, `Shell`, `Full`.
- **Mauler target:** a `scopeToolsForTurn` in the agent runtime that filters `internal/tools` registrations before each `llm.Generate`. Reuse the existing tool metadata; add a `scope`/`class` tag per tool.
- **Port from:** HelixClaw [`tool_scope_for_turn`](../../Documents/HelixClaw/crates/helixclaw-agents/src/actor.rs) and `scoped_tool_definitions`. Note the ordering lesson we just fixed: a "bypass/unattended mode" flag must **not** override the scope gate for non-action conversational turns — pure greetings stay tool-free, everything else gets at least a ReadOnly floor so tools are never zero when the model wants to act.

### Guard 2 — Loop detection (outcome-aware)

- **Failure prevented:** the model repeats the same action forever. Critical nuance: repeating a *call* is fine (retrying a fetch); repeating a call that produced the *same result* is a loop.
- **Mechanism:** hash `(normalized_call, result)` per tool execution. Same hash twice → warn (inject a nudge). Same hash three times → hard stop. Also detect **ping-pong**: A→B→A→B (period-2) and A→B→C→A→B→C (period-3) tool alternation → nudge and clear the recent-tool-hash window.
- **Mauler target:** new `internal/agent/guards.go` holding a rolling window of `(callHash, resultHash)` on the turn state.
- **Port from:** HelixClaw's ping-pong detection + outcome-aware loop detection (recorded in the OpenFang-inspired guardrail set; see `actor.rs`). Warn at 2 identical outcomes, hard-stop at 3.

### Guard 3 — Circuit breaker

- **Failure prevented:** runaway that the per-iteration cap misses — e.g. a single turn emitting many parallel calls, so total tool calls explode while the iteration count stays low.
- **Mechanism:** a monotonic `totalToolCalls` counter on the turn. Fire a hard stop at `2 × maxIterations` total calls, regardless of iteration number.
- **Mauler target:** same `guards.go`; increment in the `executeTool` fan-out.
- **Port from:** HelixClaw `[CIRCUIT BREAKER]` hard-stop at `2×max_iterations` total calls.

### Guard 4 — Parse / salvage at the boundary

- **Failure prevented:** local models leak tool calls as **plain text** (`{"name":"read_file","args":{...}}` or `run_command(...)` printed into the reply) instead of a structured call. Without salvage, the harness sees "no tool call," treats the JSON as the final answer, and ships it to the user — or the model fabricates having done the action.
- **Mechanism:** when the response has no native tool-call blocks, run a salvage pass over the response text: extract a JSON object, validate `name` against the *current turn's* tool defs, coerce `args`/`arguments`/`input` into the call, and promote it to a real dispatch. Prefer server-side extraction when the runtime supports it (llama.cpp `--jinja` + a model-matched tool template / grammar-constrained decoding) so calls arrive structured in the first place; keep text-salvage as the backstop.
- **Mauler target:** `internal/tools` gains a `salvageToolCall(text, scopedTools)`; wire it into the loop's `len(calls)==0` branch. Mauler is Go over local GGUFs via InferenceBridge — this guard is **load-bearing** here (frontier APIs return native `tool_use` and don't need it).
- **Port from:** HelixClaw `salvage_structured_tool_call_from_text` + `extract_structured_tool_call_from_value` (handles `args`/`arguments`/`input`, validates the name against scoped defs). **Critical coupling:** salvage can only validate against tools that are *in scope this turn* (Guard 1). If the scope is empty, salvage has nothing to match and the leak escapes — Guards 1 and 4 must be consistent.

### Guard 5 — Result-feedback discipline & session integrity

- **Failure prevented:** (a) a failed tool that's dropped instead of reported — the model can't recover from an error it never sees, so it guesses/fabricates; (b) a malformed transcript (a `tool_use` with no matching `tool_result`, non-alternating roles, empty/half-written entries) that desyncs every subsequent turn.
- **Mechanism:**
  - Every tool execution appends a `tool_result` — on failure, append it with an `is_error`/error payload, never drop or `panic`.
  - A repair pass validates the transcript before each send: pair every `tool_use` with a result, drop/patch malformed entries, keep roles valid. Log when repair fires; **repair firing every single turn is a symptom, not health** (it means upstream is producing malformed turns — fix the producer).
- **Mauler target:** `internal/agent/history.go` (append discipline) + a `Repair()` on the history/session before send; `internal/sessionstore` for persistence integrity.
- **Port from:** HelixClaw `session_repair.rs` and the error-as-tool-result convention in `actor.rs`.

### Guard 6 — Context management (compaction)

- **Failure prevented:** the transcript grows unbounded and eventually exceeds the model's context window; quality also degrades long before the hard limit (stale tool results crowding the window).
- **Mechanism:** track token usage per turn; when approaching a threshold, compact — summarize or evict old tool results/thinking while preserving the task, decisions, and recent turns. Cut at complete-line/complete-block boundaries, never mid-structure. Emit a `context.update` with a compaction counter for observability.
- **Mauler target:** `internal/ledger` (the brain/memory ledger — see `docs/brain-memory-ledger-tracker.md`) + `internal/sessionstore`; a `compactIfNeeded(history)` hook in the loop.
- **Port from:** HelixClaw `context_builder.rs` (ContextBudget tiers by `num_ctx`) and the truncation-at-last-complete-line fixes.

---

## 4. What to add to Mauler — port checklist

Ordered by leverage. Each maps to a Mauler package.

- [ ] **Tool scope enum + per-turn scoping** — tag each `internal/tools` registration with a scope class; add `scopeToolsForTurn(history, iter) []Tool` in the agent runtime; slice defs before every `llm.Generate`. *(Guard 1 — do this first.)*
- [ ] **`internal/agent/guards.go`** — rolling `(callHash, resultHash)` window; outcome-loop warn@2/stop@3; ping-pong period-2/3 detection; `totalToolCalls` circuit breaker at `2×maxIterations`. *(Guards 2 + 3.)*
- [ ] **`salvageToolCall` in `internal/tools`** — text→structured-call rescue validated against scoped tools; wire into the loop's no-native-call branch. Pair with launching the local model under `--jinja` + a Qwen/Gemma-matched tool template so most calls arrive native. *(Guard 4.)*
- [ ] **Result-feedback audit** — grep the loop for any path where a tool error returns without appending a `tool_result`; make error-as-tool-result the only exit. *(Guard 5a.)*
- [ ] **`history.Repair()` before send** — pair orphan `tool_use` blocks, drop malformed entries, validate role alternation; count and log repairs; alarm if repair fires every turn. *(Guard 5b.)*
- [ ] **`compactIfNeeded` hook** — token-budget-tiered compaction over `internal/ledger`; complete-boundary cuts; `compactions` counter in telemetry. *(Guard 6.)*
- [ ] **Telemetry parity** — emit per-turn `iter N/max | tools bound | ctx used/window | compactions` so the outer ring is observable (this is what surfaced every bug in the HelixClaw dashboard).

---

## 5. Multi-agent note (later)

HelixClaw nests this exact loop at each layer: CEO → Orchestrator → Supervisor → Worker, each running its own instance of the three rings with a scoped toolset (e.g. an orchestrator gets `Delegate` scope — it can only spawn/route, not touch files). When Mauler grows past a single agent, the pattern is *recursive*: a "tool" at one layer is a full harness at the layer below. Build the single-agent ring solid first; the multi-agent version is the same ring wired to a `delegate`/`agent_message` tool.

---

## References

- HelixClaw `AgentActor` loop: `crates/helixclaw-agents/src/actor.rs` (loop, `tool_scope_for_turn`, guards, salvage)
- HelixClaw session repair: `crates/helixclaw-agents/src/session_repair.rs`
- HelixClaw context budget: `crates/helixclaw-agents/src/context_builder.rs`
- Mauler existing agent-loop work: `docs/agent-loop-upgrade-roadmap.md`, `docs/agent-reliability-roadmap.md`, `docs/go-native-agent-runtime-update-2026-06-30.md`, `docs/helixclaw-parity-comparison-2026-07.md`
- Loop-guard lineage (OpenFang-inspired): ping-pong, outcome-aware loop detection, hard circuit breaker

> **Grounding caveat:** Mauler package/file targets above are inferred from the current tree (`internal/agent`, `internal/tools`, `internal/sessionstore`, `internal/ledger`, `internal/llm`). The single-agent *driver loop* file wasn't pinned down in this pass — confirm its location against `docs/go-native-agent-runtime-update-2026-06-30.md` before wiring the guards in.
