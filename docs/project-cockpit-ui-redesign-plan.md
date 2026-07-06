# Project Cockpit UI Redesign Plan

Goal: reshape TheMauler around projects/boxes first, then chat/ops/files/runs as work surfaces. The app should feel like a pentest project cockpit rather than a file explorer with agent controls attached.

2026-07 live-run cleanup note: the concrete cockpit noise/issues from current screenshots are tracked in `docs/workbench-cockpit-ui-cleanup-2026-07.md`. That tracker is the current source for deduplicating telemetry, grouping repeated AI Commands, separating chat/tool/terminal roles, focus-run layout, target identity prominence, control cleanup, and neutral status-bar styling.

## Target Layout

```text
┌──────────────────────────────┬──────────────────────────────┬──────────────────────────────┐
│ Projects / Boxes             │ Work Surface                  │ Project Inspector            │
│                              │                              │                              │
│ Search                       │ Chat | Ops | Terminal | Files │ Target                       │
│ New Project / New Box        │ Runs | Memory                 │ Environment                  │
│ Recent / pinned boxes        │                              │ Folders                      │
│ Status badges                │ Current task/run content      │ Agent/run controls           │
│                              │                              │ Current run summary          │
└──────────────────────────────┴──────────────────────────────┴──────────────────────────────┘
```

## Left Pane: Projects / Boxes

- [ ] Replace Explorer-first mental model with a project/box list.
- [ ] Search projects/boxes by name, target IP, hostname, tags, or status.
- [ ] Add `New Project` and `New Box` actions.
- [ ] Show compact status badges:
  - target IP
  - hostname
  - VPN/interface
  - shell backend/distro/user
  - evidence policy
  - last run status
- [ ] Clicking a project switches:
  - agent root folder
  - current target IP/hostname
  - VPN/interface
  - notes/scans/loot/screenshots/scripts paths
  - memory scope
  - logs/runs scope
  - terminal cwd
  - Ops context

## Center Work Surface

- [ ] Reduce primary tabs to daily-use surfaces:
  - Chat
  - Ops
  - Terminal
  - Files
  - Runs
  - Memory
- [ ] Move Brain/Benchmarks/verbose Logs behind Runs, Memory, or Settings.
- [ ] Keep Terminal and Chat easy to reach because they are the main daily workflow.
- [ ] Files becomes the place for explorer/file-tree detail instead of making the left pane carry everything.

## Right Pane: Project Inspector

- [ ] Convert the current Agent panel into a selected-project inspector.
- [ ] Target section:
  - IP / URL
  - hostname
  - OS / platform
  - notes
- [ ] Environment section:
  - main OS
  - AI shell backend
  - WSL distro/user
  - VPN/interface
  - browser/fetch route warning
- [ ] Folders section:
  - agent root
  - notes
  - scans
  - loot
  - screenshots
  - scripts
- [ ] Agent section:
  - mode override
  - autonomy
  - reasoning effort
  - toolset
  - evidence policy
- [ ] Current run section:
  - phase
  - active tool
  - latest artifact
  - stop/interrupt controls

## Settings Rethink

Settings should only hold app-wide defaults. Project-specific state should live in the project inspector.

Move out of Settings:

- [ ] target IP / hostname
- [ ] VPN/interface
- [ ] project folders
- [ ] per-project evidence policy
- [ ] per-project shell/backend preference
- [ ] HTB/current box details

Keep in Settings:

- [ ] providers
- [ ] model profiles
- [ ] global model defaults
- [ ] default project/folder templates
- [ ] default shell preferences
- [ ] theme/UI
- [ ] default tools/toolsets
- [ ] storage/retention

## Implementation Phases

### Phase 1: Plan And Safety

- [x] Capture the cockpit redesign plan.
- [x] Fix terminal WSL startup failure UX so the bottom terminal can recover cleanly.
- [ ] Preserve the existing Explorer and Agent panels while adding project-cockpit structure incrementally.

### Phase 2: Project List First Pass

- [ ] Add persisted project list state if current project profiles are not enough.
- [ ] Add left-pane project list/search above or instead of the current file tree.
- [ ] Add `Use Project` behavior that switches root/context together.
- [ ] Keep file tree accessible in a Files section.

### Phase 3: Inspector First Pass

- [ ] Add project inspector sections to the right pane.
- [ ] Move current run context controls from scattered panels into inspector sections.
- [ ] Add clear dirty/saved state for project edits.

### Phase 4: Tab Simplification

- [ ] Collapse top tab list to Chat, Ops, Terminal, Files, Runs, Memory.
- [ ] Move Brain into Memory/Runs.
- [ ] Move Benchmarks into Settings or an advanced diagnostics area.
- [ ] Merge verbose Logs into Runs, with raw logs behind an advanced toggle.

### Phase 5: Polish

- [ ] Add keyboard shortcuts for project switching.
- [ ] Add recent/pinned projects.
- [ ] Add HTB box template creation.
- [ ] Add visual run cards per project.
- [ ] Add project export/import.
