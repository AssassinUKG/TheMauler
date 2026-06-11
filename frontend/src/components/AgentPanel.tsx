import { useEffect, useState } from 'react'
import {
  ApplySafetyPreset,
  ClearMemoryEntries,
  ClearSessionRecall,
  ClearTaskRuns,
  ClearTodos,
  DeleteMemoryEntry,
  DeleteSkill,
  GetProjectInstructionsSummary,
  GetSettings,
  GetUserProfile,
  KillLocalInferenceServers,
  ListMemory,
  RunDoctor,
  ListSkills,
  ListTaskRuns,
  ListTodos,
  ReindexSessionRecall,
  RestartWSL,
  SaveMemoryEntry,
  SaveSkill,
  SaveUserProfile,
  SearchSessionRecall,
  SelectProjectInstructionDirectory,
  SelectProjectInstructionFile,
  SetAgentModeOverride,
  StopAgent,
  UpdateSettings,
  UseProjectInstructionFile,
  type DoctorResult,
  type MemoryEntry,
  type SessionSearchResult,
  type Settings,
  type Skill,
  type SkillSuggestion,
  type TaskRun,
  type TodoItem,
} from '../wailsjs/go'
import type { AgentActivity } from '../App'
import './AgentPanel.css'

interface Props {
  autonomous: boolean
  autoAgents: boolean
  activeProfile: string
  streaming: boolean
  onAutonomousChange: (enabled: boolean) => void
  onAutoAgentsChange: (enabled: boolean) => void
  onOpenSettings: () => void
  onClearChat: () => void
  onSettingsChanged: () => void
  activity: AgentActivity[]
  agentMode: string
  doctorRunRequest: number
  taskRunVersion: number
  skillSuggestion: SkillSuggestion | null
  onDismissSkillSuggestion: () => void
}

const toolLabels: Record<string, string> = {
  read_file: 'Read files',
  read_many: 'Read many files',
  file_outline: 'File outlines',
  read_chunks: 'Read chunks',
  read_pdf: 'Read PDFs',
  write_file: 'Write files',
  edit_file: 'Edit files',
  shell: 'Shell / Bash',
  glob: 'Glob',
  grep: 'Grep',
  session_search: 'Session search',
  sqlite_schema: 'SQLite schema',
  sqlite_query: 'SQLite query',
  web_search: 'Web search',
  fetch_url: 'Fetch URL',
  browser_open: 'Browser open',
  browser_snapshot: 'Browser snapshot',
  browser_click: 'Browser click',
  browser_type: 'Browser type',
  browser_extract: 'Browser extract',
  browser_screenshot: 'Browser screenshot',
  browser_close: 'Browser close',
  browser_agent: 'Browser agent',
  todo_create: 'Create plan',
  todo_update: 'Update plan',
  todo_done: 'Complete plan item',
  todo_blocked: 'Block plan item',
  todo_list: 'List plan',
  todo_clear: 'Clear plan',
  skills_list: 'List skills',
  skill_view: 'View skill',
  subagent_research: 'Subagent research',
  subagent_review: 'Subagent review',
  subagent_testfix: 'Subagent test/fix',
  subagent_summarize: 'Subagent summarize',
}

type ToolRisk = 'low' | 'medium' | 'high'

const toolRiskLabels: Record<ToolRisk, string> = {
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
  todo_create: 'low',
  todo_update: 'low',
  todo_done: 'low',
  todo_blocked: 'low',
  todo_list: 'low',
  todo_clear: 'low',
  skills_list: 'low',
  skill_view: 'low',
  subagent_research: 'medium',
  subagent_review: 'low',
  subagent_testfix: 'high',
  subagent_summarize: 'low',
}

type AgentTab = 'agent' | 'plan' | 'activity' | 'tools' | 'browser' | 'memory' | 'skills' | 'logs'

