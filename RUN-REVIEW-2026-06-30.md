# Mauler Run Review — 2026-06-30

Reviewer: Claude (Opus 4.8). Scope: the last several autonomous runs on the **`qwen3.6-nothink`** profile
(model `Qwen3.6-35B-A3B-uncensored-heretic-Native-MTP-Preserved.Q4_K_S.gguf`, the "qwen3.5 35B MTP"),
the no-think sampling settings, and the "it stops now and again on auto" symptom.

Evidence comes from `~/.config/mauler/state.db` (`task_runs`, `task_run_events`, `task_run_tools`),
`runtime-lock.json`, and `profiles.toml`. Code references are to the current working tree.

---

## TL;DR

1. **The stalls are real and reproducible.** The last two `auto`/autonomous runs both ended in
   `auto_continue_exhausted` with the **identical signature**: the model narrates intent
   ("*let me create the malicious script…*", "*now check the fwconsole binary permissions…*") and then
   **ends the turn without emitting a tool call**. `loop_metrics` shows `truncations:0, tool_errors:0` —
   so it is **not** running out of tokens and **not** failing tools. It is a pure **intent→action commit
   failure**.
2. **Two root causes, ranked:**
   - **(A) `presence_penalty = 1.5` on every no-think turn** — far too high for an agentic tool loop.
   - **(B) `thinking = false` on an A3B (3B-active) MoE for multi-step exploitation** — removes the
     scratchpad the model needs to commit to the next tool call.
3. **The loop never forces the model's hand.** `toolChoiceFor` deliberately returns `auto` (never
   `required`) even after repeated narration-only turns, so recovery is a *polite ask* the model keeps
   ignoring. This is the highest-value code fix.
4. **The MTP model itself is healthy** on tool protocol: native OpenAI tool-calls parsed cleanly (17×
   `tool_protocol_native`, **zero** `tool_protocol_repair`/`unrepaired` events). MTP + native tool-calling
   is working — this is a real improvement over the gemma era.
5. **Your no-think sampling is *mostly* right but the penalty is wrong**, and several knobs you may think
   are active (`frequency_penalty`, `repeat_penalty`, `mirostat`, dynamic temp) are **not plumbed to the
   backend at all** — only `temperature, top_p, top_k, min_p, presence_penalty, seed, max_tokens` are sent.

---

## 1. What actually happened in the last runs

Last 6 runs in `state.db`:

| started (06-30) | status | stop_reason |
|---|---|---|
| 18:26 | done | — |
| 18:04 | **stopped** | `auto_continue_exhausted` |
| 17:50 | **stopped** | `auto_continue_exhausted` |
| 14:57 | stopped | `repeated_same_tool_result` |
| 14:06 | stopped | `repeated_tool_failure` |
| 13:48 | stopped | `documentation_missing` |

### The 18:04 run ("ok carry on then") — 26 model calls, 17 tool calls, 8 auto-continues

Auto-continue reasons, in order:
```
1/8 incomplete output
2/8 incomplete output
3/8 said it would act but made no tool call  ("now check the fwconsole binary permissions and PATH…")
4/8 incomplete output
5/8 incomplete output
6/8 said it would act but made no tool call  ("Let me verify this is writable and then create a reverse shell payload.")
7/8 said it would act but made no tool call  ("let me start the listener on Windows, then create a malicious fwconsole script…")
8/8 said it would act but made no tool call  ("Let me create the malicious script and also try to get a reverse shell…")
→ stop: auto_continue_exhausted
loop_metrics: {"tool_calls":17,"auto_continues":8,"truncations":0,"tool_errors":0,"compactions":0}
```

The 17:50 run is the same pattern (8/8, ending on "said it would act but made no tool call").

**Interpretation:** the model knows the next step, says it, and then emits EOS instead of the tool call.
`truncations:0` rules out a max_tokens cut-off. This is the classic thinking-off agentic failure mode,
amplified by an aggressive repetition penalty.

