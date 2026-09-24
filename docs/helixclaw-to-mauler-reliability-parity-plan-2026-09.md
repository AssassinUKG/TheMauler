# HelixClaw-to-Mauler Reliability Parity Plan

Status: implementation in progress; lifecycle ownership M1, deterministic session repair M2 and finalized-artifact repair boundaries M3 implemented; native live smokes remain  
Scope: adopt proven HelixClaw reliability patterns where they strengthen Mauler without replacing
Mauler's native Go control plane, Wails UI, InferenceBridge-first provider path, or local-first policy.

## Decision

Mauler should borrow lifecycle continuity, session repair, artifact ownership, file intelligence,
optional LSP intelligence, and normalized channel delivery from HelixClaw. It should not copy the
CEO/orchestrator/worker hierarchy wholesale, introduce CLI-only workflows, add unrelated inference
providers, or move authorization back into prompts.

The order is reliability-first:

1. lifecycle generations and stale-event rejection;
2. deterministic session repair;
3. finalized-artifact ownership and evidence freshness;
4. any-file knowledge ingestion;
5. optional read-only LSP intelligence;
6. channel-neutral delivery parity.

## Protected Mauler foundations

- `internal/controlplane` remains authoritative for legal phases, scope, mutation and completion.
- RunLedger remains the immutable event/evidence spine.
- Assistant prose remains insufficient completion evidence.
- Local InferenceBridge remains the persistent default; cloud use is explicit and one-task only.
- Existing Explorer, Inspector, Terminal, AI Commands and splitters remain resizable.
- All operator configuration and file ingestion remain UI-first.

## M1 - Lifecycle generation ownership

Status: first production slice implemented 2026-09-03.

Every accepted task receives a JavaScript-safe, monotonically increasing generation persisted with
the task run. Run-owned Wails events append an owner containing `run_id`, `generation` and `origin` without
changing existing positional payloads. React tracks the active owner and ignores late stream,
thinking, checkpoint, tool, confirmation, compaction, state, error and completion events from a
retired or older run.

Desktop Chat accepts desktop-origin owners only. Telegram/channel runs keep their own delivery lane
and cannot take ownership of the visible desktop transcript.

Conversation clear, session load and workspace/project activation retire the visible owner. Loading
or clearing is refused in both Go and the UI while a run is active so shared backend history cannot
be replaced under an executing model.

Remaining M1 work:

- run-owned image-generation progress now carries the same causal owner and React rejects progress
  from retired runs; the backend epoch boundary now rejects retired context-owned image progress and
  HTTP artifact-refresh events before Wails delivery, while Chat applies the same owner check to
  task-run refresh, file refresh and learning suggestions (standalone Artifact Runner output remains
  a separate user-owned lifecycle);
- typed backend conversation epochs now persist with task runs, travel with run-owned event envelopes,
  advance on clear/load/workspace/scratch/resume, and suppress late run events below the UI boundary;
- make named checkpoint/resume open an explicit new generation linked to its parent;
- stale-event rejection counts and the latest payload-free rejection owner are exposed through
  Doctor/Services diagnostics without transcript noise;
- add a live Clear/Switch/Stop/queued-send race smoke at desktop and narrow widths.

The backend race fixture rotates the conversation epoch while concurrent retired events attempt to
emit, then proves that only the current owner survives and that the rejection counter is monotonic.
The remaining M1 gate is the same sequence in the rebuilt native UI at desktop and narrow widths.

M1 exit gate: no event from run A can append content, release composer state, change a confirmation,
or overwrite the status of run B or a newly loaded/cleared conversation.

## M2 - Deterministic session repair

Status: production slice implemented and fully verified 2026-09-03.

Build one code-owned repair pipeline before every model request:

1. validate supported roles;
2. remove invalid leading assistant/tool messages;
3. merge same-role messages while preserving structured tool calls;
4. remove assistant calls with no matching result when recovery cannot supply one;
5. remove orphaned tool-result messages;
6. repair empty content without inventing evidence;
7. remove unresolved trailing tool calls;
8. collapse stale continuation/nudge pairs;
9. deduplicate only exact tool calls, preserving distinct parallel work.

Every repair produces a compact typed ledger event and a deterministic fixture. It must not rewrite
user intent, tool evidence, authorization state, or control-plane history.

Implemented:

- request preflight now runs all nine code-owned phases and rejects transport only when no usable
  message survives; compacted live runs may continue from their trusted controller/task packet;
