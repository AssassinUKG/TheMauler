# TheMauler — project context

## What this is

TheMauler is a native Windows desktop AI agent workbench built with Go + Wails v2 + React + TypeScript.
It runs locally against Qwen3.6-27B (via LM Studio or llama.cpp OpenAI-compatible API) on an RTX 3090.
Primary target: Windows 11 native desktop, with WSL/bash available as an optional shell backend. Use the active shell's syntax: PowerShell on Windows auto mode, bash only when the shell backend is bash or WSL.

## Stack

- Go 1.26, module path `mauler`
- Wails v2.12 (desktop shell + Go↔JS bridge)
- React 19 + TypeScript + Vite + Monaco editor (frontend/)
- LLM: OpenAI-compat endpoint, default http://localhost:1234/v1 (LM Studio)
- Provider presets: LM Studio, llama.cpp, SGLang, and vLLM through OpenAI-compatible APIs
- Browser automation: chromedp-backed browser tools, plus optional browser-agent integration

## Build & run

```
wails dev          # dev mode with hot-reload — always run from repo root
wails build        # production binary → build/bin/
npm run build      # frontend only (from frontend/)
```

Never cd into subdirs before running wails commands — it must run from repo root.

## Key file map

| Path | Purpose |
|------|---------|
| `main.go` | Wails entry, embeds frontend/dist |
| `internal/app/app.go` | All Wails Go→JS bindings |
| `internal/agent/` | Streaming agent loop, history, rollback |
| `internal/llm/` | LLM client interface + SSE stream parser |
| `internal/llm/backends/` | openaicompat, llamacpp, lmstudio, anthropic stubs |
| `internal/settings/model.go` | Settings, Profile, ToolsConfig structs |
| `internal/settings/defaults.go` | DefaultSettings(), DefaultProfiles() |
| `internal/tools/registry.go` | Tool interface + Registry; defaults() registers all tools |
| `internal/app/subagents.go` | Bounded subagent tool runners for research, review, test/fix, and summarize |
| `internal/tools/browser.go` | chromedp browser tools (open/click/type/extract/screenshot) |
| `internal/tools/browser_agent.go` | browser-use AI agent tool (Python subprocess) |
| `internal/tools/shell.go` | Shell + Bash tools; shell backend detection |
| `frontend/src/App.tsx` | Root component, all mauler:* event wiring |
| `frontend/src/components/` | ChatPane, FileTree, FileViewer, AgentPanel, LogsPage, MemoryPage, StatusBar, SettingsModal |
| `scripts/browser_agent.py` | Python wrapper for browser-use (called by browser_agent tool) |

## Adding a tool

1. Create `internal/tools/my_tool.go` implementing the `Tool` interface (Name, Description, Schema, Run, Destructive)
2. Register it in `defaults()` in `internal/tools/registry.go`
3. Schema must be a valid JSON Schema object; use `additionalProperties: false`

## Settings

Config files live at `~/.config/mauler/`:
- `settings.toml` — global settings (ToolsConfig, UIConfig, etc.)
- `profiles.toml` — providers + model profiles

Settings.Load() returns *Settings. ProfilesFile.Profiles is map[string]Profile (not a slice).
`settings.context.workspace_dir` is applied by the app at startup and when saved through Settings. Changing workspace clears the active chat/tool context and rollback stack so stale paths from a previous project do not leak into the next run.

## LLM / backend conventions

- ToolCallDef fields: `tc.Function.Name`, `tc.Function.Arguments` (not tc.Name directly)
- Streaming: SSE parser in `internal/llm/stream.go`, tool call arguments accumulate across deltas
- Chat requests ask for streaming usage with `stream_options.include_usage=true`, disable `parallel_tool_calls` by default, and ask llama.cpp to `parse_tool_calls` when tools are present
- Thinking mode on by default (temp 0.6 coding, 1.0 general)
- Context compaction fires at 85% of budget (cfg.Context.CompactionAt)
- Each run's system prompt includes the authoritative current workspace root and top-level entries. Relative tool paths resolve from that root; if a read fails, tool output includes the current workspace and a glob/read hint.
- Tool results are treated as untrusted data before they re-enter model context. Prompt-injection/exfiltration language is labelled, and obvious credential assignments/private-key blocks are redacted.
- Bounded subagent tools are available as `subagent_research`, `subagent_review`, `subagent_testfix`, and `subagent_summarize`. They run with scratch history, explicit toolsets, time/tool budgets, and a fixed output contract.

## Local provider launch notes

