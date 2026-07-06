# The Ultimate Local Agent Architecture (2026 Edition)

Date: 2026-06-30

Audience: senior engineering team building TheMauler, HelixClaw, and a shared local-first AI core.

Scope note: this report combines current project inspection with public documentation and research. It is intentionally architecture- and implementation-oriented. It does not modify production code.

Operator-mode note: this revision assumes the near-term product target is full unrestricted local access. The architecture should still log, verify, replay, recover, and explain actions, but it should not be centered on approval gates, network egress filtering, or sandbox-first restrictions. Those can remain optional profiles later; the primary design target here is maximum local capability and speed.

Go-native runtime note: any references in this research report to Pydantic, Python sidecars, Python-first `run_script`, or Python helper validation are reference patterns only. The implementation decision for TheMauler is Go-native: orchestration, validation, model-call observability, timing, cancellation, ledgering, prompt accounting, and programmatic tool execution should stay in Go. Python remains available as workload code through unrestricted shell/bash when a task explicitly needs it. See `docs/go-native-agent-runtime-update-2026-06-30.md`.

## 0. Code Review Protocol

Before modifying TheMauler or HelixClaw code, run a short architecture review pass:

1. Map the boundaries between UI, orchestration, tool execution, memory/logging, and inference providers.
2. Identify tight coupling between the agent loop and any provider-specific behavior.
3. Check TTFT, decode speed, tool latency, context growth, and whether static prompt/tool prefixes can be reused.
4. Check prompt and memory shape: system prompt size, memory injection size, blind truncation, stale facts, and compaction loss.
5. Check planning/tool fit: whether the task needs ReAct, Plan/Execute, Reviewer, CodeAct, or a simple direct answer.
6. Propose changes only after mapping current behavior, and reference specific files or architectural layers.

## 1. Executive Summary

The strongest local agent architecture in 2026 is not a thin chat wrapper around a model. It is a harness: a deterministic runtime that controls model calls, tool boundaries, memory selection, context compression, state recovery, evaluation, and observability.

For TheMauler and HelixClaw, the right strategic move is to share an AI Core that owns the harness and exposes domain-specific capability packs. TheMauler should specialize the shared core for unrestricted local lab operations, firmware analysis, malware research, reverse engineering, evidence handling, report generation, and terminal automation. HelixClaw should specialize it for coding, desktop automation, productivity, and long-lived project assistance.

The current TheMauler codebase is already ahead of typical local agents in several important ways:

- A real agent loop with streaming, tool calls, recovery prompts, truncation handling, tool budgets, wall-clock budgets, and model-load checks in `internal/app/app.go`.
- A typed tool registry and validation layer in `internal/tools/registry.go`.
- Durable RunLedger events in `internal/ledger/ledger.go`, task logs, memory, learning candidates, and Brain UI.
- Local-provider compatibility for LM Studio, llama.cpp, OpenAI-compatible APIs, Qwen thinking mode, usage streaming, and sequential tool calls.
- Tool-result offload and `read_tool_result`, dynamic `set_reasoning_effort`, grammar probes, mutation verification, guardrails, todo/planner tools, bounded subagents, and shared terminal integration.
- The current code already has access presets and confirmation logic; for the near-term unrestricted target, these should behave as optional UX modes rather than the organizing principle of the architecture.

The next leap is not "add more tools." It is to reduce expensive model turns, externalize state, and make the runtime more programmatic:

1. Ship `run_script` / CodeAct-style programmatic tool pipelines through the existing registry.
2. Formalize graduated compaction: offload, snip, microcompact, collapse, summarize.
3. Add `PROGRESS.md` or `.mauler/progress.md` as a model-maintained resume artifact.
4. Upgrade Doctor to assert backend launch flags, Qwen parser settings, context size, quant, and speculative decoding risk.
5. Split `internal/app/app.go` into services once behavior is test-stable: LoopRunner, ToolExecutor, ContextManager, ProviderManager, LedgerRecorder.
6. Build a first-class eval bench for coding, terminal correctness, tool recovery, evidence quality, report quality, memory correctness, and long-run resume.

Best local inference answer today:

- For your RTX 3090 and GGUF Qwen workflow: keep LM Studio or llama.cpp as the interactive default because it fits 24 GB VRAM, handles GGUF quantization, and is operationally simple.
- For throughput experiments and future shared engine work: add vLLM and SGLang provider presets as second engines. vLLM is the production serving default for batching, PagedAttention, OpenAI-compatible service, structured outputs, tool parsers, and observability. SGLang is the high-performance structured-generation and agent-serving contender.
- Do not recommend Q6_K for Qwen3.6-27B at 32K on a 24 GB card. Use UD-Q4_K_XL as the safe default for 32K. Use higher quant only when measured VRAM headroom allows it.

## 2. Current TheMauler Architecture Review

### 2.1 How It Currently Works

The desktop shell is Wails plus React. `main.go` embeds `frontend/dist` and binds `internal/app.App` methods. The frontend is a VS Code-like workbench centered around `frontend/src/App.tsx`, with panes for chat, file viewing, agent controls, logs, memory, Brain, projects, terminal, and stream output.

Backend state is concentrated in `internal/app.App`:

- Settings, profiles, history, rollback, tool registry, ledger, SQLite store, context budget, model load state, agent cancellation, terminal sessions, background jobs, and speculative decoding state live on the `App` struct in `internal/app/app.go`.
- The main loop is `runAgentLoop` in `internal/app/app.go`. It creates the system prompt, builds the backend client, loads or reuses a model, applies context budget, chooses tools, performs compaction, builds `llm.Request`, streams deltas, parses tool calls, executes tools, appends tool results, checkpoints, and finishes logs.
- Agent mode routing is in `internal/app/agent_modes.go`. It selects Auto, Manual, Ops, Builder, Fixer, Reviewer, Researcher, or Planner and applies presets.
- LLM request and stream types live in `internal/llm/client.go`. Backends are in `internal/llm/backends`.
- Built-in tools are registered in `internal/tools/registry.go`, with app-bound tools registered in `internal/app/subagents.go`.
- RunLedger events are defined in `internal/ledger/ledger.go` and are mirrored to JSONL and SQLite.
- Memory and learning candidate logic lives in `internal/app/memory.go`, `memory_tool.go`, `memory_reinject_test.go`, `memory_distill_test.go`, and `learning_candidates.go`.
- Task logs live in `internal/app/tasklog.go` and `tasklog_store.go`.
- Doctor diagnostics live in `internal/app/doctor.go`.

### 2.2 Strengths

- The harness owns the loop. This is correct. Do not outsource TheMauler's loop to a generic framework.
- Tool execution has a uniform registry, argument coercion, required-argument validation, access presets, optional confirmation, budget checks, recovery hints, guardrails, mutation snapshots, and verification.
- Context pressure is actively managed. It has old tool-result clearing, compaction, backend token-pressure handling, offload handles, and persistence nudges.
- The system is already local-model aware: thinking mode, Qwen truncation, malformed tool JSON, local endpoint model loading, LM Studio duplicate-load prevention, PowerShell/bash hints, and WSL path translation are first-class.
- The RunLedger direction is exactly right. It should become the canonical event spine for task logs, Brain, Ops, eval traces, memory extraction, evidence, and replay.

### 2.3 Weaknesses and Technical Debt

- `internal/app/app.go` is carrying too many responsibilities. The run loop, client setup, tool execution, context handling, logging, terminal integration, recovery, UI events, and provider checks are interleaved.
- Tool metadata is still too thin. `tools.Tool` exposes name, description, schema, run, and destructive only. The runtime now needs latency, output-size profile, resumability, environment requirements, background-job behavior, preferred follow-up tools, evidence semantics, and whether the tool is suitable for unrestricted autonomous use.
- Compaction is behaviorally useful but not yet a named ladder. The code has primitives, but operators and evals need stage-level telemetry.
- The current framework is excellent at direct tool calls, but it still spends too many model turns on repeated read/grep/shell loops. A programmatic tool pipeline layer is the highest payoff.
- Memory is improving, but long-term evolution needs stricter confidence, sensitivity, source evidence, expiry, target scope, and conflict resolution UX.
- The backend health story should move from "reachable" to "known-good for this profile." The Doctor should assert launch flags, parser settings, loaded context, quant, KV capacity, speculative decoding state, and tool-call support.
- Evaluation is present but not yet the product gate. Agent changes should ship only with scenario evals for turn count, recovery count, tool correctness, evidence completeness, and final answer quality.

### 2.4 Performance Bottlenecks

- Every model turn on a local 27B model is expensive. Multi-step workflows that could be executed in one local script currently cost many LLM round trips.
- Tool results can still create pressure even with offload if the model repeatedly asks for large slices or re-runs commands instead of reading handles.
- Context compaction that summarizes too late can force expensive recovery loops.
- Backend configuration errors, especially parser/template/context/quant mismatches, are corrected indirectly by loop recovery instead of prevented up front.

### 2.5 Prompt Problems

- The system prompt has accumulated many defensive instructions. That is normal for a local model harness, but it risks becoming a policy pile.
- Tool-use rules should increasingly be generated from structured runtime metadata rather than hand-authored prompt text.
- Prompt packets should be explainable from ledger events: selected memory, selected skills, active toolset, active shell profile, current lab context, current objective, current progress artifact.

### 2.6 Memory Problems

- Memory has the right shape but needs stronger governance: confidence, source, target scope, sensitivity, evidence refs, last verified, expiry, and explicit conflict events.
- Auto-distill should remain precise. For unrestricted local work, the issue is less "block learning" and more "avoid polluting durable memory." Store high-value facts with source/evidence/confidence labels and make review optional, not a hard approval dependency.

### 2.7 Planning Problems

- Todo tools exist, but long-running tasks also need externalized progress. A cold new context should resume from `.mauler/progress.md`, current todos, and ledger summary, not from raw chat.
- Subagents are bounded, which is good. The next step is to make their contracts typed and evaluable: input, allowed tools, budget, output schema, evidence refs.

## 3. Best Local Inference Backend

### 3.1 Backend Comparison

