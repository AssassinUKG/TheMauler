# Mauler Native LSP Code Intelligence Plan

Status: approved planning direction; implementation not started
Date: 2026-07-20
Scope: TheMauler only; native Go backend and existing React/TypeScript desktop UI
Related: `docs/repository-intelligence-parity-plan-2026-07.md`,
`docs/mauler-agent-control-plane-improvement-plan-2026-07.md`,
`docs/agentic-reliability-issues-2026-07.md`

## Executive decision

Mauler should implement a native Language Server Protocol client and supervisor. It should use
existing language servers rather than implement languages itself. The first release is read-only:
diagnostics, symbols, hover, definitions and references for the editor and the agent. Rename and
code-action mutations come later, only through Mauler's existing scope, approval, snapshot,
rollback and verification paths.

This is a natural fit because Mauler already has a Monaco editor, workspace-authoritative file
handling, an agent tool registry, RunLedger and a native control plane. LSP adds semantic code
understanding that syntax colouring, grep and corpus retrieval cannot provide.

The feature must remain local-first and UI-only for the operator:

- no language-server CLI workflow is required from the user;
- no Python, LangGraph, Node-based LSP sidecar, additional frontend build system, localhost
  service or second project store is added;
- language-server processes run locally and are owned by Mauler;
- no source is sent to an online provider merely because LSP is enabled; and
- missing, stopped or incompatible servers cause visible degradation to current behaviour, not a
  broken editor or blocked agent run.

## What LSP owns and what it does not

| Capability | Owner |
| --- | --- |
| Arbitrary file/document/archive extraction | Files & Knowledge / `internal/repoindex` |
| Exact text and filename search | `grep`, `glob`, FTS retrieval |
| Symbols, definitions, references, hover and semantic diagnostics | LSP |
| File writes and cross-file edits | Existing Mauler write/edit, snapshots and rollback |
| Authorization, scope, approvals and legal phases | `internal/controlplane` and tool policy |
| Correctness | Builds, tests and deterministic acceptance checks |
| Evidence history | RunLedger, task run and immutable evidence records |

LSP diagnostics are strong, useful evidence but are not proof that the complete application works.
Passing diagnostics cannot replace a required build, test, browser assertion or engagement check.

## Existing behaviour that must be preserved

1. Monaco stays responsive and retains syntax selection, diff, preview, formatting, minimap,
   Run/Stop, save and open-file tabs.
2. Explorer, Inspector, bottom-work-area and Terminal/AI Commands drag handles, collapsed-edge
   reopening, keyboard resizing and persistence remain unchanged.
3. Workspace selection remains authoritative. Changing workspace cancels LSP requests, closes old
   documents and restarts only the servers required by the new workspace.
4. File tools continue to normalize Windows, WSL and Linux path forms.
5. The Files & Knowledge index remains the complete-corpus layer. LSP must not reintroduce a
   100 KB read ceiling or claim that an unprocessed large file was covered.
6. Current Go/Python/shell lint, project verification, mutation snapshots and rollback continue to
   work when LSP is absent or unhealthy.
7. Tool schemas remain compact. Do not expose one model-facing tool per LSP method.
8. InferenceBridge remains the default local provider. A one-task OpenRouter boost remains explicit
   and non-sticky.
9. Existing task contracts, RunLedger and completion gates remain authoritative. A language server
   cannot authorize an action or declare a task complete.
10. Existing generated Wails bindings and frontend behaviour must round-trip without losing settings.

## Product behaviour

### Settings > Code Intelligence

Add one UI-owned page or settings section with:

- master enable switch, default on only for trusted workspaces;
- detected workspace languages and project markers;
- installed/detected server cards with `Ready`, `Starting`, `Busy`, `Stopped`, `Missing`,
  `Crashed`, `Disabled` and `Unsupported` states;
- server executable picker and validated auto-detection;
- approved-source managed install/update/remove actions where a safe implementation exists;
- per-language enable, preferred server, startup, memory and timeout controls;
- current process, version, workspace, last error, restart count and recent request latency;
- `Restart`, `Stop`, `Open log` and `Test` actions; and
- an explicit trust explanation before starting a server in a newly selected workspace.

