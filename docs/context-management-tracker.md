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
- [x] Runtime registry skeleton for Qwen3.6/Gemma4 capability profiles.
- [x] Runtime lock snapshots for last active model/profile/backend launch.
- [x] Dedicated Benchmarks tab for profile/model tests, saved runs, comparisons, and applying recommended settings.
- [x] Context sweep benchmarking for 32k/65k/96k/current-ceiling candidates with fast/balanced/max-context scoring.
- [x] Lazy master-skill loading: register external workflow sources as compact metadata, use `skill_view` outlines and focused query excerpts instead of injecting whole files.
- [x] Shared visible terminal mode for WSL/bash shell tool calls, with terminal defaults, help, markers, and capped shell-history summaries.
- [x] Readable web fetch extraction for HTML/GitHub pages so fetched pages shed CSS/script noise before entering context.
- [x] UTF-16-ish Windows shell/terminal output decoding and ASCII lint summaries to avoid mojibake in chat/logs.
- [ ] Memory/task-run dedicated inspection helpers.
- [ ] Structural code intelligence tools (`symbol_search`, `find_references`, Go/TS symbol indexes).
- [ ] Visual feedback loop for frontend/Wails verification.
- [ ] Structured Git helpers for commit history, branch diffs, and change provenance.
- [ ] Proactive monitoring/watchers for builds, logs, and long-running processes.
- [ ] InferenceBridge raw-log integration in TheMauler logs/benchmark views for backend parser failures.
- [x] First-pass VS Code-like workspace model with separate agent root, multiple open folders, generic lab status, and user-chosen folder scaffolding.
- [ ] Recent/saved workspace files plus richer lab run cards.

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
- Runtime registry files live under `runtime/`, with built-in Go metadata in `internal/runtimeprofile` so Doctor can warn on adapter, tool-protocol, thinking, MTP, and context mismatches even before external registry loading is added.
- Successful agent runs write `runtime-lock.json` to the Mauler config directory, capturing active profile/backend/model/context/spec settings for reproducibility.
- Benchmark runs are persisted in the Mauler config directory, shown in the Benchmarks tab, compared by model/profile/context, and can apply a recommended profile back to `profiles.toml`.
- Context benchmarks now classify runs as Fast daily, Large-code, Wide-context, or Max-context, with score penalties for slow throughput, high TTFT, JSON failure, output leaks, and missing or repaired-only tools.
- Master/project workflow sources are stored as external source paths and exposed through lazy `skill_view` retrieval. Calling `skill_view master` without a query returns an outline; calling it with a focused query returns matching sections under an output cap.
- Shell tool calls can now run through the visible Terminal pane when Settings > Tools > AI shell mode is `shared_terminal` and the backend is WSL/bash. The agent sees a summarized/capped shell result while the full interactive stream stays visible to the user.
- HTML fetches now strip non-content elements and prefer main/article/body text; GitHub fetches prefer API/raw/readme content to avoid sending rendered-page chrome and CSS into context.
- InferenceBridge now has source-side safety nets for Gemma content-only fenced JSON tool calls and explicit no-thinking guidance for `enable_thinking=false` / `reasoning_effort=none`.
- InferenceBridge now strips Gemma-style `<|channel>thought ... <channel|>` markers in the shared normalizer and buffers streamed tool-request content until it can return clean OpenAI `tool_calls` or cleaned visible content.
- 2026-06-03 backend reliability follow-up: add stop-reason precedence, runtime-lock mismatch events, subagent shared-backend guardrails, one bounded pre-output inference retry, and Doctor warnings for shared one-model backends that can be mutated by subagent loads.

## Next Capability Tracks

1. Runtime foundation: keep a reproducible runtime registry (`runtime/models`, `runtime/templates`, `runtime/profiles`, `runtime/benchmarks`) plus runtime-lock snapshots for backend/model/template/profile facts. Status: started.
2. Model adapters: Qwen3.6 and Gemma4 compatibility layers for think cleanup, role normalization, stop tokens, JSON repair, and tool-call extraction. Status: started for inline tool repair; adapter package still planned.
3. Agent core state machine: make Plan -> Act -> Observe -> Reflect -> Continue an explicit loop contract, with loop protection for repeated tool calls/responses, no progress, empty output, and infinite planning. Status: partial via run state and retry brakes.
4. Tool calling: harden JSON extraction/repair/schema validation, retry invalid calls with bounded prompts, and keep strict tool schemas. Status: partial; JSON repair engine/eval suite still planned.
5. Memory system: maintain short-term, working, project, long-term, and future embedding memory layers; build prompts from relevant memory/current files/recent actions instead of dumping everything. Status: partial.
6. Router: add capability registry and automatic model/profile selection, for example Qwen for tool-heavy coding and Gemma for writing/planning when stable. Status: registry started; router planned.
7. MTP integration: detect MTP-capable artifacts, benchmark `draft-mtp` launch/profile settings, and avoid enabling MTP on normal GGUFs. Status: Doctor warnings started; benchmark UI landed; MTP-specific probes still planned.
8. Evaluation harness: add coding/tool-use/json/memory/reasoning/writing eval suites and a `mauler eval` style command/report. Status: planned.
9. UI intelligence: add model/context/VRAM/token/TPS telemetry, agent phase visualizer, and richer tool timeline. Status: partial via status bar/logs; telemetry planned.
10. Frontier layer: add planner/worker/critic/consensus flows for critical tasks. Status: bounded subagents started; orchestration planned.

## Steps Left

1. Load external runtime registry JSON from `runtime/` and merge it over built-in defaults.
2. Add first-class Qwen/Gemma adapter package around response normalization and tool-call repair.
3. Extend the Benchmarks tab with live InferenceBridge/LM Studio telemetry capture, backend raw-output links, VRAM/KV fit snapshots, and MTP-specific probes.
4. Add MTP launch validation against live backend props and selected model artifact name.
5. Continue generalizing lazy/capped retrieval to session and document outputs so bulky re-fetchable data does not live in chat history.
6. Add JSON repair engine with schema validation metrics and failing examples in task logs.
7. Add explicit Plan/Act/Observe/Reflect loop events and UI phase visualizer.
8. Add capability router for automatic profile selection per task mode.
9. Add eval harness for tool-call validity, JSON validity, loop count, latency, and task success.
10. Add richer runtime telemetry from InferenceBridge/llama.cpp/LM Studio where available.
11. Build planner/worker/critic orchestration on top of bounded subagents.
12. Continue the workspace redesign from `docs/workspace-redesign-plan.md` with recent/saved workspace files, workspace switcher, and richer lab run cards.
13. Add terminal take-over controls, command artifact pinning, and long-running command progress cards on top of shared terminal mode.

## Verification

- `go test ./internal/agent ./internal/tools ./internal/app`
- `npm run build`
- `go test ./...`
- `go vet ./...`
- `.\build.ps1`
