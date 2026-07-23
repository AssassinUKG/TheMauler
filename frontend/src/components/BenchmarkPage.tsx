import { useEffect, useMemo, useState } from 'react'
import {
  BenchmarkProfileWithCases,
  ClearBenchmarkRuns,
  GetProfiles,
  GetSettings,
  ListBenchmarkRuns,
  ListModelsForProvider,
  LoadBenchmarkModel,
  RunAgentEval,
  RunAgentEvalRepeated,
  RunContextQualityEval,
  RunEngagementAgentEval,
  RunJHUTAgentEval,
  RunGrammarToolArgsProbe,
  RunMiniAgentLoopBenchmark,
  SwitchProfile,
  UpdateProfiles,
  type AgentEvalReport,
  type AgentEvalResult,
  type BenchmarkSpecInput,
  type ContextQualityReport,
  type GrammarToolArgsProbeResult,
  type ProfileBenchmarkResult,
  type ProfilesFile,
} from '../wailsjs/go'
import './BenchmarkPage.css'
import { PackLibraryPage } from './PackLibraryPage'

type BenchView = 'matrix' | 'advanced' | 'history' | 'packs'
type LogTone = 'info' | 'ok' | 'warn' | 'run'

interface MatrixRow {
  run: ProfileBenchmarkResult
  loop?: AgentEvalResult
}

interface MatrixLogLine {
  phase: string
  text: string
  tone: LogTone
}

