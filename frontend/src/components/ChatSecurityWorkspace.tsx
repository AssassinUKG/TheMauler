import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  GetEngagement,
  GetEngagementNext,
  ListEngagements,
  type EngagementFinding,
  type EngagementWorkState,
  type EngagementNextAction,
  type EngagementRecord,
  type EngagementSummary,
} from '../wailsjs/go'

interface Props {
  version: number
  workspaceRoot: string
  onClose: () => void
  onOpenEngagement: () => void
  onPrepareRun: (prompt: string) => void
}

function normalisePath(value: string) {
  return value.trim().replaceAll('\\', '/').replace(/\/$/, '').toLowerCase()
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

export function ChatSecurityWorkspace({ version, workspaceRoot, onClose, onOpenEngagement, onPrepareRun }: Props) {
  const [summaries, setSummaries] = useState<EngagementSummary[]>([])
  const [selectedID, setSelectedID] = useState('')
  const [record, setRecord] = useState<EngagementRecord | null>(null)
  const [next, setNext] = useState<EngagementNextAction | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    void ListEngagements().then(items => {
      if (cancelled) return
      const root = normalisePath(workspaceRoot)
      const relevant = [...(items || [])]
        .filter(item => !root || normalisePath(item.workspace) === root)
        .sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime())
      setSummaries(relevant)
      setSelectedID(current => relevant.some(item => item.id === current) ? current : relevant[0]?.id || '')
      if (relevant.length === 0) setLoading(false)
    }).catch(loadError => {
      if (!cancelled) {
        setError(errorText(loadError))
        setLoading(false)
      }
    })
    return () => { cancelled = true }
  }, [version, workspaceRoot])

  useEffect(() => {
    let cancelled = false
    if (!selectedID) {
      setRecord(null)
      setNext(null)
      return () => { cancelled = true }
    }
    setLoading(true)
    setError('')
    void Promise.all([
      GetEngagement(selectedID),
      GetEngagementNext(selectedID).catch(() => null as EngagementNextAction | null),
    ])
      .then(([loaded, nextAction]) => {
        if (cancelled) return
        setRecord(loaded)
        setNext(nextAction)
        setLoading(false)
      })
      .catch(loadError => {
        if (!cancelled) {
          setError(errorText(loadError))
          setLoading(false)
        }
      })
    return () => { cancelled = true }
  }, [selectedID, version])

  const inbox = useMemo(() => {
    if (!record) return { observed: [] as EngagementWorkState[], draft: [] as EngagementFinding[], validated: [] as EngagementFinding[], rejected: [] as EngagementFinding[], stale: new Set<string>() }
    const allWork = [
      ...Object.values(record.state.steps || {}),
      ...Object.values(record.state.global_checks || {}),
      ...Object.values(record.state.endpoint_checks || {}),
    ]
    const findings = (record.state.finding_order || [])
      .map(id => record.state.findings?.[id])
      .filter((item): item is EngagementFinding => Boolean(item))
    const workWithFinding = new Set(findings.map(item => workKey(item.work)))
    const stale = new Set(findings.filter(finding => finding.evidence_ids.some(id => {
      const state = record.evidence_freshness?.[id]?.state
      return state === 'stale' || state === 'unverifiable' || !state
    })).map(item => item.id))
    return {
      observed: allWork.filter(item => item.status === 'vulnerable' && !workWithFinding.has(workKey(item.ref))),
      draft: findings.filter(item => item.state === 'draft' || (item.state === 'confirmed' && stale.has(item.id))),
      validated: findings.filter(item => item.state === 'confirmed' && !stale.has(item.id)),
      rejected: findings.filter(item => item.state === 'rejected'),
      stale,
    }
  }, [record])

  const stats = useMemo(() => {
    if (!record) return { done: 0, total: 0, evidence: 0, drafts: 0, confirmed: 0 }
    const work = [
      ...Object.values(record.state.steps || {}),
      ...Object.values(record.state.global_checks || {}),
      ...Object.values(record.state.endpoint_checks || {}),
    ]
    return {
      done: work.filter(item => item.finished).length,
      total: work.length,
      evidence: Object.keys(record.state.evidence || {}).length,
      drafts: inbox.draft.length,
      confirmed: inbox.validated.length,
    }
  }, [record, inbox])

  const prepareNext = () => {
    if (!record) return
    onPrepareRun([
      `Continue the authorised engagement "${record.state.name}".`,
      `Operate only inside its locked scope: ${record.state.scope.join(', ')}.`,
      `Claim and complete the next eligible work item${next?.work?.title ? `: ${next.work.title}` : ''}.`,
      'Reuse existing reconnaissance and evidence. Do not repeat completed work.',
      'Record observations and raw evidence in the Engagement Grid; do not mark a vulnerability confirmed without reproducible evidence.',
    ].join('\n'))
  }

  const prepareValidation = () => {
    if (!record) return
    onPrepareRun([
      `Validate the draft findings for the authorised engagement "${record.state.name}".`,
      `Operate only inside its locked scope: ${record.state.scope.join(', ')}.`,
      'For each draft, inspect its existing evidence first, run the smallest safe reproduction needed, and classify it as confirmed, rejected, or still missing evidence.',
      'Attach raw request/response, browser, screenshot, artifact, or RunLedger evidence. Assistant prose is not proof.',
      'Do not broaden reconnaissance or run destructive validation without explicit operator approval.',
    ].join('\n'))
  }

  const prepareFinding = (finding: EngagementFinding) => {
    if (!record) return
    const stale = inbox.stale.has(finding.id)
    onPrepareRun([
      `${stale ? 'Revalidate' : 'Validate'} finding "${finding.title}" (${finding.id}) in the authorised engagement "${record.state.name}".`,
      `Operate only inside its locked scope: ${record.state.scope.join(', ')}.`,
      stale ? 'Its prior file evidence fingerprint no longer matches. Inspect the current artifact, reproduce the claim, and attach fresh evidence before requesting confirmation.' : 'Inspect the referenced evidence first and run only the smallest safe reproduction needed.',
      'Update the finding through the Engagement tools. Assistant prose is not validation evidence.',
    ].join('\n'))
  }

  const prepareObservation = (work: EngagementWorkState) => {
    if (!record) return
    onPrepareRun([
      `Turn the observed vulnerable work item "${work.title}" into a draft finding for the authorised engagement "${record.state.name}".`,
      `Operate only inside its locked scope: ${record.state.scope.join(', ')}.`,
      `Start from this recorded observation: ${work.observation || 'Review the Engagement Grid observation.'}`,
      'Reuse its raw evidence, add reproducible steps and impact, and keep the finding in Draft until its evidence is independently validated.',
    ].join('\n'))
  }

  return (
    <section className="chat-security-panel" aria-label="Security workspace">
      <header>
        <div>
          <span>Authorised testing</span>
          <strong>{record?.state.name || 'Security workspace'}</strong>
        </div>
        <div className="chat-security-head-actions">
          {summaries.length > 1 && (
            <select value={selectedID} onChange={event => setSelectedID(event.target.value)} aria-label="Active engagement">
              {summaries.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
            </select>
          )}
          <button onClick={onOpenEngagement}>Open Grid</button>
          <button onClick={onClose} title="Close security workspace">×</button>
        </div>
      </header>

      {loading && <div className="chat-security-empty">Loading engagement state…</div>}
      {!loading && error && <div className="chat-security-empty error">{error}</div>}
      {!loading && !error && !record && (
        <div className="chat-security-onboarding">
          <div>
            <strong>No locked engagement in this workspace</strong>
            <p>Create an Engagement Grid before automated testing so targets, exclusions, evidence and findings have one authoritative owner.</p>
          </div>
          <button className="primary" onClick={onOpenEngagement}>Create engagement</button>
        </div>
      )}
      {!loading && !error && record && (
        <>
          <div className="chat-security-metrics">
            <SecurityMetric label="Phase" value={record.state.current_phase || 'intake'} />
            <SecurityMetric label="Coverage" value={`${stats.done}/${stats.total}`} />
            <SecurityMetric label="Evidence" value={String(stats.evidence)} />
            <SecurityMetric label="Draft findings" value={String(stats.drafts)} tone={stats.drafts ? 'warn' : ''} />
            <SecurityMetric label="Confirmed" value={String(stats.confirmed)} tone={stats.confirmed ? 'good' : ''} />
          </div>
          <div className="chat-security-scope">
            <span>Locked scope</span>
            <div>{record.state.scope.map(target => <code key={target}>{target}</code>)}</div>
          </div>
          <div className="chat-security-next">
            <div>
              <span>{next?.workflow_done ? 'Workflow complete' : 'Next controlled action'}</span>
              <strong>{next?.work?.title || (next?.workflow_done ? 'Review evidence and report' : 'Open Grid to choose work')}</strong>
              <small>{next?.work?.observation || 'Every confirmed finding must retain reproducible, fresh evidence.'}</small>
            </div>
            <div>
              {!next?.workflow_done && <button className="primary" onClick={prepareNext}>Prepare next test</button>}
              <button onClick={prepareValidation} disabled={stats.drafts === 0}>Validate drafts{stats.drafts ? ` (${stats.drafts})` : ''}</button>
            </div>
          </div>
          <div className="chat-findings-inbox" aria-label="Findings inbox">
            <FindingLane title="Observed" description="Signals awaiting a finding" tone="observed" count={inbox.observed.length} empty="No untriaged observations">
              {inbox.observed.map(work => (
                <button className="finding-card" key={workKey(work.ref)} onClick={() => prepareObservation(work)}>
                  <strong>{work.title}</strong>
                  <small>{work.observation || 'Vulnerable result recorded'}</small>
                  <span>Draft finding →</span>
                </button>
              ))}
            </FindingLane>
            <FindingLane title="Draft" description="Needs proof or revalidation" tone="draft" count={inbox.draft.length} empty="No findings awaiting validation">
              {inbox.draft.map(finding => (
                <button className="finding-card" key={finding.id} onClick={() => prepareFinding(finding)}>
                  <FindingCardTitle finding={finding} />
                  <small>{inbox.stale.has(finding.id) ? 'Fingerprint changed · revalidation required' : `${finding.evidence_ids.length} evidence reference${finding.evidence_ids.length === 1 ? '' : 's'}`}</small>
                  <span>{inbox.stale.has(finding.id) ? 'Revalidate →' : 'Validate →'}</span>
                </button>
              ))}
            </FindingLane>
            <FindingLane title="Validated" description="Fresh evidence-backed findings" tone="validated" count={inbox.validated.length} empty="No validated findings">
              {inbox.validated.map(finding => (
                <button className="finding-card" key={finding.id} onClick={onOpenEngagement}>
                  <FindingCardTitle finding={finding} />
                  <small>{finding.evidence_ids.length} fresh or immutable evidence reference{finding.evidence_ids.length === 1 ? '' : 's'}</small>
                  <span>Review evidence →</span>
                </button>
              ))}
            </FindingLane>
            <FindingLane title="Rejected" description="Disproved or withdrawn" tone="rejected" count={inbox.rejected.length} empty="No rejected findings">
              {inbox.rejected.map(finding => (
                <button className="finding-card" key={finding.id} onClick={onOpenEngagement}>
                  <FindingCardTitle finding={finding} />
                  <small>Retained for audit history</small>
                  <span>Review decision →</span>
                </button>
              ))}
            </FindingLane>
          </div>
        </>
      )}
    </section>
  )
}

function workKey(ref: { kind: string; phase_id?: string; endpoint_id?: string; id: string }) {
  if (ref.kind === 'step') return `${ref.phase_id || ''}/${ref.id}`
  if (ref.kind === 'endpoint_check') return `${ref.endpoint_id || ''}/${ref.id}`
  return ref.id
}

function FindingLane({ title, description, tone, count, empty, children }: { title: string; description: string; tone: string; count: number; empty: string; children: ReactNode }) {
  return (
    <section className={`finding-lane ${tone}`}>
      <header><div><strong>{title}</strong><small>{description}</small></div><span>{count}</span></header>
      <div className="finding-lane-items">{count ? children : <p>{empty}</p>}</div>
    </section>
  )
}

function FindingCardTitle({ finding }: { finding: EngagementFinding }) {
  return <div className="finding-card-title"><span className={`severity ${finding.severity}`}>{finding.severity}</span><strong>{finding.title}</strong></div>
}

function SecurityMetric({ label, value, tone = '' }: { label: string; value: string; tone?: string }) {
  return <div className={tone}><span>{label}</span><strong>{value}</strong></div>
}
