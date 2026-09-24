# Current state

Last consolidated: 2026-09-24. This is a concise operational view, not an exhaustive release log.
The pre-split full inventory is preserved in
`../archive/agents-handoff-snapshot-2026-07-21.md`.

## Workbench

- Native Wails desktop shell with a React/TypeScript workbench and Monaco editor.
- Chat opens first in a conversation-first shell: the sidebar owns New chat, title search, saved
  history, direct open, and a secondary collapsible Workbench. Projects are optional durable
  **Workspaces**. Complete retains model turns, tools, run milestones, and browser events with
  non-destructive filters/search. Explicit persistent conversation scratch can be promoted in place.
  See `../chat-first-workbench-redesign-2026-09.md`.
- Explorer, Inspector, bottom work area, and Terminal/AI Commands retain visible draggable
  separators, collapsed rails, keyboard resizing, narrow-layout behavior, and persistence.
- One View command menu and panel-local close controls govern those surfaces; Focus chat closes them
  together to zero-width columns, optional edge rails can be enabled, and automatic run-panel
  opening is an opt-in preference rather than a forced layout.
- Chat's floating Inspector explicitly spans the complete overlay grid rather than inheriting the
  docked workbench's zero-width fifth track. Opening it recovers a stale stored width to the supported
  range before mounting. Title-bar panels and Chat's Tools/View/Run mode menus use consistent vector
  icons, larger action targets, and accent-led active states.
- The conversation sidebar has persistent operator-owned Workbench disclosure, outside-click/Escape
  dismissal for conversation actions, saved/matching chat counts, a compact workspace/model footer,
  and distinct empty search feedback. At laptop widths it overlays Chat instead of narrowing the
  transcript and dismisses after starting, opening, or navigating a conversation. Saved-chat rows
  open immediately, and the sidebar and title-bar Conversation menu share the same guarded New,
  Save, Rename, Check/repair, and Delete action implementation so lifecycle behaviour cannot drift.
  Rename moves the transcript and its searchable recall identity together, rejects collisions, and
  rolls back the file if index migration fails. Saved rows are newest-first, show recency/message
  count, retain malformed transcripts with a Review badge, and expose per-chat Rename, Check/repair,
  and Delete without forcing the operator to open that chat first. Bounded persistent tags render as
  row chips and filter/search across both navigation surfaces; rename/delete keep tag identity in
  sync, while damaged optional tag metadata cannot hide transcript files.
- Chat now owns the full centre canvas instead of permanently surrendering a column and row to
  workbench chrome. Inspector opens as a resizable right drawer and Terminal/AI Commands as a
  resizable bottom drawer; both fully unmount when closed, stack without overlap, and remain docked
  on specialist workbench pages. Chats, Inspector, and Terminal are direct title-bar controls, with
  lower-frequency layout commands behind the overflow menu. Doctor and Settings also live in that
  grouped Workbench menu rather than competing with the three immediate surface controls. A one-time
  layout migration dismisses stale open-panel state without discarding the operator's saved dimensions.
- Plan, Tools, Workspace, Agent, and Run setup live in one compact task bar above the composer.
  Their detailed menus float above Chat rather than expanding into a large settings grid; model
  route and Supervised/Automatic confirmation mode are grouped in Run setup. Clear plan is an
  awaited, status-reporting action and keeps the SQLite plan plus legacy recovery copy synchronized,
  so a cleared checklist cannot be re-imported on refresh. Empty plan reads are guaranteed to cross
  the Wails boundary as `[]`; Chat also normalises legacy/null responses before rendering.
- Those five task menus use one readable visual hierarchy with icon/title/subtitle headers, explicit
  state chips, plain-language sections, larger action targets, and responsive widths. Plan steps wrap
  and suppress model-authored `TODO-n` decoration at presentation time; Tools separates current task,
  connected surfaces, queue, and recent activity; Workspace and Agent give names/descriptions priority
  over low-level metadata.
