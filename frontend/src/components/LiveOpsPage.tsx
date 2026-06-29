import { useEffect, useMemo, useState } from 'react'
import {
  ApplySafetyPreset,
  GetHistoryStats,
  GetLabStatus,
  GetSettings,
  GetSpecPlan,
  type SpecPlan,
  ListMemory,
  ListTaskRuns,
  ListTodos,
  SetAgentModeOverride,
  UpdateLabContext,
  UpdateSettings,
  type HistoryStats,
  type LabStatus,
  type MemoryEntry,
  type Settings,
  type TaskRun,
  type TodoItem,
} from '../wailsjs/go'
import { EventsOn } from '../wailsjs/runtime'
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

interface OpsCommand {
  id: string
  tool: string
  command: string
  status: string
  timestamp: number
  durationMs?: number
  output?: string
  tone: StatusTone
}

interface OpsEvidence {
  id: string
  label: string
  value: string
  tone: StatusTone
  detail?: string
}

interface OpsDraft {
  target: string
  vpn: string
  artifact: string
  opsProfile: string
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
  const [settings, setSettings] = useState<Settings | null>(null)
  const [opsDraft, setOpsDraft] = useState<OpsDraft>({ target: '', vpn: '', artifact: '', opsProfile: 'pentesting' })
  const [savingOps, setSavingOps] = useState('')
  const [now, setNow] = useState(Date.now())
  const [sideTab, setSideTab] = useState<'facts' | 'questions' | 'files' | 'risks' | 'review'>('facts')
  const [specPlan, setSpecPlan] = useState<SpecPlan | null>(null)

  useEffect(() => {
    void Promise.all([
      ListTaskRuns().catch(() => [] as TaskRun[]),
      GetHistoryStats().catch(() => null),
      GetLabStatus().catch(() => null),
      ListMemory().catch(() => [] as MemoryEntry[]),
      ListTodos().catch(() => [] as TodoItem[]),
      GetSettings().catch(() => null),
    ]).then(([nextRuns, nextStats, nextLab, nextMemory, nextTodos, nextSettings]) => {
      setRuns(nextRuns)
      setStats(nextStats)
      setLab(nextLab)
      setMemory(nextMemory)
      setTodos(nextTodos)
      setSettings(nextSettings)
      if (nextLab) {
        setOpsDraft({
          target: nextLab.target || '',
          vpn: nextLab.vpn_interface || '',
          artifact: nextLab.latest_artifact || '',
          opsProfile: opsProfile(nextLab.ops_profile),
        })
      }
    })
  }, [statsVersion, taskRunVersion])

