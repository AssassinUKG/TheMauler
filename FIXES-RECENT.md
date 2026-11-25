# TheMauler — FIXES-RECENT

Date: 2026-11-23  
Status: **IMPLEMENTED AND VERIFIED; EXPLICITLY PENDING ITEMS REMAIN OPEN**

## Purpose

This file is the staged update and reliability record for TheMauler. The user approved carrying out
the fixes in this document. The implementation preserves existing settings and keeps the few actions
that require separate authority visibly open rather than treating them as silently completed.

## Telegram best-of-both follow-up — 2026-11-23

The working Mauler action routing/search/queue/final-answer pipeline is preserved. General news
queries gain the same dated BBC/Sky feed path as Helix, with ordinary search retained on failure
and explicitly configured search services left in control. Jade's presentation now supplies one
editable progress card, a verbatim long-reply preview, Full report, exact Markdown download and
tool-observed Sources. Result buttons are bounded, expiring, one-shot and bound to the original
user/chat/topic/acknowledged message. They never start a model, task, command or browser.

Formatting splits balanced HTML by visible UTF-16 units without cutting entities or emoji. Delivery
uses a bounded explicit 429 retry; disconnects, 5xx, malformed/missing acknowledgements and uncertain
edits are not blindly resent. Uncertain initial progress does not create duplicate heartbeat cards.
Topics and reply targets are preserved. Report buttons expire after 15 minutes or process restart;
uncached/oversized/unbound reports retain full ordinary delivery instead of hiding unavailable text.

The operator's exact news wording passes against the existing loaded local model and real web tools.
Sources come from observations even when the model does not repeat URLs in prose. No cloud model,
production test message, account transfer, reboot or credential change is involved. Evidence and
remaining gates: `docs/telegram-news-parity-2026-11-23.json`.

## Native lifecycle, crash recovery and complete replies — 2026-11-17

Native testing found additional defects beyond the earlier headless matrix:

- Commit a recovery snapshot before the first model request and before effectful tool execution.
  Reconcile interrupted runs at startup; retain the original task contract and generation. Safe
  text/read-only recovery forks a linked run. An uncertain write is review-required, never replayed.
- Own the native data directory before opening SQLite; join cancellation before shutdown and keep
  the app busy through transcript/terminal persistence. Session saves use same-directory atomic
  replacement, and terminal checkpoint retirement shares the task-run transaction.
- Workspace changes reset the backend's saved-chat identity as well as the UI. A new workspace or
  recovered run cannot overwrite the original saved conversation.
- Preserve visible partial output and distinguish user stop, output limit and failure. Long replies
  receive a context-aware budget up to 8,192 tokens, with at most three tool-free continuations.
  No-thinking no longer imposes a hidden 1,024-token answer/tool-argument cap. Saved and live Chat
  retain the complete assembled answer; exhausting a limit is not successful completion.
- Filename words such as `append-fixture.txt` no longer select live sports/weather routing and
  remove the file-read tool. Windows PTY startup resolves an absolute executable and uses one
  process waiter rather than the dependency's racing context waiter.

The bounded Windows qualification and its explicit reboot/descendant/unattended limits are recorded
in `docs/mauler-native-lifecycle-reliability-2026-11-17.json`. This supersedes the native-process
portion of MAULER-AR-009's old "not tested" status, not its broader qualification requirements.
Fresh current-source local regression passed 13/13 with every request joined. The same loaded
RVN model/35,072-token context ended idle; normal settings/keys are unchanged and Mauler is stopped.

## Local-only chat, actions and cancellation qualification — 2026-11-17

- Fixed intent collisions that made ordinary chat invoke tools, treated `fixture`/`deliverable` as
  live-data requests, or withheld file tools when the result should be delivered in Chat.
- Added original-contract exact-byte verification for bounded explicit JSON-string create/append
  requests, including the original append prefix, native write evidence and finalized handoff.
  Mixed goals retain ordinary verification. Exhausting a budget no longer skips blocking review.
- Removed the shared backend stop call: cancellation now closes only its own HTTP request/stream.
- Added a reproducible opt-in harness using the real production agent loop, local transport, native
  file tools and control plane in isolated disposable state. The normal app and cloud models are
  never started; the already loaded local model/context cannot be switched by this harness.
