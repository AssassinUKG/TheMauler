import { useEffect, useMemo, useState } from 'react'
import {
  BenchmarkProfileWithCases,
  ClearBenchmarkRuns,
  GetProfiles,
  ListBenchmarkRuns,
  UpdateProfiles,
  type BenchmarkSpecInput,
  type ProfileBenchmarkResult,
  type ProfilesFile,
} from '../wailsjs/go'
import './BenchmarkPage.css'

export function BenchmarkPage({ version = 0, onProfilesChanged }: { version?: number; onProfilesChanged?: () => void }) {
  const [profilesFile, setProfilesFile] = useState<ProfilesFile | null>(null)
  const [selectedProfile, setSelectedProfile] = useState('')
  const [draftProfile, setDraftProfile] = useState<ProfilesFile['profiles'][string] | null>(null)
  const [runs, setRuns] = useState<ProfileBenchmarkResult[]>([])
  const [running, setRunning] = useState(false)
  const [status, setStatus] = useState('')
  const [expandedRun, setExpandedRun] = useState('')
  const [scenarioDrafts, setScenarioDrafts] = useState<BenchmarkSpecInput[]>(defaultBenchmarkScenarios())

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
  const activeProfile = draftProfile ?? selected
  const provider = activeProfile ? profilesFile?.providers[activeProfile.provider] : undefined

  useEffect(() => {
    setDraftProfile(selected ? { ...selected, name: selectedProfile, ctx_tokens: Math.min(selected.ctx_tokens || 8192, 8192) } : null)
  }, [selectedProfile, selected])

  const runBenchmark = async () => {
    if (!activeProfile || !provider || !selectedProfile) return
    setRunning(true)
    setStatus('Running benchmark suite...')
    try {
      const result = await BenchmarkProfileWithCases({ ...activeProfile, name: selectedProfile }, provider, scenarioDrafts)
      setRuns(items => [result, ...items])
      setStatus(result.summary)
    } catch (e) {
      setStatus(`Benchmark failed: ${e}`)
    } finally {
      setRunning(false)
    }
  }

  const contextCandidates = useMemo(() => {
    const current = activeProfile?.ctx_tokens || 0
    const values = [8192, 16384, 32768, 65536, 98304, 120000, current]
      .filter(value => value > 0)
      .filter(value => current <= 0 || value <= Math.max(current, 65536))
    return [...new Set(values)].sort((a, b) => a - b)
  }, [activeProfile?.ctx_tokens])

  const runContextSweep = async () => {
    if (!activeProfile || !provider || !selectedProfile) return
    setRunning(true)
    setStatus(`Running context sweep: ${contextCandidates.map(v => `${Math.round(v / 1024)}k`).join(', ')}...`)
    const newRuns: ProfileBenchmarkResult[] = []
    try {
      for (const ctx of contextCandidates) {
        setStatus(`Benchmarking ${selectedProfile} at ${ctx.toLocaleString()} context...`)
        const result = await BenchmarkProfileWithCases({
          ...activeProfile,
          name: `${selectedProfile}@${Math.round(ctx / 1024)}k`,
          ctx_tokens: ctx,
        }, provider, scenarioDrafts)
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
    const current = profilesFile.profiles[run.profile_name]
    const changes = profileChanges(current, run.recommended_profile)
    const ok = confirm(`Apply benchmark recommendation to ${run.profile_name}?\n\nThis overwrites profile settings:\n${changes.length ? changes.join('\n') : 'No setting changes detected.'}`)
    if (!ok) return
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

  const saveDraftProfile = async () => {
    if (!profilesFile || !draftProfile || !selectedProfile) return
    const next = {
      ...profilesFile,
      profiles: {
        ...profilesFile.profiles,
        [selectedProfile]: { ...draftProfile, name: selectedProfile },
      },
    }
    await UpdateProfiles(next)
    setProfilesFile(next)
    setStatus(`Saved run draft to profile ${selectedProfile}`)
    onProfilesChanged?.()
  }

  const updateScenario = (index: number, patch: Partial<BenchmarkSpecInput>) => {
    setScenarioDrafts(items => items.map((item, i) => i === index ? { ...item, ...patch } : item))
  }

  const addScenario = () => {
    setScenarioDrafts(items => [...items, {
      name: `Custom ${items.length + 1}`,
      system: 'You are a concise local assistant.',
      user: 'Answer in one short paragraph.',
      max_tokens: 128,
      temperature: 0,
      top_p: 1,
      top_k: 1,
      min_p: 0,
      presence_penalty: 0,
      seed: 1,
      expect_json: false,
      tool_mode: 'none',
    }])
  }

  const removeScenario = (index: number) => {
    setScenarioDrafts(items => items.length <= 1 ? items : items.filter((_, i) => i !== index))
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

      {activeProfile && (
        <div className="benchmark-current">
          <Metric label="Model" value={activeProfile.model_id} />
          <Metric label="Provider" value={activeProfile.provider} />
          <Metric label="Run ctx" value={`${activeProfile.ctx_tokens || 0}`} />
          <Metric label="Sweep" value={contextCandidates.map(v => `${Math.round(v / 1024)}k`).join(' / ')} />
          <Metric label="Thinking" value={activeProfile.thinking ? 'on' : 'off'} />
        </div>
      )}
      {draftProfile && (
        <div className="benchmark-editor">
          <label>
            <span>Model ID</span>
            <input value={draftProfile.model_id} onChange={e => setDraftProfile({ ...draftProfile, model_id: e.target.value })} />
          </label>
          <label>
            <span>Context</span>
            <input
              type="number"
              min={1024}
              step={1024}
              value={draftProfile.ctx_tokens || 0}
              onChange={e => setDraftProfile({ ...draftProfile, ctx_tokens: parseInt(e.target.value, 10) || 0 })}
            />
          </label>
          <label className="benchmark-check">
            <input type="checkbox" checked={draftProfile.thinking} onChange={e => setDraftProfile({ ...draftProfile, thinking: e.target.checked })} />
            <span>Thinking</span>
          </label>
          <label className="benchmark-check">
            <input type="checkbox" checked={draftProfile.preserve_thinking} onChange={e => setDraftProfile({ ...draftProfile, preserve_thinking: e.target.checked })} />
            <span>Preserve thinking</span>
          </label>
          <label>
            <span>MTP</span>
            <select value={draftProfile.spec_type || ''} onChange={e => setDraftProfile({ ...draftProfile, spec_type: e.target.value })}>
              <option value="">off</option>
              <option value="draft-mtp">draft-mtp</option>
            </select>
          </label>
          <label>
            <span>Draft n</span>
            <input
              type="number"
              min={0}
              max={8}
              value={draftProfile.spec_draft_n_max || 0}
              onChange={e => setDraftProfile({ ...draftProfile, spec_draft_n_max: parseInt(e.target.value, 10) || 0 })}
            />
          </label>
          <button onClick={() => void saveDraftProfile()} disabled={running}>Save to Profile</button>
        </div>
      )}
      <div className="benchmark-scenario-editor">
        <div className="benchmark-editor-head">
          <div>
            <strong>Run Scenarios</strong>
            <span>Editable scratch tests sent to the benchmark run. Tool mode checks formatting only; it does not execute tools.</span>
          </div>
          <button onClick={addScenario} disabled={running}>Add Scenario</button>
        </div>
        {scenarioDrafts.map((scenario, index) => (
          <details key={`${scenario.name}-${index}`} className="benchmark-scenario-edit" open={index < 2}>
            <summary>
              <span>{scenario.name || `Scenario ${index + 1}`}</span>
              <small>{scenario.max_tokens} max · temp {scenario.temperature} · tools {scenario.tool_mode}</small>
            </summary>
            <div className="benchmark-scenario-form">
              <label><span>Name</span><input value={scenario.name} onChange={e => updateScenario(index, { name: e.target.value })} /></label>
              <label><span>Max tokens</span><input type="number" min={16} max={8192} value={scenario.max_tokens} onChange={e => updateScenario(index, { max_tokens: parseInt(e.target.value, 10) || 128 })} /></label>
              <label><span>Temperature</span><input type="number" min={0} max={2} step={0.05} value={scenario.temperature} onChange={e => updateScenario(index, { temperature: parseFloat(e.target.value) || 0 })} /></label>
              <label><span>Top P</span><input type="number" min={0} max={1} step={0.05} value={scenario.top_p} onChange={e => updateScenario(index, { top_p: parseFloat(e.target.value) || 1 })} /></label>
              <label><span>Top K</span><input type="number" min={0} max={200} value={scenario.top_k} onChange={e => updateScenario(index, { top_k: parseInt(e.target.value, 10) || 1 })} /></label>
              <label><span>Tool mode</span><select value={scenario.tool_mode} onChange={e => updateScenario(index, { tool_mode: e.target.value })}><option value="none">none</option><option value="auto">auto</option><option value="required">required</option></select></label>
              <label className="benchmark-check"><input type="checkbox" checked={scenario.expect_json} onChange={e => updateScenario(index, { expect_json: e.target.checked })} /><span>Expect JSON</span></label>
              <button onClick={() => removeScenario(index)} disabled={running || scenarioDrafts.length <= 1}>Remove</button>
              <label className="wide"><span>System</span><textarea value={scenario.system} onChange={e => updateScenario(index, { system: e.target.value })} /></label>
              <label className="wide"><span>User</span><textarea value={scenario.user} onChange={e => updateScenario(index, { user: e.target.value })} /></label>
            </div>
          </details>
        ))}
      </div>
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
        ) : runs.map(run => {
          const key = run.id || `${run.created_at}-${run.model_id}`
          const expanded = expandedRun === key
          return (
            <div key={key} className={`benchmark-run-block benchmark-${run.status}`}>
              <div className="benchmark-row benchmark-run">
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
                    <span key={sc.name} className={`scenario-pill scenario-${sc.status}`} title={`${scenarioPurpose(sc.name)} ${sc.summary}`}>
                      {sc.name}: {sc.tokens_per_second ? sc.tokens_per_second.toFixed(1) : '0.0'}
                      {sc.output_leak ? ' leak' : ''}
                      {sc.expected_json ? (sc.valid_json ? ' json' : ' bad-json') : ''}
                      {sc.structured_tools || sc.repaired_tools ? ` tools ${sc.structured_tools ?? 0}/${sc.repaired_tools ?? 0}` : ''}
                    </span>
                  ))}
                </span>
                <span className="benchmark-row-actions">
                  <button onClick={() => setExpandedRun(expanded ? '' : key)}>{expanded ? 'Hide' : 'Details'}</button>
                  <button onClick={() => void applyRun(run)}>Apply</button>
                </span>
              </div>
              {expanded && (
                <div className="benchmark-details">
                  <div className="benchmark-detail-grid">
                    {(run.scenarios ?? []).map(sc => (
                      <div key={sc.name} className={`benchmark-detail-card scenario-${sc.status}`}>
                        <div className="benchmark-detail-title">{sc.name}</div>
                        <div className="benchmark-detail-purpose">{scenarioPurpose(sc.name)}</div>
                        <div className="benchmark-detail-summary">{sc.summary || 'No summary'}</div>
                        <div className="benchmark-detail-metrics">
                          TTFT {sc.ttf_ms ?? 0} ms · {sc.tokens_per_second ? sc.tokens_per_second.toFixed(1) : '0.0'} tok/s · {sc.completion_tokens ?? 0} out
                        </div>
                        {sc.name === 'Tool protocol' && (
                          <div className="benchmark-detail-metrics">
                            Structured calls {sc.structured_tools ?? 0}; repairable inline calls {sc.repaired_tools ?? 0}
                          </div>
                        )}
                        {sc.error && <pre>{sc.error}</pre>}
                      </div>
                    ))}
                  </div>
                  <div className="benchmark-apply-preview">
                    <strong>Apply would change</strong>
                    <ul>
                      {profileChanges(profilesFile?.profiles[run.profile_name ?? ''], run.recommended_profile).map(change => (
                        <li key={change}>{change}</li>
                      ))}
                    </ul>
                    {run.notes?.length > 0 && (
                      <>
                        <strong>Recommendation notes</strong>
                        <ul>{run.notes.map(note => <li key={note}>{note}</li>)}</ul>
                      </>
                    )}
                  </div>
                </div>
              )}
            </div>
          )
        })}
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

