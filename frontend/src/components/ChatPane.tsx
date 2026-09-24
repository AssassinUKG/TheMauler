import { useRef, useEffect, useState, useCallback, useMemo, useLayoutEffect, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  Undo,
  EncodeFileBase64,
  SelectChatFiles,
  PrepareChatAttachmentPath,
  IngestVideo,
  IngestVideoPath,
  PickSaveFilePath,
  SaveFileContent,
  GetWorkingDir,
  GetHistoryStats,
  GetChannelBusStatus,
  ListChannelWorkQueue,
  GetSharedTerminalState,
  GetSettings,
  GetRepositoryIndexStatus,
  IndexWorkspaceRepository,
  RefreshWorkspaceRepositoryIndex,
  CancelWorkspaceRepositoryIndex,
  SelectRepositoryIndexFolder,
  SelectRepositoryIndexFiles,
  RemoveRepositoryIndexSource,
  SetRepositoryIndexWatch,
  GetBrowserWorkflowStatus,
  StartBrowserWorkflow,
  TakeOverBrowserWorkflow,
  ResumeBrowserWorkflow,
  TranscribeVoiceClip,
  SynthesizeSpeech,
  type ChatAttachment,
  type AudioConfig,
  type VideoIngest,
  type HistoryStats,
  type TodoItem,
  type ChannelWorkItem,
  type TerminalStateSnapshot,
  type AgentDefinition,
  type Settings,
  type RepositoryIndexStatus,
  type ScratchWorkspaceStatus,
  type BrowserWorkflowStatus,
} from '../wailsjs/go'
import type { AgentActivity, BrowserHandoffPayload, ChatMessage, RunProfileOption, RunStatePayload, ToolCountdown } from '../App'
import { ChatSecurityWorkspace } from './ChatSecurityWorkspace'
import { UiIcon, type UiIconName } from './UiIcon'
import './ChatPane.css'

const sharedMarkdownComponents: Components = {
  table({ children }) {
    return (
      <div className="markdown-table-scroll" role="region" aria-label="Scrollable table" tabIndex={0}>
        <table>{children}</table>
      </div>
    )
  },
}

interface Props {
  messages: ChatMessage[]
  streaming: boolean
  streamBuffer: string
  thinkingBuffer: string
  activeProfile: string
  activeModelID: string
  thinkingSupported: boolean
  thinkingMode: string
  reasoningEffort: string
  activeRunProfile: string
  cloudRunProfiles: RunProfileOption[]
  runProfileOverride: string
  autonomous: boolean
  pendingInterrupt: boolean
  toolCountdown: ToolCountdown | null
  runState: RunStatePayload | null
  browserHandoff: BrowserHandoffPayload | null
  todos: TodoItem[]
  activity: AgentActivity[]
  settingsVersion: number
  engagementVersion: number
  scratchWorkspace: ScratchWorkspaceStatus | null
  agentDefinitions: AgentDefinition[]
  agentSelection: string
  agentMode: string
  conversationMode: 'adaptive' | 'direct' | 'agent'
  draftRequest?: { id: string; text: string } | null
  onSubmitMessage: (text: string, images: string[], attachments: ChatAttachment[], profileOverride?: string) => void
  onRunProfileOverrideChange: (profile: string) => void
  onReasoningControlChange: (thinkingMode: string, reasoningEffort: string) => void | Promise<void>
  onCancelPending: () => void
  onStopAgent: () => void
  onResumeBrowser: () => void | Promise<void>
  onStopBrowser: () => void | Promise<void>
  onClearChat: () => void
  onArtifact: (code: string, lang: string) => void
  onAutonomousChange: (enabled: boolean) => void
  onOpenQuickChat: () => void
  onConversationModeChange: (mode: 'adaptive' | 'direct' | 'agent') => void | Promise<void>
  onOpenSettings: () => void
  onAgentSelectionChange: (mode: string) => void | Promise<void>
  onChooseWorkspace: (bugBounty: boolean) => void | Promise<void>
  onSwitchWorkspace: (path: string) => void | Promise<void>
  onCreateScratchWorkspace: () => void | Promise<void>
  onCreateWorkspaceProject: (name: string, bugBounty: boolean) => void | Promise<void>
  onPromoteScratchWorkspace: (name: string) => void | Promise<void>
  onOpenProjects: () => void
  onOpenEngagement: () => void
  onPrepareEngagementRun: (prompt: string) => void
  onClearPlan: () => void | Promise<void>
}

interface ChatWorkspaceOption {
  path: string
  name: string
  kind: 'project' | 'folder'
}

interface TranscriptFilters {
  tools: boolean
  status: boolean
  browser: boolean
}

