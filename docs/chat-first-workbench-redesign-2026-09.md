# Chat-first workbench redesign

Updated: 2026-09-15

## Decision

The conversation is the primary Mauler object. A workspace enriches a chat with durable files,
tools, evidence, target scope, and terminal state; it is not a prerequisite for asking a question.
Saved projects remain durable workspace presets. Mauler will not silently create disposable projects
for ordinary chats because that makes evidence ownership and later recovery ambiguous.

Fast Chat remains the explicit no-tools/no-context lane. Agent Chat is the normal default and may use
the current safe workspace root immediately. Conversation scratch is explicit, persisted on disk,
review-only after seven days, and can be promoted without moving or deleting its evidence.

## Failure being repaired

The streaming surface previously represented only the current model call. When a useful response was
followed by a tool call, continuation, compaction, or controller finalisation, a later
`stream_replace` could remove that response. The model history still contained it, so the model could
truthfully say “the full result was in my previous message” while the desktop transcript no longer
showed that message.

## Slice 1 — implemented

### Complete transcript ownership

- Every completed, valid model response emits a run-owned `mauler:assistant_turn` event before tools
  or another model call can replace the live stream.
- Desktop Chat commits each turn by stable run/turn identity. Terminal completion deduplicates the
  same final turn instead of replacing earlier, longer responses.
- Tool calls and tool results are retained as typed transcript entries with tool name/call identity.
  Saved-session projection reconstructs the same assistant/tool sequence and preserves bounded
  reasoning content.
- Rejected fake tool results and malformed tool protocol are not promoted into the transcript.
  Primary system prompts remain in Context Inspector rather than being disclosed in Chat.
- **Complete** is the default transcript mode and shows model replies plus tool exchange.
  **Replies** hides tool cards without deleting them.
- Assistant prose emitted alongside a tool call is retained as an **Agent update**, not presented as
  another final answer. Replies-only mode can hide these updates while Complete retains the exact
  model/tool sequence.

### Chat-first shell

- Mauler opens in Chat rather than Projects.
- Primary navigation order is Chat, Run, Workspaces, Grid, and Bench.
- Projects are presented as **Workspaces**: optional durable context rather than the app's front door.
- The Chat header explains the context model and exposes Complete/Replies plus Fast Chat directly.
- Message typography, timestamps, assistant separation, tool identity, responsive spacing, and the
  composer hierarchy are tuned for long-running conversations.
- Existing Explorer, Inspector, terminal, AI Commands, resizers, collapsed rails, and status strip
  remain available and retain their stored dimensions.

## Slice 2 — implemented

### Conversation-owned workspace context

- **Start conversation scratch** creates an explicit persistent folder and attaches it without
  clearing the transcript. Seven-day expiry is an advisory review date; Mauler never auto-deletes
  files or evidence.
- **Promote** registers the same folder as a durable workspace, preserving the active conversation,
  path, evidence, and files. Failed settings writes restore the previous workspace state.
- Scratch attachment clears inherited target scope and transient tool/rollback state so a previous
  project's authorization cannot leak into a new conversation context.
- **New project** is first-class in the Workspace menu. It creates one named child folder under an
  operator-selected parent, registers the durable project, and opens it through the same clean
  workspace-transition contract. An optional checkbox selects Bug Bounty Hunter only after the
  project exists; cancelling the picker changes nothing.

### Conversation navigation and transcript inspection

- The title bar now has one searchable **Conversation** menu for New, Save As, Open, Check & repair,
  and Delete. Saved conversation titles remain independent of workspace names; one workspace can
  therefore own multiple clearly named conversations.
- `Ctrl+N` starts a new conversation safely and `Ctrl+Shift+S` opens Save As. Single selection is
  explicit before opening, avoiding stale-selection races.
- Complete transcript mode can independently show or hide tool exchanges, important run-state
  milestones, and browser handoff/resume events. Search operates over the visible filtered transcript
  and never deletes hidden events.
- Inspector starts collapsed for ordinary chat and suggests Workspace, Facts, or Activity according
  to the current surface and browser takeover state. It does not steal focus or force itself open.

## Remaining redesign

1. Run screenshot acceptance at 1366x768, 1600x900, and the user's large layout, including keyboard
   resizing and collapsed rails, before changing any more panel defaults.
2. Add optional richer conversation metadata (manual rename and lightweight tags) if title search is
   insufficient in real use; do not couple titles to workspace names.
3. Add an explicit scratch-workspace review surface for old folders. Review must remain advisory and
   deletion must always be a separately confirmed action.

