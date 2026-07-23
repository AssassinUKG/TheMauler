# Feature catalog

This catalog answers “does this exist?” without loading the historical 68 KB handoff. Code and tests
remain authoritative for implementation detail.

## Workbench and editing

- Wails desktop shell, React/Vite UI, Monaco file viewer/editor, syntax selection, minimap,
  formatting, save, scratch snippets, file tree, sessions, and artifact runner.
- Chat and File center tabs plus full-page Logs, Memory, Telegram, Brain, Projects/Services,
  Engagement, Pack Library, and related operational views.
- Resizable/persisted Explorer, Inspector, bottom work area, and Terminal/AI Commands surfaces with
  collapsed rails and narrow-layout behavior.
- Terminal and Stream bottom tabs; shared-terminal AI commands are visible without duplicating full
  scrollback/tool results.

## Agent operation

- SSE streaming loop, truncation/no-tool continuation repair, stop/interrupt, task logs, descriptive
  phases, control-plane phases, confirmations, safe-listed exact inputs, rollback, snapshots, and
  postcondition verification.
- Auto/Manual plus Builder, Fixer, Reviewer, Researcher, Planner, and Bug Bounty Hunter definitions.
- Agent presets with profile, context, autonomy, instructions, toolset, and per-tool permissions.
- Todo/plan tools and Plan UI; compact status/progress surfaces avoid transcript spam.

## Tools and research

- Compact file, shell, terminal, HTTP, glob, grep, session, todo, skill, task, web/fetch/browser,
  memory, evidence, SQLite, listener, and reasoning-support tools.
- Platform-aware Windows/WSL/Linux paths and shell selection; visible shared terminal and isolated
  execution modes.
- Search-provider fallback, readable fetch, GitHub-aware fetching, source ranking, web/browser budgets,
  and browser snapshots/interactions/screenshots.
- Text PDF extraction with page/output bounds; scanned-PDF OCR and the broader extractor registry are
  future repository-intelligence work.

## Models, providers, and settings

- Separate provider/profile settings with migration, model listing/ping, generation values, thinking
  cards, context checks, and one-model-load-per-key behavior.
- InferenceBridge local OpenAI-compatible default and OpenRouter optional cloud provider.
- UI-only masked provider secret editing with environment precedence; secrets are never returned to
  React.
- OpenRouter catalogue context/output limits seed new profiles; task-level cloud selection resets to
  the local profile after use.
- Local/Hugging Face Qwen3.6 and Gemma4 ids receive code-owned family/variant templates for context,
  sampling, adapter, tool protocol, and embedded-GGUF Jinja handling; Profiles can apply them
  explicitly and unknown models remain benchmark-required.
- Model Lab uses one excluded warm-up plus three measured text runs, keeps tool/loop latency out of
  text throughput, and reports authoritative backend decode separately from labelled warm
  end-to-end fallbacks. Matrix rows can explicitly create and activate a matching local Chat profile.
- Generation settings include end-to-end `repeat_penalty` transport for local OpenAI-compatible
  providers.
- Settings modal covers general, providers, profiles, agents, environment, tools, Telegram, context,
  storage, UI, image, and associated media controls.

## Context, memory, and evidence

- Task-aware bounded project instructions, strictly validated manifest routing, heading-aware
  excerpts, exact hash/range provenance, deterministic compact-core fallback, context compaction, and
  lazy master/project skills.
- First-class Context Inspector with non-sensitive preflight accounting, included/excluded source
  reasons, hashes/ranges, open-source actions, code-owned synopsis/warnings, and an ephemeral
  Core/Relevant/Expanded next-task selector. It exposes exact compact tool names and packet/schema
  identities without returning raw prompt content.
- Benchmark reliability surfaces include a zero-model Context Quality pass^5 over seven task
  classes and hostile content, plus single-run or repeated live Agent Eval with fixture pass^k,
  unsupported-completion, duplicate-action, tool-error, recovery, tool-count, latency, policy, and
  human-intervention metrics.
- Workspace-scoped durable memory with importance/pinning/tags, weighted retrieval, edit/filter UI,
  and explicit deletion.
- Saved-session SQLite FTS recall and a session-search tool.
- Task runs with prompt/mode/profile/status/state/timeline/tool trail/stop details.
- RunLedger JSONL event spine, Brain filtering/details/export, immutable evidence references, and
  reviewable learning candidates promoted only through approval.

## Channels and media

- Desktop Fast Chat provides a compact active-model conversation without project documents, memory,
  tools, control-plane state, planning, or review; Project Agent remains the full work lane.
- Telegram bot configuration, long polling, allow-list/mentions, separate side chat, quick controls,
  queued explicit runs, progress cadence, persistent channel queue, full-page conversation/ledger UI,
  and redaction.
- Microphone/transcription and outgoing Kokoro/Piper TTS, including Telegram voice-note conversion.

## Security and engagements

- Promptware and secret-exfiltration guards on tool results.
- Code-owned tool risk labels, confirmation policy, capability/toolset filtering, and scope boundaries.
- Native sealed task contract/control state machine with plan-gated mutations and evidence-gated
  completion.
- Engagement Grid foundation, target/scope/evidence bindings, pack library, pentesting reporting
  profile, explicit HTB/CTF profile, and Bug Bounty manual-assessment planner.

## Planned, not complete

- General any-file extractor/indexing pipeline and scanned PDF OCR.
- Native LSP client/intelligence surfaces.
- Remaining named checkpoint/dirty-tab isolation, exact agent-definition ledgering, and the recorded
  live selected-profile Agent Eval x5 closure gate.
