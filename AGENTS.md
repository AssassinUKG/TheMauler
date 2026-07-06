# TheMauler - Agent Handoff Document

Read this file at the start of every session. It is the single source of truth for the project state, architecture, and what to build next.

---

## What is TheMauler?

A native Windows desktop AI agent workbench, roughly VS Code meets Claude Code. Built with:

- Go 1.26 backend for AI logic, tools, and file operations
- Wails v2.12 desktop shell with Chromium WebView2
- React + TypeScript + Vite frontend
- Monaco editor for the file viewer/editor

The user has an RTX 3090 with 24 GB VRAM and currently runs Qwen3.6 locally through InferenceBridge, exposed to TheMauler as an OpenAI-compatible local provider. Keep new integration work Mauler-side unless explicitly asked otherwise; do not add new LM Studio/vLLM/SGLang provider paths as part of current work. Never recommend Q6_K quant; it OOMs at useful context on 24 GB. Recommend Q5/Q4-class profiles that fit the requested context.

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
        |-- read_file.go             # Legacy backend for compact read tool
        |-- write_file.go            # Legacy backend for compact write tool
        |-- edit_file.go             # Legacy backend for compact edit tool
        |-- shell.go                 # Platform-aware shell tool; no alternate model-facing shell alias
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

- Provider = endpoint/backend transport, currently the user's InferenceBridge OpenAI-compatible endpoint, for example `http://127.0.0.1:8800/v1`.
- Profile = model behaviour, for example model id, context tokens, thinking mode, and sampling params.

`profiles.toml` now has both `[providers]` and `[profiles]`. Old profile-level `backend` and `base_url` fields are migrated on load into provider entries.
Provider-only legacy profiles such as `lmstudio-default` are intentionally removed from `[profiles]`; current user config should stay InferenceBridge-only unless the user explicitly asks to re-add another provider.

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
- Model-facing tool calls use the compact registry: `read`, `write`, `edit`, `shell`, `terminal_send`, `terminal_read`, `http_probe`, `start_listener`, `glob`, `grep`, `session_search`, `todo_write`, `skill`, `task`, `web_search`, `fetch_url`, `browser`, `memory`, `progress`, `read_tool_result`, `evidence_bundle`, `sqlite`, and support tools such as `set_reasoning_effort`. Do not prompt legacy backend/alias names as model-facing tools.
- PDF text extraction is available through the read-only `read_pdf` tool with optional page ranges and output limits. It is for text-based PDFs; scanned/image-only PDFs still need OCR later.
- Shell backend is configurable as `auto`, `powershell`, `cmd`, `bash`, or `wsl`. Auto uses PowerShell on Windows and bash on Linux/WSL.
- Shell mode is configurable as `shared_terminal` or `isolated`. In `shared_terminal` mode, `shell` tool calls use the visible Terminal pane when the backend is WSL/bash, emit AI command start/done markers, and fall back to isolated execution for unsupported backends.
- Tool routing contract: `terminal_send`/`terminal_read` are for commands inside a live or interactive terminal session. Independent HTTP/webshell/curl/wget checks should use `http_probe` when possible, or `shell` when exact flags/pipelines are required. Do not type independent web probes into a connected terminal or listener.
- File tools normalize common path forms between Windows and WSL, such as `/mnt/c/...`, `/c/...`, and `C:\...`.
- Workspace selection is authoritative for tools and prompts. `SetWorkingDir` and Settings `workspace_dir` both apply the process cwd, persist the normalized path, and clear the active chat/tool context plus rollback stack when the project changes. New runs include the current workspace root and top-level entries in the system prompt; missing-file tool results include the current workspace and a glob/read hint so stale paths from another project do not keep looping.
- Model loading/context checks are hard-fail for the active provider when the backend reports an actual context smaller than the selected profile's `ctx_tokens`. The app must not silently continue at 8K when the profile requests 32K/40K/54K.
- OpenAI-compatible non-streaming model-list calls have a 10 second timeout even though streaming chat uses an unbounded HTTP client timeout.
- Chat requests copy active profile generation params: max tokens, temperature, top-p, top-k, min-p, presence penalty, seed, and thinking flags
- Confirm gate for destructive tools
- Tool confirmations can be allowed once or added to an exact-input safe list. Safe-listed tool approvals skip future prompts and can be removed in Settings > Tools.
- Tools show informational risk labels in Agent > Tools and Settings > Tools. Low: read/search-local tools; medium: web/browser read tools; high: shell/write/edit/browser interaction. Labels do not restrict autonomous mode; enabled tools remain available.
- File rollback stack through `Undo`
- Mutating agent tools are snapshotted before execution and verified after success. Compact `write` verification checks that the target file exists and content/append suffix matches the tool input; compact `edit` verification checks that the replacement landed. Language-specific lint still runs afterward for Go, Python, and shell files.
- Tool results pass through promptware and secret-exfiltration guardrails before being appended back into model history. Suspicious prompt-injection/exfiltration language is labelled as untrusted data, and obvious credential assignments/private-key blocks are redacted.
- Context compaction at 85 percent
- Settings modal with **eleven tabs** (general, providers, profiles, agents, environment, tools,
  telegram, context, storage, ui, image), live editing, and TOML persistence