## Slice 5 — pentest command centre in Chat

- Chat has a persistent **Security** control that opens a compact projection of the authoritative
  Engagement Grid without making React the owner of scope, findings, or evidence decisions.
- The panel shows the active locked engagement, scope, workflow phase, completed/total work,
  evidence count, draft/confirmed findings, the next controlled action, and a concise finding list.
- **Prepare next test** and **Validate drafts** create reviewable composer drafts. Their prompts pin
  the existing locked scope, require reuse of existing evidence, prohibit invented confirmation,
  and keep destructive validation behind explicit operator approval.
- **Open Grid** remains the route for changing engagement state, attaching evidence, confirming a
  finding, or creating a new engagement. Ordinary Chat remains available when no engagement exists.

## Slice 4 — conversation-first application shell

- The left sidebar is now a real chat surface: New chat, title search, saved-conversation history,
  current selection, and direct open live above a secondary collapsible Workbench section.
- The selected conversation title owns the window heading. Workspace and model are concise context,
  not competing primary navigation objects.
- Plan, tools, workspace, agent, next-task model, cloud boost, autonomy, and the active tool remain
  fully available inside one collapsed-by-default **Context** drawer above the composer.
- The composer now follows a message-first hierarchy: circular attachment and send actions, Voice,
  a run-only Stop action, and an overflow menu for Undo and Clear. Existing file ingestion,
  interrupt-and-send, voice, and rollback behavior is unchanged.
- Closed sidebar, Inspector, and bottom work area still occupy zero pixels. The View menu and keyboard
  shortcuts remain the authoritative show/hide path, with optional closed-panel rails for users who
  prefer persistent reopen affordances.
- The global chrome is quieter and narrower; saved chats, specialist pages, evidence panels, and
  terminal surfaces are still available without being permanently visible in ordinary Chat.

## Slice 3 — panel command and declutter pass

- The application identity lives only in the title bar; the duplicate sidebar logo/name is removed.
- Conversation lifecycle and title search live only in the Conversation menu rather than being
  repeated in the navigation panel.
- The title-bar **View** command menu owns Explorer, Inspector, bottom panel, Terminal, Stream, Jobs,
  AI Commands, Focus chat, and Default layout actions. `Ctrl+Shift+P` opens that command menu.
- Explorer, Inspector, and the bottom panel have direct close controls. `Ctrl+B`, `Ctrl+Shift+B`,
  `Ctrl+J`, and `Ctrl+backtick` toggle the corresponding surfaces. Closed side panels occupy zero
  pixels by default; **Keep closed-panel rails** preserves the optional reopen-rail behaviour.
- Runs no longer force the bottom panel open by default. **Auto-open run activity** is an explicit,
  persisted View-menu preference; terminal and command history continue collecting while hidden.
- Chat uses a calm centred reading column and rounded composer, with a simple welcome prompt and
  task starters. Transcript verbosity and tool/status/browser filters move under one **Details**
  menu so reliable operational evidence stays available without dominating ordinary conversation.
- The Chat header has a persistent **Follow on / Scroll paused** control. Scrolling upward pauses
  following immediately, and the preference survives restart, so live deltas and tool cards cannot
  repeatedly steal the operator's reading position.

## Acceptance

- A useful assistant message never disappears because a later model turn is shorter.
- “Previous message” always refers to an entry that is still visible in Complete transcript mode.
- Reloading a saved conversation reconstructs assistant turns and tool exchanges in order.
- A new user can start in Chat without creating or editing a project.
- Attaching and promoting conversation scratch does not clear that conversation or move its files.
- Transcript filters and search only change presentation; the saved event sequence remains intact.
- Attaching a workspace never clears an unrelated active turn and does not weaken evidence scope.
- Internal system prompts, secrets, and untrusted hidden context are not exposed by Complete mode.

## Slice 6 — chat canvas and work-surface drawers

- Chat now keeps the conversation at full canvas width. Opening Inspector presents a resizable,
  dismissible right drawer over Chat instead of permanently shrinking the transcript.
- Terminal/Stream/Jobs open as a resizable bottom drawer over Chat; their sessions and output remain
  mounted while dismissed. Specialist workbench pages retain the existing docked panes.
- Chats, Inspector, and Terminal have direct title-bar state controls. The expanded layout command
  menu remains available under the compact overflow control with all keyboard shortcuts.
- A one-time layout migration dismisses legacy open Inspector/terminal/AI-command slabs while
  preserving the user's saved pane dimensions. The operator can reopen any surface immediately.
