import { useEffect, useState } from 'react'
import { GetServiceHealth, RestartAudioWorker, type ServiceHealth } from '../wailsjs/go'
import type { BackgroundJob } from '../App'
import './ServicesPage.css'

interface Props { jobs: BackgroundJob[]; onOpenSettings: () => void; onOpenJobs: () => void }

export function ServicesPage({ jobs, onOpenSettings, onOpenJobs }: Props) {
  const [services, setServices] = useState<ServiceHealth[]>([])
  const [loading, setLoading] = useState(false)
  const [restartingAudio, setRestartingAudio] = useState(false)
  const refresh = async () => {
    setLoading(true)
    setServices(await GetServiceHealth().catch(() => []))
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