- Profile switcher in status bar
- Token usage bar in status bar
- Status bar backend ping skips polling during streams without restarting the interval when streaming flips on/off.
- Image paste in chat
- Sent user image attachments remain visible as clickable thumbnails in the chat transcript and are preserved when sessions are loaded
- Chat drafts remain editable while the agent is running. Enter inserts a newline; Ctrl+Enter or the Send button sends.
- Sending while the agent is running interrupts the current run and sends exactly one pending draft after the stop completes. There is no FIFO queue replay.
- Remote channel messages use the channel bus instead of the desktop Chat draft path. Telegram
  side-chat, quick actions, and `/run` work requests can queue while a project run is busy and drain
  when the run becomes idle.
- Auto-continue waits 500 ms between continuation retries to avoid hammering a local backend when the model repeatedly stops mid-task.
- Qwen/local OpenAI-compatible truncation handling: OpenAI-compatible SSE `finish_reason:"length"` sets `Delta.Truncated`; the agent auto-continues even when the text ends cleanly. If the tail says it is about to act (for example "right - let me write..."), it uses a directive prompt that requires an immediate tool call.
- No-tool narration handling: when the model says it is about to write/create/update/run but emits no tool calls, the next auto-continue uses a direct tool-call prompt immediately instead of waiting for another soft continuation.
- No-tool inspection handling: when the model says it will find/explore/check/read/search/fetch but emits no tool calls, the next auto-continue forces an immediate inspection/research tool call.
- Shell failures on Windows PowerShell that look like bash syntax return a hint telling the model to use PowerShell syntax or switch the shell backend to WSL/bash.
- Shell failures preserve captured stdout/stderr in the tool result before appending the exit error. PowerShell `curl` alias mistakes now return a specific hint to use `curl.exe` or `Invoke-WebRequest -Uri ... -UseBasicParsing`.
- Shell and terminal output decoding handles UTF-16-ish Windows output as UTF-8 text before returning results to chat/logs.
- Code block Open button opens code as a scratch snippet in the File tab
- Agent control panel for autonomous mode, tool toggles, stop, clear, and settings
- Agent panel has tabs for Agent, Plan, Activity, Tools, Browser, Memory, Skills, and Logs. Activity is scrollable and shows recent tool calls/results, including shell output; Windows shell child windows are hidden.
- The bottom status bar shows live run state driven by `mauler:run_state` events, and the title bar has a Doctor action that opens the Agent panel and runs diagnostics.
- Doctor now checks the active InferenceBridge provider for agent-backend endpoint parity: structured agent-action validation plus route availability for Anthropic `/v1/messages` and OpenAI `/v1/embeddings`, without spending a model generation.
- Explorer and Agent side panes leave narrow collapsed rails when hidden, so double-click collapse remains discoverable and reversible.
- Auto-agent router classifies each task as Builder, Fixer, Reviewer, Researcher, Planner, or Auto, injects mode instructions into the system prompt, and displays the active mode in the Agent panel.
- Auto Agents can be toggled on/off in the Agent panel. Off means Manual mode with no mode-specific routing instructions.
- Agent mode override is available in the Agent tab: Auto, Manual, Builder, Fixer, Reviewer, Researcher, Planner.
- Ops is no longer a default/auto-selected agent mode. Treat the app as one unrestricted agentic AI with optional Run/evidence surfaces. `/ops` is legacy shorthand for `/run`, and HTB/pentest-looking prompts should stay normal Auto agent runs unless the user explicitly asks for a specialised mode.
- Agent presets are persisted under `settings.agents.presets` and support preferred profile, context budget, autonomy level, instructions, and per-tool permissions. Settings now has an **Agents** tab for editing these presets directly.
- Agent access presets are available from the Agent tab: Unrestricted, Balanced, and Offline. Unrestricted enables full enabled-tool access with no prompts; Balanced keeps web/browser enabled but prompts for writes/shell; Offline disables web/fetch/browser tools for local-only work.
- Toolsets are available as first-class capability groups: `safe`, `local-code`, `web-research`, `browser`, `memory`, `offline`, `balanced`, and `unrestricted`. The active toolset is a coarse gate over per-tool toggles, and each agent preset can select its own toolset from Settings > Agents.
- Telegram remote control has a first-pass Go runtime: Settings > Telegram configures enable/token,
  bot username, mention/allow-list, default project/profile/mode/toolset, progress cadence, and
  voice preferences. When enabled, the bot long-polls Telegram through `internal/app/telegram_runtime.go`
  and routes messages through `internal/channelbus` into side-chat, control, quick-action, or queued
  work lanes. Normal Telegram text is a separate model-backed side chat and does not pollute the
  desktop Chat transcript; explicit `/run` starts or queues unrestricted project work using the
  configured defaults. Busy-time side-chat/quick/run requests are stored in the persistent
  `channel_work_queue` and drained after the active project run is idle.
