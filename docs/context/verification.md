# Verification

Run from the repository root. Start focused, then widen in proportion to the changed surface.

## Standard production gate

```powershell
go test ./... -count=1
go vet ./...
go test -race ./internal/app ./internal/tools -count=1
npm run --prefix frontend build
wails build
```

Use `./build.ps1 -Run` to build and relaunch the Windows production app. Use `wails dev` for hot
reload. On Linux/WSL use the documented `build.sh` variants.

## GitHub clean-checkout gate

The GitHub Go job runs on Ubuntu and must build `frontend/dist` before any `go build`, `go vet`, or
`go test` command that compiles `main.go`; the embedded frontend output is intentionally ignored by
Git. Keep Windows paths in settings and policy tests host-independent so the Linux gate continues to
exercise WSL/relay portability rather than being replaced with a Windows-only runner.

## Focused gates

- Control plane/store: `go test -race ./internal/controlplane ./internal/store -count=1`
- Engagement/packs: `go test -race ./internal/engagement/... ./internal/packlibrary -count=1`
- App/tools integration: `go test -race ./internal/app ./internal/tools -count=1`
- Frontend type/production bundle: `npm run --prefix frontend build`
- Context compilation: `go test ./internal/app -run 'ProjectInstruction|ContextDocuments' -count=1`
- Manifest routing/security: `go test ./internal/app -run 'ManifestContext|ActiveManifest|InvalidManifest|ManifestRejects' -count=1`
- Context Inspector/pinning: `go test ./internal/app -run 'ContextInspector|ContextPacketPin|MessagesWithPrimary|RepositoryContextDocuments' -count=1`
- Context M5 deterministic/repeated harness: `go test ./internal/app -run 'ContextQuality|AgentEval|HashToolDefs|ShellRoutingDoesNotTreatKeeping' -count=1`
- Repository intelligence: `go test -race ./internal/repoindex ./internal/store ./internal/app -run 'Repo|RepositoryIndex|MemoryTool|Store' -count=1`

Run `gofmt` on changed Go files and `git diff --check` on the files in scope. Do not format or revert
unrelated user work.

## Live smoke expectations

Use a production/native smoke when changing Wails/UI/provider/runtime behavior. Relevant checks may
include:

- app launches and responds;
- local provider remains selected after one-task cloud boost resets;
- Chat agent/workspace menus remain visible above a tall bottom panel;
- all workbench separators drag, collapsed edges reopen, and dimensions survive reload;
- terminal starts in a real validated workspace and AI command output is not duplicated;
- Telegram receives actual tool/final results, not only a completion status; direct actionable text
  works without `/cmd`, current public facts require a real tool, raw tool protocol cannot complete a
  run, natural multi-day weather wording routes to work, “route it” preserves the pending task, quiet
  runs keep one timer-refreshed progress card, long final answers are not truncated, and same-chat
  result follow-ups return the prior result;
- context inspector/event provenance matches the packet actually sent.
- Context Quality pass^5 reports 105/105 deterministic attempts and zero model calls; Agent Eval x5
  is treated as a separate, expensive live gate whose actual selected profile and result are named.

## Known non-gates

Repository-wide frontend lint has a pre-existing backlog across generated Wails bindings and older
components. It is not green and must not be presented as passing. Frontend production build/type
checking is the current gate unless a task explicitly scopes lint cleanup.

## Latest recorded baseline

- 2026-09-24 repository-intelligence M4 metadata extractors: TAR/TGZ files contribute bounded,
  deterministic inventories; SQLite contributes read-only schema/object/column metadata; and PE/ELF
  files contribute structural header, section, and imported-library metadata. Archive member bodies
  and SQLite row values are deliberately not indexed. Focused fixtures prove deterministic manifests,
  bounded untrusted chunks, content non-leakage, and explicit corrupt/over-limit non-coverage. The
  complete repoindex package, `go test ./... -count=1`, `go vet ./...`, required app/tools race tests,
  frontend production type/build, and `wails build` passed. A 15-second hidden launch with an isolated
  temporary `MAULER_CONFIG_DIR` stayed alive and left the user's persisted state untouched. Canonical
  output is `build/bin/TheMauler.exe` (SHA-256
  `1DD375D53F930D3A86982C9F4518C023A88437148C9A59154FC18AC7D2B93FA0`).

- 2026-09-24 repository-review independent conflict checks: deterministic conflict contracts group
  draft claims over exact canonical evidence and assign stable code-owned claim IDs. Each conflict
  runs in a fresh bounded reviewer context with no child reasoning or ambient tools; `review_evidence`
  exposes only the disputed sealed chunks. The controller strictly validates compatible/prefer/
  unsupported/inconclusive verdicts, durable restore reconciles legacy pending conflicts, and corrupt
  claim selections or contract drift fail replay. Brain renders check progress/verdicts with an
  adjudication-not-proof caveat. Focused conflict/scope/persistence tests, complete repoindex/store/app
  suites, `go test ./... -count=1`, `go vet ./...`, required app/tools race tests, frontend production
  type/build, and `wails build` passed. Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `AC9614E0A23B07A02B508171C1BFB615C98764E99A84E84D968EFAC48BE9D21C`). A 15-second hidden startup
  smoke with an isolated temporary `MAULER_CONFIG_DIR` kept the rebuilt process responsive and left
  the user's existing instance/database untouched. The Computer Use provider exposed no native app
  surface, so visual acceptance and a supervised live-model conflict case remain open.

