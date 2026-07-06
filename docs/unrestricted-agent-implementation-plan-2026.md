# Unrestricted Local Agent Implementation Plan

Date: 2026-06-30

Source: `docs/ultimate-local-agent-architecture-2026.md`

Scope: implement the highest-impact architecture recommendations for a fully autonomous local AI workbench. This plan intentionally ignores sandbox/security-first recommendations. The default operator model is unrestricted local access, with optional prompts/profiles only when the user chooses them.

## North Star

TheMauler should become a local autonomous operator that can:

- inspect, edit, run, browse, research, script, and resume work with minimal babysitting;
- write anywhere it has OS access when the operator enables full access;
- use its own workspace/artifact folders for state, but not be confined to them;
- reduce LLM round trips by using programmatic tool pipelines;
- preserve context through offload, memory, progress files, and compaction;
- recover from local-model weirdness without stopping early;
- explain every action through logs, ledger, replay, and eval traces.
- keep the agent runtime Go-native: orchestration, validation, timing, cancellation, ledgering, prompt accounting, and tool pipelines should live in Go.

The core principle:

```text
Unrestricted execution by default.
Deterministic observability always.
Optional restrictions only as user-selected profiles.
Go owns the agent runtime.
```

## Build Order Summary

Do this order:

1. Tools and tool routing
2. Context and progress persistence
3. Agent loop upgrades
4. Doctor/provider/runtime checks
5. Evaluation harness
6. Memory/replay/Brain polish
7. Backend performance lane
8. Refactor shared AI Core

Why this order:

- Tools reduce the most model turns.
- Context fixes prevent long-run collapse.
- Loop changes make autonomy reliable.
- Doctor catches backend problems before the loop has to recover.
- Evals stop regressions.
- Refactors are safer once behavior is locked down.

## Phase 0 - Unrestricted Mode Alignment

Goal: make the architecture and UI semantics clear: unrestricted mode is first-class, not an accident.

Files:

- `internal/settings/defaults.go`
- `internal/settings/model.go`
- `frontend/src/components/AgentPanel.tsx`
- `frontend/src/components/SettingsModal.tsx`
- `internal/app/app.go`
- `internal/tools/protected.go`

Work:

1. Add or rename an access preset to `Unrestricted Operator`.
2. Make it explicit that this mode enables write, shell, browser, web, memory, Go-native tool programs, scripts through shell, and terminal.
3. Keep optional `Balanced`, `Offline`, or `Ask` profiles, but do not route default planning around them.
4. Add a clear UI label: `Full access enabled`.
5. Keep protected-path logic optional/configurable. In full access mode, it should warn or log according to settings, not silently surprise the operator.

Acceptance:

- The user can see when TheMauler is in full-access mode.
- Agent loop does not over-prompt for shell/write/browser operations in unrestricted mode.
- Logs still capture exact tool input/output.

Priority: P0

## Phase 1 - Tool Metadata and Dynamic Tool Router

Goal: stop sending a flat wall of tools. Give the runtime enough metadata to choose the right small set per step while preserving unrestricted access.

Files:

- `internal/tools/registry.go`
- `internal/settings/defaults.go`
- `internal/app/app.go`
- `internal/app/agent_modes.go`
- `frontend/src/components/AgentPanel.tsx`
- `frontend/src/components/SettingsModal.tsx`

Add optional metadata:

```go
type ToolMetadata struct {
    AccessClass       string // read, write, exec, network, browser, memory, evidence
    LatencyClass      string // instant, short, long, background
    OutputClass       string // tiny, normal, large, artifact
    RequiresNetwork   bool
    RequiresShell     string // any, powershell, wsl, docker, ssh
    Resumable         bool
    SideEffects       []string
    PreferredNext     []string
    EvidenceKind      string
    UnrestrictedReady bool
}

type MetadataProvider interface {
    Metadata() ToolMetadata
}
```

Work:

