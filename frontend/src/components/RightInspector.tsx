import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  GetLabStatus,
  GetWorkingDir,
  ListTaskRuns,
  type LabStatus,
  type TaskRun,
} from '../wailsjs/go'
import type { AgentActivity, RunStatePayload } from '../App'
import './RightInspector.css'

type InspectorTab = 'agent' | 'workspace' | 'facts' | 'commands' | 'activity'

interface Props {
  agentPanel: ReactNode
  activity: AgentActivity[]
  runState: RunStatePayload | null
  streaming: boolean
  taskRunVersion: number
  workspaceVersion: number
  doctorFocusRequest: number
  workspaceBrowser: ReactNode
  requestedTab?: InspectorTab
  requestedTabVersion?: number
  onClose: () => void
}

export function RightInspector({
  agentPanel,
  activity,
  runState,
  streaming,
  taskRunVersion,
  workspaceVersion,
  doctorFocusRequest,
  workspaceBrowser,
  requestedTab,
  requestedTabVersion = 0,
  onClose,
}: Props) {
  const [tab, setTab] = useState<InspectorTab>('workspace')
  const [lab, setLab] = useState<LabStatus | null>(null)
  const [runs, setRuns] = useState<TaskRun[]>([])
  const [rootPath, setRootPath] = useState('')
  const latestRun = runs[0] ?? null
  const latestCommands = useMemo(() => {
    const runTools = (latestRun?.tools ?? []).slice(-10).reverse()
    if (runTools.length > 0) return runTools
    return activity.slice(0, 10).map(item => ({
      name: item.name,
      input: item.input,
      result: item.result,
      status: item.status,
      duration_ms: item.durationMs,
      timestamp: new Date(item.startTime).toISOString(),
    }))
  }, [activity, latestRun])

  const load = async () => {
    const [nextLab, nextRuns, cwd] = await Promise.all([
      GetLabStatus().catch(() => null),
      ListTaskRuns().catch(() => [] as TaskRun[]),
      GetWorkingDir().catch(() => ''),
    ])
    setLab(nextLab)
    setRuns(Array.isArray(nextRuns) ? nextRuns : [])
    setRootPath(nextLab?.agent_root || cwd)
  }

  useEffect(() => {
    void load()
  }, [taskRunVersion, workspaceVersion])

  useEffect(() => {
    if (doctorFocusRequest > 0) setTab('agent')
  }, [doctorFocusRequest])

  useEffect(() => {
    if (requestedTabVersion > 0 && requestedTab) setTab(requestedTab)
  }, [requestedTab, requestedTabVersion])

  return (
    <aside className="right-inspector">
      <div className="inspector-head">
        <div>
          <span className="inspector-kicker">Inspector</span>
          <strong title={lab?.agent_root || rootPath}>{lab?.target || shortPath(rootPath) || 'Workspace'}</strong>
        </div>
        <div className="inspector-head-actions">
          {streaming && <span className="inspector-state-chip" title={runState?.detail || statusLabel(runState, streaming)}>{statusLabel(runState, streaming)}</span>}
          <button onClick={() => void load()} title="Refresh inspector">Refresh</button>
		  <button className="inspector-close" onClick={onClose} title="Close Inspector (Ctrl+Shift+B)" aria-label="Close Inspector">×</button>
        </div>
      </div>

      <div className="inspector-tabs">
        {(['agent', 'workspace', 'facts', 'commands', 'activity'] as InspectorTab[]).map(name => (
          <button key={name} className={tab === name ? 'active' : ''} onClick={() => setTab(name)}>
            {name}
          </button>
        ))}
      </div>

      <div className="inspector-body">
        {tab === 'agent' && agentPanel}

        {tab === 'workspace' && (
          <div className="inspector-section inspector-workspace-section">
            <SummaryGrid
              items={[
                ['Root', lab?.agent_root || 'unknown'],
                ['Target', lab?.target || 'not set'],
                ['VPN', lab?.vpn_cidr || lab?.vpn_interface || 'not set'],
                ['Shell', shellLabel(lab)],
              ]}
            />
            {rootPath && <div className="workspace-root-note" title={rootPath}>{rootPath}</div>}
            <div className="inspector-file-browser">
              {workspaceBrowser}
            </div>
          </div>
        )}

        {tab === 'facts' && (
          <div className="inspector-section">
            <SummaryGrid
              items={[
                ['Access', lab?.access_preference || 'not set'],
                ['Host', lab?.hostname || 'unknown'],
                ['Profile', lab?.ops_profile || 'default'],
                ['Evidence', lab?.evidence_policy || 'not set'],
              ]}
            />
            <PanelCard title="Current Run">
              <div className="fact-line"><span>State</span><strong>{runState?.state || latestRun?.state || 'idle'}</strong></div>
              <div className="fact-line"><span>Detail</span><strong>{runState?.detail || latestRun?.stop_reason || latestRun?.status || 'ready'}</strong></div>
              <div className="fact-line"><span>Last Run</span><strong>{latestRun?.status || 'none'}</strong></div>
            </PanelCard>
            <PanelCard title="Do Not Guess">
              <p className="inspector-note">
                Target, shell, artifact, and run state shown here are the current UI facts. The agent prompt should use these instead of stale chat flow.
              </p>
            </PanelCard>
          </div>
        )}

        {tab === 'commands' && (
          <div className="inspector-section">
            {latestCommands.length === 0 ? (
              <div className="inspector-empty">No commands captured yet.</div>
            ) : latestCommands.map((tool, idx) => (
              <CommandCard
                key={`${tool.name}-${tool.timestamp ?? idx}-${idx}`}
                name={tool.name}
                status={tool.status}
                input={tool.input}
                result={tool.result}
                durationMs={tool.duration_ms}
              />
            ))}
          </div>
        )}

        {tab === 'activity' && (
          <div className="inspector-section">
            {activity.length === 0 ? (
              <div className="inspector-empty">No live activity yet.</div>
            ) : activity.slice(0, 18).map(item => (
              <CommandCard
                key={item.id}
                name={item.name}
                status={item.status}
                input={item.input}
                result={item.result}
                durationMs={item.durationMs}
              />
            ))}
          </div>
        )}
      </div>
    </aside>
  )
}