- 2026-09-20 Chat project creation and selected knowledge sources: Workspace can create/register a
  named project under a native-picked parent and switch through the authoritative clean-context
  lifecycle, optionally selecting Bug Bounty Hunter. Files can add/remove workspace-scoped,
  read-only file or folder roots; roots participate in the immutable policy digest and exact files
  are accepted by the bounded scanner without expanding tool scope. Focused lifecycle/scanner tests,
  full `go test ./... -count=1`, `go vet ./...`, required app/tools race tests, and the frontend
  production type/build gate passed. The production Wails build launched responsively in the hidden
  startup smoke; `build/bin/TheMauler.exe` has SHA-256
  `3EE3366F70C5C67C2B237A23A02B6CEF96D5310392FCA2799FBB51887A3F6EDA`.

- 2026-09-17 named conversation checkpoint/resume: a shared Chat manager creates reusable named
  transcript snapshots, distinguishes automatic recovery state, restores a UI-safe transcript with
  the exact visible continuation turn, and resumes through the existing parent-linked generation
  path. Named snapshots survive successful resume; create/resume/delete are locked during active
  runs. Focused checkpoint tests, full `go test ./... -count=1`, `go vet ./...`, the required
  `go test -race ./internal/app ./internal/tools -count=1`, and the frontend production type/build
  gate passed. The Wails production build launched responsively as PID 34700 with SHA-256
  `CBD1D6FF775AF32B0BC31623579C79D72239001AD08AEFDC22E42F23879B2F41`.
- 2026-09-17 saved-conversation mode navigation: both conversation surfaces filter by Adaptive,
  Direct, or Agent mode; the title-bar menu identifies the current chat's mode/message count; and
  each saved row can change mode in place. A dedicated background-update binding prevents changing
  another saved chat from mutating the active run mode. The focused lifecycle regression, full
  `go test ./... -count=1`, full `go vet ./...`, frontend production type/build gate, scoped diff
  check, Wails production build, and responsive rebuilt native launch passed. The launched binary
  SHA-256 was `2D5B1D2D6541CB51BD6D88F441816847EF3A1291D6035B835FE6D5E39FA945ED`.
- 2026-09-16 empty-plan render-crash repair: SQLite plan reads initialise an empty non-nil slice,
  legacy JSON null items normalise to an empty slice, and JSON writes preserve the same array
  contract. Chat independently normalises Wails results before state/render. The regression asserts
  that a cleared plan marshals as `[]`; focused todo tests, full Go suite, vet, frontend type/
  production build, scoped diff check, and Wails production build pass. Canonical output is
  `build/bin/TheMauler.exe` (SHA-256
  `8C0CAAFE3ECD755B0AD77B5922ECAA9D54BBBEF89755C1CDECB8C520A29B640F`) and is running responsively as
  PID 42884.

- 2026-09-16 readable task-menu pass: Plan, Tools, Workspace, Agent, and Run setup now use one
  icon-led menu hierarchy with larger text/targets, readable status chips, plain-language sections,
  wrapped plan steps, simplified connected-surface state, and responsive desktop/narrow layouts.
  Frontend type/production build, scoped handwritten-source diff check, and the Wails production
  build pass. Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `9EE21944149D1615C49572DC2EE5E370E4A7311762DF13454EF5B87781AC7E73`) and is running responsively as
  PID 53096. The Computer Use provider exposed browser surfaces only, so native visual acceptance
  remains for the operator and is not claimed here.

- 2026-09-16 compact task controls/Clear Plan repair: Plan, Tools, Workspace, Agent, and Run setup
  now form one labelled task bar above the composer; model routing and Supervised/Automatic mode
  moved into the focused Run setup menu. Clear plan awaits the backend mutation, reports its result,
  and synchronizes SQLite with the legacy JSON recovery copy so refresh cannot re-import a cleared
  checklist. The stale-migration regression, focused tools tests, full Go suite, vet, frontend
  production/type build, scoped handwritten-source diff check, and Wails production build pass.
  Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `3A8ED74420A0EDC57672491309BBF1743376726877FD48611332ECB559F84F77`) and is running responsively as
  PID 32728. Native visual acceptance remains for the operator.

- 2026-09-16 persistent conversation tags/current hardening: saved chats accept up to six bounded
  tags, render compact chips, match title-or-tag search, and expose shared one-click tag filters in
  the sidebar and title-bar dialog. Edit tags is available for the current chat or any saved row;
  rename transfers labels and delete clears them. Rename rolls transcript/recall back if tag
  migration fails, while corrupted optional tag metadata cannot hide or block deletion of real
  transcripts. Focused tag/summary/lifecycle tests, the full `internal/app`/`internal/sessionstore`
  suites, package vet, frontend production/type build, generated Wails bindings, handwritten-source
  diff check, and the Wails production build pass. Canonical output is `build/bin/TheMauler.exe`
  (SHA-256 `D2E38A1133945EB233F007E2F0B79AB078C75AFB7302FBD3AD6FCAD184FD36E1`). Native visual acceptance
  remains for the operator.

- 2026-09-16 conversation-library metadata/actions: both saved-chat surfaces now use newest-first
  backend summaries with last-updated age, streamed message count, and a Review badge for malformed
  or unreadable transcripts. A shared row menu can Rename, Check/repair, or Delete any saved chat
  without loading it over the active transcript; all actions retain the existing run guards,
  confirmations, rollback, and repair flow. Focused summary/lifecycle tests, the full
  `internal/app`/`internal/sessionstore` suites, package vet, frontend production/type build,
  generated Wails bindings, handwritten-source diff check, and the Wails production build pass.
  Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `3A11C2CC9A9A85AF622181EAE05E1A6E4A77B802C9E3F07D6918AF4778C83E58`). Native visual acceptance
  remains for the operator.