export function AgentPanel({
  autonomous,
  autoAgents,
  activeProfile,
  streaming,
  onAutonomousChange,
  onAutoAgentsChange,
  onOpenSettings,
  onClearChat,
  onSettingsChanged,
  activity,
  agentMode,
  doctorRunRequest,
  taskRunVersion,
  skillSuggestion,
  onDismissSkillSuggestion,
}: Props) {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [saving, setSaving] = useState(false)
  const [tab, setTab] = useState<AgentTab>('agent')
  const [memory, setMemory] = useState<MemoryEntry[]>([])
  const [runs, setRuns] = useState<TaskRun[]>([])
  const [todos, setTodos] = useState<TodoItem[]>([])
  const [logFilter, setLogFilter] = useState('')
  const [logStatusFilter, setLogStatusFilter] = useState<'all' | 'problem' | 'error' | 'stopped' | 'done'>('all')
  const [memoryDraft, setMemoryDraft] = useState({ title: '', content: '', tags: '' })
  const [memoryFilter, setMemoryFilter] = useState('')
  const [editingMemory, setEditingMemory] = useState<MemoryEntry | null>(null)
  const [recallQuery, setRecallQuery] = useState('')
  const [recallResults, setRecallResults] = useState<SessionSearchResult[]>([])
  const [recallStatus, setRecallStatus] = useState('')
  const [skills, setSkills] = useState<Skill[]>([])
  const [skillFilter, setSkillFilter] = useState('')
  const [viewingSkill, setViewingSkill] = useState<Skill | null>(null)
  const [editingSkill, setEditingSkill] = useState<Skill | null>(null)
  const [skillDraft, setSkillDraft] = useState<Skill | null>(null)
  const [projectSkillPath, setProjectSkillPath] = useState('')
  const [projectInstructionSummary, setProjectInstructionSummary] = useState('')
  const [userProfile, setUserProfile] = useState('')
  const [editingUserProfile, setEditingUserProfile] = useState(false)
  const [userProfileDraft, setUserProfileDraft] = useState('')
  const [doctorResult, setDoctorResult] = useState<DoctorResult | null>(null)
  const [doctorRunning, setDoctorRunning] = useState(false)
  const [maintenanceRunning, setMaintenanceRunning] = useState('')
  const [panelStatus, setPanelStatus] = useState('')

  const load = async () => {
    const [s, mem, taskRuns, todoItems, skillItems, profileText, instructionSummary] = await Promise.all([
      GetSettings().catch(() => null),
      ListMemory().catch(() => [] as MemoryEntry[]),
      ListTaskRuns().catch(() => [] as TaskRun[]),
      ListTodos().catch(() => [] as TodoItem[]),
      ListSkills().catch(() => [] as Skill[]),
      GetUserProfile().catch(() => ''),
      GetProjectInstructionsSummary().catch(() => ''),
    ])
    setSettings(s)
    setProjectSkillPath('')
    setProjectInstructionSummary(instructionSummary)
    setMemory(mem)
    setRuns(taskRuns)
    setTodos(todoItems)
    setSkills(skillItems)
    setUserProfile(profileText)
  }

  useEffect(() => {
    void load()
  }, [taskRunVersion])

  useEffect(() => {
    if (doctorRunRequest <= 0) return
    void runDoctor()
  }, [doctorRunRequest])

  const updateSettings = async (next: Settings) => {
    setSettings(next)
    setSaving(true)
    try {
      await UpdateSettings(next)
      onSettingsChanged()
    } finally {
      setSaving(false)
    }
  }

  const updateTools = async (tools: Settings['tools']) => {
    if (!settings) return
    await updateSettings({ ...settings, tools })
  }

  const showPanelStatus = (message: string) => {
    setPanelStatus(message)
    window.setTimeout(() => setPanelStatus(current => current === message ? '' : current), 2200)
  }

  const refreshProjectInstructionSummary = async () => {
    setProjectInstructionSummary(await GetProjectInstructionsSummary().catch(() => ''))
  }

  const activateProjectSkillPath = async (path: string) => {
    setSaving(true)
    try {
      const next = await UseProjectInstructionFile(path.trim())
      setSettings(next)
      const skillItems = await ListSkills().catch(() => [] as Skill[])
      setSkills(skillItems)
      setProjectSkillPath('')
      await refreshProjectInstructionSummary()
      onSettingsChanged()
      showPanelStatus(path.trim()
        ? 'Master skill registered for lazy use'
        : 'Master skill cleared')
    } catch (e) {
      showPanelStatus(`Project workflow failed: ${String(e)}`)
    } finally {
      setSaving(false)
    }
  }

  const pickProjectSkillPath = async () => {
    const fallback = projectSkillPath || settings?.context?.workspace_dir || ''
    const selected = await SelectProjectInstructionFile(fallback).catch(() => '')
    if (!selected) return
    setProjectSkillPath(selected)
    await activateProjectSkillPath(selected)
  }

  const pickProjectSkillFolder = async () => {
    const fallback = projectSkillPath || settings?.context?.workspace_dir || ''
    const selected = await SelectProjectInstructionDirectory(fallback).catch(() => '')
    if (!selected) return
    setProjectSkillPath(selected)
    await activateProjectSkillPath(selected)
  }

  const runDoctor = async () => {
    setDoctorRunning(true)
    setPanelStatus('')
    try {
      setDoctorResult(await RunDoctor())
      showPanelStatus('Doctor complete')
    } catch (e) {
      showPanelStatus(`Doctor failed: ${String(e)}`)
    } finally {
      setDoctorRunning(false)
    }
  }

  const killInferenceServers = async () => {
    if (!confirm('Stop local InferenceBridge and llama-server processes?')) return
    setMaintenanceRunning('inference')
    setPanelStatus('')
    try {
      const result = await KillLocalInferenceServers()
      showPanelStatus(result.summary)
    } catch (e) {
      showPanelStatus(`Inference cleanup failed: ${String(e)}`)
    } finally {
      setMaintenanceRunning('')
    }
  }

  const restartWSL = async () => {
    if (!confirm('Restart WSL now? This stops all running WSL distributions.')) return
    setMaintenanceRunning('wsl')
    setPanelStatus('')
    try {
      const result = await RestartWSL()
      showPanelStatus(result.summary)
    } catch (e) {
      showPanelStatus(`WSL restart failed: ${String(e)}`)
    } finally {
      setMaintenanceRunning('')
    }
  }

  const refreshPanelData = async () => {
    await load()
    showPanelStatus('Refreshed')
  }

  const exportLogs = async () => {
    await navigator.clipboard.writeText(JSON.stringify(runs, null, 2))
    showPanelStatus('Logs copied')
  }

  const clearLogs = async () => {
    if (!confirm('Clear all task logs?')) return
    await ClearTaskRuns()
    setRuns([])
    onSettingsChanged()
    showPanelStatus('Logs cleared')
  }

  const setToolEnabled = (name: string, enabled: boolean) => {
    if (!settings) return
    const nextEnabled = {
      ...(settings.tools.enabled_tools ?? {}),
      [name]: enabled,
    }
    if (name === 'shell') {
      nextEnabled.bash = enabled
    }
    const currentToolset = settings.tools.active_toolset || 'balanced'
    const toolsetTools = settings.tools.toolsets?.[currentToolset] ?? []
    const needsOnlineToolset = enabled && onlineTools.has(name) && !toolsetTools.includes(name)
    const nextToolset = needsOnlineToolset ? preferredOnlineToolset(name) : settings.tools.active_toolset
    const nextTools = {
      ...settings.tools,
      active_toolset: nextToolset,
      enabled_tools: nextEnabled,
    }
    void updateSettings({
      ...settings,
      agents: needsOnlineToolset ? { ...settings.agents, offline_only: false } : settings.agents,
      tools: nextTools,
    })
  }

  const setModeOverride = async (mode: string) => {
    if (!settings) return
    await SetAgentModeOverride(mode)
    setSettings({ ...settings, agents: { ...settings.agents, mode_override: mode } })
    onSettingsChanged()
  }

  const saveMemoryDraft = async () => {
    if (!memoryDraft.title.trim() && !memoryDraft.content.trim()) return
    const saved = await SaveMemoryEntry({
      id: '',
      scope: '',
      title: memoryDraft.title.trim() || 'Project note',
      content: memoryDraft.content.trim(),
      tags: memoryDraft.tags.split(',').map(t => t.trim()).filter(Boolean),
      kind: 'note',
      importance: 3,
      pinned: false,
      created_at: '',
      updated_at: '',
      last_used_at: '',
    })
    setMemory(items => [saved, ...items.filter(item => item.id !== saved.id)])
    setMemoryDraft({ title: '', content: '', tags: '' })
  }

  const saveEditedMemory = async () => {
    if (!editingMemory) return
    const saved = await SaveMemoryEntry({
      ...editingMemory,
      title: editingMemory.title.trim() || 'Project note',
      content: editingMemory.content.trim(),
      tags: (editingMemory.tags ?? []).map(t => t.trim()).filter(Boolean),
    })
    setMemory(items => [saved, ...items.filter(item => item.id !== saved.id)])
    setEditingMemory(null)
  }

  const searchRecall = async () => {
    const q = recallQuery.trim()
    if (!q) return
    setRecallStatus('searching')
    try {
      const results = await SearchSessionRecall(q, 10)
      setRecallResults(results)
      setRecallStatus(results.length === 0 ? 'no matches' : `${results.length} match${results.length === 1 ? '' : 'es'}`)
    } catch (e) {
      setRecallStatus(`error: ${String(e)}`)
    }
  }

  const reindexRecall = async () => {
    setRecallStatus('reindexing')
    try {
      const count = await ReindexSessionRecall()
      setRecallStatus(`indexed ${count} session${count === 1 ? '' : 's'}`)
    } catch (e) {
      setRecallStatus(`error: ${String(e)}`)
    }
  }

  const enabledTools = settings?.tools.enabled_tools ?? {}
  const names = Object.keys(toolLabels)
  const toolsetNames = Object.keys(settings?.tools.toolsets ?? {}).sort()
  const modeOverride = settings?.agents.mode_override || 'Auto'
  const filteredMemory = memory.filter(item => {
    const filter = memoryFilter.trim().toLowerCase()
    if (!filter) return true
    return `${item.title} ${item.content} ${(item.tags ?? []).join(' ')}`.toLowerCase().includes(filter)
  })
  // When override is "Manual" and autoAgents is on, the effective displayed mode
  // should reflect override takes precedence over auto-routing.
  const effectiveMode = !autoAgents ? 'Manual' : (modeOverride !== 'Auto' ? modeOverride : agentMode)
  const autonomyPreset = settings?.agents.offline_only
    ? 'Offline'
    : autonomous && !settings?.tools.confirm_exec && !settings?.tools.confirm_writes
      ? 'Unrestricted'
      : 'Balanced'

  const applyAutonomyPreset = async (preset: 'unrestricted' | 'balanced' | 'offline') => {
    await ApplySafetyPreset(preset)
    await load()
    onAutonomousChange(preset === 'unrestricted')
    onSettingsChanged()
  }

  return (
    <aside className="agent-panel">
      <div className="agent-panel-header">
        <span>Agent</span>
        {saving && <span className="agent-saving">saving</span>}
      </div>
      <div className="agent-tabs">
        {(['agent', 'plan', 'activity', 'tools', 'browser', 'memory', 'skills', 'logs'] as AgentTab[]).map(name => (
          <button key={name} className={tab === name ? 'active' : ''} onClick={() => setTab(name)}>
            {name.charAt(0).toUpperCase() + name.slice(1)}
          </button>
        ))}
      </div>

      <div className="agent-tab-body">
        {tab === 'agent' && (
          <div className="agent-card compact">
            <div className="agent-section-head">Status</div>
            <div className="agent-row">
              <span className="agent-label">Profile</span>
              <span className="agent-value">{activeProfile || 'none'}</span>
            </div>
            <div className="agent-row">
              <span className="agent-label">Mode</span>
              <span className="agent-mode-pill">{effectiveMode}</span>
            </div>

            <div className="agent-section-head">Mode override</div>
            <div className="mode-pills" title={!autoAgents ? 'Enable Auto Agents to use mode override' : undefined}>
              {['Auto', 'Manual', 'Builder', 'Fixer', 'Reviewer', 'Researcher', 'Planner'].map(mode => (
                <button
                  key={mode}
                  className={`mode-pill${modeOverride === mode ? ' active' : ''}`}
                  onClick={() => void setModeOverride(mode)}
                  disabled={!autoAgents}
                >
                  {mode}
                </button>
              ))}
            </div>

            <div className="agent-section-head">Behaviour</div>
            <label className="agent-setting-row">
              <div>
                <div className="agent-setting-name">Auto Agents</div>
                <div className="agent-setting-desc">Route messages to specialised modes</div>
              </div>
              <span className="toggle-switch">
                <input type="checkbox" checked={autoAgents} onChange={e => onAutoAgentsChange(e.target.checked)} />
                <span className="toggle-track" />
              </span>
            </label>
            <label className="agent-setting-row">
              <div>
                <div className="agent-setting-name">Autonomous</div>
                <div className="agent-setting-desc">Let enabled tools run without confirmation prompts</div>
              </div>
              <span className="toggle-switch">
                <input type="checkbox" checked={autonomous} onChange={e => onAutonomousChange(e.target.checked)} />
                <span className="toggle-track" />
              </span>
            </label>
            <label className="agent-setting-row">
              <div>
                <div className="agent-setting-name">Tools</div>
                <div className="agent-setting-desc">Allow tool use in responses</div>
              </div>
              <span className="toggle-switch">
                <input
                  type="checkbox"
                  checked={settings?.tools.enabled ?? false}
                  onChange={e => settings && void updateTools({ ...settings.tools, enabled: e.target.checked })}
                />
                <span className="toggle-track" />
              </span>
            </label>

            <div className="agent-section-head">Access preset</div>
            <div className="agent-preset-group">
              <button
                className={`agent-preset-unrestricted${autonomyPreset === 'Unrestricted' ? ' active' : ''}`}
                onClick={() => void applyAutonomyPreset('unrestricted')}
              >
                <strong>Unrestricted</strong>
                <span>Full enabled tools, no prompts</span>
              </button>
              <button
                className={`agent-preset-balanced${autonomyPreset === 'Balanced' ? ' active' : ''}`}
                onClick={() => void applyAutonomyPreset('balanced')}
              >
                <strong>Balanced</strong>
                <span>Web on, prompts for writes/shell</span>
              </button>
              <button
                className={`agent-preset-offline${autonomyPreset === 'Offline' ? ' active' : ''}`}
                onClick={() => void applyAutonomyPreset('offline')}
              >
                <strong>Offline</strong>
                <span>Local tools only, prompts kept</span>
              </button>
            </div>

            <div className="agent-section-head">Toolset</div>
            <select
              value={settings?.tools.active_toolset || 'balanced'}
              onChange={e => settings && void updateTools({ ...settings.tools, active_toolset: e.target.value })}
            >
              {toolsetNames.map(name => <option key={name} value={name}>{name}</option>)}
            </select>
          </div>
        )}

        {tab === 'tools' && (
          <>
            <div className="agent-section-title">Tool Access</div>
            <div className="agent-card compact">
              <div className="agent-row"><span className="agent-label">Active toolset</span><span className="agent-value">{settings?.tools.active_toolset || 'balanced'}</span></div>
              <select
                value={settings?.tools.active_toolset || 'balanced'}
                onChange={e => settings && void updateTools({ ...settings.tools, active_toolset: e.target.value })}
              >
                {toolsetNames.map(name => <option key={name} value={name}>{name}</option>)}
              </select>
            </div>
            <div className="agent-risk-note">Risk labels are visibility only. Autonomous mode can still run enabled tools without confirmation.</div>
            <div className="agent-tool-list">
              {names.map(name => (
                <label key={name} className="agent-tool">
                  <span className="toggle-switch">
                    <input
                      type="checkbox"
                      checked={enabledTools[name] ?? true}
                      disabled={!settings?.tools.enabled}
                      onChange={e => setToolEnabled(name, e.target.checked)}
                    />
                    <span className="toggle-track" />
                  </span>
                  <span className="agent-tool-name">{toolLabels[name]}</span>
                  <span className={`agent-risk agent-risk-${toolRisk[name] ?? 'medium'}`}>{toolRiskLabels[toolRisk[name] ?? 'medium']}</span>
                </label>
              ))}
            </div>
          </>
        )}

        {tab === 'plan' && (
          <>
            <div className="agent-section-title">Plan</div>
            <div className="agent-card compact">
              <div className="agent-setting-desc">The active checklist is maintained by the todo tools while the agent works.</div>
              <div className="agent-control two">
                <button onClick={() => void load()}>Refresh</button>
                <button
                  onClick={async () => {
                    if (!confirm('Clear the active task plan?')) return
                    await ClearTodos()
                    setTodos([])
                  }}
                >
                  Clear plan
                </button>
              </div>
            </div>
            <div className="agent-activity-list">
              {todos.length === 0 ? (
                <div className="agent-activity-empty">No active plan</div>
              ) : todos.map(item => (
                <details key={item.id} className="agent-activity-item" open={item.status === 'in_progress' || item.status === 'blocked'}>
                  <summary>
                    <span className={todoStatusClass(item.status)}>{item.status}</span>
                    <span className="activity-name">{item.text}</span>
                    <span className="activity-duration">{item.id}</span>
                  </summary>
                  {item.detail && (
                    <div className="activity-section">
                      <div className="activity-label">Detail</div>
                      <pre>{item.detail}</pre>
                    </div>
                  )}
                  <div className="run-meta">
                    <span>updated {new Date(item.updated_at).toLocaleTimeString()}</span>
                  </div>
                </details>
              ))}
            </div>
          </>
        )}

        {tab === 'browser' && (
          <>
            <div className="agent-section-title">Web & Browser</div>
            <div className="agent-card compact">
              <div className="agent-row"><span className="agent-label">Engine</span><span className="agent-value">{settings?.tools.web_engine || 'auto'}</span></div>
              <div className="agent-row"><span className="agent-label">Search budget</span><span className="agent-value">{settings?.tools.max_searches ?? 4}</span></div>
              <div className="agent-row"><span className="agent-label">Fetch budget</span><span className="agent-value">{settings?.tools.max_fetches ?? 6}</span></div>
              <div className="agent-row"><span className="agent-label">Browser budget</span><span className="agent-value">{settings?.tools.max_browser_actions ?? 20}</span></div>
              <div className="agent-row">
                <span className="agent-label">Offline only</span>
                <span className={settings?.agents.offline_only ? 'agent-pending' : 'agent-ok'}>{settings?.agents.offline_only ? 'on' : 'off'}</span>
              </div>
            </div>
          </>
        )}

        {tab === 'memory' && (
          <>
            <div className="agent-section-title">Memory</div>

            {/* USER.md — persistent user profile */}
            <div className="agent-card compact">
              <div className="agent-setting-row">
                <div>
                  <div className="agent-setting-name">User profile (USER.md)</div>
                  <div className="agent-setting-desc">Injected into every conversation. Describe your coding style, expertise, and preferences so the agent adapts to you over time.</div>
                </div>
                <button className="agent-inline-button" onClick={() => {
                  setUserProfileDraft(userProfile)
                  setEditingUserProfile(v => !v)
                }}>{editingUserProfile ? 'Cancel' : (userProfile ? 'Edit' : 'Create')}</button>
              </div>
              {editingUserProfile && (
                <div style={{ marginTop: 8, display: 'flex', flexDirection: 'column', gap: 6 }}>
                  <textarea
                    style={{ fontFamily: 'var(--mono, monospace)', fontSize: 11, minHeight: 120, resize: 'vertical', background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 6, color: 'var(--text)', padding: '6px 8px' }}
                    placeholder={`# User Profile\n\n## Expertise\n- ...\n\n## Preferences\n- ...\n\n## Working style\n- ...`}
                    value={userProfileDraft}
                    onChange={e => setUserProfileDraft(e.target.value)}
                  />
                  <div style={{ display: 'flex', gap: 6 }}>
                    <button onClick={async () => {
                      await SaveUserProfile(userProfileDraft)
                      setUserProfile(userProfileDraft)
                      setEditingUserProfile(false)
                    }}>Save</button>
                    <button onClick={() => setEditingUserProfile(false)}>Cancel</button>
                  </div>
                </div>
              )}
              {!editingUserProfile && userProfile && (
                <pre style={{ fontSize: 11, fontFamily: 'var(--mono, monospace)', whiteSpace: 'pre-wrap', wordBreak: 'break-word', margin: '6px 0 0', color: 'var(--text-dim)', maxHeight: 80, overflow: 'hidden' }}>
                  {userProfile.slice(0, 200)}{userProfile.length > 200 ? '…' : ''}
                </pre>
              )}
            </div>

            <div className="agent-card compact">
              <label className="agent-setting-row">
                <div><div className="agent-setting-name">Project memory</div></div>
                <span className="toggle-switch">
                  <input
                    type="checkbox"
                    checked={settings?.memory.enabled ?? true}
                    onChange={e => settings && void updateSettings({ ...settings, memory: { ...settings.memory, enabled: e.target.checked } })}
                  />
                  <span className="toggle-track" />
                </span>
              </label>
              <label className="agent-setting-row">
                <div><div className="agent-setting-name">Auto inject relevant notes</div></div>
                <span className="toggle-switch">
                  <input
                    type="checkbox"
                    checked={settings?.memory.auto_inject ?? true}
                    onChange={e => settings && void updateSettings({ ...settings, memory: { ...settings.memory, auto_inject: e.target.checked } })}
                  />
                  <span className="toggle-track" />
                </span>
              </label>
              <input placeholder="Title" value={memoryDraft.title} onChange={e => setMemoryDraft(d => ({ ...d, title: e.target.value }))} />
              <textarea placeholder="Remember this for this workspace..." value={memoryDraft.content} onChange={e => setMemoryDraft(d => ({ ...d, content: e.target.value }))} />
              <input placeholder="tags, comma separated" value={memoryDraft.tags} onChange={e => setMemoryDraft(d => ({ ...d, tags: e.target.value }))} />
              <button onClick={() => void saveMemoryDraft()}>Add memory</button>
            </div>
            <div className="agent-memory-toolbar">
              <input
                placeholder="Filter memory..."
                value={memoryFilter}
                onChange={e => setMemoryFilter(e.target.value)}
              />
              <button
                onClick={async () => {
                  if (!confirm('Delete all curated project memory entries?')) return
                  await ClearMemoryEntries()
                  setMemory([])
                }}
              >
                Delete all
              </button>
            </div>
            <div className="agent-activity-list">
              {filteredMemory.length === 0 ? <div className="agent-activity-empty">No matching memory</div> : filteredMemory.map(item => (
                <details key={item.id} className="agent-activity-item">
                  <summary>
                    <span className={item.pinned ? 'agent-pending' : 'agent-ok'}>{item.kind || 'mem'}</span>
                    <span className="activity-name">{item.title || 'Project note'}</span>
                    <span className="activity-duration">i{item.importance || 3}</span>
                  </summary>
                  {editingMemory?.id === item.id ? (
                    <div className="agent-memory-editor">
                      <input
                        value={editingMemory.title}
                        onChange={e => setEditingMemory({ ...editingMemory, title: e.target.value })}
                      />
                      <select
                        value={editingMemory.kind || 'note'}
                        onChange={e => setEditingMemory({ ...editingMemory, kind: e.target.value })}
                      >
                        {['note', 'preference', 'constraint', 'fact', 'workflow', 'decision'].map(kind => <option key={kind}>{kind}</option>)}
                      </select>
                      <textarea
                        value={editingMemory.content}
                        onChange={e => setEditingMemory({ ...editingMemory, content: e.target.value })}
                      />
                      <input
                        value={(editingMemory.tags ?? []).join(', ')}
                        onChange={e => setEditingMemory({ ...editingMemory, tags: e.target.value.split(',').map(t => t.trim()).filter(Boolean) })}
                        placeholder="tags, comma separated"
                      />
                      <div className="agent-memory-row">
                        <label>
                          <span>Importance</span>
                          <input
                            type="number"
                            min={1}
                            max={5}
                            value={editingMemory.importance || 3}
                            onChange={e => setEditingMemory({ ...editingMemory, importance: parseInt(e.target.value, 10) || 3 })}
                          />
                        </label>
                        <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                          <span className="toggle-switch">
                            <input
                              type="checkbox"
                              checked={editingMemory.pinned}
                              onChange={e => setEditingMemory({ ...editingMemory, pinned: e.target.checked })}
                            />
                            <span className="toggle-track" />
                          </span>
                          <span>Pinned</span>
                        </label>
                      </div>
                      <div className="agent-control two">
                        <button onClick={() => void saveEditedMemory()}>Save</button>
                        <button onClick={() => setEditingMemory(null)}>Cancel</button>
                      </div>
                    </div>
                  ) : (
                    <>
                      <pre>{item.content}</pre>
                      {(item.tags ?? []).length > 0 && <div className="agent-memory-tags">{item.tags.map(tag => <span key={tag}>{tag}</span>)}</div>}
                      <div className="agent-memory-actions">
                        <button className="agent-inline-button" onClick={() => setEditingMemory(item)}>Edit</button>
                        <button className="agent-inline-button" onClick={async () => { await DeleteMemoryEntry(item.id); setMemory(items => items.filter(m => m.id !== item.id)) }}>
                          Delete
                        </button>
                      </div>
                    </>
                  )}
                </details>
              ))}
            </div>

            <div className="agent-section-title">Session Recall</div>
            <div className="agent-card compact">
              <div className="agent-setting-desc">Search saved and autosaved chats indexed in the local recall database.</div>
              <div className="agent-recall-search">
                <input
                  placeholder="Search previous sessions..."
                  value={recallQuery}
                  onChange={e => setRecallQuery(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter') void searchRecall()
                  }}
                />
                <button onClick={() => void searchRecall()}>Search</button>
              </div>
              <div className="agent-control three">
                <button onClick={() => void reindexRecall()}>Reindex</button>
                <button
                  onClick={async () => {
                    if (!confirm('Clear the local session recall index? Saved session files are not deleted.')) return
                    await ClearSessionRecall()
                    setRecallResults([])
                    setRecallStatus('recall index cleared')
                  }}
                >
                  Clear index
                </button>
                <button onClick={() => { setRecallQuery(''); setRecallResults([]); setRecallStatus('') }}>Reset</button>
              </div>
              {recallStatus && <div className="agent-muted">{recallStatus}</div>}
            </div>
            <div className="agent-activity-list">
              {recallResults.map(result => (
                <details key={`${result.session_id}-${result.message_id}`} className="agent-activity-item">
                  <summary>
                    <span className="agent-muted">{result.role}</span>
                    <span className="activity-name">{result.session_name}</span>
                    {result.updated_at && <span className="activity-duration">{new Date(result.updated_at).toLocaleDateString()}</span>}
                  </summary>
                  <div className="run-meta">
                    {result.tool_name && <span>tool {result.tool_name}</span>}
                    {result.rank && <span>rank {result.rank}</span>}
                  </div>
                  <pre>{trimActivity(result.content)}</pre>
                </details>
              ))}
            </div>
          </>
        )}

        {tab === 'logs' && (
          <>
            {/* Log settings */}
            <div className="agent-card compact">
              <div className="agent-section-head">Logging</div>
              <label className="agent-setting-row">
                <div>
                  <div className="agent-setting-name">Logging enabled</div>
                  <div className="agent-setting-desc">Save task runs to disk</div>
                </div>
                <span className="toggle-switch">
                  <input type="checkbox" checked={settings?.logging?.enabled ?? true} onChange={e => settings && void updateSettings({ ...settings, logging: { ...(settings.logging ?? {}), enabled: e.target.checked } as Settings['logging'] })} />
                  <span className="toggle-track" />
                </span>
              </label>
              <label className="agent-setting-row">
                <div>
                  <div className="agent-setting-name">Log tool inputs</div>
                  <div className="agent-setting-desc">Store tool arguments in each log entry</div>
                </div>
                <span className="toggle-switch">
                  <input type="checkbox" checked={settings?.logging?.log_tool_inputs ?? true} onChange={e => settings && void updateSettings({ ...settings, logging: { ...(settings.logging ?? {}), log_tool_inputs: e.target.checked } as Settings['logging'] })} />
                  <span className="toggle-track" />
                </span>
              </label>
              <label className="agent-setting-row">
                <div>
                  <div className="agent-setting-name">Log tool results</div>
                  <div className="agent-setting-desc">Store tool output in each log entry</div>
                </div>
                <span className="toggle-switch">
                  <input type="checkbox" checked={settings?.logging?.log_tool_results ?? true} onChange={e => settings && void updateSettings({ ...settings, logging: { ...(settings.logging ?? {}), log_tool_results: e.target.checked } as Settings['logging'] })} />
                  <span className="toggle-track" />
                </span>
              </label>
              <label className="agent-setting-row">
                <div>
                  <div className="agent-setting-name">Log full responses</div>
                  <div className="agent-setting-desc">Store complete model responses (uses more disk)</div>
                </div>
                <span className="toggle-switch">
                  <input type="checkbox" checked={settings?.logging?.log_responses ?? false} onChange={e => settings && void updateSettings({ ...settings, logging: { ...(settings.logging ?? {}), log_responses: e.target.checked } as Settings['logging'] })} />
                  <span className="toggle-track" />
                </span>
              </label>
              <div className="agent-row">
                <span className="agent-label">Keep last</span>
                <input
                  type="number"
                  min={10}
                  max={1000}
                  value={settings?.logging?.max_runs ?? 100}
                  style={{ width: 64, textAlign: 'right' }}
                  onChange={e => {
                    const v = parseInt(e.target.value, 10)
                    if (settings && v > 0) void updateSettings({ ...settings, logging: { ...(settings.logging ?? {}), max_runs: v } as Settings['logging'] })
                  }}
                />
              </div>
            </div>

            {/* Toolbar */}
            <div className="log-toolbar">
              <input
                className="log-search"
                placeholder="Search logs..."
                value={logFilter}
                onChange={e => setLogFilter(e.target.value)}
              />
              <div className="log-filter-row">
                <select
                  value={logStatusFilter}
                  onChange={e => setLogStatusFilter(e.target.value as typeof logStatusFilter)}
                >
                  <option value="all">All</option>
                  <option value="problem">Problems</option>
                  <option value="error">Errors</option>
                  <option value="stopped">Stopped</option>
                  <option value="done">Done</option>
                </select>
                <button onClick={() => void refreshPanelData()}>Refresh</button>
                <button
                  title="Copy all logs as JSON"
                  onClick={() => void exportLogs()}
                >Export JSON</button>
                <button className="agent-danger-button" onClick={() => void clearLogs()}>Clear Logs</button>
              </div>
              {panelStatus && <div className="agent-panel-status">{panelStatus}</div>}
            </div>

            {/* Log entries */}
            <div className="agent-activity-list">
              {(() => {
                const q = logFilter.trim().toLowerCase()
                const filtered = runs.filter(run => {
                  if (logStatusFilter === 'problem' && !['error', 'stopped'].includes(run.status) && !run.stop_reason) return false
                  if (logStatusFilter === 'error' && run.status !== 'error') return false
                  if (logStatusFilter === 'stopped' && run.status !== 'stopped') return false
                  if (logStatusFilter === 'done' && run.status !== 'done') return false
                  if (!q) return true
                  return [
                    run.prompt,
                    run.mode,
                    run.profile,
                    run.model ?? '',
                    run.stop_reason ?? '',
                    run.stop_detail ?? '',
                    run.summary ?? '',
                    ...(run.events ?? []).flatMap(event => [event.kind, event.message, event.detail ?? '']),
                    ...(run.tools ?? []).flatMap(tool => [tool.name, tool.status, tool.input ?? '', tool.result ?? '']),
                  ].join(' ').toLowerCase().includes(q)
                })
                if (filtered.length === 0) return <div className="agent-activity-empty">{runs.length === 0 ? 'No logs yet' : 'No matching logs'}</div>
                return filtered.map(run => (
                  <details key={run.id} className="agent-activity-item" open={run.status === 'error'}>
                    <summary>
                      <span className={run.status === 'running' ? 'agent-pending' : run.status === 'error' ? 'agent-bad' : run.status === 'stopped' ? 'agent-warn' : 'agent-ok'}>{run.status}</span>
                      {run.state && <span className="run-state-pill">{run.state}</span>}
                      <span className="activity-name">{run.mode} · {run.profile}</span>
                      {run.duration_ms != null && (
                        <span className="activity-duration">{fmtDuration(run.duration_ms)}</span>
                      )}
                    </summary>

                    {/* Metadata row */}
                    <div className="run-meta">
                      <span title="Started">{new Date(run.started_at).toLocaleTimeString()}</span>
                      {run.state && <span title="Final state">{run.state}</span>}
                      {run.model && <span title="Model" className="run-model">{run.model}</span>}
                      {run.total_tokens != null && run.total_tokens > 0
                        ? <span title="Total tokens">{run.total_tokens.toLocaleString()} tok</span>
                        : run.prompt_tokens != null && run.completion_tokens != null && (
                          <span title="Prompt / completion tokens">↑{run.prompt_tokens.toLocaleString()} ↓{run.completion_tokens.toLocaleString()}</span>
                        )}
                      {(run.tools ?? []).length > 0 && (
                        <span>{(run.tools ?? []).length} tool{(run.tools ?? []).length !== 1 ? 's' : ''}</span>
                      )}
                      {compactionCount(run) > 0 && (
                        <span>{compactionCount(run)} compaction{compactionCount(run) !== 1 ? 's' : ''}</span>
                      )}
                      {run.stop_reason && (
                        <span className="run-stop-reason">{run.stop_reason}</span>
                      )}
                      <button
                        className="run-copy-button"
                        title="Copy this run as JSON"
                        onClick={e => {
                          e.preventDefault()
                          e.stopPropagation()
                          void navigator.clipboard.writeText(JSON.stringify(run, null, 2))
                        }}
                      >
                        Copy
                      </button>
                    </div>

                    {/* Prompt */}
                    <div className="activity-section">
                      <div className="activity-label">Prompt</div>
                      <pre>{trimActivity(run.prompt)}</pre>
                    </div>

                    {/* Full response (when log_responses is on) */}
                    {run.response && (
                      <div className="activity-section">
                        <div className="activity-label">Response</div>
                        <pre>{trimActivity(run.response)}</pre>
                      </div>
                    )}

                    {/* Summary / stop detail */}
                    {!run.response && (run.summary || run.stop_detail) && (
                      <div className="activity-section">
                        <div className="activity-label">{run.stop_detail ? 'Stopped' : 'Summary'}</div>
                        <pre>{trimActivity(run.stop_detail ?? run.summary ?? '')}</pre>
                      </div>
                    )}

                    {(run.events ?? []).length > 0 && (
                      <div className="activity-section">
                        <div className="activity-label">Timeline ({(run.events ?? []).length})</div>
                        <div className="run-event-list">
                          {(run.events ?? []).map((event, index) => (
                            <details key={`${run.id}-event-${index}`} className="run-event-row">
                              <summary>
                                <span className={eventStatusClass(event.kind)}>{event.kind}</span>
                                <span className="run-event-message">{event.message}</span>
                                <span className="activity-duration">{new Date(event.timestamp).toLocaleTimeString()}</span>
                              </summary>
                              {event.detail && <pre>{trimActivity(event.detail)}</pre>}
                            </details>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Tool log */}
                    {(run.tools ?? []).length > 0 && (
                      <div className="activity-section">
                        <div className="activity-label">Tools ({(run.tools ?? []).length})</div>
                        {(run.tools ?? []).map((tool, index) => (
                          <div key={`${run.id}-${index}`} className="run-tool-row">
                            <span className={activityStatusClass(tool.status)}>{tool.status}</span>
                            <span className="run-tool-name">{tool.name}</span>
                            {tool.duration_ms != null && tool.duration_ms > 0 && (
                              <span className="activity-duration">{fmtDuration(tool.duration_ms)}</span>
                            )}
                            {(tool.input || tool.result) && (
                              <details className="run-tool-detail">
                                <summary>{tool.input && tool.result ? 'input / result' : tool.input ? 'input' : 'result'}</summary>
                                {tool.input && <pre>{trimActivity(tool.input)}</pre>}
                                {tool.result && <pre>{trimActivity(tool.result)}</pre>}
                              </details>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </details>
                ))
              })()}
            </div>
          </>
        )}

        {tab === 'skills' && (
          <>
            <div className="agent-section-title">Skills</div>

            <div className="skill-suggestion-card skill-source-card">
              <div className="skill-suggestion-title">Project Workflow Source</div>
              <div className="skill-suggestion-reason">
                Register a master_skill.md, master_skills.md, or instruction folder as the selectable master skill.
              </div>
              <input
                className="agent-input"
                placeholder={skills.some(skill => skill.name === 'master' && skill.source_path) ? 'Master skill registered. Pick a file/folder to replace it.' : 'Path to master_skill.md, master_skills.md, or an instruction folder'}
                value={projectSkillPath}
                onChange={e => setProjectSkillPath(e.target.value)}
              />
              {skills.some(skill => skill.name === 'master' && skill.source_path) && (
                <div className="skill-suggestion-reason">Master skill is registered for lazy use.</div>
              )}
              <div className="skill-suggestion-actions">
                <button onClick={pickProjectSkillPath} disabled={saving}>Pick file</button>
                <button onClick={pickProjectSkillFolder} disabled={saving}>Pick folder</button>
                <button onClick={() => void activateProjectSkillPath(projectSkillPath)} disabled={saving}>
                  Add Master Skill
                </button>
                <button onClick={() => void activateProjectSkillPath('')} disabled={saving || !skills.some(skill => skill.name === 'master' && skill.source_path)}>
                  Clear
                </button>
              </div>
              {projectInstructionSummary && (
                <pre className="skill-viewer-body">{projectInstructionSummary}</pre>
              )}
            </div>

            {/* Post-run learning suggestion card */}
            {skillSuggestion && (
              <div className="skill-suggestion-card">
                <div className="skill-suggestion-title">{skillSuggestion.title}</div>
                <div className="skill-suggestion-reason">{skillSuggestion.reason}</div>
                <div className="skill-suggestion-actions">
                  <button
                    className="skill-suggestion-save"
                    onClick={() => {
                      setSkillDraft({
                        name: '',
                        description: '',
                        version: '1.0.0',
                        tags: [],
                        source_path: '',
                        body: '',
                        raw: skillSuggestion.template,
                        created_at: '',
                        updated_at: '',
                      })
                      setEditingSkill(null)
                      setViewingSkill(null)
                      onDismissSkillSuggestion()
                    }}
                  >Save as Skill</button>
                  <button className="skill-suggestion-dismiss" onClick={onDismissSkillSuggestion}>Dismiss</button>
                </div>
              </div>
            )}

            {/* Skill editor (new or edit) */}
            {(skillDraft !== null || editingSkill !== null) && (() => {
              const editing = editingSkill ?? skillDraft!
              const isNew = editingSkill === null
              return (
                <div className="skill-editor">
                  <div className="skill-editor-header">
                    <span>{isNew ? 'New Skill' : `Edit: ${editingSkill?.name}`}</span>
                    <button onClick={() => { setEditingSkill(null); setSkillDraft(null) }}>✕</button>
                  </div>
                  <input
                    className="skill-editor-name"
                    placeholder="name (slug, e.g. fix-go-tool-calls)"
                    value={editing.name}
                    onChange={e => isNew
                      ? setSkillDraft(s => s ? { ...s, name: e.target.value } : s)
                      : setEditingSkill(s => s ? { ...s, name: e.target.value } : s)
                    }
                  />
                  <input
                    className="skill-editor-desc"
                    placeholder="description (one-line trigger)"
                    value={editing.description}
                    onChange={e => isNew
                      ? setSkillDraft(s => s ? { ...s, description: e.target.value } : s)
                      : setEditingSkill(s => s ? { ...s, description: e.target.value } : s)
                    }
                  />
                  <input
                    className="skill-editor-tags"
                    placeholder="tags (comma-separated)"
                    value={editing.tags.join(', ')}
                    onChange={e => {
                      const tags = e.target.value.split(',').map(t => t.trim()).filter(Boolean)
                      isNew
                        ? setSkillDraft(s => s ? { ...s, tags } : s)
                        : setEditingSkill(s => s ? { ...s, tags } : s)
                    }}
                  />
                  <textarea
                    className="skill-editor-body"
                    placeholder="## Overview&#10;...&#10;## Steps&#10;1. ..."
                    value={editing.raw || editing.body}
                    rows={18}
                    onChange={e => isNew
                      ? setSkillDraft(s => s ? { ...s, raw: e.target.value, body: e.target.value } : s)
                      : setEditingSkill(s => s ? { ...s, raw: e.target.value, body: e.target.value } : s)
                    }
                  />
                  <div className="skill-editor-footer">
                    <button
                      disabled={!editing.name.trim()}
                      onClick={async () => {
                        const toSave: Skill = {
                          ...editing,
                          body: editing.raw || editing.body,
                          raw: editing.raw || editing.body,
                        }
                        const saved = await SaveSkill(toSave).catch(() => null)
                        if (saved) {
                          setSkills(prev => [saved, ...prev.filter(s => s.name !== saved.name)])
                          setEditingSkill(null)
                          setSkillDraft(null)
                        }
                      }}
                    >Save</button>
                    <button onClick={() => { setEditingSkill(null); setSkillDraft(null) }}>Cancel</button>
                  </div>
                </div>
              )
            })()}

            {/* Skill viewer */}
            {viewingSkill && !editingSkill && skillDraft === null && (
              <div className="skill-viewer">
                <div className="skill-viewer-header">
                  <span>{viewingSkill.name}</span>
                  <div>
                    <button onClick={() => { setEditingSkill(viewingSkill); setViewingSkill(null) }}>Edit</button>
                    <button onClick={() => setViewingSkill(null)}>✕</button>
                  </div>
                </div>
                {viewingSkill.description && <div className="skill-viewer-desc">{viewingSkill.description}</div>}
                <pre className="skill-viewer-body">{skillViewerText(viewingSkill)}</pre>
              </div>
            )}

            {/* Skill list */}
            {!editingSkill && skillDraft === null && !viewingSkill && (
              <>
                <div className="agent-row">
                  <input
                    className="agent-input"
                    placeholder="Filter skills..."
                    value={skillFilter}
                    onChange={e => setSkillFilter(e.target.value)}
                  />
                  <button onClick={() => {
                    setSkillDraft({ name: '', description: '', version: '1.0.0', tags: [], source_path: '', body: '', raw: '', created_at: '', updated_at: '' })
                    setEditingSkill(null)
                  }}>+ New</button>
                  <button onClick={() => void load()}>↺</button>
                </div>
                <div className="agent-memory-list skill-list">
                  {skills.filter(s => {
                    const f = skillFilter.trim().toLowerCase()
                    if (!f) return true
                    return `${s.name} ${s.description} ${s.tags.join(' ')}`.toLowerCase().includes(f)
                  }).length === 0 ? (
                    <div className="agent-activity-empty">
                      {skills.length === 0
                        ? 'No skills yet. Create one or let the agent suggest one after a complex task.'
                        : 'No skills match filter.'}
                    </div>
                  ) : skills.filter(s => {
                    const f = skillFilter.trim().toLowerCase()
                    if (!f) return true
                    return `${s.name} ${s.description} ${s.tags.join(' ')}`.toLowerCase().includes(f)
                  }).map(skill => (
                    <div key={skill.name} className="memory-entry">
                      <div className="memory-entry-header">
                        <span className="memory-entry-title" onClick={() => setViewingSkill(skill)} style={{ cursor: 'pointer' }}>
                          {skill.name}
                        </span>
                        <div className="memory-entry-actions">
                          <button title="Edit" onClick={() => { setEditingSkill(skill); setViewingSkill(null) }}>✎</button>
                          <button title="Delete" onClick={async () => {
                            if (!confirm(`Delete skill "${skill.name}"?`)) return
                            await DeleteSkill(skill.name).catch(() => null)
                            setSkills(prev => prev.filter(s => s.name !== skill.name))
                          }}>✕</button>
                        </div>
                      </div>
                      {skill.description && <div className="memory-entry-content">{skill.description}</div>}
                      {skill.tags.length > 0 && (
                        <div className="memory-entry-tags">
                          {skill.tags.map(t => <span key={t} className="memory-tag">{t}</span>)}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </>
            )}
          </>
        )}

        {tab === 'activity' && (
          <>
            <div className="agent-section-title">Activity</div>
            <div className="agent-activity-list">
              {activity.length === 0 ? (
                <div className="agent-activity-empty">Idle</div>
              ) : activity.map(item => (
                <details key={item.id} className="agent-activity-item">
                  <summary>
                    <span className={activityStatusClass(item.status)}>{item.status}</span>
                    <span className="activity-name">{item.name}</span>
                    {item.durationMs !== undefined && (
                      <span className="activity-duration">{item.durationMs < 1000 ? `${item.durationMs}ms` : `${(item.durationMs / 1000).toFixed(1)}s`}</span>
                    )}
                  </summary>
                  {item.input && (
                    <div className="activity-section">
                      <div className="activity-label">Input</div>
                      <pre>{trimActivity(item.input)}</pre>
                    </div>
                  )}
                  {item.result && (
                    <div className="activity-section">
                      <div className="activity-label">Result</div>
                      <pre>{trimActivity(item.result)}</pre>
                    </div>
                  )}
                </details>
              ))}
            </div>
          </>
        )}
      </div>

      {/* Doctor panel — shown when result is available */}
      {doctorResult && (
        <div className="doctor-panel">
          <div className="doctor-header">
            <span className={`doctor-grade doctor-grade-${doctorResult.grade.toLowerCase()}`}>
              {doctorResult.grade} ({doctorResult.score}/100)
            </span>
            <button className="doctor-close" onClick={() => setDoctorResult(null)}>✕</button>
          </div>
          <div className="doctor-checks">
            {doctorResult.checks.map((c, i) => (
              <div key={i} className={`doctor-check doctor-check-${c.status}`}>
                <span className="doctor-check-icon">
                  {c.status === 'ok' ? '✓' : c.status === 'fail' ? '✗' : c.status === 'warn' ? '⚠' : 'ℹ'}
                </span>
                <div className="doctor-check-body">
                  <div className="doctor-check-name">{c.name}</div>
                  <div className="doctor-check-msg">{c.message}</div>
                  {c.detail && <div className="doctor-check-detail">{c.detail}</div>}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="agent-actions">
        <button onClick={() => void StopAgent()} disabled={!streaming}>Stop</button>
        <button onClick={onClearChat} title="Clear chat history">Clear Chat</button>
        <button
          onClick={() => void runDoctor()}
          disabled={doctorRunning}
          title="Run health checks"
        >{doctorRunning ? '...' : 'Doctor'}</button>
        <button
          onClick={() => void killInferenceServers()}
          disabled={maintenanceRunning !== ''}
          title="Stop stale InferenceBridge and llama-server processes"
        >{maintenanceRunning === 'inference' ? '...' : 'Kill Inference'}</button>
        <button
          onClick={() => void restartWSL()}
          disabled={maintenanceRunning !== ''}
          title="Run wsl.exe --shutdown"
        >{maintenanceRunning === 'wsl' ? '...' : 'Restart WSL'}</button>
        <button onClick={onOpenSettings}>Settings</button>
      </div>
    </aside>
  )
}

function fmtDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  const m = Math.floor(ms / 60_000)
  const s = Math.round((ms % 60_000) / 1000)
  return `${m}m${s}s`
}

function trimActivity(text: string) {
  return text.length > 1200 ? `${text.slice(0, 1200)}\n...` : text
}

function skillViewerText(skill: Skill): string {
  if (skill.name === 'master' && skill.source_path) {
    return [
      'External master skill source is registered for lazy use.',
      '',
      'Use skill_view with name "master" and an optional focused query to read only the needed sections.',
      '',
      'The local source path is stored internally and hidden from this view.',
    ].join('\n')
  }
  return skill.raw || skill.body
}

function activityStatusClass(status: string): string {
  switch (status) {
    case 'running': return 'agent-pending'
    case 'done': return 'agent-ok'
    case 'blocked': return 'agent-warn'
    case 'denied': return 'agent-warn'
    case 'error': return 'agent-bad'
    default: return 'agent-ok'
  }
}

function eventStatusClass(kind: string): string {
  switch (kind) {
    case 'error': return 'agent-bad'
    case 'stop':
    case 'blocked':
    case 'denied':
    case 'tool_error':
    case 'guardrail': return 'agent-warn'
    case 'continue':
    case 'compaction':
    case 'truncated': return 'agent-pending'
    default: return 'agent-muted'
  }
}

function compactionCount(run: TaskRun): number {
  return (run.events ?? []).filter(event => event.kind === 'compaction').length
}

function todoStatusClass(status: string): string {
  switch (status) {
    case 'done': return 'agent-ok'
    case 'in_progress': return 'agent-pending'
    case 'blocked': return 'agent-warn'
    default: return 'agent-muted'
  }
}
