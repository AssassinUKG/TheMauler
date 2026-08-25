# Current state

Last consolidated: 2026-08-24. This is a concise operational view, not an exhaustive release log.
The pre-split full inventory is preserved in
`../archive/agents-handoff-snapshot-2026-07-21.md`.

## Workbench

- Native Wails desktop shell with a React/TypeScript workbench and Monaco editor.
- Explorer, Inspector, bottom work area, and Terminal/AI Commands retain visible draggable
  separators, collapsed rails, keyboard resizing, narrow-layout behavior, and persistence.
- Terminal preserves the user's selected panel height when a run starts, uses a higher-contrast
  readable text surface with UI-only A-/A+ sizing, and renders AI command paths as wrapped multi-line
  cards instead of squeezing them into an unreadable single row.
- Chat supports streaming markdown, sessions, workspace and agent selection, plan/tool popovers, and
  a bottom Stream surface so live activity does not flood the main Run page. Its native **Attach
  files** picker accepts multiple files; Explorer-copied absolute paths and dropped paths use the
  same validation. Path attachments remain metadata-only so complete large files are read through
  bounded chunks/extractors instead of being silently stuffed into one prompt.
- Chat renders a streaming answer as one cohesive response, gives GFM tables the available message
  width with readable headers/rows and narrow-screen horizontal scrolling, and preserves completed
  streamed text if a later run-finalisation error occurs. If a read-only shell check captures the
  requested result but finalisation stops before the model relays it, the controller promotes the
  cleaned successful evidence into Chat; raw contracts, project-verifier output, guarded/untrusted
  content, and mutation runs are excluded.
- The Context Inspector is a first-class workbench page under `More workbench pages...`. It previews
  packet policy, route, source hashes/ranges, exclusions, token categories, remaining context, and an
  ephemeral Core/Relevant/Expanded choice for the next desktop task. It also shows the exact compact
  tool names and stable packet/tool-schema identities used by quality checks.
- Workspace switching uses the native picker and recent/saved roots. Agent choice is sticky per
  workspace; transient state and path-backed tabs are cleared safely on root changes.
- While a task is running, Chat labels the actual selected **Route** rather than leaving the
  configured **Agent Auto** label visible. Answer-shaped requests such as creating a table, summary,
  report, count, or plan remain read-only unless the prompt separately asks to save/edit/update a
  workspace artifact.

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
- Read-only file/API inventory questions stay read-only. HTTP method names such as `PATCH` and
  `DELETE` do not select Builder, create a workspace-mutation contract, or launch project build
  verification. The Engagement tool is routed only for explicit Engagement Grid work.
- Windows-host fact requests (including locally running processes/games/apps, services, Task Manager,
  windows, and GPU/VRAM state) use a code-owned native PowerShell one-shot route even when the
  shared target terminal is WSL/Kali. The shell tool accepts a per-call backend, strips redundant
  `powershell.exe -Command` wrappers without changing `$` variables, and keeps native host commands
  out of an ordinary WSL prompt. Explicit Kali/WSL target work and genuine connected remote shells
  retain their existing routes.

## Providers and context

- InferenceBridge via the local OpenAI-compatible provider is the default live path.
- OpenRouter is optional and UI-only for keys. Catalogue limits seed sensible cloud context/output
  defaults. A composer selection is a one-task boost and resets to local afterward.
- Local/Hugging Face catalogue models use code-owned Qwen3.8, Qwen3.6, Qwen3.5, and Gemma4 templates before profile
  creation/update. Profiles exposes an explicit apply action, chat formatting remains the GGUF's
  embedded Jinja, and repeat penalty is transported through settings/UI/native requests. Exact
  small-model templates cover Qwen3.5 4B, Qwythos 9B, Gemma 4 12B, and Gemma 4 E4B; actual model ids
  take precedence over a borrowed profile name.
- Qwen3.8 profiles have a guided RTX 3090 setup card for official thinking/direct samplers, preserved
  reasoning, 35K local context, FP16 KV cache, reasoning depth, and provisional MTP. Preserved
  `reasoning_content` travels between local model turns and participates in bounded history
  compaction; it is not sent through generic cloud-compatible providers.
- Inspector > Agent > Behaviour exposes a persistent Chat-level **Thinking** override with
  **Profile**, **On**, and **Off** choices. Profile retains the selected model profile and adaptive
  recovery policy. On keeps supported Qwen/Gemma thinking templates enabled for the complete run
  while Reasoning Effort controls depth. Off selects direct/no-thinking sampling and does not
  preserve reasoning. Unsupported templates are reported rather than being falsely treated as
  thinking-capable.
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
  promotion learning candidates are available. Empty user-stopped runs are not promoted to run
  memory, and legacy stopped-run narration is withheld from automatic reinjection unless pinned.

## Research, files, and engagements

- Web search selects SearXNG, Brave, then DuckDuckGo fallback, with per-task budgets and ranked source
  quality. Fetch and browser tools cover readable pages and interactive sites.
- PDF text extraction exists for text PDFs. Broader any-file extractor/indexing work remains on the
  repository-intelligence plan; scanned PDF OCR is still pending.
- Home has a UI-first multi-target **Authorised scope** editor for IPs, CIDRs, hostnames, ports, and
  URL/path prefixes, with ordered primary selection, External/Internal labels, per-row notes,
  exclusions, list paste, validation, and legacy comma-list migration. Exclusions override broader
  allows. The complete list is locked into Engagement Grid state; older scalar target surfaces edit
  only the primary compatibility target.