- Terminal starts at a useful 320-pixel drawer height with a viewport-aware 280-pixel floor. Its
  visual xterm fit remains live during a drag, while PTY resize notification is sent only after the
  movement settles so readline shells do not redraw and duplicate prompts for every pointer frame.
- Terminal/AI Commands begins with a smaller command-log share and re-clamps both split directions
  when the host window changes. The overflow menu uses operator-facing work-surface names rather
  than internal panel terminology.
- The Chat content viewport follows the live bottom-drawer height. The welcome state, transcript,
  newest response, Context disclosure, and composer therefore remain usable above Terminal during
  open, close, and drag-resize operations.

## Slice 7 — consolidated Chat commands

- The former Security, Files, Browser, Follow, Search, Details, and Fast Chat button row is replaced
  by three task-oriented menus: **Tools**, **View**, and **Run mode**.
- Tools owns the Security workspace, searchable repository index, and native browser controls. Active
  indexing or browser work is still signalled on the closed menu, and each tool retains its existing
  in-Chat panel and durable state.
- View owns transcript search, persistent follow/pause behaviour, Complete versus Replies density,
  and independent tool, run-status, and browser-event visibility. These remain presentation filters;
  they never mutate saved transcript history.
- Run mode names Agent Chat as the workspace/tools/evidence path and Fast Chat as the explicit
  no-tools/no-context lane. Selecting Fast Chat uses the existing isolated route.
- Menus are mutually exclusive, dismiss on outside click or Escape, and collapse to equal-width
  controls on narrow windows.
- The title bar follows the same hierarchy: Chats, Inspector, and Terminal remain immediate surface
  toggles, while the **More** Workbench menu groups panel layout, behaviour, Doctor diagnostics, and
  Settings. Its popover becomes vertically scrollable on short displays.

## Slice 8 — responsive conversation navigation

- Workbench expansion in the conversation sidebar is operator-owned and persists through ordinary
  Chat renders; navigating to a specialist page expands it without making Chat force it closed.
- Conversation actions use a controlled menu that dismisses on outside click or Escape. Search shows
  matching/total counts, distinguishes an empty library from an empty result, and offers a direct
  clear action.
- The workspace/model footer is a compact identity row rather than a second large context card.
- At 900 pixels and below, the Chat sidebar becomes a floating drawer over the conversation instead
  of consuming its width. Starting a chat, opening a saved chat, or selecting a workbench page then
  dismisses the drawer; specialist workbench pages retain the existing docked resizable sidebar.

## Slice 9 — one conversation lifecycle

- Saved-chat rows open their conversation immediately. The title-bar picker no longer introduces a
  second selected-but-not-open state or requires a separate Open selected action.
- The title-bar Conversation dialog and conversation sidebar render one shared lifecycle action
  component for New chat, Save as, Check and repair, and Delete saved chat.
- Run-active guards and labels therefore stay identical in both locations. The current conversation
  is visibly current and cannot be reopened accidentally while the operator is already using it.
- Counts and search remain local to each navigation surface, while mutations continue through the
  same App-owned handlers, confirmations, repair preview, and persistence path.

## Slice 10 — reliable Inspector and clearer commands

- The floating Chat Inspector and its resize handle span the whole workspace grid. They no longer
  inherit the zero-width fifth track reserved for the docked workbench Inspector.
- Every Inspector open path clamps and persists a supported width before mounting, recovering stale
  layouts that previously showed only a thin rail at the right edge.
- Chats, Inspector, Terminal, More, and Chat's Tools/View/Run mode commands use a small shared SVG
  icon vocabulary. Active surfaces use the existing accent colour while labels remain visible, so
  command hierarchy improves without turning the title bar into an icon-only control strip.
- Tool menu rows visually distinguish Security, workspace files, native browser, conversation
  search, and output following while preserving their existing labels, explanations, and status.

## Slice 11 — task-oriented welcome and composer menu

- Empty Chat uses four descriptive launch cards: review the workspace, continue the latest run,
  plan a task, or open the authorised Security workspace. The first three create an editable draft;
  Security opens its existing governed projection without silently starting a run.
- Each starter states its outcome instead of presenting an unexplained prompt fragment. Ordinary
  workspace use remains co-equal with pentest work, so security language does not hijack normal Chat.
- The composer More menu provides direct, icon-and-description access to Security, repository files,
  and the native browser. These only reveal existing task surfaces and do not call tools themselves.
- Undo last file edit and Clear conversation live in a separate Conversation group. Clear remains
  disabled during an active run and retains the existing confirmation-owned lifecycle.