- Diagnostic runs exposed real routing/completion defects and harness metadata/context issues.
  After fixes, the stronger v2 grader passed 13/13, then five repeats passed 65/65. Ten cancelled
  requests each recovered with a fresh model-backed reply. The final guard-only audit rechecked
  80 retained run contracts and 10 current artifacts with zero model calls or effect replay.
- Current-source full Go tests, vet, expanded race gate and offline Wails production build pass.
  Production settings/credentials are byte-for-byte unchanged; dependencies were not upgraded.
  The rebuilt `build/bin/TheMauler.exe` is left stopped.

Evidence and binary/log hashes: `docs/mauler-local-reliability-2026-11-17.json`.
Canonical issues: MAULER-AR-006 through 009 in `docs/agentic-reliability-issues-2026-07.md`.
Native Wails UI acceptance, whole-app crash/reboot, browser/remote and full unattended Gate 1/x5
qualification remain open; 65/65 is not a claim that those broader gates passed.

## NanoCyber sandboxed-subagent addendum — 2026-10-29

TheMauler's existing bounded subagents remain the orchestrator. MN-1 adds the versioned, fail-closed
NanoCyber lease contract in `internal/nanocyber`; MN-2 now adds disabled-by-default strict-loopback
settings, protected service-token storage, a dedicated Settings > Sandbox surface, and shared
Doctor/Services health and capability checks. Token values never return to React and the password
input is not held in React state. Probes bypass proxies, reject redirects/non-loopback resolution,
bound JSON responses, and require the exact v1 lease/capability contract before reporting ready.

The companion NanoCyber tree now contains `/healthz` and an authenticated `/v1/subagent/leases`
service plus an opt-in desktop runtime, bounded scheduler, exact-lease cancel/complete, parent
cancellation, cleanup, and restart/expiry reconciliation. It does not yet expose the required
capability inventory, consumer typed-job/receipt/status endpoints, or a binding between Mauler
Engagement Grid IDs and NanoCyber-native `eng_...` authorities. TheMauler therefore reports it
incompatible and advertises no sandbox execution.

The MN-3 client foundation is now staged: Test/Fix declares `offline-code`; admission requests bind
the exact run generation/epoch and subagent attempt; and strict loopback admission/lifecycle clients
validate request digests, opaque IDs, lease lifetime, cleanup acknowledgements, and classified API
errors. If NanoCyber is explicitly enabled before the remaining integration is ready, Test/Fix stops
before model execution, records `host_fallback=false`, and does not silently run on the host. Live
lease creation, typed jobs, receipt acceptance, and completion remain intentionally unwired.

## Chat output-boundary repair — 2026-10-28

- Fixed a local-backend failure mode where a valid answer could be followed by a plain-text echo of
  previous conversation turns, the private `[control_plane]` packet, and the attachment safety
  wrapper. The backend now detects exact request-owned prompt markers and conversational anchors
  while streaming, replaces any already-started echo with the clean answer, and persists only the
  user-visible prefix.
- The guard covers partial streaming chunks and CRLF output without relying on chat-template tokens,
  records only a payload-free suppression event, and also protects the optional escalation model.
- Loading a contaminated saved conversation now scrubs echoed prompt material in memory before the
  next model request. Every private system/controller message is excluded from the loaded Chat
  transcript; typed run events remain the only UI channel for controller state.
- Focused regressions reproduce the observed output, partial streaming, ordinary requested
  repetition, user discussion of the literal marker, CRLF offsets, saved-turn repair, and private
  system-message exclusion. Repository-wide tests, vet, the app/tools race gate, and the frontend
  production build pass.

## Implementation update — 2026-10-25

Completed in the dedicated `codex/fixes-recent-go127` branch:

- Created and SHA-256 verified a source checkpoint before migration.
- Patched the production DOMPurify dependency, upgraded Monaco separately, and moved Monaco/editor
  routes out of the initial bundle with an automated lazy-load check.
- Updated Vite to 8.3.5 and resolved every remaining development-only advisory. The full production
  and development dependency audit now reports zero vulnerabilities, and the lazy-load check measures
  the complete initial JavaScript graph instead of only a tiny generated entry wrapper.
- Updated Wails to 2.17.0, the direct security/browser/storage/utility module groups, and Go to
  1.27.3 through the ordered intermediate gates described below.
