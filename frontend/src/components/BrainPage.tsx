import { useEffect, useMemo, useState } from 'react'
import {
  ClearLedgerEvents,
  ListLedgerEvents,
  ListLearningCandidates,
  PruneLedgerEvents,
  RecordLearningDecision,
  SaveMemoryEntry,
  SaveSkill,
  type LedgerEvent,
  type LearningCandidate,
  type MemoryEntry,
  type Skill,
} from '../wailsjs/go'
import { BrainRepositoryIndex } from './BrainRepositoryIndex'
import './BrainPage.css'

type KindFilter = 'all' | 'problems' | 'model' | 'tools' | 'memory' | 'subagents'

interface ReplayRun {
  id: string
  events: LedgerEvent[]
  started: string
  ended: string
  states: string[]
  toolCount: number
  problemCount: number
  contextCount: number
  modelRetryCount: number
  title: string
}

interface TuningSummary {
  modelCalls: LedgerEvent[]
  promptBudgets: LedgerEvent[]
  avgTtftMs: number | null
  avgTokensPerSecond: number | null
  promptWarnings: number
  uniquePromptHashes: number
  uniqueToolSchemaHashes: number
  latestModelCall?: LedgerEvent
  latestPromptBudget?: LedgerEvent
}

export function BrainPage({ version }: { version: number }) {
  const [events, setEvents] = useState<LedgerEvent[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [query, setQuery] = useState('')
  const [kindFilter, setKindFilter] = useState<KindFilter>('all')
  const [sourceFilter, setSourceFilter] = useState('all')
  const [actionStatus, setActionStatus] = useState('')
  const [limit, setLimit] = useState(1000)
  const [candidates, setCandidates] = useState<LearningCandidate[]>([])
  const [approvedCandidates, setApprovedCandidates] = useState<Set<string>>(() => new Set())

  const load = async (nextLimit = limit) => {
    const [loadedEvents, loadedLearning] = await Promise.all([
      ListLedgerEvents(nextLimit).catch(() => [] as LedgerEvent[]),
      ListLearningCandidates(nextLimit).catch(() => [] as LearningCandidate[]),
    ])
    const next = Array.isArray(loadedEvents) ? loadedEvents : []
    const learning = Array.isArray(loadedLearning) ? loadedLearning : []
    setEvents(next)
    setCandidates(learning)
    setSelectedId(prev => prev && next.some(event => event.id === prev) ? prev : next[0]?.id ?? '')
  }

  useEffect(() => {
    void load()
  }, [version])

  const sources = useMemo(() => {
    const set = new Set(events.map(event => event.source || 'unknown'))
    return ['all', ...Array.from(set).sort()]
  }, [events])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return events.filter(event => {
      if (sourceFilter !== 'all' && (event.source || 'unknown') !== sourceFilter) return false
      if (!matchesKindFilter(event, kindFilter)) return false
      if (!q) return true
      return eventSearchText(event).includes(q)
    })
  }, [events, kindFilter, query, sourceFilter])

  const selected = filtered.find(event => event.id === selectedId) ?? filtered[0]
  const stats = useMemo(() => buildStats(events), [events])
  const signals = useMemo(() => buildSignals(events), [events])
  const replayRuns = useMemo(() => buildReplayRuns(events), [events])
  const tuning = useMemo(() => buildTuningSummary(events), [events])

  useEffect(() => {
    if (filtered.length > 0 && (!selectedId || !filtered.some(event => event.id === selectedId))) {
      setSelectedId(filtered[0].id)
    }
  }, [filtered, selectedId])

  const showStatus = (message: string) => {
    setActionStatus(message)
    window.setTimeout(() => setActionStatus(current => current === message ? '' : current), 2200)
  }

  const refresh = async () => {
    await load()
    showStatus('Refreshed')
  }

  const exportLedger = async () => {
    await navigator.clipboard.writeText(JSON.stringify(events, null, 2))
    showStatus('Ledger copied')
  }

  const clearLedger = async () => {
    if (!confirm('Clear the RunLedger event stream? Task logs and memory are not deleted.')) return
    await ClearLedgerEvents()
    setEvents([])
    setSelectedId('')
    showStatus('Ledger cleared')
  }

  const pruneLedger = async (scope: string, label: string, ids: string[] = []) => {
    const count = scope === 'filtered' || scope === 'ids' ? ids.length : events.filter(event => eventMatchesPruneScope(event, scope)).length
    if (count <= 0) {
      showStatus(`No ${label.toLowerCase()} to clear`)
      return
    }
    if (!confirm(`Clear ${count} ${label.toLowerCase()} from Brain? This prunes RunLedger events only; task logs and saved memory are not deleted.`)) return
    const removed = await PruneLedgerEvents(scope, ids)
    await load()
    showStatus(`${removed} cleared`)
  }

  const approveCandidate = async (candidate: LearningCandidate) => {
    if (candidate.type === 'skill') {
      await SaveSkill(candidateToSkill(candidate))
      showStatus('Skill saved')
    } else {
      await SaveMemoryEntry(candidateToMemory(candidate))
      showStatus('Memory saved')
    }
    await RecordLearningDecision(candidate, 'approved', candidate.type === 'skill' ? 'Saved as skill from Brain review' : 'Saved as memory from Brain review')
    setApprovedCandidates(prev => {
      const next = new Set(prev)
      next.add(candidate.id)
      return next
    })
    setCandidates(prev => prev.filter(item => item.id !== candidate.id))
  }

  const rejectCandidate = async (candidate: LearningCandidate) => {
    await RecordLearningDecision(candidate, 'rejected', 'Dismissed from Brain review')
    setCandidates(prev => prev.filter(item => item.id !== candidate.id))
    showStatus('Candidate dismissed')
  }

  const deferCandidate = async (candidate: LearningCandidate) => {
    await RecordLearningDecision(candidate, 'deferred', 'Deferred from Brain review')
    setCandidates(prev => prev.filter(item => item.id !== candidate.id))
    showStatus('Candidate deferred')
  }

  const changeLimit = (next: number) => {
    setLimit(next)
    void load(next)
  }

  return (
    <div className="brain-page">
      <header className="brain-header">
        <div>
          <h1>Brain</h1>
          <p>Canonical RunLedger stream for agent state, tools, model loading, memory, skills, subagents, and Ops signals.</p>
        </div>
        <div className="brain-actions">
          <select value={limit} onChange={e => changeLimit(Number(e.target.value))} title="Event limit">
            <option value={250}>250</option>
            <option value={1000}>1k</option>
            <option value={3000}>3k</option>
          </select>
          <button onClick={() => void refresh()}>Refresh</button>
          <button onClick={() => void exportLedger()}>Copy JSON</button>
          <button className="danger subtle" onClick={() => void pruneLedger('problems', 'problem events')}>Clear Problems</button>
          <button className="danger subtle" onClick={() => void pruneLedger('model_errors', 'model errors')}>Clear Model Errors</button>
          <button className="danger subtle" disabled={filtered.length === 0} onClick={() => void pruneLedger('filtered', 'filtered events', filtered.map(event => event.id))}>Clear Filtered</button>
          <button className="danger" onClick={() => void clearLedger()}>Clear Ledger</button>
          {actionStatus && <span className="brain-action-status">{actionStatus}</span>}
        </div>
      </header>

      <BrainRepositoryIndex version={version} />

      <section className="brain-kpis">
        <Metric label="Events" value={events.length.toLocaleString()} />
        <Metric label="Runs" value={stats.runs.toLocaleString()} />
        <Metric label="Problems" value={stats.problems.toLocaleString()} tone={stats.problems > 0 ? 'bad' : 'ok'} />
        <Metric label="Model Calls" value={stats.modelCalls.toLocaleString()} />
        <Metric label="Avg TTFT" value={tuning.avgTtftMs == null ? '-' : `${Math.round(tuning.avgTtftMs)}ms`} />
        <Metric label="Prompt Warns" value={tuning.promptWarnings.toLocaleString()} tone={tuning.promptWarnings > 0 ? 'bad' : 'ok'} />
        <Metric label="Model Loads" value={stats.modelLoads.toLocaleString()} />
        <Metric label="Tool Events" value={stats.toolEvents.toLocaleString()} />
      </section>

      <div className="brain-filters">
        <input value={query} onChange={e => setQuery(e.target.value)} placeholder="Search event text, tool names, errors, metadata..." />
        <select value={kindFilter} onChange={e => setKindFilter(e.target.value as KindFilter)}>
          <option value="all">All kinds</option>
          <option value="problems">Problems</option>
          <option value="model">Model/provider</option>
          <option value="tools">Tools/research</option>
          <option value="memory">Memory/skills</option>
          <option value="subagents">Subagents</option>
        </select>
        <select value={sourceFilter} onChange={e => setSourceFilter(e.target.value)}>
          {sources.map(source => <option key={source} value={source}>{source === 'all' ? 'All sources' : source}</option>)}
        </select>
      </div>

      <div className="brain-layout">
        <aside className="brain-list">
          {filtered.length === 0 ? (
            <div className="brain-empty">{events.length === 0 ? 'No ledger events yet' : 'No matching events'}</div>
          ) : filtered.map(event => (
            <button
              key={event.id}
              className={`brain-event-card ${selected?.id === event.id ? 'active' : ''}`}
              onClick={() => setSelectedId(event.id)}
            >
              <div className="brain-event-topline">
                <span className={`brain-pill ${toneForEvent(event)}`}>{event.kind}</span>
                {event.status && <span className={`brain-pill subtle ${toneForStatus(event.status)}`}>{event.status}</span>}
                <time>{formatTime(event.timestamp)}</time>
              </div>
              <div className="brain-event-title">{event.tool || event.message || event.source || event.kind}</div>
              <div className="brain-event-detail">{eventListSummary(event)}</div>
            </button>
          ))}
        </aside>

        <main className="brain-detail">
          <section className="brain-section">
            <div className="brain-section-header">
              <h2>Problem Signals</h2>
              <span>{signals.length} active</span>
            </div>
            {signals.length === 0 ? (
              <div className="brain-empty inline">No problem signals in the loaded ledger window.</div>
            ) : (
              <>
              <div className="brain-section-note">Warnings, stops, model errors, and failed tool calls pulled from the ledger so tuning problems are easy to spot.</div>
              <div className="brain-signal-grid">
                {signals.slice(0, 8).map(signal => (
                  <div key={signal.id} className={`brain-signal ${signal.tone}`}>
                    <div>{signal.title}</div>
                    <p>{signal.detail}</p>
                  </div>
                ))}
              </div>
              </>
            )}
          </section>

          <section className="brain-section">
            <div className="brain-section-header">
              <h2>Learned This Run</h2>
              <span>{candidates.length} candidates</span>
            </div>
            {candidates.length === 0 ? (
              <div className="brain-empty inline">No learning candidates in the loaded ledger window.</div>
            ) : (
              <div className="brain-candidates">
                {candidates.slice(0, 10).map(candidate => (
                  <CandidateCard
                    key={candidate.id}
                    candidate={candidate}
                    approved={approvedCandidates.has(candidate.id)}
                    onApprove={() => void approveCandidate(candidate)}
                    onReject={() => void rejectCandidate(candidate)}
                    onDefer={() => void deferCandidate(candidate)}
                  />
                ))}
              </div>
            )}
          </section>

          <section className="brain-section">
            <div className="brain-section-header">
              <h2>Inference Tuning</h2>
              <span>{tuning.modelCalls.length} calls</span>
            </div>
            <div className="brain-tuning-grid">
              <Metric label="Avg TTFT" value={tuning.avgTtftMs == null ? '-' : `${Math.round(tuning.avgTtftMs)}ms`} />
              <Metric label="Avg tok/s" value={tuning.avgTokensPerSecond == null ? '-' : tuning.avgTokensPerSecond.toFixed(1)} />
              <Metric label="Prompt hashes" value={tuning.uniquePromptHashes.toLocaleString()} />
              <Metric label="Tool schema hashes" value={tuning.uniqueToolSchemaHashes.toLocaleString()} />
              <Metric label="Budget warnings" value={tuning.promptWarnings.toLocaleString()} tone={tuning.promptWarnings > 0 ? 'bad' : 'ok'} />
            </div>
            <div className="brain-tuning-cards">
              {tuning.latestModelCall ? <TelemetryCard event={tuning.latestModelCall} /> : <div className="brain-empty inline">No model-call telemetry yet.</div>}
              {tuning.latestPromptBudget ? <TelemetryCard event={tuning.latestPromptBudget} /> : null}
            </div>
          </section>

          <section className="brain-section">
            <div className="brain-section-header">
              <h2>Trajectory Replay</h2>
              <span>{replayRuns.length} runs</span>
            </div>
            {replayRuns.length === 0 ? (
              <div className="brain-empty inline">No run-linked ledger events in the loaded window.</div>
            ) : (
              <div className="brain-replay-list">
                {replayRuns.slice(0, 8).map(run => (
                  <ReplayRunCard key={run.id} run={run} onSelect={event => setSelectedId(event.id)} />
                ))}
              </div>
            )}
          </section>

          <section className="brain-section">
            <div className="brain-section-header">
              <h2>Selected Event</h2>
              {selected && <button onClick={() => void navigator.clipboard.writeText(JSON.stringify(selected, null, 2))}>Copy Event</button>}
            </div>
            {!selected ? (
              <div className="brain-empty inline">Select an event to inspect it.</div>
            ) : (
              <EventDetail event={selected} />
            )}
          </section>

          <section className="brain-section">
            <div className="brain-section-header">
              <h2>Distribution</h2>
              <span>{Object.keys(stats.byKind).length} kinds</span>
            </div>
            <div className="brain-bars">
              {Object.entries(stats.byKind).slice(0, 14).map(([kind, count]) => (
                <div className="brain-bar-row" key={kind}>
                  <span>{kind}</span>
                  <div><i style={{ width: `${Math.max(4, (count / Math.max(1, stats.maxKindCount)) * 100)}%` }} /></div>
                  <strong>{count}</strong>
                </div>
              ))}
            </div>
          </section>
        </main>
      </div>
    </div>
  )
}

