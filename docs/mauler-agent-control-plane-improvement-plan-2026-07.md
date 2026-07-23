# Mauler Agent Control Plane Improvement Plan

Status: implementation in progress; first M1/M2 narrow path landed  
Date: 2026-07-14  
Scope: TheMauler only; Go backend and React/TypeScript UI  
Related: `docs/agentic-reliability-issues-2026-07.md`, `docs/shiftgrid-engagement-grid-integration-plan-2026-07.md`

## Implementation update - 2026-07-15

The first native control-plane slice is now implemented:

- `internal/controlplane` owns a schema-versioned `TaskContract`, SHA-256 contract sealing,
  validation, immutable instruction revisions, parent-digest linkage, and an executable typed phase
  machine;
- task contracts capture the objective, deliverables, workspace mutation root, protected resources,
  acceptance checks, required evidence kinds, risk, plan policy, approval policy, completion policy,
  and run budgets;
- SQLite schema v12 stores contract JSON/digest and control phase/state with each task run; the same
  snapshot survives run checkpoints and is persisted when the contract is initialized, not only at
  final log write;
- `TaskRun.State` remains descriptive UI telemetry. The new control phase is a separate source of
  truth and emits `control_transition` RunLedger/task-run events;
- plan-required runs reject `write`, `edit`, and `shell` until `todo_write action=replace` succeeds
  with non-empty steps. `read` remains legal during planning;
- the narrow `read`/`write`/`edit`/`shell` path now records action, observation, failure/repair,
  approval, verification, completion, block, cancellation, and failure transitions;
- successful mutations receive content-addressed `file_sha256` evidence IDs even when ordinary tool
  input logging is disabled;
- project verification reuses the deterministic verify gate and receives immutable command-verdict
  evidence IDs. Assistant prose cannot satisfy a blocking contract check;
- a run cannot be saved as `done` while its control phase is non-terminal or while blocking checks
  remain unsatisfied; and
- Logs shows contract revision, risk, plan status, control phase, blocking checks, evidence counts,
  objective, and the sealed digest.

Verification on 2026-07-15 passes: `go test ./... -count=1`, `go vet ./...`,
`go test -race ./internal/app ./internal/tools -count=1`,
`go test -race ./internal/controlplane ./internal/store -count=1`, the standalone frontend
production build, and production `wails build`. A real local-model desktop smoke is still required
before calling this slice live-hardened.

Still open in this programme:

1. M0 repeated-run baseline, scorecard, and pass^k reporting;
2. live user steering wired to `ReviseTaskContract`/`MachineState.Rebase` rather than beginning an
   unrelated revision-1 run;
3. richer planner-proposed deliverable/constraint/acceptance extraction with code validation;
4. complete M3 policy metadata for every compact tool (the executable gate is deliberately limited
   to `read`, `write`, `edit`, and `shell` in this slice);
5. central M4 failure classification/recovery budgets; and
6. broader M5-M9 acceptance, context, information-flow, chaos, and minimal operator controls.

## Executive decision

Mauler should keep instructions and operating guidance in prompts, but move every rule that must be reliable into a native Go control plane.

The model should choose useful actions from a set of valid actions. It should not be responsible for remembering permissions, deciding whether its own work is proven, managing retry budgets, or declaring a run complete without external verification.

The target split is:

| Layer | Owns |
| --- | --- |
| Prompt | Goal, domain guidance, heuristics, preferred style |
| Control plane | Legal phases, scope, permissions, budgets, retries, approvals |
| Tool policy | Schemas, side effects, preconditions, postconditions, idempotency |
| Verifiers | Deterministic acceptance checks and evidence requirements |
| Ledger | Durable events, decisions, evidence provenance, recovery history |
| Model | Reasoning and selecting among currently valid actions |

This is an evolution of Mauler's existing run loop, RunLedger, tool registry, Engagement Grid, reviewer pass, and verification gates. It is not a second agent framework and must not add Python or a sidecar service.

## Why this direction

Current production guidance and agent research converge on a few practical lessons:

1. Prefer simple, composable workflows over unconstrained autonomy.
2. Treat context as a compiled working set, not an ever-growing transcript.
3. Make state durable so interruption and resumption are normal operations.
4. Put high-risk authorization at the tool boundary.
5. Separate an agent's proposed result from the system's verified result.
6. Evaluate reliability across repeated and perturbed runs, not only one successful demo.
7. Detect source-to-sink risk: untrusted content matters most when it can influence a consequential action.

Prompts remain valuable, but a prompt instruction is not an enforcement boundary.

## Existing Mauler foundations to preserve

Mauler is already ahead of a blank-slate agent runtime. Preserve and consolidate these assets:

- hard tool, web, browser, and context budgets;
- RunLedger and task-run timelines;
- run checkpoints and workspace-authoritative context;
- duplicate read/command loop detection;
- terminal execution state and shared-terminal hygiene;
- tool confirmations, exact-input safe lists, and toolsets;
- scope checks for Engagement targets and endpoints;
- mutation snapshots, verification, and rollback;
- deterministic completion and verification rails;
- a fresh-context reviewer using a thinking sibling;
- bounded subagents with distinct claimant identity;
- promptware and secret redaction on tool results;
- durable Engagement claims, evidence, findings, and revision guards.

Relevant implementation areas include:

- `internal/app/app.go`
- `internal/app/review_phase.go`
- `internal/app/verify_gate.go`
- `internal/app/completion_rails.go`
- `internal/app/tool_execution_state.go`
- `internal/app/run_checkpoint.go`
- `internal/app/guardrails.go`
- `internal/app/agent_loop_stability.go`
- `internal/tools/registry.go`
- `internal/ledger`
- `internal/engagement`

## Reliability gaps to close

### 1. Planning is still advisory

`RequirePlan` currently changes prompt instructions. A model can ignore it or produce narration that looks like a plan.

Required change: planning becomes a control-plane phase. Work cannot enter `acting` until a valid task contract and plan exist when the selected policy requires them.

### 2. Run states are descriptive rather than authoritative

The current states are useful for logs and UI, but the main loop still decides behavior through distributed branches and heuristics.

Required change: a typed finite-state machine must reject illegal transitions and expose the valid next actions for each phase.

### 3. Recovery logic is distributed

Malformed JSON, no-tool narration, truncation, stale paths, terminal failures, and repeated actions are handled in multiple places inside a large loop.

Required change: classify failures once and route them through one bounded recovery policy.

### 4. Completion inference is partly heuristic

Text patterns and extracted features help, but are not strong enough to own terminal state.

Required change: the model may propose completion. Only acceptance checks backed by evidence can commit `complete`.

### 5. Untrusted data is labelled but not tracked end to end

Current guardrails identify suspicious tool output and redact obvious secrets. They do not yet track whether untrusted content influenced a later outbound or destructive action.

Required change: attach provenance, trust, and sensitivity labels to observations and enforce source-to-sink policies at consequential tools.

### 6. Evaluation overweights single runs

Existing tests are strong regression gates, but agent reliability also needs repeated runs, prompt paraphrases, injected tool faults, interruption/resume tests, and end-state scoring.

Required change: build a native reliability harness and report pass^k-style consistency in addition to pass@1.

## Target architecture

### A. Versioned task contract

Introduce `internal/controlplane` with a durable `TaskContract` resembling:

```go
type TaskContract struct {
    Version            int
    RunID              string
    Goal               string
    WorkspaceRoot      string
    AllowedTargets     []ScopeTarget
    AllowedPaths       []PathRule
    RequiredArtifacts  []ArtifactRequirement
    AcceptanceChecks   []AcceptanceCheck
    ToolPolicy         string
    RiskPolicy         string
    Budgets            RunBudgets
    ApprovalPolicy     ApprovalPolicy
    CompletionPolicy   CompletionPolicy
}
```

Properties:

- generated from user intent and selected project/profile;
- validated by code before execution;
- immutable after starting, except through a versioned amendment event;
- persisted with the task run and checkpoint;
- compact enough to re-inject after context compaction;
- linked to Engagement scope and pinned pack snapshots when a Grid is active.

### B. Executable run-phase state machine

Use explicit phases:

```text
intake -> planning -> acting -> observing -> verifying -> complete
                          |          |          |
                          +------> repairing <--+

any active phase -> awaiting_approval | blocked | cancelled | failed
```

Each transition must specify:

- triggering event;
- required preconditions;
- state mutation;
- ledger event;
- valid next actions;
- timeout and budget effect;
- recovery route on failure.

UI state should be a projection of this machine, not a separate source of truth.

### C. Tool policy registry

Extend the compact tool registry with code-owned metadata:

```go
type ToolPolicy struct {
    Name            string
    Risk            RiskLevel
    SideEffects     SideEffectClass
    Idempotency     IdempotencyClass
    AllowedPhases   []RunPhase
    Preconditions   []Precondition
    Postconditions  []Postcondition
    Timeout         time.Duration
    RetryPolicy     RetryPolicy
    RequiredScopes  []ScopeKind
    RequiredTrust   TrustRule
    Verifier        string
}
```

This should centralize facts that are currently split across prompts, confirmation code, tool execution, and verification helpers.

Important rules:

- retries of non-idempotent actions require an idempotency key or explicit approval;
- a tool cannot execute outside its allowed phase;
- shell/browser/network targets must pass authoritative scope resolution immediately before execution;
- mutating tools must produce a postcondition result;
- approval applies to the exact normalized action, not a broad model intention.

### D. Central failure classifier and recovery controller

Use stable classes such as:

- `transport_failure`
- `model_truncation`
- `invalid_action_schema`
- `wrong_tool`
- `tool_precondition_failed`
- `tool_execution_failed`
- `tool_postcondition_failed`
- `stale_context`
- `scope_denied`
- `approval_denied`
- `conflicting_evidence`
- `verification_failed`
- `no_progress`
- `budget_exhausted`

Each class gets a bounded response: retry, repair action, re-observe, re-plan, request approval, block, or fail. Generic “try again” prompts should become a last resort.

Every retry must record the failure class, attempt number, budget cost, chosen recovery, and outcome.

### E. Evidence-owned completion

Create an acceptance evaluator that consumes structured evidence references rather than model assertions.

An acceptance check should declare:

- what must be true;
- how it can be observed;
- which verifier owns the decision;
- freshness requirements;
- whether human approval can waive it;
- which evidence records support the result.

Examples:

- file exists and its digest matches the written content;
- `go test ./...` exited zero after the final mutation;
- requested UI element is visible in a browser-backed screenshot;
- Engagement finding has reproducible raw evidence and required scope provenance;
- target action remained within the locked project scope.

The reviewer model can identify missing evidence and suggest checks. It cannot mark its own suggestion as proven.

### F. Phase-specific context compiler

Replace broad transcript replay with a deterministic context packet containing only:

- task contract;
- current phase and valid next actions;
- current plan step;
- recent relevant observations;
- unresolved failures;
- evidence index;
- applicable memory/skill excerpts;
- remaining budgets;
- exact tool schemas available in this phase.

The compiler should preserve provenance and freshness and record the packet digest in RunLedger. Compaction then becomes a normal compilation step rather than free-form summarization.

### G. Trust and information-flow controls

Represent observations with:

```go
type DataLabel struct {
    Origin      string
    Trust       TrustLevel
    Sensitivity SensitivityLevel
    RunID       string
    EvidenceID  string
}
```

Enforce policies at sinks:

- untrusted web/page content cannot silently authorize shell, write, credential use, or outbound messaging;
- secrets may be used for an explicitly authorized local target but may not be copied to unrelated network origins;
- Telegram/browser instructions remain data unless the user or a trusted control message authorizes an action;
- sanitization creates a derived observation and preserves the original provenance link.

This must be policy-based rather than a blanket ban, because Mauler legitimately handles credentials and hostile content during authorized security work.

### H. Reliability evaluation harness

Add a Go-native harness under `internal/eval` or `internal/reliability` with fixture targets and deterministic fake providers/tools.

Required suites:

1. repeated-run consistency;
2. prompt paraphrase and irrelevant-context perturbation;
3. malformed/truncated model actions;
4. transient and permanent tool failures;
5. timeout and cancellation during each phase;
6. checkpoint crash/restart/resume;
7. duplicate and non-idempotent action prevention;
8. prompt-injection source-to-sink attempts;
9. scope bypass through redirects, aliases, shell indirection, and browser navigation;
10. false-completion attempts without evidence;
11. reviewer disagreement and inconclusive verification;
12. parallel Engagement claim conflict and stale revisions.

Report end-state correctness, pass@1, pass^k, recovery rate, silent-failure rate, unauthorized-side-effect count, duplicate-action count, and median tool/model cost.

## Implementation roadmap

### M0 - Baseline and freeze the metrics

Deliverables:

- record current fixture pass@1 and repeated-run results;
- add ledger queries for retries, duplicate actions, verification failures, and terminal stop reasons;
- define the first reliability scorecard;
- keep the current race, vet, frontend, and production-build gates green.

Exit condition: improvements can be compared against a reproducible baseline.

### M1 - Task contract

Status: first implementation slice landed 2026-07-15. Contract construction, validation, sealing,
task-run/checkpoint persistence, and compact Logs projection are wired. Live user steering still
needs to create and activate a linked contract revision.

Deliverables:

- add `internal/controlplane/contract.go` and validation tests;
- derive contracts for ordinary workspace runs and Engagement runs;
- persist contract version and digest with task runs/checkpoints;
- show a compact contract summary in Run and Logs.

Exit condition: every new run has a validated, durable contract.

### M2 - Authoritative phase machine

Status: first narrow runtime path landed 2026-07-15. Legal transitions, plan-before-mutation,
approval, action/observation, repair, controller-owned verification, evidence-gated completion, and
terminal-state enforcement are active for `read`, `write`, `edit`, and `shell`. Expand tool coverage
through M3 metadata rather than adding more name-based branches.

Deliverables:

- add typed events and legal transitions;
- make `RequirePlan` an enforced transition policy;
- project phase state into existing run-state events and UI;
- deny tools that are invalid for the current phase;
- retain compatibility translation for existing checkpoints.

Exit condition: illegal transitions and premature completion are impossible through normal runtime paths.

### M3 - Tool policy consolidation

Deliverables:

- attach risk, scope, idempotency, preconditions, and verifier metadata to every model-facing tool;
- remove duplicated prompt-only tool rules where code now owns them;
- normalize exact actions before confirmation and safe-list matching;
- add policy conformance tests for every registered tool.

Exit condition: all compact tools have complete policy metadata and executable gates.

### M4 - Classified recovery

Deliverables:

- introduce `FailureClass`, `RecoveryDecision`, and retry accounting;
- migrate malformed action, truncation, no-progress, stale-path, and terminal repair branches;
- add circuit breakers for repeating failure/action pairs;
- surface recovery decisions in Logs and Brain.

Exit condition: every retry is classified, bounded, and inspectable.

### M5 - Acceptance evaluator

Deliverables:

- introduce typed acceptance checks and evidence references;
- make completion a controller decision;
- integrate mutation verification, tests, browser screenshots, reviewer findings, and Engagement evidence;
- treat missing or conflicting proof as `repairing`, `blocked`, or `inconclusive`.

Exit condition: no run can reach `complete` without satisfying its blocking checks or recording an explicit human waiver.

### M6 - Context compiler

Deliverables:

- compile phase-specific packets with stable schemas and digests;
- prioritize contract, active step, unresolved failures, and evidence;
- bound memory, skill, and ledger excerpts;
- add token, relevance, stale-context, and resume-equivalence tests.

Exit condition: resumed and compacted runs retain the same control facts without replaying irrelevant history.

### M7 - Source-to-sink guardrails

Deliverables:

- label observations by origin, trust, and sensitivity;
- propagate labels through summaries and tool arguments;
- gate network, message, shell, write, and credential sinks;
- add hostile-page, malicious-file, Telegram, and tool-output fixtures.

Exit condition: untrusted content cannot create an unauthorized consequential action, while authorized pentest workflows remain usable.

### M8 - Reliability and chaos gate

Deliverables:

- run repeated and perturbed scenarios in CI/local benchmark mode;
- inject provider, tool, network, disk, browser, and interruption faults;
- add pass^k and silent-failure thresholds;
- export a machine-readable reliability report and link failed runs to RunLedger.

Exit condition: releases fail on reliability regressions, not only compilation or unit-test regressions.

### M9 - Control-plane UI

Only after live use identifies the minimum useful controls:

- show contract, current phase, active acceptance checks, remaining budgets, and recovery reason;
- distinguish model proposals from verified facts;
- show why an action is blocked and the exact approval or evidence needed;
- avoid another broad cockpit redesign.

Exit condition: operators can understand and steer a run without reading raw prompts or the complete ledger.

## First implementation slice

Started 2026-07-15. M1 and the narrow M2 runtime path below have landed and the static/race/production
gates pass; M0 repeated-run metrics and live-smoke evidence remain:

1. ordinary file-change task;
2. enforced plan;
3. one file mutation;
4. one deterministic verification command;
5. completion only after verified evidence;
6. interruption and checkpoint resume.

Do not migrate all tools at once. Prove the state machine with `read`, `write`, `edit`, and `shell`, then expand policy coverage tool by tool.

## Definition of improved

The programme is successful when:

- unauthorized side effects are zero in the reliability suite;
- out-of-scope actions are denied at the sink, including redirects and indirection;
- every terminal `complete` state references satisfied acceptance checks;
- every retry has a failure class and bounded recovery decision;
- non-idempotent duplicate actions are prevented;
- interruption/resume produces an equivalent valid outcome;
- high-risk actions have traceable authorization and normalized arguments;
- the agent maintains a strong repeated-run pass^k score, not only a good demo pass@1;
- operators can distinguish claims, observations, evidence, and verified conclusions.

## Non-goals and constraints

- No Python runtime, LangGraph dependency, or separate orchestration service.
- No replacement of RunLedger, Engagement state, or the compact tool registry with parallel systems.
- No silent promotion of model output into trusted memory, evidence, or completion.
- No claim that prompt injection can be solved only by keyword filtering.
- No global retry loop that treats all failures as equivalent.
- No UI redesign before runtime behavior is proven through live and fixture tests.

## Research references

Primary production guidance:

- Anthropic, [Building effective agents](https://www.anthropic.com/engineering/building-effective-agents)
- Anthropic, [Effective harnesses for long-running agents](https://www.anthropic.com/engineering/effective-harnesses-for-long-running-agents)
- Anthropic, [Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
- OpenAI, [Designing agents to resist prompt injection](https://openai.com/index/designing-agents-to-resist-prompt-injection/)
- OpenAI, [How we monitor internal coding agents for misalignment](https://openai.com/index/how-we-monitor-internal-coding-agents-misalignment/)
- OpenAI Agents SDK, [Tool guardrails](https://openai.github.io/openai-agents-python/ref/tool_guardrails/)
- OpenAI Agents SDK, [Tracing](https://openai.github.io/openai-agents-python/tracing/)
- OpenAI Agents SDK, [Running agents](https://openai.github.io/openai-agents-python/running_agents/)
- LangGraph, [Interrupts](https://docs.langchain.com/oss/python/langgraph/interrupts)
- LangGraph, [Persistence](https://docs.langchain.com/oss/javascript/langgraph/persistence)

Research and evaluation references:

- [Reliability is what matters in agentic AI](https://arxiv.org/abs/2602.16666)
- [ReliabilityBench](https://arxiv.org/abs/2601.06112)
- [ToolBench-X](https://arxiv.org/abs/2606.25819)
- [Self-healing orchestrators for LLM agents](https://arxiv.org/abs/2606.01416)
- [A survey on self-correction in large language models](https://arxiv.org/abs/2406.01297)
- [AgentDojo](https://arxiv.org/abs/2406.13352)
- [SWE-agent: Agent-computer interfaces enable automated software engineering](https://arxiv.org/abs/2405.15793)
- [tau2-bench](https://arxiv.org/abs/2506.07982)

The 2026 benchmark and self-healing papers above are useful emerging evidence, but some are preprints and controlled evaluations. Their patterns should be validated against Mauler's own workloads before they become release claims.
