# TheMauler — Agent-Loop Upgrade Roadmap (Hermes / Claude-Code-class patterns)

**Created:** 2026-06-29
**Owner doc:** live, implementable source of truth for the next agent-loop capability tier.
**Analysis / why:** [agent-loop-upgrade-plan-2026-06.md](agent-loop-upgrade-plan-2026-06.md) (cross-project),
building on the completed [agent-reliability-roadmap.md](agent-reliability-roadmap.md) (R1–R10) and
[agent-loop-research-report.md](agent-loop-research-report.md).
**Audience:** an engineer or AI agent picking this up cold. Every item is self-contained — files,
signatures, algorithm, wiring anchors, tests, and acceptance criteria are spelled out so you can
implement without re-deriving context.

> Conventions
> - File anchors like `app.go:2062` are point-in-time; line numbers drift. Always `grep` the named
>   symbol (e.g. `func (a *App) runAgentLoop`) to relocate it before editing.
> - The loop lives in `internal/app/app.go`, `func (a *App) runAgentLoop(...)` (~`app.go:2062`),
>   label `agentLoop:`. Per-turn request is built by `buildChatRequest` (~`app.go:5096`); tool defs
>   + choice by `toolDefsAndChoiceForTurn` (~`app.go:8745`); system prompt by `buildSystemPrompt`
>   (~`app.go:6749`).
> - Keep `go build ./...`, `go vet ./...`, `go test ./...` green. Add an `agent_eval` scenario
>   (`internal/app/agent_eval.go` + `testdata/agent_eval/*.json`) for every behavioral item so it is
>   regression-graded. Build via `.\build.ps1`.

## Status legend
- 🔨 **Spec ready** — not started; full spec below.
- 🧪 **Held** — needs a live backend to validate before enabling.

## Item map

| ID | Title | Tier | Effort | Status |
|----|-------|------|--------|--------|
| U1 | Dynamic reasoning-effort control (`set_reasoning_effort` tool) | 1 | S | ✅ first pass |
| U2 | Tool-result disk offload + `read_tool_result` (replace blind truncation) | 1 | M | ✅ first pass |
| U3 | InferenceBridge launch-flag + quant assertions in Doctor | 1 | S | 🔨 |
| U4 | Programmatic tool calling (`run_script` over the registry) | 2 | M–L | 🔨 |
| U5 | Graduated compaction ladder (add microcompact tier) | 2 | M | 🔨 |
| U6 | Externalized `PROGRESS.md` resume artifact | 2 | S–M | 🔨 |
| U7 | R7 grammar-constrained tool args — run the live probe, then ship gated | 3 | M | 🧪 |

---

# Tier 1

## U1. Dynamic reasoning-effort control ✅ first pass  ⭐ do first

**Goal.** Let the agent scale its own thinking depth per step via a state-modifying tool, and let
the auto-router set a sensible default per mode. Directly attacks the dominant local-Qwen failure
surface (truncated `<think>`), which the loop currently only *recovers* from via `forceNoThink`.

**Why.** Hermes exposes `reasoning_effort` as a first-class tool because agents "cannot reliably use
slash commands as structured state transitions." Matching depth to task (minimal for rote
edits/reads, high for hard reasoning) cuts truncation/auto-continue churn and tokens. Today thinking
is a coarse global per-profile toggle plus the blunt `forceNoThink` after N tool calls
(`app.go:2360`).

**Files.**
- Touch: `internal/llm/client.go` (`Request` struct, ~`:88`) — add `ReasoningEffort string`.
- Touch: `internal/app/app.go` — `buildChatRequest` (~`:5096`), `runAgentLoop` loop head (effort
  state + per-turn override), tool-exec dispatch (intercept the new tool), `buildSystemPrompt`
  (~`:6749`, add the nudge).
- Touch: `internal/app/agent_modes.go` — set a default effort per `AgentMode`.
- New: `internal/app/reasoning_effort.go` + `_test.go` (the effort→params mapping helper).

