# TheMauler - Agent Handoff Document

Read this file at the start of every session. It is the single source of truth for the project state, architecture, and what to build next.

---

## What is TheMauler?

A native Windows desktop AI agent workbench, roughly VS Code meets Claude Code. Built with:

- Go 1.26 backend for AI logic, tools, and file operations
- Wails v2.12 desktop shell with Chromium WebView2
- React + TypeScript + Vite frontend
- Monaco editor for the file viewer/editor

The user has an RTX 3090 with 24 GB VRAM and runs Qwen3.6-27B locally via LM Studio or llama.cpp using an OpenAI-compatible API. Never recommend Q6_K quant; it OOMs at 32K context on 24 GB. Recommend UD-Q4_K_XL, which is about 17 GB model plus about 4 GB KV and is safe.

---

## Run Commands

All commands should be run from project root: `C:\Users\richa\Desktop\TheMauler`.

```powershell
# Development: hot reload and desktop window
wails dev

# Production build: build/bin/TheMauler.exe
.\build.ps1

# Production build and launch
.\build.ps1 -Run

# Frontend only
cd frontend
npm run build

# Go checks
go test ./...
go vet ./...
```

Linux/WSL:

```bash
# Development: hot reload and desktop window
wails dev

# Production build: build/bin/TheMauler
./build.sh

# Production build and launch
./build.sh --run

# Skip Go tests/vet when iterating
./build.sh --skip-tests

# Skip dependency bootstrap
./build.sh --skip-deps
```

---

## Project Structure

```text
TheMauler/
|-- main.go                          # Wails entry point, embeds frontend/dist
|-- wails.json                       # Wails project config
|-- go.mod                           # module mauler, go 1.26
|-- PLAN.md                          # Original full build plan
|-- AGENTS.md                        # This file
|-- frontend/
|   |-- index.html
|   |-- vite.config.ts
|   |-- src/
|       |-- main.tsx
|       |-- App.tsx                  # Root component, mauler:* event wiring
|       |-- App.css
|       |-- index.css                # Global VSCode-dark theme variables
|       |-- wailsjs/
|       |   |-- go.ts                # Type-safe wrappers for Go bindings
|       |   |-- runtime.ts           # Wails EventsOn shim
|       |-- components/
|           |-- ChatPane.tsx/css     # Streaming markdown chat, image paste
|           |-- FileTree.tsx/css     # Recursive file tree, workspace navigation
|           |-- FileViewer.tsx/css   # Center-pane Monaco file viewer/editor
|           |-- AgentPanel.tsx/css   # Right-pane agent controls and tool access
|           |-- StatusBar.tsx/css    # Token bar + profile switcher
|           |-- ConfirmDialog.tsx/css
|           |-- SettingsModal.tsx/css
|-- internal/
    |-- app/
    |   |-- app.go                   # All Wails bindings - main backend
    |-- agent/
    |   |-- history.go               # Conversation history + compaction
    |   |-- rollback.go              # File rollback stack
    |-- llm/
    |   |-- client.go                # Client interface and request types
    |   |-- stream.go                # SSE parser + tool call accumulator
    |   |-- backends/
    |       |-- openaicompat.go
    |       |-- llamacpp.go
    |       |-- lmstudio.go
|       |-- anthropic.go         # Legacy stub; app is local-provider focused
    |-- settings/
    |   |-- model.go                 # TOML + JSON tags for Wails/settings UI
    |   |-- defaults.go
    |   |-- load.go
    |   |-- save.go
    |-- tools/
        |-- registry.go
        |-- read_file.go
        |-- write_file.go
        |-- edit_file.go
        |-- shell.go                 # Platform-aware shell tool plus bash alias
        |-- paths.go                 # Windows/WSL/Linux path normalization
        |-- glob.go
        |-- grep.go
```

---

## Critical API Facts

### Settings / Profiles

```go
cfg, _ := settings.Load()              // returns *Settings
profiles, _ := settings.LoadProfiles() // returns *ProfilesFile
settings.Save(cfg)                     // takes *Settings
settings.SaveProfiles(profiles)        // takes *ProfilesFile

profile := profiles.Profiles["qwen3.6-think"] // map, not slice
cfg.Context.CompactionAt
cfg.Context.MAULERMDPath
```

