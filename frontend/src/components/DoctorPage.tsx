import { useEffect, useMemo, useState } from 'react'
import { RunDoctor, type DoctorResult } from '../wailsjs/go'
import './DoctorPage.css'

interface Props {
  runRequest: number
}

export function DoctorPage({ runRequest }: Props) {
  const [result, setResult] = useState<DoctorResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')

  const counts = useMemo(() => {
    const next = { ok: 0, warn: 0, fail: 0, info: 0 }
    for (const check of result?.checks ?? []) {
      if (check.status === 'ok') next.ok += 1
      else if (check.status === 'warn') next.warn += 1
      else if (check.status === 'fail') next.fail += 1
      else next.info += 1
    }
    return next
  }, [result])

  const run = async () => {
    setRunning(true)
    setError('')
    try {
      setResult(await RunDoctor())
    } catch (e) {
      setError(String(e))
    } finally {
      setRunning(false)
    }
  }

  useEffect(() => {
    if (runRequest > 0) void run()
  }, [runRequest])

  return (
    <section className="doctor-page">
      <header className="doctor-page-head">
        <div>
          <span>Diagnostics</span>
          <h1>Doctor</h1>
          <p>Provider, model, context, terminal, tools, memory, and runtime checks.</p>
        </div>
        <button className="doctor-run-button" onClick={() => void run()} disabled={running}>
          {running ? 'Running...' : result ? 'Run Again' : 'Run Doctor'}
        </button>
      </header>

      {error && <div className="doctor-error">{error}</div>}

      {result ? (
        <>
          <div className="doctor-score-row">
            <div className={`doctor-score doctor-score-${result.grade.toLowerCase()}`}>
              <span>Grade</span>
              <strong>{result.grade}</strong>
              <small>{result.score}/100</small>
            </div>
            <Metric label="OK" value={counts.ok} tone="ok" />
            <Metric label="Warn" value={counts.warn} tone="warn" />
            <Metric label="Fail" value={counts.fail} tone="fail" />
            <Metric label="Info" value={counts.info} tone="info" />
          </div>

          <div className="doctor-check-list">
            {result.checks.map((check, index) => (
              <article key={`${check.name}-${index}`} className={`doctor-check-card doctor-check-${check.status}`}>
                <div className="doctor-check-mark">{doctorMark(check.status)}</div>
                <div className="doctor-check-main">
                  <div className="doctor-check-title">
                    <strong>{check.name}</strong>
                    <span>{check.status}</span>
                  </div>
                  <p>{check.message}</p>
                  {check.detail && <pre>{check.detail}</pre>}
                </div>
              </article>
            ))}
          </div>
        </>
      ) : (
        <div className="doctor-empty">
          <strong>No Doctor result yet</strong>
          <span>Run diagnostics to check the current local model, provider, context, terminal, tools, memory, and runtime configuration.</span>
        </div>
      )}
    </section>
  )
}

function Metric({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className={`doctor-metric doctor-metric-${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function doctorMark(status: string) {
  switch (status) {
    case 'ok':
      return 'OK'
    case 'warn':
      return '!'
    case 'fail':
      return 'X'
    default:
      return 'i'
  }
}