- 2026-09-16 safe conversation rename: the title-bar Conversation menu and chat sidebar now share
  a guarded Rename action. The backend moves the JSON transcript and SQLite recall identity together,
  preserves FTS message row IDs, rejects existing-title collisions, supports case-only Windows
  renames, and rolls the file back if index migration fails. Focused rename/lifecycle tests, the full
  `internal/sessionstore` and `internal/app` suites, package vet, frontend production/type build,
  generated Wails bindings, scoped diff check, and the Wails production build pass. Canonical output
  is `build/bin/TheMauler.exe` (SHA-256
  `6715D8798410551CEC2EFC82D9CFF082CFBDF7EC985CDCF62F3792B7DA88F70A`). Native visual acceptance
  remains for the operator.

- 2026-09-16 task-oriented Chat actions: the welcome state now separates workspace review, recorded
  run continuation, planning, and authorised security work into descriptive launch cards. The
  composer More menu directly opens Security, repository files, and the native browser without
  submitting a model message, while Undo/Clear are grouped separately and retain existing guards.
  Frontend production/type build, scoped diff check, and the Wails production build pass. Canonical
  output is `build/bin/TheMauler.exe` (SHA-256
  `1796C4D5CFE8038DF53425B3F62F6DB52483E2B9402F20C191ACA06B6FD057E1`). Native visual acceptance
  remains for the operator.

- 2026-09-16 Inspector/open-command repair: the Chat Inspector overlay and resize handle now span
  the full workspace grid instead of inheriting its zero-width docked track, and every open route
  recovers the persisted width into the supported 360–620 pixel range before mounting. Title-bar
  panel controls and Chat's Tools/View/Run mode menus now use shared code-native SVG icons and
  clearer active states. Saved-chat rows also open directly and both conversation navigation
  surfaces share one guarded lifecycle action component. Frontend production/type build and scoped
  diff check pass; the clean Wails production build produced `build/bin/TheMauler.exe` (SHA-256
  `B2548323EFDF0E9B3B1563076F51982751EC8FC2B3FF11A978B1656330AA535A`). Native visual acceptance
  remains for the operator and is not claimed here.

- 2026-09-15 Chat canvas/work-surface redesign: the Chat route no longer reserves permanent grid
  space for Inspector or Terminal. Inspector is a resizable floating right drawer; Terminal and AI
  Commands are a resizable floating bottom drawer with a compact closed AI rail; simultaneous drawers
  stack instead of overlap. Both close completely, the title bar provides direct Chats/Inspector/
  Terminal controls, specialist pages preserve the docked IDE layout, and a one-time migration clears
  stale open-panel state while retaining dimensions. A follow-up gives Terminal a 320-pixel default
  and 280-pixel viewport-aware floor, narrows the initial AI Commands split, clamps both axes after
  host-window changes, and debounces PTY resize notification so drag frames cannot repeatedly redraw
  the shell prompt. Chat's content viewport now follows the live Terminal drawer height, keeping the
  newest message, context bar, and composer above the drawer throughout opening and resizing. The
  layout menu now uses task-oriented labels and grouped work-surface/behaviour commands. Chat's
  former seven-button command strip is consolidated into Tools, View, and Run mode menus with the
  same Security/index/browser, search/follow/transcript, and Agent/Fast Chat actions. Doctor and
  Settings are grouped into the More Workbench menu, leaving only immediate surface toggles in the
  title bar; the menu scrolls on short displays. The conversation sidebar now keeps its Workbench
  disclosure state instead of snapping shut, dismisses action menus on outside click or Escape,
  reports saved/filter-match counts, and uses a compact context footer. On Chat viewports at or below
  900 pixels it overlays the conversation rather than squeezing it, then closes after navigation or
  conversation selection. Chat's composer now has a model-labelled Thinking & Effort picker backed
  by the same persisted settings as Inspector: Profile retains adaptive tool-loop recovery, Always
  think pins preserved reasoning, and Direct is the hard no-thinking path. Qwen3.8 shows only its
  supported Auto/Low/Medium/XHigh effort choices; its existing thinking/direct samplers match the
  official model card. The focused app/settings Qwen and reasoning tests pass. Frontend
  production/type build, full Go suite, scoped diff check, generated Wails bindings, and clean Wails
  production build pass. `build.ps1 -SkipTests` now detects the optional local module index
  incorrectly classifying present standard-library packages and scopes `GODEBUG=goindex=0` to the
  Wails build; the fallback and direct source resolution passed end to end.
  Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `519560C322AED7325803C1168B723821B5AA81829A9AE90EADFFDA6CA1323422`). Native visual acceptance
  remains for the operator and is not claimed here.

- 2026-09-16 per-conversation Chat depth: normal Chat now offers persisted Adaptive, Always direct,
  and Always agent modes. Saved-session metadata follows rename/delete and legacy chats default to
  Adaptive. Direct removes tool schemas after every recovery-routing branch, forces a single useful
  text response, and cannot claim an action-shaped request completed; Agent suppresses the adaptive
  one-answer shortcut. Focused backend persistence/routing tests and the frontend production/type
  build pass. A follow-up adds readable mode badges to both conversation pickers and a validated,
  round-tripped Settings > Agents default for new chats; legacy settings migrate to Adaptive and
  clearing a chat restores the configured default without rewriting existing saved-chat choices.