The settings structs have both TOML and JSON tags. TOML stays snake_case on disk, and Wails now returns snake_case JSON fields to React, for example `active_profile`, `provider`, `base_url`, `thinking_general`, and `nothinking`.

Providers and profiles are intentionally separate:

- Provider = endpoint/backend transport, for example LM Studio at `http://100.112.166.73:1234/v1`.
- Profile = model behaviour, for example model id, context tokens, thinking mode, and sampling params.

`profiles.toml` now has both `[providers]` and `[profiles]`. Old profile-level `backend` and `base_url` fields are migrated on load into provider entries.
Provider-only legacy profiles such as `lmstudio-default` are intentionally removed from `[profiles]`; LM Studio belongs under `[providers]` only.

### Tool Calls

```go
tc.Function.Name
tc.Function.Arguments
tc.ID
```

### LLM Request

```go
req := llm.Request{
    Messages:         msgs,
    Tools:            toolDefs,
    MaxTokens:        params.MaxTokens,
    Temperature:      params.Temperature,
    TopP:             params.TopP,
    TopK:             params.TopK,
    MinP:             params.MinP,
    PresencePenalty:  params.PresencePenalty,
    EnableThinking:   profile.Thinking,
    PreserveThinking: profile.PreserveThink,
}
```

Model is baked into the client at construction. There is no `Params` sub-struct and no `Model` field in `llm.Request`.

### Tool Result Messages

```go
llm.Message{
    Role:       llm.RoleTool,
    Content:    result,
    ToolCallID: tc.ID,
    Name:       tc.Function.Name,
}
```

### Wails Events

Go emits:

```go
mauler:stream_start
mauler:delta
mauler:stream_done
mauler:stream_error
mauler:tool_call
mauler:tool_result
mauler:confirm
mauler:compact
mauler:artifact_output
mauler:artifact_done
```

TypeScript listens via `EventsOn('mauler:event_name', (...args: unknown[]) => {})`.

---

## What is Already Working

