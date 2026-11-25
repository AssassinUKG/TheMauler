# Roadmap

This is the ordered current backlog. Detailed tracked plans own acceptance criteria; this file routes
work and records what supersedes older lists.

## 1. Context packet reduction

Tracked by `../context-packet-reduction-implementation-plan-2026-07.md`.

- M1 complete: bounded task-aware project packet and provenance event.
- M2 complete: compact `AGENTS.md`, content-preserving archive, domain context documents, guarded
  sizes/rules, full production gate, and rebuilt application smoke.
- M3 complete: executable strict manifest schema, safe path and symlink containment, single-route
  relevance selection, at-most-three documents, deterministic compact-core fallback, heading-aware
  budgets, exact source provenance, repeated-run tests, and the full production gate.
- M4 complete: safe first-class Context Inspector, exact packet/provenance and exclusion accounting,
  budget map, open-source actions, rebuild synopsis, one-task Core/Relevant/Expanded pinning, fresh
  per-task system packets, focused tests, and the full production gate.
- M5 phase A complete: seven task classes x three paraphrases x pass^5, hostile-content checks,
  deterministic packet/tool identities, Benchmark UI, and reusable production Agent Eval pass^k
  telemetry. The 2026-11-24 RVN/35K/draft-MTP `n=2` UI Gate 1 passed 12/12, then its required live
  x5 finished 54/60 raw attempts with only 8/12 fixtures clean across every round. Four scorer false
  negatives found by that run are fixed; replay-equivalent accounting is 58/60 and 10/12, still not
  pass^5. The residual failures are masked verification and stale-IP leakage into todo input, so the
  selected profile remains supervised-only. Remaining closure gate: after a deliberate
  model/profile/control change, pass a fresh 12/12 UI Gate 1 and then a full clean live Agent Eval
  x5. Do not conflate the 105-attempt zero-model preflight or replay-equivalent rescoring with live
  model reliability, and do not rerun an unchanged profile until it happens to pass.
  The subsequent input-exclusion and verification-exit hardening reached 9/12 in native report
  `agent-eval-20261124-204231`: exclusions passed, masked compile checks remained blocked, grep/edit
  exceeded its continuation allowance, and a `tail/head -n` scorer false negative was identified and
  fixed. The final scorer has not had a fresh live Gate 1; x5 is locked. Next reliability work should
  target unnecessary continuation and direct verification selection without changing fixture limits.

## 2. Bug Bounty Hunter and Chat workspace isolation

Tracked by `../bug-bounty-agent-chat-workspace-integration-plan-2026-07.md`.

The M0-M3 functional slice exists. Remaining reliability edges:

- final acceptance/live UI verification after those edges land.

Landed 2026-10-25: every new task run persists the exact agent definition ID/version in task-run
state and SQLite schema v19, and every run-owned RunLedger event inherits the same provenance.
Checkpoint resume preserves the selected built-in or configured custom agent; enabled custom agents
no longer collapse to Auto, while missing/disabled definitions still fall back safely to `auto@1`.
Named checkpoint/resume landed 2026-09-17 and the shared dirty-tab Save all/Discard/Keep editing
workspace-transition guard landed 2026-09-27; neither remains an open item here.
Landed 2026-10-25: the existing three-paraphrase x pass^5 Bug Bounty route is paired with hostile
JavaScript-comment, HTTP-header, and HTML-body fixtures repeated five times each. Attachment bodies
remain model-visible untrusted evidence but cannot select agents/skills/memory, advertise tools,
expand task contracts, choose targets, alter completion gates, or regain authority after checkpoint
resume. The operator's text and the fact that attachments were selected form the separate persisted
trusted routing prompt.

Landed 2026-09-17: reusable named conversation checkpoints share the existing authoritative run-
checkpoint store, restore a UI-safe transcript, add the model-visible continuation to Chat, and
resume as a fresh generation linked by `parent_run_id`. Automatic recovery snapshots remain visibly
separate and named checkpoints are not consumed by a successful resume.

## 3. Agent reliability and native control plane

