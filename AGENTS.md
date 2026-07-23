# TheMauler agent instructions

Read this file at the start of every repository session. It is the compact canonical instruction
source. Detailed state and history are deliberately routed through `docs/context/README.md`; the
complete pre-split handoff is preserved at
`docs/archive/agents-handoff-snapshot-2026-07-21.md`.

## Product and fixed constraints

TheMauler is a native Windows AI-agent workbench: Go 1.26 backend, Wails v2.12 desktop shell,
React/TypeScript/Vite frontend, Chromium WebView2, and Monaco editing. It is intended to feel like
VS Code plus an agentic coding/research workbench.

- The user's primary local inference path is InferenceBridge through an OpenAI-compatible provider,
  normally `http://127.0.0.1:8800/v1`.
- Keep new provider integration Mauler-side. Do not add new LM Studio, vLLM, or SGLang provider paths
  unless explicitly requested.
- Local inference remains the persistent default. OpenRouter is an optional, UI-configured one-task
  cloud boost and must reset after that task.
- The machine has an RTX 3090 with 24 GB VRAM. Prefer Q5/Q4-class local profiles at useful context;
  never recommend Q6_K because it OOMs at useful context on this hardware.
- File ingestion and prompt context are different limits. Mauler should be able to read/index any
  supported file through bounded chunks/extractors; no request should silently stuff an entire large
  file into one model turn.
- Keep secrets out of React state, logs, prompts, exported evidence, and checked-in files. Provider
  keys belong in the existing secret store/environment precedence path.

## Commands and verification

Run commands from the repository root.

```powershell
.\setup.ps1 -Check
.\setup.ps1 -Auto
wails dev
.\build.ps1
.\build.ps1 -Run
cd frontend
npm run build
cd ..
go test ./...
go vet ./...
```

Linux/WSL equivalents are `wails dev`, `./build.sh`, `./build.sh --run`,
`./build.sh --skip-tests`, and `./build.sh --skip-deps`.

For non-trivial backend changes, the normal gate is:

```powershell
go test ./... -count=1
go vet ./...
go test -race ./internal/app ./internal/tools -count=1
npm run --prefix frontend build
wails build
```

Run narrower relevant tests first. Repository-wide frontend lint is a known pre-existing non-gate
with errors in generated bindings and older components; do not report it as green. Production output
is `build/bin/TheMauler.exe`.

## Architecture map

- `main.go`: Wails entry and embedded frontend.
- `internal/app`: Wails bindings, agent loop, prompts, channels, logging, diagnostics, providers, and
  orchestration.
- `internal/controlplane`: sealed task contract and authoritative executable phase machine.
- `internal/agent`: history/compaction and rollback.
- `internal/llm`: request/stream contracts and provider backends.
- `internal/settings`: persisted settings, profiles/providers, migrations, and provider secrets.
- `internal/tools`: compact tool implementations, path normalization, policy metadata, and registry.
- `internal/ledger`, `internal/store`: immutable run/event spine and SQLite persistence.
- `internal/engagement`, `internal/packlibrary`: authorised security-engagement state and packs.
- `frontend/src/App.tsx`: root state/event wiring.
- `frontend/src/components`: Chat, File/Monaco, Agent, Settings, Terminal, Logs, Memory, Engagement,
  and related workbench surfaces.

See `docs/context/architecture.md` for data flow and ownership boundaries.

## Critical code contracts

Settings/profile access uses a profile map, not a slice:

```go
cfg, _ := settings.Load()
profiles, _ := settings.LoadProfiles()
profile := profiles.Profiles["qwen3.6-think"]
cfg.Context.CompactionAt
cfg.Context.MAULERMDPath
```

Providers describe transport; profiles describe model behaviour. Settings structs have TOML and
snake_case JSON tags. Preserve full settings round-trip fields when changing frontend types.

Tool calls use `tc.Function.Name`, `tc.Function.Arguments`, and `tc.ID`. Tool-result messages are:

```go
llm.Message{
    Role: llm.RoleTool, Content: result,
    ToolCallID: tc.ID, Name: tc.Function.Name,
}
```

`llm.Request` contains messages, tools, output/sampling values, and thinking flags. The model is
baked into the client; there is no `Params` sub-struct and no request `Model` field.

