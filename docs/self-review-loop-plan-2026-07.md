# TheMauler — Self-Review Loop for Stable Automated Runs

**Created:** 2026-07-10
**Status:** [spec] — implementable source of truth. Nothing here is started yet.
**Audience:** an engineer or AI agent picking this up cold. Every item is self-contained: files,
signatures, algorithm, wiring anchors, tests, acceptance criteria. You should not need to re-derive
context.

## What this is, in one paragraph

Make TheMauler's **autonomous / automated runs** land tasks as reliably as HelixClaw by adding a
**self-review loop**: before a run is allowed to report `done`, it must pass a chain of gates —
(1) a **whole-task verification gate** (does it build / test / lint), (2) **completion rails** (did
it actually cover what was asked and produce a deliverable), and (3) a **fresh-context reviewer pass**
that critiques the work and can send the run back for one more fix cycle. Crucially, **all review is
done by the same profile/model that is already loaded** — there is *no* model swapping, no frontier
escalation, no second backend. The reviewer is the same Qwen profile running in a clean sub-context
with a read-only toolset. If a gate fails, the loop injects the exact gap as a system message and
continues instead of finishing; a hard cap on review cycles guarantees the run still terminates.

This is deliberately the **no-escalation** cousin of the shipped `agents.escalation_profile` feature
(R6). R6 hands hard steps to a stronger model; this plan keeps everything on the active profile. The
two are independent — you can run this with escalation disabled (the intended default here) or leave
R6 available as a separate opt-in.

---

## Why this specific design

Direct comparison of both codebases (see
[helixclaw-parity-comparison-2026-07.md](helixclaw-parity-comparison-2026-07.md)) found TheMauler's
biggest remaining reliability gap is the **absence of a whole-task correctness gate**. Today the loop
verifies *individual* mutations (`mutation_verifier.go`, per-write lint) and appends verifier *hints*
(`critical_verifier.go`), and it rejects a few false-done shapes (`invalidDoneReason`,
`app.go:5523`). But nothing checks "does the whole change build / do tests pass / did we do what was
asked" before declaring success. That is the difference between *"the file changed"* and *"the change
works."*

HelixClaw closes this with three mechanisms this plan ports **without** its multi-agent OS overhead:

- `verify_loop.rs` + `verification.rs` — real build/test/lint gates with a blocking verdict → **S1**.
- `guardrails.rs` (`SpecCoverageRail`, `DeliverableExistsRail`) — semantic completion gates → **S2**.
- CEO review of a worker's deliverable → adapted here as a **same-profile reviewer sub-pass** → **S3**.

The roadmap already specs the first two as U17 and U21
([agent-loop-upgrade-roadmap.md](agent-loop-upgrade-roadmap.md)). **This document supersedes and
sequences U17 + U21 into one coherent review loop, and adds S3 (the same-profile reviewer pass) and
S4 (the orchestrator that ties them together), which the roadmap did not have.** When these land,
mark U17 and U21 done and point them here.

---

## Conventions (read before editing)

- The loop lives in `internal/app/app.go`, `func (a *App) runAgentLoop(...)` (currently `app.go:2358`),
  loop label `agentLoop:` (`app.go:2544`). **Line numbers drift — always `grep` the named symbol
  before editing.**
- The **finalization** logic is the `defer func()` at the top of `runAgentLoop` (`app.go:2363`). It
  already contains the existing done-gates: `requiresLivingDocUpdate` check (`app.go:2381`),
  `invalidDoneReason` check (`app.go:2387`). **This defer runs once, at return — it is the wrong place
  to *re-enter* the loop.** The review loop must be driven from a point where the loop can still take
  another turn (see S4 for the exact seam).
- Keep `go build ./...`, `go vet ./...`, `go test ./...` green. Build via `.\build.ps1`.
- **Grade every behavioral change with `agent_eval`** (`internal/app/agent_eval.go` +
  `testdata/agent_eval/*.json`). Add a scenario for each item. This is the regression spine — a
  reliability change that isn't measured by the harness doesn't count as done.