| Backend | Strengths | Weaknesses | Best use |
|---|---|---|---|
| llama.cpp | GGUF, low dependency, Windows-friendly, CUDA/Metal/Vulkan/CPU, quantization, hybrid CPU/GPU, OpenAI-compatible server, grammar support, excellent for 24 GB cards | Lower multi-user throughput than vLLM/SGLang, feature behavior shifts quickly, tool parsing depends on templates/build flags | Primary local desktop engine for Qwen GGUF |
| LM Studio | Best UX, model management, OpenAI-compatible server, local debugging, good for daily work | Less scriptable than raw servers, metadata varies, production automation weaker | Interactive daily TheMauler engine |
| vLLM | PagedAttention, continuous batching, OpenAI-compatible service, structured outputs, tool parsers including Qwen/Qwen3-Coder families, reasoning parsers, production metrics | VRAM preallocation, Python/CUDA stack complexity, GGUF not the native happy path | Shared engine throughput track and multi-agent serving |
| SGLang | Fast serving, OpenAI-compatible API, RadixAttention prefix caching, continuous batching, structured outputs, reasoning parser, Qwen tool parser, strong agent/runtime research velocity | More moving parts, less desktop-friendly than LM Studio, CUDA/Python ops burden | High-performance structured-generation and multi-agent experiments |
| TensorRT-LLM | Maximum NVIDIA optimization, FP8/INT8 paths, production inference | Heavy build/deploy complexity, weaker desktop iteration, model conversion overhead | Dedicated appliance serving, not first-line desktop work |
| exllamav2 | Excellent single-user GPTQ/EXL2 speed and memory efficiency | Less standard OpenAI/tooling story, narrower ecosystem | Speed experiments with EXL2 quant |
| Ollama | Simple packaging, Modelfile UX, local API | Less control over exact parser/template/tool behavior, less transparent for advanced Qwen agent work | Casual local model serving |
| MLX | Apple Silicon optimized | Not relevant to RTX 3090 | Mac-only HelixClaw builds |
| Text Generation Inference | Hugging Face production server, tensor parallel, observability | Less attractive than vLLM/SGLang for Qwen agent parser work | HF-centric deployments |
| Aphrodite Engine | vLLM-derived features, sampling/serving focus | Smaller ecosystem than vLLM | Specialized serving experiments |
| Jan | Desktop UX | Less agent-serving control | End-user local chat, not core harness |
| KoboldCpp | Simple GGUF serving, roleplay/community ecosystem | Tool calling and production observability are not the center | Lightweight local serving fallback |

### 3.2 Recommendation

Use a two-lane strategy:

1. Daily driver: LM Studio or llama.cpp with GGUF Qwen3.6-27B UD-Q4_K_XL, 32K context, thinking profiles, sequential tool calls, parser diagnostics, and TheMauler-owned recovery.
2. Performance lane: SGLang and vLLM provider presets for OpenAI-compatible service, Qwen reasoning parser, Qwen tool parser, structured outputs, batching, prefix-cache experiments, and eval benchmarking. SGLang deserves special attention for shared-prefix multi-agent work because RadixAttention can reuse large common prefixes across related requests.

The reportable decision: TheMauler should not bind its architecture to any one backend. The shared AI Core should treat providers as replaceable engines behind `llm.Client`, with runtime capability probes that answer:

- native tool calls available?
- reasoning content available?
- structured output available?
- JSON schema or grammar available?
- usage tokens available?
- prefix cache or KV reuse observable?
- context length actually loaded?
- speculative decoding enabled?
- quant and memory headroom acceptable?
- continuous batching useful for this run, or is single-user TTFT the governing metric?

## 4. Best Qwen Configuration

### 4.1 Baseline for Qwen3.x Thinking Mode

Official Qwen deployment docs recommend different sampling for thinking and non-thinking modes, and show thinking-mode examples with:

```toml
temperature = 0.6
top_p = 0.95
top_k = 20
presence_penalty = 0.0
max_tokens = 8192..32768
```

For non-thinking examples, Qwen docs show:

```toml
temperature = 0.7
top_p = 0.8
top_k = 20
presence_penalty = 1.5
max_tokens = 8192
```

For TheMauler on RTX 3090:

- Quant: UD-Q4_K_XL for 32K context. Avoid Q6_K at 32K on 24 GB VRAM.
- Context: 32K default. Use 64K only if the backend confirms memory headroom and quality is measured. Use YaRN/RoPE scaling only for tasks that truly need it.
- Tool calls: `parallel_tool_calls=false`.
- llama.cpp/Qwen: use the correct Jinja chat template, reasoning format, and parser settings. For vLLM, use Qwen reasoning parser where available. For SGLang, use Qwen reasoning parser and Qwen-compatible tool parser.
- Structured output: prefer native tool calling first, JSON schema/grammar second, repair-text fallback last.

### 4.2 Recommended Profiles

| Profile | Thinking | Temp | top_p | top_k | min_p | presence | max output | Notes |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| Coding | on, low/medium effort | 0.2-0.4 | 0.8-0.9 | 20 | 0.0-0.05 | 0.0 | 8192-12288 | Deterministic edits, tests, exact paths |
| Planning | on, high effort | 0.5-0.6 | 0.9-0.95 | 20 | 0.0-0.05 | 0.0 | 12288-16384 | Architecture tradeoffs |
| Autonomous agent | dynamic effort | 0.3-0.6 | 0.85-0.95 | 20 | 0.0-0.05 | 0.0 | 8192-16384 | Let `set_reasoning_effort` move low/high |
| Report writing | on or off by section | 0.6-0.75 | 0.9-0.95 | 20-40 | 0.0-0.08 | 0.0-0.5 | 12288 | Draft with thinking, final polish without |
| Cybersecurity | on, high for exploit reasoning | 0.3-0.6 | 0.9-0.95 | 20 | 0.0-0.05 | 0.0 | 12288 | Evidence-first, avoid speculative claims |
| Firmware analysis | on, medium/high | 0.2-0.5 | 0.85-0.95 | 20 | 0.0-0.05 | 0.0 | 12288 | Needs exact offsets, strings, file refs |
| Long conversations | dynamic, mostly low/medium | 0.4-0.6 | 0.9 | 20 | 0.0-0.05 | 0.0 | 8192 | Rely on retrieval, not giant prompt |
| Research | on, medium/high | 0.5-0.7 | 0.9-0.95 | 20-40 | 0.0-0.05 | 0.0 | 12288 | Cite sources, keep uncertainty labels |

Avoid mirostat and dynamic temperature until they are eval-proven for tool use. They can improve open-ended writing but add variance to tool-call reliability.

## 5. Best Agent Framework

TheMauler should not migrate to a generic framework. Its harness is the product. Use frameworks as pattern libraries.