- Full Wails v2 desktop app shell
- Workbench layout: Explorer | center tabs | Agent controls
- Center tabs: Chat and File viewer/editor
- File viewer is a real Monaco editor surface with syntax language selector, minimap, formatting, Ctrl+S save, and status bar
- Streaming agent loop from SSE to Wails events to React state
- Tool calls: `read_file`, `read_many`, `read_pdf`, `write_file`, `edit_file`, `shell`, `bash` alias, `glob`, `grep`, `session_search`, planner/todo tools, skill tools, bounded subagent tools, `web_search`, `fetch_url`, and browser tools
- PDF text extraction is available through the read-only `read_pdf` tool with optional page ranges and output limits. It is for text-based PDFs; scanned/image-only PDFs still need OCR later.
- Shell backend is configurable as `auto`, `powershell`, `cmd`, `bash`, or `wsl`. Auto uses PowerShell on Windows and bash on Linux/WSL.
- File tools normalize common path forms between Windows and WSL, such as `/mnt/c/...`, `/c/...`, and `C:\...`.
- Workspace selection is authoritative for tools and prompts. `SetWorkingDir` and Settings `workspace_dir` both apply the process cwd, persist the normalized path, and clear the active chat/tool context plus rollback stack when the project changes. New runs include the current workspace root and top-level entries in the system prompt; missing-file tool results include the current workspace and a glob/read hint so stale paths from another project do not keep looping.
- LM Studio profiles check `/api/v1/models` for a matching loaded instance before loading. The check is intentionally tolerant of missing context metadata and treats an already-loaded target as usable to avoid LM Studio creating duplicate `:2` instances. If a matching target is loaded with a clearly too-small context, TheMauler unloads loaded models before reloading the requested profile/context. Chat uses `/v1/chat/completions` for custom tools.
- OpenAI-compatible non-streaming model-list calls have a 10 second timeout even though streaming chat uses an unbounded HTTP client timeout.
- Chat requests copy active profile generation params: max tokens, temperature, top-p, top-k, min-p, presence penalty, seed, and thinking flags
- Confirm gate for destructive tools
- Tool confirmations can be allowed once or added to an exact-input safe list. Safe-listed tool approvals skip future prompts and can be removed in Settings > Tools.
- Tools show informational risk labels in Agent > Tools and Settings > Tools. Low: read/search-local tools; medium: web/browser read tools; high: shell/write/edit/browser interaction. Labels do not restrict autonomous mode; enabled tools remain available.
- File rollback stack through `Undo`
- Mutating agent tools are snapshotted before execution and verified after success. `write_file` verification checks that the target file exists and content/append suffix matches the tool input; `edit_file` verification checks that `new_string` landed. Language-specific lint still runs afterward for Go, Python, and shell files.
- Tool results pass through promptware and secret-exfiltration guardrails before being appended back into model history. Suspicious prompt-injection/exfiltration language is labelled as untrusted data, and obvious credential assignments/private-key blocks are redacted.
- Context compaction at 85 percent
- Settings modal with **eight tabs** (general, providers, profiles, agents, tools, context, ui, image), live editing, and TOML persistence
- Profile switcher in status bar
- Token usage bar in status bar
- Status bar backend ping skips polling during streams without restarting the interval when streaming flips on/off.
- Image paste in chat
- Sent user image attachments remain visible as clickable thumbnails in the chat transcript and are preserved when sessions are loaded
- Chat drafts remain editable while the agent is running. Enter inserts a newline; Ctrl+Enter or the Send button sends.
- Sending while the agent is running interrupts the current run and sends exactly one pending draft after the stop completes. There is no FIFO queue replay.
- Auto-continue waits 500 ms between continuation retries to avoid hammering a local backend when the model repeatedly stops mid-task.
- Qwen/LM Studio truncation handling: OpenAI-compatible SSE `finish_reason:"length"` sets `Delta.Truncated`; the agent auto-continues even when the text ends cleanly. If the tail says it is about to act (for example "right - let me write..."), it uses a directive prompt that requires an immediate tool call.
- No-tool narration handling: when the model says it is about to write/create/update/run but emits no tool calls, the next auto-continue uses a direct tool-call prompt immediately instead of waiting for another soft continuation.
- No-tool inspection handling: when the model says it will find/explore/check/read/search/fetch but emits no tool calls, the next auto-continue forces an immediate inspection/research tool call.
- Shell failures on Windows PowerShell that look like bash syntax return a hint telling the model to use PowerShell syntax or switch the shell backend to WSL/bash.
- Shell failures preserve captured stdout/stderr in the tool result before appending the exit error. PowerShell `curl` alias mistakes now return a specific hint to use `curl.exe` or `Invoke-WebRequest -Uri ... -UseBasicParsing`.
- Code block Open button opens code as a scratch snippet in the File tab
- Agent control panel for autonomous mode, tool toggles, stop, clear, and settings
- Agent panel has tabs for Agent, Plan, Activity, Tools, Browser, Memory, Skills, and Logs. Activity is scrollable and shows recent tool calls/results, including shell output; Windows shell child windows are hidden.
- The bottom status bar shows live run state driven by `mauler:run_state` events, and the title bar has a Doctor action that opens the Agent panel and runs diagnostics.
- Explorer and Agent side panes leave narrow collapsed rails when hidden, so double-click collapse remains discoverable and reversible.
- Auto-agent router classifies each task as Builder, Fixer, Reviewer, Researcher, Planner, or Auto, injects mode instructions into the system prompt, and displays the active mode in the Agent panel.
- Auto Agents can be toggled on/off in the Agent panel. Off means Manual mode with no mode-specific routing instructions.
- Agent mode override is available in the Agent tab: Auto, Manual, Builder, Fixer, Reviewer, Researcher, Planner.
- Agent presets are persisted under `settings.agents.presets` and support preferred profile, context budget, autonomy level, instructions, and per-tool permissions. Settings now has an **Agents** tab for editing these presets directly.
- Agent access presets are available from the Agent tab: Unrestricted, Balanced, and Offline. Unrestricted enables full enabled-tool access with no prompts; Balanced keeps web/browser enabled but prompts for writes/shell; Offline disables web/fetch/browser tools for local-only work.
- Toolsets are available as first-class capability groups: `safe`, `local-code`, `web-research`, `browser`, `memory`, `offline`, `balanced`, and `unrestricted`. The active toolset is a coarse gate over per-tool toggles, and each agent preset can select its own toolset from Settings > Agents.
- Durable project memory is stored in `~/.config/mauler/memory.json`, scoped to the workspace, and relevant entries are injected into new conversation system prompts. Memory entries support kind, importance, pinning, tags, updated/last-used timestamps, edit-in-place, filtering, and weighted retrieval so it can grow toward embeddings/RAG later.
- Saved/autosaved sessions are also indexed into `~/.config/mauler/state.db` using SQLite FTS, and the `session_search` tool lets the agent recall prior chat decisions, errors, fixes, and tool trails without stuffing old sessions into the prompt. The Memory tab has session recall search, reindex, clear-index, and reset controls.
- Task runs are stored in `~/.config/mauler/task-runs.json` with prompt, mode, profile, status, summary, tool trail, and a lifecycle timeline. The Logs tab can search, filter, refresh, export, or clear them.
- Full-page Logs and Memory views are available from the top bar and center tabs. Use them for serious run inspection, project memory editing, and session recall search; the Agent panel tabs remain compact quick views.
- Default logging is full-detail: tool inputs, tool results, and model responses are captured, with a larger 500-run retention default.
- Task runs include stop reason/detail fields for user stops, cancelled contexts, auto-continue exhaustion, tool denials, disabled tools, budget exhaustion, model/client errors, and tool errors. Logs surface the reason plus timeline events for model ready, truncation, auto-continue prompts, tool calls, blocks, failures, and completion.
- Recoverable tool failures, including malformed local-model tool JSON such as `unexpected end of JSON input`, are logged as `tool_error` timeline events and tool rows but do not set the run's terminal stop reason if the agent later recovers and finishes. The tool result tells the model to retry with complete valid JSON.
- Task runs now carry a structured `state` and state timeline (`planning`, `model_loading`, `thinking`, `researching`, `reading`, `editing`, `testing`, `recovering`, `blocked`, `failed`, `done`) so the Logs tab can show what phase the agent reached before stopping.
- Planner/todo tools are available: `todo_create`, `todo_update`, `todo_done`, `todo_blocked`, `todo_list`, and `todo_clear`. They store the active checklist in `~/.config/mauler/todos.json`; the Agent panel has a Plan tab with refresh/clear controls and live updates after todo tool calls.
- Web search uses auto selection: configured SearXNG first, then Brave with an API key, then DuckDuckGo HTML as the no-key fallback; `fetch_url` reads source pages.
- Web research is bounded per user task with configurable max searches, max fetches, max failed web attempts, and max browser actions. Repeated failed/no-result searches stop the loop and tell the model to report uncertainty.
- Search results are ranked and labelled by source quality: official docs, GitHub/repo docs, package docs, general sources, blogs/community, and low-confidence mirrors.
- Browser automation tools are available for pages where search/fetch is not enough: `browser_open`, `browser_snapshot`, `browser_click`, `browser_type`, `browser_extract`, `browser_screenshot`, and `browser_close`.
- Session save/load/delete in the titlebar
- FileTree expand/collapse, Up/Home/Cd/Refresh, and single-click file open into the File tab
- FileTree `Cd` uses the native Windows directory picker through Wails, not `window.prompt`
- App chrome does not repeat the app name or icon; native Windows chrome carries app identity
- Windows icon assets are `build/appicon.png` and `build/windows/icon.ico`; update both before `wails build`
- UI direction: dark, rounded, Codex/VS Code-like work surfaces with a soft bottom chat composer. Explorer and Agent side panes can be shown/hidden and resized from the title bar/drag handles.
- MAULER.md auto-discovery when no explicit path is configured
- Keyboard shortcuts: `Ctrl+,`, `Esc`, `Ctrl+K`
- Settings has separate Providers and Profiles tabs; providers can be pinged and listed for models
- Profiles include an explicit Thinking behaviour card. Thinking on/off is per profile, with preserved-thinking display gated behind the thinking toggle and only the active parameter family shown.
- Anthropic/Claude defaults are removed; supported UI backends are local OpenAI-compatible providers: LM Studio and llama.cpp
- **Artifact runner fully wired end-to-end**: Run/Stop buttons in FileViewer, streaming output panel with auto-scroll and pulsing indicator, `mauler:artifact_output` / `mauler:artifact_done` events consumed in App.tsx
- **Settings round-trip data integrity fixed**: `go.ts` Settings interface now includes all fields including `think_indicator`, `diff_colours`, and the full `image` block — missing fields no longer silently zero out on save
- Regression tests cover profile generation settings, one-model-load-per-key behavior, compaction lock boundaries, workspace switching/context reset, missing-path workspace hints, Monaco save rollback snapshots, web/browser budgets, source ranking, settings default migration, toolset filtering, PDF text extraction, safety presets, shell/bash alias filtering, path normalization, and task-run logging/timeline behavior.
- `npm run build` passes clean (301 modules, no TypeScript errors)

