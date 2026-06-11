# Shared-terminal CLI — why the agent fights it, and the fix (2026-06-11)

The agent literally narrated *"the shared terminal mode is really problematic"* and started
working around it (`>> /tmp/file 2>&1; echo DONE; wc -c /tmp/file`) to force a result it could
trust. This documents why, what was fixed, and the deeper options.

## How shared-terminal mode works

`shell_mode = "shared_terminal"` runs one long-lived interactive bash
(`internal/app/app.go` `OpenShell` → `maulerBashInteractiveArgs`, `bash -i`). Each tool call
is written to that shell's stdin wrapped with sentinels:

```
set -o pipefail; printf '__MAULER_START_<id>__'; { <command>; }; status=$?; printf '__MAULER_DONE_<id>:<status>'
```

Mauler reads the shell's stdout looking for those markers to know when the command finished
and its exit code (`runSharedTerminalShell`). The win is a persistent, user-visible terminal
(cwd/env/reverse shells survive between calls). The cost is fragility.

## Why it was "problematic" (root causes)

1. **Pagers hang the whole session.** `git log`, `systemctl status`, `man`, anything that
   pipes to `less`/`more` waits for a keypress that never comes. The session blocks until the
   command times out — and because the shell is shared, *every later command* is stuck behind
   it. The session was started with **no pager/non-interactive env**.
2. **Credential / TTY prompts hang.** `sudo` (password), `apt` (confirm), `ssh` (host key),
   `git` (credential) all block on input. The isolated path forced `sudo -n`; the **shared
   path did not**, so sudo froze the session.
3. **One bad command poisons the session.** An unbalanced quote/brace leaves bash at the
   `PS2` continuation prompt; the done-marker never appears, so that *and every subsequent*
   command times out until the session is recycled.
4. **Marker-based capture is inherently racy.** Output buffering can split a marker across
   reads, or a command can emit text resembling a marker. Rare, but it makes results feel
   untrustworthy — which is why the model resorted to `echo DONE; wc -c`.

Net effect for the agent: commands that should take 1s sometimes hang for the full timeout,
results feel unreliable, and the model burns turns/context inventing defensive wrappers.

## ✅ Fixed now (2026-06-11)

- **Session hardened against hangs.** `maulerBashInteractiveArgs` now exports a
  non-interactive, no-pager environment for the whole session:
  `PAGER=cat GIT_PAGER=cat SYSTEMD_PAGER=cat MANPAGER=cat LESS=FRX`,
  `DEBIAN_FRONTEND=noninteractive GIT_TERMINAL_PROMPT=0 PIP_DISABLE_PIP_VERSION_CHECK=1`,
  `PYTHONUNBUFFERED=1`. Pagers now stream straight through instead of blocking on a keypress —
  this removes the single biggest cause of "stuck terminal."
- **Non-interactive sudo in shared mode.** `runSharedTerminalShell` now applies
  `tools.ForceNonInteractiveSudo` (bare `sudo` → `sudo -n`, idempotent), so a missing password
  fails fast with a clear hint instead of freezing the session. (As `root`/Kali this is a
  no-op; for non-root it's the difference between a 1s error and a full-timeout hang.)
- Build + full tests green; added an idempotency test for `ForceNonInteractiveSudo`.

A poisoned-`PS2` session still self-heals: the timeout handler already kills and recycles the
session, so at most one command is lost rather than all of them.

## Deeper options (recommended next, pick one)

The hardening removes the hang cliffs. If you want to eliminate the *fragility* class entirely,
two architectures:

- **A — Default the agent to isolated per-command exec; keep shared terminal opt-in.**
  Isolated mode (`runShell`) already runs each command in a fresh `wsl … bash -lc` with clean
  stdin, captured stdout/stderr, real timeout and cancellation — deterministic, no marker
  parsing, no state poisoning. Route normal enumeration (nmap/curl/gobuster/ffuf) through it,
  and only use the shared terminal when persistence is actually needed (interactive session,
  reverse shell, `cd`/env that must survive). Lowest risk, biggest reliability gain.
- **B — Persistent-cwd capture without a fragile TTY.** Keep one-shot isolated exec but make
  cwd/env survive by appending `; printf '<<CWD>>%s' "$PWD"` (and optionally dumping env) and
  parsing it back to seed the next command's `cd`. Gives session-like continuity with
  one-shot determinism. More work than A.

Recommendation: ship the hardening (done), then do **A** — it matches how the agent actually
works (mostly self-contained commands) and keeps the visible shared terminal available for the
cases that need it.

## ✅ Option A implemented (2026-06-11)

The agent now defaults to **isolated per-command execution**, with the shared terminal as an
explicit opt-in:

- **New `session` param** on the `shell`/`bash` tools (`internal/tools/shell.go`). Default
  false → fresh isolated shell (deterministic, cannot hang the session). `session=true` →
  persistent shared terminal, for interactive/reverse shells or a `cd`/`export` later commands
  depend on. Tool description tells the model to leave it false for normal enumeration.
- **Routing** (`app.go`): a shell call uses the shared terminal only when
  `shell_mode=shared_terminal` **and** `session=true`; otherwise it runs isolated. So the
  pager/prompt/state-poisoning hang classes simply don't apply to the common path.
- **Visibility preserved.** Isolated commands are mirrored to the live terminal pane via the
  existing `terminal_command_start` / `shell_output` / `terminal_command_done` events
  (`runIsolatedShellEcho`), capped at 500 lines in the view (full result still in Logs). So
  the user keeps watching the agent work — they just no longer share one fragile process.

Tests: `TestShellRequestsSession` added; full `go build`/`go test ./...` green.

Net effect: reliable, deterministic command execution by default; the live terminal stays
useful; and the genuinely stateful cases (reverse shell, persistent env) still work by setting
`session=true`.

## Related

- HTML-entity "encoding battle" in commands: see `docs/cli-encoding-analysis-2026-06-11.md`
  (fixed). That and this share a theme — the model's CLI experience degrades sharply when a
  command's result is unreliable, and it compensates with brittle workarounds.