- exact repeated controller continuations and exact same-ID/same-payload tool calls are collapsed,
  while distinct parallel calls remain intact;
- multimodal blocks, attachment metadata, display text and reasoning content survive safe same-role
  merges;
- saved conversations have a UI **Check** preview and explicit **Repair saved session** action; apply
  retains a non-session `.bak` copy of the original and refreshes recall;
- loading uses a repaired in-memory view without silently rewriting the saved transcript, while an
  unusable saved session is rejected with a diagnostic;
- checkpoint resume creates a new run ID and generation with `parent_run_id` provenance.

M2 exit gate: malformed or interrupted history is either repaired deterministically or rejected with
a useful diagnostic; it never causes duplicate execution or a blank model request.

## M3 - Finalized artifacts and fresh review evidence

Status: first production slice implemented 2026-09-15.

- Verified mutation handoffs now persist finalized artifact records containing path, SHA-256, size,
  run ID, generation, conversation epoch and verifier evidence IDs. Loading historical runs derives
  `fresh`, `changed` or `missing` from current bytes without rewriting the captured fingerprint.
- Engagement evidence records distinguish file-byte fingerprints from immutable RunLedger payload
  fingerprints. Finding confirmation re-hashes file evidence and refuses stale or unverifiable bytes.
- Chat projects the authoritative Engagement state as an inbox with `Observed`, `Draft`, `Validated`
  and `Rejected` lanes. A previously confirmed finding whose referenced file changed is shown back in
  Draft as `revalidation required`; React cannot manufacture confirmation.
- New task contracts import prior finalized files as immutable artifact boundaries. Ordinary
  continuations cannot rewrite them through file tools, shell/terminal commands, `run_script`
  helpers, or direct Python filesystem/process primitives.
- Post-handoff mutation is admitted only for a `Fixer` run whose user request names the exact file;
  that path is sealed into the contract's repair scope. Inner `run_script` helper calls now traverse
  the same control plane and record their tool outcomes/evidence.
- A successful repair still follows the ordinary mutation-verifier and project-verification gates,
  producing a new fingerprinted handoff rather than reusing the prior bytes' evidence.

The deterministic repair/handoff acceptance fixture now changes one explicitly scoped test artifact,
proves an unscoped sibling stays blocked, and verifies the new handoff fingerprint. The equivalent
rebuilt-native UI smoke remains open.

M3 exit gate: stale evidence cannot approve changed bytes, and handed-off artifacts cannot be
silently replaced by a later conversational turn.

## M4 - Files and Knowledge parity

Implement the existing repository-intelligence plan through a UI-owned extractor registry, bounded
streamed chunking, resumable indexing, provenance, deduplication and synopsis generation. Cover
source/config/notebook/log/text formats plus PDF/OCR, Office/OpenDocument, EPUB, archives, SQLite,
image OCR, media and bounded binary metadata/strings. `0` means no source-size policy limit; model
prompt limits still apply through retrieval.

M4 exit gate: every supported readable file can be indexed locally without a hidden 100 KB skip or
whole-file model request, and every exclusion is visible in the UI.

## M5 - Optional LSP intelligence

Add one shared native Go LSP manager and one compact `code_intel` surface for saved-file symbols,
definition, references, hover and diagnostics. Monaco owns document lifecycle and visible Problems;
dirty buffers must not display stale semantic evidence. Regex, repository maps and ordinary reads
remain fallbacks, and no language server becomes mandatory for basic operation.

M5 exit gate: supported projects gain bounded semantic navigation/context while unsupported projects
continue to work exactly as before.

## M6 - Channel-neutral result delivery

Normalize desktop, Telegram and future Discord delivery around one typed lifecycle: accepted,
queued, started, progress, awaiting approval, recovered answer, completed, blocked, failed and
cancelled. All channels reference the same run/generation and receive the same final assistant answer;
formatting and message splitting are adapters, not separate result logic.

M6 exit gate: channel smokes prove no generic completion replaces the answer, no raw tool protocol is
reported as success, and follow-ups resolve the exact latest result for that conversation.

## Verification programme

Each milestone requires focused unit tests, repository-wide Go tests and vet, the app/tools race gate,
frontend production build and Wails production build. Lifecycle and channel milestones additionally
require repeated Clear/Switch/Stop/queue/restart fixtures. The existing fresh 12/12 Agent Eval Gate 1
and subsequent x5 requirement remain unchanged; deterministic tests do not claim those live gates.
