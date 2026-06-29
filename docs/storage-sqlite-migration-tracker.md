# Storage SQLite Migration Tracker

Created: 2026-06-15

Goal: promote operational state into SQLite while keeping human-editable config as files. This should make Ops, Brain, Logs, cleanup, replay, memory review, and run filtering faster and less fragile without turning settings into an opaque database blob.

## Current Storage Map

Human-editable config/files to keep as files:

- `settings.toml` - app settings, tools, UI, shell backend, memory/logging config, toolsets.
- `profiles.toml` - providers and model profiles.
- Skill Markdown/source files - user-editable procedural knowledge.
- `inference-bridge.toml` where present - external bridge/runtime config.

Operational state currently spread across files:

- `~/.config/mauler/state.db` - SQLite session recall store.
- `~/.config/mauler/run-ledger.jsonl` - append-only RunLedger debug/export mirror; imported into SQLite on startup.
- `~/.config/mauler/task-runs.json` - legacy task run summaries/tool trails; imported into SQLite on startup when DB is empty.
- `~/.config/mauler/memory.json` - legacy durable project memory; imported into SQLite when the DB table is empty.
- `~/.config/mauler/todos.json` - legacy active todo/planner state; imported into SQLite when the DB table is empty.
- `~/.config/mauler/benchmark-runs.json` - benchmark results.
- `~/.config/mauler/runtime-lock.json` - runtime/model launch snapshot/lock state.

Confirmed current SQLite details:

- Driver: `modernc.org/sqlite` in `go.mod`, pure Go/no cgo.
- Existing schema lives in `internal/sessionstore/store.go`.
- Current tables: `sessions`, `messages`, and FTS5 virtual table `messages_fts`.
- `internal/store` now owns SQLite opening, sets `foreign_keys=ON`, attempts `journal_mode=WAL`, sets `busy_timeout=5000`, and runs `PRAGMA user_version` migrations.
- `internal/sessionstore` now routes through `internal/store` for schema/open behavior.
- `internal/tools/sqlite.go` opens read-only DBs with `mode=ro` and `busy_timeout=5000`; `immutable=1` has been removed so WAL commits remain visible.
- `internal/ledger` now supports an attached SQLite handle, mirrors `Record` calls into `ledger_events` plus file/artifact/metadata child tables while keeping `run-ledger.jsonl`, backfills existing JSONL history, and reads event lists from SQLite when attached.

## Direction

Use SQLite as the primary operational state spine. Keep JSONL as an append-only debug/export mirror where it is useful, and keep TOML/Markdown files for things users are expected to edit directly.

Target split:

- SQLite primary: task runs, ledger events, file changes, artifacts, todos, memory entries, learning decisions, approval queue, session metadata/search, workspace/recent-workspace state, replay/debug records.
- File primary: settings, profiles, skills, optional runtime bridge config.
- JSON/JSONL compatibility: keep export/import paths and optional mirrors during migration.

## Non-Negotiable Prerequisites

1. **Fix read-only SQLite tool stale reads first.**
   - Current `openSQLiteReadOnly` DSN uses `immutable=1`.
   - This can ignore locking/WAL and return stale data once live operational writes move into `state.db`.
   - Change to read-only WAL-aware access: keep `mode=ro`, drop `immutable=1`, add a busy timeout.
   - Add regression coverage proving a read-only tool sees rows committed through a separate writer connection.

2. **Introduce one shared DB owner.**
   - Do not let ledger, memory, todos, task-runs, and session recall each open independent writer pools to the same DB.
   - Create a central store package/owner around one shared `*sql.DB`.
   - Subsystems should use repository methods, for example `store.Runs()`, `store.Ledger()`, `store.Memory()`, rather than opening their own DB handles.
   - Configure once: `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`.

3. **Add schema migrations before adding lots of tables.**
   - Current `CREATE TABLE IF NOT EXISTS` schema init is fine only for first creation/additive basics.
   - Add a `PRAGMA user_version` migration runner with numbered migration steps.
   - Migration runner should be idempotent and tested from empty DB and older user_version values.

## Migration Order

1. **SQLite read safety**
   - Drop `immutable=1` from the read-only SQLite tool.
   - Add `busy_timeout`.
   - Add stale-read/WAL visibility regression test.
   - Status: done 2026-06-15. `immutable=1` removed; read-only DSN now uses `mode=ro` plus busy timeout. Regression test confirms read-only tool sees WAL-committed rows.

2. **Central store spine**
   - Add shared DB owner and repository layout.
   - Route existing sessionstore through the shared owner without behavior change.
   - Add `user_version` migration runner.
   - Status: partial 2026-06-15. `internal/store` centralizes DB open/pragmas/migrations; `App` owns one long-lived shared `*sql.DB`; session recall uses the shared handle through `sessionstore.Store`; package-level sessionstore wrappers remain for compatibility/tests. Next: move additional subsystem repositories behind the shared owner instead of opening ad hoc DB handles.

