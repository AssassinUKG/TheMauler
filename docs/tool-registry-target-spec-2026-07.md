# Target tool registry - Claude-shaped, Hermes-friendly (2026-07-02)

**Audience:** an AI coding agent (Codex/Claude) implementing the registry refactor.
**Goal:** reshape TheMauler's ~40-tool surface into ~22 orthogonal tools modeled on **Claude Code's
shape** (one dedicated tool per capability, no overlaps) using **Hermes function-calling
conventions** (clean snake_case names, strict JSON schema, minimal `required`) so the local models
(qwen3.6, gemma4) route correctly.

Companion docs: `docs/settings-loop-benchmark-audit-2026-07.md` (P1-1 asks for a lean default
agent/run toolset - this spec defines it) and `docs/ai-terminal-parity-and-tooling-fixes-2026-07.md`
(terminal verbs).

**Verify:**
```bash
go build ./... && go test ./internal/app/ ./internal/settings/ ./internal/tools/
```

---

## Current Status - 2026-07-06

Current Mauler direction:
- Keep one model-facing shell one-shot tool: `shell`.
- Keep interactive terminal as `terminal_send` + `terminal_read`; `terminal_send command` owns the old terminal-run shape.
- Do not expose legacy dropped names in prompts/toolsets. Existing old configs should migrate/repair into compact names.
- Keep the user's live provider config InferenceBridge-only; this registry work is about Mauler tools, not backend/provider expansion.

Done / partly done:
- Broad routing no longer advertises the old duplicate shell alias by default.
- Compact read/write/edit/todo/skill/task-style surfaces exist in current code paths.
- `terminal_send command` and `terminal_read history` have cleaner model-facing result contracts after the 2026-07-06 terminal fix.

Still open before calling the registry complete:
- Central migration/normalization tests for old enabled-tool names: `read_file`, `write_file`, `edit_file`, `todo_create`, legacy shell alias, and old browser/subagent names.
- Runtime arg repair for old local-model priors: `{"bash":"..."}` / `{"cmd":"..."}` / `{"powershell":"..."}` / `{"input":"..."}` into `shell.command`.
- Runtime arg sanitation for compact file paths containing XML-ish tags.
- `agent_eval` before/after comparison proving the smaller surface improves or at least preserves tool-selection accuracy.
- Review actual per-turn advertised tool count from `tool_routing` ledger events and keep the default run surface small.

---

## Design basis

- **Claude's shape:** rich but small; exactly one Read, one Bash, one TodoWrite, one Task
  (subagent) with a `type` arg. Never two tools that could answer the same request.
- **Hermes conventions:** the models emit `<tool_call>{"name":...,"arguments":{...}}</tool_call>`.
  They route best with (a) verb_noun snake_case names, (b) a one-sentence hint that names the
  sibling tool, (c) `additionalProperties:false` + minimal `required`, (d) enums for modes.