  useEffect(() => {
    if (!streaming) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [streaming])

  useEffect(() => {
    void GetSpecPlan().then(setSpecPlan).catch(() => {})
    return EventsOn('mauler:spec_plan', (...args: unknown[]) => setSpecPlan(args[0] as SpecPlan))
  }, [statsVersion])

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
    const tokenFraction = stats?.fraction ?? 0

    const kpiList: Array<{ label: string; value: string; tone?: StatusTone }> = [
      { label: 'Elapsed', value: elapsed != null ? fmtDuration(elapsed) : '-' },
      { label: 'Tools', value: String(toolCount) },
      { label: 'Edits', value: String(edits), tone: edits > 0 ? 'ok' : undefined },
      { label: 'Tests', value: String(tests), tone: tests > 0 ? 'ok' : undefined },
      { label: 'Tokens', value: tokens > 0 ? compactNumber(tokens) : '-', tone: tokenFraction > 0.9 ? 'bad' : tokenFraction > 0.75 ? 'warn' : undefined },
      { label: 'Recoveries', value: String(recoveries), tone: recoveries > 0 ? 'warn' : undefined },
    ]
    return kpiList
  }, [activity.length, latestRun, latestTools, now, runStartedAt, stats?.fraction, stats?.token_count, streaming])

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

  const commands = useMemo(() => deriveCommands({ activity, latestRun }), [activity, latestRun])
  const activeOpsProfile = opsProfile(lab?.ops_profile || opsDraft.opsProfile)
  const profileCopy = opsProfileCopy(activeOpsProfile)
  const evidence = useMemo(() => deriveEvidence({ activity, latestRun, lab, memory, profile: activeOpsProfile }), [activity, activeOpsProfile, lab, latestRun, memory])
  const artifacts = useMemo(() => deriveArtifacts({ activity, latestRun, lab }), [activity, lab, latestRun])

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

  const saveLab = async () => {
    setSavingOps('Saving lab context')
    try {
      const next = await UpdateLabContext(opsDraft.target, opsDraft.vpn, opsDraft.artifact, opsDraft.opsProfile)
      setLab(next)
      setOpsDraft({
        target: next.target || '',
        vpn: next.vpn_interface || '',
        artifact: next.latest_artifact || '',
        opsProfile: opsProfile(next.ops_profile),
      })
    } finally {
      setSavingOps('')
    }
  }

  const forceOpsMode = async () => {
    setSavingOps('Forcing Ops mode')
    try {
      await SetAgentModeOverride('Ops')
      if (settings) {
        setSettings({
          ...settings,
          agents: { ...settings.agents, mode_override: 'Ops' },
        })
      }
    } finally {
      setSavingOps('')
    }
  }

  const useAutoMode = async () => {
    setSavingOps('Switching to Auto')
    try {
      await SetAgentModeOverride('Auto')
      if (settings) {
        setSettings({
          ...settings,
          agents: { ...settings.agents, mode_override: 'Auto' },
        })
      }
    } finally {
      setSavingOps('')
    }
  }

  const applyDeepOps = async () => {
    setSavingOps('Applying deep Ops')
    try {
      await ApplySafetyPreset('unrestricted')
      const next = await GetSettings()
      const ops = next.agents.presets.Ops || {
        enabled: true,
        profile: '',
        context_budget: 32768,
        autonomy: 'balanced',
        toolset: 'unrestricted',
        instructions: '',
        tool_permissions: {},
      }
      await UpdateSettings({
        ...next,
        tools: {
          ...next.tools,
          shell_backend: next.tools.shell_backend === 'auto' ? 'wsl' : next.tools.shell_backend,
          shell_mode: 'shared_terminal',
          max_searches: Math.max(next.tools.max_searches || 0, 32),
          max_fetches: Math.max(next.tools.max_fetches || 0, 48),
          max_failed_fetches: Math.max(next.tools.max_failed_fetches || 0, 14),
          max_browser_actions: Math.max(next.tools.max_browser_actions || 0, 120),
          max_tool_result_chars: Math.max(next.tools.max_tool_result_chars || 0, 12000),
        },
        agents: {
          ...next.agents,
          mode_override: 'Ops',
          max_tool_calls: Math.max(next.agents.max_tool_calls || 0, 200),
          presets: {
            ...next.agents.presets,
            Ops: {
              ...ops,
              enabled: true,
              toolset: 'unrestricted',
              tool_permissions: {
                ...ops.tool_permissions,
                shell: true,
                bash: true,
                web_search: true,
                fetch_url: true,
                browser_open: true,
                browser_snapshot: true,
                browser_click: true,
                browser_type: true,
                browser_extract: true,
                browser_screenshot: true,
                browser_close: true,
                browser_agent: true,
              },
            },
          },
        },
      })
      setSettings(await GetSettings())
    } finally {
      setSavingOps('')
    }
  }

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
        <div className="ops-top-actions">
          <button type="button" onClick={forceOpsMode} disabled={savingOps !== '' || streaming}>Force Ops</button>
          <button type="button" onClick={useAutoMode} disabled={savingOps !== '' || streaming}>Auto</button>
          <button type="button" onClick={applyDeepOps} disabled={savingOps !== '' || streaming}>Deep Ops</button>
          <div className="ops-state-card">
            <span className={`ops-state-dot ops-state-${state}`} />
            <div>
              <span>Phase</span>
              <strong>{stateLabel}</strong>
            </div>
          </div>
        </div>
      </header>

      <section className="ops-kpi-strip">
        {kpis.map(kpi => (
          <div key={kpi.label} className="ops-kpi">
            <span>{kpi.label}</span>
            <strong className={kpi.tone ? `tone-${kpi.tone}` : ''}>{kpi.value}</strong>
          </div>
        ))}
      </section>

      <section className="ops-control-strip">
        <div className="ops-control-grid">
          <label>
            <span>Ops Profile</span>
            <select value={opsDraft.opsProfile} onChange={e => setOpsDraft({ ...opsDraft, opsProfile: e.target.value })}>
              <option value="pentesting">Pentesting</option>
              <option value="htb">HTB / CTF</option>
            </select>
          </label>
          <label>
            <span>Target</span>
            <input value={opsDraft.target} onChange={e => setOpsDraft({ ...opsDraft, target: e.target.value })} placeholder="10.129.x.x or host" />
          </label>
          <label>
            <span>VPN</span>
            <input value={opsDraft.vpn} onChange={e => setOpsDraft({ ...opsDraft, vpn: e.target.value })} placeholder="tun0" />
          </label>
          <label>
            <span>Report/Evidence</span>
            <input value={opsDraft.artifact} onChange={e => setOpsDraft({ ...opsDraft, artifact: e.target.value })} placeholder="report.md, PoC, scan, screenshot" />
          </label>
          <button type="button" onClick={saveLab} disabled={savingOps !== ''}>Save Context</button>
        </div>
        <div className="ops-mode-readout">
          <Readout label="Profile" value={profileCopy.label} />
          <Readout label="Override" value={settings?.agents.mode_override || 'Auto'} />
          <Readout label="Toolset" value={settings?.agents.presets?.Ops?.toolset || settings?.tools.active_toolset || '-'} />
          <Readout label="Shell" value={[lab?.shell_backend || settings?.tools.shell_backend || '-', lab?.shell_distro, lab?.shell_user].filter(Boolean).join(' / ')} />
        </div>
        <div className="ops-profile-note">{profileCopy.note}</div>
        {savingOps && <div className="ops-save-status">{savingOps}</div>}
      </section>

      <div className="ops-layout">
        <main className="ops-main">
          <section className="ops-workbench">
            <section className="ops-panel">
              <div className="ops-panel-head">
                <h2>Command Stream</h2>
                <span>{commands.length} command{commands.length === 1 ? '' : 's'}</span>
              </div>
              <div className="ops-command-list">
                {commands.length === 0 ? (
                  <div className="ops-empty">Shell and tool commands stream here as the agent runs — newest first, click any row to expand its output.</div>
                ) : commands.map(command => (
                  <details key={command.id} className="ops-command" open={command.status === 'running'}>
                    <summary>
                      <span className={`ops-command-dot ${command.tone}`} />
                      <code>{command.command}</code>
                      <span className={`ops-badge ${command.tone}`}>{command.status}</span>
                    </summary>
                    <div className="ops-command-meta">
                      <span>{command.tool}</span>
                      <span>{new Date(command.timestamp).toLocaleTimeString()}</span>
                      {command.durationMs != null && <span>{fmtDuration(command.durationMs)}</span>}
                    </div>
                    {command.output && <pre>{command.output}</pre>}
                  </details>
                ))}
              </div>
            </section>

            <section className="ops-panel">
              <div className="ops-panel-head">
                <h2>Evidence Board</h2>
                <span>{evidence.length} item{evidence.length === 1 ? '' : 's'}</span>
              </div>
              <div className="ops-evidence-grid">
                {evidence.length === 0 ? (
                  <div className="ops-empty">Hosts, open ports, CVEs, vulnerabilities, and flags surface here automatically as they appear in tool output.</div>
                ) : evidence.map(item => (
                  <details key={item.id} className={`ops-evidence ${item.tone}`}>
                    <summary>
                      <span>{item.label}</span>
                      <strong>{item.value}</strong>
                    </summary>
                    {item.detail && <pre>{item.detail}</pre>}
                  </details>
                ))}
              </div>
            </section>
          </section>

          <section className="ops-panel">
            <div className="ops-panel-head">
              <h2>Action Timeline</h2>
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
                <div className="ops-empty">Phases, tool calls, and results land here in order as the run proceeds. Click a row to inspect inputs and output.</div>
              ) : traceItems.map(item => (
                <details key={item.id} className={`ops-trace-item tone-${item.tone}`}>
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

          <section className="ops-workbench">
            <section className="ops-panel">
              <div className="ops-panel-head">
                <h2>Artifacts</h2>
                <span>{artifacts.length} path{artifacts.length === 1 ? '' : 's'}</span>
              </div>
              <InsightList empty="No artifacts found yet." items={artifacts} />
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
              <ContextRow label="Ops Profile" value={profileCopy.label} />
              <ContextRow label="MTP" value={specSummary(specPlan)} />
              <ContextRow label="Report/Evidence" value={lab?.latest_artifact || 'None'} />
            </div>
          </section>

          <section className="ops-panel ops-side-tabbed">
            <div className="ops-side-tabs" role="tablist">
              {([
                { key: 'facts', label: 'Facts', count: facts.length },
                { key: 'questions', label: 'Questions', count: openQuestions.length },
                { key: 'files', label: 'Files', count: touchedFiles.length },
                { key: 'risks', label: 'Risks', count: risks.length, tone: risks.length > 0 ? 'bad' : undefined },
                { key: 'review', label: 'Review', count: nextReview.length },
              ] as const).map(tab => (
                <button
                  key={tab.key}
                  role="tab"
                  className={sideTab === tab.key ? 'active' : ''}
                  onClick={() => setSideTab(tab.key)}
                >
                  {tab.label}
                  <span className={`ops-side-tab-count${'tone' in tab && tab.tone ? ` ${tab.tone}` : ''}`}>{tab.count}</span>
                </button>
              ))}
            </div>
            <div className="ops-side-tab-body">
              {sideTab === 'facts' && <InsightList empty="No facts available yet." items={facts} />}
              {sideTab === 'questions' && <InsightList empty="No open questions — the agent has no pending decisions." items={openQuestions} />}
              {sideTab === 'files' && <InsightList empty="No files touched yet — writes and edits will be listed here." items={touchedFiles} />}
              {sideTab === 'risks' && <InsightList empty="No risks detected — failures, guardrails, and blocks surface here." items={risks} />}
              {sideTab === 'review' && <InsightList empty="Nothing to review." items={nextReview.map(item => toInsight(item))} />}
            </div>
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

function Readout({ label, value }: { label: string; value: string }) {
  return (
    <div className="ops-readout">
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

function specSummary(plan: SpecPlan | null): string {
  if (!plan) return '-'
  if (!plan.enabled) {
    if (plan.source === 'guard') return 'Off (auto-disabled — instability)'
    return plan.locked ? 'Off (pinned)' : 'Off'
  }
  const n = plan.n_max > 0 ? ` n${plan.n_max}` : ''
  const src = plan.source === 'name' ? ' (unverified)' : ''
  return `On${n} · ${plan.source}${plan.locked ? ' (pinned)' : ''}${src}`
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

function titleCase(text: string): string {
  return text
    .replaceAll('-', ' ')
    .replace(/\b\w/g, ch => ch.toUpperCase())
}

function opsProfile(value: string | undefined): 'pentesting' | 'htb' {
  return value === 'htb' ? 'htb' : 'pentesting'
}

function opsProfileCopy(profile: 'pentesting' | 'htb'): { label: string; note: string } {
  if (profile === 'htb') {
    return {
      label: 'HTB / CTF',
      note: 'HTB mode tracks recon, foothold, user, privilege escalation, root, flags, and writeup evidence for authorised boxes.',
    }
  }
  return {
    label: 'Pentesting',
    note: 'Pentesting mode is attack/log/report focused: capture evidence and verified PoCs for authorised testing, without remediation or client-system fixes.',
  }
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

function deriveCommands({
  activity,
  latestRun,
}: {
  activity: AgentActivity[]
  latestRun?: TaskRun
}): OpsCommand[] {
  const commands: OpsCommand[] = []
  for (const item of activity) {
    const command = commandText(item.name, item.input)
    if (!command) continue
    commands.push({
      id: `live-${item.id}`,
      tool: item.name,
      command,
      status: item.status,
      timestamp: item.startTime,
      durationMs: item.durationMs,
      output: item.result,
      tone: statusTone(item.status),
    })
  }
  for (const [index, tool] of (latestRun?.tools ?? []).entries()) {
    const command = commandText(tool.name, tool.input)
    if (!command && tool.name !== 'shell' && tool.name !== 'bash') continue
    commands.push({
      id: `run-${latestRun?.id ?? 'latest'}-${index}`,
      tool: tool.name,
      command: command || tool.name,
      status: tool.status,
      timestamp: safeTime(tool.timestamp),
      durationMs: tool.duration_ms,
      output: tool.result,
      tone: statusTone(tool.status),
    })
  }
  return dedupeCommands(commands)
    .sort((a, b) => b.timestamp - a.timestamp)
    .slice(0, 12)
}

function commandText(tool: string, input?: string): string {
  if (!input) return ''
  try {
    const parsed = JSON.parse(input) as Record<string, unknown>
    if (typeof parsed.command === 'string') return parsed.command
    if (typeof parsed.query === 'string') return `${tool}: ${parsed.query}`
    if (typeof parsed.url === 'string') return `${tool}: ${parsed.url}`
    if (typeof parsed.path === 'string') return `${tool}: ${parsed.path}`
  } catch {
    // The stored input may already be plain command text.
  }
  return summarize(input)
}

function dedupeCommands(commands: OpsCommand[]): OpsCommand[] {
  const seen = new Set<string>()
  const out: OpsCommand[] = []
  for (const command of commands) {
    const key = `${command.tool}:${command.command}:${command.timestamp}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(command)
  }
  return out
}

function deriveEvidence({
  activity,
  latestRun,
  lab,
  memory,
  profile,
}: {
  activity: AgentActivity[]
  latestRun?: TaskRun
  lab: LabStatus | null
  memory: MemoryEntry[]
  profile: 'pentesting' | 'htb'
}): OpsEvidence[] {
  const text = [
    lab?.target ? `target ${lab.target}` : '',
    lab?.vpn_interface ? `vpn ${lab.vpn_interface}` : '',
    ...activity.flatMap(item => [item.input ?? '', item.result ?? '']),
    ...(latestRun?.tools ?? []).flatMap(tool => [tool.input ?? '', tool.result ?? '']),
    ...(latestRun?.events ?? []).flatMap(event => [event.message, event.detail ?? '']),
  ].join('\n')
  const items: OpsEvidence[] = []
  for (const ip of uniqueMatches(text, /\b(?:\d{1,3}\.){3}\d{1,3}\b/g).slice(0, 6)) {
    items.push({ id: `ip-${ip}`, label: 'Host', value: ip, tone: ip === lab?.target ? 'ok' : 'muted' })
  }
  for (const host of uniqueMatches(text, /\b[a-z0-9][a-z0-9.-]+\.(?:htb|local|lan|internal)\b/gi).slice(0, 6)) {
    items.push({ id: `host-${host}`, label: 'Name', value: host, tone: 'ok' })
  }
  for (const port of extractPorts(text).slice(0, 12)) {
    items.push({ id: `port-${port.port}-${port.service}`, label: 'Service', value: `${port.port}${port.service ? ` / ${port.service}` : ''}`, tone: 'pending', detail: port.line })
  }
  if (profile === 'htb') {
    for (const flag of uniqueMatches(text, /\b(?:user|root)\.txt\b|[a-f0-9]{32}/gi).slice(0, 4)) {
      items.push({ id: `flag-${flag}`, label: 'Flag/Loot', value: flag, tone: 'ok' })
    }
  }
  for (const cve of uniqueMatches(text, /\bCVE-\d{4}-\d{4,7}\b/gi).slice(0, 6)) {
    items.push({ id: `cve-${cve.toUpperCase()}`, label: 'CVE', value: cve.toUpperCase(), tone: 'warn' })
  }
  for (const vuln of uniqueMatches(text, /\b(?:SQL injection|command injection|remote code execution|RCE|auth(?:entication)? bypass|XSS|SSRF|LFI|RFI|path traversal|directory traversal|default credentials?|weak credentials?|information disclosure|IDOR|insecure deserialization|file upload bypass)\b/gi).slice(0, 8)) {
    items.push({ id: `vuln-${vuln.toLowerCase()}`, label: 'Vulnerability', value: titleCase(vuln), tone: 'warn' })
  }
  for (const poc of uniqueMatches(text, /\b(?:PoC|proof[- ]of[- ]concept|verified|reproduced|repro|exploit(?:ed)?|payload|curl|burp|request|response|screenshot)\b/gi).slice(0, 8)) {
    items.push({ id: `poc-${poc.toLowerCase()}`, label: 'PoC Signal', value: titleCase(poc), tone: 'pending' })
  }
  for (const finding of uniqueMatches(text, /\b(?:high|critical|medium|low)\s+(?:severity|risk)\b/gi).slice(0, 6)) {
    items.push({ id: `severity-${finding.toLowerCase()}`, label: 'Severity', value: titleCase(finding), tone: /critical|high/i.test(finding) ? 'bad' : 'pending' })
  }
  for (const item of memoryFacts(memory).slice(0, 3)) {
    items.push({ id: `memory-${item}`, label: 'Memory', value: item, tone: 'muted' })
  }
  return uniqueEvidence(items).slice(0, 24)
}

function deriveArtifacts({
  activity,
  latestRun,
  lab,
}: {
  activity: AgentActivity[]
  latestRun?: TaskRun
  lab: LabStatus | null
}): OpsInsight[] {
  const texts = [
    lab?.latest_artifact || '',
    ...activity.flatMap(item => [item.input ?? '', item.result ?? '']),
    ...(latestRun?.tools ?? []).flatMap(tool => [tool.input ?? '', tool.result ?? '']),
  ]
  const paths = new Map<string, StatusTone>()
  for (const text of texts) {
    for (const path of extractPaths(text)) {
      if (/\.(nmap|gnmap|xml|txt|md|log|json|csv|html|py|sh|png|jpg|jpeg|har)$/i.test(path) || /\/tmp\/|scans?|evidence|report|poc|screenshots?|writeups?/i.test(path)) {
        paths.set(path, pathTone(path))
      }
    }
  }
  if (lab?.latest_artifact) paths.set(lab.latest_artifact, 'ok')
  return [...paths.entries()]
    .slice(0, 12)
    .map(([path, tone]) => toInsight(shortPath(path), tone, path))
}

function uniqueMatches(text: string, pattern: RegExp): string[] {
  const found = new Set<string>()
  let match: RegExpExecArray | null
  while ((match = pattern.exec(text)) !== null) {
    found.add(match[0])
  }
  return [...found]
}

function extractPorts(text: string): Array<{ port: string; service: string; line: string }> {
  const out: Array<{ port: string; service: string; line: string }> = []
  const seen = new Set<string>()
  for (const line of text.split(/\r?\n/)) {
    const match = line.match(/\b(\d{1,5})\/(?:tcp|udp)\s+open\s+([^\s]+)/i)
    if (!match) continue
    const key = `${match[1]}:${match[2]}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push({ port: match[1], service: match[2], line: line.trim() })
  }
  return out
}

function uniqueEvidence(items: OpsEvidence[]): OpsEvidence[] {
  const seen = new Set<string>()
  const out: OpsEvidence[] = []
  for (const item of items) {
    const key = `${item.label}:${item.value}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(item)
  }
  return out
}

function safeTime(value: string): number {
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : Date.now()
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