### Side findings from the other stops
- `repeated_tool_failure` run: 60 `bash` done, 5 `bash` error, 1 blocked — it was largely *working*, then
  tripped the repeated-failure guard. Worth checking the guard's sensitivity (it may be counting benign
  repeats).
- **24 `memory_conflict` events** in the 18:04 run: the memory layer correctly *withheld* 15 stale memories
  ("`10.129.23.158 / connected.htb` vs current target") — this is the conflict guard doing its job, but the
  volume is noisy and worth a one-line summary event instead of 24 individual ones.

---

## 2. Are the no-think settings correct?

Active `[profiles."qwen3.6-nothink".nothinking]`:

```toml
temperature      = 0.6
top_p            = 0.8
top_k            = 20
min_p            = 0.0
presence_penalty = 1.5     # ← PROBLEM
max_tokens       = 4096
seed             = -1
```

Because `thinking = false`, `Profile.ActiveParams()` (`internal/settings/model.go:48`) **always returns the
`nothinking` block** for every general/agent turn. (Coding turns are the exception:
`buildChatRequest` at `internal/app/app.go:5586` overrides to `thinking_coding`, which has
`presence_penalty = 0.0, max_tokens = 16384` — those are fine.) So the `1.5` penalty hits exactly the
autonomous Ops turns where the stalls happen.

| Param | Current | Verdict | Recommended |
|---|---|---|---|
| temperature | 0.6 | OK (Qwen no-think guide says 0.7; 0.6 fine) | 0.6–0.7 |
| top_p | 0.8 | ✅ matches Qwen no-think | 0.8 |
| top_k | 20 | ✅ matches Qwen | 20 |
| min_p | 0.0 | ✅ | 0.0 |
| **presence_penalty** | **1.5** | ❌ **too high for tool loops** | **0.0** (agent), ≤0.5 (long prose) |
| max_tokens | 4096 | OK for tool turns; low for prose | 4096–8192 |

**Why 1.5 hurts here specifically:** after a dozen tool turns the context is dense with repeated tool-call
JSON scaffolding (`{"name":…,"arguments":{…}}`, repeated field names, paths). `presence_penalty` penalizes
*every previously-seen token*, including those structural tokens. The model is pushed away from re-emitting
the scaffolding it needs to start another call, and EOS becomes the lower-penalty path → it narrates and
stops. Qwen's own docs warn that high presence_penalty "may occasionally result in language mixing and a
slight decrease in performance," and it matters most in **non-thinking** mode — which is exactly your setup.

> **Quickest single change to test:** set the no-think `presence_penalty` to `0.0` and re-run the Connected
> HTB continuation. I expect the narration-stalls to drop sharply.

### Knobs that are NOT actually wired (so setting them does nothing)
`internal/llm/backends/openaicompat.go` `buildBody` only serializes: `temperature, top_p, top_k (if>0),
min_p (if>0), presence_penalty, seed, max_tokens`, plus thinking/MTP kwargs and grammar. There is **no**
`frequency_penalty`, **no** `repeat_penalty`, **no** mirostat, **no** dynamic-temp field. If you were
relying on any of those, they're silent no-ops today.

---

## 3. Is no-think even the right mode for autonomous Ops?

This is the deeper question behind the symptom. An **A3B MoE has only ~3B active parameters per token**.
With thinking **off**, it loses the reasoning buffer that bridges "I should do X" → "here is the tool call
for X." For single-shot chat and short coding edits that's fine; for **multi-step exploitation chains**
(enumerate → find writable path → craft payload → start listener → trigger) it is exactly where thinking-off
models drop the action.

You already have **`qwen3.6-think`** (same model, `thinking=true`, `spec_type=""`). Three viable directions:

- **Option 1 — use the think profile for Ops mode**, keep no-think for chat/quick tasks. Highest reliability,
  costs latency.
- **Option 2 — stay no-think but fix the loop + penalty** (Section 4). Keeps speed, needs a code change.
- **Option 3 — hybrid:** run no-think but force `tool_choice=required` after the model narrates without
  acting (best of both; see 4.1).

