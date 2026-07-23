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
    const loaded = await ListTaskRuns().catch(() => [] as TaskRun[])
    const next = Array.isArray(loaded) ? loaded : []
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
        run.origin ?? '',
        run.claimant_alias ?? '',
        run.claimant_id ?? '',
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
          <p>Compact run history, prompts, responses, timeline events, and tool I/O.</p>
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
        <aside className="logs-list compact">
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
                <span>{run.origin || 'desktop'}</span>
                {(run.tools ?? []).length > 0 && <span>{(run.tools ?? []).length} tools</span>}
				{run.control?.phase && <span>control {run.control.phase.replaceAll('_', ' ')}</span>}
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
  const telemetry = runTelemetry(run)
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
		<Metric label="Control" value={run.control?.phase?.replaceAll('_', ' ') || '-'} />
        <Metric label="Origin" value={run.origin || 'desktop'} />
        <Metric label="Claimant" value={run.claimant_alias || run.claimant_id || run.id} />
        <Metric label="Duration" value={run.duration_ms != null ? fmtDuration(run.duration_ms) : '-'} />
        <Metric label="Tokens" value={run.total_tokens != null && run.total_tokens > 0 ? run.total_tokens.toLocaleString() : '-'} />
        <Metric label="Avg TTFT" value={telemetry.avgTtftMs == null ? '-' : `${Math.round(telemetry.avgTtftMs)}ms`} />
        <Metric label="Tok/s" value={telemetry.avgTokensPerSecond == null ? '-' : telemetry.avgTokensPerSecond.toFixed(1)} />
      </section>

	  {run.contract && run.control && (
		<section className="logs-section logs-control-plane">
		  <h2>Task Contract</h2>
		  <div className="logs-telemetry-summary">
			<Metric label="Revision" value={`${run.contract.revision}`} />
			<Metric label="Risk" value={run.contract.risk} />
			<Metric label="Plan" value={run.control.plan_required ? (run.control.plan_accepted ? 'accepted' : 'required') : 'not required'} />
			<Metric label="Phase" value={run.control.phase.replaceAll('_', ' ')} />
			<Metric label="Checks" value={`${Object.keys(run.control.satisfied_checks ?? {}).length}/${(run.control.blocking_check_ids ?? []).length}`} />
			<Metric label="Evidence IDs" value={`${Object.values(run.control.satisfied_checks ?? {}).flat().length}`} />
		  </div>
		  <div className="logs-contract-objective">
			<span>Objective</span>
			<strong>{run.contract.objective}</strong>
			<code title={run.contract.digest}>{shortDigest(run.contract.digest)}</code>
		  </div>
		  {(run.contract.acceptance_checks ?? []).length > 0 && (
			<div className="logs-contract-checks">
			  {(run.contract.acceptance_checks ?? []).map(check => {
				const evidence = run.control?.satisfied_checks?.[check.id] ?? []
				return <div key={check.id} className={evidence.length > 0 ? 'pass' : check.blocking ? 'waiting' : ''}>
				  <span>{evidence.length > 0 ? 'verified' : check.blocking ? 'blocking' : 'advisory'}</span>
				  <strong>{check.description}</strong>
				  <small>{check.verifier} - {evidence.length} evidence ID{evidence.length === 1 ? '' : 's'}</small>
				</div>
			  })}
			</div>
		  )}
		</section>
	  )}

      {(telemetry.modelCalls.length > 0 || telemetry.promptBudgets.length > 0 || telemetry.latestLoopMetrics) && (
        <section className="logs-section">
          <h2>Inference Tuning</h2>
          <div className="logs-telemetry-summary">
            <Metric label="Model Calls" value={`${telemetry.modelCalls.length}`} />
            <Metric label="Prompt Warnings" value={`${telemetry.promptWarnings}`} />
            <Metric label="Prompt Hashes" value={`${telemetry.uniquePromptHashes}`} />
            <Metric label="Tool Hashes" value={`${telemetry.uniqueToolSchemaHashes}`} />
            <Metric label="Tools" value={`${(run.tools ?? []).length}`} />
            <Metric label="Compactions" value={`${compactionCount(run)}`} />
          </div>
          <div className="logs-telemetry-cards">
            {telemetry.latestLoopMetrics && <RunTelemetryCard event={telemetry.latestLoopMetrics} />}
            {telemetry.latestModelCall && <RunTelemetryCard event={telemetry.latestModelCall} />}
            {telemetry.latestPromptBudget && <RunTelemetryCard event={telemetry.latestPromptBudget} />}
          </div>
        </section>
      )}

      <LogSection title="Prompt" body={run.prompt} />
      {run.response && <LogSection title="Full Response" body={run.response} />}
      {!run.response && (run.summary || run.stop_detail) && (
        <LogSection title={run.stop_detail ? 'Stop Detail' : 'Summary'} body={run.stop_detail ?? run.summary ?? ''} />
      )}

      {(run.events ?? []).length > 0 && (
        <section className="logs-section">
          <h2>Timeline</h2>
          <div className="logs-timeline compact">
            {(run.events ?? []).map((event, index) => (
              <details key={`${run.id}-event-${index}`} className="logs-event compact">
                <summary>
                  <span className={`logs-status ${eventStatusClass(event.kind)}`}>{event.kind}</span>
                  <span>{timelineSummary(event)}</span>
                  <time>{new Date(event.timestamp).toLocaleTimeString()}</time>
                </summary>
                {(['model_call', 'prompt_budget', 'loop_metrics'].includes(event.kind)) && <RunTelemetryCard event={event} />}
                {event.detail && <pre>{event.detail}</pre>}
              </details>
            ))}
          </div>
        </section>
      )}

      {(run.tools ?? []).length > 0 && (
        <section className="logs-section">
          <h2>Tool Calls</h2>
          <div className="logs-tools compact">
            {(run.tools ?? []).map((tool, index) => (
              <details key={`${run.id}-tool-${index}`} className="logs-tool compact" open={tool.status !== 'ok' && tool.status !== 'done'}>
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

function shortDigest(value: string): string {
  const digest = value.replace(/^sha256:/, '')
  return digest.length > 16 ? `${digest.slice(0, 12)}...${digest.slice(-4)}` : digest
}

function RunTelemetryCard({ event }: { event: { kind: string; message: string; detail?: string } }) {
  const data = parseKV(event.detail || '')
  if (event.kind === 'loop_metrics') {
    const metrics = { ...parseJSONRecord(event.detail || ''), ...data }
    const score = num(metrics.stability_score)
    const tone = Number.isFinite(score) && score < 70 ? 'warn' : ''
    return (
      <div className={`logs-telemetry-card ${tone}`}>
        <div className="logs-telemetry-head">
          <strong>Run Stability</strong>
          <span>{Number.isFinite(score) ? `${Math.round(score)}/100` : 'n/a'}</span>
        </div>
        <div className="logs-telemetry-grid">
          <MiniMetric label="Tools" value={fmtInt(metrics.tool_calls)} />
          <MiniMetric label="Errors" value={fmtInt(metrics.tool_errors)} />
          <MiniMetric label="Repeats" value={fmtInt(metrics.repeated_tool_inputs)} />
          <MiniMetric label="Skipped" value={fmtInt(metrics.repeated_skips)} />
          <MiniMetric label="Verifiers" value={fmtInt(metrics.verifier_prompts)} />
          <MiniMetric label="Max Tools" value={fmtInt(metrics.max_routed_tools)} />
          <MiniMetric label="Prompt Warn" value={fmtInt(metrics.prompt_warnings)} />
          <MiniMetric label="Stop" value={metrics.stop_reason || '-'} />
        </div>
      </div>
    )
  }
  if (event.kind === 'model_call') {
    return (
      <div className="logs-telemetry-card">
        <div className="logs-telemetry-head">
          <strong>Model Call</strong>
          <span>{data.status || 'ok'}</span>
        </div>
        <div className="logs-telemetry-grid">
          <MiniMetric label="TTFT" value={fmtMaybeMs(data.ttft_ms)} />
          <MiniMetric label="Duration" value={fmtMaybeMs(data.duration_ms)} />
          <MiniMetric label="Tok/s" value={fmtMaybeFloat(data.tokens_per_second)} />
          <MiniMetric label="Tool Choice" value={data.tool_choice || '-'} />
          <MiniMetric label="Tools" value={data.tools || data.tool_count || '0'} />
          <MiniMetric label="Model Load" value={data.model_load || '-'} />
          <MiniMetric label="Prompt Hash" value={data.prompt_hash || '-'} />
          <MiniMetric label="Tool Hash" value={data.tool_schema_hash || '-'} />
        </div>
      </div>
    )
  }
  const promptWarn = data.over_20_pct === 'true' || data.over_system_target === 'true' || data.over_tool_target === 'true' || data.over_tool_schema_target === 'true' || event.message.toLowerCase().includes('over target') || event.message.toLowerCase().includes('exceeds')
  return (
    <div className={`logs-telemetry-card ${promptWarn ? 'warn' : ''}`}>
      <div className="logs-telemetry-head">
        <strong>Prompt Budget</strong>
        <span>{promptWarn ? 'over target' : 'ok'}</span>
      </div>
      <div className="logs-telemetry-grid">
        <MiniMetric label="Estimated" value={fmtInt(data.estimated_total_tokens || data.estimated_tokens)} />
        <MiniMetric label="System" value={targetValue(data.system_tokens, data.system_target || data.system_target_tokens)} />
        <MiniMetric label="Tools" value={targetValue(data.tool_schema_tokens, data.tool_schema_target || data.tool_schema_target_tokens)} />
        <MiniMetric label="Conversation" value={fmtInt(data.conversation_tokens)} />
        <MiniMetric label="System+Tools" value={fmtPct(data.system_plus_tools_pct || data.system_pct)} />
        <MiniMetric label="Context" value={fmtInt(data.context_window)} />
        <MiniMetric label="Prompt Hash" value={data.prompt_hash || '-'} />
        <MiniMetric label="Tool Hash" value={data.tool_schema_hash || '-'} />
      </div>
    </div>
  )
}

function MiniMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="logs-mini-metric">
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

function runTelemetry(run: TaskRun) {
  const modelCalls = (run.events ?? []).filter(event => event.kind === 'model_call')
  const promptBudgets = (run.events ?? []).filter(event => event.kind === 'prompt_budget')
  const loopMetrics = (run.events ?? []).filter(event => event.kind === 'loop_metrics')
  const ttfts = modelCalls.map(event => num(parseKV(event.detail || '').ttft_ms)).filter(isFiniteNumber)
  const tps = modelCalls.map(event => num(parseKV(event.detail || '').tokens_per_second)).filter(value => isFiniteNumber(value) && value > 0)
  const promptHashes = new Set(modelCalls.map(event => parseKV(event.detail || '').prompt_hash).filter(Boolean))
  const toolHashes = new Set(modelCalls.map(event => parseKV(event.detail || '').tool_schema_hash).filter(Boolean))
  return {
    modelCalls,
    promptBudgets,
    latestModelCall: modelCalls[modelCalls.length - 1],
    latestPromptBudget: promptBudgets[promptBudgets.length - 1],
    latestLoopMetrics: loopMetrics[loopMetrics.length - 1],
    avgTtftMs: average(ttfts),
    avgTokensPerSecond: average(tps),
    promptWarnings: promptBudgets.filter(event => event.message.toLowerCase().includes('exceeds') || parseKV(event.detail || '').over_20_pct === 'true').length,
    uniquePromptHashes: promptHashes.size,
    uniqueToolSchemaHashes: toolHashes.size,
  }
}

function timelineSummary(event: { kind: string; message: string; detail?: string }) {
  const data = parseKV(event.detail || '')
  if (event.kind === 'model_call') {
    return `TTFT ${fmtMaybeMs(data.ttft_ms)} / ${fmtMaybeFloat(data.tokens_per_second)} tok/s / tools ${data.tools || data.tool_count || '0'}`
  }
  if (event.kind === 'prompt_budget') {
    return `${event.message}: ${fmtInt(data.estimated_total_tokens || data.estimated_tokens)} est tok / system+tools ${fmtPct(data.system_plus_tools_pct || data.system_pct)}`
  }
  if (event.kind === 'loop_metrics') {
    const metrics = { ...parseJSONRecord(event.detail || ''), ...data }
    return `stability ${metrics.stability_score || '-'} / repeats ${metrics.repeated_tool_inputs || '0'} / errors ${metrics.tool_errors || '0'} / max tools ${metrics.max_routed_tools || '-'}`
  }
  return event.message
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

function parseJSONRecord(detail: string): Record<string, string> {
  try {
    const parsed = JSON.parse(detail) as Record<string, unknown>
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    return Object.fromEntries(Object.entries(parsed).map(([key, value]) => [key, String(value)]))
  } catch {
    return {}
  }
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