- **Keep snake_case** (not Claude's CamelCase): it matches the existing codebase and the local
  models' function-calling training. Adopt Claude's *structure*, not its casing.

Net: ~40 -> ~22 tools by **merging** eight families and **dropping** one duplicate. No capability is
lost - every current tool maps to a target tool or an argument on one.

---

## The target list (~22 tools)

### Files & code - modeled on Read / Write / Edit / Glob / Grep
| Target | Replaces | Hint (front-loaded trigger + sibling) | Key args |
|---|---|---|---|
| `read` | read_file, read_many, read_chunks, file_outline, read_pdf | Read file contents by path (text/PDF); supports line range and outline mode. Find files by name with `glob`, by content with `grep`. | `path` or `paths[]`, `offset`, `limit`, `mode:outline\|full` |
| `write` | write_file | Create a new file or fully replace one. For partial edits use `edit`/`apply_patch`. | `path`, `content` |
| `edit` | edit_file | Replace one exact string in a file. For multiple hunks/files use `apply_patch`. | `path`, `old`, `new`, `replace_all` |
| `apply_patch` | - | Apply a multi-file, multi-hunk patch in one atomic call. Preferred over `edit` whenever a change spans several files or several places in a file. | `patch` (Codex `*** Begin/Update/End Patch` format) |
| `glob` | glob | Find files by name/path pattern. Returns paths, not contents. | `pattern`, `path` |
| `grep` | grep | Search file contents by regex (ripgrep). | `pattern`, `glob`, `output_mode` |

### Shell & terminal - modeled on Bash (+ interactive extension for pentest)
| Target | Replaces | Hint | Key args |
|---|---|---|---|
| `shell` | shell, **bash (dropped)** | Run a short deterministic one-shot command; returns full output + exit code. `background=true` for long scans. Interactive/streaming -> `terminal_send`. | `command`, `background`, `timeout` |
| `terminal_send` | terminal_send, terminal_run | Type into the live shared terminal (keys + control/named keys) and return without waiting - REPLs, msfconsole, ssh/reverse shells, `[y/N]` prompts. Watch with `terminal_read`. | `keys`, `control`/`key`, `enter`, `wait_ms` |
| `terminal_read` | terminal_read | Look at the live terminal's current screen now. Poll a running/interactive command. | `lines`, `wait_ms` |
| `run_script` | run_script | Batch several registry tool calls programmatically in one turn (sandboxed). Cuts round-trips. | `script` |

### Pentest (ethical) - domain tools, no Claude analog
| Target | Replaces | Hint |
|---|---|---|
| `http_probe` | http_probe | Send a crafted HTTP request; returns structured status/headers/body. Prefer over `curl` in shell for web checks. |
| `start_listener` | start_listener | Start the configured reverse-shell listener (Windows ncat, VPN LHOST) BEFORE firing a payload; trigger via webshell/`shell`, catch with `terminal_read`. |
| `evidence_bundle` | evidence_bundle | Save verified findings/artifacts (creds, paths, flags) to the case file. |

### Web & research - modeled on WebSearch / WebFetch
| Target | Replaces | Hint |
|---|---|---|
| `web_search` | web_search | Discover sources for an open question (CVEs, docs, syntax). Known URL -> `fetch_url`. |
| `fetch_url` | fetch_url | Fetch and read a specific known URL as text. |
| `browser` | browser_open, _snapshot, _click, _type, _extract, _screenshot, _close, _agent | Drive a real browser for JS-heavy/interactive pages via an `action` arg (navigate/click/type/extract/screenshot). Heavy multi-step flows belong in `task{type:research}`. Not in the default lean run toolset. **Interim** - see the Chrome-integration future update below. |

> **FUTURE UPDATE - real Chrome integration (do not build now, keep the `browser` merge above as the interim path).**
> The `browser` tool above collapses the eight `browser_*` verbs into one and is the near-term
> target. The longer-term goal is a **fully integrated browser the AI drives directly**, so the model
> can research, log into authenticated targets, and interact with web apps the way a human does.
> Two viable routes (pick when this is scheduled):
> 1. **Chrome extension + native-messaging bridge (recommended).** A small MEV3 extension the user
>    installs in their own Chrome, talking to TheMauler over Chrome native messaging (or a localhost
>    WebSocket). The agent drives the user's real, already-logged-in browser profile - cookies,
>    sessions, 2FA all intact - which is exactly what pentest/research workflows need. Mirrors how
>    Claude's "claude-in-chrome" MCP works. Same one `browser` tool surface; only the backend
>    changes from a headless driver to the extension bridge.
> 2. **Embedded CDP/headless Chromium** (e.g. chromedp / go-rod) bundled with the app. Fully
>    self-contained and scriptable, but does not inherit the user's live sessions and is heavier to
>    ship.
> Keep the agent-facing tool name/shape (`browser` with an `action` arg) stable across both so the
> model's contract never changes - only the driver behind it. Tracks alongside the terminal VT work
> as the other "let the AI use the machine like a human" capability. Anchor for current code:
> `internal/tools/browser.go`, `internal/tools/browser_agent.go`.

### Planning, memory & state - modeled on TodoWrite (+ durable memory)
| Target | Replaces | Hint |
|---|---|---|
| `todo_write` | todo_create, _update, _done, _blocked, _list, _clear | Create/replace the task's to-do list in one call. |
| `progress` | progress | Append a durable progress note (survives compaction; resume point). |
| `memory` | memory | Save/recall long-term facts across sessions. |
| `read_tool_result` | read_tool_result | Re-read a large offloaded tool result by handle instead of re-running the command. |
| `file_changes` | file_changes | Show files changed this run (diff review). |
| `set_reasoning_effort` | set_reasoning_effort | Raise/lower reasoning depth for coming turns. (Only effective on a thinking-capable profile - see settings audit P0-2.) |

### Knowledge & data
| Target | Replaces | Hint |
|---|---|---|
| `skill` | skills_list, skill_view | List or open a saved skill (procedural playbook), via a `mode:list\|view` arg. |
| `sqlite` | sqlite_schema, sqlite_query | Inspect schema or run a read-only query, via a `mode:schema\|query` arg. |
| `session_search` | session_search | Search prior conversation/run transcripts. |

### Delegation - modeled on Claude's single `Task` tool
| Target | Replaces | Hint |
|---|---|---|
| `task` | subagent_explore, subagent_research, subagent_review, subagent_testfix, subagent_summarize | Delegate a bounded sub-task to a specialized agent via `type`; returns a summary, keeps main context clean. | `type:explore\|research\|review\|testfix\|summarize`, `task`, `context` |

**Total: 22 tools.** `apply_patch` is in the core set (not optional) - multi-file edits are common
enough in coding/pentest scripting that a local model benefits from one atomic patch call instead of
many single-hunk `edit` round-trips.

---

## Merge/drop math (current -> target)

| Current (count) | -> Target | Delta |
|---|---|---|
| read_file, read_many, read_chunks, file_outline, read_pdf (5) | `read` | -4 |
| todo_* (6) | `todo_write` | -5 |
| browser_* (8) | `browser` | -7 |
| subagent_* (5) | `task` | -4 |
| sqlite_schema, sqlite_query (2) | `sqlite` | -1 |
| skills_list, skill_view (2) | `skill` | -1 |
| shell, bash (2) | `shell` | -1 |
| terminal_run, terminal_send (2) | `terminal_send` | -1 |
| + apply_patch (new, core) | - | +1 |

~40 -> ~22. Every dropped name becomes an argument (`mode`/`type`/`action`) on its merged tool, so no
capability is removed - only the *decision surface* shrinks.

---

## Naming & schema conventions (Hermes-friendly - apply to every tool)

1. **verb_noun snake_case** names; no synonyms (`read`, not `read_file`+`read_many`).
2. **One-sentence hint**: trigger first, then "for X use Y instead" naming the sibling.
3. **Strict schema**: `"additionalProperties": false`, minimal `required`, `enum` for every mode arg.
4. **Structured result header**: first line machine-readable (`state`/`exit`/`next`) - already done for
   terminal tools; extend to `shell`, `http_probe`, `read`, `task`.
5. **One capability = one tool**: if two tools could answer the same ask, they must merge or their
   boundary must be a single obvious word.

---

## Toolset composition (which tools each preset exposes)

Define in `DefaultToolsets()` (`internal/settings/defaults.go:181`). Keep each preset small:

- **run-lean (new default agent/run toolset, ~16):** read, write, edit, apply_patch, glob, grep, shell,
  terminal_send, terminal_read, run_script, http_probe, start_listener, evidence_bundle, memory,
  progress, read_tool_result, todo_write, set_reasoning_effort, task. *(No browser/web at top level -
  reach them via `task{type:research}`.)*
- **web-research:** read, glob, grep, web_search, fetch_url, browser, memory, progress,
  read_tool_result, todo_write, task.
- **safe (read-only):** read, glob, grep, sqlite, session_search, skill, memory, progress,
  read_tool_result, todo_write.
- **unrestricted:** everything (kept for manual power use, not the agent default).

---

## Implementation anchors & order

1. **App-level tools:** `registerAppTools` (`internal/app/subagents.go:53`) - collapse the five
   `subagent_*` registrations into one `task` tool with a `type` arg (per-type configs stay in
   `subagentSpecs()`); fold `terminal_run` into `terminal_send`.
2. **tools package:** `internal/tools/registry.go` + the individual files (`read_file.go`,
   `read_many.go`, `read_pdf.go`, etc.) - implement `read`/`sqlite`/`skill`/`browser` as single tools
   with a `mode`/`action` arg; delete `bash.go` (alias to `shell`).
3. **Defaults & enabled map:** `internal/settings/defaults.go` (`EnabledTools`, `DefaultToolsets`) and
   the migration in `internal/settings/load.go` (`EffectiveEnabledTools` already links bash->shell;
   finish removing bash). Add a normalize step that rewrites old tool names in a loaded config to the
   merged names so existing `~/.config/mauler/settings.toml` keeps working.
4. **Tests:** update the tool-set assertions in `internal/app/app_test.go` (~`:941`, `:998`,
   `:3057`) and add a migration test that an old config with `read_file`/`todo_create`/`bash` maps to
   `read`/`todo_write`/`shell`.
5. **Grade:** run `internal/app/agent_eval.go` before/after - tool-selection accuracy should rise as
   the surface shrinks. Keep the real per-profile benchmark (settings audit P0-3) as the health gate.

## Acceptance

- Lean default runs expose ~16 tools (assert via the `tool_routing` run event `tool_count`).
- No two tools can answer the same request; every merged tool has a `mode`/`type`/`action` enum.
- An old `settings.toml` using pre-merge names loads and runs without manual edits.
- `agent_eval` tool-selection accuracy >= the 40-tool baseline.