Note on **MTP in no-think mode**: the speculative-decoding stability guard `noteSpecTurn`
(`internal/app/app.go:~2701`) only arms when `req.EnableThinking` is true. With `thinking=false` the guard
is **dormant**, so if MTP rejection were contributing to short/empty turns there is no auto-fallback. Worth
an A/B: temporarily set `spec_type=""` on the no-think profile and compare stall frequency. (Tool protocol
itself is clean, so MTP isn't corrupting tool calls — but it could still be shortening turns.)

---

## 4. The loop-level bug: recovery never forces a tool call

`toolChoiceFor` (`internal/app/app.go:~9656`) returns `auto` whenever `autoContinues > 0 ||
totalToolCallsMade > 0`, with an explicit comment that it *avoids* `"required"` because "local models can
stall or loop when forced." For the gemma era that was true. For **this model with `native-openai` tool
protocol**, the opposite is now happening — the model stalls *because* it is never forced.

So the recovery flow is: model narrates → loop appends a strongly-worded user message ("Do NOT write more
text, call the tool RIGHT NOW") via `buildDirectivePrompt` → **but sends it with `tool_choice=auto`** → model
narrates again. Eight times. Then gives up.

### 4.1 Recommended fix (highest impact, low risk)
After **2 consecutive narration-without-action continues** (`noToolContinues >= 2 && aboutToAct`), set
`toolChoice = "required"` for the next turn. The plumbing already exists — `pendingRepairToolDefs` sets
`toolChoice="required"` for malformed-markup recovery (`app.go:2437`). Reuse that path for the narration
case. For `native-openai` protocol, `required` is well-supported and will force the structured call instead
of EOS.

Optionally pair with the single-tool **grammar/JSON-schema constraint** that already exists
(`constrainedToolArgsSchema`, `app.go:5628`) when you can narrow to one obvious tool — that hard-guarantees
a well-formed call.

### 4.2 Secondary: lower the no-think `presence_penalty` (config-only, do this first)
Independent of the code change. `0.0` for the agent/no-think block.

### 4.3 Tertiary: make `documentation_missing` / `repeated_*` guards less trigger-happy
The `repeated_tool_failure` run had 60 successful bash calls vs 5 errors yet stopped — confirm the guard
isn't counting non-adjacent or benign repeats as a failure streak.

---

## 5. Telemetry gap

Every run shows `total_tokens = 0` in `task_runs`, even though the stream parser **does** read the usage
chunk (`internal/llm/stream.go:99-104`) and `buildBody` requests `StreamOptions.IncludeUsage = true`. Either
llama.cpp isn't emitting the final usage SSE for these requests, or `run.setTokens` isn't persisting. This
matters because **`backendPromptTokensNeedCompaction` keys off `usage.PromptTokens`** — if usage is always 0,
that backend-pressure compaction path never fires and you're relying solely on the local token estimate.
Worth a 10-minute verification (curl the llama.cpp `/v1/chat/completions` with `stream_options` and confirm
a usage chunk arrives).

---

## 6. Action list (in order)

| # | Change | Type | Effort | Expected effect |
|---|---|---|---|---|
| 1 | No-think `presence_penalty` 1.5 → **0.0** | config | 1 min | Big drop in narration-stalls |
| 2 | Force `tool_choice=required` after 2 narration-only continues | code (`app.go` loop + `toolChoiceFor`) | ~1 hr | Eliminates `auto_continue_exhausted` stalls |
| 3 | A/B: `spec_type=""` vs `draft-mtp` on no-think; measure stalls | experiment | 30 min | Isolates MTP's role |
| 4 | Consider `qwen3.6-think` (or hybrid) for **Ops mode** | config/routing | varies | Highest reliability for exploit chains |
| 5 | Collapse 24 `memory_conflict` events → 1 summary; re-check withhold volume | code | 30 min | Less log noise |
| 6 | Verify backend `usage` is recorded (`total_tokens=0` bug) | investigate | 10 min | Restores backend-pressure compaction + real token metrics |
| 7 | Soften `repeated_tool_failure` guard (60 ok / 5 err still stopped) | code | 30 min | Fewer false stops |

**If you change one thing today:** #1 (penalty → 0.0). **If you change two:** #1 + #2.

---

## Appendix — exact locations
- Param selection (no-think always uses `nothinking`): `internal/settings/model.go:48`
- Coding-override to `thinking_coding`: `internal/app/app.go:5586`
- Request build / sampling: `internal/app/app.go:5582` (`buildChatRequest`)
- Backend body (which params are actually sent): `internal/llm/backends/openaicompat.go:670`
- Auto-continue / narration recovery: `internal/app/app.go:2896`–`3046`
- Directive prompt: `buildDirectivePrompt` in `internal/app/app.go`
- Tool-choice (never `required`): `toolChoiceFor` in `internal/app/app.go:~9656`
- MTP stability guard (dormant in no-think): `internal/app/app.go:~2701` (`noteSpecTurn`)
- Usage parse: `internal/llm/stream.go:99`

---

## Codex follow-up fixes applied

Status after local source review:

- Fixed: default Qwen no-think `presence_penalty` is now `0.0` in `internal/settings/defaults.go`, with the settings regression test updated. The live `qwen3.6-nothink` profile in `~/.config/mauler/profiles.toml` already had `nothinking.presence_penalty = 0.0`.
- Fixed: repeated narration/no-tool recovery now applies a one-shot `tool_choice="required"` latch after repeated no-tool recovery turns where the model has said it is about to act. The event is logged as `tool_choice_required`.
- Fixed: repeated shell failure detection now resets when the same command has a later successful `done` result, so scattered old errors do not block a mostly-working run.
- Improved: when a backend does not emit streaming `usage`, task logs now record a local token estimate via a `usage_estimate` event instead of leaving run token counts at zero.
- Still pending: MTP no-think A/B, planner/executor/reviewer split, and deeper benchmark matrix for batch/ubatch, quant, context, KV, flash-attn, and sampler settings.

Done checklist:

- [x] No-think `presence_penalty` 1.5 -> 0.0 default.
- [x] Confirmed live `qwen3.6-nothink.nothinking.presence_penalty` is already 0.0.
- [x] Force `tool_choice=required` after repeated narration-only recovery.
- [x] Soften repeated shell failure guard by resetting the streak after a successful same-command result.
- [x] Add token usage estimate fallback when backend usage is missing.
- [x] Add focused regression coverage for the settings/default and repeated-shell-failure changes.
- [ ] Run MTP no-think A/B: `spec_type=""` vs MTP enabled.
- [x] Collapse noisy memory-conflict event detail into a capped summary.
- [ ] Implement planner/executor/reviewer split eval lane.
- [ ] Build benchmark matrix for batch/ubatch, quant, context, KV, flash-attn, sampler settings.

Benchmark/context follow-up:

- [x] Benchmark loads now request an exact forced model reload when the backend supports it, so 8k/16k context rows do not silently reuse a larger 32k model.
- [x] InferenceBridge `/v1/models/load` accepts `force_reload` / `forceReload` and bypasses same-model context reuse for benchmark loads.
- [x] Benchmarks tab now re-syncs provider selection when the selected profile changes, reducing stale placeholder-provider runs.
- [x] AgentEval suite reports are persisted to `agent-eval-runs.json` so future run reviews can inspect exact pass/fail results.
- [x] AgentEval now reports separate `status_pass`, `artifact_pass`, and `hygiene_pass` fields so correct artifacts with messy run shape are visible.
- [x] Storage view can list and clear AgentEval history.

---

## 7. Benchmark review (added 2026-06-30, second pass)

There are **two separate benchmark systems** and they are being confused.

### 7.1 The "Fast daily context benchmark" (ctx-probe) — currently BROKEN, ignore its results
All 8 of today's `benchmark-runs.json` entries are `profile_name = "ctx-probe"`, and every one is
**misconfigured**:
- `model_id = "model.gguf"`, `base_url = http://localhost:8080/v1` — that is the **default/placeholder
  profile**, not your active `qwen3.6-nothink` at `127.0.0.1:8800`. Nothing is even listening on `:8080`
  (verified — connection refused).
- It requested 32768 and the (wrong) backend reported `actual_ctx_tokens = 8192`.
- The run **self-labels the failure** in `notes`: *"No model-family runtime profile matched"* and
  *"Backend reported actual context 8192 after requesting 32768; this run is not a valid 32k context
  measurement."*
- Only the `Tiny` scenario ran, `0.0 tok/s`, `TTFT 0 ms`.

**Conclusion:** the ctx-probe is testing a non-existent server. Its `warn`/`score 100` results are
meaningless and are **not** a measurement of the qwen3.6 35B model. Fix: point the daily benchmark at the
active profile+provider (`qwen3.6-nothink` / `llamacpp-local` / `:8800`), not the default profile.

### 7.2 The AgentEval capability suite (6 scenarios) — this is the "tests with answers"
These are the pass/fail tests you mean. **Their results are not persisted to disk** (the harness sets
`Logging=false`, `suppressEvents=true`; there is no eval table in `state.db` and no ledger entries), so I
cannot read the exact per-test verdicts you saw in the UI — the analysis below is from the scenario
definitions (`internal/app/testdata/agent_eval/*.json`) and the scorer (`scoreAgentEvalResult`,
`agent_eval.go:213`). **Paste the failing test names (or re-run with logging on) and I'll confirm exactly.**

The six tests:

| Test | mode | max_auto_continues | expect_status | expect_files (substring) |
|---|---|---|---|---|
| chunked-write | Builder | 4 | done | `generated/long.txt` ⊃ "Line 150" |
| duplicate-read-guard | Builder | 2 | done | — |
| edit-then-verify | Fixer | 2 | done | `main.go` ⊃ `return "123"` |
| grep-then-edit | Builder | 2 | done | `src/task.txt` ⊃ "status = DONE" |
| read-and-summarize | Builder | 2 | done | — |
| stop-cleanly-on-budget | Builder | 3 | **stopped** | — |

**The scorer is all-or-nothing and AND-ed** across four gates: `expect_status` must match exactly,
every `expect_files` substring must be present, `auto_continues` must be ≤ the per-test cap, and no
`forbid_substr` may leak. A test fails if **any** gate fails.

**Why you "think it passed some" — and you're probably right.** The suite runs `autonomous=true` with your
**active profile**, which at benchmark time was still `presence_penalty = 1.5` (the fix landed at 18:40,
after the run). So the exact narration-stall from Section 1 poisons the benchmark: the model writes the
**correct file** but racks up auto-continues and ends `stopped`/`auto_continue_exhausted`. Result:

- `edit-then-verify`, `grep-then-edit` (cap 2, want `done`): **most likely false-fails** — the artifact
  (`main.go` / `src/task.txt`) is probably correct, but `status=stopped` and/or `auto_continues>2` fail it.
- `read-and-summarize`, `duplicate-read-guard` (cap 2, want `done`, no file check): fail purely on run-shape
  (status/auto-continues) — there is no artifact to give them credit for, so a stall = automatic fail.
- `chunked-write` (cap 4): more headroom, more likely to genuinely pass.
- `stop-cleanly-on-budget` (wants `stopped`): inverted — a stall still counts as `stopped`, so this one
  likely **passes** regardless.

**Recommendations:**
1. **Re-run the suite now** that `presence_penalty=0.0` and the `tool_choice=required` latch are in. I expect
   the `done`-status tests to flip to pass. This is the single most informative next step.
2. **Separate artifact correctness from run hygiene in scoring.** Today a correct answer with 3 auto-continues
   is scored identically to a wrong answer. Report `artifact_pass` (expect_files/expect_status) and
   `hygiene_pass` (auto_continues) as **distinct** fields so a false-fail is visible as "right answer, untidy
   run" rather than a flat FAIL. (`scoreAgentEvalResult`, `agent_eval.go:221-250`.)
   Status: fixed in the AgentEval report and Benchmarks UI.
3. **Persist eval results** (even a JSON next to `benchmark-runs.json`, or a `state.db` table) so these
   reviews don't depend on UI screenshots.
   Status: fixed via `agent-eval-runs.json`.

---

## 8. Context size: "loaded at 16k vs UI shows 32" — NOT a bug

Probed the live server directly. **The model is genuinely loaded at `n_ctx = 32768`**, and the UI showing
**32(k) is correct.** Nothing is loaded at 16k.

- `GET http://127.0.0.1:8800/props` → `default_generation_settings.n_ctx = 32768`, model =
  `…/Qwen3.6-35B-A3B-…-MTP-Preserved.Q4_K_S.gguf`. (This is an LM Studio-managed llama.cpp server — the model
  path is in the lm-studio cache.)
- Profile `qwen3.6-nothink.ctx_tokens = 40000` and `runtime-lock.json = 40000` are the **requested ceiling**,
  not what got loaded. The server (loaded externally by LM Studio) only gave **32768**.
- The app **correctly reconciles down**: `ensureModelLoaded` queries `ActualContextLength`, and at
  `app.go:5524` does `if actual < window { window = actual }` → sets `a.contextWindow = 32768` and resizes the
  history budget. So the status bar reflects the real loaded context. **Working as intended.**

Where "16k" comes from: that's almost certainly `thinking_coding.max_tokens = 16384` — a **max-output-tokens**
cap, which is unrelated to the context window. There is no 16k context anywhere.

**If you actually want 40k context:** you must relaunch the model **in LM Studio / the `:8800` server** with a
larger context — the Mauler profile cannot enlarge an already-loaded external model. And check VRAM first:
35B-A3B Q4_K_S weights + a 40k KV cache is tight on a 24GB 3090; 32k may already be near the practical
ceiling unless you use KV quantization (`--cache-type-k/-v q8_0`) or flash attention. So 32k is a reasonable
real-world load, and the UI is telling you the truth.

---

## 9. Codex backlog (ordered) — added 2026-06-30

Build in this order.

### Fix 1 — Prompt de-bloat (system 10k → ~3k tokens)
`buildSystemPrompt` (`app.go:7369`) is 140 `WriteString` calls of mostly-static prose re-sent every turn.
1. **Move tool-usage instructions onto each tool's `Description()`** (terminal_run, http_probe, glob/grep,
   web budget, shell timeouts). Biggest win (~3–4k); auto-scopes to enabled tools; can't drift.
2. **Layer prompt = stable cached prefix + tiny per-turn tail.** Put the invariant block first byte-identical
   for llama.cpp prefix-KV reuse; push volatile bits (date at line 7372, memory packets, lab context) to the
   end.
3. **Prose → terse bulleted rules** (~40% shrink, no behavior loss).
4. **Externalize mode/Ops fragments to files** (HTB/pentest/evidence-policy blocks), loaded by mode like
   skills — versionable, A/B-able, no recompile.
5. **Gate tools harder.** Toolset infra already exists (`ActiveToolset`/`DefaultToolsets`/
   `EffectiveEnabledTools`, `settings/load.go:429`) but the last run advertised 36 tools. Define tighter
   per-mode sets (~10–12 core) + progressive disclosure for specialists (browser, start_listener, firmware,
   subagents).
6. **Enforce a budget** off the existing `prompt_budget` events: target system ≤ 3k, tools ≤ 1.5k; log when
   exceeded so regressions are visible.

**Effort:** 1–2 days; start with #1 (fast, measurable, zero risk).

### Fix 2 — Tool registry v2 (typed, generated, MCP-shaped)
Current `tools.Tool` (`registry.go:15`) hand-writes JSON schema beside a parallel struct (drift), exposes
only `Destructive() bool`, and returns `(string, error)`.
1. **Typed tools with generated schema** — `Tool[P any]` wrapper; generate `Schema()` from `P`'s
   `jsonschema` struct tags (`github.com/invopop/jsonschema`); `Run()` unmarshals into `P`. Kills drift.
2. **MCP-aligned metadata** — replace the bool with `ToolMeta{Title, Namespace, ReadOnly, Destructive,
   Idempotent, RequiresConfirm, DefaultTimeout}`. Loop's `shouldConfirmTool`/guardrails/budget read meta
   instead of name special-casing.
3. **Structured `ToolResult{Content, IsError, Artifacts}`** (maps to MCP `CallToolResult`) so
   `toolResultForContext`/`offloadToolResultMessages` work uniformly.
4. **One registry that is also an MCP host** — built-ins and external MCP-server tools share the interface;
   built-ins can be exposed as an MCP server. This is the shared-core lever: Mauler vs HelixClaw differ only
   by toolset/namespace.
5. **Namespacing** (`fs.read_file`, `shell.exec`, `recon.http_probe`).

**Migration (no big-bang):** add `Tool[P]` alongside the current interface with an adapter; port
namespace-by-namespace starting with `fs`; add `ToolMeta` and switch loop checks to it; introduce
`ToolResult` with a legacy string wrapper; add the MCP host last.

**Effort:** multi-day, incremental.

### Ordered checklist for codex
- [x] **Fix 1.1:** move per-tool usage prose from `buildSystemPrompt` into tool `Description()`s; measure token drop. **Done 2026-06-30:** terminal/http/file/web/reverse-shell workflow prose moved into tool descriptions; global prompt now keeps a compact routing and loop-discipline reminder.
- [x] **Fix 1.2:** reorder prompt into stable prefix + volatile tail for prefix-KV reuse. **First pass done 2026-06-30:** stable identity/rules/tool-routing now precede volatile date/mode/workspace/memory context.
- [ ] **Fix 1.3:** convert remaining prose rules to terse bullets.
- [ ] **Fix 1.4:** externalize mode/Ops prompt fragments to loadable files.
- [x] **Fix 1.5:** tighten per-mode toolsets + progressive disclosure for specialist tools. **Done 2026-06-30:** Ops first-turn routing now advertises a lean shell/read/http/memory core (<=18 tools in regression coverage), keeps host web/browser tools out unless requested, and broadens specialists on continuation/recovery.
- [x] **Fix 1.6:** add system/tool token budget targets + over-budget logging. **Done in source 2026-06-30, not rebuilt yet:** prompt budget telemetry now warns on system >3k tokens, tool schema >1.5k tokens, or system+tools >20% of context; Brain/Logs show used/target values.
- [ ] **Fix 2.1:** `Tool[P]` + generated schema wrapper (adapter keeps old interface working).
- [ ] **Fix 2.2:** `ToolMeta` annotations; switch loop confirm/guardrail/budget to read meta.
- [ ] **Fix 2.3:** `ToolResult` structured returns with legacy wrapper.
- [ ] **Fix 2.4:** namespacing + single MCP-host registry.

### Active implementation pass - prompt/tool routing
- [x] Track the first prompt de-bloat pass in this review file and `ISSUES.md`.
- [x] Move live terminal, listener, HTTP probe, file inspection, and web-search routing detail out of the global system prompt.
- [x] Keep a short global routing reminder so Qwen still knows which family of tools to reach for.
- [x] Add regression coverage that the moved guidance lives in tool descriptions, not repeated prompt prose.
- [x] Run focused tests: `go test ./internal/app ./internal/tools`.
- [x] Rebuild production exe after verification: `C:\Users\richa\Desktop\TheMauler\build\bin\TheMauler.exe`.
