# TheMauler - Agent-Loop Upgrade Roadmap (Hermes / Claude-Code-class patterns)

**Created:** 2026-06-29
**Owner doc:** live, implementable source of truth for the next agent-loop capability tier.
**Analysis / why:** [agent-loop-upgrade-plan-2026-06.md](agent-loop-upgrade-plan-2026-06.md) (cross-project),
building on the completed [agent-reliability-roadmap.md](agent-reliability-roadmap.md) (R1-R10) and
[agent-loop-research-report.md](agent-loop-research-report.md).
**Audience:** an engineer or AI agent picking this up cold. Every item is self-contained - files,
signatures, algorithm, wiring anchors, tests, and acceptance criteria are spelled out so you can
implement without re-deriving context.

## Current Status - 2026-07-08

Mauler-only next order:
1. Run one live stability smoke on the current build and inspect RunLedger for repeated terminal/tool loops, `session_repair`, `tool_cache_hit`, `loop_circuit_breaker`, malformed compact args, and false loop trips during legitimate polling/pagination.
2. Fix any remaining compact-tool argument sanitation found by that run: `shell.command` aliases, XML-ish path tags, and residual HTML entities before routing/logging.
3. Add or tune `agent_eval` cases for any failure classes that appear in the live run, especially same-result different-shell-input loops or period-2 tool ping-pong if they regress.
4. Start U17 verification-gate loop only after the live smoke is clean.

Implementation update 2026-07-08: outcome-aware loop detection, period-2/3 tool-cycle detection,
read-only cached tool-result coverage, and U16 every-turn message-structure repair are implemented
and locally verified. Production build output is `build\bin\TheMauler.exe`.

Do not start U22/U23 task-DAG or experience learning until U16/U20/U18/U19/U17/U21 are green. Do not add provider/backend work here; the live path is TheMauler -> InferenceBridge OpenAI-compatible API.

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
- [spec] **Spec ready** - not started; full spec below.
- [test] **Held** - needs a live backend to validate before enabling.

## Item map

| ID | Title | Tier | Effort | Status |
|----|-------|------|--------|--------|
| U1 | Dynamic reasoning-effort control (`set_reasoning_effort` tool) | 1 | S | [done] first pass |
| U2 | Tool-result disk offload + `read_tool_result` (replace blind truncation) | 1 | M | [done] first pass |
| U3 | InferenceBridge launch-flag + quant assertions in Doctor | 1 | S | [done] first pass |
| U4 | Programmatic tool calling (`run_script` over the registry) | 2 | M-L | [done] first pass |
| U5 | Graduated compaction ladder (add microcompact tier) | 2 | M | [done] first pass |
| U6 | Externalized `PROGRESS.md` resume artifact | 2 | S-M | [done] first pass |
| U7 | R7 grammar-constrained tool args - run the live probe, then ship gated | 3 | M | [test] |
| U16 | Every-turn message-structure repair (port HelixClaw `session_repair`) | 4 | S-M | [done] first pass |
| U17 | Verification-gate loop (build/test/lint gates that block completion) | 4 | M | [spec] |
| U18 | Structural write-guards (protected paths, patch-size, file-count caps) | 4 | S-M | [spec] |
| U19 | Role-scoped tool sets (planner / executor / reviewer allowlists) | 4 | M | [spec] |
| U20 | Tool permission classes on `ToolSpec` (ReadOnly/Edit/Execute/Network/Delegate) | 4 | S | [spec] |
| U21 | Plan->review completion rails (spec-coverage + deliverable-exists) | 4 | M | [spec] |
| U22 | Experience / tool-sequence learning loop | 5 | L | [spec] |
| U23 | Task-DAG dispatcher (port HelixClaw `helix_graph`) | 5 | L | [spec] |

