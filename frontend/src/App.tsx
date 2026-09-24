import { useState, useEffect, useCallback, useRef, type CSSProperties, type MouseEvent as ReactMouseEvent } from 'react'
import { EventsOn } from './wailsjs/runtime'
import { FileTree } from './components/FileTree'
import { HermesSidebar } from './components/HermesSidebar'
import { ChatPane } from './components/ChatPane'
import { ConversationMenu } from './components/ConversationMenu'
import { ConversationCheckpointDialog } from './components/ConversationCheckpointDialog'
import { LayoutMenu } from './components/LayoutMenu'
import { UiIcon } from './components/UiIcon'
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
  CreateScratchWorkspace,
  CreateWorkspaceProject,
  DeleteSession,
  DeleteResumableRun,
  GetScratchWorkspaceStatus,
  InspectSessionRepair,
  ListSessionSummaries,
  ListResumableRuns,
  LoadSession,
  RenameSession,
  RepairSession,
  ResumeRun,
  PromoteScratchWorkspace,
  ResumeBrowserWorkflow,
  RespondConfirm,
  SaveSession,
  StartConversation,
  SaveConversationCheckpoint,
  SetSessionTags,
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
  GetConversationMode,
  ListTodos,
  SendMessage,
  SendMessageWithProfile,
  SetConversationMode,
  SetSavedConversationMode,
  UpdateSettings,
  type ChatAttachment,
  StopAgent,
  StopBrowserWorkflow,
  type ChatRole,
  type ProfilesFile,
  type SessionChatMessage,
  type SessionSummary,
  type SessionRepairReport,
  type RunCheckpoint,
  type ScratchWorkspaceStatus,
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

function automaticConversationTitle(text: string, images: string[], attachments: ChatAttachment[]): string {
  const fallback = attachments[0]?.name || (images.length > 0 ? 'Image chat' : 'New chat')
  const compact = (text.trim() || fallback).replace(/\s+/g, ' ')
  const words = compact.split(' ').slice(0, 8).join(' ')
  return (words || fallback).slice(0, 72)
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
  runId?: string
  modelTurn?: number
  toolName?: string
  toolCallId?: string
  category?: 'reply' | 'tool' | 'status' | 'browser'
}

type InspectorFocus = 'agent' | 'workspace' | 'facts' | 'commands' | 'activity'

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

function titlebarRunSummary(runState: RunStatePayload | null): string {
  const detail = String(runState?.detail || '').trim()
  if (detail.includes('tool_choice=')) {
    const choice = detail.match(/tool_choice=([^\s]+)/)?.[1] || 'auto'
    const toolCount = Number(detail.match(/tools=(\d+)/)?.[1] || 0)
    const effort = detail.match(/effort=([^\s]+)/)?.[1]
    const noThinking = detail.includes('no_think=true')
    if (choice === 'none' && noThinking) return 'Direct answer'
    const route = choice === 'none'
      ? 'Text-only turn'
      : toolCount === 1
        ? '1 tool available'
        : `${toolCount} tools available`
    return effort ? `${route} · ${effort[0].toUpperCase()}${effort.slice(1)} effort` : route
  }
  if (detail) return detail
  const state = String(runState?.state || 'working').replaceAll('_', ' ')
  return `${state[0].toUpperCase()}${state.slice(1)}`
}

export interface BrowserHandoffPayload {
  active: boolean
  action: string
  state: string
  url?: string
  title?: string
  guidance?: string
}

interface RunEventOwner {
  run_id: string
  generation: number
  conversation_epoch?: number
  origin: string
}

function runEventOwner(args: unknown[]): RunEventOwner | null {
  for (let i = args.length - 1; i >= 0; i -= 1) {
    const value = args[i]
    if (!value || typeof value !== 'object') continue
    const candidate = value as { run_id?: unknown; generation?: unknown; conversation_epoch?: unknown; origin?: unknown }
    const runID = String(candidate.run_id || '').trim()
    const generation = Number(candidate.generation)
    if (runID && Number.isSafeInteger(generation) && generation > 0) {
      const conversationEpoch = Number(candidate.conversation_epoch)
      return {
        run_id: runID,
        generation,
        conversation_epoch: Number.isSafeInteger(conversationEpoch) && conversationEpoch > 0 ? conversationEpoch : undefined,
        origin: String(candidate.origin || 'desktop').trim().toLowerCase(),
      }
    }
  }
  return null
}

