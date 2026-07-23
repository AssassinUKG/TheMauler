# Troubleshooting

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
- Bash syntax sent to PowerShell should fail with the existing shell-backend hint; change the backend
  to WSL/bash when the syntax truly requires it.
- Preserve captured stdout/stderr on error. Decode UTF-16-ish Windows output before returning it to
  chat/logs.
- Use `terminal_send` only for the active interactive session. Use `http_probe` or isolated `shell`
  for independent probes so listener/terminal state is not corrupted.

## Telegram results missing

Telegram side chat and explicit `/run` are separate lanes. A busy project run may queue later work.
Check the channel queue, RunLedger source `telegram`/`channelbus`, and task final-message artifact.
Success status alone is insufficient: the runtime must forward the actual final assistant answer or
a plain evidence-backed fallback when a run stops without one.

Tokens and token-bearing URLs must remain redacted from runtime logs and exported ledger data.

## Audio

`MAULER_TTS_ENGINE=auto` tries Kokoro then Piper. Kokoro needs Python plus `kokoro` and `soundfile`.
Telegram OGG/Opus conversion needs `ffmpeg`; Linux also checks `espeak-ng`. Use `setup.ps1 -Check` or
the normal Linux dependency bootstrap to diagnose missing tools.

## Build or generated binding failures

Run focused Go tests, `go vet`, and `npm run --prefix frontend build`. If a Wails binding changed,
regenerate/build through Wails rather than manually diverging generated JS/TS. Runtime workers must
start only after Wails `OnStartup`, otherwise headless tests may lock temporary working directories.

Repository-wide frontend lint is a known non-gate; production frontend build is the current check.
Do not use destructive Git cleanup to make a dirty tree look clean.