> **U16-U23 are HelixClaw-parity ports.** They close the architectural gap catalogued in
> [helixclaw-parity-comparison-2026-07.md](helixclaw-parity-comparison-2026-07.md). HelixClaw is a
> hierarchical multi-agent OS (CEO -> supervisor actor -> typed workers, each with an explicit
> planner->executor->observer cycle); TheMauler is a single hardened loop. These items backport the
> reliability and correctness machinery that makes HelixClaw's agents land tasks more often, in the
> order that gives the most quality per unit effort. HelixClaw source lives under
> `C:\Users\richa\Documents\HelixClaw\crates\helixclaw-agents\src\`.

---

# Reliability Spine

These items are the current highest-leverage reliability work. The goal is not more features; it is
a stricter execution spine from model decision -> tool selection -> command/session execution ->
result capture -> fact storage -> next model packet.

## U8. Deterministic tool/session state machine - first pass

**Goal.** Make tool execution boring and explicit. The model should not infer from log text whether
it should use `shell`, `terminal_run`, `terminal_read`, `terminal_send`, webshell execution, Windows,
WSL/Kali, or a background job.

**State model.**
- Shared terminal/session states: `missing`, `ready`, `running`, `listener`, `interactive_prompt`,
  `busy`, `closed`, and later `connected`, `dead`.
- Reverse-shell/listener states: `idle`, `listening`, `connected`, `busy`, `ready`, `dead`.
- Tool routing states should produce one deterministic recommendation, not prose-only advice.

**Algorithm.**
1. Centralize execution advice in a state-machine helper, starting with `terminal_run`.
2. If the shared terminal is `running`, return a tool result recommending `terminal_read`.
3. If it is `listener`, do not type over it. Recommend `terminal_read`/`terminal_send`, or separate
   `shell`/webshell execution for a reverse-shell trigger.
4. If it is `interactive_prompt`, recommend `terminal_send`.
5. If it is `busy`, recommend one `terminal_read` or Recover.
6. Long-running one-shot commands should become jobs automatically rather than blocking on timers.

**Files.**
- Started: `internal/app/tool_execution_state.go`.
- Touch next: `internal/app/interactive_terminal_tool.go`, shell/shared-terminal dispatch, Doctor,
  Run UI.

**Done so far.** `terminal_run`, `terminal_send`, `start_listener`, and shell one-shots now use the
state machine. `terminal_run`/`start_listener` refuse to type into `running`, `listener`,
`connected`, `interactive_prompt`, or unclear `busy` states and return the recommended next tool.
`terminal_send` routes idle new commands to `terminal_run`, allows prompt/live-session input, allows
Ctrl-C/Ctrl-D recovery, and blocks arbitrary sends into running/busy terminals. Shell one-shots
route connected-session commands to `terminal_send`, while allowing reverse-shell triggers as the
separate path when a listener is active. Obvious long-running shell commands are auto-backgrounded
into jobs. Shared-terminal state distinguishes `listener` from `connected`, and the UI badge shows
`connected`. Session state is now persisted as first-class `AgentSession` records and mirrored to
RunLedger `session_state` events when listeners start or terminal reads/runs detect listener,
connected, busy, ready, or dead states. Tests cover the decision tables, auto-backgrounding, state
detection, and session-registry transitions.

**Acceptance.** The agent can no longer clobber a listener or stuck prompt with a new `terminal_run`;
it gets an actionable next tool and continues without asking the user.

## U9. Live Run Facts / evidence ledger prompt packet - first pass

**Goal.** Promote pinned evidence from passive ledger events into a first-class facts layer used by
Run, Brain, recovery, compaction, and prompt construction.

**Facts to track.**
- current target IP and hostname
- working webshell URL
- current shell user/privilege
- working credentials, redacted where needed
- found paths and artifact files
- failed paths/attempts not to repeat
- active listener/session
- latest useful artifact
- next known step

**Rules.**
- Facts must carry source/evidence pointers, confidence, freshness, and conflict state.
- Prompt packets should include only compact current facts, not raw tool output.
- Compaction recovery should use facts before chat transcript replay.

**Done so far.** First pass adds `internal/app/run_facts.go`: recent `evidence_pin` ledger events
and artifact/file events are condensed into a capped system-prompt packet with target, hostname,
webshell, shell-user, credential, artifact/file, failed-attempt, and evidence categories. Tests cover
dedupe, caps, and prompt content.

## U10. Critical-action verifier loop - first pass

**Goal.** Stop false success/failure claims by forcing proper evidence-backed verification after
critical actions.

**Verifier rules.**
- Webshell works -> verify `id` and `pwd`.
- Target moved/DNS mismatch -> verify `/etc/hosts`, resolution, TCP reachability, and one service
  probe before declaring reboot/VPN failure.
- Root/user shell -> verify `id`, hostname, cwd, and expected flag/report path where applicable.
- Writeup/report updated -> verify the target file changed.
- Exploit/path failed -> verify whether the failure is network, URL/path, auth, payload, or local
  tool invocation before summarizing.

**Acceptance.** Important claims in final assistant output reference verified evidence, not a single
ambiguous tool result.

**Done so far.** `internal/app/critical_verifier.go`: execution-tool results get compact
`verifier_required` hints for working webshells, listener callbacks, shell/root identity,
file-change claims, and target-route/DNS ambiguity. The loop now appends a verifier gate system
message after such results, requiring the next action to capture command plus result evidence before
final success/failure claims. The verifier wording deliberately avoids cheap/pro-forma checks:
webshells require exact URL plus `id`/`pwd`/`hostname`, listeners require listener state plus
callback/session proof, shells require `id`/`hostname`/`pwd` and expected evidence paths, target
route claims require hosts/DNS plus TCP/service proof, and file changes require reading the changed
content back.

## U11. Phase-specific run tool routing - first pass

**Goal.** Make normal agent runs advertise only the tools for the current phase, not every maybe-useful tool.

**Phase toolsets.**
- `recon`: `terminal_run`, `http_probe`, `grep`, `read_file`, `progress`
- `webshell`: `http_probe`, `terminal_run`, `read_tool_result`, `evidence_pin`/facts
- `reverse_shell`: `start_listener`, `terminal_read`, `terminal_send`, `terminal_run`
- `report/writeup`: file tools, evidence bundle, no shell unless needed

**Acceptance.** Tool schemas stay small, Qwen sees a narrow action menu, and phase changes are
tracked explicitly in logs/Run.

**Done so far.** First pass adds `opsPhaseFromText` and `addOpsToolsForPhase` in
`internal/app/tool_router.go`, with tests for recon, webshell, reverse-shell, and report/writeup
toolsets. Each turn now emits deduped RunLedger `tool_routing` events with phase, tool choice, and
actual model-facing tool count so Run/Logs can show when the router widened or narrowed the action
menu. The main loop now uses terminal-state-aware routing: connected sessions bias to
`terminal_send`/`terminal_read`, listener terminals stop advertising another listener, and
running/busy terminals stop advertising `terminal_run`. Run routing also has a soft model-facing
tool budget: shell-centric run prompts that widen too far are trimmed back to the phase toolset
unless they explicitly need browser/research tools, and over-budget cases emit
`tool_routing_warning` ledger events for Brain/Run visibility.

## U12. Run cockpit with clickable evidence cards - first pass

**Goal.** Replace text-heavy run confusion with a cockpit showing what is true now.

**Cards.**
- Target: confirmed / stale / mismatch
- Access: none / webshell / user shell / root shell
- Current blocker
- Active command/session
- Latest useful artifact
- Next action
- Failed attempts not to repeat
- Evidence behind each claim, with click/open actions

**Done so far.** First pass: the Run page reads recent ledger events and renders evidence-backed facts
for target, webshell, access, artifacts/files, failed attempts, session state, tool-routing
phase/tool-count, and routing warnings alongside lab/memory facts. Chat tool-result cards also show
clickable `result_id` handles that prime a `read_tool_result` follow-up, so large offloaded outputs
are visible without copy/paste handle hunting.

## U14. Request-scoped execution state packet - first pass

**Goal.** Stop making the model infer current truth from transcript flow. Every model request gets a
small, fresh packet with route phase, tool count, terminal state, active sessions, and recent ledger
facts. It is not appended to stored chat history, so it helps routing without bloating compaction.

**Done so far.** Added `buildExecutionStatePrompt`: per-turn request-only system packet from
RunLedger facts plus `AgentSession` state. Tests cover routing, session, target, and webshell facts.

## U15. Structured execution result contracts - first pass

**Goal.** Tool results should tell the model what happened and what to do next in predictable fields,
not prose-only blobs.

**Done so far.** `terminal_run`, `terminal_read`, `start_listener`, and blocked state-machine results
now include a compact `contract` block with `state`, `next_tool`, `evidence`, and `do_not_repeat`.
Verifier rules now cover listener, connected session, file-write, webshell, route, user, and root
claims. `run_script` now emits structured contracts as well, including inner tool counts, timeouts,
last/failed tool, stable error fields, and next-tool guidance on success, timeout, read-error,
wait-error, and inner-tool failure paths.

## U13. Reliability benchmark suite - first pass

**Goal.** Measure reliability, not just model speed.

**Metrics.**
- actual loaded context: 40k vs 65k
- TTFT and inter-token latency
- prompt/tool budget
- tool-call success rate
- repeated-command rate
- false-done rate
- verifier pass/fail rate
- Q4_K_S vs stronger quant if a second quant is available

**Acceptance.** Benchmarks can show whether a change made the agent more reliable before a long HTB
run burns 30 minutes.

**Done so far.** Loop-health metrics now include a rough stability score plus repeated input count,
repeated skip count, verifier pressure, routing transitions, max routed tool count, prompt warnings,
and tool errors. Logs render a Run Stability telemetry card from `loop_metrics` so regressions are
visible without reading raw JSON. Agent Eval / mini-loop benchmarks now surface reliability counters:
tool success rate, repeated-tool rate, verifier prompts, max routed tools, prompt warnings, stability
score, and false-done detection when a run reports `done` but expected artifacts/evidence are missing.
The Benchmark page and CSV export show these values, so model/tool-router changes can be compared
without reading raw run logs.

---

# Tier 1

## U1. Dynamic reasoning-effort control [done] first pass  * do first

**Goal.** Let the agent scale its own thinking depth per step via a state-modifying tool, and let
the auto-router set a sensible default per mode. Directly attacks the dominant local-Qwen failure
surface (truncated `<think>`), which the loop currently only *recovers* from via `forceNoThink`.

**Why.** Hermes exposes `reasoning_effort` as a first-class tool because agents "cannot reliably use
slash commands as structured state transitions." Matching depth to task (minimal for rote
edits/reads, high for hard reasoning) cuts truncation/auto-continue churn and tokens. Today thinking
is a coarse global per-profile toggle plus the blunt `forceNoThink` after N tool calls
(`app.go:2360`).

**Files.**
- Touch: `internal/llm/client.go` (`Request` struct, ~`:88`) - add `ReasoningEffort string`.
- Touch: `internal/app/app.go` - `buildChatRequest` (~`:5096`), `runAgentLoop` loop head (effort
  state + per-turn override), tool-exec dispatch (intercept the new tool), `buildSystemPrompt`
  (~`:6749`, add the nudge).
- Touch: `internal/app/agent_modes.go` - set a default effort per `AgentMode`.
- New: `internal/app/reasoning_effort.go` + `_test.go` (the effort->params mapping helper).

**Data / mapping.**
```go
// effortToThinking maps an effort tier to thinking + budget knobs, layered on top
// of the profile's active sampler family. minimal/low => thinking off or tight budget.
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

