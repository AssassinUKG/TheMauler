import { useEffect, useMemo, useState } from 'react'
import {
  GetHistoryStats,
  GetLabStatus,
  ListMemory,
  ListTaskRuns,
  ListTodos,
  type HistoryStats,
  type LabStatus,
  type MemoryEntry,
  type TaskRun,
  type TodoItem,
} from '../wailsjs/go'
import type { AgentActivity, RunStatePayload } from '../App'
import './LiveOpsPage.css'

interface Props {
  streaming: boolean
  runState: RunStatePayload | null
  activity: AgentActivity[]
  agentMode: string
  activeProfile: string
  statsVersion: number
  taskRunVersion: number
  runStartedAt: number | null
}

type StatusTone = 'ok' | 'warn' | 'bad' | 'pending' | 'muted'

interface TraceItem {
  id: string
  timestamp: number
  type: string
  action: string
  observation: string
  outcome: string
  tone: StatusTone
  detail?: string
}

interface OpsInsight {
  id: string
  text: string
  tone: StatusTone
  detail?: string
}

export function LiveOpsPage({
  streaming,
  runState,
  activity,
  agentMode,
  activeProfile,
  statsVersion,
  taskRunVersion,
  runStartedAt,
}: Props) {
  const [runs, setRuns] = useState<TaskRun[]>([])
  const [stats, setStats] = useState<HistoryStats | null>(null)
  const [lab, setLab] = useState<LabStatus | null>(null)
  const [memory, setMemory] = useState<MemoryEntry[]>([])
  const [todos, setTodos] = useState<TodoItem[]>([])
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    void Promise.all([
      ListTaskRuns().catch(() => [] as TaskRun[]),
      GetHistoryStats().catch(() => null),
      GetLabStatus().catch(() => null),
      ListMemory().catch(() => [] as MemoryEntry[]),
      ListTodos().catch(() => [] as TodoItem[]),
    ]).then(([nextRuns, nextStats, nextLab, nextMemory, nextTodos]) => {
      setRuns(nextRuns)
      setStats(nextStats)
      setLab(nextLab)
      setMemory(nextMemory)
      setTodos(nextTodos)
    })
  }, [statsVersion, taskRunVersion])

  useEffect(() => {
    if (!streaming) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [streaming])

  const latestRun = runs[0]
  const objective = latestRun?.prompt || (streaming ? 'Current request in progress' : 'No run selected')
  const state = streaming ? (runState?.state || 'starting') : (latestRun?.state || latestRun?.status || 'idle')
  const stateLabel = formatState(state)
  const activeTool = activity.find(item => item.status === 'running')
  const lastEvents = (latestRun?.events ?? []).slice(-8).reverse()
  const latestTools = latestRun?.tools ?? []
  const traceItems = useMemo(() => buildTraceItems({
    activity,
    latestRun,
    runState,
    streaming,
    runStartedAt,
  }), [activity, latestRun, runStartedAt, runState, streaming])

  const kpis = useMemo(() => {
    const elapsed = streaming && runStartedAt ? now - runStartedAt : latestRun?.duration_ms
    const toolCount = streaming ? activity.length : latestTools.length
    const edits = latestTools.filter(tool => ['write_file', 'edit_file'].includes(tool.name)).length
    const tests = latestTools.filter(tool => tool.name === 'shell' && /test|npm run build|go vet|go test/i.test(`${tool.input ?? ''} ${tool.result ?? ''}`)).length
    const recoveries = (latestRun?.events ?? []).filter(event => ['recovering', 'tool_error', 'continue', 'truncated'].includes(event.kind)).length
    const tokens = latestRun?.total_tokens && latestRun.total_tokens > 0
      ? latestRun.total_tokens
      : stats?.token_count ?? 0

    return [
      { label: 'Elapsed', value: elapsed != null ? fmtDuration(elapsed) : '-' },
      { label: 'Tools', value: String(toolCount) },
      { label: 'Edits', value: String(edits) },
      { label: 'Tests', value: String(tests) },
      { label: 'Tokens', value: tokens > 0 ? compactNumber(tokens) : '-' },
      { label: 'Recoveries', value: String(recoveries) },
    ]
  }, [activity.length, latestRun, latestTools, now, runStartedAt, stats?.token_count, streaming])

  const facts = useMemo(() => {
    const next: string[] = []
    if (lab?.target) next.push(`Target: ${lab.target}`)
    if (lab?.agent_root) next.push(`Agent root: ${lab.agent_root}`)
    if (lab?.shell_backend) {
      const shell = [lab.shell_backend, lab.shell_distro, lab.shell_user].filter(Boolean).join(' / ')
      next.push(`Shell: ${shell}`)
    }
    if (lab?.latest_artifact) next.push(`Latest artifact: ${lab.latest_artifact}`)
    if (latestRun?.model) next.push(`Model: ${latestRun.model}`)
    if (activeProfile) next.push(`Profile: ${activeProfile}`)
    for (const item of memoryFacts(memory).slice(0, 4)) next.push(item)
    return next.map(item => toInsight(item)).slice(0, 8)
  }, [activeProfile, lab, latestRun?.model, memory])

  const openQuestions = useMemo(() => deriveOpenQuestions({
    activeTool,
    latestRun,
    runState,
    stats,
    streaming,
    todos,
  }), [activeTool, latestRun, runState, stats, streaming, todos])

  const touchedFiles = useMemo(() => deriveTouchedFiles({
    activity,
    latestRun,
    lab,
  }), [activity, latestRun, lab])

  const risks = useMemo(() => deriveRisks({
    latestRun,
    stats,
    todos,
  }), [latestRun, stats, todos])

  const nextReview = useMemo(() => {
    const items: string[] = []
    if (activeTool) items.push(`Waiting on ${activeTool.name}`)
    if (latestRun?.stop_reason) items.push(`Review stop reason: ${latestRun.stop_reason}`)
    if ((latestRun?.tools ?? []).some(tool => statusTone(tool.status) === 'bad')) items.push('Inspect failed tool output')
    if (stats && stats.fraction > 0.85) items.push('Context is near compaction range')
    if (items.length === 0) items.push(streaming ? 'Watch the trace for the next tool result' : 'Start a task or select a recent log')
    return items
  }, [activeTool, latestRun, stats, streaming])

  return (
    <div className="live-ops-page">
      <header className="ops-topbar">
        <div className="ops-title-block">
          <div className="ops-live-line">
            <span className={`ops-live-dot ${streaming ? 'active' : ''}`} />
            <span>{streaming ? 'Live Ops' : 'Last Run'}</span>
          </div>
          <h1>{objective}</h1>
        </div>
        <div className="ops-state-card">
          <span className={`ops-state-dot ops-state-${state}`} />
          <div>
            <span>Phase</span>
            <strong>{stateLabel}</strong>
          </div>
        </div>
      </header>

      <section className="ops-kpi-strip">
        {kpis.map(kpi => (
          <div key={kpi.label} className="ops-kpi">
            <span>{kpi.label}</span>
            <strong>{kpi.value}</strong>
          </div>
        ))}
      </section>

      <div className="ops-layout">
        <main className="ops-main">
          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Action Loop</h2>
              {activeTool && <span className="ops-active-tool">Running {activeTool.name}</span>}
            </div>
            <div className="ops-trace-table">
              <div className="ops-trace-head">
                <span>Time</span>
                <span>Type</span>
                <span>Action</span>
                <span>Observation</span>
                <span>Outcome</span>
              </div>
              {traceItems.length === 0 ? (
                <div className="ops-empty">No live activity yet.</div>
              ) : traceItems.map(item => (
                <details key={item.id} className="ops-trace-item">
                  <summary className="ops-trace-row">
                    <span>{new Date(item.timestamp).toLocaleTimeString()}</span>
                    <span className="ops-trace-type">{item.type}</span>
                    <span>{item.action}</span>
                    <span>{item.observation}</span>
                    <span className={`ops-badge ${item.tone}`}>{item.outcome}</span>
                  </summary>
                  {item.detail && (
                    <pre>{item.detail}</pre>
                  )}
                </details>
              ))}
            </div>
          </section>

          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Latest Timeline</h2>
              {latestRun && <span>{new Date(latestRun.started_at).toLocaleString()}</span>}
            </div>
            <div className="ops-event-list">
              {lastEvents.length === 0 ? (
                <div className="ops-empty">No logged timeline events yet.</div>
              ) : lastEvents.map((event, index) => (
                <details key={`${event.timestamp}-${index}`} className="ops-event">
                  <summary>
                    <span className={`ops-badge ${eventTone(event.kind)}`}>{event.kind}</span>
                    <span>{event.message}</span>
                    <time>{new Date(event.timestamp).toLocaleTimeString()}</time>
                  </summary>
                  {event.detail && <pre>{event.detail}</pre>}
                </details>
              ))}
            </div>
          </section>
        </main>

        <aside className="ops-side">
          <section className="ops-panel">
            <h2>Run Context</h2>
            <div className="ops-context-grid">
              <ContextRow label="Mode" value={agentMode || 'Auto'} />
              <ContextRow label="Profile" value={activeProfile || '-'} />
              <ContextRow label="Agent Root" value={lab?.agent_root || '-'} />
              <ContextRow label="Target" value={lab?.target || 'Not set'} />
              <ContextRow label="VPN" value={lab?.vpn_interface || 'Not set'} />
              <ContextRow label="Artifact" value={lab?.latest_artifact || 'None'} />
            </div>
          </section>

          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Live Facts</h2>
              <span>{facts.length} known</span>
            </div>
            <InsightList empty="No facts available yet." items={facts} />
          </section>

          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Open Questions</h2>
              <span>{openQuestions.length} active</span>
            </div>
            <InsightList empty="No open questions." items={openQuestions} />
          </section>

          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Touched Files</h2>
              <span>{touchedFiles.length} paths</span>
            </div>
            <InsightList empty="No files touched yet." items={touchedFiles} />
          </section>

          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Risks</h2>
              <span>{risks.length} flags</span>
            </div>
            <InsightList empty="No current risks." items={risks} />
          </section>

          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Next Review</h2>
              <span>{nextReview.length} item{nextReview.length === 1 ? '' : 's'}</span>
            </div>
            <InsightList empty="Nothing to review." items={nextReview.map(item => toInsight(item))} />
          </section>
        </aside>
      </div>
    </div>
  )
}

function ContextRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="ops-context-row">
      <span>{label}</span>
      <strong title={value}>{value}</strong>
    </div>
  )
}

function InsightList({ empty, items }: { empty: string; items: OpsInsight[] }) {
  if (items.length === 0) return <div className="ops-empty">{empty}</div>
  return (
    <div className="ops-insight-list">
      {items.map(item => (
        <div key={item.id} className="ops-insight-row">
          <span className={`ops-insight-dot ${item.tone}`} />
          <div>
            <strong>{item.text}</strong>
            {item.detail && <span>{item.detail}</span>}
          </div>
        </div>
      ))}
    </div>
  )
}

function formatState(state: string): string {
  if (!state || state === 'idle') return 'Idle'
  return state.replaceAll('_', ' ').replace(/\b\w/g, ch => ch.toUpperCase())
}

function fmtDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s`
  const m = Math.floor(ms / 60_000)
  const s = Math.floor((ms % 60_000) / 1000)
  return `${m}m ${s}s`
}

function compactNumber(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}m`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`
  return String(value)
}

function summarize(text: string): string {
  const cleaned = text.replace(/\s+/g, ' ').trim()
  if (!cleaned) return 'Waiting for output'
  return cleaned.length > 140 ? `${cleaned.slice(0, 140)}...` : cleaned
}

function toInsight(text: string, tone: StatusTone = 'muted', detail?: string): OpsInsight {
  return {
    id: `${tone}:${text}:${detail ?? ''}`,
    text,
    tone,
    detail,
  }
}

function memoryFacts(memory: MemoryEntry[]): string[] {
  return [...memory]
    .sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.importance - a.importance || Date.parse(b.updated_at) - Date.parse(a.updated_at))
    .map(item => item.title || summarize(item.content))
    .filter(Boolean)
}

function deriveOpenQuestions({
  activeTool,
  latestRun,
  runState,
  stats,
  streaming,
  todos,
}: {
  activeTool?: AgentActivity
  latestRun?: TaskRun
  runState: RunStatePayload | null
  stats: HistoryStats | null
  streaming: boolean
  todos: TodoItem[]
}): OpsInsight[] {
  const items: OpsInsight[] = []
  const activeTodos = todos.filter(todo => todo.status !== 'done').slice(0, 4)
  for (const todo of activeTodos) {
    items.push(toInsight(todo.text, todo.status === 'blocked' ? 'warn' : 'pending', todo.detail))
  }
  if (activeTool) items.unshift(toInsight(`Waiting on ${activeTool.name}`, 'pending', summarize(activeTool.input || 'Tool is still running')))
  if (streaming && runState?.detail) items.unshift(toInsight(formatState(runState.state), 'pending', runState.detail))
  if (latestRun?.stop_reason) items.push(toInsight(`Resolve stop reason: ${latestRun.stop_reason}`, 'warn', latestRun.stop_detail))
  if (stats && stats.fraction > 0.85) items.push(toInsight('Context is near compaction range', 'warn', `${Math.round(stats.fraction * 100)}% used`))
  return uniqueInsights(items).slice(0, 7)
}

function deriveTouchedFiles({
  activity,
  latestRun,
  lab,
}: {
  activity: AgentActivity[]
  latestRun?: TaskRun
  lab: LabStatus | null
}): OpsInsight[] {
  const texts = [
    ...activity.flatMap(item => [item.input ?? '', item.result ?? '']),
    ...(latestRun?.tools ?? []).flatMap(tool => [tool.input ?? '', tool.result ?? '']),
  ]
  const paths = new Map<string, StatusTone>()
  for (const text of texts) {
    for (const path of extractPaths(text)) {
      paths.set(path, pathTone(path))
    }
  }
  if (lab?.latest_artifact) paths.set(lab.latest_artifact, 'ok')
  return [...paths.entries()]
    .slice(0, 8)
    .map(([path, tone]) => toInsight(shortPath(path), tone, path))
}

function deriveRisks({
  latestRun,
  stats,
  todos,
}: {
  latestRun?: TaskRun
  stats: HistoryStats | null
  todos: TodoItem[]
}): OpsInsight[] {
  const items: OpsInsight[] = []
  if (latestRun?.status === 'error' || latestRun?.status === 'failed') {
    items.push(toInsight('Latest run failed', 'bad', latestRun.stop_detail || latestRun.stop_reason))
  }
  if (latestRun?.status === 'stopped') {
    items.push(toInsight('Latest run stopped before completion', 'warn', latestRun.stop_reason || latestRun.stop_detail))
  }
  for (const tool of latestRun?.tools ?? []) {
    if (statusTone(tool.status) === 'bad') items.push(toInsight(`${tool.name} failed`, 'bad', summarize(tool.result || tool.input || '')))
  }
  for (const event of latestRun?.events ?? []) {
    if (event.kind === 'guardrail') items.push(toInsight('Guardrail notice', 'warn', event.message))
    if (event.kind === 'tool_error') items.push(toInsight('Tool retry/recovery happened', 'warn', event.message))
  }
  const blocked = todos.filter(todo => todo.status === 'blocked')
  for (const todo of blocked.slice(0, 3)) items.push(toInsight(`Blocked: ${todo.text}`, 'warn', todo.detail))
  if (stats && stats.fraction > 0.9) items.push(toInsight('Context above 90%', 'bad', `${stats.token_count.toLocaleString()} / ${stats.budget.toLocaleString()} tokens`))
  return uniqueInsights(items).slice(0, 7)
}

function uniqueInsights(items: OpsInsight[]): OpsInsight[] {
  const seen = new Set<string>()
  const next: OpsInsight[] = []
  for (const item of items) {
    const key = `${item.text}:${item.detail ?? ''}`
    if (seen.has(key)) continue
    seen.add(key)
    next.push(item)
  }
  return next
}

function extractPaths(text: string): string[] {
  if (!text) return []
  const paths = new Set<string>()
  const quotedPath = /"(?:path|file|target|latest_artifact)"\s*:\s*"([^"]+)"/gi
  let match: RegExpExecArray | null
  while ((match = quotedPath.exec(text)) !== null) paths.add(match[1])

  const loosePath = /(?:[A-Za-z]:\\[^\s"'<>|]+|\/mnt\/[a-z]\/[^\s"'<>|]+|(?:\.{1,2}\/)?[\w.-]+(?:\/[\w .@()[\]-]+)+)/g
  while ((match = loosePath.exec(text)) !== null) paths.add(match[0].replace(/[),.;:]+$/, ''))
  return [...paths].filter(path => path.length > 2 && !path.includes('://')).slice(0, 20)
}

function pathTone(path: string): StatusTone {
  if (/\.(err|log|trace)$/i.test(path)) return 'warn'
  if (/\.(test|spec)\.[jt]sx?$|_test\.go$/i.test(path)) return 'pending'
  return 'ok'
}

function shortPath(path: string): string {
  const normalized = path.replaceAll('\\', '/')
  const parts = normalized.split('/').filter(Boolean)
  if (parts.length <= 3) return path
  return `.../${parts.slice(-3).join('/')}`
}

function buildTraceItems({
  activity,
  latestRun,
  runState,
  streaming,
  runStartedAt,
}: {
  activity: AgentActivity[]
  latestRun?: TaskRun
  runState: RunStatePayload | null
  streaming: boolean
  runStartedAt: number | null
}): TraceItem[] {
  const items: TraceItem[] = []

  if (streaming && runState?.state) {
    items.push({
      id: `live-state-${runState.state}-${runState.detail ?? ''}`,
      timestamp: Date.now(),
      type: 'phase',
      action: formatState(runState.state),
      observation: summarize(runState.detail || 'Agent phase updated'),
      outcome: runState.state,
      tone: eventTone(runState.state),
      detail: runState.detail,
    })
  } else if (!streaming && latestRun) {
    items.push({
      id: `run-status-${latestRun.id}`,
      timestamp: latestRun.ended_at ? Date.parse(latestRun.ended_at) : Date.parse(latestRun.started_at),
      type: 'run',
      action: latestRun.status,
      observation: summarize(latestRun.stop_detail || latestRun.summary || latestRun.response || latestRun.prompt),
      outcome: latestRun.stop_reason || latestRun.status,
      tone: statusTone(latestRun.status),
      detail: latestRun.stop_detail || latestRun.summary || latestRun.response,
    })
  }

  for (const item of activity) {
    items.push({
      id: `live-tool-${item.id}`,
      timestamp: item.startTime,
      type: 'tool',
      action: item.name,
      observation: summarize(item.result || item.input || ''),
      outcome: item.status,
      tone: statusTone(item.status),
      detail: joinDetail(item.input, item.result),
    })
  }

  for (const [index, tool] of (latestRun?.tools ?? []).entries()) {
    const parsedTime = Date.parse(tool.timestamp)
    items.push({
      id: `logged-tool-${latestRun?.id ?? 'latest'}-${index}`,
      timestamp: Number.isFinite(parsedTime) ? parsedTime : Date.now(),
      type: tool.name === 'shell' ? 'command' : 'tool',
      action: tool.name,
      observation: summarize(tool.result || tool.input || ''),
      outcome: tool.status,
      tone: statusTone(tool.status),
      detail: joinDetail(tool.input, tool.result),
    })
  }

  for (const [index, event] of (latestRun?.events ?? []).entries()) {
    const parsedTime = Date.parse(event.timestamp)
    items.push({
      id: `event-${latestRun?.id ?? 'latest'}-${index}`,
      timestamp: Number.isFinite(parsedTime) ? parsedTime : Date.now(),
      type: eventType(event.kind),
      action: event.kind,
      observation: summarize(event.message),
      outcome: event.kind,
      tone: eventTone(event.kind),
      detail: event.detail,
    })
  }

  if (streaming && runStartedAt) {
    items.push({
      id: 'live-run-started',
      timestamp: runStartedAt,
      type: 'run',
      action: 'started',
      observation: 'Run accepted and stream opened',
      outcome: 'live',
      tone: 'pending',
    })
  }

  return dedupeTraceItems(items)
    .sort((a, b) => b.timestamp - a.timestamp)
    .slice(0, 18)
}

function dedupeTraceItems(items: TraceItem[]): TraceItem[] {
  const seen = new Set<string>()
  const next: TraceItem[] = []
  for (const item of items) {
    const key = `${item.type}:${item.action}:${item.timestamp}:${item.observation}`
    if (seen.has(key)) continue
    seen.add(key)
    next.push(item)
  }
  return next
}

function joinDetail(input?: string, result?: string): string | undefined {
  const parts: string[] = []
  if (input) parts.push(`Input\n${input}`)
  if (result) parts.push(`Result\n${result}`)
  return parts.length > 0 ? parts.join('\n\n') : undefined
}

function eventType(kind: string): string {
  if (kind === 'state') return 'phase'
  if (kind === 'tool_call' || kind === 'tool_result' || kind === 'tool_error') return 'tool'
  if (kind === 'guardrail') return 'guard'
  if (kind === 'continue' || kind === 'truncated' || kind === 'recovering') return 'recover'
  if (kind === 'blocked' || kind === 'denied') return 'block'
  if (kind === 'error' || kind === 'failed') return 'error'
  return 'event'
}

function statusTone(status: string): StatusTone {
  if (status === 'done' || status === 'ok') return 'ok'
  if (status === 'running') return 'pending'
  if (status === 'blocked' || status === 'denied' || status === 'stopped') return 'warn'
  if (status === 'error' || status === 'failed') return 'bad'
  return 'muted'
}

function eventTone(kind: string): StatusTone {
  if (kind === 'error' || kind === 'failed' || kind === 'tool_error') return 'bad'
  if (kind === 'blocked' || kind === 'denied' || kind === 'continue' || kind === 'truncated') return 'warn'
  if (kind === 'state' || kind === 'done' || kind === 'tool_result' || kind === 'compaction') return 'ok'
  return 'muted'
}