| Framework | Score | Usefulness for TheMauler/HelixClaw |
|---|---:|---|
| LangGraph | 9 | Best external reference for durable graph state, human-in-loop, persistence, and explicit flow control. Heavy for embedding directly in Go/Rust. |
| OpenAI Agents SDK | 8.5 | Excellent reference for small primitives, handoffs, guardrails, sessions, tracing, MCP, sandbox agents. Cloud-first but architecture is strong. |
| PydanticAI | 8.5 | Best Python-native typed tools, outputs, evals, graph, providers, and testing. Great reference for `run_script` Python sidecars and eval harnesses. |
| smolagents | 8 | Strong CodeAgent/CodeAct lesson: code is a better action language for multi-step tool pipelines. |
| AutoGen | 7.5 | Mature multi-agent concepts, useful for conversation orchestration, but too much abstraction for core harness. |
| CrewAI | 7 | Productive crews/flows, memory, observability. Good for business automations, less ideal for low-level local cyber agent loop. |
| Semantic Kernel | 7 | Strong enterprise connector/planner ecosystem. More ceremony than needed. |
| LlamaIndex | 7.5 | Excellent retrieval, indexing, structured data, agentic RAG. Use ideas for memory/evidence retrieval. |
| Haystack | 7 | Strong RAG pipelines and evals, less central for autonomous desktop agents. |
| Letta | 8 | Important memory architecture reference: context as tiered memory with explicit operations. |
| Mastra | 7 | Modern TypeScript agent workflows, useful for web UI/JS ecosystem comparisons. |
| Atomic Agents | 6.5 | Clear typed-agent design, smaller ecosystem. |
| PocketFlow | 6.5 | Minimal graph workflow concepts, useful but not enough alone. |

Conclusion: build a native harness. Borrow LangGraph's durable state model, OpenAI Agents' primitive set and tracing, PydanticAI's typed validation/evals, smolagents' CodeAgent idea, and Letta's tiered memory model. Do not replace the Go/Wails runtime with LangGraph; use it as a reference pattern for explicit state transitions, checkpointing, replay, and graph-shaped long tasks.

## 6. Best Repository Designs

Repositories and products worth studying:

- Hermes Agent: feels smart because it makes reasoning effort and programmatic code/tool execution first-class runtime controls.
- OpenHands: strong event-sourced action history, workspace, browser/terminal/code interaction, and long-running task ergonomics.
- Aider: smartness comes from repo maps, precise edit formats, git discipline, and tight edit-verify loops.
- Continue.dev: strong IDE integration, context providers, model abstraction, and developer workflow fit.
- Goose: strong MCP-native local automation and provider abstraction. Its lesson is extensibility by tool servers, not necessarily adopting MCP as the internal runtime.
- Cline and Roo Code: strong VS Code tool-use UX, Plan/Act mode switching, diff/edit review, browser/terminal integration, and MCP.
- OpenCode: useful terminal-native coding-agent design and provider flexibility.
- LangGraph examples: durable state, graph-shaped orchestration, resumability.
- PydanticAI examples: typed tools, typed outputs, evals, direct Python ergonomics.
- OpenAI Agents SDK examples: handoffs, tracing, sessions, typed tools, and structured agent boundaries.

What makes these feel smarter than typical local agents:

1. They do not ask the model to remember operational state unaided.
2. They externalize workspace facts, repo maps, plans, checkpoints, logs, and progress.
3. They use constrained edit/action formats.
4. They verify after action.
5. They expose fewer, higher-leverage tools instead of many raw primitives.
6. They separate planner, executor, reviewer, memory, and UI concerns.
7. They treat terminal/browser/filesystem as stateful workspaces, not stateless commands.
8. They make errors recoverable and observable.
9. They expose plan/act mode switches so the agent can slow down for architecture and speed up for execution.
10. They make the event stream the source of truth, which enables replay, debugging, eval extraction, and long-run resume.

## 7. Best Planning Architecture

Use hybrid loops, selected per task:

| Loop | Workflow | Best use | Risk |
|---|---|---|---|
| ReAct | think, act, observe | General tool work | Turn-heavy on local models |
| Plan-and-execute | create plan, execute steps | Coding, reports, labs | Plan can stale |
| Reviewer loop | execute, review, patch | Code, report, exploit claims | Extra cost |
| Verification loop | action, test, verify | File edits, terminal work | Needs good oracles |
| Reflection/Reflexion | summarize lesson after failure | Long-term improvement | Can learn wrong lessons |
| Research loop | search, fetch, synthesize, cite | Web/current facts | Source quality matters |
| Memory loop | retrieve, use, update | Long projects | Bloat/conflicts |
| Tree/Graph of Thoughts | branch reasoning | Hard design/debugging | Too expensive for routine local runs |
| Operator checkpoint loop | pause only when the objective or environment is ambiguous | Long-running local work | Bad checkpoints become interruptions |
| CodeAct loop | generate script that calls tools | Multi-step local work | Needs timeouts, replay, and result summarization |

Recommended default:

```text
Route task -> retrieve prompt packet -> choose loop policy
  -> plan if needed
  -> execute with tools or run_script
  -> verify
  -> update progress
  -> ledger everything
  -> extract learning candidates
```

## 8. Best Memory Architecture

Use a tiered memory system:

- Core/working memory: active objective, current plan, open files, latest evidence, current terminal/session state.
- Recall/episodic memory: prior runs and their outcomes, searchable by SQLite FTS and ledger.
- Archival/semantic memory: stable facts, user preferences, project notes, reports, and research summaries.
- Procedural memory: skills, workflows, command patterns, report templates.
- Reflective memory: compact lessons from failures or successful recoveries.
- Evidence memory: artifact pointers, scan outputs, screenshots, reports, hashes, HTTP captures.