- **Match existing style:** table-driven tests, comments explain *why* not *what*, no new deps without
  reason. Reuse the structured-contract shape (`state` / `next_tool` / `evidence` / `do_not_repeat`)
  the loop already uses (`tool_execution_state.go`, U15) for any new model-facing gate message.
- **Do not regress** TheMauler's existing advantages: tool-result offload (U2), compaction ladder
  (U5), loop-metrics anti-loop (`loop_metrics.go`, R10), session repair (U16), and the subagent
  budgets (`subagents.go`). Port selectively.

---

## Config surface (single new settings block)

Add one block so the whole feature is discoverable, testable, and off-by-default-safe. Put it on
`settings.AgentsConfig` (`internal/settings/model.go`, defaults in `internal/settings/defaults.go`,
clamps in `internal/settings/validate.go`).

```go
// ReviewLoopConfig controls the same-profile self-review loop that gates run completion.
// It never swaps models — every review turn uses the run's active profile.
type ReviewLoopConfig struct {
    Enabled            bool     `json:"enabled"`              // master switch (default true)
    OnlyAutonomous     bool     `json:"only_autonomous"`      // gate only autonomous runs (default true)
    MaxReviewCycles    int      `json:"max_review_cycles"`    // hard cap on fix→review cycles (default 2)

    VerifyGate         bool     `json:"verify_gate"`          // S1 build/test/lint gate (default true)
    VerifyCommands     []string `json:"verify_commands"`      // override auto-detected gate cmds (default nil = auto)
    VerifyTimeoutSec   int      `json:"verify_timeout_sec"`   // per-gate-command timeout (default 120)

    CompletionRails    bool     `json:"completion_rails"`     // S2 spec-coverage + deliverable-exists (default true)
    CompletionBlocking bool     `json:"completion_blocking"`  // S2 blocks vs advisory-only (default false, flip after tuning)

    ReviewerPass       bool     `json:"reviewer_pass"`        // S3 fresh-context reviewer sub-pass (default true)
    ReviewerMaxTools   int      `json:"reviewer_max_tools"`   // reviewer sub-run tool cap (default 15)
}
```

**Validate() clamps:** `MaxReviewCycles` `<0 → 0`, `>5 → 5`; `VerifyTimeoutSec` `<=0 → 120`;
`ReviewerMaxTools` `<=0 → 15`, `>40 → 40`. Record each clamp in the adjustments list like the other
`Validate()` rules. Default the whole block from `defaults.go` so a missing block is fully populated.

**Gating rule used everywhere below:** a run is subject to the review loop iff
`cfg.Agents.ReviewLoop.Enabled && (!OnlyAutonomous || autonomous) && runIsGateable(run, mode)`.
`runIsGateable` = the run mutated files *or* the prompt implied a concrete deliverable (reuse
`runHasFileMutation`, `app.go:5510`, plus the U9 run-facts deliverable signal). **Pure research /
recon / read-only runs are never gated** — same exclusion U17 specifies.

---

## Architecture: where the review loop attaches

```
runAgentLoop
  └─ agentLoop: (per-turn ReAct loop)  ......... unchanged
        │  model turn → tool calls → results → repeat
        │
        ▼  loop reaches a natural terminal turn (model emits a final text answer, no tool call)
  ┌─────────────────────────────────────────────────────────────┐
  │  S4  REVIEW ORCHESTRATOR  (new: reviewPhase in app.go)        │
  │                                                               │
  │  if !gateable → finish as today                               │
  │  else, for cycle in 1..MaxReviewCycles:                       │
  │    S1 verify gate ── fail → inject failures, `continue` loop  │
  │    S2 completion rails ── fail(blocking) → inject gap, cont.  │
  │    S3 reviewer sub-pass ── verdict=changes → inject, continue │
  │    all pass → break → finish `done`                           │
  │  cap hit → finish honestly with residual gaps in summary      │
  └─────────────────────────────────────────────────────────────┘
        │
        ▼
  defer finalization (existing invalidDoneReason etc.) ......... unchanged
```

The orchestrator sits **between "the loop wants to finish" and "the loop returns"**, at a point where
it can still push a system message and jump back to `agentLoop:`. The existing `defer` finalization
(false-done, junk-summary, doc-missing checks) stays as the *last* line of defense and runs unchanged
after the review loop is satisfied or capped.

