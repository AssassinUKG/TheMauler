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
import { ProjectsPage } from './components/ProjectsPage'
import { TelegramPage } from './components/TelegramPage'
import { StatusBar } from './components/StatusBar'
import { SettingsModal } from './components/SettingsModal'
import { ConfirmDialog } from './components/ConfirmDialog'
import { ToastContainer, type ToastItem } from './components/Toast'
import { TerminalPane } from './components/TerminalPane'
import { StreamPane } from './components/StreamPane'
import { JobsPane } from './components/JobsPane'
import {
  ClearHistory,
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
  GetSettings,
  GetHistoryStats,
  ListTodos,
  SendMessage,
  UpdateSettings,
  type ChatAttachment,
  InterruptShellTool,
  StopAgent,
  type ChatRole,
  type SessionChatMessage,
  type SkillSuggestion,
  type TodoItem,
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

export default function App() {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [streaming, setStreaming] = useState(false)
  const [streamBuffer, setStreamBuffer] = useState('')
  const [confirm, setConfirm] = useState<ConfirmPayload | null>(null)
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(null)
  const [showSaveSession, setShowSaveSession] = useState(false)
  const [saveSessionDraft, setSaveSessionDraft] = useState('')
  const [showSettings, setShowSettings] = useState(false)
  const [statsVersion, setStatsVersion] = useState(0)
  const [sessions, setSessions] = useState<string[]>([])
  const [selectedSession, setSelectedSession] = useState('')
  const [activeProfile, setActiveProfile] = useState('')
  const [autonomous, setAutonomousState] = useState(false)
  const [autoAgents, setAutoAgentsState] = useState(true)
  const [centerTab, setCenterTab] = useState<'chat' | 'ops' | 'projects' | 'file' | 'logs' | 'memory' | 'brain' | 'telegram' | 'benchmarks'>('chat')
  const [openFiles, setOpenFiles] = useState<OpenFile[]>([])
  const [activeFileIdx, setActiveFileIdx] = useState(0)
  const [artifactOutput, setArtifactOutput] = useState('')
  const [artifactRunning, setArtifactRunning] = useState(false)
  const [activity, setActivity] = useState<AgentActivity[]>([])
  const [agentMode, setAgentMode] = useState('Auto')
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

  const persistTerminalPrefs = useCallback(async (open: boolean, height: number) => {
    const settings = await GetSettings().catch(() => null)
    if (!settings) return
    await UpdateSettings({
      ...settings,
      ui: {
        ...settings.ui,
        terminal_default_open: open,
        terminal_height: Math.round(height),
      },
    }).catch(() => null)
  }, [])
  const [pendingInterrupt, setPendingInterrupt] = useState<{ text: string; images: string[]; attachments: ChatAttachment[] } | null>(null)
  const pendingInterruptRef = useRef<{ text: string; images: string[]; attachments: ChatAttachment[] } | null>(null)
  const [leftOpen, setLeftOpen] = useState(true)
  const [rightOpen, setRightOpen] = useState(true)
  const [leftWidth, setLeftWidth] = useState(300)
  const [rightWidth, setRightWidth] = useState(460)
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

  const refreshTodos = useCallback(() => {
    void ListTodos().then(setTodos).catch(() => setTodos([]))
  }, [])

  useEffect(() => {
    const offs = [
      EventsOn('mauler:stream_start', () => {
        setStreaming(true)
        setRunStartedAt(Date.now())
        setRunState({ state: 'starting', detail: 'Preparing request' })
        setStreamBuffer('')
        setThinkingBuffer('')
        setShowTerminal(true)
        setTerminalHeight(prev => {
          const focusHeight = Math.min(520, Math.max(340, Math.round(window.innerHeight * 0.42)))
          return Math.max(prev, focusHeight)
        })
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
      EventsOn('mauler:tool_protocol_repair', () => {
        setStreamBuffer('')
      }),
      EventsOn('mauler:stream_done', () => {
        setStreaming(false)
        setToolCountdown(null)
        setStreamBuffer(prev => {
          const visible = cleanAssistantTranscriptText(prev)
          if (visible) {
            const thinking = pendingThinkingRef.current || undefined
            pendingThinkingRef.current = ''
            setMessages(m => [...m, {
              id: crypto.randomUUID(),
              role: 'assistant',
              content: visible,
              thinking,
              timestamp: Date.now(),
            }])
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
          void SendMessage(pending.text, pending.images, pending.attachments).catch(e => console.error('interrupt SendMessage:', e))
        }
      }),
      EventsOn('mauler:stream_error', (...args: unknown[]) => {
        const err = args[0] as string
        setStreaming(false)
        setToolCountdown(null)
        setStreamBuffer('')
        setRunState({ state: 'failed', detail: err })
        setMessages(m => [...m, {
          id: crypto.randomUUID(),
          role: 'system',
          content: `Error: ${err}`,
          timestamp: Date.now(),
        }])
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
          void SendMessage(pending.text, pending.images, pending.attachments).catch(e => console.error('interrupt SendMessage:', e))
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
        setMessages(m => [...m, {
          id: crypto.randomUUID(),
          role: 'system',
          content: `[Context compacted] ${summary.slice(0, 120)}...`,
          timestamp: Date.now(),
        }])
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
      EventsOn('mauler:workspace_changed', () => {
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
    const [settings, auto, autoAgentEnabled, mode] = await Promise.all([
      GetSettings().catch(() => null),
      GetAutonomous().catch(() => false),
      GetAutoAgents().catch(() => true),
      GetAgentMode().catch(() => 'Auto'),
    ])
    if (settings) {
      setActiveProfile(settings.active_profile)
      setShowToolCountdown(settings.ui.tool_countdown ?? false)
      if (!appliedInitialUI.current) {
        appliedInitialUI.current = true
        setShowTerminal(settings.ui.terminal_default_open ?? false)
        setTerminalHeight(Math.min(600, Math.max(100, settings.ui.terminal_height || 260)))
      }
      applyTheme(settings.ui.theme || 'dark')
      applyAccentColor(settings.ui.accent_color || '#4ade80')
      applyPrimaryColor(settings.ui.primary_color || settings.ui.accent_color || '#16a34a')
    }
    setAutonomousState(auto)
    setAutoAgentsState(autoAgentEnabled)
    setAgentMode(mode || 'Auto')
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
        setShowTerminal(v => {
          const next = !v
          if (next) setBottomTab('terminal')
          void persistTerminalPrefs(next, terminalHeight)
          return next
        })
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
  }, [persistTerminalPrefs, terminalHeight])

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

  const sendNow = useCallback(async (text: string, images: string[], attachments: ChatAttachment[]) => {
    handleUserMessage(text, images, attachments)
    try {
      await SendMessage(text, images, attachments)
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

  const handleSubmitMessage = useCallback((text: string, images: string[], attachments: ChatAttachment[]) => {
    if (streaming) {
      pendingInterruptRef.current = { text, images, attachments }
      setPendingInterrupt({ text, images, attachments })
      void StopAgent()
      return
    }
    void sendNow(text, images, attachments)
  }, [sendNow, streaming])

  const handleCancelPending = useCallback(() => {
    pendingInterruptRef.current = null
    setPendingInterrupt(null)
  }, [])

  const handleCancelTool = useCallback((name: string) => {
    if (name === 'shell' || name === 'bash') {
      void InterruptShellTool()
      return
    }
    void StopAgent()
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

  const mapSessionMessages = (loaded: SessionChatMessage[]): ChatMessage[] =>
    loaded.map(m => ({
      id: crypto.randomUUID(),
      role: m.role,
      content: m.content,
      images: m.images ?? [],
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
      message: 'Clear chat history?',
      confirmLabel: 'Clear',
      onConfirm: async () => {
        await ClearHistory()
        setMessages([])
        setStreamBuffer('')
        setStatsVersion(v => v + 1)
      },
    })
  }, [])

  const startResize = useCallback((side: 'left' | 'right') => (e: ReactMouseEvent) => {
    const startX = e.clientX
    const startLeft = leftWidth
    const startRight = rightWidth
    const onMove = (move: MouseEvent) => {
      const dx = move.clientX - startX
      if (side === 'left') {
        setLeftWidth(Math.min(420, Math.max(180, startLeft + dx)))
      } else {
        setRightWidth(Math.min(680, Math.max(320, startRight - dx)))
      }
    }
    const onUp = () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    e.preventDefault()
  }, [leftWidth, rightWidth])

  const gridColumns = `${leftOpen ? leftWidth : 28}px ${leftOpen ? 4 : 0}px 1fr ${rightOpen ? 4 : 0}px ${rightOpen ? rightWidth : 28}px`

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
              setRightOpen(true)
              setDoctorRunRequest(v => v + 1)
            }}
            title="Run Doctor diagnostics"
          >Doctor</button>
          <div className="titlebar-sep" />
          <button
            onClick={() => {
              setShowTerminal(v => {
                const next = !v
                if (next) setBottomTab('terminal')
                void persistTerminalPrefs(next, terminalHeight)
                return next
              })
            }}
            title="Toggle bottom panel (Ctrl+`)"
            className={showTerminal ? 'panel-toggle on' : 'panel-toggle'}
          >Bottom</button>
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
          title={leftOpen ? 'Drag to resize, double-click to collapse Explorer' : undefined}
        />

        <main className="center-pane">
          <div className="center-tabs">
            <button className={centerTab === 'chat' ? 'active' : ''} onClick={() => setCenterTab('chat')}>Chat</button>
            <button className={centerTab === 'ops' ? 'active' : ''} onClick={() => setCenterTab('ops')}>Run</button>
            <button className={centerTab === 'projects' ? 'active' : ''} onClick={() => setCenterTab('projects')}>Projects</button>
            <button className={centerTab === 'logs' ? 'active' : ''} onClick={() => setCenterTab('logs')}>Logs</button>
            <button className={centerTab === 'memory' ? 'active' : ''} onClick={() => setCenterTab('memory')}>Memory</button>
            <button className={centerTab === 'brain' ? 'active' : ''} onClick={() => setCenterTab('brain')}>Brain</button>
            <button className={centerTab === 'telegram' ? 'active' : ''} onClick={() => setCenterTab('telegram')}>Telegram</button>
            <button className={centerTab === 'benchmarks' ? 'active' : ''} onClick={() => setCenterTab('benchmarks')}>Benchmarks</button>
            {openFiles.map((f, i) => (
              <span key={`${f.path || f.name}-${i}`} className={`center-file-tab ${centerTab === 'file' && activeFileIdx === i ? 'active' : ''}`}>
                <button onClick={() => { setActiveFileIdx(i); setCenterTab('file') }}>{f.name}</button>
                <button className="tab-close" onClick={() => closeFile(i)} title="Close">×</button>
              </span>
            ))}
          </div>
          <div className="center-content">
            {centerTab === 'chat' ? (
              <ChatPane
                messages={messages}
                streaming={streaming}
                streamBuffer={streamBuffer}
                thinkingBuffer={thinkingBuffer}
                activeProfile={activeProfile}
                autonomous={autonomous}
                pendingInterrupt={pendingInterrupt !== null}
                toolCountdown={showToolCountdown ? toolCountdown : null}
                runState={runState}
                todos={todos}
                activity={activity}
                onSubmitMessage={handleSubmitMessage}
                onCancelPending={handleCancelPending}
                onCancelTool={handleCancelTool}
                onStopAgent={() => void StopAgent()}
                onClearChat={handleClearChat}
                onArtifact={handleArtifact}
                onAutonomousChange={handleToggleAutonomous}
              />
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
            ) : centerTab === 'projects' ? (
              <ProjectsPage
                version={statsVersion + workspaceVersion}
                onProjectChanged={() => {
                  setWorkspaceVersion(v => v + 1)
                  setStatsVersion(v => v + 1)
                }}
              />
            ) : centerTab === 'logs' ? (
              <LogsPage version={taskRunVersion + statsVersion} />
            ) : centerTab === 'memory' ? (
              <MemoryPage version={taskRunVersion + statsVersion} />
            ) : centerTab === 'brain' ? (
              <BrainPage version={taskRunVersion + statsVersion} />
            ) : centerTab === 'telegram' ? (
              <TelegramPage version={taskRunVersion + statsVersion} />
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
          title={rightOpen ? 'Drag to resize, double-click to collapse inspector panel' : undefined}
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
          title="Drag to resize, double-click to collapse bottom panel"
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
              void persistTerminalPrefs(showTerminal, nextH)
            }
            window.addEventListener('mousemove', onMove)
            window.addEventListener('mouseup', onUp)
            e.preventDefault()
          }}
          onDoubleClick={() => {
            setShowTerminal(false)
            void persistTerminalPrefs(false, terminalHeight)
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
          <TerminalPane visible={showTerminal && bottomTab === 'terminal'} />
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