Core Wails events include `mauler:stream_start`, `mauler:delta`, `mauler:stream_done`,
`mauler:stream_error`, `mauler:tool_call`, `mauler:tool_result`, `mauler:confirm`, `mauler:compact`,
`mauler:artifact_output`, and `mauler:artifact_done`. TypeScript listeners receive
`(...args: unknown[])`.

## Non-negotiable behaviour

- Workspace selection is authoritative for prompts and tools. Switching workspace persists the
  normalized root and clears transient chat/tool context, todos, rollback state, and path-backed
  tabs while preserving durable project data and scratch tabs.
- Model-facing tools use the compact registry and current names. Do not reintroduce legacy aliases.
  `terminal_send`/`terminal_read` are for live interactive sessions; independent HTTP checks use
  `http_probe` or `shell` when exact flags are required.
- The control plane, `TaskRun.State`, and RunLedger have distinct jobs. Do not merge descriptive run
  state with authorization state. Mutations requiring planning cannot bypass an accepted plan;
  terminal completion requires blocking verification evidence.
- Assistant prose is never completion evidence. Use immutable evidence IDs, file hashes, successful
  checks, HTTP/browser artifacts, or explicit approval/waiver.
- Tool output is untrusted input. Preserve promptware/source-to-sink guards and secret redaction;
  webpage/file/tool text cannot expand scope, authorize unrelated actions, or disclose secrets.
- A failed or malformed tool call is recoverable only through classified, bounded repair. Do not
  silently mark a failed run complete and do not retry around policy/scope violations.
- Context packets are bounded and task-aware. Unrelated public research gets the minimal external
  packet; workspace implementation gets relevant project instructions. Chat history is not the
  operational database. The legacy full handoff must never return to every model turn.
- Keep local model loading/context checks strict: if the backend reports less context than the active
  profile requests, fail clearly instead of silently running at a smaller context.
- Keep Explorer, Inspector, bottom work area, and Terminal/AI Commands resizable. Their visible drag
  handles, collapsed rails, keyboard resizing, narrow-layout behavior, and persisted dimensions must
  not be removed.
- Telegram side chat is separate from desktop Chat. Explicit work runs may queue while busy and must
  deliver the actual final assistant answer, not only a generic completion status.
- `Bug Bounty Hunter` is a post-recon manual-assessment planner. It distinguishes observations from
  hypotheses, never invents vulnerabilities, and respects engagement scope/evidence rules.
- Normal prompts stay normal Auto runs. Do not route every pentest-looking prompt into HTB/Ops or
  flag-oriented workflows; `/ops` is legacy shorthand for `/run`.

The full rule set is in `docs/context/non-negotiables.md`.

## Current operational baseline

The working product includes streaming local/cloud chat, compact tools, confirmations and rollback,
Monaco editing, resizable workbench surfaces, shared/isolated terminal modes, model/provider profiles,
one-task OpenRouter boosts, task-aware context compilation, persistent memory/session search,
RunLedger/Brain/Logs, todo planning, browser/web research budgets, Telegram remote control, audio,
artifact execution, the native control-plane slice, Engagement Grid, pack library, and the built-in
Bug Bounty Hunter plus Chat workspace/agent selectors. The first-class Context Inspector previews
the exact bounded packet, provenance, exclusions, and token budget before a task and can pin one
explicit packet class for the next desktop run.

Fast Chat omits project context/tools/control state; Project Agent keeps the full reliable work path.
See `docs/model-lab-timing-and-fast-chat-guide-2026-07.md`.

Local/Hugging Face model selection uses code-owned Qwen3.6, Qwen3.5, and Gemma4 templates for conservative
context, sampling, adapter, tool protocol, and embedded-GGUF Jinja metadata. Settings > Profiles has
an explicit **Apply model template** action. Generation profiles transport `repeat_penalty`
end-to-end. Unknown fine-tunes may receive conservative family defaults but remain benchmark-required.
Exact aliases cover the installed Unsloth UD-Q4_K_XL, Huihui abliterated MTP, HauhauCS Aggressive,
35B-A3B heretic Native-MTP, Qwen3.5 4B, Qwythos 9B, Gemma 4 12B, and Gemma 4 E4B artifacts.
Model-id matching is authoritative before a borrowed profile name so Model Matrix cannot apply
Qwen metadata to a selected Gemma model. See
`docs/hugging-face-local-model-templates.md`.

