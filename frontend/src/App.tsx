import { useState, useEffect, useCallback, useRef, type MouseEvent as ReactMouseEvent } from 'react'
import { EventsOn } from './wailsjs/runtime'
import { FileTree } from './components/FileTree'
import { HermesSidebar } from './components/HermesSidebar'
import { ChatPane } from './components/ChatPane'
import { FileViewer, type OpenFile } from './components/FileViewer'
import { AgentPanel } from './components/AgentPanel'
import { RightInspector } from './components/RightInspector'
import { LogsPage } from './components/LogsPage'
import { MemoryPage } from './components/MemoryPage'
import { BenchmarkPage } from './components/BenchmarkPage'
import { LiveOpsPage } from './components/LiveOpsPage'
import { BrainPage } from './components/BrainPage'
import { ContextInspectorPage } from './components/ContextInspectorPage'
import { ProjectsPage } from './components/ProjectsPage'
import { EngagementPage } from './components/EngagementPage'
import { TelegramPage } from './components/TelegramPage'
import { DoctorPage } from './components/DoctorPage'
import { SideChatPage } from './components/SideChatPage'
import { ServicesPage } from './components/ServicesPage'
import { StatusBar } from './components/StatusBar'
import { SettingsModal } from './components/SettingsModal'
import { ConfirmDialog } from './components/ConfirmDialog'
import { ToastContainer, type ToastItem } from './components/Toast'
import { TerminalPane } from './components/TerminalPane'
import { StreamPane } from './components/StreamPane'
import { JobsPane } from './components/JobsPane'
import {
  ClearHistory,
  ClearTodos,
  AddToolSafeRule,
  DeleteSession,
  ListSessions,
  LoadSession,
  RespondConfirm,
  SaveSession,
  SetAutoAgents,
  SetAutonomous,
  GetAutoAgents,
  GetAutonomous,
  GetAgentMode,
  ListAgentDefinitions,
  SelectWorkingDir,
  SetWorkingDir,
  SetAgentModeOverride,
  GetProfiles,
  GetSettings,
  GetHistoryStats,
  ListTodos,
  SendMessage,
  SendMessageWithProfile,
  UpdateSettings,
  type ChatAttachment,
  StopAgent,
  type ChatRole,
  type ProfilesFile,
  type SessionChatMessage,
  type SkillSuggestion,
  type TodoItem,
  type AgentDefinition,
} from './wailsjs/go'
import './App.css'

function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '')
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}

function contrastText(hex: string): string {
  const [r, g, b] = hexToRgb(hex)
  const lum = (0.299 * r + 0.587 * g + 0.114 * b) / 255
  return lum > 0.55 ? '#111111' : '#ffffff'
}

function applyTheme(theme: string) {
  const next = ['light', 'slate', 'mauler-ops', 'dark'].includes(theme) ? theme : 'mauler-ops'
  document.documentElement.setAttribute('data-theme', next === 'dark' ? 'mauler-ops' : next)
}

function applyAccentColor(hex: string) {
  const [r, g, b] = hexToRgb(hex)
  document.documentElement.style.setProperty('--accent', hex)
  document.documentElement.style.setProperty('--accent-hover', hex)
  document.documentElement.style.setProperty('--accent-glow', `rgba(${r},${g},${b},0.18)`)
  document.documentElement.style.setProperty('--accent-text', contrastText(hex))
}

function applyPrimaryColor(hex: string) {
  document.documentElement.style.setProperty('--btn-primary', hex)
  document.documentElement.style.setProperty('--btn-primary-text', contrastText(hex))
}