- 2026-09-15 repository-intelligence first production slice: M0 deterministic fixtures/benchmark,
  streaming UTF-8/UTF-16 text/code chunking, explicit omission states, immutable SQLite v17 FTS5
  generations, and cancellation-safe active replacement are implemented. The compact `memory` tool
  exposes current-workspace index/status/search with bounded untrusted hash-cited excerpts and
  metadata-only RunLedger events. Chat's first-class **Files** card exposes index/rebuild, exact
  coverage, provenance and omission review. The follow-up lifecycle slice adds live metadata-only
  file/chunk progress, responsive cancellation, workspace/shutdown ownership, and Doctor/Services
  health reporting; a mid-scan cancellation regression proves the previous complete generation stays
  active. Focused repoindex/app tests, the complete Go suite, vet, the app/tools/repoindex/store race
  gate, generated Wails bindings, and the frontend production/type gate and Wails production build
  pass. Canonical output is
  `build/bin/TheMauler.exe` (SHA-256
  `B9C9D1CA0A10748B09F14DA312B613419B803FF40E8C88036EACED95FB6EC230`). The native visual
  smoke remains open because the verification environment exposed no controllable native-app
  surface; it is not claimed here.

- 2026-09-20 repository-intelligence M4 first rich-format slice: readable PDFs and
  DOCX/PPTX/XLSX/ODS text now enter immutable bounded chunks; ZIP contributes sorted inventory
  metadata without expanding member bodies. Code-owned expanded-byte, archive-entry and timeout
  limits produce explicit `expansion_limit`/`extractor_timeout` verdicts, while encrypted, corrupt
  and scanned/no-text inputs remain non-coverage. Deterministic manifest, no-ZIP-expansion and
  SQLite/FTS evidence tests pass. Focused app/repoindex tests, `go test ./... -count=1`,
  `go vet ./...`, app/tools/repoindex race tests, frontend type/production build, Wails production
  build and a bounded hidden startup smoke pass. Canonical output is `build/bin/TheMauler.exe`
  (SHA-256 `635CAB9362B64397AFED7E1CB69213DAC9A22E084F37D7077689EF289AB68C7F`).

- 2026-09-21 repository incremental refresh/watch: replacement generations now stream SHA-256 for
  every reusable indexed file, copy prior chunks only on an exact manifest hash match, re-extract
  changed/new content, and account for deletions before atomically moving the active pointer. Chat's
  Files card exposes manual **Refresh changes**, persisted per-workspace Watch, watcher state and
  last-check time, plus reused/changed/deleted totals. Focused tests cover unchanged reuse,
  same-size/same-mtime content replacement, deletion, prior-generation readability, metadata change
  detection, and a live app watcher that activates an incremental generation after a file appears.
  Focused tests, `go test ./... -count=1`, `go vet ./...`, the app/tools race gate, frontend
  type/production build, generated Wails bindings, clean Wails production build, and a 15-second
  hidden startup smoke pass. Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `FE4C7E8CA3B62FAA72B053AB0AF1FD58392DCBDF4CE810486A0513D5FDE7A2A3`). Native visual acceptance
  remains for the operator and is not claimed here.

- 2026-09-23 Brain repository-index operations: the shared backend status now derives one
  healthy/indexing/not-indexed/attention/unavailable verdict and explanation. Brain shows current
  workspace/policy/manifest provenance, coverage, chunks/bytes/sources, Watch health, incremental
  reuse/change/deletion counts, bounded explicit omissions and live progress, and drives the same
  Refresh/Watch/Rebuild/Cancel bindings as Chat. Cancellation remains available while the original
  long-running Wails call is pending. Health-classification and watcher integration tests, the full
  Go suite, vet, app/tools race gate, frontend type/production build, regenerated Wails bindings,
  clean Wails production build, scoped diff check, and a 15-second hidden startup smoke pass.
  Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `CA0A79F2CCF0BFC0817BFCDA26344064C37C896D9D9D86DDD2D88AAE4C83D823`). Native visual acceptance
  remains for the operator and is not claimed here.

- 2026-09-15 lifecycle/artifact ownership closure: the backend conversation epoch now records
  payload-free stale-event rejection telemetry for Doctor/Services, with a concurrent epoch-rotation
  regression proving retired owners cannot reopen current event ownership. New task contracts import
  finalized file fingerprints as immutable boundaries; ordinary file, shell, terminal and Python
  orchestration mutation paths are blocked, while an explicit Fixer request naming the exact file
  receives a sealed repair scope and must produce fresh verification evidence. Focused control/run-
  script/ownership tests, the full Go suite, vet, app/tools race gate, frontend production/type build,
  generated bindings, scoped diff check, and Wails production build pass. Canonical output is
  `build/bin/TheMauler.exe`. The native desktop/narrow-width lifecycle race and scoped-repair handoff
  smokes remain open and are not claimed here.

- 2026-09-18 lifecycle secondary-event/scoped-handoff closure: context-owned image progress and HTTP
  artifact refresh now pass through the backend epoch boundary; Chat rejects stale task refresh,
  workspace-file and learning-suggestion events. The integrated Fixer acceptance fixture changes one
  explicitly scoped finalized artifact, proves an unscoped sibling remains immutable, requires fresh
  verification, and confirms the replacement handoff uses canonical workspace identity and a new
  SHA-256. Focused ownership/control-plane tests, `go test ./... -count=1`, `go vet ./...`, the
  app/tools race gate, frontend production/type build, scoped diff check, generated bindings and the
  Wails production build pass; a bounded hidden startup smoke kept the rebuilt process responsive.
  Canonical output is `build/bin/TheMauler.exe` (SHA-256
  `6380421B20FF22E173F62AF965BF7F8A58E5B0A8BFAAAD4AF35D150BEB2110AE`). The Computer Use
  provider exposed browser surfaces but no callable native-app surface, so the desktop/narrow native
  click-through smokes remain open and are not claimed.

