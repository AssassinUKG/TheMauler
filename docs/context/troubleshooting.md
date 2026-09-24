# Troubleshooting

## A good answer is followed by `finalisation stopped: loop_circuit_breaker`

The loop guard may correctly pause repeated cached reads even though enough evidence already exists
to answer. Current builds tell the model to stop tools and synthesize first. If the model still
repeats once more, Mauler keeps the blocked loop in RunLedger but delivers a substantive read-only
recovery response as **Answer recovered** in desktop Chat and Telegram. Mutation/execution tasks,
junk text, and answers without successful evidence never receive this recovered label.

## Project removal appears stuck

Project removal only updates Mauler's saved project list; it never deletes the workspace directory,
evidence, notes, or sessions. Ordinary project saves must not restart Telegram, text-to-speech, or
speech-to-text workers when those settings did not change. The confirmation has a bounded wait and
shows an actionable error instead of remaining permanently on **Removing...**. If an older build
already persisted the removal before hanging, restart Mauler and refresh Projects; the removed entry
will be gone while its files remain on disk.

## WSL startup and Relay errors

`CreateProcessCommon: chdir(/mnt/c/...) failed 2` means the saved workspace path no longer exists or
cannot be translated in WSL. Validate the Windows root before constructing `wsl.exe --cd`; fall back
to the real process directory and surface a useful workspace warning instead of landing at `/`.

`wsl: Unknown key 'nestedVirtualization' in C:\\Users\\...\\.wslconfig` is a host WSL-version/config
compatibility warning. Remove the unsupported key or update WSL after checking Microsoft guidance;
it is not Mauler's HTTP relay status code.

## Local provider or context mismatch

- Confirm InferenceBridge is running and the provider base URL matches its actual OpenAI-compatible
  endpoint, normally `http://127.0.0.1:8800/v1`.
- Use Doctor to check route/model/backend parity. Model-list calls time out independently after the
  bounded catalogue timeout.
- If the backend reports less context than the selected profile, reduce the profile to a value the
  loaded Q4/Q5 model actually supports or reload it with sufficient KV context. Do not bypass the
  hard-fail check and do not choose Q6_K on the 24 GB card.
- OpenRouter keys are entered in Settings > Providers. Environment `OPENROUTER_API_KEY` takes
  precedence. Never print the key to logs or copy it into a profile.

## Large files and context

The project-document source allowance is how much can be discovered/read before compilation; the
always-on prompt packet is capped separately. For large source/data files, prefer targeted `read`,
search, extractors, indexing, and synopsis artifacts. A full file need not fit in one prompt to remain
readable by the system.

In Chat, use **Attach files**, drop a file, or paste the absolute path copied by Explorer. A native
path attachment carries file metadata and exact read-only authority, not the entire payload; Mauler
can therefore read a 100 KB or multi-megabyte supported file in bounded chunks. Browser-only pasted
text is deliberately capped and shows a message directing the user to the full-file path flow.
Attached document content is untrusted data rather than instructions and cannot change scope or
authorize unrelated tools.

If a read-only OpenAPI question unexpectedly plans edits or runs the current project's build, inspect
the task contract and mode in Logs. `POST`, `PUT`, `PATCH`, and `DELETE` in a method inventory must be
classified as API data: low-risk/read-only, no mutation acceptance checks, and no project verifier.
The Engagement tool must likewise be absent unless the prompt explicitly asks for Engagement Grid
state. These are routing defects, not reasons to retry the build or Grid finalisation.

If Inspector shows Write/Shell enabled but a run says it has only read/web tools, distinguish the
active toolset from the model-facing per-task route. Open Inspector > Agent > Tools. **Automatic**
keeps schemas compact, but an explicit request to save, place, or leave an artifact in the workspace
must still include `write` and `edit`. Direct follow-ups such as “write the PoC in the directory” are
artifact mutations too, while “show the PoC in chat; do not save it” remains read-only. Confirm the
RunLedger `tool_routing`/`selected_tools` fields if
it does not. **Selected tools** advertises every enabled member of the active toolset and is useful
for unusual mixed tasks, at the cost of a larger prompt and potentially weaker small-model tool
choice. The per-tool switches and toolset remain hard capability gates in either mode.