Last verified: `go test ./...`, `go vet ./...`, `npm run build`, and `.\build.ps1` pass.

Production output: `C:\Users\richa\Desktop\TheMauler\build\bin\TheMauler.exe`.

---

## What to Build Next

### NEXT: Hermes-inspired "next level" agent foundation

The first Hermes-agent foundation pass is now mostly landed: session recall, structured run state, todo/planner tools, local skills, toolsets, and post-run skill suggestions are implemented. Keep the remaining UI polish sections below; do not remove them.

### Latest local-LLM compatibility work, researched 2026-05-28

P1 — Provider/tool-call compatibility hardening:
- Send `stream_options.include_usage=true` on streaming OpenAI-compatible requests so token accounting works on current LM Studio/llama.cpp-style APIs. Status: implemented.
- Send `parallel_tool_calls=false` by default when tools are present; TheMauler owns batching through tools such as `read_many`, and local Qwen reliability is better with sequential tool calls. Status: implemented.
- For llama.cpp-compatible backends, send `parse_tool_calls=true` when tools are present so native tool parsing is requested instead of relying only on text repair. Status: implemented.
- Expand Doctor for LM Studio native metadata: tool/function capability, reasoning metadata, and loaded context length. Status: first pass implemented; keep polishing as LM Studio API fields evolve.

