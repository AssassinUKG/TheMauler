import { useState, useEffect } from 'react'
import {
  GetSettings,
  ListAgentDefinitions,
  UpdateSettings,
  GetProfiles,
  UpdateProfiles,
  PingProvider,
  ListModelMetadataForProvider,
  GetProviderAPIKeyStatus,
  SetProviderAPIKey,
  ClearProviderAPIKey,
  ListWSLDistros,
  BenchmarkProfile,
  RecommendModelProfileTemplate,
  ClearStorageItem,
  ListStorageItems,
  UseProfile,
  GetChannelBusStatus,
  ListChannelWorkQueue,
  GetAudioHealth,
  RestartAudioWorker,
  SynthesizeSpeech,
  ListKokoroVoices,
  type Settings,
  type ProfilesFile,
  type Profile,
  type Provider,
  type ProviderAPIKeyStatus,
  type ModelMetadata,
  type GenerationParams,
  type ProfileBenchmarkResult,
  type StorageItem,
  type ChannelWorkItem,
  type AudioHealth,
  type AgentDefinition,
} from '../wailsjs/go'
import { ConfirmDialog } from './ConfirmDialog'
import './SettingsModal.css'

interface Props {
  onClose: () => void
  onSaved?: () => void
}

type Tab = 'general' | 'providers' | 'profiles' | 'agents' | 'environment' | 'tools' | 'telegram' | 'audio' | 'context' | 'storage' | 'ui' | 'image'

type ToolRisk = 'low' | 'medium' | 'high'

const settingsTabs: Array<{ id: Tab; label: string; description: string }> = [
  { id: 'general', label: 'General', description: 'Active profile and logging' },
  { id: 'providers', label: 'Providers', description: 'Local and cloud API endpoints' },
  { id: 'profiles', label: 'Profiles', description: 'Models and generation' },
  { id: 'agents', label: 'Agents', description: 'Modes and autonomy' },
  { id: 'environment', label: 'Environment', description: 'VPN, listener, shell paths' },
  { id: 'tools', label: 'Tools', description: 'Toolsets, shell, web limits' },
  { id: 'telegram', label: 'Telegram', description: 'Bot, voice, remote control' },
  { id: 'audio', label: 'Audio / Voice', description: 'Microphone and spoken replies' },
  { id: 'context', label: 'Context', description: 'Workspace and memory' },
  { id: 'storage', label: 'Storage', description: 'Caches and local state' },
  { id: 'ui', label: 'Interface', description: 'Theme and layout' },
  { id: 'image', label: 'Images', description: 'Vision and clipboard' },
]

const toolRisk: Record<string, ToolRisk> = {
  read: 'low',
  glob: 'low',
  grep: 'low',
  session_search: 'low',
  read_tool_result: 'low',
  file_changes: 'low',
  sqlite: 'low',
  todo_write: 'low',
  engagement: 'medium',
  skill: 'low',
  http_probe: 'medium',
  evidence_bundle: 'medium',
  task: 'medium',
  fetch_url: 'medium',
  web_search: 'medium',
  browser: 'high',
  write: 'high',
  edit: 'high',
  shell: 'high',
  terminal_send: 'high',
  terminal_read: 'low',
}

const toolRiskLabel: Record<ToolRisk, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
}

const onlineTools = new Set([
  'web_search',
  'fetch_url',
  'browser',
])

const preferredOnlineToolset = (name: string) =>
  name === 'browser' ? 'browser' : 'web-research'

const themeOptions = [
  { value: 'mauler-ops', label: 'Mauler Ops', accent: '#4ade80', primary: '#16a34a', note: 'Green-black operator console.' },
  { value: 'slate', label: 'Slate', accent: '#007acc', primary: '#007acc', note: 'Neutral VS Code-style dark.' },
  { value: 'light', label: 'Light', accent: '#0ea5e9', primary: '#0ea5e9', note: 'Bright desktop mode.' },
]

const accentSwatches = ['#4ade80', '#16a34a', '#22c55e', '#007acc', '#0ea5e9', '#7c3aed', '#f59e0b', '#ef4444', '#ec4899']

function voiceLabel(id: string): string {
  const accents: Record<string, string> = { af: 'American female', am: 'American male', bf: 'British female', bm: 'British male' }
  const [prefix, ...name] = id.split('_')
  const display = name.join(' ').replace(/\b\w/g, char => char.toUpperCase())
  return `${display || id} — ${accents[prefix] || 'Kokoro'} (${id})`
}

function previewTheme(theme: string) {
  const next = theme === 'dark' ? 'mauler-ops' : theme
  document.documentElement.setAttribute('data-theme', next)
}

function themeValue(theme: string | undefined): string {
  return theme === 'dark' || !theme ? 'mauler-ops' : theme
}

function previewAccent(hex: string) {
  document.documentElement.style.setProperty('--accent', hex)
  document.documentElement.style.setProperty('--accent-hover', hex)
  document.documentElement.style.setProperty('--accent-glow', `${hex}2e`)
  document.documentElement.style.setProperty('--accent-text', contrastText(hex))
}

function previewPrimary(hex: string) {
  document.documentElement.style.setProperty('--btn-primary', hex)
  document.documentElement.style.setProperty('--btn-primary-text', contrastText(hex))
}