1. Add metadata interfaces without changing the existing `tools.Tool` interface.
2. Add metadata for the highest-traffic tools first: `read_file`, `read_many`, `grep`, `glob`, `shell`, `bash`, `write_file`, `edit_file`, `web_search`, `fetch_url`, browser tools, memory, todos, subagents, Go-native program tools, `read_tool_result`.
3. Add `selectToolsForTurn` before `toolDefsAndChoiceForTurn`.
4. Route tool availability by task mode and recent state:
   - coding: file, grep, edit, shell, tests, todo, memory
   - research: web, fetch, browser, read, memory, summarize
   - ops/lab: shell, terminal, http_probe, evidence_bundle, browser, file, memory
   - report: read, write, memory, evidence, browser
5. Keep a stable base prefix when possible for KV/prefix-cache reuse.
6. Surface selected toolset in Logs/Brain replay.

Acceptance:

- Typical turns expose 5-12 relevant tools, not every enabled tool.
- Unrestricted mode still allows any enabled tool when the router determines it is relevant.
- Disabled tool recovery remains available.
- Tests cover router decisions for coding, research, ops, and report tasks.

Priority: P0

## Phase 2 - Go-Native Programmatic Tool Calling

Goal: collapse multi-step local workflows into one model action.

Files:

- New or replace: `internal/app/program_tool.go`
- New or replace: `internal/app/program_tool_test.go`
- Touch: `internal/app/app.go`
- Touch: `internal/settings/defaults.go`
- Touch: `frontend/src/components/AgentPanel.tsx`

Design:

The programmatic tool should be Go-native. It lets the model submit a compact, structured execution plan that calls registered Mauler tools through the real registry:

```json
{
  "steps": [
    {"id": "files", "tool": "glob", "args": {"pattern": "internal/app/*.go"}},
    {"id": "hits", "tool": "grep", "args": {"pattern": "runAgentLoop", "path": "internal/app"}},
    {"id": "snippet", "tool": "read_file", "args": {"path": "internal/app/app.go"}},
    {"id": "note", "tool": "write_file", "args": {"path": ".mauler_artifacts/notes/loop-summary.md", "content": "..." }}
  ]
}
```

Do not add Python as an internal orchestration runtime. Python can still be run through unrestricted shell when it is the workload itself, but TheMauler's agent loop, validation, timing, cancellation, and ledger events should stay in Go.

Rules for unrestricted mode:

- The Go-native program tool is allowed without prompting.
- Inner write/shell/browser calls count against the same budgets.
- Inner calls go through the existing registry, logging, offload, mutation verification, and ledger.
- Programs have max wall time and max inner tool calls to prevent accidental infinite loops.
- Return a compact JSON summary plus stdout/stderr. Large outputs use existing tool-result offload.

Program step tools:

- `read(path)`
- `read_many(paths)`
- `glob(pattern)`
- `grep(pattern, paths=None)`
- `write(path, content, append=False)`
- `edit(path, old, new)`
- `shell(command, timeout=None, background=False)`
- `web_search(query)`
- `fetch_url(url)`
- `memory(action, query_or_content)`
- `todo(action, ...)`
- `read_tool_result(result_id, offset=0, limit=8000)`

Acceptance:

- Eval: read 8 files, grep symbols, write a summary in 1-2 model turns.
- Eval: repeated shell extraction becomes one Go-native program instead of 20 separate model/tool turns.
- Eval: malformed program JSON fails with a compact error and next-step hint.
- All inner tool calls appear in RunLedger.

Priority: P0

## Phase 3 - Context Ladder and Progress Artifact

Goal: make long tasks survive context pressure and restarts.

Files:

- `internal/agent/history.go`
- `internal/app/app.go`
- `internal/app/tool_result_store.go`
- `internal/app/read_tool_result_tool.go`
- `internal/app/run_checkpoint.go`
- New: `internal/app/progress_artifact.go`
- New: `internal/app/progress_artifact_test.go`

Context ladder:

1. Offload oversized tool results.
2. Snip old tool results while preserving assistant/tool pairing.
3. Microcompact old thinking traces.
4. Collapse middle history into a task summary.
5. Full compaction only as last resort.

Progress artifact:

Default path:

```text
.mauler/progress.md
```

Suggested format:

```markdown
# Progress

## Objective

## Current State

## Decisions

## Files Touched

## Commands / Artifacts

## Verified

## Next Steps

## Open Questions
```

Work:

1. Add a `progress` tool or app-bound helper with `read`, `update`, and `append_section`.
2. Prompt the model to update progress before compaction, before long background jobs, and at successful run end.
3. On resume, inject compact progress plus todos plus milestone memory.
4. Record progress updates to RunLedger.
5. Add UI link in Brain/Logs/Projects.

Acceptance:

- Kill a long task, restart, and agent resumes from `.mauler/progress.md`.
- Context compaction emits named ladder-stage events.
- Old thinking traces can be dropped without orphaning tool calls.

Priority: P0

## Phase 4 - Agent Loop Upgrade

Goal: make the loop more autonomous, less turn-heavy, and easier to debug.

Files:

- `internal/app/app.go`
- `internal/app/reasoning_effort.go`
- `internal/app/agent_loop_stability.go`
- `internal/app/escalation.go`
- `internal/app/loop_metrics.go`
- `internal/app/agent_eval.go`

Work:

1. Keep `set_reasoning_effort`; add evals proving it reduces truncation/auto-continue churn.
2. Add loop policy selection:
   - direct answer
   - ReAct
   - Plan/Execute
   - Reviewer
   - CodeAct via Go-native programmatic tool calls
   - Research loop
3. Add `mode -> loop policy` defaults.
4. Add operator checkpoints only for ambiguity, not permission:
   - missing target
   - conflicting objective
   - impossible environment
   - destructive ambiguity with no path/target specified
5. Consolidate recovery policy table:
   - malformed JSON
   - disabled/missing tool
   - repeated shell failure
   - repeated empty output
   - duplicate fetch/read
   - no-tool narration
   - thinking-only response
6. Add model-turn budget separate from tool-call budget.

Acceptance:

- Simple coding task finishes with fewer model turns.
- Long task maintains state through compaction.
- Recovery decisions are ledgered and visible.
- Full-access mode does not stop for ordinary shell/write/browser actions.

Priority: P0/P1

## Phase 5 - Doctor and Runtime Checks

Goal: catch backend misconfiguration before the agent loop pays for it.

Files:

- `internal/app/doctor.go`
- `internal/runtimeprofile/registry.go`
- `internal/runtimeprofile/probe.go`
- `internal/llm/backends/llamacpp.go`
- `internal/llm/backends/lmstudio.go`
- `C:\Users\richa\Documents\InferenceBridge` integration points, if launch metadata lives there.

Checks:

- provider reachable
- loaded model ID
- actual context length
- quant tag and VRAM estimate
- Qwen Jinja/template/tool parser state
- reasoning output support
- native tool-call support
- JSON schema/grammar support
- usage token reporting
- speculative decoding enabled/disabled
- prefix cache/KV reuse support if exposed
- SGLang/vLLM parser flags if those providers are added

Acceptance:

- Doctor tells the user exactly what launch flag or provider setting is wrong.
- Doctor distinguishes "works for chat" from "works for autonomous tools."
- InferenceBridge launch profile can be checked from TheMauler.

Priority: P1

## Phase 6 - Evaluation Harness

Goal: turn agent changes into measurable improvements.

Files:

- `internal/app/agent_eval.go`
- `internal/app/testdata/agent_eval/*.json`
- New: `docs/eval-suite-plan.md` if the suite gets large
- No Python harness requirement; keep eval execution Go-native unless a scenario explicitly tests Python as workload code.

Add eval categories:

- tool router selects right tools
- Go-native programmatic tool execution reduces turns
- tool-result offload middle fact is recoverable
- progress artifact resume
- compaction ladder retains objective
- Qwen thinking truncation recovery
- malformed tool JSON recovery
- repeated command storm recovery
- unrestricted shell/write/browser flow
- report evidence quality
- memory conflict rejection

Metrics:

- task success
- model turns
- tool calls
- wall time
- TTFT
- token usage
- auto-continues
- recoveries
- offload bytes
- final file/artifact existence
- command exit correctness
- completion without human intervention

Optional later:

- DeepEval/RAGAS for judge-style report quality and retrieval quality.

Acceptance:

- Every major loop/tool/context change ships with at least one eval.
- Regressions are visible before manual testing.

Priority: P1