function ReplayRunCard({ run, onSelect }: { run: ReplayRun; onSelect: (event: LedgerEvent) => void }) {
  const visibleEvents = run.events.slice().reverse()
  return (
    <details className={`brain-replay-run ${run.problemCount > 0 ? 'has-problems' : ''}`}>
      <summary>
        <span className="brain-replay-title">{run.title}</span>
        <span>{run.events.length} events</span>
        <span>{run.toolCount} tools</span>
        {run.problemCount > 0 && <span className="bad">{run.problemCount} problems</span>}
        {run.contextCount > 0 && <span>{run.contextCount} context</span>}
        {run.modelRetryCount > 0 && <span>{run.modelRetryCount} retries</span>}
        <time>{formatTime(run.ended || run.started)}</time>
      </summary>
      {run.states.length > 0 && (
        <div className="brain-replay-states">
          {run.states.map(state => <span key={`${run.id}-${state}`}>{state}</span>)}
        </div>
      )}
      <div className="brain-replay-events">
        {visibleEvents.map(event => (
          <button key={event.id} onClick={() => onSelect(event)}>
            <span className={`brain-pill ${toneForEvent(event)}`}>{event.kind}</span>
            <strong>{event.tool || event.state || event.status || event.source || event.kind}</strong>
            <em>{event.error || event.message || event.detail || event.output || '-'}</em>
          </button>
        ))}
      </div>
    </details>
  )
}