## U2. Tool-result disk offload + `read_tool_result` [done] first pass

**Goal.** Replace lossy truncation of large tool results with offload-to-disk + a head/tail preview
+ a retrieval handle, so nothing needed is permanently lost.

**Why.** Claude Code offloads oversized results to disk and keeps a ~2 KB preview + handle in
context (per-tool 50 KB, per-message 200 KB caps), run before every model call. TheMauler currently
truncates (`truncateToolResult`, ~`app.go:3628`; `MaxToolResultChars` default 12000,
`model.go:78`/`defaults.go:25`) - the dropped middle is unrecoverable. This also completes the
"lazy context retrieval beyond master skills" backlog item in `AGENTS.md`.

**Files.**
- New: `internal/app/tool_result_store.go` + `_test.go` - run-scoped blob store.
- New: `internal/tools/read_tool_result.go` + `_test.go` - retrieval tool.
- Touch: `internal/app/app.go` tool-result handling (~`:2955`-`2980`, where
  `summarizeShellResultForContext` / `MaxToolResultChars` is applied) to offload instead of hard-cut.
- Touch: `internal/settings/model.go` + `defaults.go` - add `ToolResultPreviewChars` (default 2000),
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
   in-context content with: `head (preview/2) + "\n...[offloaded N chars, id=<id>; call read_tool_result to page]...\n" + tail (preview/2)`. Record id on the run for cleanup.
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
the largest raw tool results, and round-trip/preview/paging/aggregate tests. Repeated exact read and
shell calls now return a structured `[cached_tool_result]` contract with `next_tool`,
`do_not_repeat`, cache key, and extracted `result_id` when the cached output was offloaded, so the
model can use `read_tool_result` instead of rerunning the same command. Remaining: add an
`agent_eval` scenario that proves a fact from the offloaded middle can be recovered.

---

