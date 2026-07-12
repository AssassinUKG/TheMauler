# Run-review fixes (2026-06-29)

From reviewing the latest Qwen runs + Codex's terminal_run additions. Checked off as implemented. No exe rebuild - `go build` + `go test` only.

## Bugs
- [x] **1. memory.json BOM breaks every recall.** `loadMemoryJSON` (and `migrateMemoryJSONToDB` which calls it) does a raw `json.Unmarshal` that rejects the UTF-8 BOM in `~/.config/mauler/memory.json` -> `recall failed: invalid character '?'`. Strip BOM before Unmarshal in `loadMemoryJSON` + `ImportMemoryJSON`. `internal/app/memory.go`.
- [x] **2. Scan cleaning missing on interactive terminal path.** `formatTerminalScreen`/`since()` return raw scrollback - no CR-collapse / progress-strip. Since `terminal_run` is steered as the default for scans, ffuf noise reappears. Apply `tools.CollapseLineCarriageReturns` + drop `tools.IsScanProgressLine` when snapshotting. `internal/app/interactive_terminal_tool.go` (+ `terminal_scrollback.go`).

## Agentic-flow
- [x] **3 + 4. Steer to terminal_run + persistent foothold.** Interactive tools used 0x; model re-ran the full exploit chain per command. Strengthen the system-prompt/preference guidance so live/long/exploit commands go through `terminal_run`, and add a hint to establish a persistent session instead of re-exploiting per command. `internal/app/app.go` (PreferTerminalTools block ~7559) + listener-style routing.
- [x] **5. Escalate soft loop-guards to a hard stop.** (escalation already existed for identical commands; added a non-blocking one-time `foothold_nudge` for same-script re-exploitation with varying args - the real gap.) After N consecutive soft nudges (`repeated_same_tool_result`, `repeated_identical_read`) escalate to a hard stop. `internal/app/app.go` + `agent_loop_stability.go`.

## Lower-priority
- [x] **6. Non-ASCII output mangled to `?`.** (shell-side: drop low-signal `?`-art banner lines in CleanCommandOutput. read_file decode left as-is.) Preserve valid UTF-8 rather than lossy `?` replacement in command/file decode. `internal/tools/shell.go` / decode path.
- [x] **7. Heredoc repeatedly rejected.** (shared terminal now falls back to isolated `bash -lc` exec via `errSharedTerminalUnsupported` instead of rejecting - isolated handles heredocs cleanly.) Auto-rewrite simple `<<EOF` to a `printf` pipe, or give a single strong hint instead of repeated failures. `internal/app/app.go`.
- [x] **8. time_budget_exhausted with no salvage.** (already implemented: app.go:2458-2466 requests a final text-only summary turn, app.go:2819-2823 falls back to `fallbackStoppedRunSummary`. Verified it fires in the real runs.) On budget exhaustion, emit a partial-progress summary/checkpoint. `internal/app/app.go`.