function defaultBenchmarkScenarios(): BenchmarkSpecInput[] {
  return [
    {
      name: 'General chat',
      system: 'You are a concise local assistant. Answer naturally.',
      user: 'In two short sentences, explain why local language models are useful.',
      max_tokens: 96,
      temperature: 0,
      top_p: 1,
      top_k: 1,
      min_p: 0,
      presence_penalty: 0,
      seed: 1,
      expect_json: false,
      tool_mode: 'none',
    },
    {
      name: 'Coding',
      system: 'You are a senior coding assistant. Return compact, correct code only when asked.',
      user: 'Write a small TypeScript function named clamp that clamps a number between min and max. Include one example call.',
      max_tokens: 160,
      temperature: 0,
      top_p: 1,
      top_k: 1,
      min_p: 0,
      presence_penalty: 0,
      seed: 1,
      expect_json: false,
      tool_mode: 'none',
    },
    {
      name: 'JSON discipline',
      system: 'You return strict JSON only when asked. Do not wrap JSON in Markdown.',
      user: 'Return exactly one JSON object with keys "language", "safe", and "score". Use language "typescript", safe true, score 7.',
      max_tokens: 96,
      temperature: 0,
      top_p: 1,
      top_k: 1,
      min_p: 0,
      presence_penalty: 0,
      seed: 1,
      expect_json: true,
      tool_mode: 'none',
    },
    {
      name: 'Tool protocol',
      system: 'You are a tool-using coding agent. If a tool is available and relevant, call it.',
      user: 'Use the read_file tool to inspect package.json.',
      max_tokens: 96,
      temperature: 0,
      top_p: 1,
      top_k: 1,
      min_p: 0,
      presence_penalty: 0,
      seed: 1,
      expect_json: false,
      tool_mode: 'auto',
    },
  ]
}