## U3. InferenceBridge launch-flag + quant assertions in Doctor [done] first pass

**Goal.** Make the Doctor assert the backend launch configuration that is the *root cause* fix for
several failures the loop currently recovers from, and warn on an under-capable quant.

**Why.** `agent-loop-research-report.md` section 6.4 + ISSUES R7/R8: without `--jinja` (or `use_jinja`),
Qwen emits `<tool_call>` XML / `</think>` as text; `--reasoning-format deepseek` aligns the parser;
**speculative decoding** rejections at `</think>` spike EOS probability -> the early-termination the
loop defends against; very low quants sit below the tool-calling accuracy cliff, while Q4_K_S should
be benchmarked against the project default UD-Q4_K_XL on the RTX 3090. Existing
`addLlamacppAgentFlagAdvisory` + `addModelTierCheck` (`doctor.go`) are the hook - extend them.

**Files.** Touch: `internal/app/doctor.go` (extend the advisory funcs), and wherever live process /
`/props` is read (`recordBackendRuntimeMismatch`).

**Algorithm.**
1. From `/props` (and the managed-bridge launch line if available), assert: `--jinja`/`use_jinja`,
   `--reasoning-format deepseek` (or equivalent), flash-attention on, and **speculative decoding
   off** (or warn if on while truncation/repetition telemetry is high - cross-ref R10 metrics).
2. Quant check: parse the model name for known GGUF quant tags. Warn on sub-Q4, call out Q4_K_S as
   a benchmark/watch item, and keep UD-Q4_K_XL as the 24 GB RTX 3090 default. Do not recommend Q6_K
   for Qwen3.6-class 32K profiles on this machine.
3. Surface each as a doctor finding with a one-line fix (so a user can paste the corrected launch).

**Tests.** Table-driven over recorded `/props` payloads: missing-jinja warns, specdec-on warns,
Q4 warns, fully-correct passes.

**Acceptance.** Doctor flags a misconfigured bridge with actionable fixes; a correct bridge passes.

**Effort.** S. (Highest ROI per the research - zero loop risk; shared with HelixClaw, see section U-shared.)

**Done so far.** Doctor now reads `/props` for launch/config signals, reports Jinja, Qwen
reasoning-format, flash-attention, and speculative/draft decoding state, and parses GGUF quant tags
for reliability guidance. Tests cover launch-signal parsing, disabled speculative settings, quant
tag parsing, and speculative warnings. Remaining: enrich this with InferenceBridge's managed launch
line if/when that endpoint exposes exact argv.

---

# Tier 2

## U4. Programmatic tool calling (`run_script`) [done] first pass

**Goal.** One tool that runs a short, sandboxed script which can call other (curated) tools and do
control flow inside a single inference turn - collapsing N round-trips into one.

**Why.** Hermes' `execute_code` / the CodeAct paradigm: "programmatic tool calling collapses
multi-step pipelines into single inference calls." On a local 3090 every extra turn is the
expensive, failure-prone part. TheMauler already has the artifact runner (`RunArtifact(lang, code)`,
~`app.go:1693`) and a clean tool registry - this is a thin, guarded layer on top.

**Files.**
- New: `internal/tools/run_script.go` + `_test.go`.
- Touch: `internal/app/app.go` - wire a script->registry bridge so the script's tool calls reuse the
  *real* registry path (guardrails, mutation verify, rollback, budgets, confirm gate).
- Touch: `defaults.go` toolsets (gate behind `local-code`/`unrestricted`, not `safe`).

**Design (start minimal + sandboxed).**
- Expose a tiny API to the script: `read(path)`, `glob(pat)`, `grep(pat, paths)`, `write(path, s)`,
  `sh(cmd)` - each call routes back through `tools.Registry.Run` so every existing guardrail,
  budget counter (`MaxToolCalls`, wall-clock), mutation verify, and confirm gate still applies.
- Language: one to start (Python via the artifact runner, or a Go-embedded JS like `goja`). Hard
  per-script timeout + step cap (e.g. <=30 tool calls/script). Return combined stdout + a structured
  results array as one tool result (offloaded via U2 if large).
- Destructive: `run_script` is `Destructive()=true`; in non-autonomous mode it hits the confirm
  gate; in autonomous mode each inner mutating/`sh` call still respects per-tool permissions.

**Tests.** `TestRunScriptRoutesThroughRegistry`, `TestRunScriptRespectsBudgetAndTimeout`,
`TestRunScriptGuardrailBlocksSecretWrite`, `agent_eval` `read-grep-summarize` scenario finishing in
1-2 model turns vs N.

**Acceptance.** A multi-file read+grep+summarize completes in 1-2 turns; a script that tries a
blocked command or secret write is stopped by the same guardrails as a direct call.

**Effort.** M-L. Biggest token/latency payoff once U1+U2 land.

**Done so far.** `run_script` is wired as a Go-native orchestration tool that can call curated
Mauler tools through the registry path with budgets, cancellation, ledgering, tool-result offload,
and structured result contracts. It exposes helpers for read/search/file/shell/web/memory/todo and
`read_tool_result`, tracks inner-tool counts and failures, and returns `next_tool` /
`do_not_repeat` guidance on success, timeout, and inner-tool errors. Remaining: tighten the helper
surface as the target registry collapses tool families, then add an agent-eval scenario proving
multi-step workflows use fewer model turns without losing evidence.

---

## U5. Graduated compaction ladder [done] first pass

**Goal.** Formalize a cheap->expensive compaction ladder so summarization is the last resort, and add
a microcompact tier that drops old thinking traces first.

**Why.** Claude Code runs a 5-layer ladder; no single strategy fits all pressure, and the cheap
layers spare the lossy summarize. TheMauler has 2 effective tiers (`ClearOldToolResults`,
`history.go:101`; `doCompact`, `app.go:3264`) + `backendUsagePressure`. Gap: a microcompact tier and
an explicit ordering, plus "keep critical rules in MAULER.md, not mid-history."