**Data / mapping.**
```go
// effortToThinking maps an effort tier to thinking + budget knobs, layered on top
// of the profile's active sampler family. minimal/low ⇒ thinking off or tight budget.
type effortPlan struct {
    enableThinking bool
    maxTokensCap   int   // 0 = leave profile value; else min(profile.MaxTokens, cap)
    coding         bool  // prefer the deterministic coding sampler family
}
func effortToThinking(effort string, profile settings.Profile) effortPlan
// "minimal" -> {false, small, true}; "low" -> {false, _, true};
// "medium"  -> {profile.Thinking, _, false}; "high" -> {true, larger, false}.
```

**Algorithm.**
1. Add `ReasoningEffort string` to `llm.Request`; have each backend pass it through where it already
   passes `EnableThinking` (it can also map to `--reasoning-budget`/`max_tokens` think cap on
   llama.cpp; for now reuse `MaxTokens`).
2. In `runAgentLoop`, add `currentEffort` (seeded from `mode.DefaultEffort`, default `"medium"`).
   When building the request, compute `effortToThinking(currentEffort, profile)` and let it override
   `forceNoThink`/`enableThinking`/`maxTokens` (`buildChatRequest` gains an `effort` param, or apply
   the plan to the returned `llm.Request`). Keep the existing `forceNoThink` as a hard floor (it can
   still force thinking off, never on).
3. **Intercept** a `set_reasoning_effort` tool in the tool-exec loop *before* registry dispatch
   (mirror how state must mutate the in-memory run; note `todo_*` tools persist to disk via the
   registry, but this one must set `currentEffort` on the loop). On call: validate the enum, set
   `currentEffort`, return a synthetic result `{"ok":true,"effort":"low"}`. Cap changes per run
   (e.g. 6) to prevent thrash; after the cap, ignore + nudge.
4. Register the tool def so the model sees it (schema: `{effort: enum[minimal,low,medium,high]}`,
   non-destructive). Add to the default read/safe toolsets in `defaults.go` (~`:144`).
5. `buildSystemPrompt` nudge: "Before rote reads/edits/formatting, call `set_reasoning_effort`
   `low`; reserve `high` for ambiguous design or debugging. Do not change effort more than a few
   times per task."

**Tests.** `TestEffortToThinkingMapping` (table-driven), `TestSetReasoningEffortIntercept` (call
sets state, caps thrash, validates enum), `agent_eval` scenario `rote-multi-edit` asserting
truncations/auto-continues drop vs a baseline run.

**Acceptance.** Model can lower its own effort on a rote scenario and the loop honors it on the next
turn; hard-reasoning scenario unaffected; per-run change cap enforced.

**Effort.** S.

**Done so far.** Added `ReasoningEffort` to `llm.Request`, exposed/intercepted
`set_reasoning_effort`, seeded per-run effort from agent mode, mapped effort to thinking/max-token
request knobs, logged effort changes, added the tool to default toolsets, and covered mapping,
validation/cap, request behavior, and tool-def exposure in tests. Remaining: add an `agent_eval`
scenario with a live/local backend to compare truncation/auto-continue rates.

---

## U2. Tool-result disk offload + `read_tool_result` ✅ first pass

**Goal.** Replace lossy truncation of large tool results with offload-to-disk + a head/tail preview
+ a retrieval handle, so nothing needed is permanently lost.

**Why.** Claude Code offloads oversized results to disk and keeps a ~2 KB preview + handle in
context (per-tool 50 KB, per-message 200 KB caps), run before every model call. TheMauler currently
truncates (`truncateToolResult`, ~`app.go:3628`; `MaxToolResultChars` default 12000,
`model.go:78`/`defaults.go:25`) — the dropped middle is unrecoverable. This also completes the
"lazy context retrieval beyond master skills" backlog item in `AGENTS.md`.

**Files.**
- New: `internal/app/tool_result_store.go` + `_test.go` — run-scoped blob store.
- New: `internal/tools/read_tool_result.go` + `_test.go` — retrieval tool.
- Touch: `internal/app/app.go` tool-result handling (~`:2955`–`2980`, where
  `summarizeShellResultForContext` / `MaxToolResultChars` is applied) to offload instead of hard-cut.
