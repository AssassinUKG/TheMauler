# Engagement Pack Library

Date: 2026-07-14

Status: first native management slice landed.

The Pack Library is the source of workflow and checklist definitions offered when a new Engagement
Grid is created. It is implemented inside TheMauler in Go and React/TypeScript. It does not run a
Python service, Docker sidecar, or a second project database.

Open it from **Bench > Pack Library**.

## What a pack contains

A portable schema-v1 pack bundle contains:

- one ordered workflow definition;
- one checklist containing the checks selected by that workflow;
- stable pack, workflow, checklist, and version ids;
- pinned source, trust, license, and content metadata; and
- an optional `based_on` record with the source workflow/checklist versions and SHA-256 digests.

The current bundle deliberately keeps a workflow and its checklist together. The later form editor
can present checks, workflows, target packs, and source adapters as separate operator concepts
without allowing a partially valid combination to enter the active runtime catalog.

## Stores and precedence

| Store | Location | Behaviour |
| --- | --- | --- |
| Built-in | Embedded in `TheMauler.exe` | Reviewed, read-only, available offline |
| Personal | `<Mauler config>/engagement-packs/<id>/<version>.pack.json` | Available in every workspace for this user |
| Project | `<workspace>/.mauler/engagement-packs/<id>/<version>.pack.json` | Travels with and applies only to that project |

The UI shows the exact resolved store paths. For the same workflow id, the newest non-archived
version is active. A project pack wins an equal-version tie over a personal pack, and a personal
pack wins over a built-in. Built-in workflow ids are reserved; an editable derivative gets a new
local id.

Only **new** Grids use the current active catalog. Every existing Grid keeps the exact workflow and
checklist snapshots and digests stored when it was created. Cloning, importing, archiving, or later
upgrading a library pack never silently rewrites a live engagement.

## Current operator flow

1. Select a built-in, personal, or project pack.
2. Review coverage, automation references, provenance, digests, and the editorial quality score.
3. Use **Clone to edit** to create a local-trust personal or project version. The clone keeps a
   pinned `based_on` record but does not continue claiming the upstream pack's trust level.
4. Use **Copy JSON** to export a portable bundle, or **Import JSON** to validate and install one.
5. Archive a custom version to remove it from new-Grid setup; restore it to make it eligible again.

Import is strict: unknown fields, trailing JSON values, unsafe ids, invalid workflow/checklist
references, duplicate ownership, and unapproved non-local provenance are rejected. Local packs run
the same structural and editorial review as built-ins.

If a manually edited file is malformed, the library quarantines that one file instead of failing the
whole catalog. **All** shows the validation error and path. The invalid pack cannot be cloned,
exported, or offered to a new Grid, but it can be archived. Fix the file and refresh to revalidate it.

## Quality and trust

Structural validation is mandatory. The quality score separately checks check mappings,
applicability, procedure, positive/fixed signals, false-positive controls, safety, evidence policy,
maturity, and automation metadata. A low editorial score is visible and does not masquerade as a
fully reviewed pack.

Trust levels are:

- `official`: an authoritative standards source;
- `curated`: reviewed and pinned by TheMauler;
- `community`: useful but requires additional operator review; and
- `local`: operator-owned content that does not claim upstream review.

## Deliberately not in this slice

The following are the next Pack Library layers, not hidden features of the current page:

1. form-based workflow/check editor plus an advanced JSON editor;
2. immutable version history, change diffs, validation fixtures, and clone-to-new-version;
3. explicit Grid migration preview and approval while retaining the prior snapshot for rollback;
4. technology/target applicability and a curated WordPress pack;
5. an allow-listed, pinned Nuclei metadata/update adapter; and
6. execution/result adapters that group templates into check families and treat matches only as
   candidate evidence until reproduced or independently verified.

Nuclei templates must not become thousands of Grid rows. Source updates remain inert until their
metadata, license, digest, diff, fixtures, and operator approval pass the native Go gates.

