# CLI / shell "encoding battle" — root-cause analysis (2026-06-11)

Analysis of the live task log (`~/.config/mauler/task-runs.json`, 8 runs, 211 shell/bash
tool calls) to explain why the agent "gets caught in an encoding battle half the time" and
how to fix the CLI experience. Findings + prioritized fixes; nothing changed yet.

## TL;DR

The model emits shell operators **HTML-escaped** (`&amp;` for `&`, `&gt;` for `>`,
`&lt;` for `<`), and the escaping **compounds every retry** into a doom loop. Mauler tries
to undo it but **caps the unescape at 3 passes**, so deep escaping survives, the command
errors, and the model "fixes" it by escaping even harder. A one-line change (unescape to a
fixpoint instead of 3 passes) breaks the loop at the execution layer.

## Evidence

- **33% of shell calls (69 / 211)** contained HTML entities in the command.
- Entity counts: `&amp;` ×106, `&gt;` ×32, `&lt;` ×2.
- **27 of those 69 errored** (~39%).
- The escaping **escalates across consecutive retries** of the same intent:
  ```
  2>&1                                   (what the model means)
  2&gt;&amp;1                             (1 layer)
  2&gt;&amp;amp;amp;1                     (3 layers)
  2&amp;amp;amp;gt;&amp;…×8…1            (8 layers)
  2&amp;amp;…×11…gt;&amp;…×12…1          (worst observed)
  ```
- **14 errored commands were still broken after Mauler's 3 unescape passes** — i.e. the cap
  is the direct cause of those failures.

## Root cause

`internal/tools/shell.go` → `cleanShellCommand`:

```go
for i := 0; i < 3; i++ {                 // <-- capped at 3
    next := html.UnescapeString(command)
    if next == command { break }
    command = next
}
```

`html.UnescapeString` peels exactly **one** layer per call (`&amp;amp;` → `&amp;`). The
model routinely emits 5–12 layers, so 3 passes leave `&amp;…1` in the command → the shell
sees a malformed redirect → error. The model interprets the error as "still not escaped
enough" and adds more layers next turn. **The 3-pass cap turns a cosmetic quirk into a
self-reinforcing failure loop.**

Verified: feeding the worst real command through a loop-until-stable unescape collapses it
back to exactly `... 2>&1`, while the 3-pass version stays broken.

Note: results almost never reflect the escaped form back (1/69), so the model is escalating
on its own in reaction to the *errors* — which means **removing the errors removes the
trigger.**

## ✅ Implemented (2026-06-11)

All four fixes landed; `go build ./...` + full `go test ./...` green.

- **#1 full unescape** — `cleanShellCommand` (`internal/tools/shell.go`) now unescapes to a
  fixpoint (bounded at 24 passes) instead of 3. Regression test
  `TestCleanShellCommandCollapsesDeeplyEscapedOperators` pins the worst real command from
  the log collapsing back to `2>&1`.
- **#2 recovery hint** — `runShell` records whether the raw command contained HTML entities
  (`htmlEntityRE`) and, on a non-zero exit, appends a hint telling the model to use literal
  operators and stop escalating the escaping.
- **#3 history normalization** — the agent loop now runs `normalizeToolCallArguments` over
  the tool calls **before** appending the assistant message to history (`app.go`), so the
  model never re-reads its own `&amp;amp;` commands. This also cleans the UI display and the
  executor input; the existing per-call normalize is now a harmless no-op.
- **#4 prompt nudge** — the `shell`/`bash` tool description now tells the model to write
  literal operators and never HTML-escape them.

> Re: "slower/dumber last run" — the encoding doom loop was almost certainly a big part of
> it: ~33% of shell calls were escaped and 39% of those errored, so a third of the run's
> tool budget and context was being burned re-trying mangled commands and reading the
> failures. Removing the loop should restore both speed and apparent quality.

## Fixes (prioritized)

### 1. (P0, one line) Unescape to a fixpoint, not 3 passes
In `cleanShellCommand`, replace the `for i := 0; i < 3` cap with a loop that runs until
`html.UnescapeString` is stable (bounded by a generous safety cap, e.g. 20, since the loop
strictly shortens and converges). This makes any depth of HTML escaping execute correctly,
so the command never errors on this account and the escalation loop never starts. Highest
impact, lowest risk. Add a regression test with the worst real command above.

### 2. (P1) Targeted recovery hint when an errored command contained entities
`shellFailureHint` doesn't cover this case. When a command errors **and** the raw input
contained `&amp;`/`&gt;`/`&lt;`, append:
> hint: shell operators were HTML-escaped. Write them literally — use `&`, `>`, `<`, `|`,
> not `&amp;`, `&gt;`, `&lt;`. Do not add more escaping.
This redirects the model toward the fix instead of escalating. Cheap, and it's the exact
moment the model is deciding what to try next.

### 3. (P1) Normalize stored tool-call arguments so history shows clean commands
The agent history stores the model's escaped `tool_call` arguments verbatim
(`app.go` appends `tc` as-is), so on later turns the model re-reads its own `2&amp;amp;1`
and is primed to keep escaping. After a successful clean, write the **cleaned** command back
into the stored tool-call arguments (or store a normalized copy) so the conversation the
model sees contains literal operators. Removes the reinforcement signal at the source.
Medium effort; pairs well with #1.

### 4. (P2) Prompt nudge in the shell tool description / system prompt
Add one line to the `shell`/`bash` tool description: *"Provide commands as plain text with
literal operators (`&`, `>`, `<`, `|`). Never HTML-escape them (`&amp;`, `&gt;`)."* Won't
fully stop a model that's prone to it, but reduces the rate; combine with #1 as the backstop.

## Secondary CLI-experience notes (lower priority)

- **URL-encoding vs shell-operator confusion.** In command-injection payloads the model
  mixes URL-encoded operators (`%3e%261`) *and* shell entities (`2&gt;&amp;1`) in the same
  curl call. Inherent model confusion between "this is a URL query param" and "this is the
  outer shell." Hard to fix at platform level; #2's hint helps nudge it.
- **Shared-terminal marker noise.** The visible terminal shows the raw
  `__MAULER_START_<id>__ … __MAULER_DONE_<id>:<status>` wrapper and a doubled command echo.
  Functional (run/exit tracking) but noisy; consider hiding the marker lines from the
  *visible* terminal while still parsing them. Cosmetic.

## Suggested order

1 → 2 → 3 → 4. #1 alone should eliminate most of the "encoding battle"; the rest harden it
and stop the model from re-learning the bad pattern from its own history.