- 2026-09-14 visible-browser/follow-output repair: the inspected failed run made 19 browser calls,
  including five repeated inputs and four identical outcomes, because its first compact open omitted
  visibility and later revisited a Cloudflare-blocked host. Explicit browser-window wording now
  forces `visible=true`, blocked-host reopens are skipped, Chat can restart a headless session
  visibly, and persistent Follow/Paused scrolling prevents streaming output stealing the viewport.
  Tool-bearing assistant prose is retained as Agent updates rather than repeated final replies.
  Focused browser-policy tests, full Go suite, vet, app/tools race gate, frontend production/type
  build, scoped diff check, and Wails production build pass. Canonical output remains
  `build/bin/TheMauler.exe`.

- 2026-09-14 first-class Chat browser control: Chat's primary header now exposes Browser status and
  opens an in-chat visible-session launcher. The surface reports readiness, current URL/title, tab
  count, and controller state, and provides Take over, controller-observed Resume, Stop, and refresh
  without requiring Services. Model-issued handoffs open it automatically; ownership, typed-value
  secrecy, and conversation cleanup remain code-owned. Frontend production/type build, scoped diff
  check, and Wails production build pass. Canonical output is `build/bin/TheMauler.exe`.
  A same-day routing repair makes the active UI session authoritative for model turns: sanitized
  browser state is injected into the fresh execution packet, direct references to the open page
  require a browser snapshot, and shell/http probes are reserved for explicit protocol checks.
  Focused routing/redaction tests and their race gate pass; frontend and Wails production builds
  pass. The manifest source reader now reserves a fair input share for every selected document, so a
  growing earlier file cannot silently starve the final route-specific source. The deterministic
  context gate passes 7/7 fixtures and 105/105 attempts; full Go, vet, and app/tools race gates pass.

- 2026-09-14 conversation-first shell Slice 4: the left panel is now saved-chat history with New,
  search, current selection, direct open, and a secondary collapsible Workbench. The window heading
  follows the selected conversation; all reliable run controls live in one collapsed Context drawer;
  the composer exposes attach, voice, run-only stop, overflow actions, and circular send without
  removing file ingestion, rollback, model, agent, tool, or autonomy behavior. Closed panels remain
  zero-width and View/keyboard commands remain authoritative. Frontend type/production build,
  scoped diff check, and Wails production build pass. Canonical output is
  `build/bin/TheMauler.exe`.
  A follow-up native layout defect was fixed by assigning all five workbench children explicit grid
  columns: hiding the zero-width chat sidebar can no longer auto-place Chat into a splitter column
  and blank the centre surface. The frontend and Wails production builds pass after the repair, and
  the rebuilt native process launches responsively.

- 2026-09-14 chat-first panel command/declutter pass: the sidebar duplicate product identity and
  conversation controls are removed; one View command menu governs Explorer, Inspector, bottom
  panel tabs, AI Commands, Focus chat, and Default layout. Major panels have local close controls,
  keyboard toggles remain available, hidden AI Commands do not reopen on tool events, and automatic
  run-panel opening is opt-in. Frontend production/type build, scoped handwritten diff check, and
  Wails production build pass; the immediately preceding full Go, vet, and app/tools race gates
  remain green because this pass changes frontend presentation only.

- 2026-09-14 zero-width/ChatGPT-style follow-up: closed Explorer and Inspector columns and disabled
  splitters occupy zero pixels by default, while optional edge rails preserve the prior reopen path.
  Chat now uses a centred reading/composer column, simplified welcome state, prompt starters, and one
  Details menu for complete/replies and tool/status/browser visibility. Frontend production/type and
  Wails production builds pass.

- 2026-09-14 chat-first workbench Slice 2: explicit persistent conversation scratch attaches without
  clearing history, drops inherited target scope, has an advisory seven-day review date, and promotes
  into a durable workspace without moving/deleting files. The unified Conversation menu provides
  title search and lifecycle actions; transcript presentation independently filters tools, run
  milestones, and browser events; contextual Inspector suggestions do not force the panel open.
  Focused scratch/settings tests, full Go suite, vet, app/tools race gate, frontend production/type
  build, generated bindings, scoped handwritten diff check, and Wails production build pass.
  Canonical output remains `build/bin/TheMauler.exe`.

- 2026-09-14 chat-first transcript/UI Slice 1: every valid completed model response is emitted and
  committed by run/turn identity before later tool/continuation output can replace the live buffer;
  tool calls/results are typed transcript entries, terminal completion deduplicates the last turn,
  and saved-session projection retains assistant/tool ordering plus bounded reasoning content.
  Complete/Replies controls activity visibility without deletion. Chat is now the default entry
  point and Projects are optional Workspaces. Focused transcript preservation, the 105-attempt
  deterministic context suite, full Go suite, vet, app/tools race gate, frontend production/type
  build, generated bindings, and the Wails production build pass. Canonical output is
  `build/bin/TheMauler.exe`.