- Telegram has a full-page UI (`TelegramPage`) for bot status, queue, chat-style conversation,
  raw ledger events, manual sends, and delete-message actions. Telegram/channel events are recorded
  in RunLedger with sources `telegram` and `channelbus`; tokens and token-bearing URLs must stay
  redacted from logs.
- Durable project memory is stored in `~/.config/mauler/memory.json`, scoped to the workspace, and relevant entries are injected into new conversation system prompts. Memory entries support kind, importance, pinning, tags, updated/last-used timestamps, edit-in-place, filtering, and weighted retrieval so it can grow toward embeddings/RAG later.
- Saved/autosaved sessions are also indexed into `~/.config/mauler/state.db` using SQLite FTS, and the `session_search` tool lets the agent recall prior chat decisions, errors, fixes, and tool trails without stuffing old sessions into the prompt. The Memory tab has session recall search, reindex, clear-index, and reset controls.
- Task runs are stored in `~/.config/mauler/task-runs.json` with prompt, mode, profile, status, summary, tool trail, and a lifecycle timeline. The Logs tab can search, filter, refresh, export, or clear them.
- RunLedger core slices are implemented in `internal/ledger`: task-run events, state transitions, tool results, stops, finishes, confirmations, artifact lifecycle/output, terminal shell lifecycle/input-size, memory writes/deletes, skill writes/deletes, learning suggestions, provider/model diagnostics, web/browser/planner/subagent category events, and bounded subagent lifecycle are mirrored to UTF-8 JSONL at `~/.config/mauler/run-ledger.jsonl`, with Wails bindings `ListLedgerEvents` and `ClearLedgerEvents`. The center-pane Brain tab reads this ledger with filters, signals, selected-event detail, export, and clear controls. Use this as the event spine for new Brain/Ops/logging work rather than adding another partial log path.
- Reviewable learning candidates are exposed by `ListLearningCandidates`: it derives skill/reflection/evidence suggestions from recent ledger events, redacts sensitive snippets, and the Brain tab can approve them into Memory entries or Skills. Keep this approval-first pattern for future learning; do not silently promote model guesses into durable memory.
- Full-page Logs and Memory views are available from the top bar and center tabs. Use them for serious run inspection, project memory editing, and session recall search; the Agent panel tabs remain compact quick views.
- Default logging is full-detail: tool inputs, tool results, and model responses are captured, with a larger 500-run retention default.
- Task runs include stop reason/detail fields for user stops, cancelled contexts, auto-continue exhaustion, tool denials, disabled tools, budget exhaustion, model/client errors, and tool errors. Logs surface the reason plus timeline events for model ready, truncation, auto-continue prompts, tool calls, blocks, failures, and completion.
- Recoverable tool failures, including malformed local-model tool JSON such as `unexpected end of JSON input`, are logged as `tool_error` timeline events and tool rows but do not set the run's terminal stop reason if the agent later recovers and finishes. The tool result tells the model to retry with complete valid JSON.
- Task runs now carry a structured `state` and state timeline (`planning`, `model_loading`, `thinking`, `researching`, `reading`, `editing`, `testing`, `recovering`, `blocked`, `failed`, `done`) so the Logs tab can show what phase the agent reached before stopping.
- Planner/todo tools are available: `todo_create`, `todo_update`, `todo_done`, `todo_blocked`, `todo_list`, and `todo_clear`. They store the active checklist in `~/.config/mauler/todos.json`; the Agent panel has a Plan tab with refresh/clear controls and live updates after todo tool calls.
- Master/project workflow skills are now lazy-loaded: registering a master skill stores the source path and a compact outline, while `skill` with `mode=view`, `name=master`, and an optional focused `query` returns an outline or excerpt. Do not reintroduce full master-skill injection into every chat/system prompt.
- Web search uses auto selection: configured SearXNG first, then Brave with an API key, then DuckDuckGo HTML as the no-key fallback; `fetch_url` reads source pages.
- `fetch_url` now favors readable page text over raw CSS/script noise, and GitHub URLs are fetched through repository/raw content paths where possible.
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
- Profiles include an explicit Thinking behaviour card. Default active profile is `qwen3.6-nothink`; chat, coding, and tool-heavy runs should stay no-thinking unless the user explicitly selects a planner/reviewer/deep-thinking pass. Tool-enabled turns force no-thinking from the first tool turn even if a thinking profile is selected.
- Anthropic/Claude defaults are removed; the current supported live path for this user is local InferenceBridge via the OpenAI-compatible provider shape.
- **Artifact runner fully wired end-to-end**: Run/Stop buttons in FileViewer, streaming output panel with auto-scroll and pulsing indicator, `mauler:artifact_output` / `mauler:artifact_done` events consumed in App.tsx
- **Terminal pane first-class for daily work**: terminal visibility/height persist in UI settings, the terminal opens by default when configured, includes a short Help panel, and can be used interactively while AI shell runs are visible in the same stream.
- Terminal result hygiene: `terminal_send command` returns only the command plus output appended after that command, not the whole current screen/scrollback; `terminal_read mode=history` without `grep` no longer dumps raw old scrollback into the model; the AI Commands panel does not duplicate terminal tools through generic tool-result events and treats normal terminal states such as `prompt_or_idle` as non-error.
- Bottom work area now has Terminal and Stream tabs. New agent runs auto-open the Stream tab while manual bottom-panel toggles open Terminal, so live model text/tool activity no longer consumes the main Run page.
- Composer/toolbox popovers now carry plan/tool/channel status so todo and tool-state surfaces do
  not have to spam the main transcript. Todo tool calls/results are suppressed from the visible chat
  and represented through plan surfaces instead.
