# Interactive terminal tools — settings + frontend wiring plan (2026-06-29)

## Context (what already shipped)

Two new agent-facing tools give the model a tmux-style, non-blocking view of the
shared PTY instead of the blocking, marker-framed `shell` call:

- `terminal_send` — type keystrokes into the live shared terminal and return
  immediately (optional `control` key for Ctrl-C etc.; brief `wait_ms` then
  returns the latest screen lines).
- `terminal_read` — snapshot the live screen right now, non-blocking, callable
  repeatedly.

Backend code (already merged, do **not** redo):
- `internal/app/interactive_terminal_tool.go` — the two tools.
- `internal/app/terminal_scrollback.go` — always-on rolling line buffer per
  `shellSession`, populated in `pipeShellOutput` (`internal/app/app.go`).
- Registered in `internal/app/subagents.go` (`registerAppTools`).
- Tests: `interactive_terminal_tool_test.go` (unit), `interactive_terminal_live_test.go`
  (live WSL, gated behind `MAULER_LIVE_TERMINAL_TEST=1`). All green.

Output already streams to xterm.js (`TerminalPane.tsx`) because these tools drive
the **same** PTY the human terminal renders. **No streaming/UI plumbing is needed.**

## The actual gaps to close

Verified gating path: `ToEnabledToolDefs` (`internal/tools/registry.go:79`) and
`toolEnabled` (`internal/app/app.go:4788`) both **default an unknown tool to
enabled**. Because `terminal_send`/`terminal_read` are in neither
`EnabledTools` nor any toolset (`internal/settings/defaults.go`), they are:

1. **Active in every toolset**, including `safe`, `explore`, `web-research`,
   `offline` — toolsets that deliberately disable `shell`. `terminal_send` runs
   arbitrary commands (≥ `shell`), so it currently bypasses the safety presets.
   They must be gated **exactly like `shell`**.
2. **Invisible in the Tools toggle UI** (`AgentPanel.tsx` renders
   `Object.keys(toolLabels)`), so the user can't see, risk-rate, or disable them.

## Task 1 — Govern them like `shell` (REQUIRED) · `internal/settings/defaults.go`

Treat `terminal_send` + `terminal_read` as a paired unit that follows `shell`
everywhere `shell`/`bash` appear, and is **absent** everywhere `shell` is absent.

1. **`EnabledTools` map** (~line 31): add
   ```go
   "terminal_send":        true,
   "terminal_read":        true,
   ```

2. **`DefaultToolsets()`** (~line 148): add both to the toolset bases that already
   contain `shell`, and to NONE of the others:
   - `localCode` (line 150) — append `"terminal_send", "terminal_read"`.
     This propagates to `local-code`, `offline`, `balanced`, and `unrestricted`
     (all built from `localCode`). Verify each still lists them after the spread.
   - Do **not** add to `coreRead`, `explore`, `webResearch`, `browser`, `safe`,
     or `memory`. Confirm `safe` and `explore` exclude them (this is the safety fix).

3. **Agent mode presets** (`defaultAgentPresets`, ~line 168): add
   `"terminal_send": true, "terminal_read": true` to the `ToolPermissions` of the
   modes that grant `shell` — **Ops** (line 177) and **Builder** (line 190) and
   **Fixer** (line ~200, verify). Skip modes that omit `shell`.

4. **Tests** (`internal/settings/load_test.go`, `defaults` tests): if any test
   asserts an exact tool count or the membership of `safe`/`explore`, update
   expectations. Add an assertion that `EffectiveEnabledTools` returns
   `terminal_send=false` under the `safe` toolset and `true` under `balanced`.

### Decision for the implementer
`terminal_read` is read-only (no command execution). You may keep it in the same
toolsets as `terminal_send` for simplicity (recommended — a read tool with no
send tool is useless), OR additionally include `terminal_read` in `safe`/`explore`
if you want the agent to watch a terminal it can't drive. Default: **pair them**.

## Task 2 — Make them visible/toggleable (REQUIRED) · `frontend/src/components/AgentPanel.tsx`

1. **`toolLabels`** (~line 66): add
   ```ts
   terminal_send: 'Terminal send',
   terminal_read: 'Terminal read',
   ```
   Place them right after `shell: 'Shell / Bash'` so they group visually.

2. **`toolRisk`** (~line 133): add
   ```ts
   terminal_send: 'high',   // runs arbitrary commands, like shell
   terminal_read: 'low',    // read-only screen snapshot
   ```

3. **`setToolEnabled` mirroring** (~line 415): `shell` already mirrors to `bash`.
   Decide whether toggling `shell` should also toggle the terminal tools. Two
   options — pick one and keep it consistent with Task 1:
   - **A (recommended):** make `terminal_send`/`terminal_read` independently
     toggleable (just the labels/risk above; no mirroring). Simpler, lets a user
     keep one-shot `shell` while disabling the interactive pair or vice-versa.
   - **B:** mirror them under `shell` (extend the `if (name === 'shell')` block)
     so the three move together. Choose only if you want a single "terminal"
     switch.

4. **No countdown / cancel widget needed.** The `shell` tool's countdown +
   "Cancel shell" UI is driven by the blocking run's timeout events; the
   interactive tools return fast and emit none, so they render as ordinary tool
   calls. Confirm a `terminal_send` call renders cleanly in the run's tool list
   (`run.tools` map, ~line 1253) — no special-casing required.

## Task 3 — Model guidance (OPTIONAL but recommended) · prompt/system text

Wherever the system prompt enumerates shell guidance (search the codebase for the
`shell` tool guidance / "shared terminal" prose), add one line steering tool
choice:
> Use `shell` for one-shot commands you want fully back in one turn. Use
> `terminal_send` + `terminal_read` to drive and watch interactive or
> long-running sessions (reverse/ssh shells, msfconsole, REPLs, prompts, scans you
> may interrupt with `terminal_send {control:"c"}`).

This reduces the model defaulting to blocking `shell` for interactive work.

## Verify

```bash
go build ./...
go test ./internal/settings/ ./internal/app/ ./internal/tools/
# live (Windows + WSL kali-linux, pre-boot the distro first):
MAULER_LIVE_TERMINAL_TEST=1 go test ./internal/app -run TestInteractiveTerminalLive -v
cd frontend && npm run build   # or the project's typecheck/lint
```

Manual smoke in the running app:
1. Tools tab shows "Terminal send" (High) and "Terminal read" (Low).
2. Switch toolset to `safe` → both disappear from the effective set (agent gets
   the "disabled by active toolset" message if it tries `terminal_send`).
3. Switch to `balanced`/Ops → agent can `terminal_send {keys:"top"}` then
   `terminal_read` and watch it refresh; `terminal_send {control:"c"}` exits.

## Acceptance criteria

- [ ] `terminal_send`/`terminal_read` enabled under `local-code`, `offline`,
      `balanced`, `unrestricted`, and Ops/Builder/Fixer; **disabled** under
      `safe`, `explore`, `web-research`, `browser`, `memory`.
- [ ] Both appear in the Tools toggle list with correct risk badges.
- [ ] Toggling behavior matches the chosen option (A or B) and is covered by a
      settings test.
- [ ] `go build`, Go tests, and the frontend build all pass.
```