- Workspace now creates a durable project directly from Chat: the operator names it, chooses the
  parent folder in the native picker, and may open it with Bug Bounty Hunter. Go validates Windows
  names, refuses collisions, creates exactly one child folder, registers it as the active project,
  and switches through the authoritative workspace lifecycle so old chat/tool/todo/path context
  cannot leak into the new project.
- Empty Chat provides descriptive launch cards for workspace review, safe run continuation, planning,
  and the authorised Security workspace. The composer's More menu mirrors the primary task surfaces
  (Security, repository files, native browser) and groups Undo/Clear separately as conversation
  actions; choosing a surface does not submit a model prompt or broaden engagement scope.
- Chat's command strip is consolidated into **Tools**, **View**, and **Run mode** menus. Tools owns
  Security, searchable workspace files, and the native browser; View owns search, persistent output
  following, transcript density, and tool/status/browser event visibility. Run mode is saved per
  conversation: **Adaptive** answers ordinary questions directly but retains the full tool/evidence
  loop for tasks, **Always direct** is one text-only response, and **Always agent** suppresses the
  direct shortcut. The separate context-free Fast Chat window remains available. Live tool state
  remains visible without restoring the previous row of competing buttons. Both conversation
  navigation surfaces filter saved chats by effective mode, identify the current chat with its mode
  and message count, and expose an in-row mode picker. Updating a background row persists only that
  saved chat and cannot silently alter the active run mode. Settings > Agents defines the mode used
  only for newly created chats.
- Chat has a persistent Security workspace that projects the current locked Engagement Grid into the
  conversation: scope, phase, coverage, evidence, next work, and an `Observed → Draft → Validated →
  Rejected` findings inbox. File evidence is re-hashed by Go; changed/missing evidence returns a
  confirmed item to the Draft lane as revalidation-required. The Grid and Go engagement service
  remain authoritative for every mutation and confirmation.
- Terminal preserves the user's selected panel height when a run starts, uses a higher-contrast
  readable text surface with UI-only A-/A+ sizing, and renders AI command paths as wrapped multi-line
  cards instead of squeezing them into an unreadable single row. The floating drawer opens at a
  useful 320-pixel height, clamps to the current viewport with a 280-pixel floor, and debounces PTY
  resize notifications so dragging the work surface cannot spam shell prompt redraws. Terminal and
  AI Commands also re-clamp their split after a window resize. While the drawer is open, Chat's
  transcript and composer follow its live height so the input never sits underneath the terminal.
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
- Chat auto-scroll is operator-controlled and persistent. Scrolling upward pauses following; the
  header shows the current Follow/Paused state. Model prose paired with a tool call is labelled as an
  Agent update and remains available through Complete transcript mode without masquerading as a
  repeated final answer.
- The loop guard treats exact cached rereads as no-progress signals and first offers a synthesis exit:
  when enough read-only evidence already exists, the model must stop tools and answer directly. If
  the guard still pauses redundant work, a substantive evidence-backed recovery answer is handed to
  desktop and Telegram as **Answer recovered** without the misleading finalisation-failed banner;
  the underlying blocked loop remains recorded in RunLedger and control state.
- Lifecycle ownership M1 is active: new task runs receive a persisted monotonic generation and typed
  conversation epoch; live run events carry `run_id`/`generation`/`conversation_epoch` ownership.
  Epochs advance on clear, load, workspace/scratch replacement and checkpoint resume, so the Go
  event boundary suppresses retired-run emissions before Desktop Chat applies its own ownership
  check. Context-owned image progress and HTTP artifact refreshes cross the same boundary, while
  Chat owner-checks task refresh, workspace-file and learning-suggestion events. Both Go and the UI
  refuse to clear or load a different conversation while a run executes.
  Doctor/Services expose a payload-free stale-event counter and latest rejected owner for diagnosis.
