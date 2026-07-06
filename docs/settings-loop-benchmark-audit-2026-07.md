# Settings + Loop + Benchmark audit — actionable handoff (2026-07-02)

**Audience:** an AI coding agent (Codex/Claude) that will implement these fixes.
**Scope:** analysis of the live settings, the agent loop, and the benchmark, with concrete edits for
better agentic reliability. Findings are grounded in the current code and the user's live config on
disk (`~/.config/mauler/settings.toml`, `profiles.toml`, `benchmark-runs.json` as of 2026-07-02).

Companion doc: `docs/ai-terminal-parity-and-tooling-fixes-2026-07.md` (the terminal/VT work). Do the
P0 items here and there in parallel; they compound.

**Verify every change:**
```bash
go build ./... && go test ./internal/app/ ./internal/settings/ ./internal/tools/
cd frontend && npm run build   # only if you touch frontend/
```
Run all Go/wails commands from `C:\Users\richa\Desktop\TheMauler` (never cd into a subdir first).

---

## Current Status - 2026-07-06

Keep this track Mauler-only. The live user path is InferenceBridge through the OpenAI-compatible provider shape; do not re-add LM Studio/SGLang/vLLM providers unless explicitly asked.

Done since this audit:
- Context shortfall is now a hard failure when the active backend reports actual context below the selected profile's `ctx_tokens`. The app must not run silently at 8K when the user selected 32K/40K/54K.
- The user's live `profiles.toml` is InferenceBridge-only. Non-IB provider entries were stripped from the live config.
- Qwen and Gemma sampler/context profile fixes were applied in the live config, including Gemma `top_k=64`, presence penalty cleanup, and lowering the dead 120K Gemma profile to a realistic context.
- `tool_grammar_constraint` is disabled in the live config until a grammar/tool-call probe proves it helps.

Still open:
- Reasoning Effort remains conceptually weak on no-think profiles unless the runtime either switches to a think sibling for high effort or makes the UI honest.
- Real profile benchmarks and `agent_eval` gating still need to be run and wired into the workflow.
- Tool-result budgets should be reviewed after one or two stable live runs at the real context size.

---

## P0 — the three things that dominate everything else

### P0-1. Context window may be silently capped at 8K despite profiles asking for 40K

**Evidence.** The newest probe in `~/.config/mauler/benchmark-runs.json` (2026-07-02T09:16) requested
`ctx_tokens=32768` but the backend reported `actual_ctx_tokens=8192`. The active profile
`qwen3.6-nothink` declares `ctx_tokens=40000`. If the running llama.cpp server was launched with a
small `-c/--ctx-size`, **every agent run is truncated to ~8K** while the app believes it has 40K.
This alone would cause the compaction-thrash, "forgot the creds it just found," and
re-run-the-same-command instability that reads as "the model is dumb."

**Fix.**
1. **Doctor assertion (P0).** Add a check that compares the backend's reported context against the
   profile's `ctx_tokens` and fails loudly when `actual < configured`. The query helper already
   exists: `benchmarkActualContext` / the client's `ActualContextLength(ctx)` interface
   (`internal/app/benchmark.go:231`). Wire the same call into `internal/app/doctor.go` as a
   check (e.g. `checkActualContextMatchesProfile`) that returns `fail` with the two numbers and the
   remediation ("relaunch llama.cpp with `-c 40960` / `--ctx-size 40960`").
2. **Status bar surfacing.** Plumb `actual_ctx_tokens` to the frontend context indicator so a
   shortfall is visible during runs, not just in Doctor. The token bar already renders
   `context_window`; show `actual` when it differs from the profile request.
3. **Startup log line.** On model load (`ensureModelLoaded` path around `app.go:2404`) emit a run
   event when actual < configured so the run ledger records it.

**Acceptance:** with a server started at `-c 8192`, Doctor shows a red "context shortfall 8192 <
40000" check and the status bar shows the real number; with `-c 40960` the check passes.

---

### P0-2. "Reasoning Effort: High" is inert on the active (no-think) profile

**Root cause (verified).** `effortToThinking` (`internal/app/reasoning_effort.go:55`) maps `high` →
`enableThinking: profile.Thinking`. The active profile `qwen3.6-nothink` has `thinking=false`, so
High resolves to thinking-OFF. The `set_reasoning_effort` tool cannot add depth it never had, and
`no_think_after_tool_calls=2` is mostly a fallback because tool-enabled Qwen turns already force no-thinking. The UI (AgentPanel "Reasoning Effort" dropdown, set to High)
presents a lever that is not connected on this profile.