Tracked by `../mauler-agent-control-plane-improvement-plan-2026-07.md` and
`../agentic-reliability-issues-2026-07.md`.

- Native Windows follow-up 2026-11-17: initial/pre-effect durable checkpoints, explicit safe recovery,
  OS-released data ownership, shutdown joining, atomic session replacement, saved-chat workspace
  identity, visible partial outcomes and longer bounded text replies are implemented and qualified
  to the recorded native-process boundary. Text and post-append hard kills do not authorize effect
  replay. See `../mauler-native-lifecycle-reliability-2026-11-17.json`. Next qualification remains
  descendant containment and wider mixed/browser/remote journeys; reboot/power-loss was deliberately
  not performed. A larger response budget is not completion evidence for file/action tasks.

- Landed 2026-11-17: a bounded local-only production-loop matrix passed 65/65 across ordinary chat,
  file reads, exact-byte create/append handoff, cancellation and fresh same-chat recovery. Routing
  collisions, budget-exhausted review bypass and shared backend cancellation are fixed. Final
  current-source admission audit: 80 retained snapshots and 10 current exact artifacts passed.
  See `../mauler-local-reliability-2026-11-17.json`. MAULER-AR-009's native-process slice is superseded
  by the follow-up above, while reboot and broader qualification stay open; the original fresh 12/12
  UI Gate 1 and full Agent Eval x5 requirements are unchanged.

- Expand typed task contracts and classified recovery without weakening the landed mutation/complete
  gates.
- Keep evidence-owned completion and code-owned tool policy central.
- Improve circuit-breaker recovery, exact final-message delivery, loop observability, and repeated-run
  reliability reporting.
- Landed 2026-09-03: cached tool results count as no-progress signals, the corrective prompt permits
  immediate evidence synthesis, and safe read-only recovery answers use an explicit recovered handoff
  across desktop and Telegram while retaining the blocked-loop audit record.
- Landed 2026-09-03: mixed public-research/artifact tasks retain write/edit capabilities for explicit
  placement language (including “write the PoC” and “leave it in the directory”), the exact routed
  tool names are supplied to every model turn as capability truth, and Tools exposes Automatic versus
  full Selected-tool routing without bypassing capability or policy gates.
- Landed 2026-08-27: separated artifact refresh from workspace switching so an active Chat cannot be
  erased by `http_probe`, and added code-owned `terminal_send` HTTP-to-isolated-shell routing with a
  bounded timeout. Next evidence gate is a focused live regression run, then the unchanged fresh
  12/12 Gate 1 requirement before Agent Eval x5.
- Keep protected tests/verifier settings outside agent mutation scope.
- Landed 2026-11-16: the external MCP boundary uses the official Go SDK for stdio, legacy SSE, and
  Streamable HTTP `/mcp`. It stays one compact Mauler tool instead of importing untrusted remote
  definitions into the native registry, is lazy/lifecycle-owned, confirmation-gated, bounded, and
  restricted to direct loopback/private-LAN network endpoints with no tunnel or relay behavior.

### HelixClaw-derived reliability parity

Tracked by `../helixclaw-to-mauler-reliability-parity-plan-2026-09.md`.

- M1 started 2026-09-03: task runs own a durable monotonic generation and backend conversation epoch; live run events carry
  backward-compatible owner metadata; React rejects stale/retired run events; and active runs prevent
  conversation clear/session-load races. Conversation epochs now rotate on conversation/workspace
  replacement and suppress late backend events. Payload-free stale-event counts/latest-owner metadata
  are now visible in Doctor/Services and a concurrent backend epoch-rotation fixture passes. Secondary
  task/file/learning/progress events now use the same owner boundary. The desktop/narrow-width
  Clear/Switch/Stop/queued-send smoke remains.
- M2 is implemented: nine-phase request preflight, saved-session health preview/backup/apply, useful
  rejection diagnostics, and checkpoint resume into a new generation linked by `parent_run_id`.
- Landed 2026-09-15: verified runs persist finalized artifact fingerprints with run/generation/verifier
  provenance; Engagement confirmation re-hashes file evidence; and Chat includes an authoritative
  `Observed → Draft → Validated → Rejected` findings inbox with stale confirmations returned to Draft.