The normal workflow must not require terminal commands or hand-edited configuration. Advanced
paths and server arguments may be edited in the UI, with risky options clearly marked.

### Monaco editor

When the selected server advertises the capability, provide:

- diagnostics as Monaco markers and a compact Problems list;
- hover information;
- document/workspace symbols;
- go to definition and peek definition;
- find references;
- completion and signature help;
- semantic tokens without removing Monaco's normal syntax colouring;
- status-bar server health and document-sync state; and
- F12, Shift+F12 and F2-compatible navigation, with visible buttons for non-technical use.

If an external TypeScript/JavaScript server is active, deduplicate or disable overlapping Monaco
worker diagnostics so the user does not receive two copies of each error.

### Agent code intelligence

Add one compact model-facing tool, provisionally `code_intel`, with actions:

- `status`
- `symbols`
- `definition`
- `references`
- `hover`
- `diagnostics`
- `rename_preview` (later milestone)

Go derives workspace, server, trust, document version, budgets and permitted paths. Results are
bounded and return exact file/line ranges, source server, document/file version and evidence ID.
Large result sets use an opaque continuation/result ID rather than flooding model context.

The tool is read-only through the first production release. `rename_preview` only returns a
versioned workspace edit. Applying it is a separate controller-authorized mutation that reuses the
normal write/edit and rollback machinery.

## Native architecture

### `internal/lsp`

Create a focused native Go package rather than adding protocol handling to `internal/app/app.go`:

```text
internal/lsp/
  protocol.go       typed JSON-RPC/LSP request and response envelopes
  framing.go        Content-Length framing and bounded message reader
  client.go         request IDs, cancellation and capability negotiation
  supervisor.go     process lifecycle, backoff, health and shutdown
  registry.go       language/project markers and server descriptors
  documents.go      URI, version and open/change/save/close state
  results.go        normalized locations, diagnostics and continuation IDs
  evidence.go       content-addressed diagnostic/symbol snapshots
  pathmap.go        Windows/WSL URI and canonical-root handling
```

The app layer owns Wails bindings, events, settings projection and integration with tools,
RunLedger and the control plane. The frontend never talks directly to a language-server process.

### Protocol and lifecycle requirements

Support the required LSP lifecycle and only negotiated capabilities:

- `initialize`, `initialized`, `shutdown` and `exit`;
- `textDocument/didOpen`, `didChange`, `didSave` and `didClose`;
- `textDocument/publishDiagnostics`;
- hover, definition, references, document symbols, workspace symbols and completion;
- request cancellation and out-of-order responses; and
- server messages/logs through a bounded, redacted diagnostic channel.

Begin with full-document synchronization if needed for correctness, then add incremental sync when
the server advertises it. Document versions must be monotonic. Position conversion must be tested
with Unicode and the server's negotiated encoding; Monaco commonly uses UTF-16 positions.

Never automatically accept server-initiated `workspace/applyEdit` or arbitrary `workspace/executeCommand`.
Convert proposed edits to a preview or reject the request with an explicit reason.

### Server registry

Use code-owned descriptors rather than model-generated commands. A descriptor includes:

- stable server and language IDs;
- executable and fixed argument template;
- supported extensions and workspace markers;
- Windows/WSL execution host;
- initialization options allowlist;
- default startup/idle/response timeouts;
- restart and crash-loop budget;
- trust/risk note; and
- version probe and readiness check.

Initial language priority:

1. Go (`gopls`) and TypeScript/JavaScript;
2. Python and Rust;
3. PowerShell, shell, JSON and YAML; and
4. additional servers through reviewed descriptors.

The internal M1 slice may begin by detecting developer-installed servers, but a language is not
user-ready until Mauler can set it up, select an existing executable and remove/repair it entirely
through the UI. Managed installation may download only pinned, checksummed releases from approved
sources into a Mauler-owned directory. It must not execute an untrusted repository script as an
installer or make a terminal command part of the normal operator workflow.
An external language server may require its own runtime, but that remains an optional local tool,
not a new Mauler backend or frontend build dependency.

