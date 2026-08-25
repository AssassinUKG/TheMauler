# Verification

Run from the repository root. Start focused, then widen in proportion to the changed surface.

## Standard production gate

```powershell
go test ./... -count=1
go vet ./...
go test -race ./internal/app ./internal/tools -count=1
npm run --prefix frontend build
wails build
```

Use `./build.ps1 -Run` to build and relaunch the Windows production app. Use `wails dev` for hot
reload. On Linux/WSL use the documented `build.sh` variants.

## GitHub clean-checkout gate

The GitHub Go job runs on Ubuntu and must build `frontend/dist` before any `go build`, `go vet`, or
`go test` command that compiles `main.go`; the embedded frontend output is intentionally ignored by
Git. Keep Windows paths in settings and policy tests host-independent so the Linux gate continues to
exercise WSL/relay portability rather than being replaced with a Windows-only runner.

## Focused gates

- Control plane/store: `go test -race ./internal/controlplane ./internal/store -count=1`
- Engagement/packs: `go test -race ./internal/engagement/... ./internal/packlibrary -count=1`
- App/tools integration: `go test -race ./internal/app ./internal/tools -count=1`
- Frontend type/production bundle: `npm run --prefix frontend build`
- Context compilation: `go test ./internal/app -run 'ProjectInstruction|ContextDocuments' -count=1`
- Manifest routing/security: `go test ./internal/app -run 'ManifestContext|ActiveManifest|InvalidManifest|ManifestRejects' -count=1`
- Context Inspector/pinning: `go test ./internal/app -run 'ContextInspector|ContextPacketPin|MessagesWithPrimary|RepositoryContextDocuments' -count=1`
- Context M5 deterministic/repeated harness: `go test ./internal/app -run 'ContextQuality|AgentEval|HashToolDefs|ShellRoutingDoesNotTreatKeeping' -count=1`

Run `gofmt` on changed Go files and `git diff --check` on the files in scope. Do not format or revert
unrelated user work.

## Live smoke expectations

Use a production/native smoke when changing Wails/UI/provider/runtime behavior. Relevant checks may
include:

- app launches and responds;
- local provider remains selected after one-task cloud boost resets;
- Chat agent/workspace menus remain visible above a tall bottom panel;
- all workbench separators drag, collapsed edges reopen, and dimensions survive reload;
- terminal starts in a real validated workspace and AI command output is not duplicated;
- Telegram receives actual tool/final results, not only a completion status; direct actionable text
  works without `/cmd`, current public facts require a real tool, raw tool protocol cannot complete a
  run, natural multi-day weather wording routes to work, “route it” preserves the pending task, quiet
  runs keep one timer-refreshed progress card, long final answers are not truncated, and same-chat
  result follow-ups return the prior result;
- context inspector/event provenance matches the packet actually sent.
- Context Quality pass^5 reports 105/105 deterministic attempts and zero model calls; Agent Eval x5
  is treated as a separate, expensive live gate whose actual selected profile and result are named.

## Known non-gates

Repository-wide frontend lint has a pre-existing backlog across generated Wails bindings and older
components. It is not green and must not be presented as passing. Frontend production build/type
checking is the current gate unless a task explicitly scopes lint cleanup.

## Latest recorded baseline

- 2026-08-24 multi-target authorised scope: Home now persists ordered allow/exclude rows for IP,
  CIDR, hostname, host/port, and HTTP(S) URL/path scope. Existing comma-separated targets migrate
  into separate rows; the first allowed row remains the compatibility primary target. Explicit
  exclusions are evaluated before broad CIDR/host allows, model-requested path broadening is denied,
  and relative endpoints inherit only the primary concrete target. Full Go tests, full vet, focused
  settings/engagement/app/tools race suites, frontend type-drift/production build, scoped handwritten
  diff check, and canonical Wails production build pass. Output is `build/bin/TheMauler.exe`. A native
  visual two-IP create/reopen/Grid-preview smoke was not run, so no visual UX claim is added.

- 2026-08-22 Telegram natural-range/heartbeat closure: the exact “weather over the next seven days”
  wording routes to required external evidence, route confirmation is code-owned, quiet runs refresh
  one live status card, and inaccessible settings no longer panic during `App.New`. All scoped
  `TestTelegram*`, `TestDispatchChannel*`, channelbus, and Telegram package tests pass; focused race,
  app/channel/Telegram vet, frontend production build, and Wails production build also pass. The
  managed execution sandbox denied unrelated manifest `EvalSymlinks`, browser-fixture, and external
  config/temp access, so the complete app suite and live Telegram-network smoke were not rerun in
  this closure. Output remains `build/bin/TheMauler.exe`.