**Files.** Touch: `internal/agent/history.go` (add `DropOldReasoning(keepRecent int)` /
`MicroCompact`), `internal/app/app.go` compaction decision site (sequence the tiers by threshold).

**Ladder (apply in order, each gated by a rising threshold).**
1. **Offload** oversized tool results (U2) - already trims the biggest contributor.
2. **Snip** old tool results (`ClearOldToolResults`) - keep each assistant->tool *pair* intact (never
   orphan a tool result from its call).
3. **Microcompact** - `DropOldReasoning`: strip `<think>`/reasoning from turns older than keepRecent
   (cheap, lossless for the task; reasoning is already a separable field).
4. **Collapse** - protect last N turns verbatim, summarize the middle.
5. **Auto-summarize** - existing `doCompact` (last resort).
- After any tier fires, re-inject goal + todo state (R9 already does this on `contextDropped`) and
  ensure critical rules live in MAULER.md (system prompt), which survives all tiers.

**Tests.** `TestDropOldReasoningKeepsRecentAndPairs`, `TestCompactionLadderOrder` (asserts cheaper
tier fires before summarize given moderate pressure), `agent_eval` long-run scenario showing
`doCompact` fires fewer times than before.

**Acceptance.** Summarization fires materially less on a long run; tool-call pairs never orphaned;
post-compaction request still carries the objective + rules.

**Effort.** M.

**Done so far.** Added tool-result offload (U2), old tool-result clearing, compact-evidence
preservation, `MicrocompactThinking`, a `context_ladder` event path, preflight overflow handling,
and progress/goal reminders after context loss. Remaining: add an agent-eval long-run scenario that
proves summarization fires less often than the old two-tier path.

---

## U6. Externalized `PROGRESS.md` resume artifact [done] first pass

**Goal.** A single workspace-scoped, human- and model-readable progress file the agent maintains, so
a fresh/cold context resumes long work with intent intact.

**Why.** Anthropic's long-running-agents guidance pairs compaction with externalized state
(`progress.txt` + init + git commit). TheMauler has the storage (R3 checkpoint/resume
`run_checkpoint.go:46`; milestone memory M1-M4; session FTS) but no single canonical "where am I"
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

**Effort.** S-M.

**Done so far.** Added the `progress` tool backed by workspace `.mauler/progress.md`, append hooks on
context drop and run finish, `run_script` helper access, ledger `progress_update` events, and prompt
injection of the compact progress artifact at run start. Remaining: wire `ResumeRun`/Resume Last Run
to explicitly show the progress packet it used and add a kill/resume eval.

---

# Tier 3

## U7. R7 grammar-constrained tool args - probe then ship [test]

**Goal.** Force valid tool-call JSON at generation time for the single-tool/`required` case,
eliminating the malformed-args class - but only after the live probe confirms it's safe.

**Why / held.** Spec is in `agent-reliability-roadmap.md` R7; the `RunGrammarToolArgsProbe` Benchmark
action already exists. llama.cpp `json_schema` constrains message *content*, not the tool-call
envelope, so it can corrupt all tool calling if shipped blind.

**Plan.** Run the probe against the live InferenceBridge Qwen profile repeatedly. If it preserves a
valid `tool_calls` envelope, gate it exactly in `buildChatRequest`/`toolDefsAndChoiceForTurn`: when
`toolChoice=="required"` and `len(toolDefs)==1`, attach `JSONSchema = toolDefs[0].Function.Parameters`
behind a `Profile.GrammarToolArgs` flag (default off). Otherwise leave unset.

**Acceptance.** With the flag on + gated condition met, malformed-arg recoveries -> ~0 in the eval
harness; flag-off path unchanged.

**Effort.** M (once a live server is on hand).

---

# Tier 4 - HelixClaw Parity Ports

These backport the reliability/correctness spine that HelixClaw already ships. Full architectural
comparison and rationale: [helixclaw-parity-comparison-2026-07.md](helixclaw-parity-comparison-2026-07.md).
Grade each on both loops with the same multi-step scenarios (see section U-shared).

## U16. Every-turn message-structure repair [done] first pass

**Goal.** Sanitize the *message history structure* before every model call, not just during
compaction, so a malformed transcript can never wedge the loop or a local model.

**Implementation update 2026-07-08.** `internal/agent/history.go` now exposes
`RepairStructure() []RepairAction` / `RepairMessages(...)` covering the seven repair phases, and
`internal/app/app.go` calls it before each model request while logging `session_repair` events.
The 2026-07-08 pass split dangling trailing assistant tool calls into explicit phase 7
`strip_trailing_tool_call` actions and added no-op clean-history coverage.

**Why.** HelixClaw's `session_repair.rs` runs a 7-phase repair on every packet and is a large part
of why its agents "just work" on local models: it strips invalid roles, drops a leading assistant
turn, merges consecutive user messages, strips assistant tool-calls that have no matching results,
removes orphaned tool-results, fills empty content with a placeholder, and strips trailing tool-calls.
TheMauler only does a subset (`sanitizeCompactedMessages`, `history.go:343` - orphaned tool-result +
trailing tool-call handling) and only during compaction. The other phases (leading assistant,
consecutive-user merge, invalid role, empty-content fill) are missing, and none run on the normal
per-turn path.

**Reference.** `helixclaw-agents/src/session_repair.rs` - `pub fn repair(messages) -> Vec<RepairAction>`
plus `is_repair_placeholder`; its tests enumerate all 7 phases and the multiple-consecutive-tool-results
allowance.

**Files.**
- Touch: `internal/agent/history.go` - generalize `sanitizeCompactedMessages` into
  `RepairStructure() []RepairAction` covering all phases; keep the existing behavior as phases 4/5/7.
