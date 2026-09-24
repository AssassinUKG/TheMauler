# Architecture

## Runtime shape

```text
React/Wails UI
    -> internal/app bindings and orchestration
        -> context compiler + agent router + control plane
        -> internal/llm client/backends
        -> compact internal/tools registry
        -> RunLedger / task logs / checkpoints / memory / SQLite
    <- Wails events for stream, tools, state, artifacts, channels, and UI refresh
```

The UI presents state but does not own provider secrets, authorization policy, evidence verdicts, or
tool-risk metadata. Go validates and persists those decisions.

## Package ownership

- `internal/app`: application composition, Wails bindings, agent loop, prompt construction, task-aware
  context, routing, provider selection, diagnostics, Telegram/channel runtime, task logs, and UI event
  emission.
- `internal/controlplane`: typed/sealed task contract, authoritative run phase transitions, mutation
  eligibility, verification checks, and completion eligibility.
- `internal/llm`: provider-neutral request/message/tool-call types and SSE accumulation. Backends
  translate provider wire formats without moving product policy into transport code.
- `internal/tools`: compact schemas/handlers, paths, metadata, preconditions, and result validation.
  Model-facing arguments remain compact; code owns risk, retry, and scope policy.
- `internal/ledger`: append-only UTF-8 JSONL event spine. New Brain/Ops/audit work should extend this
  spine instead of creating another partial logger.
- `internal/store`: SQLite schema, checkpoints, FTS, and structured persistence.
- `internal/repoindex`: code-owned repository roots/policy, streaming decode/chunk/hash manifests,
  immutable SQLite FTS5 generations, activation, incremental refresh, and bounded search.
  Incremental refresh copies chunks only after the current file SHA-256 matches the prior manifest;
  changed files are re-extracted inside the replacement transaction. Repository text is always
  untrusted; active pointers move only after a complete transaction commits. The app-owned watcher
  polls metadata cheaply, then invokes that same hash-verifying replacement path when it detects a
  candidate change.
- `internal/settings`: defaults, migrations, TOML/JSON contracts, profiles/providers, and secret
  storage. Providers and profiles remain separate concepts.
- `internal/engagement` and `internal/packlibrary`: authorised engagement objects, scope, evidence,
  and reusable pack definitions.
- `frontend/src`: stateful presentation and Wails calls. `App.tsx` is the event/state root; reusable
  workbench surfaces live in `components`.

## Run flow and state separation

1. Intake chooses workspace, agent definition, profile/provider, toolset, and task text.
2. The context compiler selects a bounded packet and records policy/byte/source provenance. An
   optional inspector pin may request Core, Relevant, or Expanded for one desktop task.
3. Go validates and seals the task contract. The control plane enters planning or an allowed phase.
4. The model receives only allowed tools and bounded operational context.
5. Tool calls pass policy, confirmation, scope, promptware, secret, and postcondition handling.
6. Observations and evidence are persisted; verification may repair, block, or permit completion.
7. RunLedger and task logs describe the run; terminal control-plane state authorizes completion.

Verified mutation handoffs also become inputs to the next task contract. Their normalized path and
captured SHA-256 form an immutable boundary for ordinary continuations. Only an explicit Fixer task
that names an exact protected path receives repair scope, and file tools plus shell, terminal and
Python-orchestration paths pass the same guard before a new verifier-owned handoff is possible.
Conversation epochs sit at the backend event boundary; retired emissions increment payload-free
diagnostic telemetry instead of reaching the transcript.

Keep these state types distinct:

- `TaskRun.State`: user-facing descriptive phase such as thinking, reading, editing, or testing.
- Control-plane phase: authoritative transition/mutation/completion eligibility.
- Tool/terminal state: execution-local status and visible process lifecycle.
- Channel queue state: remote request scheduling, not agent authorization.

## Context architecture

