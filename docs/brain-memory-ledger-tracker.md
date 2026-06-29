# Brain, Memory, and Ledger Tracker

Created: 2026-06-13

Goal: make TheMauler genuinely smarter over time without bloating the live prompt. The direction is a central run ledger plus layered memory, reflection, skills, evidence, and retrieval planning.

Storage note: operational state is now large enough to need a SQLite migration plan. Track database-spine work in `docs/storage-sqlite-migration-tracker.md`; keep this Brain tracker focused on what the ledger/memory system should do, not where every record is physically stored.

## Current Research Notes

- Context engineering is now framed as selecting the right information per agent step, not stuffing the largest possible prompt. LangChain describes the main strategies as write, select, compress, and isolate: https://www.langchain.com/blog/context-engineering-for-agents
- Anthropic's 2026 cookbook groups long-running-agent context control into memory, compaction, and tool-result clearing, which matches TheMauler's existing partial direction: https://platform.claude.com/cookbook/tool-use-context-engineering-context-engineering-tools
- Recent agent-memory surveys separate memory from RAG/context engineering and emphasize memory forms, functions, and dynamics rather than only short-term vs long-term buckets: https://arxiv.org/abs/2512.13564
- MemGPT/Letta-style systems treat context as tiered memory managed like an OS: short context, archival memory, and explicit memory operations: https://arxiv.org/abs/2310.08560
- Reflexion shows that compact verbal lessons from successes/failures can improve later attempts without fine-tuning: https://arxiv.org/abs/2303.11366
- Voyager shows the value of promoting repeated successful procedures into a reusable skill library instead of repeatedly relearning them: https://arxiv.org/abs/2305.16291
- Newer context-overflow work points toward memory pointers and external storage for arbitrary-length tool responses rather than raw tool-output replay: https://arxiv.org/html/2511.22729v1

## Architecture Direction

Use one central event spine:

```text
RunLedger
  -> live UI activity
  -> task-run logs
  -> Ops/engagement view
  -> memory extractor
  -> reflection extractor
  -> skill promotion
  -> evidence/artifact index
  -> retrieval planner
  -> prompt builder
```

All tool calls, model responses, state changes, confirmations, errors, terminal commands, artifacts, memory writes, and skill suggestions should record one canonical event through the ledger. Other systems should derive their views from that event stream instead of each layer writing its own separate partial log.

## Memory Layers

- **Working memory:** current task facts, active files, recent commands, open decisions. Short-lived and aggressively summarized.
- **Episodic memory:** prior runs and what happened. Stored externally and searched on demand.
- **Semantic memory:** stable facts and user preferences. Inject only high-confidence, relevant entries.
- **Procedural memory:** skills/workflows. Keep as lazy `skill_view` sources, outline/query first.
- **Reflective memory:** lessons from failures/successes, for example "do not repeat this command pattern" or "this model needs tool JSON nudges."
- **Evidence memory:** files, command output, screenshots, HTTP captures, reports, artifacts. Store pointers and summaries, not raw blobs in prompt.

## Context-Bloat Rules

- Never make raw logs, full shell output, full web pages, or whole skill sources default prompt material.
- Store bulky data externally and pass reversible pointers plus short summaries.
- Prefer outline/query/chunk tools over direct full reads.
- Run a retrieval-planning step before prompt construction: classify task, identify workspace/target, select only relevant memory/skills/evidence.
- Mark memory with confidence and source: `confirmed`, `likely`, `hypothesis`, `stale`; source can be user, tool, model inference, or previous run.
- Detect conflicts before injection, for example current target differs from remembered target.
- Keep compaction summaries stable and overwrite/update them rather than appending endless session-state memories.

## Build Plan

