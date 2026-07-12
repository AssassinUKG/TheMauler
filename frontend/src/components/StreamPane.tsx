import { useEffect, useMemo, useRef, useState } from 'react'
import type { AgentActivity, RunStatePayload, ToolCountdown } from '../App'
import './StreamPane.css'

interface Props {
  visible: boolean
  streaming: boolean
  streamBuffer: string
  thinkingBuffer: string
  runState: RunStatePayload | null
  activity: AgentActivity[]
  toolCountdown: ToolCountdown | null
  onStopAgent: () => void
}

export function StreamPane({
  visible,
  streaming,
  streamBuffer,
  thinkingBuffer,
  runState,
  activity,
  toolCountdown,
  onStopAgent,
}: Props) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const recentActivity = useMemo(() => activity.slice(0, 10), [activity])
  const activeTool = useMemo(() => activity.find(item => item.status === 'running') ?? null, [activity])
  const lastTool = useMemo(() => activity.find(item => item.status !== 'running') ?? null, [activity])
  const [streamStartedAt, setStreamStartedAt] = useState<number | null>(null)
  const [clockNow, setClockNow] = useState(Date.now())
  const statusLines = useMemo(
    () => buildStatusLines(runState, activeTool, lastTool, streamBuffer, streaming, streamStartedAt, clockNow),
    [runState, activeTool, lastTool, streamBuffer, streaming, streamStartedAt, clockNow],
  )

  useEffect(() => {
    if (!visible) return
    const node = scrollRef.current
    if (!node) return
    node.scrollTop = node.scrollHeight
  }, [activity, streamBuffer, thinkingBuffer, visible])

  useEffect(() => {
    if (streaming) {
      setStreamStartedAt(current => current ?? Date.now())
      setClockNow(Date.now())
      const timer = window.setInterval(() => setClockNow(Date.now()), 1000)
      return () => window.clearInterval(timer)
    }
    setStreamStartedAt(null)
    setClockNow(Date.now())
    return undefined
  }, [streaming])

  return (
    <div className="stream-pane" style={{ display: visible ? 'flex' : 'none' }}>
      <div className="stream-header">
        <div>
          <span className="stream-title">Stream</span>
          <span className={`stream-state ${streaming ? 'active' : ''}`}>
            {formatState(runState?.state || (streaming ? 'thinking' : 'idle'))}
          </span>
        </div>
        <div className="stream-header-actions">
          {toolCountdown && (
            <span className="stream-countdown">
              {toolCountdown.name} {remainingSeconds(toolCountdown.deadline)}s
            </span>
          )}
          <button className="stream-btn stream-btn-danger" onClick={onStopAgent} disabled={!streaming}>
            Stop
          </button>
        </div>
      </div>

      <div className="stream-body" ref={scrollRef}>
        <section className="stream-now">
          <div className="stream-now-main">
            <div className="stream-status-title">{statusLines.title}</div>
            <div className="stream-status-lines">
              {statusLines.rows.map(row => (
                <div className="stream-status-line" key={row.label}>
                  <span>{row.label}</span>
                  <strong>{row.value}</strong>
                </div>
              ))}
            </div>
          </div>
          {lastTool && (
            <div className={`stream-now-side ${lastTool.status}`}>
              <span>Last Tool</span>
              <strong>{lastTool.name} - {lastTool.status}</strong>
              {lastTool.result && <p>{compactOneLine(lastTool.result, 140)}</p>}
            </div>
          )}
        </section>

        {activeTool && (
          <section className="stream-card stream-active-tool">
            <div className="stream-card-title">Running Tool</div>
            <div className="stream-tool-command">{activeTool.name}</div>
            {activeTool.input && <pre>{compactToolText(activeTool.input, 1800)}</pre>}
          </section>
        )}

        {thinkingBuffer.trim() && (
          <details className="stream-card stream-thinking" open={streaming}>
            <summary>Thinking</summary>
            <pre>{thinkingBuffer}</pre>
          </details>
        )}

        <section className="stream-card stream-response">
          <div className="stream-card-title">Assistant Text</div>
          {streamBuffer.trim() ? (
            <pre>{streamBuffer}</pre>
          ) : (
            <div className="stream-empty">{streaming ? 'No model text yet. Watch Current Action and Recent Tool Activity.' : 'No active stream.'}</div>
          )}
        </section>

        <section className="stream-card">
          <div className="stream-card-title">Recent Tool Activity</div>
          {recentActivity.length === 0 ? (
            <div className="stream-empty">No tool activity yet.</div>
          ) : (
            <div className="stream-tool-list">
              {recentActivity.map(item => (
                <details key={item.id} className={`stream-tool ${item.status}`} open={item.status === 'running'}>
                  <summary>
                    <span>{item.name}</span>
                    <strong>{item.status}</strong>
                  </summary>
                  {item.input && (
                    <>
                      <div className="stream-tool-label">Input</div>
                      <pre>{compactToolText(item.input, 2000)}</pre>
                    </>
                  )}
                  {item.result && (
                    <>
                      <div className="stream-tool-label">Result</div>
                      <pre>{compactToolText(item.result, 2400)}</pre>
                    </>
                  )}
                </details>
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  )
}

interface StatusLine {
  label: string
  value: string
}

function buildStatusLines(
  runState: RunStatePayload | null,
  activeTool: AgentActivity | null,
  lastTool: AgentActivity | null,
  streamBuffer: string,
  streaming: boolean,
  streamStartedAt: number | null,
  clockNow: number,
): { title: string; rows: StatusLine[] } {
  const elapsed = streaming && streamStartedAt ? formatElapsed(clockNow - streamStartedAt) : 'idle'
  const rows: StatusLine[] = [
    { label: 'Agent', value: humanRunState(runState?.state, streaming) },
    { label: 'Now', value: currentActionLabel(runState, activeTool, streaming) },
  ]
  const waiting = waitingReason(runState, activeTool, streaming)
  if (waiting) rows.push({ label: 'Waiting', value: waiting })
  const latest = currentActionDetail(runState, activeTool, lastTool, streamBuffer)
  if (latest) rows.push({ label: 'Latest', value: latest })
  return { title: streaming ? `Working - ${elapsed}` : 'Idle', rows }
}

function currentActionLabel(runState: RunStatePayload | null, activeTool: AgentActivity | null, streaming: boolean): string {
  if (activeTool) return describeToolAction(activeTool.name)
  if (runState?.detail) return compactOneLine(runState.detail, 120)
  if (runState?.state) return humanRunState(runState.state, streaming)
  return streaming ? 'starting the next step' : 'ready'
}

function currentActionDetail(
  runState: RunStatePayload | null,
  activeTool: AgentActivity | null,
  lastTool: AgentActivity | null,
  streamBuffer: string,
): string {
  if (activeTool?.input) return compactOneLine(activeTool.input, 160)
  if (runState?.detail) return compactOneLine(runState.detail, 160)
  if (streamBuffer.trim()) return compactOneLine(streamBuffer, 160)
  if (lastTool?.result) return compactOneLine(lastTool.result, 160)
  return 'Waiting for the next model or tool event.'
}

function waitingReason(runState: RunStatePayload | null, activeTool: AgentActivity | null, streaming: boolean): string {
  if (activeTool) return activeTool.name
  if (!streaming) return ''
  if (runState?.state === 'model_loading') return 'model response'
  if (runState?.state === 'testing') return 'verification result'
  if (runState?.state === 'recovering') return 'review or retry result'
  return ''
}

function describeToolAction(name: string): string {
  const lower = name.toLowerCase()
  if (lower === 'shell') return 'running shell command'
  if (lower === 'terminal_send' || lower === 'terminal_read' || lower === 'start_listener') return 'using terminal'
  if (lower === 'read' || lower === 'glob' || lower === 'grep' || lower === 'read_tool_result') return 'reading workspace'
  if (lower === 'write' || lower === 'edit' || lower === 'apply_patch') return 'editing files'
  if (lower === 'web_search' || lower === 'fetch_url' || lower === 'browser') return 'checking external context'
  if (lower === 'todo_write') return 'updating plan'
  if (lower === 'task') return 'running subtask'
  if (lower === 'memory' || lower === 'skill') return 'updating knowledge'
  return `using ${name}`
}

function humanRunState(state: string | undefined, streaming: boolean): string {
  if (!state || state === 'idle') return streaming ? 'starting' : 'idle'
  const labels: Record<string, string> = {
    planning: 'planning the run',
    model_loading: 'loading the model',
    thinking: 'thinking',
    researching: 'researching',
    reading: 'reading context',
    editing: 'editing files',
    testing: 'verifying the work',
    recovering: 'recovering',
    blocked: 'blocked',
    failed: 'failed',
    done: 'done',
  }
  return labels[state] ?? state.replaceAll('_', ' ')
}

function compactOneLine(text: string, max: number): string {
  const single = text.replace(/\s+/g, ' ').trim()
  if (single.length <= max) return single
  return `${single.slice(0, Math.max(0, max - 1))}...`
}

function compactToolText(text: string, max: number): string {
  const trimmed = text.trim()
  if (trimmed.length <= max) return trimmed
  return `${trimmed.slice(0, max)}\n...`
}

function formatState(state: string): string {
  if (!state || state === 'idle') return 'Idle'
  return state.replaceAll('_', ' ').replace(/\b\w/g, ch => ch.toUpperCase())
}

function formatElapsed(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (minutes === 0) return `${seconds}s`
  return `${minutes}m ${seconds.toString().padStart(2, '0')}s`
}

function remainingSeconds(deadline: number): number {
  return Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
}
