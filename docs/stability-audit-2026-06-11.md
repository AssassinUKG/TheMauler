# TheMauler — Stability & Tool-Calling Audit (2026-06-11)

Audience: a follow-up AI/engineer who will implement the fixes.
Scope requested: tool calling, agent/subagent loops, stability. Reviewer did **not**
change any code — this is a findings doc only.

Primary use case to optimize for: **local unrestricted pen-testing against HTB**,
running Qwen3.6-27B (and Gemma 4 as a faster, weaker-tool-calling fallback) through
**InferenceBridge** (the managed llama.cpp OpenAI-compatible backend at
`C:\Users\richa\Documents\InferenceBridge`). Several findings below are flagged
specifically because they hurt that workflow even though they look "correct" in the
abstract. The operator has explicitly stated the agent must be **unrestricted** — do
not add content/credential filtering that blocks the model from doing its job.

Severity legend: **P0** = silent run failure / data loss class, **P1** = wrong
behavior for the core use case, **P2** = correctness/robustness, **P3** = polish.

---

## ✅ FIXED (2026-06-11) — P0 — SSE parser silently drops tool calls when the stream ends without `finish_reason: "tool_calls"`

**Resolution.** `internal/llm/stream.go` now flushes accumulated tool-call fragments at
both stream-exit points. Extracted `flushAccumulatedToolCalls(accum)` from the
`case "tool_calls":` body and call it when `data == "[DONE]"` (if `len(accum) > 0`) and
after the scanner loop ends at EOF (if `len(accum) > 0`); only fall back to the plain
`Delta{Done: true}` when nothing was accumulated. Truncated/incomplete tool-call JSON
still routes through the `Truncated` recovery path unchanged. Regression tests added in
`internal/llm/stream_test.go`: `TestParseSSEFlushesToolCallsOnDoneWithoutFinishReason`,
`TestParseSSEFlushesToolCallsOnEOFWithoutFinishReason`,
`TestParseSSEFlushesTruncatedToolCallOnDone`. Full `ParseSSE` suite passes (18 tests).

> Note for follow-up: capture a real InferenceBridge SSE transcript (via
> `/v1/debug/logs?limit=N` or `inference-bridge-logs-export-*.json`) and confirm the
> fixture framing matches its actual `[DONE]`/finish_reason behavior. The fix is
> framing-agnostic (it flushes on both `[DONE]` and EOF), so this is verification, not a
> blocker.

### Original finding

**File:** `internal/llm/stream.go` (loop exits at `:79-81` on `[DONE]` and at `:180-186`
on scanner EOF; tool-call accumulator `accum` is only flushed inside
`case "tool_calls":` at `:144-176`).

**Problem.** Tool-call argument fragments accumulate in `accum` across deltas. They are
only emitted when a chunk arrives with `finish_reason == "tool_calls"`. If the backend
streams tool-call deltas and then terminates with `data: [DONE]` (or the body just EOFs)
*without* a separate `finish_reason: "tool_calls"` chunk, the loop `break`s / falls
through to the final `ch <- Delta{Done: true}` and **the accumulated tool calls are never
emitted**. The agent then sees a turn with zero tool calls and routes into the
"thinking-only / empty output" recovery path.

This is the exact failure class already hit twice before (see `ISSUES.md` R1 and R8 —
"tool calls written as text", "blank run", reasoning streamed as `delta.reasoning`).
**InferenceBridge is the backend in use**, and its streaming `tool_calls` path is exactly
the kind of managed-llama.cpp proxy that is inconsistent about emitting a terminal
`finish_reason` chunk — R8 was specifically an InferenceBridge streaming-extraction
failure. So this is not a theoretical edge case; it is the highest-risk path for this
setup.

**Verify against the real backend.** Capture a raw SSE transcript from InferenceBridge
for a tool-calling turn (it exposes `/v1/debug/logs?limit=N` per R9, and there's an
`inference-bridge-logs-export-*.json` already in the repo root). Confirm whether the
final tool-call chunk carries `finish_reason: "tool_calls"` or whether the stream ends
on `[DONE]` alone. Build the SSE test fixture from that actual transcript so the flush
fix is validated against InferenceBridge's real framing, not an assumed shape.

**Fix.** Before the two non-tool exit points, flush any pending `accum`:

- Factor the `case "tool_calls":` body (validate JSON per index, emit `Truncated` on
  invalid JSON, otherwise emit `Delta{ToolCalls, Done: true}`) into a helper.
- Call that helper when `data == "[DONE]"` and when the scanner loop ends, if
  `len(accum) > 0`. Only fall back to the plain `Delta{Done: true}` when `accum` is empty.

**Test.** Feed an SSE fixture that streams `tool_calls` deltas and ends with `[DONE]`
and **no** `finish_reason` chunk; assert a `ToolCalls` delta is produced.

---

## ✅ FIXED (2026-06-11) — P0/P1 — Secret & private-key redaction in tool results breaks the pen-test workflow

**Resolution.** Added `ToolsConfig.RedactSecrets bool` (`internal/settings/model.go`),
defaulting to `false` (zero value; not set in `defaults.go`, and absent from existing
saved `settings.toml` → off). `guardToolResult` now takes a `redactSecrets bool` and only
calls `redactSensitiveToolResult` when it's true; the prompt-injection / exfiltration
*labeling* is unchanged and still always runs (non-destructive). Callers
`internal/app/app.go:2156` and `internal/app/subagents.go:243` pass
`cfg.Tools.RedactSecrets`. So by default recovered keys/passwords/hashes reach the model
verbatim and the agent stays unrestricted. Tests updated in
`internal/app/guardrails_test.go`: existing redaction tests pass `true`; new
`TestGuardToolResultKeepsSecretsWhenRedactionOff` asserts secrets pass through verbatim
with the default-off flag. Optional follow-up: surface the toggle in SettingsModal for
anyone who wants it on for a generic coding workspace (not needed for the unrestricted
default).

### Original finding

**Decision (operator-confirmed): the agent must stay unrestricted. Default redaction
OFF.**

**Files:** `internal/app/guardrails.go:40-41` (`privateKeyBlockRe`, `secretAssignRe`),
`:92-95` (`redactSensitiveToolResult`). Applied to every tool result at
`internal/app/app.go:2156` and to every subagent tool result at
`internal/app/subagents.go:243`.

**Problem.** When a target box yields an SSH private key, a password in a config file,
an `/etc/shadow` hash, an `Authorization: Bearer ...` token, etc., the guardrail rewrites
it to `[REDACTED PRIVATE KEY]` / `${key}=[REDACTED]` **before the model sees it**.
On HTB the recovered credential *is the objective* — redacting it stops the agent from
pivoting, escalating, or logging in. For this explicitly unrestricted platform this is a
functional bug, not a safety win.

**Fix (implement this).**
- Add `ToolsConfig.RedactSecrets bool` and **default it to `false`** in
  `settings/defaults.go`. Gate the `redactSensitiveToolResult` call in
  `guardToolResult` behind it so recovered credentials/keys reach the model verbatim.
- **Keep** the prompt-injection / exfiltration *labeling*
  (`promptInjectionPhrases`, `secretExfiltrationPhrases`). It only prepends a
  "treat as untrusted data" note and does **not** mutate or hide the content, so it
  costs nothing and still helps when a target box serves hostile text. This is the
  right trust boundary to keep even on an unrestricted agent.
- Leave the flag in the UI so it *can* be turned on for a generic coding workspace, but
  the shipped default is off. This is the single change most likely to improve real HTB
  runs.

---

## ✅ FIXED (2026-06-11) — P1 — Subagent shell default timeout is 30s while the schema advertises 120s

**Resolution.** Registry now constructs `&Shell{TimeoutSecs: 120}` / `&Bash{TimeoutSecs: 120}`
(`internal/tools/registry.go:138-139`), and the `runShell` fallback when no default is
provided is 120s (`internal/tools/shell.go`), matching the schema-advertised default. The
shared-terminal fallback in `internal/app/app.go` (`runSharedTerminalShell`) was also
bumped 30→120 for consistency. Now subagent and direct-registry shell calls that omit the
`timeout` field get 120s instead of 30s, so omitted-timeout scans (nmap/gobuster/ffuf/
hydra) aren't cut off early. All `internal/tools` and `internal/app` tests pass.

### Original finding

**Files:** advertised default in `internal/tools/shell.go:46` ("default 120, max 300");
actual registry instance `&Shell{TimeoutSecs: 30}` / `&Bash{TimeoutSecs: 30}` in
`internal/tools/registry.go:138-139`; `runShell` fallback is 30 at `shell.go:94-100`.

