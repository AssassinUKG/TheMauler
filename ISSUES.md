# TheMauler — Feature Backlog

The original 10 UI features (bottom table) are implemented. Active work continues in the
regression log (R-series) and UI Ops tracker (U-series) below — see each row's Status.

---

## Active Regression Log - 2026-05-28

These are the current issues to fix before adding new agent foundation features.

| # | Issue | Evidence | Status |
|---|-------|----------|--------|
| R1 | Tool calls are being written as plain assistant text instead of emitted as actual tool calls. | Chat transcript shows `glob("**/*.go")` after the model says it will explore files; no matching tool call appears. Recent task logs also show several zero-tool runs for prompts that should inspect files. Direct backend probe returned assistant `content` containing `<tool_call>{"pattern":"**/*.go"}` even with `tool_choice:"required"`. | Fixed: repaired function-style tool text, Qwen `<tool_call><function=...><parameter=...>` blocks, and malformed single-JSON `<tool_call>{...}` blocks into structured tool calls. |
| R2 | Access preset/toolset state is confusing and may make tools look unavailable. | `settings.toml` had `agents.offline_only = true`, `tools.active_toolset = "offline"`, and browser tools disabled while the Agent UI screenshot highlighted Offline/Balanced area. | Fixed locally: switched saved config to Balanced and re-enabled read-only browser tools. Doctor now reports the active access preset/toolset. |
| R3 | Provider/backend reliability needs clearer diagnosis. | Recent task logs include `model_load_error`, `stream_error`, and `chat_error` from the local OpenAI-compatible backend. `/logs` and `/log` returned 404; `/props`, `/slots`, `/health`, and `/v1/health` worked. `/props` reports `chat_format:"Content-only"` and `/v1/models` reports `template_source:"builtin:fallback"`. | Improved: Doctor now warns when llama.cpp reports `chat_format=Content-only` or a fallback template, because native `tool_calls` may be emitted as text. |
| R4 | Stale todo state leaks between unrelated work. | `~/.config/mauler/todos.json` still contained an old Imagescrub checklist while the current workspace is TheMauler. | Fixed locally: cleared the stale active checklist. Durable workspace-scoped todo storage remains a future polish item. |
| R5 | Doctor exists but is too hidden for this kind of failure. | Backend `RunDoctor` and Agent panel overlay exist, but health findings are not surfaced as a first-class readiness/debug flow. | Partially improved: added Doctor checks for tool-call format and access preset. A full Health view remains planned. |
| R6 | Native tool-call compatibility is brittle across llama.cpp versions. | Recent llama.cpp issues mention `function.arguments` sometimes being returned as an object instead of an OpenAI-compatible JSON string. Mauler also sent assistant tool-call history back with `arguments` as raw JSON. | Fixed: SSE parsing now accepts string or object `arguments`, and outbound assistant tool-call history serializes `arguments` as an OpenAI-compatible string. |
| R7 | InferenceBridge was launching Qwen without llama.cpp Jinja/template mode. | Live process command line lacked `--jinja`, `/props` reported `chat_format:"Content-only"`, and `/v1/models` reported `template_source:"builtin:fallback"` for Qwen3.6-27B. This matches the backend returning textual `<tool_call>{"pattern":"**/*.go"}` instead of structured tool calls; after a Jinja reload, a probe returned fenced `{ "tool": "glob", "input": ... }` JSON as assistant text. | Fixed in `C:\Users\richa\Documents\InferenceBridge`: Qwen managed loads now force `--jinja`, the active AppData config has `use_jinja = true`, Qwen tool prompts specify the exact XML format, and the parser repairs bare JSON `<tool_call>` plus fenced tool/input JSON output. TheMauler mirrors the fenced JSON repair. Live model was reloaded with `use_jinja=true`, but the source-level InferenceBridge parser fixes require rebuilding/restarting the app binary to affect API responses. |
| R8 | Blank TheMauler run after asking "whats in this repo?". | Latest task run `task-2026-05-28T19-40-56+01-00` had `status:"done"` but no `response`, no `summary`, and no tools. A streamed tool probe showed InferenceBridge emitted only whitespace plus hidden Qwen tool markup; the streaming normalizer then failed to extract it because Qwen stopped after `</parameter>` without closing `</function>`. The provider also streams reasoning as `delta.reasoning`, not `delta.reasoning_content`. | Fixed: TheMauler now treats empty model output as recoverable/blocked instead of success, listens for `delta.reasoning`, and preserves assistant tool-call history even when content is empty. InferenceBridge now accepts Qwen function blocks cut at end-of-text, emits streamed `tool_calls`, and passes `--flash-attn on` for current llama-server. Rebuilt and relaunched both production binaries; streamed probe now returns a real `glob` tool call. |
| R9 | InferenceBridge leaked prior OpenAI tool-call history as literal `[tool_calls]` prompt text. | Latest run `task-2026-05-28T20-27-09+01-00` successfully called `read_file`, then the next assistant message displayed `[tool_calls] [{"function":...}]` as normal chat text and no second tool event. The bridge normalized assistant `tool_calls` into a plain `[tool_calls]` marker inside the rendered prompt, so Qwen copied the marker instead of emitting a new tool call. The first parsed Qwen path also contained a leading newline (`"\nmain.go"`). | Fixed in InferenceBridge: assistant tool-call history now renders as profile-correct Qwen/Hermes tool markup, tool results render as Qwen `<tool_response>` history for Qwen profiles, Qwen XML parameters trim template newline padding, and `/v1/debug/logs?limit=N` now exposes the headless log buffer. Rebuilt release, restarted bridge, reloaded Qwen with Jinja, and verified non-streaming plus SSE probes both return real `read_file` tool calls with clean `internal/app/app.go` arguments. |
| R10 | Workspace path was applied to file tools but stale chat/system context still made Qwen ask for files from the previous project. | Explorer showed the image-scrub project with `app.py`, `idea.md`, and `requirements.txt`, but the model called `read_many` on `main.go`, `wails.json`, `internal/app/app.go`, and `frontend/src/App.tsx`; the file tools correctly returned not-found from the selected project. | Fixed: workspace changes through Explorer or Settings now apply cwd, persist the path, and clear active chat/tool context plus rollback state. New runs stamp the current workspace root/top-level files into the system prompt, and missing-file tool results include the current workspace plus a glob/read hint. Regression tests cover workspace switching, Settings `workspace_dir`, prompt stamping, and missing-path hints. |
| R11 | Local-provider compatibility should track current LM Studio/llama.cpp tool and usage APIs. | Current LM Studio and llama.cpp APIs expose streaming usage, parallel tool-call controls, llama.cpp `parse_tool_calls`, LM Studio model capabilities/reasoning metadata, and experimental llama.cpp server-side built-in tools. TheMauler already repairs many local-model tool formats but should request native behavior explicitly where safe. | In progress: OpenAI-compatible requests now send `stream_options.include_usage=true`, `parallel_tool_calls=false` when tools are present, and `parse_tool_calls=true` for llama.cpp tool requests. Doctor now reads LM Studio native model metadata for tool/function capability, reasoning metadata, and loaded context length. Doctor also warns if dangerous llama.cpp server-side file/shell built-ins appear in `/props`. Default providers include SGLang/vLLM OpenAI-compatible presets, launch notes are documented in `MAULER.md`, Qwen thinking defaults now use `presence_penalty=0.0`, Hermes JSON tool-call repair is regression-tested, mutation verification is in place, tool-result guardrails have a first pass, and bounded subagent tools are implemented. Remaining: LSP diagnostics, worktree isolation, and continued Doctor/UI polish. |
| R12 | Mutating tool calls need post-write verification before the agent continues. | P3 review found the app only appended syntax lint output after writes, and rollback snapshots were only taken on the confirmation path, not every autonomous write/edit. That left autonomous mutations with weaker rollback/verification guarantees. | Fixed: write/edit tools now snapshot before execution whenever known mutating tools run, then verify successful mutations on disk. `write_file` checks exact content or append suffix; `edit_file` checks `new_string` landed and warns if `old_string` remains. Existing Go/Python/shell lint still runs after verification. Regression tests cover write, append, edit, and mismatch detection. |
| R13 | Shell failures were hiding the useful stderr/stdout and PowerShell `curl` alias mistakes looked like generic exits. | Chat showed repeated shell results as only `error: exit code 1` for `curl -s ...` and `Invoke-WebRequest ...`, so the model/user could not see whether the failure was syntax, network, aliasing, or stderr output. On Windows PowerShell, `curl` may resolve to `Invoke-WebRequest`, where curl flags such as `-s` are invalid. | Fixed: failed tools now preserve captured stdout/stderr and append the exit error instead of replacing the output. PowerShell shell failures now add a hint when `curl` alias usage is likely, telling the model to use `curl.exe` or proper `Invoke-WebRequest -Uri ... -UseBasicParsing`. Regression tests cover output preservation and curl-alias hints. |
| R14 | Promptware or secret-bearing tool output can be fed straight back into the model. | Fetched docs, browser extracts, repo files, and shell output may include hostile text such as "ignore previous instructions" or credential-looking assignments. Hermes-style agents need a clear tool-output trust boundary before the next model turn. | Fixed first pass: every successful or failed tool result now passes through guardrails before being appended to model history or logged to chat. Suspicious prompt-injection/exfiltration language is labelled as untrusted data, and obvious credential assignments/private-key blocks are redacted. Regression tests cover prompt-injection warnings, assignment redaction, private-key redaction, and benign output passthrough. |
| R15 | UI still hides important agent health/state behind secondary surfaces. | The Agent panel has logs and Doctor controls, but live run phase, diagnostics, collapsed panes, and guardrail notices are not prominent enough during failure debugging. | Fixed first pass: added live `mauler:run_state` in the bottom status bar, a top-bar Doctor action, collapsed Explorer/Agent rails, and clearer chat/log guardrail styling. |
| R17 | Plain greetings can still trigger arbitrary tool calls from local Qwen. | A simple "hello" turn called skill tools and only then answered. `tool_choice:"none"` was not enough because local stacks still saw the tool schema and drifted toward available skills. | Fixed: conversational first turns now send no tool definitions at all. The detection covers general small talk and Q&A while still allowing short task prompts such as "fix bug", "run tests", "read README.md", and "list files" to use tools. |
| R16 | Subagent delegation is only a plan, not a bounded runtime capability. | Tracker called for focused Researcher, Reviewer, Test/Fix, and Summarizer runners with explicit profile, toolset, timeout, context budget, and output contracts. | Fixed first pass: added `subagent_research`, `subagent_review`, `subagent_testfix`, and `subagent_summarize` tools. Each uses scratch history, bounded turns/tool calls/time, an explicit toolset, current workspace context, guardrailed tool results, and a fixed report contract. |
| R18 | Backend inference failures can be misreported and subagent loads can downshift a shared local backend. | Latest run `task-2026-06-03T19-10-00+01-00` ended after InferenceBridge returned `HTTP 500` for `http://127.0.0.1:20688/completion`, but the task log kept an older `tool_disabled` stop reason from an earlier `web_search` attempt. InferenceBridge logs show the parent loaded Qwen at 32768 ctx, then a subagent/request reloaded the same model at 24576 ctx with thinking disabled. | Fixed first pass: terminal model/client errors override older recoverable stop reasons; InferenceBridge records post-failure health/slot/props/stderr diagnostics; same-model lower/equal context requests reuse the larger loaded backend instead of reloading down; Mauler logs runtime mismatches before generation; subagents no longer shrink backend launch ctx; pre-output inference failures get one bounded retry; Doctor warns about shared-backend subagent risk. |
| R19 | Master workflow skills can consume too much context when registered. | Registering a large external master skill caused the bottom token bar to jump because the source could be appended into chat/system context instead of being retrieved only when needed. | Fixed: master/project workflow sources are registered as lazy external skills. The system prompt now carries source metadata only, `skill_view master` returns an outline by default, and focused queries return capped matching excerpts. Regression tests cover no auto-injection and focused external-source retrieval. |
| R20 | The live agent flow needs a first-class ops surface. | Reference UI review on 2026-06-11 showed useful patterns from an agent workbench: live KPIs, operational trace, memory/facts sidebar, operator console, and lab/workspace cards. TheMauler already captures run state, activity, task logs, memory, terminal output, todos, and lab context, but these are split across Chat, Logs, Memory, Agent, and Terminal. | First pass landed: center `Ops` view shows current phase, objective, KPIs, recent tool activity, latest run timeline, workspace/lab context, and next-step/fact style side panels. Follow-ups: live facts extracted during a run, richer terminal/command cards, browser screenshot previews, run replay/debug view, and richer lab run cards. |

