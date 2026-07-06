# Go-Native Agent Runtime Update

Date: 2026-06-30

This note supersedes any older roadmap text that suggests adding Python, Pydantic, Python sidecars, or a Python event loop as TheMauler's internal agent orchestration layer.

## Decision

Keep TheMauler's agent runtime Go-native.

Go owns:

- agent orchestration and loop policy;
- model-call observability;
- TTFT and inter-token timing;
- prompt and tool-schema hashing;
- prompt-section token budgets;
- tool argument validation through JSON Schema plus Go checks;
- programmatic tool execution over the existing registry;
- cancellation and timeout handling;
- RunLedger/task-log events;
- context ladder and progress/resume state.

Python remains available only as workload code through unrestricted `shell` or `bash` calls when a task explicitly needs to run Python scripts. It should not become the app's internal planning, validation, tracing, or tool-pipeline runtime.

## Next Build Slice

Build observability first, in Go:

1. Capture request start, first streamed delta, last streamed delta, completion, and usage.
2. Compute TTFT, total stream duration, average inter-token gap, and decode tokens/sec when usage is available.
3. Record whether model loading/reloading happened before the request.
4. Hash the system prompt and selected tool schema for each model call.
5. Record selected tool names and tool count.
6. Estimate prompt sections and flag when system/developer/tool-schema content exceeds 20% of the usable context window.
7. Write all of the above to RunLedger and task logs, then expose it in Brain/Logs.

Primary files:

- `internal/llm/client.go`
- `internal/llm/backends/openaicompat.go`
- `internal/app/app.go`
- `internal/app/loop_metrics.go`
- `internal/ledger`
- `frontend/src/components/BrainPage.tsx`
- `frontend/src/components/LogsPage.tsx`

## Programmatic Tool Direction

Replace the current Python-first `run_script` concept with a Go-native structured program tool:

```json
{
  "steps": [
    {"id": "files", "tool": "glob", "args": {"pattern": "internal/app/*.go"}},
    {"id": "hits", "tool": "grep", "args": {"pattern": "runAgentLoop", "path": "internal/app"}},
    {"id": "note", "tool": "write_file", "args": {"path": ".mauler_artifacts/notes/summary.md", "content": "..."}}
  ],
  "timeout_seconds": 60,
  "max_steps": 20
}
```

Each step must route through the existing Go registry so full-access behavior, tool budgets, rollback snapshots, mutation verification, output offload, and ledger events remain consistent with direct tool calls.