- 2026-09-14 browser workflow assistant slice 3 and cockpit usability pass: stable conversation-owned
  tab refs support open/list/switch/close without exposing CDP IDs; named workspace checkpoints retain
  sanitized URL/title metadata only and explicitly do not restore credentials, cookies, form values,
  query strings, or fragments. Pre-cancelled open/snapshot/click/type/extract/upload/download phases
  return promptly with classified recovery. The title bar and primary sidebar now give clearer
  page/run/profile/navigation hierarchy while retaining all existing resizers, rails, terminal
  boundaries, and controls. Focused native-browser tests, the deterministic browser 5/5 gate, full
  Go suite, vet, app/tools race tests, frontend production/type build, generated bindings, scoped
  handwritten diff check, and Wails production build pass. Canonical output is
  `build/bin/TheMauler.exe`.

- 2026-09-14 browser workflow assistant slice 2: model-issued handoff blocks the same tool/run and
  displays a run-owned Chat takeover card until resume/stop. Structured snapshots issue owner-scoped
  element refs; uploads are confined to regular active-workspace files; controlled downloads are
  stored below `.mauler/browser-downloads` with relative path, size, and SHA-256 evidence. Recovery
  output classifies retryability, observation-first, and ambiguous outcomes. The deterministic native
  browser reliability gate passed 5/5 fresh sessions covering stable refs, upload, download, and
  hashes. Full Go tests, vet, app/tools race tests, frontend production build, and Wails production
  build pass. Canonical output is `build/bin/TheMauler.exe`.

- 2026-09-14 browser workflow assistant slice 1: interactive signup/login/form/verification wording
  routes to the compact native browser; Chrome/Edge sessions are persistent and conversation-owned;
  Services provides visible launch, status, pause/takeover, controller-observed resume, and stop.
  Local fixtures pass for validation, cookies, redirects, simulated verification, paused automation,
  owner isolation, credential-output hygiene, and a cancelled ambiguous submit executed exactly once.
  The user-authorised `https://admin.clara.co/` smoke opened and snapshotted the login page without
  credentials or submission. Full Go tests, vet, app/tools race tests, frontend production build,
  generated bindings, and Wails production build pass. Output is `build/bin/TheMauler.exe`.

- 2026-08-24 multi-target authorised scope: Home now persists ordered allow/exclude rows for IP,
  CIDR, hostname, host/port, and HTTP(S) URL/path scope. Existing comma-separated targets migrate
  into separate rows; the first allowed row remains the compatibility primary target. Explicit
  exclusions are evaluated before broad CIDR/host allows, model-requested path broadening is denied,
  and relative endpoints inherit only the primary concrete target. Full Go tests, full vet, focused
  settings/engagement/app/tools race suites, frontend type-drift/production build, scoped handwritten
  diff check, and canonical Wails production build pass. Output is `build/bin/TheMauler.exe`. A native
  visual two-IP create/reopen/Grid-preview smoke was not run, so no visual UX claim is added.

- 2026-08-22 Telegram natural-range/heartbeat closure: the exact “weather over the next seven days”
  wording routes to required external evidence, route confirmation is code-owned, quiet runs refresh
  one live status card, and inaccessible settings no longer panic during `App.New`. All scoped
  `TestTelegram*`, `TestDispatchChannel*`, channelbus, and Telegram package tests pass; focused race,
  app/channel/Telegram vet, frontend production build, and Wails production build also pass. The
  managed execution sandbox denied unrelated manifest `EvalSymlinks`, browser-fixture, and external
  config/temp access, so the complete app suite and live Telegram-network smoke were not rerun in
  this closure. Output remains `build/bin/TheMauler.exe`.

- 2026-08-22 Telegram task/result repair: direct imperative and changing-data requests now enter the
  queued project-work lane without requiring `/cmd`; current public facts require a narrowed real
  tool set, and raw textual tool protocol blocks completion. Telegram final cards use structured
  Markdownish formatting, retain long answers through the client’s multi-message path, replace stale
  live cards for multi-part results, and bridge task/result pairs back into the same bounded Telegram
  chat. Deterministic follow-ups return the exact prior result, with a claimant-scoped persisted-run
  fallback after restart. Focused channel/app/Telegram tests, Context Quality pass^5 (105/105), the
  full Go suite, vet, app/tools race suite, frontend type/production build, Wails production build,
  and canonical native startup smoke all pass. Output remains `build/bin/TheMauler.exe`; no live
  Telegram-network, live-model, or unattended Agent Eval result is claimed.

- 2026-08-21 Windows-host shell and Chat Thinking repair: local process/game/app/service/window and
  GPU/VRAM questions route to an isolated native PowerShell one-shot even when the shared target
  terminal is WSL/Kali. Redundant nested PowerShell wrappers are removed without allowing bash to
  expand `$` variables; explicit Kali/WSL work and genuine remote Windows sessions remain intact.
  Inspector > Agent > Behaviour now provides a persisted Profile/On/Off Thinking override. On
  enables supported model thinking plus preserved reasoning for the complete run, Off selects the
  direct sampler and clears preserved reasoning, and Profile keeps the existing adaptive policy.
  Settings migration/validation, request-level Qwen behavior, shell routing, wrapper preservation,
  the 105-attempt deterministic context gate, full Go suite, vet, app/tools race suite, frontend
  type/production build, Wails production build, and a short canonical native startup smoke pass.
  The canonical output is
  `build/bin/TheMauler.exe`; no live-model or unattended Agent Eval claim is added.