## Slice 12 — safe conversation rename

- The shared conversation action set now includes **Rename chat** in both the title-bar menu and
  conversation sidebar. It edits the current saved title in place instead of creating a second copy.
- Rename is disabled during an active run, sanitises titles through the same backend rule as Save,
  refuses collisions, and supports case-only title changes on Windows through a temporary path.
- The JSON transcript and SQLite recall identity move together. Indexed message row IDs remain
  stable, so full-text results immediately report the new title without rebuilding the transcript.
- If recall migration fails after the file moves, Mauler rolls the file back to its original name
  and reports the failure. Legacy JSON-only conversations remain renameable even when no historical
  recall row exists.

## Slice 13 — useful conversation library rows

- Saved conversations are ordered newest-first and show last-updated age plus message count in both
  the chat sidebar and title-bar Conversation dialog. Metadata is derived from the transcript, so
  no migration is required for existing chats.
- Transcript counting streams one JSON message at a time rather than reading every saved chat into
  one navigation request. A malformed or unreadable transcript remains visible with a **Review**
  badge and can be sent through the existing Check and repair flow.
- Each saved-chat row has a compact action menu for Rename, Check and repair, and Delete. Operators
  can manage an older conversation without first replacing the active Chat transcript.
- Row actions share the same App-owned handlers, active-run guards, confirmations, rollback, and
  repair dialog as the current-conversation actions; the two navigation surfaces cannot drift into
  separate lifecycle implementations.

## Slice 14 — persistent conversation tags

- Saved conversations support up to six user-owned tags, each bounded to 24 characters. Tags are
  normalised, case-insensitive for deduplication/filtering, and stored outside transcript files so
  Chat history and recall evidence remain unchanged.
- Tag chips appear on saved rows in both navigation surfaces. Search matches titles or tags, while
  the shared tag strip provides an explicit one-click filter and clears itself if its final matching
  tag is removed.
- Edit tags is available from current-conversation actions and every saved-row action menu. It uses
  one backend validation/persistence path and refreshes both navigation projections immediately.
- Rename transfers tags to the new title; Delete removes them. A tag-write failure during Rename
  rolls recall and the transcript filename back together rather than leaving split identities.
- Optional tag metadata is deliberately fail-open for listing: if that file is damaged, saved
  transcripts remain visible without tags. Editing labels reports the metadata error instead of
  silently overwriting it.

## Slice 15 — compact task controls and reliable plan clearing

- The composer's expandable Context form is replaced by a single compact task bar. Plan, Tools,
  Workspace, Agent, and Run setup are now equal, labelled controls with stable icons and concise
  state summaries; their detailed surfaces float above Chat instead of pushing the composer down.
- Model route and confirmation mode live together in Run setup. Supervised and Automatic are
  explicit choices, cloud profiles remain one-task overrides, and all controls lock while a run is
  active without hiding the selected state.
- Clear plan now waits for the backend mutation and refresh, reports success/failure in Chat, and
  shows an in-progress state in the plan menu. It refuses to race an active run.
- SQLite and the legacy JSON recovery/export copy are updated together. Clearing an empty SQLite
  plan can therefore no longer trigger legacy migration that silently resurrects the old checklist
  on the next read.
- The empty-plan binding contract is always an array. SQLite and legacy JSON readers construct `[]`
  rather than a nil slice, while the frontend normalises a legacy/null response before storing or
  rendering it. Clearing the final row therefore cannot crash Chat through a `.length` access.

## Slice 16 — readable task-menu system

- Plan, Tools, Workspace, Agent, and Run setup share one menu header system: recognisable icon,
  plain-language title/subtitle, a readable state chip, consistent section labels, and larger action
  targets. Dense uppercase/monospace treatment is reserved for compact machine-state values.
- Plan removes repeated `[*] TODO-n:` decoration from display without changing stored plan data,
  wraps long steps, separates the current step from the remaining list, and uses readable status
  chips instead of fixed-width uppercase columns.
- Tools separates task context from connected surfaces and recent activity. Terminal, worker,
  Telegram, and queue state are scannable rows rather than eight equal cards competing for attention.
- Workspace groups scratch/attach actions and recent roots, with consistent icons, current-state
  badges, full paths, and a clear route to workspace management. Agent rows give the description
  priority and render autonomy/toolset as secondary policy chips.
- All task menus are wider on desktop, remain viewport-clamped by the existing portal, scroll within
  short windows, and collapse their multi-column choices for narrow layouts.
