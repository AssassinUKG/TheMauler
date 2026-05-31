# Context Management Tracker

Goal: make TheMauler stable for long-running local-agent work by keeping high-signal context, shedding bulky re-fetchable data, and preserving durable project guidance.

## Scope

- [x] Create this tracker.
- [x] Layer project instruction files (`MAULER.md`, `AGENTS.md`, overrides/fallbacks) with size caps and source markers.
- [x] Replace free-form compaction with structured summaries.
- [x] Clear stale tool results before full compaction.
- [x] Add large-file context tools for outlines and chunked reads.
- [x] Add tests and run verification.
- [x] Fix model load context vs agent working-budget separation.
- [x] Doctor first-pass polish for profile/model/context/toolset/provider mismatches.
- [x] SQLite/state inspection tools (`sqlite_schema`, `sqlite_query`).
- [x] Gemma/InferenceBridge inline tool-call diagnostics and repair hardening.
- [x] Malformed inline tool-call retry brake to prevent repeat loops.
- [ ] Memory/task-run dedicated inspection helpers.
- [ ] Structural code intelligence tools (`symbol_search`, `find_references`, Go/TS symbol indexes).
- [ ] Visual feedback loop for frontend/Wails verification.
- [ ] Structured Git helpers for commit history, branch diffs, and change provenance.
- [ ] Proactive monitoring/watchers for builds, logs, and long-running processes.

## Notes

- `master_skill.md` is intentionally not authored in this pass. The user has one to drop in later.
- The existing skill system should remain the home for procedural knowledge. The context layer should load compact indexes and retrieve details on demand.
- Tool-result clearing should preserve the fact that a tool ran and enough metadata to re-read or reproduce the result.

## Implementation Notes

- Project instructions now load from root to current workspace directory. In each directory, `MAULER.override.md` or `AGENTS.override.md` wins before fallback files.
- The fallback file list defaults to `MAULER.md, AGENTS.md` and can be edited in Settings > Context.
- Tool-result clearing runs before full compaction and keeps the most recent tool outputs intact.
- New read-only tools: `file_outline` and `read_chunks`.
- Profile `ctx_tokens` is the backend launch context. Agent preset `context_budget` is only a working/history budget and must not shrink model-load requests.
- Doctor now warns on profile/model family mismatch, offline/web blocking, and smaller agent working budgets.
- New read-only state tools: `sqlite_schema` and `sqlite_query`. Queries are limited to single SELECT/WITH statements and open databases read-only.
- Tool-call protocol diagnostics now record request protocol, structured backend `tool_calls`, repaired inline markup, and unrepaired raw markup tails in task-run events.
- Gemma/InferenceBridge compatibility repair now handles self-closing named tool calls with `parameters={...}`, `parameters="{...}"`, and escaped `parameters="{\"...\"}"` forms. Malformed-protocol retries are capped separately from normal auto-continue.

## Next Capability Tracks

1. Doctor/health checks: surface live context mismatch, offline mode blocking web tools, profile names pointing at the wrong model family, InferenceBridge content-only tool output, and preset budget vs launch context clearly.
2. State inspection: expose read-only SQLite/memory/task-run inspection without ad hoc shell queries.
3. Code intelligence: add AST/symbol/reference tools for Go and TypeScript before broader language support.
4. Visual loop: start with browser/dev screenshots for frontend verification, then investigate true desktop capture for Wails.
5. Git helpers: structured wrappers around log/diff/branch history when `.git` metadata is present.
6. Monitoring: later background watchers for builds/logs/processes with opt-in notifications.

## Verification

- `go test ./internal/agent ./internal/tools ./internal/app`
- `npm run build`
- `go test ./...`
- `go vet ./...`
- `.\build.ps1`