1. **RunLedger service**
   - Add `internal/ledger` as the canonical UTF-8 JSONL event stream.
   - Introduce a single `Record(Event)` path for run state, tool calls/results, confirmations, model errors, terminal commands, artifacts, memory events, and skill suggestions.
   - First slice is implemented: task-run events, state transitions, tool results, stops, and finishes mirror into `~/.config/mauler/run-ledger.jsonl`; Wails exposes `ListLedgerEvents` and `ClearLedgerEvents`.
   - Second slice is implemented: confirmation requests/responses, artifact start/output/error/stop/done, terminal shell start/input-size/exit/close, memory write/delete/clear, skill write/delete, and learning suggestions now record via the App ledger helper.
   - Third slice is implemented: provider ping/model listing, model load attempts/retries/reuse/failures, categorized web/browser/planner/subagent tool events, direct todo clear binding, and bounded subagent start/tool/done lifecycle now record through the ledger.
   - Next: migrate Activity/Logs/Ops reads toward the ledger, then add post-run extractors for facts, lessons, evidence pointers, and reusable procedures.

2. **Post-run extractors**
   - Extract facts, preferences, failures, artifacts, unresolved questions, and reusable procedures from the ledger after each run.
   - Store suggestions first; require user approval for sensitive or high-impact memories.
   - First slice is implemented: `ListLearningCandidates` derives reviewable skill/reflection/evidence candidates from recent ledger events, redacts sensitive snippets, and Brain shows "Learned This Run" approval cards.

3. **Memory schema upgrade**
   - Add fields for confidence, source, source_run_id, evidence_refs, workspace, target/engagement scope, last_verified_at, expires_at, and sensitivity.
   - Keep existing memory JSON migration-compatible.

4. **Retrieval planner**
   - Before prompt building, select memory by task mode, workspace, target/engagement, confidence, freshness, and user preference priority.
   - Inject short memory packets, not raw memory bodies.
   - First slice is implemented: prompt-time memory selection now builds a compact retrieval plan with task intent, query terms, per-layer slots for preferences, confirmed facts, previous-run recall, and unverified/stale hints. The same plan can add compact prior-session recall and evidence/artifact pointers without dumping raw transcripts or logs. Selected context items are logged as a `memory_retrieval_plan` run/ledger event so Brain/replay can explain why a packet entered context. Next: make Brain/replay render the selected packet layers directly.

5. **Reflection loop**
   - On repeated failure or successful recovery, create/update reflective lessons.
   - Examples: malformed tool-call pattern, terminal wrapper leak, WSL/Windows host mismatch, repeated timeout, bad model profile.

6. **Skill promotion**
   - If a procedure succeeds repeatedly, suggest or auto-draft a skill.
   - Keep skills lazy: outline first, focused query second, capped excerpt always.

7. **Brain UI**
   - Add a "Brain" or Memory+Logs combined view with tabs: Memories, Reflections, Skills, Evidence, Conflicts, Learned This Run.
   - Give the user edit/delete/approve controls for learned items.
   - First slice is implemented: a full-page Brain tab reads `ListLedgerEvents`, supports search/source/kind filters, event limits, JSON export, ledger clear, KPI counts, problem signals, selected-event expansion, and event-kind distribution.
   - Approval slice is implemented: Brain can save learning candidates as Memory entries or Skills through existing Wails bindings.

## Hermes-Inspired Capability Tracker

Added: 2026-06-14

Goal: borrow the best practical patterns from Hermes-style agent systems while keeping TheMauler local-first, WSL/pentest-friendly, and compact enough for long runs on local models.

Hard constraints for every item:
- Prompt compactness: metadata, pipelines, and debug structure feed the router, Doctor, recovery logic, and UI. They must not become default prompt bulk.
- Local-model brittleness: prefer fewer, simpler tool calls; compact outputs; explicit "unverified" wording when a fact is not confirmed.
- Reuse first: generalize the ledger, learning-candidate, auto-distill, skill, toolset, and shell/recovery machinery already shipped before adding parallel systems.

1. **Brain/Skill Curator** — planned.
   - Add a curator pass that reviews recent runs, repeated command patterns, recovered failures, useful procedures, and successful research paths.
   - Draft skills/reflections from evidence, but keep them reviewable before they become durable guidance.
   - Prefer focused procedure cards over broad prompt bloat.
   - Constraints: keep curator output as learning candidates, not automatic master-skill injection; use compact metadata and evidence pointers rather than raw prompt text.
   - Implementation note: make this the cross-run layer above `autoDistillLearnings`/`buildLearningCandidates`, not a duplicate extractor. Reuse `shellCommandFamily`/largest-family style detection for repeated command patterns.