export function BenchmarkPage({ version = 0, onProfilesChanged }: { version?: number; onProfilesChanged?: () => void }) {
  const [view, setView] = useState<BenchView>('matrix')
  const [profilesFile, setProfilesFile] = useState<ProfilesFile | null>(null)
  const [selectedProfile, setSelectedProfile] = useState('')
  const [agentEvalProfile, setAgentEvalProfile] = useState('')
  const [selectedProviderName, setSelectedProviderName] = useState('')
  const [draftProfile, setDraftProfile] = useState<ProfilesFile['profiles'][string] | null>(null)
  const [runs, setRuns] = useState<ProfileBenchmarkResult[]>([])
  const [running, setRunning] = useState(false)
  const [modelListLoading, setModelListLoading] = useState(false)
  const [status, setStatus] = useState('')
  const [expandedRun, setExpandedRun] = useState('')
  const [scenarioDrafts, setScenarioDrafts] = useState<BenchmarkSpecInput[]>(defaultBenchmarkScenarios())
  const [agentEval, setAgentEval] = useState<AgentEvalReport | null>(null)
  const [contextQuality, setContextQuality] = useState<ContextQualityReport | null>(null)
  const [grammarProbe, setGrammarProbe] = useState<GrammarToolArgsProbeResult | null>(null)
  const [matrixRows, setMatrixRows] = useState<MatrixRow[]>([])
  const [matrixLog, setMatrixLog] = useState<MatrixLogLine[]>([])
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [selectedMatrixModels, setSelectedMatrixModels] = useState<string[]>([])
  const [modelPickerValue, setModelPickerValue] = useState('')
  const [matrixContext8k, setMatrixContext8k] = useState(true)
  const [matrixContext16k, setMatrixContext16k] = useState(true)

  const load = async () => {
    const [pf, history, settings] = await Promise.all([
      GetProfiles(),
      ListBenchmarkRuns().catch(() => [] as ProfileBenchmarkResult[]),
      GetSettings().catch(() => null),
    ])
    setProfilesFile(pf)
    setRuns(history)
    const names = Object.keys(pf.profiles ?? {}).filter(name => Boolean(pf.profiles[name]?.model_id?.trim()))
    const configuredActive = settings?.active_profile && pf.profiles[settings.active_profile]
      ? settings.active_profile
      : names[0] ?? ''
    setSelectedProfile(prev => prev && pf.profiles[prev] ? prev : configuredActive)
    setAgentEvalProfile(prev => prev && pf.profiles[prev] ? prev : configuredActive)
    const providerNames = Object.keys(pf.providers ?? {})
    setSelectedProviderName(prev => prev && pf.providers[prev] ? prev : pf.profiles[configuredActive]?.provider || providerNames[0] || '')
  }

  useEffect(() => { void load() }, [version])

  const profileNames = useMemo(
    () => Object.keys(profilesFile?.profiles ?? {}).filter(name => Boolean(profilesFile?.profiles[name]?.model_id?.trim())),
    [profilesFile],
  )
  const providerNames = useMemo(() => Object.keys(profilesFile?.providers ?? {}), [profilesFile])
  const selected = profilesFile?.profiles[selectedProfile]
  const selectedAgentEvalProfile = profilesFile?.profiles[agentEvalProfile]
  const activeProfile = draftProfile ?? selected
  const provider = profilesFile?.providers[selectedProviderName] ?? (activeProfile ? profilesFile?.providers[activeProfile.provider] : undefined)
  const contextCandidates = useMemo(() => {
    const current = activeProfile?.ctx_tokens || 0
    const values = [8192, 16384, 32768, 65536, 98304, 120000, current]
      .filter(value => value > 0)
      .filter(value => current <= 0 || value <= Math.max(current, 120000))
    return [...new Set(values)].sort((a, b) => a - b)
  }, [activeProfile?.ctx_tokens])
  const matrixSummary = useMemo(() => buildMatrixSummary(matrixRows), [matrixRows])

  useEffect(() => {
    setDraftProfile(selected ? { ...selected, name: selectedProfile } : null)
  }, [selectedProfile, selected])

  useEffect(() => {
    if (selected?.provider && profilesFile?.providers?.[selected.provider]) {
      setSelectedProviderName(selected.provider)
    }
  }, [selectedProfile, selected?.provider, profilesFile?.providers])

  useEffect(() => {
    if (!selectedProviderName && activeProfile?.provider) setSelectedProviderName(activeProfile.provider)
  }, [activeProfile?.provider, selectedProviderName])

  const addMatrixLog = (phase: string, text: string, tone: LogTone = 'info') => {
    setMatrixLog(log => [{ phase, text, tone }, ...log].slice(0, 120))
  }

  const refreshProviderModels = async () => {
    if (!provider) return
    setModelListLoading(true)
    try {
      setStatus(`Listing models from ${provider.name || provider.backend}...`)
      const models = await ListModelsForProvider(provider)
      const filtered = uniqueModels(models)
      setAvailableModels(filtered)
      setModelPickerValue(current => current && filtered.includes(current) ? current : filtered[0] || '')
      setSelectedMatrixModels(prev => {
        const kept = prev.filter(model => filtered.includes(model))
        return kept.length > 0 ? kept : filtered.slice(0, Math.min(filtered.length, 1))
      })
      setStatus(filtered.length ? `Found ${filtered.length} models from ${provider.name || provider.backend}.` : 'No models returned by provider.')
    } catch (e) {
      setStatus(`Model list failed: ${e}`)
    } finally {
      setModelListLoading(false)
    }
  }

  const toggleMatrixModel = (model: string) => {
    setSelectedMatrixModels(prev => prev.includes(model) ? prev.filter(item => item !== model) : [...prev, model])
  }

  const addPickedModel = () => {
    const model = modelPickerValue.trim()
    if (!model) return
    setSelectedMatrixModels(items => uniqueModels([...items, model]))
  }

  const matrixContexts = () => {
    const contexts = []
    if (matrixContext8k) contexts.push(8192)
    if (matrixContext16k) contexts.push(16384)
    return contexts.length ? contexts : [8192]
  }

  const selectedOrListedModels = async () => {
    let models = uniqueModels(selectedMatrixModels)
    if (models.length > 0) return models
    if (availableModels.length > 0) return uniqueModels(availableModels)
    if (!provider) return []
    const listed = uniqueModels(await ListModelsForProvider(provider))
    setAvailableModels(listed)
    setModelPickerValue(current => current && listed.includes(current) ? current : listed[0] || '')
    setSelectedMatrixModels(listed)
    return listed
  }

  const loadSelectedModels = async () => {
    if (!activeProfile || !provider || !selectedProfile) return
    setRunning(true)
    setView('matrix')
    setMatrixRows([])
    setMatrixLog([])
    const contexts = matrixContexts()
    try {
      addMatrixLog('discover', `Preparing selected model load from ${provider.name || provider.backend}`, 'run')
      const models = await selectedOrListedModels()
      if (models.length === 0) {
        setStatus('No models selected or returned by provider.')
        addMatrixLog('discover', 'No models selected or returned by provider.', 'warn')
        return
      }
      for (const model of models) {
        for (const ctx of contexts) {
          const label = `${shortModelName(model)} @ ${ctx / 1024}k`
          setStatus(`Loading ${label}...`)
          addMatrixLog('load', `Loading ${label}`, 'run')
          const rowProfile = {
            ...activeProfile,
            name: `${selectedProfile}:${shortModelName(model)}@${ctx / 1024}k`,
            provider: selectedProviderName || activeProfile.provider,
            model_id: model,
            ctx_tokens: ctx,
            thinking: false,
            preserve_thinking: false,
          }
          try {
            const result = await LoadBenchmarkModel(rowProfile, provider)
            setMatrixRows(rows => [...rows, { run: result }])
            const actual = result.actual_ctx_tokens ? `actual ${Math.round(result.actual_ctx_tokens / 1024)}k` : 'actual n/a'
            addMatrixLog('load', `${label}: ${result.status}, ${actual}`, result.status === 'ok' ? 'ok' : 'warn')
          } catch (e) {
            addMatrixLog('error', `${label}: load failed - ${String(e)}`, 'warn')
          }
        }
      }
      setStatus(`Load complete: ${models.length} selected models x ${contexts.length} contexts.`)
      addMatrixLog('done', `Load complete: ${models.length} selected models x ${contexts.length} contexts.`, 'ok')
    } catch (e) {
      setStatus(`Load selected failed: ${e}`)
      addMatrixLog('error', `Load selected failed: ${String(e)}`, 'warn')
    } finally {
      setRunning(false)
    }
  }

  const runModelMatrix = async () => {
    if (!activeProfile || !provider || !selectedProfile) return
    setRunning(true)
    setView('matrix')
    setMatrixRows([])
    setMatrixLog([])
    const contexts = matrixContexts()
    try {
      const filteredModels = await selectedOrListedModels()
      if (filteredModels.length === 0) {
        setStatus('No models returned by provider.')
        addMatrixLog('discover', 'No models returned by provider.', 'warn')
        return
      }
      addMatrixLog('discover', `Testing ${filteredModels.length} selected models at ${contexts.map(ctx => `${ctx / 1024}k`).join(' and ')}.`, 'ok')
      const matrixScenarios = [matrixSpeedScenario(), matrixToolScenario()]
      for (const model of filteredModels) {
        for (const ctx of contexts) {
          const label = `${shortModelName(model)} @ ${ctx / 1024}k`
          setStatus(`Benchmarking ${label}...`)
          addMatrixLog('load', `Loading ${label}`, 'run')
          try {
            const rowProfile = {
              ...activeProfile,
              name: `${selectedProfile}:${shortModelName(model)}@${ctx / 1024}k`,
              provider: selectedProviderName || activeProfile.provider,
              model_id: model,
              ctx_tokens: ctx,
              thinking: false,
              preserve_thinking: false,
            }
            const result = await BenchmarkProfileWithCases(rowProfile, provider, matrixScenarios)
            setMatrixRows(rows => [...rows, { run: result }])
            const actual = result.actual_ctx_tokens ? `actual ${Math.round(result.actual_ctx_tokens / 1024)}k` : 'actual n/a'
            const toolCase = result.scenarios?.find(sc => sc.name === 'Tool call')
            addMatrixLog(
              'bench',
              `${label}: ${result.status}, decode ${formatTPS(result.decode_tokens_per_second)}, warm E2E ${formatTPS(result.end_to_end_tokens_per_second)}, ${timingSourceShort(result.timing_source)}, tool ${toolCase?.structured_tools ?? 0}/${toolCase?.repaired_tools ?? 0}, ${actual}`,
              result.status === 'ok' ? 'ok' : 'warn',
            )
            setStatus(`Mini agent-loop smoke: ${label}...`)
            addMatrixLog('loop', `Running mini agent loop for ${label}`, 'run')
            const loop = await RunMiniAgentLoopBenchmark(rowProfile, provider)
            setMatrixRows(rows => rows.map(row => row.run.id === result.id ? { ...row, loop } : row))
            addMatrixLog('loop', `${label}: loop ${loop.pass ? 'pass' : 'fail'} score ${loop.stability_score ?? 0}, success ${loop.tool_success_rate ?? 0}% (${loop.tool_calls ?? 0} tools, ${Math.round((loop.duration_ms ?? 0) / 1000)}s)`, loop.pass ? 'ok' : 'warn')
          } catch (e) {
            const failed: ProfileBenchmarkResult = {
              status: 'warn',
              id: `matrix-error-${Date.now()}`,
              created_at: new Date().toISOString(),
              profile_name: `${selectedProfile}:${shortModelName(model)}@${ctx / 1024}k`,
              provider_name: provider.name,
              model_id: model,
              ctx_tokens: ctx,
              summary: `Matrix row failed: ${e}`,
              notes: [String(e)],
              scenarios: [],
              recommended_profile: { ...activeProfile, model_id: model, ctx_tokens: ctx },
            }
            setMatrixRows(rows => [...rows, { run: failed }])
            addMatrixLog('error', `${label}: failed - ${String(e)}`, 'warn')
          }
        }
      }
      setStatus(`Model matrix complete: ${filteredModels.length} selected models x ${contexts.length} contexts.`)
      addMatrixLog('done', `Model matrix complete: ${filteredModels.length} models x ${contexts.length} contexts.`, 'ok')
    } catch (e) {
      setStatus(`Model matrix failed: ${e}`)
      addMatrixLog('error', `Model matrix failed: ${String(e)}`, 'warn')
    } finally {
      setRunning(false)
    }
  }

  const runBenchmark = async () => {
    if (!activeProfile || !provider || !selectedProfile) return
    setRunning(true)
    setStatus('Running custom benchmark suite...')
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

  const runContextSweep = async () => {
    if (!activeProfile || !provider || !selectedProfile) return
    setRunning(true)
    setStatus(`Running context sweep: ${contextCandidates.map(v => `${Math.round(v / 1024)}k`).join(', ')}...`)
    const newRuns: ProfileBenchmarkResult[] = []
    try {
      for (const ctx of contextCandidates) {
        setStatus(`Benchmarking ${selectedProfile} at ${ctx.toLocaleString()} context...`)
        const result = await BenchmarkProfileWithCases({ ...activeProfile, name: `${selectedProfile}@${Math.round(ctx / 1024)}k`, ctx_tokens: ctx }, provider, scenarioDrafts)
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

  const runAgentEval = async () => {
    if (!agentEvalProfile) return
    setRunning(true)
    setStatus(`Running agent eval suite against ${agentEvalProfile}...`)
    setAgentEval(null)
    try {
      const report = await RunAgentEval(agentEvalProfile)
      setAgentEval(report)
      setStatus(`Agent eval complete: ${report.pass_count}/${report.total} passed`)
    } catch (e) {
      setStatus(`Agent eval failed: ${e}`)
    } finally {
      setRunning(false)
    }
  }

  const runAgentEvalRepeated = async () => {
    if (!agentEvalProfile) return
    setRunning(true)
    setStatus(`Running production Agent Eval pass^5 against ${agentEvalProfile}. This performs five complete live suite rounds...`)
    setAgentEval(null)
    try {
      const report = await RunAgentEvalRepeated(agentEvalProfile, 5)
      setAgentEval(report)
      setStatus(`Repeated Agent Eval: ${report.pass_power} · ${report.fixture_pass_count}/${report.fixture_count} fixtures · ${report.pass_count}/${report.total} attempts`)
    } catch (e) {
      setStatus(`Repeated Agent Eval failed: ${e}`)
    } finally {
      setRunning(false)
    }
  }

  const runContextQuality = async () => {
    if (!agentEvalProfile) return
    setRunning(true)
    setStatus(`Running deterministic Context Quality pass^5 for ${agentEvalProfile}...`)
    setContextQuality(null)
    try {
      const report = await RunContextQualityEval(agentEvalProfile, 5)
      setContextQuality(report)
      setStatus(`Context Quality: ${report.pass_power} · ${report.pass_count}/${report.total} fixtures · ${report.attempt_pass_count}/${report.attempt_total} attempts`)
    } catch (e) {
      setStatus(`Context Quality failed: ${e}`)
    } finally {
      setRunning(false)
    }
  }

  const runJHUTEval = async () => {
    if (!agentEvalProfile) return
    setRunning(true); setAgentEval(null); setStatus(`Running browser-backed JHUT eval against ${agentEvalProfile}…`)
    try {
      const report = await RunJHUTAgentEval(agentEvalProfile)
      setAgentEval(report)
      setStatus(`JHUT browser eval complete: ${report.pass_count}/${report.total} passed`)
    } catch (e) { setStatus(`JHUT eval failed: ${e}`) } finally { setRunning(false) }
  }

  const runEngagementEval = async () => {
    setRunning(true); setAgentEval(null); setStatus('Running native Engagement lifecycle eval…')
    try {
      const report = await RunEngagementAgentEval()
      setAgentEval(report)
      setStatus(`Engagement eval complete: ${report.pass_count}/${report.total} passed`)
    } catch (e) { setStatus(`Engagement eval failed: ${e}`) } finally { setRunning(false) }
  }

  const runGrammarProbe = async () => {
    if (!agentEvalProfile) return
    setRunning(true)
    setStatus(`Running grammar tool-args probe against ${agentEvalProfile}...`)
    setGrammarProbe(null)
    try {
      const result = await RunGrammarToolArgsProbe(agentEvalProfile)
      setGrammarProbe(result)
      setStatus(result.supported ? 'Grammar probe passed: structured tool calls survived the schema constraint.' : 'Grammar probe did not pass for this profile.')
    } catch (e) {
      setStatus(`Grammar probe failed: ${e}`)
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
    const next = { ...profilesFile, profiles: { ...profilesFile.profiles, [run.profile_name]: { ...run.recommended_profile, name: run.profile_name } } }
    await UpdateProfiles(next)
    setProfilesFile(next)
    setStatus(`Applied settings to ${run.profile_name}`)
    onProfilesChanged?.()
  }

  const saveDraftProfile = async () => {
    if (!profilesFile || !draftProfile || !selectedProfile) return
    const next = { ...profilesFile, profiles: { ...profilesFile.profiles, [selectedProfile]: { ...draftProfile, name: selectedProfile } } }
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

  const selectProfile = (profileName: string) => {
    setSelectedProfile(profileName)
    setAgentEvalProfile(profileName)
    const profileProvider = profilesFile?.profiles[profileName]?.provider
    if (profileProvider && profilesFile?.providers[profileProvider]) {
      setSelectedProviderName(profileProvider)
    }
  }

  const copyMatrixCSV = async () => {
    await navigator.clipboard.writeText(matrixRowsToCSV(matrixRows))
    setStatus('Matrix CSV copied to clipboard')
  }

  const useMatrixRunInChat = async (run: ProfileBenchmarkResult) => {
    if (!profilesFile || !run.recommended_profile || !run.model_id) return
    setRunning(true)
    try {
      const profileName = matrixChatProfileName(run)
      const nextProfile = {
        ...run.recommended_profile,
        name: profileName,
        provider: run.provider_name || run.recommended_profile.provider,
        model_id: run.model_id,
        ctx_tokens: run.ctx_tokens || run.recommended_profile.ctx_tokens,
      }
      const next = {
        ...profilesFile,
        profiles: {
          ...profilesFile.profiles,
          [profileName]: nextProfile,
        },
      }
      await UpdateProfiles(next)
      await SwitchProfile(profileName)
      setProfilesFile(next)
      setSelectedProfile(profileName)
      setAgentEvalProfile(profileName)
      setSelectedProviderName(nextProfile.provider)
      setStatus(`Chat now uses ${shortModelName(run.model_id)} at ${Math.round((nextProfile.ctx_tokens || 0) / 1024)}k via profile ${profileName}.`)
      onProfilesChanged?.()
    } catch (error) {
      setStatus(`Could not use this matrix model in Chat: ${String(error)}`)
    } finally {
      setRunning(false)
    }
  }

  return (
    <div className="benchmark-page">
      <header className="benchmark-header">
        <div>
          <h1>{view === 'packs' ? 'Pack Library' : 'Model Lab'}</h1>
          <p>{view === 'packs' ? 'Manage the versioned workflows and vulnerability checks offered to new Engagement Grids.' : 'Load, verify context, benchmark text/tool behavior, and run a tiny agent loop across local models.'}</p>
        </div>
        {view !== 'packs' && <div className="benchmark-actions">
          <label className="benchmark-inline-control">
            <span>Provider</span>
            <select value={selectedProviderName} onChange={e => setSelectedProviderName(e.target.value)}>
              {providerNames.map(name => <option key={name} value={name}>{name}</option>)}
            </select>
          </label>
          <label className="benchmark-inline-control">
            <span>Profile defaults</span>
            <select value={selectedProfile} onChange={e => selectProfile(e.target.value)}>
              {profileNames.map(name => <option key={name} value={name}>{name}</option>)}
            </select>
          </label>
          <button className="primary" onClick={() => void runModelMatrix()} disabled={running || !activeProfile || !provider}>
            {running ? 'Running...' : 'Run Matrix'}
          </button>
          <button onClick={() => void load()} disabled={running}>Refresh</button>
        </div>}
      </header>

      <nav className="benchmark-view-tabs">
        <button className={view === 'matrix' ? 'active' : ''} onClick={() => setView('matrix')}>Model Matrix</button>
        <button className={view === 'advanced' ? 'active' : ''} onClick={() => setView('advanced')}>Advanced Suite</button>
        <button className={view === 'history' ? 'active' : ''} onClick={() => setView('history')}>History</button>
        <button className={view === 'packs' ? 'active' : ''} onClick={() => setView('packs')}>Pack Library</button>
      </nav>

      {view !== 'packs' && activeProfile && (
        <div className="benchmark-current">
          <Metric label="Model" value={activeProfile.model_id} />
          <Metric label="Provider" value={selectedProviderName || activeProfile.provider} />
          <Metric label="Run ctx" value={`${activeProfile.ctx_tokens || 0}`} />
          <Metric label="Matrix ctx" value={matrixContexts().map(ctx => `${ctx / 1024}k`).join(' / ')} />
          <Metric label="Thinking" value={activeProfile.thinking ? 'on' : 'off'} />
        </div>
      )}

      {view !== 'packs' && status && <div className="benchmark-status">{status}</div>}

      {view === 'packs' && <PackLibraryPage />}

      {view === 'matrix' && (
        <>
          <section className="matrix-hero">
            <div>
              <h2>Model Matrix</h2>
              <p>Select provider models directly. Tests use one excluded warm-up plus three measured text runs; choosing Use in Chat creates a named local profile.</p>
            </div>
            <div className="matrix-hero-actions">
              <button onClick={() => void refreshProviderModels()} disabled={running || modelListLoading || !provider}>{modelListLoading ? 'Listing...' : 'List Models'}</button>
              <button onClick={() => void loadSelectedModels()} disabled={running || !activeProfile || !provider}>{running ? 'Loading...' : 'Load Selected'}</button>
              <button className="primary" onClick={() => void runModelMatrix()} disabled={running || !activeProfile || !provider}>{running ? 'Running Matrix...' : 'Load + Benchmark Selected'}</button>
              <button onClick={() => void copyMatrixCSV()} disabled={matrixRows.length === 0}>Copy CSV</button>
            </div>
          </section>

          <section className="matrix-selector">
            <div className="matrix-selector-head">
              <div><strong>Model Picker</strong><span>Choose models returned by the selected provider. Profiles are created only when you explicitly click Use in Chat.</span></div>
              <div className="matrix-context-toggles">
                <label><input type="checkbox" checked={matrixContext8k} onChange={e => setMatrixContext8k(e.target.checked)} />8k</label>
                <label><input type="checkbox" checked={matrixContext16k} onChange={e => setMatrixContext16k(e.target.checked)} />16k</label>
              </div>
            </div>
            <div className="matrix-model-picker">
              <select value={modelPickerValue} onChange={e => setModelPickerValue(e.target.value)} disabled={running || availableModels.length === 0}>
                {availableModels.length === 0 ? <option value="">No models listed</option> : availableModels.map(model => <option key={model} value={model}>{shortModelName(model)}</option>)}
              </select>
              <button onClick={addPickedModel} disabled={running || !modelPickerValue}>Add Selected</button>
              <button onClick={() => setSelectedMatrixModels(availableModels)} disabled={running || availableModels.length === 0}>Select All</button>
              <button onClick={() => setSelectedMatrixModels([])} disabled={running || selectedMatrixModels.length === 0}>Clear</button>
            </div>
            <div className="matrix-model-list">
              {availableModels.length === 0 ? (
                <div className="benchmark-empty">No model list yet. Click List Models to populate the picker.</div>
              ) : availableModels.map(model => (
                <label className="matrix-model-option" key={model} title={model}>
                  <input type="checkbox" checked={selectedMatrixModels.includes(model)} onChange={() => toggleMatrixModel(model)} />
                  <span>{shortModelName(model)}</span>
                  <em>{model}</em>
                </label>
              ))}
            </div>
          </section>

          <section className="matrix-summary">
            <Metric label="Rows" value={`${matrixSummary.rows}`} />
            <Metric label="Best Text" value={matrixSummary.bestText} />
            <Metric label="Best Loop" value={matrixSummary.bestLoop} />
            <Metric label="Tool OK" value={matrixSummary.toolOK} />
            <Metric label="Failures" value={`${matrixSummary.failures}`} />
          </section>

          <div className="matrix-lab-grid">
            <section className="matrix-panel">
              <div className="matrix-head"><div><strong>Live Run Log</strong><span>Newest phase events first.</span></div></div>
              <div className="matrix-log">
                {matrixLog.length === 0 ? <span>No matrix log yet.</span> : matrixLog.map((line, index) => (
                  <div className={`matrix-log-line ${line.tone}`} key={`${line.phase}-${line.text}-${index}`}>
                    <span>{line.phase}</span>
                    <strong>{line.text}</strong>
                  </div>
                ))}
              </div>
            </section>

            <section className="matrix-panel">
              <div className="matrix-head"><div><strong>Run Shape</strong><span>Per model/context row.</span></div></div>
              <div className="matrix-flow">
                <div><span>1</span><strong>Load</strong><em>request ctx 8k or 16k</em></div>
                <div><span>2</span><strong>Verify</strong><em>record actual context</em></div>
                <div><span>3</span><strong>Warm</strong><em>excluded startup pass</em></div>
                <div><span>4</span><strong>Text ×3</strong><em>decode and warm end-to-end timing</em></div>
                <div><span>5</span><strong>Tool</strong><em>required read call, scored separately</em></div>
                <div><span>6</span><strong>Loop</strong><em>read/write/verify mini task</em></div>
              </div>
            </section>
          </div>

          <MatrixTable rows={matrixRows} running={running} onUseInChat={run => void useMatrixRunInChat(run)} />
        </>
      )}

      {view === 'advanced' && (
        <AdvancedSuite
          running={running}
          draftProfile={draftProfile}
          scenarioDrafts={scenarioDrafts}
          agentEval={agentEval}
          contextQuality={contextQuality}
          grammarProbe={grammarProbe}
          selectedProfile={selectedProfile}
          agentEvalProfile={agentEvalProfile}
          agentEvalProfileData={selectedAgentEvalProfile ?? null}
          profileNames={profileNames}
          profiles={profilesFile?.profiles ?? {}}
          onDraftChange={setDraftProfile}
          onScenarioChange={updateScenario}
          onScenarioAdd={addScenario}
          onScenarioRemove={removeScenario}
          onSaveDraft={() => void saveDraftProfile()}
          onAgentEvalProfileChange={selectProfile}
          onRunBenchmark={() => void runBenchmark()}
          onRunContextSweep={() => void runContextSweep()}
          onRunAgentEval={() => void runAgentEval()}
          onRunAgentEvalRepeated={() => void runAgentEvalRepeated()}
          onRunContextQuality={() => void runContextQuality()}
          onRunEngagementEval={() => void runEngagementEval()}
          onRunJHUTEval={() => void runJHUTEval()}
          onRunGrammarProbe={() => void runGrammarProbe()}
        />
      )}

      {view === 'history' && (
        <HistoryTable
          runs={runs}
          running={running}
          expandedRun={expandedRun}
          profilesFile={profilesFile}
          onClear={() => void clearRuns()}
          onExpand={setExpandedRun}
          onApply={run => void applyRun(run)}
        />
      )}
    </div>
  )
}

function MatrixTable({ rows, running, onUseInChat }: { rows: MatrixRow[]; running: boolean; onUseInChat: (run: ProfileBenchmarkResult) => void }) {
  return (
    <section className="matrix-panel">
      <div className="matrix-head"><div><strong>Results</strong><span>Decode speed is backend-reported; warm E2E includes prompt processing. Tool and loop latency are separate.</span></div></div>
      <div className="matrix-table">
        <div className="matrix-row matrix-row-head">
          <span>Model</span><span>Requested</span><span>Actual</span><span>Status</span><span>Decode</span><span>Warm E2E</span><span>Source</span><span>Tool</span><span>Loop</span><span>TTFT</span><span>Load</span><span>Total</span><span>Chat</span><span>Notes</span>
        </div>
        {rows.length === 0 ? (
          <div className="benchmark-empty">No matrix results yet.</div>
        ) : rows.map((row, index) => {
          const run = row.run
          const toolCase = run.scenarios?.find(sc => sc.name === 'Tool call')
          return (
            <div className={`matrix-row matrix-${run.status}`} key={`${run.model_id}-${run.ctx_tokens}-${index}`}>
              <span title={run.model_id}>{run.model_id}</span>
              <span>{run.ctx_tokens ? `${Math.round(run.ctx_tokens / 1024)}k` : 'n/a'}</span>
              <span>{run.actual_ctx_tokens ? `${Math.round(run.actual_ctx_tokens / 1024)}k` : 'n/a'}</span>
              <span>{run.status}</span>
              <span title="Backend-reported decode throughput">{formatTPS(run.decode_tokens_per_second)}</span>
              <span title="Completion tokens divided by complete warm request wall time">{formatTPS(run.end_to_end_tokens_per_second)}</span>
              <span title={timingSourceDescription(run.timing_source)}>{timingSourceShort(run.timing_source)}</span>
              <span title="structured/repaired">{toolCase ? `${toolCase.structured_tools ?? 0}/${toolCase.repaired_tools ?? 0}` : 'n/a'}</span>
              <span title={loopReliabilityTitle(row.loop)}>{row.loop ? loopReliabilityLabel(row.loop) : 'pending'}</span>
              <span>{run.text_ttf_ms ? `${run.text_ttf_ms} ms` : 'n/a'}</span>
              <span>{run.load_ms != null ? `${run.load_ms} ms` : 'n/a'}</span>
              <span>{run.total_ms ? `${(run.total_ms / 1000).toFixed(1)}s` : 'n/a'}</span>
              <span><button className="matrix-use-chat" onClick={() => onUseInChat(run)} disabled={running || !run.recommended_profile || !run.model_id}>Use in Chat</button></span>
              <span title={[run.summary, ...(run.notes ?? [])].join('\n')}>{run.summary || run.notes?.[0] || '-'}</span>
            </div>
          )
        })}
      </div>
    </section>
  )
}

function AdvancedSuite({
  running,
  draftProfile,
  scenarioDrafts,
  agentEval,
  contextQuality,
  grammarProbe,
  selectedProfile,
  agentEvalProfile,
  agentEvalProfileData,
  profileNames,
  profiles,
  onDraftChange,
  onScenarioChange,
  onScenarioAdd,
  onScenarioRemove,
  onSaveDraft,
  onAgentEvalProfileChange,
  onRunBenchmark,
  onRunContextSweep,
  onRunAgentEval,
  onRunAgentEvalRepeated,
  onRunContextQuality,
  onRunEngagementEval,
  onRunJHUTEval,
  onRunGrammarProbe,
}: {
  running: boolean
  draftProfile: ProfilesFile['profiles'][string] | null
  scenarioDrafts: BenchmarkSpecInput[]
  agentEval: AgentEvalReport | null
  contextQuality: ContextQualityReport | null
  grammarProbe: GrammarToolArgsProbeResult | null
  selectedProfile: string
  agentEvalProfile: string
  agentEvalProfileData: ProfilesFile['profiles'][string] | null
  profileNames: string[]
  profiles: ProfilesFile['profiles']
  onDraftChange: (profile: ProfilesFile['profiles'][string] | null) => void
  onScenarioChange: (index: number, patch: Partial<BenchmarkSpecInput>) => void
  onScenarioAdd: () => void
  onScenarioRemove: (index: number) => void
  onSaveDraft: () => void
  onAgentEvalProfileChange: (profile: string) => void
  onRunBenchmark: () => void
  onRunContextSweep: () => void
  onRunAgentEval: () => void
  onRunAgentEvalRepeated: () => void
  onRunContextQuality: () => void
  onRunEngagementEval: () => void
  onRunJHUTEval: () => void
  onRunGrammarProbe: () => void
}) {
  return (
    <>
      {draftProfile && (
        <section className="benchmark-editor">
          <label className="benchmark-model-profile">
            <span>Model / profile</span>
            <select value={agentEvalProfile} onChange={e => onAgentEvalProfileChange(e.target.value)} disabled={running} title={agentEvalProfileData?.model_id || ''}>
              {profileNames.map(name => (
                <option key={name} value={name}>{shortModelName(profiles[name]?.model_id || '')} ({name})</option>
              ))}
            </select>
            <small title={agentEvalProfileData?.model_id || ''}>{agentEvalProfileData?.model_id || 'No configured model'}</small>
          </label>
          <label><span>Context</span><input type="number" value={draftProfile.ctx_tokens} onChange={e => onDraftChange({ ...draftProfile, ctx_tokens: Number(e.target.value) || 0 })} /></label>
          <label className="benchmark-check"><input type="checkbox" checked={draftProfile.thinking} onChange={e => onDraftChange({ ...draftProfile, thinking: e.target.checked })} /><span>Thinking</span></label>
          <label className="benchmark-check"><input type="checkbox" checked={draftProfile.preserve_thinking} onChange={e => onDraftChange({ ...draftProfile, preserve_thinking: e.target.checked })} /><span>Preserve thinking</span></label>
          <label><span>MTP</span><select value={draftProfile.spec_type || ''} onChange={e => onDraftChange({ ...draftProfile, spec_type: e.target.value })}><option value="">off</option><option value="draft-mtp">draft-mtp</option></select></label>
          <label><span>Draft N</span><input type="number" value={draftProfile.spec_draft_n_max || 0} onChange={e => onDraftChange({ ...draftProfile, spec_draft_n_max: Number(e.target.value) || 0 })} /></label>
          <button onClick={onSaveDraft} disabled={running || !selectedProfile}>Save to Profile</button>
        </section>
      )}

      <section className="benchmark-scenario-editor">
        <div className="benchmark-editor-head">
          <div><strong>Advanced Run Suite</strong><span>Edit scenarios, sweep context, and run live reliability probes.</span></div>
          <div className="advanced-actions">
            <button onClick={onScenarioAdd} disabled={running}>Add Scenario</button>
            <button className="primary" onClick={onRunBenchmark} disabled={running || !draftProfile}>Run Suite</button>
            <button onClick={onRunContextSweep} disabled={running || !draftProfile}>Context Sweep</button>
          </div>
        </div>
        {scenarioDrafts.map((scenario, index) => (
          <details className="benchmark-scenario-edit" key={`${scenario.name}-${index}`} open={index < 2}>
            <summary><span>{scenario.name || `Scenario ${index + 1}`}</span><small>{scenario.max_tokens} max / temp {scenario.temperature} / tools {scenario.tool_mode}</small></summary>
            <div className="benchmark-scenario-form">
              <label><span>Name</span><input value={scenario.name} onChange={e => onScenarioChange(index, { name: e.target.value })} /></label>
              <label><span>Max tokens</span><input type="number" value={scenario.max_tokens} onChange={e => onScenarioChange(index, { max_tokens: Number(e.target.value) || 0 })} /></label>
              <label><span>Temp</span><input type="number" step="0.05" value={scenario.temperature} onChange={e => onScenarioChange(index, { temperature: Number(e.target.value) })} /></label>
              <label><span>Top P</span><input type="number" step="0.05" value={scenario.top_p} onChange={e => onScenarioChange(index, { top_p: Number(e.target.value) })} /></label>
              <label><span>Top K</span><input type="number" value={scenario.top_k} onChange={e => onScenarioChange(index, { top_k: Number(e.target.value) || 0 })} /></label>
              <label><span>Tool mode</span><select value={scenario.tool_mode} onChange={e => onScenarioChange(index, { tool_mode: e.target.value })}><option value="none">none</option><option value="auto">auto</option><option value="required">required</option></select></label>
              <label className="benchmark-check"><input type="checkbox" checked={scenario.expect_json} onChange={e => onScenarioChange(index, { expect_json: e.target.checked })} /><span>Expect JSON</span></label>
              <button onClick={() => onScenarioRemove(index)} disabled={running || scenarioDrafts.length <= 1}>Remove</button>
              <label className="wide"><span>System</span><textarea value={scenario.system} onChange={e => onScenarioChange(index, { system: e.target.value })} /></label>
              <label className="wide"><span>User</span><textarea value={scenario.user} onChange={e => onScenarioChange(index, { user: e.target.value })} /></label>
            </div>
          </details>
        ))}
      </section>

      <section className="benchmark-diagnostics">
        <div className="benchmark-editor-head">
          <div><strong>Reliability Probes</strong><span>Live checks for production agent path and grammar-constrained tool arguments.</span></div>
          <div className="advanced-actions">
            <button onClick={onRunAgentEval} disabled={running || !agentEvalProfile}>Run Agent Eval</button>
            <button onClick={onRunAgentEvalRepeated} disabled={running || !agentEvalProfile} title="Runs the complete live model suite five times">Agent Eval x5</button>
            <button className="primary" onClick={onRunContextQuality} disabled={running || !agentEvalProfile} title="Deterministic routing, tool, source, budget, and hostile-content checks with no model calls">Context Quality pass^5</button>
            <button onClick={onRunEngagementEval} disabled={running}>Run Engagement Eval</button>
            <button onClick={onRunJHUTEval} disabled={running || !agentEvalProfile}>Run JHUT Browser Eval</button>
            <button onClick={onRunGrammarProbe} disabled={running || !agentEvalProfile}>Grammar Probe</button>
          </div>
        </div>
        <AgentEvalPanel report={agentEval} />
        <ContextQualityPanel report={contextQuality} />
        <GrammarProbePanel result={grammarProbe} />
      </section>
    </>
  )
}

function AgentEvalPanel({ report }: { report: AgentEvalReport | null }) {
  if (!report) return <div className="benchmark-empty">No agent eval run yet.</div>
  const ok = report.pass_count === report.total
  const repeats = report.repeats || 1
  const fixtures = report.fixture_count || report.total
  const fixturePasses = report.fixture_count ? report.fixture_pass_count : report.pass_count
  return (
    <section className="agent-eval-panel">
      <div className="agent-eval-head">
        <div><strong>Agent Eval: {report.profile}</strong><span>Production-style loop checks for tool use, recovery, and evidence-owned completion.</span></div>
        <div className={`agent-eval-score ${ok ? 'ok' : 'warn'}`}>{report.pass_power || `pass^${repeats}`} / {report.pass_count}/{report.total}</div>
      </div>
      <div className="agent-eval-aggregate">
        <Metric label="Fixtures" value={`${fixturePasses}/${fixtures}`} />
        <Metric label="Unsupported" value={`${(report.unsupported_completion_rate || 0).toFixed(1)}%`} />
        <Metric label="Duplicates" value={`${(report.duplicate_action_rate || 0).toFixed(1)}%`} />
        <Metric label="Tool errors" value={`${(report.tool_error_rate || 0).toFixed(1)}%`} />
        <Metric label="Recovery" value={`${(report.recovery_success_rate || 0).toFixed(1)}%`} />
        <Metric label="Avg tools" value={(report.average_tool_calls || 0).toFixed(1)} />
        <Metric label="Policy blocks" value={String(report.policy_violations || 0)} />
        <Metric label="Human actions" value={String(report.human_interventions || 0)} />
      </div>
      <div className="agent-eval-grid">
        {report.results.map((result, index) => (
          <div className={`agent-eval-card ${result.pass ? 'ok' : 'warn'}`} key={`${result.name}-${result.attempt || 1}-${index}`}>
            <div className="agent-eval-title"><strong>{result.name}{repeats > 1 ? ` / attempt ${result.attempt || 1} of ${repeats}` : ''}</strong><span>{result.status}</span></div>
            <div className="agent-eval-metrics">{result.tool_calls} tools / score {result.stability_score ?? 0} / success {result.tool_success_rate ?? 0}% / repeats {result.repeat_tool_rate ?? 0}% / {Math.round((result.duration_ms || 0) / 1000)}s</div>
            <div className="agent-eval-dimensions">
              <span className={result.status_pass ? 'ok' : 'warn'}>status</span>
              <span className={result.artifact_pass ? 'ok' : 'warn'}>artifact</span>
              <span className={result.hygiene_pass ? 'ok' : 'warn'}>hygiene</span>
              {result.verifier_version && <span className={result.runtime_pass ? 'ok' : 'warn'}>runtime</span>}
              <span className={result.false_done ? 'warn' : 'ok'}>false-done</span>
            </div>
            {result.fail_reason && <div className="agent-eval-fail">{result.fail_reason}</div>}
            {result.verifier_version && <div className="agent-eval-fail">{result.verifier_version} · {result.model_id} · {result.context_tokens} ctx · artifact {result.artifact_hash?.slice(0, 12) || 'missing'}<br />Desktop: {result.desktop_screenshot}<br />Mobile: {result.mobile_screenshot}</div>}
          </div>
        ))}
      </div>
    </section>
  )
}

function ContextQualityPanel({ report }: { report: ContextQualityReport | null }) {
  if (!report) return <div className="benchmark-empty">No deterministic context-quality run yet.</div>
  return (
    <section className="agent-eval-panel context-quality-panel">
      <div className="agent-eval-head">
        <div><strong>Context Quality: {report.profile}</strong><span>{report.evaluation_envelope || 'Canonical defaults + selected profile/workspace context'}. Deterministic routing, compact tools, hostile-content guards, and repeat stability. Zero model calls.</span></div>
        <div className={`agent-eval-score ${report.pass ? 'ok' : 'warn'}`}>{report.pass_power} / {report.pass_count}/{report.total}</div>
      </div>
      <div className="agent-eval-aggregate context-quality-summary">
        <Metric label="Fixtures" value={`${report.pass_count}/${report.total}`} />
        <Metric label="Attempts" value={`${report.attempt_pass_count}/${report.attempt_total}`} />
        <Metric label="Hostile guard" value={report.hostile_pass ? 'pass' : 'fail'} />
        <Metric label="Model calls" value="0" />
      </div>
      <div className="agent-eval-grid">
        {report.results.map(result => (
          <div className={`agent-eval-card ${result.pass ? 'ok' : 'warn'}`} key={result.id}>
            <div className="agent-eval-title"><strong>{result.category}</strong><span>{result.pass_power}</span></div>
            <div className="agent-eval-metrics">{result.variant_count} paraphrases / {result.passed_repeats}/{result.repeats} repeat rounds</div>
            <div className="agent-eval-dimensions">
              <span className={result.policy_pass ? 'ok' : 'warn'}>policy</span>
              <span className={result.tool_pass ? 'ok' : 'warn'}>tools</span>
              <span className={result.source_pass ? 'ok' : 'warn'}>sources</span>
              <span className={result.budget_pass ? 'ok' : 'warn'}>budget</span>
              <span className={result.hostile_pass ? 'ok' : 'warn'}>hostile</span>
              <span className={result.determinism_pass ? 'ok' : 'warn'}>stable</span>
            </div>
            <div className="context-quality-variants">
              {result.variants.map(variant => (
                <div key={`${result.id}-${variant.prompt_index}`} className={variant.pass ? 'ok' : 'warn'}>
                  <span>Prompt {variant.prompt_index}</span>
                  <strong>{variant.policy} / {variant.agent_mode}</strong>
                  <em>{variant.project_tokens} project tokens / {variant.tool_names.length} tools</em>
                </div>
              ))}
            </div>
            {result.failures?.slice(0, 4).map((failure, index) => <div className="agent-eval-fail" key={`${result.id}-failure-${index}`}>{failure}</div>)}
          </div>
        ))}
      </div>
    </section>
  )
}

function GrammarProbePanel({ result }: { result: GrammarToolArgsProbeResult | null }) {
  if (!result) return <div className="benchmark-empty">No grammar probe run yet.</div>
  return (
    <section className="grammar-probe-panel">
      <div className="grammar-probe-head">
        <div><strong>Grammar Probe: {result.profile}</strong><span>{result.backend} / {result.model_id}</span></div>
        <div className={`grammar-probe-badge ${result.supported ? 'ok' : 'warn'}`}>{result.supported ? 'pass' : 'hold'}</div>
      </div>
      <div className="grammar-probe-grid">
        <Metric label="Structured" value={result.structured_call ? 'yes' : 'no'} />
        <Metric label="Args" value={result.valid_arguments ? 'valid' : 'invalid'} />
        <Metric label="Tool" value={result.tool_name || 'n/a'} />
        <Metric label="Supported" value={result.supported ? 'yes' : 'no'} />
      </div>
      {result.arguments && <pre className="grammar-probe-text">{result.arguments}</pre>}
      {result.error && <pre className="grammar-probe-error">{result.error}</pre>}
      <div className="grammar-probe-note">{result.recommendation}</div>
    </section>
  )
}

function HistoryTable({
  runs,
  running,
  expandedRun,
  profilesFile,
  onClear,
  onExpand,
  onApply,
}: {
  runs: ProfileBenchmarkResult[]
  running: boolean
  expandedRun: string
  profilesFile: ProfilesFile | null
  onClear: () => void
  onExpand: (id: string) => void
  onApply: (run: ProfileBenchmarkResult) => void
}) {
  return (
    <section className="benchmark-table">
      <div className="benchmark-editor-head">
        <div><strong>Benchmark History</strong><span>Stored suite and sweep runs.</span></div>
        <div className="history-actions"><button onClick={onClear} disabled={running || runs.length === 0}>Clear History</button></div>
      </div>
      <div className="benchmark-row benchmark-row-head">
        <span>Run</span><span>Model</span><span>Context</span><span>Tier</span><span>Text speed</span><span>Score</span><span>Scenarios</span><span>Summary</span><span>Action</span>
      </div>
      {runs.length === 0 ? <div className="benchmark-empty">No benchmark history yet.</div> : runs.map(run => {
        const id = run.id || `${run.profile_name}-${run.created_at}`
        const expanded = expandedRun === id
        return (
          <div className="benchmark-run-block" key={id}>
            <div className={`benchmark-row benchmark-run benchmark-${run.status}`}>
              <span><strong>{run.profile_name || 'profile'}</strong><small>{formatDate(run.created_at)}</small></span>
              <span title={run.model_id}>{run.model_id}</span>
              <span>{run.ctx_tokens?.toLocaleString() ?? 'n/a'}</span>
              <span>{run.context_tier || 'n/a'}</span>
              <span title={timingSourceDescription(run.timing_source)}>{headlineSpeed(run)}</span>
              <span>{run.score ?? 0}</span>
              <span className="scenario-list">{run.scenarios?.map((sc, index) => <span className={`scenario-pill ${sc.status !== 'ok' ? 'scenario-warn' : ''}`} key={`${sc.name}-${sc.iteration || index}`}>{sc.name}{sc.iteration && sc.iteration > 1 ? ` #${sc.iteration}` : ''}</span>)}</span>
              <span title={run.summary}>{run.summary}</span>
              <span className="benchmark-row-actions">
                <button onClick={() => onExpand(expanded ? '' : id)}>{expanded ? 'Hide' : 'Details'}</button>
                <button onClick={() => onApply(run)} disabled={running || !run.recommended_profile || !profilesFile?.profiles[run.profile_name || '']}>Apply</button>
              </span>
            </div>
            {expanded && (
              <div className="benchmark-details">
                <div className="benchmark-detail-grid">
                  {run.warmup && (
                    <div className={`benchmark-detail-card ${run.warmup.status !== 'ok' ? 'scenario-warn' : ''}`}>
                      <div className="benchmark-detail-title">Warm-up · excluded</div>
                      <div className="benchmark-detail-purpose">Loads templates, kernels, and prompt cache before measured text runs.</div>
                      <div className="benchmark-detail-summary">{run.warmup.summary}</div>
                      <div className="benchmark-detail-metrics">{scenarioTimingSummary(run.warmup)}</div>
                    </div>
                  )}
                  {run.scenarios?.map((sc, index) => (
                    <div className="benchmark-detail-card" key={`${sc.name}-${sc.iteration || index}`}>
                      <div className="benchmark-detail-title">{sc.name}{sc.iteration ? ` · run ${sc.iteration}` : ''}</div>
                      <div className="benchmark-detail-purpose">{scenarioPurpose(sc.name)}</div>
                      <div className="benchmark-detail-summary">{sc.summary}</div>
                      <div className="benchmark-detail-metrics">{scenarioTimingSummary(sc)} / {sc.structured_tools ?? 0} structured / {sc.repaired_tools ?? 0} repaired</div>
                      {sc.error && <pre>{sc.error}</pre>}
                    </div>
                  ))}
                </div>
                <div className="benchmark-apply-preview">
                  <strong>Recommended changes</strong>
                  <ul>{profileChanges(profilesFile?.profiles[run.profile_name || ''], run.recommended_profile).map(change => <li key={change}>{change}</li>)}</ul>
                </div>
              </div>
            )}
          </div>
        )
      })}
    </section>
  )
}

function pickBest(runs: ProfileBenchmarkResult[]) {
  return [...runs].sort((a, b) => (b.score ?? 0) - (a.score ?? 0))[0]
}

function describeRun(run?: ProfileBenchmarkResult) {
  if (!run) return 'n/a'
  return `${Math.round((run.ctx_tokens ?? 0) / 1024)}k (${run.score ?? 0}/100, ${headlineSpeed(run)})`
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div className="benchmark-metric"><span>{label}</span><strong title={value}>{value}</strong></div>
}

function defaultBenchmarkScenarios(): BenchmarkSpecInput[] {
  return [
    matrixSpeedScenario(),
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
      seed: 2,
      expect_json: false,
      tool_mode: 'none',
    },
    {
      name: 'JSON discipline',
      system: 'Return only valid JSON matching the user request.',
      user: 'Return {"verdict":"ok","items":["alpha","beta"],"count":2}.',
      max_tokens: 96,
      temperature: 0,
      top_p: 1,
      top_k: 1,
      min_p: 0,
      presence_penalty: 0,
      seed: 3,
      expect_json: true,
      tool_mode: 'none',
    },
    matrixToolScenario(),
  ]
}

function matrixSpeedScenario(): BenchmarkSpecInput {
  return {
    name: 'Text speed',
    system: 'You are a concise local assistant. Answer naturally.',
    user: 'In two short paragraphs, explain why local language models are useful for developer tools.',
    max_tokens: 256,
    temperature: 0,
    top_p: 1,
    top_k: 1,
    min_p: 0,
    presence_penalty: 0,
    seed: 1,
    expect_json: false,
    tool_mode: 'none',
  }
}

function matrixToolScenario(): BenchmarkSpecInput {
  return {
    name: 'Tool call',
    system: 'You are testing tool protocol reliability. Use the available read tool exactly once.',
    user: 'Read the file notes.txt and answer with one short sentence about what it contains.',
    max_tokens: 128,
    temperature: 0,
    top_p: 1,
    top_k: 1,
    min_p: 0,
    presence_penalty: 0,
    seed: 4,
    expect_json: false,
    tool_mode: 'required',
  }
}

function shortModelName(model: string) {
  return model.split(/[\\/]/).pop()?.replace(/\.(gguf|bin|safetensors)$/i, '') || model
}

function matrixChatProfileName(run: ProfileBenchmarkResult) {
  const model = shortModelName(run.model_id || 'local-model')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 56)
  const context = Math.max(1, Math.round((run.ctx_tokens || run.recommended_profile?.ctx_tokens || 0) / 1024))
  return `lab-${model || 'local-model'}-${context}k`
}

function benchmarkTextSpeed(run: ProfileBenchmarkResult) {
  return run.text_tokens_per_second || run.tokens_per_second || 0
}

function formatTPS(value?: number) {
  return value && value > 0 ? `${value.toFixed(1)} tok/s` : 'n/a'
}

function headlineSpeed(run: ProfileBenchmarkResult) {
  const value = benchmarkTextSpeed(run)
  if (!value) return 'n/a'
  return `${value.toFixed(1)} tok/s · ${timingSourceShort(run.timing_source)}`
}

function timingSourceShort(source?: string) {
  switch ((source || '').toLowerCase()) {
    case 'backend_decode': return 'decode'
    case 'wall_clock_e2e': return 'E2E'
    case 'estimated_wall_clock': return 'est. E2E'
    case 'mixed': return 'mixed'
    default: return 'unknown'
  }
}

function timingSourceDescription(source?: string) {
  switch ((source || '').toLowerCase()) {
    case 'backend_decode':
      return 'InferenceBridge/llama.cpp supplied authoritative decode timing. Prompt processing and model load are excluded.'
    case 'wall_clock_e2e':
      return 'The backend omitted decode timing. This is completion tokens divided by complete warm request wall time, including prompt processing.'
    case 'estimated_wall_clock':
      return 'The backend omitted token usage and decode timing. Tokens and warm end-to-end throughput are locally estimated.'
    case 'mixed':
      return 'Measured text runs used more than one timing source.'
    default:
      return 'Timing source was not recorded by this older benchmark run.'
  }
}

function scenarioTimingSummary(sc: ProfileBenchmarkResult['scenarios'][number]) {
  const decode = formatTPS(sc.decode_tokens_per_second)
  const e2e = formatTPS(sc.end_to_end_tokens_per_second)
  return `decode ${decode} / E2E ${e2e} / ${timingSourceShort(sc.timing_source)} / TTFT ${sc.ttf_ms ?? 0} ms`
}

function uniqueModels(models: string[]) {
  return models.map(model => model.trim()).filter(Boolean).filter((model, index, arr) => arr.indexOf(model) === index)
}

function scenarioPurpose(name: string) {
  const lower = name.toLowerCase()
  if (lower.includes('tool')) return 'Checks structured tool-call protocol and repair pressure.'
  if (lower.includes('json')) return 'Checks clean structured output without prose leakage.'
  if (lower.includes('code')) return 'Checks compact coding response quality.'
  return 'Measures text generation speed and basic instruction following.'
}

function profileChanges(current?: ProfilesFile['profiles'][string], recommended?: ProfilesFile['profiles'][string]) {
  if (!recommended) return []
  const changes: string[] = []
  const fields: Array<keyof ProfilesFile['profiles'][string]> = ['model_id', 'ctx_tokens', 'thinking', 'preserve_thinking', 'spec_type', 'spec_draft_n_max']
  for (const field of fields) {
    if (current?.[field] !== recommended[field]) changes.push(`${String(field)}: ${String(current?.[field] ?? 'n/a')} -> ${String(recommended[field] ?? 'n/a')}`)
  }
  return changes
}

function buildMatrixSummary(rows: MatrixRow[]) {
  const bestTextRow = [...rows].filter(row => benchmarkTextSpeed(row.run) > 0).sort((a, b) => benchmarkTextSpeed(b.run) - benchmarkTextSpeed(a.run))[0]
  const bestLoopRow = [...rows].filter(row => row.loop?.pass).sort((a, b) => benchmarkTextSpeed(b.run) - benchmarkTextSpeed(a.run))[0]
  const toolOK = rows.filter(row => {
    const sc = row.run.scenarios?.find(item => item.name === 'Tool call')
    return (sc?.structured_tools ?? 0) > 0
  }).length
  const failures = rows.filter(row => row.run.status !== 'ok' || Boolean(row.loop && !row.loop.pass)).length
  return {
    rows: rows.length,
    bestText: bestTextRow ? `${shortModelName(bestTextRow.run.model_id || '')} ${headlineSpeed(bestTextRow.run)}` : 'n/a',
    bestLoop: bestLoopRow ? `${shortModelName(bestLoopRow.run.model_id || '')} score ${bestLoopRow.loop?.stability_score ?? 0}` : 'n/a',
    toolOK: rows.length ? `${toolOK}/${rows.length}` : '0/0',
    failures,
  }
}

function loopReliabilityLabel(loop?: AgentEvalResult) {
  if (!loop) return 'pending'
  const status = loop.pass ? 'pass' : 'fail'
  return `${status} s${loop.stability_score ?? 0} ${loop.tool_success_rate ?? 0}%`
}

function loopReliabilityTitle(loop?: AgentEvalResult) {
  if (!loop) return ''
  return [
    loop.fail_reason || '',
    `tools=${loop.tool_calls ?? 0}`,
    `success=${loop.tool_success_rate ?? 0}%`,
    `repeats=${loop.repeat_tool_rate ?? 0}%`,
    `repeated_inputs=${loop.repeated_tool_inputs ?? 0}`,
    `repeated_skips=${loop.repeated_skips ?? 0}`,
    `verifier_prompts=${loop.verifier_prompts ?? 0}`,
    `max_routed_tools=${loop.max_routed_tools ?? 0}`,
    `prompt_warnings=${loop.prompt_warnings ?? 0}`,
    `false_done=${Boolean(loop.false_done)}`,
  ].filter(Boolean).join('\n')
}

function matrixRowsToCSV(rows: MatrixRow[]) {
  const header = ['model', 'requested_ctx', 'actual_ctx', 'status', 'text_tps', 'decode_tps', 'warm_e2e_tps', 'timing_source', 'measured_text_runs', 'load_ms', 'warmup_ttft_ms', 'text_ttft_ms', 'prompt_ms', 'decode_ms', 'tool_structured', 'tool_repaired', 'loop_pass', 'loop_tools', 'loop_success_rate', 'loop_repeat_rate', 'loop_stability_score', 'loop_max_routed_tools', 'loop_prompt_warnings', 'loop_false_done', 'loop_duration_ms', 'total_ms', 'summary']
  const lines = rows.map(row => {
    const toolCase = row.run.scenarios?.find(sc => sc.name === 'Tool call')
    return [
      row.run.model_id || '',
      row.run.ctx_tokens ?? '',
      row.run.actual_ctx_tokens ?? '',
      row.run.status,
      benchmarkTextSpeed(row.run) || '',
      row.run.decode_tokens_per_second ?? '',
      row.run.end_to_end_tokens_per_second ?? '',
      row.run.timing_source ?? '',
      row.run.measured_text_runs ?? '',
      row.run.load_ms ?? '',
      row.run.warmup?.ttf_ms ?? '',
      row.run.text_ttf_ms ?? row.run.ttf_ms ?? '',
      row.run.prompt_ms ?? '',
      row.run.decode_ms ?? '',
      toolCase?.structured_tools ?? '',
      toolCase?.repaired_tools ?? '',
      row.loop ? String(row.loop.pass) : '',
      row.loop?.tool_calls ?? '',
      row.loop?.tool_success_rate ?? '',
      row.loop?.repeat_tool_rate ?? '',
      row.loop?.stability_score ?? '',
      row.loop?.max_routed_tools ?? '',
      row.loop?.prompt_warnings ?? '',
      row.loop ? String(Boolean(row.loop.false_done)) : '',
      row.loop?.duration_ms ?? '',
      row.run.total_ms ?? '',
      row.run.summary || row.loop?.fail_reason || '',
    ].map(csvCell).join(',')
  })
  return [header.join(','), ...lines].join('\n')
}

function csvCell(value: unknown) {
  const text = String(value ?? '')
  return /[",\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text
}

function formatDate(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