- Touch: `internal/app/app.go` - call it in `runAgentLoop` just before `buildChatRequest`
  (idempotent; no-op when history is already clean).
- New test data: extend `history_test.go`.

**Algorithm (phases, in order, idempotent).**
1. Drop messages with a role outside {system, user, assistant, tool}.
2. Drop a leading assistant message (no user turn to answer).
3. Merge consecutive user messages into one (join with `\n`).
4. Strip assistant `ToolCalls` that have no following tool result (reuse existing logic).
5. Drop tool messages whose `ToolCallID` matches no pending assistant call (reuse existing logic).
6. Fill empty assistant/user content with a placeholder (`is_repair_placeholder`-recognizable) so
   backends that reject empty content don't 400.
7. Strip trailing assistant tool-calls with no result yet (reuse existing logic).
- Return a `[]RepairAction` list; emit a `session_repair` RunLedger event when any action fires so
  Logs/Brain can see how often the transcript was malformed (a model/quant health signal).

**Tests.** Port HelixClaw's phase tests: `TestRepairRemovesInvalidRole`,
`TestRepairDropsLeadingAssistant`, `TestRepairMergesConsecutiveUser`,
`TestRepairStripsToolCallsWithoutResults`, `TestRepairRemovesOrphanedToolResult`,
`TestRepairFillsEmptyContent`, `TestRepairStripsTrailingToolCalls`, `TestRepairNoopOnCleanHistory`,
`TestRepairAllowsMultipleConsecutiveToolResults`.

**Acceptance.** A synthetic malformed transcript (leading assistant + orphaned tool result + empty
content) is repaired to a valid packet with no backend 400; a clean transcript is unchanged and
returns zero actions.

**Verified.** `go test ./internal/agent`, `go test ./internal/app/...`, `go test ./...`,
`go vet ./...`, `npm run --prefix frontend build`, and `.\build.ps1 -SkipTests` passed on
2026-07-08.

## U17. Verification-gate loop [spec]

**Goal.** Gate task completion on real build/test/lint evidence with a structured verdict, so the
agent cannot declare success on code that doesn't compile or pass.

**Why.** HelixClaw runs `verification::run_verification_gates` (build/clippy/test gates) and a
`verify_loop` that parses a `GateVerdictEnvelope` and carries `VerifierImprovement` items with a
**blocking** severity (`is_blocking`) that *prevents completion* and feeds the exact failures back to
the model. TheMauler verifies individual mutations (`mutation_verifier.go` - per-write lint) and
appends verifier hints (`critical_verifier.go`), but never gates the whole task on "does the project
build / do tests pass." This is the difference between "the file changed" and "the change works."

**Reference.** `helixclaw-agents/src/verify_loop.rs` (`ImprovementSeverity::is_blocking`,
`GateVerdictEnvelope`, `parse_gate_verdict`, `into_enriched_output`) and
`helixclaw-agents/src/verification.rs` (`run_verification_gates`, `run_cargo_gate`).

**Files.**
- New: `internal/app/verify_gate.go` + `_test.go`.
- Touch: `internal/app/app.go` - before a `done`/final assistant turn on a coding task, run the gate;
  if it blocks, inject the failures as a system message and continue the loop instead of finishing.
- Reuse: `mutation_verifier.go` stays the per-file layer; this is the whole-task layer.

**Design.**
- Gate command set is project-type-detected (Go: `go build ./...`, `go vet ./...`, `go test ./...`;
  generic: a configurable command list). Do not hardcode cargo - that is HelixClaw's stack.
- Parse each gate into a `GateResult{name, passed, summary, output}` and a `VerifyVerdict{status:
  pass|fail|error, blocking bool, improvements []string}`. Truncate/offload large gate output via U2.
- Only gate when the run actually mutated files (track via the existing rollback/mutation signal) and
  only in Builder/Fixer modes - never gate a pure research/recon run.
- Cap gate re-runs per task (e.g. 3) to avoid an infinite fix->verify->fix loop; after the cap, surface
  the residual failures in the final output honestly rather than looping.

**Tests.** `TestVerifyGateParsesPassFail`, `TestVerifyGateBlocksCompletionOnFailure`,
`TestVerifyGateSkipsNonCodingRun`, `TestVerifyGateReRunCap`, `agent_eval` scenario: a task that writes
code with a compile error must not report `done` until the error is fixed.

**Acceptance.** A Builder run that leaves the tree non-compiling loops back with the exact errors
instead of declaring success; a passing run finishes normally; a research run is never gated.

**Effort.** M.

## U18. Structural write-guards [spec]

**Goal.** Hard limits on what the agent can mutate in one run: protected paths, max patch size, max
file count - enforced at the tool boundary, not by prompt.

**Why.** HelixClaw's `guard.rs` has `GuardConfig` (protected-path denylist, `check_patch_size`,
`check_file_count`) plus a `GuardTracker` that accumulates modifications across the run and refuses
once caps are hit. TheMauler's `guardrails.go` only redacts secrets and labels injection in tool
*output* - it cannot stop the model from rewriting 40 files or touching a protected path. Its rollback
(`internal/agent/rollback.go`) is in-memory only and dies with the process.

**Reference.** `helixclaw-agents/src/guard.rs` - `GuardConfig::{check_protected_path, check_patch_size,
check_file_count}`, `GuardViolation`, `GuardTracker::record_modification`, `normalize_path`.

**Files.**
- New: `internal/app/write_guard.go` + `_test.go` (config load + per-run tracker).
- Touch: `write_file` / `edit_file` dispatch in `internal/app/app.go` (the registry-run path) to
  consult the guard before executing and return a `guard_violation` contract on refusal.