Storage:

- SQLite as the primary operational store.
- SQLite FTS for sessions, logs, evidence summaries, and memory search.
- File store for large artifacts and tool-result blobs.
- Optional Qdrant or LanceDB later for embeddings when semantic recall outgrows FTS.
- Avoid Chroma as core durable storage for this project class; it is fine for prototypes but not the central state spine.
- Use Neo4j or a graph layer only for explicit entity relationships in engagements, malware families, infrastructure, CVE chains, or project dependency maps.
- LanceDB and Qdrant are the most relevant local vector options when FTS stops being enough. Keep them behind a retrieval interface so SQLite remains the operational source of truth.

Memory should evolve over months by promotion:

```text
ledger event -> candidate -> optional review -> memory/skill/evidence pointer
  -> confidence/source/scope/sensitivity labels
  -> retrieval planner selects compact packet
  -> stale/conflict detection
  -> periodic pruning and consolidation
```

Never dump raw logs, full web pages, or whole skills into the prompt.

## 9. Best Tool Architecture

The ideal tool layer has:

- Typed schema, runtime validation, and coercion.
- Native OpenAI-compatible function calling when reliable.
- Grammar or JSON-schema constrained fallback for local backends.
- Automatic retries for classified failure modes, with bounded attempts and loop detection.
- Execution policy by tool, environment, shell profile, background-job behavior, and task mode. In unrestricted mode, policy should route and log rather than block.
- Full-access shell/script/browser/network execution as the primary mode, with optional restricted profiles available later.
- Tool result hygiene: prompt-injection labeling, secret redaction only when explicitly enabled, offload handles, and evidence pointers.
- Offload handles for large results.
- Tool metadata for risk, latency, output size, side effects, resumability, evidence value, and required environment.
- MCP client/server support at the boundary, not as the internal execution model.
- A2A support for cross-application delegation later, not inside the local single-user loop yet.
- A dynamic Tool Router that sends only the most relevant 5-10 tools for the current step while preserving a stable prefix when cache reuse matters.
- Pydantic-style typed validation for Python sidecars and `run_script` helpers, while keeping the Go registry as the canonical tool runtime.

Recommended internal interface extension:

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

Do not widen `tools.Tool` immediately. Add optional metadata via type assertion.

## 10. Best Performance Optimisations

Highest impact for local agents:

1. Reduce model turns with `run_script` and higher-level pipelines.
2. Keep tool definitions stable across turns for prefix/KV reuse where backend supports it.
3. Use offload handles for large tool results.
4. Use dynamic reasoning effort to avoid unnecessary thinking.
5. Treat speculative decoding as an opt-in performance profile until tool-call/reasoning evals prove it does not increase truncation or parser failures.
6. Prefer sequential tool calls for local Qwen unless the harness batches explicitly.
7. Use background jobs for long scans and poll compact summaries.
8. Keep WSL warm during runs.
9. Preflight backend launch flags and context.
10. Use retrieval-planned prompt packets rather than wide prompt stuffing.
11. Use SGLang RadixAttention or vLLM PagedAttention for shared-prefix, multi-agent, and batch experiments.
12. Parallelize independent deterministic tools inside the runtime or `run_script`; do not ask the model to orchestrate five identical file reads turn by turn.
13. Stream early to the UI, but keep final structured traces in the ledger for replay and eval.

vLLM/SGLang performance lane:

- benchmark TTFT, decode tok/s, tool-call parse correctness, JSON validity, long-context quality, and VRAM at 16K/32K/64K.
- measure single-user latency separately from batch throughput.
- specifically measure prefix-cache hit behavior when system prompt and tool definitions are stable.

## 11. Best Evaluation Strategy

Build evals as product gates:

- Coding: edit target file, preserve unrelated code, run tests, avoid fake completion.
- Tool use: valid JSON, correct tool choice, no unnecessary repeats, recovers from malformed args.
- Terminal: PowerShell vs WSL correctness, long job polling, no repeated command storms.
- Cybersecurity: evidence completeness, no unsupported exploit claims, correct severity, PoC verification labels.
- Firmware: binwalk/file/strings extraction, offset preservation, reportable artifacts.
- Memory: retrieves relevant prior fact, rejects conflicting stale target fact, records evidence source.
- Report quality: findings, evidence, impact, reproduction, limitations, artifacts.
- Long-run resume: kill/restart and resume from progress artifact.
- Unrestricted execution quality: shell/script/browser tools run without unnecessary prompts, while the ledger still captures exact commands, outputs, files, and recovery decisions.

Metrics:

- task success
- tool-call count
- model-turn count
- wall time
- token usage
- truncated responses
- recovery prompts
- repeated commands
- verified file mutations
- evidence refs produced
- final answer groundedness
- TTFT and inter-token latency
- prefix-cache hit rate where exposed
- tool-result offload bytes and re-read count
- completion without human intervention

Evaluation tools:

- DeepEval and RAGAS are useful for LLM-as-judge and retrieval-quality checks, but they should not be the only oracle.
- Deterministic checks should score file diffs, command exit codes, JSON validity, artifact existence, and evidence references.
- A golden dataset should include at least 100 local tasks across coding, terminal automation, research synthesis, firmware/reverse-engineering triage, report writing, and long-run resume.

CI:

- existing Go tests and frontend build remain necessary.
- add deterministic harness simulations for local-model failure modes.
- add live backend evals as opt-in nightly or manual because they depend on LM Studio/llama.cpp state.

## 12. Recent AI Research and Standards

The important 2025-2026 direction is not one new loop. It is convergence around runtime engineering:

- Agent protocols: MCP moved tool/resource/prompt integration toward a standard client/server boundary. A2A moved cross-agent delegation toward typed task/message exchange.
- Context engineering: modern guidance splits context work into write, select, compress, and isolate. TheMauler already follows this direction with memory packets, skill lazy loading, offload handles, and bounded subagents.
- Long-running agents: Anthropic-style guidance emphasizes memory, compaction, external state, and tool-result clearing. This maps directly to TheMauler's RunLedger plus the proposed progress artifact.
- Reasoning models: Qwen/DeepSeek-style thinking streams make reasoning useful but fragile under local token limits. Runtime-level effort control is now more important than static "thinking on" settings.
- CodeAct: smolagents/Hermes-style programmatic actions show that multi-step local work should often be expressed as code that calls tools, then verified by the harness.
- Memory: Letta/MemGPT-style tiered memory remains the right mental model, but production systems need evidence refs, confidence, conflict handling, and approval workflows.
- Structured generation: JSON schema, grammar constraints, and native tool parsers are table stakes for local agents. Repair-text parsing should be treated as a degraded mode.
- Evaluation: agent systems need trace-level evals, not just final-answer evals. Tool calls, retries, context drops, and memory injections are part of correctness.
- Event-sourced agents: OpenHands-style event streams are valuable because every action can be replayed, scored, resumed, and mined for learning candidates.
- Plan/Act UX: Cline/Roo-style mode switching is useful even for autonomous systems because planning and execution have different latency, context, and verification needs.

Representative dated sources are listed in the bibliography.

## 13. Shared AI Core Design

```text
Desktop UI / CLI / API
        |
Application Layer
  TheMauler: ops, evidence, lab, malware, firmware, reports
  HelixClaw: coding, productivity, desktop automation, research
        |
Shared AI Core
  Router
  LoopRunner
  PromptBuilder
  ContextManager
  ToolRouter
  ExecutionPolicyManager
  MemoryManager
  Ledger
  Evaluator
  ProviderManager
  StreamingBus
  Plugin/MCP/A2A boundary
        |
Execution Layer
  filesystem, shell profiles, browser, web, git, scripts, artifacts
        |
Model Providers
  llama.cpp, LM Studio, vLLM, SGLang, OpenAI-compatible
```

Shared layer should own:

- model request/stream abstractions
- agent loop state machine
- tool registry and metadata model
- execution policy engine, with unrestricted mode as the default operator profile
- context compaction/offload
- memory retrieval planner
- RunLedger/event schema
- eval harness
- provider probes and Doctor checks
- MCP/A2A adapters

App-specific layer should own:

- TheMauler Ops profiles, evidence bundles, listeners, target cards, exploit workflow copy, report templates.
- HelixClaw IDE/productivity tools, project assistant behavior, desktop automation profiles, coding workflows.
- UI composition and domain-specific pages.

## 14. The Mauler Blueprint

Target architecture:

```text
TheMauler UI
  Explorer | File/Chat/Ops/Brain | Agent/Tools/Terminal
      |
TheMauler Domain Services
  LabContext, Evidence, Listener, Firmware, Malware, Reports
      |
Shared AI Core
      |
Shell Profiles: PowerShell, WSL/Kali, Docker, SSH
Browser/Web/File/Git/Memory/MCP tools
```

Immediate priorities:

- `run_script` for read/grep/shell/file workflows.
- Doctor U3: launch flags, parser, quant, context, speculative decoding.
- Formal compaction ladder with stage telemetry.
- `.mauler/progress.md`.
- Tool metadata and shell profile checks.
- Ledger-backed replay view that shows prompt packets, selected memory, tool calls, recovery, and compaction stages.

TheMauler should optimize for evidence-first autonomy:

- Every exploit or vulnerability statement should have an evidence ref or be labeled hypothesis.
- Long shell output should become artifact plus summary.
- Reports should cite local artifact paths and commands.
- Agent should prefer persistent footholds and background jobs over repeated one-shot exploit loops.
- The default operator profile should be Unrestricted: full shell, filesystem, browser, network, script, and workspace access. Logging, replay, and verification should make that power legible rather than interrupting it.
- Optional restricted profiles can exist later for client work or demos, but they should not slow the primary local-lab experience.

## 15. HelixClaw Blueprint

HelixClaw should share the same AI Core but expose a calmer general-purpose assistant:

- Coding mode: repo map, edit plan, patch, tests, review.
- Productivity mode: desktop actions, files, docs, reminders.
- Research mode: web/source synthesis and local notes.
- Planning mode: project plans and task decomposition.
- Memory mode: long-term preferences, projects, procedures.

HelixClaw-specific tools should be less offensive-security oriented and more user-workflow oriented:

- IDE/git integrations
- document/spreadsheet helpers
- app automation
- task/project memory
- browser workflows
- local file organization

## 16. Implementation Roadmap

### 1 Week

| Item | Effort | Difficulty | Perf | Accuracy | Reasoning | Maintenance | Priority |
|---|---:|---:|---:|---:|---:|---:|---:|
| Doctor launch/parser/context/quant assertions | S | M | M | H | M | L | P0 |
| Name compaction stages and ledger events | S | M | M | M | M | L | P0 |
| `.mauler/progress.md` format and prompt nudge | S | M | M | H | H | M | P0 |
| Tool metadata optional interface | S | M | M | M | M | M | P1 |
| Add eval scenarios for U1/U2 | S | M | M | H | H | M | P1 |

### 1 Month