- Run page evidence is now profile-driven. Default `Pentesting` profile is attack/log/report focused for authorised work: hosts, services, CVEs, vulnerability hints, PoC verification signals, severity, artifacts, and report/evidence paths, with no remediation/client-fix loop. `HTB / CTF` is the explicit profile for recon/foothold/user/privesc/root/flag/writeup flows. Do not make normal agent runs revolve around HTB user/root flags.
- **Settings round-trip data integrity fixed**: `go.ts` Settings interface now includes all fields including `think_indicator`, `diff_colours`, and the full `image` block — missing fields no longer silently zero out on save
- Regression tests cover profile generation settings, one-model-load-per-key behavior, compaction lock boundaries, workspace switching/context reset, missing-path workspace hints, Monaco save rollback snapshots, web/browser budgets, source ranking, settings default migration, toolset filtering, PDF text extraction, safety presets, compact shell/toolset filtering, path normalization, and task-run logging/timeline behavior.
- `npm run build` passes clean (301 modules, no TypeScript errors)

Last focused verification 2026-07-06: terminal-result hygiene tests under `go test ./internal/app -run "TestTerminalRunResult|TestTerminalReadHistory|TestFormatTerminal|TestWaitForTerminal|TestClampRead|TestCleanTerminal|TestClassifyTerminal"` pass, `npm run --prefix frontend build` passes, and `.\build.ps1 -SkipTests` builds `build\bin\TheMauler.exe`.

Production output: `C:\Users\richa\Desktop\TheMauler\build\bin\TheMauler.exe`.

---

## What to Build Next

### NEXT: Workbench cockpit UI cleanup

