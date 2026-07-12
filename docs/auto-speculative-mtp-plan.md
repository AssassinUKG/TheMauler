# Auto-Speculative / MTP Plan (handoff)

**Status:** Phases 1, 2 & 3 IMPLEMENTED + rebuilt. Only Phase 4 polish + Phase 3b (acceptance
floor) remain.
**Created:** 2026-06-23
**Decision on detection:** GGUF/bridge metadata probe with name fallback (the robust option).

### Changelog
- **2026-06-23 (a)** - Verified the bridge contract. Found and fixed the real blocker: the
  InferenceBridge only emitted `--spec-type`/`--spec-draft-n-max` when a separate draft model
  path was set, so self-MTP (single MTP GGUF, no draft model) never activated. Confirmed
  `draft-mtp` is a valid llama.cpp `--spec-type` value (so Mauler's internal value was always
  correct - the earlier "draft-mtp vs mtp" worry is RESOLVED, no Mauler change needed). See section 0.1.
- **2026-06-23 (b)** - Implemented Phase 1 (auto-detect + auto-apply + badge). See section 3.5 for the
  exact files. Both apps rebuilt: InferenceBridge (`cargo`/`tauri build` -> release exe) and
  TheMauler (`build.ps1` -> `build/bin/TheMauler.exe`, which regenerated the Wails bindings).
- **2026-06-23 (c)** - Implemented Phase 3 (stability guard) + the carried-over bridge-build Doctor
  check. See section 4.1. TheMauler rebuilt (bridge unchanged this round). Acceptance-rate floor deferred
  to Phase 3b (needs a bridge metric) - the truncation-signature auto-fallback is in.
- **2026-06-23 (d)** - Implemented Phase 2 (auto-tune n + calibration cache). See section 5.1. TheMauler
  rebuilt. Cached best-n now feeds every load via `autoApplySpec`; a "Tune" button on the MTP chip
  runs the sweep. NOTE: the live sweep (model reloads + tok/s) is written but UNVALIDATED on real
  hardware in this session - needs a live model + the fixed bridge to confirm reloads-per-n work.
**Goal:** When the user selects a model, TheMauler should *automatically* detect whether the
model supports Multi-Token Prediction (MTP) / speculative decoding, apply the best settings
without the user having to think, and stay stable (auto-fallback) when MTP misbehaves.

This document is written so another agent can implement it cold. Read it top to bottom, then
start at Phase 1.

---

## 0. Background - what MTP is and what llama.cpp needs

MTP is speculative decoding **baked into the model**: the model emits several candidate tokens
per forward pass via built-in prediction heads, then verifies them in one shot. No separate
draft model required (though llama.cpp also supports a separate `--model-draft`; we support both
via `SpecDraftModel`).

Ground-truth facts (verified 2026-06-23 against Unsloth docs + llama.cpp community writeups):