export function ChatPane({
  messages,
  streaming,
  streamBuffer,
  thinkingBuffer,
  activeProfile,
  activeModelID,
  thinkingSupported,
  thinkingMode,
  reasoningEffort,
  activeRunProfile,
  cloudRunProfiles,
  runProfileOverride,
  autonomous,
  pendingInterrupt,
  toolCountdown,
  runState,
  browserHandoff,
  todos,
  activity,
  settingsVersion,
  engagementVersion,
  scratchWorkspace,
  agentDefinitions,
  agentSelection,
  agentMode,
  conversationMode,
  draftRequest,
  onSubmitMessage,
  onRunProfileOverrideChange,
  onReasoningControlChange,
  onCancelPending,
  onStopAgent,
  onResumeBrowser,
  onStopBrowser,
  onClearChat,
  onArtifact,
  onAutonomousChange,
  onOpenQuickChat,
  onConversationModeChange,
  onOpenSettings,
  onAgentSelectionChange,
  onChooseWorkspace,
  onSwitchWorkspace,
  onCreateScratchWorkspace,
  onCreateWorkspaceProject,
  onPromoteScratchWorkspace,
  onOpenProjects,
  onOpenEngagement,
  onPrepareEngagementRun,
  onClearPlan,
}: Props) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const messagesRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const toolbarRef = useRef<HTMLDivElement>(null)
  const composerMoreRef = useRef<HTMLDetailsElement>(null)
  const planTriggerRef = useRef<HTMLButtonElement>(null)
  const toolsTriggerRef = useRef<HTMLButtonElement>(null)
  const workspaceTriggerRef = useRef<HTMLButtonElement>(null)
  const agentTriggerRef = useRef<HTMLButtonElement>(null)
  const runSetupTriggerRef = useRef<HTMLButtonElement>(null)
  const reasoningTriggerRef = useRef<HTMLButtonElement>(null)
  const [input, setInput] = useState('')
  const [images, setImages] = useState<string[]>([])
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [attachmentStatus, setAttachmentStatus] = useState<string | null>(null)
  const [videoStatus, setVideoStatus] = useState<string | null>(null)
  const [lightboxImage, setLightboxImage] = useState<string | null>(null)
  const [attachmentEditor, setAttachmentEditor] = useState<{ attachment: ChatAttachment; source: 'draft' | 'message' } | null>(null)
  const [attachmentEditorText, setAttachmentEditorText] = useState('')
  const [attachmentCopied, setAttachmentCopied] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [showSearch, setShowSearch] = useState(false)
  const [toolbarMenu, setToolbarMenu] = useState<'tools' | 'view' | 'run' | null>(null)
  const [showBrowserPanel, setShowBrowserPanel] = useState(false)
  const [showIndexPanel, setShowIndexPanel] = useState(() => localStorage.getItem('mauler.chat.indexPanel') === 'true')
  const [showSecurityPanel, setShowSecurityPanel] = useState(() => localStorage.getItem('mauler.chat.securityPanel') === 'true')
  const [followOutput, setFollowOutput] = useState(() => localStorage.getItem('mauler.chat.followOutput') !== 'false')
  const [browserStatus, setBrowserStatus] = useState<BrowserWorkflowStatus | null>(null)
  const [browserURL, setBrowserURL] = useState('')
  const [browserBusy, setBrowserBusy] = useState(false)
  const [browserError, setBrowserError] = useState('')
  const [repositoryIndex, setRepositoryIndex] = useState<RepositoryIndexStatus | null>(null)
  const [repositoryIndexBusy, setRepositoryIndexBusy] = useState(false)
  const [repositorySourceBusy, setRepositorySourceBusy] = useState(false)
  const [repositoryIndexError, setRepositoryIndexError] = useState('')
  const [nowMs, setNowMs] = useState(Date.now())
  const [workspaceRoot, setWorkspaceRoot] = useState('')
  const [historyStats, setHistoryStats] = useState<HistoryStats | null>(null)
  const [channelStatus, setChannelStatus] = useState<Record<string, string>>({})
  const [channelQueue, setChannelQueue] = useState<ChannelWorkItem[]>([])
  const [terminalState, setTerminalState] = useState<TerminalStateSnapshot | null>(null)
  const [openPopover, setOpenPopover] = useState<'plan' | 'tools' | 'workspace' | 'agent' | 'runSetup' | 'reasoning' | null>(null)
  const [reasoningBusy, setReasoningBusy] = useState(false)
  const [reasoningError, setReasoningError] = useState('')
  const [transcriptFilters, setTranscriptFilters] = useState<TranscriptFilters>(() => {
    const legacyReplies = localStorage.getItem('mauler.chat.transcriptMode') === 'replies'
    try {
      const saved = JSON.parse(localStorage.getItem('mauler.chat.transcriptFilters') || '') as Partial<TranscriptFilters>
      return { tools: saved.tools !== false, status: saved.status !== false, browser: saved.browser !== false }
    } catch {
      return { tools: !legacyReplies, status: !legacyReplies, browser: !legacyReplies }
    }
  })
  const [audioConfig, setAudioConfig] = useState<AudioConfig | null>(null)
  const [workspaceOptions, setWorkspaceOptions] = useState<ChatWorkspaceOption[]>([])
  const [recording, setRecording] = useState(false)
  const [voiceSession, setVoiceSession] = useState(false)
  const [voiceStatus, setVoiceStatus] = useState('')
  const recorderRef = useRef<MediaRecorder | null>(null)
  const microphoneRef = useRef<MediaStream | null>(null)
  const recordedChunksRef = useRef<Blob[]>([])
  const recordingStartedRef = useRef(0)
  const playbackRef = useRef<HTMLAudioElement | null>(null)
  const speechGenerationRef = useRef(0)
  const speechQueueRef = useRef<Promise<void>>(Promise.resolve())
  const speechOffsetRef = useRef(0)
  const speechBufferRef = useRef('')
  const wasStreamingRef = useRef(false)
  const appliedDraftRequestRef = useRef('')

  useEffect(() => {
    if (!draftRequest?.id || appliedDraftRequestRef.current === draftRequest.id) return
    appliedDraftRequestRef.current = draftRequest.id
    setInput(current => current.trim() ? `${current}\n\n${draftRequest.text}` : draftRequest.text)
    window.setTimeout(() => inputRef.current?.focus(), 0)
  }, [draftRequest])

  const transcriptMessages = useMemo(() => messages.filter(message => {
    if (message.role === 'tool_call' || message.role === 'tool_result' || message.category === 'tool') return transcriptFilters.tools
    if (message.category === 'browser') return transcriptFilters.browser
    if (message.role === 'system' || message.category === 'status') return transcriptFilters.status
    return true
  }), [messages, transcriptFilters])

  const visibleMessages = useMemo(() => {
    if (!searchQuery.trim()) return transcriptMessages
    const q = searchQuery.toLowerCase()
    return transcriptMessages.filter(m => {
      const attachmentText = (m.attachments ?? []).map(a => `${a.name} ${a.content ?? ''} ${a.path ?? ''}`).join(' ')
      return `${m.content} ${m.toolName ?? ''} ${m.category ?? ''} ${attachmentText}`.toLowerCase().includes(q)
    })
  }, [searchQuery, transcriptMessages])

  const storeTranscriptFilters = useCallback((filters: TranscriptFilters) => {
    setTranscriptFilters(filters)
    localStorage.setItem('mauler.chat.transcriptFilters', JSON.stringify(filters))
    localStorage.setItem('mauler.chat.transcriptMode', filters.tools || filters.status || filters.browser ? 'complete' : 'replies')
  }, [])

  const changeTranscriptMode = useCallback((mode: 'complete' | 'replies') => {
    const enabled = mode === 'complete'
    storeTranscriptFilters({ tools: enabled, status: enabled, browser: enabled })
  }, [storeTranscriptFilters])

  const toggleTranscriptFilter = useCallback((name: keyof TranscriptFilters) => {
    storeTranscriptFilters({ ...transcriptFilters, [name]: !transcriptFilters[name] })
  }, [storeTranscriptFilters, transcriptFilters])

  const transcriptMode = transcriptFilters.tools && transcriptFilters.status && transcriptFilters.browser
    ? 'complete'
    : !transcriptFilters.tools && !transcriptFilters.status && !transcriptFilters.browser ? 'replies' : 'custom'

  const storeFollowOutput = useCallback((enabled: boolean) => {
    setFollowOutput(enabled)
    localStorage.setItem('mauler.chat.followOutput', String(enabled))
  }, [])

  useEffect(() => {
    if (!toolbarMenu) return
    const dismissToolbar = (event: globalThis.PointerEvent) => {
      if (event.target instanceof Node && !toolbarRef.current?.contains(event.target)) {
        setToolbarMenu(null)
      }
    }
    const dismissOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') setToolbarMenu(null)
    }
    window.addEventListener('pointerdown', dismissToolbar)
    window.addEventListener('keydown', dismissOnEscape)
    return () => {
      window.removeEventListener('pointerdown', dismissToolbar)
      window.removeEventListener('keydown', dismissOnEscape)
    }
  }, [toolbarMenu])

  // Follow new output only when the operator wants it. Scrolling upward pauses
  // following immediately so a streaming tool result cannot steal the viewport.
  useEffect(() => {
    if (!followOutput) return
    bottomRef.current?.scrollIntoView({ behavior: 'auto', block: 'end' })
  }, [messages, streamBuffer, thinkingBuffer, followOutput])

  useEffect(() => {
    if (!toolCountdown) return
    const id = window.setInterval(() => setNowMs(Date.now()), 500)
    return () => window.clearInterval(id)
  }, [toolCountdown])

  useEffect(() => {
    void GetSettings().then(value => {
      setAudioConfig(value.audio)
      setWorkspaceOptions(chatWorkspaceOptions(value))
    }).catch(() => {
      setAudioConfig(null)
      setWorkspaceOptions([])
    })
  }, [settingsVersion])

  const refreshBrowserStatus = useCallback(async () => {
    const status = await GetBrowserWorkflowStatus()
    setBrowserStatus(status)
    if (status.url) setBrowserURL(status.url)
    return status
  }, [])

  useEffect(() => {
    void refreshBrowserStatus().catch(error => setBrowserError(String(error)))
    const id = window.setInterval(() => {
      void refreshBrowserStatus().catch(() => {})
    }, 5000)
    return () => window.clearInterval(id)
  }, [refreshBrowserStatus, settingsVersion])

  useEffect(() => {
    if (!browserHandoff?.active) return
    setShowBrowserPanel(true)
    void refreshBrowserStatus().catch(() => {})
  }, [browserHandoff?.active, refreshBrowserStatus])

  const runBrowserAction = useCallback(async (action: () => unknown | Promise<unknown>) => {
    setBrowserBusy(true)
    setBrowserError('')
    try {
      await action()
      await refreshBrowserStatus()
    } catch (error) {
      setBrowserError(error instanceof Error ? error.message : String(error))
      await refreshBrowserStatus().catch(() => {})
    } finally {
      setBrowserBusy(false)
    }
  }, [refreshBrowserStatus])

  const refreshRepositoryIndex = useCallback(async () => {
    const status = await GetRepositoryIndexStatus()
    setRepositoryIndex(status)
    return status
  }, [])

  useEffect(() => {
	void refreshRepositoryIndex().catch(error => setRepositoryIndexError(String(error)))
	if (!showIndexPanel) return
	const id = window.setInterval(() => { void refreshRepositoryIndex().catch(() => {}) }, repositoryIndexBusy || repositoryIndex?.indexing ? 750 : 8000)
	return () => window.clearInterval(id)
	}, [refreshRepositoryIndex, settingsVersion, workspaceRoot, showIndexPanel, repositoryIndexBusy, repositoryIndex?.indexing])

  const rebuildRepositoryIndex = useCallback(async () => {
    setRepositoryIndexBusy(true)
    setRepositoryIndexError('')
    try {
      setRepositoryIndex(await IndexWorkspaceRepository())
	} catch (error) {
	  const message = error instanceof Error ? error.message : String(error)
	  if (!message.toLowerCase().includes('context canceled')) setRepositoryIndexError(message)
	  await refreshRepositoryIndex().catch(() => {})
    } finally {
      setRepositoryIndexBusy(false)
    }
  }, [refreshRepositoryIndex])

  const refreshChangedRepositoryFiles = useCallback(async () => {
    setRepositoryIndexBusy(true)
    setRepositoryIndexError('')
    try {
      setRepositoryIndex(await RefreshWorkspaceRepositoryIndex())
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      if (!message.toLowerCase().includes('context canceled')) setRepositoryIndexError(message)
      await refreshRepositoryIndex().catch(() => {})
    } finally {
      setRepositoryIndexBusy(false)
    }
  }, [refreshRepositoryIndex])

  const cancelRepositoryIndex = useCallback(async () => {
	setRepositoryIndexError('')
	try {
	  setRepositoryIndex(await CancelWorkspaceRepositoryIndex())
	} catch (error) {
	  setRepositoryIndexError(error instanceof Error ? error.message : String(error))
	}
  }, [])

  const updateRepositorySources = useCallback(async (action: () => Promise<RepositoryIndexStatus>) => {
    setRepositorySourceBusy(true)
    setRepositoryIndexError('')
    try {
      setRepositoryIndex(await action())
    } catch (error) {
      setRepositoryIndexError(error instanceof Error ? error.message : String(error))
    } finally {
      setRepositorySourceBusy(false)
    }
  }, [])

  const stopSpeech = useCallback(() => {
    speechGenerationRef.current += 1
    playbackRef.current?.pause()
    playbackRef.current = null
    speechQueueRef.current = Promise.resolve()
    speechBufferRef.current = ''
  }, [])

  const enqueueSpeech = useCallback((text: string) => {
    const spoken = text.trim()
    if (!spoken) return
    const generation = speechGenerationRef.current
    speechQueueRef.current = speechQueueRef.current.catch(() => {}).then(async () => {
      const result = await SynthesizeSpeech(spoken)
      if (generation !== speechGenerationRef.current) return
      const player = new Audio(result.data_uri)
      playbackRef.current = player
      await new Promise<void>((resolve, reject) => {
        player.onended = () => resolve()
        player.onerror = () => reject(new Error('Audio playback failed'))
        void player.play().catch(reject)
      })
      if (playbackRef.current === player) playbackRef.current = null
    }).catch(error => setVoiceStatus(`Voice reply unavailable: ${String(error)}`))
  }, [])

  useEffect(() => {
    if (!voiceSession || !audioConfig?.enabled || !audioConfig.speak_replies) {
      wasStreamingRef.current = streaming
      speechOffsetRef.current = streamBuffer.length
      return
    }
    if (streaming && !wasStreamingRef.current) {
      speechOffsetRef.current = 0
      speechBufferRef.current = ''
      stopSpeech()
    }
    if (streamBuffer.length < speechOffsetRef.current) speechOffsetRef.current = 0
    const delta = streamBuffer.slice(speechOffsetRef.current)
    speechOffsetRef.current = streamBuffer.length
    if (delta) speechBufferRef.current += delta
    const extracted = takeSpeechChunks(speechBufferRef.current, audioConfig.clause_min_chars || 36, !streaming && wasStreamingRef.current)
    speechBufferRef.current = extracted.rest
    extracted.chunks.forEach(enqueueSpeech)
    wasStreamingRef.current = streaming
  }, [audioConfig, enqueueSpeech, stopSpeech, streamBuffer, streaming, voiceSession])

  useEffect(() => () => {
    microphoneRef.current?.getTracks().forEach(track => track.stop())
    stopSpeech()
  }, [stopSpeech])

  const stopRecording = useCallback(() => {
    const recorder = recorderRef.current
    if (recorder?.state === 'recording') recorder.stop()
  }, [])

  const startRecording = useCallback(async () => {
    if (!audioConfig?.enabled) {
      setVoiceStatus('Enable Audio / Voice in Settings first.')
      return
    }
    stopSpeech()
    if (streaming && audioConfig.barge_in) onStopAgent()
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          deviceId: audioConfig.input_device ? { exact: audioConfig.input_device } : undefined,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      })
      const preferred = ['audio/webm;codecs=opus', 'audio/ogg;codecs=opus'].find(type => MediaRecorder.isTypeSupported(type))
      const recorder = preferred ? new MediaRecorder(stream, { mimeType: preferred }) : new MediaRecorder(stream)
      microphoneRef.current = stream
      recorderRef.current = recorder
      recordedChunksRef.current = []
      recorder.ondataavailable = event => { if (event.data.size > 0) recordedChunksRef.current.push(event.data) }
      recorder.onstop = () => {
        setRecording(false)
        stream.getTracks().forEach(track => track.stop())
        microphoneRef.current = null
        const blob = new Blob(recordedChunksRef.current, { type: recorder.mimeType || 'audio/webm' })
        if (blob.size === 0) return
        if (Date.now() - recordingStartedRef.current < 400) {
          setVoiceStatus('Recording was too short. Hold Talk while speaking.')
          return
        }
        setVoiceStatus('Transcribing...')
        void blobToDataURI(blob)
          .then(TranscribeVoiceClip)
          .then(transcript => {
            setVoiceStatus(`Heard: ${transcript} · sending to Project Agent with configured tools`)
            onSubmitMessage(transcript, [], [], runProfileOverride)
          })
          .catch(error => setVoiceStatus(`Transcription failed: ${String(error)}`))
      }
      recorder.start(200)
      recordingStartedRef.current = Date.now()
      setVoiceSession(true)
      setRecording(true)
      setVoiceStatus('Listening...')
    } catch (error) {
      setVoiceStatus(`Microphone unavailable: ${String(error)}`)
    }
  }, [audioConfig, onStopAgent, onSubmitMessage, runProfileOverride, stopSpeech, streaming])

  useEffect(() => {
    let cancelled = false
    const loadRunPacket = async () => {
      const [cwd, stats] = await Promise.all([
        GetWorkingDir().catch(() => ''),
        GetHistoryStats().catch(() => null),
      ])
      if (cancelled) return
      setWorkspaceRoot(cwd)
      setHistoryStats(stats)
    }
    void loadRunPacket()
    const id = window.setInterval(() => { void loadRunPacket() }, streaming ? 2000 : 8000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [messages.length, streaming, activeProfile])

  useEffect(() => {
    let cancelled = false
    const loadToolboxState = async () => {
      const [status, queue, terminal] = await Promise.all([
        GetChannelBusStatus().catch(() => ({} as Record<string, string>)),
        ListChannelWorkQueue().then(items => items.slice(0, 5)).catch(() => [] as ChannelWorkItem[]),
        GetSharedTerminalState().catch(() => null),
      ])
      if (cancelled) return
      setChannelStatus(status)
      setChannelQueue(queue)
      setTerminalState(terminal)
    }
    void loadToolboxState()
    const id = window.setInterval(() => { void loadToolboxState() }, openPopover === 'tools' || streaming ? 1500 : 6000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [openPopover, streaming])

  const handleSend = useCallback(async () => {
    const text = input.trim()
    if (!text && images.length === 0 && attachments.length === 0) return

    // /save <filename> — write last assistant reply to disk without invoking AI
    if (text.startsWith('/save ')) {
      const filename = text.slice(6).trim()
      if (filename) {
        const last = [...messages].reverse().find(m => m.role === 'assistant')
        if (last) {
          try {
            const cwd = await GetWorkingDir()
            const isAbsolute = filename.startsWith('/') || /^[A-Za-z]:[\\/]/.test(filename)
            const savePath = isAbsolute ? filename : `${cwd}/${filename}`
            await SaveFileContent(savePath, last.content)
          } catch (e) {
            console.error('/save failed:', e)
          }
        }
      }
      setInput('')
      return
    }

    setInput('')
    const imgs = [...images]
    const atts = [...attachments]
    setImages([])
    setAttachments([])
    onSubmitMessage(text, imgs, atts, runProfileOverride)
  }, [input, images, attachments, messages, onSubmitMessage, runProfileOverride])

  const applyReasoningControl = useCallback(async (nextThinkingMode: string, nextReasoningEffort: string) => {
    if (streaming || reasoningBusy) return
    setReasoningBusy(true)
    setReasoningError('')
    try {
      await onReasoningControlChange(nextThinkingMode, nextReasoningEffort)
    } catch (error) {
      setReasoningError(error instanceof Error ? error.message : String(error))
    } finally {
      setReasoningBusy(false)
    }
  }, [onReasoningControlChange, reasoningBusy, streaming])

  const handleKeyDown = useCallback((e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey) {
      e.preventDefault()
      void handleSend()
    }
    if (e.key === 'z' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      void Undo().then(msg => console.log('undo:', msg))
    }
    if (e.key === 'f' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      setShowSearch(v => {
        if (!v) setTimeout(() => searchRef.current?.focus(), 0)
        return !v
      })
    }
  }, [handleSend])

  // Also open search via Ctrl+F when chat area has focus
  useEffect(() => {
    const handler = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'f' && (e.ctrlKey || e.metaKey) && document.activeElement !== inputRef.current) {
        e.preventDefault()
        setShowSearch(v => {
          if (!v) setTimeout(() => searchRef.current?.focus(), 0)
          return true
        })
      }
      if (e.key === 'Escape' && showSearch) {
        setShowSearch(false)
        setSearchQuery('')
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [showSearch])

  const IMAGE_EXTS: Record<string, string> = { png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp' }
  const VIDEO_EXTS: Record<string, string> = { mp4: 'video/mp4', m4v: 'video/mp4', mov: 'video/quicktime', webm: 'video/webm', mkv: 'video/x-matroska', avi: 'video/x-msvideo' }
  const TEXT_EXTS = new Set(['txt', 'md', 'markdown', 'csv', 'tsv', 'json', 'jsonl', 'xml', 'yaml', 'yml', 'toml', 'ini', 'log', 'go', 'ts', 'tsx', 'js', 'jsx', 'css', 'html', 'py', 'ps1', 'sh', 'sql'])
  const MAX_ATTACHMENT_CHARS = 24_000

  const addAttachment = useCallback((attachment: Omit<ChatAttachment, 'id'>) => {
    setAttachments(prev => [...prev, { ...attachment, id: crypto.randomUUID() }])
  }, [])

  const showAttachmentStatus = useCallback((message: string) => {
    setAttachmentStatus(message)
    window.setTimeout(() => setAttachmentStatus(current => current === message ? null : current), 5000)
  }, [])

  const readTextFileAttachment = useCallback((file: File) => {
    const ext = file.name.split('.').pop()?.toLowerCase() ?? ''
    const isText = file.type.startsWith('text/') || TEXT_EXTS.has(ext)
    if (!isText) {
      const path = (file as File & { path?: string }).path
      addAttachment({
        name: file.name || 'Attached file',
        kind: 'file',
        mime: file.type,
        path,
        size: file.size,
        content: path ? `Local file path: ${path}` : `File "${file.name}" was attached, but its contents were not readable from the browser clipboard/drop payload.`,
      })
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      const raw = String(reader.result ?? '')
      const truncated = raw.length > MAX_ATTACHMENT_CHARS
      addAttachment({
        name: file.name || 'Pasted text.txt',
        kind: 'document',
        mime: file.type || 'text/plain',
        size: file.size,
        content: truncated ? raw.slice(0, MAX_ATTACHMENT_CHARS) : raw,
        truncated,
      })
      if (truncated) {
        showAttachmentStatus(`Only the first ${MAX_ATTACHMENT_CHARS.toLocaleString()} characters were attached inline. Use Attach files or paste the file path to let Mauler read the complete file in safe chunks.`)
      }
    }
    reader.readAsText(file)
  }, [TEXT_EXTS, addAttachment, showAttachmentStatus])

  // Video: local vision models can't decode raw video, so the Go side samples
  // keyframes (added as images) plus an optional audio transcript (added as a
  // context attachment).
  const applyVideoIngest = useCallback((res: VideoIngest, label: string) => {
    if (res.frames?.length) setImages(prev => [...prev, ...res.frames])
    const parts = [res.note?.trim() || `Video "${label}" attached as keyframes.`]
    if (res.transcript?.trim()) parts.push(`\nAudio transcript:\n${res.transcript.trim()}`)
    addAttachment({
      name: `${label} — video context`,
      kind: 'document',
      mime: 'text/plain',
      content: parts.join('\n'),
    })
  }, [addAttachment])

  const ingestVideoData = useCallback(async (file: File) => {
    setVideoStatus(`Analyzing ${file.name || 'video'}…`)
    try {
      const dataURI = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result ?? ''))
        reader.onerror = () => reject(reader.error)
        reader.readAsDataURL(file)
      })
      applyVideoIngest(await IngestVideo(dataURI, file.name || 'clip.mp4'), file.name || 'video')
    } catch (err) {
      addAttachment({ name: file.name || 'video', kind: 'file', content: `Video could not be analyzed: ${String(err)}` })
    } finally {
      setVideoStatus(null)
    }
  }, [applyVideoIngest, addAttachment])

  const ingestVideoPath = useCallback(async (path: string) => {
    const name = path.split(/[\\/]/).pop() || 'video'
    setVideoStatus(`Analyzing ${name}…`)
    try {
      applyVideoIngest(await IngestVideoPath(path), name)
    } catch (err) {
      addAttachment({ name, kind: 'file', path, content: `Video could not be analyzed: ${String(err)}` })
    } finally {
      setVideoStatus(null)
    }
  }, [applyVideoIngest, addAttachment])

  const addPreparedFile = useCallback(async (attachment: ChatAttachment) => {
    const path = attachment.path || ''
    if (!path) {
      addAttachment(attachment)
      return
    }
    if (attachment.kind === 'image' || attachment.mime?.startsWith('image/')) {
      const mime = attachment.mime || IMAGE_EXTS[path.split('.').pop()?.toLowerCase() ?? ''] || 'image/png'
      const b64 = await EncodeFileBase64(path)
      setImages(prev => [...prev, `data:${mime};base64,${b64}`])
      return
    }
    if (attachment.kind === 'video' || attachment.mime?.startsWith('video/')) {
      await ingestVideoPath(path)
      return
    }
    addAttachment(attachment)
  }, [IMAGE_EXTS, addAttachment, ingestVideoPath])

  const attachLocalPath = useCallback(async (path: string): Promise<boolean> => {
    try {
      const attachment = await PrepareChatAttachmentPath(path)
      await addPreparedFile(attachment)
      showAttachmentStatus(`Attached ${attachment.name}. Mauler can read the complete file in bounded chunks.`)
      return true
    } catch (err) {
      showAttachmentStatus(`Could not attach ${path}: ${String(err)}`)
      return false
    }
  }, [addPreparedFile, showAttachmentStatus])

  const chooseChatFiles = useCallback(async () => {
    try {
      const selected = await SelectChatFiles()
      for (const attachment of selected) {
        await addPreparedFile(attachment)
      }
      if (selected.length > 0) {
        showAttachmentStatus(`Attached ${selected.length} file${selected.length === 1 ? '' : 's'}. Large files will be read in bounded chunks.`)
        inputRef.current?.focus()
      }
    } catch (err) {
      showAttachmentStatus(`Could not attach files: ${String(err)}`)
    }
  }, [addPreparedFile, showAttachmentStatus])

  const handleDrop = useCallback(async (e: React.DragEvent<HTMLTextAreaElement>) => {
    e.preventDefault()
    const files = Array.from(e.dataTransfer.files ?? [])
    if (files.length > 0) {
      for (const file of files) {
        const localPath = (file as File & { path?: string }).path
        if (localPath) {
          await attachLocalPath(localPath)
          continue
        }
        const ext = file.name.split('.').pop()?.toLowerCase() ?? ''
        const mime = file.type || IMAGE_EXTS[ext]
        if (mime?.startsWith('image/')) {
          const reader = new FileReader()
          reader.onload = () => setImages(prev => [...prev, reader.result as string])
          reader.readAsDataURL(file)
        } else if (mime?.startsWith('video/') || VIDEO_EXTS[ext]) {
          void ingestVideoData(file)
        } else {
          readTextFileAttachment(file)
        }
      }
      inputRef.current?.focus()
      return
    }
    const path = e.dataTransfer.getData('text/plain')
    if (!path) return
    await attachLocalPath(path)
    inputRef.current?.focus()
  }, [IMAGE_EXTS, VIDEO_EXTS, readTextFileAttachment, ingestVideoData, attachLocalPath])

  // Paste images, copied files, and larger/multiline text as attachments.
  const handlePaste = useCallback((e: React.ClipboardEvent) => {
    const items = Array.from(e.clipboardData.items)
    let handledBinary = false
    for (const item of items) {
      if (item.kind !== 'file') continue
      const file = item.getAsFile()
      if (!file) continue
      const localPath = (file as File & { path?: string }).path
      if (localPath) {
        handledBinary = true
        void attachLocalPath(localPath)
      } else if (item.type.startsWith('image/')) {
        handledBinary = true
        const reader = new FileReader()
        reader.onload = () => {
          setImages(prev => [...prev, reader.result as string])
        }
        reader.readAsDataURL(file)
      } else if (item.kind === 'file' && item.type.startsWith('video/')) {
        handledBinary = true
        void ingestVideoData(file)
      } else {
        handledBinary = true
        readTextFileAttachment(file)
      }
    }
    const pastedText = e.clipboardData.getData('text/plain')
    const pastedPaths = clipboardFilePathCandidates(pastedText)
    if (!handledBinary && pastedPaths.length > 0) {
      e.preventDefault()
      void (async () => {
        const failed: string[] = []
        for (const path of pastedPaths) {
          if (!await attachLocalPath(path)) failed.push(path)
        }
        if (failed.length > 0) {
          setInput(prev => prev ? `${prev}\n${failed.join('\n')}` : failed.join('\n'))
        }
      })()
      return
    }
    const shouldAttachText = pastedText && (pastedText.length > 600 || pastedText.split(/\r?\n/).length > 8)
    if (shouldAttachText) {
      e.preventDefault()
      const truncated = pastedText.length > MAX_ATTACHMENT_CHARS
      addAttachment({
        name: 'Pasted text.txt',
        kind: 'document',
        mime: 'text/plain',
        size: pastedText.length,
        content: truncated ? pastedText.slice(0, MAX_ATTACHMENT_CHARS) : pastedText,
        truncated,
      })
      if (truncated) {
        showAttachmentStatus(`Only the first ${MAX_ATTACHMENT_CHARS.toLocaleString()} characters were attached inline. Use Attach files or paste a file path for the complete source.`)
      }
    } else if (handledBinary) {
      e.preventDefault()
    }
  }, [addAttachment, readTextFileAttachment, ingestVideoData, attachLocalPath, showAttachmentStatus])

  const removeImage = useCallback((idx: number) => {
    setImages(prev => prev.filter((_, i) => i !== idx))
  }, [])

  const removeAttachment = useCallback((id: string | undefined) => {
    setAttachments(prev => prev.filter(a => a.id !== id))
  }, [])

  const openAttachment = useCallback((attachment: ChatAttachment, source: 'draft' | 'message') => {
    setAttachmentEditor({ attachment, source })
    setAttachmentEditorText(attachment.content ?? attachment.path ?? '')
    setAttachmentCopied(false)
  }, [])

  const closeAttachment = useCallback(() => {
    setAttachmentEditor(null)
    setAttachmentEditorText('')
    setAttachmentCopied(false)
  }, [])

  const copyAttachment = useCallback(async () => {
    await navigator.clipboard.writeText(attachmentEditorText)
    setAttachmentCopied(true)
    window.setTimeout(() => setAttachmentCopied(false), 1600)
  }, [attachmentEditorText])

  const saveAttachmentEdit = useCallback(() => {
    if (!attachmentEditor || !attachmentIsEditable(attachmentEditor.attachment)) return
    if (attachmentEditor.source === 'draft') {
      setAttachments(prev => prev.map(att => att.id === attachmentEditor.attachment.id
        ? { ...att, content: attachmentEditorText, size: attachmentEditorText.length, truncated: false }
        : att))
    } else {
      setAttachments(prev => [...prev, {
        ...attachmentEditor.attachment,
        id: crypto.randomUUID(),
        name: editedAttachmentName(attachmentEditor.attachment.name),
        path: undefined,
        content: attachmentEditorText,
        size: attachmentEditorText.length,
        truncated: false,
      }])
    }
    closeAttachment()
    window.setTimeout(() => inputRef.current?.focus(), 0)
  }, [attachmentEditor, attachmentEditorText, closeAttachment])

  useEffect(() => {
    if (!attachmentEditor) return
    const closeOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') closeAttachment()
    }
    window.addEventListener('keydown', closeOnEscape)
    return () => window.removeEventListener('keydown', closeOnEscape)
  }, [attachmentEditor, closeAttachment])

  // Open code blocks as scratch snippets in the File tab.
  const handleCodeBlock = useCallback((code: string, lang: string) => {
    onArtifact(code, lang || 'plaintext')
  }, [onArtifact])

  return (
    <div className="chat-pane">
      <div className="chat-lane-banner">
        <div className="chat-lane-identity">
          <strong>Mauler chat</strong>
          <span>Chat immediately; a workspace adds durable files, tools and evidence.</span>
        </div>
        <div className="chat-header-actions" ref={toolbarRef}>
          <div className={`chat-toolbar-menu ${toolbarMenu === 'tools' ? 'open' : ''}`}>
            <button className={(showSecurityPanel || showIndexPanel || showBrowserPanel) ? 'active' : ''} onClick={() => setToolbarMenu(value => value === 'tools' ? null : 'tools')} aria-haspopup="menu" aria-expanded={toolbarMenu === 'tools'}>
              <UiIcon name="tools" />
              <span>Tools</span>
              {(repositoryIndex?.active || browserStatus?.active) && <i aria-hidden="true" />}
              <b aria-hidden="true">⌄</b>
            </button>
            {toolbarMenu === 'tools' && (
              <div className="chat-toolbar-popover" role="menu" aria-label="Chat tools">
                <div className="chat-toolbar-popover-heading"><strong>Tools</strong><span>Open a task surface in Chat</span></div>
                <button
                  className={showSecurityPanel ? 'active' : ''}
                  onClick={() => {
                    setShowSecurityPanel(value => {
                      localStorage.setItem('mauler.chat.securityPanel', String(!value))
                      return !value
                    })
                    setToolbarMenu(null)
                  }}
                  role="menuitem"
                >
                  <UiIcon name="security" className="chat-menu-item-icon" /><span><strong>Security workspace</strong><small>Scope, findings and validation</small></span>
                  <em>{showSecurityPanel ? 'Open' : 'Security'}</em>
                </button>
                <button
                  className={showIndexPanel ? 'active' : ''}
                  onClick={() => {
                    setShowIndexPanel(value => {
                      localStorage.setItem('mauler.chat.indexPanel', String(!value))
                      return !value
                    })
                    setToolbarMenu(null)
                  }}
                  role="menuitem"
                >
                  <UiIcon name="files" className="chat-menu-item-icon" /><span><strong>Workspace files</strong><small>Searchable repository knowledge</small></span>
                  <em>{repositoryIndexLabel(repositoryIndex)}</em>
                </button>
                <button
                  className={`${showBrowserPanel ? 'active' : ''} ${browserStatus?.active ? 'live' : ''}`}
                  onClick={() => {
                    setShowBrowserPanel(value => !value)
                    setToolbarMenu(null)
                  }}
                  role="menuitem"
                >
                  <UiIcon name="browser" className="chat-menu-item-icon" /><span><strong>Native browser</strong><small>Visible session and takeover controls</small></span>
                  <em>{browserStatusLabel(browserStatus)}</em>
                </button>
              </div>
            )}
          </div>
          <div className={`chat-toolbar-menu ${toolbarMenu === 'view' ? 'open' : ''}`}>
            <button className={(!followOutput || showSearch || transcriptMode !== 'replies') ? 'active' : ''} onClick={() => setToolbarMenu(value => value === 'view' ? null : 'view')} aria-haspopup="menu" aria-expanded={toolbarMenu === 'view'}>
              <UiIcon name="view" />
              <span>View</span>
              <small>{followOutput ? (transcriptMode === 'custom' ? 'Custom' : transcriptMode === 'complete' ? 'Complete' : 'Replies') : 'Paused'}</small>
              <b aria-hidden="true">⌄</b>
            </button>
            {toolbarMenu === 'view' && (
              <div className="chat-toolbar-popover chat-view-popover" role="menu" aria-label="Chat view">
                <div className="chat-toolbar-popover-heading"><strong>View</strong><span>Control the conversation display</span></div>
                <button onClick={() => {
                  setShowSearch(value => {
                    if (!value) window.setTimeout(() => searchRef.current?.focus(), 0)
                    return !value
                  })
                  setToolbarMenu(null)
                }} role="menuitem">
                  <UiIcon name="search" className="chat-menu-item-icon" /><span><strong>Search conversation</strong><small>Find visible messages and events</small></span>
                  <kbd>Ctrl+F</kbd>
                </button>
                <button className={followOutput ? 'active' : 'paused'} onClick={() => storeFollowOutput(!followOutput)} role="menuitemcheckbox" aria-checked={followOutput}>
                  <UiIcon name="follow" className="chat-menu-item-icon" /><span><strong>Follow new output</strong><small>{followOutput ? 'Chat stays on the latest activity' : 'Reading position is preserved'}</small></span>
                  <em>{followOutput ? 'On' : 'Paused'}</em>
                </button>
                <div className="chat-toolbar-section-label">Transcript detail</div>
                <div className="chat-toolbar-choice" role="group" aria-label="Transcript detail">
                  <button className={transcriptMode === 'complete' ? 'active' : ''} onClick={() => changeTranscriptMode('complete')}>Complete</button>
                  <button className={transcriptMode === 'replies' ? 'active' : ''} onClick={() => changeTranscriptMode('replies')}>Replies only</button>
                </div>
                <div className="chat-toolbar-section-label">Show activity</div>
                <div className="chat-toolbar-checks">
                  <button className={transcriptFilters.tools ? 'active' : ''} onClick={() => toggleTranscriptFilter('tools')} aria-pressed={transcriptFilters.tools}>Tools</button>
                  <button className={transcriptFilters.status ? 'active' : ''} onClick={() => toggleTranscriptFilter('status')} aria-pressed={transcriptFilters.status}>Run status</button>
                  <button className={transcriptFilters.browser ? 'active' : ''} onClick={() => toggleTranscriptFilter('browser')} aria-pressed={transcriptFilters.browser}>Browser</button>
                </div>
              </div>
            )}
          </div>
          <div className={`chat-toolbar-menu chat-run-mode-menu ${toolbarMenu === 'run' ? 'open' : ''}`}>
            <button className={conversationMode !== 'adaptive' ? 'active' : ''} onClick={() => setToolbarMenu(value => value === 'run' ? null : 'run')} aria-haspopup="menu" aria-expanded={toolbarMenu === 'run'}>
              <UiIcon name="run" />
              <span>Run mode</span><small>{conversationMode === 'direct' ? 'Direct' : conversationMode === 'agent' ? 'Agent' : 'Adaptive'}</small><b aria-hidden="true">⌄</b>
            </button>
            {toolbarMenu === 'run' && (
              <div className="chat-toolbar-popover" role="menu" aria-label="Run mode">
                <div className="chat-toolbar-popover-heading"><strong>Conversation mode</strong><span>Saved with this conversation</span></div>
                <button className={conversationMode === 'adaptive' ? 'active' : ''} onClick={() => { setToolbarMenu(null); void onConversationModeChange('adaptive') }} role="menuitemradio" aria-checked={conversationMode === 'adaptive'}>
                  <span><strong>Adaptive</strong><small>Direct for questions; tools and validation for tasks</small></span>
                  <em>{conversationMode === 'adaptive' ? 'Current' : 'Recommended'}</em>
                </button>
                <button className={conversationMode === 'direct' ? 'active' : ''} onClick={() => { setToolbarMenu(null); void onConversationModeChange('direct') }} role="menuitemradio" aria-checked={conversationMode === 'direct'}>
                  <span><strong>Always direct</strong><small>One text answer with no tools, repair, or reviewer pass</small></span>
                  <em>{conversationMode === 'direct' ? 'Current' : 'Fast'}</em>
                </button>
                <button className={conversationMode === 'agent' ? 'active' : ''} onClick={() => { setToolbarMenu(null); void onConversationModeChange('agent') }} role="menuitemradio" aria-checked={conversationMode === 'agent'}>
                  <span><strong>Always agent</strong><small>Keep planning, recovery, validation, and evidence checks</small></span>
                  <em>{conversationMode === 'agent' ? 'Current' : 'Thorough'}</em>
                </button>
                <button onClick={() => { setToolbarMenu(null); onOpenQuickChat() }} role="menuitemradio" aria-checked="false">
                  <span><strong>Fast chat window</strong><small>Separate lightweight chat without workspace context</small></span>
                  <em>Open</em>
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
      {showSecurityPanel && (
        <ChatSecurityWorkspace
          version={engagementVersion}
          workspaceRoot={workspaceRoot}
          onClose={() => {
            setShowSecurityPanel(false)
            localStorage.setItem('mauler.chat.securityPanel', 'false')
          }}
          onOpenEngagement={onOpenEngagement}
          onPrepareRun={onPrepareEngagementRun}
        />
      )}
      {showIndexPanel && (
        <section className={`chat-index-panel ${repositoryIndex?.active ? 'active' : ''}`} aria-label="Workspace repository index">
          <header>
            <div>
              <span>Workspace knowledge</span>
              <strong title={repositoryIndex?.workspace || workspaceRoot}>{shortPath(repositoryIndex?.workspace || workspaceRoot) || 'Current workspace'}</strong>
            </div>
            <div className="chat-index-panel-state">
              <i aria-hidden="true" />
			  <span>{repositoryIndex?.indexing ? repositoryIndex.status : repositoryIndexBusy ? 'starting' : repositoryIndexLabel(repositoryIndex)}</span>
			  <button onClick={() => void refreshRepositoryIndex()} disabled={repositoryIndexBusy && !repositoryIndex?.indexing} title="Refresh index status">↻</button>
              <button onClick={() => {
                setShowIndexPanel(false)
                localStorage.setItem('mauler.chat.indexPanel', 'false')
              }} title="Close workspace index">×</button>
            </div>
          </header>
          {repositoryIndexError || repositoryIndex?.error ? (
            <p className="chat-index-error">{repositoryIndexError || repositoryIndex?.error}</p>
          ) : (
			<p>Mauler streams supported text, code, readable PDFs and Office documents into bounded chunks; ZIP files contribute a safe inventory only. Unsupported, encrypted, corrupt or over-expanded inputs remain explicit omissions. Content stays untrusted and every search result carries immutable file and chunk hashes.</p>
		  )}
		  {repositoryIndex?.indexing && (
			<div className="chat-index-progress" role="status" aria-live="polite">
			  <div className="chat-index-metrics">
				<div><span>Files seen</span><strong>{repositoryIndex.progress_files_seen.toLocaleString()}</strong></div>
				<div><span>Files indexed</span><strong>{repositoryIndex.progress_files_indexed.toLocaleString()}</strong></div>
				<div><span>Chunks</span><strong>{repositoryIndex.progress_chunk_count.toLocaleString()}</strong></div>
				<div><span>Read</span><strong>{formatByteCount(repositoryIndex.progress_bytes_read)}</strong></div>
			  </div>
			  <small title={repositoryIndex.current_path}>{repositoryIndex.status === 'cancelling' ? 'Cancelling safely…' : repositoryIndex.current_path || 'Preparing scanner…'}</small>
			</div>
		  )}
          <section className="chat-index-sources" aria-label="Knowledge sources">
            <header>
              <div><strong>Knowledge sources</strong><span>Current workspace plus explicit read-only additions</span></div>
              <div>
                <label className="chat-index-watch-toggle" title="Poll metadata and SHA-verify changes before activating a replacement generation">
                  <input type="checkbox" checked={Boolean(repositoryIndex?.watch_enabled)} onChange={event => void updateRepositorySources(() => SetRepositoryIndexWatch(event.target.checked))} disabled={repositorySourceBusy} />
                  Watch
                </label>
                <button onClick={() => void updateRepositorySources(SelectRepositoryIndexFiles)} disabled={repositorySourceBusy || Boolean(repositoryIndex?.indexing)}>+ Files</button>
                <button onClick={() => void updateRepositorySources(SelectRepositoryIndexFolder)} disabled={repositorySourceBusy || Boolean(repositoryIndex?.indexing)}>+ Folder</button>
              </div>
            </header>
            <div className="chat-index-source-root"><UiIcon name="workspace" /><span><strong>Workspace</strong><small title={repositoryIndex?.workspace || workspaceRoot}>{repositoryIndex?.workspace || workspaceRoot}</small></span><em>Required</em></div>
            {(repositoryIndex?.sources ?? []).map(source => (
              <div className="chat-index-source-root" key={`${source.kind}:${source.path}`}>
                <UiIcon name={source.kind === 'folder' ? 'workspace' : 'files'} />
                <span><strong>{source.kind === 'folder' ? 'Folder' : 'File'}</strong><small title={source.path}>{source.path}</small></span>
                <button title={`Remove ${source.path} from future indexes`} onClick={() => void updateRepositorySources(() => RemoveRepositoryIndexSource(source.path))} disabled={repositorySourceBusy || Boolean(repositoryIndex?.indexing)}>Remove</button>
              </div>
            ))}
          </section>
          {repositoryIndex?.watch_enabled && (
            <div className={`chat-index-watch-state ${repositoryIndex.watch_state === 'error' ? 'error' : ''}`}>
              <i aria-hidden="true" />
              <span><strong>{repositoryIndex.watch_state?.replaceAll('_', ' ') || 'starting'}</strong>{repositoryIndex.watch_error || (repositoryIndex.watch_last_check ? `Last checked ${new Date(repositoryIndex.watch_last_check).toLocaleTimeString()}` : 'Waiting for first check')}</span>
            </div>
          )}
          {repositoryIndex?.active ? (
            <>
              <div className="chat-index-metrics">
                <div><span>Files indexed</span><strong>{repositoryIndex.files_indexed.toLocaleString()} / {repositoryIndex.files_seen.toLocaleString()}</strong></div>
                <div><span>Search chunks</span><strong>{repositoryIndex.chunk_count.toLocaleString()}</strong></div>
                <div><span>Read</span><strong>{formatByteCount(repositoryIndex.bytes_read)}</strong></div>
                <div className={repositoryIndex.omission_count ? 'warn' : ''}><span>Explicit omissions</span><strong>{repositoryIndex.omission_count.toLocaleString()}</strong></div>
              </div>
              <div className="chat-index-provenance">
                <span title={repositoryIndex.generation_id}>Generation {shortDigest(repositoryIndex.generation_id)}</span>
                <span title={repositoryIndex.manifest_digest}>Manifest {shortDigest(repositoryIndex.manifest_digest)}</span>
                <span>{repositoryIndex.completed_at ? new Date(repositoryIndex.completed_at).toLocaleString() : 'Complete'}</span>
              </div>
              {repositoryIndex.refresh_mode === 'incremental' && (
                <div className="chat-index-refresh-summary">
                  <span><strong>{repositoryIndex.files_reused.toLocaleString()}</strong> hash-verified reused</span>
                  <span><strong>{repositoryIndex.files_changed.toLocaleString()}</strong> changed/new</span>
                  <span><strong>{repositoryIndex.files_deleted.toLocaleString()}</strong> deleted</span>
                </div>
              )}
              {repositoryIndex.omissions.length > 0 && (
                <details className="chat-index-omissions">
                  <summary>Review {repositoryIndex.omission_count.toLocaleString()} non-indexed entr{repositoryIndex.omission_count === 1 ? 'y' : 'ies'}</summary>
                  <div>
                    {repositoryIndex.omissions.map((item, index) => (
                      <article key={`${item.path}-${item.status}-${index}`}>
                        <strong title={item.path}>{item.path || '(workspace root)'}</strong>
                        <span>{item.status.replaceAll('_', ' ')}</span>
                        {item.detail && <small>{item.detail}</small>}
                      </article>
                    ))}
                    {repositoryIndex.omissions_truncated && <p>Only the first {repositoryIndex.omissions.length.toLocaleString()} notices are shown here; the manifest retains the complete count.</p>}
                  </div>
                </details>
              )}
            </>
          ) : (
            <div className="chat-index-empty">
              <strong>No complete index for this workspace</strong>
              <span>Create one before asking Mauler to search or review a large repository.</span>
            </div>
          )}
          <footer>
			<span>{repositoryIndexBusy || repositoryIndex?.indexing ? 'Scanning without loading whole files into Chat…' : 'Generated/dependency directories and unsupported files are reported, never silently counted as covered.'}</span>
			{repositoryIndex?.indexing ? (
			  <button className="danger" onClick={() => void cancelRepositoryIndex()} disabled={!repositoryIndex.can_cancel}>{repositoryIndex.can_cancel ? 'Cancel scan' : 'Cancelling…'}</button>
			) : (
			  <div className="chat-index-footer-actions">
                {repositoryIndex?.active && <button onClick={() => void refreshChangedRepositoryFiles()} disabled={repositoryIndexBusy || repositoryIndex?.available === false}>Refresh changes</button>}
                <button className="primary" onClick={() => void rebuildRepositoryIndex()} disabled={repositoryIndexBusy || repositoryIndex?.available === false}>
				  {repositoryIndexBusy ? 'Starting…' : repositoryIndex?.active ? 'Full rebuild' : 'Index workspace'}
			    </button>
              </div>
			)}
          </footer>
        </section>
      )}
      {showBrowserPanel && (
        <section className={`chat-browser-panel ${browserStatus?.active ? 'active' : ''} ${browserStatus?.paused ? 'paused' : ''}`} aria-label="Native browser controls">
          <header>
            <div>
              <span>Native browser</span>
              <strong>{browserStatus?.title || (browserStatus?.available ? 'Ready for a visible session' : 'Browser unavailable')}</strong>
            </div>
            <div className="chat-browser-panel-state">
              <i aria-hidden="true" />
              <span>{browserStatusLabel(browserStatus)}</span>
              <button onClick={() => void runBrowserAction(refreshBrowserStatus)} disabled={browserBusy} title="Refresh browser status">↻</button>
              <button onClick={() => setShowBrowserPanel(false)} title="Close browser controls">×</button>
            </div>
          </header>
          <p>{browserError || browserStatus?.last_error || browserStatus?.guidance || 'Open a visible Chrome or Edge session for this conversation.'}</p>
          <div className="chat-browser-location">
            <input
              value={browserURL}
              onChange={event => setBrowserURL(event.target.value)}
              placeholder="https://example.com/login"
              aria-label="Browser URL"
              disabled={browserBusy || Boolean(browserStatus?.active)}
              onKeyDown={event => {
                if (event.key === 'Enter' && browserURL.trim() && !browserStatus?.active && !browserBusy) {
                  event.preventDefault()
                  void runBrowserAction(() => StartBrowserWorkflow(browserURL.trim()))
                }
              }}
            />
            {!browserStatus?.active && (
              <button
                className="primary"
                onClick={() => void runBrowserAction(() => StartBrowserWorkflow(browserURL.trim()))}
                disabled={browserBusy || !browserURL.trim() || !browserStatus?.available || !browserStatus?.tool_enabled}
              >{browserBusy ? 'Opening…' : 'Open visible browser'}</button>
            )}
          </div>
          {browserStatus?.active && (
            <div className="chat-browser-session">
              <div>
                <span>Current page</span>
                <strong title={browserStatus.url}>{browserStatus.url || 'Waiting for page URL'}</strong>
                <small>{browserStatus.active_tab || 't1'} · {browserStatus.tab_count || 1} tab{(browserStatus.tab_count || 1) === 1 ? '' : 's'} · conversation-only session</small>
              </div>
              <div className="chat-browser-actions">
                {!browserStatus.visible && (
                  <button
                    className="primary"
                    onClick={() => void runBrowserAction(async () => {
                      const reopenURL = browserStatus.url || browserURL
                      await onStopBrowser()
                      await StartBrowserWorkflow(reopenURL)
                    })}
                    disabled={browserBusy || !(browserStatus.url || browserURL)}
                  >Restart visible</button>
                )}
                <button
                  onClick={() => void runBrowserAction(TakeOverBrowserWorkflow)}
                  disabled={browserBusy || browserStatus.paused || !browserStatus.visible}
                >Take over</button>
                <button
                  className="primary"
                  onClick={() => void runBrowserAction(async () => browserHandoff?.active ? onResumeBrowser() : ResumeBrowserWorkflow())}
                  disabled={browserBusy || !browserStatus.paused}
                >I’ve completed this—continue</button>
                <button
                  className="danger"
                  onClick={() => void runBrowserAction(onStopBrowser)}
                  disabled={browserBusy}
                >Stop</button>
              </div>
            </div>
          )}
          <footer>Chrome/Edge opens in its own window. Cookies and page state belong only to this conversation; typed values are not returned to Chat.</footer>
        </section>
      )}
      {showSearch && (
        <div className="chat-search-bar">
          <input
            ref={searchRef}
            className="chat-search-input"
            placeholder="Search messages..."
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Escape') { setShowSearch(false); setSearchQuery('') }
            }}
          />
          <span className="chat-search-count">
            {searchQuery.trim() ? `${visibleMessages.length} / ${transcriptMessages.length}` : ''}
          </span>
          <button className="chat-search-close" onClick={() => { setShowSearch(false); setSearchQuery('') }}>x</button>
        </div>
      )}
      <div
        ref={messagesRef}
        className="chat-messages"
        onWheel={event => {
          if (event.deltaY < 0 && followOutput) storeFollowOutput(false)
        }}
        onTouchMove={() => {
          if (followOutput) storeFollowOutput(false)
        }}
      >
        {messages.length === 0 && !streaming && (
          <div className="chat-empty">
            <div className="chat-empty-logo">M</div>
            <h1>What are we working on?</h1>
            <p>Ask anything, attach files, or give Mauler a task to complete in your workspace.</p>
            <div className="chat-empty-prompts">
              <button onClick={() => { setInput('Summarise this workspace, identify the important files and evidence, and suggest the most useful next step.'); inputRef.current?.focus() }}>
                <UiIcon name="files" /><span><strong>Review workspace</strong><small>Understand files, evidence, and current state</small></span><em>Draft →</em>
              </button>
              <button onClick={() => { setInput('Review the latest run, explain what happened, and continue safely without repeating completed work.'); inputRef.current?.focus() }}>
                <UiIcon name="run" /><span><strong>Continue latest run</strong><small>Resume from recorded state and evidence</small></span><em>Draft →</em>
              </button>
              <button onClick={() => { setInput('Help me plan this task before making changes: '); inputRef.current?.focus() }}>
                <UiIcon name="tools" /><span><strong>Plan a task</strong><small>Agree the approach before tools or edits</small></span><em>Draft →</em>
              </button>
              <button className="security" onClick={() => { setShowSecurityPanel(true); localStorage.setItem('mauler.chat.securityPanel', 'true') }}>
                <UiIcon name="security" /><span><strong>Security assessment</strong><small>Scope, test coverage, findings, and validation</small></span><em>Open →</em>
              </button>
            </div>
            <div className="chat-empty-context">
              <span>{activeProfile || 'Local model'}</span>
              <span>{autonomous ? 'Autonomous' : 'Supervised'}</span>
              <span title={workspaceRoot}>{shortPath(workspaceRoot) || 'No workspace'}</span>
              <span>{formatContext(historyStats)}</span>
            </div>
          </div>
        )}
        {messages.length === 0 && streaming && !thinkingBuffer.trim() && !streamBuffer.trim() && (
          <div className="chat-active-run" role="status" aria-live="polite">
            <span className="chat-active-run-pulse" aria-hidden="true" />
            <div>
              <strong>Run still active</strong>
              <span>{runState?.detail || activity.find(item => item.status === 'running')?.name || 'Waiting for the next agent update'}</span>
            </div>
          </div>
        )}
        {visibleMessages.map(msg => (
          <MessageBubble
            key={msg.id}
            msg={msg}
            onCodeBlock={handleCodeBlock}
            onImageClick={setLightboxImage}
            onAttachmentOpen={att => openAttachment(att, 'message')}
            onReadResult={(id) => {
              setInput(`Use read_tool_result to read result_id=${id} offset=0 limit=8000`)
              setTimeout(() => inputRef.current?.focus(), 0)
            }}
          />
        ))}

        {(streaming && (thinkingBuffer.trim() || streamBuffer.trim())) && (
          <LiveModelBubble thinking={thinkingBuffer} content={streamBuffer} />
        )}

        <div ref={bottomRef} />
      </div>

      {videoStatus && (
        <div className="video-status">
          <span className="video-status-spinner" /> {videoStatus}
        </div>
      )}

      {/* Image previews */}
      {images.length > 0 && (
        <div className="image-previews">
          {images.map((src, i) => (
            <div key={i} className="image-preview-wrapper">
              <img src={src} alt="pasted" className="image-preview" />
              <button className="image-remove" onClick={() => removeImage(i)}>x</button>
            </div>
          ))}
        </div>
      )}

      {lightboxImage && (
        <button className="image-lightbox" onClick={() => setLightboxImage(null)} title="Close image preview">
          <img src={lightboxImage} alt="chat attachment preview" />
        </button>
      )}

      {attachmentEditor && (
        <div className="attachment-dialog-backdrop" onMouseDown={event => {
          if (event.target === event.currentTarget) closeAttachment()
        }}>
          <section className="attachment-dialog" role="dialog" aria-modal="true" aria-label={`Attachment ${attachmentEditor.attachment.name}`}>
            <header className="attachment-dialog-header">
              <div>
                <strong>{attachmentEditor.attachment.name}</strong>
                <span>{attachmentSubtitle(attachmentEditor.attachment)}{attachmentEditor.attachment.truncated ? ' / truncated' : ''}</span>
              </div>
              <button onClick={closeAttachment} title="Close attachment">Close</button>
            </header>
            <textarea
              className="attachment-dialog-editor"
              value={attachmentEditorText}
              onChange={event => setAttachmentEditorText(event.target.value)}
              readOnly={!attachmentIsEditable(attachmentEditor.attachment)}
              spellCheck={false}
              aria-label="Attachment contents"
            />
            <footer className="attachment-dialog-actions">
              <span>
                {attachmentEditorText.length.toLocaleString()} characters
                {attachmentEditor.source === 'message' && attachmentIsEditable(attachmentEditor.attachment)
                  ? ' / edits become a new draft attachment'
                  : ''}
              </span>
              <div>
                <button onClick={() => void copyAttachment()}>{attachmentCopied ? 'Copied' : 'Copy all'}</button>
                {attachmentIsEditable(attachmentEditor.attachment) && (
                  <button className="primary" onClick={saveAttachmentEdit}>
                    {attachmentEditor.source === 'draft' ? 'Save changes' : 'Add edited copy to draft'}
                  </button>
                )}
              </div>
            </footer>
          </section>
        </div>
      )}

      <div className="chat-input-area">
        {browserHandoff?.active && (
          <section className="browser-takeover-card" aria-live="assertive" aria-label="Browser waiting for you">
            <div className="browser-takeover-copy">
              <div className="browser-takeover-heading">
                <span className="browser-takeover-badge">Waiting for you</span>
                <strong>{browserHandoff.title || 'Browser takeover'}</strong>
              </div>
              {browserHandoff.url && <div className="browser-takeover-url" title={browserHandoff.url}>{browserHandoff.url}</div>}
              <p>{browserHandoff.guidance || 'Complete the manual step in the visible browser. This same task is paused and will continue from a fresh page observation.'}</p>
            </div>
            <div className="browser-takeover-actions">
              <button type="button" className="primary" onClick={() => void onResumeBrowser()}>
                I’ve completed this—continue
              </button>
              <button type="button" onClick={() => void onStopBrowser()}>Stop browser</button>
            </div>
          </section>
        )}
        {pendingInterrupt && (
          <div className="chat-pending-interrupt">
            <span>Interrupting current run. Your next message will send as soon as it stops.</span>
            <button onClick={onCancelPending}>Cancel</button>
          </div>
        )}
        <div className={`chat-context-toolbar ${streaming ? 'running' : ''}`} aria-label="Task context and run controls">
          <div className="run-popover-wrap">
            <button
              ref={planTriggerRef}
              type="button"
              className={`context-toolbar-button ${openPopover === 'plan' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'plan' ? null : 'plan')}
              title="Show the active task plan"
            >
              <UiIcon name="plan" />
              <span><strong>Plan</strong><small>{todoSummary(todos)}</small></span>
            </button>
            {openPopover === 'plan' && (
              <ComposerPopoverPortal anchor={planTriggerRef.current} width={600}>
                <PlanPopover todos={todos} onClear={onClearPlan} />
              </ComposerPopoverPortal>
            )}
          </div>
          <div className="run-popover-wrap">
            <button
              ref={toolsTriggerRef}
              type="button"
              className={`context-toolbar-button ${openPopover === 'tools' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'tools' ? null : 'tools')}
              title="Show current tool/run state"
            >
              <UiIcon name="tools" />
              <span><strong>Tools</strong><small>{toolSummary(activity, toolCountdown)}</small></span>
            </button>
            {openPopover === 'tools' && (
              <ComposerPopoverPortal anchor={toolsTriggerRef.current} width={580}>
                <ToolboxPopover
                  activity={activity}
                  countdown={toolCountdown}
                  runState={runState}
                  profile={activeProfile}
                  context={formatContext(historyStats)}
                  workspace={workspaceRoot}
                  autonomous={autonomous}
                  channelStatus={channelStatus}
                  channelQueue={channelQueue}
                  terminalState={terminalState}
                />
              </ComposerPopoverPortal>
            )}
          </div>
          <div className="run-popover-wrap run-workspace-wrap">
            <button
              ref={workspaceTriggerRef}
              type="button"
              className={`context-toolbar-button ${openPopover === 'workspace' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'workspace' ? null : 'workspace')}
              title={workspaceRoot || 'Choose the active workspace'}
            >
              <UiIcon name="workspace" />
              <span><strong>Workspace</strong><small>{shortPath(workspaceRoot) || 'Choose'}</small></span>
            </button>
            {openPopover === 'workspace' && (
              <ComposerPopoverPortal anchor={workspaceTriggerRef.current} width={620}>
                <WorkspacePopover
                  current={workspaceRoot}
                  options={workspaceOptions}
                  scratch={scratchWorkspace}
                  disabled={streaming}
                  onChoose={async bugBounty => {
                    setOpenPopover(null)
                    await onChooseWorkspace(bugBounty)
                  }}
                  onSwitch={async path => {
                    setOpenPopover(null)
                    await onSwitchWorkspace(path)
                  }}
                  onCreateScratch={async () => {
                    setOpenPopover(null)
                    await onCreateScratchWorkspace()
                  }}
                  onCreateProject={async (name, bugBounty) => {
                    setOpenPopover(null)
                    await onCreateWorkspaceProject(name, bugBounty)
                  }}
                  onPromoteScratch={async name => {
                    await onPromoteScratchWorkspace(name)
                    setOpenPopover(null)
                  }}
                  onManage={() => {
                    setOpenPopover(null)
                    onOpenProjects()
                  }}
                />
              </ComposerPopoverPortal>
            )}
          </div>
          <div className="run-popover-wrap run-agent-wrap">
            <button
              ref={agentTriggerRef}
              type="button"
              className={`context-toolbar-button ${openPopover === 'agent' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'agent' ? null : 'agent')}
              title={streaming ? `Active task route: ${agentMode || agentSelection || 'Auto'}` : 'Choose the agent remembered for this workspace'}
            >
              <UiIcon name="agent" />
              <span><strong>{streaming ? 'Route' : 'Agent'}</strong><small>{streaming ? (agentMode || agentSelection || 'Auto') : (agentSelection || 'Auto')}</small></span>
            </button>
            {openPopover === 'agent' && (
              <ComposerPopoverPortal anchor={agentTriggerRef.current} width={620}>
                <AgentPopover
                  definitions={agentDefinitions}
                  selected={agentSelection || 'Auto'}
                  disabled={streaming}
                  onSelect={async mode => {
                    setOpenPopover(null)
                    await onAgentSelectionChange(mode)
                  }}
                />
              </ComposerPopoverPortal>
            )}
          </div>
          <div className="run-popover-wrap">
            <button
              ref={runSetupTriggerRef}
              type="button"
              className={`context-toolbar-button context-toolbar-run ${openPopover === 'runSetup' ? 'active' : ''}`}
              onClick={() => setOpenPopover(value => value === 'runSetup' ? null : 'runSetup')}
              title="Choose the next-task model and confirmation mode"
            >
              <UiIcon name="run" />
              <span><strong>Run setup</strong><small>{runProfileOverride ? 'Cloud once' : autonomous ? 'Automatic' : 'Supervised'}</small></span>
            </button>
            {openPopover === 'runSetup' && (
              <ComposerPopoverPortal anchor={runSetupTriggerRef.current} width={560}>
                <RunSetupPopover
                  activeProfile={activeProfile}
                  activeRunProfile={activeRunProfile}
                  cloudRunProfiles={cloudRunProfiles}
                  runProfileOverride={runProfileOverride}
                  autonomous={autonomous}
                  streaming={streaming}
                  toolCountdown={toolCountdown}
                  nowMs={nowMs}
                  onProfileChange={onRunProfileOverrideChange}
                  onAutonomousChange={onAutonomousChange}
                  onOpenSettings={onOpenSettings}
                />
              </ComposerPopoverPortal>
            )}
          </div>
          {streaming && <span className="context-toolbar-live" title={runState?.detail || 'Working'}>{runState?.detail || 'Working'}</span>}
        </div>
        <div className="chat-input-card">
          {attachments.length > 0 && (
            <div className="attachment-previews">
              {attachments.map(att => (
                <AttachmentChip
                  key={att.id}
                  attachment={att}
                  onOpen={() => openAttachment(att, 'draft')}
                  onRemove={() => removeAttachment(att.id)}
                />
              ))}
            </div>
          )}
          <textarea
            ref={inputRef}
            className="chat-input"
            value={input}
            onChange={e => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            onDrop={e => void handleDrop(e)}
            onDragOver={e => e.preventDefault()}
            placeholder="Ask anything... attach, paste a file path, or drop files here. Enter sends; Ctrl+Enter adds a new line."
            rows={4}
            disabled={false}
            spellCheck
            lang="en"
            autoCapitalize="sentences"
          />
          <div className="chat-input-actions">
            <button className="composer-icon-btn composer-attach-btn" onClick={() => void chooseChatFiles()} title="Attach files" aria-label="Attach files">
              ＋
            </button>
            <div className="composer-reasoning-wrap">
              <button
                ref={reasoningTriggerRef}
                type="button"
                className={`composer-reasoning-trigger mode-${normaliseComposerThinkingMode(thinkingMode)}${openPopover === 'reasoning' ? ' active' : ''}`}
                onClick={() => setOpenPopover(value => value === 'reasoning' ? null : 'reasoning')}
                disabled={streaming}
                title={`Thinking and effort for the next run · ${activeModelID || activeProfile || 'active model'}`}
                aria-haspopup="dialog"
                aria-expanded={openPopover === 'reasoning'}
              >
                <span aria-hidden="true">✦</span>
                <strong>{composerReasoningLabel(thinkingMode, reasoningEffort)}</strong>
                <small>{compactModelLabel(activeModelID || activeProfile)}</small>
              </button>
              {openPopover === 'reasoning' && (
                <ComposerPopoverPortal anchor={reasoningTriggerRef.current} width={470}>
                  <ReasoningPopover
                    modelID={activeModelID || activeProfile}
                    supported={thinkingSupported}
                    thinkingMode={thinkingMode}
                    reasoningEffort={reasoningEffort}
                    disabled={streaming || reasoningBusy}
                    error={reasoningError}
                    onChange={(mode, effort) => void applyReasoningControl(mode, effort)}
                    onClose={() => setOpenPopover(null)}
                  />
                </ComposerPopoverPortal>
              )}
            </div>
            <button
              className={`composer-voice-btn ${recording ? 'recording' : voiceSession ? 'active' : ''}`}
              onClick={() => recording ? stopRecording() : void startRecording()}
              disabled={!audioConfig?.enabled}
              title={recording ? 'Stop recording and send' : 'Talk to Mauler'}
            >
              {recording ? 'Send voice' : 'Voice'}
            </button>
            {voiceSession && (
              <button className="composer-voice-btn active" onClick={() => { setVoiceSession(false); stopSpeech(); setVoiceStatus('') }} title="Turn spoken replies off">
                Voice on
              </button>
            )}
            {streaming && <button className="composer-stop-btn danger" onClick={onStopAgent} title="Stop the current run">Stop</button>}
            <details className="composer-more-menu" ref={composerMoreRef}>
              <summary title="More composer actions" aria-label="More composer actions"><UiIcon name="more" /></summary>
              <div>
                <span className="composer-menu-label">Task surfaces</span>
                <button onClick={() => { setShowSecurityPanel(true); localStorage.setItem('mauler.chat.securityPanel', 'true'); if (composerMoreRef.current) composerMoreRef.current.open = false }}><UiIcon name="security" /><span><strong>Security workspace</strong><small>Scope, findings, and validation</small></span></button>
                <button onClick={() => { setShowIndexPanel(true); localStorage.setItem('mauler.chat.indexPanel', 'true'); if (composerMoreRef.current) composerMoreRef.current.open = false }}><UiIcon name="files" /><span><strong>Workspace files</strong><small>Repository index and omissions</small></span></button>
                <button onClick={() => { setShowBrowserPanel(true); if (composerMoreRef.current) composerMoreRef.current.open = false }}><UiIcon name="browser" /><span><strong>Native browser</strong><small>Visible session and takeover</small></span></button>
                <span className="composer-menu-label divided">Conversation</span>
                <button className="composer-undo-btn" onClick={() => { void Undo(); if (composerMoreRef.current) composerMoreRef.current.open = false }} title="Undo last file change (Ctrl+Z)"><UiIcon name="undo" /><span><strong>Undo last file edit</strong><small>Restore the previous workspace change</small></span></button>
                <button className="chat-clear-btn danger" onClick={() => { onClearChat(); if (composerMoreRef.current) composerMoreRef.current.open = false }} title="Clear chat history (Ctrl+K)" disabled={streaming}><UiIcon name="trash" /><span><strong>Clear conversation</strong><small>Start again without deleting workspace files</small></span></button>
              </div>
            </details>
            <button
              className={`primary composer-icon-btn composer-send-btn ${streaming ? 'interrupt' : ''}`}
              onClick={() => void handleSend()}
              disabled={(!input.trim() && images.length === 0 && attachments.length === 0) || pendingInterrupt}
              title={streaming ? 'Interrupt the current run and send this draft' : 'Send'}
            >
              <span aria-hidden="true">↑</span>
            </button>
          </div>
          {attachmentStatus && <div className="composer-attachment-status" role="status">{attachmentStatus}</div>}
          {voiceStatus && <div className="composer-voice-status" role="status">{voiceStatus}</div>}
        </div>
      </div>
    </div>
  )
}