- Updated the unrestricted profile to the exact installed RVN GGUF while leaving aligned Qwen as
  the daily default; the exact RVN required-tool regression passed and the prior daily model was
  restored afterward.
- Added presence-only provider authentication diagnostics and fixed InferenceBridge port diagnosis
  so the highest-precedence 8804 configuration is authoritative over stale lower-precedence files.
- Added bounded scanned-PDF OCR and metadata-only 7z inventory extraction. OCR is local, page/output/
  time bounded, and explicitly reports non-coverage when Poppler or Tesseract is unavailable.
- Added three-size native screenshot acceptance and workspace-owned, SHA-256-sealed browser evidence
  manifests with stale-evidence detection and explicit Engagement promotion.
- Added exact built-in/custom agent definition ID/version provenance to task-run state, SQLite v19,
  resumed checkpoints, Logs, and every run-owned RunLedger event; custom UI agents now execute as
  their selected definition instead of silently collapsing to Auto.
- Split trusted operator routing text from untrusted attachment bodies. Hostile JavaScript comments,
  HTTP headers, and HTML bodies can still be analysed by the model but cannot enable agents, skills,
  memory, tools, task mutations, target inference, or completion requirements; five-repeat fixtures
  cover each source and checkpoint resume preserves the boundary.
- Added `govulncheck`, production npm audit, the required race gate, and a Windows Wails production
  build to CI.

Still open by design:

- Copying the existing InferenceBridge API key into Mauler's secret store requires the user's exact
  confirmation. The credential has not been copied, printed, logged, or committed.
- Logging/redaction, root-shell, unrestricted-tool, and normal-run budget changes require a separate
  settings decision and have not been changed.
- The live UI Gate 1 remains 11/12 in the latest recorded evidence. A fresh 12/12 followed by five
  clean rounds and a supervised browser pass^k is still required; this update does not claim it.

The complete checkpoint, version, test, build, startup, and binary-hash evidence is recorded in
`docs/fixes-recent-execution-2026-10-25.md`.

## User settings that must remain unchanged

Preserve the active files under `~/.config/mauler` and their existing values, including:

- The InferenceBridge endpoint and port `8804`.
- All local/LAN choices. Do not replace the configured host, port, or provider with a repository
  default such as port 8800.
- Profiles, model choices, context, thinking, MTP, KV-cache, default autonomy, task routing, toolset,
  workspaces, shell/WSL selection, time/call budgets, Telegram/audio, logging, and UI layout.
- The OpenRouter one-task boost behaviour and provider secret precedence.
- Existing unrestricted profiles and engagement workflows.

Security settings such as secret redaction, confirmations, root execution, or tool scope may be
recommended below, but they require a separate explicit decision and must not be silently changed
during a dependency upgrade.

## Current verified baseline

- Repository branch at execution: `codex/fixes-recent-go127`.
- Last committed baseline observed: `8ebe2f8aea1180b7b0895da5f4decad8fb32b6a8`.
- Working tree at audit time: 39 modified/staged entries and 9 untracked entries. Treat them as user
  work.
- `go test ./... -count=1` passed.
- `go vet ./...` passed.
- Frontend type-drift check and production build passed. The main frontend chunk was approximately
  617 KB and is a later code-splitting candidate.
- `govulncheck ./...` found zero reachable vulnerable symbols. It still identified advisory-bearing
  imported/module versions that should be upgraded.
- The current live Agent Eval promotion gate is not complete: the last recorded UI Gate 1 was
  11/12, so pass^5 remains locked.

## Immediate issues

### M1 — Protect the current work before dependency changes

1. Create a checkpoint branch or verified archive.
2. Separate existing source changes from generated bindings/build output.
3. Record current settings/profile schema hashes, Go/Wails/Node versions, active provider/model,
   runtime lock, and the full green test commands without storing secrets.
4. Ensure every dependency phase has a single-command rollback to its prior `go.mod`, `go.sum`,
   `package.json`, lockfile, generated bindings, and Wails build.

### M2 — Fix the critical frontend dependency advisory

Mauler's frontend overrides DOMPurify to 3.4.12 through Monaco. The production audit reported one
critical and one moderate finding.

Plan:

1. Update the explicit DOMPurify override to 3.4.17 or a newer patched compatible 3.4.x.
2. Keep Monaco 0.56 initially; do not combine the sanitizer fix with Monaco's breaking ESM changes.
3. Run `npm audit --omit=dev`, type-drift, editor-tab tests, TypeScript, production build, and manual
   untrusted Markdown/HTML/SVG/Monaco-hover probes.