3. **Task runs to SQLite**
   - Move `task-runs.json` into tables for runs, tools, events, stop reasons, summaries, token counts, and timings.
   - Keep JSON export/import for compatibility and backup.
   - Highest early value for Logs/Ops filtering and pagination.
   - Status: done 2026-06-15 for backend migration. Schema migration v2 adds `task_runs`, `task_run_tools`, and `task_run_events`; app startup imports existing `task-runs.json` into SQLite when the DB is empty; `ListTaskRuns`, `ClearTaskRuns`, and run-finish save now use SQLite through the app's shared handle. `ExportTaskRunsJSON` and `ImportTaskRunsJSON` Wails bindings are available for compatibility/backup. Optional legacy JSON mirror can still be added later if useful.

4. **Ledger events to SQLite plus JSONL mirror**
   - Dual-write `ledger_events` to SQLite and keep `run-ledger.jsonl` as debug/export mirror.
   - Add indexes on run id, kind, source, tool, status, timestamp.
   - Derive/query file changes and artifacts from ledger events at first; consider materialized tables once query patterns settle.
   - Status: done 2026-06-15 for backend migration. Schema migration v3 adds `ledger_events`, `ledger_event_files`, `ledger_event_artifacts`, and `ledger_event_metadata`; app attaches its shared DB to RunLedger; `Record` writes JSONL plus SQLite; startup backfills existing `run-ledger.jsonl` when the DB table is empty; `ListLedgerEvents` reads from SQLite when attached; clear/prune update both storage paths.

5. **File changes and artifacts**
   - Use `file_change` events as the initial source of truth.
   - Add first-class queries for "files created by run X", "created but not deliverable", "modified existing files", and "artifact paths for report/replay".
   - Later add filesystem diffing around shell commands so files created outside `write_file`/`edit_file` are also auditable.
   - Status: first write/edit ledger slice implemented; SQLite storage planned.

6. **Todos to SQLite**
   - Move `todos.json` to a SQLite table.
   - Preserve active checklist behavior.
   - Status: first pass done 2026-06-15. Schema migration v4 adds `todos`; the app injects the shared DB into todo tools; `LoadTodos`, `SaveTodos`, and planner tool calls use SQLite; legacy `todos.json` is imported when the DB table is empty. Remaining: add workspace/run scope columns and explicit todo export/import if the UI needs them.

7. **Memory to SQLite last**
   - Move `memory.json` after the store spine is proven.
   - Preserve confidence/source/scope/tags and approval-first learning behavior.
   - Add learning decisions/rejections/defer state before migration or as part of a later learning-queue table.
   - This is correctness-sensitive because prompt injection depends on it.
   - Status: first pass done 2026-06-15. Schema migration v5 adds `memory_entries` and `memory_tags`; app startup attaches the shared DB to memory helpers and imports legacy `memory.json` only when the DB table is empty; memory list/save/delete/clear, recall, prompt injection, usage marking, and memory tool calls now use SQLite through the existing helper functions. `ExportMemoryJSON` and `ImportMemoryJSON` Wails bindings are available for backup/restore. Schema migration v6 adds `learning_decisions`; Brain approvals/dismissals are persisted and handled candidates are filtered out on future loads. Remaining: consider FTS/embedding-backed retrieval after the table shape settles.

8. **Benchmark/runtime state**
   - Evaluate whether `benchmark-runs.json` and `runtime-lock.json` should stay as files or move into SQLite.
   - Runtime lock/snapshot may remain a file if it needs simple external inspection.
   - Status: undecided.

## Schema Candidates

Initial candidate tables once the migration spine exists:

- `task_runs(id, prompt, mode, profile, model, status, state, stop_reason, stop_detail, started_at, ended_at, duration_ms, prompt_tokens, completion_tokens, total_tokens, summary, response, workspace_scope)`
- `task_run_tools(id, run_id, idx, name, input, result, status, timestamp, duration_ms)`
- `task_run_events(id, run_id, idx, kind, message, detail, timestamp)`
- `ledger_events(id, run_id, kind, source, tool, status, state, message, detail, input, output, error, duration_ms, timestamp)`
- `ledger_event_files(event_id, path)`
- `ledger_event_artifacts(event_id, path)`
- `ledger_event_metadata(event_id, key, value)`
- `todos(id, workspace_scope, run_id, text, status, detail, created_at, updated_at)`
- `memory_entries(id, scope, title, content, kind, confidence, source, importance, pinned, created_at, updated_at, last_used_at)`
- `memory_tags(memory_id, tag)`
- `learning_decisions(id, candidate_id, run_id, decision, reason, created_at)`

## Risks To Avoid

- Do not open independent writer DB pools from every subsystem.
- Do not use `immutable=1` for live app DB reads.
- Do not migrate memory first; mistakes there directly affect prompts.
- Do not remove JSON export/import until SQLite paths have been dogfooded.
- Do not dump raw ledger/tool output into prompts just because it is easier to query.
- Do not make settings/profiles opaque unless there is a separate user-editable export path.

## Immediate Next Task

Next implementation slice:

- Add workspace/run scoping to SQLite todos or defer until the workspace model stabilizes.
- Consider FTS/embedding-backed memory retrieval once the SQLite memory tables have been exercised.
- Consider whether `benchmark-runs.json` belongs in SQLite after memory is stable.