- Verified mutation handoffs persist finalized artifact path, SHA-256, size, run/generation/epoch and
  verifier-evidence provenance. Task-run reads derive whether those exact bytes are still fresh,
  changed or missing without overwriting the captured fingerprint. Later tasks import those files as
  protected contract boundaries: only an explicit Fixer request naming the exact file receives repair
  scope, and the repaired bytes must earn a new verifier-backed handoff. Mutation evidence
  canonicalizes relative tool paths against the authoritative workspace before sealing that handoff.
- Deterministic session repair M2 validates and normalizes history before model transport. Its nine
  phases preserve structured content and distinct parallel tool calls, emit typed ledger actions,
  and reject unusable packets instead of sending a blank request. Saved sessions have UI health
  preview and explicit backup/apply. Both conversation menus open a first-class checkpoint manager:
  named transcript snapshots are reusable, automatic crash-recovery snapshots remain distinct, and
  resume restores the visible transcript plus a visible continuation turn before opening a new run
  generation linked to the captured parent. Create/resume/delete are blocked while another run is
  active.
- Tool-created artifacts refresh Explorer/Inspector through a file-change event that does not clear
  Chat, replace its draft, or remount the active terminal. If an active stream has no visible token
  yet, Chat displays its current run state instead of an unexplained empty centre pane.
- The Context Inspector is a first-class workbench page under `More workbench pages...`. It previews
  packet policy, route, source hashes/ranges, exclusions, token categories, remaining context, and an
  ephemeral Core/Relevant/Expanded choice for the next desktop task. It also shows the exact compact
  tool names and stable packet/tool-schema identities used by quality checks.
- Workspace switching uses the native picker and recent/saved roots. Agent choice is sticky per
  workspace; transient state and path-backed tabs are cleared safely on root changes.
- Projects can be removed directly from the selected-project header through an in-app confirmation.
  Removal only drops Mauler's saved project entry; workspace files, evidence, notes, and sessions
  remain on disk. Mauler prevents removing the only active project and offers to create its
  replacement first.
- While a task is running, Chat labels the actual selected **Route** rather than leaving the
  configured **Agent Auto** label visible. Answer-shaped requests such as creating a table, summary,
  report, count, or plan remain read-only unless the prompt separately asks to save/edit/update a
  workspace artifact. Command-composition requests such as “give me the curl command” also use one
  direct text response, even when they contain PoC, exploit, payload, target, or callback wording;
  only explicit run/test/validate wording authorises execution. Out-of-band findings require an
  observed callback event before Chat can describe them as confirmed or validated.

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
- Inspector > Agent > Tools separates capability selection from per-task routing. **Automatic** is
  the compact default and preserves `write`/`edit` when a mixed research request explicitly asks to
  write, save, place, or leave an artifact in the workspace. Every model turn also receives the exact
  routed tool names as its authoritative capability packet, preventing enabled-tool summaries from
  being mistaken for the narrower turn route. **Selected tools** bypasses semantic narrowing
  and advertises every enabled member of the active toolset; capability, confirmation, protected-path,
  engagement-scope, and control-plane policy gates still apply.
- Windows-host fact requests (including locally running processes/games/apps, services, Task Manager,
  windows, and GPU/VRAM state) use a code-owned native PowerShell one-shot route even when the
  shared target terminal is WSL/Kali. The shell tool accepts a per-call backend, strips redundant
  `powershell.exe -Command` wrappers without changing `$` variables, and keeps native host commands
  out of an ordinary WSL prompt. Explicit Kali/WSL target work and genuine connected remote shells
  retain their existing routes.
- Independent `curl`/`wget` commands accidentally selected through `terminal_send` are rewritten
  before history persistence to a 30-second isolated WSL `shell` call. The tool-call name, policy,
  result, ledger, and evidence therefore stay consistent, while the shared interactive terminal
  remains usable. Model-selected `shell` calls with explicit WSL/bash backends are isolated too.