2. **Memory approval queue** — partial foundation exists.
   - Extend "Learned This Run" into a proper queue with approve/edit/reject/defer controls.
   - Show why each memory was proposed, which run/tool evidence supports it, confidence, sensitivity, and scope.
   - Avoid silently learning secrets, flags, client data, or one-off noisy errors.
   - Constraints: approval candidates stay outside the main prompt unless selected by retrieval; sensitive or redacted items must be explicitly reviewed.
   - Implementation note: extend `ListLearningCandidates` and Brain "Learned This Run" cards. Persist accept/reject/defer as `learning_decision` ledger events so the one-spine architecture remains intact.
   - Done so far: Brain approval cards now support save, dismiss, and defer; `RecordLearningDecision` persists approved/rejected/deferred choices and records `learning_decision` ledger events; handled candidates are filtered out of the active review list.
   - Reconciliation: `autoDistillLearnings` currently saves some reflections directly. Route anything sensitive, secret-like, target-specific, or redacted through the approval queue instead of silent-save.

3. **Split memory into core facts, run recall, hypotheses, and user preferences** — partial foundation landed 2026-06-14.
   - Separate confirmed facts from model guesses and stale run leftovers.
   - Add clear UI filters and prompt packets for: stable preferences, workspace facts, engagement facts, hypotheses, lessons, evidence pointers, and previous-run recall.
   - Inject hypotheses with explicit wording so the model does not treat them as verified truth.
   - Constraints: separate packets should be short and retrieval-planned; low-confidence entries must be injected as `UNVERIFIED:` or equivalent wording.
   - Implementation note: avoid new stores at first. Upgrade `MemoryEntry` with confidence/source fields and use existing kind/scope/tags to build separate injection packets.
   - Done so far: `MemoryEntry` now has `confidence` (`confirmed`, `likely`, `hypothesis`, `stale`) and `source` (`user`, `agent`, `tool`, `model`, `previous_run`, `auto_distill`, `system`); prompt injection prefixes likely/hypothesis/stale entries as unverified/stale; Memory UI exposes both fields; memory recall includes both fields in metadata; prompt injection now emits separate compact packets for user preferences/constraints, confirmed facts/decisions, previous-run recall, and unverified/stale memories; initial prompt injection and mid-run re-injection now withhold target-specific memories when their IP/host refs conflict with the current lab target or user prompt, while keeping them available through explicit memory recall. Next: add a small Brain/Logs filter for `memory_conflict` events and make the conflict reason visible from the run replay view.

4. **Tool availability-aware skills** — planned.
   - Skills should declare required tools, shell backend, network/browser needs, and write/shell permissions.
   - The prompt builder should warn or adapt when a selected skill needs tools that the active profile/toolset cannot use.
   - This should reduce dead runs where the model follows a skill that assumes shell/write access but the current mode blocks it.
   - Constraints: requirements should guide skill selection/annotation and Doctor warnings, not add long skill metadata to the prompt.
   - Implementation note: add optional skill frontmatter such as `required_tools`, `shell_backend`, `needs_network`, and `needs_write`; cross-check with effective enabled tools/toolset during relevant-skill selection and prompt building.

5. **Richer tool registry metadata** — planned.
   - Extend tool definitions with risk, latency, output size, resumability, side effects, required environment, and preferred follow-up tools.
   - Use that metadata in Doctor, Agent panel, routing, and recovery prompts.
   - Make disabled-tool messages and toolset previews explain what is actually available.
   - Constraints: metadata feeds UI/router/recovery only; do not dump full registry metadata into model prompts.
   - Implementation note: do not widen the core `Tool` interface. Add an optional `Metadata() ToolMetadata` interface and infer defaults for tools that do not implement it. Start with fields that have immediate consumers: risk and required environment.

