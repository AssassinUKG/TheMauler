# Multi-target authorised scope plan

Date: 2026-08-24

Status: implemented and production-gated; native visual smoke remains operator-visible follow-up.

## Outcome

One Mauler project can represent a real client engagement containing multiple external IPs, internal
CIDRs, hostnames, and URL/path-constrained applications without encoding them as one malformed
comma-separated string. The operator remains the only authority for scope. The model may select a
narrower in-scope value, but it cannot add assets, broaden a URL path, or bypass an exclusion.

The legacy scalar `target` and `hostname` fields remain as derived compatibility values. The first
allowed structured row is the primary target used by older status, reachability, and tool surfaces.

## Data contract

Each project and active lab context persist ordered `scope_targets` rows containing:

- target value: exact IP, CIDR, hostname, host/port, or HTTP(S) URL/path prefix;
- code-derived type;
- environment label: Auto, External, or Internal;
- optional operator label and restrictions/notes; and
- allow or explicit exclude disposition.

On load, an old value such as `13.134.229.195, 3.9.20.132` is split into two allowed rows. Existing
target plus hostname projects migrate to ordered rows. Invalid historical strings stay visible for
repair but are omitted from locked scope.

## Policy rules

1. At least one valid allowed row is required to create an Engagement Grid.
2. Exclusions are represented separately in project settings and locked as deny rules; deny matching
   runs before allow matching regardless of list order.
3. CIDRs authorise only contained IPs. Exact port and URL path restrictions remain attached to their
   entry.
4. A relative endpoint inherits the first allowed concrete primary target. It cannot broaden that
   primary URL's path prefix or bypass its exclusions. A CIDR cannot supply a relative endpoint base.
5. Requested Grid scope must be equal to or narrower than operator scope. Same-host path broadening is
   rejected.
6. Scope remains immutable after Grid creation. Correct project scope, then recreate only the Grid
   when authorisation changes.

## UI delivery

Home now provides a first-class **Authorised scope** editor:

- paste comma-, semicolon-, or newline-separated lists;
- add/remove and reorder individual rows;
- mark Allow or Exclude;
- label External/Internal/Auto;
- attach a friendly label and restrictions/notes;
- see code-owned type validation before save; and
- see allowed/excluded counts and the primary target in project cards and resume state.

The former **+ New HTB box** action is now **+ New project** and starts with Pentesting/client-safe
defaults. HTB/CTF remains available explicitly through the Ops profile selector.

Settings and older Target controls edit the primary row only. Home owns the complete scope list.

## Verification and acceptance

- [x] legacy comma-list migration test;
- [x] structured settings TOML round trip;
- [x] IP, CIDR, hostname, host:port, URL, and invalid-value classification;
- [x] explicit exclusion overrides broad CIDR and URL allows;
- [x] relative endpoint binds to primary and respects path/exclusion constraints;
- [x] model-requested broader URL scope is rejected;
- [x] full structured list appears in guided setup and is locked into Grid creation;
- [x] focused settings, engagement, and app suites;
- [x] frontend type-drift and production bundle;
- [x] complete repository Go/vet/race/Wails production gate;
- [ ] native UI smoke: create a two-IP client, reopen it, preview the Grid, and verify persistence.

## Follow-on work after this slice

1. Add optional batch readiness for selected concrete targets. CIDRs remain validation-only rather
   than contacting every address automatically.
2. Add per-asset filters/grouping to Grid endpoints, evidence, findings, and exports so large client
   engagements remain easy to review.
3. Finish the existing Bug Bounty/Chat isolation edges: exact agent-definition ledgering, named
   checkpoint/resume, dirty-tab workspace switching, and hostile/paraphrase routing fixtures.
4. Make the deliberate reliability change needed for a fresh 12/12 Agent Eval Gate 1, then and only
   then run the selected local profile's expensive x5 unattended gate.
5. Continue the repository-intelligence/extractor plan, followed by incremental LSP work.
