# Run-Review Fixes - 2026-07-06 (for Codex to implement)

**Source run:** `task-2026-07-06T18-11-56` (connected.htb, profile `qwen3.6-nothink`).
**Author:** run-ledger post-mortem. **Status:** F1-F5 first pass implemented; reliability agent-eval scenarios added.

**Implementation update 2026-07-06.** F1/F2 were already landed before this pass. This pass added
F3 loop circuit-breaker actuation in `runAgentLoop`, F4 confirmed-target stale-IP hints, F5
orientation/no-repeat guidance, and four embedded `agent_eval` reliability scenarios:
`redirect-loop-guard`, `off-target-ip-guard`, `terminal-routing-discipline`, and
`compact-arg-repair`.
**Audience:** an engineer/agent picking this up cold. Each item is self-contained: symbol anchors,
algorithm, tests, acceptance. Keep `go build ./...`, `go vet ./...`, `go test ./...` green; add an
`agent_eval` scenario where noted.

> Anchors are point-in-time - always `grep` the named symbol to relocate before editing. The loop is
> `func (a *App) runAgentLoop` in `internal/app/app.go`; per-turn tool-result hinting happens around
> the `appendCriticalVerifierHint(tc, result)` call (~`app.go:3446`); loop metrics are built by
> `buildLoopMetrics` (`internal/app/loop_metrics.go`).

## What went wrong (evidence)

18 tool calls, user-stopped after ~2 min, `stability_score = 0`, ~119k tokens.
1. **Redirect loop (the visible stall):** `curl <url>` with no `-L` returned the same **301 Moved
   Permanently** every time; the agent re-ran it as `| head -30`, `| head -50`, `| head -100`,
   `| head -100`. The repeat guard caught only the ONE byte-identical dup (`repeated_tool_inputs=1`)
   because the `head -N` suffix changes the dedup key. It never followed the redirect.
2. **~50% of model calls produce no tool call**, every run (`model ~ 2x tool`; protocol-recovery >=
   native every run). Systemic token/latency waste.
3. **Orientation overhead:** 10 of 18 calls were re-reading its own `recap`/`progress`/notes and
   globbing 3 empty dirs (`scans/ loot/ notes/`) before touching the target.
4. **No authoritative target:** 4 IPs in play (`connected.htb`, `.26.26`, `.14.129`, `.23.41`); it
   wasted 2 calls probing the dead `10.129.14.129` (exit 7, timed out).
5. **`stability_score` hit 0 but nothing intervened** - the loop ran until the user stopped it.

Note: the 36 `session_repair` events are the U16 net *working* (filling empty assistant content so
the backend does not 400). Do not "fix" the repair count - fix the causes above.

---

## F1. Normalize the repeat/dedup key so pager-only variations collide [spec] * do first

**Goal.** `curl URL | head -30/-50/-100` (same URL, different pager tail) must count as the *same*
command for the repeat guard and the result cache, so the loop is caught and redirected instead of
running 4x.

**Why.** `normaliseLoopToolInput` (`loop_metrics.go:92`) only normalizes `timeout`/`wait_ms`; the
`| head -N` suffix survives, so the guard missed 3 of 4 repeats. Same gap in the cache key path
(`cachedToolResultForCall`, `internal/app/tool_result_cache.go:14`) and the read-repeat block
(`repeatedIdenticalReadBlock` / `canonicalToolArgs`, `internal/app/agent_loop_stability.go:32`).

**Files.**
- `internal/app/loop_metrics.go` - extend `normaliseLoopToolInput`.
- `internal/app/agent_loop_stability.go` - apply the same normalization inside `canonicalToolArgs`
  (or a shared helper) so the in-loop repeat block uses it too.
- `internal/app/tool_result_cache.go` - normalize the shell `command` before building the cache key.
- New shared helper: `func normalizeShellCommandForDedup(cmd string) string`.

