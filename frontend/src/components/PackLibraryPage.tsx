import { useEffect, useMemo, useState } from 'react'
import {
  ClonePack,
  ExportPackJSON,
  ImportPackJSON,
  ListPackLibrary,
  SetPackArchived,
  type PackLibrarySnapshot,
  type PackSummary,
} from '../wailsjs/go'
import './PackLibraryPage.css'

type LibraryFilter = 'active' | 'all' | 'archived'
type WritableScope = 'personal' | 'project'

export function PackLibraryPage() {
  const [snapshot, setSnapshot] = useState<PackLibrarySnapshot | null>(null)
  const [selectedKey, setSelectedKey] = useState('')
  const [filter, setFilter] = useState<LibraryFilter>('active')
  const [loading, setLoading] = useState(false)
  const [status, setStatus] = useState('')
  const [cloneOpen, setCloneOpen] = useState(false)
  const [cloneScope, setCloneScope] = useState<WritableScope>('project')
  const [cloneID, setCloneID] = useState('')
  const [cloneName, setCloneName] = useState('')
  const [cloneVersion, setCloneVersion] = useState('0.1.0')
  const [importOpen, setImportOpen] = useState(false)
  const [importScope, setImportScope] = useState<WritableScope>('project')
  const [importJSON, setImportJSON] = useState('')

  const load = async (preferredKey = '') => {
    setLoading(true)
    try {
      const next = await ListPackLibrary()
      setSnapshot(next)
      setSelectedKey(current => {
        const wanted = preferredKey || current
        if (wanted && next.packs.some(item => item.key === wanted)) return wanted
        return next.packs.find(item => item.active)?.key || next.packs[0]?.key || ''
      })
    } catch (error) {
      setStatus(`Pack Library failed: ${error}`)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { void load() }, [])

  const visible = useMemo(() => {
    const packs = snapshot?.packs ?? []
    if (filter === 'active') return packs.filter(item => item.active)
    if (filter === 'archived') return packs.filter(item => item.archived)
    return packs
  }, [snapshot?.packs, filter])
  const selected = snapshot?.packs.find(item => item.key === selectedKey) ?? visible[0]

  const openClone = (pack: PackSummary) => {
    if (!pack.valid) return
    setSelectedKey(pack.key)
    setCloneID(uniqueCloneID(pack, snapshot?.packs ?? []))
    setCloneName(`${pack.name} Local`)
    setCloneVersion('0.1.0')
    setCloneScope(snapshot?.project_root ? 'project' : 'personal')
    setCloneOpen(true)
    setImportOpen(false)
    setStatus('')
  }

  const clone = async () => {
    if (!selected) return
    setLoading(true)
    try {
      const created = await ClonePack({
        source_key: selected.key,
        scope: cloneScope,
        id: cloneID.trim().toLowerCase(),
        name: cloneName.trim(),
        version: cloneVersion.trim(),
      })
      setStatus(`Created ${created.name} ${created.version}. It is now available in guided setup.`)
      setCloneOpen(false)
      setFilter('active')
      await load(created.key)
    } catch (error) {
      setStatus(`Clone failed: ${error}`)
      setLoading(false)
    }
  }

  const importPack = async () => {
    if (!importJSON.trim()) return
    setLoading(true)
    try {
      const created = await ImportPackJSON(importScope, importJSON)
      setStatus(`Imported ${created.name} ${created.version}.`)
      setImportJSON('')
      setImportOpen(false)
      setFilter('active')
      await load(created.key)
    } catch (error) {
      setStatus(`Import failed: ${error}`)
      setLoading(false)
    }
  }

  const exportPack = async () => {
    if (!selected) return
    try {
      const raw = await ExportPackJSON(selected.key)
      await navigator.clipboard.writeText(raw)
      setStatus(`Copied ${selected.id}@${selected.version} bundle JSON.`)
    } catch (error) {
      setStatus(`Export failed: ${error}`)
    }
  }

  const toggleArchived = async () => {
    if (!selected || selected.built_in) return
    setLoading(true)
    try {
      const changed = await SetPackArchived(selected.key, !selected.archived)
      setStatus(`${changed.archived ? 'Archived' : 'Restored'} ${changed.name} ${changed.version}. Existing Grids were not changed.`)
      await load(changed.key)
    } catch (error) {
      setStatus(`Archive update failed: ${error}`)
      setLoading(false)
    }
  }

  return (
    <div className="pack-library">
      <section className="pack-library-hero">
        <div>
          <span className="pack-eyebrow">ENGAGEMENT DEFINITIONS</span>
          <h2>Pack Library</h2>
          <p>Versioned workflows and vulnerability checks used by new Grids. Live engagements keep their pinned snapshots.</p>
        </div>
        <div className="pack-library-actions">
          <button onClick={() => void load()} disabled={loading}>{loading ? 'Loading…' : 'Refresh'}</button>
          <button onClick={() => { setImportOpen(value => !value); setCloneOpen(false) }}>Import JSON</button>
          <button className="primary" onClick={() => selected && openClone(selected)} disabled={!selected?.valid}>Clone to edit</button>
        </div>
      </section>

      <section className="pack-library-metrics">
        <PackMetric label="Built-in" value={snapshot?.built_in_count ?? 0} />
        <PackMetric label="Active" value={snapshot?.active_count ?? 0} />
        <PackMetric label="Archived" value={snapshot?.archived_count ?? 0} />
        <PackMetric label="Quarantined" value={snapshot?.invalid_count ?? 0} />
        <PackMetric label="Project store" value={snapshot?.project_root ? 'Ready' : 'No workspace'} />
      </section>

      {status && <div className={`pack-library-status ${/failed|unavailable/i.test(status) ? 'error' : ''}`}>{status}</div>}

      {(cloneOpen || importOpen) && (
        <section className="pack-library-editor">
          {cloneOpen && selected && <>
            <div className="pack-editor-head"><div><strong>Clone {selected.name}</strong><span>Create the first editable version without changing the source pack.</span></div><button onClick={() => setCloneOpen(false)}>Close</button></div>
            <div className="pack-editor-grid">
              <label><span>Pack ID</span><input value={cloneID} onChange={event => setCloneID(event.target.value)} placeholder="wordpress-local" /></label>
              <label><span>Name</span><input value={cloneName} onChange={event => setCloneName(event.target.value)} /></label>
              <label><span>Version</span><input value={cloneVersion} onChange={event => setCloneVersion(event.target.value)} /></label>
              <label><span>Store</span><select value={cloneScope} onChange={event => setCloneScope(event.target.value as WritableScope)}><option value="project" disabled={!snapshot?.project_root}>This project</option><option value="personal">Personal library</option></select></label>
            </div>
            <div className="pack-editor-footer"><span>Definitions are copied with local trust and retain a pinned based-on record.</span><button className="primary" onClick={() => void clone()} disabled={loading || cloneID.trim().length < 2 || !cloneName.trim()}>Create local pack</button></div>
          </>}
          {importOpen && <>
            <div className="pack-editor-head"><div><strong>Import Pack Library JSON</strong><span>Only structurally valid local or allow-listed pinned sources are accepted.</span></div><button onClick={() => setImportOpen(false)}>Close</button></div>
            <label className="pack-json-field"><span>Bundle JSON</span><textarea value={importJSON} onChange={event => setImportJSON(event.target.value)} placeholder="Paste a Pack Library export here…" /></label>
            <div className="pack-editor-footer"><select value={importScope} onChange={event => setImportScope(event.target.value as WritableScope)}><option value="project" disabled={!snapshot?.project_root}>This project</option><option value="personal">Personal library</option></select><button className="primary" onClick={() => void importPack()} disabled={loading || !importJSON.trim()}>Validate and import</button></div>
          </>}
        </section>
      )}

      <div className="pack-library-layout">
        <aside className="pack-library-list">
          <div className="pack-filter-tabs">
            {(['active', 'all', 'archived'] as LibraryFilter[]).map(value => <button key={value} className={filter === value ? 'active' : ''} onClick={() => setFilter(value)}>{value}</button>)}
          </div>
          <div className="pack-list-scroll">
            {visible.length === 0 ? <div className="pack-empty">No packs in this view.</div> : visible.map(pack => (
              <button key={pack.key} className={`pack-list-item ${selected?.key === pack.key ? 'selected' : ''} ${pack.archived ? 'archived' : ''} ${!pack.valid ? 'invalid' : ''}`} onClick={() => setSelectedKey(pack.key)}>
                <div><strong>{pack.name}</strong><span>{pack.id}@{pack.version}</span></div>
                <div className="pack-badges"><em>{pack.scope}</em>{!pack.valid && <em className="invalid">quarantined</em>}{pack.active && <em className="active">active</em>}</div>
              </button>
            ))}
          </div>
        </aside>

        <main className="pack-library-detail">
          {!selected ? <div className="pack-empty">Select a pack.</div> : <>
            <header className="pack-detail-head">
              <div><span className="pack-eyebrow">{selected.scope} · {selected.trust}</span><h3>{selected.name}</h3><p>{selected.description || 'No pack description.'}</p></div>
              <div className="pack-detail-actions"><button onClick={() => void exportPack()} disabled={!selected.valid}>Copy JSON</button><button onClick={() => openClone(selected)} disabled={!selected.valid}>Clone</button>{!selected.built_in && <button className={selected.archived ? '' : 'danger'} onClick={() => void toggleArchived()}>{selected.archived ? 'Restore' : 'Archive'}</button>}</div>
            </header>

            {!selected.valid ? <section className="pack-quarantine">
              <span>QUARANTINED LOCAL PACK</span>
              <strong>This file is excluded from new Grids.</strong>
              <p>{selected.validation_error || 'The pack failed structural validation.'}</p>
              <code>{selected.path}</code>
              <small>Fix the JSON file and refresh, or archive it. Existing engagements are unaffected.</small>
            </section> : <>
            <section className="pack-definition-grid">
              <PackFact label="Workflow" value={`${selected.workflow_id}@${selected.workflow_version}`} />
              <PackFact label="Checklist" value={`${selected.checklist_id}@${selected.checklist_version}`} />
              <PackFact label="Coverage" value={`${selected.global_checks} global · ${selected.endpoint_checks} endpoint`} />
              <PackFact label="Automation" value={`${selected.automated_checks} of ${selected.check_count} checks`} />
              <PackFact label="Phases" value={`${selected.phase_count}`} />
              <PackFact label="License" value={selected.license} />
            </section>

            <section className={`pack-quality ${selected.quality_ready ? 'ready' : 'warning'}`}>
              <div><span>QUALITY GATE</span><strong>{selected.quality_score}/100 · {selected.quality_ready ? 'publish ready' : 'needs curation'}</strong></div>
              <p>Structural validation is mandatory. The editorial score checks mappings, procedures, positive and fixed signals, false-positive controls, safety, evidence, and maturity.</p>
            </section>

            <section className="pack-issues">
              <div className="pack-section-head"><strong>Review issues</strong><span>{selected.quality_issues?.length ?? 0}</span></div>
              {!selected.quality_issues?.length ? <div className="pack-empty compact">No unresolved quality issues.</div> : selected.quality_issues.slice(0, 40).map((issue, index) => <div className="pack-issue" key={`${issue.check_id}-${issue.field}-${index}`}><code>{issue.check_id || 'pack'}</code><strong>{issue.field}</strong><span>{issue.message}</span></div>)}
            </section>

            <section className="pack-storage">
              <span>{selected.built_in ? 'Embedded read-only pack' : 'Version file'}</span>
              <code>{selected.path || 'Compiled into TheMauler.exe'}</code>
              <small>Workflow {shortDigest(selected.workflow_digest)} · Checklist {shortDigest(selected.checklist_digest)}</small>
            </section>
            </>}
          </>}
        </main>
      </div>
    </div>
  )
}

function PackMetric({ label, value }: { label: string; value: string | number }) {
  return <div><span>{label}</span><strong>{value}</strong></div>
}

function PackFact({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><strong>{value}</strong></div>
}

function shortDigest(value: string) {
  return value ? value.slice(0, 12) : 'pending'
}

function uniqueCloneID(pack: PackSummary, packs: PackSummary[]) {
  const base = `${pack.id.replace(/-local(?:-\d+)?$/, '')}-local`
  let candidate = base
  let suffix = 2
  const ids = new Set(packs.map(item => item.id))
  while (ids.has(candidate)) candidate = `${base}-${suffix++}`
  return candidate
}