- Interactive account, signup, form, login, verification, CAPTCHA, MFA, and OTP wording now routes
  to the compact native browser instead of a fetch-only or file-only lane. Chrome/Edge state is
  persistent but conversation-owned; cookies cannot cross owners and are retired on clear, load,
  workspace switch, or shutdown. Services exposes visible launch, status/title/URL, pause, human
  takeover, controller-observed resume, and stop controls. Chat now exposes a first-class Browser
  button that launches a visible native session and reports live readiness, URL/title, tab count,
  and controller state with Take over, Resume, Stop, and refresh controls. A model-issued takeover
  opens the surface automatically, blocks the same run, and remains visible until the user continues
  or stops it. The model's fresh execution-state packet now receives sanitized active-session
  metadata, and references such as “can you see it?” require a browser snapshot instead of being
  misrouted through shell/curl. Snapshots issue
  bounded stable refs for interactive elements; uploads are confined to regular workspace files and
  downloads are saved under `.mauler/browser-downloads` with size/SHA-256 RunLedger evidence.
  Classified recovery prevents blind replay of ambiguous click/submit/upload/download outcomes, and
  the deterministic native-browser ref/upload/download reliability gate passes 5/5. Stable
  conversation-owned tab refs support open/list/switch/close, and Services can save metadata-only
  checkpoints that strip query/fragment/userinfo and never retain credentials, cookies, or form
  values. Every browser phase has bounded cancellation coverage. Typed values are not returned in
  tool output. Explicit open/show/launch wording is enforced as a visible session even when a local
  model omits the flag; Chat can explicitly restart an existing headless session visibly. Reopening
  a host already blocked by CAPTCHA, Cloudflare, unusual traffic, or access denial is skipped. See
  `../browser-workflow-assistant-plan-2026-09.md`.

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
- Chat's composer and Inspector > Agent > Behaviour expose the same persistent Chat-level
  **Thinking** override with **Profile**, **Always think**, and **Direct** choices. The composer adds
  an effort picker beside the attachment control and shows the active model; Qwen3.8 is limited to
  its advertised Auto/Low/Medium/XHigh ladder rather than displaying unsupported generic tiers.
  Profile retains the selected model profile and adaptive
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
- Workspace repository intelligence now has a deterministic streaming production slice: supported
  UTF-8/UTF-16 text/code/config files are hashed and emitted as bounded, line-addressed untrusted
  chunks into immutable SQLite FTS5 generations. Cancelled/failed replacements never displace the
  active complete generation, and only entries with a final `indexed` verdict are searchable. The
  compact `memory` tool owns index/status/search actions and returns bounded hash-cited excerpts.
  Chat's **Files** card can build/rebuild the authoritative current workspace index and shows exact
  coverage, chunks, bytes, generation/manifest provenance, and every explicit omission category.
  Replacement scans expose live metadata-only path/file/chunk progress and a responsive Cancel
  action; workspace switching is blocked until the scan completes or cancels, and shutdown waits
  briefly for rollback before closing SQLite. Doctor and Services report the same index health.
  The rich-format registry adds readable PDF and DOCX/PPTX/XLSX/ODS text, bounded ZIP and TAR/TGZ
  inventory metadata, SQLite schema metadata, and PE/ELF structural metadata. Archive member bodies
  and SQLite row values are not indexed. Expanded bytes, archive/metadata entries and extractor time
  are limited; corrupt, encrypted, scanned/no-text and over-expanded inputs remain explicit
  non-indexed manifest entries.
  Incremental refresh now creates a fresh immutable generation while copying only SHA-verified
  unchanged chunks and re-extracting changed/new files; deletions disappear only when the complete
  replacement commits. The Files card exposes manual **Refresh changes**, exact reused/changed/
  deleted counts, and a workspace-scoped persisted Watch switch with live state and last-check time.
  Watch uses low-cost metadata detection, but all candidate reuse is still verified by SHA-256.
  Brain now provides the corresponding first-class operations surface over that same backend status:
  a code-owned healthy/indexing/not-indexed/attention/unavailable verdict, coverage and provenance,
  Watch health, incremental reuse facts, bounded omission review, live scan progress, and direct
  Refresh/Watch/Rebuild/Cancel controls. It does not maintain a second index state.
  The M3 split-review path now derives deterministic read-only shards from that same immutable
  generation and can execute them as conservative sequential child reviews. Each child receives an
  empty ambient tool registry plus one shard-sealed `review_evidence` capability for bounded catalog,
  FTS search, and exact chunk reads. Backend-only contracts contain the allowed chunk IDs, file/text
  hashes and line ranges; both reads and searches revalidate those references before returning source.
  Brain shows one parent review card with live progress, cancel, failed/cancelled-shard retry, merge
  counts, conflicts, and merged drafts. The controller merge gate rejects narrative-only, stale-hash,
  cross-shard and out-of-range findings, deduplicates matching claim/evidence pairs, and preserves
  contradictions for independent review. Each deterministic conflict contract now runs in a fresh
  bounded reviewer context with no child reasoning or ambient tools; its `review_evidence` capability
  exposes only the exact disputed immutable chunks. Strict `compatible`, `prefer`, `unsupported`, or
  `inconclusive` verdicts are validated against stable controller-owned claim IDs, persisted, resumed,
  and rendered in Brain with an explicit adjudication-not-proof caveat. Evidence ownership and an
  independent verdict are never mislabeled as proof that a child's prose claim is true. Schema v18
  persists the parent contract, shard attempts,
  submissions, and deterministic merge decisions transactionally. Startup turns an unclean active
  review into an explicit interrupted state; Brain can resume unfinished shards or conflict checks only
  after revalidating the active generation, manifest, and rebuilt plan digest. Completed shard
  submissions and adjudications remain intact.