**Fix — pick one (prefer B):**
- **A (honest UI):** when the resolved profile has `thinking=false`, disable/grey the Reasoning
  Effort control and show "profile has thinking disabled." Frontend: `AgentPanel.tsx` effort
  selector; gate on the active profile's `thinking`.
- **B (make it work):** when effort ≥ `high` and the active profile is a no-think variant that has a
  think sibling (e.g. `qwen3.6-nothink` → `qwen3.6-think`, same `model_id`), auto-route the run to
  the think profile (or set `req.EnableThinking=true` if the same GGUF supports a think mode). Keep
  `low`/`minimal` on the fast path. This is the higher-value option because it lets one effort knob
  span both speed and depth.

Either way, add a test in `reasoning_effort_test.go` asserting the effort→thinking resolution for a
no-think profile so this can't silently regress.

**Acceptance:** setting effort High on a no-think profile either (A) is visibly disabled with a
reason, or (B) produces `enableThinking=true` in the chat request (assert via the
`tool_protocol_request` run event `thinking=` field).

---

### P0-3. The benchmark history is 100% ctx-probes — no real agentic measurement exists

**Evidence.** All 107 entries in `benchmark-runs.json` are `profile_name="ctx-probe"`,
`model_id="model.gguf"`, a single `"Tiny"` scenario, `0 tok/s`, `score 100`. The real 4-scenario
benchmark (General chat / Coding / JSON discipline / Tool protocol in `benchmark.go:272`) has
**never been run** on the actual models. Every green "100" is meaningless for agent quality.

**Fix.**
1. Run the real profile benchmark (`BenchmarkProfileWithCases`) for `qwen3.6-nothink`,
   `qwen3.6-think`, and `gemma4-26b-a4b-qat`, and keep those runs. The **Tool protocol** case is the
   important one: `structured_tools` vs `repaired_tools` tells you whether grammar-constraining tool
   calls is needed for that model (ties to the dormant `tool_grammar_constraint` flag).
2. **Separate probes from real runs.** ctx-probes and scenario benchmarks share one file and the
   probes drown out real data. Either write ctx-probes to a different file, or tag them so
   `ListBenchmarkRuns` / the SettingsModal benchmark view can filter probes out by default.
   Anchor: `saveBenchmarkRun` / `loadBenchmarkRuns` (`benchmark.go:548-585`), and the
   Settings benchmark UI in `SettingsModal.tsx`.
3. **Promote `agent_eval` to the real gate.** `internal/app/agent_eval.go`
   (`runAgentEvalScenarios`) measures multi-step loop behavior — that is the actual agentic-quality
   signal, and it is built but unused. Add a way to run it (a button or a `go test`-invokable
   harness) and treat it as the pre/post regression gate for every change in this doc.

**Acceptance:** the benchmark view shows real 4-scenario runs per model with non-zero tok/s and a
Tool-protocol structured/repaired count; `agent_eval` is runnable and produces a scored report.

---

## P1 — settings edits for agentic reliability

These are safe config edits. Live file: `~/.config/mauler/settings.toml`. Prefer changing
`internal/settings/defaults.go` for anything that should be the shipped default, and only touch the
user's TOML for user-specific choices.

### P1-1. Tool count is too high for a 3B-active MoE — build a lean Ops toolset

The active toolset `unrestricted` exposes ~40 tools, including **five overlapping shell verbs**
(`shell`, `bash`, `terminal_run`, `terminal_send`, `terminal_read`) plus `start_listener`,
`http_probe`, `run_script`, browser tools, and 5 subagents. The user's own notes record that the
local models' tool-calling is fragile; this much surface causes mis-routing. Claude/Codex stay
reliable with a *small orthogonal* set.

**Fix.**
- Add an `ops-lean` toolset (~20 tools) to `DefaultToolsets()` (`internal/settings/defaults.go:181`)
  and make it the Ops preset default. Suggested members: `read_file, read_many, glob, grep,
  write_file, edit_file, terminal_run, terminal_send, terminal_read, start_listener, http_probe,
  run_script, memory, progress, file_changes, evidence_bundle, todo_*, read_tool_result,
  set_reasoning_effort`.
- **Drop `bash` as a distinct tool** — it duplicates `shell`. Note `EffectiveEnabledTools`
  (`internal/settings/load.go:462`) already hard-links `bash` to `shell`; finish the job by removing
  `bash` from the toolsets/enabled map and the registry, or alias it. One shell one-shot verb +
  the three terminal verbs is the target.