function contrastText(hex: string): string {
  const h = hex.replace('#', '')
  const r = parseInt(h.slice(0, 2), 16)
  const g = parseInt(h.slice(2, 4), 16)
  const b = parseInt(h.slice(4, 6), 16)
  return (0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.55 ? '#111111' : '#ffffff'
}

function isOneTaskCloudProfile(name: string, profilesFile: ProfilesFile): boolean {
  const profile = profilesFile.profiles?.[name]
  if (!profile) return false
  return isCloudProvider(profile.provider, profilesFile)
}

const STANDARD_CLOUD_CONTEXT_TOKENS = 131072
const FALLBACK_CONTEXT_TOKENS = 32768
const STANDARD_MAX_OUTPUT_TOKENS = 8192

function isCloudProvider(providerName: string, profilesFile: ProfilesFile): boolean {
  const normalizedName = String(providerName || '').toLowerCase()
  const provider = profilesFile.providers?.[providerName]
  const baseURL = String(provider?.base_url || '').toLowerCase()
  return normalizedName === 'openrouter' || baseURL.includes('openrouter.ai')
}

function recommendedCloudContext(model: ModelMetadata): number {
  const hardLimit = Number(model.context_length || 0)
  return hardLimit > 0
    ? Math.min(hardLimit, STANDARD_CLOUD_CONTEXT_TOKENS)
    : FALLBACK_CONTEXT_TOKENS
}

function recommendedMaxOutput(model: ModelMetadata, contextTokens: number): number {
  const providerLimit = Number(model.max_completion_tokens || 0)
  return Math.max(256, Math.min(
    STANDARD_MAX_OUTPUT_TOKENS,
    providerLimit > 0 ? providerLimit : STANDARD_MAX_OUTPUT_TOKENS,
    Math.max(256, Math.floor(contextTokens / 2)),
  ))
}

function withCloudModelDefaults(profile: Profile, model: ModelMetadata): Profile {
  const ctxTokens = recommendedCloudContext(model)
  const maxTokens = recommendedMaxOutput(model, ctxTokens)
  return {
    ...profile,
    model_id: model.id,
    ctx_tokens: ctxTokens,
    thinking_general: { ...profile.thinking_general, max_tokens: maxTokens },
    thinking_coding: { ...profile.thinking_coding, max_tokens: maxTokens },
    nothinking: { ...profile.nothinking, max_tokens: maxTokens },
  }
}

function compactTokenCount(tokens: number): string {
  if (tokens >= 1048576) return `${(tokens / 1048576).toFixed(tokens % 1048576 === 0 ? 0 : 1)}M`
  if (tokens >= 1024) return `${Math.round(tokens / 1024)}K`
  return String(tokens)
}

function modelCatalogueLabel(model: ModelMetadata, cloud: boolean): string {
  if (!model.context_length) return model.id
  const maximum = `${compactTokenCount(model.context_length)} maximum`
  if (!cloud) return `${model.id} - ${maximum}`
  return `${model.id} - ${maximum} - ${compactTokenCount(recommendedCloudContext(model))} standard`
}

export function SettingsModal({ onClose, onSaved }: Props) {
  const [tab, setTab] = useState<Tab>('providers')
  const [settings, setSettings] = useState<Settings | null>(null)
  const [profilesFile, setProfilesFile] = useState<ProfilesFile | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveStatus, setSaveStatus] = useState('')
  const [pingResult, setPingResult] = useState('')
  const [models, setModels] = useState<ModelMetadata[]>([])
  const [profileModels, setProfileModels] = useState<ModelMetadata[]>([])
  const [selectedModelMetadata, setSelectedModelMetadata] = useState<ModelMetadata | null>(null)
  const [wslDistros, setWslDistros] = useState<string[]>([])
  const [selectedProvider, setSelectedProvider] = useState('')
  const [providerAPIKey, setProviderAPIKey] = useState('')
  const [providerAPIKeyStatus, setProviderAPIKeyStatus] = useState<ProviderAPIKeyStatus | null>(null)
  const [providerAPIKeySaving, setProviderAPIKeySaving] = useState(false)
  const [showProviderAPIKey, setShowProviderAPIKey] = useState(false)
  const [selectedProfile, setSelectedProfile] = useState('')
  const [deleteProfileConfirm, setDeleteProfileConfirm] = useState<string | null>(null)
  const [benchmarking, setBenchmarking] = useState(false)
  const [benchmarkResult, setBenchmarkResult] = useState<ProfileBenchmarkResult | null>(null)
  const [storageItems, setStorageItems] = useState<StorageItem[]>([])
  const [storageStatus, setStorageStatus] = useState('')
  const [channelStatus, setChannelStatus] = useState<Record<string, string>>({})
  const [channelQueue, setChannelQueue] = useState<ChannelWorkItem[]>([])
  const [audioHealth, setAudioHealth] = useState<AudioHealth | null>(null)
  const [audioTesting, setAudioTesting] = useState(false)
  const [kokoroVoices, setKokoroVoices] = useState<string[]>([])
  const [agentDefinitions, setAgentDefinitions] = useState<AgentDefinition[]>([])

  useEffect(() => {
    void Promise.all([GetSettings(), GetProfiles(), ListAgentDefinitions().catch(() => [] as AgentDefinition[])]).then(([s, pf, definitions]) => {
      setSettings(s)
      setProfilesFile(pf)
      setAgentDefinitions(definitions)
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
    void refreshStorage()
    void refreshChannelBus()
    void refreshAudioHealth()
    void ListKokoroVoices().then(setKokoroVoices).catch(() => setKokoroVoices(['af_heart']))
  }, [])

  useEffect(() => {
    if (tab !== 'telegram') return
    const id = window.setInterval(() => { void refreshChannelBus() }, 3000)
    return () => window.clearInterval(id)
  }, [tab])

  useEffect(() => {
    const provider = profilesFile?.providers?.[selectedProvider]
    setProviderAPIKey('')
    setShowProviderAPIKey(false)
    if (!provider) {
      setProviderAPIKeyStatus(null)
      return
    }
    void GetProviderAPIKeyStatus(provider)
      .then(setProviderAPIKeyStatus)
      .catch(() => setProviderAPIKeyStatus(null))
  }, [selectedProvider, profilesFile?.providers?.[selectedProvider]?.api_key_env])

  useEffect(() => {
    if (tab !== 'audio') return
    void refreshAudioHealth()
    const id = window.setInterval(() => { void refreshAudioHealth() }, 4000)
    return () => window.clearInterval(id)
  }, [tab])

  const refreshStorage = async () => {
    const items = await ListStorageItems().catch(() => [] as StorageItem[])
    setStorageItems(items)
  }

  const refreshChannelBus = async () => {
    const [status, queue] = await Promise.all([
      GetChannelBusStatus().catch(() => ({} as Record<string, string>)),
      ListChannelWorkQueue().then(items => items.slice(0, 8)).catch(() => [] as ChannelWorkItem[]),
    ])
    setChannelStatus(status)
    setChannelQueue(queue)
  }

  const refreshAudioHealth = async () => {
    setAudioHealth(await GetAudioHealth().catch(() => null))
  }

  const testVoice = async () => {
    setAudioTesting(true)
    try {
      if (dirty) await save(false)
      const result = await SynthesizeSpeech('Voice system is ready.')
      const player = new Audio(result.data_uri)
      await player.play()
      setSaveStatus(`Voice test passed using ${result.engine}`)
    } catch (error) {
      setSaveStatus(`Voice test failed: ${String(error)}`)
    } finally {
      setAudioTesting(false)
      await refreshAudioHealth()
    }
  }

  const restartVoice = async () => {
    setAudioTesting(true)
    try {
      setAudioHealth(await RestartAudioWorker())
      setSaveStatus('Voice workers are restarting and warming in the background.')
    } finally {
      setAudioTesting(false)
    }
  }

  const clearStorage = async (item: StorageItem) => {
    if (!item.clearable) return
    const ok = confirm(`Clear ${item.label}?\n\n${item.path}\n\nThis cannot be undone.`)
    if (!ok) return
    await ClearStorageItem(item.id)
    await refreshStorage()
    setStorageStatus(`${item.label} cleared`)
    window.setTimeout(() => setStorageStatus(current => current === `${item.label} cleared` ? '' : current), 2500)
  }

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
      await refreshChannelBus()
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

  const updateEnvironment = (patch: Partial<Settings['environment']>) => {
    if (!settings) return
    updateSettings('environment', { ...settings.environment, ...patch })
  }

  const updateTelegram = (patch: Partial<Settings['telegram']>) => {
    if (!settings) return
    updateSettings('telegram', { ...settings.telegram, ...patch })
  }

  const updateAudio = (patch: Partial<Settings['audio']>) => {
    if (!settings) return
    updateSettings('audio', { ...settings.audio, ...patch })
  }

  const updateLab = (patch: Partial<Settings['context']['lab']>) => {
    if (!settings) return
    updateSettings('context', {
      ...settings.context,
      lab: { ...settings.context.lab, ...patch },
    })
  }

  const saveCurrentLabProfile = () => {
    if (!settings) return
    const lab = settings.context.lab
    const id = (lab.id || settings.context.active_lab_profile || lab.name || 'default').trim().replace(/[^a-zA-Z0-9_.-]+/g, '-').replace(/^-+|-+$/g, '') || 'default'
    const profile = {
      id,
      name: lab.name || id,
      workspace_dir: settings.context.workspace_dir || '',
      target: lab.target || '',
      hostname: lab.hostname || '',
      vpn_interface: lab.vpn_interface || '',
      latest_artifact: lab.latest_artifact || '',
      ops_profile: lab.ops_profile || 'pentesting',
      evidence_policy: lab.evidence_policy || defaultEvidencePolicy(lab.ops_profile),
      access_preference: lab.access_preference || 'auto',
      notes: lab.notes || '',
    }
    const profiles = [...(settings.context.lab_profiles ?? [])]
    const existing = profiles.findIndex(item => item.id === id)
    if (existing >= 0) profiles[existing] = profile
    else profiles.unshift(profile)
    updateSettings('context', {
      ...settings.context,
      active_lab_profile: id,
      lab: { ...lab, id, name: profile.name },
      lab_profiles: profiles,
    })
  }

  const selectLabProfile = (id: string) => {
    if (!settings) return
    const profile = (settings.context.lab_profiles ?? []).find(item => item.id === id)
    if (!profile) return
    updateSettings('context', {
      ...settings.context,
      active_lab_profile: profile.id,
      workspace_dir: profile.workspace_dir || settings.context.workspace_dir,
      lab: {
        id: profile.id,
        name: profile.name || profile.id,
        target: profile.target || '',
        hostname: profile.hostname || '',
        vpn_interface: profile.vpn_interface || '',
        latest_artifact: profile.latest_artifact || '',
        ops_profile: profile.ops_profile || 'pentesting',
        evidence_policy: profile.evidence_policy || defaultEvidencePolicy(profile.ops_profile),
        access_preference: profile.access_preference || 'auto',
        notes: profile.notes || '',
      },
    })
  }

  const newLabProfile = () => {
    if (!settings) return
    const id = `lab-${Date.now()}`
    const profile = {
      id,
      name: 'New lab',
      workspace_dir: settings.context.workspace_dir || '',
      target: '',
      hostname: '',
      vpn_interface: settings.context.lab.vpn_interface || '',
      latest_artifact: '',
      ops_profile: 'pentesting',
      evidence_policy: 'research_assisted',
      access_preference: 'auto',
      notes: '',
    }
    updateSettings('context', {
      ...settings.context,
      active_lab_profile: id,
      lab: {
        id,
        name: profile.name,
        target: '',
        hostname: '',
        vpn_interface: profile.vpn_interface,
        latest_artifact: '',
        ops_profile: 'pentesting',
        evidence_policy: 'research_assisted',
        access_preference: 'auto',
        notes: '',
      },
      lab_profiles: [profile, ...(settings.context.lab_profiles ?? [])],
    })
  }

  const deleteLabProfile = (id: string) => {
    if (!settings) return
    const profiles = (settings.context.lab_profiles ?? []).filter(item => item.id !== id)
    updateSettings('context', {
      ...settings.context,
      lab_profiles: profiles,
      active_lab_profile: settings.context.active_lab_profile === id ? (profiles[0]?.id || '') : settings.context.active_lab_profile,
    })
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
      const current = prev.profiles[name]
      const nextProfile = { ...current, [field]: val }
      if (field === 'spec_type' && !String(val ?? '').trim()) {
        nextProfile.spec_draft_model = ''
        nextProfile.spec_draft_n_max = 0
      }
      return {
        ...prev,
        profiles: {
          ...prev.profiles,
          [name]: nextProfile,
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
      ctx_tokens: FALLBACK_CONTEXT_TOKENS,
      thinking: false,
      preserve_thinking: false,
      mmproj: '',
      thinking_general: { temperature: 0.6, top_p: 0.95, top_k: 40, min_p: 0, presence_penalty: 0, repeat_penalty: 1.05, max_tokens: 8192, seed: -1 },
      thinking_coding: { temperature: 0.6, top_p: 0.95, top_k: 40, min_p: 0, presence_penalty: 0, repeat_penalty: 1.05, max_tokens: 8192, seed: -1 },
      nothinking: { temperature: 0.7, top_p: 0.95, top_k: 40, min_p: 0, presence_penalty: 0, repeat_penalty: 1.05, max_tokens: 4096, seed: -1 },
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
  const localProfileNames = profileNames.filter(name => !isOneTaskCloudProfile(name, profilesFile))
  const toolsetNames = Object.keys(settings.tools.toolsets ?? {}).sort()
  const agentModeNames = agentDefinitions.length > 0
    ? agentDefinitions.map(definition => definition.name)
    : ['Auto', 'Manual', 'Bug Bounty Hunter', 'Builder', 'Fixer', 'Reviewer', 'Researcher', 'Planner']
  const agentPresetNames = Array.from(new Set([
    ...agentDefinitions.filter(definition => definition.name !== 'Manual').map(definition => definition.name),
    ...Object.keys(settings.agents.presets ?? {}),
  ])).filter(name => Boolean(settings.agents.presets?.[name]))
  const activeToolsetName = settings.tools.active_toolset || 'balanced'
  const activeToolsetTools = settings.tools.toolsets?.[activeToolsetName] ?? []
  const enabledToolNames = effectiveToolNames(settings.tools.enabled_tools, activeToolsetTools)
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
    const ms = await ListModelMetadataForProvider(provider).catch(() => [] as ModelMetadata[])
    setModels(ms)
  }

  const saveProviderAPIKey = async () => {
    if (!provider || !providerAPIKey.trim()) return
    setProviderAPIKeySaving(true)
    try {
      await SetProviderAPIKey(selectedProvider, providerAPIKey.trim())
      setProviderAPIKey('')
      setProviderAPIKeyStatus(await GetProviderAPIKeyStatus(provider))
      setSaveStatus(`API key saved for ${selectedProvider}`)
    } catch (error) {
      setSaveStatus(`API key save failed: ${String(error)}`)
    } finally {
      setProviderAPIKeySaving(false)
    }
  }

  const clearProviderAPIKey = async () => {
    if (!provider || !confirm(`Clear the stored API key for ${selectedProvider}?`)) return
    setProviderAPIKeySaving(true)
    try {
      await ClearProviderAPIKey(selectedProvider)
      setProviderAPIKey('')
      setProviderAPIKeyStatus(await GetProviderAPIKeyStatus(provider))
      setSaveStatus(`Stored API key cleared for ${selectedProvider}`)
    } catch (error) {
      setSaveStatus(`API key clear failed: ${String(error)}`)
    } finally {
      setProviderAPIKeySaving(false)
    }
  }

  const applyLocalModelTemplate = async (candidate: Profile): Promise<Profile> => {
    const result = await RecommendModelProfileTemplate(candidate).catch(() => null)
    if (!result?.matched) {
      return { ...candidate, ctx_tokens: candidate.ctx_tokens || FALLBACK_CONTEXT_TOKENS }
    }
    const configured = {
      ...result.profile,
      name: candidate.name,
      provider: candidate.provider,
      model_id: candidate.model_id,
    }
    const templateLabel = [result.template_id, result.chat_template].filter(Boolean).join(' · ')
    setSaveStatus(`Applied local model template: ${templateLabel}`)
    return configured
  }

  const selectProviderModel = async (model: ModelMetadata) => {
    if (!profilesFile || !selectedProvider) return
    if (profile && profile.provider === selectedProvider) {
      const nextProfile = isCloudProvider(selectedProvider, profilesFile)
        ? withCloudModelDefaults(profile, model)
        : await applyLocalModelTemplate({ ...profile, model_id: model.id })
      setProfilesFile({
        ...profilesFile,
        profiles: { ...profilesFile.profiles, [selectedProfile]: nextProfile },
      })
      setSelectedModelMetadata(model)
      markDirty()
      setTab('profiles')
      return
    }
    const modelSlug = model.id.split('/').pop()?.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '').toLowerCase() || 'model'
    const base = `${selectedProvider}-${modelSlug}`
    let name = base
    let suffix = 2
    while (profilesFile.profiles[name]) name = `${base}-${suffix++}`
    const params: GenerationParams = { temperature: 0.7, top_p: 0.95, top_k: 0, min_p: 0, presence_penalty: 0, repeat_penalty: 1.05, max_tokens: 8192, seed: -1 }
    const next: Profile = {
      name,
      provider: selectedProvider,
      model_id: model.id,
      ctx_tokens: isCloudProvider(selectedProvider, profilesFile) ? FALLBACK_CONTEXT_TOKENS : 0,
      thinking: false,
      preserve_thinking: false,
      mmproj: '',
      thinking_general: { ...params },
      thinking_coding: { ...params },
      nothinking: { ...params },
      spec_type: '',
      spec_draft_n_max: 0,
      spec_draft_model: '',
    }
    const configured = isCloudProvider(selectedProvider, profilesFile)
      ? withCloudModelDefaults(next, model)
      : await applyLocalModelTemplate(next)
    setProfilesFile({ ...profilesFile, profiles: { ...profilesFile.profiles, [name]: configured } })
    setSelectedProfile(name)
    setSelectedModelMetadata(model)
    setTab('profiles')
    markDirty()
  }

  const fetchProfileModels = async () => {
    if (!profile || !profilesFile) return
    const prov = profilesFile.providers[profile.provider]
    if (!prov) return
    setProfileModels([])
    const ms = await ListModelMetadataForProvider(prov).catch(() => [] as ModelMetadata[])
    setProfileModels(ms)
    setSelectedModelMetadata(ms.find(model => model.id === profile.model_id) ?? null)
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

  const handleApplyModelTemplate = async () => {
    if (!profile || !selectedProfile) return
    const result = await RecommendModelProfileTemplate({ ...profile, name: selectedProfile }).catch(error => {
      setSaveStatus(`Model template failed: ${String(error)}`)
      return null
    })
    if (!result) return
    if (!result.matched) {
      setSaveStatus('No code-owned template matched this model; existing settings were kept')
      return
    }
    replaceProfile(selectedProfile, { ...result.profile, name: selectedProfile })
    setSaveStatus(`Applied ${result.template_id}${result.chat_template ? ` · ${result.chat_template}` : ''}`)
  }

  const applyBenchmarkRecommendation = () => {
    if (!benchmarkResult || !selectedProfile) return
    replaceProfile(selectedProfile, benchmarkResult.recommended_profile)
    setBenchmarkResult(null)
    setSaveStatus('Benchmark recommendations applied')
  }

  return (
    <div className="overlay" onClick={e => { if (e.target === e.currentTarget) void close() }}>
      <div className="settings-modal control-center">
        <div className="settings-header control-center-header">
          <div>
            <span className="settings-kicker">Mauler Control Center</span>
            <span className="settings-title">Configure the agent workbench</span>
          </div>
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

        {settings && profilesFile && (
          <div className="control-center-summary">
            <div>
              <span>Profile</span>
              <strong>{settings.active_profile || 'none'}</strong>
            </div>
            <div>
              <span>Toolset</span>
              <strong>{settings.tools?.active_toolset || 'default'}</strong>
            </div>
            <div>
              <span>Shell</span>
              <strong>{[settings.tools?.shell_backend, settings.tools?.shell_mode].filter(Boolean).join(' / ') || 'auto'}</strong>
            </div>
            <div>
              <span>Providers</span>
              <strong>{Object.keys(profilesFile.providers ?? {}).length}</strong>
            </div>
          </div>
        )}

        <div className="settings-body">
          <div className="settings-tabs control-center-nav">
            {settingsTabs.map(item => (
              <button
                key={item.id}
                className={`tab-btn ${tab === item.id ? 'active' : ''}`}
                onClick={() => setTab(item.id)}
              >
                <span>{item.label}</span>
                <small>{item.description}</small>
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
                    {localProfileNames.map(n => <option key={n} value={n}>{n}</option>)}
                  </select>
                  <small>Local profiles are defaults. OpenRouter profiles are selected for one task beside the Chat composer.</small>
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
                        setSelectedModelMetadata(null)
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
                            key={m.id}
                            className="model-item"
                            onClick={() => void selectProviderModel(m)}
                          >
                            {modelCatalogueLabel(m, isCloudProvider(selectedProvider, profilesFile))}
                          </button>
                        ))}
                      </div>
                    )}

                    <Field label="Backend">
                      <select value={provider.backend} onChange={e => updateProviderField(selectedProvider, 'backend', e.target.value)}>
                        {['llamacpp', 'lmstudio', 'openai-compatible'].map(b => <option key={b}>{b}</option>)}
                      </select>
                    </Field>
                    <Field label="Base URL">
                      <input value={provider.base_url} onChange={e => updateProviderField(selectedProvider, 'base_url', e.target.value)} />
                    </Field>
                    <Field label="API key env">
                      <input value={provider.api_key_env ?? ''} onChange={e => updateProviderField(selectedProvider, 'api_key_env', e.target.value)} />
                    </Field>
                    {provider.backend === 'openai-compatible' && (
                      <Field label="API key">
                        <div className="provider-secret-editor">
                          <div className="provider-secret-input">
                            <input
                              type={showProviderAPIKey ? 'text' : 'password'}
                              value={providerAPIKey}
                              placeholder={providerAPIKeyStatus?.effective_configured ? 'A key is configured — enter a replacement' : 'Paste API key'}
                              autoComplete="new-password"
                              aria-label={`${selectedProvider} API key`}
                              onChange={e => setProviderAPIKey(e.target.value)}
                            />
                            <button onClick={() => setShowProviderAPIKey(value => !value)}>{showProviderAPIKey ? 'Hide' : 'Show'}</button>
                          </div>
                          <div className="provider-secret-actions">
                            <button className="primary" disabled={providerAPIKeySaving || !providerAPIKey.trim()} onClick={() => void saveProviderAPIKey()}>
                              {providerAPIKeySaving ? 'Saving...' : 'Save API key'}
                            </button>
                            <button disabled={providerAPIKeySaving || !providerAPIKeyStatus?.stored_configured} onClick={() => void clearProviderAPIKey()}>Clear stored key</button>
                            <span className={providerAPIKeyStatus?.effective_configured ? 'provider-secret-ready' : 'provider-secret-missing'}>
                              {providerAPIKeyStatus?.environment_configured
                                ? `Configured through ${provider.api_key_env || 'environment'}`
                                : providerAPIKeyStatus?.stored_configured
                                  ? 'Stored locally'
                                  : 'No key configured'}
                            </span>
                          </div>
                          <small>Environment variables take precedence. Stored keys stay outside profiles.toml and are never returned to the interface.</small>
                        </div>
                      </Field>
                    )}
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
                        disabled={saving || settings.active_profile === selectedProfile || isOneTaskCloudProfile(selectedProfile, profilesFile)}
                      >
                        {isOneTaskCloudProfile(selectedProfile, profilesFile)
                          ? 'Available as cloud boost'
                          : settings.active_profile === selectedProfile ? 'Active profile' : 'Use as local default'}
                      </button>
                      <button onClick={newProfile} title="Create a blank profile">New</button>
                      <button onClick={duplicateProfile} title="Clone this profile">Duplicate</button>
                      <button onClick={() => void handleBenchmarkProfile()} disabled={benchmarking} title="Probe the selected provider and recommend model settings">
                        {benchmarking ? 'Benchmarking...' : 'Benchmark LLM'}
                      </button>
                      <button onClick={() => void handleApplyModelTemplate()} disabled={benchmarking} title="Apply code-owned defaults for a recognised local or Hugging Face model">
                        Apply model template
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
                            key={m.id}
                            className={`model-item ${m.id === profile.model_id ? 'active' : ''}`}
                            onClick={() => {
                              const nextProfile = isCloudProvider(profile.provider, profilesFile)
                                ? withCloudModelDefaults(profile, m)
                                : { ...profile, model_id: m.id }
                              setProfilesFile({
                                ...profilesFile,
                                profiles: { ...profilesFile.profiles, [selectedProfile]: nextProfile },
                              })
                              setSelectedModelMetadata(m)
                              setProfileModels([])
                              markDirty()
                            }}
                          >
                            {modelCatalogueLabel(m, isCloudProvider(profile.provider, profilesFile))}
                          </button>
                        ))}
                      </div>
                    )}
                    <Field label="Context tokens">
                      <input type="number" value={profile.ctx_tokens} onChange={e => updateProfileField(selectedProfile, 'ctx_tokens', parseInt(e.target.value, 10) || 0)} />
                      {isCloudProvider(profile.provider, profilesFile) && (
                        <small>
                          Working budget used by Mauler before compaction. Standard is 128K; the cloud provider still enforces the model's hard maximum
                          {selectedModelMetadata?.id === profile.model_id && selectedModelMetadata.context_length
                            ? ` (${compactTokenCount(selectedModelMetadata.context_length)} for this route).`
                            : '.'}
                        </small>
                      )}
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
                        onChange={e => updateProfileField(selectedProfile, 'spec_draft_model', e.target.value)}
                        placeholder="C:\\path\\to\\draft-model.gguf"
                      />
                      <span className="field-hint">
                        Passed to llama.cpp as -md / draft_model_path only when Spec type is enabled.
                      </span>
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
                    {agentModeNames.map(mode => (
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
                    max={2000}
                    value={settings.agents.max_tool_calls}
                    onChange={e => updateSettings('agents', { ...settings.agents, max_tool_calls: parseInt(e.target.value, 10) || 200 })}
                  />
                </Field>
                <Field label="Max run seconds">
                  <input
                    type="number"
                    min={0}
                    max={86400}
                    value={settings.agents.max_run_seconds ?? 1800}
                    onChange={e => updateSettings('agents', { ...settings.agents, max_run_seconds: parseInt(e.target.value, 10) || 0 })}
                  />
                  <span className="field-hint">Wall-clock task budget. Use 0 for unlimited.</span>
                </Field>
                <Field label="Escalation profile">
                  <input
                    value={settings.agents.escalation_profile ?? ''}
                    placeholder="empty disables escalation"
                    onChange={e => updateSettings('agents', { ...settings.agents, escalation_profile: e.target.value })}
                  />
                  <span className="field-hint">Optional profile for one hard recovery step after truncation or malformed tool-call dead ends.</span>
                </Field>
                <Field label="Disable thinking after N tool calls">
                  <input
                    type="number"
                    min={1}
                    max={20}
                    value={settings.agents.no_think_after_tool_calls ?? 2}
                    onChange={e => updateSettings('agents', { ...settings.agents, no_think_after_tool_calls: parseInt(e.target.value, 10) || 2 })}
                  />
                  <span className="field-hint">Qwen3 fix: disables &lt;think&gt; once this many tool calls have been made (default 2)</span>
                </Field>

                <h3>Self-review loop</h3>
                <Field label="Review loop">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.review_loop?.enabled ?? true}
                      onChange={e => updateSettings('agents', {
                        ...settings.agents,
                        review_loop: { ...settings.agents.review_loop, enabled: e.target.checked }
                      })}
                    />
                    Gate automated run completion with verification and review
                  </label>
                </Field>
                <Field label="Autonomous only">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.review_loop?.only_autonomous ?? true}
                      onChange={e => updateSettings('agents', {
                        ...settings.agents,
                        review_loop: { ...settings.agents.review_loop, only_autonomous: e.target.checked }
                      })}
                    />
                    Skip manual/read-only runs
                  </label>
                </Field>
                <Field label="Max review cycles">
                  <input
                    type="number"
                    min={0}
                    max={5}
                    value={settings.agents.review_loop?.max_review_cycles ?? 2}
                    onChange={e => updateSettings('agents', {
                      ...settings.agents,
                      review_loop: { ...settings.agents.review_loop, max_review_cycles: parseInt(e.target.value, 10) || 0 }
                    })}
                  />
                  <span className="field-hint">Hard cap on fix-review retries before the run stops honestly.</span>
                </Field>
                <Field label="Verify gate">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.review_loop?.verify_gate ?? true}
                      onChange={e => updateSettings('agents', {
                        ...settings.agents,
                        review_loop: { ...settings.agents.review_loop, verify_gate: e.target.checked }
                      })}
                    />
                    Run detected build/test/lint commands before done
                  </label>
                </Field>
                <Field label="Verify timeout">
                  <input
                    type="number"
                    min={1}
                    max={3600}
                    value={settings.agents.review_loop?.verify_timeout_sec ?? 120}
                    onChange={e => updateSettings('agents', {
                      ...settings.agents,
                      review_loop: { ...settings.agents.review_loop, verify_timeout_sec: parseInt(e.target.value, 10) || 120 }
                    })}
                  />
                  <span className="field-hint">Seconds per verification command.</span>
                </Field>
                <Field label="Verify commands">
                  <input
                    value={(settings.agents.review_loop?.verify_commands ?? []).join('; ')}
                    placeholder="auto-detect"
                    onChange={e => updateSettings('agents', {
                      ...settings.agents,
                      review_loop: {
                        ...settings.agents.review_loop,
                        verify_commands: e.target.value.split(';').map(v => v.trim()).filter(Boolean)
                      }
                    })}
                  />
                  <span className="field-hint">Optional semicolon-separated override. Empty means auto-detect.</span>
                </Field>
                <Field label="Completion rails">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.review_loop?.completion_rails ?? true}
                      onChange={e => updateSettings('agents', {
                        ...settings.agents,
                        review_loop: { ...settings.agents.review_loop, completion_rails: e.target.checked }
                      })}
                    />
                    Check requested deliverables and feature coverage
                  </label>
                </Field>
                <Field label="Completion blocking">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.review_loop?.completion_blocking ?? false}
                      onChange={e => updateSettings('agents', {
                        ...settings.agents,
                        review_loop: { ...settings.agents.review_loop, completion_blocking: e.target.checked }
                      })}
                    />
                    Let completion rails send the run back for fixes
                  </label>
                </Field>
                <Field label="Reviewer pass">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.agents.review_loop?.reviewer_pass ?? true}
                      onChange={e => updateSettings('agents', {
                        ...settings.agents,
                        review_loop: { ...settings.agents.review_loop, reviewer_pass: e.target.checked }
                      })}
                    />
                    Run a fresh same-profile read-only review before done
                  </label>
                </Field>
                <Field label="Reviewer tool cap">
                  <input
                    type="number"
                    min={1}
                    max={40}
                    value={settings.agents.review_loop?.reviewer_max_tools ?? 15}
                    onChange={e => updateSettings('agents', {
                      ...settings.agents,
                      review_loop: { ...settings.agents.review_loop, reviewer_max_tools: parseInt(e.target.value, 10) || 15 }
                    })}
                  />
                </Field>

                <div className="preset-editor-list">
                  {agentPresetNames.map(name => {
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
                          <details className="advanced-drawer">
                            <summary>
                              <span>Advanced per-tool overrides</span>
                              <small>{summarizePresetToolPermissions(preset.tool_permissions)}</small>
                            </summary>
                            <div className="tool-grid compact-tool-grid">
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
                          </details>
                        </Field>
                      </details>
                    )
                  })}
                </div>
              </div>
            )}

            {tab === 'environment' && (
              <div className="settings-section">
                <h3>Environment Routing</h3>
                <Field label="Main OS">
                  <select value={settings.environment.main_os || 'windows'} onChange={e => updateEnvironment({ main_os: e.target.value })}>
                    <option value="auto">auto</option>
                    <option value="windows">windows</option>
                    <option value="linux">linux</option>
                    <option value="macos">macos</option>
                  </select>
                </Field>
                <Field label="AI shell backend">
                  <select value={settings.environment.ai_shell_backend || settings.tools.shell_backend || 'wsl'} onChange={e => updateEnvironment({ ai_shell_backend: e.target.value })}>
                    <option value="auto">auto</option>
                    <option value="wsl">wsl</option>
                    <option value="powershell">powershell</option>
                    <option value="cmd">cmd</option>
                    <option value="bash">bash</option>
                  </select>
                  <span className="field-hint">What the agent should treat as its normal work shell. For HTB on Windows this is usually WSL/Kali.</span>
                </Field>
                <Field label="AI WSL distro">
                  <select value={settings.environment.ai_shell_distro || settings.tools.shell_distro || ''} onChange={e => updateEnvironment({ ai_shell_distro: e.target.value })}>
                    <option value="">default distro</option>
                    {wslDistros.map(name => <option key={name} value={name}>{name}</option>)}
                  </select>
                </Field>
                <Field label="AI WSL user">
                  <input value={settings.environment.ai_shell_user || settings.tools.shell_user || ''} onChange={e => updateEnvironment({ ai_shell_user: e.target.value })} placeholder="root" />
                </Field>
                <Field label="Target work backend">
                  <select value={settings.environment.target_work_backend || 'ai_shell'} onChange={e => updateEnvironment({ target_work_backend: e.target.value })}>
                    <option value="ai_shell">AI shell</option>
                    <option value="wsl">WSL</option>
                    <option value="windows_powershell">Windows PowerShell</option>
                    <option value="bash">bash</option>
                  </select>
                  <span className="field-hint">Where scans, curl/ffuf, exploit checks, and target filesystem tooling should run.</span>
                </Field>
                <Field label="Listener backend">
                  <select value={settings.environment.listener_backend || 'windows_powershell'} onChange={e => updateEnvironment({ listener_backend: e.target.value })}>
                    <option value="windows_powershell">Windows PowerShell</option>
                    <option value="ai_shell">AI shell</option>
                    <option value="wsl">WSL</option>
                    <option value="bash">bash</option>
                    <option value="manual">manual / user-owned</option>
                  </select>
                </Field>
                <Field label="Listener command">
                  <input value={settings.environment.listener_command || ''} onChange={e => updateEnvironment({ listener_command: e.target.value })} placeholder="ncat.exe -lvp {port}" />
                  <span className="field-hint">Use placeholders like {'{port}'}. The prompt tells the agent to start this with terminal_send, not blocking shell.</span>
                </Field>
                <Field label="LHOST source">
                  <select value={settings.environment.lhost_source || 'selected_vpn_interface'} onChange={e => updateEnvironment({ lhost_source: e.target.value })}>
                    <option value="selected_vpn_interface">selected VPN/interface</option>
                    <option value="manual">manual LHOST</option>
                    <option value="auto">auto</option>
                  </select>
                </Field>
                <Field label="Manual LHOST">
                  <input value={settings.environment.manual_lhost || ''} onChange={e => updateEnvironment({ manual_lhost: e.target.value })} placeholder="10.10.x.x" />
                </Field>
                <Field label="Terminal tools">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.environment.prefer_terminal_tools}
                      onChange={e => updateEnvironment({ prefer_terminal_tools: e.target.checked })} />
                    Prefer terminal_send / terminal_read for live shells and listeners
                  </label>
                </Field>
                <Field label="Correction policy">
                  <select value={settings.environment.user_correction_policy || 'latest_user_wins'} onChange={e => updateEnvironment({ user_correction_policy: e.target.value })}>
                    <option value="latest_user_wins">latest user instruction wins</option>
                    <option value="normal">normal</option>
                  </select>
                  <span className="field-hint">Use latest_user_wins when you want interrupts like "use webshell" to override stale plans immediately.</span>
                </Field>
                <Field label="Reverse shell guidance">
                  <textarea rows={5} value={settings.environment.reverse_shell_guidance || ''} onChange={e => updateEnvironment({ reverse_shell_guidance: e.target.value })} />
                </Field>

                <h3>Current Project / Box</h3>
                <Field label="Saved lab profile">
                  <div className="settings-inline-actions">
                    <select value={settings.context.active_lab_profile || settings.context.lab.id || ''} onChange={e => selectLabProfile(e.target.value)}>
                      {(settings.context.lab_profiles ?? []).map(profile => <option key={profile.id} value={profile.id}>{profile.name || profile.id}</option>)}
                    </select>
                    <button type="button" onClick={newLabProfile}>New</button>
                    <button type="button" onClick={saveCurrentLabProfile}>Save current</button>
                  </div>
                </Field>
                <Field label="Lab name">
                  <input value={settings.context.lab.name || ''} onChange={e => updateLab({ name: e.target.value })} placeholder="Box name / HTB box / client test" />
                </Field>
                <Field label="Profile id">
                  <input value={settings.context.lab.id || ''} onChange={e => updateLab({ id: e.target.value })} placeholder="connected" />
                </Field>
                <Field label="Target IP / URL">
                  <input value={settings.context.lab.target || ''} onChange={e => updateLab({ target: e.target.value })} placeholder="10.129.x.x or https://host" />
                </Field>
                <Field label="Hostname">
                  <input value={settings.context.lab.hostname || ''} onChange={e => updateLab({ hostname: e.target.value })} placeholder="boxname.htb" />
                </Field>
                <Field label="VPN/interface">
                  <input value={settings.context.lab.vpn_interface || ''} onChange={e => updateLab({ vpn_interface: e.target.value })} placeholder="Ethernet 3 / tun0 / 10.10.x.x" />
                </Field>
                <Field label="Ops profile">
                  <select value={settings.context.lab.ops_profile || 'pentesting'} onChange={e => updateLab({ ops_profile: e.target.value })}>
                    <option value="pentesting">Pentesting</option>
                    <option value="htb">HTB / CTF</option>
                  </select>
                </Field>
                <Field label="Evidence policy">
                  <select value={settings.context.lab.evidence_policy || defaultEvidencePolicy(settings.context.lab.ops_profile)} onChange={e => updateLab({ evidence_policy: e.target.value })}>
                    <option value="discovery_first">Discovery-first</option>
                    <option value="research_assisted">Research-assisted</option>
                    <option value="reference_allowed">Reference allowed</option>
                    <option value="fastest_path">Fastest path</option>
                  </select>
                  <span className="field-hint">Discovery-first avoids exact-box writeups for discovery while still allowing CVEs, vendor docs, and PoCs after live evidence.</span>
                </Field>
                <Field label="Preferred access">
                  <select value={settings.context.lab.access_preference || 'auto'} onChange={e => updateLab({ access_preference: e.target.value })}>
                    <option value="auto">auto</option>
                    <option value="webshell">webshell</option>
                    <option value="reverse_shell">reverse shell</option>
                    <option value="bind_shell">bind shell</option>
                    <option value="none">none / report only</option>
                  </select>
                </Field>
                <Field label="Latest artifact">
                  <input value={settings.context.lab.latest_artifact || ''} onChange={e => updateLab({ latest_artifact: e.target.value })} placeholder="scans/nmap_full.xml, report.md, screenshot.png" />
                </Field>
                <Field label="Lab notes">
                  <textarea rows={5} value={settings.context.lab.notes || ''} onChange={e => updateLab({ notes: e.target.value })} placeholder="Per-box constraints, known bad paths, client scope, or 'do not retry RCE path X'." />
                </Field>
                <Field label="Saved profiles">
                  <div className="safe-rule-list">
                    {(settings.context.lab_profiles ?? []).length === 0 ? (
                      <div className="safe-rule-empty">No saved lab profiles yet</div>
                    ) : settings.context.lab_profiles.map(profile => (
                      <div className="safe-rule" key={profile.id}>
                        <div className="safe-rule-main">
                          <strong>{profile.name || profile.id}</strong>
                          <span>{[profile.target, profile.hostname, profile.access_preference, profile.workspace_dir].filter(Boolean).join(' | ') || profile.id}</span>
                        </div>
                        <button type="button" onClick={() => selectLabProfile(profile.id)}>Use</button>
                        <button type="button" onClick={() => deleteLabProfile(profile.id)}>Delete</button>
                      </div>
                    ))}
                  </div>
                </Field>
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
                    Pause before write / edit
                  </label>
                </Field>
                <Field label="Confirm exec">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.tools.confirm_exec}
                      onChange={e => updateSettings('tools', { ...settings.tools, confirm_exec: e.target.checked })} />
                    Pause before shell execution
                  </label>
                </Field>
                <Field label="Tool-call grammar">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={!!settings.tools.tool_grammar_constraint}
                      onChange={e => updateSettings('tools', { ...settings.tools, tool_grammar_constraint: e.target.checked })} />
                    Force valid tool-call JSON (GBNF) for local models
                  </label>
                  <span className="field-hint">Constrains non-native (gemma / repair-text) models to emit a valid tool call. Experimental — verify against your llama.cpp build before relying on it.</span>
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
                  <span className="field-hint">{enabledToolNames.length} tools active for model prompts after this coarse gate.</span>
                </Field>
                <Field label="Toolset contents">
                  <div className="toolset-summary-card">
                    <div className="toolset-summary-head">
                      <div>
                        <strong>{activeToolsetName}</strong>
                        <span>{activeToolsetTools.length} allowed by toolset · {enabledToolNames.length} enabled</span>
                      </div>
                      <div className="toolset-counts">
                        <span>{countRisk(enabledToolNames, 'low')} low</span>
                        <span>{countRisk(enabledToolNames, 'medium')} med</span>
                        <span>{countRisk(enabledToolNames, 'high')} high</span>
                      </div>
                    </div>
                    <ToolPillGrid tools={enabledToolNames} />
                    <details className="advanced-drawer">
                      <summary>
                        <span>All toolsets</span>
                        <small>{toolsetNames.length} groups</small>
                      </summary>
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
                    </details>
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
                  <input type="number" min={0} max={100000} value={settings.tools.max_tool_result_chars ?? 12000}
                    onChange={e => updateSettings('tools', { ...settings.tools, max_tool_result_chars: parseInt(e.target.value, 10) || 0 })} />
                  <span className="field-hint">Offloads larger outputs to disk and keeps a preview in context. 0 = no offload. Default: 12000</span>
                </Field>
                <Field label="Tool result preview chars">
                  <input type="number" min={200} max={20000} value={settings.tools.tool_result_preview_chars ?? 2000}
                    onChange={e => updateSettings('tools', { ...settings.tools, tool_result_preview_chars: parseInt(e.target.value, 10) || 2000 })} />
                  <span className="field-hint">Head/tail preview size for offloaded tool results. The full output remains available through read_tool_result.</span>
                </Field>
                <Field label="Tool access">
                  <details className="advanced-drawer">
                    <summary>
                      <span>Advanced per-tool switches</span>
                      <small>{knownTools(settings.tools.enabled_tools).length} known tools</small>
                    </summary>
                    <div className="tool-grid compact-tool-grid">
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
                  </details>
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

            {tab === 'telegram' && (
              <div className="settings-section">
                <h3>Telegram Bot</h3>
                <Field label="Enable bot">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={!!settings.telegram?.enabled}
                      onChange={e => updateTelegram({ enabled: e.target.checked })}
                    />
                    Accept Telegram updates through the channel bus
                  </label>
                </Field>
                <Field label="Bot token">
                  <input
                    type="password"
                    value={settings.telegram?.token ?? ''}
                    onChange={e => updateTelegram({ token: e.target.value })}
                    placeholder="123456:ABC..."
                  />
                  <span className="field-hint">Stored in settings.toml for now. Leave blank to keep the bot disabled at runtime.</span>
                </Field>
                <Field label="Bot username">
                  <input
                    value={settings.telegram?.bot_username ?? ''}
                    onChange={e => updateTelegram({ bot_username: e.target.value })}
                    placeholder="TheMaulerBot"
                  />
                </Field>
                <Field label="Require mention">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.telegram?.require_mention ?? true}
                      onChange={e => updateTelegram({ require_mention: e.target.checked })}
                    />
                    Ignore group messages unless they mention the bot or use a command
                  </label>
                </Field>
                <Field label="Allowed chats/users">
                  <textarea
                    rows={3}
                    value={(settings.telegram?.allow_from ?? []).join('\n')}
                    onChange={e => updateTelegram({ allow_from: e.target.value.split(/\r?\n|,/).map(v => v.trim()).filter(Boolean) })}
                    placeholder="Telegram user IDs, chat IDs, or @usernames; one per line"
                  />
                  <span className="field-hint">Empty means no allow-list filter. Use IDs for reliability.</span>
                </Field>

                <h3>Remote Routing Defaults</h3>
                <Field label="Default project">
                  <input
                    value={settings.telegram?.default_project ?? ''}
                    onChange={e => updateTelegram({ default_project: e.target.value })}
                    placeholder={settings.context.workspace_dir || 'Use current workspace'}
                  />
                </Field>
                <Field label="Default profile">
                  <select
                    value={settings.telegram?.default_profile || ''}
                    onChange={e => updateTelegram({ default_profile: e.target.value })}
                  >
                    <option value="">Use active profile</option>
                    {profileNames.map(profileName => <option key={profileName} value={profileName}>{profileName}</option>)}
                  </select>
                </Field>
                <Field label="Default mode">
                  <select
                    value={settings.telegram?.default_mode || 'Auto'}
                    onChange={e => updateTelegram({ default_mode: e.target.value })}
                  >
                    {agentModeNames.map(mode => (
                      <option key={mode} value={mode}>{mode}</option>
                    ))}
                  </select>
                </Field>
                <Field label="Default toolset">
                  <select
                    value={settings.telegram?.default_toolset || settings.tools.active_toolset || 'unrestricted'}
                    onChange={e => updateTelegram({ default_toolset: e.target.value })}
                  >
                    {toolsetNames.map(name => <option key={name} value={name}>{name}</option>)}
                  </select>
                </Field>
                <Field label="Progress replies">
                  <label className="checkbox-label">
                    <input
                      type="checkbox"
                      checked={settings.telegram?.send_progress ?? true}
                      onChange={e => updateTelegram({ send_progress: e.target.checked })}
                    />
                    Send concise run progress updates back to Telegram
                  </label>
                </Field>
                <Field label="Progress interval">
                  <input
                    type="number"
                    min={3}
                    max={300}
                    value={settings.telegram?.progress_interval_s ?? 10}
                    onChange={e => updateTelegram({ progress_interval_s: parseInt(e.target.value, 10) || 10 })}
                  />
                  <span className="field-hint">Seconds between progress updates while a remote-launched task is running.</span>
                </Field>

                <h3>Voice</h3>
                <Field label="Voice replies">
                  <select
                    value={settings.telegram?.voice_replies || 'on_voice'}
                    onChange={e => updateTelegram({ voice_replies: e.target.value })}
                  >
                    <option value="off">off</option>
                    <option value="on_voice">reply with voice only to voice messages</option>
                    <option value="always">always send voice replies</option>
                  </select>
                </Field>
                <Field label="Transcription mode">
                  <select
                    value={settings.telegram?.transcription_mode || 'local'}
                    onChange={e => updateTelegram({ transcription_mode: e.target.value })}
                  >
                    <option value="disabled">disabled</option>
                    <option value="local">local helper</option>
                    <option value="openai_compatible">OpenAI-compatible endpoint</option>
                  </select>
                </Field>
                <Field label="Transcription URL">
                  <input
                    value={settings.telegram?.transcription_url ?? ''}
                    onChange={e => updateTelegram({ transcription_url: e.target.value })}
                    placeholder="optional local speech endpoint"
                  />
                </Field>

                <h3>Channel Bus</h3>
                <div className="telegram-status-grid">
                  {Object.entries(channelStatus).length === 0 ? (
                    <div className="telegram-status-card">
                      <span>Status</span>
                      <strong>Not loaded</strong>
                    </div>
                  ) : Object.entries(channelStatus).map(([key, value]) => (
                    <div className="telegram-status-card" key={key}>
                      <span>{key.replaceAll('_', ' ')}</span>
                      <strong>{value || '-'}</strong>
                    </div>
                  ))}
                </div>
                <Field label="Queued work">
                  <div className="safe-rule-list">
                    <div className="settings-inline-actions">
                      <button type="button" onClick={() => void refreshChannelBus()}>Refresh queue</button>
                    </div>
                    {channelQueue.length === 0 ? (
                      <div className="safe-rule-empty">No remote work requests queued</div>
                    ) : channelQueue.map(item => (
                      <div className="safe-rule" key={item.id}>
                        <div className="safe-rule-main">
                          <strong>{item.envelope.source} · {item.route.lane} · {item.status}</strong>
                          <span>{item.envelope.text}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </Field>
              </div>
            )}

            {tab === 'audio' && (
              <div className="settings-section">
                <h3>Desktop Voice</h3>
                <div className={`audio-health-card audio-health-${audioHealth?.overall || 'checking'}`}>
                  <div className="audio-health-head">
                    <div><span>Voice health</span><strong>{audioHealth?.overall || 'checking'}</strong></div>
                    <div className="audio-health-actions">
                      <button onClick={() => void refreshAudioHealth()} disabled={audioTesting}>Refresh</button>
                      <button onClick={() => void testVoice()} disabled={audioTesting || !(settings.audio?.enabled ?? true)}>{audioTesting ? 'Working...' : 'Play test voice'}</button>
                      <button onClick={() => void restartVoice()} disabled={audioTesting}>Restart voice</button>
                    </div>
                  </div>
                  <div className="audio-health-grid">
                    <div><span>TTS configured</span><strong>{audioHealth?.configured_tts || settings.audio?.tts_engine || 'auto'}</strong></div>
                    <div><span>Actually used</span><strong>{audioHealth?.actual_tts || 'not tested'}</strong></div>
                    <div><span>Kokoro worker</span><strong>{audioHealth?.worker_state || 'checking'}{audioHealth?.worker_pid ? ` · PID ${audioHealth.worker_pid}` : ''}</strong></div>
                    <div><span>Speech recognition</span><strong>{audioHealth?.stt_ready ? `${audioHealth.stt_engine} ${audioHealth.stt_worker_state || 'ready'}` : `${audioHealth?.stt_engine || 'whisper'} missing`}</strong></div>
                    <div><span>Whisper worker</span><strong>{audioHealth?.stt_worker_state || 'checking'}{audioHealth?.stt_worker_pid ? ` · PID ${audioHealth.stt_worker_pid}` : ''}</strong></div>
                    <div><span>Whisper model</span><strong>{audioHealth?.stt_model || 'tiny.en'}</strong></div>
                    <div><span>Last transcription</span><strong>{audioHealth?.stt_last_duration_ms ? `${(audioHealth.stt_last_duration_ms / 1000).toFixed(2)}s for ${(audioHealth.stt_last_audio_ms / 1000).toFixed(2)}s audio` : 'none yet'}</strong></div>
                    <div><span>Worker window</span><strong>{audioHealth?.worker_hidden ? 'hidden background process' : 'unknown'}</strong></div>
                    <div><span>Last success</span><strong>{audioHealth?.last_success ? new Date(audioHealth.last_success).toLocaleString() : 'none yet'}</strong></div>
                  </div>
                  {audioHealth?.last_error && <div className="audio-health-error">{audioHealth.last_error}</div>}
                  {audioHealth?.stt_last_error && <div className="audio-health-error">Whisper: {audioHealth.stt_last_error}</div>}
                </div>
                <Field label="Enable audio">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.audio?.enabled ?? true} onChange={e => updateAudio({ enabled: e.target.checked })} />
                    Show the Talk control and allow local speech input
                  </label>
                </Field>
                <Field label="Conversation mode">
                  <select value={settings.audio?.mode || 'push_to_talk'} onChange={e => updateAudio({ mode: e.target.value })}>
                    <option value="push_to_talk">Push to talk</option>
                    <option value="open_mic">Open mic (VAD milestone)</option>
                  </select>
                  <span className="field-hint">Push to talk is active now. Open mic is reserved for the Silero VAD pass.</span>
                </Field>
                <Field label="Speech recognition">
                  <select value={settings.audio?.stt_engine || 'whisper'} onChange={e => updateAudio({ stt_engine: e.target.value })}>
                    <option value="whisper">Local Whisper</option>
                    <option value="parakeet">Parakeet (when installed)</option>
                  </select>
                </Field>
                <Field label="Speech engine">
                  <select value={settings.audio?.tts_engine || 'auto'} onChange={e => updateAudio({ tts_engine: e.target.value })}>
                    <option value="auto">Kokoro, then Piper fallback</option>
                    <option value="kokoro">Kokoro</option>
                    <option value="piper">Piper</option>
                  </select>
                </Field>
                <Field label="Kokoro voice">
                  <select value={settings.audio?.voice || 'af_heart'} onChange={e => updateAudio({ voice: e.target.value })}>
                    {(kokoroVoices.length ? kokoroVoices : ['af_heart']).map(voice => <option key={voice} value={voice}>{voiceLabel(voice)}</option>)}
                  </select>
                  <span className="field-hint">All installed Kokoro voices. af_heart remains the default.</span>
                </Field>
                <Field label="Voice speed">
                  <input type="number" min={0.8} max={1.4} step={0.05} value={settings.audio?.speed || 1} onChange={e => updateAudio({ speed: parseFloat(e.target.value) || 1 })} />
                </Field>
                <Field label="Streaming speech">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.audio?.speak_replies ?? true} onChange={e => updateAudio({ speak_replies: e.target.checked })} />
                    Speak completed clauses while the model is still generating
                  </label>
                </Field>
                <Field label="Barge in">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.audio?.barge_in ?? true} onChange={e => updateAudio({ barge_in: e.target.checked })} />
                    Starting a new recording stops speech and interrupts the current run
                  </label>
                </Field>
                <Field label="Clause size">
                  <input type="number" min={16} max={240} value={settings.audio?.clause_min_chars || 36} onChange={e => updateAudio({ clause_min_chars: parseInt(e.target.value, 10) || 36 })} />
                  <span className="field-hint">Lower starts speaking sooner; higher produces smoother prosody.</span>
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
                  <span className="field-hint">Source discovery/read allowance. Large files remain readable, while the always-on packet is separately compiled and capped.</span>
                </Field>
                <Field label="Always-on instruction packet">
                  <input value="Validated manifest · task-aware · bounded" readOnly />
                  <span className="field-hint">External research receives no project documents. Workspace work gets up to three heading-aware excerpts with hashes and line ranges. Preview Core, Relevant, or one-task Expanded packets from More → Context.</span>
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

                <h3>Memory</h3>
                <Field label="Project memory">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.memory.enabled}
                      onChange={e => updateSettings('memory', { ...settings.memory, enabled: e.target.checked })} />
                    Enable durable project memory
                  </label>
                </Field>
                <Field label="Auto-inject memory">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.memory.auto_inject} disabled={!settings.memory.enabled}
                      onChange={e => updateSettings('memory', { ...settings.memory, auto_inject: e.target.checked })} />
                    Inject relevant memory at the start of a run (and re-inject as it drifts)
                  </label>
                </Field>
                <Field label="Auto-distill lessons">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={!settings.memory.disable_auto_distill} disabled={!settings.memory.enabled}
                      onChange={e => updateSettings('memory', { ...settings.memory, disable_auto_distill: !e.target.checked })} />
                    Save high-importance lessons from failures into memory when a run finishes
                  </label>
                  <span className="field-hint">Entries are tagged "auto" so you can review or prune them on the Brain page.</span>
                </Field>
              </div>
            )}

            {tab === 'storage' && (
              <div className="settings-section">
                <h3>Storage</h3>
                <div className="storage-toolbar">
                  <div>
                    <strong>App data paths</strong>
                    <span>Review log, benchmark, memory, session, and cache footprints.</span>
                  </div>
                  <button onClick={() => void refreshStorage()}>Refresh</button>
                  {storageStatus && <span className="save-status">{storageStatus}</span>}
                </div>
                <div className="storage-list">
                  {storageItems.length === 0 ? (
                    <div className="storage-empty">No storage information available.</div>
                  ) : storageItems.map(item => (
                    <div className="storage-item" key={item.id}>
                      <div className="storage-main">
                        <div className="storage-title">
                          <strong>{item.label}</strong>
                          <span>{item.kind}</span>
                        </div>
                        <div className="storage-description">{item.description}</div>
                        <code>{item.path}</code>
                      </div>
                      <div className="storage-side">
                        <strong>{item.size}</strong>
                        <button
                          className={item.clearable ? 'danger' : ''}
                          disabled={!item.clearable}
                          onClick={() => void clearStorage(item)}
                        >
                          {item.clearable ? 'Clear' : 'Managed elsewhere'}
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
                <div className="storage-note">
                  Memory, skills, profiles, and settings are shown for visibility but are not cleared here. Use Memory/Brain or the relevant editor for those.
                </div>
              </div>
            )}

            {tab === 'ui' && (
              <div className="settings-section">
                <h3>Startup</h3>
                <Field label="Terminal on launch">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.ui.terminal_default_open ?? false}
                      onChange={e => updateSettings('ui', { ...settings.ui, terminal_default_open: e.target.checked })} />
                    Show the terminal panel when TheMauler opens
                  </label>
                  <span className="field-hint">Applies on the next launch. The Terminal button controls only the current session.</span>
                </Field>
                <h3>Appearance</h3>
                <div className="theme-preset-grid">
                  {themeOptions.map(option => (
                    <button
                      key={option.value}
                      type="button"
                      className={`theme-preset-card ${themeValue(settings.ui.theme) === option.value ? 'active' : ''}`}
                      onClick={() => {
                        const nextUI = { ...settings.ui, theme: option.value, accent_color: option.accent, primary_color: option.primary }
                        updateSettings('ui', nextUI)
                        previewTheme(option.value)
                        previewAccent(option.accent)
                        previewPrimary(option.primary)
                      }}
                    >
                      <span className="theme-preset-swatch" style={{ background: option.accent }} />
                      <strong>{option.label}</strong>
                      <small>{option.note}</small>
                    </button>
                  ))}
                </div>
                <Field label="Theme">
                  <select value={themeValue(settings.ui.theme)}
                    onChange={e => {
                      const theme = e.target.value
                      updateSettings('ui', { ...settings.ui, theme })
                      previewTheme(theme)
                    }}>
                    {themeOptions.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
                  </select>
                </Field>
                <Field label="Accent color">
                  <div className="accent-picker">
                    {accentSwatches.map(c => (
                      <button
                        key={c}
                        className={`accent-swatch ${(settings.ui.accent_color ?? '#4ade80') === c ? 'active' : ''}`}
                        style={{ background: c }}
                        onClick={() => {
                          updateSettings('ui', { ...settings.ui, accent_color: c })
                          previewAccent(c)
                        }}
                        title={c}
                      />
                    ))}
                    <input
                      type="color"
                      className="accent-custom"
                      value={settings.ui.accent_color ?? '#4ade80'}
                      onChange={e => {
                        updateSettings('ui', { ...settings.ui, accent_color: e.target.value })
                        previewAccent(e.target.value)
                      }}
                      title="Custom color"
                    />
                  </div>
                </Field>
                <Field label="Button color">
                  <div className="accent-picker">
                    {accentSwatches.map(c => (
                      <button
                        key={c}
                        className={`accent-swatch ${(settings.ui.primary_color ?? '#16a34a') === c ? 'active' : ''}`}
                        style={{ background: c }}
                        onClick={() => {
                          updateSettings('ui', { ...settings.ui, primary_color: c })
                          previewPrimary(c)
                        }}
                        title={c}
                      />
                    ))}
                    <input
                      type="color"
                      className="accent-custom"
                      value={settings.ui.primary_color ?? '#16a34a'}
                      onChange={e => {
                        updateSettings('ui', { ...settings.ui, primary_color: e.target.value })
                        previewPrimary(e.target.value)
                      }}
                      title="Custom button color"
                    />
                  </div>
                </Field>
                <h3>Layout</h3>
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

                <h3>Video</h3>
                <Field label="Video enabled">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.image.video_enabled}
                      onChange={e => updateSettings('image', { ...settings.image, video_enabled: e.target.checked })} />
                    Analyze pasted/dropped video (samples keyframes via ffmpeg)
                  </label>
                </Field>
                <Field label="Max keyframes">
                  <input type="number" min={1} max={32} step={1} value={settings.image.video_max_frames}
                    onChange={e => updateSettings('image', { ...settings.image, video_max_frames: parseInt(e.target.value) })} />
                  <span className="field-hint">frames sampled evenly across the clip</span>
                </Field>
                <Field label="Keyframe width">
                  <input type="number" min={128} max={2048} step={64} value={settings.image.video_frame_width}
                    onChange={e => updateSettings('image', { ...settings.image, video_frame_width: parseInt(e.target.value) })} />
                  <span className="field-hint">px</span>
                </Field>
                <Field label="Transcribe audio">
                  <label className="checkbox-label">
                    <input type="checkbox" checked={settings.image.video_transcribe}
                      onChange={e => updateSettings('image', { ...settings.image, video_transcribe: e.target.checked })} />
                    Include an audio transcript (requires whisper in PATH)
                  </label>
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
    'read',
    'write',
    'edit',
    'shell',
    'terminal_send',
    'terminal_read',
    'run_script',
    'glob',
    'grep',
    'web_search',
    'fetch_url',
    'browser',
    'session_search',
    'file_changes',
    'sqlite',
    'todo_write',
    'engagement',
    'skill',
    'http_probe',
    'start_listener',
    'evidence_bundle',
    'memory',
    'progress',
    'read_tool_result',
    'set_reasoning_effort',
    'task',
    ...Object.keys(enabled ?? {}),
  ])).sort()
}

