# TheMauler Agentic Reliability Issues

Last reviewed: 2026-07-15
Status: MAULER-AR-001 through MAULER-AR-005 are closed; keep this register as the verification record.

This is the canonical issue register for failures that can make an autonomous run act on incorrect terminal state, lose evidence, interfere with another run, or approve incomplete work.

## Priority Order

1. MAULER-AR-001 - synchronize terminal session state
2. MAULER-AR-002 - join scripted-tool output readers
3. MAULER-AR-003 - isolate agent evaluation from live process state
4. MAULER-AR-004 - make completion evidence independent and reviewer failures explicit
5. MAULER-AR-005 - add the shared JHUT end-to-end scenario to Agent Eval

## MAULER-AR-001 - Terminal session state has confirmed data races [P0]

Status: closed 2026-07-10

Implemented: shell-session OSC and classifier state now uses a session mutex and immutable snapshots. OSC payload parsing updates state transactionally, while blocking terminal I/O remains outside the state lock.

Verification: `go test -race ./internal/app ./internal/tools` passes.

Evidence:

`go test -race ./internal/app ./internal/tools` reports races between OSC-133 updates in `applyShellSessionOSCPayload()` and reads in `terminalSessionPromptReady()`, `terminalExitLabel()`, and related terminal-state classifiers.

Impact:

- A command can be reported finished while it is still running.
- A stale exit code or working directory can be attributed to the next command.
- The agent can overlap terminal actions or choose the wrong next tool.

Affected code:

- `internal/app/app.go` - OSC/session writers
- `internal/app/interactive_terminal_tool.go` - terminal-state readers

Required fix:

1. Put mutable shell-session state behind one mutex, or publish immutable snapshots atomically.
2. Make OSC parsing update the complete state as one transaction.
3. Make classification and result formatting consume one coherent snapshot.
4. Do not hold the state lock during blocking terminal I/O.

Required tests:

- Existing terminal tests pass under `-race`.
- Concurrent OSC completion, read, interrupt, and remote/local transition tests remain deterministic.

Closure criteria: `go test -race ./internal/app ./internal/tools` passes with terminal tests enabled.

## MAULER-AR-002 - run_script reads stderr before its copier is joined [P1]

Status: closed 2026-07-10

Implemented: the stderr copier is joined after process wait and before any buffer read. Timeout, scan-error, and process-error results now preserve captured stdout/stderr.

Verification: `go test -race ./internal/app ./internal/tools` passes.

Evidence:

The race detector reports concurrent `bytes.Buffer.String()` and `io.Copy()` access in `runPythonToolScript()`. The stderr copier goroutine is started but no completion channel or wait group is awaited before formatting the final result.

Impact: failure evidence may be truncated, corrupted, or associated with the wrong completion state.

Required fix:

1. Join stdout/stderr copier goroutines before reading their buffers.
2. Avoid unsynchronized `bytes.Buffer` access.
3. Ensure cancellation closes pipes and cannot leave a copier blocked.
4. Preserve stderr on timeout and inner-tool failure paths.

Required tests: success, non-zero exit, cancellation, large stderr, and failed inner-tool cases under `-race`.

Closure criteria: all `run_script` tests pass under the race detector with complete evidence.

## MAULER-AR-003 - Agent Eval mutates process-global run state [P1]

Status: closed 2026-07-10

Implemented: Agent Eval now enters a process-state exclusion gate before creating a scenario workspace. Active desktop runs, artifacts, channel dispatch/queued work, terminal commands, and tracked background jobs block eval startup; new desktop/channel/artifact/terminal work is rejected while eval is active. Settings and workspace changes are serialized against eval. Each scenario restores both cwd and the exact previous tool-config snapshot through guaranteed cleanup paths.

Verification: preflight tests cover live-run and queued-channel blocking, tool snapshot restoration is tested directly, and `go test -race ./internal/app ./internal/tools` passes.

Evidence:

- `runOneAgentEvalScenario()` calls `os.Chdir()` into a temporary workspace.
- The eval mutex serializes evals only; it does not exclude a normal desktop or channel run.
- `tools.SetConfigSnapshot()` replaces global tool settings and is not restored after the scenario.

Impact: running Agent Eval beside a live task can redirect relative file/shell operations into the eval workspace or apply eval tool settings to the live task.

Required fix:

1. Remove process-wide cwd dependence from tools; pass an explicit workspace/run context to every tool invocation.
2. Make tool configuration request-scoped instead of global.
3. Until that lands, hard-block Agent Eval while any run, subagent, terminal command, or queued work item is active.
4. Restore any temporary global state in a guaranteed cleanup path.

Required tests: a live mock run and an eval scenario execute concurrently without path or config crossover.

Closure criteria: Agent Eval has no process-global side effects observable by another run.

## MAULER-AR-004 - Completion integrity can fail open [P2]

Status: closed 2026-07-10

Implemented: reviewer outcomes are now `pass`, `fail`, or blocking `inconclusive`. Transport, model-load, stream, malformed JSON, invalid enum, and turn-budget failures retry once and then stop with `review_incomplete`; they can no longer approve a run. Unknown project verifiers are also blocking/inconclusive. Completion coverage excludes model summary/response claims and ordinary tool claims, uses existing touched-file contents, explicit evidence/file-change bundles, and successful verification commands, and records the independent source satisfying each requested feature. Deliverable checks verify that a mutated file actually exists.

Verification: tests cover malformed reviewer JSON, provider failure, unsupported project verification, false summary claims, independent file evidence, per-feature evidence attribution, and explicit review-incomplete stopping.

Evidence:

- Reviewer transport errors, malformed verdicts, and turn-budget exhaustion currently return approval.
- Completion feature coverage searches the model's summary/response and tool inputs/results, so a claim can count as evidence without independent artifact verification.
- Projects with no detected build/test command can skip deterministic verification.

Required fix:

1. Introduce a reviewer result state `pass | fail | inconclusive`.
2. In blocking autonomous mode, treat reviewer infrastructure/protocol failures as `inconclusive` and stop with a clear review-incomplete reason after bounded retry.
3. Exclude model-authored summary claims from blocking feature-coverage evidence.
4. Prefer file diffs, parsed artifacts, successful verification commands, and explicit evidence bundles.
5. Record which evidence satisfied each requested feature.

Required tests: malformed reviewer JSON, provider failure, unsupported project verifier, false summary claims, and independently verified feature coverage.

Closure criteria: unavailable review infrastructure cannot silently strengthen a completion verdict.

## MAULER-AR-005 - Shared JHUT benchmark is not a built-in Agent Eval scenario [P2]

Status: closed 2026-07-11

Implemented: Benchmark > Run JHUT Browser Eval loads the exact shared prompt from
`Documents/MaulerBench/jhut-threejs/MAULER.md`, runs it through the production agent loop for the
selected profile, serves the resulting workspace locally, and performs independent Chrome checks.
The verifier captures desktop/mobile screenshots, console/runtime failures, canvas pixel variance
and coverage, orbit interaction change, and responsive resizing. Reports persist model/profile,
provider, context, seed, tool trace, stop reason, runtime, artifact hash, verifier version, and
screenshot paths under the Mauler config directory.

Verification: the browser verifier has an end-to-end Chrome test using a locally served interactive
canvas fixture; `go test ./...`, `go vet ./...`, `go test -race ./internal/app ./internal/tools`, and
the frontend build pass.

Evidence:

- The JHUT scenario exists in `C:/Users/richa/Documents/MaulerBench` as a manual workspace/runbook.
- The built-in `internal/app/testdata/agent_eval` suite does not run that full scenario.
- Existing JHUT verification is source-pattern based and does not prove browser rendering.

Required fix:

1. Add a reusable external-workspace scenario adapter to Agent Eval.
2. Run the exact same prompt and scoring contract used by HelixClaw.
3. Add browser, console, canvas-pixel, interaction, responsive, and screenshot checks.
4. Record model/profile, provider, context, seed, tool trace, stop reason, runtime, artifact hash, and screenshots.

Closure criteria: one command from the Benchmark page runs the same end-to-end JHUT contract for any selected profile.

## Verification Baseline

Review verification on 2026-07-15:

- `go test ./...`: passed.
- `go vet ./...`: passed.
- `npm run --prefix frontend build`: passed, with a non-blocking large-bundle warning.
- `go test -race ./internal/app ./internal/tools -count=1`: passed.
- `go test -race ./internal/engagement/... ./internal/store -count=1`: passed.
- `go test -race ./internal/controlplane ./internal/store -count=1`: passed.
- Headless settings regression: runtime-owned audio/Telegram workers do not start before Wails
  `OnStartup`, preventing asynchronous worker processes from inheriting and locking test workspaces.
