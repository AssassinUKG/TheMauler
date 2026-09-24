# Feature catalog

This catalog answers “does this exist?” without loading the historical 68 KB handoff. Code and tests
remain authoritative for implementation detail.

## Workbench and editing

- Wails desktop shell, React/Vite UI, Monaco file viewer/editor, syntax selection, minimap,
  formatting, save, scratch snippets, file tree, sessions, and artifact runner.
- Chat-first startup with optional durable Workspaces, promotable persistent conversation scratch,
  one searchable Conversation menu, contextual Inspector suggestions, non-destructive transcript
  event filters/search, run-owned turn retention, and saved-session reconstruction. Saved-chat rows
  open directly; the sidebar and title-bar menu reuse one lifecycle action set with matching
  streaming guards for New, Save, Rename, Check/repair, and Delete. Rename preserves searchable
  message identities, rejects title collisions, supports Windows case-only changes, and rolls back
  the transcript file if recall-index migration fails. Newest-first saved rows expose recency and
  streaming message counts, flag unreadable transcripts for review, and provide per-chat lifecycle
  menus without loading the selected transcript. Persistent bounded tags support chips, title/tag
  search, one-click filtering, rename/delete synchronisation, and fail-open transcript listing when
  optional label metadata is damaged.
- Chat and File center tabs plus full-page Logs, Memory, Telegram, Brain, Projects/Services,
  Engagement, Pack Library, and related operational views.
- Resizable/persisted Explorer, Inspector, bottom work area, and Terminal/AI Commands surfaces with
  collapsed rails and narrow-layout behavior.
- Unified View commands, panel-local close controls, Focus chat/Default layout presets, and optional
  run-activity auto-open; the title bar is the single application identity surface.
- Inspector overlay grid recovery prevents a visible-only resize rail when Chat uses zero-width
  docked side tracks; stale widths are clamped on every open. Shared code-native icons distinguish
  title-bar panel toggles and the Chat Tools/View/Run mode commands without adding another toolbar.
- Terminal and Stream bottom tabs; shared-terminal AI commands are visible without duplicating full
  scrollback/tool results.
- Native multi-file Chat attachment picker plus Explorer clipboard-path and drag/drop-path handling.
  Exact path attachments stay on disk for bounded full-file reading; inline browser-only text is a
  clearly signalled bounded fallback. Attachment content is labelled untrusted data and cannot
  override the user's request, expand scope, authorize actions, or request secret disclosure.
- Chat welcome actions distinguish review, continuation, planning, and authorised security work;
  compact composer navigation opens Security, repository files, or the native browser without
  sending an accidental prompt. Undo and Clear remain a visually separate conversation group.

## Agent operation

- SSE streaming loop, truncation/no-tool continuation repair, stop/interrupt, task logs, descriptive
  phases, control-plane phases, confirmations, safe-listed exact inputs, rollback, snapshots, and
  postcondition verification.
- Persisted monotonic task generations plus backend conversation epochs and run-owned Wails event
  metadata; Go suppresses retired emissions and Desktop Chat rejects stale stream/tool/state/final
  events after conversation retirement or a newer run takes ownership. Payload-free rejection
  diagnostics are available through Doctor/Services.
- Finalized-artifact seals with SHA-256, size, run/generation/epoch and verifier provenance; loaded
  task runs derive fresh/changed/missing against current bytes. Sealed files become contract-owned
  boundaries that only an exact, user-named Fixer repair scope can mutate before fresh verification.
- Nine-phase deterministic session repair before model transport, with UI health preview,
  explicit backup/apply for saved sessions, typed ledger actions, and parent-linked checkpoint resume.
- Auto/Manual plus Builder, Fixer, Reviewer, Researcher, Planner, and Bug Bounty Hunter definitions.
- Agent presets with profile, context, autonomy, instructions, toolset, and per-tool permissions.
- Todo/plan tools and Plan UI; compact status/progress surfaces avoid transcript spam.

## Tools and research

- Compact file, shell, terminal, HTTP, glob, grep, session, todo, skill, task, web/fetch/browser,
  memory, evidence, SQLite, listener, and reasoning-support tools.
- The `memory` tool also exposes current-workspace repository index/status/search actions. The Go
  indexer streams supported text/code/config data into bounded untrusted chunks, stores immutable
  FTS5 generations, and returns short results with stable generation/file/chunk evidence hashes.
  Chat's **Files** card displays coverage and explicit omissions, can rebuild the active index, and
  shows cancellable metadata-only scan progress without contending with the SQLite write transaction.
  Doctor and Services expose current index readiness/progress.
- Platform-aware Windows/WSL/Linux paths and shell selection; visible shared terminal and isolated
  execution modes. Per-call backend selection routes local Windows process/app/service/GPU facts to
  native PowerShell without disturbing the WSL/Kali target terminal, and preserves PowerShell `$`
  variables by removing redundant cross-shell wrappers.