- Landed 2026-09-15: task contracts protect finalized files from ordinary continuations and common
  file/shell/terminal/Python-orchestration mutation paths. Only an explicit Fixer request naming the
  exact file receives repair scope; repair tool calls still require new verification evidence. A
  deterministic integrated acceptance fixture changes that artifact, blocks an unscoped sibling and
  proves the refreshed canonical-path handoff has a new fingerprint.
- Next: run the rebuilt-native M1 lifecycle race and M3 scoped-repair/handoff UI smokes, then start M4
  bounded any-file ingestion without introducing a second control framework.
- Landed 2026-09-15: image-generation progress inherits the parent run owner and late progress from
  retired runs is rejected by the backend epoch boundary and Chat.
- Files & Knowledge, optional LSP and channel-neutral delivery remain routed through sections 4-6;
  do not implement them as parallel frameworks.

## 4. Files and repository intelligence

Tracked by `../repository-intelligence-parity-plan-2026-07.md`.

- Landed 2026-09-15: deterministic manifest fixtures/benchmark, streaming UTF-8/UTF-16 text/code
  chunking, file/chunk hashes and line provenance, explicit omission/error states, SQLite v17
  immutable generations with FTS5, cancellation-safe activation, and code-owned current-workspace
  scope. The compact `memory` tool now provides `index_workspace`, `index_status`, and bounded
  `search_workspace` with RunLedger metadata-only events.
- Chat now has a first-class **Files** scan card: index/rebuild, coverage totals, bytes/chunks,
  immutable generation/manifest identifiers, a reviewable explicit omission list, live metadata-only
  scan progress, and responsive cancellation. Doctor and Services expose index health.
- Landed 2026-09-20: the first M4 rich-format registry indexes readable PDF and
  DOCX/PPTX/XLSX/ODS text and safe ZIP inventories into the same bounded, hash-cited FTS generation.
  Extraction has code-owned expanded-byte, archive-entry and timeout limits; corrupt, encrypted,
  scanned/no-text and over-expanded files remain explicit omissions rather than claimed coverage.
- Landed 2026-09-20: Files accepts additional operator-selected file and folder roots. These are
  workspace-scoped, persistent, read-only index authority, visible/removable in Chat, and part of
  the immutable policy digest; they do not widen workspace, tools, or engagement scope.
- Landed 2026-09-21: manual incremental refresh and persisted per-workspace watch mode create fresh
  immutable generations, SHA-verify every reused file before copying its prior chunks, re-extract
  changed/new files, remove deleted files transactionally, and leave the previous generation
  readable until commit. Chat reports Watch health/last-check plus reused/changed/deleted totals.
- Landed 2026-09-23: Brain exposes the shared code-owned index health verdict, workspace/policy/
  manifest provenance, coverage, Watch state, live progress, incremental reuse facts, bounded
  omission review, and Refresh/Watch/Rebuild/Cancel controls. Long scans remain cancellable while
  their original Wails call is pending; Brain is a projection, not a second index authority.
- Landed 2026-09-23: the M3 split-review foundation deterministically balances one immutable active
  generation into sealed read-only shards. Each backend contract owns exact paths, chunk IDs,
  file/text hashes and line bounds; the merge gate rejects missing, stale, cross-shard or out-of-range
  evidence, merges exact claim/evidence duplicates, and preserves contradictory claims as conflicts.
  Brain previews metadata, balance and digests without shipping source bodies or chunk IDs to React.
- Landed 2026-09-24: sealed shards execute through a bounded child-review lane with no ambient tools;
  the sole capability can catalog/search/read only the assigned immutable evidence. Brain owns one
  parent progress card with cancel, retry-shard, merge counts and draft/conflict results, while child
  chatter stays out of Chat. Execution is sequential by design until live reliability evidence supports
  safe parallelism.
- Landed 2026-09-24: schema v18 transactionally persists each parent review contract, shard attempts,
  accepted submissions and controller merge decisions. Startup classifies abandoned active reviews as
  interrupted; Brain resumes only unfinished shards after exact active-generation/manifest/plan
  revalidation, while deterministic replay rejects corrupt or inconsistent saved merges.
