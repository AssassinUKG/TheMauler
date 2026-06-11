import { useState, useEffect } from 'react'
import {
  GetSettings,
  UpdateSettings,
  GetProfiles,
  UpdateProfiles,
  PingProvider,
  ListModelsForProvider,
  ListWSLDistros,
  BenchmarkProfile,
  UseProfile,
  type Settings,
  type ProfilesFile,
  type Profile,
  type Provider,
  type GenerationParams,
  type ProfileBenchmarkResult,
} from '../wailsjs/go'
import { ConfirmDialog } from './ConfirmDialog'
import './SettingsModal.css'

interface Props {
  onClose: () => void
  onSaved?: () => void
}

type Tab = 'general' | 'providers' | 'profiles' | 'agents' | 'tools' | 'context' | 'ui' | 'image'

type ToolRisk = 'low' | 'medium' | 'high'

const toolRisk: Record<string, ToolRisk> = {
  read_file: 'low',
  read_many: 'low',
  file_outline: 'low',
  read_chunks: 'low',
  read_pdf: 'low',
  glob: 'low',
  grep: 'low',
  session_search: 'low',
  sqlite_schema: 'low',
  sqlite_query: 'low',
  todo_create: 'low',
  todo_update: 'low',
  todo_done: 'low',
  todo_blocked: 'low',
  todo_list: 'low',
  todo_clear: 'low',
  skills_list: 'low',
  skill_view: 'low',
  fetch_url: 'medium',
  web_search: 'medium',
  browser_open: 'medium',
  browser_snapshot: 'medium',
  browser_extract: 'medium',
  browser_screenshot: 'medium',
  write_file: 'high',
  edit_file: 'high',
  shell: 'high',
  bash: 'high',
  browser_click: 'high',
  browser_type: 'high',
  browser_agent: 'high',
}

const toolRiskLabel: Record<ToolRisk, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
}

const onlineTools = new Set([
  'web_search',
  'fetch_url',
  'browser_open',
  'browser_snapshot',
  'browser_click',
  'browser_type',
  'browser_extract',
  'browser_screenshot',
  'browser_close',
  'browser_agent',
  'subagent_research',
])

const preferredOnlineToolset = (name: string) =>
  name.startsWith('browser_') || name === 'browser_agent' ? 'browser' : 'web-research'