---

# The items

Implement in order S0 → S1 → S2 → S3 → S4. S0 is the substrate; S1–S3 are independently testable
gates; S4 wires them into the loop. Ship S1 alone first (biggest correctness win), then layer S2/S3.

---

## S0. Review-loop config + gateability substrate  [spec]  · Effort: S

**Goal.** Land the config block, defaults, clamps, and the `runIsGateable` predicate so every later
item has a single switch and a single "should this run be reviewed?" decision.

**Files.**
- Touch: `internal/settings/model.go` (add `ReviewLoop ReviewLoopConfig` to `AgentsConfig` + the
  struct above), `internal/settings/defaults.go` (populate defaults), `internal/settings/validate.go`
  (clamps + adjustments), `internal/settings/validate_test.go`.
- New: `internal/app/review_gate.go` — home for `runIsGateable`, shared helpers, and the gate types
  S1–S3 fill in. `internal/app/review_gate_test.go`.
- Touch: `frontend/src/components/SettingsModal.tsx` — add the fields under an existing tab (General
  or a new "Review" sub-section). `frontend/src/wailsjs/go/models.ts` regenerates from the Go structs.

**Signatures.**
```go
// runIsGateable reports whether the review loop should run for this run.
// True when the run mutated files or the prompt implied a concrete deliverable,
// and the mode is a build/fix/ops mode (never pure research/recon/read-only).
func runIsGateable(run TaskRun, mode AgentMode) bool

// VerifyVerdict is the shared result shape all three gates return, mirroring
// HelixClaw's GateVerdictEnvelope. Status ∈ {pass, fail, error}.
type VerifyVerdict struct {
    Gate         string   `json:"gate"`          // "build" | "test" | "lint" | "completion" | "reviewer"
    Status       string   `json:"status"`        // pass | fail | error
    Blocking     bool     `json:"blocking"`      // if true and !pass, completion is refused
    Summary      string   `json:"summary"`       // one line for logs / cockpit
    Improvements []string `json:"improvements"`  // concrete, model-facing "fix these" items
    Evidence     string   `json:"evidence"`      // command+output pointer (offload via U2 if large)
}
```

**Tests.** `TestRunIsGateableMutation`, `TestRunIsGateableDeliverablePrompt`,
`TestRunIsGateableSkipsResearch`, `TestReviewLoopConfigClamps`, `TestReviewLoopConfigDefaultsPopulate`.

**Acceptance.** A build run with an edit is gateable; a "map this repo" research run is not; a broken
config yields clamped values + a non-empty adjustments list; the Settings UI round-trips the block.

---

## S1. Whole-task verification gate  [spec]  · Effort: M  · (supersedes U17)

**Goal.** Gate completion on **real build/test/lint evidence** with a structured verdict, so the agent
cannot declare success on code that doesn't compile or pass. Same model — this runs *commands*, not a
second LLM.

**Files.**
- New: `internal/app/verify_gate.go` + `_test.go`.
- Reuse: `mutation_verifier.go` stays the per-file layer; this is the whole-task layer. Offload large
  gate output through the U2 tool-result store.