- Landed 2026-09-24: deterministic conflict contracts receive a fresh bounded reviewer with no prior
  child reasoning and only the exact disputed immutable chunks. Strict compatible/prefer/unsupported/
  inconclusive verdicts use stable code-owned claim IDs, survive checkpoint/restart, reject corrupt
  durable state, and render in Brain as adjudication rather than proof. A supervised live-model
  conflict case remains part of M5 hardening.
- Landed 2026-09-24: the next M4 extractor slice adds bounded TAR/TGZ inventory, read-only SQLite
  schema, and PE/ELF structural metadata to the same immutable untrusted chunks. Tests prove archive
  bodies and database row values are not indexed, and corrupt/over-limit inputs remain explicit
  non-coverage.
- Landed 2026-10-25: scanned PDFs fall back to local Poppler/Tesseract OCR one bounded page at a
  time, and 7z files contribute metadata-only inventories under entry/logical-size/time limits.
  Missing OCR dependencies and over-limit inputs remain explicit non-coverage; archive member bodies
  are not indexed.
- Next: optional embeddings. Database row sampling remains excluded until it has an explicit
  privacy/redaction policy.
- Remove arbitrary whole-file UX barriers while enforcing provider/model context budgets through
  retrieval rather than truncation-by-accident.
- Add OCR for scanned PDFs and the other planned formats through bounded extractors.
- Keep ingestion local by default; cloud use remains an explicit task/profile choice.

## 5. LSP code intelligence

Tracked by `../lsp-code-intelligence-plan-2026-07.md`.

Add LSP incrementally for symbols, definitions/references, diagnostics, hover, and safe rename where
it materially improves Mauler and HelixClaw workflows. Do not replace working file tools or make an
LSP server a hard dependency for basic operation.

## 6. Workbench and channels

- Chat-first redesign Slices 1-2 and 6 are implemented: Chat is the entry point, Projects are optional
  durable Workspaces, model/tool turns remain visible, and explicit persistent scratch promotes in
  place. One searchable Conversation menu, contextual Inspector suggestions, and non-destructive
  tool/status/browser filters are present. The centre Chat canvas now uses floating, fully
  dismissible Inspector and Terminal work drawers while specialist pages retain the docked IDE
  layout. Three-size native screenshot acceptance landed 2026-10-25; next is optional richer title
  metadata or a confirmed old-scratch review surface. See
  `../chat-first-workbench-redesign-2026-09.md`.
- Landed 2026-09-16: saved conversations have one safe Rename action in both navigation surfaces;
  transcript files and SQLite recall identities move atomically with collision/run guards and file
  rollback on index failure. Richer tags and other title metadata remain evidence-driven options.
- Landed 2026-09-16: the saved-chat library is newest-first with streamed message counts, recency,
  malformed-transcript review badges, and per-row Rename/Check/Delete menus that do not replace the
  active transcript.
- Landed 2026-09-16: bounded user-authored conversation tags persist independently from transcripts,
  survive rename, clear on delete, render as row chips, and drive title/tag search plus one-click
  filters in both navigation surfaces. Optional metadata corruption cannot blank the chat library.
- Landed 2026-09-24: ChatGPT-style automatic conversation identity closes the remaining lifecycle
  gap. Startup is a blank New chat; the first prompt receives a collision-safe title; end-of-run
  autosave updates that named transcript before the sidebar refreshes; New chat preserves and leaves
  the current row; and the internal recovery autosave is excluded from visible history.
- Landed 2026-09-26: the final-completion guard distinguishes greetings and capability openers from
  unfinished work. Their answers may invite the operator to provide a real task, while factual
  questions and action requests remain subject to the existing autonomous-completion requirement.
- Landed 2026-09-27: editor drafts and saved baselines are owned per tab, with visible dirty markers
  and a shared Save all/Discard/Keep editing guard across every UI-owned workspace transition. Failed
  saves and cancelled pickers preserve drafts; successful switches still clear only path-backed tabs
  and retain scratch snippets.
