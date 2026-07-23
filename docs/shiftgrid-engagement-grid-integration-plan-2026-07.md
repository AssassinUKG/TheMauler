# Shiftgrid-inspired Engagement Grid for TheMauler

Date: 2026-07-13

Status: implementation in progress. The Go-native definition/state-machine and check-pack curation
foundation, SQLite snapshot store/shared service, compact model tool, bounded prompt packet, Wails
bindings, Home resume card, first React operator page, evidence/finding gates, and the first
structured-network scope guard landed on 2026-07-13. Stable desktop/Telegram claimant provenance,
focused bounded-task assignments, and native engagement JSON export/import are also in place.
Revision-safe claim heartbeats, clean-exit/manual release, Telegram progress edits, and live lease
health are also wired. Deterministic parallel item selection, fixture-backed native detector checks,
and a native end-to-end Engagement Agent Eval are also wired. A six-step guided setup wizard now
previews immutable scope, discovers candidate workspace artifacts by path, checks provider/target
readiness, creates the Grid, and prepares an editable first-run Chat draft. The operator page follows the
source UI's project mini-app shape with persistent Workflow, Checklist, Endpoints, Notes, Findings,
and Evidence views. Project notes are revision-guarded SQLite state shared through the compact
tool and a bounded run-packet excerpt. A first-class Go-native Pack Library now merges embedded,
personal, and project workflow/checklist versions for new Grids while preserving existing Grid
snapshots. It supports strict import/export, local-trust cloning, archive/restore, quality review,
collision rejection, and malformed-file quarantine through **Bench > Pack Library**. Shell/browser scope handling,
independent final review, and live evaluation remain.

Operator guide: [Engagement Grid operator guide](engagement-grid-operator-guide.md).

Current delivery stage: **functional beta / onboarding and live-hardening**. The authoritative Go
runtime, SQLite state, agent tool, project-scoped Grid UI, evidence/finding gates, claimant lanes,
portable export/import, fixture checks, deterministic lifecycle evaluation, and guided first-run
onboarding are wired. Next, finish the remaining browser/shell scope guards, repeat/result controls,
independent final review, and authorised live smoke tests.