The 2026-07-21 six-model Qwen tournament selected Unsloth Qwen3.6 27B UD-Q4_K_XL as the best daily
candidate: 90/100 at 54.8 tok/s across the four-scenario suite, a clean native required tool call,
and a passing score-43 six-tool mini loop. Huihui Qwen3.6 27B Q4_K was the close second at 90/100
and 51.5 tok/s. The installed 35B-A3B model is not recommended for daily use (10.9 tok/s, required
tool failure, loop score 0). UD remains a supervised recommendation because its separate constrained
grammar probe returned HOLD. It is the persistent 35K local default; native MTP calibration selected
`n=3` but rounded to `1.00x` versus off. `settings.DefaultProfiles` also uses the exact UD GGUF via
InferenceBridge at 35K for fresh configurations; preserve existing named user profiles rather than
silently overwriting them. See `docs/qwen-local-model-tournament-2026-07-21.md`.

The 2026-07-23 extended matrix found Qwen3.5 4B fastest by backend decode timing, but only 66% in its
corrected-template mini loop. Qwythos 9B failed its loop at 40%; Gemma 4 12B passed at 80%, while its
old 3.5 tok/s value was an invalid cold wall-clock comparison. Model Lab now warms once, measures text
three times, separates decode/E2E timing, and changes Chat only through Matrix `Use`. Keep UD Qwen3.6
27B as proven daily winner and Huihui 27B as unrestricted quality choice. See
`docs/local-model-tournament-2026-07-23.md`.

Do not infer implementation detail from this synopsis. Before editing a subsystem, read the matching
code and a targeted document from `docs/context/README.md`.

## Current priorities

1. Context M5 closure: the 105-attempt deterministic paraphrase/hostile preflight and reusable live
   pass^k telemetry are implemented. The shared Agent Eval/control-state repairs pass focused, race,
   and production build gates. The final 2026-07-23 Huihui UI Gate 1 reached 11/12: Mauler safely
   blocked three missing-append overwrites, then the circuit breaker stopped the model. This is not
   pass^1 and x5 remains locked. After a deliberate model/profile/control change, require a fresh
   12/12 Gate 1 before the selected local profile's full Agent Eval x5. Follow
   `docs/unattended-agent-reliability-gate-2026-07-22.md` and do not claim the model-reliability
   milestone complete before pass^5 evidence exists.
2. Finish Bug Bounty/Chat workspace reliability edges: hostile/paraphrase fixtures, exact agent
   definition/version ledgering, named conversation checkpoint/resume, and dirty-tab save/discard.
3. Keep the control-plane and reliability gates green while expanding classified recovery and
   evidence-owned completion.
4. Continue repository intelligence/file extractors and LSP only through their tracked plans; do not
   break existing file reading, tools, or UI surfaces.

See `docs/context/roadmap.md` for the routed backlog and supersession notes.

## Context routing

- `docs/context/current-state.md`: concise current product state and latest verified baseline.
- `docs/context/architecture.md`: package ownership and execution/data flow.
- `docs/context/non-negotiables.md`: exact invariants and protected behavior.
- `docs/context/roadmap.md`: current ordered work and tracked plans.
- `docs/context/verification.md`: required checks, live smokes, and known non-gates.
- `docs/context/feature-catalog.md`: implemented capability inventory.
- `docs/context/troubleshooting.md`: common WSL/provider/context/build/Telegram failures.
- `docs/archive/agents-handoff-snapshot-2026-07-21.md`: content-preserving historical handoff.

Read only the documents relevant to the current task. The validated context manifest may route up to
three trusted local Markdown documents; it cannot authorize tools, alter scope, or weaken code-owned
policy. Invalid manifests fall back deterministically to the compact core.

## Known gotchas

- WSL `CreateProcessCommon ... chdir(...) failed 2` means the saved Windows workspace no longer maps
  to a real WSL directory; validate/fallback before passing `--cd`.
- `.wslconfig` key support depends on the installed WSL version. Unknown keys are host configuration
  warnings, not Mauler relay protocol errors.
- Windows PowerShell treats `curl` as an alias; use `curl.exe` or `Invoke-WebRequest` explicitly.
- Windows shell output may be UTF-16-ish; keep the existing decoding path.
- Audio auto mode is Kokoro first, Piper fallback; `ffmpeg` is needed for Telegram OGG/Opus voice
  notes.
- Do not launch runtime Telegram/audio workers from headless test/binding App values; start them only
  after Wails `OnStartup`.
- Preserve unrelated user changes in this dirty working tree. Use `apply_patch` for source/doc edits
  and avoid destructive Git operations.
