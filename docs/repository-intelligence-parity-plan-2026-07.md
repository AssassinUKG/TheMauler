# Mauler Native Files & Knowledge Intelligence Plan

Status: approved direction; implementation not started  
Date: 2026-07-15  
Scope: TheMauler only; native Go backend and existing React/TypeScript desktop UI  
Related: `docs/brain-memory-ledger-tracker.md`,
`docs/mauler-agent-control-plane-improvement-plan-2026-07.md`,
`docs/agentic-reliability-issues-2026-07.md`

## Executive decision

Mauler should gain a first-class Files & Knowledge corpus index and an optional split-audit flow in Chat.
It should reuse SQLite, RunLedger, task contracts, bounded `task` workers, promptware guards, and the
existing InferenceBridge provider. Do not add Python, LangGraph, a vector-database service, or a
second project store.

The source model is deliberately general: users may add folders or individual files containing
code, documents, notes, logs, databases, archives, images or mixed evidence. A Git repository is
only one possible source.

"Read all" means every selected, supported file is streamed, hashed, and chunked into the index. It
does not mean copying a whole repository or a multi-megabyte file into one model prompt. Retrieval
must remain bounded and cite the exact indexed chunks used for a conclusion.

## Why Mauler currently feels behind HelixClaw here

This is an architecture and sequencing gap, not primarily a model-quality or general stability gap.

- HelixClaw has a dedicated memory crate that scans configured paths, chunks files, stores FTS and
  optional vectors, and continuously re-indexes changes.
- Mauler's current durable Memory stores curated facts/preferences, while `state.db` FTS indexes
  saved conversations. Neither is a workspace source-code corpus index.
- Mauler's recent engineering went into desktop execution, shared-terminal hygiene, deterministic
  verification, RunLedger, Engagement evidence/scope, rollback, and the native control plane.
  Those make changes and authorised security work safer, but they do not answer broad repository
  questions until the model manually reads files.

Mauler is already stronger at evidence ownership, operator visibility, hostile-content labelling,
mutation verification, and bounded worker identity. The correct upgrade is to add corpus retrieval
under those controls, then use the existing strengths during audits.

## Product behaviour

### Single-chat Files & Knowledge scan

Chat gains an `Add files / Scan knowledge` action with:

- sources: current workspace, selected folders, or selected individual files;
- mode: `Index only`, `Quick review`, or `Deep review`;
- coverage: all built-in formats or a user-edited allowlist;
- size policy: `0` means unlimited per file, otherwise an explicit byte guard;
- split review: off, automatic, or a user-selected shard count; and
- access: read-only by default, with later repair work beginning as a separate contract revision.

The transcript receives one compact scan card, not thousands of file/tool messages. The card shows
manifest progress, bytes read, files indexed, unsupported/binary files, explicit size skips,
decode/extractor errors, shard progress, and cited findings. Nothing is silently omitted.

### Split review in Chat

Automatic splitting starts only after manifest creation. The controller partitions stable work by
top-level module, language, or size-balanced directory groups. Each child receives:

- a sealed read-only task contract;
- one immutable manifest/shard digest;
- exact allowed paths and index chunk IDs;
- a finding schema covering claim, severity, confidence, file/line evidence, and follow-up; and
- bounded tool, token, time, and retry budgets.

Use the existing bounded `task` runtime and distinct child claimant identities. Default concurrency
is conservative and configurable; the RTX 3090/local provider must not be flooded with simultaneous
generation requests. A controller-owned merge pass deduplicates findings, resolves contradictory
claims through an independent check, and emits one parent evidence bundle. Child prose alone is not
proof.

## Native architecture

### `internal/repoindex`

Add a Go service with these code-owned types:

```go
type IndexPolicy struct {
    Roots            []string
    Include          []string
    Exclude          []string
    MaxFileBytes     int64 // 0 = unlimited
    MaxTotalBytes    int64 // 0 = unlimited; optional operator budget
    FollowSymlinks   bool  // false by default
    ExtractorPolicy  ExtractorPolicy
}

type ManifestEntry struct {
    Path, Language, Encoding, Extractor string
    Size                               int64
    ModTime                            time.Time
    SHA256                             string
    Status, Detail                     string
    ChunkCount                         int
}

type EvidenceRef struct {
    IndexID, ChunkID, FileSHA256 string
    Path                         string
    StartLine, EndLine           int
}
```

The scanner must use streaming reads and rolling SHA-256. It must not call `os.ReadFile` for an
arbitrarily large repository file. Text is decoded and emitted to bounded chunks incrementally;
embedding requests are separately batched. Cancellation, workspace change, and re-index restart
must leave the previous complete index readable until the replacement transaction commits.

### Storage

Extend `internal/store` rather than opening an unrelated database. Add schema-versioned tables for:

- index roots and policy digest;
- scan generations and status;
- file manifest entries;
- chunks with text, line range, hash, trust label, and extractor version;
- FTS5 content/search;
- optional embedding cache/vector metadata; and
- shard assignments, finding references, and merge decisions.

An index generation is immutable after completion. Changed files create a new generation or replace
only affected file/chunk rows inside a transaction. RunLedger records start/progress/skip/error/
finish events and content-addressed evidence IDs without copying full source bodies into JSONL.