6. **Recover instead of stop** — partial foundation exists; recovery policy consolidation started 2026-06-14.
   - Continue expanding one-shot recovery after bad tool calls, disabled tools, malformed JSON, empty outputs, repeated failures, and backend hiccups.
   - Recovery should summarize the issue, name the safest next step, and avoid immediately repeating the same failing action.
   - Keep hard stops for user stop, explicit denial, repeated identical disabled-tool calls, and dangerous ambiguity.
   - Constraints: recovery prompts must be compact and action-oriented; local models should get one clear next move rather than a long diagnostic lecture.
   - Implementation note: consolidate scattered recovery checks into a recovery-policy table: condition -> soft hint, one-shot recovery turn, or hard stop. Reconsider which repeated-shell hard blocks should become one-turn recoveries.
   - Done so far: repeated shell failure, repeated empty shell output, and repeated identical shell result guards now route through a `preToolRecoveryRules` policy table with explicit stop reason/event mapping; disabled-tool handling now uses `evaluateDisabledToolRecoveryPolicy`; duplicate `fetch_url` skips now use `evaluateSkipRecoveryPolicy`. Next: fold malformed JSON retry and one-shot recovery reports into the same table style.

7. **Terminal backend profiles** — planned.
   - Make WSL/Kali, local PowerShell, Docker, SSH, and future remote shells first-class selectable execution profiles.
   - Each profile should expose cwd mapping, environment facts, allowed tools, latency expectations, and artifact paths.
   - Doctor should verify the selected terminal profile before long autonomous runs.
   - Constraints: profile facts should shape tool execution and Doctor checks; only a compact shell summary belongs in prompt context.
   - Implementation note: build on existing shell backend/mode/distro/user settings. A profile is a named bundle of backend, distro, user, cwd map, allowed tools, and health checks. Make WSL keepalive profile-aware.

8. **Programmatic tool pipelines** — first pipeline landed 2026-06-14.
   - Add reusable workflow tools for common multi-step operations instead of forcing the model to spam raw shell calls.
   - Candidate pipelines: scan job with progress, HTTP probe suite, web content capture, exploit research packet, evidence bundle, report skeleton.
   - Pipelines should produce compact summaries plus artifact paths, with expandable raw output in the UI.
   - Constraints: pipelines exist to reduce prompt/tool spam; return summaries and artifact paths, not giant raw output.
   - Implementation note: ship only the proven recurring pipelines first, such as HTTP probe and evidence bundle. Wire command-storm recovery hints toward these pipelines once available.
   - Done so far: added `http_probe`, a bounded shell-backed HTTP probe pipeline that runs compact curl checks through the configured shell backend, saves raw output under `.mauler_artifacts/http_probe/`, returns a summary plus artifact path, appears in toolsets/UI, and is suggested by prompt/storm hints when repeated curl probing appears. Added `evidence_bundle`, which gathers common scan/note/loot/report files into `.mauler_artifacts/evidence_bundle/` with short previews and ledger artifact pointers. Next: add a scan-with-progress pipeline or exploit research packet once the UX needs it.

9. **Trajectory replay/debug** — partial foundation exists.
   - Build a replay view over RunLedger/task-runs that shows prompt packets, selected memories, model replies, tool calls, results, stop reasons, recovery decisions, and state transitions.
   - Add filters for loops, repeated commands, empty outputs, disabled tools, context drops, backend retries, and memory injections.
   - Use replay output to generate focused regression tests and new reflection candidates.
   - Constraints: replay reads ledger/task-run data on demand; it must not increase live prompt size.
   - Implementation note: group by run ID and render the per-turn packet. Filter on event kinds such as `context_clear`, `compaction`, `command_storm`, `persist_nudge`, `memory_reinject`, `memory_distill`, `inference_retry`, and state transitions. Add a replay-to-golden-test exporter later.

Recommended build order:
1. Tool availability-aware skills plus trajectory replay over existing ledger data.
2. Memory schema upgrade, because it unlocks curator, queue, and split memory packets.
3. Recovery policy consolidation before adding more one-off recovery branches.
4. One or two proven programmatic pipelines only: HTTP probe and evidence bundle first.
5. Curator, approval queue, and split memory layers on top of the upgraded schema.
6. Terminal profiles and richer tool metadata as the next enabling layer.

Open reconciliation flags:
- Auto-distill vs approval queue: decide exactly when direct auto-save is allowed and when a candidate must go through review.
- Curator vs auto-distill: keep per-run distillation small and immediate; make the curator the cross-run generalization layer.