- Landed 2026-09-15: Chat now includes a first-class Security workspace with locked scope, workflow
  coverage, evidence/finding counts, next controlled action, and reviewable prompts for the next
  test or draft-finding validation. Engagement Grid remains authoritative for mutations.
- Multi-target client scope is implemented through the Home **Authorised scope** editor, legacy
  comma-list migration, explicit exclusions, and complete Grid locking. Remaining enhancements are
  batch readiness for selected concrete hosts and per-asset Grid/evidence filters; see
  `../multi-target-authorised-scope-plan-2026-08.md`.
- The September cockpit pass adds readable icon-and-label primary navigation, clearer active
  page/session hierarchy, and compact page/run/profile identity in the title bar without removing
  resizers, collapsed rails, Terminal/AI Commands boundaries, or daily controls. Continue the
  three-tier typography cleanup only where live visual testing finds remaining density problems.
- Finish Telegram/audio live-smoke edges and guarantee remote delivery of the same final answer shown
  in Mauler UI.
- Continue Engagement Grid and pack-library evaluation from their tracked plans after core reliability
  gates remain green.

## 7. Browser workflow assistant

Tracked by `../browser-workflow-assistant-plan-2026-09.md`.

- Slice 1 landed 2026-09-14: honest browser-intent routing, conversation-owned persistent native
  Chrome/Edge state, visible launch, cookie/redirect isolation, pause/takeover/resume/stop controls,
  post-handoff observation, no typed-value echo, local signup/verification fixtures, and a
  non-destructive authorised `admin.clara.co` login-page smoke.
- Slice 2 landed 2026-09-14: the exact tool/run waits behind a first-class Chat takeover card;
  snapshots issue stable owner-scoped element refs; upload/download paths are workspace-scoped with
  SHA-256 evidence; browser failures carry code-owned retry/ambiguity classification; and the
  deterministic native-browser reliability gate passes 5/5.
- Slice 3 landed 2026-09-14: bounded cancellation fixtures cover every browser phase; stable
  owner-scoped tab refs support open/list/switch/close; Services provides named metadata-only
  checkpoint/resume that strips credentials, cookies, form values, query strings, and fragments.
- Slice 5 landed 2026-10-25: screenshots support three bounded viewport sizes and are saved with
  workspace-owned immutable manifests containing SHA-256, dimensions, capture mode, and sanitized
  source metadata. Review rehashes each artifact, marks changed files stale, and permits only fresh
  captures to be promoted explicitly into Engagement evidence.
- Next: a supervised local-model browser pass^k.

## 8. NanoCyber sandboxed subagents

Tracked by `../nanocyber-sandboxed-subagents-plan-2026-10.md`.

- MN-1 landed: TheMauler owns a typed, bounded NanoCyber lease-v1 client contract with no Docker or
  host-shell implementation.
- MN-2 landed client-side: optional strict-loopback settings, protected credential presence/storage,
  and fail-closed Doctor/Services health plus capability inventory checks. The capability remains
  disabled until the companion runtime publishes the authenticated compatible inventory.
- MN-3 foundation landed: Test/Fix declares `offline-code`; strict admission/cancel/complete/parent-
  cancellation transports validate bounded receipts; an admission plan seals exact run generation,
  epoch, subagent attempt, budgets, evidence, expiry, and a NanoCyber-native engagement authority.
  If NanoCyber is enabled before readiness, Test/Fix stops before model execution with
  `host_fallback=false`; read-only subagents remain unchanged.
- Next: add the companion authenticated capability inventory and consumer typed-job, receipt, and
  terminal lease-status APIs, then define an explicit cross-product engagement binding. Only then
  wire live admission, cancellation, ledger receipt ownership, and result acceptance. Do not claim
  sandbox readiness before those cross-repository gates and the lifecycle qualification pass.

## Supersession

The pre-2026-07-21 giant handoff roadmap in the archive is historical detail. This ordered list and
the linked plans are current. When priorities change, update this document and the owning plan rather
than appending another permanent list to `AGENTS.md`.