function CandidateCard({
  candidate,
  approved,
  onApprove,
  onReject,
  onDefer,
}: {
  candidate: LearningCandidate
  approved: boolean
  onApprove: () => void
  onReject: () => void
  onDefer: () => void
}) {
  return (
    <article className={`brain-candidate ${candidate.type}`}>
      <div className="brain-candidate-head">
        <span className="brain-pill pending">{candidate.type}</span>
        <strong>{candidate.title}</strong>
        <button disabled={approved} onClick={onApprove}>{approved ? 'Saved' : candidate.type === 'skill' ? 'Save Skill' : 'Save Memory'}</button>
        <button disabled={approved} onClick={onDefer}>Defer</button>
        <button disabled={approved} onClick={onReject}>Dismiss</button>
      </div>
      <p>{candidate.reason}</p>
      <pre>{candidate.content}</pre>
      {candidate.evidence?.length ? (
        <details>
          <summary>Evidence</summary>
          <ul>
            {candidate.evidence.map((item, index) => <li key={`${candidate.id}-ev-${index}`}>{item}</li>)}
          </ul>
        </details>
      ) : null}
    </article>
  )
}

function EventDetail({ event }: { event: LedgerEvent }) {
  return (
    <div className="brain-event-detail-panel">
      {(event.kind === 'model_call' || event.kind === 'prompt_budget') && <TelemetryCard event={event} expanded />}
      <div className="brain-detail-grid">
        <Field label="ID" value={event.id} />
        <Field label="Run" value={event.run_id || '-'} />
        <Field label="Kind" value={event.kind} />
        <Field label="Source" value={event.source || '-'} />
        <Field label="Tool" value={event.tool || '-'} />
        <Field label="Status" value={event.status || '-'} />
        <Field label="State" value={event.state || '-'} />
        <Field label="Time" value={formatDate(event.timestamp)} />
        <Field label="Duration" value={event.duration_ms != null ? `${event.duration_ms}ms` : '-'} />
      </div>
      {event.message && <TextBlock title="Message" value={event.message} />}
      {event.detail && <TextBlock title="Detail" value={event.detail} />}
      {event.error && <TextBlock title="Error" value={event.error} tone="bad" />}
      {event.input && <TextBlock title="Input" value={event.input} />}
      {event.output && <TextBlock title="Output" value={event.output} />}
      {event.files?.length ? <TextBlock title="Files" value={event.files.join('\n')} /> : null}
      {event.artifacts?.length ? <TextBlock title="Artifacts" value={event.artifacts.join('\n')} /> : null}
      {event.metadata && Object.keys(event.metadata).length > 0 && (
        <TextBlock title="Metadata" value={JSON.stringify(event.metadata, null, 2)} />
      )}
    </div>
  )
}

