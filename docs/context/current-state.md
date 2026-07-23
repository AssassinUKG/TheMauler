# Current state

Last consolidated: 2026-07-21. This is a concise operational view, not an exhaustive release log.
The pre-split full inventory is preserved in
`../archive/agents-handoff-snapshot-2026-07-21.md`.

## Workbench

- Native Wails desktop shell with a React/TypeScript workbench and Monaco editor.
- Explorer, Inspector, bottom work area, and Terminal/AI Commands retain visible draggable
  separators, collapsed rails, keyboard resizing, narrow-layout behavior, and persistence.
- Chat supports streaming markdown, attachments, sessions, workspace and agent selection, plan/tool
  popovers, and a bottom Stream surface so live activity does not flood the main Run page.
- The Context Inspector is a first-class workbench page under `More workbench pages...`. It previews
  packet policy, route, source hashes/ranges, exclusions, token categories, remaining context, and an
  ephemeral Core/Relevant/Expanded choice for the next desktop task. It also shows the exact compact
  tool names and stable packet/tool-schema identities used by quality checks.
- Workspace switching uses the native picker and recent/saved roots. Agent choice is sticky per
  workspace; transient state and path-backed tabs are cleared safely on root changes.

## Agent and tools

- The compact model-facing registry covers file read/write/edit, shell/terminal, HTTP, glob/grep,
  session search, todos, lazy skills, bounded subagents, web/fetch/browser, memory, progress,
  evidence, SQLite, and supporting reasoning controls.
- Destructive actions have confirmation/safe-list support, mutation snapshots, postcondition checks,
  and rollback.
- Auto routing offers Builder, Fixer, Reviewer, Researcher, Planner, Auto, Manual, and the built-in
  Bug Bounty Hunter. Toolsets provide safe, local-code, web-research, browser, memory, offline,
  balanced, and unrestricted capability groups.
- The first native control-plane slice persists a sealed task contract and independent phase
  machine. Planning gates mutations and terminal completion requires blocking evidence.

## Providers and context

- InferenceBridge via the local OpenAI-compatible provider is the default live path.
- OpenRouter is optional and UI-only for keys. Catalogue limits seed sensible cloud context/output
  defaults. A composer selection is a one-task boost and resets to local afterward.
- Local/Hugging Face catalogue models use code-owned Qwen3.6, Qwen3.5, and Gemma4 templates before profile
  creation/update. Profiles exposes an explicit apply action, chat formatting remains the GGUF's
  embedded Jinja, and repeat penalty is transported through settings/UI/native requests. Exact
  small-model templates cover Qwen3.5 4B, Qwythos 9B, Gemma 4 12B, and Gemma 4 E4B; actual model ids
  take precedence over a borrowed profile name.
- Active-provider context mismatch is a hard failure. OpenAI-compatible model-list calls have a
  bounded timeout; chat streaming remains unbounded by the HTTP client's global timeout.
- Context M1-M4 are live: packets are task-aware and capped; unrelated public research receives no
  project documents; the 68 KB handoff is archived and split into domain documents; and a strictly
  validated manifest routes at most three heading-aware excerpts with exact hash/range provenance,
  deterministic compact-core fallback, and a safe UI preview/one-task selector.
- Context M5 phase A is live: seven task classes, three paraphrases each, hostile file/web/browser
  content, and five deterministic repetitions produce a 105-attempt zero-model preflight. The
  production Agent Eval also supports explicit pass^k rounds and aggregate reliability telemetry;
  a live local-profile pass^5 remains a separate operator-run gate.
- Durable memory, SQLite FTS session recall, task-run timelines, RunLedger, Brain, and review-before-
  promotion learning candidates are available.

## Research, files, and engagements

- Web search selects SearXNG, Brave, then DuckDuckGo fallback, with per-task budgets and ranked source
  quality. Fetch and browser tools cover readable pages and interactive sites.
- PDF text extraction exists for text PDFs. Broader any-file extractor/indexing work remains on the
  repository-intelligence plan; scanned PDF OCR is still pending.
- Engagement Grid, scope/evidence bindings, pack library, pentesting evidence profiles, and HTB/CTF
  profile separation are implemented foundations for authorised work.

## Remote and media

- Desktop Fast Chat is a compact no-tools lane with no workspace packet, memory, control-plane,
  planning, or reviewer pass; Project Agent keeps the complete reliable work path.
- Telegram long polling uses the channel bus, separate side chat, explicit queued work runs, progress
  updates, a full-page UI, and redacted ledger events.
- Audio supports incoming transcription and outgoing Kokoro-first/Piper-fallback speech; `ffmpeg` is
  used for Telegram voice-note conversion.

Model Lab separates backend decode, warm end-to-end, load, warm-up, prompt, TTFT, tool, and loop
timing. It warms once, measures text three times, labels timing provenance, and changes Chat only
through an explicit Matrix `Use` action.

## Latest verified baseline

Context M5 phase A passed the production gate on 2026-07-21: the seven-class/three-paraphrase
deterministic harness, canonical-envelope override isolation, hostile-content checks, stable packet
and tool identities, repeated Agent Eval telemetry, full Go tests, vet, app/tools race tests,
frontend production build, clean Wails production build, and a native Benchmark smoke all passed.
The rebuilt app reported pass^5, 7/7 fixtures, 105/105 attempts, hostile guard pass, and zero model
calls against the selected Gemma profile/workspace context. The full live model Agent Eval x5 remains
an explicit separate closure gate. See `verification.md`.

The same date's local-model comparison measured Qwen3.6 27B Fable/Fus Q4_K_M at 90/100 and 33.3
tok/s versus the installed HauhauCS Gemma 4 26B-A4B QAT at 55/100 and 4.3 tok/s. The Gemma 31B
profile referenced a missing artifact and was not scored. Family templates, repeat-penalty transport,
full gates, production build, and native apply/save smoke passed; the rebuilt app was left open.

A follow-up six-model Qwen tournament selected the installed Unsloth Qwen3.6 27B UD-Q4_K_XL as the
best daily local model: 90/100 and 54.8 tok/s across text/coding/JSON/tool scenarios, a clean native
tool call with zero repair, and a passing score-43 six-tool mini loop. Huihui Qwen3.6 27B Q4_K was the
close second at 90/100 and 51.5 tok/s. The 35B-A3B build was rejected as a daily model after 10.9
tok/s, no required structured tool call, and loop score 0. The winning UD profile remains a supervised
recommendation because its separate grammar-constrained probe returned HOLD. Exact HF aliases and
24-GB-safe contexts now exist for UD, Huihui, HauhauCS Aggressive, and the installed 35B artifact.
The UD profile is the persistent local default at 35K context, and `DefaultProfiles` now uses that
same exact GGUF plus InferenceBridge for fresh configurations. Existing user profiles are preserved.
Its native MTP sweep selected `n=3` but measured only `1.00x` versus off, so no speculative-decoding
speedup is claimed.

The 2026-07-23 extended matrix added eight installed models and repaired a model-template matching
defect exposed by the run. Corrected reruns measured Qwen3.5 4B at 125.4 tok/s with a clean tool call
and a pass/66% mini loop, Qwythos 9B at 83.5 tok/s with a clean tool call but fail/40% mini loop, and
Gemma 4 12B at 3.5 tok/s with a clean tool call and pass/80% mini loop. Corrected E4B Q4/Q8 reruns
remain pending after desktop activity stopped automation. The prior UD/Huihui decision remains
unchanged. See `../local-model-tournament-2026-07-23.md`.