## UI Ops Update Tracker - 2026-06-11

| # | Item | Status |
|---|------|--------|
| U1 | Live Ops Center tab for watching the agent in flow | First pass done |
| U2 | Agent operational trace with outcome badges | First pass done |
| U3 | Live memory/facts/open-questions panel | First pass done |
| U4 | Run KPI strip: elapsed, tools, edits, tests, tokens, recoveries | Planned |
| U5 | Workspace/lab run cards with target, shell, agent root, latest artifact | Planned |
| U6 | Operator console cards: AI commands, pinned artifacts, pause/take-over | Planned |
| U7 | Optional compact Ops visual density mode | Planned |
| U8 | Shell timer cancel button and cleaner shared-terminal command display | First pass done |
| U9 | Expand web/exploit research budgets and guidance | First pass done |
| U10 | Harden shared-terminal scan execution with pipefail and no-head scan guidance | First pass done |
| U11 | Fix shared-terminal DONE marker parsing and wrapper leakage in tool results | First pass done |
| U12 | Treat requested README/writeup/docs updates as living milestone artifacts | First pass done |
| U13 | Durable milestone memory and resume flow for HTB/lab/coding runs | First pass: run-end milestone memory landed |
| U14 | Normalize shell tool command escaping before UI/log/shared-terminal execution | Fixed: shell entities decoded before command events, logs, confirmation, and execution |
| U15 | Treat "hack the box" / user-root lab prompts as deep exploit research for web budgets | Fixed: spaced HTB and user/root language now triggers boosted research budget |
| U16 | Prevent doc-required runs from finishing green without write/edit | Fixed: blocked run status plus file-tool-only documentation recovery turn |
| U17 | Prune stale/moved workspace folders from Explorer instead of resurrecting them | Fixed: missing open-folder paths are filtered and config retargeted to moved HTB folder |
| U18 | Keep registered master skill source internal instead of permanently displaying absolute folder paths | Fixed: UI, summaries, and skill_view labels hide full source path while lazy loading still works |