For LM Studio, use a current 0.4.x build or newer for Qwen3.6 parser fixes. TheMauler uses `/v1/chat/completions` for chat and `/api/v1/models` / `/api/v1/models/load` for native model metadata and loading.

For llama.cpp, launch Qwen with Jinja/template support and keep server-side built-in tools disabled. TheMauler owns file/shell tools, confirmations, rollback, and task logs; llama.cpp built-ins such as `exec_shell_command`, `read_file`, `write_file`, `edit_file`, and `grep_search` can bypass those controls.

For Gemma 4, use the model's Gemma 4 chat template rather than Gemma 3's `<start_of_turn>` template. Gemma 4 native tools use special-token markup such as `<|tool_call>call:name{...}<tool_call|>`; prefer a server parser that converts this into OpenAI `tool_calls`, and keep TheMauler's repair-text fallback enabled for llama.cpp/InferenceBridge content-only streams.

Recommended Gemma 4 sampling from Google's defaults and Unsloth's Gemma 4 guide is `temperature=1.0`, `top_p=0.95`, and `top_k=64`. Keep thinking disabled for ordinary agent/tool work unless the serving stack is explicitly using the Gemma 4 reasoning parser, because raw `<|channel>thought ... <channel|>` output is easy for local runtimes to leak.

Gemma 4 QAT should be treated as a separate runtime profile from regular `Q4_K_M`/dynamic GGUF. QAT aims to recover quality lost by naive 4-bit quantization while keeping the same low-bit inference footprint. For the 26B-A4B QAT model, TheMauler tags it as `gemma4-26b-a4b-qat`, keeps tool repair enabled, and uses the exact model id reported by the serving API. The current InferenceBridge managed llama.cpp model id is `gemma-4-26B-A4B-it-QAT-Q4_0.gguf`, not the repository id, and the default launch context is a snappy 49,152 tokens for a 24GB RTX 3090. The profile follows the live `/props` sampling defaults: temperature 1.0, top_p 0.95, top_k 64, min_p 0.05, repeat_penalty 1.0. Unsloth lists 26B-A4B as the speed/quality tradeoff model with text+image support and 256K max context.

For SGLang:

```bash
python -m sglang.launch_server \
  --model-path Qwen/Qwen3.6-27B \
  --host 127.0.0.1 --port 30000 \
  --reasoning-parser qwen3 \
  --tool-call-parser qwen3_coder
```

Use provider `sglang-local` (`http://localhost:30000/v1`).

For vLLM:

```bash
vllm serve Qwen/Qwen3.6-27B \
  --host 127.0.0.1 --port 8000 \
  --reasoning-parser qwen3 \
  --tool-call-parser qwen3_coder \
  --enable-auto-tool-choice
```

Use provider `vllm-local` (`http://localhost:8000/v1`). These generic OpenAI-compatible providers do not expose TheMauler's native LM Studio/llama.cpp model-management checks, so Doctor can verify reachability but not loaded-context metadata.

For vLLM Gemma 4 26B-A4B:

```bash
vllm serve google/gemma-4-26B-A4B-it \
  --host 127.0.0.1 --port 8000 \
  --enable-auto-tool-choice \
  --tool-call-parser gemma4 \
  --reasoning-parser gemma4 \
  --chat-template examples/tool_chat_template_gemma4.jinja
```

For llama.cpp/GGUF Gemma 4 26B-A4B QAT, use a current build, launch with Jinja/template mode, and load the matching `mmproj` sidecar for vision. TheMauler should connect to the OpenAI-compatible `/v1` endpoint; do not enable llama.cpp server-side file/shell tools.

## browser-use setup (one-time)

```bash
pip install browser-use langchain-openai
playwright install chromium
```

Configure via env vars (defaults work for LM Studio):
- `BROWSER_USE_API_BASE` — default: http://localhost:1234/v1
- `BROWSER_USE_API_KEY`  — default: lm-studio
- `BROWSER_USE_MODEL`    — default: qwen3.6-27b

## Code conventions

- No comments unless the WHY is non-obvious (hidden constraint, subtle invariant, workaround)
- No docstrings or multi-line comment blocks
- No error handling for impossible cases — trust internal guarantees
- No backwards-compat shims for removed code
- Frontend: prefer editing existing components over creating new ones
- Go: standard library first; reach for external packages only when stdlib is clearly insufficient

## Profile

profile = "qwen3.6-think"

## Ignore

build/
frontend/node_modules/
frontend/dist/
*.pb.go