## Active Implementation (2026-06-14)

Concrete, code-grounded work items derived from an audit of memory.go, ledger.go, shell.go, and the app.go shell dispatch. Ordered by leverage.

1. **Unify background/job in the standalone shell tool** — DONE (2026-06-14). New `internal/tools/shell_background.go` implements a backend-agnostic detached job manager (os/exec + temp logfile + goroutine `cmd.Wait()` for real exit codes), wired through `shellParams`'s now-parsed `background`/`job` fields. Sets `WSL_UTF8=1` so wsl.exe's merged stdout/stderr stays UTF-8-decodable. Covers default shell mode and the PowerShell backend where the shared-terminal path bailed. Tests in `shell_background_test.go`.
2. **Memory tool (recall/remember)** — DONE (2026-06-14). `internal/app/memory_tool.go` registers a `memory` tool (action=recall|remember) holding an `*App` ref like `subagentTool`. recall reuses the shared `rankMemory` scorer via `searchMemory`; remember tags entries `agent` and persists through `SaveMemoryEntry` (ledgered). Added to `defaults.EnabledTools`, `coreRead`, and the `memory` toolset; system prompt nudges its use when memory is enabled. Tests in `memory_tool_test.go`.
3. **Per-turn memory re-injection** — DONE (2026-06-14). `maybeReinjectMemory` runs at the top of the agent loop (after the first tool call), re-scores memory via `searchMemory` against a window of recent non-system messages (`recentContextText`), and appends a compact note for entries that (a) textually hit a query term (`memoryHasTermHit`, not just importance/recency boosts) and (b) were not already injected. Bounded to `maxMemoryReinjections` (3) notes/run, ≤3 entries each, deduped via a per-run injected-ID set seeded from the up-front injection. Tests in `memory_reinject_test.go`.
4. **Auto-distill on run finish** — DONE (2026-06-14). `autoDistillLearnings` runs in the run-finish defer (after milestone memory), mines this run's ledger via `buildLearningCandidates`, and persists the top `maxAutoDistilledMemories` (2) reflection-class candidates (importance ≥4) as constraint memories tagged `auto`/`distilled`, deduped against existing titles in the workspace scope. Tests in `memory_distill_test.go`.
6. **WSL warm-session keepalive** — DONE (2026-06-14). `startShellKeepalive` holds the WSL2 distro VM warm for a run's duration (one detached `wsl.exe -- sleep`) so commands don't pay cold-boot latency after the VM idles out while a slow local model thinks. No-op except on Windows + WSL backend + isolated mode (shared_terminal already keeps a warm PTY). Wired into `runAgentLoop` after model-ready with a deferred stop.
7. **Auto-distill settings toggle** — DONE (2026-06-14). Added `MemoryConfig.DisableAutoDistill` (opt-out, so older configs stay default-on), gated `autoDistillLearnings`, surfaced in SettingsModal's Context tab under a new Memory section (also exposes Enabled + Auto-inject which were previously uneditable), updated the generated TS model.
8. **Jobs tab works for all agent jobs** — DONE (2026-06-14). The shared-terminal path already emitted `mauler:job_update`; the new standalone path (isolated mode / PowerShell backend) did not, so those jobs were invisible. Added a `tools.OnBackgroundJobUpdate` observer hook fired on job start/poll, wired in `OnStartup` to emit the same Wails event. Frontend: jobs no longer yank focus to the Jobs tab on every poll (only on first appearance), and done jobs show exit code instead of an empty PID.
9. **File-change ledger for cleanup/verification** — DONE (2026-06-15). `write_file` and `edit_file` now record `file_change` RunLedger events after successful verification, including created vs modified, before/after size, before/after SHA-256, verification status, files list, and a cleanup hint. Added a read-only `file_changes` tool so the agent can query its own created/modified file list before end-of-run cleanup. Tests in `file_change_tracker_test.go`.

5. **Memory scoring fixes** — DONE (2026-06-14). `containsWordish` now requires alphanumeric boundaries (kills the "cat"→"category" trap, keeps "connected"→"connected.htb"); `scoreMemory` adds a `usageBoost(LastUsedAt)` term; capacity eviction uses a combined `evictionScore` (importance + pinned + recency + usage) instead of `UpdatedAt`-only, so pinned/used facts survive. Tests in `memory_test.go`.

