# Context packet reduction implementation plan - 2026-07

## Objective

Reduce Mauler's always-on prompt cost without losing project constraints, current state, or task
quality. Large files must remain fully readable through UI-driven agent tools; reducing automatic
injection must never become a file-size or file-format restriction.

The target architecture is a small exact core, task-aware document selection, bounded excerpts, and
visible source/token accounting. A model-generated synopsis is advisory only and must never replace
code-owned safety rules or hand-curated non-negotiable constraints.

## Starting state

- `AGENTS.md` is approximately 65 KB and is the canonical project handoff.
- `MAULER.md` is approximately 8.5 KB, contains useful older material, and also contains stale or
  duplicate provider/runtime guidance.
- Context was configured to discover up to 64,512 bytes of project documents. Before the first
  bounded compiler hotfix, both files could consume roughly 26,000 estimated system tokens.
- The prompt compiler now caps project instruction content at 16 KiB, but intent-aware source
  selection and source-document restructuring are still needed.

Current implementation after the first slice:

- Intent-aware `minimal_external_research` and `relevant_workspace` selection is active.
- The selected policy and byte accounting are recorded in each run.
- The former `MAULER.md` content is archived and the live file is now a compatibility pointer.
- The former `AGENTS.md` is also archived, the live core is 10.5 KiB, and the context directory is
  populated with routed domain documents.
- A strictly validated manifest routes at most three bounded, heading-aware project excerpts.
- The Context Inspector previews the exact packet budget/provenance without exposing prompt contents
  and supports an ephemeral one-task Core/Relevant/Expanded override.

## Target document layout

```text
AGENTS.md                         canonical compact entry point (8-12 KB)
MAULER.md                         compatibility pointer (<1 KB)
docs/context/
|-- README.md                     context map and authoring rules
|-- manifest.json                 topic/intent to document routes
|-- current-state.md              current working features only
|-- architecture.md               packages, APIs, and data flow
|-- non-negotiables.md            UI, provider, safety, and scope rules
|-- roadmap.md                    current priorities only
|-- verification.md               latest verification evidence only
|-- feature-catalog.md            complete implemented-feature inventory
`-- troubleshooting.md            build, WSL, provider, and context failures
docs/archive/
|-- agents-handoff-snapshot-2026-07-21.md
|-- mauler-legacy-snapshot-2026-07-21.md
`-- verification-history.md
```

`AGENTS.md` remains the canonical entry point. Domain documents are authoritative for their linked
topic; `MAULER.md` must not become a second independent source of truth.

## Packet classes

| Packet | Intended task | Project-document allowance |
| --- | --- | --- |
| `minimal_external_research` | Public CVEs, current news/docs, unrelated web research | Source pointers only; <=500 tokens |
| `relevant_workspace` | Coding, UI, provider, docs, testing, workspace inspection | 16 KiB compiled maximum in M1; lower after manifest routing |
| `expanded_workspace` | User explicitly requests broad architecture/audit context | Explicit opt-in, bounded and logged; never the default |

Code-owned safety, scope, tool policy, control-plane rules, and latest-user-wins behavior apply to
all packet classes.

## Milestones

### M0 - Baseline and incident containment

Status: complete.

- Capture prompt-budget telemetry in RunLedger.
- Preserve authoritative llama.cpp usage/timing information.
- Cap always-on project instructions at 16 KiB while keeping full files readable.
- Fix direct CVE/PoC routing so unrelated research does not crawl the master methodology skill.

### M1 - Intent-aware project instruction selection

Status: complete. The production gate passed on 2026-07-21.

- Classify each run as `minimal_external_research` or `relevant_workspace` before the first model
  request.
- Omit repository handoff contents from unrelated public research.
- Retain project instructions when a task mentions repository inspection, implementation, writing,
  saving, downloading, running, testing, UI/backend/frontend, Mauler, or HelixClaw work.
- Record policy, source bytes, compiled prompt bytes, and source paths in the run ledger.
- Explain the two independent limits in Settings > Context: source-read allowance and prompt packet.
- Add exact regression fixtures for the failed CVE request and mixed research+implementation tasks.

### M2 - Canonical document split

Status: complete. The production gate passed on 2026-07-21.

1. Snapshot the complete current `AGENTS.md` and `MAULER.md` under `docs/archive`. Both
   content-preserving snapshots are complete.
