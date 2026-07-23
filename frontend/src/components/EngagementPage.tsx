import { useEffect, useMemo, useRef, useState } from 'react'
import {
  AddEngagementEvidence,
  AddEngagementEndpoint,
  ConfirmEngagementFinding,
  DeleteEngagement,
  ExportEngagementJSON,
  GetEngagement,
  GetEngagementAvailable,
  GetEngagementNext,
  ImportEngagementJSON,
  ListEngagements,
  ReleaseEngagementClaim,
  SetEngagementEndpointGroup,
  SetEngagementNotes,
  UpsertEngagementFinding,
  type EngagementEndpoint,
  type EngagementAvailableWork,
  type EngagementEvidence,
  type EngagementFinding,
  type EngagementNextAction,
  type EngagementRecord,
  type EngagementSummary,
  type EngagementWorkState,
} from '../wailsjs/go'
import { EngagementSetupWizard } from './EngagementSetupWizard'
import './EngagementPage.css'

interface Props {
  version: number
  onOpenChat: () => void
  onPrepareRun: (prompt: string) => void
}

interface PhaseView {
  id: string
  name: string
  kind?: string
  steps?: Array<{ id?: string; check?: string; title: string }>
}

type EngagementSection = 'workflow' | 'checklist' | 'endpoints' | 'notes' | 'findings' | 'evidence'

interface ChecklistItemView {
  id: string
  title: string
  scope: string
  category?: string
  category_name?: string
  verified?: boolean
}

const terminalStatuses = new Set(['done', 'skipped', 'passed', 'not_applicable'])