- **Stock llama.cpp flags:** `--spec-type mtp --spec-draft-n-max N`. Merged ~May 2026 (PR #22673).
  Requires a recent build (Unsloth references "b9180+").
- **`N` (draft tokens/step) is hardware-dependent.** Unsloth: "try 1-6, use whichever is fastest."
  Do **not** assume 2 is optimal.
- **Dense vs MoE:** dense models gain 1.4-2.2x (Qwen3.6-27B). MoE gains only ~1.15-1.25x
  (Qwen3.6-35B-A3B). The auto-tuner must expect a smaller win on MoE and not thrash.
- **Only real MTP GGUFs work.** A normal GGUF won't gain anything from these flags; an MTP GGUF
  has extra heads/tensors. Detection on filename alone is unreliable.
- **Recommended sampling (Qwen3.6):**
  - thinking-general: `temp 1.0, top_p 0.95, top_k 20, min_p 0`
  - thinking-coding: `temp 0.6, top_p 0.95, top_k 20, min_p 0`
  - non-thinking: `temp 0.7, top_p 0.8, top_k 20, presence_penalty 1.5`
- **Quant / VRAM (RTX 3090, 24 GB):** `UD-Q4_K_XL` (or `UD-Q2_K_XL` if tight). 27B MTP ~ 19-20 GB
  (+~1 GB for MTP heads). KV cache `q8_0/q8_0`, flash-attention on. Registry note already warns
  "avoid Q6_K at 32K" on the 3090.
- **Stability landmine:** speculative rejection at `</think>` spikes EOS probability -> early
  truncation / repetition. Documented in `docs/agent-loop-research-report.md:148`. The auto system
  MUST guard this or it trades speed for instability.

### 0.1 Bridge contract - VERIFIED & FIXED (2026-06-23)

The InferenceBridge is a separate Tauri/Rust app at `C:\Users\richa\Documents\InferenceBridge`.
Mauler talks to it via `POST {baseURL}/models/load` with `spec_type` / `spec_draft_n_max` /
`draft_model_path` (see `internal/llm/backends/openaicompat.go:142`). Findings:

1. **`draft-mtp` is correct - no Mauler change needed.** llama.cpp `--spec-type` accepts a
   comma-separated enum: `none, draft-simple, draft-eagle3, draft-mtp, ngram-simple,
   ngram-map-k, ngram-map-k4v, ngram-mod, ngram-cache`. So `draft-mtp` is valid and the bridge's
   verbatim passthrough (`--spec-type <value>`) is right. The earlier "should it be `mtp`?" worry
   is dead. (Both `mtp` and `draft-mtp` appear in the wild; llama.cpp registers the impl as
   `draft-mtp` internally.)

2. **THE REAL BUG (now fixed):** in `src-tauri/src/engine/process.rs`, the launch builder nested
   `--spec-type` and `--spec-draft-n-max` *inside* `if !config.draft_model_path.is_empty()`. Self-MTP
   needs **no** draft model (it drafts from the main model's own MTP heads), so with an MTP GGUF and
   no `-md`, the spec flags were silently dropped and MTP never engaged. **Fix:** restructured so
   `--spec-type`/`--spec-draft-n-max` emit whenever `spec_type` is set, independent of `-md`; the
   `-md` model and `--draft-*` tuning flags stay gated on `draft_model_path`. Added regression test
   `self_mtp_emits_spec_type_without_draft_model`.

   - Files changed in the bridge repo: `src-tauri/src/engine/process.rs` (launch builder + test).
   - **Build/commit are separate** - the bridge is its own git repo and must be rebuilt
     (`cargo build` / its normal Tauri build) and committed there. Mauler does NOT pull it in.
   - Compile verified here (`cargo test --lib engine::process` -> `Finished` in ~30s). The test
     *executable* can't launch in a headless shell (`STATUS_ENTRYPOINT_NOT_FOUND` - missing
     WebView2/native DLL exports at load time, unrelated to the change); run the test in the normal
     bridge dev environment to see it green.

3. **Escape hatch exists:** the bridge appends `extra_args` verbatim, so `--spec-type draft-mtp`
   could already be forced via `extra_args` - but the proper fix above is what makes Mauler's
   structured `spec_type` field work for self-MTP.

4. **Still worth a Mauler Doctor check:** confirm at runtime that the connected bridge build
   includes this fix (e.g. probe a load preview / version), so an old bridge doesn't silently
   no-op MTP. Add when wiring Phase 1.

---

## 1. Current state - what already exists (DO NOT rebuild)

| Capability | Location | Notes |
|---|---|---|
| Capability record `Supports.MTP`, sampling `Defaults`, `RecommendedCtx`, KV types | `internal/runtimeprofile/registry.go:43` | Built-in for qwen3.6-27b (MTP=true), gemma4 (MTP=false) |
| Name-based MTP detector | `runtimeprofile.LooksMTPModel` `internal/runtimeprofile/registry.go:201` | Only checks if "mtp" in name+id - to be supplemented, not removed |
| Best-match model->profile | `runtimeprofile.Match` `internal/runtimeprofile/registry.go:118` | |
| Profile fields `SpecType` / `SpecDraftNMax` / `SpecDraftModel` | `internal/settings/model.go:37` | TOML+JSON tags present |
| Recommendation engine (sets draft-mtp, n=2, correct sampling) | `recommendProfileSettings` `internal/app/benchmark.go:531` | Already emits the right sampling per mode; only runs during benchmark today |
| Benchmark harness returning `RecommendedProfile` + tok/s | `App.BenchmarkProfile` `internal/app/benchmark.go:76`; `BenchmarkProfileWithCases:80` | Reusable for the n-sweep in Phase 2 |
| Benchmark run persistence (200 cap) | `saveBenchmarkRun` / `loadBenchmarkRuns` `internal/app/benchmark.go:511/492` | Pattern to copy for calibration cache |
| Load path shipping spec params to bridge | `loadLlamaCppModel` `internal/llm/backends/openaicompat.go:142` | Sends `spec_type`, `spec_draft_n_max`, `draft_model_path`; uses `echo_load_config: true` |
| llamacpp client wiring | `NewLlamacpp` `internal/llm/backends/llamacpp.go:13` | Copies profile spec fields onto the client |
| Client spec fields | `internal/llm/client.go:111` | |
| Doctor MTP checks (status + compatibility) | `internal/app/doctor.go:338` and `:834` | Good diagnostics; reuse for the UI badge tooltip |
| Runtime-lock persistence of spec settings | `internal/app/runtime_lock.go:32` | Persists across restarts |
| Existing rough notes | `PLAN.md:930-960` | Mentions `--spec-type mtp --spec-draft-n-max 3` |

**Conclusion:** values, load wiring, persistence, and diagnostics are done. Missing pieces are:
(a) it only happens on a manual benchmark/apply, (b) detection is name-only, (c) `N` is hardcoded
to 2, (d) no stability auto-fallback, (e) no glanceable UI.

---

## 2. Target architecture

New file `internal/app/autospec.go` owning the decision logic, plus a small probe in
`internal/runtimeprofile`. The agent loop / profile switch calls into it; nothing else changes
shape.

```
SwitchProfile / model load
        |
        v
ResolveSpecPlan(profile, runtimeProfile, probe) --> SpecPlan{Enabled, NMax, Reason, Source}
        |                         ^
        |                         +-- ProbeModelMTP(bridge, modelID)  (Phase 1)
        |                         +-- calibration cache lookup        (Phase 2)
        v
apply to settings.Profile (SpecType/SpecDraftNMax) + persist runtime-lock
        |
        v
loadLlamaCppModel sends to bridge   (already exists)
        |
        v
runtime watchdog (acceptance rate, </think> signature) --> auto-fallback (Phase 3)
        |
        v
UI badge: "MTP  -  auto  -  n=3  -  1.8x"  (Phase 4)
```

### Core types (new, in `internal/app/autospec.go`)
```go
type SpecSource string
const (
    SpecSourceProbe    SpecSource = "probe"     // GGUF/bridge metadata confirmed heads
    SpecSourceName     SpecSource = "name"      // fell back to LooksMTPModel
    SpecSourceRegistry SpecSource = "registry"  // Supports.MTP only
    SpecSourceManual   SpecSource = "manual"    // user override; never auto-touch
    SpecSourceDisabled SpecSource = "disabled"
)

type SpecPlan struct {
    Enabled  bool       `json:"enabled"`
    NMax     int        `json:"n_max"`
    Source   SpecSource `json:"source"`
    Reason   string     `json:"reason"`     // human-readable, shown in badge tooltip + Doctor
    Speedup  float64    `json:"speedup"`    // from calibration cache; 0 if unknown
    Locked   bool       `json:"locked"`     // user forced on/off -> don't auto-change
}
```

---

## 3. Phase 1 - Auto-detect + auto-apply on model load (ship first)

**Outcome:** selecting a Qwen3.6-MTP model turns MTP on with correct settings, no dialog.

### 3.1 Probe (robust detection - the approved option)
Add to `internal/runtimeprofile` (new file `probe.go`):

```go
// ModelMTPInfo is what we learn about a concrete artifact (not just its family).
type ModelMTPInfo struct {
    HasMTPHeads bool
    Confident   bool   // true if we actually inspected metadata; false = guess
    Detail      string
}
```

Two probe sources, in priority order:
1. **Bridge model-info endpoint.** The bridge already accepts `echo_load_config: true` on load.
   Prefer a *pre-load* read: try `GET {baseURL}/models/info?model={id}` (or whatever the bridge
   exposes - check the InferenceBridge source / `internal/app/doctor.go` health probes for the
   real endpoint). Look for spec/draft head config in the response. If the only signal is
   post-load `echo_load_config`, capture it after the first load and cache it (see 3.3) so the
   *next* switch is informed.
2. **GGUF header inspection.** If a local GGUF path is known, read the GGUF metadata key-value
   header (the GGUF format is documented; keys live near the file start). Presence of MTP/draft
   head tensors or `*.mtp.*` / `*.nextn.*`-style keys => `HasMTPHeads=true, Confident=true`.
   Keep this in a small self-contained reader; do not pull a heavy dependency.
3. **Fallback:** if neither works, use `LooksMTPModel` (name) => `Confident=false`.

`ProbeModelMTP(...) ModelMTPInfo` returns the best available signal. Cache results by
`(modelID, fileMtime/size)` to avoid re-reading GGUF headers on every switch.

### 3.2 Decision: `ResolveSpecPlan`
```go
func ResolveSpecPlan(p settings.Profile, rp runtimeprofile.RuntimeProfile,
                     info runtimeprofile.ModelMTPInfo, cal *SpecCalibration) SpecPlan
```
Rules:
1. If the user has explicitly locked spec on/off (Phase 4 toggle), return `Source=manual, Locked`.
2. If `!rp.Supports.MTP` -> `Enabled=false, Source=registry, Reason="family has no MTP variant"`.
3. If probe says `HasMTPHeads`:
   - `Enabled=true`, `Source=probe`.
   - `NMax` = calibration winner if present, else 2 (conservative default).
   - `Reason` e.g. "MTP heads confirmed in artifact; n=2 default (calibrate to tune)".
4. If probe not confident but name looks MTP **and** family supports MTP -> `Enabled=true,
   Source=name`, `Reason` notes it's a name-based guess (so Doctor can nudge a calibration).
5. Else -> disabled.

Apply the plan by reusing `recommendProfileSettings` for sampling/ctx (already correct), then set
`SpecType`/`SpecDraftNMax` from the plan, and persist via the existing runtime-lock writer.

### 3.3 Wire it into model load / profile switch
- Find `SwitchProfile` (App method; see `frontend/src/wailsjs/go.ts` bindings + `internal/app/app.go`).
  After resolving the active profile, call `ResolveSpecPlan` and persist the adjusted profile +
  runtime lock **before** the client loads the model, so `loadLlamaCppModel` ships the right body.
- Expose `func (a *App) GetSpecPlan() SpecPlan` for the frontend badge.
- Emit a run/event so it shows on the Ops timeline (reuse the existing event emitter used for
  `runtime_lock` / `model` events visible in the Ops Action Timeline).

### 3.4 Tests
- Extend `internal/app/benchmark_test.go` style: table tests for `ResolveSpecPlan` covering
  {probe-confident MTP, probe-confident non-MTP, name-only guess, registry no-MTP, user-locked}.
- A GGUF-probe unit test with a tiny fixture header (or a mocked reader).
- Confirm `recommendProfileSettings` still produces the documented sampling values.

### 3.5 What shipped (2026-06-23) - file map

**InferenceBridge repo** (`C:\Users\richa\Documents\InferenceBridge`):
- `src-tauri/src/engine/process.rs` - ungated `--spec-type`/`--spec-draft-n-max` from the draft-model
  block (self-MTP fix, section 0.1); added a focused `target: "speculative"` launch log; added regression
  test `self_mtp_emits_spec_type_without_draft_model`. **Rebuilt** to `target/release/inference-bridge.exe`.

**TheMauler repo:**
- `internal/runtimeprofile/probe.go` (+ `probe_test.go`) - `ModelMTPInfo` + `ProbeGGUFForMTP(path)`:
  opens the local GGUF, reads up to 8 MiB of header, requires the `GGUF` magic, substring-scans for
  `nextn` / `multi_token` / `mtp`. Non-GGUF / unreadable => `Confident=false` (caller falls back).
- `internal/app/autospec.go` (+ `autospec_test.go`) - `SpecPlan`, `SpecSource`, the pure
  `ResolveSpecPlan(...)` decision, and App methods `autoApplySpec`, `GetSpecPlan`, `SetSpecMode`,
  plus `probeModelMTP` / `bridgeModelPath` (GET `{baseURL}/models/{id}` -> local `path` -> probe).
- `internal/app/app.go` - `App` gained `specMu/specPlan/specOverrides`; `SwitchProfile` and
  `UseProfile` now call `autoApplySpec(name, active)` off the lock after activating a profile.
  Decision is persisted onto the stored profile's `SpecType`/`SpecDraftNMax` (so `buildChatRequest`
  and the bridge load body pick it up) and the model is marked for reload.