`internal/app/context_docs.go` currently discovers project instructions and chooses a code-owned
policy. `minimal_external_research` injects pointers, not repository content. `relevant_workspace`
compiles a bounded packet containing source provenance, head, heading index, and tail. The source
read allowance and prompt packet cap are intentionally different settings.

`docs/context/manifest.json` is the active M3 document-routing contract. Go strictly rejects unknown
fields, unsupported versions/status, missing files, duplicate or excessive routes/documents, unsafe
budgets, non-Markdown sources, workspace escapes, symlink escapes, and sources outside `AGENTS.md` or
`docs/context`. Each task selects a single best route and at most three documents. Selection is
deterministic, heading-aware, and bounded by both source-read and prompt-packet budgets.

The exact packet is constructed once per accepted task, logged, and installed into the primary
Mauler system prompt. Continuing chats refresh that primary packet instead of retaining stale task
context or duplicating unrelated control messages. Provenance
contains the manifest SHA-256, route, source SHA-256 values, source/prompt bytes, estimated tokens,
partial status, and selected line ranges. Invalid manifests use `AGENTS.md` as the deterministic
compact-core fallback. None of this routing state can alter tools, permissions, scope, approvals,
protected paths, or evidence requirements.

`PreviewContext`, `GetNextContextPacketClass`, and `SetNextContextPacketClass` expose a safe Wails
boundary for the Context Inspector. Preview construction is non-mutating: it does not mark memory as
used or return raw system instructions, memory/session/ledger contents, profile text, tool results,
credentials, or secrets. The UI receives only code-owned counts, estimates, warnings, reasons, and
trusted local-file provenance.

`RunContextQualityEval` uses the same manifest selector, mode router, tool router, prompt builder,
and tool-result guard as production without calling a model or mutating workspace files. It runs
embedded paraphrases repeatedly and treats packet/tool-schema identity drift as a failure. Its
evaluation envelope is canonical code defaults plus the selected profile and live workspace/context
configuration, so deliberate operator overrides such as Manual or Unrestricted do not masquerade as
default-product regressions. The
separate `RunAgentEvalRepeated` path uses the production agent loop and reports live pass^k and
aggregate recovery/completion/tool telemetry. The Benchmark UI keeps these deterministic and live
lanes visibly separate.

## Provider/profile boundary

A provider owns endpoint/backend transport and credentials. A profile owns model ID, context/output
budget, thinking mode, and sampling parameters. The client is constructed with a model; therefore
`llm.Request` has no `Model` field. OpenRouter keys never round-trip to React.

## UI event boundary

Go emits `mauler:*` events and Wails bindings. React listens with unknown argument arrays and validates
shape locally. Do not bypass this boundary with hidden parallel state channels when an existing event
spine or binding can be extended.

Run-owned events preserve their established positional payload and append an owner containing
`run_id`, a JavaScript-safe monotonic `generation`, a backend `conversation_epoch`, and the channel
`origin`. React accepts compatibility events without an owner, but current production events are
applied only when that owner matches the active run. Clearing, loading or switching the visible
conversation advances the backend epoch and retires its frontend owner, so late deltas, errors,
confirmations or terminal events cannot mutate the replacement view.
Desktop Chat accepts desktop-origin owners only; channel work retains its separate delivery lane.

## Session repair and resume

`internal/agent` owns deterministic conversation normalization. Immediately before model transport
it validates roles, removes invalid leading turns, safely merges same-role content, reconciles tool
pairs, marks empty content without inventing evidence, removes unresolved calls, collapses exact
stale controller continuations, and deduplicates only exact same-ID tool calls. Changes are reported
as typed phase/actions and mirrored to RunLedger; unusable packets are rejected before transport.

Saved-session inspection uses the same pipeline without mutation. Explicit repair retains a `.bak`
and refreshes recall; ordinary Load repairs only its in-memory view. A checkpoint resume receives a
new run ID/generation and persists the interrupted run as `parent_run_id`.