- Touch: `internal/settings/model.go` + `defaults.go` — add `ToolResultPreviewChars` (default 2000),
  keep `MaxToolResultChars` as the offload trigger; add a per-message aggregate cap field.

**Signature.**
```go
// SaveToolResult writes the full result to a run-scoped store and returns a stable id.
func (a *App) saveToolResult(runID string, full string) (id string)
// LoadToolResultSlice returns [offset, offset+limit) bytes/chars of a stored result.
func (a *App) loadToolResultSlice(runID, id string, offset, limit int) (string, bool)
```

**Algorithm.**
1. When a tool result length > `MaxToolResultChars`: write the full text to
   `~/.config/mauler/run-artifacts/<runID>/<id>.txt` (or the sessionstore), then replace the
   in-context content with: `head (preview/2) + "\n…[offloaded N chars, id=<id>; call read_tool_result to page]…\n" + tail (preview/2)`. Record id on the run for cleanup.
2. Track a per-message aggregate; if several results in one turn exceed the aggregate cap, offload
   the largest first until under budget.
3. `read_tool_result(result_id, offset?, limit?)` tool (read-only) returns the slice via
   `loadToolResultSlice`. Register it; add to read/safe toolsets.
4. Apply the same path in subagents (`subagents.go` already calls `truncateToolResult`).
5. On clean run exit, optionally GC the run-artifacts dir (keep until session end for resume).

**Tests.** `TestToolResultOffloadRoundTrip`, `TestOffloadPreviewHasHeadAndTail`,
`TestReadToolResultPaging`, `agent_eval` web/recon scenario asserting per-turn context drops with no
loss of a fact that lives in the offloaded middle (a follow-up `read_tool_result` recovers it).

**Acceptance.** A 60 KB result costs ~2 KB on its turn; `read_tool_result` returns the requested
slice; subagent path uses the same offload.

**Effort.** M.

**Done so far.** Added run-scoped offload storage under the Mauler config dir, context previews with
recoverable `result_id` handles, app-bound `read_tool_result`, default toolset/settings wiring,
Settings UI round-trip fields, main-loop and subagent offload usage, per-turn aggregate offload of
the largest raw tool results, and round-trip/preview/paging/aggregate tests. Remaining: add an
`agent_eval` scenario that proves a fact from the offloaded middle can be recovered.

---

## U3. InferenceBridge launch-flag + quant assertions in Doctor 🔨

**Goal.** Make the Doctor assert the backend launch configuration that is the *root cause* fix for
several failures the loop currently recovers from, and warn on an under-capable quant.

**Why.** `agent-loop-research-report.md` §6.4 + ISSUES R7/R8: without `--jinja` (or `use_jinja`),
Qwen emits `<tool_call>` XML / `</think>` as text; `--reasoning-format deepseek` aligns the parser;
**speculative decoding** rejections at `</think>` spike EOS probability → the early-termination the
loop defends against; Q4 sits below the tool-calling accuracy cliff vs Q5/Q6. Existing
`addLlamacppAgentFlagAdvisory` + `addModelTierCheck` (`doctor.go`) are the hook — extend them.

**Files.** Touch: `internal/app/doctor.go` (extend the advisory funcs), and wherever live process /
`/props` is read (`recordBackendRuntimeMismatch`).

**Algorithm.**
1. From `/props` (and the managed-bridge launch line if available), assert: `--jinja`/`use_jinja`,
   `--reasoning-format deepseek` (or equivalent), flash-attention on, and **speculative decoding
   off** (or warn if on while truncation/repetition telemetry is high — cross-ref R10 metrics).
2. Quant check: if the model name parses to Q4 or below for an agent profile, warn "prefer
   Q5_K_XL/Q6 for tool-calling accuracy" (extend `modelParamBillions` logic to read quant tag).
3. Surface each as a doctor finding with a one-line fix (so a user can paste the corrected launch).

**Tests.** Table-driven over recorded `/props` payloads: missing-jinja warns, specdec-on warns,
Q4 warns, fully-correct passes.

**Acceptance.** Doctor flags a misconfigured bridge with actionable fixes; a correct bridge passes.

