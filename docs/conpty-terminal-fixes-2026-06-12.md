# ConPTY / terminal — review + fix list (2026-06-12)

Review of the shared-PTY shell (ConPTY on Windows, go-pty everywhere). The agent
protocol (marker-framed commands) and session lifecycle are sound. The biggest
problem is that the UI pipeline **strips all VT/ANSI escapes and renders plain
`<div>`s**, which breaks colours, TUIs and progress bars. This doc is the punch
list for the xterm.js migration and the surrounding correctness fixes.

Files: `internal/app/app.go` (terminal section ~L3578-4120),
`internal/app/shell_windows.go`, `frontend/src/components/TerminalPane.tsx`,
`frontend/src/wailsjs/go.ts`.

---

## DONE in this pass (xterm.js migration)

The frontend now renders a real terminal. Backend splits two consumers:

- **UI path** — `pipeShellOutput` emits **raw PTY bytes (base64)** on
  `mauler:shell_output` (`{id, data}` where `data` is base64). `TerminalPane.tsx`
  decodes to `Uint8Array` and writes to xterm.js. Colours, cursor moves, `\r`
  repaints, `vim`/`htop`/`less` now work.
- **Agent path** — unchanged semantics: complete lines are assembled from the raw
  stream **first**, then `sanitizeTerminalLine` runs per line and the plain-text
  record is pushed to `sess.output` for the marker protocol. This fixes the
  escape-split-across-`Read()` bug (#2) for the agent because a sequence is only
  sanitized once its whole line has arrived.
- `ShellInput(id, text)` now writes **raw bytes** (xterm `onData` already includes
  `\r`, `\x7f`, arrow/`\x1b[` sequences, `\t`, `\x03`). The old "append `\r` /
  single-control-char special case" is gone.
- Dropped-line accounting: when `sess.output` is full the agent now gets a
  `[N line(s) dropped]` marker instead of silent truncation (#2).
- Deps added: `@xterm/xterm`, `@xterm/addon-fit`. Resize uses `FitAddon` (real
  cell measurement) instead of the `clientWidth/7.4` guess.
- **Framing markers + `stty -echo` no longer leak into the UI.** `uiMarkerFilter`
  is now line-aware: it drops the whole echoed wrapper-command line (both markers
  on one line), drops lone START lines, and strips only the token from a DONE
  marker attached to real output (keeping the output + newline). Incomplete
  non-marker lines stream immediately so prompts/TUIs stay responsive. Covered by
  `TestUIMarkerFilterStripsMarkers`.
- **Removed the racy `stty -echo` / `stty echo` toggle entirely** (this also
  resolves the old item #7). Echo stays on; the agent already skips the wrapper
  echo via `isSharedTerminalWrapperEcho`, and the UI filter drops it, so the
  terminal no longer shows `stty -echo` or the giant wrapper command.
- **write_file/edit_file verification now follows WSL routing.** Previously the
  verifier `os.Stat`'d the Windows host for a `/tmp/...` path that only exists
  inside WSL, producing `Verification failed: stat /tmp/…: GetFileAttributesEx
  \tmp\…`. `readVerifiedFile` now uses `tools.ShouldUseWSLForPath` +
  `tools.ReadFileViaWSL` to read the file back inside the distro (and skips host
  linting for WSL-routed files).

### Echo-wrapping parse crash fix (2026-06-12, latest)

- **Symptom:** commands failed instantly with `shared terminal: invalid exit code "'"`
  and result fragments like `4297200__' "$PWD"; printf '%s%s\n' '`. Cause: the
  terminal **wrapped the echoed wrapper command across rows**; a fragment carrying
  the `__MAULER_DONE_…:` marker (split from START) was parsed as the real
  completion, with status = a stray quote. The cwd line lengthened the wrapper and
  made it worse.
- **Fix:** `sharedTerminalWrapper` now builds the marker prefix from a shell
  variable — `M=__MA''ULER_; … printf '%s\n' "${M}START_<id>__"`. The `''` splits
  the literal, so the **echoed command never contains a literal `__MAULER_`** —
  only the printf OUTPUT (after expansion) does. Echo fragments therefore can't be
  mistaken for markers, and since `started` only flips on the real START output,
  the echo is never captured. Verified live in WSL kali.
- Consolidated the line classification into a tested `sharedTerminalParser` shared
  by the foreground loop, `runSharedCommandSimple`, and `interruptSharedTerminalRun`.
  A non-numeric DONE status is now ignored (defense-in-depth) instead of fatal.
  Covered by `TestSharedTerminalParserIgnoresEchoAndWrapping` +
  `TestSharedTerminalWrapperUsesMarkers` (asserts no literal marker in the wrapper).

### Agent-effectiveness pass (2026-06-12, later)

- **Timeout → Ctrl+C, session preserved.** `interruptSharedTerminalRun` sends
  `\x03` then probes with a fresh `__MAULER_RECOVER_<runid>__` sentinel. Since
  bash aborts the wrapper's command list on SIGINT (the DONE marker never prints),
  the old code waited for a marker that never came and killed the session anyway;
  the sentinel confirms the shell is back at a prompt so cwd/env/foothold survive.
  Only a genuinely wedged shell is killed.
- **Background jobs.** `shell` tool gained `background: true` (launch detached →
  returns a `jN` handle, output to `/tmp/mauler_job_jN.log`, PID to a pidfile) and
  `job: "jN"` (poll running/done + tail). See `startBackgroundJob` /
  `pollBackgroundJob` / `runSharedCommandSimple`. Lets the agent fire `nmap -p-` /
  gobuster and poll instead of blocking + risking the timeout-kill.
- **cwd in the result footer.** The wrapper emits a `__MAULER_CWD_<runid>__<pwd>`
  line (its own printf); the UI filter drops it whole (new `markerCWD` branch in
  `matchMarker`), and the foreground path appends `cwd: <path>` to the result so
  the agent stays oriented after `cd`. Covered by `TestSharedTerminalCWDExtractAndStrip`.

---

## Still TODO for Codex (not done in this pass)

### High value

1. ~~**Timeout should not nuke the session.**~~ **DONE** (see agent-effectiveness
   pass above). Original note: `runCtx.Done()` branch called `a.shellSess.cancel()`
   and dropped the session,
   destroying cwd/env/venv state that is the entire point of a shared terminal.
   Escalate instead: write `\x03` (Ctrl+C → ConPTY real interrupt), wait ~2s for
   the done marker or a fresh prompt, and only kill if still wedged.

2. **`ensureShellSession` TOCTOU race.** It releases `shellMu` between the nil
   check and `OpenShell()`, so two concurrent shell tool calls can both open a
   session and the second kills the first mid-command (and per-session `runMu`
   stops serialising). Add a dedicated `shellOpenMu`, or do check-and-create
   under one lock. Masked today only because the agent loop is serial.

3. **PowerShell / cmd lose shared-terminal mode.** `sharedTerminalSupportsBackend`
   only allows bash/wsl. Add a PowerShell wrapper for parity on native Windows:
   `'MARKER'; & { <cmd> }; "DONE:$LASTEXITCODE"`.

### Correctness

4. **`ansiEscape` regex is incomplete** — misses DCS (`\x1bP…\x1b\\`), APC, SOS/PM
   string sequences that PowerShell 7 / modern CLIs emit. Now that the UI uses
   xterm.js this only matters for the agent's sanitized text; widen the regex or
   use a small state machine.

5. **`decodeTerminalOutput` UTF-16 heuristic is probably dead code.** ConPTY
   output (including `wsl.exe` run *through* the PTY) is UTF-8; the UTF-16LE path
   only triggered when wsl.exe wrote to a plain pipe. It can misfire on binary
   output and breaks if a pair straddles a `Read()`. Verify once with `wsl.exe`
   through the PTY, then delete.

6. **Exit-code parse ignores errors** — `strconv.Atoi(codeText)` silently yields 0
   on garbage. Treat a parse failure as unknown (-1), not success.

7. ~~`stty -echo` + `time.Sleep(50ms)` + drain is a race.~~ **DONE** — dropped the
   stty dance; the UI filter drops the wrapper echo instead.

### Frontend polish (mostly handled by xterm.js, verify)

8. Auto-scroll no longer fights the user (xterm keeps position when scrolled up).
   Confirm `scrollback` is set high enough (e.g. 5000).
9. Partial multi-byte UTF-8 at a `Read()` boundary can still become `?` in the
   agent's sanitized text. Buffer an incomplete trailing UTF-8 sequence if it
   shows up in practice (low priority).

### Windows-specific to verify

10. `hidePtyShellWindow` sets `SysProcAttr.HideWindow` on the `pty.Cmd`, but
    go-pty's ConPTY start path uses its own `STARTUPINFOEX`/attribute list and may
    ignore `SysProcAttr`. With ConPTY the child has no console window anyway, so
    it's likely a harmless no-op — confirm against go-pty's conpty start code so we
    aren't relying on it.