### Process and request management

- Share at most one healthy server per normalized workspace, execution host and server ID.
- Start lazily when a supported file opens or a semantic request arrives.
- Hide child console windows on Windows.
- Use bounded stdout/stderr readers, request queues, timeouts and cancellation.
- Apply exponential crash backoff with a hard circuit breaker; no infinite restart loop.
- Shut down cleanly on workspace change and application exit, then terminate after a bounded grace
  period if the server does not exit.
- Debounce editor changes and discard stale diagnostics for older document versions.
- Cache immutable read-only results by workspace, server, file hash/document version and request.

LSP should not eagerly open every repository file. Files & Knowledge performs corpus scanning;
language servers manage their own workspace model while Mauler synchronizes open or queried
documents.

### Paths, drafts and large files

- Canonicalize every file URI and reject results outside the selected workspace unless an explicit
  reviewed library-location policy permits navigation.
- Keep Windows-native and WSL server hosts distinct. Do not send a `C:\...` URI to a WSL process
  without the tested path mapper, or `/mnt/c/...` to a Windows process.
- Treat an unsaved Monaco buffer as a versioned draft. Diagnostics and hover may use that draft;
  file-hash evidence must say whether it describes disk content or a draft version.
- Before an agent mutation touches a dirty open editor buffer, require reconcile/save/discard
  handling rather than silently overwriting it.
- Do not add an implicit 100 KB file cap. If a particular server refuses or performs poorly on a
  file, show the server-specific limitation and fall back to normal reading/indexing. Never report
  LSP coverage for a file the server rejected.

### Security and information flow

Language servers are local programs with workspace read access and may interpret project config.
Treat them as privileged workspace tooling:

- require trusted-workspace approval before first start;
- launch only an allowlisted executable selected/detected through the UI;
- never interpolate repository text into a shell command;
- restrict cwd and canonical file requests to the approved workspace;
- record process start/stop/crash/version without leaking source or secrets;
- label diagnostics, hover and documentation text as untrusted project/tool content;
- preserve prompt-injection and secret-exfiltration guardrails before model use;
- do not permit server output to widen scope, enable tools or authorize upload; and
- make it visible when code excerpts will enter an explicitly selected cloud-model task.

## Evidence and control-plane integration

A normalized read-only result should carry:

```go
type CodeIntelEvidence struct {
    ID              string
    WorkspaceDigest string
    ServerID        string
    ServerVersion   string
    Method          string
    Path            string
    FileSHA256      string
    DocumentVersion int
    ResultDigest    string
    CreatedAt       time.Time
}
```

RunLedger records lifecycle, health, request class, latency, cancellation, bounded result metadata
and evidence IDs. It must not copy whole source files or full completion lists into JSONL.

Controller policy:

- symbol/hover/definition/reference/diagnostic requests are read-only observations;
- diagnostics may satisfy an acceptance check only when the contract explicitly names that
  verifier and the evidence matches current disk hashes;
- draft-only evidence cannot prove a saved deliverable;
- stale diagnostics are refreshed rather than treated as current;
- conflicting LSP/build results trigger a targeted independent check; and
- rename/code-action application requires an acting/repairing phase, scope validation, approval
  where configured, snapshots and post-write verification.

## Milestones

### M0 - Protocol contract and non-regression harness

- Define typed protocol, normalized result and server descriptor contracts.
- Build a deterministic fake stdio language server covering fragmented frames, malformed messages,
  out-of-order responses, delayed responses, crashes, cancellation and Unicode positions.
- Capture baseline editor, workspace-switch, file-tool, layout and verification behaviour.
- Gate on zero behaviour change when Code Intelligence is disabled or no server is installed.

### M1 - Read-only native service and compact tool

- Implement lifecycle, framing, request routing, cancellation, health and capability negotiation.
- Add Go/TypeScript/Python/Rust descriptors and UI-based detection.
- Implement `status`, `symbols`, `definition`, `references`, `hover` and `diagnostics`.
- Add bounded results, continuation IDs, path/scope checks and RunLedger events.
- Keep existing lint and grep fallbacks active.

