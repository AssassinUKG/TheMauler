import { useEffect, useMemo, useRef } from 'react'
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

  useEffect(() => {
    if (!visible) return
    const node = scrollRef.current
    if (!node) return
    node.scrollTop = node.scrollHeight
  }, [activity, streamBuffer, thinkingBuffer, visible])

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
            <span>Current Action</span>
            <strong>{currentActionLabel(runState, activeTool, streaming)}</strong>
            <p>{currentActionDetail(runState, activeTool, streamBuffer)}</p>
          </div>
          {lastTool && (
            <div className={`stream-now-side ${lastTool.status}`}>
              <span>Last Tool</span>
              <strong>{lastTool.name} · {lastTool.status}</strong>
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

function currentActionLabel(runState: RunStatePayload | null, activeTool: AgentActivity | null, streaming: boolean): string {
  if (activeTool) return `Running ${activeTool.name}`
  if (runState?.state) return formatState(runState.state)
  return streaming ? 'Working' : 'Idle'
}

function currentActionDetail(runState: RunStatePayload | null, activeTool: AgentActivity | null, streamBuffer: string): string {
  if (activeTool?.input) return compactOneLine(activeTool.input, 180)
  if (runState?.detail) return runState.detail
  if (streamBuffer.trim()) return compactOneLine(streamBuffer, 180)
  return 'Waiting for the next model or tool event.'
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

function remainingSeconds(deadline: number): number {
  return Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
}