function scenarioPurpose(name: string) {
  switch (name) {
    case 'General chat': return 'Basic answer quality and clean visible output.'
    case 'Coding': return 'Small code generation at deterministic settings.'
    case 'JSON discipline': return 'Strict JSON output without Markdown or extra text.'
    case 'Tool protocol': return 'Synthetic read_file tool-call formatting; the tool is not executed.'
    default: return 'Benchmark probe.'
  }
}

function profileChanges(current: ProfilesFile['profiles'][string] | undefined, recommended: ProfilesFile['profiles'][string] | undefined) {
  if (!recommended) return []
  const changes: string[] = []
  const fields: Array<[keyof typeof recommended, string]> = [
    ['model_id', 'model_id'],
    ['ctx_tokens', 'ctx_tokens'],
    ['thinking', 'thinking'],
    ['preserve_thinking', 'preserve_thinking'],
    ['spec_type', 'spec_type'],
    ['spec_draft_n_max', 'spec_draft_n_max'],
  ]
  for (const [field, label] of fields) {
    const before = current?.[field]
    const after = recommended[field]
    if (String(before ?? '') !== String(after ?? '')) {
      changes.push(`${label}: ${String(before ?? '') || 'empty'} -> ${String(after ?? '') || 'empty'}`)
    }
  }
  for (const family of ['thinking_general', 'thinking_coding', 'nothinking'] as const) {
    for (const key of ['temperature', 'top_p', 'top_k', 'min_p', 'presence_penalty', 'max_tokens'] as const) {
      const before = current?.[family]?.[key]
      const after = recommended[family]?.[key]
      if (String(before ?? '') !== String(after ?? '')) {
        changes.push(`${family}.${key}: ${String(before ?? '') || 'empty'} -> ${String(after ?? '') || 'empty'}`)
      }
    }
  }
  return changes
}

function formatDate(value?: string) {
  if (!value) return ''
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
}