function TelemetryCard({ event, expanded = false }: { event: LedgerEvent; expanded?: boolean }) {
  const data = eventTelemetry(event)
  if (event.kind === 'model_call') {
    return (
      <div className="brain-telemetry-card">
        <div className="brain-telemetry-head">
          <strong>Model Call</strong>
          <span>{data.status || event.status || 'ok'}</span>
        </div>
        <div className="brain-telemetry-grid">
          <Field label="TTFT" value={fmtMaybeMs(data.ttft_ms)} />
          <Field label="Duration" value={fmtMaybeMs(data.duration_ms)} />
          <Field label="Tok/s" value={fmtMaybeFloat(data.tokens_per_second)} />
          <Field label="Tool Choice" value={data.tool_choice || '-'} />
          <Field label="Tools" value={data.tool_count || '0'} />
          <Field label="Model Load" value={data.model_load || '-'} />
          <Field label="Prompt Hash" value={data.prompt_hash || '-'} />
          <Field label="Tool Hash" value={data.tool_schema_hash || '-'} />
        </div>
        {expanded && <TextBlock title="Selected Tools" value={splitCSV(data.selected_tools).join('\n') || '-'} />}
      </div>
    )
  }
  const promptWarn = data.over_20_pct === 'true' || data.over_system_target === 'true' || data.over_tool_target === 'true' || data.over_tool_schema_target === 'true' || event.status === 'warn'
  return (
    <div className={`brain-telemetry-card ${promptWarn ? 'warn' : ''}`}>
      <div className="brain-telemetry-head">
        <strong>Prompt Budget</strong>
        <span>{promptWarn ? 'over target' : 'ok'}</span>
      </div>
      <div className="brain-telemetry-grid">
        <Field label="Estimated" value={fmtInt(data.estimated_tokens)} />
        <Field label="System" value={targetValue(data.system_tokens, data.system_target || data.system_target_tokens)} />
        <Field label="Tools" value={targetValue(data.tool_schema_tokens, data.tool_schema_target || data.tool_schema_target_tokens)} />
        <Field label="Conversation" value={fmtInt(data.conversation_tokens)} />
        <Field label="System+Tools" value={fmtPct(data.system_pct)} />
        <Field label="Context" value={fmtInt(data.context_window)} />
        <Field label="Prompt Hash" value={data.prompt_hash || '-'} />
        <Field label="Tool Hash" value={data.tool_schema_hash || '-'} />
      </div>
      {expanded && <TextBlock title="Selected Tools" value={splitCSV(data.selected_tools).join('\n') || '-'} />}
    </div>
  )
}