## Stability Hardening (2026-06-14)

9. **CI** — DONE. `.github/workflows/ci.yml` runs `go build ./...`, `go vet ./internal/...`, `go test ./... -count=1`, and frontend `tsc --noEmit` on push to main + all PRs, with in-progress-run cancellation.
10. **Background-job leak reaper + cap + sweep** — DONE. Standalone manager (`shell_background.go`): `MaxConcurrentBackgroundJobs=24` cap rejects new jobs when too many are unfinished; `reapFinishedShellJobs` frees finished-but-unpolled jobs (map entry + temp logfile); `SweepStaleJobLogs` clears orphan `mauler_job_*.log` files on startup; `ReapBackgroundShellJobs` runs at run-end. App shared-terminal manager got the same concurrency cap. Wired: sweep in `OnStartup`, reap in the run-finish defer. Tests in `shell_background_test.go`.

Known follow-ups (not yet done): split the 8k-line `app.go`; unify the two background-job implementations (pidfile-based vs `cmd.Wait()`-based); WSL-side `/tmp` orphan log sweep (current sweep only covers the Windows temp dir used by the standalone path); add filesystem diffing around shell commands so files created outside `write_file`/`edit_file` can also be audited and cleaned up safely.

## Run-Flow Fixes from Log Analysis (2026-06-14)

Derived from analysing the 2026-06-13 23:18 run (FreePBX CVE-2025-57819 SQLi): a successful exploit that stalled into 72 tool calls / 36 min / user-stopped because context-clearing kept wiping the hash chunks it was extracting, so it re-issued queries it had already answered.

1. **Preserve compact evidence + persist nudge** — `internal/agent/history.go`: `ClearOldToolResults` now skips small results matching evidence patterns (EXTRACTVALUE `~..~` leaks, password/hash/flag/private-key/`id` output) so accumulating extraction data survives clearing. `app.go` emits a one-time `persist_nudge` system message the first time context is dropped, telling the model to save findings to a file or the memory tool. Tests in `history_test.go`.
2. **Run-your-script nudge** — folded into the storm hint: when the agent wrote a `.py/.sh/.ps1/.rb/.pl` artifact, the nudge names it ("run freepbx_cve.py instead").
3. **Command-storm soft hint** — `shellCommandFamily` groups commands by binary+endpoint (query stripped); `shellCommandStormHint` appends a non-blocking nudge to script the loop once a family hits 10, then every 5. Complements the existing hard `repeatedShell*Block` guards (which only catch identical-result/failure loops, not distinct-payload extraction storms). Tests in `command_storm_test.go`.
4. **Inefficiency distillation** — `autoDistillLearnings` now also distills a "script repeated <binary> extraction" constraint when a run's largest command family ≥10, so the brain learns from slow-but-successful runs, not only failures. Reworked to not early-return on empty ledger (the storm lesson reads `run.Tools`). Tests in `memory_distill_test.go`.

Note: two pre-existing `internal/llm/backends` tests (`ActualContextLength*`) fail in the working tree (dial a hardcoded :5510 / need a live server) — unrelated to these changes, but they will make the new CI workflow red until addressed.

## Status

- Central ledger: core producer slices implemented (`internal/ledger`, task-run mirroring, confirmations, artifacts, terminal lifecycle, memory, skills, learning suggestions, provider/model diagnostics, web/browser/planner/subagent category events, bounded subagent lifecycle, file-change records for write/edit cleanup, Wails read/clear bindings).
- Existing task-run logs and state timeline: partial foundation exists.
- Existing durable memory: partial foundation exists.
- Existing session recall FTS: partial foundation exists.
- Existing lazy master skill: implemented.
- Existing tool-result clearing and compaction: partial foundation exists.
- Post-run memory/reflection extraction: first reviewable candidate pass implemented.
- Retrieval planner: first prompt-packet slice implemented for durable memory, prior-session recall pointers, and evidence/artifact pointers; Brain/replay rendering still planned.
- Brain UI: first ledger-backed inspection page implemented.
