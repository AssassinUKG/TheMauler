# HelixClaw <-> TheMauler - Agentic Parity Comparison

**Created:** 2026-07-06
**Purpose:** Record the architectural gap between HelixClaw's agents and TheMauler's, grounded in a
direct read of both codebases (not just planning docs), and point at the concrete backport items.
**Actionable items:** [agent-loop-upgrade-roadmap.md](agent-loop-upgrade-roadmap.md) section U16-U23.
**Sources read:**
- TheMauler: `internal/app/` (agent loop, guards, verify, subagents), `internal/agent/`,
  `internal/tools/`.
- HelixClaw: `C:\Users\richa\Documents\HelixClaw\crates\helixclaw-agents\src\` (Rust).

---

## The one difference that drives all the others

**HelixClaw is a hierarchical multi-agent OS. TheMauler is a single, well-hardened loop.**

- **HelixClaw** runs a *team*: a **CEO** (`ceo.rs`) plans -> assigns typed workers -> **reviews**
  deliverables, coordinated by a ~20k-line **supervisor actor** (`supervisor.rs`, message-passing over
  `mpsc`). Each worker runs an explicit **planner -> executor -> observer** cycle (`claude/planner.rs`,
  `claude/executor.rs`, `claude/observer.rs`, `claude/state.rs`) with *role-scoped tool sets*, and a
  **HelixGraph** (`helix_graph.rs`) lets the model emit a task DAG once that a deterministic dispatcher
  runs with parallel branches and retries.
- **TheMauler** runs one `runAgentLoop` (in `internal/app/app.go`) that swaps a **persona/mode**
  (`agent_modes.go`) per task and can spawn short-lived **subagents** (`subagents.go`). It's a flat
  ReAct loop with a strong reliability spine bolted on - but no persistent team, no plan->review gate,
  and no whole-task verification gate.

Everything below follows from that.

---

## Capability matrix

| Dimension | HelixClaw | TheMauler | Gap | Backport |
|---|---|---|---|---|
| Loop shape | planner->executor->observer phases (`claude/*`) | single ReAct loop, persona swap (`agent_modes.go`) | Big | U19 |
| Orchestration | CEO + supervisor actor + typed worker team | ephemeral subagents (`subagents.go`) | Big | (U23 partial) |
| Task graph | HelixGraph DAG + deterministic dispatcher | none (sequential) | Big | U23 |
| Whole-task verification | build/clippy/test gates + `verify_loop` blocking severities + `GateVerdictEnvelope` | per-write lint (`mutation_verifier.go`), hints (`critical_verifier.go`) | Big | U17 |
| Structural write-guards | protected paths + patch-size + file-count caps + `GuardTracker` (`guard.rs`) | none - only output redaction (`guardrails.go`) | Big | U18 |
| Message-structure repair | 7-phase sanitizer (`session_repair.rs`), every packet | partial, compaction-only (`sanitizeCompactedMessages`) | Big | U16 |
| Learning | `ExperienceLogger` + `WorkflowLearner` + `ToolSequenceAnalyzer` + model success rates | partial signal only (`learning_candidates.go`, `spec_calibration.go`, `milestone_memory.go`) | Medium | U22 |
| Model routing | complexity -> `ReasoningBudget` -> `model_for_tier` | effort tool + escalation (`escalation.go`, `reasoning_effort.go`) | Medium | (covered by U1) |
| Permission model | classes ReadOnly/Edit/Execute/Network/Delegate (`tools.rs`) | binary `Destructive()` only | Medium | U20 |
| Completion check | plan/review rails (spec-coverage, deliverable-exists) | false-done in benchmarks only, not a gate | Medium | U21 |
| Rollback | git snapshots (`git_snapshots.rs`) | in-memory op stack, dies with process (`rollback.go`) | Medium | (future) |
| Large-file writes | chunked file-gen controller surviving respawns (`file_gen_controller.rs`) | plain `write_file` | Small | (future) |
| Plan mode / ask_user | `EnterPlanMode`/`ExitPlanMode`/`ask_user` tools | none | Small | (future) |
| Tool surface | ~40 canonical + mgmt tools, Claude-name aliasing | ~50 tools, no aliasing | ~parity | - |

---

## What HelixClaw has that TheMauler lacks (the "more agentic" pieces)

1. **Role-separated loop with role-scoped tools** (`claude/executor.rs`). Distinct
   `planner_tools()` / `executor_tools()` / `reviewer_tools()`; the planner literally cannot edit, the
   reviewer cannot write. `observer.rs` classifies each reply as an explicit state transition
   (`is_terminal`, blocked, needs-verify). -> **U19**.
2. **Plan->review guardrails** (`guardrails.rs`). `SpecCoverageRail` fails a plan that misses a goal
   feature; `DeliverableExistsRail` fails an approval with no deliverable. Semantic completion gates.
   -> **U21**.
3. **Verification-gate loop** (`verify_loop.rs` + `verification.rs`). Real build/clippy/test gates, a
   structured `GateVerdictEnvelope`, and `VerifierImprovement` items with a **blocking** severity that
   prevents completion. -> **U17**.
4. **Structural write-guards** (`guard.rs`). Protected-path denylist, max patch size, max file count,
   `GuardTracker` accumulating across the run. -> **U18**.
5. **Session repair** (`session_repair.rs`). 7-phase message-history sanitizer. The single biggest
   reason HelixClaw survives malformed local-model transcripts. -> **U16**.
6. **Experience/learning loop** (`experience.rs`). `WorkflowLearner` suggests tool sequences and
   complexity overrides from past outcomes; tracks per-model success rates. -> **U22**.
7. **HelixGraph** (`helix_graph.rs`). One LLM call emits a task DAG; a deterministic dispatcher runs it
   with parallel branches, dependency gates, retries - zero further model tokens. -> **U23**.
8. **Permission classes + tool-name aliasing** (`tools.rs`). Every tool maps to
   ReadOnly/Edit/Execute/Network/Delegate, and Claude-style names normalize to internal tools. -> **U20**
   (classes); aliasing is a smaller nice-to-have.

---

## What TheMauler already does better or at parity (do not regress)

- **Tool-result disk offload + `read_tool_result`** (`tool_result_store.go`) - HelixClaw has no obvious
  equivalent; avoids blind truncation.
- **Microcompact ladder + externalized `PROGRESS.md`** (`context_ladder.go`) and **resumable run
  checkpoints** (`run_checkpoint.go`).
- **Loop-stability scoring + command-storm / repeated-read guards** (`loop_metrics.go`,
  `agent_loop_stability.go`) - HelixClaw's anti-loop story is weaker.
- **Grammar-constrained tool-args probe** + **`agent_eval` regression harness** (`agent_eval.go`) - a
  real test spine HelixClaw lacks.
- **Deterministic terminal/tool state machine** (`tool_execution_state.go`) - arguably more disciplined
  than HelixClaw here.
  Audit correction 2026-07-10: the state model is strong, but shell-session fields are not currently
  synchronized. Treat this advantage as provisional until MAULER-AR-001 in
  `agentic-reliability-issues-2026-07.md` closes under the race detector.
- **Subagent contracts with explicit turn / tool / output / context budgets** (`subagents.go`).

These are why the port is *selective*: back-port HelixClaw's correctness/reliability spine without
losing TheMauler's context-management and anti-loop advantages.

---

## Recommended order (per roadmap section U16-U23)

1. **U16** session-structure repair - biggest reliability ROI, self-contained, offline-testable.
2. **U20** permission classes - cheap substrate.
3. **U18** structural write-guards - high safety, low risk.
4. **U19** role-scoped tool sets - builds on U20.
5. **U17** verification gates - correctness gate for coding runs.
6. **U21** plan->review completion rails - advisory, then blocking.
7. **U22** experience learning, then **U23** task-DAG - deeper builds, only once the reliability spine
   is green (a DAG over an unreliable step executor just fails in parallel).