2. Extract feature inventory, architecture, current roadmap, verification history, troubleshooting,
   and non-negotiable rules without rewriting their meaning.
3. Reduce `AGENTS.md` to the exact always-required core and links.
4. Replace `MAULER.md` with a compatibility pointer to `AGENTS.md` and `docs/context/README.md`.
5. Add a duplicate/conflict test for provider defaults, tool names, context policy, and UI invariants.

No source paragraph is deleted until its archive copy and destination are verified. The live core is
10.5 KiB, the preserved AGENTS snapshot is 68 KiB, and repository tests guard the core/shim size,
archive presence, required rules, context-map destinations, manifest validity, and non-injection of
the archive.

### M3 - Manifest-driven retrieval

Status: complete. The production gate passed on 2026-07-21.

- Validate `docs/context/manifest.json` in Go; invalid manifests fall back to the compact core.
- Map intent/topic signals to at most one to three documents.
- Retrieve bounded heading-aware excerpts instead of fixed full-document prefixes.
- Preserve immutable evidence of selected document paths, hashes, excerpt ranges, and token estimates.
- Do not let untrusted file/web content change source selection, tool permissions, or scope.

Implementation notes:

- The JSON decoder rejects unknown fields and trailing values. Version/status, packet budgets,
  required packet classes, route IDs/signals/priorities, and document counts are validated.
- Documents must resolve to the workspace `AGENTS.md` or Markdown under `docs/context`; traversal and
  symlink escapes are rejected. Minimal external research cannot name project documents.
- One highest-priority matching route selects at most three sources. Large documents use
  task-scored Markdown sections with exact line ranges instead of fixed prefixes.
- Every packet records manifest/source SHA-256 values, reason/route, source and prompt bytes, token
  estimates, partial state, and excerpt ranges. The packet is built once and the exact same content is
  logged and sent.
- Invalid manifests use the compact `AGENTS.md` fallback; missing manifests in other projects retain
  bounded legacy discovery for compatibility. Five identical selection runs are asserted stable.

Initial routes:

- `frontend`, `ui`, `layout`, `splitter` -> UI invariants + architecture.
- `llamacpp`, `InferenceBridge`, `provider`, `OpenRouter` -> provider/runtime.
- `telegram`, `channelbus`, `voice` -> channel/audio runtime.
- `controlplane`, `verification`, `recovery`, `loop` -> reliability/control plane.
- `workspace`, `files`, `memory`, `sessions` -> workspace/context/storage.
- `bug bounty`, `CVE`, `pentest` -> scope/evidence rules; repository docs only when workspace work is
  also requested.

### M4 - Context Inspector UI

Status: complete. The production gate passed on 2026-07-21.

Add a UI-only Context Inspector showing:

- Packet policy and working-context limit.
- Code-owned system tokens, project-document tokens, tool-schema tokens, memory/progress tokens, and
  total estimated or exact preflight tokens.
- Every included source with reason, hash, excerpt range, and open-source action.
- Excluded large sources and the reason they were excluded.
- Per-task `Core`, `Relevant`, and explicit `Expanded` choices.
- Pin/unpin for the next task and rebuild synopsis actions.

Secrets and raw sensitive tool output must never be displayed in this view.

Implementation notes:

- `PreviewContext` compiles a non-mutating preview using the active workspace, agent, profile,
  toolset, memory/session/ledger counts, conversation history, task text, and the same manifest
  selector used by a run. It returns accounting and provenance metadata, never raw system prompts,
  memory contents, profile text, tool results, credentials, or secret material.
- The inspector exposes working context, provider output reserve, preflight total, remaining budget,
  category estimates, route/manifest status, exact source hashes and line ranges, and code-owned
  exclusion reasons. Trusted files can be opened directly in the workbench.
- `Core`, `Relevant`, and `Expanded` are explicit packet requests. `Auto` restores code-owned task
  selection. Pinning is in-memory, applies to one accepted desktop task only, is recorded on the
  `TaskRun`, and is not consumed by Telegram or another remote lane.
- The primary Mauler system prompt is rebuilt for every accepted task so a changed task or one-task
  packet choice cannot leave stale M3 context in a continuing chat. Other system/control messages
  remain intact and are not duplicated.
- Expanded remains bounded and uses the manifest's three-document maximum; it is not a persistent
  default and cannot change tools, authorization, scope, or evidence policy.