The live-run UI cleanup tracker is `docs/workbench-cockpit-ui-cleanup-2026-07.md`. Direction:
deduplicate profile/state/context telemetry into one status strip, keep chat for assistant speech,
keep AI Commands for structured grouped tool history, keep Terminal as raw PTY output, make repeated
commands collapse with `xN` health badges, make errors visually loud, add focus-run layout, promote
target/VPN/shell identity, stabilize primary actions, and tone down the persistent green context bar.
First pass landed: AI Commands groups consecutive similar rows and highlights error rows more
strongly. Continue with the single authoritative status strip and focus-run layout.

### NEXT: Mauler stability gate, then U16

Immediate Mauler-only order:
1. Live-smoke the current build against a real run and inspect RunLedger for repeated terminal/tool loops.
2. Patch any remaining compact-tool arg-shape sanitation: legacy `bash`/`cmd`/`powershell` keys into `shell.command`, XML-ish closing tags in path args, and residual HTML entity escalation before routing/logging.
3. Add/run `agent_eval` cases for terminal routing, compact shell arg repair, path sanitation, and repeated blocked terminal calls.
4. Implement U16 every-turn message-structure repair from `docs/agent-loop-upgrade-roadmap.md`.

### NEXT: AI terminal ↔ human parity + tooling fixes

Prioritized, self-contained implementation tasks for making the agent drive the shared terminal
"like a human" (a real VT screen model instead of a flat ANSI-stripped line log, OSC-133
completion/exit-code detection, in-place screen-change detection, full keystroke vocabulary) plus
tool-surface cleanup and Claude/Codex-style tooling upgrades live in
`docs/ai-terminal-parity-and-tooling-fixes-2026-07.md`. P0-1 through P0-4 now have first-pass
implementations: rendered agent-side screen, OSC-133 prompt/exit/cwd markers, in-place screen-change
wakeups, and richer keystroke vocabulary. 2026-07-06 added command-delta terminal results,
no raw history dump without grep, and AI Commands de-dup/error-status fixes. Continue terminal work
only when live smoke tests show a real remaining gap.

### DEFER: Voice / audio (STT + TTS, natural conversation)

Full implementation plan for talking to the agent and hearing it reply, low-latency and fully local
on the 3090, lives in `docs/voice-audio-implementation-plan-2026-07.md`. Decision: **all audio in the
Mauler, InferenceBridge untouched.** Stack: **Parakeet TDT 0.6B v3** STT (true streaming, no
silence-hallucination) + **Kokoro-82M** TTS (RTF 0.03 on a 3090, ~45 ms first-audio) + Silero VAD
(`@ricky0123/vad-web`), ideally unified in-process via **sherpa-onnx-go** (C API, CUDA). The natural
feel comes from sentence-chunking the existing `mauler:delta` stream into TTS + VAD barge-in via
`StopAgent`. Milestones: M1 push-to-talk (Python sidecar) → M2 streaming + barge-in → M3 in-process
sherpa-onnx. New `internal/audio/` package + `AudioConfig` settings + a VoicePane frontend.

### DEFER: Telegram remote-control bot

Full integration plan lives in `docs/telegram-bot-integration-plan-2026-07.md`. Direction: add a
Go-native Telegram Bot API long-polling adapter inspired by HelixClaw's Rust implementation at
`C:\Users\richa\Documents\HelixClaw\crates\helixclaw-channels\src\telegram.rs`, but keep TheMauler
Go-only. The bot is a separate channel chat, not the desktop project chat, while still being able to
select projects, start/stop runs, inspect facts/logs/files/artifacts, and drive the terminal through
the existing unrestricted toolset and state machine. Voice notes should download via Telegram
`getFile`, transcribe through the Mauler audio/STT path, and optionally reply with `sendVoice` /
`sendAudio` once TTS is wired. First pass channel bus is in `internal/channelbus`: side chat is
model-backed but project-isolated, explicit `/run` work queues when a project run is active, quick
terminal chores can route through a `quick_action` lane, and the queue is durable in SQLite via
`channel_work_queue`. First pass Telegram transport/runtime is implemented in `internal/telegram`
and `internal/app/telegram_runtime.go`: direct Bot API client, long polling, update-offset
persistence, allow-list/mention filtering, duplicate-message suppression, safe Telegram HTML
formatting/chunking, fake-server tests, message send/delete, file lookup/download, voice/audio
attachment routing, and queued-reply delivery. The frontend Telegram page shows chats, queue,
status, raw events, manual send, and delete actions. Next steps: finish full project-control
commands (`/projects`, `/project`, `/files`, `/file`, `/artifact`, `/brain`), direct trusted
`/terminal_send` execution through the state machine, progress edit-message UX, HelixClaw settings
import, and real STT/TTS voice handling.