If unrelated CVE/public research appears to carry repository handoff content, inspect the
`context_packet` run event. It should show `minimal_external_research` and a short pointer packet.

For workspace tasks, that event is JSON containing `manifest_status`, `manifest_sha256`, `route_id`,
packet bytes/tokens, and per-source hashes/line ranges. `manifest_status: invalid` with route
`fallback-compact-core` means the manifest failed strict validation; `fallback_reason` gives the exact
unknown field, missing document, unsafe path, budget, or schema error. Fix the manifest rather than
weakening the fallback.

Open `More workbench pages... > Context` to preview the next packet. `Rebuild synopsis` recalculates
against the current task draft without starting a model run. A Core/Relevant/Expanded pin applies to
one accepted desktop task and then clears; choose Auto or Unpin to restore normal selection. The
inspector intentionally shows hashes, ranges, counts, and code-owned reasons instead of raw system
prompts, memory contents, tool output, or provider secrets.

## Terminal and shell

- In PowerShell, `curl` is an alias. Use `curl.exe` or
  `Invoke-WebRequest -Uri ... -UseBasicParsing`.
- Windows-host inspection and WSL/Kali target work are different execution lanes. Requests about
  locally running processes, games/apps, services, Task Manager state, windows, or GPU/VRAM state
  are routed to an isolated native PowerShell shell call even when the visible shared terminal is
  configured for WSL/Kali. Target scans, VPN/DNS checks, and Kali tools remain on WSL.
- Do not send `powershell.exe -Command "...$p...$_..."` through a WSL terminal. Bash expands
  PowerShell `$` variables first (the observed failure turned `$_.ProcessName` into
  `1.ProcessName`). Current builds expose a per-call shell backend, unwrap redundant one-shot
  PowerShell wrappers, preserve the script payload, and reject Windows-host one-shots typed into an
  ordinary WSL prompt. A genuinely connected remote Windows shell and interactive Windows listener
  remain valid Terminal workflows.
- Bash syntax sent to PowerShell should fail with the existing shell-backend hint; change the backend
  to WSL/bash when the syntax truly requires it.
- Preserve captured stdout/stderr on error. Decode UTF-16-ish Windows output before returning it to
  chat/logs.
- Use `terminal_send` only for the active interactive session. Use `http_probe` or isolated `shell`
  for independent probes so listener/terminal state is not corrupted.
- Current builds deterministically rewrite a mistaken `terminal_send` `curl`/`wget` call to a
  bounded, isolated WSL `shell` call before it enters history. Explicit WSL/bash shell backends also
  bypass the shared PTY. This keeps a slow or non-HTTP port from owning the visible terminal and
  exhausting loop recovery on repeated reads/Ctrl-C attempts; the circuit breaker remains the final
  no-progress safety gate.

Terminal is the live execution surface, so command output can appear there before the model's final
Chat synthesis. If a read-only run later stops or trips the circuit breaker, current builds recover
the best successful non-verifier shell result into Chat instead of leaving the answer only in
Terminal. The fallback strips shell contracts and refuses guarded/untrusted output. If only the
technical stopped-run summary appears, confirm that the successful tool was a read-only `shell`
result rather than a failed command, mutation, project build/test, or prompt-injection finding.

If the centre Chat pane goes empty while Terminal/AI Commands continues working, check for an older
build where `http_probe` artifact creation emitted `mauler:workspace_changed`. That event cleared Chat
and remounted the Terminal even though the root had not changed. Current builds use the separate
`mauler:workspace_files_changed` refresh event and show a live run-status card instead of a blank
surface if a stream has not produced visible text yet.

## Telegram results missing

Telegram work runs must forward the actual final assistant answer or a plain evidence-backed
fallback, never only a completion status or textual `<tool_call>` block. Actionable plain messages
are automatically promoted to the project-work lane; `/cmd` and `/run` are optional explicit
overrides. Side chat remains a separate no-tools lane, but each completed Telegram task/result is
bridged back to that same chat for later questions. A busy project run may queue later work. Check
the channel route reason, tool-routing event, channel queue, RunLedger source `telegram`/`channelbus`,
and task final-message artifact. Current weather/forecast and other changing public facts should show
`tool_choice=required` with `http_probe`, `web_search`, or `fetch_url`; if not, the live-information
classifier or active toolset is the likely fault.

