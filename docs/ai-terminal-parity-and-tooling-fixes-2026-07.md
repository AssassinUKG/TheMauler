# AI Terminal Parity + Tooling Fixes - actionable handoff (2026-07-02)

**Audience:** an AI coding agent (Codex/Claude) that will implement these changes.
**Goal (user's words):** *"fix the mauler and the AI terminal ... for the AI to be able to use the terminal fully like a human. It's close but not 1:1 yet"* - plus a review of the tools/loops with concrete tooling upgrades.

This doc is self-contained: it states the root cause, the exact files/lines, the fix, and the acceptance test for each item. Work top-down; P0 items are the ones that close the "not 1:1 with a human" gap. Verify every change with:

```bash
go build ./... && go test ./internal/app/ ./internal/tools/
cd frontend && npm run build   # only if you touch frontend/
```

Run all Go/wails commands from `C:\Users\richa\Desktop\TheMauler` (never `cd` into a subdir first - WSL/wails path assumption).

---

## Current Status - 2026-07-06

Keep terminal work Mauler-side. Do not add backend/provider work here.

Done since this doc was written:
- `terminal_send command` now returns only the command plus output appended after that command. It no longer returns the whole visible terminal screen/scrollback as the command result.
- `terminal_read mode=history` without `grep` no longer dumps raw historical scrollback into the model. History mode is for search/evidence; use `grep` or read the current screen.
- The AI Commands panel no longer creates duplicate cards for `terminal_send` / `terminal_read` via generic tool-result events.
- Normal terminal states such as `prompt_or_idle`, `connected`, `listener`, and `interactive_prompt` are no longer shown as red tool errors in the AI Commands panel.

Still open:
- Live shell smoke tests across WSL bash, PowerShell, cmd, and any configured zsh-like shells.
- Any richer VT fidelity found by those smoke tests.
- Compact-tool argument sanitation that affects terminal loops: legacy shell arg keys, XML-ish path tags, and residual HTML entity escalation before routing/logging.

---

## 0. How the terminal works today (grounding - read before editing)

There is **one shared PTY** (`shellSession`, ConPTY on Windows via `github.com/aymanbagabas/go-pty`). Both the human and the agent write to the same PTY master:

- **Human input:** xterm.js `onData` -> `ShellInput(id, data)` (`frontend/src/components/TerminalPane.tsx:306`).
- **Agent input:** `terminal_run` / `terminal_send` -> `sess.input.Write` (`internal/app/interactive_terminal_tool.go`).

Output fans out in `pipeShellOutput` (`internal/app/app.go:6215`):

- **Human view:** *raw* PTY bytes -> `mauler:shell_output` -> **xterm.js**, a full VT emulator (colors, cursor addressing, `\r` repaints, full-screen TUIs render correctly).
- **Agent view:** bytes are split on `\n`, each whole line passed through `sanitizeTerminalLine` (`app.go:6543`, strips OSC + ANSI + C0 controls), then appended to a **flat line buffer** `terminalScrollback` (`internal/app/terminal_scrollback.go`). `terminal_read` snapshots the tail of that flat buffer.

**This asymmetry is the whole problem.** The human sees a *rendered screen*; the agent sees a *flattened, ANSI-stripped, append-only log of lines*. Everything in P0 flows from closing that gap.

Tool registration for the shared terminal: `internal/app/subagents.go:53` (`registerAppTools`) registers `terminal_run`, `terminal_send`, `terminal_read`, `start_listener` (plus `shell`/`bash` from `internal/tools`).

---

## P0 - Give the agent a real screen (the "1:1 with a human" fix)

### P0-1. Render a headless VT screen for the agent instead of a flat line log [done] first pass

**Root cause.** `pipeShellOutput` (`app.go:6215`) treats the PTY stream as line-oriented text and *strips* cursor-control escapes in `sanitizeTerminalLine` rather than *applying* them. So any program that addresses the cursor - `msfconsole`'s interactive console, `python`/`ipython` REPLs (prompt_toolkit), `less`/`man`, `top`/`htop`, `vim`/`nano`, `sqlmap`'s live-updating tables, progress bars redrawn in place - produces duplicated, out-of-order, or empty lines for the agent while the human sees a clean screen. The agent literally cannot "read the screen."

**Fix.** Run an in-process headless terminal emulator over the *same* raw byte stream the UI gets, and let the agent read its rendered cell grid - exactly like `tmux capture-pane`.

- Add a VT emulator dependency. `github.com/hinshun/vt10x` or `github.com/charmbracelet/x/vt` are the practical choices (a `go-ansi-parser` indirect dep already exists but is not a screen model). Size the screen to the PTY size (`OpenShell` starts at 200x50, `app.go:6089`; keep it synced with `ShellResize`).
- In `shellSession` (`app.go:5967`) add `screen *vtScreen` (wraps the emulator). In `pipeShellOutput`, feed the **raw** `buf[:n]` (the same bytes as `emitUI`) into the emulator, *before* the newline-splitting path. Keep the flat `scroll` buffer too (it's still useful for history/grep), but make the emulator the source of truth for "what's on screen now."
- Add a `renderScreen(lines int) []string` that returns the emulator's current visible grid (trailing non-empty rows), trailing-whitespace-trimmed.

**Wire into the read tools.** `terminal_read` / `terminal_run` / `terminal_send` should return the **rendered screen** for interactive/TUI state, and fall back to `scroll` tail only for pure line-stream commands. Concretely, `formatTerminalScreen` (`interactive_terminal_tool.go:533`) gains a screen-aware path.

**Acceptance:**
- Start `python3` in the shared terminal via `terminal_run`, then `terminal_send {keys:"print(2+2)"}`; `terminal_read` shows the `>>> ` prompt and `4` on the correct lines with no duplicated/echoed garbage.
- `terminal_run {command:"top -b -n1"}` vs an interactive `top`: interactive `top` returns a coherent single screen, not stacked repaints.
- Existing `internal/app/interactive_terminal_tool_test.go` still passes; add a test feeding a cursor-up + clear-line escape sequence and asserting the rendered screen (not the raw lines).

> If pulling a VT lib is undesirable, the minimum viable version is: apply `\r`, cursor-up (`ESC[A`), and erase-line (`ESC[K`) to a small ring of "current rows" in Go. But a real emulator is far less bug-prone and is the correct 1:1 answer.

---

### P0-2. Exact command-completion detection via shell integration markers (kill the poll-loop bug class) [done] first pass

**Root cause.** Completion is guessed by regex: `endsWithShellPrompt` (`interactive_terminal_tool.go:251`) only accepts a line ending in `$` or `#`. Two failures:
1. Default `ShellBackend` is `"auto"` -> **PowerShell** on Windows (`settings/defaults.go:13`, `app.go:6067`), whose prompt ends in `>`. Prompt detection then *never* fires and the agent falls back into the historical `terminal_read {wait_ms:10000}` wait-loop on already-finished commands (the exact bug in `docs/terminal-completion-and-listener-plan-2026-06-29.md`).
2. Any custom `PS1`, a path containing `#`, or a program printing `$` mid-output can false-positive/negative.

**Fix.** Emit explicit shell-integration markers and detect *those*, not the prompt glyph.
- In `maulerBashInteractiveArgs` (`app.go:6009`) set `PROMPT_COMMAND` to print an OSC 133 sequence (`\e]133;D;$?\a` after each command, `\e]133;A\a` before each prompt) - the same protocol VS Code/iTerm use. For PowerShell/cmd backends, inject the equivalent prompt hook.
- Parse those markers in `pipeShellOutput` to flip a `sess.promptReady bool` and capture the **real exit code** (`$?`). Strip the markers from both the UI stream and the agent scroll (the `__MAULER_` filter at `app.go:6247` is the existing pattern to copy).
- `classifyTerminalState` (`interactive_terminal_tool.go:211`) uses `sess.promptReady`/last-exit instead of `endsWithShellPrompt` glyph-matching. Keep the glyph heuristic only as a fallback when markers are absent.

**Payoff:** `terminal_run` can now truthfully report `state=prompt_or_idle exit=0` and the agent stops polling finished commands regardless of shell/prompt. This also lets the blocking `shell` tool report a **real exit code** for interactive-started commands.

**Acceptance:** `terminal_run {command:"ls /"}` then `terminal_read` returns `state=prompt_or_idle` with an exit code, on both bash and PowerShell backends. Add a test that feeds an OSC-133-framed transcript and asserts the classified state + exit code.

---

### P0-3. Detect in-place screen changes, not just new lines [done] first pass

**Root cause.** `waitForTerminalOutput` (`interactive_terminal_tool.go:139`) only returns early when `scroll.length()` **grows**. A progress bar or TUI that repaints the *same* rows (via `\r`/cursor-up) never grows the line count, so the tool waits the full `wait_ms` and then reports `no_output_yet` even though the screen is changing every frame - the agent concludes "nothing is happening" on a busy command.

**Fix.** Once P0-1 lands, watch a cheap hash/generation counter of the rendered screen instead of `scroll.length()`. Bump a `sess.screenGen` counter on every emulator write; `waitForTerminalOutput` returns when `screenGen` changes. Falls back to length-growth when no emulator.

**Acceptance:** a command that only redraws one line (e.g. `for i in 1 2 3; do printf "\rprogress %d" $i; sleep 0.3; done`) causes `terminal_read {wait_ms:2000}` to return early with the latest value, not time out as `no_output_yet`.

---

### P0-4. Full keystroke vocabulary (drive TUIs/REPLs like a human) [done] first pass

**Root cause.** `controlKeys` (`interactive_terminal_tool.go:295`) only maps `c,d,z,l,u,esc,tab`. A human driving `less`, `vim`, `msfconsole`, a paged menu, or shell history uses arrows, Home/End, PageUp/Down, Ctrl-A/E/R/W/K, Delete, and function keys. The agent can't send any of these, so it can't navigate any TUI.

**Fix.** Extend the key vocabulary in `terminal_send`:
- Add a `key` field (or extend `control`) accepting named keys -> escape sequences: `up`=`\e[A`, `down`=`\e[B`, `right`=`\e[C`, `left`=`\e[D`, `home`=`\e[H`, `end`=`\e[F`, `pageup`=`\e[5~`, `pagedown`=`\e[6~`, `delete`=`\e[3~`, `enter`=`\r`, `backspace`=`\x7f`, plus `ctrl-a`..`ctrl-z` generically (`ctrl-<x>` -> byte `x&0x1f`). Keep the existing short names as aliases.
- Support sending a **sequence** of keys in one call (array) so the agent can do e.g. `["/", "search", "enter"]` in `less`.
- Consider enabling **bracketed paste** for multi-line `keys` so pasted code into a REPL isn't interpreted line-by-line as commands.

**Acceptance:** `terminal_send {key:"ctrl-r"}` starts reverse-history-search in bash; `terminal_send {keys:[...]}` navigates `less`. Unit-test `buildTerminalSendPayload` (`interactive_terminal_tool.go:420`) for each new key -> byte mapping.

---

## P1 - Tool-surface cleanup (loops + "obvious issues")

### P1-1. Collapse the overlapping shell entry points [spec] partial

**Problem.** The agent is offered `shell`, `bash`, `terminal_run`, `terminal_send`, `terminal_read`, `start_listener`, plus `http_probe`, `run_script`, `read_tool_result`. Every terminal-tool description carries a long "use THIS not THAT" paragraph (see `interactive_terminal_tool.go:45-49`, `320-326`). This is exactly the ambiguity small local models (per memory: gemma4 tool-calling is fragile; qwen better) resolve incorrectly, and it's more decision surface than Claude/Codex expose. Claude Code ships **one** `Bash` (persistent, backgroundable, exit-code) + a couple of read helpers; Codex similar. Orthogonality beats coverage.

**Fix (pick one, lowest-risk first):**
- **Minimal:** drop `bash` as a separate tool (it's a near-duplicate of `shell`); keep `shell` for one-shot deterministic + `terminal_*` for interactive. Confirm `internal/tools/shell.go` vs `bash.go` and remove/alias the redundant one.
- **Better:** merge `terminal_run` into `terminal_send` with a `run:true`/no-op distinction, so the interactive surface is `terminal_send` (write) + `terminal_read` (look). Three interactive verbs where two suffice is a classic model-confusion source.
- Trim each description to one sentence + a one-line "use X instead when...". Move the long rationale to this doc / system prompt, not every schema.

**Acceptance:** `internal/app/app_test.go` tool-set assertions (around `:941`, `:3057`) updated; agent_eval (`internal/app/agent_eval.go`) shows equal-or-better tool-selection accuracy.

### P1-2. Prompt-glyph detection is bash-only across the codebase [done] first pass

Beyond P0-2, audit every place that assumes a `$`/`#` prompt (`endsWithShellPrompt`, routing notes at `app.go:6940-6946`, doctor checks `doctor.go:574,708`). Once OSC-133 markers exist, route all of these through `sess.promptReady` so PowerShell/cmd/zsh users aren't second-class.

### P1-3. `terminal_read` conflates "screen" and "history" [done] first pass

`terminal_read` returns `formatTerminalScreen(scroll, lines)` - always the tail of the flat history. There's no way to ask for *just the current screen* vs *scrollback history*, and no way to search within it. Add a `mode` (`screen` | `history`) and/or a `grep` filter param (reuse `internal/tools/scan_output.go` hygiene). tmux exposes both; the agent currently can't get the clean "what's on screen right now" view for a TUI without history noise.

### P1-4. Confirm `run_script` executes over the tool registry, sandboxed [done] first pass

`run_script` (`internal/app/run_script_tool.go`) is the programmatic-tool-calling bet (U4). Verify it actually routes inner calls through the registry (budgets, rollback, offload, ledger) per `docs/architecture-review-unrestricted-2026-06-30.md:143`, and that it can't shell out unsandboxed. This is the highest-leverage "better tool" pattern from Claude/Codex (batch many tool ops in one model turn) - make sure it's real, not a stub.

---

## P2 - Tooling upgrades that match Claude/Codex patterns

These are additive "better tools," ordered by ROI:

1. **`terminal_run` with an optional blocking mode.** Add `wait_for:"prompt"` so a one-shot interactive command *blocks until the prompt marker returns* (bounded by a timeout) and returns full output + exit code in a single turn - the ergonomics of Claude's `Bash` but on the live PTY. Eliminates the entire poll-loop failure class for deterministic commands while keeping non-blocking mode for streams. Depends on P0-2.
2. **Structured tool results everywhere.** The terminal tools already emit a `contract:` block (`interactive_terminal_tool.go:174`). Extend that to a compact machine-readable header (`state`, `exit`, `cwd`, `next_tool`) on *every* shell-family result so small models parse it deterministically instead of reading prose hints.
3. **`cwd`/env awareness in the read tools.** Surface the current working directory (from OSC 133 `;P;cwd=` or `pwd` marker) in each terminal result so the agent never loses track after `cd`. Humans see the prompt; the agent should get it structured.
4. **A `apply_patch`-style multi-file edit tool** (Codex's `apply_patch`). Current edits are `old_string/new_string` single-hunk. A patch tool that applies several hunks across files atomically reduces round-trips and matches what frontier CLIs use. Optional - only if edit round-trips show up as a bottleneck in `agent_eval`.
5. **Scrollback search / `read_tool_result` for the terminal.** Large scans already offload via `read_tool_result` (`tool_result_store.go`). Make the terminal scrollback addressable the same way (a handle + `grep`) so the agent can pull the one `[200 OK]` finding out of 2000 lines without re-running the scan (this is the failure mode from memory `scan_output_starvation`).

---

## Priority summary

| # | Item | Why it matters | Effort |
|---|------|----------------|--------|
| P0-1 | Headless VT screen for the agent | The core 1:1-with-human gap; unlocks all TUIs/REPLs | [done] first pass |
| P0-2 | OSC-133 completion + real exit code | Kills the poll-loop-on-finished-command bug on every backend | [done] first pass |
| P0-3 | Detect in-place screen change | Busy commands stop looking idle to the agent | [done] first pass |
| P0-4 | Full keystroke vocabulary | Actually navigate less/vim/msfconsole/history | [done] first pass |
| P1-1 | Collapse overlapping shell tools | Fewer wrong tool choices (esp. small models) | [spec] partial |
| P1-2 | De-bash-ify prompt detection | PowerShell/zsh parity | [done] first pass |
| P1-3 | Split screen vs history in read | Clean TUI reads | [done] first pass |
| P1-4 | Verify run_script sandbox/registry | Programmatic calling must be real+safe | [done] first pass |
| P2-* | Blocking mode, structured results, cwd, patch tool, scrollback search | Match Claude/Codex ergonomics | [done] first pass for wait_for/cwd/contracts/search; patch tool open |

**Status:** P0-1 through P0-4 are implemented as first passes. Continue with live shell smoke tests
and richer VT fidelity before treating terminal parity as fully complete. P1-1 remains partial:
`bash` is hidden from broad routing by default, but the registry has not yet fully collapsed shell /
terminal tools into the final Claude-shaped surface.

## Implementation notes - 2026-07-02

- Done: Chat now suppresses `todo_*` tool call/result spam from the main transcript. Todo tools still update run activity and task-run state, but the visible plan belongs in the composer Plan popover and existing Plan/Run views.
- Done: Composer now has Plan and Tools popovers on the bottom chatbar. Plan state is labelled separately from evidence so a completed todo does not imply proof of root/user access.
- Done: `terminal_send` now supports `key` and `key_sequence` for named keys: arrows, Home/End, PageUp/PageDown, Delete, Enter, Backspace, Tab/Esc, and `ctrl-a` through `ctrl-z`. Existing `keys` and `control` remain compatible.
- Done: `terminal_read` now accepts `mode:"screen"|"history"` and includes the selected mode in the structured result contract. Today both modes still use cleaned scrollback; this preserves API shape for the future headless VT screen.
- Done: broad shell tool routing no longer advertises the duplicate `bash` alias by default. The alias still exists for compatibility, but new turns see the smaller shell surface.
- Done: shared-terminal `__MAULER_DONE_<id>:<exit>` and `__MAULER_CWD_<id>__<cwd>` markers now update live session state. `terminal_read` / `terminal_run` include `exit` and `cwd` when marker state is available, and marker-ready sessions classify as `prompt_or_idle` instead of relying only on `$`/`#` prompt glyphs.
- Done: Workspace inspector duplicate cards for Open Folders and Latest Artifact were removed; the right panel now relies on the actual file browser/facts surfaces instead of repeating file lists.
- Done: first-pass headless terminal screen model landed. The shared terminal now keeps a rendered agent-side screen fed from the marker-filtered raw PTY byte stream, applies common controls (`\r`, newline, cursor movement, erase line/screen, cursor position), resizes with `ShellResize`, and `terminal_read mode:"screen"` / `terminal_run` return the rendered screen rather than only flat scrollback.
- Done: `terminal_read` waits now also wake on rendered-screen generation changes, so in-place repaint/progress/TUI updates no longer look like `no_output_yet` just because scrollback line count did not grow.
- Done: bash/WSL and PowerShell shared terminals now install OSC-133 prompt hooks. The backend parses raw `OSC 133;D`, `OSC 133;P;cwd=...`, and `OSC 133;A` markers before sanitization, updating live exit/cwd/prompt-ready state for arbitrary prompt returns, not only TheMauler wrapped shell calls.
- Done: `terminal_run` now supports `wait_for:"prompt"` for Claude/Codex-style deterministic terminal runs that return when the prompt marker comes back, bounded by `wait_ms`, with exit/cwd surfaced in the contract when available.
- Done: `terminal_read` now supports `grep` and `save_result`. Grep searches the current screen or terminal history without rerunning commands, automatically saves search output as an offloaded tool result, and returns a `result_id` for `read_tool_result`. This makes terminal scrollback addressable like other large tool output.
- Done: shell-family results now carry compact machine-readable contracts. Foreground `shell` / `bash`, standalone background jobs, and shared-terminal wrapped shell results include `state`, `backend`, `exit`, `cwd`, `result_id`, and `next_tool` while preserving the old human-readable exit/footer lines.
- Done: `run_script` now returns the same kind of structured contract as the shell family, including `state`, `inner_tool_calls`, `max_tool_calls`, `timeout_seconds`, `last_tool`, `failed_tool`, `error`, `next_tool`, and `do_not_repeat` for success, timeout, read-error, wait-error, and failed-inner-tool paths. It still preserves stdout/stderr/exit sections for human inspection.
- Done: Run routing now has a soft schema budget. If a shell-centric prompt widens past the budget, the router trims back to the current phase toolset unless the prompt explicitly needs browser/research tools, and emits a `tool_routing_warning` ledger event so Brain/Run can show the count and phase.
- Done: Telegram/channel-bus natural-language requests can now use a narrow `quick_action` lane for simple terminal chores such as opening a tmux session, instead of starting a full model/project run.
- Done: Chat tool-result cards now surface `result_id` handles as `Read result` actions. Clicking one primes the composer with a `read_tool_result` request instead of making the user copy handles by hand.
- Done: Doctor now includes deterministic tooling smoke checks for OSC-133 parsing, terminal-history search, shared-terminal contracts, and `run_script` contracts. These are local/unit-style checks, not network or target probes.
- Done: Critical-action verifier prompts now require proper evidence-backed verification, not cheap/pro-forma checks. The verifier packet demands command plus result evidence for webshells, listeners, sessions/root, target routes, and file changes before success/failure claims.
- Verified: `go build ./...` and `go test ./internal/app/ ./internal/tools/` pass after the shell-result contract pass. Earlier UI pass also verified `frontend/npm run build`.
- Still open: P0-1 full VT-library-grade emulation if we need richer alternate screen/mouse/private-mode fidelity. Current in-house renderer covers the practical shell/REPL/progress/TUI basics.
- Still open: P0-2 polish for non-bash shells beyond the first PowerShell hook, plus live smoke tests against Windows PowerShell/cmd/zsh variants.
- Still open: live end-to-end terminal smoke suite across Windows PowerShell, cmd, WSL bash, and zsh to prove the smoke-tested primitives hold in real shells.

## Verify (every change)

```bash
go build ./... && go test ./internal/app/ ./internal/tools/
```
Plus a live smoke test in `wails dev`: start `python3`, run a command, drive `less`/`msfconsole`, and confirm the agent's `terminal_read` matches what the human sees in the xterm.js pane. Grade tool-selection/loop behavior with `internal/app/agent_eval.go` before/after.