function SummaryGrid({ items }: { items: Array<[string, string]> }) {
  return (
    <div className="inspector-summary-grid">
      {items.map(([label, value]) => (
        <div key={label} className={`summary-${label.toLowerCase().replace(/\s+/g, '-')}`}>
          <span>{label}</span>
          <strong title={value}>{value}</strong>
        </div>
      ))}
    </div>
  )
}

function PanelCard({
  title,
  children,
  collapsible = false,
  defaultOpen = true,
}: {
  title: string
  children: ReactNode
  collapsible?: boolean
  defaultOpen?: boolean
}) {
  if (collapsible) {
    return (
      <details className="inspector-card inspector-card-details" open={defaultOpen}>
        <summary className="inspector-card-title">{title}</summary>
        <div className="inspector-card-content">{children}</div>
      </details>
    )
  }
  return (
    <section className="inspector-card">
      <div className="inspector-card-title">{title}</div>
      {children}
    </section>
  )
}

function CommandCard({
  name,
  status,
  input,
  result,
  durationMs,
}: {
  name: string
  status: string
  input?: string
  result?: string
  durationMs?: number
}) {
  const parsed = parseInput(input)
  const command = typeof parsed?.command === 'string' ? parsed.command : ''
  const copy = (value: string) => {
    void navigator.clipboard.writeText(value)
  }
  return (
    <section className={`command-card command-${status || 'unknown'}`}>
      <div className="command-card-head">
        <div>
          <strong>{name}</strong>
          <span>{status || 'unknown'}{durationMs ? ` / ${durationMs}ms` : ''}</span>
        </div>
        <div className="command-card-actions">
          {(command || input) && <button onClick={() => copy(command || input || '')}>Copy cmd</button>}
          {result && <button onClick={() => copy(result)}>Copy result</button>}
        </div>
      </div>
      {command && <pre className="command-line">{command}</pre>}
      {!command && input && <pre className="command-line">{compact(input)}</pre>}
      {result && <pre className="command-result">{compact(result)}</pre>}
    </section>
  )
}

function parseInput(input?: string): Record<string, unknown> | null {
  if (!input) return null
  try {
    const parsed = JSON.parse(input)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : null
  } catch {
    return null
  }
}

function compact(text: string): string {
  const trimmed = text.trim()
  return trimmed.length > 1200 ? `${trimmed.slice(0, 1200)}\n...` : trimmed
}

function statusLabel(runState: RunStatePayload | null, streaming: boolean): string {
  if (runState?.state) return runState.state.replaceAll('_', ' ')
  return streaming ? 'running' : 'ready'
}

function shortPath(path: string): string {
  if (!path) return ''
  const normalized = path.replace(/\\/g, '/')
  const parts = normalized.split('/').filter(Boolean)
  if (parts.length <= 2) return normalized
  return `.../${parts.slice(-2).join('/')}`
}

function shellLabel(lab: LabStatus | null): string {
  if (!lab) return 'unknown'
  return [lab.shell_backend, lab.shell_distro, lab.shell_user].filter(Boolean).join(' / ') || 'unknown'
}