Weather wording does not need to say “forecast”: phrases such as “weather over the next seven days”
must enter project work. If an older/missed side-chat classification offers to route a request, “route
it” uses the remembered original task. Without a remembered task it returns a deterministic correction
and must never claim that a run started.

Long final answers are sent as Telegram-safe chunks. When a live status card exists, Mauler removes
that short card before sending a multi-part final so a stale “working” message is not left behind. With
`send_progress=true`, the runtime refreshes the same live card every `progress_interval_s` even when no
new model/tool event arrives. Missing heartbeats indicate either progress was disabled, the chat was not
attached to a real run, or the Telegram progress-send ledger event failed.

Tokens and token-bearing URLs must remain redacted from runtime logs and exported ledger data.

## Thinking control

Open Inspector > Agent > Behaviour and use **Thinking**. **Profile** keeps the model profile and
Mauler's bounded adaptive fallback. **On** keeps thinking enabled throughout a run for a supported
Qwen/Gemma template; use **Reasoning Effort** beside it to choose depth. **Off** uses the profile's
direct/no-thinking sampler and does not preserve reasoning between turns. The selector applies to
new Project Agent runs and is saved in settings; Fast Chat remains its deliberately direct,
no-tools lane. If On reports unsupported, apply the correct model template in Settings > Profiles
instead of making Mauler guess that an unknown model supports a thinking protocol.

## Good Chat answer followed by an HTTP 400

`Cannot have 2 or more assistant messages at the end of the list` is a transcript-shape rejection
from an OpenAI-compatible local bridge/template, not a Wails process crash. The 2026-07-29 incident
produced a complete answer, then incorrectly treated the optional phrase `let me know if...` as a
pending action. Two controller repair turns followed; the selected template ignored their late
system role and saw consecutive assistant turns.

Current behavior prevents that chain in three layers:

- optional follow-up offers do not count as unfinished actions;
- late code-owned controller prompts are transported as ordered user turns for OpenAI-compatible
  templates, while the primary system prompt remains first;
- if a later finalisation error still occurs, Chat preserves the already-streamed assistant answer
  and adds a clear finalisation warning instead of replacing the answer with the error.

Use the run ledger and SQLite task run to distinguish an agent-run failure from an application
process crash. A responsive `TheMauler.exe` process plus a terminal `run_finish` error means the app
survived and the run failed.

## Audio

`MAULER_TTS_ENGINE=auto` tries Kokoro then Piper. Kokoro needs Python plus `kokoro` and `soundfile`.
Telegram OGG/Opus conversion needs `ffmpeg`; Linux also checks `espeak-ng`. Use `setup.ps1 -Check` or
the normal Linux dependency bootstrap to diagnose missing tools.

Desktop Talk submits the transcript to Project Agent, so it has the same configured tools as a typed
request. Actionable Telegram typed and voice requests are also promoted to the queued project-work
lane without requiring `/cmd`.
Current time/date requests require a fresh local shell result rather than a model-memory answer.
Tool permissions, workspace scope, confirmations, and control-plane gates still apply; voice never
bypasses those code-owned protections. If a spoken action is answered as no-tools chat, inspect the
channel route and confirm the active toolset still enables the required tool.

## Build or generated binding failures

Run focused Go tests, `go vet`, and `npm run --prefix frontend build`. If a Wails binding changed,
regenerate/build through Wails rather than manually diverging generated JS/TS. Runtime workers must
start only after Wails `OnStartup`, otherwise headless tests may lock temporary working directories.

Repository-wide frontend lint is a known non-gate; production frontend build is the current check.
Do not use destructive Git cleanup to make a dirty tree look clean.

If Go reports a present GOROOT package such as `unsafe` as "not in std", its optional module index is
stale. `build.ps1` probes that exact condition and temporarily sets `GODEBUG=goindex=0` for the Wails
build, returning the previous environment value afterward. This uses direct standard-library source
discovery; it does not change dependencies or persist a machine-wide workaround.