## Research, files, and engagements

- Web search selects SearXNG, Brave, then DuckDuckGo fallback, with per-task budgets and ranked source
  quality. Fetch and browser tools cover readable pages and interactive sites.
- Readable PDFs, DOCX/PPTX/XLSX/ODS text, ZIP and TAR/TGZ inventories, SQLite schema metadata, and
  PE/ELF structural metadata now participate in workspace indexing and keyword search. Scanned-PDF
  OCR and 7z remain on the repository-intelligence plan. Archive member bodies and SQLite row values
  are deliberately excluded; any future row sampling requires a separate explicit privacy policy.
- The Files card can add exact files or folders as explicit read-only knowledge sources. Sources
  persist per authoritative workspace, are visible/removable in Chat, and affect only the immutable
  index policy—not file-tool scope or engagement authorization. Changing sources makes the previous
  policy generation inactive until rebuilt; individual-file roots retain exact provenance.
- Home has a UI-first multi-target **Authorised scope** editor for IPs, CIDRs, hostnames, ports, and
  URL/path prefixes, with ordered primary selection, External/Internal labels, per-row notes,
  exclusions, list paste, validation, and legacy comma-list migration. Exclusions override broader
  allows. The complete list is locked into Engagement Grid state; older scalar target surfaces edit
  only the primary compatibility target.
- Engagement Grid, scope/evidence bindings, pack library, pentesting evidence profiles, and HTB/CTF
  profile separation are implemented foundations for authorised work.
- Run-owned image-generation progress carries the same run/generation owner as the invoking tool;
  late progress from a retired task cannot alter the replacement Chat activity state.

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

- Context-manifest source budgeting now reserves input for every selected document before the
  heading-aware compiler runs. The channels/media route can no longer lose its final troubleshooting
  source as earlier canonical documents grow. The deterministic context gate passes 7/7 fixtures
  and 105/105 attempts.

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