- Frontend: `src/wailsjs/go.ts` (+ generated `wailsjs/go/models.ts`) - `SpecPlan` type + `GetSpecPlan`
  / `SetSpecMode` bindings. `components/AgentPanel.tsx` + `.css` - clickable **MTP chip** in the
  Status block (green=verified on, amber=name-guess on, dim=off; click cycles auto->on->off; tooltip =
  reason). `components/LiveOpsPage.tsx` - read-only **MTP row** in Run Context. Both subscribe to the
  `mauler:spec_plan` event.

**Logging (the "all logged" requirement):**
- Bridge: full `args` vector + a dedicated `target:"speculative"` line at launch (greppable).
- Mauler: `autoApplySpec` emits `mauler:spec_plan` (UI + any event listeners); the plan carries a
  human `Reason`. Doctor's existing MTP checks (`doctor.go:338`, `:834`) still apply.

### 3.6 Known limitations / follow-ups from Phase 1
- **Bridge-build Doctor check (section 0.1 item 4) NOT yet added** - if the user runs an *old* bridge
  without the self-MTP fix, Mauler will set `spec_type=draft-mtp` and the bridge will silently no-op
  it (no draft model => flags dropped). Add a Doctor check that confirms the connected bridge honors
  self-MTP (e.g. inspect a load preview, or a bridge version/capability field).
