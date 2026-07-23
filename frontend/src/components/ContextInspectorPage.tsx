import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  GetNextContextPacketClass,
  PreviewContext,
  ReadFileContent,
  SetNextContextPacketClass,
  type ContextInspection,
  type ContextInspectionRange,
} from '../wailsjs/go'
import type { OpenFile } from './FileViewer'
import './ContextInspectorPage.css'

type PacketClass = 'auto' | 'core' | 'relevant' | 'expanded'

interface Props {
  version: number
  onOpenFile: (file: OpenFile) => void
  onOpenSettings: () => void
}

const PACKETS: Array<{ id: PacketClass; label: string; description: string }> = [
  { id: 'auto', label: 'Auto', description: 'Task-aware minimal or relevant routing.' },
  { id: 'core', label: 'Core', description: 'Canonical AGENTS.md only.' },
  { id: 'relevant', label: 'Relevant', description: 'Best matching route, up to three sources.' },
  { id: 'expanded', label: 'Expanded', description: 'Explicit broad packet for one task only.' },
]

function compactNumber(value: number): string {
  return new Intl.NumberFormat('en-GB', { maximumFractionDigits: 1, notation: value >= 10_000 ? 'compact' : 'standard' }).format(value || 0)
}

function formatBytes(value: number): string {
  if (!value) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(value >= 10 * 1024 ? 0 : 1)} KiB`
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`
}

function formatRanges(ranges: ContextInspectionRange[]): string {
  return (ranges ?? []).map(range => range.start_line === range.end_line ? `${range.start_line}` : `${range.start_line}-${range.end_line}`).join(', ')
}

function shortHash(hash?: string): string {
  if (!hash) return 'not available'
  return `${hash.slice(0, 12)}…${hash.slice(-8)}`
}

function safePercent(value: number): number {
  return Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0))
}