- 2026-08-19 Chat attachment and read-only routing repair: native multi-file selection, copied
  Explorer paths, dropped paths, bounded large-file access, and untrusted attachment boundaries pass
  focused tests. The exact OpenAPI `POST, PUT, PATCH, DELETE` coverage prompt now receives a low-risk
  read-only control contract, no mutation/listener/Engagement tools, and no project build verifier.
  Stopped read-only runs also promote cleaned successful shell evidence into Chat when no assistant
  checkpoint exists, while verifier output, mutation runs, and guarded content remain excluded.
  Empty stopped-run memory and legacy reinjection regressions are covered. Context Quality returned
  7/7 fixtures and 105/105 deterministic attempts. The full Go suite, vet, app/tools race suite,
  frontend production build, generated bindings, and Wails production build pass. The temporary
  side-by-side build name used while an older desktop process was open was retired on 2026-08-19;
  the current verified production output is the canonical `build/bin/TheMauler.exe`. No live model
  or unattended Agent Eval claim is added.

- 2026-08-19 Terminal readability repair: run start preserves the persisted/user-resized bottom-panel
  height; terminal text defaults to 14 px with persisted UI A-/A+ controls and improved contrast;
  AI Command cards wrap commands across their full width and retain the draggable split/rail.

- 2026-08-19 task-routing repair: answer-output wording (table/summary/report/count/plan) no longer
  creates a Builder mutation contract by itself; mixed prompts with an explicit save/edit/update/fix
  action retain mutation planning and verification. Chat shows the actual live route while running.

- 2026-08-17 Qwen3.8 settings/UI follow-up: the local llama.cpp request path preserves separate
  `reasoning_content` between turns, Qwen3.8 emits only supported `low`/`medium`/`xhigh` effort
  values (mapping Mauler `high` to `xhigh` and omitting the field for direct mode), and old reasoning
  is token-counted and micro-compacted. Profiles now includes the Qwen3.8 RTX 3090 setup card. Focused
  backend/settings/history tests, the full Go suite, vet, app/tools race suite, frontend production
  build, and Wails production build pass. A live model comparison was not run because InferenceBridge
  was not active, so MTP remains provisional and no new unattended/pass^k claim is recorded.

- 2026-08-16 Qwen3.8 default and control-loop hardening: fresh and live settings select
  `qwen3.8-agent-stability` through InferenceBridge at 35K. Code-owned templates carry the official
  thinking/no-thinking samplers, preserved thinking, normalized `none`-to-`xhigh` effort, and a
  conservative draft-MTP `n=2` probe. Tool availability no longer disables thinking on the first
  Qwen3.8 step; bounded recovery can still force no-thinking. The completion evidence rail is
  blocking by default. The full Go suite, vet, app/tools race suite, frontend type/production build,
  scoped diff check, and clean Wails production build pass. The rebuilt native desktop launches
  responsively and visibly reports `qwen3.8-agent-stability`, backend `ok`, and the expected
  35,000-token context.

- 2026-07-23 repaired live Agent Eval closure: four diagnostic 12-fixture UI runs exposed and
  regression-closed alternate-valid-artifact scoring, accidental chunk overwrite, consecutive
  controller-message request shape, planning-only/completion-feature classification, and stale
  read-cache defects. Full Go tests, vet, app/tools race tests, frontend production build, Wails
  production build, and `git diff --check` passed. The final fresh Huihui 27B/35K report
  `agent-eval-20260723-195504` passed 11/12 with zero unsupported completions, policy violations,
  and human interventions. `chunked-write` remained a genuine model-loop failure: the model omitted
  `append=true` three times after explicit correction; Mauler preserved the existing 100 lines,
  blocked every overwrite, and stopped through the circuit breaker. Gate 1 is not pass^1, Agent Eval
  x5 was not run, and the profile remains supervised-only. See
  `../huihui-qwen36-agent-eval-2026-07-21.md`.
- 2026-07-23 GitHub/frontend dependency closure: clean-checkout CI builds the ignored embedded
  frontend before Go compilation and uses the current Node 24/action toolchain. Vite 8.1.5,
  Monaco 0.56.0, patched DOMPurify 3.4.12, Babel 7.29.7, and brace-expansion 5.0.8 produce a zero-
  vulnerability `npm audit`. Rolldown size-based vendor splitting with strict execution ordering
  removes the oversized-chunk warning without blanking WebView2. Full Go tests, vet, frontend
  production build, Wails production build, and a native Monaco/xterm/resizable-workbench/backend
  smoke passed.
- 2026-07-21 Huihui live Agent Eval: the active `qwen3.6-nothink-copy-copy` profile uses the exact
  `Huihui-Qwen3.6-27B-abliterated-ggml-model-Q4_K.gguf` alias at 35K, no-thinking sampling
  (`temperature=0.2`, `top_p=0.95`, `top_k=20`, `repeat_penalty=1.05`, `max_tokens=8192`) and native
  MTP `n=2`. One complete 12-fixture live suite passed 2/12 with zero unsupported completions, eight
  policy violations, 12.9% duplicate actions, 12.9% tool errors, and zero measured recovery success.
  The earlier UD run also passed exactly the same 2/12 fixtures with a near-identical stop pattern,
  exposing a shared Agent Eval/control-plane compatibility problem rather than a trustworthy model
  ranking. The result and required harness corrections are recorded in
  `docs/huihui-qwen36-agent-eval-2026-07-21.md`. An x5 result was not produced and is not implied.
