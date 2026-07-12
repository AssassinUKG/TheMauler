# Agent-Loop Upgrade Plan - TheMauler + HelixClaw

**Date:** 2026-06-29
**Author:** research + cross-project synthesis pass
**Scope:** Compare both local-first agents against the current state of the art in agent-loop
("harness") design - Hermes Agent (Nous Research), the Claude Code harness, OpenHands/Aider/Cline,
and the 12-Factor Agents canon - then give a concrete, prioritized plan to make each project
better. Both projects share the same `InferenceBridge` backend and the same local-Qwen/Gemma
failure taxonomy, so a final section covers shared work that should be ported both ways.

Companion docs (already in-tree, do not duplicate):
- TheMauler: `docs/agent-loop-research-report.md`, `docs/agent-reliability-roadmap.md`, `ISSUES.md`
- HelixClaw: `HELIXCLAW_MAULER_IB_IMPROVEMENTS.md`, `RELIABILITY_HARDENING_PLAN.md`,
  `HELIXCLAW_SOLIDITY_TRACKER.md`, `CLAUDE_CODE_PARITY_TRACKER.md`

---

## 0. TL;DR - what the research says, and what to actually build

The premise both projects already operate on is correct and current: **the loop (harness) is the
product; stability is engineered into the harness, not bought with parameters.** Every credible
2026 source converges on the same minimal loop and the same hard-won wrappers around it (budgets,
loop-detection, context curation, tool-boundary validation, edit->verify, subagents). On that
checklist, **both projects are already ahead of the typical reference loop** for the local-model
case - TheMauler's `agent-loop-research-report.md` documents this in detail, and HelixClaw's
`HELIXCLAW_MAULER_IB_IMPROVEMENTS.md` shows the same fixes landing on the Rust side.

So this plan deliberately skips the table-stakes (already done in both) and targets the **five
genuinely new, high-leverage patterns** that neither project fully has yet, drawn from the loops
that are winning in 2026:

| # | Pattern | Source | Why it matters here | Status in TheMauler | Status in HelixClaw |
|---|---------|--------|---------------------|--------------------|---------------------|
| **A** | **Dynamic reasoning-effort control** - a first-class tool the agent calls to scale its own thinking up/down per step | Hermes (`reasoning_effort` tool, issue #7273) | Direct lever on Qwen3 `<think>` budget - the #1 local failure surface. Low effort for reads/edits, high for hard reasoning | [no] none | [no] none |
| **B** | **Programmatic / code-act tool calling** - one `execute_code` tool that runs a short script calling other tools, collapsing multi-step pipelines into one inference call | Hermes (`execute_code`), smolagents/CodeAct | Fewer round-trips = far fewer chances for a local model to spin out; big token + latency win on a 3090 | [no] none | [no] none |
| **C** | **Tool-result disk offload + pointer/preview** - oversized results go to disk, context keeps a ~2 KB preview + a retrieve handle, not the whole blob or a blind truncation | Claude Code (50 KB/tool, 200 KB/msg caps, offload-to-disk) | Replaces lossy truncation; the model can re-fetch the part it needs instead of losing it | [warn] truncates only | [warn] truncates only |
| **D** | **Graduated multi-tier compaction** - cheap->expensive ladder (budget-trim -> snip -> micro -> collapse -> summarize) instead of one summarize step | Claude Code 5-layer pipeline | Spends the least context-surgery needed; avoids the "summarize destroyed my rules" failure | [warn] 2 tiers | [warn] tiered budget |
| **E** | **Externalized progress ledger for fresh-context resume** - a structured `progress` file/record the agent maintains so a brand-new context can resume long work | Anthropic long-running-agents guidance; Hermes session lineage | Long HTB/lab/coding runs survive restarts and compaction with intent intact | [warn] checkpoint + milestone memory | [warn] SSOT + sessions |

Everything below is organized as: **A-E shared bets -> TheMauler-specific plan -> HelixClaw-specific
plan -> shared InferenceBridge track -> suggested order.**

---

## 1. The five cross-cutting bets (A-E), specified

### A. Dynamic reasoning-effort control  * highest ROI, smallest change

**What the field does.** Hermes exposes `reasoning_effort` as a tool the *agent* can call mid-run
to set its own thinking depth (minimal/low/medium/high), because agents "cannot reliably use slash
commands as structured state transitions." Match reasoning depth to task complexity: minimal for
lookups/formatting/rote edits, high for genuinely hard reasoning. The matching prompt guidance is
part of the feature ("when to use it, and when not to thrash it").

**Why it's perfect for these two.** Both run Qwen3.6 with `<think>` on by default, and both spend
real code defending against thinking-block failures (TheMauler's `forceNoThink` after N tool calls;
HelixClaw's empty-response/`ToolChoice::Required` recovery). Today thinking is a coarse global
toggle. A per-step effort dial turns that into a deliberate control surface - and the model can
*lower* its own effort on rote steps, which directly cuts the truncated-thinking failure rate.

**TheMauler implementation.**
- Add `EnableThinking` is already per-request; add a `ReasoningEffort string` to `llm.Request`
  and a per-turn override on the loop. Map effort -> (`enable_thinking`, `max_tokens` for the think
  budget, `--reasoning-budget` if the backend supports it, temperature family). `minimal`/`low`
  => thinking off or tight budget + coding sampler; `high` => thinking on, larger budget.
- Register a `set_reasoning_effort` tool (state-modifying, intercepted before the registry like
  Hermes does - mirror how `todo_*` tools already short-circuit). It just sets a field on the run.
- System-prompt nudge: "For simple reads/edits/formatting, call `set_reasoning_effort` with
  `low` first; reserve `high` for ambiguous design or debugging."
- Wire into the auto-router (`agent_modes.go`): Builder/Fixer default `medium`, Reviewer/Planner
  `high`, trivial Q&A `low`.

**HelixClaw implementation.** Same shape in `actor.rs`: add an effort field to the loop state, a
state-modifying `set_reasoning_effort` tool (bypass the registry like its `todo`/`memory` tools),
and map effort -> request thinking flags + budget in `llm/openai.rs` `req_json` (~4847).

**Acceptance (both).** Agent eval shows truncated-thinking and auto-continue counts drop on a
"rote multi-edit" scenario when the model lowers its own effort; no regression on a hard-reasoning
scenario. Cap effort changes per run (e.g. 6) to prevent thrash.

**Effort:** S each. **Do first.**

---

### B. Programmatic / code-act tool calling

**What the field does.** Hermes' `execute_code` (and the smolagents/CodeAct paradigm) lets the
model emit a short program that calls tools and does control flow *inside one action*, instead of
N separate reason->call->observe round-trips. "Programmatic tool calling collapses multi-step
pipelines into single inference calls."

**Why it matters here.** On a local 3090, every extra LLM turn is the expensive part and every turn
is another chance for a local model to mis-emit a tool call. A task like "read these 6 files, grep
for X in them, and summarize" is one script, not 13 turns. Both projects already have the safe
primitives (`read_many`, sandboxed shell/artifact runner) - this is a thin, high-value layer on top.

**Recommended scope (start minimal, sandboxed).**
- TheMauler: reuse the **artifact runner** (`internal/artifact` / `code_run`) but expose a curated
  in-process API to the script: `read(path)`, `glob(pat)`, `grep(pat, paths)`, `write(path, s)`,
  `sh(cmd)` - each routing back through the *existing* tool registry (so guardrails, mutation
  verify, rollback, and budgets still apply). Language: start with one (Python or a tiny JS via the
  runner) and a hard timeout. Return stdout + structured results as a single tool result.
- HelixClaw: it already has `execute_code`-style ambitions in the Claude-mode harness; formalize a
  `run_script` tool that calls the `AgentTool` registry from a sandboxed interpreter with the same
  budgets/guardrails.
- **Critical guardrails:** the script's tool calls must still pass `tool_guardrails`/mutation
  verification and count against the same tool-call + wall-clock budgets; destructive ops inside a
  script still hit the confirm gate (or are blocked in non-autonomous mode). Per-script step cap.

**Acceptance.** A "read N files + grep + summarize" eval finishes in 1-2 model turns instead of
N; guardrails still fire on a script that tries to write a secret or run a blocked command.

**Effort:** M-L each. **Highest token/latency payoff; do after A.**

---

### C. Tool-result disk offload + pointer/preview (replace blind truncation)

**What the field does.** Claude Code offloads oversized tool results to disk and replaces them in
context with a ~2 KB preview plus a handle; per-tool cap 50 KB, per-message aggregate 200 KB, run
*before every model call*. The model can re-read the offloaded blob on demand. This beats
truncation because nothing is permanently lost.

**Where both projects are now.** Both **truncate** (`MaxToolResultChars` in TheMauler;
`truncate_output` in HelixClaw). Truncation is lossy - the model can't recover the dropped middle.

**Implementation (both).**
- On a tool result over the cap: write the full result to a run-scoped store (TheMauler:
  `~/.config/mauler/run-artifacts/<runid>/<n>.txt` or the sessionstore; HelixClaw: a
  `ProjectContext`/temp store keyed by id). Replace the in-context content with: head+tail preview
  (Claude Code keeps head+tail; HelixClaw IB Item 9 already notes "verify head+tail truncation"),
  total size, and a `result_id`.
- Add a `read_tool_result(result_id, offset, limit)` retrieval tool so the model can page into the
  offloaded blob. (TheMauler already has the "lazy retrieval beyond master skills" idea in `AGENTS.md`
  -> this is the same pattern generalized to tool results.)
- Keep the per-message aggregate cap so several medium results can't collectively blow the window.

**Acceptance.** A 60 KB grep/web/shell result consumes ~2 KB of context on the turn it's produced;
a follow-up `read_tool_result` returns the requested slice; eval token-per-run drops on web/recon
scenarios with no loss of needed detail.

**Effort:** M each. Finishes HelixClaw IB **Item 9** and TheMauler's "lazy context retrieval"
backlog item in one stroke.

---

### D. Graduated multi-tier compaction

**What the field does.** Claude Code runs a five-stage ladder - budget-reduction -> snip ->
microcompact -> context-collapse -> auto-compact - cheapest first, summarization only as last resort.
The point: no single strategy fits all pressure, and the cheap layers spare you the lossy one.
Anthropic's own guidance: **never rely on compaction for critical rules - keep them in the system
prompt (MAULER.md / CLAUDE.md) so they survive.**

**Where both are now.** TheMauler has 2 effective tiers (`ClearOldToolResults(N)` then `doCompact`
summarize) plus `backendUsagePressure`. HelixClaw has `ContextBudget` tiers + compaction memory.
Both are close; the gap is formalizing the ladder and protecting the right things.

**Implementation (both).** Define an explicit ladder, each with a trigger threshold:
1. **Offload** oversized tool results (bet C) - already trimming the biggest contributor.
2. **Snip / clear old tool results** (keep the assistant+tool *pairing* intact - Hermes/Claude both
   stress never orphaning a tool result from its call).
3. **Microcompact** - drop reasoning/thinking traces from old turns first (cheap, lossless for the
   task); both keep reasoning in a separate field already, so it's droppable.
4. **Collapse** - protect_last_N turns verbatim, summarize the middle.
5. **Auto-summarize** - the existing `doCompact`, last resort.
- After any compaction, re-inject the goal + todo state (TheMauler already does R9 goal-reanchor;
  HelixClaw should mirror) and confirm critical rules live in MAULER.md/CLAUDE.md, not mid-history.

**Acceptance.** On a long run, summarization fires materially less often (cheaper tiers absorb most
pressure); after a compaction the next request still contains the original objective + rules.

**Effort:** M each (mostly formalizing + thresholds; the primitives exist).

---

### E. Externalized progress ledger for fresh-context resume

**What the field does.** Anthropic's long-running-agents guidance pairs compaction with
*externalized state*: an `init.sh`, a `claude-progress.txt` the agent appends to, and an initial
git commit, so each fresh context can "quickly understand the state of work." Hermes generates a
new session-lineage id on compression so history is traceable.

**Where both are now.** TheMauler has R3 checkpoint/resume (SQLite) + milestone memory (M1-M4) +
session_search FTS. HelixClaw has SSOT + session persistence. Both have the *storage*; what's
missing is a **single human- and model-readable progress artifact** that's the canonical "where am
I" the model maintains itself.

**Implementation (both).**
- A workspace-scoped `PROGRESS.md` (or `.mauler/progress.md`) the agent is prompted to keep:
  objective, decisions, files touched, what's verified, what's next, reusable commands. This is the
  durable twin of the in-memory todo list and survives any compaction or restart.
- On resume (TheMauler `ResumeRun`; HelixClaw session resume): seed the new context from
  PROGRESS.md + active todos + latest milestone memory, not from raw history.
- Tie into D: when compaction fires, the agent flushes a progress update *first* (Hermes flushes
  memory to disk before compression - same principle, "persist before loss").

**Acceptance.** Kill a long run mid-task, restart cold, and the agent resumes with correct
objective + next step from PROGRESS.md without re-reading the whole repo.

**Effort:** S-M each. TheMauler is closest (extend milestone memory + R3); HelixClaw extends SSOT.

---

## 2. TheMauler - project-specific plan

TheMauler's reliability roadmap (R1-R10) is essentially **complete** - eval harness, validate-on-load,
checkpoint/resume, wall-clock budget, subagent_explore, Anthropic escalation, plan/goal re-anchor,
telemetry all landed; only R7 (grammar-constrained tool args) is held pending a live llama.cpp probe.
So new work should be the A-E bets plus these TheMauler-only items:

1. **A - reasoning-effort tool** (see section 1A). Smallest, highest ROI. Wire into `agent_modes.go` router.
2. **C - tool-result offload** + `read_tool_result`. Directly retires the "lazy context retrieval
   beyond master skills" backlog item in `AGENTS.md`.
3. **B - `code_run`-backed programmatic tool calling**, reusing the existing artifact runner +
   registry so guardrails/rollback/budgets are inherited.
4. **D - formalize the compaction ladder** around existing `ClearOldToolResults`/`doCompact`; add a
   microcompact tier that drops old `<think>` traces first.
5. **E - `PROGRESS.md` artifact**, extending milestone memory (M-series) and `ResumeRun` (R3).
6. **R7 unblock** - run the existing **Grammar Probe** (`RunGrammarToolArgsProbe`) against the live
   InferenceBridge Qwen profile; if it preserves a valid `tool_calls` envelope, ship grammar-
   constrained args for the single-tool/`required` case (eliminates the malformed-args class).
7. **Open UI/ops polish** (from ISSUES U4-U7, M5-M6): run-KPI strip, lab run cards, "Resume Last
   Run" helper - these now have a real data spine (RunLedger) to draw from; finish them.

**Verification for every item:** add an `agent_eval` scenario (the R1 harness is built for exactly
this) so each change is regression-graded, and keep `go test ./... && go vet ./... && npm run build`
green; build via `.\build.ps1`.

---

## 3. HelixClaw - project-specific plan

HelixClaw is a Rust multi-agent framework (CEO -> Orchestrator -> Workers + Quality Gates) - a
**superset architecture** of TheMauler, with parallel agents, channels, voice, hybrid memory
(BM25+vector+MMR), and a dashboard. Its `HELIXCLAW_MAULER_IB_IMPROVEMENTS.md` log shows the
shared failure-taxonomy fixes landing (typed-arg coercion, terminal-error precedence, marker-leak
gate, native flags). Outstanding items there + the A-E bets:

1. **Finish the open IB items** (from the tracker): **Item 5** post-write mutation verification +
   snapshot-before-mutate on the autonomous path; **Item 2** supervisor empty-result gate (don't let
   `run_task_quality_pipeline` mark an empty run `done`); **Item 9** head+tail offload (folds into
   bet C); **Item 10/10b/10c** shared-backend context-downshift guard + compaction-memory-pollution
   guard + ctx/profile sizing; **Item 11/12** correlation IDs + IB-replay failure evidence;
   **Item 14** dashboard Ops live-run surface; **Item 16** automated benchmark runner.
2. **A - reasoning-effort tool** in `actor.rs` (bypass-registry tool like `todo`/`memory`).
3. **B - `run_script` programmatic tool calling** over the `AgentTool` registry, sandboxed, same
   budgets/guardrails. HelixClaw's actor already has parallel + sequential dispatch paths - script
   tool-calls route through the same guardrail/coercion helpers (`coerce_args_against_schema`).
4. **C - disk offload** replacing `truncate_output`, with a `read_tool_result` tool. Closes Item 9.
5. **D - graduated compaction**: it has `ContextBudget` tiers; add the microcompact (drop old
   reasoning) + protect-last-N + keep-tool-pairs rules, and re-inject goal after compaction (mirror
   TheMauler R9).
6. **E - SSOT/PROGRESS resume**: it already has SSOT (decisions, artifacts, glossary) + session
   persistence; expose a single `PROGRESS.md`-style artifact per project and seed fresh worker
   contexts from it. This also strengthens the **CEO recovery / Stalled -> Building** path.
7. **Claude-mode parity follow-ups** (ROADMAP): deeper permission lanes (read-only / accept-edits /
   shell / network / delegation), stronger non-project subagent lifecycle + worktree isolation,
   repair telemetry.

**Verification:** `cargo build` (workspace) + `cargo test -p helixclaw-agents` (currently ~796
passing); add unit tests next to the code per the doc's conventions; reference the item number in a
comment for traceability.

---

## 4. Shared InferenceBridge track (port fixes both ways)

Both projects hit the **same backend** and the **same local-model failure modes**, so treat the
loop contract as shared and port fixes in both directions. The single biggest reliability lever for
both lives *below* the Go/Rust loop - in how InferenceBridge launches and serves Qwen3.6:

- **Launch flags are root-cause fixes** (TheMauler `agent-loop-research-report.md` section 6.4, confirmed
  by ISSUES R7/R8): `--jinja` (or `use_jinja=true`), `--reasoning-format deepseek`, flash-attention
  on, and **disable speculative decoding** when truncation/repetition appears (specdec rejections at
  `</think>` spike EOS probability - the exact early-termination both loops defend against in code).
  Both projects' Doctors should assert these and warn loudly; fixing the launch removes failures the
  loops currently spend effort *recovering* from.
- **Quant/effective-capability floor:** stay at the ~9B-effective-and-up tool-tuned tier and prefer
  a higher quant (Q5/Q6 over Q4) for tool-calling accuracy; don't drop to 4B to fit more context.
- **Shared-backend context-downshift guard** (HelixClaw Item 10, TheMauler R18 already fixed):
  a subagent/second request must not reload the shared model at a *smaller* ctx than the parent.
  Make sure both sides reuse the larger loaded backend.
- **Correlation IDs + replay evidence** (HelixClaw Items 11/12): per-call ids that map a loop
  failure back to the exact IB request/response for debugging - port the concept to TheMauler's
  RunLedger.
- **One shared eval/bench suite:** TheMauler has the R1 `agent_eval` harness; HelixClaw has Item 16
  (benchmark runner, needs live IB) + MaulerBench. Run the *same* multi-step scenarios against both
  through InferenceBridge so a backend change is graded on both loops at once.

---

## 5. Suggested execution order (both projects)

1. **A - reasoning-effort tool** (S, both) - biggest ROI per line; directly attacks the dominant
   local failure surface.
2. **C - tool-result offload + retrieve** (M, both) - kills lossy truncation; closes HelixClaw
   Item 9 and TheMauler's lazy-retrieval backlog.
3. **InferenceBridge launch-flag + quant assertions in both Doctors** (S) - root-cause, zero loop risk.
4. **B - programmatic tool calling** (M-L, both) - largest token/latency win once A+C are in.
5. **D - graduated compaction ladder** (M, both).
6. **E - PROGRESS.md resume artifact** (S-M, both).
7. **Project-specific cleanup:** TheMauler R7 grammar probe + UI/ops polish; HelixClaw IB Items
   2/5/10/11/12/14/16 + Claude-mode permission lanes.

Each step ships with a shared agent-eval scenario so "does it just work" is measured, not asserted.

---

## Sources

- [Hermes Agent - Agent Loop Internals (Nous Research docs)](https://hermes-agent.nousresearch.com/docs/developer-guide/agent-loop)
- [Hermes Agent - runtime reasoning_effort tool proposal (issue #7273)](https://github.com/NousResearch/hermes-agent/issues/7273)
- [Hermes Agent Desktop App overview - Medium (E. Mak)](https://medium.com/@tentenco/hermes-agent-desktop-app-everything-you-need-to-know-about-nous-researchs-self-improving-ai-agent-3cb59bd31e5f)
- [Effective harnesses for long-running agents - Anthropic Engineering](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)
- [Context management in agent harnesses: memory, files, and subagents - Arize AI](https://arize.com/blog/context-management-in-agent-harnesses/)
- [Inside Claude Code's architecture: how the agent loop works - callsphere.ai](https://callsphere.ai/blog/inside-claude-code-s-architecture-how-the-agent-loop-works)
- [Dive into Claude Code: The Design Space of Today's and Future AI Agent Systems - arXiv 2604.14228](https://arxiv.org/abs/2604.14228)
- [Claude Code Subagents: A 2026 Practical Guide - Tembo.io](https://www.tembo.io/blog/claude-code-subagents)
- [12-Factor Agents - HumanLayer (GitHub)](https://github.com/humanlayer/12-factor-agents)
- [Best Open-Source AI Coding Tools 2026: Cline, Roo, OpenHands, Kilo - Frontman](https://frontman.sh/blog/best-open-source-ai-coding-tools-2026/)
- [Best AI Coding Agents (June 2026): Scored Leaderboard - Morph](https://www.morphllm.com/best-ai-coding-agents-2026)
- [The unreasonable effectiveness of an LLM agent loop with tool use - Hacker News #43998472](https://news.ycombinator.com/item?id=43998472)