export function EngagementPage({ version, onOpenChat, onPrepareRun }: Props) {
  const [engagements, setEngagements] = useState<EngagementSummary[]>([])
  const [selectedID, setSelectedID] = useState('')
  const [record, setRecord] = useState<EngagementRecord | null>(null)
  const [next, setNext] = useState<EngagementNextAction | null>(null)
  const [available, setAvailable] = useState<EngagementAvailableWork | null>(null)
  const [nextBlocker, setNextBlocker] = useState('')
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState('')
  const [error, setError] = useState('')
  const [createName, setCreateName] = useState('')
  const [setupOpen, setSetupOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [importRaw, setImportRaw] = useState('')
  const [endpointDraft, setEndpointDraft] = useState({ method: 'GET', url: '', feature_group: '' })
  const [groupDrafts, setGroupDrafts] = useState<Record<string, string>>({})
  const [evidenceDraft, setEvidenceDraft] = useState({ source_kind: 'artifact' as EngagementEvidence['source_kind'], path: '', ledger_event_id: '', description: '' })
  const [findingDraft, setFindingDraft] = useState({ title: '', severity: 'medium' as EngagementFinding['severity'], description: '', reproduction: '', evidence_ids: '' })
  const [waiverDrafts, setWaiverDrafts] = useState<Record<string, string>>({})
  const [section, setSection] = useState<EngagementSection>('workflow')
  const [notesDraft, setNotesDraft] = useState('')
  const notesDirty = useRef(false)
  const [clock, setClock] = useState(Date.now())

  const load = async (preferredID = selectedID, quiet = false) => {
    if (!quiet) setLoading(true)
    setError('')
    try {
      const listed = await ListEngagements()
      const items = Array.isArray(listed) ? listed : []
      setEngagements(items)
      const id = items.some(item => item.id === preferredID) ? preferredID : items[0]?.id || ''
      setSelectedID(id)
      if (!id) {
        setRecord(null)
        setNext(null)
        setAvailable(null)
        setNextBlocker('')
        return
      }
      const loaded = await GetEngagement(id)
      setRecord(loaded)
      if (!notesDirty.current) setNotesDraft(loaded.state.notes || '')
      setGroupDrafts(Object.fromEntries(Object.values(loaded.state.endpoints ?? {}).map(endpoint => [endpoint.id, endpoint.feature_group || ''])))
      try {
        setNext(await GetEngagementNext(id))
        setNextBlocker('')
      } catch (nextError) {
        setNext(null)
        setNextBlocker(errorText(nextError))
      }
      try {
        setAvailable(await GetEngagementAvailable(id, 8))
      } catch (availableError) {
        setAvailable({ phase_id: loaded.state.current_phase, phase_name: '', parallel: false, items: [], blocker: errorText(availableError) })
      }
    } catch (loadError) {
      setRecord(null)
      setNext(null)
      setAvailable(null)
      setError(errorText(loadError))
    } finally {
      if (!quiet) setLoading(false)
    }
  }

  useEffect(() => {
    notesDirty.current = false
    void load()
  }, [version])

  const phases = useMemo(() => normalisePhases(record?.workflow.phases), [record])
  const progress = useMemo(() => engagementProgress(record), [record])
  const currentPhase = phases.find(phase => phase.id === record?.state.current_phase)
  const currentPhaseSteps = currentPhase?.steps ?? []
  const endpoints = record ? record.state.endpoint_order.map(id => record.state.endpoints[id]).filter(Boolean) : []
  const globalChecks = record ? record.state.global_check_order.map(id => record.state.global_checks[id]).filter(Boolean) : []
  const evidence = record ? (record.state.evidence_order ?? []).map(id => record.state.evidence?.[id]).filter(Boolean) : []
  const findings = record ? (record.state.finding_order ?? []).map(id => record.state.findings?.[id]).filter(Boolean) : []
  const checklistGroups = useMemo(() => groupChecklist(record), [record])
  const endpointGroups = useMemo(() => groupEndpoints(endpoints), [endpoints])
  const activeClaimedWork = next?.work?.claim ? next.work : undefined
  const activeClaimKey = activeClaimedWork?.claim
    ? `${activeClaimedWork.claim.claimant.id}:${activeClaimedWork.claim.lease_until}`
    : ''

  useEffect(() => {
    if (!activeClaimKey || !selectedID) return
    const clockTimer = window.setInterval(() => setClock(Date.now()), 1000)
    const refreshTimer = window.setInterval(() => void load(selectedID, true), 15000)
    return () => {
      window.clearInterval(clockTimer)
      window.clearInterval(refreshTimer)
    }
  }, [activeClaimKey, selectedID])

  const showStatus = (message: string) => {
    setStatus(message)
    window.setTimeout(() => setStatus(current => current === message ? '' : current), 2600)
  }

  const exportJSON = async () => {
    if (!record) return
    setError('')
    try {
      const raw = await ExportEngagementJSON(record.state.id)
      await navigator.clipboard.writeText(raw)
      showStatus('Portable engagement JSON copied')
    } catch (exportError) {
      setError(errorText(exportError))
    }
  }

  const importJSON = async () => {
    if (!importRaw.trim()) return
    setError('')
    try {
      const imported = await ImportEngagementJSON(importRaw)
      setImportRaw('')
      setImportOpen(false)
      showStatus('Engagement imported into this workspace')
      await load(imported.state.id)
    } catch (importError) {
      setError(errorText(importError))
    }
  }

  const remove = async () => {
    if (!record || !confirm(`Delete engagement "${record.state.name}"? Project files and RunLedger evidence are not deleted.`)) return
    try {
      await DeleteEngagement(record.state.id)
      showStatus('Engagement removed')
      await load('')
    } catch (deleteError) {
      setError(errorText(deleteError))
    }
  }

  const releaseClaim = async () => {
    if (!record || !activeClaimedWork?.claim) return
    const claimant = activeClaimedWork.claim.claimant
    if (!confirm(`Release the claim held by ${claimant.alias || claimant.id}? Unfinished work returns to pending.`)) return
    try {
      await ReleaseEngagementClaim(record.state.id, claimant.id)
      showStatus('Claim released; work returned to the queue')
      await load(record.state.id, true)
    } catch (releaseError) {
      setError(errorText(releaseError))
    }
  }

  const addEndpoint = async () => {
    if (!record || !endpointDraft.url.trim()) return
    try {
      await AddEngagementEndpoint(record.state.id, {
        id: '', method: endpointDraft.method || 'GET', url: endpointDraft.url.trim(),
        name: '', feature_group: endpointDraft.feature_group.trim(),
      })
      setEndpointDraft({ method: 'GET', url: '', feature_group: '' })
      showStatus('Endpoint added and checks materialised')
      await load(record.state.id)
    } catch (endpointError) {
      setError(errorText(endpointError))
    }
  }

  const saveGroup = async (endpoint: EngagementEndpoint) => {
    if (!record) return
    try {
      await SetEngagementEndpointGroup(record.state.id, endpoint.id, groupDrafts[endpoint.id] || 'ungrouped')
      showStatus('Endpoint group updated')
      await load(record.state.id)
    } catch (groupError) {
      setError(errorText(groupError))
    }
  }

  const saveNotes = async () => {
    if (!record || !notesDirty.current) return
    try {
      await SetEngagementNotes(record.state.id, notesDraft, record.state.notes_revision || 1)
      notesDirty.current = false
      showStatus('Project notes saved')
      await load(record.state.id)
    } catch (notesError) {
      setError(errorText(notesError))
    }
  }

  const selectEngagement = (id: string) => {
    notesDirty.current = false
    setNotesDraft('')
    setSection('workflow')
    void load(id)
  }

  const addEvidence = async () => {
    if (!record || !activeClaimedWork || !evidenceDraft.description.trim()) return
    try {
      await AddEngagementEvidence(record.state.id, {
        work: activeClaimedWork.ref, source_kind: evidenceDraft.source_kind,
        path: evidenceDraft.path.trim() || undefined, ledger_event_id: evidenceDraft.ledger_event_id.trim() || undefined,
        description: evidenceDraft.description.trim(),
      })
      setEvidenceDraft({ source_kind: 'artifact', path: '', ledger_event_id: '', description: '' })
      showStatus('Evidence provenance attached')
      await load(record.state.id)
    } catch (evidenceError) { setError(errorText(evidenceError)) }
  }

  const addFinding = async () => {
    if (!record || !activeClaimedWork || !findingDraft.title.trim()) return
    try {
      await UpsertEngagementFinding(record.state.id, {
        work: activeClaimedWork.ref, title: findingDraft.title.trim(), severity: findingDraft.severity,
        description: findingDraft.description.trim(), reproduction: findingDraft.reproduction.trim(),
        evidence_ids: findingDraft.evidence_ids.split(',').map(value => value.trim()).filter(Boolean),
      })
      setFindingDraft({ title: '', severity: 'medium', description: '', reproduction: '', evidence_ids: '' })
      showStatus('Draft finding added')
      await load(record.state.id)
    } catch (findingError) { setError(errorText(findingError)) }
  }

  const confirmFinding = async (finding: EngagementFinding) => {
    if (!record) return
    try {
      await ConfirmEngagementFinding(record.state.id, finding.id, finding.revision, waiverDrafts[finding.id] || '')
      showStatus('Finding confirmed')
      await load(record.state.id)
    } catch (confirmError) { setError(errorText(confirmError)) }
  }

  return (
    <div className="engagement-page">
      <header className="engagement-header">
        <div>
          <span className="engagement-kicker">Operator workflow</span>
          <h1>Engagement Grid</h1>
          <p>One durable claim → act → observe → finish workflow shared by you and the agent.</p>
        </div>
        <div className="engagement-actions">
          {status && <span>{status}</span>}
          <button onClick={() => setImportOpen(value => !value)}>{importOpen ? 'Close import' : 'Import JSON'}</button>
          <button onClick={() => void load()}>Refresh</button>
          {record && <button className="primary" onClick={onOpenChat}>Continue in Chat</button>}
        </div>
      </header>

      {error && <div className="engagement-error">{error}</div>}
      {importOpen && <section className="engagement-import" aria-label="Import portable engagement JSON">
        <div>
          <strong>Import engagement snapshot</strong>
          <span>The current workspace becomes authoritative. Pinned workflow/checklist digests are verified and live claims are cleared.</span>
        </div>
        <textarea value={importRaw} onChange={event => setImportRaw(event.target.value)} placeholder="Paste an exported Engagement Grid JSON snapshot" spellCheck={false} />
        <div className="engagement-import-actions">
          <button onClick={() => { setImportRaw(''); setImportOpen(false) }}>Cancel</button>
          <button className="primary" disabled={!importRaw.trim()} onClick={() => void importJSON()}>Verify and import</button>
        </div>
      </section>}

      <div className="engagement-shell">
        <aside className="engagement-list">
          <div className="engagement-list-head"><span>Project engagements</span><strong>{engagements.length}</strong></div>
          {engagements.map(item => (
            <button key={item.id} className={item.id === selectedID ? 'active' : ''} onClick={() => selectEngagement(item.id)}>
              <strong>{item.name}</strong>
              <span>{item.current_phase.replaceAll('_', ' ')}</span>
              <small>{item.workflow_id}@{item.workflow_version || 'pinned'}</small>
            </button>
          ))}
          {!loading && engagements.length === 0 && <div className="engagement-list-empty">No grid exists for this workspace yet.</div>}
          <div className="engagement-create">
            <label><span>New engagement</span><input value={createName} onChange={event => setCreateName(event.target.value)} placeholder="Uses active project name" /></label>
            <small>Web workflow · 20-check OWASP pack · locked project scope</small>
            <button className="primary" onClick={() => setSetupOpen(true)}>Guided setup</button>
          </div>
        </aside>

        <main className="engagement-main">
          {loading ? <div className="engagement-empty">Loading engagement state…</div> : !record ? (
            <div className="engagement-empty engagement-onboarding">
              <div className="engagement-onboarding-copy">
                <span className="engagement-kicker">Project operator workspace</span>
                <strong>Turn this project into a governed test</strong>
                <p>Create a grid to pin the reviewed workflow, 20-check OWASP pack, authorised target scope, and evidence rules into Mauler's native SQLite state.</p>
              </div>
              <div className="engagement-onboarding-flow" aria-label="Engagement workflow preview">
                <div><span>1</span><strong>Workflow</strong><small>Seven gated phases</small></div>
                <div><span>2</span><strong>Checklist</strong><small>Global coverage</small></div>
                <div><span>3</span><strong>Endpoints</strong><small>Per-route checks</small></div>
                <div><span>4</span><strong>Notes</strong><small>Shared risk model</small></div>
                <div><span>5</span><strong>Findings</strong><small>Evidence gates</small></div>
              </div>
              <div className="engagement-onboarding-callout">
                <span>Ready when you are</span>
                <strong>Use the project name or enter one on the left, then create the grid.</strong>
                <button className="primary" onClick={() => setSetupOpen(true)}>Start guided setup</button>
              </div>
            </div>
          ) : <>
            <section className="engagement-overview">
              <div><span className="engagement-kicker">Active engagement</span><h2>{record.state.name}</h2><p>{record.workflow.name} · {record.checklist.name}</p></div>
              <div className="engagement-overview-actions"><span className="engagement-lock">Scope locked</span><button onClick={() => void exportJSON()}>Export JSON</button><button className="danger" onClick={() => void remove()}>Delete</button></div>
            </section>

            <section className="engagement-metrics">
              <Metric label="Phase" value={currentPhase?.name || record.state.current_phase} />
              <Metric label="Progress" value={`${progress.done}/${progress.total}`} />
              <Metric label="Endpoints" value={endpoints.length.toString()} />
              <Metric label="Evidence" value={evidence.length.toString()} />
              <Metric label="Findings" value={findings.length.toString()} />
              <Metric label="Revision" value={record.state.revision.toString()} />
            </section>

            <section className="engagement-phases" aria-label="Workflow phases">
              {phases.map((phase, index) => {
                const phaseProgress = progressForPhase(record, phase)
                const active = phase.id === record.state.current_phase
                return <div key={phase.id} className={`${active ? 'active' : ''}${phaseProgress.total > 0 && phaseProgress.done === phaseProgress.total ? ' complete' : ''}`}>
                  <span>{phaseProgress.total > 0 && phaseProgress.done === phaseProgress.total ? '✓' : index + 1}</span><strong>{phase.name}</strong><small>{phaseProgress.total ? `${phaseProgress.done}/${phaseProgress.total}` : phase.kind || 'gate'}</small>
                </div>
              })}
            </section>

            <nav className="engagement-section-nav" aria-label="Engagement operator views">
              <SectionButton active={section === 'workflow'} label="Workflow" count={`${progress.done}/${progress.total}`} onClick={() => setSection('workflow')} />
              <SectionButton active={section === 'checklist'} label="Checklist" count={globalChecks.length.toString()} onClick={() => setSection('checklist')} />
              <SectionButton active={section === 'endpoints'} label="Endpoints" count={endpoints.length.toString()} onClick={() => setSection('endpoints')} />
              <SectionButton active={section === 'notes'} label="Notes" count={record.state.notes?.trim() ? '1' : '0'} onClick={() => setSection('notes')} />
              <SectionButton active={section === 'findings'} label="Findings" count={findings.length.toString()} onClick={() => setSection('findings')} />
              <SectionButton active={section === 'evidence'} label="Evidence" count={evidence.length.toString()} onClick={() => setSection('evidence')} />
            </nav>

            {section === 'workflow' && <>

            <section className="engagement-next">
              <div>
                <span className="engagement-kicker">Deterministic next action</span>
                {next?.work ? <><h3>{next.work.title}</h3><p>{next.work.ref.kind.replaceAll('_', ' ')} · {next.action} · revision {next.work.revision}</p></> : next ? <><h3>{next.workflow_done ? 'Workflow complete' : next.phase_complete ? 'Phase ready to advance' : next.action}</h3><p>{next.phase_name || next.phase_id || record.state.current_phase}</p></> : <><h3>Operator attention needed</h3><p>{nextBlocker || 'No next action is currently available.'}</p></>}
              </div>
              {next?.work?.claim && <div className={`engagement-claim ${claimRemaining(next.work.claim.lease_until, clock).expired ? 'stale' : ''}`}>
                <span>Claimed by</span>
                <strong>{next.work.claim.claimant.alias || next.work.claim.claimant.id}</strong>
                <small>{claimantLane(next.work.claim.claimant.id)} · heartbeat {formatTime(next.work.claim.heartbeat_at || next.work.claim.claimed_at)}</small>
                <code title={next.work.claim.claimant.id}>{next.work.claim.claimant.id}</code>
                <small>{claimRemaining(next.work.claim.lease_until, clock).label}</small>
                <button onClick={() => void releaseClaim()}>Release</button>
              </div>}
              <button className="primary" onClick={onOpenChat}>Ask agent to continue</button>
            </section>

            <section className={`engagement-queue ${available?.parallel ? 'parallel' : ''}`} aria-label="Available engagement work">
              <div className="engagement-queue-head">
                <div><span className="engagement-kicker">Deterministic queue</span><h3>{available?.parallel ? 'Parallel checks available' : 'Available work'}</h3></div>
                <span>{available?.items.length || 0} ready</span>
              </div>
              <div className="engagement-queue-items">
                {(available?.items || []).map((work, index) => <div key={`${work.ref.kind}:${work.ref.endpoint_id || work.ref.phase_id || ''}:${work.ref.id}`} className="engagement-queue-item">
                  <span>{index + 1}</span><div><strong>{work.title}</strong><small>{work.ref.kind.replaceAll('_', ' ')}{work.ref.endpoint_id ? ` · ${work.ref.endpoint_id}` : ''}</small></div><code>{work.ref.id}</code>
                </div>)}
                {!available?.items.length && <div className="engagement-inline-empty">{available?.blocker || (next?.workflow_done ? 'Workflow complete.' : 'No unclaimed work is ready in this phase.')}</div>}
              </div>
              {available?.parallel && <p>Each row is independently claimable. The main agent can delegate distinct rows to bounded tasks without duplicating work.</p>}
            </section>

            <section className="engagement-scope"><span>Locked scope</span><div>{record.state.scope.map(item => <code key={item}>{item}</code>)}</div></section>

            <div className="engagement-workspace-view">
              <section className="engagement-panel">
                <div className="engagement-panel-head"><div><span className="engagement-kicker">Current phase</span><h3>{currentPhase?.name || record.state.current_phase}</h3></div><span>{currentPhaseSteps.length || globalChecks.length} items</span></div>
                <div className="engagement-work-list">
                  {currentPhaseSteps.map(step => {
                    const id = step.id || step.check || ''
                    const work = record.state.steps[`${record.state.current_phase}/${id}`]
                    return <WorkRow key={id} work={work} fallbackTitle={step.title} />
                  })}
                  {currentPhaseSteps.length === 0 && globalChecks.map(work => <WorkRow key={work.ref.id} work={work} />)}
                  {currentPhaseSteps.length === 0 && globalChecks.length === 0 && <div className="engagement-inline-empty">No materialised items in this phase.</div>}
                </div>
              </section>
            </div>
            </>}

            {section === 'endpoints' && <div className="engagement-catalog-view">
              <div className="engagement-view-intro"><div><span className="engagement-kicker">Discovered attack surface</span><h2>Endpoints</h2><p>Group related routes, then expand an endpoint to inspect its complete per-endpoint checklist.</p></div><strong>{endpoints.length} recorded</strong></div>
              <section className="engagement-panel engagement-full-panel">
                <div className="engagement-endpoint-form">
                  <select value={endpointDraft.method} onChange={event => setEndpointDraft(value => ({ ...value, method: event.target.value }))}><option>GET</option><option>POST</option><option>PUT</option><option>PATCH</option><option>DELETE</option></select>
                  <input value={endpointDraft.url} onChange={event => setEndpointDraft(value => ({ ...value, url: event.target.value }))} placeholder="/api/users or https://target/path" />
                  <input value={endpointDraft.feature_group} onChange={event => setEndpointDraft(value => ({ ...value, feature_group: event.target.value }))} placeholder="Feature group" />
                  <button className="primary" onClick={() => void addEndpoint()} disabled={!endpointDraft.url.trim()}>Add endpoint</button>
                </div>
                <div className="engagement-endpoint-groups">
                  {endpointGroups.map(group => <section key={group.name} className="engagement-endpoint-group">
                    <div className="engagement-endpoint-group-head"><strong>{group.name}</strong><span>{group.endpoints.length} endpoint{group.endpoints.length === 1 ? '' : 's'}</span></div>
                    {group.endpoints.map(endpoint => {
                      const endpointChecks = Object.values(record.state.endpoint_checks).filter(work => work.ref.endpoint_id === endpoint.id)
                      const done = endpointChecks.filter(work => work.finished).length
                      return <details key={endpoint.id} className="engagement-endpoint-card">
                        <summary><span className="engagement-method">{endpoint.method}</span><div><strong>{endpoint.name || endpoint.url}</strong><code>{endpoint.url}</code></div><ProgressBadge done={done} total={endpointChecks.length} /></summary>
                        <div className="engagement-endpoint-card-body">
                          <div className="engagement-group-edit"><label>Feature group</label><input value={groupDrafts[endpoint.id] ?? ''} onChange={event => setGroupDrafts(value => ({ ...value, [endpoint.id]: event.target.value }))} placeholder="Ungrouped" /><button onClick={() => void saveGroup(endpoint)}>Save group</button></div>
                          <div className="engagement-work-list">{endpointChecks.map(work => <WorkRow key={work.ref.id} work={work} />)}</div>
                        </div>
                      </details>
                    })}
                  </section>)}
                  {endpoints.length === 0 && <div className="engagement-inline-empty">No endpoints recorded yet. Add one here or let the agent materialise routes during reconnaissance.</div>}
                </div>
              </section>
            </div>}

            {section === 'checklist' && <div className="engagement-catalog-view">
              <div className="engagement-view-intro"><div><span className="engagement-kicker">Whole-application coverage</span><h2>Checklist</h2><p>Global checks are grouped by test family. Every result remains attributed, revisioned, and evidence-gated.</p></div><ProgressBadge done={globalChecks.filter(item => item.finished).length} total={globalChecks.length} /></div>
              {checklistGroups.map(group => {
                const groupWork = group.items.map(item => record.state.global_checks[item.id]).filter(Boolean)
                const done = groupWork.filter(item => item.finished).length
                return <section key={group.id} className="engagement-check-group">
                  <div className="engagement-check-group-head"><div><strong>{group.name}</strong><small>{group.items.length} checks</small></div><ProgressBadge done={done} total={group.items.length} /></div>
                  <div className="engagement-work-list">
                    {group.items.map(item => <WorkRow key={item.id} work={record.state.global_checks[item.id]} fallbackTitle={item.title} verified={item.verified} />)}
                  </div>
                </section>
              })}
            </div>}

            {section === 'notes' && <div className="engagement-catalog-view">
              <div className="engagement-view-intro"><div><span className="engagement-kicker">Shared compact context</span><h2>Project notes</h2><p>Keep the target model, trust boundaries, roles, hypotheses, and highest-value risks here—not a chronological command log.</p></div><button className="primary" disabled={!notesDirty.current} onClick={() => void saveNotes()}>Save notes</button></div>
              <section className="engagement-notes-panel">
                <textarea value={notesDraft} maxLength={24000} onChange={event => { notesDirty.current = true; setNotesDraft(event.target.value) }} placeholder={'# Target model\n\n## Main use case\n\n## Roles and trust boundaries\n\n## Highest-value risks\n\n## Open hypotheses'} spellCheck={false} />
                <footer><span>{notesDraft.length.toLocaleString()} / 24,000 characters</span><span>Revision {record.state.notes_revision || 1}{record.state.notes_updated_at ? ` · updated ${formatTime(record.state.notes_updated_at)}` : ''}</span></footer>
              </section>
            </div>}

            {section === 'evidence' && <div className="engagement-proof-grid">
              <section className="engagement-panel">
                <div className="engagement-panel-head"><div><span className="engagement-kicker">Immutable pointers</span><h3>Evidence provenance</h3></div><span>{evidence.length}</span></div>
                <div className="engagement-proof-form">
                  <select value={evidenceDraft.source_kind} onChange={event => setEvidenceDraft(value => ({ ...value, source_kind: event.target.value as EngagementEvidence['source_kind'] }))}>
                    <option value="artifact">Artifact</option><option value="http_capture">HTTP capture</option><option value="screenshot">Screenshot</option><option value="ledger_event">Ledger event</option><option value="external_file">External file</option>
                  </select>
                  <input value={evidenceDraft.path} onChange={event => setEvidenceDraft(value => ({ ...value, path: event.target.value }))} placeholder="Workspace file path" />
                  <input value={evidenceDraft.ledger_event_id} onChange={event => setEvidenceDraft(value => ({ ...value, ledger_event_id: event.target.value }))} placeholder="RunLedger event ID (optional)" />
                  <input value={evidenceDraft.description} onChange={event => setEvidenceDraft(value => ({ ...value, description: event.target.value }))} placeholder="What this proves" />
                  <button onClick={() => void addEvidence()} disabled={!activeClaimedWork || !evidenceDraft.description.trim()}>Attach</button>
                  {!activeClaimedWork && <small>Evidence can be attached while the next work item is actively claimed.</small>}
                </div>
                <div className="engagement-proof-list">
                  {evidence.map(item => <div key={item.id} className="engagement-proof-row"><span className={`engagement-status ${item.agent_composed ? 'warn' : 'ok'}`}>{item.source_kind.replaceAll('_', ' ')}</span><div><strong>{item.description}</strong><small>{item.path || item.ledger_event_id} · run {item.run} · {item.agent_composed ? 'agent-composed' : 'raw provenance'}</small></div><code>{item.sha256?.slice(0, 10) || 'ledger'}</code></div>)}
                  {evidence.length === 0 && <div className="engagement-inline-empty">No evidence pointers yet. Raw bodies remain in artifacts and RunLedger.</div>}
                </div>
              </section>
            </div>}

            {section === 'findings' && <div className="engagement-proof-grid">
              <section className="engagement-panel engagement-full-panel">
                <div className="engagement-panel-head"><div><span className="engagement-kicker">Report gates</span><h3>Findings</h3></div><span>{findings.length}</span></div>
                <div className="engagement-finding-form">
                  <input value={findingDraft.title} onChange={event => setFindingDraft(value => ({ ...value, title: event.target.value }))} placeholder="Finding title" />
                  <select value={findingDraft.severity} onChange={event => setFindingDraft(value => ({ ...value, severity: event.target.value as EngagementFinding['severity'] }))}><option>info</option><option>low</option><option>medium</option><option>high</option><option>critical</option></select>
                  <input value={findingDraft.description} onChange={event => setFindingDraft(value => ({ ...value, description: event.target.value }))} placeholder="Description / impact" />
                  <input value={findingDraft.reproduction} onChange={event => setFindingDraft(value => ({ ...value, reproduction: event.target.value }))} placeholder="Reproduction steps" />
                  <input value={findingDraft.evidence_ids} onChange={event => setFindingDraft(value => ({ ...value, evidence_ids: event.target.value }))} placeholder="Evidence IDs, comma-separated" />
                  <button onClick={() => void addFinding()} disabled={!activeClaimedWork || !findingDraft.title.trim()}>Add draft</button>
                </div>
                <div className="engagement-proof-list">
                  {findings.map(item => <div key={item.id} className="engagement-finding-row">
                    <div><span className={`engagement-status ${item.state === 'confirmed' ? 'ok' : 'warn'}`}>{item.state}</span><strong>{item.title}</strong><small>{item.severity} · {item.evidence_ids.length} evidence ref(s) · revision {item.revision}</small></div>
                    {item.state === 'draft' && <div className="engagement-waiver"><input value={waiverDrafts[item.id] || ''} onChange={event => setWaiverDrafts(value => ({ ...value, [item.id]: event.target.value }))} placeholder="Operator waiver if screenshot is unsuitable" /><button onClick={() => void confirmFinding(item)}>Confirm</button></div>}
                  </div>)}
                  {findings.length === 0 && <div className="engagement-inline-empty">No findings yet. Confirmation requires reproduction and raw evidence.</div>}
                </div>
              </section>
            </div>}
          </>}
        </main>
      </div>
      {setupOpen && <EngagementSetupWizard
        defaultName={createName}
        onClose={() => setSetupOpen(false)}
        onCreated={async created => {
          setCreateName('')
          showStatus('Engagement created with locked project scope')
          await load(created.state.id)
        }}
        onOpenGrid={() => setSetupOpen(false)}
        onPrepareRun={onPrepareRun}
      />}
    </div>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><strong>{value}</strong></div>
}

function SectionButton({ active, label, count, onClick }: { active: boolean; label: string; count: string; onClick: () => void }) {
  return <button className={active ? 'active' : ''} onClick={onClick} aria-current={active ? 'page' : undefined}><span>{label}</span><strong>{count}</strong></button>
}

function ProgressBadge({ done, total }: { done: number; total: number }) {
  const percent = total > 0 ? Math.round((done / total) * 100) : 0
  return <span className="engagement-progress-badge" title={`${done} of ${total} complete`}><span style={{ width: `${percent}%` }} /><strong>{done}/{total}</strong></span>
}

function groupChecklist(record: EngagementRecord | null) {
  const groups = new Map<string, { id: string; name: string; items: ChecklistItemView[] }>()
  const items = (record?.checklist.items || []) as ChecklistItemView[]
  for (const item of items) {
    if (item.scope !== 'global') continue
    const id = item.category || 'other'
    const group = groups.get(id) || { id, name: item.category_name || id.replaceAll('_', ' '), items: [] }
    group.items.push(item)
    groups.set(id, group)
  }
  return [...groups.values()]
}

function groupEndpoints(endpoints: EngagementEndpoint[]) {
  const groups = new Map<string, EngagementEndpoint[]>()
  for (const endpoint of endpoints) {
    const name = endpoint.feature_group?.trim() || 'Ungrouped'
    groups.set(name, [...(groups.get(name) || []), endpoint])
  }
  return [...groups.entries()].map(([name, grouped]) => ({ name, endpoints: grouped }))
}

function WorkRow({ work, fallbackTitle, verified = false }: { work?: EngagementWorkState; fallbackTitle?: string; verified?: boolean }) {
  const status = work?.status || 'pending'
  return <div className="engagement-work-row">
    <span className={`engagement-status ${statusTone(status)}`}>{status.replaceAll('_', ' ')}</span>
    <div><strong>{work?.title || fallbackTitle || 'Untitled work item'}</strong><small>{work?.observation || work?.ref.id || 'Not started'}</small></div>
    <div className="engagement-work-meta">{verified && <span className="engagement-verified">fixture verified</span>}<span className="engagement-run-count">{work ? `${work.runs_completed}/${work.runs === 'indefinite' ? '∞' : work.runs}` : '-'}</span></div>
  </div>
}

function normalisePhases(input: unknown[] | undefined): PhaseView[] {
  if (!Array.isArray(input)) return []
  return input.filter((item): item is PhaseView => Boolean(item && typeof item === 'object' && 'id' in item && 'name' in item))
}

function engagementProgress(record: EngagementRecord | null) {
  if (!record) return { done: 0, total: 0 }
  const work = [...Object.values(record.state.steps), ...Object.values(record.state.global_checks), ...Object.values(record.state.endpoint_checks)]
  return { done: work.filter(item => item.finished).length, total: work.length }
}

function progressForPhase(record: EngagementRecord, phase: PhaseView) {
  const steps = Object.values(record.state.steps).filter(work => work.ref.phase_id === phase.id)
  const work = phase.kind === 'checklist'
    ? [...steps, ...Object.values(record.state.global_checks)]
    : phase.kind === 'endpoint'
      ? [...steps, ...Object.values(record.state.endpoint_checks)]
      : steps
  return { done: work.filter(item => item.finished).length, total: work.length }
}

function statusTone(status: string) {
  if (['vulnerable', 'failed'].includes(status)) return 'bad'
  if (['warning', 'focused'].includes(status)) return 'warn'
  if (terminalStatuses.has(status)) return 'ok'
  return 'muted'
}

function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function claimRemaining(value: string, now: number) {
  const expiry = new Date(value).getTime()
  if (!Number.isFinite(expiry)) return { expired: true, label: 'invalid lease timestamp' }
  const remaining = expiry - now
  if (remaining <= 0) return { expired: true, label: 'lease expired; safe to release' }
  const totalSeconds = Math.ceil(remaining / 1000)
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return { expired: false, label: `${minutes}m ${seconds.toString().padStart(2, '0')}s remaining` }
}

function claimantLane(id: string) {
  if (id.startsWith('channel:')) return id.split(':')[1] || 'channel'
  if (id.includes('/subagent-') || id.startsWith('subagent-')) return 'bounded task'
  return 'desktop run'
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}
