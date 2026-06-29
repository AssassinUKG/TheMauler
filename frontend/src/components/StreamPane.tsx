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
  const recentActivity = useMemo(() => activity.slice(-10).reverse(), [activity])

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
        {runState?.detail && (
          <section className="stream-card">
            <div className="stream-card-title">Current Phase</div>
            <div className="stream-phase">{runState.detail}</div>
          </section>
        )}

        {thinkingBuffer.trim() && (
          <details className="stream-card stream-thinking" open={streaming}>
            <summary>Thinking</summary>
            <pre>{thinkingBuffer}</pre>
          </details>
        )}

        <section className="stream-card stream-response">
          <div className="stream-card-title">Assistant Output</div>
          {streamBuffer.trim() ? (
            <pre>{streamBuffer}</pre>
          ) : (
            <div className="stream-empty">{streaming ? 'Waiting for model text or tool calls...' : 'No active stream.'}</div>
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
                      <pre>{item.input}</pre>
                    </>
                  )}
                  {item.result && (
                    <>
                      <div className="stream-tool-label">Result</div>
                      <pre>{item.result}</pre>
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

function formatState(state: string): string {
  if (!state || state === 'idle') return 'Idle'
  return state.replaceAll('_', ' ').replace(/\b\w/g, ch => ch.toUpperCase())
}

function remainingSeconds(deadline: number): number {
  return Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
}