**Effort.** S. (Highest ROI per the research — zero loop risk; shared with HelixClaw, see §U-shared.)

---

# Tier 2

## U4. Programmatic tool calling (`run_script`) 🔨

**Goal.** One tool that runs a short, sandboxed script which can call other (curated) tools and do
control flow inside a single inference turn — collapsing N round-trips into one.

**Why.** Hermes' `execute_code` / the CodeAct paradigm: "programmatic tool calling collapses
multi-step pipelines into single inference calls." On a local 3090 every extra turn is the
expensive, failure-prone part. TheMauler already has the artifact runner (`RunArtifact(lang, code)`,
~`app.go:1693`) and a clean tool registry — this is a thin, guarded layer on top.

**Files.**
- New: `internal/tools/run_script.go` + `_test.go`.
- Touch: `internal/app/app.go` — wire a script→registry bridge so the script's tool calls reuse the
  *real* registry path (guardrails, mutation verify, rollback, budgets, confirm gate).
- Touch: `defaults.go` toolsets (gate behind `local-code`/`unrestricted`, not `safe`).

**Design (start minimal + sandboxed).**
- Expose a tiny API to the script: `read(path)`, `glob(pat)`, `grep(pat, paths)`, `write(path, s)`,
  `sh(cmd)` — each call routes back through `tools.Registry.Run` so every existing guardrail,
  budget counter (`MaxToolCalls`, wall-clock), mutation verify, and confirm gate still applies.
- Language: one to start (Python via the artifact runner, or a Go-embedded JS like `goja`). Hard
  per-script timeout + step cap (e.g. ≤30 tool calls/script). Return combined stdout + a structured
  results array as one tool result (offloaded via U2 if large).
- Destructive: `run_script` is `Destructive()=true`; in non-autonomous mode it hits the confirm
  gate; in autonomous mode each inner mutating/`sh` call still respects per-tool permissions.

**Tests.** `TestRunScriptRoutesThroughRegistry`, `TestRunScriptRespectsBudgetAndTimeout`,
`TestRunScriptGuardrailBlocksSecretWrite`, `agent_eval` `read-grep-summarize` scenario finishing in
1–2 model turns vs N.

**Acceptance.** A multi-file read+grep+summarize completes in 1–2 turns; a script that tries a
blocked command or secret write is stopped by the same guardrails as a direct call.

**Effort.** M–L. Biggest token/latency payoff once U1+U2 land.

---

## U5. Graduated compaction ladder 🔨

**Goal.** Formalize a cheap→expensive compaction ladder so summarization is the last resort, and add
a microcompact tier that drops old thinking traces first.

**Why.** Claude Code runs a 5-layer ladder; no single strategy fits all pressure, and the cheap
layers spare the lossy summarize. TheMauler has 2 effective tiers (`ClearOldToolResults`,
`history.go:101`; `doCompact`, `app.go:3264`) + `backendUsagePressure`. Gap: a microcompact tier and
an explicit ordering, plus "keep critical rules in MAULER.md, not mid-history."

**Files.** Touch: `internal/agent/history.go` (add `DropOldReasoning(keepRecent int)` /
`MicroCompact`), `internal/app/app.go` compaction decision site (sequence the tiers by threshold).

**Ladder (apply in order, each gated by a rising threshold).**
1. **Offload** oversized tool results (U2) — already trims the biggest contributor.
2. **Snip** old tool results (`ClearOldToolResults`) — keep each assistant→tool *pair* intact (never
   orphan a tool result from its call).
3. **Microcompact** — `DropOldReasoning`: strip `<think>`/reasoning from turns older than keepRecent
   (cheap, lossless for the task; reasoning is already a separable field).
4. **Collapse** — protect last N turns verbatim, summarize the middle.
5. **Auto-summarize** — existing `doCompact` (last resort).
- After any tier fires, re-inject goal + todo state (R9 already does this on `contextDropped`) and
  ensure critical rules live in MAULER.md (system prompt), which survives all tiers.

**Tests.** `TestDropOldReasoningKeepsRecentAndPairs`, `TestCompactionLadderOrder` (asserts cheaper
tier fires before summarize given moderate pressure), `agent_eval` long-run scenario showing
`doCompact` fires fewer times than before.