### NEXT: Target tool registry (Claude-shaped, Hermes-friendly)

The concrete ~22-tool target registry — one orthogonal tool per capability modeled on Claude Code's
shape with Hermes function-calling conventions (snake_case, strict schema, minimal required) — lives
in `docs/tool-registry-target-spec-2026-07.md`. It collapses ~40 tools to ~22 by merging eight
families (read_*→`read`, todo_*→`todo_write`, browser_*→`browser`, subagent_*→`task`,
sqlite_*->`sqlite`, skills_*->`skill`, legacy shell aliases->`shell`, and legacy terminal command names->`terminal_send`) and dropping
the old model-facing shell alias; every dropped name becomes a `mode`/`type`/`action` arg so no capability is lost. Defines the
`ops-lean` toolset the settings audit asks for. `apply_patch` is in the core set. Grade with
`agent_eval` before/after. Includes a **future update** spec for real Chrome integration (extension +
native-messaging bridge so the AI drives the user's logged-in browser) — the `browser` merge is the
interim path.

### NEXT: Settings + loop + benchmark audit (agentic reliability)

Prioritized, evidence-backed fixes from a full audit of the live config, the agent loop, and the
benchmark live in `docs/settings-loop-benchmark-audit-2026-07.md`. Current status: context shortfall
is now a hard failure when the backend reports actual context below the profile request;
non-InferenceBridge providers have been stripped from the live config; grammar constraints are
disabled until a live probe proves they help. Still do: make Reasoning Effort coherent on no-think
profiles, run real profile benchmarks, and promote `agent_eval` as the regression gate.

### NEXT: Agent-loop upgrade tier (Hermes / Claude-Code-class patterns)

Implementable specs (files, signatures, algorithm, wiring anchors, tests, acceptance) live in
`docs/agent-loop-upgrade-roadmap.md` (U1–U7), with the cross-project analysis in
`docs/agent-loop-upgrade-plan-2026-06.md`. Order: U1 dynamic reasoning-effort tool → U2 tool-result
disk offload + `read_tool_result` → U3 Doctor launch-flag/quant assertions → U4 Go-native
programmatic tool execution over the registry → U5 graduated compaction ladder → U6 externalized
`PROGRESS.md` resume → U7 grammar-constrained tool args (held on a live probe). Go-native runtime
update: `docs/go-native-agent-runtime-update-2026-06-30.md` supersedes older Python-first
`run_script` wording. Keep orchestration, validation, timing, cancellation, ledgering, prompt
accounting, and program step execution in Go; Python is only workload code via shell/WSL when
needed. This builds on the now-complete reliability roadmap (R1–R10). The HelixClaw mirror is
`C:\Users\richa\Documents\HelixClaw\HELIXCLAW_AGENT_LOOP_UPGRADE.md`.

Reliability-spine follow-up now lives in `docs/agent-loop-upgrade-roadmap.md` as U8-U13:
deterministic tool/session state machine, live Run Facts/evidence prompt packet, critical-action
verifier loop, phase-specific run tool routing, clickable Run evidence cockpit, and reliability
benchmarks. First passes are in place for U8-U15: terminal/session state-machine guards,
Run Facts/evidence prompt packets, verifier gates, phase routing, Run evidence cards,
request-scoped execution-state packets, structured result contracts, and reliability benchmark
counters. Continue by hardening the explicit Plan/Act/Observe/Reflect loop, live shell smoke tests,
and the final lean tool registry.

**HelixClaw-parity ports (U16–U23).** A direct code-read comparison of both agent cores
(`docs/helixclaw-parity-comparison-2026-07.md`) found the remaining gap: HelixClaw is a hierarchical
multi-agent OS (CEO → supervisor actor → typed workers, each planner→executor→observer with
role-scoped tools); TheMauler is one hardened loop. Backport specs live in
`docs/agent-loop-upgrade-roadmap.md` as U16–U23, in priority order: U16 every-turn message-structure
repair (7-phase, from HelixClaw `session_repair.rs`) → U20 tool permission classes → U18 structural
write-guards (protected paths / patch-size / file-count caps) → U19 role-scoped tool sets → U17
verification-gate loop (build/test/lint gates that block completion) → U21 plan→review completion
rails → U22 experience/tool-sequence learning → U23 task-DAG dispatcher. Port selectively — do not
regress Mauler's existing leads (tool-result offload, compaction ladder, loop-metrics anti-loop,
`agent_eval` harness). HelixClaw source: `C:\Users\richa\Documents\HelixClaw\crates\helixclaw-agents\src\`.

### NEXT: Local model tuning and role-split evals

Track the concrete tuning lanes in `docs/context-management-tracker.md`: benchmark `batch` /
`ubatch`, Q4_K_S vs Q5_K_M, exact context-size fit, KV cache, flash-attn, sampler profiles,
and MTP settings with TTFT/inter-token latency/tool-call validity/VRAM stability. Add a
planner/executor/reviewer eval lane before making split-agent orchestration default, and measure
whether the uncensored fine-tune improves autonomy or hurts tool discipline through hallucinated
tools, repeated loops, invalid schemas, and weak evidence grounding. Do not recommend Q6_K on the
RTX 3090; it OOMs at useful context sizes.

### NEXT: Hermes-inspired "next level" agent foundation

The first Hermes-agent foundation pass is now mostly landed: session recall, structured run state, todo/planner tools, local skills, toolsets, and post-run skill suggestions are implemented. Keep the remaining UI polish sections below; do not remove them.

### NEXT: Brain / RunLedger memory architecture

Track the "smarter over time without context bloat" redesign in `docs/brain-memory-ledger-tracker.md`. Direction: centralize all run/tool/model/UI logging through one RunLedger event spine, then derive task logs, Run views, memory extraction, reflection lessons, skill promotion, evidence pointers, and retrieval-planned prompt packets from that ledger. Core RunLedger producer slices, Brain inspection UI, and reviewable learning candidates are in place; continue with retrieval-planned prompt packets and ledger-backed Run polish. Do not grow prompts by dumping raw logs, full shell output, web pages, or whole skills; keep bulky data external and retrieve summaries/chunks on demand.

### NEXT: Workspace/folder model redesign

Explorer now has a first-pass VS Code-like split between **Agent Root** and browse-only **Open Folders**. The agent root remains the authoritative cwd for tools, shell, memory/session scope, and prompts; extra folders can be added to Explorer without changing cwd or clearing chat context. The Explorer also has a generic pentest/lab status card for target, VPN/interface, shell, latest artifact, and user-chosen folder scaffolding. Continue the redesign with recent/saved workspace files and richer lab run cards. See `docs/workspace-redesign-plan.md`.

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
- Add HTB/lab run cards that show target, shell backend, agent root, command phase, and latest scan/output artifact.

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
- Extend lazy context retrieval beyond master skills: apply outline/query/capped retrieval patterns to large web, shell, session, and document results.
- Add a run replay/debug view for reviewing agent decisions, tool calls, denials, and stop reasons.
- Terminal follow-up: add an explicit AI pause/take-over control, command attachment/pinning for scan artifacts, and richer long-running command progress cards.

---

## Known Gotchas

1. Always run `wails dev` and `wails build` from `C:\Users\richa\Desktop\TheMauler`.
2. Do not launch a plain `go build` binary such as root `mauler.exe`; Wails will show a build-tags dialog. Use `wails build -clean` or `.\build.ps1`, then run `build\bin\TheMauler.exe`.
3. On Linux/WSL, use `./build.sh` and run `build/bin/TheMauler`; do not run a plain `go build` binary.
4. `frontend/dist/` must exist for Wails packaging. `.\build.ps1` and `./build.sh` handle this.
5. Shell execution is platform-aware. On Windows auto uses PowerShell; choose `wsl` in Settings when commands should run inside WSL. Shared terminal mode only applies to WSL/bash backends; PowerShell/cmd shell tool calls fall back to isolated execution. `terminal_send` is for live terminal interaction; `http_probe`/`shell` are for independent HTTP/webshell/curl checks. File tools normalize common Windows/WSL path forms.
6. `ProfilesFile.Profiles` is `map[string]Profile`.
7. `encoding/base64` in `app.go` is used by `EncodeFileBase64`.
8. There is no `.git` metadata in this workspace right now, so use direct file inspection rather than git diff/status.
9. Do not delete working code just to simplify a change. Preserve good code and existing capabilities unless there is a very strong, explicit reason to remove them; prefer additive, guarded, or compatibility-preserving edits.