function blobToDataURI(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error || new Error('Could not read recording'))
    reader.readAsDataURL(blob)
  })
}

// Windows Explorer commonly places copied files on the clipboard as an
// absolute path string in WebView2. Only treat the clipboard as files when
// every non-empty line is an absolute path/URL; ordinary prose remains prose.
function clipboardFilePathCandidates(raw: string): string[] {
  const text = raw.trim()
  if (!text || text.length > 16_000) return []
  const candidates = text.split(/\r?\n/).map(line => line.trim()).filter(Boolean)
  if (candidates.length === 0 || candidates.length > 20) return []
  const stripQuotes = (value: string) => value.replace(/^["']|["']$/g, '')
  const paths = candidates.map(stripQuotes)
  const isAbsoluteFilePath = (value: string) => (
    /^[a-zA-Z]:[\\/]/.test(value)
    || /^\\\\[^\\]+\\[^\\]+/.test(value)
    || /^\/mnt\/[a-zA-Z]\//.test(value)
    || /^file:\/\//i.test(value)
  )
  return paths.every(isAbsoluteFilePath) ? paths : []
}

function takeSpeechChunks(input: string, minChars: number, flush: boolean): { chunks: string[]; rest: string } {
  const chunks: string[] = []
  let start = 0
  for (let i = 0; i < input.length; i += 1) {
    const length = i + 1 - start
    const boundary = /[.!?;\n]/.test(input[i])
    const softBoundary = length >= 150 && /[,,:]/.test(input[i])
    const hardBoundary = length >= 230 && /\s/.test(input[i])
    if ((boundary && length >= minChars) || softBoundary || hardBoundary) {
      const chunk = input.slice(start, i + 1).trim()
      if (chunk) chunks.push(chunk)
      start = i + 1
    }
  }
  const rest = input.slice(start)
  if (flush && rest.trim()) {
    chunks.push(rest.trim())
    return { chunks, rest: '' }
  }
  return { chunks, rest }
}

function todoSummary(todos: TodoItem[]): string {
  if (!Array.isArray(todos)) return 'none'
  if (todos.length === 0) return 'none'
  const done = todos.filter(t => t.status === 'done').length
  const blocked = todos.filter(t => t.status === 'blocked').length
  if (blocked > 0) return `${done}/${todos.length}, ${blocked} blocked`
  return `${done}/${todos.length}`
}

function todoDisplayText(value: string): string {
  return String(value || '')
    .replace(/^\s*\[[*x X]?\]\s*/u, '')
    .replace(/^TODO[- _]?\d+\s*:\s*/iu, '')
    .trim()
}

function todoStatusLabel(value: string): string {
  switch (String(value || '').trim().toLowerCase()) {
    case 'in_progress': return 'In progress'
    case 'done': return 'Done'
    case 'blocked': return 'Blocked'
    case 'pending': return 'Pending'
    case 'running': return 'Running'
    case 'error': return 'Error'
    case 'denied': return 'Denied'
    default: return titleCase(String(value || 'Ready').replaceAll('_', ' '))
  }
}

function toolSummary(activity: AgentActivity[], countdown: ToolCountdown | null): string {
  if (countdown) return countdown.name
  const running = activity.find(item => item.status === 'running')
  if (running) return running.name
  const last = activity[0]
  return last ? `${last.name} ${last.status}` : 'idle'
}

function browserStatusLabel(status: BrowserWorkflowStatus | null): string {
  if (!status) return 'checking'
  if (!status.tool_enabled) return 'disabled'
  if (!status.available) return 'unavailable'
  if (status.paused) return 'waiting for you'
  if (status.active) return status.visible ? 'visible' : 'headless'
  return 'ready'
}

function repositoryIndexLabel(status: RepositoryIndexStatus | null): string {
  if (!status) return 'checking'
  if (!status.available) return 'unavailable'
  if (!status.active) return 'not indexed'
  return `${status.files_indexed} indexed`
}

function formatByteCount(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, unit)
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`
}

function shortDigest(value?: string): string {
  if (!value) return 'pending'
  return value.length > 14 ? `${value.slice(0, 12)}…` : value
}

function ComposerPopoverPortal({
  anchor,
  width = 520,
  children,
}: {
  anchor: HTMLElement | null
  width?: number
  children: ReactNode
}) {
  const [style, setStyle] = useState<CSSProperties>({ visibility: 'hidden' })

  useLayoutEffect(() => {
    if (!anchor) return
    const update = () => {
      const rect = anchor.getBoundingClientRect()
      const margin = 12
      const gap = 8
      const resolvedWidth = Math.min(width, Math.max(280, window.innerWidth - margin * 2))
      const left = Math.min(
        Math.max(margin, rect.left),
        Math.max(margin, window.innerWidth - resolvedWidth - margin),
      )
      const above = Math.max(0, rect.top - margin - gap)
      const below = Math.max(0, window.innerHeight - rect.bottom - margin - gap)
      if (below > above) {
        setStyle({ left, top: rect.bottom + gap, width: resolvedWidth, maxHeight: Math.max(140, below) })
      } else {
        setStyle({ left, bottom: window.innerHeight - rect.top + gap, width: resolvedWidth, maxHeight: Math.max(140, above) })
      }
    }
    update()
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [anchor, width])

  return createPortal(
    <div className="composer-popover-portal" style={style}>{children}</div>,
    document.body,
  )
}

function normaliseComposerThinkingMode(value: string): 'auto' | 'on' | 'off' {
  return value === 'on' || value === 'off' ? value : 'auto'
}

function compactModelLabel(value: string): string {
  const clean = String(value || 'Local model').replace(/\.gguf$/i, '')
  return clean.length > 24 ? `${clean.slice(0, 21)}…` : clean
}

function titleCase(value: string): string {
  const clean = String(value || '').trim()
  return clean ? `${clean.charAt(0).toUpperCase()}${clean.slice(1)}` : 'Auto'
}

function composerReasoningLabel(thinkingMode: string, reasoningEffort: string): string {
  const mode = normaliseComposerThinkingMode(thinkingMode)
  if (mode === 'off') return 'Direct'
  if (mode === 'auto') return reasoningEffort && reasoningEffort !== 'auto'
    ? `Profile · ${reasoningEffort === 'xhigh' ? 'XHigh' : titleCase(reasoningEffort)}`
    : 'Profile'
  const effort = reasoningEffort && reasoningEffort !== 'auto' ? reasoningEffort : 'auto'
  return effort === 'xhigh' ? 'Think · XHigh' : `Think · ${titleCase(effort)}`
}

function ReasoningPopover({
  modelID,
  supported,
  thinkingMode,
  reasoningEffort,
  disabled,
  error,
  onChange,
  onClose,
}: {
  modelID: string
  supported: boolean
  thinkingMode: string
  reasoningEffort: string
  disabled: boolean
  error: string
  onChange: (thinkingMode: string, reasoningEffort: string) => void
  onClose: () => void
}) {
  const mode = normaliseComposerThinkingMode(thinkingMode)
  const qwen38 = /qwen3[._-]?8/i.test(modelID)
  const efforts = qwen38
    ? [
      { value: 'auto', label: 'Auto' },
      { value: 'low', label: 'Low' },
      { value: 'medium', label: 'Medium' },
      { value: 'xhigh', label: 'XHigh' },
    ]
    : [
      { value: 'auto', label: 'Auto' },
      { value: 'minimal', label: 'Minimal' },
      { value: 'low', label: 'Low' },
      { value: 'medium', label: 'Medium' },
      { value: 'high', label: 'High' },
      { value: 'xhigh', label: 'XHigh' },
    ]
  const selectedEffort = qwen38 && reasoningEffort === 'high' ? 'xhigh' : (reasoningEffort || 'auto')

  return (
    <div className="composer-popover composer-reasoning-popover" role="dialog" aria-label="Thinking and reasoning effort">
      <div className="composer-popover-head">
        <span>Thinking &amp; effort</span>
        <div>
          <strong title={modelID}>{compactModelLabel(modelID)}</strong>
          <button type="button" onClick={onClose} aria-label="Close thinking controls">Close</button>
        </div>
      </div>
      <p className="composer-popover-note">Choose how the next run starts. The model may still adjust effort during a task, but it cannot override an explicit Direct or Always think choice.</p>
      <section className="reasoning-control-section">
        <div className="reasoning-control-heading">
          <div><strong>Thinking</strong><span>Template behaviour for the complete run</span></div>
          <small>{mode === 'auto' ? 'Recommended' : mode === 'on' ? 'Pinned on' : 'Pinned off'}</small>
        </div>
        <div className="reasoning-mode-picker" role="radiogroup" aria-label="Thinking mode">
          <button type="button" role="radio" aria-checked={mode === 'auto'} className={mode === 'auto' ? 'active' : ''} disabled={disabled} onClick={() => onChange('auto', reasoningEffort || 'auto')}>
            <strong>Profile</strong><span>Adaptive</span>
          </button>
          <button type="button" role="radio" aria-checked={mode === 'on'} className={mode === 'on' ? 'active' : ''} disabled={disabled || !supported} onClick={() => onChange('on', reasoningEffort || 'auto')}>
            <strong>Always think</strong><span>Preserve reasoning</span>
          </button>
          <button type="button" role="radio" aria-checked={mode === 'off'} className={mode === 'off' ? 'active direct' : 'direct'} disabled={disabled} onClick={() => onChange('off', reasoningEffort || 'auto')}>
            <strong>Direct</strong><span>No thinking</span>
          </button>
        </div>
        {!supported && <p className="reasoning-capability-note warn">This profile does not advertise thinking support. Profile and Direct remain available; Mauler will not invent an unsupported model capability.</p>}
      </section>
      <section className="reasoning-control-section">
        <div className="reasoning-control-heading">
          <div><strong>Starting effort</strong><span>{mode === 'off' ? 'Direct mode ignores effort' : 'Depth before the agent adapts'}</span></div>
          <small>{selectedEffort === 'xhigh' ? 'XHigh' : titleCase(selectedEffort)}</small>
        </div>
        <div className={`reasoning-effort-picker${mode === 'off' ? ' disabled' : ''}`} role="radiogroup" aria-label="Reasoning effort">
          {efforts.map(effort => (
            <button
              key={effort.value}
              type="button"
              role="radio"
              aria-checked={selectedEffort === effort.value}
              className={selectedEffort === effort.value ? 'active' : ''}
              disabled={disabled || mode === 'off'}
              onClick={() => onChange(mode, effort.value)}
              title={effort.value === 'auto' ? 'Use the selected agent mode default' : `${effort.label} starting reasoning effort`}
            >
              <i aria-hidden="true" />
              <span>{effort.label}</span>
            </button>
          ))}
        </div>
      </section>
      <footer className="reasoning-popover-footer">
        <span>{qwen38 ? 'Qwen3.8 supports Low, Medium, and XHigh; XHigh is the model default for difficult work.' : 'Profile is the safest default for mixed chat, tool, and coding work.'}</span>
        {error && <strong role="alert">{error}</strong>}
      </footer>
    </div>
  )
}

function TaskMenuHeader({
  icon,
  title,
  subtitle,
  status,
  tone = 'neutral',
  actions,
}: {
  icon: UiIconName
  title: string
  subtitle: string
  status?: string
  tone?: 'neutral' | 'live' | 'warn'
  actions?: ReactNode
}) {
  return (
    <header className="task-menu-header">
      <span className="task-menu-header-icon"><UiIcon name={icon} /></span>
      <span className="task-menu-header-copy"><strong>{title}</strong><small>{subtitle}</small></span>
      <span className={`task-menu-header-state ${tone}`}>{status}</span>
      {actions && <div className="task-menu-header-actions">{actions}</div>}
    </header>
  )
}

function RunSetupPopover({
  activeProfile,
  activeRunProfile,
  cloudRunProfiles,
  runProfileOverride,
  autonomous,
  streaming,
  toolCountdown,
  nowMs,
  onProfileChange,
  onAutonomousChange,
  onOpenSettings,
}: {
  activeProfile: string
  activeRunProfile: string
  cloudRunProfiles: RunProfileOption[]
  runProfileOverride: string
  autonomous: boolean
  streaming: boolean
  toolCountdown: ToolCountdown | null
  nowMs: number
  onProfileChange: (profile: string) => void
  onAutonomousChange: (enabled: boolean) => void
  onOpenSettings: () => void
}) {
  return (
    <div className="composer-popover composer-run-setup-popover">
      <TaskMenuHeader
        icon="run"
        title="Run setup"
        subtitle="Model route and confirmation behaviour"
        status={streaming ? 'Locked during run' : 'Applies to next task'}
        tone={streaming ? 'warn' : 'neutral'}
      />
      <p className="composer-popover-note">Choose the model route and how Mauler asks before higher-risk tool actions. Policy and scope checks always remain active.</p>
      <section className="run-setup-section">
        <div className="run-setup-heading"><strong>Model route</strong><small>Cloud choices reset to local after one task</small></div>
        <label className={`run-profile-once ${runProfileOverride ? 'cloud' : 'local'}`}>
          <span>Next task</span>
          <select
            value={runProfileOverride}
            onChange={event => onProfileChange(event.target.value)}
            aria-label="Model for next task"
            disabled={streaming}
          >
            <option value="">Local default · {activeProfile || 'local profile'}</option>
            {cloudRunProfiles.map(profile => (
              <option key={profile.name} value={profile.name}>Cloud once · {profile.model}</option>
            ))}
          </select>
        </label>
        {cloudRunProfiles.length === 0 && (
          <button type="button" className="run-cloud-setup" onClick={onOpenSettings}>Configure an optional cloud boost</button>
        )}
        {activeRunProfile && activeRunProfile !== activeProfile && (
          <div className="run-setup-live"><span>This run</span><strong>{activeRunProfile}</strong></div>
        )}
      </section>
      <section className="run-setup-section">
        <div className="run-setup-heading"><strong>Tool confirmations</strong><small>Choose how independently tools may run</small></div>
        <div className="run-setup-choice" role="radiogroup" aria-label="Run confirmation mode">
          <button type="button" role="radio" aria-checked={!autonomous} className={!autonomous ? 'active' : ''} disabled={streaming} onClick={() => onAutonomousChange(false)}>
            <UiIcon name="agent" /><span><strong>Supervised</strong><small>Ask before protected actions</small></span>
          </button>
          <button type="button" role="radio" aria-checked={autonomous} className={autonomous ? 'active' : ''} disabled={streaming} onClick={() => onAutonomousChange(true)}>
            <UiIcon name="run" /><span><strong>Automatic</strong><small>Use enabled tools without prompts</small></span>
          </button>
        </div>
      </section>
      {toolCountdown && (
        <footer className="run-setup-tool"><span>Active tool</span><strong>{toolCountdown.name}</strong><small>{formatDuration(Math.ceil(Math.max(0, toolCountdown.deadline - nowMs) / 1000))} remaining</small></footer>
      )}
    </div>
  )
}

function PlanPopover({ todos, onClear }: { todos: TodoItem[]; onClear: () => void | Promise<void> }) {
  const [clearing, setClearing] = useState(false)
  const [clearError, setClearError] = useState('')
  const items = Array.isArray(todos) ? todos : []
  const current = items.find(t => t.status === 'in_progress') || items.find(t => t.status === 'blocked') || items.find(t => t.status !== 'done')
  const clearPlan = async () => {
    if (clearing || items.length === 0) return
    setClearing(true)
    setClearError('')
    try {
      await onClear()
    } catch (error) {
      setClearError(String(error))
    } finally {
      setClearing(false)
    }
  }
  const done = items.filter(item => item.status === 'done').length
  const blocked = items.filter(item => item.status === 'blocked').length
  const remaining = current ? items.filter(item => item.id !== current.id) : items
  return (
    <div className="composer-popover composer-plan-popover">
      <TaskMenuHeader
        icon="plan"
        title="Task plan"
        subtitle={items.length === 0 ? 'No steps yet' : `${done} completed · ${items.length - done} remaining`}
        status={blocked > 0 ? `${blocked} blocked` : todoSummary(items)}
        tone={blocked > 0 ? 'warn' : done === items.length && items.length > 0 ? 'live' : 'neutral'}
        actions={<button className="task-menu-danger-action" onClick={() => void clearPlan()} disabled={items.length === 0 || clearing}>{clearing ? 'Clearing…' : 'Clear plan'}</button>}
      />
      <p className="composer-popover-note">Plan state is not evidence. Confirm important claims in Facts, logs, files, or terminal output.</p>
      {clearError && <div className="plan-clear-error" role="alert">Could not clear plan: {clearError}</div>}
      {current && (
        <div className={`plan-current plan-current-${current.status}`}>
          <span className="task-status-chip">Current step</span>
          <strong>{todoDisplayText(current.text)}</strong>
          {current.detail && <small>{current.detail}</small>}
        </div>
      )}
      {items.length > 0 && <div className="task-menu-section-label"><strong>{current ? 'Other steps' : 'Steps'}</strong><span>{remaining.length}</span></div>}
      <div className="plan-popover-list">
        {items.length === 0 ? (
          <div className="empty-popover-row"><strong>No active plan</strong><span>Ask Mauler to plan a multi-step task and its progress will appear here.</span></div>
        ) : remaining.map(item => (
          <div key={item.id} className={`plan-popover-item plan-${item.status}`}>
            <span className="task-status-chip">{todoStatusLabel(item.status)}</span>
            <strong>{todoDisplayText(item.text)}</strong>
            {item.detail && <small>{item.detail}</small>}
          </div>
        ))}
      </div>
    </div>
  )
}

function chatWorkspaceOptions(settings: Settings): ChatWorkspaceOption[] {
  const seen = new Set<string>()
  const options: ChatWorkspaceOption[] = []
  const add = (path: string, name: string, kind: ChatWorkspaceOption['kind']) => {
    const cleanPath = String(path || '').trim().replaceAll('\\', '/')
    if (!cleanPath) return
    const key = cleanPath.toLowerCase().replace(/\/$/, '')
    if (seen.has(key)) return
    seen.add(key)
    options.push({ path: cleanPath, name: String(name || '').trim() || shortPath(cleanPath), kind })
  }
  for (const project of settings.context.lab_profiles ?? []) {
    add(project.workspace_dir, project.name || project.id, 'project')
  }
  for (const folder of settings.context.open_folders ?? []) {
    add(folder.path, folder.name, 'folder')
  }
  add(settings.context.workspace_dir, shortPath(settings.context.workspace_dir), 'folder')
  return options
}

function WorkspacePopover({
  current,
  options,
  scratch,
  disabled,
  onChoose,
  onSwitch,
  onCreateScratch,
  onCreateProject,
  onPromoteScratch,
  onManage,
}: {
  current: string
  options: ChatWorkspaceOption[]
  scratch: ScratchWorkspaceStatus | null
  disabled: boolean
  onChoose: (bugBounty: boolean) => void | Promise<void>
  onSwitch: (path: string) => void | Promise<void>
  onCreateScratch: () => void | Promise<void>
  onCreateProject: (name: string, bugBounty: boolean) => void | Promise<void>
  onPromoteScratch: (name: string) => void | Promise<void>
  onManage: () => void
}) {
  const [promoteName, setPromoteName] = useState(scratch?.name || 'Saved workspace')
  const [projectName, setProjectName] = useState('')
  const [projectBugBounty, setProjectBugBounty] = useState(false)
  const currentKey = current.replaceAll('\\', '/').replace(/\/$/, '').toLowerCase()
  return (
    <div className="composer-popover composer-workspace-popover">
      <TaskMenuHeader
        icon="workspace"
        title="Workspace"
        subtitle="Files and durable context for this chat"
        status={shortPath(current) || 'Not selected'}
        tone={current ? 'live' : 'warn'}
      />
      <p className="composer-popover-note">Attach scratch context to this chat, or switch to a durable workspace. Switching workspaces starts a clean chat; promotion keeps this conversation and every file in place.</p>
      {disabled && <div className="workspace-switch-warning">Stop the active run before changing workspace.</div>}
      <div className="task-menu-section-label"><strong>Start or attach</strong><span>Choose how this chat stores work</span></div>
      <section className="workspace-new-project-card">
        <div>
          <span><UiIcon name="workspace" /><strong>New project</strong></span>
          <small>Create a new folder, register it, and open a clean project chat.</small>
        </div>
        <div className="workspace-new-project-row">
          <input
            value={projectName}
            onChange={event => setProjectName(event.target.value)}
            onKeyDown={event => {
              if (event.key === 'Enter' && projectName.trim() && !disabled) void onCreateProject(projectName.trim(), projectBugBounty)
            }}
            placeholder="Project or client name"
            maxLength={96}
          />
          <button disabled={disabled || !projectName.trim()} onClick={() => void onCreateProject(projectName.trim(), projectBugBounty)}>Choose location…</button>
        </div>
        <label><input type="checkbox" checked={projectBugBounty} onChange={event => setProjectBugBounty(event.target.checked)} disabled={disabled} /> Open with Bug Bounty Hunter</label>
      </section>
      {scratch?.active ? (
        <section className={`scratch-workspace-card${scratch.review_due ? ' review-due' : ''}`}>
          <div className="scratch-workspace-head"><span><UiIcon name="files" />Conversation scratch</span><strong>{scratch.review_due ? 'Review due' : 'Active'}</strong></div>
          <code title={scratch.path}>{shortPath(scratch.path || current)}</code>
          <p>{scratch.retention_policy}. Review {scratch.review_after_unix ? new Date(scratch.review_after_unix * 1000).toLocaleDateString() : 'when finished'}.</p>
          <div className="scratch-promote-row">
            <input value={promoteName} onChange={event => setPromoteName(event.target.value)} placeholder="Workspace name" />
            <button disabled={disabled || !promoteName.trim()} onClick={() => void onPromoteScratch(promoteName.trim())}>Promote</button>
          </div>
        </section>
      ) : (
        <button className="scratch-create-button" disabled={disabled} onClick={() => void onCreateScratch()}>
          <UiIcon name="files" /><span><strong>Start conversation scratch</strong><small>Private working folder; review after 7 days, never auto-deleted</small></span><em>New</em>
        </button>
      )}
      <div className="workspace-action-grid">
        <button disabled={disabled} onClick={() => void onChoose(false)}>
          <UiIcon name="workspace" /><span><strong>Choose workspace</strong><small>Open any existing folder</small></span>
        </button>
        <button disabled={disabled} onClick={() => void onChoose(true)}>
          <UiIcon name="security" /><span><strong>Choose bounty workspace</strong><small>Open with Bug Bounty Hunter</small></span>
        </button>
      </div>
      <div className="task-menu-section-label"><strong>Recent workspaces</strong><span>{options.length}</span></div>
      <div className="workspace-popover-list">
        {options.length === 0 ? (
          <div className="empty-popover-row"><strong>No recent workspaces</strong><span>Choose a folder above to add one.</span></div>
        ) : options.map(option => {
          const key = option.path.replaceAll('\\', '/').replace(/\/$/, '').toLowerCase()
          const active = key === currentKey
          return (
            <button
              key={option.path}
              className={`workspace-popover-row${active ? ' active' : ''}`}
              disabled={disabled || active}
              onClick={() => void onSwitch(option.path)}
              title={option.path}
            >
              <UiIcon name={option.kind === 'project' ? 'workspace' : 'files'} />
              <span className="workspace-popover-copy"><strong>{option.name}</strong><small>{option.path}</small></span>
              <em>{active ? 'Current' : option.kind}</em>
            </button>
          )
        })}
      </div>
      <button className="workspace-manage-button" onClick={onManage}><UiIcon name="workspace" />Manage all workspaces</button>
    </div>
  )
}

function AgentPopover({
  definitions,
  selected,
  disabled,
  onSelect,
}: {
  definitions: AgentDefinition[]
  selected: string
  disabled: boolean
  onSelect: (mode: string) => void | Promise<void>
}) {
  const items = definitions.length > 0 ? definitions : [{
    id: 'auto', name: 'Auto', description: 'Choose the right working style for the task.', version: '1',
    default_profile: '', default_toolset: 'balanced', default_autonomy: 'balanced', planning_only: false, builtin: true,
  }]
  return (
    <div className="composer-popover composer-agent-popover">
      <TaskMenuHeader
        icon="agent"
        title="Agent"
        subtitle="Working style remembered for this workspace"
        status={selected}
        tone="live"
      />
      <p className="composer-popover-note">This choice is remembered for the current workspace. Security agents may select a task-specific local profile, but never overwrite your normal chat default.</p>
      <div className="task-menu-section-label"><strong>Choose an agent</strong><span>{items.length} available</span></div>
      <div className="agent-popover-list">
        {items.map(definition => (
          <button
            key={definition.id}
            className={`agent-popover-row${definition.name === selected ? ' active' : ''}`}
            disabled={disabled || definition.name === selected}
            onClick={() => void onSelect(definition.name)}
          >
            <span className="agent-popover-icon"><UiIcon name={definition.name === 'Bug Bounty Hunter' ? 'security' : definition.planning_only ? 'plan' : 'agent'} /></span>
            <span className="agent-popover-copy">
              <span className="agent-popover-title"><strong>{definition.name}</strong>{definition.name === selected && <small>Selected</small>}{definition.planning_only && <small>Planning only</small>}</span>
              <span className="agent-popover-description">{definition.description}</span>
              <span className="agent-popover-policy"><em>{titleCase(definition.default_autonomy || 'balanced')}</em><em>{titleCase(definition.default_toolset || 'balanced')} tools</em>{definition.default_profile && <em title={definition.default_profile}>Model · {compactModelLabel(definition.default_profile)}</em>}</span>
            </span>
            <span className="agent-popover-select">{definition.name === selected ? 'Current' : 'Select'}</span>
          </button>
        ))}
      </div>
      {disabled && <div className="workspace-switch-warning">Stop the active run before changing agent.</div>}
    </div>
  )
}

function ToolboxPopover({
  activity,
  countdown,
  runState,
  profile,
  context,
  workspace,
  autonomous,
  channelStatus,
  channelQueue,
  terminalState,
}: {
  activity: AgentActivity[]
  countdown: ToolCountdown | null
  runState: RunStatePayload | null
  profile: string
  context: string
  workspace: string
  autonomous: boolean
  channelStatus: Record<string, string>
  channelQueue: ChannelWorkItem[]
  terminalState: TerminalStateSnapshot | null
}) {
  const queued = Number(channelStatus.queued_work || channelQueue.length || 0)
  const runLabel = formatRunState(runState?.state || 'ready')
  return (
    <div className="composer-popover composer-tools-popover">
      <TaskMenuHeader
        icon="tools"
        title="Tools and run status"
        subtitle="Live execution context and recent activity"
        status={runLabel}
        tone={runState?.state && !['ready', 'complete', 'idle'].includes(runState.state.toLowerCase()) ? 'warn' : 'live'}
      />
      <div className="task-menu-section-label"><strong>Current task</strong><span>{autonomous ? 'Automatic' : 'Supervised'}</span></div>
      <div className="toolbox-grid">
        <div><span>Model profile</span><strong title={profile}>{profile || 'Not selected'}</strong></div>
        <div><span>Workspace</span><strong title={workspace}>{shortPath(workspace) || 'Not selected'}</strong></div>
        <div><span>Context use</span><strong>{context}</strong></div>
        <div><span>Confirmations</span><strong>{autonomous ? 'Automatic' : 'Supervised'}</strong></div>
      </div>
      {countdown && (
        <div className="toolbox-active">
          <span className="task-status-chip">Running now</span>
          <strong>{countdown.name}</strong><small>Mauler is waiting for this tool to finish.</small>
        </div>
      )}
      <div className="task-menu-section-label"><strong>Connected surfaces</strong><span>Live</span></div>
      <div className="toolbox-surfaces">
        <div><UiIcon name="terminal" /><span><strong>Shared terminal</strong><small>{terminalState?.summary || 'Terminal state is unavailable'}</small></span><em className={terminalState?.state === 'ready' ? 'live' : ''}>{titleCase(terminalState?.state || 'unknown')}</em></div>
        <div><UiIcon name="agent" /><span><strong>Agent worker</strong><small>Runs the active task and tool loop</small></span><em className={channelStatus.agent_running === 'true' ? 'live' : ''}>{channelStatus.agent_running === 'true' ? 'Running' : 'Idle'}</em></div>
        <div><UiIcon name="chats" /><span><strong>Telegram channel</strong><small title={channelStatus.telegram_status}>Remote task and status channel</small></span><em className={channelStatus.telegram_running === 'true' ? 'live' : ''}>{channelStatus.telegram_running === 'true' ? 'Running' : 'Off'}</em></div>
        <div><UiIcon name="run" /><span><strong>Queued work</strong><small>Requests waiting for the agent</small></span><em className={queued > 0 ? 'warn' : ''}>{queued}</em></div>
      </div>
      {channelQueue.length > 0 && (
        <>
        <div className="task-menu-section-label"><strong>Queued requests</strong><span>{channelQueue.length}</span></div>
        <div className="toolbox-activity toolbox-queue">
          {channelQueue.map(item => (
            <div key={item.id} className="toolbox-activity-row tool-running">
              <span className="task-status-chip">{todoStatusLabel(item.status)}</span>
              <strong>{item.route.lane}</strong>
              <small>{truncateMiddle(item.envelope.text, 46)}</small>
            </div>
          ))}
        </div>
        </>
      )}
      <div className="task-menu-section-label"><strong>Recent activity</strong><span>{Math.min(activity.length, 6)}</span></div>
      <div className="toolbox-activity">
        {activity.length === 0 ? (
          <div className="empty-popover-row"><strong>No recent tool activity</strong><span>Calls and results will appear here during a run.</span></div>
        ) : activity.slice(0, 6).map(item => (
          <div key={item.id} className={`toolbox-activity-row tool-${item.status}`}>
            <span className="task-status-chip">{todoStatusLabel(item.status)}</span>
            <strong>{item.name}</strong>
            {typeof item.durationMs === 'number' && <small>{Math.max(0, Math.round(item.durationMs))}ms</small>}
          </div>
        ))}
      </div>
    </div>
  )
}

function formatRunState(state: string): string {
  if (!state) return 'Working'
  return state.replaceAll('_', ' ').replace(/\b\w/g, ch => ch.toUpperCase())
}

function formatContext(stats: HistoryStats | null): string {
  if (!stats || stats.budget <= 0) return '-'
  return `${compactNumber(stats.token_count)} / ${compactNumber(stats.budget)}`
}

function compactNumber(value: number): string {
  if (!Number.isFinite(value)) return '-'
  if (Math.abs(value) >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}k`
  return String(Math.round(value))
}

function truncateMiddle(value: string, max: number): string {
  const text = (value || '').trim()
  if (text.length <= max) return text
  const head = Math.max(8, Math.floor((max - 1) * 0.62))
  const tail = Math.max(6, max - head - 1)
  return `${text.slice(0, head)}...${text.slice(-tail)}`
}

function shortPath(path: string): string {
  if (!path) return ''
  const parts = path.split(/[\\/]/).filter(Boolean)
  if (parts.length <= 2) return path
  return `.../${parts.slice(-2).join('/')}`
}

function attachmentSubtitle(att: ChatAttachment): string {
  if (att.kind === 'pdf') return 'PDF'
  if (att.kind === 'document') return 'Document'
  if (att.path) return 'Local file'
  return 'File'
}

function attachmentIsEditable(att: ChatAttachment): boolean {
  return att.kind === 'document' && att.content !== undefined
}

function editedAttachmentName(name: string): string {
  const value = name.trim() || 'Pasted text.txt'
  const dot = value.lastIndexOf('.')
  if (dot > 0) return `${value.slice(0, dot)} (edited)${value.slice(dot)}`
  return `${value} (edited)`
}

function AttachmentChip({
  attachment,
  onOpen,
  onRemove,
}: {
  attachment: ChatAttachment
  onOpen?: () => void
  onRemove?: () => void
}) {
  const openOnKeyboard = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (!onOpen || (event.key !== 'Enter' && event.key !== ' ')) return
    event.preventDefault()
    onOpen()
  }
  return (
    <div
      className={`attachment-chip${onOpen ? ' attachment-chip-openable' : ''}`}
      title={onOpen ? `Open ${attachment.name}` : (attachment.path || attachment.name)}
      role={onOpen ? 'button' : undefined}
      tabIndex={onOpen ? 0 : undefined}
      onClick={onOpen}
      onKeyDown={openOnKeyboard}
    >
      <div className="attachment-icon">TXT</div>
      <div className="attachment-meta">
        <div className="attachment-name">{attachment.name}</div>
        <div className="attachment-kind">
          {attachmentSubtitle(attachment)}{attachment.truncated ? ' / truncated' : ''}
          {onOpen && <span className="attachment-open-hint"> / {onRemove ? 'open or edit' : 'open or copy'}</span>}
        </div>
      </div>
      {onRemove && (
        <button
          className="attachment-remove"
          onClick={event => {
            event.stopPropagation()
            onRemove()
          }}
          title="Remove attachment"
        >x</button>
      )}
    </div>
  )
}

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  if (m <= 0) return `${s}s`
  return `${m}:${String(s).padStart(2, '0')}`
}