## Durable Run Memory Plan - 2026-06-11

Goal: make TheMauler reliably remember what it did, what it found, and where to resume, especially for long HTB/lab runs and documentation-driven tasks.

| # | Item | Status |
|---|------|--------|
| M1 | Detect meaningful milestones from tool/run events: recon complete, service found, creds found, shell gained, user flag found, privesc lead, root flag, tests passed, doc updated | First pass done: recon, web research, fetched sources, file updates, flag-path sightings, stop reasons, and resume hints |
| M2 | Auto-save concise project memory after milestones, scoped to the active workspace and tagged by target/box/repo | First pass done: task completion now writes concise workspace-scoped memory entries |
| M3 | Treat requested writeups/READMEs/notes as living artifacts and require a doc update after each major verified milestone | First prompt pass done |
| M4 | Add run-end summary memory: done, blocked reason, next action, important paths/artifacts, commands worth reusing | First pass done: status, stop reason, target, milestones, and next action are persisted |
| M5 | Add an Ops/Memory card showing latest remembered facts, open questions, touched files, current target, and resume hints | Partial: Ops side panel first pass done |
| M6 | Add a "Resume Last Run" helper that pulls latest task log, active todos, project memory, and named writeup sections into a compact prompt | Planned |
| M7 | Add tests that milestone memory does not store secrets unless explicitly allowed, and that memories are workspace/target scoped | Partial: redaction and target-tag tests added |

