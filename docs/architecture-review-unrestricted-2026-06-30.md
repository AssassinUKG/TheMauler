# TheMauler Architecture Review - Unrestricted Agent Focus

Date: 2026-06-30

Scope: this review applies the "map first, propose after" checklist to the current TheMauler codebase. Security sandboxing, Docker/gVisor, network egress controls, and SGLang work are intentionally out of scope for now because the target product mode is a fully autonomous local agent with unrestricted access.

## 1. Architecture Map

```text
React / TypeScript UI
  frontend/src/App.tsx
  frontend/src/components/*
  frontend/src/wailsjs/go.ts
        |
        | Wails bindings + mauler:* runtime events
        v
Desktop app boundary
  main.go
  internal/app/app.go
        |
        +--> Agent orchestration
        |     internal/app/app.go:2260 runAgentLoop
        |     internal/app/agent_modes.go
        |     internal/app/tool_router.go
        |     internal/app/context_ladder.go
        |     internal/app/tasklog.go
        |     internal/app/ledger_bindings.go
        |
        +--> Prompt, history, memory, context
        |     internal/app/app.go:7396 buildSystemPrompt
        |     internal/app/app.go:3468 ensureRequestContextRoom
        |     internal/app/app.go:3574 doCompact
        |     internal/agent/history.go
        |     internal/app/memory_tool.go
        |     internal/app/progress_tool.go
        |
        +--> Tool execution
        |     internal/tools/registry.go:145 Registry.Run
        |     internal/tools/*.go
        |     internal/app/subagents.go
        |     internal/app/run_script_tool.go
        |     internal/app/read_tool_result_tool.go
        |
        +--> Inference backend
              internal/llm/client.go:125 Client
              internal/app/app.go:5340 buildClient
              internal/app/app.go:5609 buildChatRequest
              internal/llm/backends/openaicompat.go:537 Chat
              internal/llm/backends/lmstudio.go
              internal/llm/backends/llamacpp.go
```

### Layer Boundaries

UI boundary: `frontend/src/App.tsx`, `frontend/src/components/*`, and `frontend/src/wailsjs/go.ts` consume Wails methods and `mauler:*` events. This layer should stay display- and interaction-focused: chat transcript, file tree/editor, settings, Brain/Logs/Memory views, stream/tool state, and benchmark UI.

Orchestration boundary: `internal/app/app.go` currently owns most agent behavior: run lifecycle, provider setup, request construction, tool-call loop, confirmation flow, rollback/verification, compaction, ledger/task-log updates, and prompt construction. This is the highest-value refactor target because it is a large coordination layer with several domain boundaries inside one file.

Tool execution boundary: the generic tool contract is in `internal/tools/registry.go`, while many app-aware tools live under `internal/app/*_tool.go` because they need app state, memory, ledger, terminal sessions, or provider settings. `Registry.Run` is the correct execution choke point for validation and dispatch.

Inference boundary: `internal/llm/client.go` defines the provider-neutral client shape, and the OpenAI-compatible implementation lives in `internal/llm/backends/openaicompat.go`. The app still contains provider-specific decision logic in `buildClient`, `buildChatRequest`, model-loading checks, runtime protocol selection, and diagnostics.

State boundary: conversation state is `internal/agent/history.go`; task runs are `internal/app/tasklog.go`; the canonical event spine is RunLedger through `internal/ledger` and `internal/app/ledger_bindings.go`; durable memory and progress are app-level stores.

## 2. Weaknesses And Technical Debt

The main coupling problem is not UI-to-backend coupling; it is orchestration density inside `internal/app/app.go`. The file currently spans the agent loop (`runAgentLoop`), context policy (`ensureRequestContextRoom`, `doCompact`), provider construction (`buildClient`), request shaping (`buildChatRequest`), prompt assembly (`buildSystemPrompt`), model loading, terminal/session handling, diagnostics helpers, and tool loop logic. This makes every serious agent-loop change riskier than it needs to be.

Provider coupling is partially abstracted, but not finished. `internal/llm/client.go` is a good interface, and `internal/llm/backends/openaicompat.go` correctly owns request transport details such as `stream_options.include_usage`, `parallel_tool_calls`, and `parse_tool_calls`. However, app-layer code still decides provider behavior in `internal/app/app.go` around `buildClient`, `ensureModelLoaded`, `buildChatRequest`, runtime tool protocol handling, and mismatch recording. That means Qwen/LM Studio/llama.cpp compatibility details leak into orchestration.

There is no central Python event loop in the product, and there should not be one. The main loop is Go, not asyncio. The Python-related blocking risks are subprocess boundaries: `scripts/browser_agent.py` runs `asyncio.run`, and the current first-pass `internal/app/run_script_tool.go` starts `python -u` and synchronously bridges stdout JSON lines back into Go tool calls. Treat that as a temporary implementation detail. The durable architecture should keep orchestration, validation, timing, cancellation, ledger events, and programmatic tool pipelines in Go.