Source reviewed: [BuFuuu/shiftgrid](https://github.com/BuFuuu/shiftgrid) at commit
`953e23e9212fe86532f9c2a8dbffa46546fb9836` (2026-07-12). Shiftgrid is MIT licensed. If its
workflow/checklist definitions or code are copied, retain its copyright and license notice in the
repository's third-party notices.

## Decision

Build a **native Mauler Engagement Grid**, then add a bundled procedural skill and file-format
compatibility for Shiftgrid JSON.

This implementation is Go plus React/TypeScript only. Do not add or launch the external Shiftgrid
Python/Docker service, and do not implement this as only a skill.

The split should be:

- **Native Go domain/service:** authoritative project workflow, scope, claims, observations,
  endpoints, checks, findings, evidence links, and phase gates.
- **SQLite plus RunLedger:** durable project-scoped state and the existing event/evidence spine.
- **One compact model-facing tool:** the agent reads and updates the grid through one orthogonal
  capability instead of learning a large REST API.
- **Bundled skill/workflow packs:** procedural pentest guidance and editable JSON definitions.
- **React operator page:** human-owned scope and workflow oversight using the same Go service.
- **Compatibility layer:** read/write compatible JSON files directly from Go. No Python process or
  HTTP sidecar.

This keeps Mauler as the execution runtime and adopts the part Shiftgrid does especially well: an
enforced claim -> act -> observe -> evidence -> finish -> advance loop.

## Current implementation status

### Stage snapshot

| Area | Stage | Remaining work |
| --- | --- | --- |
| Native domain, SQLite, transitions, claims | Landed | Keep race/stability gates green |
| Agent tool and bounded prompt packet | Landed | Live-observe tool behavior during real runs |
| Project/Home/Grid operator UI | Functional beta | Live-use polish and operator result/repeat controls |
| Evidence, findings, structured HTTP scope | Mostly landed | Browser/shell scope coverage and independent final review |
| Parallel task and Telegram claimant lanes | Landed | Live multi-lane smoke testing |
| JSON compatibility and deterministic eval | Landed | Durable existing-artifact indexing and authorised live fixture/HTB runs |
| Pack Library and active definition catalog | First management slice landed | Form/JSON editor, history/diffs, Grid migration preview, WordPress pack, and pinned Nuclei adapter |

The implementation is therefore past the architecture/prototype stage. It is usable for controlled
testing, but it is not yet the finished default workflow for a first-time operator.

Landed so far:

- `internal/engagement` Go package with Shiftgrid-compatible workflow/checklist types;
- validation for ids, phase/step/check uniqueness, phase kinds, scopes, run counts, checklist
  references, and global/per-endpoint bundle requirements;
- embedded copies of the reviewed seven-phase workflow and 20-check checklist;
- claim-before-write semantics, expiring leases, one active claim per claimant, observation limits,
  optimistic item revisions, terminal-status validation, repeat runs with preserved observation
  history, ordered step/global-check routing, and blocked incomplete phase advance;
- MIT attribution in `THIRD_PARTY_NOTICES.md`;
- unit and race tests for importer/validation and the initial state machine.
- definition schema v2 with pinned version/source/license/trust metadata and calculated local
  SHA-256 digests while preserving legacy Shiftgrid JSON compatibility;
- a Go-native check-pack foundation with an allowlisted upstream registry, target applicability
  selection, editorial quality scoring, and item-level version diffs.
- SQLite schema v11 with project-scoped engagement rows that snapshot exact workflow/checklist JSON,
  versions, and calculated digests alongside optimistic-revision state, plus persisted claimant
  id/alias/origin provenance on task runs;
- a shared Go service for create/list/get/now/claim/observe/finish/advance/delete operations with
  engagement RunLedger producers;
- deterministic endpoint creation/grouping and per-endpoint check materialisation, including the
  endpoint-phase setup/check/summary gate and blocked empty endpoint coverage;
- reopen/resume tests, stale-write and silent-pack-replacement rejection, migration coverage, and
  focused race coverage for the new store/service/check-pack code.
- a compact production `engagement` model tool with strict `list/create/now/status/claim/observe/
  finish/advance/add_endpoint/group_endpoint` actions and run-derived claimant identity;
- authoritative scope creation from the active project target/hostname, with model-selected
  out-of-scope entries rejected and scope locked on creation;
- a fresh bounded prompt packet containing pinned identity, progress, locked scope, and the exact
  next action without observations, evidence bodies, or full checklist definitions;
- project-scoped Wails bindings plus regenerated Wails model mirrors;
- a first-class Grid navigation/page for create/resume, phase/progress/claim oversight, endpoint
  creation/grouping, and a Home card that creates or resumes the active project's grid;
- a reusable six-step guided setup wizard on Home and Grid that previews immutable project scope,
  discovers bounded workspace artifact candidates without reading their contents, runs live
  provider/target readiness checks, creates the Grid, and prepares (but never auto-sends) the first
  Chat run prompt;
- container-aware Grid layout behavior verified at both wide and side-pane-constrained widths.
- durable evidence references with RunLedger/file provenance, workspace-local SHA-256 and size,
  source kind, run number, claimant, and explicit agent-composed classification; raw bodies remain
  in their original artifact or ledger event rather than being copied into engagement state;
- deterministic current-run evidence requirements, including a fail-closed vulnerable-result gate
  and support for per-check minimum/source-kind policies;
- draft/confirmed finding state with optimistic revisions, reproducibility and raw-evidence gates,
  plus screenshot-or-explicit-operator-waiver requirements for medium/high/critical findings;
- compact `add_evidence/list_evidence/upsert_finding/confirm_finding/list_findings` actions and
  evidence/finding oversight and operator controls on the shared Grid page; and
- exact host/IP/CIDR/port/URL-prefix matching at endpoint creation and the `http_probe` boundary,
  with every allowed/denied structured scope decision mirrored to RunLedger;
- stable claimant identities for desktop and channel-started runs, including Telegram message/user
  attribution persisted with task-run history;
- bounded `task` assignments that validate one exact unfinished engagement work reference, give the
  child agent only the focused item plus bounded locked-scope context, and execute engagement writes
  under a distinct child claimant id; and
- schema-versioned native engagement JSON export/import with pinned definition-digest checks,
  workspace-relative evidence paths, path-escape rejection, duplicate-id protection, stale-claim
  clearing, service tests, Wails bindings, and Grid copy/paste controls; and
- background claim heartbeats for desktop/channel and bounded-task runs that extend the lease
  without invalidating the model's work revision, retry snapshot conflicts, release unfinished work
  on clean exit, and leave crash recovery to expiry. Grid shows the claimant lane, last heartbeat,
  live countdown, and an operator release action; Logs shows persisted run origin/claimant details;
- a deterministic available-work queue that keeps setup/summary gates sequential while returning
  stable ordered global/endpoint checks for parallel claimants; the compact `engagement available`
  action, bounded-task validator, Wails binding, and Grid queue consume the same domain rule;
- an inert Go-native HTTP fixture harness with pinned adapters and positive/negative controls. The
  security-header and cookie-flag checks are the first fixture-verified checks, mapped to current
  OWASP WSTG procedures. Missing controls/adapters fail closed and detector output remains candidate
  evidence rather than an automatically confirmed finding; and
- a Benchmarks > Run Engagement Eval scenario that deterministically exercises SQLite create/now/
  claim, raw RunLedger HTTP evidence, unsupported-finding rejection, restart/resume, endpoint/global
  completion, locked-scope denial, and native JSON export/import parity without a model or network; and
- a source-inspired operator information architecture inside Mauler's dark cockpit: the phase rail
  remains visible across dedicated Workflow, grouped Checklist, expandable grouped Endpoints,
  Notes, Findings, and Evidence views. Notes use their own optimistic revision, survive native
  export/import, expose `get_notes`/`set_notes` through the existing compact tool, and contribute
  only a bounded excerpt to fresh model context.

Deliberately not wired yet:

- endpoint repeat-count editing, operator edits for work results, browser target-action scope
  guards, and best-effort shell target inspection;
- the allowlisted remote pack download/update/approval UI, independent final review, and authorised
  live fixture/HTB smoke tests.

Verification after the parallel queue, fixture, and native-evaluation slice: `go test ./...
-count=1`, `go vet ./...`,
`npm run --prefix frontend build`, `go test -race ./internal/engagement/... ./internal/store
-count=1`, `go test -race ./internal/app -count=1`, `go test -race ./internal/tools -count=1`, and
the production `wails build` pass on 2026-07-13. The app/tools race packages are run separately on Windows because
their concurrent WSL probes can serialize long enough to exceed the combined wrapper timeout.

## What Shiftgrid adds

Shiftgrid is not another shell, exploit framework, or LLM loop. Its README describes it as a prompt
engine for agentic pentesting with human oversight. Its core is a workflow/checklist state machine
shared by an agent API and an operator UI.

The useful contracts found in the reviewed source are:

1. JSON workflow definitions containing ordered phases and steps.
2. Checklists split into global checks and checks repeated for every endpoint.
3. A mandatory focus/claim before an agent can write a result.
4. Short observations attached to the exact step/check/endpoint that produced them.
5. Raw evidence attached separately from model-authored summaries.
6. Linear phase gates, with flexible endpoint/checklist edits inside the engagement.
7. A project-wide notes page for a compact holistic risk model.
8. Findings written during testing, not reconstructed only at the end.
9. Repeat counts ("try harder") for phases, checks, and endpoints.
10. Agent identity and read-before-overwrite guards for concurrent work.
11. Operator-owned, optionally locked scope.
12. Resume from a deterministic `now`/next-action view instead of asking the model to infer state
    from chat history.

The default reviewed workflow has seven phases: reconnaissance, free reconnaissance, global
checks, endpoint testing, free testing, additional work/false-positive review, and finding writing.
The default checklist has 20 checks: 12 global and 8 per-endpoint.

## Why a skill alone is insufficient

A Mauler skill can teach an agent *how* to test, but it cannot reliably enforce or coordinate:

- scope locking;
- one claimed work item per agent;
- legal state transitions;
- evidence requirements;
- optimistic-concurrency checks;
- endpoint-by-endpoint coverage;
- multi-run reset behavior;
- consistent resume after interruption or a different agent takes over;
- a human and an agent editing the same authoritative state.

Use a skill for tactics and tool-routing guidance. Put workflow truth and completion gates in Go.

## Why not run Shiftgrid beside Mauler by default

The external service would introduce Python, Flask/FastAPI, Docker, two more localhost ports, a
second project store, a second UI, and process lifecycle/health management. Shiftgrid deliberately
has no login and relies on binding its UI/API to `127.0.0.1`, so embedding it would also add a local
security boundary Mauler does not need. It will not be part of this integration. Interoperability is
limited to Go-native JSON import/export.

## Target user experience

### New project

When creating an HTB/CTF or Pentesting project:

1. Choose a workflow pack: `HTB machine`, `Web application pentest`, or `Manual/lightweight`.
2. Enter the authorised scope and lock it.
3. Mauler creates the workspace and native engagement state.
4. Home shows `Continue engagement` with the current phase, current step, progress, and last
   evidence.
5. A Run starts with a compact current-state packet. The agent calls `engagement action=now` for
   full detail, claims a unit, performs real work with normal Mauler tools, records the result and
   evidence, finishes it, and continues.

### Existing project

Existing LabProfiles and workspaces must remain valid. Add `Start engagement grid` as an explicit,
non-destructive action that:

- selects a workflow pack;
- seeds scope from target/hostname;
- indexes existing scans, notes, screenshots, and reports as candidate evidence;
- does not mark any workflow item complete automatically;
- lets the operator confirm imported endpoints and evidence before the first run.

After a beta period, make Engagement Grid the default for newly created Pentesting/HTB projects,
while leaving existing projects opt-in.

### Operator UI

Add one full-page `Grid`/`Engagement` view without redesigning the rest of the cockpit:

- phase rail with progress and gates;
- current-step card with claim owner and next action;
- global checklist grouped by category;
- endpoint groups with per-endpoint check progress;
- findings with severity, status, and evidence readiness;
- compact project notes/risk model;
- evidence links that open existing files, artifacts, screenshots, or RunLedger events;
- operator controls for scope lock, skip/not-applicable reasons, reset, repeat count, and release
  stale claims.

Home gets only the compact resume card. Run remains the agent conversation/execution surface. Brain
remains the event/debug view. Do not duplicate Terminal, AI Commands, Memory, or Logs inside Grid.

## Mauler concept mapping

| Shiftgrid concept | Mauler implementation |
|---|---|
| Project | Existing `LabProfile` plus one project-scoped `Engagement` record |
| Workflow JSON | Versioned workflow packs under `internal/engagement/workflows` or embedded assets |
| Checklist JSON | Versioned checklist packs under `internal/engagement/checklists` or embedded assets |
| Current phase/step | Engagement state machine, injected as a compact run-start packet |
| Agent API | One native `engagement` model tool plus Wails bindings |
| Operator web UI | React center page using the same Go service |
| Agent id/alias | Main run id, channel id, or bounded-task id plus a display alias |
| Focus | Transactional claim with owner and expiry/heartbeat |
| Observations | Short, versioned records on a workflow step/check/endpoint |
| Notes | Project-wide risk/context note with edit semantics and revision |
| Raw captures | Links to RunLedger events and workspace artifacts, with hash/provenance |
| Endpoints | Structured engagement endpoints, grouped by feature/host/service |
| Findings | Structured findings linked to checks/endpoints/evidence |
| Runs/try harder | Required run count and completed run count on phases/checks/endpoints |
| Scope lock | Operator-owned canonical target set plus network-tool guard |
| Recent/next | Deterministic service query, not inferred from chat text |

## Data and persistence design

Use `~/.config/mauler/state.db` as the authoritative transactional store, keyed by canonical
workspace and LabProfile id. Do not overload the existing global todo list; todo plans are for one
agent run, while engagement state must survive chats, agents, restarts, and weeks of work.

Suggested tables:

- `engagements`: id, workspace scope, lab profile id, workflow id/version, current phase, status,
  locked scope, settings, timestamps, revision.
- `engagement_steps`: engagement id, phase/step ids, status, observation, runs, attribution,
  timestamps, revision.
- `engagement_checks`: copied/version-pinned check definitions for that engagement.
- `engagement_check_results`: global or endpoint-scoped status, observation, runs, attribution,
  timestamps, revision.
- `engagement_endpoints`: stable address/name, kind, group, status, observation, runs, attribution.
- `engagement_findings`: title, severity, description, impact, recommendation, confidence/state,
  timestamps.
- `engagement_evidence`: target kind/id, RunLedger event id and/or artifact path, source type, hash,
  size, model-authored flag, description.
- `engagement_claims`: target kind/id, claimant, acquired/heartbeat/expiry times.
- `engagement_notes`: body, revision, updated-by, updated-at.

Every mutation runs in a SQLite transaction and emits a RunLedger event such as
`engagement_claim`, `engagement_observation`, `engagement_evidence`, `engagement_finding`,
`engagement_finish`, or `engagement_advance`.

Provide JSON export/import for portability and recovery. Export is a snapshot, not a second live
source of truth. Workflow/checklist definitions record their source id, version/hash, and license
attribution so a later definition edit cannot silently rewrite an active engagement.

## Claim and concurrency contract

Port Shiftgrid's claim-before-write rule, with one Mauler improvement: expiring leases.

- A main run, Telegram run, or bounded task gets a stable claimant id.
- One claimant can hold one workflow step, one global check, or one endpoint at a time.
- Result writes require the matching live claim.
- Claims heartbeat while a run is active and expire after interruption/crash.
- The operator can release a stale claim.
- Observations and notes use a revision or exact-old-value guard; a conflicting write returns the
  current value and requires a re-read/merge.
- Claims prevent accidental duplicate work, not deliberate collaboration. The operator can assign
  a second run explicitly if needed.

## Model-facing tool contract

Register one app-level tool named `engagement`. Keep its schema strict and action-based. A first
implementation can expose:

```text
now
claim
record
finish
advance
add_endpoint
group_endpoint
upsert_finding
edit_notes
list
```

Common selectors are `kind`, `id`, and optional `endpoint_id`. `record` accepts a precise terminal
status, a short observation, and evidence references/paths. The service validates which fields are
legal for each target kind.

Do not expose a REST-shaped collection of workflow/check/finding tools to the model. The current
compact-tool target is important for local Qwen reliability. In Pentesting/HTB runs, substitute
`engagement` for `todo_write` in the routed tool set when a grid is active, so the per-turn tool
count does not grow. General coding runs continue to receive `todo_write`.

The tool result starts with a compact structured header, for example:

```text
state=focused phase=reconnaissance item=quick_port_scan next=act
```

followed by a bounded human-readable detail and explicit next legal actions.

## Prompt and skill design

When a grid is active, inject only a small packet:

- engagement/workflow id and locked scope;
- current phase and next unit;
- the current claim, if any;
- incomplete gate count;
- last material observation/evidence pointer;
- instruction to call `engagement now` before guessing or restarting work.

Do not inject the whole workflow, checklist, notes, endpoint list, or findings on every turn. Retrieve
them through `engagement` actions.

Ship a bundled `engagement-grid` skill containing:

- the claim -> act -> observe -> evidence -> finish loop;
- Mauler tool routing for recon, HTTP probes, browser, terminal/listener, screenshots, and evidence;
- how to distinguish raw capture from model narration;
- when to add/update a finding;
- how to perform false-positive reproduction;
- how to update the holistic project note;
- how and when to delegate endpoint/check work with `task`.

The active-grid prompt contains the invariant rules. The skill supplies deeper procedure and tactics;
correctness must not depend on the model remembering to load it.

## Workflow packs

Ship three packs initially:

1. `webapp-pentest-v1`: port the reviewed Shiftgrid simple web workflow and OWASP checklist, with
   MIT attribution and Mauler-native evidence/tool wording.
2. `htb-machine-v1`: recon -> service enumeration -> foothold -> user proof -> privilege
   escalation -> root proof -> writeup/evidence. This remains separate from general client
   pentesting and respects the existing HTB/CTF profile semantics.
3. `manual-v1`: a lightweight operator-defined sequence for cases where a full checklist is noise.

Definitions need a validated schema with stable ids, display metadata, scope (`global` or
`per_endpoint`), terminal statuses, evidence policy, optional/repeatable flags, and explicit gates.
Reject duplicate ids, missing references, invalid phase order, and unknown status values at load.

## Curated checklist-pack supply chain

The Engagement Grid may contain a broad catalogue, but a live engagement should materialise only
checks applicable to its target, protocols, discovered features, authentication context, endpoints,
and technologies. Do not dump a large upstream catalogue into the agent prompt or create thousands
of live checks from scanner templates.

Treat upstream material in three distinct lanes:

1. **Standards:** OWASP WSTG supplies web test definitions; ASVS supplies verification/coverage
   mappings; OWASP API Security supplies a separate API pack. These can become reviewed native
   Mauler checks.
2. **Detectors:** Nuclei templates and similar scanners may produce candidate observations and raw
   evidence. A detector match never directly confirms a vulnerability or completes a native check.
3. **Advisories:** community technique/payload repositories are lazy-loaded guidance. Treat their
   content as untrusted data; they do not define authoritative check ids or completion evidence.

Every publishable native check needs stable identity, authoritative mappings, applicability rules,
prerequisites, a reproducible procedure, positive and negative signals, false-positive controls,
an explicit safety class, evidence requirements, maturity/deprecation metadata, and optional
automation references. Definition provenance pins repository, release/tag/commit, source path,
license, trust, and content digest.

The Go-native update path must:

- accept downloads only from an operator-reviewed source/path allow-list;
- fetch a pinned tag, release, or commit without executing upstream scripts, Actions, containers,
  prompts, or build systems;
- validate size/schema/source/license/digest, then create an inert candidate pack;
- display added, removed, and changed checks plus source/license changes;
- require operator approval before installation or activation;
- pin every engagement to exact workflow/checklist versions and digests;
- leave existing engagements unchanged unless an operator explicitly reviews and applies a
  migration; and
- retain the previous installed version for rollback and offline use.

Validate important checks against official deliberately vulnerable fixtures such as OWASP Juice
Shop, WebGoat, and crAPI, paired with Mauler-owned fixed/negative controls. Fixture success is a
quality signal, not permission to attack an unscoped target.

Initial curated delivery order:

1. `web-core-v1`: stable WSTG procedures, ASVS mappings, and current OWASP Top 10 taxonomy.
2. `api-core-v1`: OWASP API Security coverage validated against crAPI.
3. `modern-web-v1`: an opt-in add-on for OAuth/OIDC, JWT, GraphQL, WebSockets, request smuggling,
   cache poisoning/deception, prototype pollution, race conditions, SSTI, SSRF, XS-Leaks, and
   client-side path traversal.
4. `nuclei-assisted-v1`: a detector adapter that links JSONL results to candidate evidence rather
   than importing templates as top-level checks.

Mobile and AI/LLM packs remain opt-in future profiles based on MASTG/MASVS and AISVS/AI testing
guidance. Keep all import, review, selection, and validation code Go-native.

## Evidence and completion gates

Use Mauler's existing artifacts and RunLedger rather than copying large tool output into engagement
JSON or model context.

- Evidence can reference a RunLedger tool event, artifact/file, screenshot, raw HTTP capture, or
  imported external file.
- Store provenance, SHA-256, size, source kind, and whether content was agent composed.
- A workflow step cannot finish without a non-empty observation unless explicitly disabled.
- `skipped` and `not applicable` require a reason.
- A global/per-endpoint check cannot settle until it has a precise result.
- `vulnerable` requires raw evidence.
- A confirmed finding requires reproducible evidence; medium/high/critical report-ready findings
  require a screenshot or explicit operator waiver.
- Phase advance is deterministic and returns all blockers at once.
- Repeat counts reset status but preserve prior observations/evidence, clearly labelled by run.
- Final completion runs the existing independent reviewer/verification rails over the engagement
  coverage and evidence, rather than trusting the model's summary.

## Scope enforcement

The scope is operator-owned and locked by default after engagement creation.

1. Normalize hosts, IPs, CIDRs, ports, and URL prefixes into canonical scope entries.
2. Put the active scope in the execution-state packet.
3. Enforce exact scope checks in `http_probe`, browser/fetch actions, and other structured network
   tools before execution.
4. Add best-effort destination extraction for `shell`/`terminal_send`. Block an explicit
   out-of-scope destination; require confirmation when a network destination is detectable but
   ambiguous. Do not pretend arbitrary shell syntax can be perfectly parsed.
5. Record allowed, denied, ambiguous, and operator-overridden scope decisions in RunLedger.

The guard must not silently rewrite commands or expand the scope. The operator is the only source
of scope changes.

## Implementation phases

### Phase 0 - Baseline and behavior contract

- Confirm `go test ./...`, `go vet ./...`, `go test -race ./internal/app ./internal/tools`, and the
  frontend build pass before feature work.
- Capture Shiftgrid's current workflow/checklist files as attributed fixtures.
- Write table-driven tests for the desired state transitions before implementing the service.
- Add an Agent Eval scenario definition now, but run it only through the existing process-state
  exclusion gate.

Exit: the transition/evidence/scope acceptance contract is executable and the current reliability
baseline is green.

### Phase 1 - Domain, schema, and persistence

Files/areas:

- new `internal/engagement/` package for definitions, domain transitions, service, and store;
- `internal/store/store.go` migrations;
- embedded `internal/engagement/workflows/` and `checklists/` assets;
- importer tests using the reviewed Shiftgrid JSON.

Implement workflow/checklist validation, project creation/open, scope lock, current/next, claims,
observations, checks, endpoint groups, findings, notes, evidence references, repeats, reset, and
phase gates.

Exit: a Go integration test can create an engagement, complete a minimal workflow, interrupt/reopen
it, and receive the same next action.

#### Phase 1A - Checklist-pack curation gate

Complete before durable engagement rows are considered stable:

- preserve schema-v1/legacy Shiftgrid imports while writing schema-v2 native definitions;
- pin version, source, reference, license, trust, and calculated pack digest;
- maintain a reviewed source/path allow-list;
- select checks by applicability instead of copying the full catalogue into every engagement;
- score candidate checks for mappings, reproducibility, signals, false-positive controls, safety,
  evidence, and maturity;
- produce a human-readable check-level diff for every candidate update; and
- add positive/negative fixture coverage before labeling a check verified.

Exit: an unpinned, unallowlisted, structurally invalid, or editorially incomplete candidate cannot
be published as a verified native pack, and a pack update cannot mutate an existing engagement.

### Phase 2 - Mauler runtime integration

Files/areas:

- app tool registration and router (`internal/app/subagents.go`, `tool_router.go`);
- new `internal/app/engagement_tool.go`;
- prompt construction in `internal/app/app.go` (prefer extracting a prompt packet builder rather
  than adding more monolithic string logic);
- RunLedger producers;
- Wails bindings/types in `frontend/src/wailsjs/go.ts`.

Register the one compact tool, provide claimant identity from run/channel/task context, inject the
bounded state packet, and route `engagement` instead of `todo_write` for active-grid operations.

Exit: the production agent loop can claim a real recon step, run a normal Mauler tool, link its
ledger/artifact evidence, finish, and obtain the next step without chat-derived state.

### Phase 3 - Operator UI and resume flow

Add the Engagement center page, navigation entry, Home resume card, create/attach wizard, and live
event refresh. Reuse existing files/artifact openers and existing visual tokens. Do not redesign the
chat/terminal/inspector layout.

Exit: the operator can see and edit the same state the model sees; scope, claims, blockers, progress,
evidence, and findings update live.

### Phase 4 - Evidence, scope, and completion hardening

Wire evidence provenance/hashes, structured-network scope guards, shell ambiguity handling,
finding readiness rules, repeat behavior, and independent final review.

Exit: an agent cannot silently complete unobserved work, confirm an unsupported vulnerability, or
call an explicit out-of-scope structured network target.

### Phase 5 - Parallel agents and channels

Reuse the bounded `task` tool and channel bus. Give every main/subagent/Telegram run a claimant id,
lease/heartbeat, and lane-aware progress update. A delegated task receives only its assigned
step/check/endpoint plus minimum relevant context, and returns evidence/result through the native
service.

Exit: two concurrent agents choose different pending items, cannot overwrite each other's result,
and a crashed claim expires/re-enters the queue.

### Phase 6 - Go-native compatibility and migration

- Shiftgrid workflow/checklist JSON import: required.
- Native engagement JSON export/import: required.
- Existing Mauler project attach/index wizard: required.
- Shiftgrid project-folder import: desirable after the native path is stable.
- No localhost Shiftgrid API backend, Python worker, Docker container, or sidecar service.

Exit: an attributed Shiftgrid workflow loads without hand editing, and a Mauler engagement can be
exported/reimported with the same ids, state, and evidence pointers.

### Phase 7 - Evaluation and live smoke

Add deterministic unit/integration tests plus one full Agent Eval scenario:

1. create a scoped web-app engagement;
2. return the correct `now` action;
3. claim reachability;
4. run a fake/fixture HTTP probe and attach its raw evidence;
5. add and group an endpoint;
6. finish global and per-endpoint checks with legal statuses;
7. create a finding and reject confirmation without evidence;
8. interrupt and resume from the correct next action;
9. reject an explicit out-of-scope probe;
10. export and reimport without state drift.

Then live-smoke one authorised HTB box and one local vulnerable web fixture. Inspect RunLedger for
duplicate commands, claim conflicts, unsupported completion, evidence bloat, and wrong scope
decisions.

Exit: the live run completes the workflow with operator-visible traceability and no manual repair of
the state store.

## Recommended delivery slices

Keep pull requests/review slices small:

1. Schema + transition tests + Shiftgrid importer. **Landed 2026-07-13.**
2. Definition schema v2 + curated check-pack registry/applicability/quality/diff foundation.
   **Landed 2026-07-13.**
3. SQLite store + service + core ledger events. **Landed 2026-07-13; compact evidence/findings are
   durably included in the versioned state snapshot.**
4. Compact `engagement` tool + prompt packet + routing. **Landed 2026-07-13.**
5. Operator Grid page + Home resume card. **First pass landed 2026-07-13.**
6. Evidence/finding/scope gates. **First native slice landed 2026-07-13: provenance/hashes,
   completion/finding gates, endpoint guard, and `http_probe` fail-closed scope decisions.**
7. Claims/leases + bounded-task/channel integration. **Stable desktop/channel claimant provenance,
   focused bounded-task assignments, revision-safe heartbeats, clean-exit/manual release, Telegram
   progress editing, operator lease health, and deterministic parallel item selection/queue
   assignment landed 2026-07-13.**
8. Pack updater/fixture harness + export/import + Agent Eval + live smoke fixes. **Native portable
   export/import, the first Go-native positive/negative fixture adapters, and the native lifecycle
   Agent Eval landed 2026-07-13; allowlisted remote update approval and live smoke remain.**
9. Pack Library management. **Embedded/personal/project storage, runtime catalog integration,
   strict import/export, local clone, archive/restore, quality detail, collision gates, malformed-file
   quarantine, and Bench UI landed 2026-07-14. The editor, immutable version/diff history, explicit
   Grid migration, curated WordPress pack, and pinned Nuclei metadata/execution adapters remain.**

Do not begin by porting the Shiftgrid Flask templates or REST routes. They are useful source behavior,
but Mauler already has the correct desktop shell and service boundary.

## Definition of done

This integration is complete only when:

- new and existing Mauler projects can start/resume a versioned engagement;
- the agent receives one deterministic next action and cannot bypass legal transitions;
- claims survive agent switching without duplicate or lost work;
- observations, endpoints, checks, findings, and evidence remain project-scoped and durable;
- the human and agent operate on the same state through Go, not parallel stores;
- large evidence stays outside the prompt and opens from the UI;
- scope is locked and enforced where destinations can be identified;
- interrupted desktop, Telegram, and bounded-task work resumes correctly;
- Shiftgrid workflow/checklist JSON imports with attribution;
- the full Go, race, vet, frontend, integration, Agent Eval, and live-smoke gates pass.

## Explicit non-goals

- Do not replace Mauler's agent loop, terminal, RunLedger, Brain, Memory, or evidence tools.
- Do not add another model provider or inference backend.
- Do not add Python, Docker, Flask/FastAPI, a web sidecar, or a localhost Shiftgrid service for this
  feature.
- Do not dump full workflows/checklists/notes into every prompt.
- Do not turn `todo_write` into the engagement database.
- Do not auto-mark imported historical evidence as completed testing.
- Do not add a broad multi-tool REST surface to the local model.