function effectiveToolNames(enabled: Record<string, boolean> | undefined, toolsetTools: string[]): string[] {
  const allowed = new Set(toolsetTools)
  return toolsetTools
    .filter(name => (enabled?.[name] ?? true) && allowed.has(name))
    .sort()
}

function countRisk(tools: string[], risk: ToolRisk): number {
  return tools.filter(name => (toolRisk[name] ?? 'medium') === risk).length
}

function summarizePresetToolPermissions(permissions: Record<string, boolean> | undefined): string {
  const entries = Object.entries(permissions ?? {})
  if (entries.length === 0) return 'inherits toolset'
  const enabled = entries.filter(([, value]) => value).length
  const disabled = entries.length - enabled
  return `${enabled} enabled overrides, ${disabled} disabled`
}

function ToolPillGrid({ tools }: { tools: string[] }) {
  if (tools.length === 0) {
    return <div className="safe-rule-empty">No tools enabled in the active toolset</div>
  }
  return (
    <div className="tool-pill-grid">
      {tools.map(name => (
        <span className={`tool-pill tool-pill-${toolRisk[name] ?? 'medium'}`} key={name}>{name}</span>
      ))}
    </div>
  )
}

function defaultEvidencePolicy(opsProfile: string): string {
  return /htb|ctf/i.test(opsProfile || '') ? 'discovery_first' : 'research_assisted'
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
      {numField('Repeat penalty', 'repeat_penalty')}
      {numField('Max tokens', 'max_tokens', 256)}
    </div>
  )
}