P2 — Backend/profile modernization:
- Add first-class provider presets/docs for SGLang and vLLM OpenAI-compatible Qwen3.6 serving, including official `--reasoning-parser qwen3` and `--tool-call-parser qwen3_coder` guidance. Status: provider presets and launch notes implemented.
- Re-check default Qwen3.6 profile sampling against the official model card: thinking/general, coding, and non-thinking parameter families. Status: defaults updated so thinking general/coding use `presence_penalty=0.0`, non-thinking keeps `presence_penalty=1.5`.
- Keep llama.cpp diagnostics current for `chat_format`, fallback templates, `parse_tool_calls`, reasoning output format, and experimental server-side built-in tools. Status: Doctor covers format/template and warns if dangerous server-side built-in file/shell tools appear enabled in `/props`.

P3 — Hermes-style agent foundation follow-up:
- Add bounded subagents for Researcher, Reviewer, Test/Fix, and Summarizer with profile/toolset/context/time/output contracts. Status: first pass implemented as `subagent_research`, `subagent_review`, `subagent_testfix`, and `subagent_summarize` tools with scratch history and budgets.
- Add post-write verification: file mutation verifier plus optional LSP/diagnostic run after edits. Status: mutation verifier implemented; LSP diagnostics still planned.
- Add promptware/secret-exfiltration guardrails for fetched docs, repo content, and tool results before passing them back into the model. Status: first pass implemented for all tool results; keep tuning patterns and UX.
- Consider worktree-per-task isolation for high-risk autonomous changes. Status: planned.
- Add regression tests for Hermes/Qwen XML tool-call examples and local-provider compatibility flags. Status: partial; compatibility flag tests and Hermes JSON `<tool_call>` tests added.