Tool execution is correctly centralized through `internal/tools/registry.go`, but app-level execution policy is spread through the agent loop. Confirmation, safe lists, budgets, rollback snapshots, mutation verification, output offload, guardrails, task logs, and ledger events are still interleaved in `internal/app/app.go` around the tool-call handling path. This wants a dedicated tool executor layer.

## 3. Performance Bottlenecks

TTFT and inter-token latency are not first-class run metrics yet. The streaming client in `internal/llm/backends/openaicompat.go:537` produces deltas and usage, while `internal/app/loop_metrics.go` tracks high-level loop events such as compactions and tool activity. The missing piece is per-request timing: request start, first delta, last delta, token counts, TTFT, average inter-token gap, and decode tokens/sec.

KV-cache reuse can be invalidated by prompt and tool-schema churn. Recent dynamic tool routing in `internal/app/tool_router.go` is valuable because it reduces schema bulk, but changing the tool set across turns can still change the prompt prefix. Conversational turns also appear able to send no tools when `tool_choice` is `none`, while task turns send selected schemas. The app should log a stable `system_prompt_hash`, `tool_schema_hash`, selected tool names, and request role/mode per model call so cache churn becomes visible.

Provider warmup and model load behavior is mostly guarded, but not measured end to end. `internal/app/app.go:5357 ensureModelLoaded` and OpenAI-compatible model-list calls have timeouts, and benchmark paths exist in `internal/app/benchmark.go`. The normal run loop should also record whether a model load/reload happened before a request, because TTFT is meaningless without separating cold load, warm prompt prefill, and first-token generation.

WSL/process calls can block or hang at the edges. Shell tools use contexts and timeouts in `internal/tools/shell.go`, but some utility/test paths still call `CombinedOutput` directly around WSL operations, for example `internal/tools/write_file.go` and some tests. This is visible in the current test suite behavior where WSL-dependent tests can hang independently of agent-loop code. The fix is not a security change; it is a reliability/performance timeout change.

## 4. Prompt And Memory Problems

The system prompt is not currently budgeted as a percentage of usable context. `internal/app/app.go:7396 buildSystemPrompt` assembles the main prompt packet, while memory, skills, workspace entries, tool schemas, and profile instructions can all contribute to prompt mass. Add a prompt-packet attribution step that estimates tokens by section and warns/logs when system/developer/tool schema content exceeds 20% of the profile context window.

Conversation history is no longer being blindly truncated in the old sense. `internal/agent/history.go` has compaction, tool-result clearing, and microcompaction, and the app now has progress updates plus `read_tool_result` for large offloaded outputs. The remaining issue is coordination: old tool-result markers should point the agent toward handles/chunks when possible, and `.mauler/progress.md` should be read into the prompt on run start or resume rather than only being written on drops/finishes.

Memory is safer than raw transcript stuffing. Session search, project memory, lazy skills, RunLedger, and reviewable learning candidates are already in place. The next weakness is retrieval planning: `buildSystemPrompt` should receive a small, attributed prompt packet selected from memory, progress, recent ledger signals, and session recall instead of pulling each source independently.

## 5. Planning And Tool Issues

Pydantic is not the validation layer for TheMauler. The native validation layer is JSON Schema plus Go-side argument validation in `internal/tools/registry.go`, and tests in `internal/tools/registry_test.go` cover coercion and required/nested args. For any programmatic tool pipeline, Go should remain the validation authority.

The remaining gap is a Go-native structured call builder for programmatic workflows. Replace the current Python helper boundary with a Go implementation that composes registered tool calls, validates each step through `Registry.Run`, records per-step ledger events, streams progress, and preserves cancellation/timeouts without crossing a Python subprocess boundary.

The agent has moved beyond a simple ReAct loop in pieces: it has tool routing, todo tools, progress, subagents, compaction ladder, no-tool recovery, and `run_script`. It still lacks an explicit loop-policy selector. Tasks should be classified into policies such as `DirectAnswer`, `InspectThenAct`, `PlanExecute`, `CodeAct`, `ResearchSynthesize`, and `ReviewOnly`, with the policy logged to RunLedger and used to select tools, prompt instructions, and stopping criteria.

## 6. Proposed Refactoring Steps

1. Add model-call observability first.

   Layer: inference/orchestration/ledger.

   Files: `internal/llm/client.go`, `internal/llm/backends/openaicompat.go`, `internal/app/app.go`, `internal/app/loop_metrics.go`, `internal/ledger`, `frontend/src/components/BrainPage.tsx`, `frontend/src/components/LogsPage.tsx`.

   Implement request-start, first-delta, last-delta, usage, TTFT, inter-token average, model-load flag, `system_prompt_hash`, `tool_schema_hash`, and selected tool names. This should be visible in Brain/Logs before deeper refactors, because it will prove whether later changes improve cache and latency.

   Go-only rule: collect timings at the Go streaming boundary and write Go ledger/task-log records. Do not add Python profilers, Python event loops, or Python sidecars for normal model-call observability.