- 2026-07-21 permanent default-winner closure: `settings.DefaultProfiles` now routes the stable
  `qwen3.6-nothink` fresh-install profile through `inference-bridge` to the exact
  `Qwen3.6-27B-UD-Q4_K_XL.gguf` winner at the verified 35K working context. The modern
  `profiles.toml.example`, tournament report, HF/GGUF template guide, current-state guide, and
  handoff document record the same choice. Existing named user profiles remain untouched; this
  installation retains its verified `qwen3.6-nothink-copy` profile as the active local default.
  Focused settings tests, `build.ps1` (full Go tests, full vet, frontend type/build, clean Wails
  production build), `go test -race ./internal/app ./internal/tools -count=1`, and scoped
  `git diff --check` pass. The rebuilt native app launches with backend `ok`, the existing winner
  selected, and the visible token budget reporting the expected 35,000-token context; it is left
  open.
- 2026-07-21 Qwen tournament/default closure: six installed Qwen variants completed the same live
  matrix; UD-Q4_K_XL and Huihui then completed the same four-scenario Advanced Suite. UD won at
  90/100 and 54.8 tok/s, passed ordinary required-tool and mini-loop gates, and returned HOLD only in
  the separate constrained-grammar probe. The exact UD/Huihui/HauhauCS/35B aliases passed focused
  and full Go tests plus vet. The runtime-profile race test, frontend type/build gate, Wails production
  build, rebuilt native launch, persistent default-profile smoke, and built-in MTP `n=1..5` sweep
  passed. UD is the local default at 35K with `n=3`; the sweep rounded to `1.00x` versus off, so it is
  not recorded as a material MTP speedup.
- 2026-07-21 local-model templates/comparison: exact-model Advanced Suite recorded Qwen3.6 27B
  Fable/Fus Q4_K_M at 90/100 and 33.3 tok/s; HauhauCS Gemma 4 26B-A4B QAT at 55/100 and 4.3 tok/s
  with invalid JSON but a valid structured tool call; Gemma 4 31B returned model-not-found and was
  not scored. Code-owned HF/GGUF templates and repeat-penalty transport passed focused tests,
  `go test ./... -count=1`, vet, app/tools race, frontend build, Wails production build, and native
  UI apply/save smoke for Qwen and exact HauhauCS Gemma. Rebuilt Mauler was left open.
- 2026-07-23 extended local-model matrix/template repair: the initial eight-model 16K matrix
  completed with 8/8 direct structured-tool calls and exposed that borrowed Qwen profile names could
  override selected Gemma/Qwen3.5 model ids. Model-id-first matching plus exact Qwen3.5 4B, Qwythos
  9B, Gemma 4 12B, and Gemma 4 E4B templates passed focused/full runtimeprofile and app tests, vet,
  full `build.ps1`, frontend production build, and Wails production build. Repaired live reruns
  measured Qwen3.5 4B at 125.4 tok/s with pass/66% mini loop, Qwythos 9B at 83.5 tok/s with fail/40%
  mini loop, and Gemma 4 12B at 3.5 tok/s with pass/80% mini loop. All three direct tool gates
  passed without repair. E4B corrected reruns remain pending because user input stopped UI
  automation. See `../local-model-tournament-2026-07-23.md`.
- 2026-07-21 context M5 phase A: seven task classes x three paraphrases x five repeats, hostile
  file/web/browser content, canonical-envelope isolation from intentional Manual/Unrestricted/tool
  overrides, stable packet/tool-schema identities, repeated Agent Eval telemetry, full Go tests,
  vet, app/tools race gate, frontend type/production build, clean Wails production build, and native
  Benchmark smoke passed. The real rebuilt app reported 7/7 fixtures and 105/105 attempts with the
  selected `gemma4-26b-a4b-qat` profile, hostile guard pass, and zero model calls. The native smoke
  first exposed an 85/105 false failure caused by intentional Unrestricted settings; the canonical
  evaluation envelope fix was regression-tested before the final 105/105 run. A live model Agent
  Eval x5 has not been run and is not implied by this result.
- 2026-07-21 context M4: exact Core/Relevant/Expanded preview accounting, safe provenance and
  exclusions, one-task desktop-only pin consumption, primary-system refresh without duplicate
  control packets, live three-document manifest validation, full Go tests, vet, app/tools race gate,
  frontend type/production build, clean Wails production build, and responsive rebuilt app process
  passed. Native automation also confirmed the Context route in the real Wails sidebar; the Windows
  helper could inspect but not click through the WebView child-process boundary.
- 2026-07-21 context M3: active-manifest routing, unknown-field/path-escape/document-count rejection,
  deterministic compact-core fallback, heading selection, exact hash/range provenance, five-run
  determinism, full Go tests, vet, app/tools race gate, frontend build, clean Wails production build,
  and responsive rebuilt app process passed.
- 2026-07-21 context M2: archive/core/shim/domain-map invariants, archive non-injection test, full Go
  tests, vet, app/tools race gate, frontend build, clean Wails production build, and responsive
  rebuilt app process passed.
- 2026-07-21 context M1: full Go tests, vet, app/tools race gate, frontend build, Wails production
  build, and rebuilt app launch passed.
- 2026-07-20 OpenRouter, one-task cloud boost, cloud-context defaults, and Bug Bounty/Chat workspace
  slices passed their focused/full gates and native UI smokes.
- 2026-07-15 control-plane/engagement races, full Go/vet/frontend/Wails gates, and workbench splitter
  live smoke passed; MAULER-AR-001 through MAULER-AR-005 remained closed.

Advance this baseline only after the stated checks genuinely pass.
