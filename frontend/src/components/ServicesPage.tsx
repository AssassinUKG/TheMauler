import { useEffect, useState } from 'react'
import {
  GetBrowserWorkflowStatus,
  GetServiceHealth,
  ListBrowserCheckpoints,
  PauseBrowserWorkflow,
  RestartAudioWorker,
  ResumeBrowserWorkflow,
  ResumeBrowserWorkflowCheckpoint,
  SaveBrowserWorkflowCheckpoint,
  StartBrowserWorkflow,
  StopBrowserWorkflow,
  TakeOverBrowserWorkflow,
  type BrowserWorkflowStatus,
  type BrowserCheckpointStatus,
  type ServiceHealth,
} from '../wailsjs/go'
import type { BackgroundJob } from '../App'
import './ServicesPage.css'

interface Props { jobs: BackgroundJob[]; onOpenSettings: () => void; onOpenJobs: () => void }

export function ServicesPage({ jobs, onOpenSettings, onOpenJobs }: Props) {
  const [services, setServices] = useState<ServiceHealth[]>([])
  const [loading, setLoading] = useState(false)
  const [restartingAudio, setRestartingAudio] = useState(false)
  const [browser, setBrowser] = useState<BrowserWorkflowStatus | null>(null)
  const [browserURL, setBrowserURL] = useState('')
  const [browserBusy, setBrowserBusy] = useState(false)
  const [browserError, setBrowserError] = useState('')
  const [checkpoints, setCheckpoints] = useState<BrowserCheckpointStatus[]>([])
  const [checkpointName, setCheckpointName] = useState('')
  const refresh = async () => {
    setLoading(true)
    const [nextServices, nextBrowser, nextCheckpoints] = await Promise.all([
      GetServiceHealth().catch(() => []),
      GetBrowserWorkflowStatus().catch(() => null),
      ListBrowserCheckpoints().catch(() => []),
    ])
    setServices(nextServices)
    setBrowser(nextBrowser)
    setCheckpoints(nextCheckpoints)
    setLoading(false)
  }
  useEffect(() => {
    void refresh()
    const id = window.setInterval(() => { void refresh() }, 5000)
    return () => window.clearInterval(id)
  }, [])

  const restartAudio = async () => {
    setRestartingAudio(true)
    try {
      await RestartAudioWorker()
      await refresh()
    } finally {
      setRestartingAudio(false)
    }
  }

  const runBrowserAction = async (action: () => Promise<BrowserWorkflowStatus>) => {
    setBrowserBusy(true)
    setBrowserError('')
    try {
      const next = await action()
      setBrowser(next)
      if (next.url) setBrowserURL(next.url)
    } catch (error) {
      setBrowserError(error instanceof Error ? error.message : String(error))
      setBrowser(await GetBrowserWorkflowStatus().catch(() => browser))
    } finally {
      setBrowserBusy(false)
    }
  }

  const saveCheckpoint = async () => {
    const name = checkpointName.trim()
    if (!name) return
    setBrowserBusy(true)
    setBrowserError('')
    try {
      await SaveBrowserWorkflowCheckpoint(name)
      setCheckpointName('')
      setCheckpoints(await ListBrowserCheckpoints())
    } catch (error) {
      setBrowserError(error instanceof Error ? error.message : String(error))
    } finally {
      setBrowserBusy(false)
    }
  }

  return <div className="services-page">
    <header className="services-header"><div><span>Runtime control</span><h1>Services</h1><p>One view for the background systems that support agent work.</p></div><div><button onClick={() => void refresh()} disabled={loading}>{loading ? 'Checking…' : 'Refresh all'}</button><button onClick={onOpenSettings}>Settings</button></div></header>
    <div className="services-grid">
      {services.map(service => <article key={service.id} className={`service-card service-${service.status}`}>
        <div className="service-card-head"><div><span>{service.name}</span><strong>{service.status}</strong></div><i /></div>
        <p>{service.summary}</p>
        {service.detail && <div className="service-error-detail">{service.detail}</div>}
        {service.metadata && <dl>{orderedMetadata(service).map(([key, value]) => <div key={key}><dt>{key.replaceAll('_', ' ')}</dt><dd title={value}>{value}</dd></div>)}</dl>}
        <div className="service-actions">
          {service.id === 'audio' && <button onClick={() => void restartAudio()} disabled={restartingAudio}>{restartingAudio ? 'Restarting…' : 'Restart voice'}</button>}
          {service.id === 'jobs' && <button onClick={onOpenJobs}>Open jobs</button>}
          <small>{new Date(service.updated_at).toLocaleTimeString()}</small>
        </div>
      </article>)}
      {services.length === 0 && !loading && <div className="services-empty">Service status is unavailable.</div>}
    </div>
    <section className={`browser-workflow browser-workflow-${browser?.state ?? 'unknown'}`} aria-label="Browser workflow controls">
      <div className="browser-workflow-head">
        <div><span>Native browser workflow</span><h2>{browser?.title || 'Persistent visible session'}</h2></div>
        <strong>{browser?.state?.replaceAll('_', ' ') || 'checking'}</strong>
      </div>
      <p>{browser?.guidance || 'Checking Chrome or Edge readiness…'}</p>
      <div className="browser-location">
        <label htmlFor="browser-workflow-url">Site</label>
        <input
          id="browser-workflow-url"
          value={browserURL}
          onChange={event => setBrowserURL(event.target.value)}
          placeholder="https://example.com/login"
          disabled={browserBusy || Boolean(browser?.active)}
        />
        <button
          className="primary"
          onClick={() => void runBrowserAction(() => StartBrowserWorkflow(browserURL.trim()))}
          disabled={browserBusy || Boolean(browser?.active) || !browserURL.trim() || !browser?.available || !browser?.tool_enabled}
        >Open browser</button>
      </div>
      {browser?.url && <div className="browser-current"><span>Current page</span><strong title={browser.url}>{browser.url}</strong></div>}
      {browser?.last_action && <div className="browser-current"><span>Last meaningful action</span><strong>{browser.last_action.replaceAll('_', ' ')}</strong></div>}
      {browser?.active && <div className="browser-current"><span>Tabs</span><strong>{browser.active_tab || 't1'} · {browser.tab_count || 1} open</strong></div>}
      {(browserError || browser?.last_error) && <div className="browser-workflow-error" role="alert">{browserError || browser?.last_error}</div>}
      <div className="browser-workflow-actions">
        <button onClick={() => void runBrowserAction(PauseBrowserWorkflow)} disabled={browserBusy || !browser?.active || browser?.paused}>Pause</button>
        <button onClick={() => void runBrowserAction(TakeOverBrowserWorkflow)} disabled={browserBusy || !browser?.active || browser?.paused || !browser?.visible}>Take over</button>
        <button className="primary" onClick={() => void runBrowserAction(ResumeBrowserWorkflow)} disabled={browserBusy || !browser?.active || !browser?.paused}>I’ve completed this—continue</button>
        <button className="danger" onClick={() => void runBrowserAction(StopBrowserWorkflow)} disabled={browserBusy || !browser?.active}>Stop</button>
        <small>{browser?.visible ? 'Visible browser' : browser?.active ? 'Headless browser' : 'No browser session'} · cookies remain only for this conversation</small>
      </div>
      <div className="browser-checkpoints">
        <div className="browser-checkpoint-copy"><strong>Safe checkpoints</strong><span>Stores URL/title only—never cookies, credentials, form values, query tokens, or fragments.</span></div>
        <div className="browser-checkpoint-create">
          <input value={checkpointName} onChange={event => setCheckpointName(event.target.value)} placeholder="checkpoint-name" disabled={browserBusy || !browser?.active} />
          <button onClick={() => void saveCheckpoint()} disabled={browserBusy || !browser?.active || !checkpointName.trim()}>Save checkpoint</button>
        </div>
        {checkpoints.length > 0 && <div className="browser-checkpoint-list">{checkpoints.map(checkpoint => <button key={checkpoint.name} onClick={() => void runBrowserAction(() => ResumeBrowserWorkflowCheckpoint(checkpoint.name))} disabled={browserBusy} title={checkpoint.url}><strong>{checkpoint.name}</strong><span>{checkpoint.title || checkpoint.url}</span></button>)}</div>}
      </div>
    </section>
    {jobs.length > 0 && <div className="services-live-note"><strong>{jobs.length} UI-tracked job{jobs.length === 1 ? '' : 's'}</strong><span>Open the Jobs panel for output and controls.</span><button onClick={onOpenJobs}>View jobs</button></div>}
  </div>
}

function orderedMetadata(service: ServiceHealth): Array<[string, string]> {
  if (!service.metadata) return []
  const entries = Object.entries(service.metadata).filter(([, value]) => value && value !== '0')
  if (service.id !== 'audio') return entries.slice(0, 5)
  const order = ['voice', 'tts_state', 'tts_worker_pid', 'stt_state', 'stt_model', 'stt_worker_pid']
  return entries.sort(([a], [b]) => order.indexOf(a) - order.indexOf(b)).slice(0, 6)
}