### M2 - Monaco integration and UI oversight

- Wire document lifecycle and versions from Monaco through Wails.
- Add markers, Problems, hover, definitions, references, symbols and completion.
- Add Settings > Code Intelligence, editor health indicator and recovery controls.
- Complete UI-managed setup/select/repair/remove for the first user-facing Go and
  TypeScript/JavaScript servers; do not publish a CLI-only setup path.
- Test open/save/diff/preview/run behaviour and every movable workbench separator.

### M3 - Evidence and verification integration

- Persist immutable diagnostic/symbol evidence IDs tied to current file hashes.
- Add explicit task-contract acceptance-check support without replacing builds/tests.
- Add stale-result refresh, conflict classification and control-plane recovery routing.
- Add Doctor checks for server discovery, process health, path mapping and capability readiness.

### M4 - Broader managed servers and execution hosts

- Extend pinned/checksummed managed installers where licensing and packaging permit.
- Add PowerShell, shell, JSON and YAML descriptors.
- Add WSL-hosted servers only after path, URI, cancellation and shutdown fixtures pass.
- Add per-language resource/timeout presets and observable large-file refusal handling.

### M5 - Safe edit previews and live hardening

- Implement `prepareRename`, rename preview and supported code-action preview.
- Translate workspace edits into version-checked Mauler mutations; never auto-apply server edits.
- Snapshot all affected files, reject stale/draft conflicts, and run diagnostics plus required
  build/tests after application.
- Run representative repositories 5-10 times and record pass^k, stale-result rate, duplicate
  diagnostics, crashes, restarts, latency, memory, wrong-root responses and recovery success.

## Required verification gates

### Always-required existing gates

- `go test ./... -count=1`
- `go vet ./...`
- `go test -race ./internal/app ./internal/tools -count=1`
- `go test -race ./internal/controlplane ./internal/store -count=1`
- `npm run --prefix frontend build`
- production `wails build`

Repository-wide frontend lint remains a known non-gate until its existing backlog is fixed.

### LSP-specific gates

- Zero automatic edits from server-initiated requests.
- Zero out-of-workspace locations accepted without an explicit library policy.
- Zero stale diagnostic snapshots accepted as current evidence.
- Zero leaked child processes after workspace change, app exit or test cancellation.
- Zero infinite restart/retry loops.
- No duplicate diagnostics when Monaco and an external server overlap.
- No editor failure, blocked run or changed tool result when no server is available.
- No silent large-file exclusion or false LSP coverage claim.
- Unicode, spaces, Windows drive letters and WSL path mappings round-trip correctly.
- Existing layout resize/persistence and Terminal/AI Commands behaviour pass live smoke tests.

## Rollout and rollback

- Ship behind a persisted `code_intelligence.enabled` setting and per-language switches.
- Default to read-only capabilities; mutation previews have a separate feature flag.
- Keep native lint, grep and repository-index fallbacks through all milestones.
- A crash circuit breaker disables only the affected server and explains the fallback.
- Schema additions must be backward-compatible; old settings load with safe defaults.
- Rollback is disabling/removing the LSP integration layer, not reverting Monaco, tools, index,
  control plane or storage.

## Immediate implementation order

1. Keep the existing stability/race gate green and land the Files & Knowledge M0/M1 foundation.
2. Implement M0 fake-server fixtures and protocol contracts without touching editor behaviour.
3. Land M1 read-only service plus `code_intel` behind a disabled-by-default feature switch.
4. Add Monaco and complete UI-only server setup in M2, first for Go and
   TypeScript/JavaScript.
5. Connect immutable evidence and Doctor checks in M3.
6. Add managed setup, WSL and broader languages only after native Windows lifecycle tests pass.
7. Add mutation previews last, after stale-edit, rollback and multi-file verification gates pass.

The first useful increment is a local, optional, read-only semantic service. It must improve code
understanding without becoming a new requirement for opening, reading, indexing, editing or
verifying files.
