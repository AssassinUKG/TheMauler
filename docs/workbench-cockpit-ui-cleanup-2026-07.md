# Workbench Cockpit UI Cleanup - 2026-07

Goal: make the live workbench feel like a clear project cockpit, not three competing logs. The user should always know what the agent is doing, what machine/project is active, and where to look for raw output versus structured tool history.

The screenshot-driven continuation and implementation order for remaining polish is
`ui-polish-plan-2026-07-12.md`. It preserves this tracker's surface roles and four-zone layout.

## Surface Roles

- Main chat: user messages, assistant replies, and a compact live "what the agent is doing now" card. Do not dump tool output here.
- AI Commands: structured tool history with command/result copy buttons, artifact links, grouped repeats, and loud errors.
- Terminal: raw live PTY only. This is where live shell output belongs.
- Right inspector: project/workspace facts, target identity, files, and run evidence.
- Persistent status strip: one authoritative place for profile, state, context, target, and workspace.

## Top Issues

1. Duplicate telemetry is shown two or three times.
   Profile/model appears in the left model box, center profile dropdown, and bottom status bar. State appears in the center chip, right panel header, and terminal header. Context appears in the center chip and bottom bar. Collapse these into one persistent status strip and remove duplicates.

2. Repeated AI commands create noise and hide looping.
   Consecutive similar tool calls such as repeated `curl ... | head -100`, `head -50`, and `head -30` should render as one grouped row with an `xN` badge. Errors must be visually loud and should not blend into done rows.

3. Run mode gives pixels to the wrong surface.
   While an agent is running, the center chat can be mostly empty while terminal and AI Commands are cramped. Add a focus-run layout or auto-expanded bottom work area for active runs.

4. Too many tiny all-caps gray labels compete for attention.
   `ROOT`, `TARGET`, `VPN`, `SHELL`, `WORKBENCH`, `EXPLORER`, `PLAN`, `TOOLS`, `STATE`, `WORKSPACE`, and `CONTEXT` all read at similar priority. Define a three-tier type scale and reserve all-caps labels for section headers only.

5. Terminal and AI Commands need stronger separation.
   AI Commands is the structured, collapsible command/result history. Terminal is the raw live PTY. Clicking a command row should eventually scroll the terminal to the related marker/output.

6. Elevate the run identity block.
   `TARGET`, `VPN`, and `SHELL` are the most valuable "am I on the right box" signals in HTB-style work. They should be prominent, scan-friendly, and part of the single status strip or inspector header.

7. Unify control groups.
   Session save/load/delete, model save/load/delete, title toggles, and actions currently look too similar. Toggles should read differently from one-shot actions.

8. Stabilize the primary action.
   `Send`, `Interrupt & Send`, and `Stop` should keep predictable placement. Destructive actions such as stop/kill should be visually secondary unless the run is actually dangerous or stuck.

9. Tone down the bottom context bar.
   Persistent saturated green reads as success. Use neutral styling for ongoing context/budget telemetry and reserve strong green for positive events.

10. Keep context maintenance out of the conversation.
    Repeated `[Context compacted]` system cards interrupt assistant answers and make the transcript
    look as if messages were replaced. Compaction is operational activity, not assistant speech;
    show it in Activity/Logs and filter legacy compaction cards when loading saved sessions.

## Implementation Order

- C1 done: Group consecutive similar AI Commands and make errors loud. Files: `frontend/src/components/TerminalPane.tsx`, `frontend/src/components/TerminalPane.css`.
- C2 done: Create one authoritative status strip for profile, run state, context, target, VPN, shell, and workspace. Files: `frontend/src/components/StatusBar.tsx`, `frontend/src/App.tsx`, `frontend/src/components/ChatPane.tsx`, `frontend/src/components/RightInspector.tsx`.
- C3 first pass done: Live runs auto-open and enlarge the bottom panel without switching away from the user's selected Terminal/Stream/Jobs tab. Continue later with a full focus-run layout if visual testing shows it is still cramped.
- C4: Apply the three-tier type scale and contrast cleanup. Files: `frontend/src/index.css`, component CSS files.
- C5 done: Promote target/VPN/shell identity into the inspector/status hierarchy. Files: `frontend/src/components/RightInspector.tsx`, `frontend/src/components/StatusBar.tsx`.
- C6 first pass done: Profile no longer duplicates in the sidebar, Doctor opens the correct panel, session controls are grouped, and composer `Stop`/`Send` positions are stable. Continue later with iconography/compact topbar treatment if needed.
- C8 done: Doctor is a first-class center page/tab with its own run button, score cards, grouped check list, and persisted in-tab result while browsing the workbench. The topbar Doctor action opens/runs this page directly instead of hiding results inside Agent settings.
- C7 done: Neutralize the bottom context bar and reserve saturated green for success states. Files: `frontend/src/components/StatusBar.css`.
- C9 done: Restore first-class workbench resizing. Terminal and AI Commands now have a visible,
  persistent splitter even when the command rail is collapsed or empty; dragging the collapsed edge
  reopens it, wide layouts resize horizontally, and narrow layouts resize vertically. Explorer and
  Inspector widths persist locally, all four workbench separators have larger visible hit targets,
  and arrow-key resizing is supported. Files: `frontend/src/App.tsx`, `frontend/src/App.css`,
  `frontend/src/components/TerminalPane.tsx`, `frontend/src/components/TerminalPane.css`.
- C10 done: Context-compaction events no longer append repeated system cards to Chat. They remain
  visible as operational Activity/Logs evidence, and legacy `[Context compacted]` cards are filtered
  when saved sessions are loaded. Files: `frontend/src/App.tsx`.

## Acceptance

- A live run shows assistant speech/status in chat, structured grouped tool calls in AI Commands, and raw PTY output in Terminal.
- Repeated commands collapse into one row with an obvious count.
- Error command rows are obvious without opening them.
- Profile, state, context, target, and workspace each have one primary display location.
- Active run layout gives the terminal/command stream enough space to inspect what is happening without hiding the agent's current intent.
- Explorer, Inspector, bottom work area, and Terminal/AI Commands dividers remain draggable; saved
  horizontal pane positions survive a reload, and an empty AI Commands history can still be opened
  and resized.
