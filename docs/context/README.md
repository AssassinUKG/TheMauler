# Mauler context map

This directory holds task-retrievable project context. `../../AGENTS.md` is the compact canonical
instruction source; these documents carry detail that should be loaded only when relevant. The
content-preserving pre-split handoff is
`../archive/agents-handoff-snapshot-2026-07-21.md`.

## Documents

| Document | Read when the task concerns |
| --- | --- |
| `current-state.md` | What is working now and the latest verified baseline |
| `architecture.md` | Package ownership, model/tool flow, state, persistence, or events |
| `non-negotiables.md` | Protected behavior, provider policy, safety, evidence, or UI invariants |
| `roadmap.md` | What to build next, dependencies, or superseded priorities |
| `verification.md` | Tests, race gates, production build, live smoke, or known non-gates |
| `feature-catalog.md` | Whether a product capability already exists and where it belongs |
| `troubleshooting.md` | WSL, provider, context, terminal, Telegram, audio, or build failures |

## Authoring rules

- Put each exact non-negotiable constraint in one canonical location and link to it elsewhere.
- Keep current state separate from historical verification.
- Prefer headings that can be retrieved independently.
- State supersession explicitly when behavior changes.
- Do not store secrets, tokens, raw private tool output, or copied web content here.
- A synopsis is navigation, not proof. Code, tests, and evidence artifacts remain authoritative.
- Update the relevant domain document during implementation rather than growing `AGENTS.md`.

## Packet status

M4 is implemented: the old 68 KB handoff is archived, the live canonical core is bounded, and
`manifest.json` is validated and executable for documentation retrieval. A task selects no more than
three trusted Markdown sources, compiled through heading-aware bounded excerpts. The run ledger
records the manifest hash, route, exact source hashes, line ranges, byte counts, and token estimates.

The first-class Context Inspector is available from `More workbench pages... > Context`. It previews
the actual packet and working budget without returning prompt contents or sensitive data. Core,
Relevant, and Expanded can be pinned for one desktop task; Auto restores normal task-aware selection.
It also shows the exact compact tool names and stable packet/tool-schema identities.

Benchmark > Advanced provides a deterministic Context Quality pass^5 for routing, sources, budgets,
tools, hostile content, and repeat stability. It makes no model calls. The adjacent Agent Eval x5
runs five complete live model suites and reports model-loop reliability separately.

The manifest is never an authorization source. Unknown fields, unsafe paths, invalid budgets, missing
documents, or malformed routes trigger a deterministic compact-core fallback. Code-owned intent,
scope, tool policy, approvals, protected paths, and evidence gates remain authoritative.
