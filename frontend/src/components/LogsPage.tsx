import { useEffect, useMemo, useState } from 'react'
import {
  ClearTaskRuns,
  ListTaskRuns,
  type TaskRun,
} from '../wailsjs/go'
import './LogsPage.css'

type StatusFilter = 'all' | 'problem' | 'error' | 'stopped' | 'done'

export function LogsPage({ version }: { version: number }) {
  const [runs, setRuns] = useState<TaskRun[]>([])
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [selectedId, setSelectedId] = useState('')
  const [actionStatus, setActionStatus] = useState('')

  const load = async () => {
    const next = await ListTaskRuns().catch(() => [] as TaskRun[])
    setRuns(next)
    setSelectedId(prev => prev && next.some(run => run.id === prev) ? prev : next[0]?.id ?? '')
  }

  useEffect(() => {
    void load()
  }, [version])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return runs.filter(run => {
      if (status === 'problem' && !['error', 'stopped'].includes(run.status) && !run.stop_reason) return false
      if (status === 'error' && run.status !== 'error') return false
      if (status === 'stopped' && run.status !== 'stopped') return false
      if (status === 'done' && run.status !== 'done') return false
      if (!q) return true
      return [
        run.prompt,
        run.mode,
        run.profile,
        run.model ?? '',
        run.status,
        run.state ?? '',
        run.stop_reason ?? '',
        run.stop_detail ?? '',
        run.summary ?? '',
        run.response ?? '',
        ...(run.events ?? []).flatMap(event => [event.kind, event.message, event.detail ?? '']),
        ...(run.tools ?? []).flatMap(tool => [tool.name, tool.status, tool.input ?? '', tool.result ?? '']),
      ].join(' ').toLowerCase().includes(q)
    })
  }, [query, runs, status])

  const selected = filtered.find(run => run.id === selectedId) ?? filtered[0]

  const showActionStatus = (message: string) => {
    setActionStatus(message)
    window.setTimeout(() => setActionStatus(current => current === message ? '' : current), 2200)
  }

  const refresh = async () => {
    await load()
    showActionStatus('Refreshed')
  }

  const exportLogs = async () => {
    await navigator.clipboard.writeText(JSON.stringify(runs, null, 2))
    showActionStatus('Logs copied')
  }

  const clearLogs = async () => {
    if (!confirm('Clear all task logs?')) return
    await ClearTaskRuns()
    setRuns([])
    setSelectedId('')
    showActionStatus('Logs cleared')
  }

  useEffect(() => {
    if (filtered.length > 0 && (!selectedId || !filtered.some(run => run.id === selectedId))) {
      setSelectedId(filtered[0].id)
    }
  }, [filtered, selectedId])

  return (
    <div className="logs-page">
      <header className="logs-header">
        <div>
          <h1>Logs</h1>
          <p>Full run history, prompts, responses, timeline events, and tool I/O.</p>
        </div>
        <div className="logs-actions">
          <button onClick={() => void refresh()}>Refresh</button>
          <button onClick={() => void exportLogs()}>Export JSON</button>
          <button className="danger" onClick={() => void clearLogs()}>Clear Logs</button>
          {actionStatus && <span className="logs-action-status">{actionStatus}</span>}
        </div>
      </header>

      <div className="logs-filters">
        <input
          value={query}
          onChange={e => setQuery(e.target.value)}
          placeholder="Search prompts, responses, tools, errors..."
        />
        <select value={status} onChange={e => setStatus(e.target.value as StatusFilter)}>
          <option value="all">All runs</option>
          <option value="problem">Problems</option>
          <option value="error">Errors</option>
          <option value="stopped">Stopped</option>
          <option value="done">Done</option>
        </select>
      </div>

      <div className="logs-layout">
        <aside className="logs-list">
          {filtered.length === 0 ? (
            <div className="logs-empty">{runs.length === 0 ? 'No logs yet' : 'No matching logs'}</div>
          ) : filtered.map(run => (
            <button
              key={run.id}
              className={`logs-run-card ${selected?.id === run.id ? 'active' : ''}`}
              onClick={() => setSelectedId(run.id)}
            >
              <div className="logs-run-line">
                <span className={`logs-status ${statusClass(run.status)}`}>{run.status}</span>
                {run.state && <span className="logs-state">{run.state}</span>}
                <span className="logs-time">{new Date(run.started_at).toLocaleString()}</span>
              </div>
              <div className="logs-run-title">{run.mode} / {run.profile}</div>
              <div className="logs-run-prompt">{run.prompt || '(empty prompt)'}</div>
              <div className="logs-run-meta">
                {run.duration_ms != null && <span>{fmtDuration(run.duration_ms)}</span>}
                {(run.tools ?? []).length > 0 && <span>{(run.tools ?? []).length} tools</span>}
                {compactionCount(run) > 0 && <span>{compactionCount(run)} compact</span>}
                {run.total_tokens != null && run.total_tokens > 0 && <span>{run.total_tokens.toLocaleString()} tok</span>}
                {run.stop_reason && <span>{run.stop_reason}</span>}
              </div>
            </button>
          ))}
        </aside>

        <main className="logs-detail">
          {!selected ? (
            <div className="logs-empty">Select a run to inspect it.</div>
          ) : (
            <RunDetail run={selected} />
          )}
        </main>
      </div>
    </div>
  )
}