- Probe runs on profile switch only. If the bridge can't answer `/models/{id}` (model not scanned /
  not loaded yet), it falls back to name+registry. Re-probing at load time would tighten this.
- `n_max` is still the static default (2) - Phase 2 calibration replaces it.

---

## 4. Phase 3 - Stability guard + auto-fallback (do before Phase 2)

Auto-tuning is pointless if the feature isn't safe to leave on. Hook into the existing stability
code (`internal/app/agent_loop_stability.go`, see also `stream.go`).

1. **Acceptance-rate floor.** llama.cpp `--metrics` / the bridge expose draft acceptance. If
   acceptance < ~30% (MTP net-negative), step `NMax` down by 1; if still poor, disable for the run.
2. **`</think>` truncation signature.** Reuse/extend the detector described in
   `docs/agent-loop-research-report.md:148`. On a hit, disable spec for the remainder of the run.
3. Everything **reversible and logged** to the Ops timeline with the reason, and surfaced in
   Doctor (extend the checks at `internal/app/doctor.go:338`).
4. Never silently override a user lock; instead log "MTP unstable - recommend disabling" so the
   badge can show a warning.

### 4.1 What shipped (2026-06-23) - Phase 3 file map

- `internal/app/autospec.go` - added `SpecSourceGuard`; `ResolveSpecPlan` gained a `guardReason`
  param (guard disables MTP, but an explicit user `on`/`off` still wins). New App methods:
  - `noteSpecTurn(profile, suspect)` - called once per MTP turn; a clean turn resets the streak,
    a *suspect* turn increments it. Suspect = truncated **and** thinking present **and** no usable
    visible answer (<24 chars) **and** no tool call - the speculative-rejection-at-`</think>` shape.
  - `tripSpecGuard(profile, reason)` at `specGuardThreshold` (2 suspect turns in a row): records
    the guard, re-resolves (MTP -> off), persists, emits `mauler:spec_plan`. Applies at the **next
    model load** - deliberately no disruptive mid-run reload.
  - `SetSpecMode` clears the guard + streak (an explicit user choice takes back control).