function oneTaskCloudProfiles(profilesFile: ProfilesFile): RunProfileOption[] {
  return Object.entries(profilesFile.profiles ?? {})
    .filter(([, profile]) => {
      const providerName = String(profile.provider || '').toLowerCase()
      const provider = profilesFile.providers?.[profile.provider]
      const baseURL = String(provider?.base_url || '').toLowerCase()
      return providerName === 'openrouter' || baseURL.includes('openrouter.ai')
    })
    .map(([name, profile]) => ({ name, model: profile.model_id || name }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

function toolTimeoutFromInput(input: string): number {
  try {
    const parsed = JSON.parse(input) as { timeout?: unknown }
    const timeout = Number(parsed.timeout)
    if (Number.isFinite(timeout) && timeout > 0) return Math.floor(timeout)
  } catch {
    return 0
  }
  return 0
}

function isPlanTool(name: string): boolean {
  return name.startsWith('todo_')
}

function cleanAssistantTranscriptText(text: string): string {
  let next = stripVisibleThinkTags(text).trim()
  if (next === '*' || next === '.' || next === '-' || next === '...') return ''
  if (next.length < 4) return ''
  return next
}

function stripVisibleThinkTags(text: string): string {
  let next = text || ''
  while (true) {
    const lower = next.toLowerCase()
    const start = lower.indexOf('<think')
    if (start < 0) return next
    const tagEnd = lower.indexOf('>', start)
    if (tagEnd < 0) return next.slice(0, start).trim()
    const close = lower.indexOf('</think>', tagEnd + 1)
    if (close < 0) return next.slice(0, start).trim()
    next = `${next.slice(0, start).trim()}\n${next.slice(close + '</think>'.length).trim()}`
  }
}

export interface ChatMessage {
  id: string
  role: ChatRole
  content: string
  timestamp: number
  images?: string[]
  attachments?: ChatAttachment[]
  queued?: boolean
  thinking?: string
}

interface PendingMessage {
  text: string
  images: string[]
  attachments: ChatAttachment[]
  profileOverride: string
}

export interface RunProfileOption {
  name: string
  model: string
}

export interface ConfirmPayload {
  id: string
  name: string
  input: string
}

interface ConfirmAction {
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
  onConfirm: () => void | Promise<void>
}

export interface AgentActivity {
  id: string
  name: string
  status: 'running' | 'done' | 'blocked' | 'denied' | 'error'
  input?: string
  result?: string
  startTime: number
  durationMs?: number
}

export interface RunStatePayload {
  id?: string
  state: string
  detail?: string
}

export interface ToolCountdown {
  id: string
  name: string
  timeoutSec: number
  startedAt: number
  deadline: number
}

export interface BackgroundJob {
  id: string
  command: string
  state: string
  log?: string
  pidfile?: string
  output?: string
  elapsed_sec?: number
  next_poll_sec?: number
  verbose?: boolean
  exit_code?: number
  updated_at_unix?: number
}

const LEFT_PANE_DEFAULT = 300
const LEFT_PANE_MIN = 180
const LEFT_PANE_MAX = 520
const RIGHT_PANE_DEFAULT = 460
const RIGHT_PANE_MIN = 320
const RIGHT_PANE_MAX = 840

function loadStoredLayoutNumber(key: string, fallback: number, min: number, max: number) {
  const value = Number(localStorage.getItem(key) || '')
  return Number.isFinite(value) && value >= min && value <= max ? value : fallback
}

function loadStoredLayoutBoolean(key: string, fallback: boolean) {
  const value = localStorage.getItem(key)
  if (value === '1') return true
  if (value === '0') return false
  return fallback
}

function storeLayoutNumber(key: string, value: number) {
  localStorage.setItem(key, String(Math.round(value)))
}

export default function App() {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [streaming, setStreaming] = useState(false)
  const [streamBuffer, setStreamBuffer] = useState('')
  const answerCheckpointRef = useRef('')
  const streamErrorHandledRef = useRef(false)
  const [confirm, setConfirm] = useState<ConfirmPayload | null>(null)
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(null)
  const [showSaveSession, setShowSaveSession] = useState(false)
  const [saveSessionDraft, setSaveSessionDraft] = useState('')
  const [showSettings, setShowSettings] = useState(false)
  const [statsVersion, setStatsVersion] = useState(0)
  const [sessions, setSessions] = useState<string[]>([])
  const [selectedSession, setSelectedSession] = useState('')
  const [activeProfile, setActiveProfile] = useState('')
  const [cloudRunProfiles, setCloudRunProfiles] = useState<RunProfileOption[]>([])
  const [runProfileOverride, setRunProfileOverride] = useState('')
  const [activeRunProfile, setActiveRunProfile] = useState('')
  const [autonomous, setAutonomousState] = useState(false)
  const [autoAgents, setAutoAgentsState] = useState(true)
  const [centerTab, setCenterTab] = useState<'chat' | 'ops' | 'projects' | 'engagement' | 'services' | 'file' | 'logs' | 'memory' | 'brain' | 'context' | 'telegram' | 'benchmarks' | 'doctor'>('projects')
  const [chatLane, setChatLane] = useState<'project' | 'quick'>('project')
  const [openFiles, setOpenFiles] = useState<OpenFile[]>([])
  const [activeFileIdx, setActiveFileIdx] = useState(0)
  const [artifactOutput, setArtifactOutput] = useState('')
  const [artifactRunning, setArtifactRunning] = useState(false)
  const [activity, setActivity] = useState<AgentActivity[]>([])
  const [agentMode, setAgentMode] = useState('Auto')
  const [agentSelection, setAgentSelection] = useState('Auto')
  const [agentDefinitions, setAgentDefinitions] = useState<AgentDefinition[]>([])
  const [runState, setRunState] = useState<RunStatePayload | null>(null)
  const [runStartedAt, setRunStartedAt] = useState<number | null>(null)
  const [doctorRunRequest, setDoctorRunRequest] = useState(0)
  const [taskRunVersion, setTaskRunVersion] = useState(0)
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const toastThresholds = useRef<Set<string>>(new Set())
  const appliedInitialUI = useRef(false)

  const pushToast = useCallback((message: string, level: ToastItem['level'] = 'warn') => {
    const id = crypto.randomUUID()
    setToasts(prev => [...prev, { id, message, level }])
  }, [])

  const persistTerminalHeight = useCallback(async (height: number) => {
    const settings = await GetSettings().catch(() => null)
    if (!settings) return
    await UpdateSettings({
      ...settings,
      ui: {
        ...settings.ui,
        terminal_height: Math.round(height),
      },
    }).catch(() => null)
  }, [])
  const [pendingInterrupt, setPendingInterrupt] = useState<PendingMessage | null>(null)
  const pendingInterruptRef = useRef<PendingMessage | null>(null)
  const [leftOpen, setLeftOpen] = useState(() => loadStoredLayoutBoolean('mauler.layout.leftOpen', true))
  const [rightOpen, setRightOpen] = useState(() => loadStoredLayoutBoolean('mauler.layout.rightOpen', true))
  const [leftWidth, setLeftWidth] = useState(() => loadStoredLayoutNumber('mauler.layout.leftWidth', LEFT_PANE_DEFAULT, LEFT_PANE_MIN, LEFT_PANE_MAX))
  const [rightWidth, setRightWidth] = useState(() => loadStoredLayoutNumber('mauler.layout.rightWidth', RIGHT_PANE_DEFAULT, RIGHT_PANE_MIN, RIGHT_PANE_MAX))
  const [thinkingBuffer, setThinkingBuffer] = useState('')
  const pendingThinkingRef = useRef('')
  const [showTerminal, setShowTerminal] = useState(false)
  const [terminalHeight, setTerminalHeight] = useState(220)
  const [bottomTab, setBottomTab] = useState<'terminal' | 'stream' | 'jobs'>('terminal')
  const [skillSuggestion, setSkillSuggestion] = useState<SkillSuggestion | null>(null)
  const [workspaceVersion, setWorkspaceVersion] = useState(0)
  const [toolCountdown, setToolCountdown] = useState<ToolCountdown | null>(null)
  const [showToolCountdown, setShowToolCountdown] = useState(false)
  const [backgroundJobs, setBackgroundJobs] = useState<BackgroundJob[]>([])
  const [todos, setTodos] = useState<TodoItem[]>([])
  const [chatDraftRequest, setChatDraftRequest] = useState<{ id: string; text: string } | null>(null)

  useEffect(() => {
    localStorage.setItem('mauler.layout.leftOpen', leftOpen ? '1' : '0')
  }, [leftOpen])

  useEffect(() => {
    localStorage.setItem('mauler.layout.rightOpen', rightOpen ? '1' : '0')
  }, [rightOpen])

  const toggleTerminalPanel = useCallback(() => {
    if (showTerminal && bottomTab === 'terminal') {
      setShowTerminal(false)
      return
    }
    setBottomTab('terminal')
    setShowTerminal(true)
  }, [bottomTab, showTerminal])

  const prepareEngagementRun = useCallback((prompt: string) => {
    setChatDraftRequest({ id: crypto.randomUUID(), text: prompt })
    setChatLane('project')
    setCenterTab('chat')
  }, [])

  const refreshTodos = useCallback(() => {
    void ListTodos().then(setTodos).catch(() => setTodos([]))
  }, [])

  useEffect(() => {
    const sendPendingMessage = (pending: PendingMessage) => {
      const request = pending.profileOverride
        ? SendMessageWithProfile(pending.text, pending.images, pending.attachments, pending.profileOverride)
        : SendMessage(pending.text, pending.images, pending.attachments)
      void request
        .then(() => setRunProfileOverride(''))
        .catch(e => console.error('interrupt SendMessage:', e))
    }
    const offs = [
      EventsOn('mauler:stream_start', (...args: unknown[]) => {
        setStreaming(true)
        setActiveRunProfile(String(args[0] || ''))
        setRunStartedAt(Date.now())
        setRunState({ state: 'starting', detail: 'Preparing request' })
        setStreamBuffer('')
        setThinkingBuffer('')
        answerCheckpointRef.current = ''
        streamErrorHandledRef.current = false
        setShowTerminal(true)
		// Opening live activity must not discard the height the user chose with
		// the visible terminal splitter. The previous 42%-of-window expansion
		// produced a huge empty terminal for short commands.
        pendingThinkingRef.current = ''
      }),
      EventsOn('mauler:budget_updated', () => {
        setStatsVersion(v => v + 1)
      }),
      EventsOn('mauler:thinking', (...args: unknown[]) => {
        const chunk = args[0] as string
        pendingThinkingRef.current += chunk
        setThinkingBuffer(prev => prev + chunk)
      }),
      EventsOn('mauler:thinking_done', (...args: unknown[]) => {
        // Store final thinking with the next assistant message
        pendingThinkingRef.current = args[0] as string
      }),
      EventsOn('mauler:delta', (...args: unknown[]) => {
        const chunk = args[0] as string
        setStreamBuffer(prev => prev + chunk)
      }),
      EventsOn('mauler:stream_replace', (...args: unknown[]) => {
        setStreamBuffer(args[0] as string)
      }),
      EventsOn('mauler:answer_checkpoint', (...args: unknown[]) => {
        const candidate = cleanAssistantTranscriptText(String(args[0] || ''))
        if (candidate.length > answerCheckpointRef.current.length) {
          answerCheckpointRef.current = candidate
        }
      }),
      EventsOn('mauler:tool_protocol_repair', () => {
        setStreamBuffer('')
      }),
      EventsOn('mauler:stream_done', (...args: unknown[]) => {
        setStreaming(false)
        setActiveRunProfile('')
        setToolCountdown(null)
        if (streamErrorHandledRef.current) {
          streamErrorHandledRef.current = false
          answerCheckpointRef.current = ''
          setStreamBuffer('')
          return
        }
        const terminalAnswer = cleanAssistantTranscriptText(String(args[0] || ''))
        const terminalStatus = String(args[1] || 'done')
        const stopReason = String(args[2] || '')
        setStreamBuffer(prev => {
          const current = cleanAssistantTranscriptText(prev)
          const checkpoint = cleanAssistantTranscriptText(answerCheckpointRef.current)
          const visible = terminalStatus === 'done'
            ? (terminalAnswer || current || checkpoint)
            : (checkpoint || terminalAnswer || current)
          answerCheckpointRef.current = ''
          if (visible) {
            const thinking = pendingThinkingRef.current || undefined
            pendingThinkingRef.current = ''
            setMessages(m => [
              ...m,
              {
                id: crypto.randomUUID(),
                role: 'assistant',
                content: visible,
                thinking,
                timestamp: Date.now(),
              },
              ...(terminalStatus !== 'done' && stopReason ? [{
                id: crypto.randomUUID(),
                role: 'system' as const,
                content: `The answer above was preserved, but run finalisation stopped: ${stopReason}`,
                timestamp: Date.now(),
              }] : []),
            ])
          } else {
            pendingThinkingRef.current = ''
          }
          return ''
        })
        setStatsVersion(v => v + 1)
        const pending = pendingInterruptRef.current
        if (pending) {
          pendingInterruptRef.current = null
          setPendingInterrupt(null)
          setMessages(m => [...m, {
            id: crypto.randomUUID(),
            role: 'user',
            content: pending.text,
            images: pending.images,
            attachments: pending.attachments,
            timestamp: Date.now(),
          }])
          sendPendingMessage(pending)
        }
      }),
      EventsOn('mauler:stream_error', (...args: unknown[]) => {
        const err = args[0] as string
        streamErrorHandledRef.current = true
        setStreaming(false)
        setActiveRunProfile('')
        setToolCountdown(null)
        setRunState({ state: 'failed', detail: err })
        setStreamBuffer(prev => {
          const checkpoint = cleanAssistantTranscriptText(answerCheckpointRef.current)
          const visible = checkpoint || cleanAssistantTranscriptText(prev)
          answerCheckpointRef.current = ''
          const thinking = pendingThinkingRef.current || undefined
          pendingThinkingRef.current = ''
          setMessages(m => [
            ...m,
            ...(visible ? [{
              id: crypto.randomUUID(),
              role: 'assistant' as const,
              content: visible,
              thinking,
              timestamp: Date.now(),
            }] : []),
            {
              id: crypto.randomUUID(),
              role: 'system' as const,
              content: visible
                ? `The answer above was preserved, but the follow-up run finalisation failed: ${err}`
                : `Error: ${err}`,
              timestamp: Date.now(),
            },
          ])
          return ''
        })
        const pending = pendingInterruptRef.current
        if (pending) {
          pendingInterruptRef.current = null
          setPendingInterrupt(null)
          setMessages(m => [...m, {
            id: crypto.randomUUID(),
            role: 'user',
            content: pending.text,
            images: pending.images,
            attachments: pending.attachments,
            timestamp: Date.now(),
          }])
          sendPendingMessage(pending)
        }
      }),
      EventsOn('mauler:tool_call', (...args: unknown[]) => {
        const tc = args[0] as { id: string; name: string; input: string; timeout?: string }
        const nextItem: AgentActivity = {
          id: tc.id,
          name: tc.name,
          status: 'running',
          input: tc.input,
          startTime: Date.now(),
        }
        setActivity(items => [nextItem, ...items].slice(0, 12))
        const emittedTimeout = Number(tc.timeout)
        const timeoutSec = Number.isFinite(emittedTimeout) && emittedTimeout > 0
          ? Math.floor(emittedTimeout)
          : toolTimeoutFromInput(tc.input)
        if (timeoutSec > 0) {
          const now = Date.now()
          setToolCountdown({
            id: tc.id,
            name: tc.name,
            timeoutSec,
            startedAt: now,
            deadline: now + timeoutSec * 1000,
          })
        }
      }),
      EventsOn('mauler:tool_result', (...args: unknown[]) => {
        const tr = args[0] as { id: string; name: string; result: string }
        setToolCountdown(prev => prev?.id === tr.id ? null : prev)
        setActivity(items => {
          const next = items.map(item => item.id === tr.id
            ? { ...item, status: 'done' as const, result: tr.result, durationMs: Date.now() - item.startTime }
            : item)
          if (next.some(item => item.id === tr.id)) return next
          const nextItem: AgentActivity = {
            id: tr.id,
            name: tr.name,
            status: 'done',
            result: tr.result,
            startTime: Date.now(),
            durationMs: 0,
          }
          return [nextItem, ...items].slice(0, 12)
        })
        setStatsVersion(v => v + 1)
        if (isPlanTool(tr.name)) {
          setTaskRunVersion(v => v + 1)
          refreshTodos()
        }
      }),
      EventsOn('mauler:image_progress', (...args: unknown[]) => {
        const progress = args[0] as {
          tool_call_id?: string
          job_id?: string
          status?: string
          message?: string
          progress?: number
          current_step?: number
          total_steps?: number
        }
        if (!progress?.tool_call_id) return
        const percent = Math.max(0, Math.min(100, Math.round((progress.progress ?? 0) * 100)))
        const steps = progress.total_steps
          ? ` · step ${progress.current_step ?? 0}/${progress.total_steps}`
          : ''
        const detail = `${progress.message || progress.status || 'Generating image'} · ${percent}%${steps}`
        setActivity(items => items.map(item => item.id === progress.tool_call_id
          ? { ...item, result: detail }
          : item))
      }),
      EventsOn('mauler:job_update', (...args: unknown[]) => {
        const job = args[0] as BackgroundJob
        if (!job?.id) return
        let isNew = false
        setBackgroundJobs(prev => {
          isNew = !prev.some(item => item.id === job.id)
          const next = prev.filter(item => item.id !== job.id)
          return [{ ...prev.find(item => item.id === job.id), ...job }, ...next]
            .sort((a, b) => (b.updated_at_unix ?? 0) - (a.updated_at_unix ?? 0))
            .slice(0, 20)
        })
        // Do not auto-switch the bottom panel; users may be watching Terminal.
        void isNew
      }),
      EventsOn('mauler:confirm', (...args: unknown[]) => {
        setConfirm(args[0] as ConfirmPayload)
      }),
      EventsOn('mauler:compact', (...args: unknown[]) => {
        const summary = args[0] as string
        const now = Date.now()
        setActivity(items => [{
          id: `context-compact-${now}`,
          name: 'context_compaction',
          status: 'done' as const,
          result: summary,
          startTime: now,
          durationMs: 0,
        }, ...items].slice(0, 12))
      }),
      EventsOn('mauler:agent_mode', (...args: unknown[]) => {
        setAgentMode((args[0] as string) || 'Auto')
      }),
      EventsOn('mauler:run_state', (...args: unknown[]) => {
        const payload = args[0] as RunStatePayload
        if (payload?.state) setRunState(payload)
      }),
      EventsOn('mauler:task_run', () => {
        setTaskRunVersion(v => v + 1)
        refreshTodos()
      }),
      EventsOn('mauler:engagement_changed', () => {
        setTaskRunVersion(v => v + 1)
      }),
      EventsOn('mauler:workspace_changed', () => {
        setMessages([])
        setStreamBuffer('')
        setThinkingBuffer('')
        setRunProfileOverride('')
        setActiveRunProfile('')
        setSelectedSession('')
        setConfirm(null)
        setConfirmAction(null)
        setOpenFiles(files => files.filter(file => !file.path))
        setActiveFileIdx(0)
        setCenterTab('chat')
        setChatLane('project')
        refreshTodos()
        setWorkspaceVersion(v => v + 1)
        setStatsVersion(v => v + 1)
      }),
      EventsOn('mauler:suggest_learning', (...args: unknown[]) => {
        const suggestion = args[0] as SkillSuggestion
        if (suggestion) setSkillSuggestion(suggestion)
      }),
      EventsOn('mauler:artifact_output', (...args: unknown[]) => {
        const chunk = args[0] as string
        setArtifactOutput(prev => prev + chunk)
        setArtifactRunning(true)
      }),
      EventsOn('mauler:artifact_done', () => {
        setArtifactRunning(false)
      }),
    ]
    return () => offs.forEach(off => off())
  }, [refreshTodos])

  useEffect(() => {
    refreshTodos()
  }, [refreshTodos, taskRunVersion])

  const refreshSessions = useCallback(async () => {
    const names = await ListSessions().catch(() => [] as string[])
    setSessions(names)
    setSelectedSession(prev => prev || names[0] || '')
  }, [])

  useEffect(() => {
    void refreshSessions()
  }, [refreshSessions])

  const refreshProfiles = useCallback(async () => {
    const [settings, profilesFile, auto, autoAgentEnabled, mode, definitions] = await Promise.all([
      GetSettings().catch(() => null),
      GetProfiles().catch(() => null),
      GetAutonomous().catch(() => false),
      GetAutoAgents().catch(() => true),
      GetAgentMode().catch(() => 'Auto'),
      ListAgentDefinitions().catch(() => [] as AgentDefinition[]),
    ])
    if (settings) {
      setActiveProfile(settings.active_profile)
      setShowToolCountdown(settings.ui.tool_countdown ?? false)
      setAgentSelection(settings.agents.mode_override || 'Auto')
      if (!appliedInitialUI.current) {
        appliedInitialUI.current = true
        setShowTerminal(settings.ui.terminal_default_open ?? false)
        setTerminalHeight(Math.min(600, Math.max(100, settings.ui.terminal_height || 260)))
      }
      applyTheme(settings.ui.theme || 'dark')
      applyAccentColor(settings.ui.accent_color || '#4ade80')
      applyPrimaryColor(settings.ui.primary_color || settings.ui.accent_color || '#16a34a')
    }
    setCloudRunProfiles(profilesFile ? oneTaskCloudProfiles(profilesFile) : [])
    setAutonomousState(auto)
    setAutoAgentsState(autoAgentEnabled)
    setAgentMode(mode || 'Auto')
    setAgentDefinitions(definitions)
  }, [])

  useEffect(() => {
    void refreshProfiles()
  }, [refreshProfiles, statsVersion])

  useEffect(() => {
    void GetHistoryStats().then(stats => {
      const f = stats.fraction
      const addToast = (key: string, message: string, level: ToastItem['level']) => {
        if (toastThresholds.current.has(key)) return
        toastThresholds.current.add(key)
        const id = crypto.randomUUID()
        setToasts(prev => [...prev, { id, message, level }])
      }
      if (f < 0.70) {
        toastThresholds.current.delete('warn75')
        toastThresholds.current.delete('danger90')
      } else if (f >= 0.90) {
        addToast('danger90', 'Context 90% full — compaction will trigger soon', 'danger')
      } else if (f >= 0.75) {
        addToast('warn75', 'Context 75% full — consider saving a session', 'warn')
      }
    }).catch(() => {})
  }, [statsVersion])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === ',' && (e.ctrlKey || e.metaKey)) {
        setShowSettings(true)
        e.preventDefault()
      }
      if (e.key === '`' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        toggleTerminalPanel()
      }
      if (e.key === 'Escape') {
        setShowSettings(false)
        setConfirm(null)
        setConfirmAction(null)
        setShowSaveSession(false)
      }
      if (e.key.toLowerCase() === 'k' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        setConfirmAction({
          title: 'Clear Chat',
          message: 'Clear chat history?',
          confirmLabel: 'Clear',
          onConfirm: async () => {
            await ClearHistory()
            setMessages([])
            setStreamBuffer('')
            setStatsVersion(v => v + 1)
          },
        })
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [toggleTerminalPanel])

  const handleUserMessage = useCallback((text: string, images: string[], attachments: ChatAttachment[] = []) => {
    setMessages(m => [...m, {
      id: crypto.randomUUID(),
      role: 'user',
      content: text,
      images,
      attachments,
      timestamp: Date.now(),
    }])
  }, [])

  const sendNow = useCallback(async (text: string, images: string[], attachments: ChatAttachment[], profileOverride = '') => {
    handleUserMessage(text, images, attachments)
    try {
      if (profileOverride) {
        await SendMessageWithProfile(text, images, attachments, profileOverride)
      } else {
        await SendMessage(text, images, attachments)
      }
      setRunProfileOverride('')
    } catch (e) {
      console.error('SendMessage failed:', e)
      setMessages(m => [...m, {
        id: crypto.randomUUID(),
        role: 'system',
        content: `Error: ${e}`,
        timestamp: Date.now(),
      }])
    }
  }, [handleUserMessage])

  const handleSubmitMessage = useCallback((text: string, images: string[], attachments: ChatAttachment[], profileOverride = '') => {
    if (streaming) {
      pendingInterruptRef.current = { text, images, attachments, profileOverride }
      setPendingInterrupt({ text, images, attachments, profileOverride })
      void StopAgent()
      return
    }
    void sendNow(text, images, attachments, profileOverride)
  }, [sendNow, streaming])

  const handleCancelPending = useCallback(() => {
    pendingInterruptRef.current = null
    setPendingInterrupt(null)
  }, [])

  const handleConfirmRespond = useCallback(async (allow: boolean, remember = false) => {
    const payload = confirm
    setConfirm(null)
    if (allow && remember && payload) {
      await AddToolSafeRule(payload.name, payload.input)
      pushToast('Tool request added to safe list')
    }
    await RespondConfirm(allow)
  }, [confirm, pushToast])

  const openOrFocusFile = useCallback((file: OpenFile) => {
    setOpenFiles(prev => {
      const existingIdx = file.path ? prev.findIndex(f => f.path === file.path) : -1
      if (existingIdx >= 0) {
        setActiveFileIdx(existingIdx)
        return prev
      }
      const next = [...prev, file]
      setActiveFileIdx(next.length - 1)
      return next
    })
    setArtifactOutput('')
    setCenterTab('file')
  }, [])

  const handleArtifact = useCallback((code: string, lang: string) => {
    const ext = lang === 'typescript' ? 'ts' : lang === 'javascript' ? 'js' : lang === 'markdown' ? 'md' : lang || 'txt'
    openOrFocusFile({ path: '', name: `snippet.${ext}`, content: code, lang: lang || 'plaintext' })
  }, [openOrFocusFile])

  const handleOpenFile = useCallback((file: OpenFile) => {
    openOrFocusFile(file)
  }, [openOrFocusFile])

  const closeFile = useCallback((idx: number) => {
    setOpenFiles(prev => {
      const next = prev.filter((_, i) => i !== idx)
      setActiveFileIdx(i => {
        if (next.length === 0) { setCenterTab('chat'); return 0 }
        return Math.min(i, next.length - 1)
      })
      return next
    })
  }, [])

  const handleToggleAutonomous = useCallback(async (enabled: boolean) => {
    await SetAutonomous(enabled)
    setAutonomousState(enabled)
  }, [])

  const handleToggleAutoAgents = useCallback(async (enabled: boolean) => {
    await SetAutoAgents(enabled)
    setAutoAgentsState(enabled)
    setAgentMode(enabled ? 'Auto' : 'Manual')
  }, [])

  const handleAgentSelectionChange = useCallback(async (mode: string) => {
    const next = mode.trim() || 'Auto'
    try {
      if (next === 'Manual') {
        await SetAgentModeOverride(next)
        await SetAutoAgents(false)
        setAutoAgentsState(false)
      } else {
        await SetAutoAgents(true)
        await SetAgentModeOverride(next)
        setAutoAgentsState(true)
      }
      setAgentSelection(next)
      setAgentMode(next)
      setStatsVersion(v => v + 1)
      pushToast(`${next} selected for this workspace.`, 'success')
    } catch (error) {
      pushToast(`Could not change agent: ${String(error)}`, 'danger')
    }
  }, [pushToast])

  const handleSwitchWorkspace = useCallback(async (path: string) => {
    if (streaming) {
      pushToast('Stop the active run before changing workspace.')
      return
    }
    try {
      await SetWorkingDir(path)
      pushToast(`Workspace opened: ${path}`, 'success')
    } catch (error) {
      pushToast(`Could not open workspace: ${String(error)}`, 'danger')
    }
  }, [pushToast, streaming])

  const handleChooseWorkspace = useCallback(async (bugBounty: boolean) => {
    if (streaming) {
      pushToast('Stop the active run before changing workspace.')
      return
    }
    try {
      const settings = await GetSettings()
      const selected = await SelectWorkingDir(settings.context.workspace_dir || '')
      if (!selected) return
      if (bugBounty) {
        await SetAutoAgents(true)
        await SetAgentModeOverride('Bug Bounty Hunter')
        setAutoAgentsState(true)
        setAgentSelection('Bug Bounty Hunter')
        setAgentMode('Bug Bounty Hunter')
      }
      setStatsVersion(v => v + 1)
      pushToast(`${bugBounty ? 'Bug bounty workspace' : 'Workspace'} opened: ${selected}`, 'success')
    } catch (error) {
      pushToast(`Could not choose workspace: ${String(error)}`, 'danger')
    }
  }, [pushToast, streaming])

  const mapSessionMessages = (loaded: SessionChatMessage[]): ChatMessage[] =>
    loaded
      .filter(m => !(m.role === 'system' && m.content.trimStart().startsWith('[Context compacted]')))
      .map(m => ({
      id: crypto.randomUUID(),
      role: m.role,
      content: m.content,
      images: m.images ?? [],
      attachments: m.attachments ?? [],
      timestamp: Date.now(),
      }))

  const cleanSessionName = (name: string) =>
    name.trim().replace(/[^A-Za-z0-9._-]+/g, '-').replace(/^[._-]+|[._-]+$/g, '')

  const handleSaveSession = useCallback(async () => {
    const fallback = new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')
    setSaveSessionDraft(selectedSession || `session-${fallback}`)
    setShowSaveSession(true)
  }, [selectedSession])

  const submitSaveSession = useCallback(async () => {
    const name = saveSessionDraft.trim()
    if (!name) return
    await SaveSession(name)
    await refreshSessions()
    setSelectedSession(cleanSessionName(name))
    setShowSaveSession(false)
  }, [refreshSessions, saveSessionDraft])

  const handleLoadSession = useCallback(async () => {
    if (!selectedSession) return
    const loaded = await LoadSession(selectedSession)
    setMessages(mapSessionMessages(loaded))
    setStreamBuffer('')
    setStatsVersion(v => v + 1)
  }, [selectedSession])

  const handleDeleteSession = useCallback(async () => {
    if (!selectedSession) return
    setConfirmAction({
      title: 'Delete Session',
      message: `Delete session "${selectedSession}"?`,
      confirmLabel: 'Delete',
      onConfirm: async () => {
        await DeleteSession(selectedSession)
        setSelectedSession('')
        await refreshSessions()
      },
    })
  }, [refreshSessions, selectedSession])

  const handleClearChat = useCallback(() => {
    setConfirmAction({
      title: 'Clear Chat',
      message: 'Clear this conversation and its active plan? Project files, saved sessions, and memory are preserved.',
      confirmLabel: 'Clear',
      onConfirm: async () => {
        await ClearHistory()
        await ClearTodos()
        setMessages([])
        setStreamBuffer('')
        refreshTodos()
        setStatsVersion(v => v + 1)
      },
    })
  }, [refreshTodos])

  const startResize = useCallback((side: 'left' | 'right') => (e: ReactMouseEvent) => {
    const startX = e.clientX
    const startLeft = leftWidth
    const startRight = rightWidth
    let nextLeft = startLeft
    let nextRight = startRight
    document.documentElement.classList.add('workbench-resizing-col')
    const onMove = (move: MouseEvent) => {
      const dx = move.clientX - startX
      if (side === 'left') {
        nextLeft = Math.min(LEFT_PANE_MAX, Math.max(LEFT_PANE_MIN, startLeft + dx))
        setLeftWidth(nextLeft)
      } else {
        nextRight = Math.min(RIGHT_PANE_MAX, Math.max(RIGHT_PANE_MIN, startRight - dx))
        setRightWidth(nextRight)
      }
    }
    const onUp = () => {
      document.documentElement.classList.remove('workbench-resizing-col')
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      if (side === 'left') storeLayoutNumber('mauler.layout.leftWidth', nextLeft)
      else storeLayoutNumber('mauler.layout.rightWidth', nextRight)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    e.preventDefault()
  }, [leftWidth, rightWidth])

  const resizePaneByKeyboard = useCallback((side: 'left' | 'right', key: string, shiftKey: boolean) => {
    if (!['ArrowLeft', 'ArrowRight', 'Home'].includes(key)) return false
    if (key === 'Home') {
      if (side === 'left') {
        setLeftWidth(LEFT_PANE_DEFAULT)
        storeLayoutNumber('mauler.layout.leftWidth', LEFT_PANE_DEFAULT)
      } else {
        setRightWidth(RIGHT_PANE_DEFAULT)
        storeLayoutNumber('mauler.layout.rightWidth', RIGHT_PANE_DEFAULT)
      }
      return true
    }
    const amount = shiftKey ? 48 : 16
    if (side === 'left') {
      const next = Math.min(LEFT_PANE_MAX, Math.max(LEFT_PANE_MIN, leftWidth + (key === 'ArrowRight' ? amount : -amount)))
      setLeftWidth(next)
      storeLayoutNumber('mauler.layout.leftWidth', next)
    } else {
      const next = Math.min(RIGHT_PANE_MAX, Math.max(RIGHT_PANE_MIN, rightWidth + (key === 'ArrowLeft' ? amount : -amount)))
      setRightWidth(next)
      storeLayoutNumber('mauler.layout.rightWidth', next)
    }
    return true
  }, [leftWidth, rightWidth])

  const gridColumns = `${leftOpen ? leftWidth : 28}px ${leftOpen ? 6 : 0}px 1fr ${rightOpen ? 6 : 0}px ${rightOpen ? rightWidth : 28}px`

  return (
    <div className="app-shell">
      <div className="titlebar">
        <div className="titlebar-brand">
          <span className="titlebar-name">TheMauler</span>
        </div>
        <div className="titlebar-actions">
          <div className="titlebar-group titlebar-session-group" aria-label="Session actions">
            <select
              className="session-select"
              value={selectedSession}
              onChange={e => setSelectedSession(e.target.value)}
              title="Saved sessions"
            >
              <option value="">Sessions</option>
              {sessions.map(name => <option key={name} value={name}>{name}</option>)}
            </select>
            <button onClick={() => void handleSaveSession()} title="Save session">Save</button>
            <button onClick={() => void handleLoadSession()} disabled={!selectedSession} title="Load session">Load</button>
            <button onClick={() => void handleDeleteSession()} disabled={!selectedSession} title="Delete saved session">Delete</button>
            <button className="titlebar-clear-session" onClick={handleClearChat} disabled={streaming} title="Clear current chat transcript">Clear Chat</button>
          </div>
          <div className="titlebar-sep" />
          <div className="titlebar-group">
            <button onClick={() => setLeftOpen(v => !v)} title="Toggle Explorer panel" className={leftOpen ? 'panel-toggle on' : 'panel-toggle'}>Explorer</button>
            <button onClick={() => setRightOpen(v => !v)} title="Toggle inspector panel" className={rightOpen ? 'panel-toggle on' : 'panel-toggle'}>Inspector</button>
          </div>
          <div className="titlebar-sep" />
          <button
            className="titlebar-doctor"
            onClick={() => {
              setCenterTab('doctor')
              setDoctorRunRequest(v => v + 1)
            }}
            title="Run Doctor diagnostics"
          >Doctor</button>
          <div className="titlebar-sep" />
          <button
            onClick={toggleTerminalPanel}
            title="Show or hide Terminal (Ctrl+`)"
            className={showTerminal && bottomTab === 'terminal' ? 'panel-toggle on' : 'panel-toggle'}
          >Terminal</button>
          <div className="titlebar-sep" />
          <button onClick={() => setShowSettings(true)} title="Settings (Ctrl+,)">Settings</button>
        </div>
      </div>

      <div className="workspace" style={{ gridTemplateColumns: gridColumns }}>
        <div className={leftOpen ? 'pane-slot' : 'pane-slot closed'}>
          {leftOpen ? (
            <HermesSidebar
              sessions={sessions}
              selectedSession={selectedSession}
              activeProfile={activeProfile}
              centerTab={centerTab}
              onSelectSession={setSelectedSession}
              onLoadSession={() => void handleLoadSession()}
              onSaveSession={() => void handleSaveSession()}
              onDeleteSession={() => void handleDeleteSession()}
              onClearChat={handleClearChat}
              onSelectTab={setCenterTab}
              onOpenSettings={() => setShowSettings(true)}
            />
          ) : (
            <button className="collapsed-rail collapsed-rail-left" onClick={() => setLeftOpen(true)} title="Open Explorer">
              <span>Explorer</span>
            </button>
          )}
        </div>
        <div
          className={leftOpen ? 'resize-handle resize-handle-left' : 'resize-handle disabled'}
          onMouseDown={leftOpen ? startResize('left') : undefined}
          onDoubleClick={() => setLeftOpen(false)}
          onKeyDown={e => {
            if (leftOpen && resizePaneByKeyboard('left', e.key, e.shiftKey)) e.preventDefault()
          }}
          role="separator"
          aria-label="Resize Explorer"
          aria-orientation="vertical"
          tabIndex={leftOpen ? 0 : -1}
          title={leftOpen ? 'Drag or use arrow keys to resize; double-click to collapse Explorer' : undefined}
        />

        <main className="center-pane">
          <div className="center-tabs">
            <button className={centerTab === 'projects' ? 'active' : ''} onClick={() => setCenterTab('projects')}>Home</button>
            <button className={centerTab === 'chat' ? 'active' : ''} onClick={() => setCenterTab('chat')}>Chat</button>
            <button className={centerTab === 'ops' ? 'active' : ''} onClick={() => setCenterTab('ops')}>Run</button>
            <button className={centerTab === 'engagement' ? 'active' : ''} onClick={() => setCenterTab('engagement')}>Grid</button>
            <button className={centerTab === 'benchmarks' ? 'active' : ''} onClick={() => setCenterTab('benchmarks')}>Benchmarks</button>
            <select className="center-more-select" value={['services','logs','memory','brain','context','telegram','doctor'].includes(centerTab) ? centerTab : ''} onChange={e => { if (e.target.value) setCenterTab(e.target.value as typeof centerTab) }} title="Secondary workbench pages">
              <option value="">More…</option><option value="services">Services</option><option value="logs">Logs</option><option value="memory">Memory</option><option value="brain">Brain</option><option value="context">Context</option><option value="telegram">Telegram</option><option value="doctor">Doctor</option>
            </select>
            {openFiles.map((f, i) => (
              <span key={`${f.path || f.name}-${i}`} className={`center-file-tab ${centerTab === 'file' && activeFileIdx === i ? 'active' : ''}`}>
                <button onClick={() => { setActiveFileIdx(i); setCenterTab('file') }}>{f.name}</button>
                <button className="tab-close" onClick={() => closeFile(i)} title="Close">x</button>
              </span>
            ))}
          </div>
          <div className="center-content">
            {centerTab === 'chat' && chatLane === 'project' ? (
              <ChatPane
                key={`project-chat-${workspaceVersion}`}
                messages={messages}
                streaming={streaming}
                streamBuffer={streamBuffer}
                thinkingBuffer={thinkingBuffer}
                activeProfile={activeProfile}
                activeRunProfile={activeRunProfile}
                cloudRunProfiles={cloudRunProfiles}
                runProfileOverride={runProfileOverride}
                autonomous={autonomous}
                pendingInterrupt={pendingInterrupt !== null}
                toolCountdown={showToolCountdown ? toolCountdown : null}
                runState={runState}
                todos={todos}
                activity={activity}
                settingsVersion={statsVersion}
                agentDefinitions={agentDefinitions}
                agentSelection={agentSelection}
                agentMode={agentMode}
                draftRequest={chatDraftRequest}
                onSubmitMessage={handleSubmitMessage}
                onRunProfileOverrideChange={setRunProfileOverride}
                onCancelPending={handleCancelPending}
                onStopAgent={() => void StopAgent()}
                onClearChat={handleClearChat}
                onArtifact={handleArtifact}
                onAutonomousChange={handleToggleAutonomous}
                onOpenQuickChat={() => setChatLane('quick')}
                onOpenSettings={() => setShowSettings(true)}
                onAgentSelectionChange={handleAgentSelectionChange}
                onChooseWorkspace={handleChooseWorkspace}
                onSwitchWorkspace={handleSwitchWorkspace}
                onOpenProjects={() => setCenterTab('projects')}
                onClearPlan={async () => { await ClearTodos(); refreshTodos() }}
              />
            ) : centerTab === 'chat' ? (
              <SideChatPage streaming={streaming} onOpenProjectChat={() => setChatLane('project')} />
            ) : centerTab === 'ops' ? (
              <LiveOpsPage
                streaming={streaming}
                runState={runState}
                activity={activity}
                agentMode={agentMode}
                activeProfile={activeProfile}
                statsVersion={statsVersion}
                taskRunVersion={taskRunVersion}
                runStartedAt={runStartedAt}
                onOpenFile={handleOpenFile}
              />
            ) : centerTab === 'services' ? (
              <ServicesPage jobs={backgroundJobs} onOpenSettings={() => setShowSettings(true)} onOpenJobs={() => { setShowTerminal(true); setBottomTab('jobs') }} />
            ) : centerTab === 'engagement' ? (
              <EngagementPage version={taskRunVersion + statsVersion + workspaceVersion} onOpenChat={() => { setChatLane('project'); setCenterTab('chat') }} onPrepareRun={prepareEngagementRun} />
            ) : centerTab === 'projects' ? (
              <ProjectsPage
                version={statsVersion + workspaceVersion}
                onOpenEngagement={() => setCenterTab('engagement')}
                onPrepareEngagementRun={prepareEngagementRun}
                onProjectChanged={(project, previousName) => {
                  setWorkspaceVersion(v => v + 1)
                  setStatsVersion(v => v + 1)
                  setMessages([])
                  setStreamBuffer('')
                  setCenterTab('chat')
                  pushToast(`Opened ${project.name || project.id}. Previous: ${previousName}. Project files and saved sessions preserved; chat and terminal reset.`, 'success')
                }}
              />
            ) : centerTab === 'logs' ? (
              <LogsPage version={taskRunVersion + statsVersion} />
            ) : centerTab === 'memory' ? (
              <MemoryPage version={taskRunVersion + statsVersion} />
            ) : centerTab === 'brain' ? (
              <BrainPage version={taskRunVersion + statsVersion} />
            ) : centerTab === 'context' ? (
              <ContextInspectorPage version={taskRunVersion + statsVersion + workspaceVersion} onOpenFile={handleOpenFile} onOpenSettings={() => setShowSettings(true)} />
            ) : centerTab === 'telegram' ? (
              <TelegramPage version={taskRunVersion + statsVersion} />
            ) : centerTab === 'doctor' ? (
              <DoctorPage runRequest={doctorRunRequest} />
            ) : centerTab === 'benchmarks' ? (
              <BenchmarkPage version={statsVersion} onProfilesChanged={() => { void refreshProfiles(); setStatsVersion(v => v + 1) }} />
            ) : (
              <FileViewer
                file={openFiles[activeFileIdx] ?? null}
                openFiles={openFiles}
                activeIndex={activeFileIdx}
                artifactOutput={artifactOutput}
                artifactRunning={artifactRunning}
                onArtifactOutputClear={() => setArtifactOutput('')}
                onSwitchFile={idx => {
                  setActiveFileIdx(idx)
                  setCenterTab('file')
                }}
                onCloseFile={closeFile}
                onClose={() => closeFile(activeFileIdx)}
              />
            )}
          </div>
        </main>

        <div
          className={rightOpen ? 'resize-handle resize-handle-right' : 'resize-handle disabled'}
          onMouseDown={rightOpen ? startResize('right') : undefined}
          onDoubleClick={() => setRightOpen(false)}
          onKeyDown={e => {
            if (rightOpen && resizePaneByKeyboard('right', e.key, e.shiftKey)) e.preventDefault()
          }}
          role="separator"
          aria-label="Resize Inspector"
          aria-orientation="vertical"
          tabIndex={rightOpen ? 0 : -1}
          title={rightOpen ? 'Drag or use arrow keys to resize; double-click to collapse Inspector' : undefined}
        />
        <div className={rightOpen ? 'pane-slot' : 'pane-slot closed'}>
          {rightOpen ? (
            <RightInspector
              activity={activity}
              runState={runState}
              streaming={streaming}
              taskRunVersion={taskRunVersion}
              workspaceVersion={workspaceVersion}
              doctorFocusRequest={doctorRunRequest}
              workspaceBrowser={<FileTree key={workspaceVersion} onOpenFile={handleOpenFile} />}
              agentPanel={(
                <AgentPanel
                  autonomous={autonomous}
                  autoAgents={autoAgents}
                  activeProfile={activeProfile}
                  streaming={streaming}
                  onAutonomousChange={handleToggleAutonomous}
                  onAutoAgentsChange={handleToggleAutoAgents}
                  onOpenSettings={() => setShowSettings(true)}
                  onClearChat={handleClearChat}
                  onSettingsChanged={() => setStatsVersion(v => v + 1)}
                  activity={activity}
                  agentMode={agentMode}
                  doctorRunRequest={doctorRunRequest}
                  taskRunVersion={taskRunVersion}
                  skillSuggestion={skillSuggestion}
                  onDismissSkillSuggestion={() => setSkillSuggestion(null)}
                />
              )}
            />
          ) : (
            <button className="collapsed-rail collapsed-rail-right" onClick={() => setRightOpen(true)} title="Open Inspector panel">
              <span>Inspector</span>
            </button>
          )}
        </div>
      </div>

      {/* Terminal resize handle — only visible when panel is open */}
      {showTerminal && (
        <div
          className="terminal-resize-handle"
          role="separator"
          aria-label="Resize bottom panel"
          aria-orientation="horizontal"
          tabIndex={0}
          title="Drag or use arrow keys to resize; double-click to collapse bottom panel"
          onMouseDown={e => {
            const startY = e.clientY
            const startH = terminalHeight
            let nextH = terminalHeight
            const onMove = (mv: MouseEvent) => {
              const dy = startY - mv.clientY
              nextH = Math.min(600, Math.max(100, startH + dy))
              setTerminalHeight(nextH)
            }
            const onUp = () => {
              window.removeEventListener('mousemove', onMove)
              window.removeEventListener('mouseup', onUp)
              void persistTerminalHeight(nextH)
            }
            window.addEventListener('mousemove', onMove)
            window.addEventListener('mouseup', onUp)
            e.preventDefault()
          }}
          onKeyDown={e => {
            const amount = e.shiftKey ? 48 : 16
            const next = e.key === 'ArrowUp'
              ? Math.min(600, terminalHeight + amount)
              : e.key === 'ArrowDown'
                ? Math.max(100, terminalHeight - amount)
                : e.key === 'Home' ? 260 : null
            if (next == null) return
            e.preventDefault()
            setTerminalHeight(next)
            void persistTerminalHeight(next)
          }}
          onDoubleClick={() => {
            setShowTerminal(false)
          }}
        />
      )}
      {/* Terminal panel — always mounted so session/output survive toggle */}
      <div
        className={`terminal-panel ${streaming ? 'terminal-panel-live' : ''}`}
        style={{ height: showTerminal ? terminalHeight : 0, display: showTerminal ? 'flex' : 'none' }}
      >
        <div className="bottom-panel-tabs">
          <button
            type="button"
            className={bottomTab === 'terminal' ? 'active' : ''}
            onClick={() => setBottomTab('terminal')}
          >
            Terminal
          </button>
          <button
            type="button"
            className={bottomTab === 'stream' ? 'active' : ''}
            onClick={() => setBottomTab('stream')}
          >
            Stream
          </button>
          <button
            type="button"
            className={bottomTab === 'jobs' ? 'active' : ''}
            onClick={() => setBottomTab('jobs')}
          >
            Jobs {backgroundJobs.length > 0 ? <span className="bottom-tab-count">{backgroundJobs.length}</span> : null}
          </button>
          <span className="bottom-panel-state">
            {runState?.state ? runState.state.replaceAll('_', ' ') : streaming ? 'running' : 'idle'}
          </span>
        </div>
        <div className="bottom-panel-body">
          <TerminalPane key={`terminal-${workspaceVersion}`} visible={showTerminal && bottomTab === 'terminal'} />
          <StreamPane
            visible={showTerminal && bottomTab === 'stream'}
            streaming={streaming}
            streamBuffer={streamBuffer}
            thinkingBuffer={thinkingBuffer}
            runState={runState}
            activity={activity}
            toolCountdown={showToolCountdown ? toolCountdown : null}
            onStopAgent={() => void StopAgent()}
          />
          <JobsPane
            visible={showTerminal && bottomTab === 'jobs'}
            jobs={backgroundJobs}
            onClearCompleted={() => setBackgroundJobs(prev => prev.filter(job => job.state !== 'done'))}
            onClearAll={() => setBackgroundJobs([])}
          />
        </div>
      </div>

      <StatusBar statsVersion={statsVersion} runState={runState} onProfileChanged={() => { void refreshProfiles(); setStatsVersion(v => v + 1) }} />

      <ToastContainer
        toasts={toasts}
        onDismiss={id => setToasts(prev => prev.filter(t => t.id !== id))}
      />

      {confirm && (
        <ConfirmDialog
          payload={confirm}
          onAllow={() => handleConfirmRespond(true)}
          onAllowRemember={() => handleConfirmRespond(true, true)}
          onDeny={() => handleConfirmRespond(false)}
        />
      )}

      {confirmAction && (
        <ConfirmDialog
          title={confirmAction.title}
          message={confirmAction.message}
          confirmLabel={confirmAction.confirmLabel}
          cancelLabel={confirmAction.cancelLabel ?? 'Cancel'}
          danger={confirmAction.danger ?? true}
          onAllow={async () => {
            const action = confirmAction
            setConfirmAction(null)
            await action.onConfirm()
          }}
          onDeny={() => setConfirmAction(null)}
        />
      )}

      {showSaveSession && (
        <div className="overlay">
          <div className="confirm-dialog">
            <div className="confirm-header">
              <span className="confirm-title">Save Session</span>
            </div>
            <div className="save-session-form">
              <label htmlFor="save-session-name">Session name</label>
              <input
                id="save-session-name"
                value={saveSessionDraft}
                onChange={e => setSaveSessionDraft(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') void submitSaveSession()
                  if (e.key === 'Escape') setShowSaveSession(false)
                }}
                autoFocus
              />
            </div>
            <div className="confirm-actions">
              <button onClick={() => setShowSaveSession(false)}>Cancel</button>
              <button className="primary" onClick={() => void submitSaveSession()} disabled={!saveSessionDraft.trim()}>
                Save
              </button>
            </div>
          </div>
        </div>
      )}

      {showSettings && (
        <SettingsModal onClose={() => setShowSettings(false)} onSaved={() => { void refreshProfiles(); setStatsVersion(v => v + 1) }} />
      )}
    </div>
  )
}