- 2026-08-22 Telegram task/result repair: direct imperative and changing-data requests now enter the
  queued project-work lane without requiring `/cmd`; current public facts require a narrowed real
  tool set, and raw textual tool protocol blocks completion. Telegram final cards use structured
  Markdownish formatting, retain long answers through the client’s multi-message path, replace stale
  live cards for multi-part results, and bridge task/result pairs back into the same bounded Telegram
  chat. Deterministic follow-ups return the exact prior result, with a claimant-scoped persisted-run
  fallback after restart. Focused channel/app/Telegram tests, Context Quality pass^5 (105/105), the
  full Go suite, vet, app/tools race suite, frontend type/production build, Wails production build,
  and canonical native startup smoke all pass. Output remains `build/bin/TheMauler.exe`; no live
  Telegram-network, live-model, or unattended Agent Eval result is claimed.

- 2026-08-21 Windows-host shell and Chat Thinking repair: local process/game/app/service/window and
  GPU/VRAM questions route to an isolated native PowerShell one-shot even when the shared target
  terminal is WSL/Kali. Redundant nested PowerShell wrappers are removed without allowing bash to
  expand `$` variables; explicit Kali/WSL work and genuine remote Windows sessions remain intact.
  Inspector > Agent > Behaviour now provides a persisted Profile/On/Off Thinking override. On
  enables supported model thinking plus preserved reasoning for the complete run, Off selects the
  direct sampler and clears preserved reasoning, and Profile keeps the existing adaptive policy.
  Settings migration/validation, request-level Qwen behavior, shell routing, wrapper preservation,
  the 105-attempt deterministic context gate, full Go suite, vet, app/tools race suite, frontend
  type/production build, Wails production build, and a short canonical native startup smoke pass.
  The canonical output is
  `build/bin/TheMauler.exe`; no live-model or unattended Agent Eval claim is added.

- 2026-08-19 Chat attachment and read-only routing repair: native multi-file selection, copied
  Explorer paths, dropped paths, bounded large-file access, and untrusted attachment boundaries pass
  focused tests. The exact OpenAPI `POST, PUT, PATCH, DELETE` coverage prompt now receives a low-risk
  read-only control contract, no mutation/listener/Engagement tools, and no project build verifier.
  Stopped read-only runs also promote cleaned successful shell evidence into Chat when no assistant
  checkpoint exists, while verifier output, mutation runs, and guarded content remain excluded.
  Empty stopped-run memory and legacy reinjection regressions are covered. Context Quality returned
  7/7 fixtures and 105/105 deterministic attempts. The full Go suite, vet, app/tools race suite,
  frontend production build, generated bindings, and Wails production build pass. The temporary
  side-by-side build name used while an older desktop process was open was retired on 2026-08-19;
  the current verified production output is the canonical `build/bin/TheMauler.exe`. No live model
  or unattended Agent Eval claim is added.

- 2026-08-19 Terminal readability repair: run start preserves the persisted/user-resized bottom-panel
  height; terminal text defaults to 14 px with persisted UI A-/A+ controls and improved contrast;
  AI Command cards wrap commands across their full width and retain the draggable split/rail.

- 2026-08-19 task-routing repair: answer-output wording (table/summary/report/count/plan) no longer
  creates a Builder mutation contract by itself; mixed prompts with an explicit save/edit/update/fix
  action retain mutation planning and verification. Chat shows the actual live route while running.

- 2026-08-17 Qwen3.8 settings/UI follow-up: the local llama.cpp request path preserves separate
  `reasoning_content` between turns, Qwen3.8 emits only supported `low`/`medium`/`xhigh` effort
  values (mapping Mauler `high` to `xhigh` and omitting the field for direct mode), and old reasoning
  is token-counted and micro-compacted. Profiles now includes the Qwen3.8 RTX 3090 setup card. Focused
  backend/settings/history tests, the full Go suite, vet, app/tools race suite, frontend production
  build, and Wails production build pass. A live model comparison was not run because InferenceBridge
  was not active, so MTP remains provisional and no new unattended/pass^k claim is recorded.