- `internal/app/app.go` - `App` gained `specGuard map[string]string` + `specTruncStreak int`; the
  agent turn loop calls `noteSpecTurn` right after the delta stream when `profile.SpecType != ""`.
- `internal/app/doctor.go` - self-MTP **bridge-build** info check: when MTP is on with no draft
  model, reminds the user the bridge must be the build that emits `--spec-type` without `-md`
  (the 2026-06-23 fix), and how to confirm it in the bridge log.
- Frontend - `AgentPanel.tsx` chip shows `off (auto-disabled)` in amber for `source==='guard'`;
  `LiveOpsPage.tsx` Run Context shows `Off (auto-disabled - instability)`. Tooltip carries the
  guard reason.
- Tests: `TestResolveSpecPlan_GuardDisablesButUserOverrideStillWins` (guard precedence).

**Deferred to Phase 3b:** the draft-acceptance-rate floor. It needs a real acceptance number from
the bridge/llama.cpp `--metrics`; the bridge does not yet surface it to Mauler. When it does, add
an acceptance check that steps `n` down (or trips the guard) below ~30%.

---

## 5. Phase 2 - Auto-tune `N` (one-time calibration, cached)

Reuse `BenchmarkProfileWithCases` to sweep `SpecDraftNMax in {1,2,3,4,5}` on a tiny fixed prompt
set, measure tok/s, pick the fastest, and cache it.