function tryPrettyJson(text: string): string {
  const t = text.trim()
  if ((t.startsWith('{') || t.startsWith('[')) && t.length < 8000) {
    try { return JSON.stringify(JSON.parse(t), null, 2) } catch { /* fall through */ }
  }
  return text
}

function formatMsgTime(ts: number): string {
  const d = new Date(ts)
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
    d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function MessageBubble({
  msg,
  onCodeBlock,
  onImageClick,
  onAttachmentOpen,
  onReadResult,
}: {
  msg: ChatMessage
  onCodeBlock: (code: string, lang: string) => void
  onImageClick: (src: string) => void
  onAttachmentOpen: (attachment: ChatAttachment) => void
  onReadResult: (id: string) => void
}) {
  const [copied, setCopied] = useState(false)
  const [saved, setSaved] = useState(false)
  const COLLAPSE_THRESHOLD = 8
  const isToolMsg = msg.role === 'tool_call' || msg.role === 'tool_result'
  const prettyContent = isToolMsg ? tryPrettyJson(msg.content) : msg.content
  const planOutput = msg.role === 'tool_result' && isPlanToolOutput(prettyContent)
  const guardedToolOutput = msg.role === 'tool_result' && prettyContent.startsWith('[Guardrail:')
  const lineCount = prettyContent.split('\n').length
  const collapsible = isToolMsg && !planOutput && lineCount > COLLAPSE_THRESHOLD
  const [collapsed, setCollapsed] = useState(collapsible)
  const roleClass = `msg msg-${msg.role}${msg.category ? ` msg-event-${msg.category}` : ''}${msg.queued ? ' msg-queued' : ''}${guardedToolOutput ? ' msg-guardrail' : ''}`

  const roleLabel: Record<ChatMessage['role'], string> = {
    user: 'You',
    assistant: 'Assistant',
    tool_call: 'Tool call',
    tool_result: 'Result',
    system: 'System',
  }

  const copyMessage = () => {
    const attachmentText = (msg.attachments ?? []).map(att => {
      const content = att.content ?? att.path ?? ''
      return content ? `${att.name}\n${content}` : att.name
    })
    const copyText = [msg.content, ...attachmentText].filter(Boolean).join('\n\n')
    void navigator.clipboard.writeText(copyText).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1600)
    })
  }

  const saveMessage = async () => {
    try {
      const path = await PickSaveFilePath('response.md')
      if (!path) return
      await SaveFileContent(path, msg.content)
      setSaved(true)
      setTimeout(() => setSaved(false), 1600)
    } catch (e) {
      console.error('save message failed:', e)
    }
  }

  const canCopy = msg.role === 'user' || msg.role === 'assistant' || msg.role === 'system' || msg.role === 'tool_result'

  return (
    <div className={roleClass}>
      <div className="msg-header">
        <span className="msg-role">{msg.category === 'browser' ? 'Browser' : msg.category === 'status' ? (msg.role === 'assistant' ? 'Agent update' : 'Run status') : roleLabel[msg.role]}</span>
        {msg.queued && <span className="msg-queued-badge">queued</span>}
        {isToolMsg && msg.toolName && <span className="msg-tool-name">{msg.toolName}</span>}
        {msg.timestamp > 0 && <span className="msg-time">{formatMsgTime(msg.timestamp)}</span>}
        {msg.role === 'assistant' && (
          <button className="msg-save-btn" onClick={() => void saveMessage()} title="Save reply to file">
            {saved ? 'Saved' : 'Save'}
          </button>
        )}
        {canCopy && (
          <button className="msg-copy-btn" onClick={copyMessage} title="Copy message">
            {copied ? 'Copied' : 'Copy'}
          </button>
        )}
      </div>
      {msg.role === 'assistant' && msg.thinking && (
        <ThinkingBlock text={msg.thinking} />
      )}
      <div
        className={`msg-body${collapsed ? ' msg-body-collapsed' : ''}`}
        onClick={collapsible && collapsed ? () => setCollapsed(false) : undefined}
        style={collapsible && collapsed ? { cursor: 'pointer' } : undefined}
      >
        {msg.images && msg.images.length > 0 && (
          <div className="message-images">
            {msg.images.map((src, index) => (
              <button
                key={`${src.slice(0, 40)}-${index}`}
                className="message-image-button"
                onClick={() => onImageClick(src)}
                title="Open image preview"
              >
                <img src={src} alt={`attachment ${index + 1}`} />
              </button>
            ))}
          </div>
        )}
        {msg.attachments && msg.attachments.length > 0 && (
          <div className="message-attachments">
            {msg.attachments.map((att, index) => (
              <AttachmentChip
                key={att.id || `${att.name}-${index}`}
                attachment={att}
                onOpen={() => onAttachmentOpen(att)}
              />
            ))}
          </div>
        )}
        {planOutput ? (
          <PlanResultCard content={prettyContent} rawContent={msg.content} />
        ) : isToolMsg ? (
          <ToolMessageCard role={msg.role} name={msg.toolName} content={prettyContent} rawContent={msg.content} guarded={guardedToolOutput} onReadResult={onReadResult} />
        ) : (
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              ...sharedMarkdownComponents,
              code({ className, children, ...props }) {
                const match = /language-(\w+)/.exec(className ?? '')
                const lang = match?.[1] ?? ''
                const isBlock = !props.ref // inline code has no ref
                const code = String(children).replace(/\n$/, '')
                if (isBlock && lang) {
                  return (
                    <div className="code-block-wrapper">
                      <div className="code-block-header">
                        <span className="code-lang">{lang}</span>
                        <button
                          className="code-send-artifact"
                          onClick={() => onCodeBlock(code, lang)}
                          title="Open as scratch snippet"
                        >Open</button>
                        <button
                          className="code-copy"
                          onClick={() => navigator.clipboard.writeText(code)}
                          title="Copy"
                        >Copy</button>
                      </div>
                      <pre className={`language-${lang}`}><code>{children}</code></pre>
                    </div>
                  )
                }
                return <code className={className} {...props}>{children}</code>
              },
            }}
          >
            {msg.content}
          </ReactMarkdown>
        )}
        {collapsible && (
          <button className="msg-collapse-btn" onClick={() => setCollapsed(v => !v)}>
            {collapsed ? `Show all ${lineCount} lines` : 'Collapse'}
          </button>
        )}
      </div>
    </div>
  )
}

