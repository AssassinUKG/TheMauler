# TheMauler UI polish plan - 2026-07-12

Status: proposed plan; no redesign authorised by this document.

This plan preserves the current layout: navigation/project list on the left, primary work surface in
the centre, project inspector on the right, and Terminal/Stream/Jobs at the bottom. It is the
screenshot-driven continuation of `workbench-cockpit-ui-cleanup-2026-07.md`, not a competing UI
architecture.

## What the current layout gets right

- The four zones have clear jobs and match the way the app is used during HTB work.
- Target, VPN, shell, profile, workspace, and context stay visible.
- Chat, Run, Bench, Explorer, Inspector, Doctor, and Bottom are reachable without nested menus.
- The terminal remains first-class and does not compete with chat for transcript space.
- The project editor exposes the important box context without sending the user into Settings.

The goal is therefore better hierarchy and adaptive use of space, not different navigation.

## Screenshot findings

1. **Home is visually dominated by a long settings form.** The active project is already selected,
   but the centre continues to spend most of its area on fields that are edited occasionally.
2. **The left pane has low information density.** It uses a large area for one project, pinned
   autosave, and duplicate session controls. It should remain calm, but it can carry recent boxes and
   clearer active/running badges.
3. **Project identity repeats without a clear owner.** Root and target appear in the centre editor,
   inspector, Explorer, and status strip. The status strip/inspector should own live identity; the
   form should appear only while editing.
4. **The bottom split is expensive while idle.** An empty AI Commands column permanently consumes
   width. It is valuable during a run, but should collapse to a rail or summary when empty.
5. **Typography is too uniformly small and uppercase.** Labels, navigation, terminal chrome, and
   important status all compete at almost the same visual weight.
6. **The top bar has too many equally weighted controls.** Session actions, layout toggles, Doctor,
   and Settings read like one continuous toolbar.
7. **Primary actions move conceptually.** `New HTB box`, `Save`, `Resume box`, and `Delete` are all
   prominent even though Resume is the normal action and Delete is rare.
8. **The interface has good information but weak progressive disclosure.** Advanced project fields
   and empty operational surfaces are shown before they are needed.

## Target behaviour

```text
Top bar       Session selector | Save/Load menu       Layout toggles | Doctor | Settings
Left          Home/Chat/Run/Bench + searchable recent boxes + New conversation
Centre Home   Active-box summary + Resume             Edit details (collapsed by default)
Right         Authoritative target/environment/files inspector
Bottom idle   Terminal fills width; AI Commands is a narrow count/error rail
Bottom run    Terminal and AI Commands expand according to activity
Status strip  Profile | workspace | target | VPN | shell | run state | context
```

## Implementation phases

### P0 - Baseline and guardrails

- Capture screenshots at 1366x768, 1600x900, and the user's current large-window size for Home,
  Chat idle, Chat running, Terminal active, Settings Voice, and Bench.
- Add a small visual checklist to the existing UI tracker. Do not approve a change that makes the
  1366x768 layout require horizontal scrolling.
- Preserve panel resizing, collapsed rails, keyboard shortcuts, and saved layout settings.

Acceptance: the same project/session can be reached before and after each phase, and no work surface
is removed.

### P1 - Make Home a resume surface

Files: `ProjectsPage.tsx`, `ProjectsPage.css`.

- Replace the always-open project form with an active-box summary card.
- Make `Resume box` the single primary button.
- Put `Edit details` beside it; expanding it reveals the existing form without losing any field.
- Move `Delete` into a small overflow/danger area inside edit mode.
- Show last run state, latest artifact, and last-opened time in the summary when available.
- Keep `New HTB box` visible but visually secondary to Resume when a box is active.

Acceptance: returning users can resume the current box with one obvious click; editing remains one
click away; no project data is hidden permanently.

### P2 - Improve hierarchy and typography

Files: `index.css`, `App.css`, `HermesSidebar.css`, `ProjectsPage.css`, inspector and terminal CSS.