**Algorithm.** For any tool whose args carry a `command` string, before hashing:
1. Strip a trailing output-pager pipeline: remove `| head [-n] N`, `| tail [-n] N`, `| head`, `| tail`,
   `| less`, `| more`, `| cat` (repeatedly, right to left).
2. Collapse whitespace; drop a trailing `2>&1`/redirect noise already handled elsewhere.
3. Keep the URL/host/flags intact - only the *display truncation* is stripped.
Return the normalized command; the existing key builders hash that. Do NOT mutate the executed
command - this is dedup-key-only.

**Tests.** `TestNormalizeShellCommandForDedup` (table: `curl X | head -30` == `curl X | head -100`
== `curl X`; but `curl X` != `curl Y`; `nmap -sV X` unchanged). `TestRepeatGuardCatchesPagerVariants`
(a `TaskRun` with the 4 curl variants reports >=3 repeats via `repeatedToolInputCount`).

**Acceptance.** The 4-curl redirect sequence from the source run reports as repeated; the in-loop
guard returns a "repeated command" contract on the 2nd variant instead of the 4th.

---

## F2. Deterministic 3xx-redirect hint on shell/http_probe results [spec]

**Goal.** When a command's output is an HTTP 3xx redirect body/header, tell the model exactly what to
do - re-request with `-L` (or hit the Location target) - instead of leaving it to infer.

**Why.** The agent saw `301 Moved Permanently ... document has moved to http://connected.htb/` four
times and never added `-L`. This is deterministic from the output.

**Files.**
- `internal/app/critical_verifier.go` - add a hint branch in `criticalVerifierHint` (it already runs
  for `shell`/`http_probe`/`terminal_send` results via `appendCriticalVerifierHint` at ~`app.go:3446`).
  This is the cheapest wiring: it flows through the existing hint path.
- Add a detector `func httpRedirectHint(result string) string`.

**Algorithm.**
1. Detect a redirect in the result: `HTTP/... 3NN`, or a body containing `Moved Permanently` /
   `301`/`302`/`307`/`308` with a `Location:`/`href="..."` target.
2. Extract the target URL if present.
3. Return: `"[hint:redirect] Response is a 3NN redirect to <target>. curl did not follow it - re-run
   with 'curl -L' (or request <target> directly). Do NOT re-run the same non-following curl with a
   different | head -N; the body will be identical."`
4. Only emit once per identical target within a run (dedupe against a run-scoped set, mirror how
   `appendCriticalVerifierHint` guards `[verifier_required:` duplication).

**Tests.** `TestHTTPRedirectHintDetects301WithLocation`, `TestHTTPRedirectHintIgnoresNon3xx`,
`agent_eval` scenario: a mocked 301 result yields the redirect hint in the tool result.

**Acceptance.** A 301 body produces the `[hint:redirect]` line with the Location target; a 200 body
produces nothing.

---

## F3. Loop circuit-breaker on collapsed stability [spec]

**Goal.** When the run is demonstrably stuck (stability crater + repeated near-identical results),
the loop injects a hard corrective system message and/or auto-pauses - instead of running until the
user stops it.

**Why.** `loopStabilityScore` (`loop_metrics.go:144`) correctly scored the source run **0**, but it is
pure observability with no actuator. The loop continued through ~8 wasted calls.

**Files.**
- `internal/app/loop_metrics.go` - expose a predicate, e.g.
  `func (m LoopMetrics) LoopStalled() bool` (score <= threshold AND
  `RepeatedToolInputs >= 2` OR `RepeatedSkips >= 2` OR `ToolErrors >= 4`).
- `internal/app/app.go` `runAgentLoop` - after `buildLoopMetrics(run)` each turn (there is already a
  metrics build at ~`app.go:2370`), check `LoopStalled()`. On first trip: append a system message
  (once per stall episode) - *"Loop-health is critical: your last actions repeated with the same
  result. Stop repeating. State the single blocking fact, then take a DIFFERENT action (follow the
  redirect with -L, switch to the confirmed target IP, or read the failing result once)."* - and bump
  reasoning effort one tier if not already max. On a second consecutive trip: pause the run with a
  clear blocker (`run_stop` reason `loop_circuit_breaker`) so the user sees why.
