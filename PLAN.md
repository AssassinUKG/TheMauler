# TheMauler — Full Build Plan

> Current planning note (May 26, 2026): this original plan is historical. Use
> `AGENTS.md` as the live source of truth. The Hermes-inspired foundation pass
> has landed for session recall, structured run logs/state, todo/planner tools,
> local skills, toolsets, and post-run skill suggestions. Remaining planned
> foundation work is bounded subagents, diagnostics polish, live in-run state
> polish, sandbox shell backends, and later skill import/marketplace support.
> The UI polish work is still active and should not be removed.

> Go TUI agent app for WSL. Streaming agentic loop, artifact runner, multimodal chat,
> project context engine, and a live settings editor for everything.

---

## Table of Contents

1. [Vision](#1-vision)
2. [Layout & UI](#2-layout--ui)
3. [Component Map](#3-component-map)
4. [Agent Engine](#4-agent-engine)
5. [Tool Registry](#5-tool-registry)
6. [Artifact Runner](#6-artifact-runner)
7. [LLM / Inference Bridge](#7-llm--inference-bridge)
8. [Image Support](#8-image-support)
9. [Project Context Engine](#9-project-context-engine)
10. [In-App Settings Editor](#10-in-app-settings-editor)
11. [Modal Input (Helix-style)](#11-modal-input-helix-style)
12. [Mode Switching](#12-mode-switching)
13. [Profiles & Config](#13-profiles--config)
14. [Qwen3.6-27B Setup](#14-qwen36-27b-setup)
15. [File Layout](#15-file-layout)
16. [Dependency List](#16-dependency-list)
17. [Build Order](#17-build-order)
18. [Key Bindings Reference](#18-key-bindings-reference)

---

## 1. Vision

TheMauler is a terminal-native AI agent workbench. It combines:

- **ClawCode** bits: streaming tool-call loop, confirmation gates for destructive ops,
  `MAULER.md` context loading, slash-command palette
- **HelixClaw** bits: modal input (normal/insert/visual), inline diff preview,
  tree-sitter symbol navigation
- **Inference Bridge** bits: backend-agnostic LLM client, hot-swap model mid-session,
  per-profile parameter sets, thinking-mode toggle

Primary backend: **Qwen3.6-27B** via LM Studio or llama.cpp (OpenAI-compatible server).
Fallback: any OpenAI-compatible endpoint, or Anthropic API.

Hardware baseline: RTX 3090 24 GB VRAM. Default quant: **UD-Q4_K_XL** (~17 GB) —
leaves ~7 GB for KV cache, supporting 32K context comfortably with headroom to spare.

Target: WSL2 on Windows 11. All tooling uses Unix paths and bash.

---

## 2. Layout & UI

### Three-pane project layout

```
┌────────────────────────────────────────────────────────────────────────┐
│ TheMauler  [model: qwen3.6-27b ◉thinking]  [ctx: 4821/32768]  [?]help │
├──────────────┬──────────────────────────────┬───────────────────────────┤
│  FILE TREE   │   CHAT / AGENT THREAD        │   ARTIFACT / OUTPUT       │
│              │                              │                           │
│  src/        │  ┄ session: project.mauler ┄ │  ┌─ shell ─────────────┐ │
│  ├ main.go   │                              │  │ $ go run ./cmd/...   │ │
│  ├ agent/    │  you: fix the parser edge    │  │ ok: built in 1.2s    │ │
│  │ ├ loop.go │       case on empty input    │  └─────────────────────┘ │
│  │ └ tool.go │                              │                           │
│  ├ ui/       │  ◉ agent [thinking...]       │  ┌─ preview ───────────┐ │
│  └ llm/      │    reading main.go:42        │  │                     │ │
│              │    [tool: read_file]  ✓      │  │  (rendered output   │ │
│  [P]roject   │    [tool: edit_file]  ✓      │  │   or image)         │ │
│  MAULER.md   │    diff applied              │  │                     │ │
│  .env        │                              │  └─────────────────────┘ │
│              │  ──────────────────────────  │                           │
│              │  you: [img: screenshot.png]  │  ┌─ diff ──────────────┐ │
│              │    ↳ vision: 1 image         │  │ - old line          │ │
│              │                              │  │ + new line          │ │
│              │  > _                         │  └─────────────────────┘ │
├──────────────┴──────────────────────────────┴───────────────────────────┤
│ [Tab] focus  [Ctrl+P] palette  [Ctrl+E] run  [Ctrl+M] model  [Ctrl+,] settings │
└────────────────────────────────────────────────────────────────────────┘
```

### Chat-only layout (--chat mode)

```
┌─────────────────────────────────────────────────────┐
│  TheMauler  [model: qwen3.6-27b]  [Ctrl+,] settings │
├─────────────────────────────────────────────────────┤
│                                                     │
│   you: explain this image [img: arch.png]           │
│                                                     │
│   ◉ agent: The diagram shows a three-tier...        │
│                                                     │
│   > _                                               │
│                                                     │
└─────────────────────────────────────────────────────┘
```

### Settings overlay (Ctrl+,)

Slides in over any layout. Full details in section 10.

### Stack

| Concern        | Library                        |
|----------------|-------------------------------|
| TUI framework  | `github.com/charmbracelet/bubbletea` |
| Styling        | `github.com/charmbracelet/lipgloss`  |
| Components     | `github.com/charmbracelet/bubbles`   |
| File tree      | custom bubbletea model               |
| Syntax hilight | `github.com/alecthomas/chroma`       |
| Diff render    | `github.com/sergi/go-diff`           |
| Image display  | sixel via `github.com/mattn/go-sixel` + kitty fallback |
| Tree-sitter    | `github.com/smacker/go-tree-sitter`  |
| Config         | `github.com/BurntSushi/toml`         |
| CLI flags      | `github.com/spf13/cobra`             |

---

## 3. Component Map

```
cmd/mauler/
  main.go               entry point, cobra root command

internal/
  agent/
    loop.go             streaming agentic loop
    dispatch.go         tool call router
    history.go          message history + compaction trigger
    rollback.go         undo last N tool mutations

  tools/
    registry.go         tool registration, JSON Schema export
    read_file.go
    write_file.go
    edit_file.go        unified diff patch apply
    bash.go             shell exec with confirm gate
    glob.go
    grep.go
    code_run.go         delegates to artifact runner
    web_search.go       DuckDuckGo / Brave API
    image_read.go       clipboard or path → base64
    symbol_jump.go      tree-sitter goto-definition

  artifact/
    runner.go           subprocess manager, language dispatch
    sandbox.go          timeout, kill, resource limits
    output.go           streaming stdout/stderr → bubbletea msg

  llm/
    client.go           Client interface definition
    stream.go           SSE / chunked JSON consumer
    backends/
      lmstudio.go       OpenAI-compat @ localhost:1234
      llamacpp.go       OpenAI-compat @ localhost:8080, thinking kwargs
      anthropic.go      Anthropic API v1

  image/
    paste.go            OSC 52 clipboard read, xclip/wl-paste
    encode.go           → base64 PNG for API
    display.go          sixel / kitty render in pane
    wslpath.go          /mnt/c/... path translation

  context/
    loader.go           MAULER.md / CLAUDE.md discovery + parse
    budget.go           token counting, compaction trigger
    compactor.go        rolling summary injection
    ignore.go           .maulerignore gitignore-style filter

  settings/
    model.go            Settings struct (all fields)
    load.go             TOML load + live reload
    save.go             atomic write back to disk
    editor.go           bubbletea settings overlay UI
    validator.go        range checks, backend reachability ping

  ui/
    app.go              root bubbletea model, pane routing
    pane_tree.go        file tree pane
    pane_chat.go        chat / agent thread pane
    pane_artifact.go    artifact / output pane
    overlay_settings.go settings editor overlay
    overlay_palette.go  Ctrl+P command palette
    overlay_model.go    Ctrl+M model picker
    input.go            modal input box (normal/insert/visual)
    status.go           bottom status bar
    theme.go            lipgloss colour definitions
```

---

## 4. Agent Engine

### Loop (`internal/agent/loop.go`)

```
user message
    │
    ▼
build messages[] (system + history + new msg)
    │
    ▼
llm.Client.Chat(ctx, messages, tools)  ← streaming
    │
    ├─ delta chunks → render to chat pane live
    │
    ├─ tool_call detected
    │     │
    │     ├─ destructive? → confirm gate (y/N prompt)
    │     │
    │     ├─ dispatch to tools.Registry.Run(call)
    │     │
    │     ├─ result → append tool_result message
    │     │
    │     └─ loop back to llm.Client.Chat
    │
    └─ finish_reason: stop → done
```

### Tool confirmation gate

- Read-only tools (`read_file`, `glob`, `grep`, `symbol_jump`): auto-approve
- Mutating tools (`write_file`, `edit_file`): show diff preview, require `y`
- Exec tools (`bash`, `code_run`): always show command, require `y`
- Gate can be disabled per-session via settings (`confirm_tools = false`)

### History compaction

- Tracks token count on every append (estimated via char-count heuristic, exact
  if the backend returns `usage.prompt_tokens` in the stream)
- When `used > budget * 0.85`: trigger compaction
  1. Ask the model to summarise work so far (terse, 5 sentences max)
  2. Replace middle messages with the summary assistant turn
  3. Always keep: system prompt, first 2 user/assistant turns, last 8 turns, summary
- Compaction banner shown in chat: `┄ context compacted at 85% ┄`
- Budget comes from active profile `ctx_tokens`
- Threshold configurable in settings (default 85%)

### Rollback

- Every mutating tool call pushed to `rollback.Stack`
- `/undo` or `Ctrl+Z` pops the stack, reverses the file write

---

## 5. Tool Registry

All tools declared as JSON Schema, compatible with OpenAI `tools` array format.
New tools: implement `Tool` interface, register in `init()`.

```go
type Tool interface {
    Name()        string
    Description() string
    Schema()      json.RawMessage   // JSON Schema for parameters
    Run(ctx context.Context, params json.RawMessage) (string, error)
    Destructive() bool
}
```

### Built-in tools

| Tool          | Destructive | Description |
|---------------|-------------|-------------|
| `read_file`   | no  | Read file with optional line range |
| `write_file`  | yes | Overwrite file |
| `edit_file`   | yes | Apply unified diff patch |
| `bash`        | yes | Run shell command, stream output |
| `glob`        | no  | Find files by pattern |
| `grep`        | no  | Regex search across files |
| `code_run`    | yes | Execute code block in artifact sandbox |
| `web_search`  | no  | Search DuckDuckGo or Brave API |
| `image_read`  | no  | Read image from clipboard or path → base64 |
| `symbol_jump` | no  | Tree-sitter goto-definition |
| `diff_view`   | no  | Show unified diff between two files |

---

## 5b. Tool Calling Quality

Getting reliable tool use from a local model requires more care than with Claude/GPT.
These measures close the gap.

### System prompt engineering

The system prompt includes a tools preamble that explains the contract explicitly:

```
You are an expert coding agent. You have access to tools. Rules:
- Call ONE tool at a time unless tools are explicitly independent reads.
- Always fill every required parameter. Never omit required fields.
- After a tool result, reason briefly before the next action.
- If a tool call fails, read the error and try a corrected call — do not give up.
- Never fabricate tool results. Only use what was returned.
```

Qwen3.6 with thinking mode follows this well — the `<think>` block plans the tool
call before emitting it, catching parameter mistakes before they happen.

### Strict JSON schema

Every tool schema uses `"additionalProperties": false` and marks all required
fields in `"required"`. This gives the model a tight target and lets the backend's
grammar-based sampling (llama.cpp `--json-schema`) constrain the output to valid
JSON automatically.

### Grammar-constrained tool calls (llama.cpp)

When the agent loop detects a `tool_use` turn is expected, it can pass the
combined JSON Schema of all tools as a grammar to llama.cpp via the
`response_format` field:

```json
{ "type": "json_schema", "json_schema": { "schema": <tools_union_schema> } }
```

This forces the model to emit syntactically valid JSON — no more truncated or
malformed tool calls. LM Studio supports this too via its structured output toggle.

### Retry with error feedback

If a tool call fails to parse or a required parameter is missing, the loop injects
an error tool_result instead of crashing:

```
tool_result: { "error": "missing required parameter: path. Please retry with path set." }
```

The model sees the error and self-corrects on the next turn. Max 3 retries per
tool call before surfacing the failure to the user.

### Parallel read-only dispatch

When the agent emits multiple tool calls in one turn and all are read-only
(`read_file`, `glob`, `grep`), they are dispatched concurrently via goroutines
and results are collected before the next LLM call. This cuts multi-file reads
from O(N×RTT) to O(1×RTT).

### Thinking mode and tool calling

With thinking enabled (temp 1.0 general, 0.6 coding), Qwen3.6 reasons inside
`<think>...</think>` before emitting the tool call JSON. This dramatically reduces
parameter errors and hallucinated paths. `preserve_thinking = true` keeps the
trace visible in the chat pane (collapsed by default, expandable with `t`).

For pure tool-heavy agentic sessions (many sequential edits), switch to the
`thinking_coding` profile (temp 0.6, presence_penalty 0.0) — more deterministic,
less verbose thinking.

---

## 6. Artifact Runner

Runs code blocks from the agent or from the user's `/run` command.
Output streams live into the right-hand artifact pane.

### Supported runtimes

| Language   | Command                         |
|------------|---------------------------------|
| Go         | `go run <file>`                 |
| Python     | `python3 <file>`                |
| Bash       | `bash -c <code>`                |
| JavaScript | `node <file>`                   |
| TypeScript | `npx ts-node <file>`            |
| Rust       | `cargo script <file>` (if avail)|

### Execution model

1. Write code block to temp file in `/tmp/mauler-artifacts/<uuid>/`
2. Spawn subprocess with 30s timeout (configurable in settings)
3. Stream stdout/stderr line-by-line via channel → artifact pane
4. On timeout: SIGTERM → 2s grace → SIGKILL
5. Exit code + duration appended to output
6. Output injected back as `tool_result` if triggered by agent tool call
7. `/rerun` re-executes last artifact with same code
8. Watch mode (`/watch`): re-run on file save using `fsnotify`

---

## 7. LLM / Inference Bridge

### Client interface (`internal/llm/client.go`)

```go
type Delta struct {
    Content   string
    ToolCalls []ToolCall
    Done      bool
    Error     error
}

type Client interface {
    Chat(ctx context.Context, req Request) (<-chan Delta, error)
    Models(ctx context.Context) ([]string, error)
    Ping(ctx context.Context) error
}
```

### Backends

**lmstudio** (`localhost:1234/v1`)
- Standard OpenAI chat completions with streaming
- Model ID passed as-is (LM Studio uses the loaded model name)
- Vision: multipart content array with `image_url` base64

**llamacpp** (`localhost:8080/v1`)
- Same OpenAI-compat API
- Passes `chat_template_kwargs` for `enable_thinking` / `preserve_thinking` per request
- Supports `response_format.json_schema` for grammar-constrained tool calls
- Vision: multipart `image_url` base64 format, requires `--mmproj` at server start
- MTP: transparent — if server was started with MTP flags, client sees no difference

**anthropic** (api.anthropic.com)
- Uses Anthropic Messages API v1
- Tool use via `tools` array
- Vision via `image` content blocks
- Thinking via `thinking: {type: "enabled", budget_tokens: N}`

### Hot-swap model (`Ctrl+M`)

1. Opens model picker overlay (list of profiles from `profiles.toml`)
2. On select: pings new backend (`client.Ping`)
3. If reachable: injects handoff message into history
   `"[Model switched to qwen3.6-27b / thinking mode. Context preserved.]"`
4. All subsequent requests use new client

### Backend reachability

On startup and on every model switch: `GET /v1/models` with 2s timeout.
Status shown in top bar: `◉ online` / `◯ offline`.

---

## 8. Image Support

### Paste flow (WSL)

```
user presses Ctrl+V in chat input
    │
    ├─ try xclip -selection clipboard -t image/png -o  (X11)
    ├─ try wl-paste --type image/png                   (Wayland)
    └─ try PowerShell Get-Clipboard -Format Image      (WSL2 Windows bridge)
         │
         ▼
    PNG bytes → save to /tmp/mauler-img-<uuid>.png
         │
         ▼
    display: sixel thumbnail in chat pane (max 200px wide)
         │
         ▼
    encode: base64 PNG → stored in message content array
         │
         ▼
    sent to model as vision content block
```

### Drag & drop / path

- User can type or paste a Windows path: `C:\Users\...`
- WSL path translator converts to `/mnt/c/...`
- Same encode + display pipeline

### Kitty / sixel fallback

- Detect terminal: check `$TERM`, `$TERM_PROGRAM`, kitty `\x1b_Ga` probe
- Kitty graphics protocol preferred (better quality)
- Sixel fallback for Windows Terminal / other
- If neither: show `[image: filename.png 1920x1080]` text placeholder

---

## 9. Project Context Engine

### MAULER.md / CLAUDE.md

- Discovered by walking up from CWD
- Loaded as system prompt prefix on every request
- Sections supported:
  - `# Context` — injected verbatim
  - `# Tools` — overrides which tools are enabled
  - `# Ignore` — same as .maulerignore
  - `# Profile` — override default model profile for this project

### .maulerignore

Gitignore-style patterns. Always ignores: `.git/`, `node_modules/`, `__pycache__/`,
`*.sum`, `go.sum`, `dist/`, `build/`.

### Auto-context injection (opt-in per settings)

When enabled, every message gets a prefix:
```
[Context: editing src/parser.go, cursor ~line 42]
[Project: TheMauler, Go 1.22, WSL2]
```

Toggle: `Ctrl+,` → Context → Auto-inject file context

### Compaction

- Rolling summary generated by the model when budget hits 75%
- Summary prompt: `"Summarize the work done so far in 3-5 sentences, focusing on
  files changed, decisions made, and what remains. Be terse."`
- Summary prepended to condensed history
- Compaction event shown in chat pane as `┄ context compacted ┄`

---

## 10. In-App Settings Editor

**Trigger:** `Ctrl+,` from anywhere. Slides in as a full-width overlay.
All changes apply immediately (hot reload). Saved to `~/.config/mauler/settings.toml`
on exit from the overlay or on `Ctrl+S` within it.

### Navigation

```
[↑/↓] move field    [Enter] edit    [Esc] cancel edit / close overlay
[Tab]  next section  [Ctrl+S] save now   [r] reset field to default
[/]    search fields
```

### Settings sections

---

#### Model & Backend

```
Active profile      [qwen3.6-27b          ▼]   (dropdown, all profiles listed)
Backend             [llamacpp             ▼]   lmstudio | llamacpp | anthropic
Base URL            [http://localhost:8080/v1 ]
Model ID            [qwen3.6-27b          ]
Context window      [32768                ]   tokens
Status              ◉ reachable (ping: 42ms)   [Test Connection]
```

---

#### Thinking Mode

```
Thinking enabled    [x]   toggle (on/off)
Preserve thinking   [x]   keep <think> trace visible in chat
Mode                [general ▼]   general | coding
```

Mode presets auto-fill the parameter fields below:

```
── General (thinking) ──────────────────────────────
  Temperature         [1.0 ]   (slider 0.0–2.0, step 0.05)
  Top-p               [0.95]
  Top-k               [20  ]
  Min-p               [0.0 ]
  Presence penalty    [1.5 ]

── Coding (thinking) ───────────────────────────────
  Temperature         [0.6 ]
  Top-p               [0.95]
  Top-k               [20  ]
  Min-p               [0.0 ]
  Presence penalty    [0.0 ]

── Non-thinking ────────────────────────────────────
  Temperature         [0.7 ]
  Top-p               [0.8 ]
  Top-k               [20  ]
  Min-p               [0.0 ]
  Presence penalty    [1.5 ]
```

Each field is individually editable even after a preset is applied.
Changed fields show a `*` marker. `[r]` resets to preset value.

---

#### Generation

```
Max tokens          [4096 ]
Stop sequences      [      ]   comma-separated
Repeat penalty      [1.1  ]
Frequency penalty   [0.0  ]
Seed                [-1   ]   (-1 = random)
Stream              [x]
```

---

#### Context & Memory

```
Context tokens      [32768 ]
Auto-inject file ctx [ ]
Auto-inject cursor  [ ]
Compaction at       [85    ]%
Show compaction msg [x]
MAULER.md path      [auto-discover ▼]   auto | custom path
```

---

#### Tools

```
Tools enabled       [x]
Confirm read tools  [ ]   (always auto-approve reads)
Confirm write tools [x]
Confirm bash/exec   [x]
Bash timeout        [30   ]s
Artifact timeout    [30   ]s
Web search engine   [duckduckgo ▼]   duckduckgo | brave
Brave API key       [              ]
```

Per-tool enable/disable list (toggle each):

```
  [x] read_file      [x] write_file    [x] edit_file
  [x] bash           [x] glob          [x] grep
  [x] code_run       [x] web_search    [x] image_read
  [x] symbol_jump    [x] diff_view
```

---

#### Image & Vision

```
Vision enabled      [x]
Clipboard method    [auto ▼]   auto | xclip | wl-paste | powershell
Image display       [sixel ▼]  sixel | kitty | text
Max display width   [200  ]px
WSL path translate  [x]
```

---

#### Profiles manager

A sub-screen listing all profiles from `profiles.toml`:

```
  NAME              BACKEND     MODEL                  CTX
  ─────────────────────────────────────────────────────────
▶ qwen3.6-think      llamacpp    qwen3.6-27b UD-Q4_K_XL  32768   thinking=on  coding
  qwen3.6-chat       llamacpp    qwen3.6-27b UD-Q4_K_XL  32768   thinking=on  general
  qwen3.6-nothink    llamacpp    qwen3.6-27b UD-Q4_K_XL  32768   thinking=off
  lmstudio-default   lmstudio    (loaded model)           16384   thinking=off
  claude-sonnet      anthropic   claude-sonnet-4-6        200000  thinking=off

  [n] new profile   [d] duplicate   [Del] delete   [Enter] edit
```

Editing a profile opens the same Model & Backend + Thinking + Generation fields
scoped to that profile only.

---

#### UI & Display

```
Theme               [dark ▼]   dark | light | system
Status bar          [x]
Token counter       [x]
Thinking indicator  [x]   show ◉ thinking spinner
Syntax highlight    [x]
Diff colours        [x]
Chat timestamps     [ ]
Pane widths         tree [20]%  chat [50]%  artifact [30]%
```

---

#### Keybindings

Read-only display of all bindings with a `[Edit keybindings]` button that opens
`~/.config/mauler/keybindings.toml` in `$EDITOR`.

---

#### About / Debug

```
Version             v0.1.0
Config dir          ~/.config/mauler/
Log file            ~/.config/mauler/mauler.log
Log level           [info ▼]   debug | info | warn | error
[Open log]   [Clear log]   [Copy diagnostics]
```

---

### Live apply rules

| Setting category  | Apply timing |
|-------------------|-------------|
| Model params      | Next request |
| Thinking toggle   | Next request |
| Backend / URL     | Immediately (re-ping) |
| Active profile    | Immediately |
| Tool enable/disable | Immediately |
| UI / theme        | Immediately (re-render) |
| Context budget    | Next compaction check |
| Pane widths       | Immediately |

---

## 11. Modal Input (Helix-style)

The chat input supports three modes, indicated in the status bar.

| Mode   | Enter via | Exit via | Behaviour |
|--------|-----------|----------|-----------|
| INSERT | `i` or just typing | `Esc` | Normal typing, newline on `Enter` |
| NORMAL | `Esc` | `i` | Navigate history, yank blocks |
| VISUAL | `v` from NORMAL | `Esc` | Select output text, pipe to tool |

### NORMAL mode bindings

```
k / ↑      scroll chat up
j / ↓      scroll chat down
gg         scroll to top
G          scroll to bottom
/          search in chat history
n / N      next / prev search match
y          yank selection to clipboard
p          paste clipboard into input
u          undo last agent tool action
:          command palette (same as Ctrl+P)
```

### Command palette (`:` or `Ctrl+P`)

```
:model     open model picker
:clear     clear chat history
:save      save session to file
:load      load session from file
:run       run last code block
:watch     toggle watch mode on artifact
:undo      undo last tool mutation
:diff      show last file diff
:settings  open settings overlay (same as Ctrl+,)
:theme     quick theme toggle
:project   switch to project mode
:chat      switch to chat-only mode
:help      show help overlay
```

---

## 12. Mode Switching

| Mode         | Trigger              | Layout change |
|--------------|----------------------|---------------|
| Chat         | `--chat` / `:chat`   | Single pane: chat only |
| Project      | `--project` / `:project` | Three-pane full layout |
| Inline edit  | `:edit <file>`       | File opens in tree pane, diff in artifact pane |
| Artifact     | `:run` / tool trigger | Right pane activates with runner output |
| Settings     | `Ctrl+,`             | Overlay on current layout |
| Model picker | `Ctrl+M`             | Overlay on current layout |
| Palette      | `Ctrl+P` / `:`       | Overlay input at bottom |

Mode is persisted per-session in `~/.config/mauler/session.toml`.

---

## 13. Profiles & Config

### File locations

```
~/.config/mauler/
  settings.toml      global settings (all Ctrl+, fields)
  profiles.toml      model profiles (editable in-app)
  keybindings.toml   keybinding overrides
  session.toml       last session state (mode, open files)
  mauler.log         debug log
```

### profiles.toml (full schema)

```toml
[profiles.qwen3-6-27b]
name        = "qwen3.6-27b"
backend     = "llamacpp"           # llamacpp | lmstudio | anthropic
base_url    = "http://localhost:8080/v1"
model_id    = "qwen3.6-27b"
ctx_tokens  = 32768
thinking    = true
mmproj      = ""                   # path to mmproj.gguf for vision, empty = auto-detect

  [profiles.qwen3-6-27b.thinking_general]
  temperature      = 1.0
  top_p            = 0.95
  top_k            = 20
  min_p            = 0.0
  presence_penalty = 1.5
  max_tokens       = 4096

  [profiles.qwen3-6-27b.thinking_coding]
  temperature      = 0.6
  top_p            = 0.95
  top_k            = 20
  min_p            = 0.0
  presence_penalty = 0.0
  max_tokens       = 8192

  [profiles.qwen3-6-27b.nothinking]
  temperature      = 0.7
  top_p            = 0.8
  top_k            = 20
  min_p            = 0.0
  presence_penalty = 1.5
  max_tokens       = 4096

[profiles.lmstudio-default]
name       = "lmstudio-default"
backend    = "lmstudio"
base_url   = "http://localhost:1234/v1"
model_id   = ""                    # empty = use whatever LM Studio has loaded
ctx_tokens = 16384
thinking   = false

  [profiles.lmstudio-default.nothinking]
  temperature      = 0.7
  top_p            = 0.9
  top_k            = 40
  min_p            = 0.0
  presence_penalty = 0.0
  max_tokens       = 2048

[profiles.claude-sonnet]
name       = "claude-sonnet"
backend    = "anthropic"
base_url   = "https://api.anthropic.com"
model_id   = "claude-sonnet-4-6"
ctx_tokens = 200000
thinking   = false
api_key_env = "ANTHROPIC_API_KEY"

  [profiles.claude-sonnet.nothinking]
  temperature      = 0.7
  top_p            = 0.9
  top_k            = 0
  min_p            = 0.0
  presence_penalty = 0.0
  max_tokens       = 8192
```

---

## 14. Qwen3.6-27B Setup

### Model facts

- Released April 22, 2026 by Alibaba
- Dense 27B (no MoE), multimodal (text + image + video)
- Native context: 262,144 tokens (extendable to 1,010,000 via YaRN)
- SWE-bench Verified: 77.2% (beats Qwen3.5-397B MoE at 76.2%)
- Ships as two files: `model.gguf` + `mmproj-F16.gguf` (vision projector)

### Quant selection (RTX 3090 — 24 GB VRAM)

KV cache consumption at 32K context is roughly 4–5 GB on top of model weight.
This constrains the safe upper bound on 24 GB.

| Quant       | Model size | + KV @32K | Total   | Safe on 3090? | KLD mean | Notes |
|-------------|------------|-----------|---------|---------------|----------|-------|
| **UD-Q4_K_XL** | ~17 GB  | ~4 GB     | ~21 GB  | ✓ yes — 3 GB headroom | 0.0227 | **Default. SOTA 4-bit.** |
| Q4_K_M      | 16.8 GB    | ~4 GB     | ~21 GB  | ✓ yes         | similar  | Widest compat if UD unavail |
| Q5_K_M      | ~20 GB     | ~4 GB     | ~24 GB  | ⚠ tight       | lower    | Cap context at 16K to be safe |
| Q6_K        | 22.5 GB    | ~4 GB     | ~26 GB  | ✗ OOMs at 32K | lower    | Only safe at ≤8K context |
| Q8_0        | ~29 GB     | —         | —       | ✗ too large   | 0.0028   | Needs 40 GB+ |
| UD-Q2_K_XL  | ~11 GB     | ~4 GB     | ~15 GB  | ✓ lots of room| 0.0734   | Fallback for small VRAM |

**For this hardware: use `UD-Q4_K_XL`.** It gives near-Q6 quality (Unsloth's
dynamic upcasting of critical layers) while staying safely under 24 GB at 32K
context. Q6_K sounds tempting but will OOM mid-session as KV cache grows.

Unsloth Dynamic quants upcast important layers to 8/16-bit — better than standard
quants at same file size. Source: `unsloth/Qwen3.6-27B-GGUF` on Hugging Face.

### Download

```bash
# Recommended: UD-Q4_K_XL with vision projector
pip install huggingface_hub
hf download unsloth/Qwen3.6-27B-GGUF \
  --local-dir ~/models/qwen3.6-27b \
  --include "*UD-Q4_K_XL*" "*mmproj-F16*"
```

### Standard llama.cpp server

```bash
./llama-server \
  -m ~/models/qwen3.6-27b/qwen3.6-27b-UD-Q4_K_XL.gguf \
  --mmproj ~/models/qwen3.6-27b/mmproj-F16.gguf \
  --jinja \
  -c 32768 -ngl 99 -fa \
  --temp 1.0 --top-p 0.95 --top-k 20 --min-p 0.0 --presence-penalty 1.5 \
  --chat-template-kwargs '{"preserve_thinking":true}'
```

Disable thinking per request: `--chat-template-kwargs '{"enable_thinking":false}'`
(TheMauler sends this per-request based on the active profile's `thinking` flag.)

### MTP (Multi-Token Prediction) — future / optional

MTP delivers 1.5–2× faster generation with no accuracy loss by using the model's
own internal draft heads. **However:**

- **LM Studio does not support MTP.** The server it runs is standard llama.cpp
  release — no `--spec-type mtp` flag available.
- **Standard llama.cpp releases do not include MTP yet.** It lives in a custom
  branch (`mtp-clean` by am17an, PR #22673) that has not been merged to main.

**So MTP is not available through LM Studio or a stock llama.cpp install.**
TheMauler's inference bridge does not need to do anything special for it — if you
later build the MTP branch yourself, it runs transparently server-side and the
client sees no difference.

When to revisit: once PR #22673 lands in llama.cpp mainline, it will surface in LM
Studio builds too. At that point, add `--spec-type mtp --spec-draft-n-max 3` to
your server launch and download `unsloth/Qwen3.6-27B-MTP-GGUF` instead of the
standard GGUF. No client-side changes needed.

```bash
# Future reference — only valid with custom MTP branch build
./llama-server \
  -m ~/models/qwen3.6-27b-mtp-UD-Q4_K_XL.gguf \
  --mmproj mmproj-F16.gguf \
  --jinja -c 32768 -ngl 99 -fa \
  --spec-type mtp --spec-draft-n-max 3 \
  --temp 1.0 --top-p 0.95 --top-k 20 --min-p 0.0 --presence-penalty 1.5 \
  --chat-template-kwargs '{"preserve_thinking":true}'
```

### LM Studio setup

1. Open LM Studio → Model search
2. Search `unsloth/Qwen3.6-27B-GGUF`
3. Download `UD-Q4_K_XL` (17 GB) or `Q4_K_M` (16.8 GB)
4. Load model → Start local server (port 1234)
5. Enable Thinking toggle in LM Studio chat settings
6. TheMauler profile: set `backend = "lmstudio"`, `base_url = "http://localhost:1234/v1"`

### Known issues

- **Do not use CUDA 13.2** — produces garbage output. NVIDIA working on fix.
- **Ollama does not work** with Qwen3.6 GGUFs (separate mmproj file). Use llama.cpp or LM Studio.
- Jinja C++ template quirks: use `--jinja` flag. Community-patched templates avoid
  `|items` / `|safe` filters not supported in llama.cpp's Jinja runtime.
- Full 1M context via YaRN adds 20–40 GB extra KV cache. Keep context ≤ 64K for
  normal use.

---

## 15. File Layout

```
TheMauler/
│
├── cmd/
│   └── mauler/
│       └── main.go
│
├── internal/
│   ├── agent/
│   │   ├── loop.go
│   │   ├── dispatch.go
│   │   ├── history.go
│   │   └── rollback.go
│   │
│   ├── artifact/
│   │   ├── runner.go
│   │   ├── sandbox.go
│   │   └── output.go
│   │
│   ├── context/
│   │   ├── loader.go
│   │   ├── budget.go
│   │   ├── compactor.go
│   │   └── ignore.go
│   │
│   ├── image/
│   │   ├── paste.go
│   │   ├── encode.go
│   │   ├── display.go
│   │   └── wslpath.go
│   │
│   ├── llm/
│   │   ├── client.go
│   │   ├── stream.go
│   │   └── backends/
│   │       ├── lmstudio.go
│   │       ├── llamacpp.go
│   │       └── anthropic.go
│   │
│   ├── settings/
│   │   ├── model.go
│   │   ├── load.go
│   │   ├── save.go
│   │   ├── editor.go
│   │   └── validator.go
│   │
│   ├── tools/
│   │   ├── registry.go
│   │   ├── read_file.go
│   │   ├── write_file.go
│   │   ├── edit_file.go
│   │   ├── bash.go
│   │   ├── glob.go
│   │   ├── grep.go
│   │   ├── code_run.go
│   │   ├── web_search.go
│   │   ├── image_read.go
│   │   ├── symbol_jump.go
│   │   └── diff_view.go
│   │
│   └── ui/
│       ├── app.go
│       ├── pane_tree.go
│       ├── pane_chat.go
│       ├── pane_artifact.go
│       ├── overlay_settings.go
│       ├── overlay_palette.go
│       ├── overlay_model.go
│       ├── input.go
│       ├── status.go
│       └── theme.go
│
├── profiles.toml.example
├── MAULER.md.example
├── go.mod
└── go.sum
```

---

## 16. Dependency List

```
github.com/charmbracelet/bubbletea      TUI framework
github.com/charmbracelet/lipgloss       styling
github.com/charmbracelet/bubbles        input, list, spinner components
github.com/charmbracelet/harmonica      animation easing (status transitions)
github.com/alecthomas/chroma/v2         syntax highlighting
github.com/sergi/go-diff                unified diff generation + rendering
github.com/BurntSushi/toml              config read/write
github.com/spf13/cobra                  CLI flags
github.com/mattn/go-sixel               sixel image encoding
github.com/smacker/go-tree-sitter       tree-sitter bindings
github.com/fsnotify/fsnotify            file watch (artifact watch mode)
github.com/atotto/clipboard             clipboard fallback
golang.org/x/term                       terminal size detection
```

All pure Go or cgo-minimal. No Electron, no GUI framework, no CGO beyond tree-sitter
(optional — symbol_jump degrades gracefully if not built).

---

## 17. Build Order

Phase 0 — Skeleton
  - go.mod, cobra CLI, config loader, profiles.toml parser
  - Basic bubbletea app loop, blank panes

Phase 1 — LLM client
  - llm.Client interface
  - llamacpp backend (streaming OpenAI-compat)
  - Single-pane chat with streaming render
  - Smoke test: chat with Qwen3.6-27B

Phase 2 — Agent tools
  - Tool interface + registry
  - read_file, write_file, edit_file, bash, glob, grep
  - Tool dispatch in agent loop
  - Confirm gate (y/N prompts)
  - Rollback stack + /undo

Phase 3 — Settings editor
  - Settings struct + TOML load/save
  - Ctrl+, overlay: Model, Thinking, Generation sections
  - Live apply (next request picks up changes)
  - Profiles manager (list, edit, new, delete)

Phase 4 — Image support
  - Clipboard paste detection (xclip / wl-paste / PowerShell bridge)
  - base64 encode for API
  - Sixel display in chat pane
  - WSL path translation

Phase 5 — Three-pane layout
  - File tree pane (fsnotify-backed)
  - Artifact pane
  - Pane focus / resize

Phase 6 — Artifact runner
  - Subprocess manager, language dispatch
  - Streaming output to artifact pane
  - code_run tool
  - Watch mode

Phase 7 — Context engine
  - MAULER.md discovery + parse
  - Token budget tracking
  - Rolling compaction
  - .maulerignore

Phase 8 — Polish
  - lmstudio + anthropic backends
  - Modal input (NORMAL/VISUAL modes)
  - Command palette
  - Kitty graphics protocol fallback
  - Symbol jump (tree-sitter)
  - Session save/load
  - Remaining settings sections (UI, keybindings, debug)

---

## 18. Key Bindings Reference

```
Global
  Ctrl+,        open settings editor
  Ctrl+M        model picker
  Ctrl+P / :    command palette
  Ctrl+E        run last code block in artifact pane
  Ctrl+Z        undo last tool mutation
  Tab           cycle pane focus
  ?             help overlay
  Ctrl+C        quit

Chat pane (INSERT mode)
  Enter         send message
  Shift+Enter   newline in input
  Ctrl+V        paste image from clipboard
  Esc           switch to NORMAL mode
  ↑/↓           scroll through input history

Chat pane (NORMAL mode)
  i             back to INSERT
  k/j           scroll up/down
  gg / G        top / bottom
  /             search chat
  n / N         next / prev match
  y             yank to clipboard
  u             undo tool action

Settings overlay
  ↑/↓           navigate fields
  Enter         edit field
  Esc           cancel / close
  Tab           next section
  Ctrl+S        save now
  r             reset field to default
  /             search fields

File tree pane
  ↑/↓           navigate
  Enter         open file in chat context
  Space         toggle expand directory
  e             open in $EDITOR
```

---

*Sources: [Unsloth Qwen3.6 docs](https://unsloth.ai/docs/models/qwen3.6) ·
[llama.cpp MTP PR #22673](https://github.com/ggml-org/llama.cpp/pull/22673) ·
[unsloth/Qwen3.6-27B-GGUF](https://huggingface.co/unsloth/Qwen3.6-27B-GGUF) ·
[bartowski/Qwen_Qwen3.6-27B-GGUF](https://huggingface.co/bartowski/Qwen_Qwen3.6-27B-GGUF) ·
[Qwen3.6-27B review](https://www.buildfastwithai.com/blogs/qwen3-6-27b-review-2026) ·
[willitrunai VRAM guide](https://willitrunai.com/blog/qwen-3-6-27b-vram-requirements)*