| Item | Effort | Difficulty | Perf | Accuracy | Reasoning | Maintenance | Priority |
|---|---:|---:|---:|---:|---:|---:|---:|
| `run_script` through registry | M/L | H | H | H | H | M | P0 |
| Microcompact old thinking traces | M | M | M | M | H | M | P0 |
| Shell profiles: WSL/Kali, PowerShell, Docker, SSH design | M | M | M | H | M | M | P1 |
| Replay view over ledger prompt packets | M | M | M | H | H | M | P1 |
| vLLM/SGLang live benchmark harness | M | M | H | M | M | M | P1 |

### 3 Months

| Item | Effort | Difficulty | Perf | Accuracy | Reasoning | Maintenance | Priority |
|---|---:|---:|---:|---:|---:|---:|---:|
| Split App services | L | H | M | M | M | H | P1 |
| Evaluation dashboard and golden datasets | L | H | M | H | H | M | P0 |
| Evidence graph and report artifact model | M/L | H | M | H | M | M | P1 |
| MCP server/client boundary | M | M | M | M | M | M | P2 |
| Memory schema v2 with conflicts and expiry | M | M | M | H | H | M | P1 |

### 6-12 Months

| Item | Effort | Difficulty | Perf | Accuracy | Reasoning | Maintenance | Priority |
|---|---:|---:|---:|---:|---:|---:|---:|
| Shared AI Core package used by both apps | XL | H | H | H | H | H | P0 |
| Multi-provider scheduler and model routing | L | H | H | M | M | H | P1 |
| A2A delegation between TheMauler/HelixClaw | M/L | M | M | M | M | M | P2 |
| Embedding/vector memory lane | M | M | M | M | M | M | P2 |
| Optional worktree/container execution profiles | L | H | M | H | M | M | P2 |

## 17. Top 100 Improvements Ranked by Impact

1. Add `run_script` programmatic tool calling through the registry.
2. Doctor asserts Qwen parser/template/reasoning/tool flags.
3. Doctor asserts loaded context length.
4. Doctor asserts quant and VRAM safety.
5. Doctor warns on speculative decoding for fragile tool profiles.
6. Formalize compaction ladder stages.
7. Add microcompact old thinking traces.
8. Add `.mauler/progress.md`.
9. Resume from progress plus todos plus milestone memory.
10. Add tool metadata optional interface.
11. Build ledger replay by run.
12. Show prompt packets in replay.
13. Show selected memories in replay.
14. Show compaction stages in replay.
15. Add shell profile model.
16. Add Docker shell backend.
17. Add SSH shell backend.
18. Add WSL/Kali health checks.
19. Add browser wait/navigation helpers.
20. Add source cards for web results.
21. Cache web results per run.
22. Add scan-with-progress pipeline.
23. Add exploit research packet pipeline.
24. Add firmware triage pipeline.
25. Add malware static triage pipeline.
26. Add report skeleton pipeline.
27. Add evidence graph.
28. Add artifact retention policy.
29. Add artifact sensitivity labels.
30. Add memory confidence UI filters.
31. Add memory source evidence refs.
32. Add memory expiry/staleness.
33. Add target-scope conflict warnings.
34. Add learning review queue persistence as an optional Brain workflow.
35. Route low-confidence auto-distill to review instead of blocking high-confidence local learning.
36. Add skill required-tools metadata.
37. Warn when selected skill lacks enabled tools.
38. Add lazy chunk retrieval for session logs.
39. Add lazy chunk retrieval for web pages.
40. Add lazy chunk retrieval for shell artifacts.
41. Add file outline/chunk use nudges.
42. Add eval for malformed tool JSON.
43. Add eval for repeated command storms.
44. Add eval for long-run resume.
45. Add eval for tool-result offload middle fact.
46. Add eval for Qwen thinking truncation.
47. Add eval for backend context mismatch.
48. Add eval for PowerShell/bash correction.
49. Add eval for report evidence quality.
50. Add eval for memory conflict rejection.
51. Add provider benchmark page.
52. Add TTFT/tok-s health panel.
53. Add loaded model/context/quant panel.
54. Add vLLM provider preset.
55. Add SGLang provider preset.
56. Add structured-output capability probe.
57. Add tool-parser capability probe.
58. Add reasoning-parser capability probe.
59. Add usage-token capability probe.
60. Add prefix-cache observability when available.
61. Keep tool definitions stable for cache reuse.
62. Add per-run token attribution by prompt packet.
63. Add prompt-packet budget UI.
64. Add command family grouping UI.
65. Add terminal command pinning.
66. Add AI pause/take-over control.
67. Add manual terminal-to-evidence attach.
68. Add screenshot previews in browser activity.
69. Add HTB/lab run card polish.
70. Add explicit Offline/Balanced explanations.
71. Add profile create/rename validation.
72. Add provider URL validation.
73. Add model picker from provider `/models`.
74. Add local provider firewall hints.
75. Add missing `/v1` endpoint hints.
76. Add OCR fallback plan for scanned PDFs.
77. Add binary-safe file handling and explicit binary edit tools.
78. Add full-access path UX that clearly shows workspace/root scope without blocking operator intent.
79. Add git worktree isolation option.
80. Add before/after diff evidence.
81. Add LSP diagnostics after edits.
82. Add test command detection.
83. Add codebase language profile detection.
84. Add repo map for coding mode.
85. Add dependency graph cache.
86. Add package manager command profiles.
87. Add structured reviewer output.
88. Add structured planner output.
89. Add typed subagent contracts.
90. Add subagent result grading.
91. Add subagent timeout UI.
92. Add model-turn count budget.
93. Add recovery policy table consolidation.
94. Add backend retry telemetry.
95. Add context-overflow simulation tests.
96. Split `App` into LoopRunner.
97. Split ToolExecutor service.
98. Split ContextManager service.
99. Split ProviderManager service.
100. Extract shared AI Core for HelixClaw.

## 18. Common Mistakes to Avoid