4. Consider Monaco 0.57 in a separate later phase with editor, diff, LSP, tab, persistence, and
   generated-binding verification.

Acceptance: no high/critical production advisory and no model/tool/file content can create an
executable WebView payload.

### M3 — Repair InferenceBridge authentication

InferenceBridge's live server requires an API key. Mauler's provider secret store currently has an
OpenRouter entry but no `inference-bridge` entry, and the provider's `api_key_env` is empty.

Plan after explicit approval:

1. Store the existing InferenceBridge credential using Mauler's provider-secret API, or name an
   environment variable in the provider profile.
2. Never write the credential to `profiles.toml`, React state, logs, prompts, exported evidence, or
   this repository.
3. Add/retain a presence-only diagnostic.
4. Verify authenticated models, chat, streaming, cancellation, model load and image paths.

Do not solve this by disabling InferenceBridge authentication.

### M4 — Repair the unrestricted model identity

The saved unrestricted profile references `Qwen3.8-27B-Uncensored-Q4_K_M.gguf`, while the installed
working artifact uses the exact RVN filename and the runtime lock already records that override.

Plan:

1. Select the exact installed RVN GGUF in the profile or introduce a tested stable alias in
   InferenceBridge.
2. Validate the exact template, thinking, preserved thinking, context, MTP x2, required tool call,
   argument repair, cancellation, and reload behaviour.
3. Keep aligned Qwen3.8 as the reliable daily profile and RVN as the deliberate unrestricted lane.
4. Update Bug Bounty/Ops profile references only after the exact RVN route passes.

### M5 — Review logging and host privileges separately

Current live settings include secret redaction disabled, full response/tool logging, an unrestricted
toolset, long run/tool budgets, and a Kali/WSL root shell. These are not dependency-upgrade fields.

Recommended separate review:

- Enable secret redaction while retaining model capability.
- Keep the unrestricted toolset only for explicit profiles; use semantic narrowing and the existing
  scope/control-plane gates for normal work.
- Use a non-root WSL user for routine runs and a disposable/root profile for exceptional work.
- Keep the 200-call/1800-second budget only for deliberate long operations; use a lower normal
  budget to bound loops.
- Define ledger rotation/retention and sanitize existing logs only after the user chooses what must
  be retained.

No item in this subsection may be changed as a side effect of the update phases.

## Dependency update phases

### Phase M-A — Go security patch within the current release

Update the toolchain directive from Go 1.26.3 to Go 1.26.10 first. This is the smallest security
update and should precede a Go 1.27 migration.

Gate:

```powershell
go test ./... -count=1
go vet ./...
go test -race ./internal/app ./internal/tools -count=1
npm run --prefix frontend build
wails build
```

### Phase M-B — Wails 2.17 security update

Update Wails 2.12.0 to 2.17.0. The release hardens `wails dev` against cross-origin IPC WebSocket
access and DNS rebinding and removes a PowerShell notification fallback.

Review and test:

- Generated bindings and the 406-field settings model remain in sync.
- App startup/shutdown, WebView2 IPC, notification behaviour, window/layout persistence, dialogs,
  file operations, terminal sessions, browser workflows, Telegram/audio startup ownership, and
  production packaging.
- `wails dev` is tested from the normal loopback workflow without adding broad allowed hosts.
- Do not migrate to Wails 3 while it is beta.

### Phase M-C — Direct Go module security/maintenance updates

Upgrade direct modules in small groups, not with one blanket `go get -u`:

1. Security group: `golang.org/x/crypto` 0.51.0 to current tested 0.58.x and
   `golang.org/x/text` 0.37.0 to current tested 0.43.x. `govulncheck` found no reachable vulnerable
   calls, but the old versions contain advisory-bearing packages/modules.
2. Browser group: `chromedp` 0.15.1 to 0.21.x plus a compatible `cdproto`. Run native browser,
   cancellation, evidence, download, promptware, and supervised pass^k tests.
3. Storage group: `modernc.org/sqlite` 1.50.1 to 1.61.x. Run migrations against copied databases,
   WAL/restart/recovery, ledger, memory, sessions, and concurrency/race tests.