function LiveModelBubble({ thinking, content }: { thinking: string; content: string }) {
  const hasThinking = thinking.trim().length > 0
  const visibleContent = content.replace(/\r\n/g, '\n').trim()
  const hasContent = visibleContent.length > 0
  if (!hasThinking && !hasContent) return null
  return (
    <div className="msg msg-assistant msg-streaming msg-live-model">
      {hasThinking && <ThinkingBlock text={thinking} live />}
      {hasContent && (
        <div className="live-response-message">
          <div className="msg-header">
            <span className="msg-role">Assistant</span>
            <span className="msg-time">live</span>
          </div>
          <div className="msg-body live-response-body">
            <ReactMarkdown remarkPlugins={[remarkGfm]} components={sharedMarkdownComponents}>{visibleContent}</ReactMarkdown>
          </div>
        </div>
      )}
    </div>
  )
}

function isPlanToolOutput(content: string): boolean {
  const text = content.trim().toLowerCase()
  if (!text.includes('active task plan')) return false
  return text.includes('[done]') || text.includes('[in_progress]') || text.includes('[pending]') || text.includes('todo-')
}

function PlanResultCard({ content, rawContent }: { content: string; rawContent: string }) {
  const [expanded, setExpanded] = useState(false)
  const lines = content.split('\n').map(line => line.trim()).filter(Boolean)
  const todoLines = lines.filter(line => /^\s*-\s*\[/.test(line) || line.includes('todo-'))
  const done = todoLines.filter(line => line.includes('[done]')).length
  const blocked = todoLines.filter(line => line.includes('[blocked]')).length
  const current = todoLines.find(line => line.includes('[in_progress]')) || todoLines.find(line => line.includes('[pending]')) || todoLines[0]
  const copy = () => void navigator.clipboard.writeText(rawContent)
  return (
    <div className="tool-card plan-result-card">
      <div className="tool-card-head">
        <div className="tool-card-title">
          <span>Plan update</span>
          <strong title={current || 'Active task plan updated'}>{current ? stripPlanBullet(current) : 'Active task plan updated'}</strong>
        </div>
        <div className="tool-card-actions">
          <span className="tool-chip">{done}/{todoLines.length || '?'} done</span>
          {blocked > 0 && <span className="tool-chip">{blocked} blocked</span>}
          <button onClick={copy}>Copy</button>
          <button onClick={() => setExpanded(v => !v)}>{expanded ? 'Hide plan' : 'Show plan'}</button>
        </div>
      </div>
      <div className="plan-result-note">
        Plan state moved to the composer Plan popup. Chat only keeps this compact update.
      </div>
      {expanded && (
        <pre className="tool-raw">{rawContent}</pre>
      )}
    </div>
  )
}

function stripPlanBullet(line: string): string {
  return line.replace(/^\s*-\s*\[[^\]]+\]\s*/, '').trim()
}