- New `internal/app/spec_calibration.go`:
  ```go
  type SpecCalibration struct {
      Key      string  `json:"key"`       // hash of (modelID, quant, gpu, ctx)
      BestN    int     `json:"best_n"`
      TokPerS  float64 `json:"tok_per_s"`
      Baseline float64 `json:"baseline_tok_per_s"` // n=0 / spec off
      Speedup  float64 `json:"speedup"`
      RanAt    string  `json:"ran_at"`
  }
  ```
  Persist to `{ConfigDir}/spec-calibration.json` (copy the `benchmark-runs.json` pattern at
  `internal/app/benchmark.go:484`).
- Key includes a GPU identifier (read from the bridge/Doctor; there's already GPU/VRAM probing in
  `doctor.go`). Re-run only when the key changes.
- Trigger: background, after first successful load of an MTP model with no cached calibration.
  Expose `func (a *App) CalibrateSpec(profileName string) SpecCalibration` for a manual re-run
  button, and a setting to disable auto-calibration.
- Feed `BestN` back into `ResolveSpecPlan` (the `cal` arg) and the badge `Speedup`.

Expected: 27B dense lands on n~3; 35B-A3B MoE smaller gain, possibly n=2. If `Speedup < ~1.05x`,
record it but consider leaving MTP off for the MoE (log the reason).

### 5.1 What shipped (2026-06-23) - Phase 2 file map

- `internal/app/spec_calibration.go` (+ `spec_calibration_test.go`):
  - `SpecCalibration` / `SpecCalibrationSample` types; cache at `{ConfigDir}/spec-calibration.json`
    keyed by `modelID|ctx=N` (the GGUF name encodes quant; single-GPU box => no GPU term yet).
  - `CalibrateSpec(profileName)` - guards on `agentRunning`, requires llama.cpp + MTP-capable, then
    measures a spec-off baseline and sweeps `spec_draft_n_max in {1..5}`, picks the fastest, caches
    it, writes `BestN` onto the profile, and re-resolves the plan. Emits `mauler:spec_calibration`
    progress/done events.
  - `measureSpecTokPerSec(profile)` - **clears `loadedModelKey` to force a reload** (modelLoadKey
    does NOT encode spec params), `ensureModelLoaded`, runs 2 benchmark cases, returns avg tok/s.
  - `pickBestSample` / `baselineTokPerSec` - pure, unit-tested.
  - `GetSpecCalibration()` - single return (empty `Key` = none) for an easy Wails binding.
- `internal/app/autospec.go` - `autoApplySpec` now overrides `plan.NMax` with the cached `BestN`
  when enabled, and appends ` -  tuned n=X (Yx vs off)` to the reason. So calibrate once -> every
  future load of that (model, ctx) uses the fastest n automatically.
- Frontend - `go.ts` + generated `models.ts`: `SpecCalibration[Sample]` types + `CalibrateSpec` /
  `GetSpecCalibration` bindings. `AgentPanel.tsx` + `.css`: a **"Tune"** button on the MTP row
  (shown when MTP is on, disabled while streaming/calibrating) -> runs `CalibrateSpec`, shows the
  result via panel status.

**UNVALIDATED on hardware (do this on a live box):** confirm the bridge actually relaunches
llama-server with the new `--spec-draft-n-max` on each sweep step after `loadedModelKey` is cleared
(if the bridge dedupes same-model loads, the sweep would measure one config repeatedly - in that
case add spec params to `modelLoadKey`, or have the bridge treat differing spec params as a reload).