4. Utility group: TOML 1.4 to 1.6 and the PDF extractor to a reviewed commit, with settings
   round-trip and bounded-file fixtures.

Commit and verify each group independently. Do not update unrelated indirect modules merely because
a newer tag exists.

### Phase M-D — Go 1.27.3 candidate

After all previous phases are stable, update the `go` directive/toolchain to 1.27.3 on a dedicated
branch. Run the complete backend, race, frontend, native Wails, browser, provider, settings migration,
and agent reliability gates. Do not combine it with Wails or module-version changes.

### Phase M-E — Frontend maintenance and code splitting

Only after the sanitizer fix:

- Evaluate Monaco 0.57 and `@monaco-editor/react` compatibility separately.
- Split heavy workbench/editor routes so Monaco is not in the initial path when unused.
- Preserve resizable panels, tabs, generated bindings, editor state, LSP, diffs, and narrow layouts.
- Keep the current React/Vite/TypeScript versions when already current unless a concrete advisory or
  fix justifies movement.

## Reliability completion plan

The app's deterministic gates are strong, but live model promotion is unfinished.

Required order:

1. Make one deliberate model/profile/control change at a time.
2. Run fresh UI Gate 1 and achieve 12/12.
3. Run five independently clean rounds: 60/60 total attempts. Any failed round is not pass^5.
4. Run the rebuilt-native M1 lifecycle-race and M3 scoped-repair/handoff UI smokes.
5. Run a supervised local-model browser pass^k.
6. Complete Telegram/audio live smokes and prove remote delivery matches the final desktop answer.
7. Preserve exact model, profile, settings, binary, scenario, and evidence hashes in the result.

Do not call the selected profile unattended-reliable before this evidence exists.

## Remaining product work

- Add optional InferenceBridge embeddings only after the bounded extractor baseline remains stable.
- Run the supervised local-model browser pass^k and the complete live Agent Eval promotion sequence.
- Complete the separately authorised InferenceBridge secret-store repair and then run authenticated
  models/chat/stream/cancellation/load/image smokes against the preserved 8804 endpoint.
- Consider richer browser-evidence inspection only after operator use identifies a concrete gap; the
  immutable review and explicit Engagement promotion baseline is now implemented.

## Cross-project coordination

Required order:

1. Checkpoint all three dirty repositories.
2. Fix InferenceBridge's critical frontend advisory while retaining the current API/config.
3. Repair Mauler's InferenceBridge secret and exact model identity after approval.
4. Apply Mauler's DOMPurify fix.
5. Move through Go 1.26.10, Wails 2.17, direct module groups, then Go 1.27.3.
6. Qualify against the working InferenceBridge runtime before testing any llama.cpp candidate.
7. Run Gate 1 and pass^5 only on the final exact binary/runtime/profile combination.

## Required final verification

```powershell
go test ./... -count=1
go vet ./...
go test -race ./internal/app ./internal/tools -count=1
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
npm ci --prefix frontend
npm audit --prefix frontend
npm run --prefix frontend check-types
npm run --prefix frontend test:editor-tabs
npm run --prefix frontend build
wails build
```

Repository-wide frontend lint remains a documented non-gate until its generated/legacy baseline is
cleaned up; do not falsely report it green.

## Explicitly out of scope until separately approved

- Changing port 8804, host/bind choice, provider order, local/LAN behaviour, or model defaults.
- Rewriting existing profiles/settings with fresh defaults.
- Enabling/disabling secret redaction, confirmations, root shell, unrestricted tools, logging, or
  budgets as a hidden migration.
- Deleting configs, backups, state databases, run evidence, complete models, or rollback builds.
- Wails 3, blanket `go get -u`, or a combined Go/Wails/browser/storage migration.

## Execution checklist

- [x] Checkpoint/backup created and verified.
- [x] DOMPurify patch implemented.
- [ ] InferenceBridge credential transfer explicitly confirmed and completed.
- [x] Exact RVN model identity updated and tested.
- [ ] Optional logging/privilege changes reviewed separately.
- [x] Go 1.26.10 intermediate phase passed.
- [x] Wails 2.17 phase passed.
- [x] Direct-module groups migrated in order.
- [x] Go 1.27.3 candidate implemented on a dedicated branch.
- [ ] Fresh live Gate 1 12/12 and pass^5 completed.