**Acceptance.** Summarization fires materially less on a long run; tool-call pairs never orphaned;
post-compaction request still carries the objective + rules.

**Effort.** M.

---

## U6. Externalized `PROGRESS.md` resume artifact 🔨

**Goal.** A single workspace-scoped, human- and model-readable progress file the agent maintains, so
a fresh/cold context resumes long work with intent intact.

**Why.** Anthropic's long-running-agents guidance pairs compaction with externalized state
(`progress.txt` + init + git commit). TheMauler has the storage (R3 checkpoint/resume
`run_checkpoint.go:46`; milestone memory M1–M4; session FTS) but no single canonical "where am I"
artifact the model owns.

**Files.** New: `internal/app/progress_artifact.go` + `_test.go`. Touch: `runAgentLoop` (flush a
progress update before compaction / at milestones), `ResumeRun` (~`run_checkpoint.go:46`) and the
"Resume Last Run" helper (ISSUES M6) to seed from PROGRESS.md.

**Algorithm.**
1. Maintain `<workspace>/.mauler/PROGRESS.md` with sections: Objective, Decisions, Files touched,
   Verified, Next, Reusable commands. Update it from milestone events (reuse the M-series milestone
   detector) and on each compaction (persist-before-loss, like Hermes flushing memory first).
2. Expose a `progress_update` tool (or fold into the todo tool) so the model can append explicitly.
3. On resume: build the new context from PROGRESS.md + active todos + latest milestone memory rather
   than raw history.

**Tests.** `TestProgressArtifactRoundTrip`, `TestResumeSeedsFromProgress`, `agent_eval`
kill-and-resume scenario asserting correct objective + next step recovered without re-reading the
repo.

**Acceptance.** Kill a long run mid-task; cold restart resumes with correct objective/next step from
PROGRESS.md.

**Effort.** S–M.

---

# Tier 3

## U7. R7 grammar-constrained tool args — probe then ship 🧪

**Goal.** Force valid tool-call JSON at generation time for the single-tool/`required` case,
eliminating the malformed-args class — but only after the live probe confirms it's safe.

**Why / held.** Spec is in `agent-reliability-roadmap.md` R7; the `RunGrammarToolArgsProbe` Benchmark
action already exists. llama.cpp `json_schema` constrains message *content*, not the tool-call
envelope, so it can corrupt all tool calling if shipped blind.

**Plan.** Run the probe against the live InferenceBridge Qwen profile repeatedly. If it preserves a
valid `tool_calls` envelope, gate it exactly in `buildChatRequest`/`toolDefsAndChoiceForTurn`: when
`toolChoice=="required"` and `len(toolDefs)==1`, attach `JSONSchema = toolDefs[0].Function.Parameters`
behind a `Profile.GrammarToolArgs` flag (default off). Otherwise leave unset.

**Acceptance.** With the flag on + gated condition met, malformed-arg recoveries → ~0 in the eval
harness; flag-off path unchanged.

**Effort.** M (once a live server is on hand).

---

## U-shared. Cross-project (InferenceBridge) — see the cross-project plan

U1, U2, U3, U5, U6 are mirrored in HelixClaw
(`C:\Users\richa\Documents\HelixClaw\HELIXCLAW_AGENT_LOOP_UPGRADE.md`). Because both hit the same
InferenceBridge backend and the same local-model failure taxonomy, port fixes both ways and grade a
backend change on **both** loops with the same multi-step scenarios (TheMauler `agent_eval`,
HelixClaw Item 16 benchmark runner).

## Suggested order
1. **U1** (reasoning-effort) — smallest, highest ROI.
2. **U2** (tool-result offload) — kills lossy truncation; closes a backlog item.
3. **U3** (Doctor launch-flag/quant) — root cause, zero loop risk.
4. **U4** (programmatic tool calling) — biggest token/latency win after U1+U2.
5. **U5** (compaction ladder) → **U6** (PROGRESS.md).
6. **U7** (grammar args) once a live server validates the probe.