function candidateToMemory(candidate: LearningCandidate): MemoryEntry {
  const now = new Date().toISOString()
  return {
    id: '',
    scope: '',
    title: candidate.title,
    content: [
      candidate.content,
      candidate.evidence?.length ? `\nEvidence:\n${candidate.evidence.map(item => `- ${item}`).join('\n')}` : '',
    ].join('').trim(),
    tags: candidate.tags ?? [],
    kind: candidate.kind || 'note',
    confidence: candidate.type === 'evidence' ? 'likely' : 'hypothesis',
    source: 'auto_distill',
    importance: candidate.importance || 3,
    pinned: false,
    created_at: now,
    updated_at: now,
    last_used_at: '',
  }
}

function candidateToSkill(candidate: LearningCandidate): Skill {
  return {
    name: slugify(candidate.title.replace(/^Save\s+/i, '').replace(/\?$/, '')),
    description: candidate.reason || candidate.title,
    version: '1.0.0',
    tags: candidate.tags?.length ? candidate.tags : ['suggested'],
    source_path: '',
    required_tools: [],
    shell_backend: '',
    needs_network: false,
    needs_write: false,
    body: candidate.template || candidate.content,
    raw: '',
    created_at: '',
    updated_at: '',
  }
}

function slugify(text: string) {
  const slug = text.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
  return slug.slice(0, 64) || 'learned-skill'
}

