# FIXES-RECENT execution evidence — 2026-10-25

This record covers the approved repository update on `codex/fixes-recent-go127`. It does not convert
pending secret transfer or live model-promotion work into completed evidence.

## Recovery evidence

- Pre-update archive: `.mauler/checkpoints/fixes-recent-preupdate-20261025-130500.zip`
- Archive entries: 573
- Archive SHA-256: `F76642B38032C5A81EBB10A451316512B0F1800D2AB777DF95884EA0CA251B4E`
- Pre-update settings SHA-256: `EF4406B74CF5D000B0B5ECB0F3F9B58090913C1D3CFFF2BE296300F3409E9E1B`
- Pre-update profiles SHA-256: `0D6FC5BEEB5482100FF0326867DCDC1816D23E4AA63BB933ADF4CF442EDF1331`
- Pre-update runtime-lock SHA-256: `2234596477446E974DEA5E2AB6031CFED6CE35716D568C1F864C20AC0B6C622A`
- SQLite migration used a copied database. The original was preserved at
  `.mauler/storage-migration-20261025/state-before.db` with SHA-256
  `EEB2C81E4B78C0266F78FC567F716EB433248EB53E5431CFF52841D0669A90BC`.

## Implemented candidate

- Go 1.27.3 and Wails CLI/module 2.17.0.
- `x/crypto` 0.58.0, `x/text` 0.43.0, `chromedp` 0.21.0, compatible `cdproto`
  0.157.8, SQLite 1.61.0, TOML 1.6.0, and the reviewed PDF extractor commit.
- DOMPurify 3.4.17, Monaco 0.57.0, and Vite 8.3.5. Monaco workers are local and editor/workbench
  routes are lazy. The strengthened assertion verifies a 1,018,284-byte initial JavaScript graph
  across ten assets whose largest application-shell asset is 373,378 bytes, rather than mistaking
  Vite's 47-byte generated entry wrapper for the complete initial path.
- The unrestricted profile points at the exact installed RVN GGUF. Its required-tool regression
  passed and aligned Qwen was restored as the daily runtime afterward.
- Doctor reports provider credential presence/source only and follows runtime configuration
  precedence for the active InferenceBridge port 8804.
- Repository indexing supports bounded local scanned-PDF OCR and metadata-only 7z inventory. The
  installed Poppler/Tesseract live smoke passed, alongside missing-tool, page/output-limit, and 7z
  logical-size tests.
- Browser screenshots support bounded viewport/full-page capture and workspace-owned SHA-256
  manifests. Native Chromium acceptance passed at 375x667, 1024x768, and 1440x900; stale artifacts
  cannot be promoted into Engagement evidence.
- Task runs now seal the exact built-in/custom agent definition ID and version in SQLite schema v19
  and every run-owned RunLedger event. Checkpoint resume retains and reattaches that provenance;
  configured custom agents no longer collapse to Auto, and legacy records remain readable.
- Task runs also persist a separate trusted routing prompt formed only from operator text plus the
  fact that attachments were selected. Hostile JavaScript comments, HTTP headers, and HTML bodies
  were each routed five times without affecting agent identity, tool exposure, mutation authority,
  target inference, completion gates, or checkpoint-resume authority.
- CI now includes repository-wide vet, reachable vulnerability scanning, the app/tools race gate,
  production npm audit, and a Windows Wails production build.

## Final verification

The ordered final rerun produced:

- `go test ./... -count=1` — pass on Go 1.27.3.
- `go vet ./...` — pass.
- `go test -race ./internal/app ./internal/tools -count=1` — pass after the agent-provenance and
  trusted attachment-routing follow-ons; app 293.688 s, tools 54.695 s.
- `govulncheck ./...` using the installed Go 1.27.3 binary — zero reachable vulnerabilities and zero
  vulnerabilities in imported packages. The sole module-level notice is `GO-2026-5932` for the
  unused `golang.org/x/crypto/openpgp` package; Mauler does not import it and the advisory has no
  fixed module version.
- `npm ci --prefix frontend` — pass.
- `npm audit --prefix frontend` — zero vulnerabilities across production and development
  dependencies. The six formerly outstanding development advisories were repaired with compatible
  Vite/PostCSS/NanoID/source-map/Zod/brace-expansion updates.
- Frontend generated-type drift — pass, 410 snake_case fields.
- Editor-tab regression — 5/5 pass.
- Frontend production build and lazy-Monaco assertion — pass.
- Focused app/tools/repository-index tests, live OCR smoke, three-size native Chromium evidence test,
  and evidence path-escape/stale checks — pass.
- `wails build` — pass with Wails 2.17.0.
- Hidden native startup with an isolated temporary `MAULER_CONFIG_DIR` — stayed alive for 15 seconds.
- Final `build/bin/TheMauler.exe` SHA-256:
  `10F32C37229E099A6309B7A7B79CE695EFA7711A1C4CAC34EE4DEC26FBFE4E3B`.

The local `go` launcher is older and auto-selected Go 1.26.10 for `govulncheck@latest`, which cannot
analyze a Go 1.27 module. The successful local scan used the installed Go 1.27.3 binary. CI sets
`GOTOOLCHAIN=local` after `setup-go` installs the version from `go.mod`, preventing that downgrade.

## Explicitly pending

- The InferenceBridge credential is still absent from Mauler's provider secret store. Copying the
  existing credential requires exact user confirmation; no secret value was read into output,
  committed, or placed in frontend state.
- Secret-redaction/logging, routine root use, unrestricted-tool defaults, and normal run budgets were
  not changed because they require a separate operational decision.
- Live promotion remains incomplete: the last recorded UI Gate 1 is 11/12. A fresh 12/12, five clean
  independent rounds, the native lifecycle/scoped-repair smokes, and supervised browser pass^k are
  still required before claiming unattended reliability.
