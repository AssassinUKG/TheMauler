# Mauler Run Failure Review - 2026-07-06

## Scope

This review covers the two runs immediately after the terminal Ctrl-C / Enter recovery rebuild:

- `task-2026-07-06T11-20-38+01-00`
- `task-2026-07-06T11-20-56+01-00`

Primary evidence source: `~/.config/mauler/run-ledger.jsonl`.

## Executive Summary

The rebuild fixed the previous Ctrl-C storm class, but the agent still failed badly. The new dominant failure was a terminal state-machine false positive: TheMauler classified the shared terminal as a connected live shell/session because the terminal tail contained the word `whoami` inside a curl webshell command line. Once classified as `connected`, `terminal_send command=...` was blocked repeatedly, but each block was returned as a normal `done` tool result. The model then kept trying more `terminal_send` commands against the same blocked state.

The run also showed unresolved compact-tool compatibility failures:

- The model emitted legacy `{"bash":"..."}` args to the compact `shell` tool, causing `shell: command is required`.
- The model emitted XML-ish tag leakage in read args, e.g. `scans</path>`, causing invalid Windows paths.
- HTML entity escaping still appeared in logged terminal inputs and escalated across retries, especially `2&gt;&amp;1`, `2&amp;gt;&amp;amp;amp;1`, etc. The command path decodes before execution when it runs, but many calls were blocked before execution, so the bad args still polluted model history and repeated attempts.

## Current Status - 2026-07-06

Fixed or improved after this review:
- Connected-state false positives from curl/webshell command text have focused regression coverage and routing fixes.
- `terminal_send command` normalizes command text before routing/execution and now returns only the output appended after that command, not stale terminal screen/history.
- `terminal_read mode=history` without `grep` no longer dumps old scrollback into the model.
- AI Commands no longer duplicates terminal tools and no longer marks normal terminal read states as red errors.

Still open / verify next:
- Add a central compact-tool arg sanitizer so legacy `{"bash":"..."}`, `{"cmd":"..."}`, `{"powershell":"..."}`, and `{"input":"..."}` repair into `shell.command`.
- Strip trailing XML-ish closing tags from path args, and reject remaining angle-bracket markup before file tools run.
- Ensure residual HTML entities are rejected or repaired before routing/logging, so malformed raw variants do not enter model history.
- Add `agent_eval` cases for repeated terminal state-machine blocks, legacy shell args, XML-ish paths, and terminal command delta results.

## Run `11:20:38`

Short run, stopped by the user.

Counts:

- Events: `36`
- Tool calls: `1`
- Tool used: `read`
- Stop: `user_stopped`

Sequence:

1. Started in report phase.
2. Read `C:/Users/richa/Documents/HTB_writeups/Connected.md`.
3. User stopped the run during the next model call.

No significant autonomous loop evidence in this run.

## Run `11:20:56`

This is the main failed run.

Counts:

- Events: `670`
- Tool calls: `50`
- `terminal_send`: `34`
- `terminal_read`: `5`
- `todo_write`: `6`
- `shell`: `2`
- `read`: `2`
- `glob`: `1`
- Blocked state-machine terminal results: `30`
- Escaped command inputs containing `&amp;` / `&gt;`: `12`
- Tool errors: `3`

The run was eventually stopped by the user at `11:26:32`.

## Primary Failure: False `connected` Terminal State

The key code path is:

- `internal/app/app.go`
- `sharedTerminalStateSnapshot`
- `terminalTailLooksLikeConnectedSession`

The current connected-session heuristic returns true if the last terminal tail contains any of:

- `connection received`
- `connected to`
- `accepted connection`
- `open shell`
- `shell opened`
- `meterpreter session`
- or in the last 8 lines: `uid=`, `gid=`, or `whoami`

The bad condition is `whoami`.

In the failed run, the terminal tail included the literal curl command:

```text
curl -sk https://connected.htb/ekx2wj1wzq/lpazncch.php?cmd=id%3Bwhoami%3Bhostname ...
```

That command line contains `whoami`, but it is not evidence of a connected live shell. The classifier treated it as one anyway.

After that, repeated calls returned:

```text
[tool_state_machine state=connected allowed=false recommended=terminal_send]
decision: shared terminal appears to contain a connected live shell/session; drive it with terminal_send and inspect with terminal_read instead of starting a new command
```

This blocked normal `terminal_send command=...` work even though the terminal was just a local shell/prompt surface.

## Secondary Failure: Blocks Count As `done`

The terminal state machine block is returned as a tool result with status `done`.

That means the model sees a successful tool call whose content says "blocked". This is too weak for local models. It does not trip a hard retry policy, and it allows the model to keep selecting the same tool family.

Observed result:

- `30` state-machine terminal blocks.
- The model kept trying variants of root/webshell curl checks.
- The run got slower and prompt budget ballooned.

The block should be represented as `blocked` or `routed`, and repeated semantic blocks should hard-stop or force a specific recovery tool.

## Secondary Failure: Legacy `bash` Args Still Leak Into `shell`