- Engagement Grid, scope/evidence bindings, pack library, pentesting evidence profiles, and HTB/CTF
  profile separation are implemented foundations for authorised work.

## Remote and media

- Desktop Fast Chat is a compact no-tools lane with no workspace packet, memory, control-plane,
  planning, or reviewer pass; Project Agent keeps the complete reliable work path.
- Telegram long polling uses the channel bus, separate side chat, automatic natural-language task
  promotion, optional explicit `/cmd`/`/run` overrides, safe queueing, progress updates, a full-page
  UI, and redacted ledger events. Completed task/result pairs are bridged back into the same Telegram
  conversation so follow-ups such as “what’s the result?” resolve deterministically. Current weather,
  news, prices, scores, and travel-state questions require fresh tool evidence rather than model
  memory. Final cards use Telegram formatting, preserve the complete answer across multiple messages,
  and deliver the actual final assistant answer while rejecting unexecuted raw tool-call markup as a
  successful result. Date-range wording such as “weather over the next seven days” is treated as live
  work. If side chat explicitly offers an agent route, a later “route it” deterministically starts the
  remembered request instead of allowing the no-tools model to promise work. A timer-backed heartbeat
  refreshes the single live progress card at the configured cadence even during a quiet model/tool call.
- Audio supports incoming transcription and outgoing Kokoro-first/Piper-fallback speech; `ffmpeg` is
  used for Telegram voice-note conversion.

Model Lab separates backend decode, warm end-to-end, load, warm-up, prompt, TTFT, tool, and loop
timing. It warms once, measures text three times, labels timing provenance, and changes Chat only
through an explicit Matrix `Use` action.

## Latest verified baseline

On 2026-08-16 the fresh-install and live desktop default moved to
`Qwen3.8-27B-Q4_K_M.gguf` through InferenceBridge at the verified 35K RTX 3090 working context.
The profile preserves thinking and uses Qwen's thinking sampler for planning/review, while direct
tool/no-thinking turns use temperature 0.7, top-p 0.8, top-k 20, presence penalty 1.5, and repeat
penalty 1.0. Planner/reviewer default to `xhigh`, fixer to `high`, and other thinking work to
`medium`; tool-call recovery can still force no-thinking after bounded failures when Chat Thinking
is set to Profile, while an explicit On selection remains authoritative for that run. Completion
blocking is on by default, so the desktop cannot report success without its existing evidence rail. Native
MTP starts conservatively at `n=2` and remains benchmark-required rather than a claimed speedup.
The official 262K native window is documented but is not used as the 24-GB local default.

The 2026-07-23 clean-build follow-up repaired GitHub CI, updated the frontend toolchain to Vite
8.1.5, pinned Monaco's sanitizer to patched DOMPurify 3.4.12, and reduced the largest JavaScript
chunk from roughly 1.12 MB to 473 KB. `npm audit` reports zero known vulnerabilities. Strict
execution ordering protects the manually split bundle inside WebView2; a native production smoke
confirmed normal startup, Monaco file rendering, the ready xterm terminal, preserved resizable
workbench surfaces, the 35K profile, and a healthy InferenceBridge backend.

Context M5 phase A passed the production gate on 2026-07-21: the seven-class/three-paraphrase
deterministic harness, canonical-envelope override isolation, hostile-content checks, stable packet
and tool identities, repeated Agent Eval telemetry, full Go tests, vet, app/tools race tests,
frontend production build, clean Wails production build, and a native Benchmark smoke all passed.
The rebuilt app reported pass^5, 7/7 fixtures, 105/105 attempts, hostile guard pass, and zero model
calls against the selected Gemma profile/workspace context. The full live model Agent Eval x5 remains
an explicit separate closure gate. See `verification.md`.

On 2026-07-23 the repaired live Huihui 27B/35K Gate 1 reached 11/12 after product-level scorer,
overwrite-protection, controller-message, completion-rail, planning-only, and cache-invalidation
repairs. The final failure was genuine model-loop behavior: `chunked-write` omitted `append=true`
three times after explicit correction. Mauler prevented data loss and stopped through its circuit
breaker. Gate 1 is not pass^1, x5 was not started, and Huihui remains supervised-only for agent
loops. See `../huihui-qwen36-agent-eval-2026-07-21.md`.

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
The UD profile was the persistent local default at 35K context until the Qwen3.8 replacement above.
It remains available as a retained comparison/fallback profile; it is no longer the fresh-install
or live desktop default.
Its native MTP sweep selected `n=3` but measured only `1.00x` versus off, so no speculative-decoding
speedup is claimed.

The 2026-07-23 extended matrix added eight installed models and repaired a model-template matching
defect exposed by the run. Corrected reruns measured Qwen3.5 4B at 125.4 tok/s with a clean tool call
and a pass/66% mini loop, Qwythos 9B at 83.5 tok/s with a clean tool call but fail/40% mini loop, and
Gemma 4 12B at 3.5 tok/s with a clean tool call and pass/80% mini loop. Corrected E4B Q4/Q8 reruns
remain pending after desktop activity stopped automation. The prior UD/Huihui decision remains
unchanged. See `../local-model-tournament-2026-07-23.md`.