export function SettingsModal({ onClose, onSaved }: Props) {
  const [tab, setTab] = useState<Tab>('providers')
  const [settings, setSettings] = useState<Settings | null>(null)
  const [profilesFile, setProfilesFile] = useState<ProfilesFile | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveStatus, setSaveStatus] = useState('')
  const [pingResult, setPingResult] = useState('')
  const [models, setModels] = useState<string[]>([])
  const [profileModels, setProfileModels] = useState<string[]>([])
  const [wslDistros, setWslDistros] = useState<string[]>([])
  const [selectedProvider, setSelectedProvider] = useState('')
  const [selectedProfile, setSelectedProfile] = useState('')
  const [deleteProfileConfirm, setDeleteProfileConfirm] = useState<string | null>(null)
  const [benchmarking, setBenchmarking] = useState(false)
  const [benchmarkResult, setBenchmarkResult] = useState<ProfileBenchmarkResult | null>(null)

  useEffect(() => {
    void Promise.all([GetSettings(), GetProfiles()]).then(([s, pf]) => {
      setSettings(s)
      setProfilesFile(pf)
      const providerNames = Object.keys(pf.providers ?? {})
      const profileNames = Object.keys(pf.profiles ?? {}).filter(name => Boolean(pf.profiles[name]?.model_id?.trim()))
      if (profileNames.length > 0) {
        const active = pf.profiles[s.active_profile] ? s.active_profile : profileNames[0]
        setSelectedProfile(active)
        setSelectedProvider(pf.profiles[active]?.provider || providerNames[0] || '')
      } else if (providerNames.length > 0) {
        setSelectedProvider(providerNames[0])
      }
    }).catch(() => {})
    void ListWSLDistros().then(setWslDistros).catch(() => setWslDistros([]))
  }, [])

  const markDirty = () => {
    setDirty(true)
    setSaveStatus('')
  }

  const save = async (closeAfter = false) => {
    if (!settings || !profilesFile) return
    setSaving(true)
    try {
      await UpdateSettings(settings)
      await UpdateProfiles(profilesFile)
      setDirty(false)
      setSaveStatus('Saved')
      onSaved?.()
      if (closeAfter) onClose()
    } catch (e) {
      alert(`Save failed: ${e}`)
    } finally {
      setSaving(false)
    }
  }

  const close = async () => {
    if (dirty) await save(true)
    else onClose()
  }

  const updateSettings = <K extends keyof Settings>(key: K, val: Settings[K]) => {
    setSettings(prev => prev ? { ...prev, [key]: val } : prev)
    markDirty()
  }

  const setToolEnabled = (toolName: string, enabled: boolean) => {
    setSettings(prev => {
      if (!prev) return prev
      const currentToolset = prev.tools.active_toolset || 'balanced'
      const toolsetTools = prev.tools.toolsets?.[currentToolset] ?? []
      const needsOnlineToolset = enabled && onlineTools.has(toolName) && !toolsetTools.includes(toolName)
      const nextToolset = needsOnlineToolset ? preferredOnlineToolset(toolName) : prev.tools.active_toolset
      return {
        ...prev,
        agents: needsOnlineToolset ? { ...prev.agents, offline_only: false } : prev.agents,
        tools: {
          ...prev.tools,
          active_toolset: nextToolset,
          enabled_tools: {
            ...(prev.tools.enabled_tools ?? {}),
            [toolName]: enabled,
            ...(toolName === 'shell' ? { bash: enabled } : {}),
          },
        },
      }
    })
    markDirty()
  }

  const updateProviderField = (name: string, field: keyof Provider, val: unknown) => {
    setProfilesFile(prev => {
      if (!prev) return prev
      return {
        ...prev,
        providers: {
          ...prev.providers,
          [name]: { ...prev.providers[name], [field]: val },
        },
      }
    })
    setPingResult('')
    setModels([])
    markDirty()
  }

  const updateProfileField = (name: string, field: keyof Profile, val: unknown) => {
    setProfilesFile(prev => {
      if (!prev) return prev
      return {
        ...prev,
        profiles: {
          ...prev.profiles,
          [name]: { ...prev.profiles[name], [field]: val },
        },
      }
    })
    markDirty()
  }

  const replaceProfile = (name: string, next: Profile) => {
    setProfilesFile(prev => {
      if (!prev) return prev
      return {
        ...prev,
        profiles: {
          ...prev.profiles,
          [name]: { ...next, name },
        },
      }
    })
    markDirty()
  }

  const updateParams = (
    profileName: string,
    paramKey: 'thinking_general' | 'thinking_coding' | 'nothinking',
    field: keyof GenerationParams,
    val: number,
  ) => {
    const safeVal = field === 'max_tokens' ? Math.max(256, Math.min(32768, Math.round(val))) : val
    setProfilesFile(prev => {
      if (!prev) return prev
      const p = prev.profiles[profileName]
      return {
        ...prev,
        profiles: {
          ...prev.profiles,
          [profileName]: {
            ...p,
            [paramKey]: { ...p[paramKey], [field]: safeVal },
          },
        },
      }
    })
    markDirty()
  }

  const updateAgentPreset = (name: string, patch: Partial<Settings['agents']['presets'][string]>) => {
    setSettings(prev => {
      if (!prev) return prev
      const preset = prev.agents.presets?.[name] ?? {
        enabled: true,
        profile: '',
        context_budget: 32768,
        autonomy: prev.agents.default_autonomy || 'balanced',
        toolset: prev.tools.active_toolset || 'balanced',
        instructions: '',
        tool_permissions: {},
      }
      return {
        ...prev,
        agents: {
          ...prev.agents,
          presets: {
            ...(prev.agents.presets ?? {}),
            [name]: { ...preset, ...patch },
          },
        },
      }
    })
    markDirty()
  }

  const updateAgentPresetTool = (presetName: string, toolName: string, enabled: boolean) => {
    const preset = settings?.agents.presets?.[presetName]
    updateAgentPreset(presetName, {
      tool_permissions: {
        ...(preset?.tool_permissions ?? {}),
        [toolName]: enabled,
      },
    })
  }

  const removeSafeRule = (id: string) => {
    if (!settings) return
    updateSettings('tools', {
      ...settings.tools,
      safe_rules: (settings.tools.safe_rules ?? []).filter(rule => rule.id !== id),
    })
  }

  const newProfile = () => {
    if (!profilesFile) return
    const base = 'new-profile'
    let name = base
    let i = 2
    while (profilesFile.profiles[name]) {
      name = `${base}-${i++}`
    }
    const firstProvider = Object.keys(profilesFile.providers ?? {})[0] ?? ''
    const blank: Profile = {
      name,
      provider: firstProvider,
      model_id: '',
      ctx_tokens: 32768,
      thinking: false,
      preserve_thinking: false,
      mmproj: '',
      thinking_general: { temperature: 0.6, top_p: 0.95, top_k: 40, min_p: 0, presence_penalty: 0, max_tokens: 8192, seed: -1 },
      thinking_coding: { temperature: 0.6, top_p: 0.95, top_k: 40, min_p: 0, presence_penalty: 0, max_tokens: 8192, seed: -1 },
      nothinking: { temperature: 0.7, top_p: 0.95, top_k: 40, min_p: 0, presence_penalty: 0, max_tokens: 4096, seed: -1 },
      spec_type: '',
      spec_draft_n_max: 0,
      spec_draft_model: '',
    }
    setProfilesFile({
      ...profilesFile,
      profiles: { ...profilesFile.profiles, [name]: blank },
    })
    setSelectedProfile(name)
    markDirty()
  }

  const duplicateProfile = () => {
    if (!profilesFile || !profile || !selectedProfile) return
    const base = `${selectedProfile}-copy`
    let name = base
    let i = 2
    while (profilesFile.profiles[name]) {
      name = `${base}-${i++}`
    }
    setProfilesFile({
      ...profilesFile,
      profiles: {
        ...profilesFile.profiles,
        [name]: { ...profile, name },
      },
    })
    setSelectedProfile(name)
    markDirty()
  }

  const deleteProfile = () => {
    if (!settings || !profilesFile || !selectedProfile || profileNames.length <= 1) return
    setDeleteProfileConfirm(selectedProfile)
  }

  const confirmDeleteProfile = () => {
    if (!settings || !profilesFile || !deleteProfileConfirm || profileNames.length <= 1) return
    const name = deleteProfileConfirm
    const nextProfiles = { ...profilesFile.profiles }
    delete nextProfiles[name]
    const nextName = Object.keys(nextProfiles).find(n => Boolean(nextProfiles[n]?.model_id?.trim())) ?? ''
    setProfilesFile({ ...profilesFile, profiles: nextProfiles })
    setSelectedProfile(nextName)
    if (settings.active_profile === name && nextName) {
      setSettings({ ...settings, active_profile: nextName })
    }
    setDeleteProfileConfirm(null)
    markDirty()
  }

  if (!settings || !profilesFile) {
    return (
      <div className="overlay">
        <div className="settings-modal">
          <div style={{ padding: 20, color: 'var(--text-dim)' }}>Loading...</div>
        </div>
      </div>
    )
  }

  const providerNames = Object.keys(profilesFile.providers ?? {})
  const profileNames = Object.keys(profilesFile.profiles ?? {}).filter(name => Boolean(profilesFile.profiles[name]?.model_id?.trim()))
  const toolsetNames = Object.keys(settings.tools.toolsets ?? {}).sort()
  const provider = profilesFile.providers[selectedProvider]
  const profile = profilesFile.profiles[selectedProfile]

  const handlePing = async () => {
    if (!provider) return
    setPingResult('...')
    const r = await PingProvider(provider).catch(e => `error: ${e}`)
    setPingResult(r)
  }

  const handleListModels = async () => {
    if (!provider) return
    const ms = await ListModelsForProvider(provider).catch(() => [] as string[])
    setModels(ms)
  }

  const fetchProfileModels = async () => {
    if (!profile || !profilesFile) return
    const prov = profilesFile.providers[profile.provider]
    if (!prov) return
    setProfileModels([])
    const ms = await ListModelsForProvider(prov).catch(() => [] as string[])
    setProfileModels(ms)
  }

  const handleUseProfile = async () => {
    if (!selectedProfile) return
    setSaving(true)
    try {
      await UseProfile(selectedProfile, settings, profilesFile)
      setSettings({ ...settings, active_profile: selectedProfile })
      setDirty(false)
      setSaveStatus(`Using ${selectedProfile}`)
      onSaved?.()
    } catch (e) {
      alert(`Switch failed: ${e}`)
    } finally {
      setSaving(false)
    }
  }

  const handleBenchmarkProfile = async () => {
    if (!profile || !profilesFile || !selectedProfile) return
    const prov = profilesFile.providers[profile.provider]
    if (!prov) {
      setBenchmarkResult({
        status: 'warn',
        summary: `Provider ${profile.provider} was not found.`,
        notes: [],
        scenarios: [],
        recommended_profile: profile,
      })
      return
    }
    setBenchmarking(true)
    setBenchmarkResult(null)
    try {
      const res = await BenchmarkProfile({ ...profile, name: selectedProfile }, prov)
      setBenchmarkResult(res)
    } catch (e) {
      setBenchmarkResult({
        status: 'warn',
        summary: `Benchmark failed: ${e}`,
        notes: [],
        scenarios: [],
        recommended_profile: profile,
      })
    } finally {
      setBenchmarking(false)
    }
  }

  const applyBenchmarkRecommendation = () => {
    if (!benchmarkResult || !selectedProfile) return
    replaceProfile(selectedProfile, benchmarkResult.recommended_profile)
    setBenchmarkResult(null)
    setSaveStatus('Benchmark recommendations applied')
  }

  return (
    <div className="overlay" onClick={e => { if (e.target === e.currentTarget) void close() }}>
      <div className="settings-modal">
        <div className="settings-header">
          <span className="settings-title">Settings</span>
          <div className="settings-header-actions">
            {saveStatus && <span className="save-status">{saveStatus}</span>}
            {(dirty || saving) && (
              <button className="primary" onClick={() => void save()} disabled={saving}>
                {saving ? 'Saving...' : 'Save'}
              </button>
            )}
            <button onClick={() => void close()}>Close</button>
          </div>
        </div>

        <div className="settings-body">
          <div className="settings-tabs">
            {(['general', 'providers', 'profiles', 'agents', 'tools', 'context', 'ui', 'image'] as Tab[]).map(t => (
              <button
                key={t}
                className={`tab-btn ${tab === t ? 'active' : ''}`}
                onClick={() => setTab(t)}
              >
                {t.charAt(0).toUpperCase() + t.slice(1)}
              </button>
            ))}
          </div>

          <div className="settings-panel">
            {tab === 'general' && (
              <div className="settings-section">
                <h3>General</h3>
                <Field label="Active profile">
                  <select
                    value={settings.active_profile}
                    onChange={e => {
                      updateSettings('active_profile', e.target.value)
                      setSelectedProfile(e.target.value)
                    }}
                  >
                    {profileNames.map(n => <option key={n} value={n}>{n}</option>)}
                  </select>
                </Field>
                <Field label="Log level">
                  <select value={settings.log_level} onChange={e => updateSettings('log_level', e.target.value)}>
                    {['debug', 'info', 'warn', 'error'].map(l => <option key={l}>{l}</option>)}
                  </select>
                </Field>
              </div>
            )}

            {tab === 'providers' && (
              <div className="settings-section">
                <h3>Providers</h3>
                <div className="profile-selector">
                  {providerNames.map(n => (
                    <button
                      key={n}
                      className={`profile-tab ${selectedProvider === n ? 'active' : ''}`}
                      onClick={() => {
                        setSelectedProvider(n)
                        setPingResult('')
                        setModels([])
                      }}
                    >
                      {n}
                    </button>
                  ))}
                </div>

                {provider && (
                  <div className="profile-editor">
                    <div className="profile-actions">
                      <button onClick={() => void handlePing()}>Ping provider</button>
                      {pingResult && <span className={`ping-result ${pingResult === 'ok' ? 'ok' : 'fail'}`}>{pingResult}</span>}
                      <button onClick={() => void handleListModels()}>List models</button>
                    </div>

                    {models.length > 0 && (
                      <div className="model-list">
                        {models.map(m => (
                          <button
                            key={m}
                            className="model-item"
                            onClick={() => {
                              if (profile && profile.provider === selectedProvider) {
                                updateProfileField(selectedProfile, 'model_id', m)
                                setTab('profiles')
                              }
                            }}
                          >
                            {m}
                          </button>
                        ))}
                      </div>
                    )}

                    <Field label="Backend">
                      <select value={provider.backend} onChange={e => updateProviderField(selectedProvider, 'backend', e.target.value)}>
                        {['llamacpp', 'lmstudio'].map(b => <option key={b}>{b}</option>)}
                      </select>
                    </Field>
                    <Field label="Base URL">
                      <input value={provider.base_url} onChange={e => updateProviderField(selectedProvider, 'base_url', e.target.value)} />
                    </Field>
                    <Field label="API key env">
                      <input value={provider.api_key_env ?? ''} onChange={e => updateProviderField(selectedProvider, 'api_key_env', e.target.value)} />
                    </Field>
                  </div>
                )}
              </div>
            )}

            {tab === 'profiles' && (
              <div className="settings-section">
                <h3>Profiles</h3>
                <div className="profile-selector">
                  {profileNames.map(n => (
                    <button
                      key={n}
                      className={`profile-tab ${selectedProfile === n ? 'active' : ''}`}
                      onClick={() => {
                        setSelectedProfile(n)
                        setSelectedProvider(profilesFile.profiles[n]?.provider || selectedProvider)
                      }}
                    >
                      {n}
                    </button>
                  ))}
                  <button
                    className="profile-tab profile-tab-new"
                    onClick={newProfile}
                    title="Create a new blank profile"
                  >
                    + New
                  </button>
                </div>

                {profile && (
                  <div className="profile-editor">
                    <div className="profile-actions">
                      <button
                        className="primary"
                        onClick={() => void handleUseProfile()}
                        disabled={saving || settings.active_profile === selectedProfile}
                      >
                        {settings.active_profile === selectedProfile ? 'Active profile' : 'Use this profile'}
                      </button>
                      <button onClick={newProfile} title="Create a blank profile">New</button>
                      <button onClick={duplicateProfile} title="Clone this profile">Duplicate</button>
                      <button onClick={() => void handleBenchmarkProfile()} disabled={benchmarking} title="Probe the selected provider and recommend model settings">
                        {benchmarking ? 'Benchmarking...' : 'Benchmark LLM'}
                      </button>
                      <button onClick={deleteProfile} disabled={profileNames.length <= 1} title="Delete this profile">Delete</button>
                    </div>

                    {benchmarkResult && (
                      <div className={`benchmark-card benchmark-${benchmarkResult.status}`}>
                        <div className="benchmark-card-head">
                          <div>
                            <div className="benchmark-title">{benchmarkResult.summary}</div>
                            <div className="benchmark-metrics">
                              {benchmarkResult.ttf_ms ? `TTFT ${benchmarkResult.ttf_ms} ms` : 'TTFT n/a'}
                              {' · '}
                              {benchmarkResult.tokens_per_second ? `${benchmarkResult.tokens_per_second.toFixed(1)} tok/s` : 'tok/s n/a'}
                              {' · '}
                              {benchmarkResult.completion_tokens ?? 0} output tokens
                            </div>
                          </div>
                          <button onClick={applyBenchmarkRecommendation}>Apply</button>
                        </div>
                        {benchmarkResult.notes.length > 0 && (
                          <ul className="benchmark-notes">
                            {benchmarkResult.notes.map((note, idx) => <li key={idx}>{note}</li>)}
                          </ul>
                        )}
                        {benchmarkResult.scenarios?.length > 0 && (
                          <div className="benchmark-scenarios">
                            {benchmarkResult.scenarios.map(sc => (
                              <div key={sc.name} className={`benchmark-scenario scenario-${sc.status}`}>
                                <div className="scenario-name">{sc.name}</div>
                                <div className="scenario-summary">{sc.summary}</div>
                                <div className="scenario-metrics">
                                  TTFT {sc.ttf_ms ?? 0} ms
                                  {' · '}
                                  {sc.tokens_per_second ? sc.tokens_per_second.toFixed(1) : '0.0'} tok/s
                                  {' · '}
                                  {sc.completion_tokens ?? 0} out
                                  {sc.structured_tools || sc.repaired_tools || sc.inline_tool_markup
                                    ? ` · tools native ${sc.structured_tools ?? 0}, repaired ${sc.repaired_tools ?? 0}`
                                    : ''}
                                  {sc.output_leak ? ' · output leak' : ''}
                                </div>
                                {sc.error && <div className="scenario-error">{sc.error}</div>}
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )}

                    <Field label="Provider">
                      <select
                        value={profile.provider}
                        onChange={e => {
                          updateProfileField(selectedProfile, 'provider', e.target.value)
                          setSelectedProvider(e.target.value)
                        }}
                      >
                        {providerNames.map(n => <option key={n} value={n}>{n}</option>)}
                      </select>
                    </Field>
                    <Field label="Model ID">
                      <div className="model-id-row">
                        <input
                          value={profile.model_id}
                          onChange={e => {
                            updateProfileField(selectedProfile, 'model_id', e.target.value)
                            setProfileModels([])
                          }}
                        />
                        <button onClick={() => void fetchProfileModels()} title="Fetch models from provider">Pick</button>
                      </div>
                    </Field>
                    {profileModels.length > 0 && (
                      <div className="model-list">
                        {profileModels.map(m => (
                          <button
                            key={m}
                            className={`model-item ${m === profile.model_id ? 'active' : ''}`}
                            onClick={() => { updateProfileField(selectedProfile, 'model_id', m); setProfileModels([]) }}
                          >
                            {m}
                          </button>
                        ))}
                      </div>
                    )}
                    <Field label="Context tokens">
                      <input type="number" value={profile.ctx_tokens} onChange={e => updateProfileField(selectedProfile, 'ctx_tokens', parseInt(e.target.value, 10) || 0)} />
                    </Field>

                    <div className={`thinking-card ${profile.thinking ? 'thinking-on' : 'thinking-off'}`}>
                      <div className="thinking-card-head">
                        <div>
                          <div className="thinking-title">Thinking behaviour</div>
                          <div className="thinking-subtitle">
                            {profile.thinking
                              ? 'This profile uses reasoning-oriented parameters.'
                              : 'This profile uses the faster no-thinking parameter set.'}
                          </div>
                        </div>
                        <div className="thinking-toggle-group">
                          <button
                            className={!profile.thinking ? 'active' : ''}
                            onClick={() => updateProfileField(selectedProfile, 'thinking', false)}
                            type="button"
                          >
                            Off
                          </button>
                          <button
                            className={profile.thinking ? 'active' : ''}
                            onClick={() => updateProfileField(selectedProfile, 'thinking', true)}
                            type="button"
                          >
                            On
                          </button>
                        </div>
                      </div>
                      <div className="thinking-options">
                        <label className="checkbox-label">
                          <input
                            type="checkbox"
                            checked={profile.preserve_thinking}
                            disabled={!profile.thinking}
                            onChange={e => updateProfileField(selectedProfile, 'preserve_thinking', e.target.checked)}
                          />
                          Show preserved thinking in chat
                        </label>
                      </div>
                    </div>

                    {profile.thinking ? (
                      <>
                        <h4>Thinking general</h4>
                        <ParamsEditor params={profile.thinking_general} onChange={(f, v) => updateParams(selectedProfile, 'thinking_general', f, v)} />

                        <h4>Thinking coding</h4>
                        <ParamsEditor params={profile.thinking_coding} onChange={(f, v) => updateParams(selectedProfile, 'thinking_coding', f, v)} />
                      </>
                    ) : (
                      <>
                        <h4>No-thinking</h4>
                        <ParamsEditor params={profile.nothinking} onChange={(f, v) => updateParams(selectedProfile, 'nothinking', f, v)} />
                      </>
                    )}

                    <h4>MTP Speculative Decoding <span style={{fontWeight:400,fontSize:'11px',color:'var(--text-dim)'}}>llama.cpp b9180+ · 1.4–2.2× faster</span></h4>
                    <Field label="Spec type">
                      <select
                        value={profile.spec_type ?? ''}
                        onChange={e => updateProfileField(selectedProfile, 'spec_type', e.target.value)}
                      >
                        <option value="">Disabled</option>
                        <option value="draft-mtp">draft-mtp (Gemma / Qwen MTP)</option>
                      </select>
                    </Field>
                    <Field label="Draft model path">
                      <input
                        value={profile.spec_draft_model ?? ''}
                        disabled={!profile.spec_type}
                        onChange={e => updateProfileField(selectedProfile, 'spec_draft_model', e.target.value)}
                        placeholder="C:\\path\\to\\draft-model.gguf"
                      />
                      <span className="field-hint">Passed to llama.cpp as -md / draft_model_path.</span>
                    </Field>
                    <Field label="Draft tokens per step">
                      <input
                        type="number" min={1} max={8}
                        value={profile.spec_draft_n_max ?? 0}
                        disabled={!profile.spec_type}
                        onChange={e => updateProfileField(selectedProfile, 'spec_draft_n_max', parseInt(e.target.value, 10) || 0)}
                      />
                      <span className="field-hint">0 = server default · 2 is safe for Qwen3.6</span>
                    </Field>
                  </div>
                )}
              </div>
            )}

            {tab === 'agents' && (
              <div className="settings-section">
                <h3>Agents</h3>
                <Field label="Mode override">
                  <select
                    value={settings.agents.mode_override || 'Auto'}
                    onChange={e => updateSettings('agents', { ...settings.agents, mode_override: e.target.value })}
                  >
                    {['Auto', 'Manual', 'Builder', 'Fixer', 'Reviewer', 'Researcher', 'Planner'].map(mode => (
                      <option key={mode} value={mode}>{mode}</option>
                    ))}
                  </select>
                </Field>
                <Field label="Default autonomy">
                  <select
                    value={settings.agents.default_autonomy || 'balanced'}
                    onChange={e => updateSettings('agents', { ...settings.agents, default_autonomy: e.target.value })}
                  >
                    <option value="ask">ask</option>
                    <option value="balanced">balanced</option>
                    <option value="full">full / unrestricted</option>
                  </select>
                </Field>
                <Field label="Access presets">
                  <div className="settings-preset-help">
                    <strong>Unrestricted</strong> runs enabled tools without prompts. <strong>Balanced</strong> keeps web/browser enabled but asks before shell and writes. <strong>Offline</strong> disables web/browser tools for local-only work.
                  </div>
                </Field>
                <Field label="Offline only">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.offline_only}
                      onChange={e => updateSettings('agents', { ...settings.agents, offline_only: e.target.checked })}
                    />
                    Disable web and browser tools for routed agents
                  </label>
                </Field>
                <Field label="Require plan">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.require_plan}
                      onChange={e => updateSettings('agents', { ...settings.agents, require_plan: e.target.checked })}
                    />
                    Ask agents to state a concise plan for substantial tasks
                  </label>
                </Field>
                <Field label="Max tool calls">
                  <input
                    type="number"
                    min={1}
                    max={200}
                    value={settings.agents.max_tool_calls}
                    onChange={e => updateSettings('agents', { ...settings.agents, max_tool_calls: parseInt(e.target.value, 10) || 40 })}
                  />
                </Field>
                <Field label="Disable thinking after N tool calls">
                  <input
                    type="number"
                    min={1}
                    max={20}
                    value={settings.agents.no_think_after_tool_calls ?? 3}
                    onChange={e => updateSettings('agents', { ...settings.agents, no_think_after_tool_calls: parseInt(e.target.value, 10) || 3 })}
                  />
                  <span className="field-hint">Qwen3 fix: disables &lt;think&gt; once this many tool calls have been made (default 3)</span>
                </Field>

                <div className="preset-editor-list">
                  {['Auto', 'Builder', 'Fixer', 'Reviewer', 'Researcher', 'Planner'].map(name => {
                    const preset = settings.agents.presets?.[name]
                    if (!preset) return null
                    return (
                      <details key={name} className="preset-editor" open={name === 'Builder'}>
                        <summary>
                          <span>{name}</span>
                          <span className="preset-summary">{preset.autonomy || 'balanced'} / {preset.toolset || 'balanced'} / {preset.context_budget || 32768} ctx</span>
                        </summary>
                        <Field label="Enabled">
                          <label className="checkbox-label">
                            <input
                              type="checkbox"
                              checked={preset.enabled}
                              onChange={e => updateAgentPreset(name, { enabled: e.target.checked })}
                            />
                            Route tasks to this mode
                          </label>
                        </Field>
                        <Field label="Preferred profile">
                          <select
                            value={preset.profile || ''}
                            onChange={e => updateAgentPreset(name, { profile: e.target.value })}
                          >
                            <option value="">Use active profile</option>
                            {profileNames.map(profileName => <option key={profileName} value={profileName}>{profileName}</option>)}
                          </select>
                        </Field>
                        <Field label="Context budget">
                          <input
                            type="number"
                            min={4096}
                            max={262144}
                            step={1024}
                            value={preset.context_budget || 32768}
                            onChange={e => updateAgentPreset(name, { context_budget: parseInt(e.target.value, 10) || 32768 })}
                          />
                        </Field>
                        <Field label="Autonomy">
                          <select
                            value={preset.autonomy || 'balanced'}
                            onChange={e => updateAgentPreset(name, { autonomy: e.target.value })}
                          >
                            {['ask', 'balanced', 'full'].map(mode => <option key={mode} value={mode}>{mode}</option>)}
                          </select>
                        </Field>
                        <Field label="Toolset">
                          <select
                            value={preset.toolset || settings.tools.active_toolset || 'balanced'}
                            onChange={e => updateAgentPreset(name, { toolset: e.target.value })}
                          >
                            {toolsetNames.map(toolset => <option key={toolset} value={toolset}>{toolset}</option>)}
                          </select>
                        </Field>
                        <Field label="Instructions">
                          <textarea
                            className="settings-textarea"
                            value={preset.instructions || ''}
                            onChange={e => updateAgentPreset(name, { instructions: e.target.value })}
                          />
                        </Field>
                        <Field label="Tool permissions">
                          <div className="tool-grid">
                            {knownTools(settings.tools.enabled_tools).map(toolName => (
                              <label className="checkbox-label" key={`${name}-${toolName}`}>
                                <input
                                  type="checkbox"
                                  checked={preset.tool_permissions?.[toolName] ?? settings.tools.enabled_tools?.[toolName] ?? true}
                                  onChange={e => updateAgentPresetTool(name, toolName, e.target.checked)}
                                />
                                {toolName}
                              </label>
                            ))}
                          </div>
                        </Field>
                      </details>
                    )
                  })}
                </div>
              </div>
            )}

            {tab === 'tools' && (
              <div className="settings-section">
                <h3>Tools</h3>
                <Field label="Tools enabled">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.tools.enabled}
                      onChange={e => updateSettings('tools', { ...settings.tools, enabled: e.target.checked })} />
                    Allow tool use
                  </label>
                </Field>
                <Field label="Confirm writes">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.tools.confirm_writes}
                      onChange={e => updateSettings('tools', { ...settings.tools, confirm_writes: e.target.checked })} />
                    Pause before write_file / edit_file
                  </label>
                </Field>
                <Field label="Confirm exec">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.tools.confirm_exec}
                      onChange={e => updateSettings('tools', { ...settings.tools, confirm_exec: e.target.checked })} />
                    Pause before shell execution
                  </label>
                </Field>
                <Field label="Shell backend">
                  <select value={settings.tools.shell_backend || 'auto'}
                    onChange={e => updateSettings('tools', { ...settings.tools, shell_backend: e.target.value })}>
                    <option value="auto">auto</option>
                    <option value="powershell">powershell</option>
                    <option value="cmd">cmd</option>
                    <option value="bash">bash</option>
                    <option value="wsl">wsl</option>
                  </select>
                </Field>
                <Field label="AI shell mode">
                  <select value={settings.tools.shell_mode || 'shared_terminal'}
                    onChange={e => updateSettings('tools', { ...settings.tools, shell_mode: e.target.value })}>
                    <option value="shared_terminal">shared terminal</option>
                    <option value="isolated">isolated one-shot</option>
                  </select>
                  <span className="field-hint">Shared terminal runs AI shell calls in the visible terminal; isolated keeps the old hidden one-shot behavior.</span>
                </Field>
                <Field label="WSL distro">
                  <select
                    value={settings.tools.shell_distro || ''}
                    disabled={(settings.tools.shell_backend || 'auto') !== 'wsl'}
                    onChange={e => updateSettings('tools', { ...settings.tools, shell_distro: e.target.value })}
                  >
                    <option value="">default distro</option>
                    {wslDistros.map(name => <option key={name} value={name}>{name}</option>)}
                  </select>
                  <span className="field-hint">Used when Shell backend is wsl; choose kali-linux for Kali tools</span>
                </Field>
                <Field label="WSL user">
                  <input
                    value={settings.tools.shell_user || ''}
                    disabled={(settings.tools.shell_backend || 'auto') !== 'wsl'}
                    onChange={e => updateSettings('tools', { ...settings.tools, shell_user: e.target.value })}
                    placeholder="root"
                  />
                  <span className="field-hint">Passed to wsl.exe as --user. Use root for Kali; WSL does not use a password here.</span>
                </Field>
                <Field label="Shell timeout (s)">
                  <input type="number" min={1} max={300} value={settings.tools.bash_timeout}
                    onChange={e => updateSettings('tools', { ...settings.tools, bash_timeout: parseInt(e.target.value, 10) || 120 })} />
                </Field>
                <Field label="Protected paths">
                  <textarea
                    value={(settings.tools.protected_paths ?? []).join('\n')}
                    onChange={e => updateSettings('tools', {
                      ...settings.tools,
                      protected_paths: e.target.value.split('\n').map(v => v.trim()).filter(Boolean)
                    })}
                    rows={3}
                  />
                  <span className="field-hint">Mauler blocks write/edit and destructive shell commands touching these paths</span>
                </Field>
                <Field label="Active toolset">
                  <select value={settings.tools.active_toolset || 'balanced'}
                    onChange={e => updateSettings('tools', { ...settings.tools, active_toolset: e.target.value })}>
                    {toolsetNames.map(name => <option key={name} value={name}>{name}</option>)}
                  </select>
                </Field>
                <Field label="Toolset contents">
                  <div className="safe-rule-list">
                    {toolsetNames.map(name => (
                      <div className="safe-rule" key={name}>
                        <div className="safe-rule-main">
                          <strong>{name}</strong>
                          <span>{(settings.tools.toolsets?.[name] ?? []).join(', ')}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </Field>
                <Field label="Web engine">
                  <select value={settings.tools.web_engine || 'auto'}
                    onChange={e => updateSettings('tools', { ...settings.tools, web_engine: e.target.value })}>
                    <option value="auto">auto</option>
                    <option value="duckduckgo">duckduckgo</option>
                    <option value="searxng">searxng</option>
                    <option value="brave">brave</option>
                  </select>
                </Field>
                <Field label="Web base URL">
                  <input value={settings.tools.web_base_url ?? ''}
                    onChange={e => updateSettings('tools', { ...settings.tools, web_base_url: e.target.value })}
                    placeholder="SearXNG URL, e.g. http://localhost:8081" />
                </Field>
                <Field label="Web API key env">
                  <input value={settings.tools.web_api_key_env ?? ''}
                    onChange={e => updateSettings('tools', { ...settings.tools, web_api_key_env: e.target.value })}
                    placeholder="BRAVE_API_KEY" />
                </Field>
                <Field label="Max searches">
                  <input type="number" min={1} max={50} value={settings.tools.max_searches ?? 8}
                    onChange={e => updateSettings('tools', { ...settings.tools, max_searches: parseInt(e.target.value, 10) || 8 })} />
                </Field>
                <Field label="Max fetches">
                  <input type="number" min={1} max={80} value={settings.tools.max_fetches ?? 12}
                    onChange={e => updateSettings('tools', { ...settings.tools, max_fetches: parseInt(e.target.value, 10) || 12 })} />
                </Field>
                <Field label="Max failed web attempts">
                  <input type="number" min={1} max={20} value={settings.tools.max_failed_fetches ?? 5}
                    onChange={e => updateSettings('tools', { ...settings.tools, max_failed_fetches: parseInt(e.target.value, 10) || 5 })} />
                </Field>
                <Field label="Max browser actions">
                  <input type="number" min={1} max={150} value={settings.tools.max_browser_actions ?? 35}
                    onChange={e => updateSettings('tools', { ...settings.tools, max_browser_actions: parseInt(e.target.value, 10) || 35 })} />
                </Field>
                <Field label="Max tool result chars">
                  <input type="number" min={0} max={100000} value={settings.tools.max_tool_result_chars ?? 8000}
                    onChange={e => updateSettings('tools', { ...settings.tools, max_tool_result_chars: parseInt(e.target.value, 10) || 0 })} />
                  <span className="field-hint">Truncates large tool outputs before they enter history. 0 = no limit. Default: 8000</span>
                </Field>
                <Field label="Tool access">
                  <div className="tool-grid">
                    {knownTools(settings.tools.enabled_tools).map(name => (
                      <label className="checkbox-label tool-risk-row" key={name}>
                        <span className="tool-risk-control">
                          <input
                            type="checkbox"
                            checked={settings.tools.enabled_tools?.[name] ?? true}
                            onChange={e => setToolEnabled(name, e.target.checked)}
                          />
                          {name}
                        </span>
                        <span className={`settings-risk settings-risk-${toolRisk[name] ?? 'medium'}`}>{toolRiskLabel[toolRisk[name] ?? 'medium']}</span>
                      </label>
                    ))}
                  </div>
                </Field>
                <Field label="Safe list">
                  <div className="safe-rule-list">
                    {(settings.tools.safe_rules ?? []).length === 0 ? (
                      <div className="safe-rule-empty">No remembered tool approvals</div>
                    ) : settings.tools.safe_rules.map(rule => (
                      <div className="safe-rule" key={rule.id || `${rule.tool}-${rule.input_hash}`}>
                        <div className="safe-rule-main">
                          <strong>{rule.tool}</strong>
                          <span>{rule.label || rule.input_hash}</span>
                        </div>
                        <button type="button" onClick={() => removeSafeRule(rule.id)}>Remove</button>
                      </div>
                    ))}
                  </div>
                </Field>
              </div>
            )}

            {tab === 'context' && (
              <div className="settings-section">
                <h3>Context</h3>
                <Field label="Compaction threshold">
                  <input type="number" min={0.5} max={0.99} step={0.01} value={settings.context.compaction_at}
                    onChange={e => updateSettings('context', { ...settings.context, compaction_at: parseFloat(e.target.value) || 0.85 })} />
                </Field>
                <Field label="MAULER.md path">
                  <input value={settings.context.mauler_md_path}
                    onChange={e => updateSettings('context', { ...settings.context, mauler_md_path: e.target.value })}
                    placeholder="auto-discover" />
                </Field>
                <Field label="Project docs max bytes">
                  <input type="number" min={4096} max={131072} step={1024} value={settings.context.project_doc_max_bytes || 32768}
                    onChange={e => updateSettings('context', { ...settings.context, project_doc_max_bytes: parseInt(e.target.value, 10) || 32768 })} />
                  <span className="field-hint">Caps layered project instruction files before they enter context. Master skills are registered from the Skills tab.</span>
                </Field>
                <Field label="Project doc filenames">
                  <input value={(settings.context.project_doc_fallback_filenames || ['MAULER.md', 'AGENTS.md']).join(', ')}
                    onChange={e => updateSettings('context', {
                      ...settings.context,
                      project_doc_fallback_filenames: e.target.value.split(',').map(v => v.trim()).filter(Boolean)
                    })} />
                  <span className="field-hint">Checked in order in each workspace directory; directory entries load all Markdown files</span>
                </Field>
                <Field label="Workspace dir">
                  <input value={settings.context.workspace_dir ?? ''}
                    onChange={e => updateSettings('context', { ...settings.context, workspace_dir: e.target.value })}
                    placeholder="auto-detect from launch path" />
                </Field>
                <Field label="Auto-inject file context">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.context.auto_inject_file}
                      onChange={e => updateSettings('context', { ...settings.context, auto_inject_file: e.target.checked })} />
                    Inject open file into context
                  </label>
                </Field>
              </div>
            )}

            {tab === 'ui' && (
              <div className="settings-section">
                <h3>UI</h3>
                <Field label="Theme">
                  <select value={settings.ui.theme}
                    onChange={e => {
                      updateSettings('ui', { ...settings.ui, theme: e.target.value })
                      document.documentElement.setAttribute('data-theme', e.target.value === 'light' ? 'light' : 'dark')
                    }}>
                    {['dark', 'light'].map(t => <option key={t}>{t}</option>)}
                  </select>
                </Field>
                <Field label="Accent color">
                  <div className="accent-picker">
                    {['#007acc', '#7c3aed', '#0ea5e9', '#10b981', '#f59e0b', '#ef4444', '#ec4899', '#f97316'].map(c => (
                      <button
                        key={c}
                        className={`accent-swatch ${(settings.ui.accent_color ?? '#007acc') === c ? 'active' : ''}`}
                        style={{ background: c }}
                        onClick={() => updateSettings('ui', { ...settings.ui, accent_color: c })}
                        title={c}
                      />
                    ))}
                    <input
                      type="color"
                      className="accent-custom"
                      value={settings.ui.accent_color ?? '#007acc'}
                      onChange={e => updateSettings('ui', { ...settings.ui, accent_color: e.target.value })}
                      title="Custom color"
                    />
                  </div>
                </Field>
                <Field label="Button color">
                  <div className="accent-picker">
                    {['#007acc', '#7c3aed', '#0ea5e9', '#10b981', '#f59e0b', '#ef4444', '#ec4899', '#f97316'].map(c => (
                      <button
                        key={c}
                        className={`accent-swatch ${(settings.ui.primary_color ?? '#007acc') === c ? 'active' : ''}`}
                        style={{ background: c }}
                        onClick={() => updateSettings('ui', { ...settings.ui, primary_color: c })}
                        title={c}
                      />
                    ))}
                    <input
                      type="color"
                      className="accent-custom"
                      value={settings.ui.primary_color ?? '#007acc'}
                      onChange={e => updateSettings('ui', { ...settings.ui, primary_color: e.target.value })}
                      title="Custom button color"
                    />
                  </div>
                </Field>
                <Field label="Status bar">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.status_bar}
                      onChange={e => updateSettings('ui', { ...settings.ui, status_bar: e.target.checked })} />
                    Show status bar
                  </label>
                </Field>
                <Field label="Token counter">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.token_counter}
                      onChange={e => updateSettings('ui', { ...settings.ui, token_counter: e.target.checked })} />
                    Show token usage bar
                  </label>
                </Field>
                <Field label="Think indicator">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.think_indicator}
                      onChange={e => updateSettings('ui', { ...settings.ui, think_indicator: e.target.checked })} />
                    Show thinking animation while streaming
                  </label>
                </Field>
                <Field label="Tool countdown">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.tool_countdown ?? false}
                      onChange={e => updateSettings('ui', { ...settings.ui, tool_countdown: e.target.checked })} />
                    Show countdown for long-running tool calls
                  </label>
                </Field>
                <Field label="Terminal default">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.terminal_default_open ?? false}
                      onChange={e => updateSettings('ui', { ...settings.ui, terminal_default_open: e.target.checked })} />
                    Open terminal by default
                  </label>
                </Field>
                <Field label="Terminal height">
                  <input type="number" min={100} max={600} value={settings.ui.terminal_height || 260}
                    onChange={e => updateSettings('ui', { ...settings.ui, terminal_height: parseInt(e.target.value, 10) || 260 })} />
                </Field>
                <Field label="Diff colours">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.diff_colours}
                      onChange={e => updateSettings('ui', { ...settings.ui, diff_colours: e.target.checked })} />
                    Colour-code diffs in code blocks
                  </label>
                </Field>
                <Field label="Timestamps in chat">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.chat_timestamps}
                      onChange={e => updateSettings('ui', { ...settings.ui, chat_timestamps: e.target.checked })} />
                    Show message timestamps
                  </label>
                </Field>
                <Field label="Syntax highlight">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.syntax_highlight}
                      onChange={e => updateSettings('ui', { ...settings.ui, syntax_highlight: e.target.checked })} />
                    Highlight code blocks
                  </label>
                </Field>
              </div>
            )}

            {tab === 'image' && (
              <div className="settings-section">
                <h3>Image &amp; Vision</h3>
                <Field label="Vision enabled">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.image.vision_enabled}
                      onChange={e => updateSettings('image', { ...settings.image, vision_enabled: e.target.checked })} />
                    Send images to the model (requires multimodal model)
                  </label>
                </Field>
                <Field label="Clipboard method">
                  <select value={settings.image.clipboard_method}
                    onChange={e => updateSettings('image', { ...settings.image, clipboard_method: e.target.value })}>
                    {['auto', 'powershell', 'xclip', 'wl-paste'].map(m => <option key={m}>{m}</option>)}
                  </select>
                </Field>
                <Field label="WSL path translate">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.image.wsl_path_translate}
                      onChange={e => updateSettings('image', { ...settings.image, wsl_path_translate: e.target.checked })} />
                    Translate /mnt/c/... paths to Windows C:\... when pasting
                  </label>
                </Field>
                <Field label="Max display width">
                  <input type="number" min={50} max={2000} step={50} value={settings.image.max_display_width}
                    onChange={e => updateSettings('image', { ...settings.image, max_display_width: parseInt(e.target.value) })} />
                  <span className="field-hint">px</span>
                </Field>
              </div>
            )}
          </div>
        </div>
      </div>
      {deleteProfileConfirm && (
        <ConfirmDialog
          title="Delete Profile"
          message={`Delete profile "${deleteProfileConfirm}"?`}
          confirmLabel="Delete"
          cancelLabel="Cancel"
          onAllow={confirmDeleteProfile}
          onDeny={() => setDeleteProfileConfirm(null)}
        />
      )}
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="field">
      <label className="field-label">{label}</label>
      <div className="field-control">{children}</div>
    </div>
  )
}