- Define three explicit text tiers:
  - section: 11-12 px, uppercase, spaced;
  - control/body: 12-13 px, normal case;
  - primary identity: 14-18 px, semibold.
- Reserve bright green for active/success/action states, not ordinary labels.
- Increase muted-text contrast slightly while reducing border contrast.
- Standardise control heights to 30/34 px and spacing tokens to 4/8/12/16/24 px.
- Use monospace only for paths, IP addresses, terminal content, and model identifiers.

Acceptance: target, active box, current run, and primary action are identifiable in a two-second
glance; section labels no longer compete with values.

### P3 - Make the bottom panel adaptive

Files: `App.tsx`, `App.css`, `TerminalPane.tsx/css`.

- When AI Commands has zero rows and no error, collapse it to a narrow `AI Commands · 0` rail.
- Expand it automatically on a new AI command or error, without stealing terminal focus.
- Keep the user's manual width/collapse choice for the rest of the session.
- Add `Fit`, `Focus terminal`, and `Focus commands` behaviours to the existing bottom toggle/menu,
  not more permanent toolbar buttons.
- During a run, allocate space based on activity: raw PTY-heavy work favours Terminal; structured
  tool-heavy work gives AI Commands more width.

Acceptance: an idle terminal gains meaningful width; command errors remain impossible to miss; no
terminal input loses focus because a command arrived.

### P4 - Simplify top and left controls

Files: `App.tsx/css`, `HermesSidebar.tsx/css`.

- Keep the session selector visible; group Save/Load/Delete under one labelled session control, with
  Delete separated as destructive.
- Keep Explorer/Inspector/Bottom as a visually distinct layout-toggle group.
- Keep Doctor and Settings as utility actions.
- Replace the large inactive sidebar session block with a compact recent-session list or hide it
  when it only repeats the top session selector.
- Give recent boxes compact status dots for active, running, failed, or stale target context.

Acceptance: session actions, layout toggles, and diagnostics read as three groups rather than one
row of equal buttons.

### P5 - Inspector progressive disclosure

Files: `RightInspector.tsx/css`, `FileTree.tsx/css`.

- Keep target/VPN/shell visible at all times.
- Collapse the repeated root path into one copyable breadcrumb.
- Persist the selected inspector tab per project.
- Add compact empty states instead of blank panels.
- In Workspace, prioritise folders/files; move editing of target metadata back to Home edit mode.

Acceptance: the right pane answers “what box, what route, what shell, what file?” without repeating
the same path three times.

### P6 - Focus-run polish, only after an HTB smoke test

- Observe one complete recon-to-evidence run before changing focus behaviour.
- Record concrete failures: hidden assistant status, cramped PTY, missed command error, moving Stop,
  or inaccessible artifact.
- Fix only observed failures. Do not create a new full-screen run mode by assumption.

Acceptance: the smoke-test issue is reproducible before the change and absent afterward.

## Recommended order

1. P1 Home summary/edit disclosure.
2. P2 typography and spacing tokens.
3. P3 adaptive empty AI Commands panel.
4. P4 top/left grouping.
5. P5 inspector deduplication.
6. P6 only from live-run evidence.

P1-P3 should deliver most of the visible improvement with low architectural risk.

## Explicit non-goals

- No replacement of the four-zone layout.
- No new navigation framework.
- No removal of Chat, Run, Bench, Terminal, Inspector, or Explorer.
- No styling pass that changes agent/runtime behaviour.
- No Silero/open-mic UI work as part of cockpit polish.
- No mobile/responsive redesign; minimum desktop-window resilience is sufficient.

## Final acceptance checklist

- Current box and Resume are the dominant Home elements.
- Project editing is available but not permanently expanded.
- Empty AI Commands does not consume a quarter of the bottom width.
- Target/VPN/shell each have one authoritative display.
- Top-bar groups are visually understandable without tooltips.
- Text remains readable at 1366x768 and 100% Windows scaling.
- Panel collapse/resize and terminal focus behaviour still work.
- No redesign work proceeds past P5 without a concrete HTB smoke-test finding.