**Status by path:**
- Main agent loop: uses `runSharedTerminalShell(..., cfg.Tools.BashTimeout)` with
  `BashTimeout` defaulting to **120** (`settings/defaults.go:12`) — OK.
- **Subagents** (`subagent_testfix` etc.) call `registry.Run` directly → `Shell.Run` →
  `TimeoutSecs = 30`. Any scan the subagent launches without an explicit `timeout`
  field is killed at 30s. Because the schema text tells the model the default is 120,
  the model routinely omits the field, so nmap/gobuster/ffuf/hydra get cut off early.

**Fix.** Make the real default match the contract: set the registry `Shell`/`Bash`
instances to `TimeoutSecs: 120` (and the `runShell` fallback to 120), or thread
`cfg.Tools.BashTimeout` into the registry-constructed tools so all paths agree.

---

## ✅ FIXED (2026-06-11) — P2 — Empty tool-call IDs are not synthesized; compaction can orphan tool results

**Resolution.** `flushAccumulatedToolCalls` in `internal/llm/stream.go` now synthesizes a
stable per-index id (`call_<idx>`) whenever the backend leaves `id` empty, and preserves
any real id the backend provides. Every assistant tool_call and its tool-result message
therefore share a non-empty `tool_call_id`, which (a) satisfies strict OpenAI-compatible
servers and (b) lets the existing `sanitizeCompactedMessages` pairing logic
(`internal/agent/history.go`) work without orphaning results during compaction. Tests
added: `TestParseSSESynthesizesEmptyToolCallID`, `TestParseSSEPreservesProvidedToolCallID`.

### Original finding

**Files:** `internal/llm/stream.go:127-129` (keeps `a.id` empty if the backend never
sends one); `internal/agent/history.go:242-283` (`sanitizeCompactedMessages`).

**Problem.** Local Qwen/llama.cpp frequently emit tool calls with an empty `id`.
Downstream:
- Tool result messages are built with `tool_call_id == ""` (`newToolResultMsg(tc.ID,…)`).
  Strict OpenAI-compatible servers reject a `tool` message with no matching
  `tool_call_id`, and reject assistant `tool_calls` with no following result.
- In `sanitizeCompactedMessages`, `pendingToolIDs` is only populated for `tc.ID != ""`
  (`history.go:266-268`), and `tool` messages with empty `ToolCallID` are appended
  unconditionally (`:246-252`). After a compaction you can end up with an orphaned
  `tool` message whose assistant `tool_calls` were dropped — a 400 on the next request.

**Fix.** Synthesize a stable ID at parse time in `ParseSSE` when `tc.ID == ""` (e.g.
`fmt.Sprintf("call_%d", idx)`), so every assistant tool_call and its result share a
non-empty ID. Then the existing pairing logic in `sanitizeCompactedMessages` works.

---

## ✅ FIXED (2026-06-11) — P2 — Tools re-read settings from disk on every call (and can diverge from the running config)

**Resolution.** Added `internal/tools/config.go` with a thread-safe config snapshot
(`SetConfigSnapshot` / accessors `configuredShellBackend`, `configuredShellDistro`,
`configuredShellUser`, `configuredProtectedPaths`). `shell.go` and `protected.go` now read
the in-memory snapshot and only fall back to `settings.Load()` when no snapshot is set
(e.g. unit tests), so production code paths no longer hit disk per call. The app pushes the
snapshot via `a.syncToolConfig()` in `New()`, `UpdateSettings`, `UseProfile`, and
`ApplySafetyPreset` (the last matters because the "unrestricted" preset sets
`ProtectedPaths = nil` — the snapshot must refresh or stale protected paths would keep
blocking). `settings` import removed from `shell.go`/`protected.go`.

### Original finding

**Files:** `internal/tools/shell.go:463-490` (`detectShellBackend`, `activeWSLDistro`,
`activeWSLUser` each call `settings.Load()`); also `Shell.Description()` at `:28-39`
calls `detectShellBackend("")` → `Load()` at tool-def build time;
`internal/tools/protected.go:155` (`configuredProtectedPaths` → `Load()`).

**Problem.** Every shell/edit/write/tool-def build does a disk read of `settings.toml`.
Worse, it reads *persisted* settings rather than the in-memory `a.cfg` the app is
actually running with, so ephemeral or mid-session changes (and the active workspace)
can diverge between what the agent thinks it's using and what the tool enforces.
Under load this is also a needless per-call syscall hit.