function ToolMessageCard({
  role,
  name,
  content,
  rawContent,
  guarded,
  onReadResult,
}: {
  role: ChatMessage['role']
  name?: string
  content: string
  rawContent: string
  guarded: boolean
  onReadResult: (id: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const parsed = parseToolPayload(content)
  const command = parsed.command || parsed.cmd || parsed.path || parsed.query || ''
  const status = toolStatus(content, role, guarded)
  const summary = command || parsed.detail || parsed.error || name || firstMeaningfulLine(content) || role
  const timeout = parsed.timeout ? `${parsed.timeout}s` : ''
  const isResult = role === 'tool_result'
  const resultIds = extractResultIds(content)
  const copy = (value: string) => {
    void navigator.clipboard.writeText(value)
  }

  return (
    <div className={`tool-card tool-card-${status}`}>
      <div className="tool-card-head">
        <div className="tool-card-title">
          <span>{name || (isResult ? 'Result' : toolCallLabel(parsed, content))}</span>
          <strong title={summary}>{summary}</strong>
        </div>
        <div className="tool-card-actions">
          {timeout && <span className="tool-chip">{timeout}</span>}
          <span className="tool-chip">{status}</span>
          {resultIds.map(id => (
            <button key={id} className="tool-result-link" onClick={() => onReadResult(id)} title={`Read ${id} with read_tool_result`}>
              Read result
            </button>
          ))}
          <button onClick={() => copy(command || rawContent)}>{command ? 'Copy cmd' : 'Copy'}</button>
          <button onClick={() => setExpanded(v => !v)}>{expanded ? 'Hide raw' : 'Raw'}</button>
        </div>
      </div>
      {command && (
        <pre className="tool-command-line">{command}</pre>
      )}
      {isResult && (
        <pre className="tool-result-preview">{compactToolOutput(content)}</pre>
      )}
      {expanded && (
        <pre className="tool-raw">{rawContent}</pre>
      )}
    </div>
  )
}

function extractResultIds(text: string): string[] {
  const ids = new Set<string>()
  const re = /\bresult_id[=:]\s*"?([A-Za-z0-9._-]+\/[A-Za-z0-9._-]+)"?/g
  for (const match of text.matchAll(re)) {
    ids.add(match[1].replace(/[.,;)\]]+$/, ''))
  }
  return Array.from(ids).slice(0, 3)
}