| # | Feature | Status |
|---|---------|--------|
| 1 | Markdown preview in FileViewer | ✅ Done |
| 2 | FileTree right-click context menu | ✅ Done |
| 3 | Inline chat search (Ctrl+F) | ✅ Done |
| 4 | Token budget warning toast | ✅ Done |
| 5 | FileTree file icons by type | ✅ Done |
| 6 | Session auto-save on every assistant message | ✅ Done |
| 7 | Drag file from FileTree into chat input | ✅ Done |
| 8 | Terminal panel (persistent shell below editor) | ⏭ Deferred |
| 9 | Multi-file tabs in center pane | ✅ Done |
| 10 | Agent task queue (queue multiple prompts) | ✅ Done |

---

## Implementation notes

**1 · Markdown preview** — `FileViewer.tsx`: toggle Preview/Edit button when `language === 'markdown'`;
renders via `react-markdown + remark-gfm`. Three-way render: preview / diff / editor.

**2 · Context menu** — `FileTree.tsx` complete rewrite: right-click opens positioned menu with Copy path,
Rename (inline input), New file, New folder, Delete. Go bindings: `RenameFile`, `DeleteFile`, `CreateFile`, `CreateDir`.
CSS in `FileTree.css`: `.ctx-menu`, `.ctx-danger`, `.ctx-divider`, `.file-rename-input`, `.file-tree-create`.