2. Extract provider capabilities from app orchestration.

   Layer: inference backend.

   Files: `internal/llm/client.go`, `internal/llm/backends/openaicompat.go`, `internal/llm/backends/lmstudio.go`, `internal/llm/backends/llamacpp.go`, `internal/app/app.go`.

   Add a provider capability/profile object covering native tool parsing, reasoning support, grammar support, model-list/load support, context metadata, usage support, and recommended request flags. Then reduce app-layer provider branches in `buildClient`, `ensureModelLoaded`, and `buildChatRequest`.

3. Extract a prompt packet builder.

   Layer: prompt/memory/context.

   Files: new `internal/app/prompt_builder.go`, existing `internal/app/app.go:7396`, `internal/app/memory_tool.go`, `internal/app/progress_tool.go`, `internal/tools/skills.go`, `internal/tools/session_search.go`.

   Move system prompt construction into a component that returns sections with names, estimated tokens, hashes, and source labels. Enforce the 20% system-prompt budget warning here. Inject `.mauler/progress.md` at run start/resume as a compact continuity section.

4. Extract a context manager.

   Layer: context/history.

   Files: new `internal/app/context_manager.go`, existing `internal/app/context_ladder.go`, `internal/app/app.go:3468`, `internal/app/app.go:3574`, `internal/agent/history.go`.

   Move preflight context checks, tool-result clearing, microcompaction, summarization, and progress-drop updates behind a `ContextManager.PrepareRequest` method. This makes blind truncation regressions easier to test and prevents context policy from being tangled with provider calls.

5. Extract a tool executor.

   Layer: tool execution/orchestration.

   Files: new `internal/app/tool_executor.go`, existing `internal/app/app.go` tool-call block around `a.registry.Run`, `internal/tools/registry.go`, `internal/app/tool_result_store.go`, `internal/app/file_change_tracker.go`, `internal/agent/rollback.go`.

   Centralize tool budget checks, confirmation policy, safe-list checks, rollback snapshotting, mutation verification, linting, output offload, guardrail labeling, RunLedger events, and task-log rows. Keep `Registry.Run` as the low-level validation and dispatch layer.

6. Add an explicit loop policy selector.

   Layer: orchestration/planning.

   Files: new `internal/app/loop_policy.go`, existing `internal/app/agent_modes.go`, `internal/app/tool_router.go`, `internal/app/app.go:2260`, `internal/app/run_script_tool.go`, `internal/tools/todo.go`.

   Pick a policy per task and expose it to the prompt, router, and stop criteria. For example, `CodeAct` can prefer `run_script`, `read_many`, `grep`, `edit_file`, and `shell`; `PlanExecute` can require todo creation before mutating tools; `ReviewOnly` can suppress write/shell tools even in unrestricted mode unless the user explicitly asks for fixes.

7. Replace `run_script` with a Go-native programmatic tool worker.

   Layer: tool execution/planning.

   Files: `internal/app/run_script_tool.go`, `internal/app/run_script_tool_test.go`, `internal/tools/metadata.go`, `internal/app/tool_router.go`, and optionally new `internal/app/program_tool.go`.

   Retire the Python subprocess bridge as the default CodeAct path. Implement a Go-native mini program/execution plan format that can call registered tools, stream per-step status, enforce max steps/timeouts, and log all inner calls to RunLedger. Python should remain available only through ordinary unrestricted shell execution when the user or model explicitly needs to run a Python script as workload code.

8. Make WSL/process edge calls consistently bounded.

   Layer: tool execution/reliability.

   Files: `internal/tools/write_file.go`, `internal/tools/edit_file_test.go`, WSL helper paths in `internal/app/*`, and tests that call `CombinedOutput`.

   Replace unbounded WSL `CombinedOutput` calls with `CommandContext` and small timeouts. This avoids test/build hangs and improves responsiveness without changing the unrestricted-access product goal.

## 7. Recommended Build Order

First: Go-native observability for TTFT, token latency, prompt/tool schema hashes, model-load flags, selected tool names, and prompt-section budgets. This gives immediate visibility into whether the agent is fast, cache-friendly, and context-efficient.

Second: prompt packet builder plus context manager. This addresses the prompt/memory checklist and creates a clean place for progress resume, summary retrieval, and budget enforcement.

Third: tool executor extraction plus loop policy selector. This makes the full-auto behavior more deliberate: not just "ReAct until done", but different execution styles for build, research, review, and CodeAct tasks.

Fourth: provider capabilities. This reduces app-layer provider branching after the request/metrics shape is stable.

Fifth: Go-native programmatic tool execution and WSL/process timeout cleanup. This makes multi-step local workflows faster and prevents subprocess edges from stalling the app.
