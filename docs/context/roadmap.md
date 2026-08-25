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
  telemetry. The repaired 2026-07-23 Huihui Gate 1 reached 11/12 and is not pass^1, so x5 remains
  locked. Remaining closure gate: after a deliberate model/profile/control change, pass a fresh
  12/12 UI Gate 1 and then run and record the full live Agent Eval x5. Do not conflate the
  105-attempt zero-model preflight with live model reliability or rerun an unchanged profile until
  it happens to pass.

## 2. Bug Bounty Hunter and Chat workspace isolation

Tracked by `../bug-bounty-agent-chat-workspace-integration-plan-2026-07.md`.

The M0-M3 functional slice exists. Remaining reliability edges:

- repeated paraphrase and hostile-content routing fixtures;
- exact agent definition/version in RunLedger;
- named conversation checkpoint/resume;
- dirty-tab save/discard behavior on workspace switch;
- final acceptance/live UI verification after those edges land.

## 3. Agent reliability and native control plane

Tracked by `../mauler-agent-control-plane-improvement-plan-2026-07.md` and
`../agentic-reliability-issues-2026-07.md`.

- Expand typed task contracts and classified recovery without weakening the landed mutation/complete
  gates.
- Keep evidence-owned completion and code-owned tool policy central.
- Improve circuit-breaker recovery, exact final-message delivery, loop observability, and repeated-run
  reliability reporting.
- Keep protected tests/verifier settings outside agent mutation scope.

## 4. Files and repository intelligence

Tracked by `../repository-intelligence-parity-plan-2026-07.md`.

- UI-first add-any-files/projects surface, format discovery, extractor registry, streamed chunking,
  resumable indexing, provenance, deduplication, and useful synopsis generation.
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

- Multi-target client scope is implemented through the Home **Authorised scope** editor, legacy
  comma-list migration, explicit exclusions, and complete Grid locking. Remaining enhancements are
  batch readiness for selected concrete hosts and per-asset Grid/evidence filters; see
  `../multi-target-authorised-scope-plan-2026-08.md`.
- Continue the cockpit cleanup plan without removing resizers, collapsed rails, Terminal/AI Commands
  boundaries, or daily controls.
- Finish Telegram/audio live-smoke edges and guarantee remote delivery of the same final answer shown
  in Mauler UI.
- Continue Engagement Grid and pack-library evaluation from their tracked plans after core reliability
  gates remain green.

## Supersession

The pre-2026-07-21 giant handoff roadmap in the archive is historical detail. This ordered list and
the linked plans are current. When priorities change, update this document and the owning plan rather
than appending another permanent list to `AGENTS.md`.