- Guard against thrash: only fire once per N turns; reset the episode flag when stability recovers.

**Tests.** `TestLoopStalledPredicate` (table over metrics), `TestCircuitBreakerInjectsOnceThenPauses`
(simulate 3 near-identical repeated inputs -> first trip injects, second pauses), `agent_eval`
regression: the 4-curl redirect scenario ends via circuit-breaker, not `max_tool_calls`.

**Acceptance.** A run that repeats the same command with the same result 2-3x gets a corrective
injection, and if it persists, auto-pauses with reason `loop_circuit_breaker`.

---

## F4. Pin one authoritative target; warn on off-target probes [spec]

**Goal.** The run has a single confirmed target; probing a different IP than the confirmed target
gets a hint, so the agent stops wasting calls on stale/dead IPs.

**Why.** Four IPs were in play; 2 calls hit the dead `10.129.14.129`. The facts layer already
extracts a `target` fact (`runFactTargetIPRe`, `internal/app/run_facts.go:19/135`) but nothing warns
when a command targets a *different* IP.

**Files.**
- `internal/app/run_facts.go` - surface the current confirmed `target` value (helper
  `func currentTargetIP(events) string`).
- `internal/app/critical_verifier.go` (or the execution-state hint path) - when a shell/http_probe
  command contains an IPv4 that differs from the confirmed target AND the confirmed target is set,
  append: `"[hint:target] Confirmed target is <T>. This command probes <X>. Confirm <X> is intended;
  do not chase stale IPs."`
- Do not hard-block (targets legitimately change) - hint only.

**Tests.** `TestOffTargetProbeHint` (confirmed target `.26.26`, command hits `.14.129` -> hint;
command hits `.26.26` -> no hint; no confirmed target -> no hint).

**Acceptance.** With a confirmed target, a command against a different IP gets the `[hint:target]`
line; on-target commands are silent.

---

## F5. Cut orientation overhead (inject recap/progress; skip empty globs) [spec]

**Goal.** Stop spending the first half of a run re-reading files the loop could inject, and stop
globbing directories already known to be empty.

**Why.** 10 of 18 calls were `read recap`/`read progress`(paged)/`glob scans|loot|notes` (all empty)
before any target interaction.

**Files.**
- `internal/app/execution_state_prompt.go` / `internal/app/context_docs.go` - if `.mauler/progress.md`
  and `.mauler/project-recap.md` exist, inject a compact head (already partially done for progress via
  the progress artifact) so the model has them without a `read` call. Cap size; offload the rest.
- `internal/app/tool_router.go` or the glob tool result path - when `glob` returns zero matches for a
  workspace dir, the result contract should say `"empty; do not re-glob this path"`, and the run
  should remember empty globs (run-scoped set) to short-circuit repeats.

**Tests.** `TestEmptyGlobResultAdvisesNoRepeat`, `agent_eval`: a run with progress/recap present makes
0 `read` calls against those files before its first target action.

**Acceptance.** Recap/progress content is present in the prompt without a tool call; a second glob of
a known-empty dir is short-circuited.

---

## Also verify (config, not code)

- **`tool_grammar_constraint = true`** (settings.toml) was enabled but is the roadmap's "held pending
  live probe" item. A/B it: run the same task with it off, compare `tool_protocol_request` counts in
  the ledger. If it does not lower the malformed-tool-call rate, disable it. See U7 in
  `docs/agent-loop-upgrade-roadmap.md`.
- Confirm `reasoning_effort = "high"` is not forcing thinking on the `nothink` profile (empty
  assistant turns correlate with truncated think blocks).

## Suggested order
F1 -> F2 (kill the visible redirect loop) -> F3 (auto-brake the next loop) -> F4 -> F5. F1+F3 alone would
have ended the source run without user intervention.