## Phase 7 - Brain, Replay, and Memory

Goal: make unrestricted autonomy inspectable and self-improving.

Files:

- `internal/ledger/ledger.go`
- `internal/app/ledger_bindings.go`
- `internal/app/learning_candidates.go`
- `internal/app/memory.go`
- `frontend/src/components/BrainPage.tsx`
- `frontend/src/components/LogsPage.tsx`

Work:

1. Add run replay grouped by model turn:
   - prompt packets
   - selected tools
   - model response
   - tool calls/results
   - offload handles
   - compaction stage
   - recovery decision
   - progress update
2. Add selected-memory explainability.
3. Add optional learning review queue:
   - approve
   - save
   - dismiss
   - defer
   - edit
4. Add memory conflict panel.
5. Add evidence/artifact pointers as first-class Brain objects.

Acceptance:

- A failed run can be replayed and turned into an eval fixture.
- User can see why a memory entered context.
- Learning does not bloat prompts.

Priority: P1

## Phase 8 - Backend Performance Lane

Goal: benchmark and optionally add high-performance provider presets without hurting the desktop daily driver.

Files:

- `internal/settings/defaults.go`
- `internal/llm/backends/openaicompat.go`
- `internal/app/benchmark.go`
- `frontend/src/components/BenchmarkPage.tsx`
- InferenceBridge provider/launch configs

Work:

1. Add SGLang provider preset:
   - OpenAI-compatible URL
   - Qwen reasoning parser
   - Qwen tool parser
   - structured output support
2. Add vLLM provider preset:
   - OpenAI-compatible URL
   - Qwen tool parser
   - reasoning parser
3. Benchmark:
   - TTFT
   - tok/s
   - prompt prefill time
   - prefix-cache behavior
   - tool-call validity
   - JSON validity
   - VRAM
   - 16K/32K/64K context behavior
4. Compare against LM Studio/llama.cpp.

Acceptance:

- User can run a local benchmark and see which backend is best for this machine and profile.
- SGLang/vLLM are optional performance lanes, not required to use TheMauler.

Priority: P2

## Phase 9 - Shared AI Core Refactor

Goal: prepare TheMauler and HelixClaw to share one engine without destabilizing current behavior.

Do after Phases 1-7 have tests.

Extract services:

- `LoopRunner`
- `ToolExecutor`
- `ToolRouter`
- `ContextManager`
- `ProgressManager`
- `MemoryManager`
- `ProviderManager`
- `LedgerRecorder`
- `EvalRunner`

Current source anchors:

- `internal/app/app.go` -> split loop orchestration
- `internal/tools/registry.go` -> shared tool registry contracts
- `internal/llm/client.go` -> shared provider interface
- `internal/ledger/ledger.go` -> shared event schema
- `internal/settings/model.go` -> shared config structs or compatibility layer

Acceptance:

- No behavior regression.
- TheMauler still builds and runs.
- HelixClaw can consume the shared AI Core or mirror its contracts.

Priority: P2/P3

## First 10 Concrete Updates

1. Add `Unrestricted Operator` preset and UI label.
2. Add optional `ToolMetadata` interface.
3. Add metadata to core read/write/shell/web/browser/memory tools.
4. Add dynamic `selectToolsForTurn`.
5. Implement Go-native programmatic tool execution.
6. Add `.mauler/progress.md` manager/tool.
7. Add named context ladder events and microcompact old thinking traces.
8. Add loop policy selection: Direct, ReAct, Plan/Execute, Reviewer, CodeAct, Research.
9. Expand Doctor for parser/context/quant/speculative/prefix-cache checks.
10. Add eval scenarios for router, Go-native program execution, progress resume, context ladder, and unrestricted execution.

## What Not To Do First

- Do not split `app.go` before the loop/tool/context behavior is covered by evals.
- Do not replace the native Go loop with LangGraph.
- Do not add Python as TheMauler's internal agent orchestration runtime.
- Do not make MCP the internal tool runtime; use it at the boundary.
- Do not add more raw tools before metadata and routing.
- Do not tune speculative decoding as a default until tool-call evals pass.
- Do not build vector memory before SQLite/ledger/replay memory is fully explainable.