function eventBelongsToActiveRun(args: unknown[], active: RunEventOwner | null): boolean {
  const owner = runEventOwner(args)
  if (!owner) return true // compatibility with events from an older running binary
  return Boolean(active
    && active.run_id === owner.run_id
    && active.generation === owner.generation
    && (!owner.conversation_epoch || !active.conversation_epoch || active.conversation_epoch === owner.conversation_epoch))
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

const LEFT_PANE_DEFAULT = 272
const LEFT_PANE_MIN = 180
const LEFT_PANE_MAX = 520
const RIGHT_PANE_DEFAULT = 460
const RIGHT_PANE_MIN = 360
const RIGHT_PANE_MAX = 620
const TERMINAL_HEIGHT_DEFAULT = 320
const TERMINAL_HEIGHT_MIN = 280
const TERMINAL_HEIGHT_MAX = 600

function clampTerminalHeight(value: number) {
  const viewportMax = typeof window === 'undefined'
    ? TERMINAL_HEIGHT_MAX
    : Math.max(TERMINAL_HEIGHT_MIN, window.innerHeight - 230)
  return Math.min(TERMINAL_HEIGHT_MAX, viewportMax, Math.max(TERMINAL_HEIGHT_MIN, value))
}

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

const workbenchPageLabels: Record<string, string> = {
  projects: 'Workspaces', chat: 'Chat', ops: 'Run activity', engagement: 'Engagement grid',
  services: 'Services', file: 'Editor', logs: 'Logs', memory: 'Memory', brain: 'Brain',
  context: 'Context inspector', telegram: 'Telegram', benchmarks: 'Model lab', doctor: 'Doctor',
}

export default function App() {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [streaming, setStreaming] = useState(false)
  const [streamBuffer, setStreamBuffer] = useState('')
  const answerCheckpointRef = useRef('')
  const streamErrorHandledRef = useRef(false)
  const activeRunOwnerRef = useRef<RunEventOwner | null>(null)
  const retiredRunGenerationRef = useRef(0)
  const [confirm, setConfirm] = useState<ConfirmPayload | null>(null)
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(null)
  const [showSaveSession, setShowSaveSession] = useState(false)
  const [saveSessionDraft, setSaveSessionDraft] = useState('')
  const [showRenameSession, setShowRenameSession] = useState(false)
  const [renameSessionSource, setRenameSessionSource] = useState('')
  const [renameSessionDraft, setRenameSessionDraft] = useState('')
  const [showSessionTags, setShowSessionTags] = useState(false)
  const [tagSessionName, setTagSessionName] = useState('')
  const [sessionTagsDraft, setSessionTagsDraft] = useState('')
  const [sessionRepairReport, setSessionRepairReport] = useState<SessionRepairReport | null>(null)
  const [sessionRepairBusy, setSessionRepairBusy] = useState(false)
  const [runCheckpoints, setRunCheckpoints] = useState<RunCheckpoint[]>([])
  const [showConversationCheckpoints, setShowConversationCheckpoints] = useState(false)
  const [checkpointDefaultName, setCheckpointDefaultName] = useState('')
  const [checkpointBusy, setCheckpointBusy] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [statsVersion, setStatsVersion] = useState(0)
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [selectedSession, setSelectedSession] = useState('')
  const [conversationMode, setConversationModeState] = useState<'adaptive' | 'direct' | 'agent'>('adaptive')
  const [scratchWorkspace, setScratchWorkspace] = useState<ScratchWorkspaceStatus | null>(null)
  const [activeProfile, setActiveProfile] = useState('')
  const [activeModelID, setActiveModelID] = useState('')
  const [thinkingSupported, setThinkingSupported] = useState(false)
  const [thinkingMode, setThinkingMode] = useState('auto')
  const [reasoningEffort, setReasoningEffort] = useState('auto')
  const [workspaceRootLabel, setWorkspaceRootLabel] = useState('')
  const [cloudRunProfiles, setCloudRunProfiles] = useState<RunProfileOption[]>([])
  const [runProfileOverride, setRunProfileOverride] = useState('')
  const [activeRunProfile, setActiveRunProfile] = useState('')
  const [autonomous, setAutonomousState] = useState(false)
  const [autoAgents, setAutoAgentsState] = useState(true)
  const [centerTab, setCenterTab] = useState<'chat' | 'ops' | 'projects' | 'engagement' | 'services' | 'file' | 'logs' | 'memory' | 'brain' | 'context' | 'telegram' | 'benchmarks' | 'doctor'>('chat')
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
  const [browserHandoff, setBrowserHandoff] = useState<BrowserHandoffPayload | null>(null)
  const [inspectorFocus, setInspectorFocus] = useState<InspectorFocus>('workspace')
  const [inspectorFocusRequest, setInspectorFocusRequest] = useState(0)
  const runMilestonesRef = useRef<Set<string>>(new Set())
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

  const handleReasoningControlChange = useCallback(async (nextThinkingMode: string, nextReasoningEffort: string) => {
    try {
      const current = await GetSettings()
      await UpdateSettings({
        ...current,
        agents: {
          ...current.agents,
          thinking_mode: nextThinkingMode,
          reasoning_effort: nextReasoningEffort,
        },
      })
      setThinkingMode(nextThinkingMode)
      setReasoningEffort(nextReasoningEffort)
      setStatsVersion(value => value + 1)
    } catch (error) {
      pushToast(`Could not update thinking controls: ${String(error)}`, 'danger')
      throw error
    }
  }, [pushToast])

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
  const [rightOpen, setRightOpen] = useState(() => loadStoredLayoutBoolean('mauler.layout.rightOpen', false))
  const [closedPanelRails, setClosedPanelRails] = useState(() => loadStoredLayoutBoolean('mauler.layout.closedPanelRails', false))
  const [leftWidth, setLeftWidth] = useState(() => loadStoredLayoutNumber('mauler.layout.leftWidth', LEFT_PANE_DEFAULT, LEFT_PANE_MIN, LEFT_PANE_MAX))
  const [rightWidth, setRightWidth] = useState(() => loadStoredLayoutNumber('mauler.layout.rightWidth', RIGHT_PANE_DEFAULT, RIGHT_PANE_MIN, RIGHT_PANE_MAX))
  const [thinkingBuffer, setThinkingBuffer] = useState('')
  const pendingThinkingRef = useRef('')
  const [showTerminal, setShowTerminal] = useState(false)
  const [aiCommandsOpen, setAICommandsOpen] = useState(() => loadStoredLayoutBoolean('mauler.aiCommandsVisible', false))
  const [autoOpenRunPanel, setAutoOpenRunPanel] = useState(() => loadStoredLayoutBoolean('mauler.layout.autoOpenRunPanel', false))
  const [terminalHeight, setTerminalHeight] = useState(TERMINAL_HEIGHT_DEFAULT)
  const [bottomTab, setBottomTab] = useState<'terminal' | 'stream' | 'jobs'>('terminal')
  const [skillSuggestion, setSkillSuggestion] = useState<SkillSuggestion | null>(null)
  const [workspaceVersion, setWorkspaceVersion] = useState(0)
  const [workspaceFilesVersion, setWorkspaceFilesVersion] = useState(0)
  const [toolCountdown, setToolCountdown] = useState<ToolCountdown | null>(null)
  const [showToolCountdown, setShowToolCountdown] = useState(false)
  const [backgroundJobs, setBackgroundJobs] = useState<BackgroundJob[]>([])
  const [todos, setTodos] = useState<TodoItem[]>([])
  const [chatDraftRequest, setChatDraftRequest] = useState<{ id: string; text: string } | null>(null)

	// One-time migration from the earlier cockpit layout. Existing dimensions
	// remain intact, but Chat starts with auxiliary work surfaces dismissed.
	useEffect(() => {
		const revision = 'chat-canvas-v3'
		if (localStorage.getItem('mauler.layout.revision') === revision) return
		setRightOpen(false)
		setShowTerminal(false)
		setAICommandsOpen(false)
		setClosedPanelRails(false)
		localStorage.setItem('mauler.layout.rightOpen', '0')
		localStorage.setItem('mauler.aiCommandsVisible', '0')
		localStorage.setItem('mauler.layout.closedPanelRails', '0')
		localStorage.setItem('mauler.layout.revision', revision)
	}, [])

	// Surface the automatic conversation library once when upgrading. After
	// this one-time introduction, the operator's open/closed choice persists.
	useEffect(() => {
		const revision = 'automatic-conversation-library-v1'
		if (localStorage.getItem('mauler.chat.libraryRevision') === revision) return
		setLeftOpen(true)
		localStorage.setItem('mauler.layout.leftOpen', '1')
		localStorage.setItem('mauler.chat.libraryRevision', revision)
	}, [])

  const retireActiveRunUI = useCallback(() => {
    const active = activeRunOwnerRef.current
    if (active) retiredRunGenerationRef.current = Math.max(retiredRunGenerationRef.current, active.generation)
    activeRunOwnerRef.current = null
    answerCheckpointRef.current = ''
    pendingThinkingRef.current = ''
    streamErrorHandledRef.current = false
    setStreaming(false)
    setActiveRunProfile('')
    setToolCountdown(null)
    setStreamBuffer('')
    setThinkingBuffer('')
    setBrowserHandoff(null)
  }, [])

  useEffect(() => {
    localStorage.setItem('mauler.layout.leftOpen', leftOpen ? '1' : '0')
  }, [leftOpen])

  useEffect(() => {
    localStorage.setItem('mauler.layout.rightOpen', rightOpen ? '1' : '0')
  }, [rightOpen])

  useEffect(() => {
    localStorage.setItem('mauler.layout.closedPanelRails', closedPanelRails ? '1' : '0')
  }, [closedPanelRails])

  useEffect(() => {
    localStorage.setItem('mauler.aiCommandsVisible', aiCommandsOpen ? '1' : '0')
  }, [aiCommandsOpen])

  useEffect(() => {
    const clampToViewport = () => setTerminalHeight(current => clampTerminalHeight(current))
    window.addEventListener('resize', clampToViewport)
    return () => window.removeEventListener('resize', clampToViewport)
  }, [])

  useEffect(() => {
    localStorage.setItem('mauler.layout.autoOpenRunPanel', autoOpenRunPanel ? '1' : '0')
  }, [autoOpenRunPanel])

  useEffect(() => {
    if (browserHandoff?.active) setInspectorFocus('activity')
    else if (centerTab === 'file' || centerTab === 'projects') setInspectorFocus('workspace')
    else if (centerTab === 'engagement') setInspectorFocus('facts')
    else if (centerTab === 'ops') setInspectorFocus('activity')
  }, [browserHandoff?.active, centerTab])

  const toggleTerminalPanel = useCallback(() => {
    if (showTerminal && bottomTab === 'terminal') {
      setShowTerminal(false)
      return
    }
    setBottomTab('terminal')
    setShowTerminal(true)
  }, [bottomTab, showTerminal])

  const showBottomTab = useCallback((tab: 'terminal' | 'stream' | 'jobs') => {
    setBottomTab(tab)
    setShowTerminal(true)
  }, [])

  const setInspectorVisible = useCallback((open: boolean) => {
    if (open) {
      // Recover layouts saved by older builds before mounting the drawer.
      const recovered = Math.min(RIGHT_PANE_MAX, Math.max(RIGHT_PANE_MIN, rightWidth || RIGHT_PANE_DEFAULT))
      if (recovered !== rightWidth) setRightWidth(recovered)
      storeLayoutNumber('mauler.layout.rightWidth', recovered)
      setInspectorFocusRequest(value => value + 1)
    }
    setRightOpen(open)
  }, [rightWidth])

  const toggleInspector = useCallback(() => {
    setInspectorVisible(!rightOpen)
  }, [rightOpen, setInspectorVisible])

  const focusChatLayout = useCallback(() => {
    setLeftOpen(false)
    setRightOpen(false)
    setShowTerminal(false)
    setClosedPanelRails(false)
  }, [])

  const restoreDefaultLayout = useCallback(() => {
    setLeftOpen(true)
    setRightOpen(false)
    setShowTerminal(false)
    setAICommandsOpen(false)
    setClosedPanelRails(false)
  }, [])

  const prepareEngagementRun = useCallback((prompt: string) => {
    setChatDraftRequest({ id: crypto.randomUUID(), text: prompt })
    setChatLane('project')
    setCenterTab('chat')
  }, [])

  const refreshTodos = useCallback(async () => {
    try {
      const loaded = await ListTodos()
      setTodos(Array.isArray(loaded) ? loaded : [])
    } catch {
      setTodos([])
    }
  }, [])

  const refreshSessions = useCallback(async () => {
    const summaries = await ListSessionSummaries().catch(() => [] as SessionSummary[])
    const names = summaries.map(summary => summary.name)
    setSessions(summaries)
    // A saved row is not the active transcript until the operator opens it.
    // Starting the app therefore always presents an honest blank New chat.
    setSelectedSession(previous => previous && names.includes(previous) ? previous : '')
  }, [])

  const handleClearPlan = useCallback(async () => {
    if (streaming) {
      pushToast('Stop the active run before clearing its plan.', 'warn')
      return
    }
    try {
      await ClearTodos()
      await refreshTodos()
      setTaskRunVersion(value => value + 1)
      pushToast('Active plan cleared.', 'success')
    } catch (error) {
      pushToast(`Could not clear the active plan: ${String(error)}`, 'danger')
      throw error
    }
  }, [pushToast, refreshTodos, streaming])

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
        const owner = runEventOwner(args)
        if (owner) {
          if (owner.origin !== 'desktop') return
          if (owner.generation <= retiredRunGenerationRef.current) return
          const active = activeRunOwnerRef.current
          if (active && owner.generation < active.generation) return
          activeRunOwnerRef.current = owner
        }
        setStreaming(true)
        setActiveRunProfile(String(args[0] || ''))
        setRunStartedAt(Date.now())
        setRunState({ state: 'starting', detail: 'Preparing request' })
        setStreamBuffer('')
        setThinkingBuffer('')
        setBrowserHandoff(null)
        answerCheckpointRef.current = ''
        streamErrorHandledRef.current = false
        runMilestonesRef.current.clear()
		if (autoOpenRunPanel) setShowTerminal(true)
		// Opening live activity must not discard the height the user chose with
		// the visible terminal splitter. The previous 42%-of-window expansion
		// produced a huge empty terminal for short commands.
        pendingThinkingRef.current = ''
      }),
      EventsOn('mauler:budget_updated', () => {
        setStatsVersion(v => v + 1)
      }),
      EventsOn('mauler:thinking', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const chunk = args[0] as string
        pendingThinkingRef.current += chunk
        setThinkingBuffer(prev => prev + chunk)
      }),
      EventsOn('mauler:thinking_done', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        // Store final thinking with the next assistant message
        pendingThinkingRef.current = args[0] as string
      }),
      EventsOn('mauler:delta', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const chunk = args[0] as string
        setStreamBuffer(prev => prev + chunk)
      }),
      EventsOn('mauler:stream_replace', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        setStreamBuffer(args[0] as string)
      }),
      EventsOn('mauler:answer_checkpoint', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const candidate = cleanAssistantTranscriptText(String(args[0] || ''))
        if (candidate.length > answerCheckpointRef.current.length) {
          answerCheckpointRef.current = candidate
        }
      }),
      EventsOn('mauler:assistant_turn', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const payload = (args[0] || {}) as { content?: unknown; thinking?: unknown; turn?: unknown; tool_calls?: unknown }
        const content = cleanAssistantTranscriptText(String(payload.content || ''))
        if (!content) return
        const owner = runEventOwner(args)
        const modelTurn = Number(payload.turn)
        const toolCallCount = Number(payload.tool_calls)
        const id = owner && Number.isSafeInteger(modelTurn) && modelTurn > 0
          ? `${owner.run_id}:assistant:${modelTurn}`
          : crypto.randomUUID()
        setMessages(current => {
          if (current.some(message => message.id === id)) return current
          return [...current, {
            id,
            role: 'assistant',
            content,
            thinking: String(payload.thinking || '').trim() || undefined,
            timestamp: Date.now(),
            runId: owner?.run_id,
            modelTurn: Number.isSafeInteger(modelTurn) ? modelTurn : undefined,
            category: Number.isFinite(toolCallCount) && toolCallCount > 0 ? 'status' : 'reply',
          }]
        })
        setStreamBuffer('')
        setThinkingBuffer('')
        pendingThinkingRef.current = ''
      }),
      EventsOn('mauler:tool_protocol_repair', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        setStreamBuffer('')
      }),
      EventsOn('mauler:stream_done', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const owner = runEventOwner(args)
        setStreaming(false)
        setActiveRunProfile('')
        setToolCountdown(null)
        setBrowserHandoff(null)
        if (streamErrorHandledRef.current) {
          streamErrorHandledRef.current = false
          answerCheckpointRef.current = ''
          setStreamBuffer('')
          void refreshSessions()
          if (owner) {
            retiredRunGenerationRef.current = Math.max(retiredRunGenerationRef.current, owner.generation)
            activeRunOwnerRef.current = null
          }
          return
        }
        const terminalAnswer = cleanAssistantTranscriptText(String(args[0] || ''))
        const terminalStatus = String(args[1] || 'done')
        const stopReason = String(args[2] || '')
        const answerDelivered = terminalStatus === 'done' || terminalStatus === 'recovered'
        if (terminalStatus === 'recovered') {
          setRunState({ state: 'recovered', detail: 'Answer delivered from gathered evidence after redundant work was stopped.' })
        }
        setStreamBuffer(prev => {
          const current = cleanAssistantTranscriptText(prev)
          const checkpoint = cleanAssistantTranscriptText(answerCheckpointRef.current)
          const visible = answerDelivered
            ? (terminalAnswer || current || checkpoint)
            : (checkpoint || terminalAnswer || current)
          answerCheckpointRef.current = ''
          if (visible) {
            const thinking = pendingThinkingRef.current || undefined
            pendingThinkingRef.current = ''
            setMessages(m => {
              const sameTurnAlreadyVisible = [...m].reverse().some(message =>
                message.role === 'assistant' &&
                (!owner || message.runId === owner.run_id) &&
                cleanAssistantTranscriptText(message.content) === visible
              )
              return [
                ...m,
                ...(!sameTurnAlreadyVisible ? [{
                id: crypto.randomUUID(),
                role: 'assistant' as const,
                content: visible,
                thinking,
                timestamp: Date.now(),
                runId: owner?.run_id,
              }] : []),
              ...(!answerDelivered && stopReason ? [{
                id: crypto.randomUUID(),
                role: 'system' as const,
                content: `The answer above was preserved, but run finalisation stopped: ${stopReason}`,
                timestamp: Date.now(),
              }] : []),
              ]
            })
          } else {
            pendingThinkingRef.current = ''
          }
          return ''
        })
        setStatsVersion(v => v + 1)
        void refreshSessions()
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
        if (owner) {
          retiredRunGenerationRef.current = Math.max(retiredRunGenerationRef.current, owner.generation)
          activeRunOwnerRef.current = null
        }
      }),
      EventsOn('mauler:stream_error', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const err = args[0] as string
        streamErrorHandledRef.current = true
        setStreaming(false)
        setActiveRunProfile('')
        setToolCountdown(null)
        setBrowserHandoff(null)
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
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const tc = args[0] as { id: string; name: string; input: string; timeout?: string }
        const nextItem: AgentActivity = {
          id: tc.id,
          name: tc.name,
          status: 'running',
          input: tc.input,
          startTime: Date.now(),
        }
        setActivity(items => [nextItem, ...items].slice(0, 12))
        const owner = runEventOwner(args)
        setMessages(items => items.some(item => item.id === `${owner?.run_id || 'run'}:tool-call:${tc.id}`) ? items : [...items, {
          id: `${owner?.run_id || 'run'}:tool-call:${tc.id}`,
          role: 'tool_call',
          content: tc.input,
          toolName: tc.name,
          toolCallId: tc.id,
          runId: owner?.run_id,
          timestamp: Date.now(),
        }])
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
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const tr = args[0] as { id: string; name: string; result: string }
        setToolCountdown(prev => prev?.id === tr.id ? null : prev)
        const owner = runEventOwner(args)
        setMessages(items => items.some(item => item.id === `${owner?.run_id || 'run'}:tool-result:${tr.id}`) ? items : [...items, {
          id: `${owner?.run_id || 'run'}:tool-result:${tr.id}`,
          role: 'tool_result',
          content: tr.result,
          toolName: tr.name,
          toolCallId: tr.id,
          runId: owner?.run_id,
          timestamp: Date.now(),
        }])
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
		if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
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
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        setConfirm(args[0] as ConfirmPayload)
      }),
      EventsOn('mauler:compact', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
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
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const payload = args[0] as RunStatePayload
        if (payload?.state) {
          setRunState(payload)
          const state = String(payload.state).toLowerCase()
          if (['recovering', 'blocked', 'recovered', 'failed'].includes(state)) {
            const owner = runEventOwner(args)
            const milestoneKey = `${owner?.run_id || 'run'}:${state}`
            if (!runMilestonesRef.current.has(milestoneKey)) {
              runMilestonesRef.current.add(milestoneKey)
              setMessages(items => [...items, {
                id: `${milestoneKey}:status`,
                role: 'system',
                category: 'status',
                content: `${state[0].toUpperCase()}${state.slice(1)}${payload.detail ? ` — ${payload.detail}` : ''}`,
                runId: owner?.run_id,
                timestamp: Date.now(),
              }])
            }
          }
        }
      }),
      EventsOn('mauler:browser_handoff', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        const raw = (args[0] || {}) as Record<string, unknown>
        const active = raw.active === true || String(raw.active || '').toLowerCase() === 'true'
        const owner = runEventOwner(args)
        if (!active) {
          setBrowserHandoff(null)
          const id = `${owner?.run_id || 'run'}:browser:resumed`
          setMessages(items => items.some(item => item.id === id) ? items : [...items, {
            id,
            role: 'system',
            category: 'browser',
            content: 'Browser takeover completed — the same run resumed from a fresh page observation.',
            runId: owner?.run_id,
            timestamp: Date.now(),
          }])
          return
        }
        const handoff = {
          active: true,
          action: String(raw.action || 'takeover'),
          state: String(raw.state || 'waiting_user'),
          url: String(raw.url || ''),
          title: String(raw.title || ''),
          guidance: String(raw.guidance || ''),
        }
        setBrowserHandoff(handoff)
        setInspectorFocus('activity')
        const id = `${owner?.run_id || 'run'}:browser:waiting`
        setMessages(items => items.some(item => item.id === id) ? items : [...items, {
          id,
          role: 'system',
          category: 'browser',
          content: `Browser waiting for you${handoff.title ? ` — ${handoff.title}` : ''}${handoff.url ? `\n${handoff.url}` : ''}`,
          runId: owner?.run_id,
          timestamp: Date.now(),
        }])
      }),
      EventsOn('mauler:task_run', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        setTaskRunVersion(v => v + 1)
        refreshTodos()
      }),
      EventsOn('mauler:engagement_changed', () => {
        setTaskRunVersion(v => v + 1)
      }),
      EventsOn('mauler:workspace_changed', () => {
        retireActiveRunUI()
        setMessages([])
        setStreamBuffer('')
        setThinkingBuffer('')
        setRunProfileOverride('')
        setActiveRunProfile('')
        setSelectedSession('')
        setScratchWorkspace(null)
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
      EventsOn('mauler:workspace_files_changed', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
        // Artifacts created by tools should refresh Explorer/Inspector only.
        // They are not workspace switches and must never clear Chat, its draft,
        // the active stream, or the shared terminal while a run is working.
        setWorkspaceFilesVersion(v => v + 1)
      }),
      EventsOn('mauler:workspace_attached', () => {
        // Explicit scratch attachment changes the tool root but belongs to the
        // same conversation. Preserve the transcript while refreshing all
        // path-backed workspace surfaces.
        setOpenFiles(files => files.filter(file => !file.path))
        setActiveFileIdx(0)
        setWorkspaceVersion(v => v + 1)
        setStatsVersion(v => v + 1)
        refreshTodos()
        void GetScratchWorkspaceStatus().then(status => setScratchWorkspace(status.active ? status : null)).catch(() => {})
      }),
      EventsOn('mauler:scratch_workspace', (...args: unknown[]) => {
        const status = args[0] as ScratchWorkspaceStatus
        setScratchWorkspace(status?.active ? status : null)
        setStatsVersion(v => v + 1)
      }),
      EventsOn('mauler:suggest_learning', (...args: unknown[]) => {
        if (!eventBelongsToActiveRun(args, activeRunOwnerRef.current)) return
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
  }, [autoOpenRunPanel, refreshSessions, refreshTodos, retireActiveRunUI])

  useEffect(() => {
    refreshTodos()
  }, [refreshTodos, taskRunVersion])

  const refreshRunCheckpoints = useCallback(async () => {
    const checkpoints = await ListResumableRuns().catch(() => [] as RunCheckpoint[])
    setRunCheckpoints(Array.isArray(checkpoints) ? checkpoints : [])
  }, [])

  useEffect(() => {
    void refreshSessions()
    void refreshRunCheckpoints()
  }, [refreshRunCheckpoints, refreshSessions])

  const refreshProfiles = useCallback(async () => {
    const [settings, profilesFile, auto, autoAgentEnabled, mode, definitions, scratch, savedConversationMode] = await Promise.all([
      GetSettings().catch(() => null),
      GetProfiles().catch(() => null),
      GetAutonomous().catch(() => false),
      GetAutoAgents().catch(() => true),
      GetAgentMode().catch(() => 'Auto'),
      ListAgentDefinitions().catch(() => [] as AgentDefinition[]),
      GetScratchWorkspaceStatus().catch(() => null),
      GetConversationMode().catch(() => 'adaptive'),
    ])
    if (settings) {
      setActiveProfile(settings.active_profile)
      const selectedProfile = profilesFile?.profiles?.[settings.active_profile]
      const modelID = selectedProfile?.model_id || settings.active_profile || ''
      setActiveModelID(modelID)
      setThinkingSupported(Boolean(selectedProfile?.thinking) || /qwen3[._-]?(?:5|6|8)/i.test(modelID))
      setThinkingMode(settings.agents.thinking_mode || 'auto')
      setReasoningEffort(settings.agents.reasoning_effort || 'auto')
      setWorkspaceRootLabel(settings.context.workspace_dir || '')
      setShowToolCountdown(settings.ui.tool_countdown ?? false)
      setAgentSelection(settings.agents.mode_override || 'Auto')
      if (!appliedInitialUI.current) {
        appliedInitialUI.current = true
        setShowTerminal(settings.ui.terminal_default_open ?? false)
        setTerminalHeight(clampTerminalHeight(settings.ui.terminal_height || TERMINAL_HEIGHT_DEFAULT))
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
    setScratchWorkspace(scratch?.active ? scratch : null)
    setConversationModeState(savedConversationMode === 'direct' || savedConversationMode === 'agent' ? savedConversationMode : 'adaptive')
  }, [])

  const handleConversationModeChange = useCallback(async (nextMode: 'adaptive' | 'direct' | 'agent') => {
    if (streaming) {
      pushToast('Stop the active run before changing this conversation mode.', 'warn')
      return
    }
    try {
      await SetConversationMode(selectedSession, nextMode)
      setConversationModeState(nextMode)
      await refreshSessions()
      const label = nextMode === 'direct' ? 'Direct' : nextMode === 'agent' ? 'Agent' : 'Adaptive'
      pushToast(`${label} mode selected for this conversation.`, 'success')
    } catch (error) {
      pushToast(`Could not change conversation mode: ${String(error)}`, 'danger')
    }
  }, [pushToast, refreshSessions, selectedSession, streaming])

  const handleSavedConversationModeChange = useCallback(async (name: string, nextMode: 'adaptive' | 'direct' | 'agent') => {
    if (streaming) {
      pushToast('Stop the active run before changing a conversation mode.', 'warn')
      return
    }
    try {
      if (name === selectedSession) {
        await SetConversationMode(name, nextMode)
        setConversationModeState(nextMode)
      } else {
        await SetSavedConversationMode(name, nextMode)
      }
      await refreshSessions()
      const label = nextMode === 'direct' ? 'Direct' : nextMode === 'agent' ? 'Agent' : 'Adaptive'
      pushToast(`${name} will use ${label} mode.`, 'success')
    } catch (error) {
      pushToast(`Could not change conversation mode: ${String(error)}`, 'danger')
    }
  }, [pushToast, refreshSessions, selectedSession, streaming])

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
      if (e.key.toLowerCase() === 'b' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        if (e.shiftKey) toggleInspector()
        else setLeftOpen(value => !value)
      }
      if (e.key.toLowerCase() === 'j' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        setShowTerminal(value => !value)
      }
      if (e.key === 'Escape') {
        setShowSettings(false)
        setConfirm(null)
        setConfirmAction(null)
        setShowSaveSession(false)
      }
      if (e.key.toLowerCase() === 'k' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        if (streaming) {
          pushToast('Stop the active run before clearing this conversation.', 'warn')
          return
        }
        setConfirmAction({
          title: 'Clear Chat',
          message: 'Clear chat history?',
          confirmLabel: 'Clear',
          onConfirm: async () => {
            await ClearHistory()
            const resetMode = await GetConversationMode().catch(() => 'adaptive')
            retireActiveRunUI()
            setMessages([])
            setStreamBuffer('')
            setConversationModeState(resetMode === 'direct' || resetMode === 'agent' ? resetMode : 'adaptive')
            setStatsVersion(v => v + 1)
          },
        })
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [pushToast, retireActiveRunUI, streaming, toggleInspector, toggleTerminalPanel])

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
    if (!selectedSession) {
      try {
        const name = await StartConversation(automaticConversationTitle(text, images, attachments))
        setSelectedSession(name)
      } catch (error) {
        setMessages(current => [...current, {
          id: crypto.randomUUID(),
          role: 'system',
          content: `Could not start a new conversation: ${error}`,
          timestamp: Date.now(),
        }])
        return
      }
    }
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
  }, [handleUserMessage, selectedSession])

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

  const handleCreateScratchWorkspace = useCallback(async () => {
    if (streaming) {
      pushToast('Stop the active run before attaching a scratch workspace.', 'warn')
      return
    }
    try {
      const status = await CreateScratchWorkspace(selectedSession || 'Scratch chat')
      setScratchWorkspace(status)
      setInspectorFocus('workspace')
      pushToast('Scratch workspace attached. This conversation was preserved and files will never be deleted automatically.', 'success')
    } catch (error) {
      pushToast(`Could not create scratch workspace: ${String(error)}`, 'danger')
    }
  }, [pushToast, selectedSession, streaming])

  const handleCreateWorkspaceProject = useCallback(async (name: string, bugBounty: boolean) => {
    if (streaming) {
      pushToast('Stop the active run before creating a project.', 'warn')
      return
    }
    try {
      const created = await CreateWorkspaceProject(name, '')
      if (!created) return
      if (bugBounty) {
        await SetAutoAgents(true)
        await SetAgentModeOverride('Bug Bounty Hunter')
        setAutoAgentsState(true)
        setAgentSelection('Bug Bounty Hunter')
        setAgentMode('Bug Bounty Hunter')
      }
      setScratchWorkspace(null)
      setStatsVersion(v => v + 1)
      pushToast(`Project created and opened: ${created}`, 'success')
    } catch (error) {
      pushToast(`Could not create project: ${String(error)}`, 'danger')
    }
  }, [pushToast, streaming])

  const handlePromoteScratchWorkspace = useCallback(async (name: string) => {
    if (streaming) {
      pushToast('Stop the active run before promoting this workspace.', 'warn')
      return
    }
    try {
      const promoted = await PromoteScratchWorkspace(name)
      setScratchWorkspace(null)
      setStatsVersion(v => v + 1)
      pushToast(`${promoted.name || name || 'Workspace'} is now a durable Workspace; its files stayed in place.`, 'success')
    } catch (error) {
      pushToast(`Could not promote scratch workspace: ${String(error)}`, 'danger')
    }
  }, [pushToast, streaming])

  const mapSessionMessages = (loaded: SessionChatMessage[]): ChatMessage[] =>
    loaded
      .filter(m => !(m.role === 'system' && m.content.trimStart().startsWith('[Context compacted]')))
      .map(m => ({
      id: crypto.randomUUID(),
      role: m.role,
      content: m.content,
      thinking: m.thinking,
      toolName: m.tool_name,
      toolCallId: m.tool_call_id,
      category: m.role === 'tool_call' || m.role === 'tool_result' ? 'tool' : m.role === 'system' ? 'status' : 'reply',
      images: m.images ?? [],
      attachments: m.attachments ?? [],
      timestamp: Date.now(),
      }))

  const cleanSessionName = (name: string) =>
    name.trim().replace(/[^A-Za-z0-9._-]+/g, '-').replace(/^[._-]+|[._-]+$/g, '')

  const handleOpenConversationCheckpoints = useCallback(async () => {
    const stamp = new Date().toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
    setCheckpointDefaultName(`${selectedSession || 'Chat'} · ${stamp}`.slice(0, 80))
    await refreshRunCheckpoints()
    setShowConversationCheckpoints(true)
  }, [refreshRunCheckpoints, selectedSession])

  const handleCreateConversationCheckpoint = useCallback(async (name: string) => {
    if (streaming) {
      pushToast('Stop the active run before creating a checkpoint.', 'warn')
      return false
    }
    setCheckpointBusy(true)
    try {
      await SaveConversationCheckpoint(name, selectedSession)
      await refreshRunCheckpoints()
      pushToast(`Created checkpoint “${name.trim()}”.`, 'success')
      return true
    } catch (error) {
      pushToast(`Could not create checkpoint: ${String(error)}`, 'danger')
      return false
    } finally {
      setCheckpointBusy(false)
    }
  }, [pushToast, refreshRunCheckpoints, selectedSession, streaming])

  const handleResumeConversationCheckpoint = useCallback(async (checkpoint: RunCheckpoint) => {
    if (streaming) {
      pushToast('Stop the active run before resuming a checkpoint.', 'warn')
      return false
    }
    const previousMessages = messages
    const previousSelected = selectedSession
    const previousMode = conversationMode
    const mode = checkpoint.conversation_mode === 'direct' || checkpoint.conversation_mode === 'agent' ? checkpoint.conversation_mode : 'adaptive'
    setCheckpointBusy(true)
    retireActiveRunUI()
    setSelectedSession('')
    const restoredMessages = mapSessionMessages(checkpoint.chat_messages || [])
    if (checkpoint.explicit) {
      const instruction = `Resume the saved conversation from its captured state. Reconcile the existing evidence first and do not repeat completed work unless verification requires it.${checkpoint.prompt?.trim() ? `\n\nOriginal task: ${checkpoint.prompt.trim()}` : ''}`
      restoredMessages.push({ id: crypto.randomUUID(), role: 'user', content: instruction, images: [], attachments: [], timestamp: Date.now(), category: 'reply' })
    }
    setMessages(restoredMessages)
    setConversationModeState(mode)
    setStreamBuffer('')
    setCenterTab('chat')
    try {
      await ResumeRun(checkpoint.run_id)
      setShowConversationCheckpoints(false)
      setStatsVersion(value => value + 1)
      pushToast(`Resumed “${checkpoint.name || 'automatic recovery'}” as a new linked run.`, 'success')
      return true
    } catch (error) {
      setMessages(previousMessages)
      setSelectedSession(previousSelected)
      setConversationModeState(previousMode)
      pushToast(`Could not resume checkpoint: ${String(error)}`, 'danger')
      return false
    } finally {
      setCheckpointBusy(false)
    }
  }, [conversationMode, messages, pushToast, retireActiveRunUI, selectedSession, streaming])

  const handleDeleteConversationCheckpoint = useCallback(async (checkpoint: RunCheckpoint) => {
    setCheckpointBusy(true)
    try {
      await DeleteResumableRun(checkpoint.run_id)
      await refreshRunCheckpoints()
      pushToast(`Removed checkpoint “${checkpoint.name || 'automatic recovery'}”.`, 'success')
      return true
    } catch (error) {
      pushToast(`Could not remove checkpoint: ${String(error)}`, 'danger')
      return false
    } finally {
      setCheckpointBusy(false)
    }
  }, [pushToast, refreshRunCheckpoints])

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

  const handleRenameSession = useCallback((requestedName?: string) => {
    const source = requestedName || selectedSession
    if (!source || streaming) return
    setRenameSessionSource(source)
    setRenameSessionDraft(source)
    setShowRenameSession(true)
  }, [selectedSession, streaming])

  const submitRenameSession = useCallback(async () => {
    const name = renameSessionDraft.trim()
    if (!renameSessionSource || !name || streaming) return
    try {
      await RenameSession(renameSessionSource, name)
      const cleanName = cleanSessionName(name)
      if (selectedSession === renameSessionSource) setSelectedSession(cleanName)
      await refreshSessions()
      setShowRenameSession(false)
      pushToast(`Renamed conversation to “${cleanName}”.`, 'success')
    } catch (error) {
      pushToast(`Could not rename conversation: ${String(error)}`, 'danger')
    }
  }, [pushToast, refreshSessions, renameSessionDraft, renameSessionSource, selectedSession, streaming])

  const handleEditSessionTags = useCallback((requestedName?: string) => {
    const name = requestedName || selectedSession
    if (!name || streaming) return
    const summary = sessions.find(session => session.name === name)
    setTagSessionName(name)
    setSessionTagsDraft((summary?.tags || []).join(', '))
    setShowSessionTags(true)
  }, [selectedSession, sessions, streaming])

  const submitSessionTags = useCallback(async () => {
    if (!tagSessionName || streaming) return
    const tags = sessionTagsDraft.split(',').map(tag => tag.trim()).filter(Boolean)
    try {
      await SetSessionTags(tagSessionName, tags)
      await refreshSessions()
      setShowSessionTags(false)
      pushToast(tags.length > 0 ? `Updated tags for “${tagSessionName}”.` : `Cleared tags for “${tagSessionName}”.`, 'success')
    } catch (error) {
      pushToast(`Could not update conversation tags: ${String(error)}`, 'danger')
    }
  }, [pushToast, refreshSessions, sessionTagsDraft, streaming, tagSessionName])

  const handleLoadSession = useCallback(async (requestedName?: string) => {
    const name = requestedName || selectedSession
    if (!name) return
    if (streaming) {
      pushToast('Stop the active run before loading another conversation.', 'warn')
      return
    }
    const loaded = await LoadSession(name)
    const loadedMode = await GetConversationMode().catch(() => 'adaptive')
    retireActiveRunUI()
    setSelectedSession(name)
    setMessages(mapSessionMessages(loaded))
    setConversationModeState(loadedMode === 'direct' || loadedMode === 'agent' ? loadedMode : 'adaptive')
    setStreamBuffer('')
    setStatsVersion(v => v + 1)
  }, [pushToast, retireActiveRunUI, selectedSession, streaming])

  const handleDeleteSession = useCallback(async (requestedName?: string) => {
    const name = requestedName || selectedSession
    if (!name) return
    setConfirmAction({
      title: 'Delete Session',
      message: `Delete session "${name}"?`,
      confirmLabel: 'Delete',
      onConfirm: async () => {
        const deletingActiveConversation = selectedSession === name
        await DeleteSession(name)
        if (deletingActiveConversation) {
          await ClearHistory()
          await ClearTodos()
          retireActiveRunUI()
          setSelectedSession('')
          setMessages([])
          setStreamBuffer('')
          setThinkingBuffer('')
          setRunState(null)
          setActivity([])
          refreshTodos()
        }
        await refreshSessions()
      },
    })
  }, [refreshSessions, refreshTodos, retireActiveRunUI, selectedSession])

  const handleInspectSession = useCallback(async (requestedName?: string) => {
    const name = requestedName || selectedSession
    if (!name) return
    setSessionRepairBusy(true)
    try {
      setSessionRepairReport(await InspectSessionRepair(name))
    } catch (error) {
      pushToast(`Could not inspect session: ${String(error)}`, 'danger')
    } finally {
      setSessionRepairBusy(false)
    }
  }, [pushToast, selectedSession])

  const handleApplySessionRepair = useCallback(async () => {
    const name = sessionRepairReport?.name || selectedSession
    if (!name || streaming) return
    setSessionRepairBusy(true)
    try {
      const report = await RepairSession(name)
      setSessionRepairReport(report)
      await refreshSessions()
      pushToast(report.applied ? 'Session repaired. The original was retained as a backup.' : 'Session is already structurally clean.', 'success')
    } catch (error) {
      pushToast(`Could not repair session: ${String(error)}`, 'danger')
    } finally {
      setSessionRepairBusy(false)
    }
  }, [pushToast, refreshSessions, selectedSession, sessionRepairReport?.name, streaming])

  const handleNewChat = useCallback(async () => {
    if (streaming) {
      pushToast('Stop the active run before starting a new conversation.', 'warn')
      return
    }
    try {
      // End-of-run autosave normally owns this write. Saving once more here
      // also protects a conversation immediately before the operator leaves it.
      if (selectedSession && messages.length > 0) {
        await SaveSession(selectedSession)
      }
      await ClearHistory()
      const resetMode = await GetConversationMode().catch(() => 'adaptive')
      await ClearTodos()
      retireActiveRunUI()
      setSelectedSession('')
      setConversationModeState(resetMode === 'direct' || resetMode === 'agent' ? resetMode : 'adaptive')
      setMessages([])
      setStreamBuffer('')
      setThinkingBuffer('')
      setRunState(null)
      setActivity([])
      refreshTodos()
      await refreshSessions()
      setStatsVersion(value => value + 1)
    } catch (error) {
      pushToast(`Could not start a new conversation: ${String(error)}`, 'danger')
    }
  }, [messages.length, pushToast, refreshSessions, refreshTodos, retireActiveRunUI, selectedSession, streaming])

  const handleClearChat = useCallback(() => {
    if (streaming) {
      pushToast('Stop the active run before starting a new conversation.', 'warn')
      return
    }
    setConfirmAction({
      title: 'Clear Chat',
      message: 'Clear this conversation and its active plan? Project files, saved sessions, and memory are preserved.',
      confirmLabel: 'Clear',
      onConfirm: async () => {
        await ClearHistory()
        const resetMode = await GetConversationMode().catch(() => 'adaptive')
        await ClearTodos()
        retireActiveRunUI()
        setSelectedSession('')
        setConversationModeState(resetMode === 'direct' || resetMode === 'agent' ? resetMode : 'adaptive')
        setMessages([])
        setStreamBuffer('')
        refreshTodos()
        setStatsVersion(v => v + 1)
      },
    })
  }, [pushToast, refreshTodos, retireActiveRunUI, streaming])

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey)) return
      if (event.key.toLowerCase() === 'n') {
        event.preventDefault()
        void handleNewChat()
      } else if (event.shiftKey && event.key.toLowerCase() === 's') {
        event.preventDefault()
        void handleSaveSession()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [handleNewChat, handleSaveSession])

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

	const chatCanvas = centerTab === 'chat'
	const gridColumns = chatCanvas
		? `${leftOpen ? leftWidth : closedPanelRails ? 28 : 0}px ${leftOpen ? 6 : 0}px 1fr 0px 0px`
		: `${leftOpen ? leftWidth : closedPanelRails ? 28 : 0}px ${leftOpen ? 6 : 0}px 1fr ${rightOpen ? 6 : 0}px ${rightOpen ? rightWidth : closedPanelRails ? 28 : 0}px`
	const shellLayoutStyle = {
		'--left-pane-width': `${leftWidth}px`,
		'--right-pane-width': `${rightWidth}px`,
		'--terminal-panel-height': `${terminalHeight}px`,
		'--chat-left-offset': `${leftOpen ? leftWidth + 6 : closedPanelRails ? 28 : 0}px`,
	} as CSSProperties

  return (
	<div className={`app-shell ${chatCanvas ? 'chat-canvas-shell' : 'workbench-canvas-shell'}${showTerminal ? ' bottom-surface-open' : ''}`} style={shellLayoutStyle}>
      <div className="titlebar">
        <div className="titlebar-brand">
          <span className="titlebar-logo">M</span>
          <span className="titlebar-name">TheMauler</span>
          <span className="titlebar-context-sep" aria-hidden="true" />
          <span className="titlebar-context">
            <strong>{centerTab === 'chat' ? (selectedSession || 'New chat') : (workbenchPageLabels[centerTab] || 'Workbench')}</strong>
            <small>{streaming
              ? `${activeRunProfile || activeProfile || 'Local model'} · ${titlebarRunSummary(runState)}`
              : `${activeProfile || 'Local model'} · ${autonomous ? 'autonomous' : 'supervised'}`}</small>
          </span>
        </div>
        <div className="titlebar-actions">
		  <button className={`panel-toggle chats-toggle ${leftOpen ? 'on' : ''}`} onClick={() => setLeftOpen(value => !value)} aria-pressed={leftOpen} title="Show or hide conversations (Ctrl+B)"><UiIcon name="chats" /><span>Chats</span></button>
          {!leftOpen && <>
            <ConversationMenu
              sessions={sessions}
              selected={selectedSession}
              streaming={streaming}
              onOpen={name => void handleLoadSession(name)}
              onNew={() => void handleNewChat()}
              onSave={() => void handleSaveSession()}
              onRename={handleRenameSession}
              onEditTags={handleEditSessionTags}
              onInspect={name => void handleInspectSession(name)}
              onCheckpoints={() => void handleOpenConversationCheckpoints()}
              checkpointCount={runCheckpoints.length}
              onDelete={name => void handleDeleteSession(name)}
              onModeChange={(name, mode) => void handleSavedConversationModeChange(name, mode)}
            />
            <div className="titlebar-sep" />
          </>}
		  <button
			className={`panel-toggle inspector-toggle ${rightOpen ? 'on' : ''}`}
			onClick={toggleInspector}
			aria-pressed={rightOpen}
			title="Show or hide Inspector (Ctrl+Shift+B)"
		  ><UiIcon name="inspector" /><span>Inspector</span></button>
		  <button className={`panel-toggle terminal-toggle ${showTerminal ? 'on' : ''}`} onClick={toggleTerminalPanel} aria-pressed={showTerminal} title="Show or hide Terminal (Ctrl+backtick)"><UiIcon name="terminal" /><span>Terminal</span></button>
          <LayoutMenu
            explorerOpen={leftOpen}
            inspectorOpen={rightOpen}
            bottomOpen={showTerminal}
            bottomTab={bottomTab}
            aiCommandsOpen={aiCommandsOpen}
            autoOpenRunPanel={autoOpenRunPanel}
            closedPanelRails={closedPanelRails}
            onExplorerChange={setLeftOpen}
            onInspectorChange={setInspectorVisible}
            onBottomChange={setShowTerminal}
            onShowBottom={showBottomTab}
            onAICommandsChange={setAICommandsOpen}
            onAutoOpenRunPanelChange={setAutoOpenRunPanel}
            onClosedPanelRailsChange={setClosedPanelRails}
            onFocusChat={focusChatLayout}
            onRestoreDefault={restoreDefaultLayout}
            onOpenDoctor={() => {
              setCenterTab('doctor')
              setDoctorRunRequest(v => v + 1)
            }}
            onOpenSettings={() => setShowSettings(true)}
          />
        </div>
      </div>

      <div className="workspace" style={{ gridTemplateColumns: gridColumns }}>
        <div className={leftOpen ? 'pane-slot' : closedPanelRails ? 'pane-slot closed' : 'pane-slot hidden'}>
          {leftOpen ? (
            <HermesSidebar
              sessions={sessions}
              selectedSession={selectedSession}
              activeProfile={activeProfile}
              workspaceLabel={workspaceRootLabel}
              streaming={streaming}
              centerTab={centerTab}
              onNewChat={() => {
                void handleNewChat()
                if (window.innerWidth <= 900) setLeftOpen(false)
              }}
              onOpenSession={name => {
                void handleLoadSession(name)
                if (window.innerWidth <= 900) setLeftOpen(false)
              }}
              onSaveChat={() => void handleSaveSession()}
              onRenameChat={handleRenameSession}
              onEditChatTags={handleEditSessionTags}
              onInspectChat={name => void handleInspectSession(name)}
              onOpenCheckpoints={() => void handleOpenConversationCheckpoints()}
              onDeleteChat={name => void handleDeleteSession(name)}
              onChatModeChange={(name, mode) => void handleSavedConversationModeChange(name, mode)}
              onSelectTab={tab => {
                setCenterTab(tab)
                if (window.innerWidth <= 900) setLeftOpen(false)
                if (tab === 'engagement') setInspectorFocus('facts')
                else if (tab === 'ops') setInspectorFocus('activity')
                else if (tab === 'projects' || tab === 'file') setInspectorFocus('workspace')
              }}
              onClose={() => setLeftOpen(false)}
            />
          ) : closedPanelRails ? (
            <button className="collapsed-rail collapsed-rail-left" onClick={() => setLeftOpen(true)} title="Open chat sidebar">
              <span>Chats</span>
            </button>
          ) : null}
        </div>
        <div
          className={leftOpen ? 'resize-handle resize-handle-left' : 'resize-handle disabled'}
          onMouseDown={leftOpen ? startResize('left') : undefined}
          onDoubleClick={() => setLeftOpen(false)}
          onKeyDown={e => {
            if (leftOpen && resizePaneByKeyboard('left', e.key, e.shiftKey)) e.preventDefault()
          }}
          role="separator"
          aria-label="Resize chat sidebar"
          aria-orientation="vertical"
          tabIndex={leftOpen ? 0 : -1}
          title={leftOpen ? 'Drag or use arrow keys to resize; double-click to collapse the chat sidebar' : undefined}
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
                activeModelID={activeModelID}
                thinkingSupported={thinkingSupported}
                thinkingMode={thinkingMode}
                reasoningEffort={reasoningEffort}
                activeRunProfile={activeRunProfile}
                cloudRunProfiles={cloudRunProfiles}
                runProfileOverride={runProfileOverride}
                autonomous={autonomous}
                pendingInterrupt={pendingInterrupt !== null}
                toolCountdown={showToolCountdown ? toolCountdown : null}
                runState={runState}
                browserHandoff={browserHandoff}
                todos={todos}
                activity={activity}
                settingsVersion={statsVersion}
                engagementVersion={taskRunVersion + workspaceVersion}
                scratchWorkspace={scratchWorkspace}
                agentDefinitions={agentDefinitions}
                agentSelection={agentSelection}
                agentMode={agentMode}
                conversationMode={conversationMode}
                draftRequest={chatDraftRequest}
                onSubmitMessage={handleSubmitMessage}
                onRunProfileOverrideChange={setRunProfileOverride}
                onReasoningControlChange={handleReasoningControlChange}
                onCancelPending={handleCancelPending}
                onStopAgent={() => void StopAgent()}
                onResumeBrowser={async () => {
                  try {
                    await ResumeBrowserWorkflow()
                  } catch (error) {
					pushToast(`Could not resume browser: ${String(error)}`, 'danger')
                  }
                }}
                onStopBrowser={async () => {
                  try {
                    await StopBrowserWorkflow()
                    setBrowserHandoff(null)
                  } catch (error) {
					pushToast(`Could not stop browser: ${String(error)}`, 'danger')
                  }
                }}
                onClearChat={handleClearChat}
                onArtifact={handleArtifact}
                onAutonomousChange={handleToggleAutonomous}
                onOpenQuickChat={() => setChatLane('quick')}
                onConversationModeChange={handleConversationModeChange}
                onOpenSettings={() => setShowSettings(true)}
                onAgentSelectionChange={handleAgentSelectionChange}
                onChooseWorkspace={handleChooseWorkspace}
                onSwitchWorkspace={handleSwitchWorkspace}
                onCreateScratchWorkspace={handleCreateScratchWorkspace}
                onCreateWorkspaceProject={handleCreateWorkspaceProject}
                onPromoteScratchWorkspace={handlePromoteScratchWorkspace}
                onOpenProjects={() => setCenterTab('projects')}
                onOpenEngagement={() => setCenterTab('engagement')}
                onPrepareEngagementRun={prepareEngagementRun}
                onClearPlan={handleClearPlan}
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
                  retireActiveRunUI()
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
              <ContextInspectorPage version={taskRunVersion + statsVersion + workspaceVersion + workspaceFilesVersion} onOpenFile={handleOpenFile} onOpenSettings={() => setShowSettings(true)} />
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
		  className={rightOpen ? `resize-handle resize-handle-right${chatCanvas ? ' inspector-drawer-resize' : ''}` : 'resize-handle disabled'}
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
		<div className={`${rightOpen ? 'pane-slot' : closedPanelRails && !chatCanvas ? 'pane-slot closed' : 'pane-slot hidden'}${chatCanvas ? ' inspector-drawer-slot' : ''}`}>
          {rightOpen ? (
            <RightInspector
              activity={activity}
              runState={runState}
              streaming={streaming}
              taskRunVersion={taskRunVersion}
              workspaceVersion={workspaceVersion + workspaceFilesVersion}
              doctorFocusRequest={doctorRunRequest}
              requestedTab={inspectorFocus}
              requestedTabVersion={inspectorFocusRequest}
              onClose={() => setRightOpen(false)}
              workspaceBrowser={<FileTree key={`${workspaceVersion}-${workspaceFilesVersion}`} onOpenFile={handleOpenFile} />}
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
                  settingsVersion={statsVersion}
                  skillSuggestion={skillSuggestion}
                  onDismissSkillSuggestion={() => setSkillSuggestion(null)}
                />
              )}
            />
          ) : closedPanelRails ? (
            <button className="collapsed-rail collapsed-rail-right" onClick={() => setInspectorVisible(true)} title={`Open ${inspectorFocus} Inspector`}>
              <span>Inspector</span>
            </button>
          ) : null}
        </div>
      </div>

      {/* Terminal resize handle — only visible when panel is open */}
      {showTerminal && (
        <div
		  className={`terminal-resize-handle${chatCanvas ? ' chat-terminal-resize' : ''}`}
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
              nextH = clampTerminalHeight(startH + dy)
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
              ? clampTerminalHeight(terminalHeight + amount)
              : e.key === 'ArrowDown'
                ? clampTerminalHeight(terminalHeight - amount)
                : e.key === 'Home' ? clampTerminalHeight(TERMINAL_HEIGHT_DEFAULT) : null
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
		className={`terminal-panel ${streaming ? 'terminal-panel-live' : ''}${chatCanvas ? ' chat-terminal-drawer' : ''}`}
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
          <button className="bottom-panel-close" onClick={() => setShowTerminal(false)} title="Close bottom panel (Ctrl+J)" aria-label="Close bottom panel">×</button>
        </div>
        <div className="bottom-panel-body">
          <TerminalPane key={`terminal-${workspaceVersion}`} visible={showTerminal && bottomTab === 'terminal'} aiCommandsOpen={aiCommandsOpen} onAICommandsOpenChange={setAICommandsOpen} />
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

      {showConversationCheckpoints && (
        <ConversationCheckpointDialog
          checkpoints={runCheckpoints}
          defaultName={checkpointDefaultName}
          canCreate={!streaming && messages.some(message => message.role === 'user' && message.content.trim())}
          busy={checkpointBusy}
          runActive={streaming}
          onClose={() => setShowConversationCheckpoints(false)}
          onCreate={handleCreateConversationCheckpoint}
          onResume={handleResumeConversationCheckpoint}
          onDelete={handleDeleteConversationCheckpoint}
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

      {showRenameSession && (
        <div className="overlay">
          <div className="confirm-dialog">
            <div className="confirm-header">
              <span className="confirm-title">Rename conversation</span>
            </div>
            <div className="save-session-form">
              <label htmlFor="rename-session-name">Conversation title</label>
              <input
                id="rename-session-name"
                value={renameSessionDraft}
                onChange={e => setRenameSessionDraft(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') void submitRenameSession()
                  if (e.key === 'Escape') setShowRenameSession(false)
                }}
                autoFocus
              />
            </div>
            <div className="confirm-actions">
              <button onClick={() => setShowRenameSession(false)}>Cancel</button>
              <button className="primary" onClick={() => void submitRenameSession()} disabled={!cleanSessionName(renameSessionDraft) || cleanSessionName(renameSessionDraft) === renameSessionSource}>
                Rename
              </button>
            </div>
          </div>
        </div>
      )}

      {showSessionTags && (
        <div className="overlay">
          <div className="confirm-dialog">
            <div className="confirm-header">
              <span className="confirm-title">Tags for {tagSessionName}</span>
            </div>
            <div className="save-session-form">
              <label htmlFor="session-tags">Tags</label>
              <input
                id="session-tags"
                value={sessionTagsDraft}
                onChange={e => setSessionTagsDraft(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') void submitSessionTags()
                  if (e.key === 'Escape') setShowSessionTags(false)
                }}
                placeholder="client, urgent, research"
                autoFocus
              />
              <small>Separate tags with commas. Up to 6 tags, 24 characters each.</small>
            </div>
            <div className="confirm-actions">
              <button onClick={() => setShowSessionTags(false)}>Cancel</button>
              <button className="primary" onClick={() => void submitSessionTags()}>Save tags</button>
            </div>
          </div>
        </div>
      )}

      {sessionRepairReport && (
        <div className="overlay">
          <div className="confirm-dialog session-repair-dialog">
            <div className="confirm-header">
              <span className={`session-repair-status ${sessionRepairReport.status}`}>{sessionRepairReport.status}</span>
              <span className="confirm-title">Session health: {sessionRepairReport.name}</span>
            </div>
            <div className="session-repair-summary">
              <strong>{sessionRepairReport.before_messages}</strong> stored messages
              <span>→</span>
              <strong>{sessionRepairReport.after_messages}</strong> safe messages
              <span>·</span>
              <strong>{sessionRepairReport.actions.length}</strong> repair action{sessionRepairReport.actions.length === 1 ? '' : 's'}
            </div>
            {sessionRepairReport.diagnostic && <div className="session-repair-diagnostic">{sessionRepairReport.diagnostic}</div>}
            {sessionRepairReport.actions.length > 0 && (
              <div className="session-repair-actions-list">
                {sessionRepairReport.actions.map((action, index) => (
                  <div key={`${action.phase}-${action.index}-${index}`}>
                    <span>Phase {action.phase}</span>
                    <strong>{action.action.replaceAll('_', ' ')}</strong>
                    <small>message {action.index + 1}{action.detail ? ` · ${action.detail}` : ''}</small>
                  </div>
                ))}
              </div>
            )}
            {sessionRepairReport.backup_path && <div className="session-repair-backup">Backup: {sessionRepairReport.backup_path}</div>}
            <div className="confirm-actions">
              <button onClick={() => setSessionRepairReport(null)}>Close</button>
              <button
                className="primary"
                onClick={() => void handleApplySessionRepair()}
                disabled={sessionRepairBusy || streaming || !sessionRepairReport.valid || sessionRepairReport.applied || sessionRepairReport.actions.length === 0}
              >
                {sessionRepairBusy ? 'Repairing…' : sessionRepairReport.applied ? 'Repaired' : 'Repair saved session'}
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
