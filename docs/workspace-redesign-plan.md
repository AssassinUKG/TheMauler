# Workspace And Folder Model Redesign

Goal: make TheMauler's workspace handling feel closer to VS Code while keeping the agent's execution root explicit and safe.

## Current Problem

The Explorer has one current working directory. That directory drives the visible tree, file tools, memory/session scope, project instructions, and shell context. This is simple, but it makes several advanced workflows awkward:

- HTB labs need a target workspace, notes/loot folders, wordlists, scripts, VPN files, and sometimes mounted Windows paths.
- Coding tasks often need multiple roots: app repo, docs repo, generated output, and scratch artifacts.
- The UI does not distinguish "folders open in Explorer" from "agent execution root".
- Switching folders clears chat/tool context, which is correct for project isolation but painful when the user only wants to browse another folder.
- There is no recent workspace list, pinned workspace, or per-workspace metadata.

## Proposed Model

Introduce a first-class workspace object:

```text
Workspace
|-- name
|-- agent_root        # authoritative cwd for tools and prompts
|-- folders[]         # Explorer roots, VS Code-style
|-- notes_dir         # optional notes/loot/artifacts path
|-- shell_backend     # optional per-workspace override
|-- shell_distro
|-- toolset
|-- profile
|-- instruction_source
|-- recent_targets[]  # optional HTB/IP/url context
```

The important split:

- **Agent Root**: where file tools, relative paths, shell commands, memory scope, and system prompt context point.
- **Open Folders**: what the Explorer displays and lets the user browse/edit.
- **Run Context**: target URL/IP, VPN/interface notes, credentials supplied by the user, and current objective.

## UI Direction

Explorer top area should become compact but richer:

```text
EXPLORER                         [+] [...]
Workspace: TheMauler             switcher
Agent root: C:/.../TheMauler     change/root badge

OPEN FOLDERS
v TheMauler                      root
v htb-loot                       notes
> wordlists                      reference

CONTEXT
Target: 10.10.x.x / http://...
Shell: WSL kali-linux root
Toolset: Unrestricted
```

Expected controls:

- Add Folder
- Remove Folder From Workspace
- Set As Agent Root
- Open Recent Workspace
- Save Workspace As `.mauler-workspace.json`
- New HTB Workspace
- Reveal In Shell
- Copy Path

## Backend Changes

Add settings/state support:

- `settings.context.workspace_dir` remains as legacy/current `agent_root`.
- Add `workspace_state.json` or `settings.workspaces` for recent and saved workspace objects.
- Add Wails bindings:
  - `ListWorkspaces`
  - `SaveWorkspace`
  - `OpenWorkspace`
  - `AddWorkspaceFolder`
  - `RemoveWorkspaceFolder`
  - `SetAgentRoot`
  - `CreateHTBWorkspace`

File tree changes:

- `GetFileTree` should accept one root at a time and return shallow metadata quickly.
- Frontend should render multiple roots by calling `GetFileTree` per root.
- Preserve expanded state per root path.
- Agent root changes clear active run context; adding/removing Explorer folders should not.

## HTB Preset

Add a "New HTB Workspace" flow:

- Root: chosen lab folder, for example `~/htb/<box>`
- Notes: `notes.md`
- Loot: `loot/`
- Scans: `scans/`
- Scripts: `scripts/`
- Shell: WSL `kali-linux`
- Toolset: `unrestricted`
- Profile: `qwen3.6-nothink` by default for tool-heavy work
- Master skill: optional lazy-loaded HTB workflow source

Prompt context should include a compact HTB block:

```text
Current lab context:
- Target: ...
- Authorized lab: HackTheBox/DVWA/local vulnerable app
- Agent root: ...
- Notes dir: ...
- Shell: WSL kali-linux as root
```

## Implementation Steps

1. Add workspace state structs and persistence for open folders. Status: first pass done in settings context.
2. Add backend bindings for folder CRUD and agent-root changes. Status: first pass done.
3. Refactor Explorer to render multiple roots and an agent-root badge. Status: first pass done.
4. Add "Set as Agent Root" and "Add Folder" commands. Status: first pass done.
5. Add a generic pentest/lab scaffold that creates user-chosen folders. Status: first pass done.
6. Update system prompt workspace context to include open folders and lab target metadata. Status: first pass done.
7. Add tests for agent-root changes vs browse-only folder changes. Status: first pass done.
8. Add recent/saved workspace files and a workspace switcher. Status: planned.
9. Add richer lab run cards and scan artifact previews. Status: planned.

## Guardrails

This should not reduce unrestricted/autonomous capability. The point is clarity:

- Make it obvious where commands run.
- Make it obvious which folder the agent can mutate by default.
- Keep per-lab notes and outputs organized.
- Avoid stale workspace context leaking between projects or boxes.