export function ContextInspectorPage({ version, onOpenFile, onOpenSettings }: Props) {
  const [task, setTask] = useState('Continue the next implementation milestone in this workspace safely.')
  const [packetClass, setPacketClass] = useState<PacketClass>('auto')
  const [inspection, setInspection] = useState<ContextInspection | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const rebuild = useCallback(async (nextClass: PacketClass = packetClass) => {
    setLoading(true)
    setError('')
    try {
      setInspection(await PreviewContext(task, nextClass))
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setLoading(false)
    }
  }, [packetClass, task])

  useEffect(() => {
    let active = true
    void GetNextContextPacketClass()
      .then(value => {
        if (!active) return
        const initial = (['core', 'relevant', 'expanded'].includes(value) ? value : 'auto') as PacketClass
        setPacketClass(initial)
        return PreviewContext(task, initial)
      })
      .then(value => { if (active && value) setInspection(value) })
      .catch(reason => { if (active) setError(reason instanceof Error ? reason.message : String(reason)) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [version]) // Refresh after workspace/settings/run changes; task text remains local.

  const choosePacket = async (next: PacketClass) => {
    setPacketClass(next)
    await rebuild(next)
  }

  const togglePin = async () => {
    if (!inspection) return
    try {
      const alreadyPinned = Boolean(inspection.pinned_next_class) && (packetClass === 'auto' || inspection.pinned_next_class === packetClass)
      await SetNextContextPacketClass(alreadyPinned ? 'auto' : packetClass)
      await rebuild(packetClass)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  const openPath = async (path: string) => {
    try {
      const content = await ReadFileContent(path)
      const name = path.replaceAll('\\', '/').split('/').pop() || 'context.md'
      const ext = name.split('.').pop()?.toLowerCase()
      onOpenFile({ path, name, content, lang: ext === 'json' ? 'json' : 'markdown' })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  const budgetRows = useMemo(() => inspection ? [
    { id: 'core', label: 'Core / system', value: inspection.budget.core_system_tokens, colour: '#38bdf8' },
    { id: 'project', label: 'Project docs', value: inspection.budget.project_document_tokens, colour: '#22c55e' },
    { id: 'tools', label: 'Tool schemas', value: inspection.budget.tool_schema_tokens, colour: '#f59e0b' },
    { id: 'memory', label: 'Memory / progress', value: inspection.budget.memory_progress_tokens, colour: '#a78bfa' },
    { id: 'skills', label: 'Skills', value: inspection.budget.skill_tokens, colour: '#f472b6' },
    { id: 'profile', label: 'User profile', value: inspection.budget.user_profile_tokens, colour: '#64748b' },
    { id: 'history', label: 'Conversation', value: inspection.budget.conversation_tokens, colour: '#14b8a6' },
    { id: 'task', label: 'Task', value: inspection.budget.user_task_tokens, colour: '#fb7185' },
  ] : [], [inspection])

  const pinned = inspection?.pinned_next_class || ''
  const canPin = packetClass !== 'auto' || Boolean(pinned)

  return (
    <section className="context-inspector-page">
      <header className="context-inspector-header">
        <div>
          <span className="context-eyebrow">NEXT TASK PREFLIGHT</span>
          <h1>Context Inspector</h1>
          <p>See exactly what Mauler will send, why it was selected, and how much working context remains.</p>
        </div>
        <div className="context-header-actions">
          <button onClick={() => void rebuild()} disabled={loading}>{loading ? 'Rebuilding…' : 'Rebuild synopsis'}</button>
          <button onClick={onOpenSettings}>Context settings</button>
        </div>
      </header>

      <div className="context-task-card">
        <label htmlFor="context-preview-task">Preview a task</label>
        <textarea id="context-preview-task" rows={3} value={task} onChange={event => setTask(event.target.value)} />
        <div className="context-packet-picker" role="group" aria-label="Context packet class">
          {PACKETS.map(packet => (
            <button key={packet.id} className={packetClass === packet.id ? 'active' : ''} onClick={() => void choosePacket(packet.id)}>
              <strong>{packet.label}</strong>
              <span>{packet.description}</span>
            </button>
          ))}
        </div>
        <div className="context-pin-row">
          <div>
            <span className={`context-pin-state ${pinned ? 'pinned' : ''}`}>{pinned ? `${pinned} pinned for next desktop task` : 'No one-task packet pinned'}</span>
            <span>Pinning is consumed once and never changes the persistent local default.</span>
          </div>
          <button className={(pinned === packetClass || (packetClass === 'auto' && pinned)) ? 'danger-soft' : 'primary'} disabled={!canPin || loading} onClick={() => void togglePin()}>
            {(pinned === packetClass || (packetClass === 'auto' && pinned)) ? 'Unpin next task' : 'Use for next task'}
          </button>
        </div>
      </div>

      {error && <div className="context-error">{error}</div>}

      {inspection && (
        <>
          <div className="context-summary-grid">
            <article><span>Effective packet</span><strong>{inspection.effective_class}</strong><small>{inspection.policy}</small></article>
            <article><span>Manifest</span><strong className={`status-${inspection.manifest_status}`}>{inspection.manifest_status}</strong><small>{inspection.route_id || 'default packet'}</small></article>
            <article><span>Working context</span><strong>{compactNumber(inspection.working_context_tokens)}</strong><small>{compactNumber(inspection.output_reserve_tokens)} response reserve</small></article>
            <article><span>Preflight estimate</span><strong>{compactNumber(inspection.budget.total_preflight_tokens)}</strong><small>{inspection.budget.usage_percent.toFixed(1)}% of working context</small></article>
            <article><span>Remaining</span><strong>{compactNumber(inspection.budget.remaining_working_tokens)}</strong><small>{compactNumber(inspection.model_max_output_tokens)} max output</small></article>
            <article><span>Initial tool route</span><strong>{inspection.tool_count}</strong><small>{inspection.tool_choice} · {inspection.agent_mode}</small></article>
          </div>

          <article className="context-panel context-budget-panel">
            <div className="context-panel-head">
              <div><span className="context-eyebrow">TOKEN MAP</span><h2>Preflight budget</h2></div>
              <span>{compactNumber(inspection.budget.total_preflight_tokens)} / {compactNumber(inspection.working_context_tokens)}</span>
            </div>
            <div className="context-budget-track" aria-label={`${inspection.budget.usage_percent.toFixed(1)} percent context used`}>
              {budgetRows.filter(row => row.value > 0).map(row => (
                <span key={row.id} title={`${row.label}: ${row.value} tokens`} style={{ width: `${safePercent(row.value / inspection.working_context_tokens * 100)}%`, background: row.colour }} />
              ))}
            </div>
            <div className="context-budget-legend">
              {budgetRows.map(row => <div key={row.id}><i style={{ background: row.colour }} /><span>{row.label}</span><strong>{compactNumber(row.value)}</strong></div>)}
            </div>
            <p className="context-budget-note">Total preflight is a conservative chat-template estimate, so it can be higher than the simple category sum.</p>
          </article>

          <article className="context-panel context-synopsis">
            <div className="context-panel-head"><div><span className="context-eyebrow">CODE-OWNED SYNOPSIS</span><h2>What the next task receives</h2></div><span>{new Date(inspection.generated_at).toLocaleTimeString()}</span></div>
            <p>{inspection.synopsis}</p>
            <div className="context-meta-strip">
              <span>Profile <strong>{inspection.profile_name}</strong></span>
              <span>Model <strong>{inspection.model_id}</strong></span>
              <span>Packet cap <strong>{compactNumber(inspection.packet_limit_tokens)} tokens</strong></span>
            </div>
            {inspection.tool_names.length > 0 && (
              <div className="context-tool-list" title={`Tool schema ${inspection.tool_schema_sha256 || 'not available'}`}>
                {inspection.tool_names.map(tool => <span key={tool}>{tool}</span>)}
              </div>
            )}
          </article>

          {inspection.warnings.length > 0 && (
            <div className="context-warning-list">
              {inspection.warnings.map((warning, index) => <div key={`${warning}-${index}`}>{warning}</div>)}
            </div>
          )}

          <article className="context-panel">
            <div className="context-panel-head">
              <div><span className="context-eyebrow">INCLUDED</span><h2>Trusted sources</h2></div>
              <span>{inspection.sources.length} selected</span>
            </div>
            {inspection.sources.length === 0 ? (
              <div className="context-empty">No project documents are injected for this packet.</div>
            ) : (
              <div className="context-source-list">
                {inspection.sources.map(source => (
                  <div className="context-source-card" key={source.path}>
                    <div className="context-source-main">
                      <div className="context-source-title"><strong>{source.display_path}</strong>{source.partial && <span>bounded excerpt</span>}</div>
                      <p>{source.reason}</p>
                      <div className="context-source-facts">
                        <span>{compactNumber(source.estimated_tokens)} tokens</span>
                        <span>{formatBytes(source.prompt_bytes)} in packet</span>
                        <span>{formatBytes(source.source_bytes)} source</span>
                        <span>lines {formatRanges(source.excerpt_ranges)}</span>
                      </div>
                      <code title={source.sha256}>sha256 {shortHash(source.sha256)}</code>
                    </div>
                    <button onClick={() => void openPath(source.path)}>Open source</button>
                  </div>
                ))}
              </div>
            )}
          </article>

          <article className="context-panel">
            <div className="context-panel-head">
              <div><span className="context-eyebrow">NOT SENT</span><h2>Excluded sources</h2></div>
              <span>{inspection.excluded_sources.length} omitted</span>
            </div>
            <div className="context-exclusion-list">
              {inspection.excluded_sources.map(source => (
                <div key={source.path} className={source.large ? 'large' : ''}>
                  <div><strong>{source.display_path}</strong><span>{source.reason}</span></div>
                  <span>{formatBytes(source.source_bytes)}{source.large ? ' · large' : ''}</span>
                </div>
              ))}
            </div>
          </article>

          <article className="context-panel context-manifest-panel">
            <div>
              <span className="context-eyebrow">ROUTING EVIDENCE</span>
              <h2>Manifest provenance</h2>
              <p>{inspection.fallback_reason || 'Manifest validated. Documentation routing cannot alter tools, scope, approvals, protected paths, or evidence gates.'}</p>
              <code title={inspection.manifest_sha256}>sha256 {shortHash(inspection.manifest_sha256)}</code>
            </div>
            {inspection.manifest_path && <button onClick={() => void openPath(inspection.manifest_path!)}>Open manifest</button>}
          </article>
        </>
      )}
    </section>
  )
}