- Trim to a single interactive-terminal write verb if the companion terminal doc's P1-1 lands
  (merge `terminal_run` into `terminal_send`).

**Acceptance:** Ops runs offer ~20 tools (assert via the `tool_routing` run event `tool_count`);
`agent_eval` tool-selection accuracy is equal-or-better than the 40-tool baseline.

### P1-2. Right-size tool-result context budgets to the *real* window

`tool_result_aggregate_chars=24000` is ~60% of a 40K window in a single turn — and catastrophic if
P0-1 shows the real window is 8K. After P0-1 is confirmed:
- If real window is 40K: set `tool_result_aggregate_chars` to **12000–16000**.
- Keep `tool_result_preview_chars=2000` (the user reverted a prior bump; the scan-output cleaning is
  the real fix — see memory `scan_output_starvation`).
- Anchors: `internal/settings/defaults.go:25-27`, clamp logic in `internal/settings/load.go:79-85`.

### P1-3. Profile guidance for the user's HTB use case
- For **hard multi-step boxes**: keep execution on `qwen3.6-nothink`; use `qwen3.6-think` only for a
  short no-tool planner/reviewer pass when the agent needs deeper branching.
- For **speed / rote work**: `qwen3.6-nothink` with effort `low`. `spec_type=draft-mtp`,
  `spec_draft_n_max=2` is correct only after the backend proves stable.
- This should be an explicit mode/pass boundary, not a manual toggle the user forgets mid-run.

---

## Reference — how the loop looks today (so Codex has the map)

Core loop: `runAgentLoop` (`internal/app/app.go:2294`). Per-turn structure:

```
setup: system prompt (mode+memories+skills) · load model (retries) · context budget · keepalive
loop:
  1. budget gates        tool/time exhausted → force text-only summary; recovery/doc-recovery gates
  2. routing             toolDefsAndChoiceForTurnWithState(phase, #calls, terminal state) → subset + tool_choice
  3. memory              re-score & re-inject newly-relevant memories
  4. context ladder      needsCompaction → clear old tool results → microcompact → summarize
  5. nudges              foothold (re-exploit loop) · listener (reverse shell) · persist · goal
  6. build request       execution-state prompt · forceNoThink after N calls · effort · optional GBNF
  7. stream              client.Chat → thinking / visible text / tool calls
  8. tool protocol       native tool_calls else repair inline markup / schema / reject hallucinated
  9. spec guard          MTP truncation-at-</think> → fall back to stable decode
 10. execute tools       confirm · rollback snapshot · run · offload big results · mutation-verify · ledger
 11. continue?           tool calls → loop · clean text answer → finish
finish (defer): living-doc check · loop-metrics · checkpoint delete · save run · milestone memory · distill
```

**Strengths (keep):** graduated compaction ladder, memory re-injection, tool-protocol repair chain,
MTP stability guard, pentest foothold/listener nudges.

**Risks to address (in priority order, mostly covered above):**
- Injected-message pressure per turn (execution-state prompt + nudges + goal reminders) is fine on a
  true 40K window, crushing on a silently-8K one → **P0-1**.
- Routing depends on terminal state, which is heuristically classified and bash-prompt-biased →
  fixed by the VT screen model in the companion terminal doc.
- Loop leans on the model choosing well among many tools → **P1-1** (lean toolset) reduces the
  surface it must get right.

---

## Priority summary

| # | Item | Why | Effort |
|---|------|-----|--------|
| P0-1 | Doctor + status-bar assertion: actual vs configured context | Silent 8K cap would dominate all instability | S–M |
| P0-2 | Make reasoning-effort coherent on no-think profiles | "High" is currently inert / misleading | S–M |
| P0-3 | Run real benchmarks + promote agent_eval as the gate | No real agentic measurement exists today | M |
| P1-1 | ops-lean toolset (~20), drop `bash` | Fewer wrong tool choices on a 3B-active MoE | M |
| P1-2 | Right-size tool-result budgets to real window | 24k aggregate crowds context | S |
| P1-3 | Profile-per-task guidance (think vs nothink) | Match model depth to task | S (follows P0-2B) |

**Do P0-1 first.** Confirming or ruling out the 8K context cap changes how you tune everything else.

## Verify (every change)
```bash
go build ./... && go test ./internal/app/ ./internal/settings/ ./internal/tools/
```
Grade loop behavior with `internal/app/agent_eval.go` before/after each change; keep the real
per-profile benchmark runs (not ctx-probes) as the inference-health baseline.