- Treating a better model as a substitute for a better harness.
- Dumping more memory into prompt instead of selecting better memory.
- Adding tools without metadata, execution policy, evals, and UI observability.
- Designing the default mode around approval prompts when the target experience is unrestricted local autonomy.
- Letting the model infer state that the runtime can store deterministically.
- Trusting web pages, tool output, or MCP tool descriptions as instructions.
- Using generic multi-agent chatter where one typed subagent or script would do.
- Letting compaction summarize critical rules instead of keeping them in system/project docs.
- Optimizing batch throughput while ignoring single-user TTFT.
- Enabling speculative decoding on tool/reasoning profiles without eval proof.
- Running a high quant/context combination because it loads once, not because it survives long tasks.

## 19. Future Trends Worth Watching

- Native reasoning controls exposed as model API parameters.
- Agentic structured output with grammar/JSON schema on every backend.
- CodeAct/programmatic tool calls becoming the default for local agents.
- MCP registry maturation and stronger permission semantics.
- A2A for cross-agent delegation between apps.
- KV cache offload/reuse becoming a standard desktop feature.
- Local multimodal agents for screenshots, PDFs, firmware UIs, and desktop tasks.
- Eval-first agent development, with trace replay generating regression cases.
- Hybrid symbolic/runtime planners that reduce model turns.
- Memory governance as a product feature, not an invisible vector DB.

## 20. Bibliography and Source Links

Primary docs and sources inspected or used:

- Qwen vLLM deployment docs, current Qwen docs, accessed 2026-06-30: https://qwen.readthedocs.io/en/latest/deployment/vllm.html
- Qwen SGLang deployment docs, current Qwen docs, accessed 2026-06-30: https://qwen.readthedocs.io/en/latest/deployment/sglang.html
- Qwen llama.cpp local docs, current Qwen docs, accessed 2026-06-30: https://qwen.readthedocs.io/en/latest/run_locally/llama.cpp.html
- vLLM tool calling docs, current latest docs, accessed 2026-06-30: https://docs.vllm.ai/en/latest/features/tool_calling/
- vLLM structured outputs docs, current latest docs, accessed 2026-06-30: https://docs.vllm.ai/en/latest/features/structured_outputs/
- llama.cpp server docs/repository, accessed 2026-06-30: https://github.com/ggml-org/llama.cpp/tree/master/tools/server
- SGLang docs, accessed 2026-06-30: https://docs.sglang.ai/
- OpenAI Agents SDK docs, current docs, accessed 2026-06-30: https://openai.github.io/openai-agents-python/
- LangGraph overview, current docs, accessed 2026-06-30: https://docs.langchain.com/oss/python/langgraph/overview
- PydanticAI docs, current docs, accessed 2026-06-30: https://pydantic.dev/docs/ai/overview/
- AutoGen docs, current docs, accessed 2026-06-30: https://microsoft.github.io/autogen/stable/
- CrewAI docs, current docs, accessed 2026-06-30: https://docs.crewai.com/
- Hugging Face smolagents docs, current docs, accessed 2026-06-30: https://huggingface.co/docs/smolagents/index
- LlamaIndex agent docs, current docs, accessed 2026-06-30: https://developers.llamaindex.ai/python/framework/understanding/agent/
- Letta docs, current docs, accessed 2026-06-30: https://docs.letta.com/
- Model Context Protocol specification, latest version 2025-11-25, accessed 2026-06-30: https://modelcontextprotocol.io/specification/
- A2A protocol specification, accessed 2026-06-30: https://a2a-protocol.org/latest/specification/
- OpenHands repository and docs, accessed 2026-06-30: https://github.com/All-Hands-AI/OpenHands
- Aider repository and docs, accessed 2026-06-30: https://github.com/Aider-AI/aider
- Continue repository and docs, accessed 2026-06-30: https://github.com/continuedev/continue
- Goose repository and docs, accessed 2026-06-30: https://github.com/block/goose
- Cline repository and docs, accessed 2026-06-30: https://github.com/cline/cline
- Roo Code repository and docs, accessed 2026-06-30: https://github.com/RooCodeInc/Roo-Code
- OpenCode repository and docs, accessed 2026-06-30: https://github.com/sst/opencode
- MemGPT paper, 2023-10: https://arxiv.org/abs/2310.08560
- Reflexion paper, 2023-03: https://arxiv.org/abs/2303.11366
- Voyager paper, 2023-05: https://arxiv.org/abs/2305.16291
- vLLM paper, 2023: https://arxiv.org/abs/2309.06180
- SGLang paper, 2023/2024 lineage, project docs accessed 2026-06-30: https://github.com/sgl-project/sglang
- OpenAI Agents SDK release/docs, 2025-2026 docs lineage, accessed 2026-06-30: https://openai.github.io/openai-agents-python/
- LangChain context engineering for agents, 2025/2026 guidance, accessed 2026-06-30: https://blog.langchain.com/context-engineering-for-agents/
- Qwen docs speed and quantization pages, accessed 2026-06-30: https://qwen.readthedocs.io/

Project-local sources reviewed:

- `AGENTS.md`
- `docs/agent-loop-upgrade-roadmap.md`
- `docs/agent-loop-upgrade-plan-2026-06.md`
- `docs/brain-memory-ledger-tracker.md`
- `internal/app/app.go`
- `internal/app/agent_modes.go`
- `internal/app/doctor.go`
- `internal/app/reasoning_effort.go`
- `internal/app/tool_result_store.go`
- `internal/app/read_tool_result_tool.go`
- `internal/tools/registry.go`
- `internal/llm/client.go`
- `internal/ledger/ledger.go`
- `internal/settings/model.go`