---

## 6. Phase 4 - Glanceable UI (ties into the recent Ops/Agent polish)

- **MTP chip** in the Agent panel Status block (`frontend/src/components/AgentPanel.tsx`, the
  `tab === 'agent'` Status section) and in the Ops `Run Context` panel
  (`frontend/src/components/LiveOpsPage.tsx`). Format: `MTP  -  auto  -  n=3  -  1.8x`.
  - green = active, dim = N/A (non-MTP family), amber = on-but-unstable (from Phase 3).
  - Tooltip = `SpecPlan.Reason` + acceptance rate. Click = open Doctor MTP checks / force toggle.
- Backed by `GetSpecPlan()`; toggle calls a new `SetSpecOverride(enabled bool)` that sets
  `SpecPlan.Locked` so the auto layer leaves it alone.
- Add a Wails binding + regenerate `frontend/src/wailsjs/go.ts` / `frontend/wailsjs/go/*`
  (the project has a type-drift check: `frontend/scripts/check-type-drift.mjs`, run via
  `npm run build`). Keep the models in sync or the build fails.

---

## 7. Recommended settings the auto layer should write (Qwen3.6 on RTX 3090)

```
spec_type        = mtp        (internal "draft-mtp" - VERIFY bridge maps it; see section 0)
spec_draft_n_max = 2 -> calibrated 1..5 (expect 3 on 27B dense)
quant            = UD-Q4_K_XL   (UD-Q2_K_XL if VRAM-tight)
ctx_tokens       = 32768
kv cache         = q8_0 / q8_0, flash-attn on
sampling (think general) temp 1.0 / top_p 0.95 / top_k 20 / min_p 0
sampling (think coding)  temp 0.6 / top_p 0.95 / top_k 20 / min_p 0
sampling (no-think)      temp 0.7 / top_p 0.8  / top_k 20 / presence 1.5
VRAM budget      ~ 19-20 GB (27B) - fits 24 GB with headroom
```
These already match `recommendProfileSettings` output - Phase 1 makes them apply automatically,
Phase 2 tunes `n`.

---

## 8. Build / test commands

- Go tests: `go test ./...` (or scoped: `go test ./internal/app/... ./internal/runtimeprofile/...`)
- Go vet: `go vet ./...`
- Frontend typecheck/build: `cd frontend && npx tsc -b` then `npm run build`
  (note: `npm run build` runs `check-type-drift.mjs` first - keep Wails models in sync)
- Full exe: `powershell -ExecutionPolicy Bypass -File ./build.ps1` (add `-SkipTests` for frontend-only)

## 9. Recommended sequencing
1. ~~Verify bridge contract / self-MTP path~~ - **DONE 2026-06-23** (section 0.1). Bridge `process.rs`
   fixed + tested; needs its own rebuild/commit in the InferenceBridge repo.
2. ~~Phase 1 (auto-detect + auto-apply + badge)~~ - **DONE 2026-06-23** (section 3.5). Both apps rebuilt.
   Outstanding sub-item: the bridge-build Doctor check (section 3.6 / section 0.1 item 4).
3. ~~Phase 3 (stability guard) + bridge-build Doctor check~~ - **DONE 2026-06-23** (section 4.1). The
   `</think>` truncation auto-fallback is in; the acceptance-rate floor is deferred to Phase 3b
   (needs a bridge metric).
4. ~~Phase 2 (auto-tune n + calibration cache)~~ - **DONE 2026-06-23** (section 5.1). Cached best-n feeds
   every load; "Tune" button runs the sweep. Live-hardware validation of the per-n reload is the one
   open item (see section 5.1).
5. **<- NEXT:** Phase 4 polish (show speedup/acceptance in the badge) + Phase 3b acceptance-rate
   floor (needs a bridge metric). Both are small once a live model confirms the Phase 2 sweep.

## 10. Open questions for the implementer
- What exact endpoint does the InferenceBridge expose for model metadata pre-load? (Confirm in the
  bridge source; otherwise rely on post-load `echo_load_config` + cache.)
- Does the bridge surface draft acceptance rate / `--metrics`? If not, Phase 3's acceptance floor
  needs the truncation-signature path only.
- GPU identifier source for the calibration key - reuse whatever `doctor.go` already probes.