- Config: add a `.mauler/guard.toml` (or a `settings.Settings` block) - protected globs, max patch
  lines, max files/run; sensible defaults (deny `.git/`, secrets, the guard config itself).

**Algorithm.**
1. Load `GuardConfig` from repo/workspace at run start (defaults if absent).
2. On each mutating tool call: `normalizePath`, check protected globs -> refuse with the reason; check
   accumulated file count and this patch's line count against caps -> refuse when exceeded.
3. Track modifications in a run-scoped `GuardTracker`; expose the count in loop metrics / Run cockpit.
4. Refusals return a structured contract (`state=guard_violation, next_tool, do_not_repeat`) so the
   model adapts instead of retrying.

**Tests.** Port `guard.rs` tests: `TestGuardProtectedDirectory`, `TestGuardProtectedExactFile`,
`TestGuardPatchSizeLimit`, `TestGuardFileCountLimit`, `TestGuardTrackerAccumulates`,
`TestGuardNormalizeBackslashes` (Windows paths).

**Acceptance.** An attempt to write into a protected path or exceed the file/patch caps is refused with
an actionable contract; normal edits within caps pass unchanged.

**Effort.** S-M. High safety value, low loop risk.

## U19. Role-scoped tool sets [spec]

**Goal.** Give planner / executor / reviewer phases *different tool allowlists*, so a planning or
review phase structurally cannot edit files and a reviewer cannot run destructive commands.

**Why.** HelixClaw's `claude/executor.rs` exposes `planner_tools()`, `executor_tools()`,
`reviewer_tools()` - the role bounds capability, not just the prompt. TheMauler swaps a *persona*
(`agent_modes.go`) but keeps the same tool set, so a "Reviewer" run still has write/shell tools and can
drift into editing. This composes with U20 (permission classes) and the existing phase router
(`tool_router.go`, U11).

**Files.**
- Touch: `internal/app/agent_modes.go` - attach an allowed-tool-class set (or explicit allowlist) to
  each `AgentMode`.
- Touch: `internal/app/tool_router.go` - intersect the phase toolset (U11) with the mode's role
  allowlist when building `toolDefsAndChoiceForTurn`.
- Depends on: U20 for the class taxonomy (or use explicit tool-name lists in the interim).

**Design.**
- Planner/Reviewer/Researcher -> ReadOnly + Network + Delegate classes (no Edit/Execute).
- Builder/Fixer/Ops -> full set (Edit + Execute) as today.
- Keep it a *narrowing* on top of the existing enabled/allowed gates; never widen beyond what the user
  enabled.

**Tests.** `TestReviewerModeHasNoWriteTools`, `TestPlannerModeHasNoExecuteTools`,
`TestBuilderModeUnchanged`, `agent_eval` review scenario asserting zero mutations occur.

**Acceptance.** A Reviewer/Planner run is offered no Edit/Execute tools; Builder/Fixer unchanged.

**Effort.** M.

## U20. Tool permission classes on `ToolSpec` [spec]

**Goal.** Tag every tool with a permission class so gating (U18/U19), confirm prompts, and the UI can
reason about capability uniformly instead of the current binary `Destructive()`.

**Why.** HelixClaw's `runtime_tool_permission_class` maps every tool to
`ReadOnly | Edit | Execute | Network | Delegate`. TheMauler only has `Destructive() bool`
(`internal/tools/registry.go:25`), which can't distinguish "reads the network" from "runs code" from
"edits files." A class taxonomy is the clean substrate for role-scoping and phase routing.

**Reference.** `helixclaw-agents/src/tools.rs` - `runtime_tool_permission_class`,
`ToolPermissionClass`.

**Files.**
- Touch: `internal/tools/registry.go` - add `PermissionClass` to `ToolSpec` and a
  `Class() ToolClass` method (default derived from `Destructive()` for back-compat).
- Touch: each tool (or a central classifier func like HelixClaw's) to assign classes.
- Touch: confirm-gate + `tool_router.go` to read the class.

**Design.** `type ToolClass int` with `ReadOnly, Edit, Execute, Network, Delegate`. Keep
`Destructive()` working (Edit/Execute => destructive). Additive; no behavior change until U18/U19
consume it.

**Tests.** `TestToolClassAssignments` (table over the registry), `TestDestructiveDerivedFromClass`.

**Acceptance.** Every registered tool reports a class; `Destructive()` still returns the same values.

**Effort.** S. Do this before U19 (it's the substrate).

## U21. Plan->review completion rails [spec]

**Goal.** Semantic completion gates: before finishing, check the plan covered every asked-for feature
and that an actual deliverable exists - not just that the loop ran out of steps.

**Why.** HelixClaw's `guardrails.rs` has `SpecCoverageRail` (fails a plan that doesn't cover every
extracted goal feature) and `DeliverableExistsRail` (fails an approval with no deliverable), run
through a `GuardrailPipeline`. TheMauler has nothing checking "did we actually do what the user
asked" - false-done detection exists in benchmarks (U13) but isn't a completion gate.

**Reference.** `helixclaw-agents/src/guardrails.rs` - `Guardrail` trait, `SpecCoverageRail`,
`DeliverableExistsRail`, `GuardrailPipeline::{check_plan, check_review}`, `extract_goal_features`.

**Files.**
- New: `internal/app/completion_rails.go` + `_test.go`.
- Touch: `runAgentLoop` finalization - run the rails before emitting `done`; on failure, inject the
  gap ("goal feature X not addressed / no deliverable produced") and continue.
- Reuse: the U9 run-facts / evidence layer for deliverable detection.

**Design.**
- `extractGoalFeatures(goal)` -> keyword/feature set (port the stopword + keyword logic).
- Spec-coverage: compare features against work done (files touched, todos closed, evidence pins).
- Deliverable-exists: require at least one artifact/file/evidence when the task implied one.
- Advisory-then-blocking: start advisory (log only) to tune against false positives, then flip to
  blocking behind a flag once the eval harness shows it doesn't nag on satisfied runs.

**Tests.** Port `spec_coverage_passes/fails`, `deliverable_exists` tests; `agent_eval` scenario where
the model stops early with a feature unaddressed and the rail forces one more pass.

**Acceptance.** A run that ignored part of the ask is nudged to finish it; a complete run passes
without extra turns.

**Effort.** M.

# Tier 5 - Deeper architecture (defer until Tier 4 lands)

## U22. Experience / tool-sequence learning loop [spec]

**Goal.** Learn from past runs: suggest a likely tool sequence for a new task and a complexity
override, and track per-model success rates to inform routing.

**Why.** HelixClaw's `experience.rs` (`ExperienceLogger`, `ToolSequenceAnalyzer`, `WorkflowLearner`,
`compute_outcome_quality`, `model_success_rates`) turns run history into priors that shorten future
loops. TheMauler collects raw signal (`learning_candidates.go`, `spec_calibration.go`,
`milestone_memory.go`) but doesn't feed a tool-sequence prior back into the loop.

