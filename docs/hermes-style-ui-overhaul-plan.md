# Hermes-Style UI Overhaul Plan

Created: 2026-07-01

Goal: reshape TheMauler into a calmer, larger, Hermes-class agent console while keeping the Mauler
green/black theme and all existing backend capabilities. Hermes WebUI's strongest pattern is one
coherent three-panel workspace: sessions/navigation on the left, chat in the center, workspace/files
on the right, with model/profile/workspace controls near the composer and deeper controls in a
Control Center.

## Principles

- Keep chat and current execution state visible by default.
- Reduce top-level page hopping; use panels, drawers, and Control Center sections.
- Prefer compact, readable command/result cards over raw JSON blocks.
- Make files, artifacts, run facts, and commands clickable everywhere.
- Increase default UI scale: larger controls, readable form rows, wider settings.
- Keep the existing colour tokens for now; change layout and hierarchy before palette.

## Phase 1 - Control Center Shell

Status: first pass complete. Frontend build passed on 2026-07-01.

Replace the cramped settings modal presentation with a larger Control Center shell. Keep the existing
settings logic intact in the first pass.

Tasks:
- Increase width/height to roughly 88vw x 86vh.
- Add Control Center title, subtitle, and status summary.
- Convert tabs into a clearer left navigation rail with short descriptions.
- Keep Save/Close visible in a sticky header.
- Enlarge form rows and inputs.

Acceptance:
- Settings are readable without feeling tiny.
- Existing Settings behaviour still works.
- No backend changes.

## Phase 2 - Three-Panel App Shell

Status: corrected. The app now follows the Hermes structure more closely: left sidebar is
sessions/navigation, center is chat/work content, and the right Inspector owns Workspace/Facts/
Commands/Activity. Frontend build passed on 2026-07-01.

Reshape the main app layout:
- Left sidebar: sessions, new chat, primary nav, pinned/recent runs.
- Center: chat-first work area.
- Right panel: Workspace, Preview, Run Facts, Commands, Agent.
- Bottom: terminal/stream/jobs, larger by default.

Acceptance:
- User can keep chat visible while inspecting files and live state.
- Top tab bar becomes secondary or disappears.

## Phase 3 - Composer Footer

Status: first pass complete. Chat composer now shows profile, autonomy, live state, workspace root,
context usage, and active tool countdown. Frontend build passed on 2026-07-01.

Move vital run controls near the composer:
- profile/model
- mode
- autonomy
- toolset
- workspace root
- context ring/token meter
- current run state

Acceptance:
- User sees execution configuration while typing.

## Phase 4 - Command and Tool Cards

Status: first pass complete. Chat tool calls/results and Inspector command/activity cards now use
structured command cards with status, command/result previews, raw toggle, and copy controls.
Frontend build passed on 2026-07-01.

Convert tool calls/results into Hermes-style cards:
- `shell(command="...")` compact header.
- status, duration, exit/state.
- copy command/result buttons.
- expand/collapse raw input/result.
- highlight `contract` fields.

Acceptance:
- Chat, Stream, Terminal AI Commands, Logs, and Ops all use the same command-card language.

## Phase 5 - Workspace Right Panel

Status: first pass complete. The Inspector Workspace tab now shows live root facts, open folders,
latest artifact, and a compact clickable agent-root file list that opens files in the center editor.

Make files/artifacts active, not static:
- right panel file browser always available.
- inline preview for small files/artifacts.
- open-in-center action.
- evidence/artifact links share the same open path.

Acceptance:
- Ops evidence and run artifacts are clickable and inspectable.

## Phase 6 - Page Consolidation

Move deep pages into Control Center or right-panel views:
- Logs -> Control Center diagnostics, with Run Stability cards.
- Brain -> Control Center diagnostics / ledger.
- Memory -> Control Center memory.
- Benchmarks -> Control Center diagnostics.
- Projects -> left workspace switcher plus Control Center detail.
- Ops -> center cockpit plus right Run Facts.

Acceptance:
- Fewer top-level tabs, more persistent work context.

## Phase 7 - Visual Scale Pass

- Base UI font 14px.
- Buttons 30-34px.
- Inputs/selects 32-36px.
- Composer taller.
- Cards less cramped.
- Keep radius 6-8px.
- Reduce decorative background if it hurts readability.

Acceptance:
- The app is readable at desktop resolutions without losing operator density.

## Phase 8 - Verification

- `npm run build`
- `go test ./...`
- Wails build
- Playwright screenshots for desktop and narrower widths.
- Manual smoke: settings save, provider/profile edit, chat run, file open, logs, terminal.