**3 · Chat search** — `ChatPane.tsx`: `Ctrl+F` toggles search bar at top; filters `visibleMessages` by
substring. Count shown as `N / total`. `Escape` dismisses.

**4 · Budget toast** — `Toast.tsx` + `Toast.css` new components. `App.tsx` checks `GetHistoryStats()` on
each `statsVersion` bump; fires warn toast at 75%, danger toast at 90%. Auto-dismisses after 6 s.
Deduped by threshold key so it doesn't re-fire until fraction drops below the trigger.

**5 · File icons** — embedded in FileTree rewrite: `fileIcon()` maps ~20 extensions to emojis
(🔷 ts/tsx, 🟨 js/jsx, 🐹 go, 🐍 py, 📝 md, {} json, 🎨 css, 🌐 html, ⚡ sh/ps1, 🖼️ images, 📁 dir, …).

**6 · Auto-save** — `app.go`: `autoSave()` helper calls `SaveSession("_autosave")` silently.
Called at the end of every `runAgentLoop` defer, so `_autosave` always holds the last agent run.

**7 · Drag to chat** — `FileTree.tsx`: file nodes are `draggable`, `dragStart` sets `text/plain` = path.
`ChatPane.tsx`: textarea `onDrop` reads path; images are encoded via `EncodeFileBase64` and attached;
text files are appended as `@path` to the input.

**8 · Terminal panel** — Deferred: requires new Go bindings (`OpenShell`, `ShellInput`, `ShellClose`),
PTY/pipe management, and xterm.js or equivalent frontend. Scoped for a future session.

**9 · Multi-file tabs** — `App.tsx`: replaced `openFile: OpenFile | null` with `openFiles: OpenFile[]`
+ `activeFileIdx`. `openOrFocusFile()` deduplicates by path. Tabs strip shows each open file with × close
button. `FileViewer` receives `openFiles[activeFileIdx] ?? null` unchanged.

**10 · Task queue** — `App.tsx`: `taskQueue` state. `ChatPane` sends immediately when idle, enqueues when
streaming. Queued messages show `[Queued] …` in chat. On `stream_done`, first queued item is dequeued and
sent. Send button label shows `Queue (+N)` while streaming with pending items.