**Reference.** `helixclaw-agents/src/experience.rs`.

**Files.** New `internal/app/experience.go` (+ store), consuming existing `learning_candidates` /
milestone data; inject a compact "past runs like this used: read->grep->edit->test" hint at run start.

**Design.** Log `ExperienceEntry{task_type, keywords, tool_sequence, outcome_quality, model}` per run;
on a new task, classify + match keywords, suggest a sequence and complexity. Keep it advisory (a
prompt hint), never a hard override, so a bad prior can't wedge a run.

**Tests.** Outcome-quality scoring, keyword extraction, sequence suggestion, model-success-rate
aggregation (port HelixClaw's test cases).

**Acceptance.** After N logged runs of a task type, a new run of that type receives a relevant
sequence/complexity hint; cold start degrades gracefully to no hint.

**Effort.** L.

## U23. Task-DAG dispatcher [spec]

**Goal.** Let the model emit a task DAG once, then run it deterministically - parallel independent
branches, dependency gates, retries - with no further LLM tokens for orchestration.

**Why.** HelixClaw's `helix_graph.rs` (`HelixGraph` + `HelixDispatcher`) fans out parallel branches
and enforces dependency gates from a single JSON plan. TheMauler is strictly sequential; multi-part
tasks pay a full model turn per step. This is the largest build and should follow the reliability
items - a DAG over an unreliable step executor just fails in parallel.

**Reference.** `helixclaw-agents/src/helix_graph.rs` - `HelixGraph`, `HelixDispatcher`,
dependency/retry handling; and `submit_helix_graph` in `tools.rs`.

**Files.** New `internal/app/task_graph.go` + a `submit_task_graph` tool; the dispatcher reuses the
existing subagent runner (`subagents.go`) as the per-node executor with its budgets/contracts.

**Design.** Node = a scoped subagent task with declared inputs/deps; dispatcher topologically runs
ready nodes concurrently (bounded), gates on dependencies, retries a failed node once, and aggregates
results. Guard with a max-node cap and per-node budgets (reuse subagent budgets). Prerequisite: U16
(repair) + U17 (gates) so parallel nodes fail cleanly.

**Tests.** `TestGraphRunsIndependentNodesConcurrently`, `TestGraphRespectsDependencyGates`,
`TestGraphRetriesFailedNodeOnce`, `TestGraphMaxNodeCap`.

**Acceptance.** A 3-branch task (two independent + one dependent) runs the independents concurrently
and the dependent after both, in fewer wall-clock seconds than sequential, with correct aggregation.

**Effort.** L.

---

## U-shared. Cross-project (InferenceBridge) - see the cross-project plan

U1, U2, U3, U5, U6 are mirrored in HelixClaw
(`C:\Users\richa\Documents\HelixClaw\HELIXCLAW_AGENT_LOOP_UPGRADE.md`). Because both hit the same
InferenceBridge backend and the same local-model failure taxonomy, port fixes both ways and grade a
backend change on **both** loops with the same multi-step scenarios (TheMauler `agent_eval`,
HelixClaw Item 16 benchmark runner).

**2026-07 follow-up.** TheMauler Doctor now has a first-pass InferenceBridge agent-backend endpoint
parity check. It probes the deterministic agent-action validator and cheaply confirms Anthropic
`/v1/messages` plus OpenAI `/v1/embeddings` routes are present without forcing a generation call.
Remaining cross-project work is live benchmark proof for structured output, Anthropic streaming, and
embedding-model runs.

## Suggested order
1. **U1** (reasoning-effort) - smallest, highest ROI.
2. **U2** (tool-result offload) - kills lossy truncation; closes a backlog item.
3. **U3** (Doctor launch-flag/quant) - root cause, zero loop risk.
4. **U4** (programmatic tool calling) - biggest token/latency win after U1+U2.
5. **U5** (compaction ladder) -> **U6** (PROGRESS.md).
6. **U7** (grammar args) once a live server validates the probe.

**HelixClaw-parity ports (Tier 4, do in this order):**
7. **U16** (session-structure repair) - biggest reliability ROI, self-contained, offline-testable.
8. **U20** (permission classes) - cheap substrate for the next two.
9. **U18** (structural write-guards) - high safety, low risk.
10. **U19** (role-scoped tool sets) - builds on U20.
11. **U17** (verification gates) - correctness gate for coding runs.
12. **U21** (plan->review completion rails) - semantic done-check, advisory-then-blocking.

**Deeper architecture (Tier 5, defer until Tier 4 is green):**
13. **U22** (experience/tool-sequence learning).
14. **U23** (task-DAG dispatcher) - last; needs U16+U17 underneath it.