- 2026-08-16 Qwen3.8 default and control-loop hardening: fresh and live settings select
  `qwen3.8-agent-stability` through InferenceBridge at 35K. Code-owned templates carry the official
  thinking/no-thinking samplers, preserved thinking, normalized `none`-to-`xhigh` effort, and a
  conservative draft-MTP `n=2` probe. Tool availability no longer disables thinking on the first
  Qwen3.8 step; bounded recovery can still force no-thinking. The completion evidence rail is
  blocking by default. The full Go suite, vet, app/tools race suite, frontend type/production build,
  scoped diff check, and clean Wails production build pass. The rebuilt native desktop launches
  responsively and visibly reports `qwen3.8-agent-stability`, backend `ok`, and the expected
  35,000-token context.

- 2026-07-23 repaired live Agent Eval closure: four diagnostic 12-fixture UI runs exposed and
  regression-closed alternate-valid-artifact scoring, accidental chunk overwrite, consecutive
  controller-message request shape, planning-only/completion-feature classification, and stale
  read-cache defects. Full Go tests, vet, app/tools race tests, frontend production build, Wails
  production build, and `git diff --check` passed. The final fresh Huihui 27B/35K report
  `agent-eval-20260723-195504` passed 11/12 with zero unsupported completions, policy violations,
  and human interventions. `chunked-write` remained a genuine model-loop failure: the model omitted
  `append=true` three times after explicit correction; Mauler preserved the existing 100 lines,
  blocked every overwrite, and stopped through the circuit breaker. Gate 1 is not pass^1, Agent Eval
  x5 was not run, and the profile remains supervised-only. See
  `../huihui-qwen36-agent-eval-2026-07-21.md`.
- 2026-07-23 GitHub/frontend dependency closure: clean-checkout CI builds the ignored embedded
  frontend before Go compilation and uses the current Node 24/action toolchain. Vite 8.1.5,
  Monaco 0.56.0, patched DOMPurify 3.4.12, Babel 7.29.7, and brace-expansion 5.0.8 produce a zero-
  vulnerability `npm audit`. Rolldown size-based vendor splitting with strict execution ordering
  removes the oversized-chunk warning without blanking WebView2. Full Go tests, vet, frontend
  production build, Wails production build, and a native Monaco/xterm/resizable-workbench/backend
  smoke passed.
- 2026-07-21 Huihui live Agent Eval: the active `qwen3.6-nothink-copy-copy` profile uses the exact
  `Huihui-Qwen3.6-27B-abliterated-ggml-model-Q4_K.gguf` alias at 35K, no-thinking sampling
  (`temperature=0.2`, `top_p=0.95`, `top_k=20`, `repeat_penalty=1.05`, `max_tokens=8192`) and native
  MTP `n=2`. One complete 12-fixture live suite passed 2/12 with zero unsupported completions, eight
  policy violations, 12.9% duplicate actions, 12.9% tool errors, and zero measured recovery success.
  The earlier UD run also passed exactly the same 2/12 fixtures with a near-identical stop pattern,
  exposing a shared Agent Eval/control-plane compatibility problem rather than a trustworthy model
  ranking. The result and required harness corrections are recorded in
  `docs/huihui-qwen36-agent-eval-2026-07-21.md`. An x5 result was not produced and is not implied.
- 2026-07-21 permanent default-winner closure: `settings.DefaultProfiles` now routes the stable
  `qwen3.6-nothink` fresh-install profile through `inference-bridge` to the exact
  `Qwen3.6-27B-UD-Q4_K_XL.gguf` winner at the verified 35K working context. The modern
  `profiles.toml.example`, tournament report, HF/GGUF template guide, current-state guide, and
  handoff document record the same choice. Existing named user profiles remain untouched; this
  installation retains its verified `qwen3.6-nothink-copy` profile as the active local default.
  Focused settings tests, `build.ps1` (full Go tests, full vet, frontend type/build, clean Wails
  production build), `go test -race ./internal/app ./internal/tools -count=1`, and scoped
  `git diff --check` pass. The rebuilt native app launches with backend `ok`, the existing winner
  selected, and the visible token budget reporting the expected 35,000-token context; it is left
  open.