The model emitted:

```json
{"bash":"grep -n \"connected\" /etc/hosts"}
```

Against the compact `shell` tool.

Current `shellParams` only accepts:

```go
Command string `json:"command"`
```

So the result was:

```text
error: shell: command is required
```

This happened twice at `11:21:09` and `11:21:12`.

Cause:

- Removing the `bash` tool name was not enough.
- The model still has old arg-shape priors.
- The compact `shell` tool should either repair legacy `bash`/`cmd`/`powershell` arg keys into `command`, or reject with a stronger structured retry message.

## Secondary Failure: XML-ish Tool Arg Leakage

The model emitted:

```json
{"path":"C:/Users/richa/Documents/HTB_writeups/scans</path>"}
```

The read tool tried to open:

```text
C:\Users\richa\Documents\HTB_writeups\scans<\path>
```

and failed:

```text
The filename, directory name, or volume label syntax is incorrect.
```

Cause:

- Tool argument sanitation handles some inline markup but does not sanitize compact tool args at execution time.
- Compact `read` should strip trailing XML-ish closing tags from path fields and reject remaining angle-bracket markup.

## Secondary Failure: HTML Entity Escalation Still Pollutes History

Examples from the run:

```text
2&gt;&amp;1
2&gt;&amp;amp;1
2&amp;gt;&amp;amp;amp;1
2&amp;amp;gt;&amp;amp;amp;amp;1
2&amp;amp;amp;amp;amp;gt;&amp;amp;amp;amp;amp;amp;amp;amp;amp;amp;amp;1
```

The terminal command execution path now decodes commands when actually executed. But many of these calls were blocked before execution because the terminal was misclassified as `connected`, so the raw escaped input and blocked result entered the conversation history. The model then escalated escaping in retries.

Fix direction:

- Normalize terminal command args before state-machine checks.
- Log the normalized command into tool history, not the raw escaped command.
- If residual entities remain, return a clear `tool_error`/`blocked` result and do not let the raw malformed input become the main evidence packet.

## Prompt Budget Degradation

The run's prompt budget warnings worsened over time:

```text
system 5818>3000, tools 2427>1500
...
system 16732>3000, tools 3208>1500, system+tools over 20 percent
```

Causes:

- Repeated blocked tool results were appended.
- Verifier prompts were repeatedly queued.
- Evidence pins were created for blocked terminal state-machine outputs.
- The system prompt is already too large before the loop starts.

This likely made the local model worse turn by turn.

## What Got Better

The old Ctrl-C storm did not recur in this run.

The patch successfully changed the failure mode away from repeated `Ctrl+C`, but it exposed deeper terminal/router weaknesses.

## What Got Worse

The terminal state machine now blocked too aggressively because of the false `connected` state.

The agent had no hard "blocked result loop" escape hatch for `terminal_send`, so it repeated the same failed action class 30 times.

## Recommended Fixes

### P0-1: Fix Connected-Session Detection

Do not classify a terminal as `connected` merely because a command line contains `whoami`.

Safer rules:

- `uid=` / `gid=` should only count if they appear in output lines, not in an echoed command line.
- `whoami` should not be a connected-session signal by itself.
- Prefer explicit connection phrases plus shell-output proof, e.g. `Connection received` followed by `uid=` output or a non-local prompt.
- Ignore lines that start with the local shell prompt and include a typed command.

### P0-2: Allow Local Commands When State Is Falsely Connected Or Stale

If state is `connected` but the recent tail also contains a normal local shell prompt, allow `terminal_send command=...` or route it to isolated `shell`.

### P0-3: Treat State-Machine Blocks As Blocks

Blocked state-machine results should not be logged as ordinary `done` tool results.

Use status `blocked` or `routed`, and add a repeated-block policy:

- After 2 semantic `terminal_send` blocks with the same state, force `terminal_read` or Recover.
- After 3, hard-stop with a clear blocker.

### P0-4: Normalize Args Before Routing And Logging

For `terminal_send command=...`:

- Decode HTML entities before state-machine checks.
- Use the normalized command in ledger/tool history.
- Reject residual malformed entities before the model sees another escaped variant.

### P0-5: Repair Legacy Compact Tool Arg Shapes

For `shell`:

- Accept `bash`, `cmd`, `powershell`, or `input` as legacy aliases for `command`.
- Log a repair note once, not an error loop.

For `read`:

- Strip trailing XML-ish tags from `path`.
- Reject remaining `<...>` markup with a strong retry message.

### P0-6: Stop Pinning Blocked Tool Outputs As Evidence

Blocked state-machine outputs should not become evidence pins. They are control-plane feedback, not target evidence.

### P1: Reduce Prompt Bloat

The system/tool prompt remains far above target. Repeated verifier text and blocked outputs compound the issue. Once P0 loop guards are fixed, reduce:

- repeated verifier injections,
- repeated memory conflict summaries,
- full blocked tool result bodies,
- state-machine output verbosity after first occurrence.