function RunDetail({ run }: { run: TaskRun }) {
  return (
    <>
      <section className="logs-detail-hero">
        <div>
          <div className="logs-detail-title">{run.mode} / {run.profile}</div>
          <div className="logs-detail-subtitle">{run.model || 'unknown model'}</div>
        </div>
        <button onClick={() => void navigator.clipboard.writeText(JSON.stringify(run, null, 2))}>Copy Run</button>
      </section>

      <section className="logs-kpis">
        <Metric label="Status" value={run.stop_reason || run.status} />
        <Metric label="State" value={run.state || '-'} />
        <Metric label="Duration" value={run.duration_ms != null ? fmtDuration(run.duration_ms) : '-'} />
        <Metric label="Tokens" value={run.total_tokens != null && run.total_tokens > 0 ? run.total_tokens.toLocaleString() : '-'} />
        <Metric label="Tools" value={`${(run.tools ?? []).length}`} />
        <Metric label="Compactions" value={`${compactionCount(run)}`} />
      </section>

      <LogSection title="Prompt" body={run.prompt} />
      {run.response && <LogSection title="Full Response" body={run.response} />}
      {!run.response && (run.summary || run.stop_detail) && (
        <LogSection title={run.stop_detail ? 'Stop Detail' : 'Summary'} body={run.stop_detail ?? run.summary ?? ''} />
      )}

      {(run.events ?? []).length > 0 && (
        <section className="logs-section">
          <h2>Timeline</h2>
          <div className="logs-timeline">
            {(run.events ?? []).map((event, index) => (
              <details key={`${run.id}-event-${index}`} className="logs-event">
                <summary>
                  <span className={`logs-status ${eventStatusClass(event.kind)}`}>{event.kind}</span>
                  <span>{event.message}</span>
                  <time>{new Date(event.timestamp).toLocaleTimeString()}</time>
                </summary>
                {event.detail && <pre>{event.detail}</pre>}
              </details>
            ))}
          </div>
        </section>
      )}

      {(run.tools ?? []).length > 0 && (
        <section className="logs-section">
          <h2>Tool Calls</h2>
          <div className="logs-tools">
            {(run.tools ?? []).map((tool, index) => (
              <details key={`${run.id}-tool-${index}`} className="logs-tool" open={tool.status !== 'ok' && tool.status !== 'done'}>
                <summary>
                  <span className={`logs-status ${statusClass(tool.status)}`}>{tool.status}</span>
                  <span>{tool.name}</span>
                  {tool.duration_ms != null && tool.duration_ms > 0 && <time>{fmtDuration(tool.duration_ms)}</time>}
                </summary>
                {tool.input && <LogSection title="Input" body={tool.input} compact />}
                {tool.result && <LogSection title="Result" body={tool.result} compact />}
              </details>
            ))}
          </div>
        </section>
      )}
    </>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="logs-metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function LogSection({ title, body, compact = false }: { title: string; body: string; compact?: boolean }) {
  return (
    <section className={`logs-section ${compact ? 'compact' : ''}`}>
      <h2>{title}</h2>
      <pre>{body}</pre>
    </section>
  )
}

function fmtDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`
}

function statusClass(status: string): string {
  if (status === 'done' || status === 'ok') return 'ok'
  if (status === 'running') return 'pending'
  if (status === 'blocked' || status === 'denied' || status === 'stopped') return 'warn'
  if (status === 'error' || status === 'failed') return 'bad'
  return 'muted'
}

function eventStatusClass(kind: string): string {
  if (kind === 'error' || kind === 'failed' || kind === 'tool_error') return 'bad'
  if (kind === 'blocked' || kind === 'denied' || kind === 'continue') return 'warn'
  if (kind === 'state' || kind === 'done' || kind === 'tool_result' || kind === 'compaction') return 'ok'
  return 'muted'
}

function compactionCount(run: TaskRun): number {
  return (run.events ?? []).filter(event => event.kind === 'compaction').length
}