function Metric({ label, value, tone = 'neutral' }: { label: string; value: string; tone?: 'neutral' | 'ok' | 'bad' }) {
  return (
    <div className={`brain-metric ${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="brain-field">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function targetValue(value?: string, target?: string) {
  const base = fmtInt(value)
  const cap = fmtInt(target)
  return target ? `${base} / ${cap}` : base
}

function TextBlock({ title, value, tone }: { title: string; value: string; tone?: 'bad' }) {
  return (
    <details className={`brain-text-block ${tone || ''}`} open={['Error', 'Message', 'Detail'].includes(title)}>
      <summary>{title}</summary>
      <pre>{value}</pre>
    </details>
  )
}

function buildStats(events: LedgerEvent[]) {
  const byKind: Record<string, number> = {}
  const runIds = new Set<string>()
  let problems = 0
  let modelLoads = 0
  let modelCalls = 0
  let toolEvents = 0
  let memoryWrites = 0
  for (const event of events) {
    byKind[event.kind] = (byKind[event.kind] || 0) + 1
    if (event.run_id) runIds.add(event.run_id)
    if (isProblem(event)) problems++
    if (event.kind === 'model_load' || event.kind.startsWith('provider_')) modelLoads++
    if (event.kind === 'model_call') modelCalls++
    if (event.source === 'tool' || event.kind === 'tool_result') toolEvents++
    if (event.kind === 'memory_write') memoryWrites++
  }
  const sortedByKind = Object.fromEntries(Object.entries(byKind).sort((a, b) => b[1] - a[1]))
  return {
    byKind: sortedByKind,
    maxKindCount: Math.max(1, ...Object.values(byKind)),
    modelLoads,
    modelCalls,
    memoryWrites,
    problems,
    runs: runIds.size,
    toolEvents,
  }
}

function buildTuningSummary(events: LedgerEvent[]): TuningSummary {
  const modelCalls = events.filter(event => event.kind === 'model_call')
  const promptBudgets = events.filter(event => event.kind === 'prompt_budget')
  const ttfts = modelCalls.map(event => num(eventTelemetry(event).ttft_ms)).filter(isFiniteNumber)
  const tps = modelCalls.map(event => num(eventTelemetry(event).tokens_per_second)).filter(value => isFiniteNumber(value) && value > 0)
  const promptHashes = new Set(modelCalls.map(event => eventTelemetry(event).prompt_hash).filter(Boolean))
  const toolHashes = new Set(modelCalls.map(event => eventTelemetry(event).tool_schema_hash).filter(Boolean))
  return {
    modelCalls,
    promptBudgets,
    avgTtftMs: average(ttfts),
    avgTokensPerSecond: average(tps),
    promptWarnings: promptBudgets.filter(event => event.status === 'warn' || eventTelemetry(event).over_20_pct === 'true').length,
    uniquePromptHashes: promptHashes.size,
    uniqueToolSchemaHashes: toolHashes.size,
    latestModelCall: modelCalls[0],
    latestPromptBudget: promptBudgets[0],
  }
}

function buildSignals(events: LedgerEvent[]) {
  return events
    .filter(event => isProblem(event) || event.kind === 'model_load' && ['retry', 'error', 'cancelled'].includes(event.status || ''))
    .slice(0, 20)
    .map(event => ({
      id: event.id,
      title: `${event.kind}${event.status ? ` / ${event.status}` : ''}`,
      detail: event.error || event.detail || event.message || event.tool || 'Problem event recorded.',
      tone: isProblem(event) ? 'bad' : 'warn',
    }))
}

function buildReplayRuns(events: LedgerEvent[]): ReplayRun[] {
  const byRun = new Map<string, LedgerEvent[]>()
  for (const event of events) {
    if (!event.run_id) continue
    const list = byRun.get(event.run_id) ?? []
    list.push(event)
    byRun.set(event.run_id, list)
  }
  return Array.from(byRun.entries()).map(([id, runEvents]) => {
    const sorted = runEvents.slice().sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
    const states = Array.from(new Set(sorted.map(event => event.state).filter(Boolean) as string[]))
    const titleEvent = sorted.find(event => event.kind === 'run_start' || event.message)
    return {
      id,
      events: sorted.slice().reverse(),
      started: sorted[0]?.timestamp ?? '',
      ended: sorted[sorted.length - 1]?.timestamp ?? '',
      states,
      toolCount: sorted.filter(event => event.source === 'tool' || event.tool || event.kind === 'tool_result').length,
      problemCount: sorted.filter(isProblem).length,
      contextCount: sorted.filter(event => ['context_clear', 'compaction', 'persist_nudge', 'memory_reinject', 'memory_distill'].includes(event.kind)).length,
      modelRetryCount: sorted.filter(event => event.kind === 'model_load' && ['retry', 'error'].includes(event.status || '') || event.kind === 'inference_retry').length,
      title: titleEvent?.message || id,
    }
  }).sort((a, b) => new Date(b.ended || b.started).getTime() - new Date(a.ended || a.started).getTime())
}

function matchesKindFilter(event: LedgerEvent, filter: KindFilter) {
  switch (filter) {
    case 'problems':
      return isProblem(event)
    case 'model':
      return event.kind === 'model_load' || event.kind === 'model_call' || event.kind === 'prompt_budget' || event.kind.startsWith('provider_')
    case 'tools':
      return event.source === 'tool' || ['web_research', 'browser_action', 'planner_event', 'tool_result'].includes(event.kind)
    case 'memory':
      return event.kind.startsWith('memory_') || event.kind.startsWith('skill_') || event.kind === 'learning_suggestion'
    case 'subagents':
      return event.kind.startsWith('subagent_')
    default:
      return true
  }
}

function eventListSummary(event: LedgerEvent) {
  if (event.kind === 'model_call') {
    const data = eventTelemetry(event)
    return `TTFT ${fmtMaybeMs(data.ttft_ms)} / ${fmtMaybeFloat(data.tokens_per_second)} tok/s / tools ${data.tool_count || '0'} / ${data.model_load || '-'}`
  }
  if (event.kind === 'prompt_budget') {
    const data = eventTelemetry(event)
    return `${fmtInt(data.estimated_tokens)} est tok / system+tools ${fmtPct(data.system_pct)} / tools ${fmtInt(data.tool_schema_tokens)}`
  }
  return event.error || event.detail || event.output || event.input || '-'
}

function eventTelemetry(event: LedgerEvent): Record<string, string> {
  return { ...parseKV(event.detail || ''), ...(event.metadata || {}) }
}

function parseKV(detail: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of detail.split(/\r?\n/)) {
    const idx = line.indexOf('=')
    if (idx <= 0) continue
    out[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
  }
  return out
}

function num(value?: string) {
  if (!value) return NaN
  const n = Number(value)
  return Number.isFinite(n) ? n : NaN
}

function isFiniteNumber(value: number): value is number {
  return Number.isFinite(value)
}

function average(values: number[]) {
  if (values.length === 0) return null
  return values.reduce((sum, value) => sum + value, 0) / values.length
}

function fmtMaybeMs(value?: string) {
  const n = num(value)
  return Number.isFinite(n) ? `${Math.round(n)}ms` : '-'
}

function fmtMaybeFloat(value?: string) {
  const n = num(value)
  return Number.isFinite(n) ? n.toFixed(1) : '-'
}

function fmtInt(value?: string) {
  const n = num(value)
  return Number.isFinite(n) ? Math.round(n).toLocaleString() : '-'
}

function fmtPct(value?: string) {
  const n = num(value)
  return Number.isFinite(n) ? `${Math.round(n * 100)}%` : '-'
}

function splitCSV(value?: string) {
  return (value || '').split(',').map(item => item.trim()).filter(Boolean)
}

function eventSearchText(event: LedgerEvent) {
  return [
    event.id,
    event.run_id || '',
    event.kind,
    event.source || '',
    event.tool || '',
    event.status || '',
    event.state || '',
    event.message || '',
    event.detail || '',
    event.input || '',
    event.output || '',
    event.error || '',
    JSON.stringify(event.metadata || {}),
  ].join(' ').toLowerCase()
}

function isProblem(event: LedgerEvent) {
  const status = (event.status || '').toLowerCase()
  const kind = event.kind.toLowerCase()
  return Boolean(event.error) ||
    ['error', 'failed', 'cancelled', 'blocked', 'denied', 'unreachable', 'exhausted'].includes(status) ||
    ['run_stop', 'tool_error'].includes(kind) ||
    kind.includes('error')
}

function eventMatchesPruneScope(event: LedgerEvent, scope: string) {
  switch (scope) {
    case 'problems':
    case 'errors':
      return isProblem(event)
    case 'tool_errors':
      return isProblem(event) && (event.source === 'tool' || event.kind === 'tool_error' || event.kind === 'tool_result' || Boolean(event.tool))
    case 'model_errors':
      return isProblem(event) && (event.kind === 'model_load' || event.kind.startsWith('provider_') || event.source === 'model')
    case 'learning':
      return event.kind === 'learning_suggestion'
    default:
      return false
  }
}

function toneForEvent(event: LedgerEvent) {
  if (isProblem(event)) return 'bad'
  if (['retry', 'attempt', 'pending', 'running'].includes((event.status || '').toLowerCase())) return 'pending'
  if (['done', 'ok', 'saved', 'created', 'updated', 'cleared'].includes((event.status || '').toLowerCase())) return 'ok'
  return 'muted'
}

function toneForStatus(status: string) {
  const lower = status.toLowerCase()
  if (['error', 'failed', 'cancelled', 'blocked', 'denied', 'unreachable', 'exhausted'].includes(lower)) return 'bad'
  if (['retry', 'attempt', 'pending', 'running', 'requested'].includes(lower)) return 'pending'
  if (['done', 'ok', 'saved', 'created', 'updated', 'allowed'].includes(lower)) return 'ok'
  return 'muted'
}

function formatTime(ts: string) {
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleTimeString()
}

function formatDate(ts: string) {
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString()
}