1. **Bounded subagents** — add focused subagent runners for Researcher, Reviewer, Test/Fix, and Summarizer with explicit profile, toolset, timeout, context budget, and output contract. Status: first pass implemented as bounded subagent tools.
2. **Doctor diagnostics** — one-click health report for provider reachability, duplicate LM Studio loads, context mismatch, shell backend, path translation, browser automation, memory DB, logs, and web search. Status: first pass backend/app surface appears present; verify UX in app and fill any missing checks.
3. **Live in-run state updates** — structured run state exists in task logs and now streams into the bottom status bar during a run. Status: implemented; keep polishing state copy.
4. **PDF/OCR document handling** — `read_pdf` now extracts text from text-based PDFs. Next: add OCR fallback or a clear scanned-PDF workflow for image-only PDFs. Status: text extraction implemented.
5. **Sandbox shell backends** — keep local/WSL first, then add Docker and SSH execution backends for safer unrestricted work. Status: planned.
6. **Skill import/marketplace later** — support direct URL/GitHub import once local skills are stable. Status: planned.

### 2. Agent Controls Polish

- Add a compact live run-state indicator, backed by live `mauler:run_state` events, so the user can see thinking/reading/editing/testing/recovering while a task is running. Status: implemented in the bottom status bar.
- Add a first-class top-bar Doctor/Health action that opens/runs diagnostics without hunting through the Agent footer. Status: implemented.
- Add collapsed side rails for Explorer and Agent panes so hidden panes remain discoverable after double-click collapse. Status: implemented.
- Make guarded/tool/system outputs visually distinct in chat and logs, especially promptware guardrail notices. Status: implemented for chat guardrails and log guardrail severity.
- Verify access preset UX in-app: Unrestricted, Balanced, and Offline copy/state should be obvious from the Agent tab.
- Continue improving full-auto flow: mode selection, task budgets, safe-list interaction, and concise stop/report behavior when blocked.
- Add visible explanation of Offline vs Balanced presets.
- Add richer browser activity cards with screenshot preview links.

### 3. Settings UX Polish

- Add profile create/rename alongside duplicate/delete.
- Add validation for empty profile names, invalid URLs, and numeric bounds.
- The Image settings tab was added in the last session — verify it saves and loads correctly end-to-end.

### 4. Local Provider Polish

- Add a quick model picker that fills `model_id` from the selected provider's `/models` response (calls existing `ListModelsForProvider` binding).
- Better error messages for unreachable LAN IPs, blocked firewalls, and missing `/v1` path.
- Add a model health panel showing loaded model, context, quantization, TTFT, and tokens/sec when the provider exposes it.

### 5. Web Tools Polish

- Prefer a configured local SearXNG instance for private search.
- Keep DuckDuckGo as no-key fallback and Brave as optional API-key mode.
- Add result caching and source cards in the agent panel.
- Add browser wait/navigation helpers and optional visible-browser mode for inspecting automation live.
- Add tool-result summaries so large web/shell outputs do not bloat the chat context.
- Add a run replay/debug view for reviewing agent decisions, tool calls, denials, and stop reasons.

---

## Known Gotchas

1. Always run `wails dev` and `wails build` from `C:\Users\richa\Desktop\TheMauler`.
2. Do not launch a plain `go build` binary such as root `mauler.exe`; Wails will show a build-tags dialog. Use `wails build -clean` or `.\build.ps1`, then run `build\bin\TheMauler.exe`.
3. On Linux/WSL, use `./build.sh` and run `build/bin/TheMauler`; do not run a plain `go build` binary.
4. `frontend/dist/` must exist for Wails packaging. `.\build.ps1` and `./build.sh` handle this.
5. Shell execution is platform-aware. On Windows auto uses PowerShell; choose `wsl` in Settings when commands should run inside WSL. File tools normalize common Windows/WSL path forms.
6. `ProfilesFile.Profiles` is `map[string]Profile`.
7. `encoding/base64` in `app.go` is used by `EncodeFileBase64`.
8. There is no `.git` metadata in this workspace right now, so use direct file inspection rather than git diff/status.
