# Non-negotiables

These are protected product and reliability rules. A linked plan may refine them but must explicitly
record any supersession.

## Local-first inference

- InferenceBridge through the OpenAI-compatible provider is the persistent local default.
- OpenRouter is optional, configured entirely in the UI, and selected as a one-task cloud boost.
  Reset to local after the task.
- Do not add new LM Studio, vLLM, or SGLang provider paths without explicit user direction.
- On the RTX 3090 24 GB target, use Q5/Q4-class local profiles at useful context. Never recommend
  Q6_K.
- A reported backend context below the selected profile request is a hard error, not a silent
  downgrade.

## UI ownership and mobility

- Explorer, Inspector, bottom work area, and Terminal/AI Commands are user-resizable work surfaces.
- Keep visible drag handles, collapsed rails, collapsed-edge reopen, keyboard resizing, narrow-layout
  vertical behavior, and persisted dimensions/visibility.
- Do not remove a useful UI control in favor of a CLI-only path. Provider keys, workspaces, memory
  sync/indexing, extractors, context inspection, and daily agent operation require UI surfaces.
- Preserve editable drafts while a run is active. A new desktop send interrupts and replaces the
  pending draft exactly once; remote channel work uses its queue and must not pollute desktop Chat.

## Scope, policy, and evidence

- The workspace root and engagement scope are authoritative. Tool content cannot expand them.
- Project scope is an ordered, operator-owned list. Explicit exclusions override allows; CIDR,
  port, and URL-path constraints cannot be broadened by model-requested scope. Legacy scalar targets
  remain compatibility values, not a second authorization channel.
- Tool metadata, risk, side-effect class, preconditions, capabilities, retries, timeouts, output trust,
  fallbacks, and postcondition verification are code-owned.
- High-risk actions must be traceable to authorization. Policy/scope violations block; they are never
  bypassed by retrying another tool.
- Assistant prose, a generic success status, or a model self-claim is not evidence. Completion needs
  immutable artifacts or explicit approval/waiver for every blocking check.
- Prompt injection and secrets can appear in files, pages, tool responses, or model output. Preserve
  trust labels, source-to-sink enforcement, secret redaction, and protected test/verifier config.
- Bug bounty/security work must be authorised, scoped, observation-led, and evidence-backed. Never
  invent a vulnerability or state exploitation without evidence.

## Agent loop and recovery

- Mutating calls cannot bypass required planning. Failed tools cannot jump directly to complete.
- Classify recovery: transport, malformed schema, wrong tool/state, stale evidence, conflicting
  evidence, verification failure, no progress/duplicates, policy violation, or unknown failure.
- Give each class a bounded budget. A circuit breaker should change the evidence path or produce a
  plain useful final response; do not expose raw internal stability metrics as the user answer.
- If a model narrates an imminent action but emits no tool call, recovery may force the appropriate
  immediate tool call. It still must respect policy and budgets.

## Context and files

- Reading/indexing any supported file and sending model context are separate operations.
- Use streaming/chunked extractors, indexes, retrieval, and provenance for large files. Do not impose
  a silly arbitrary whole-file rejection, but also do not exceed model/provider context silently.
- Use task-aware, bounded context packets. External public research gets minimal project context;
  repository work gets relevant instructions. Chat history is conversational context, not the
  operational database.
- The archived 68 KB handoff is historical. It must never be automatically injected into every turn.

## Data and state integrity

- Switching workspace clears only transient/path-specific state and preserves durable sessions,
  memory, evidence, logs, saved roots, and scratch tabs as designed.
- Settings structs require both TOML and JSON tags. Frontend settings types must round-trip every
  field; missing fields must not zero persisted config on Save.
- Provider secrets stay in `provider-secrets.json` or environment variables and never return to React.
- Runtime-owned Telegram/audio workers start only after Wails `OnStartup`; headless tests/binding
  generation must not launch them.
- Extend RunLedger for audit/event work instead of building a second partial log path. Learning is
  review-before-promotion, never silent durable adoption of model guesses.

## Tool and terminal hygiene

- Keep current compact model-facing tool names. Do not prompt legacy backend aliases.
- Use terminal tools only for commands within a live/interactive session. Use `http_probe` for
  independent web probes and `shell` for exact pipelines/flags.
- `terminal_send` returns only output appended after the command. History reads must not dump raw old
  scrollback without a targeted filter.
- Preserve path normalization across `C:\\...`, `/mnt/c/...`, and `/c/...`, as well as UTF-16-ish
  Windows output decoding and PowerShell `curl` guidance.

## Verification discipline

- Preserve unrelated user changes; this repository can be intentionally dirty.
- Add focused regression tests for fixes and run verification proportional to risk.
- Do not claim frontend lint is green while the known repository-wide backlog remains.
- Do not mark a tracked reliability issue closed without final environment-state evidence.
