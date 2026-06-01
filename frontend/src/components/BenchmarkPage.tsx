import { useEffect, useMemo, useState } from 'react'
import {
  BenchmarkProfile,
  ClearBenchmarkRuns,
  GetProfiles,
  ListBenchmarkRuns,
  UpdateProfiles,
  type ProfileBenchmarkResult,
  type ProfilesFile,
} from '../wailsjs/go'
import './BenchmarkPage.css'

export function BenchmarkPage({ version = 0, onProfilesChanged }: { version?: number; onProfilesChanged?: () => void }) {
  const [profilesFile, setProfilesFile] = useState<ProfilesFile | null>(null)
  const [selectedProfile, setSelectedProfile] = useState('')
  const [runs, setRuns] = useState<ProfileBenchmarkResult[]>([])
  const [running, setRunning] = useState(false)
  const [status, setStatus] = useState('')

  const load = async () => {
    const [pf, history] = await Promise.all([
      GetProfiles(),
      ListBenchmarkRuns().catch(() => [] as ProfileBenchmarkResult[]),
    ])
    setProfilesFile(pf)
    setRuns(history)
    const names = Object.keys(pf.profiles ?? {}).filter(name => Boolean(pf.profiles[name]?.model_id?.trim()))
    setSelectedProfile(prev => prev && pf.profiles[prev] ? prev : names[0] ?? '')
  }

  useEffect(() => { void load() }, [version])

  const profileNames = useMemo(
    () => Object.keys(profilesFile?.profiles ?? {}).filter(name => Boolean(profilesFile?.profiles[name]?.model_id?.trim())),
    [profilesFile],
  )
  const selected = profilesFile?.profiles[selectedProfile]
  const provider = selected ? profilesFile?.providers[selected.provider] : undefined

  const runBenchmark = async () => {
    if (!selected || !provider || !selectedProfile) return
    setRunning(true)
    setStatus('Running benchmark suite...')
    try {
      const result = await BenchmarkProfile({ ...selected, name: selectedProfile }, provider)
      setRuns(items => [result, ...items])
      setStatus(result.summary)
    } catch (e) {
      setStatus(`Benchmark failed: ${e}`)
    } finally {
      setRunning(false)
    }
  }

  const contextCandidates = useMemo(() => {
    const current = selected?.ctx_tokens || 0
    const values = [32768, 65536, 98304, 120000, current]
      .filter(value => value > 0)
      .filter(value => current <= 0 || value <= Math.max(current, 65536))
    return [...new Set(values)].sort((a, b) => a - b)
  }, [selected?.ctx_tokens])

  const runContextSweep = async () => {
    if (!selected || !provider || !selectedProfile) return
    setRunning(true)
    setStatus(`Running context sweep: ${contextCandidates.map(v => `${Math.round(v / 1024)}k`).join(', ')}...`)
    const newRuns: ProfileBenchmarkResult[] = []
    try {
      for (const ctx of contextCandidates) {
        setStatus(`Benchmarking ${selectedProfile} at ${ctx.toLocaleString()} context...`)
        const result = await BenchmarkProfile({
          ...selected,
          name: `${selectedProfile}@${Math.round(ctx / 1024)}k`,
          ctx_tokens: ctx,
        }, provider)
        newRuns.push(result)
        setRuns(items => [result, ...items])
      }
      const bestBalanced = pickBest(newRuns.filter(run => (run.context_tier ?? '') !== 'ceiling'))
      const bestCeiling = pickBest(newRuns.filter(run => (run.context_tier ?? '') === 'ceiling' || (run.ctx_tokens ?? 0) === Math.max(...newRuns.map(run => run.ctx_tokens ?? 0))))
      setStatus(`Sweep complete. Best daily/large-code: ${describeRun(bestBalanced)}. Max-context candidate: ${describeRun(bestCeiling)}.`)
    } catch (e) {
      setStatus(`Context sweep failed: ${e}`)
    } finally {
      setRunning(false)
    }
  }

  const applyRun = async (run: ProfileBenchmarkResult) => {
    if (!profilesFile || !run.profile_name || !run.recommended_profile) return
    const next = {
      ...profilesFile,
      profiles: {
        ...profilesFile.profiles,
        [run.profile_name]: { ...run.recommended_profile, name: run.profile_name },
      },
    }
    await UpdateProfiles(next)
    setProfilesFile(next)
    setStatus(`Applied settings to ${run.profile_name}`)
    onProfilesChanged?.()
  }

  const clearRuns = async () => {
    await ClearBenchmarkRuns()
    setRuns([])
    setStatus('Benchmark history cleared')
  }

  return (
    <div className="benchmark-page">
      <header className="benchmark-header">
        <div>
          <h1>Benchmarks</h1>
          <p>Compare model speed, context, output cleanliness, and tool protocol reliability.</p>
        </div>
        <div className="benchmark-actions">
          <select value={selectedProfile} onChange={e => setSelectedProfile(e.target.value)}>
            {profileNames.map(name => <option key={name} value={name}>{name}</option>)}
          </select>
          <button className="primary" onClick={() => void runBenchmark()} disabled={running || !selected || !provider}>
            {running ? 'Running...' : 'Run Benchmark'}
          </button>
          <button onClick={() => void runContextSweep()} disabled={running || !selected || !provider}>
            Context Sweep
          </button>
          <button onClick={() => void load()} disabled={running}>Refresh</button>
          <button className="danger" onClick={() => void clearRuns()} disabled={running || runs.length === 0}>Clear</button>
        </div>
      </header>

      {selected && (
        <div className="benchmark-current">
          <Metric label="Model" value={selected.model_id} />
          <Metric label="Provider" value={selected.provider} />
          <Metric label="Context" value={`${selected.ctx_tokens || 0}`} />
          <Metric label="Sweep" value={contextCandidates.map(v => `${Math.round(v / 1024)}k`).join(' / ')} />
          <Metric label="Thinking" value={selected.thinking ? 'on' : 'off'} />
        </div>
      )}
      {status && <div className="benchmark-status">{status}</div>}

      <div className="benchmark-table">
        <div className="benchmark-row benchmark-row-head">
          <span>Run</span>
          <span>Model</span>
          <span>Context</span>
          <span>Tier</span>
          <span>Average</span>
          <span>Score</span>
          <span>Scenarios</span>
          <span>Action</span>
        </div>
        {runs.length === 0 ? (
          <div className="benchmark-empty">No benchmark runs yet.</div>
        ) : runs.map(run => (
          <div key={run.id || `${run.created_at}-${run.model_id}`} className={`benchmark-row benchmark-run benchmark-${run.status}`}>
            <span>
              <strong>{run.profile_name || 'profile'}</strong>
              <small>{formatDate(run.created_at)}</small>
            </span>
            <span title={run.model_id}>{run.model_id}</span>
            <span>{run.ctx_tokens || run.recommended_profile?.ctx_tokens || 0}</span>
            <span>{run.context_role || run.context_tier || 'n/a'}</span>
            <span>{run.tokens_per_second ? `${run.tokens_per_second.toFixed(1)} tok/s` : 'n/a'}</span>
            <span>{typeof run.score === 'number' ? `${run.score}/100` : 'n/a'}</span>
            <span className="scenario-list">
              {(run.scenarios ?? []).map(sc => (
                <span key={sc.name} className={`scenario-pill scenario-${sc.status}`} title={sc.summary}>
                  {sc.name}: {sc.tokens_per_second ? sc.tokens_per_second.toFixed(1) : '0.0'}
                  {sc.output_leak ? ' leak' : ''}
                  {sc.expected_json ? (sc.valid_json ? ' json' : ' bad-json') : ''}
                  {sc.structured_tools || sc.repaired_tools ? ` tools ${sc.structured_tools ?? 0}/${sc.repaired_tools ?? 0}` : ''}
                </span>
              ))}
            </span>
            <span><button onClick={() => void applyRun(run)}>Apply</button></span>
          </div>
        ))}
      </div>
    </div>
  )
}

function pickBest(runs: ProfileBenchmarkResult[]) {
  if (runs.length === 0) return undefined
  return [...runs].sort((a, b) => {
    const scoreDelta = (b.score ?? 0) - (a.score ?? 0)
    if (scoreDelta !== 0) return scoreDelta
    return (b.tokens_per_second ?? 0) - (a.tokens_per_second ?? 0)
  })[0]
}

function describeRun(run?: ProfileBenchmarkResult) {
  if (!run) return 'n/a'
  const ctx = run.ctx_tokens ? `${Math.round(run.ctx_tokens / 1024)}k` : 'unknown ctx'
  const speed = run.tokens_per_second ? `${run.tokens_per_second.toFixed(1)} tok/s` : 'n/a'
  const score = typeof run.score === 'number' ? `${run.score}/100` : 'n/a'
  return `${ctx} (${score}, ${speed})`
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="benchmark-metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

function formatDate(value?: string) {
  if (!value) return ''
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
}