### M5 - Quality and repeated-run gates

Status: in progress. The deterministic preflight and reusable live pass^k runner are implemented;
the selected local profile's full live pass^5 result still needs to be run and recorded before M5 is
closed as a model-reliability milestone.

Run each fixture five times for local profiles and report pass^5:

- Simple public CVE lookup.
- Go backend bug fix.
- React/UI splitter modification.
- Telegram remote task.
- Bug Bounty Hunter assessment.
- OpenRouter one-task cloud boost.
- WSL/terminal operational task.

Each fixture verifies required constraints are present, irrelevant history is absent, correct tools
are advertised, project-document budget is respected, and the final environment/result is correct.

Implemented 2026-07-21:

- `RunContextQualityEval` embeds seven exact task classes with three equivalent paraphrases each. A
  default pass^5 is 105 deterministic packet builds. It checks packet policy/class/route, agent mode,
  required and forbidden tools, the 24-tool ceiling, selected/forbidden sources, packet budgets,
  stable packet/tool-schema hashes, prompt markers, and hostile file/web/browser result handling.
- The deterministic lane uses canonical code defaults plus the selected profile and live
  workspace/context configuration. Intentional operator choices such as Manual mode, Unrestricted,
  or disabled web tools are not misreported as default-product regressions; the report names this
  evaluation envelope explicitly.
- The hostile-content fixtures assert that prompt-injection text remains untrusted data, cannot
  become instruction, preserves the safe observation, and has obvious secrets redacted.
- Tool-schema hashes are canonicalized by tool name so harmless registry declaration order cannot
  create false instability. Repair/update task wording now selects workspace context and code tools;
  the shell router uses whole-word `ping`, so words such as `keeping` cannot select a network path.
- `RunAgentEvalRepeated` executes the production model/tool loop one to ten times and reports
  `pass^k`, fixture-level all-runs pass, unsupported-completion, duplicate-action, tool-error,
  recovery-success, average tool count, average latency, policy violations, and human interventions.
- Benchmark > Advanced exposes two deliberately separate actions: `Context Quality pass^5` is a
  fast zero-model preflight; `Agent Eval x5` is five full live model-suite rounds and is labelled as
  such. A single-run Agent Eval remains available for diagnosis.
- Context Inspector now displays the exact advertised tool names plus stable packet and tool-schema
  identities without exposing raw prompts or sensitive content.

The deterministic repository gate currently passes 7 fixtures x 3 paraphrases x 5 repeats =
105/105 attempts. This is not a substitute for the live selected-profile Agent Eval x5 and must not
be reported as if it were.

Phase A production verification passed on 2026-07-21: focused and full Go tests, vet, the app/tools
race gate, frontend type/production build, production Wails build, and a native rebuilt-app run all
passed. The native Benchmark result was 7/7 fixtures and 105/105 attempts with hostile guard pass
and zero model calls for the selected `gemma4-26b-a4b-qat` profile/workspace context.

## Budgets

- Code-owned base prompt: target <=5,000 tokens.
- Canonical `AGENTS.md` core: target 2,500-4,000 tokens.
- Task-selected project excerpts: 2,000-4,000 tokens.
- Tool schemas: target <=1,500 tokens.
- Unrelated public research: <=500 project-document tokens.
- Initial local request: target <=10,000-12,000 tokens where the task allows it.

These are working budgets, not file-read limits.

## Acceptance gates

- Never inject full `AGENTS.md` and `MAULER.md` together.
- Every non-negotiable rule has one canonical source and a regression assertion.
- Public research does not receive repository development history.
- Coding/provider/UI tasks receive their relevant architecture and invariants.
- Large files remain fully readable through UI-driven targeted/chunked reads.
- No model-generated synopsis is authoritative evidence.
- Context selection is visible in Logs/Brain and the Context Inspector.
- Five-run reliability shows no regression against the pre-split fixtures.
- Full Go tests, vet, required race gates, frontend build, and production Wails build pass each slice.

## Rollback

Each milestone is independently reversible. M2 keeps content-preserving archive snapshots. Manifest or
selection failures fall back to the current bounded `relevant_workspace` packet, never to an empty
or unbounded prompt. Expanded context requires an explicit per-task choice and cannot become the
persistent default accidentally.
