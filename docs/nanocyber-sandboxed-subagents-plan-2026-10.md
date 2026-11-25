# TheMauler NanoCyber sandboxed-subagent plan

Date: 2026-10-29  
Status: **MN-1/MN-2 plus the MN-3 client and fail-closed routing foundation implemented; live execution remains gated**

## Decision

TheMauler already has bounded Explorer, Researcher, Reviewer, Test/Fix, and Summarizer subagents with
separate context, toolsets, timeouts, turn/tool-call budgets, engagement assignments, ledger events,
and output contracts. That system remains the subagent scheduler.

NanoCyber becomes the environment authority for subagents that execute generated code or PoCs. It
does not replace TheMauler's control plane, RunLedger, task contract, plan approval, evidence gate,
workspace authority, or final-message ownership.

## Landed foundation

`internal/nanocyber/lease.go` defines TheMauler's client-side view of NanoCyber lease schema v1. It
provides:

- the offline/dependency/scoped-target network-policy types;
- a versioned request with exact parent-run and subagent IDs;
- an `offline-code` request builder for execution-bearing subagents;
- explicit allowed tools, turn/tool/job/output budgets, required evidence, and TTL; and
- fail-closed validation for authority expansion, duplicate tools, missing scope, excessive budgets,
  and excessive expiry.

The package has no Docker or shell implementation. The strict loopback client now covers admission,
exact-lease cancel/complete, and parent-run cancellation with bounded, strict response validation.
The attempt-plan builder binds the request digest to the authoritative Mauler run generation,
conversation epoch, and exact subagent attempt. These transports are not yet used for live execution.

## Routing policy

| Subagent | Default execution policy |
|---|---|
| Explorer | Existing read-only local inspection; no lease |
| Researcher | Existing bounded public research; lease only for a typed reproduction |
| Reviewer | Existing read-only evidence review; clean-room lease only when explicitly required |
| Test/Fix | NanoCyber `offline-code` lease required before any generated-code execution |
| Summarizer | No lease and no execution tools |

Bug-bounty and engagement work that contacts a target uses `scoped-poc`, the exact active engagement
ID, immutable scope revision, and explicit endpoint set. Dependency acquisition is a separate
dependency-only stage. Unrestricted environment internet is not part of v1. Public research remains
through the existing bounded web path.

TheMauler's present shared/isolated terminal modes are operator workbench features, not a hostile-code
sandbox. A task contract that requires NanoCyber isolation must never silently route to `shell`,
`bash`, `terminal_send`, `run_script`, an AI Commands terminal, WSL, or PowerShell when NanoCyber is
unavailable.

## Control-plane binding

The lease is created only after the task type, accepted plan, workspace, agent definition, run ID,
generation, engagement assignment, and mutation scope are authoritative. The request captures the
exact subagent ID and the remaining parent budgets. It cannot increase any of them.

The returned lease ID/request digest is written to RunLedger and bound to the subagent result. A
result is usable only when:

- the parent run/generation is still current;
- the subagent attempt and task contract are unchanged;
- the engagement work claim and scope revision are still valid;
- required NanoCyber terminal receipts are present and hash-valid; and
- cleanup is complete or explicitly retained under an admitted TTL.

Late results remain ledger evidence but cannot mutate files, satisfy verification, or complete the
current run. Tool output stays untrusted and passes through the existing promptware/secret guards.

## Implementation slices

### MN-1 — contract (landed)

- Add typed v1 request construction and validation.
- Test a valid offline Test/Fix lease, offline network expansion rejection, and hard job-budget
  rejection.

### MN-2 — settings and Doctor (landed client-side)

- Add an optional loopback NanoCyber endpoint plus protected credential reference.
- Validate loopback URL, API version, health, and capability inventory in Doctor/Services.
- Keep secrets outside React state, prompts, logs, exports, and RunLedger.
- Advertise sandbox execution only while the compatible API is healthy.

TheMauler now has an opt-in **Settings > Sandbox** surface, protected environment/store token
precedence, strict loopback URL validation, proxy-free and redirect-free bounded probes, and shared
Doctor/Services health. The token is accepted through an uncontrolled password input and is never
returned to React; only presence flags cross Wails. Runtime compatibility requires `/healthz` plus
an authenticated `/v1/capabilities` inventory declaring product `nanocyber`, API `v1`, lease schema
1, `offline-code`, and the `subagent_leases`, `typed_jobs`, `receipt_evidence`,
`lease_cancellation`, and `cleanup_status` capabilities.

The companion NanoCyber tree now starts an opt-in authenticated loopback service with `/healthz`,
lease admission, a bounded scheduler, exact-lease cancel/complete, parent cancellation, cleanup,
and restart/expiry reconciliation. It still does not publish the authenticated capability inventory
or expose consumer-facing typed-job submission, receipt retrieval, and terminal lease-status
reconciliation. Its admission service also requires a NanoCyber-native `eng_...` engagement with a
locked scope; a Mauler Engagement Grid ID is not interchangeable authority. TheMauler therefore
reports the present service as **incompatible**, remains disabled by default, and advertises no
sandbox execution. This is an intentional cross-repository gate, not a host-shell fallback.

### MN-3 — subagent admission (foundation landed; live admission gated)

- Test/Fix now carries an explicit `offline-code` environment requirement; read-only subagents keep
  their existing behavior without unnecessary containers.
- The admission-plan builder seals exact run generation/epoch, subagent attempt, budgets, evidence,
  expiry, and a NanoCyber-native engagement authority into a stable request digest.
- When the operator enables NanoCyber, Test/Fix fails closed before model execution while readiness
  is incomplete and records `host_fallback=false`; it cannot silently execute with host tools.
- The disabled-by-default staged configuration preserves the existing local Test/Fix route until the
  production gate is deliberately completed and enabled.
- Live admission, lease/digest ledgering, and result acceptance remain blocked until engagement
  mapping plus typed jobs, receipts, and terminal status/cleanup reconciliation are available.

### MN-4 — typed coding tools

- Replace execution-bearing Test/Fix tools with NanoCyber typed file/job/artifact operations while
  the lease is active.
- Transfer inputs by logical ID and digest, never arbitrary host paths.
- Return bounded output and receipt references; large output remains in the offload/evidence store.
- Require verification evidence before protected-file handoff or task completion.

### MN-5 — lifecycle and UI

- Show environment profile, network state, lease owner, TTL, jobs, evidence, and cleanup status in
  the existing run/subagent surfaces.
- Stop/cancel cascades to the exact lease. Unconfirmed cleanup blocks a success state.
- Add retry only for classified transient transport/capacity failures; never retry policy/scope
  rejection or silently widen a request.

### MN-6 — qualification

- Repeatedly test cancel/replace/checkpoint/workspace switch/app restart/NanoCyber restart at every
  lifecycle phase.
- Prove two simultaneous subagents cannot share environment, network, artifact, or cancellation
  authority.
- Prove NanoCyber absence cannot fall back to local execution.
- Run a clean-room PoC reproduction and require fresh receipt-backed evidence.

## Production gate

The feature stays disabled until NanoCyber exposes the authenticated lease API and TheMauler passes
the lifecycle, scope, evidence, and no-host-fallback tests. A successful model answer or terminal
exit code alone is not qualification evidence.
