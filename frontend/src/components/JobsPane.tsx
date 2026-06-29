import type { BackgroundJob } from '../App'
import './JobsPane.css'

interface Props {
  visible: boolean
  jobs: BackgroundJob[]
  onClearCompleted: () => void
  onClearAll: () => void
}

export function JobsPane({ visible, jobs, onClearCompleted, onClearAll }: Props) {
  const running = jobs.filter(job => job.state !== 'done').length

  return (
    <div className="jobs-pane" style={{ display: visible ? 'flex' : 'none' }}>
      <div className="jobs-header">
        <div>
          <span className="jobs-title">Jobs</span>
          <span className="jobs-subtitle">{running} running / {jobs.length} tracked</span>
        </div>
        <div className="jobs-actions">
          <button onClick={onClearCompleted} disabled={!jobs.some(job => job.state === 'done')}>Clear Done</button>
          <button onClick={onClearAll} disabled={jobs.length === 0}>Clear All</button>
        </div>
      </div>

      <div className="jobs-body">
        {jobs.length === 0 ? (
          <div className="jobs-empty">No background jobs yet. Long scans started with background=true will appear here.</div>
        ) : (
          <div className="jobs-grid">
            {jobs.map(job => (
              <details key={job.id} className={`job-row ${job.state || 'unknown'}`} open={job.state !== 'done'}>
                <summary>
                  <span className="job-id">{job.id}</span>
                  <span className="job-state">{job.state || 'unknown'}</span>
                  <span className="job-command">{job.command || '(command unavailable)'}</span>
                  <span className="job-meta">{formatElapsed(job.elapsed_sec)}{job.next_poll_sec ? ` · next ${job.next_poll_sec}s` : ''}</span>
                </summary>
                <div className="job-detail">
                  <div className="job-facts">
                    <Fact label="Log" value={job.log || '-'} />
                    {job.state === 'done'
                      ? <Fact label="Exit" value={job.exit_code === undefined ? '-' : String(job.exit_code)} />
                      : <Fact label="PID" value={job.pidfile || '-'} />}
                    <Fact label="Verbose" value={job.verbose ? 'on' : 'off'} />
                    <Fact label="Updated" value={formatTime(job.updated_at_unix)} />
                  </div>
                  <pre>{job.output?.trim() || 'No output captured yet.'}</pre>
                </div>
              </details>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="job-fact">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function formatElapsed(seconds?: number): string {
  if (!seconds || seconds < 0) return '0s'
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${minutes}m ${rest}s`
}

function formatTime(ms?: number): string {
  if (!ms) return '-'
  return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