### Retrieval and model-facing surface

Keep the model API compact. Extend the existing `memory` tool with actions such as:

- `index_workspace`
- `index_status`
- `search_workspace`
- `read_index_chunk`
- `cancel_index`

Go derives policy, trust, roots, and budgets. Search returns short ranked excerpts plus evidence
references. FTS5 is the required baseline. Optional semantic retrieval uses the current
InferenceBridge `/v1/embeddings` route only after Doctor confirms it is available; lack of an
embedding model must never disable keyword search.

### Context compiler integration

The run context packet contains only:

- index generation and policy digest;
- current shard/plan step;
- selected trusted metadata;
- bounded retrieved chunks labelled `UNTRUSTED REPOSITORY CONTENT`;
- evidence references; and
- remaining budgets/blocking condition.

Repository instructions, README text, comments, issue templates, notebooks, and logs cannot expand
scope, authorize tools, disclose secrets, or override user/system instructions. Existing promptware
and secret guards remain mandatory at retrieval and source-to-sink boundaries.

## Format coverage

M1 supports UTF-8/UTF-16 text, source, scripts, notebooks, configuration, manifests, logs, CSV/TSV,
and common extensionless project files. Detection combines extension, filename, BOM/encoding, and a
binary sniff; `*` may opt into trying unknown files as text.

M2 adds format-aware, read-only extractors:

- text PDFs through the existing PDF foundation, then OCR for scanned pages;
- DOCX, PPTX, XLSX/ODS metadata and text/cell extraction;
- SQLite schema/table sampling through the existing read-only SQLite path;
- ZIP/TAR/7z inventory and selected member extraction without executing content; and
- optional PE/ELF metadata and printable strings for authorised security review.

Every extractor is versioned and declares supported MIME/extensions, risk, timeout, maximum
expanded bytes, and evidence mapping. Unsupported/encrypted/corrupt inputs appear in the manifest;
they are never quietly treated as covered.

## Milestones

### M0 - Coverage/reliability fixture gate

- Build representative small, large-file, mixed-format, hostile-content, symlink, unreadable,
  encoding, archive-bomb, and cancellation fixtures.
- Record the current manual-read baseline and repeated-run pass^k.
- Gate on zero silent omissions, zero out-of-root reads, stable hashes/chunk IDs, and identical final
  manifests across repeated runs.

### M1 - Streaming text/code index

- Implement `internal/repoindex`, schema, transactional generations, broad text/code detection,
  FTS5, incremental hash-based updates, cancellation, and sync stats.
- Default `MaxFileBytes` to `0`; generated/dependency directories remain explicit code-owned
  exclusions that the operator can review and override.
- Add unit/race tests before UI work.

### M2 - Compact tool and desktop oversight

- Add the compact `memory` actions, RunLedger events, Wails bindings, Memory/Brain index page, and
  the Chat scan card.
- Add settings for include/exclude patterns, file-size/total budgets, formats, watch mode, and
  extractor policy.
- Doctor reports index health, FTS availability, extractor readiness, and embedding-route status.

### M3 - Split review and evidence merge

- Add deterministic manifest sharding over existing bounded tasks.
- Show child progress inside one parent Chat card with pause/cancel/retry-shard controls.
- Merge/deduplicate findings and require chunk/file-hash evidence before calling a claim verified.
- Keep mutation disabled until the user explicitly begins a repair revision.

### M4 - Rich extractors and optional embeddings

- Land document/archive/binary metadata extractors behind fixtures and expansion/time limits.
- Add InferenceBridge embeddings, hybrid ranking, MMR, and embedding cache invalidation.
- Keyword-only mode remains fully supported and tested.

### M5 - Live hardening

- Run the same real repositories through HelixClaw and Mauler 5-10 times.
- Compare coverage, unsupported-completion rate, duplicate reads, finding precision/recall, latency,
  memory, tool count, child recovery, and operator interventions.
- Promote defaults only after no-silent-omission and no-scope-bypass gates remain green.

## Non-negotiable gates

- Zero files reported as covered unless a manifest entry and final chunk/extractor verdict exist.
- Zero silent size, decode, unsupported-format, extractor, or permission skips.
- Zero reads outside canonical approved roots; symlinks do not bypass scope.
- Large-file support cannot create one unbounded model or embedding request.
- Repository content is always untrusted operational data.
- Child tasks cannot mutate, widen scope, or mark their own unsupported findings verified.
- Completion references immutable manifest, file-hash, chunk, and verifier evidence IDs.
- Repeated-run coverage and reliability are reported separately from a one-off successful demo.

## Immediate implementation order

1. Add M0 fixtures and a manifest-only benchmark.
2. Implement the streaming manifest/chunker plus SQLite FTS5 generation in `internal/repoindex`.
3. Expose `index_workspace`, `index_status`, and `search_workspace` through the existing `memory`
   tool and RunLedger.
4. Add the single Chat scan card and operator-visible omission/error list.
5. Add deterministic read-only split review and evidence merge.
6. Add rich formats, then optional InferenceBridge embeddings.

Do not begin with prompt rewrites or parallel free-form agents. The first useful increment is a
deterministic, inspectable repository manifest and FTS index that can prove what it did and did not
read.