function parseToolPayload(text: string): Record<string, string> {
  const t = text.trim()
  if (t.startsWith('{')) {
    try {
      const parsed = JSON.parse(t) as Record<string, unknown>
      const out: Record<string, string> = {}
      for (const [key, value] of Object.entries(parsed)) {
        if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
          out[key] = String(value)
        }
      }
      return out
    } catch {
      return {}
    }
  }
  return {}
}

function toolCallLabel(parsed: Record<string, string>, content: string): string {
  if (parsed.command) return 'shell'
  if (parsed.path) return 'file'
  if (parsed.query || parsed.pattern) return 'search'
  const first = firstMeaningfulLine(content)
  if (first.includes('{')) return 'tool call'
  return first.slice(0, 28) || 'tool call'
}

function toolStatus(content: string, role: ChatMessage['role'], guarded: boolean): string {
  if (guarded) return 'guarded'
  const lower = content.toLowerCase()
  if (lower.includes('exit code 0') || lower.includes('[wsl exit 0') || lower.includes('status: ok')) return 'ok'
  if (lower.includes('exit code') || lower.includes('error:') || lower.includes('failed') || lower.includes('denied')) return 'error'
  if (role === 'tool_call') return 'call'
  return 'done'
}

function firstMeaningfulLine(text: string): string {
  return text.split(/\r?\n/).map(line => line.trim()).find(Boolean) ?? ''
}

function compactToolOutput(text: string): string {
  const trimmed = text.trim()
  if (trimmed.length <= 1800) return trimmed
  return `${trimmed.slice(0, 1800)}\n...`
}

function ThinkingBlock({ text, live = false }: { text: string; live?: boolean }) {
  const [open, setOpen] = useState(false)
  if (!text.trim()) return null
  return (
    <details className={`thinking-block ${live ? 'live' : ''}`} open={live || open} onToggle={e => setOpen(e.currentTarget.open)}>
      <summary>{live ? 'Thinking live' : 'Thinking'} {live ? '' : (open ? 'open' : 'closed')}</summary>
      <pre>{text}</pre>
    </details>
  )
}