function knownTools(enabled: Record<string, boolean> | undefined): string[] {
  return Array.from(new Set([
    'read_file',
    'read_many',
    'read_pdf',
    'write_file',
    'edit_file',
    'shell',
    'bash',
    'glob',
    'grep',
    'web_search',
    'fetch_url',
    'browser_open',
    'browser_snapshot',
    'browser_click',
    'browser_type',
    'browser_extract',
    'browser_screenshot',
    'browser_close',
    'browser_agent',
    'session_search',
    'sqlite_schema',
    'sqlite_query',
    'todo_create',
    'todo_update',
    'todo_done',
    'todo_blocked',
    'todo_list',
    'todo_clear',
    'skills_list',
    'skill_view',
    ...Object.keys(enabled ?? {}),
  ])).sort()
}

function ParamsEditor({
  params,
  onChange,
}: {
  params: GenerationParams
  onChange: (field: keyof GenerationParams, value: number) => void
}) {
  if (!params) return null
  const numField = (label: string, key: keyof GenerationParams, step = 0.01) => (
    <Field label={label}>
      <input
        type="number"
        step={step}
        min={key === 'max_tokens' ? 256 : undefined}
        max={key === 'max_tokens' ? 32768 : undefined}
        value={params[key] ?? 0}
        onChange={e => onChange(key, parseFloat(e.target.value) || 0)}
      />
    </Field>
  )

  return (
    <div className="params-grid">
      {numField('Temperature', 'temperature')}
      {numField('Top P', 'top_p')}
      {numField('Top K', 'top_k', 1)}
      {numField('Min P', 'min_p')}
      {numField('Presence penalty', 'presence_penalty')}
      {numField('Max tokens', 'max_tokens', 256)}
    </div>
  )
}