- 2026-07-21 Qwen tournament/default closure: six installed Qwen variants completed the same live
  matrix; UD-Q4_K_XL and Huihui then completed the same four-scenario Advanced Suite. UD won at
  90/100 and 54.8 tok/s, passed ordinary required-tool and mini-loop gates, and returned HOLD only in
  the separate constrained-grammar probe. The exact UD/Huihui/HauhauCS/35B aliases passed focused
  and full Go tests plus vet. The runtime-profile race test, frontend type/build gate, Wails production
  build, rebuilt native launch, persistent default-profile smoke, and built-in MTP `n=1..5` sweep
  passed. UD is the local default at 35K with `n=3`; the sweep rounded to `1.00x` versus off, so it is
  not recorded as a material MTP speedup.
- 2026-07-21 local-model templates/comparison: exact-model Advanced Suite recorded Qwen3.6 27B
  Fable/Fus Q4_K_M at 90/100 and 33.3 tok/s; HauhauCS Gemma 4 26B-A4B QAT at 55/100 and 4.3 tok/s
  with invalid JSON but a valid structured tool call; Gemma 4 31B returned model-not-found and was
  not scored. Code-owned HF/GGUF templates and repeat-penalty transport passed focused tests,
  `go test ./... -count=1`, vet, app/tools race, frontend build, Wails production build, and native
  UI apply/save smoke for Qwen and exact HauhauCS Gemma. Rebuilt Mauler was left open.
- 2026-07-23 extended local-model matrix/template repair: the initial eight-model 16K matrix
  completed with 8/8 direct structured-tool calls and exposed that borrowed Qwen profile names could
  override selected Gemma/Qwen3.5 model ids. Model-id-first matching plus exact Qwen3.5 4B, Qwythos
  9B, Gemma 4 12B, and Gemma 4 E4B templates passed focused/full runtimeprofile and app tests, vet,
  full `build.ps1`, frontend production build, and Wails production build. Repaired live reruns
  measured Qwen3.5 4B at 125.4 tok/s with pass/66% mini loop, Qwythos 9B at 83.5 tok/s with fail/40%
  mini loop, and Gemma 4 12B at 3.5 tok/s with pass/80% mini loop. All three direct tool gates
  passed without repair. E4B corrected reruns remain pending because user input stopped UI
  automation. See `../local-model-tournament-2026-07-23.md`.
- 2026-07-21 context M5 phase A: seven task classes x three paraphrases x five repeats, hostile
  file/web/browser content, canonical-envelope isolation from intentional Manual/Unrestricted/tool
  overrides, stable packet/tool-schema identities, repeated Agent Eval telemetry, full Go tests,
  vet, app/tools race gate, frontend type/production build, clean Wails production build, and native
  Benchmark smoke passed. The real rebuilt app reported 7/7 fixtures and 105/105 attempts with the
  selected `gemma4-26b-a4b-qat` profile, hostile guard pass, and zero model calls. The native smoke
  first exposed an 85/105 false failure caused by intentional Unrestricted settings; the canonical
  evaluation envelope fix was regression-tested before the final 105/105 run. A live model Agent
  Eval x5 has not been run and is not implied by this result.
- 2026-07-21 context M4: exact Core/Relevant/Expanded preview accounting, safe provenance and
  exclusions, one-task desktop-only pin consumption, primary-system refresh without duplicate
  control packets, live three-document manifest validation, full Go tests, vet, app/tools race gate,
  frontend type/production build, clean Wails production build, and responsive rebuilt app process
  passed. Native automation also confirmed the Context route in the real Wails sidebar; the Windows
  helper could inspect but not click through the WebView child-process boundary.
- 2026-07-21 context M3: active-manifest routing, unknown-field/path-escape/document-count rejection,
  deterministic compact-core fallback, heading selection, exact hash/range provenance, five-run
  determinism, full Go tests, vet, app/tools race gate, frontend build, clean Wails production build,
  and responsive rebuilt app process passed.
- 2026-07-21 context M2: archive/core/shim/domain-map invariants, archive non-injection test, full Go
  tests, vet, app/tools race gate, frontend build, clean Wails production build, and responsive
  rebuilt app process passed.
- 2026-07-21 context M1: full Go tests, vet, app/tools race gate, frontend build, Wails production
  build, and rebuilt app launch passed.
- 2026-07-20 OpenRouter, one-task cloud boost, cloud-context defaults, and Bug Bounty/Chat workspace
  slices passed their focused/full gates and native UI smokes.
- 2026-07-15 control-plane/engagement races, full Go/vet/frontend/Wails gates, and workbench splitter
  live smoke passed; MAULER-AR-001 through MAULER-AR-005 remained closed.

Advance this baseline only after the stated checks genuinely pass.