**Fix.** Inject the relevant config (shell backend, distro, user, protected paths) into
the tool structs when the registry is built, or pass it via context, instead of calling
`settings.Load()` inside the tools. Re-register/refresh tools on settings save.

---

## ✅ FIXED (2026-06-11) — P2 — Minor data races around the agent-run config snapshot

**Resolution.** `SendMessage` now reads `autonomous`/`autoAgents` under `a.mu` (moved
inside the existing locked section). Added `cloneToolsConfigRefs` to deep-copy the
`EnabledTools`/`Toolsets` maps and `ProtectedPaths`/`SafeRules` slices into the run's
snapshot, so in-place UI mutations (notably `ApplySafetyPreset` writing `EnabledTools`)
can't race with a running agent reading them. Verified with `go test -race ./internal/app/`.

### Original finding

**File:** `internal/app/app.go:500-501` reads `a.autonomous` / `a.autoAgents` without
holding `a.mu`; `:495` `cfg := *a.cfg` is a shallow copy — nested slices/maps in
`ToolsConfig` (e.g. `ProtectedPaths`, enabled-tools map) remain shared with whatever the
UI thread may mutate via `UpdateSettings`.

**Fix.** Read `autonomous`/`autoAgents` under the lock; deep-copy (or treat as
immutable) the slice/map fields that the run will read while the UI can write. Run the
suite with `go test -race ./...` and add a race-checked test that calls `UpdateSettings`
concurrently with a stubbed run.

---

## ✅ DONE (2026-06-11) — P3 — Smaller items

- ✅ **Dead branch:** collapsed the duplicate trailing branches of
  `normalizeSSEToolArguments` (`internal/llm/stream.go`) into a single `return string(raw)`.
- ✅ **Operator precedence:** parenthesized the `&&` clause in `looksLikeBashSyntax`
  (`internal/tools/shell.go`) so the `||`/`&&` grouping is explicit.
- ✅ **Per-loop backend HTTP:** `requestContextLimit` is now a method with a per-run cache
  keyed by model load key (`a.ctxLimitKey`/`a.ctxLimitVal`), so the loaded-context query
  runs once per model instead of every agent turn. Transient (`<=0`) results are not cached,
  so a later turn can still pick up the real value. `ActualContextLength`/`/props` no longer
  hit the backend each loop.
- ✅ **Stale doc:** reworded `ISSUES.md` header so it no longer claims everything is done.

Build + tests green (`go build ./...`, `go test ./internal/{app,llm,tools}/`).

---

## What looks solid (no action needed)

- Inline tool-markup repair path (`app.go:1816-1845`) and the truncated-tool-call JSON
  validation in the SSE parser (`stream.go:154-174`) are good defenses for local models.
- Seed default is `-1` (random) everywhere (`settings/defaults.go`), so there's no
  accidental-determinism bug despite `Seed >= 0` gating the field.
- Rollback snapshot is taken before every known write/edit on the autonomous path
  (`app.go:2127-2132`) and in subagents (`subagents.go:230-234`).
- Tool-budget / turn-budget / auto-continue limits are bounded and can't spin forever.
- Streaming cancel path closes the body and issues `/inference/cancel` for llama.cpp
  (`backends/openaicompat.go:546-572`).

---

## Suggested fix order

1. ~~**P0 SSE tool-call flush** (`stream.go`)~~ — ✅ **DONE 2026-06-11** (see above).
2. ~~**P0/P1 gate secret redaction off** for pentest~~ — ✅ **DONE 2026-06-11**.
3. ~~**P1 subagent shell timeout**~~ — ✅ **DONE 2026-06-11**.
4. ~~**P2 synthesize tool-call IDs** (`stream.go`)~~ — ✅ **DONE 2026-06-11**.
5. ~~**P2 config injection into tools** (`shell.go`, `protected.go`)~~ — ✅ **DONE 2026-06-11**.
6. ~~**P2 race cleanup**~~ — ✅ **DONE 2026-06-11**. Remaining: the **P3 polish** below.

Add regression tests alongside 1–4; they're all unit-testable with SSE fixtures or
in-memory history, matching the existing `*_test.go` style.

---

## Status: ALL items (P0/P1/P2/P3) fixed as of 2026-06-11. Audit complete.