**Design.**
- **Project-type detection**, not hardcoded commands:
  - Go project (`go.mod` present) → `go build ./...`, `go vet ./...`, `go test ./...`
  - Node (`package.json` with a `build`/`test` script) → the detected npm scripts
  - Python (`pyproject.toml`/`setup.py`) → `python -m compileall`, `pytest -q` if pytest present
  - Generic / unknown → `VerifyCommands` from config if set, else **skip with a `status=skip` verdict**
    (never fail a run just because we don't know how to verify it).
  - `VerifyCommands` in config always overrides detection.
- Run each command with a `VerifyTimeoutSec` timeout through the **same execution path the tools use**
  (respect the workspace CWD, env, and the guardrails). Capture exit code + combined output.
- Parse into a per-command `VerifyVerdict{gate, status, blocking, summary, evidence}`. **Build/vet/
  compile failures are `Blocking:true`; test failures are `Blocking:true`; lint-only warnings are
  `Blocking:false`** (advisory) — configurable later, but this split matches "must compile, should
  pass tests, may lint clean."
- **Only gate when `runIsGateable`** and only in Builder/Fixer/Ops-style modes. Never gate a
  research/recon run (S0's predicate already handles this).
- **Cap:** the S4 orchestrator enforces `MaxReviewCycles`; S1 itself is stateless — it just runs and
  returns verdicts.

**Signature.**
```go
// runVerifyGate detects the project's verify commands, runs them, and returns one
// verdict per command. It never blocks by itself — S4 decides based on Blocking.
func (a *App) runVerifyGate(ctx context.Context, run *TaskRun, cfg *settings.Settings) []VerifyVerdict
```

**Model-facing message on failure** (built by S4, shown as a system turn): reuse the contract shape —
```
[verify_gate:failed] The task is not complete: `go build ./...` failed.
next_tool: edit_file
evidence: <offloaded result_id or inline tail>
fix: <each Improvements line>
do_not_repeat: declaring done before build/vet/test pass.
```

**Tests.** `TestVerifyGateDetectsGoProject`, `TestVerifyGateParsesPassFail`,
`TestVerifyGateBlockingSplit` (build/test blocking, lint advisory), `TestVerifyGateSkipsUnknownProject`,
`TestVerifyGateRespectsTimeout`, `TestVerifyGateSkipsNonCodingRun`. Plus `agent_eval` scenario
`verify-blocks-compile-error`: seed a `.go` file with a compile error, prompt "fix the bug and finish";
assert the run does **not** report `done` until `go build` passes (reuse the `edit-then-verify`
scenario shape already in `testdata/agent_eval/`).

**Acceptance.** A Builder run that leaves the tree non-compiling loops back with the exact errors
instead of declaring success; a passing run finishes normally; a research run is never gated; an
unknown project type is skipped, not failed.

---

## S2. Completion rails (spec-coverage + deliverable-exists)  [spec]  · Effort: M  · (supersedes U21)

**Goal.** Semantic completion gates: before finishing, check the run **covered every asked-for
feature** and **produced an actual deliverable** — not just that the loop ran out of steps. This is
the "did we do what the user asked" check that even a green build won't catch (build passes, but the
user asked for two functions and we wrote one).

**Files.**
- New: `internal/app/completion_rails.go` + `_test.go`.
- Reuse: U9 run-facts / evidence layer (`run_facts.go`) for deliverable detection; the todo tool state
  for closed-step signal; `runHasFileMutation` for the file signal.

**Design (port HelixClaw `guardrails.rs`).**
- `extractGoalFeatures(goal string) []string` — tokenize the original prompt, drop stopwords, keep
  salient nouns/verbs and any explicit enumerations ("add X **and** Y", numbered lists). Port the
  stopword + keyword logic from `guardrails.rs::extract_goal_features`.
- **SpecCoverageRail:** compare extracted features against work done — files touched, todos closed,
  evidence pins, and the final summary text. A feature is "covered" if it appears in a touched file
  path/content, a closed todo, or the final summary with a concrete action. Uncovered salient features
  → `Improvements: ["goal feature 'export to CSV' not addressed"]`.
- **DeliverableExistsRail:** if the prompt implied a concrete artifact (file/report/writeup/patch) and
  no artifact/file/evidence pin exists, fail with `"no deliverable produced for a task that asked for
  one"`.
- **Advisory → blocking:** default `CompletionBlocking=false` (log a `completion_rail` ledger event
  only, never delay a run) so you can tune false positives against real runs first. Flip to blocking
  per-config once `agent_eval` shows it doesn't nag satisfied runs. **Do not ship blocking by default
  until the eval suite is clean** — a nagging rail is worse than no rail.

**Signature.**
```go
// runCompletionRails checks spec-coverage and deliverable-exists. Returns verdicts;
// Blocking honors cfg.Agents.ReviewLoop.CompletionBlocking.
func runCompletionRails(run *TaskRun, cfg *settings.Settings) []VerifyVerdict
```

**Tests.** Port `spec_coverage_passes`, `spec_coverage_fails`, `deliverable_exists_pass`,
`deliverable_exists_fail` from `guardrails.rs`; `TestExtractGoalFeaturesDropsStopwords`;
`TestCompletionRailsAdvisoryDoesNotBlock`. `agent_eval` scenario `completion-covers-both-asks`: prompt
"add a `min` and a `max` helper"; a run that adds only `min` is nudged to add `max` (blocking mode).

**Acceptance.** A run that ignored part of the ask is nudged to finish it (blocking mode); a complete
run passes without extra turns; advisory mode only logs.

---

## S3. Same-profile reviewer sub-pass  [spec]  · Effort: M  · (new — the "review" in review loop)

**Goal.** After S1/S2 pass, run **one fresh-context reviewer turn using the same profile** that
critiques the actual diff/deliverable and returns a structured verdict: `approve` or
`request_changes` with concrete items. This catches the class of problems mechanical gates miss —
logic that compiles and covers the ask but is wrong, unsafe, or ignores an obvious edge case. It is
the local-model, no-escalation stand-in for HelixClaw's CEO reviewing a worker's deliverable.

**Why a sub-pass and not just a prompt.** Reviewing in the *same* context that produced the work
inherits that context's blind spots and bloat (>40% "dumb zone"). A fresh sub-context with **only the
task + the diff + the deliverable** and a **read-only reviewer toolset** produces a sharper critique
and keeps the main loop's context clean. TheMauler already has the machinery: `subagents.go` runs
bounded sub-runs with explicit turn/tool/output/context budgets, and U19's role-scoped read-only
toolset (`read_file`, `read_tool_result`, `glob`, `grep`) is exactly the reviewer allowlist.

**Files.**
- New: `internal/app/reviewer_pass.go` + `_test.go`.
- Reuse: the bounded subagent runner in `subagents.go` (same pattern as `subagent_explore`), the U2
  offload for large diffs, and the run-facts/diff assembly. **Same backend/profile** — do not touch
  `escalation.go` or add a client; the sub-run shares `a`'s active profile exactly like the existing
  subagents do.

**Design.**
- Assemble a **compact review packet**: original objective, the list of files touched with their diffs
  (or full content for new files), closed todos, and the final summary. Cap the packet (offload via U2
  if large) so it fits the profile's context.
- Spawn a sub-run with:
  - system instruction: *"You are a strict reviewer. You did not write this. Review the change against
    the objective for correctness, missed edge cases, safety, and whether it truly satisfies the ask.
    You may read files to verify. Do not edit. Return a JSON verdict."*
  - read-only toolset only (`read_file`, `read_tool_result`, `glob`, `grep`), `MaxToolCalls =
    ReviewerMaxTools`, tight context budget.
  - **grammar-constrained final verdict** (reuse the JSON-schema request path):
    `{verdict: "approve"|"request_changes", severity: "blocking"|"advisory", items: []string}`.
- Return a `VerifyVerdict{gate:"reviewer", status: pass|fail, blocking: severity=="blocking",
  improvements: items}`. **A reviewer `request_changes/blocking` verdict sends the run back** with the
  items injected; `advisory` is logged but does not block.
- **Cap:** the reviewer pass runs at most once per review cycle, and cycles are capped by
  `MaxReviewCycles` in S4 — so at most `MaxReviewCycles` reviewer sub-runs per task. Bound the cost.
- **Self-review honesty guard:** because reviewer and author are the same model, add a light
  anti-rubber-stamp nudge ("list at least the top risk even if approving"). Empty/garbage output,
  provider failure, and reviewer turn exhaustion must
  produce an explicit `inconclusive` verdict. Advisory interactive mode may continue with a visible
  warning; blocking autonomous mode should retry within budget and then stop as `review_incomplete`.
  Session repair (U16) protects the sub-run transcript. Audit correction 2026-07-10: track the
  current fail-open implementation as MAULER-AR-004 in `agentic-reliability-issues-2026-07.md`.

**Signature.**
```go
// runReviewerPass spawns a bounded, read-only, same-profile sub-run that critiques the
// run's deliverable and returns pass, fail, or inconclusive without silently approving malformed output.
func (a *App) runReviewerPass(ctx context.Context, run *TaskRun, profile settings.Profile, cfg *settings.Settings) VerifyVerdict
```

**Tests.** `TestReviewerPassReadOnlyToolset` (spawned toolset excludes write/edit/shell),
`TestReviewerPassParsesVerdict` (mock client returns `request_changes` → blocking verdict),
`TestReviewerPassReturnsInconclusiveOnGarbage`, `TestReviewerPassRespectsToolCap`. `agent_eval` scenario
`reviewer-catches-missed-edge-case`: seed a task whose obvious-but-uncovered edge case a good reviewer
flags; assert the run takes one more cycle and addresses it (use a scripted mock reviewer verdict so
the test is backend-independent).

**Acceptance.** The reviewer sub-run cannot edit; it returns an actionable verdict; a `request_changes`
verdict adds exactly one more fix cycle; a malformed verdict is treated as approve and never blocks.

---

## S4. Review orchestrator — wire the gates into the loop  [spec]  · Effort: M

**Goal.** Drive S1→S2→S3 as a bounded fix→review loop at the seam where the run wants to finish, and
guarantee termination.

**The exact seam.** The loop finishes when the model emits a **final text answer with no tool call**.
In `runAgentLoop`, that is the branch that currently sets `finalSummary = visibleText` and breaks out
of `agentLoop:` (around `app.go:3003`, the "no tool calls → we're done" path; grep
`finalSummary = visibleText` and the `break agentLoop` / natural loop-exit near it). **Insert the
orchestrator immediately before that terminal break.** Do *not* put it in the `defer` (that runs at
return and cannot take another turn).

**Algorithm.**
```
// reviewCyclesUsed persists across iterations (declare above agentLoop:).
onModelWantsToFinish:
    if !cfg.Agents.ReviewLoop.Enabled || !gateable(run, mode, autonomous):
        proceed to normal finish            // unchanged behavior
    verdicts := []
    if cfg...VerifyGate:      verdicts += runVerifyGate(...)       // S1
    if cfg...CompletionRails: verdicts += runCompletionRails(...)  // S2
    if cfg...ReviewerPass:    verdicts += [runReviewerPass(...)]   // S3
    blocking := verdicts.filter(v => !pass && v.Blocking)
    record a `review_gate` ledger event with every verdict (pass+fail) // cockpit/logs
    if blocking is empty:
        proceed to normal finish            // all gates satisfied → done
    if reviewCyclesUsed >= cfg...MaxReviewCycles:
        // termination guarantee: stop honestly, do NOT loop forever
        append residual gaps to finalSummary ("Completed with unresolved: <blocking summaries>")
        set status "stopped" (reason "review_incomplete") and proceed to finish
    reviewCyclesUsed++
    inject one system message built from `blocking` (contract shape: state/next_tool/fix/do_not_repeat)
    `continue agentLoop`                     // take another fix turn with the same profile
```

- **Ordering matters:** S1 (build) before S2 (coverage) before S3 (reviewer). No point asking the
  reviewer to critique code that doesn't compile. If S1 blocks, skip S2/S3 this cycle to save tokens.
- **One injected message per cycle**, consolidating all blocking items, so the transcript stays clean
  and session-repair-friendly (U16).
- **Reuse existing budget guards:** if `toolBudgetExhausted` or `timeBudgetExhausted` is already set,
  **do not start a review cycle** — honor the existing graceful-stop path. The review loop is a
  completion gate, not a way to blow past budgets.
- **Metrics:** extend `buildLoopMetrics` (`loop_metrics.go`) with `review_cycles`, `verify_gate_fails`,
  `completion_rail_fails`, `reviewer_change_requests`, so regressions are visible on the Logs
  stability card (R10 surface) and in `agent_eval` reports.
- **Cockpit:** add a "Review" card to the Run page reading the `review_gate` ledger events — show each
  gate's pass/fail with its evidence handle, and the cycle count. Reuses the U12 evidence-card pattern.

**Files.**
- Touch: `internal/app/app.go` — the terminal-finish branch in `runAgentLoop`; declare
  `reviewCyclesUsed` above `agentLoop:`; call a new `func (a *App) runReviewPhase(...) (proceed bool,
  injected string)` that returns whether to finish or loop.
- New: `internal/app/review_phase.go` + `_test.go` — the orchestrator (`runReviewPhase`), kept out of
  the already-huge `app.go` where practical.
- Touch: `internal/app/loop_metrics.go` (+ test) for the new counters.
- Touch: `frontend/src/components/RunPage.tsx` (or the current Run cockpit component) for the card.

**Tests.** `TestReviewPhaseProceedsWhenAllPass`, `TestReviewPhaseLoopsOnBlockingVerdict`,
`TestReviewPhaseRespectsMaxCycles` (caps and stops honestly), `TestReviewPhaseSkipsNonGateableRun`,
`TestReviewPhaseHonorsBudgetExhaustion`, `TestReviewPhaseInjectsOneConsolidatedMessage`. `agent_eval`
scenario `review-loop-recovers`: a task that first finishes with a compile error is driven to a green,
covered, reviewer-approved finish within `MaxReviewCycles`, and a scenario `review-loop-terminates`:
an unfixable task stops honestly at the cap with residual gaps in the summary (never infinite).

**Acceptance.** With the loop enabled, an autonomous Builder run cannot report `done` while the tree
fails to build, a requested feature is unaddressed, or the reviewer requests blocking changes — it
takes up to `MaxReviewCycles` more fix turns, then either finishes clean or stops honestly. With the
loop disabled, behavior is byte-for-byte unchanged. No model other than the active profile is ever
called.

---

## Execution order & milestones

1. **S0** — config + `runIsGateable`. Half a day. Nothing behaves differently yet.
2. **S1** — verify gate, wired through S4's seam with S2/S3 stubbed to always-pass. **This alone is the
   biggest win** — ship and measure it before layering more. Add `agent_eval verify-blocks-compile-error`.
3. **S4 (partial)** — orchestrator with just S1, `MaxReviewCycles` termination, metrics, cockpit card.
   Prove the loop terminates and recovers on the eval suite.
4. **S2** — completion rails in **advisory** mode first; watch the `completion_rail` ledger events on
   real runs for false positives, then flip `CompletionBlocking` on.
5. **S3** — reviewer sub-pass. Land read-only + fail-open first, tune the reviewer prompt against the
   eval suite so it doesn't rubber-stamp or over-nag.
6. Flip defaults on once the full `agent_eval` suite is green on the active profile.

## Definition of "done and stable"

- `go build ./... && go vet ./... && go test ./...` green; `.\build.ps1` green.
- The full `agent_eval` suite passes on the active profile, **including** the four new scenarios
  (`verify-blocks-compile-error`, `completion-covers-both-asks`, `reviewer-catches-missed-edge-case`,
  `review-loop-terminates`).
- An autonomous Builder run that leaves a compile error, a missed ask, or a reviewer-flagged bug does
  **not** report `done` — it recovers within `MaxReviewCycles` or stops honestly with the gap named.
- `ReviewLoop.Enabled=false` reproduces today's behavior exactly (regression-checked by an eval run
  with the block off).
- **No frontier/escalation call anywhere in the path** — grep confirms `runReviewPhase`,
  `runVerifyGate`, `runCompletionRails`, and `runReviewerPass` never touch `escalation.go` or a
  non-active-profile client.

## Non-goals (explicitly out of scope here)

- Model swapping / frontier escalation (that is the separate, already-shipped R6
  `agents.escalation_profile`; leave it independent and default-off for this feature).
- The multi-agent task-DAG (U23) and experience learning (U22) — deeper architecture, defer until this
  review spine is green, exactly as the roadmap orders.
- Structural write-guards (U18) and permission classes (U20) — complementary safety, tracked
  separately in [agent-loop-upgrade-roadmap.md](agent-loop-upgrade-roadmap.md); S3's read-only reviewer
  toolset leans on U19/U20 if present but does not require them (use an explicit tool-name allowlist in
  the interim).