- Search-provider fallback, readable fetch, GitHub-aware fetching, source ranking, web/browser budgets,
  and browser snapshots/interactions/screenshots.
- Conversation-owned persistent Chrome/Edge workflows with headless or explicit visible launch,
  cookies/redirects, owner isolation, runtime status, pause/takeover/resume/stop controls, protected
  typed-value output, deterministic same-run Chat takeover, and post-handoff re-observation. Chat's
  primary header has a Browser button and in-chat control surface for visible launch, live
  URL/title/tab status, Take over, controller-observed Resume, Stop, and refresh without visiting
  Services.
  Structured snapshots issue stable interactive-element refs. Workspace-scoped uploads and controlled
  downloads record relative path, size, and SHA-256 evidence; recovery classes distinguish safe
  observation retry from ambiguous state-changing outcomes. Stable owner-scoped tab refs support
  open/list/switch/close. Named checkpoints persist only sanitized URL/title metadata and never
  credentials, cookies, form values, query strings, or fragments. Browser intent includes signup,
  login, forms, email verification, CAPTCHA, MFA, and OTP; disabled capability is reported honestly.
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
- Local/Hugging Face Qwen3.8, Qwen3.6, Qwen3.5, and Gemma4 ids receive code-owned family/variant templates for context,
  sampling, adapter, tool protocol, and embedded-GGUF Jinja handling; Profiles can apply them
  explicitly and unknown models remain benchmark-required.
- Model Lab uses one excluded warm-up plus three measured text runs, keeps tool/loop latency out of
  text throughput, and reports authoritative backend decode separately from labelled warm
  end-to-end fallbacks. Matrix rows can explicitly create and activate a matching local Chat profile.
- Generation settings include end-to-end `repeat_penalty` transport for local OpenAI-compatible
  providers.
- Qwen3.8 profiles support normalized `none` through `xhigh` reasoning effort, preserved thinking,
  official thinking/no-thinking samplers, bounded tool-call fallback, and blocking completion evidence.
- Chat has a request-effective Thinking selector both beside the composer and in Inspector > Agent >
  Behaviour. Profile follows profile/adaptive policy, Always think forces thinking for supported
  templates across the run, and Direct forces generation with no preserved reasoning. The composer
  presents Qwen3.8's actual Auto/Low/Medium/XHigh effort ladder and active model identity; both
  surfaces edit the same persisted backend settings rather than separate React-only state.
- Profiles shows a Qwen3.8-only guided setup card with per-setting health, one-click template apply,
  reasoning-depth selection, and direct benchmarking. Local preserved reasoning uses the separate
  `reasoning_content` field and is removed from older turns by bounded micro-compaction.
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
- Telegram bot configuration, long polling, allow-list/mentions, separate conversational side chat,
  natural-language actionable-task routing without requiring `/cmd`, optional slash overrides, quick
  controls, safe queueing, progress cadence, persistent channel queue, full-page conversation/ledger
  UI, and redaction. Completed Telegram work is fed back into that chat’s bounded history; direct
  result follow-ups use the exact latest result. Formatted final cards support multi-message answers
  without the old result truncation and never present raw tool protocol as successful output. Natural
  date-range requests are live-work signals; “route it” consumes a remembered pending task rather than
  producing a model-only promise. A code-owned heartbeat edits the same progress card while a run is
  quiet, using the latest real run state and elapsed time.
- Microphone/transcription and outgoing Kokoro/Piper TTS, including Telegram voice-note conversion.
  Desktop Talk uses the normal Project Agent path. Actionable typed or voice Telegram requests,
  including live time/date and changing public-information checks, enter the same queued project-work
  path with the selected agent tools; ordinary Telegram conversation remains isolated side chat.

## Security and engagements

- Promptware and secret-exfiltration guards on tool results.
- Code-owned tool risk labels, confirmation policy, capability/toolset filtering, and scope boundaries.
- Native sealed task contract/control state machine with plan-gated mutations and evidence-gated
  completion.
- Intent-safe read-only routing distinguishes HTTP method names from workspace mutation verbs and
  exposes Engagement Grid state only when the task explicitly requests that workflow.
- Engagement Grid foundation, target/scope/evidence bindings, pack library, pentesting reporting
  profile, explicit HTB/CTF profile, and Bug Bounty manual-assessment planner.
- UI-first multi-target client projects with ordered IP/CIDR/hostname/URL scope, paste-list import,
  primary-target compatibility, External/Internal labels, per-row restrictions/notes, explicit
  exclusions, deny-before-allow matching, legacy comma-list migration, and complete immutable Grid
  scope locking.

## Planned, not complete

- Rich any-file extractors (Office/archive/database/binary metadata), incremental watch/refresh,
  split review/evidence merge, optional embeddings, and scanned PDF OCR.
- Native LSP client/intelligence surfaces.
- Remaining named checkpoint/dirty-tab isolation, exact agent-definition ledgering, and the recorded
  live selected-profile Agent Eval x5 closure gate.
