import { useRef, useEffect, useState, useCallback, useMemo, useLayoutEffect, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  Undo,
  EncodeFileBase64,
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
} from '../wailsjs/go'
import type { AgentActivity, ChatMessage, RunProfileOption, RunStatePayload, ToolCountdown } from '../App'
import './ChatPane.css'

interface Props {
  messages: ChatMessage[]
  streaming: boolean
  streamBuffer: string
  thinkingBuffer: string
  activeProfile: string
  activeRunProfile: string
  cloudRunProfiles: RunProfileOption[]
  runProfileOverride: string
  autonomous: boolean
  pendingInterrupt: boolean
  toolCountdown: ToolCountdown | null
  runState: RunStatePayload | null
  todos: TodoItem[]
  activity: AgentActivity[]
  settingsVersion: number
  agentDefinitions: AgentDefinition[]
  agentSelection: string
  draftRequest?: { id: string; text: string } | null
  onSubmitMessage: (text: string, images: string[], attachments: ChatAttachment[], profileOverride?: string) => void
  onRunProfileOverrideChange: (profile: string) => void
  onCancelPending: () => void
  onStopAgent: () => void
  onClearChat: () => void
  onArtifact: (code: string, lang: string) => void
  onAutonomousChange: (enabled: boolean) => void
  onOpenQuickChat: () => void
  onOpenSettings: () => void
  onAgentSelectionChange: (mode: string) => void | Promise<void>
  onChooseWorkspace: (bugBounty: boolean) => void | Promise<void>
  onSwitchWorkspace: (path: string) => void | Promise<void>
  onOpenProjects: () => void
  onClearPlan: () => void | Promise<void>
}

interface ChatWorkspaceOption {
  path: string
  name: string
  kind: 'project' | 'folder'
}

export function ChatPane({
  messages,
  streaming,
  streamBuffer,
  thinkingBuffer,
  activeProfile,
  activeRunProfile,
  cloudRunProfiles,
  runProfileOverride,
  autonomous,
  pendingInterrupt,
  toolCountdown,
  runState,
  todos,
  activity,
  settingsVersion,
  agentDefinitions,
  agentSelection,
  draftRequest,
  onSubmitMessage,
  onRunProfileOverrideChange,
  onCancelPending,
  onStopAgent,
  onClearChat,
  onArtifact,
  onAutonomousChange,
  onOpenQuickChat,
  onOpenSettings,
  onAgentSelectionChange,
  onChooseWorkspace,
  onSwitchWorkspace,
  onOpenProjects,
  onClearPlan,
}: Props) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const planTriggerRef = useRef<HTMLButtonElement>(null)
  const toolsTriggerRef = useRef<HTMLButtonElement>(null)
  const workspaceTriggerRef = useRef<HTMLButtonElement>(null)
  const agentTriggerRef = useRef<HTMLButtonElement>(null)
  const [input, setInput] = useState('')
  const [images, setImages] = useState<string[]>([])
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [videoStatus, setVideoStatus] = useState<string | null>(null)
  const [lightboxImage, setLightboxImage] = useState<string | null>(null)
  const [attachmentEditor, setAttachmentEditor] = useState<{ attachment: ChatAttachment; source: 'draft' | 'message' } | null>(null)
  const [attachmentEditorText, setAttachmentEditorText] = useState('')
  const [attachmentCopied, setAttachmentCopied] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [showSearch, setShowSearch] = useState(false)
  const [nowMs, setNowMs] = useState(Date.now())
  const [workspaceRoot, setWorkspaceRoot] = useState('')
  const [historyStats, setHistoryStats] = useState<HistoryStats | null>(null)
  const [channelStatus, setChannelStatus] = useState<Record<string, string>>({})
  const [channelQueue, setChannelQueue] = useState<ChannelWorkItem[]>([])
  const [terminalState, setTerminalState] = useState<TerminalStateSnapshot | null>(null)
  const [openPopover, setOpenPopover] = useState<'plan' | 'tools' | 'workspace' | 'agent' | null>(null)
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

  const visibleMessages = useMemo(() => {
    if (!searchQuery.trim()) return messages
    const q = searchQuery.toLowerCase()
    return messages.filter(m => {
      const attachmentText = (m.attachments ?? []).map(a => `${a.name} ${a.content ?? ''} ${a.path ?? ''}`).join(' ')
      return `${m.content} ${attachmentText}`.toLowerCase().includes(q)
    })
  }, [messages, searchQuery])

  // Auto-scroll to bottom
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, streamBuffer])

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
            setVoiceStatus(`Heard: ${transcript}`)
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
  const MAX_ATTACHMENT_CHARS = 180_000

  const addAttachment = useCallback((attachment: Omit<ChatAttachment, 'id'>) => {
    setAttachments(prev => [...prev, { ...attachment, id: crypto.randomUUID() }])
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
    }
    reader.readAsText(file)
  }, [TEXT_EXTS, addAttachment])

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

  const handleDrop = useCallback(async (e: React.DragEvent<HTMLTextAreaElement>) => {
    e.preventDefault()
    const files = Array.from(e.dataTransfer.files ?? [])
    if (files.length > 0) {
      for (const file of files) {
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
    const ext = path.split('.').pop()?.toLowerCase() ?? ''
    const mime = IMAGE_EXTS[ext]
    if (VIDEO_EXTS[ext]) {
      void ingestVideoPath(path)
    } else if (mime) {
      try {
        const b64 = await EncodeFileBase64(path)
        setImages(prev => [...prev, `data:${mime};base64,${b64}`])
      } catch {
        setInput(prev => prev ? `${prev} @${path}` : `@${path}`)
      }
    } else {
      addAttachment({
        name: path.split(/[\\/]/).pop() || 'Attached file',
        kind: ext === 'pdf' ? 'pdf' : 'file',
        path,
        content: `Local file path: ${path}`,
      })
    }
    inputRef.current?.focus()
  }, [IMAGE_EXTS, VIDEO_EXTS, addAttachment, readTextFileAttachment, ingestVideoData, ingestVideoPath])

  // Paste images, copied files, and larger/multiline text as attachments.
  const handlePaste = useCallback((e: React.ClipboardEvent) => {
    const items = Array.from(e.clipboardData.items)
    let handledBinary = false
    for (const item of items) {
      if (item.kind === 'file' && item.type.startsWith('image/')) {
        const file = item.getAsFile()
        if (!file) continue
        handledBinary = true
        const reader = new FileReader()
        reader.onload = () => {
          setImages(prev => [...prev, reader.result as string])
        }
        reader.readAsDataURL(file)
      } else if (item.kind === 'file' && item.type.startsWith('video/')) {
        const file = item.getAsFile()
        if (!file) continue
        handledBinary = true
        void ingestVideoData(file)
      } else if (item.kind === 'file') {
        const file = item.getAsFile()
        if (!file) continue
        handledBinary = true
        readTextFileAttachment(file)
      }
    }
    const pastedText = e.clipboardData.getData('text/plain')
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
    } else if (handledBinary) {
      e.preventDefault()
    }
  }, [addAttachment, readTextFileAttachment, ingestVideoData])

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
      <div className="chat-lane-banner"><strong>Project agent</strong><span>Tools and selected-box context are active.</span><button onClick={onOpenQuickChat}>Fast chat · no tools/context</button></div>
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
            {searchQuery.trim() ? `${visibleMessages.length} / ${messages.length}` : ''}
          </span>
          <button className="chat-search-close" onClick={() => { setShowSearch(false); setSearchQuery('') }}>x</button>
        </div>
      )}
      <div className="chat-messages">
        {messages.length === 0 && !streaming && (
          <div className="chat-empty">
            <div className="chat-empty-head">
              <span>$ TheMauler</span>
              <strong>Ready for a local agent run</strong>
            </div>
            <div className="chat-empty-grid">
              <div><span>Profile</span><strong>{activeProfile || 'none'}</strong></div>
              <div><span>Mode</span><strong>{autonomous ? 'Autonomous' : 'Manual'}</strong></div>
              <div><span>Workspace</span><strong title={workspaceRoot}>{shortPath(workspaceRoot) || 'unknown'}</strong></div>
              <div><span>Context</span><strong>{formatContext(historyStats)}</strong></div>
            </div>
            <div className="chat-empty-actions">
              <div><strong>Start</strong><span>Ask a task, drop files, or open artifacts from Workspace.</span></div>
              <div><strong>Inspect</strong><span>Use the right panel for files, facts, commands, and activity.</span></div>
              <div><strong>Control</strong><span>Profile, autonomy, state, and context are pinned above the composer.</span></div>
            </div>
            <div className="chat-empty-shortcuts">
              <div className="chat-shortcut"><kbd>Enter</kbd><span>Send message</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+Enter</kbd><span>New line</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+F</kbd><span>Search chat</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+K</kbd><span>Clear chat</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+,</kbd><span>Settings</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+Z</kbd><span>Undo last edit</span></div>
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
        {pendingInterrupt && (
          <div className="chat-pending-interrupt">
            <span>Interrupting current run. Your next message will send as soon as it stops.</span>
            <button onClick={onCancelPending}>Cancel</button>
          </div>
        )}
        <div className="chat-run-footer">
          <div className="run-popover-wrap">
            <button
              ref={planTriggerRef}
              type="button"
              className={`run-popover-trigger ${openPopover === 'plan' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'plan' ? null : 'plan')}
              title="Show the active task plan"
            >
              Plan <strong>{todoSummary(todos)}</strong>
            </button>
            {openPopover === 'plan' && (
              <ComposerPopoverPortal anchor={planTriggerRef.current}>
                <PlanPopover todos={todos} onClear={onClearPlan} />
              </ComposerPopoverPortal>
            )}
          </div>
          <div className="run-popover-wrap">
            <button
              ref={toolsTriggerRef}
              type="button"
              className={`run-popover-trigger ${openPopover === 'tools' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'tools' ? null : 'tools')}
              title="Show current tool/run state"
            >
              Tools <strong>{toolSummary(activity, toolCountdown)}</strong>
            </button>
            {openPopover === 'tools' && (
              <ComposerPopoverPortal anchor={toolsTriggerRef.current}>
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
              className={`run-popover-trigger ${openPopover === 'workspace' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'workspace' ? null : 'workspace')}
              title={workspaceRoot || 'Choose the active workspace'}
            >
              Workspace <strong>{shortPath(workspaceRoot) || 'choose'}</strong>
            </button>
            {openPopover === 'workspace' && (
              <ComposerPopoverPortal anchor={workspaceTriggerRef.current} width={560}>
                <WorkspacePopover
                  current={workspaceRoot}
                  options={workspaceOptions}
                  disabled={streaming}
                  onChoose={async bugBounty => {
                    setOpenPopover(null)
                    await onChooseWorkspace(bugBounty)
                  }}
                  onSwitch={async path => {
                    setOpenPopover(null)
                    await onSwitchWorkspace(path)
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
              className={`run-popover-trigger ${openPopover === 'agent' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'agent' ? null : 'agent')}
              title="Choose the agent remembered for this workspace"
            >
              Agent <strong>{agentSelection || 'Auto'}</strong>
            </button>
            {openPopover === 'agent' && (
              <ComposerPopoverPortal anchor={agentTriggerRef.current} width={560}>
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
          <label className={`run-profile-once ${runProfileOverride ? 'cloud' : 'local'}`} title="Choose a model for the next task only. Mauler always returns to the local default afterward.">
            <span>Next task</span>
            <select
              value={runProfileOverride}
              onChange={event => onRunProfileOverrideChange(event.target.value)}
              aria-label="Model for next task"
            >
              <option value="">Local default · {activeProfile || 'local profile'}</option>
              {cloudRunProfiles.map(profile => (
                <option key={profile.name} value={profile.name}>Cloud once · {profile.model}</option>
              ))}
            </select>
          </label>
          {cloudRunProfiles.length === 0 && (
            <button type="button" className="run-cloud-setup" onClick={onOpenSettings} title="Add an OpenRouter model in Settings, then use it for one task at a time.">
              Add cloud boost
            </button>
          )}
          {activeRunProfile && activeRunProfile !== activeProfile && (
            <RunPill label="This run" value={`Cloud · ${activeRunProfile}`} tone="live" />
          )}
          <label className={`run-autonomy ${autonomous ? 'active' : ''}`} title="Autonomous mode lets the agent run tools without confirmation prompts.">
            <input
              type="checkbox"
              checked={autonomous}
              onChange={e => onAutonomousChange(e.target.checked)}
              disabled={streaming}
            />
            <span>{autonomous ? 'Auto' : 'Manual'}</span>
          </label>
          {toolCountdown && <RunPill label="Tool" value={`${toolCountdown.name} ${formatDuration(Math.ceil(Math.max(0, toolCountdown.deadline - nowMs) / 1000))}`} tone="live" />}
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
            placeholder="Ask anything... drop a file to attach. Enter sends; Ctrl+Enter adds a new line."
            rows={4}
            disabled={false}
            spellCheck
            lang="en"
            autoCapitalize="sentences"
          />
          <div className="chat-input-actions">
            <button
              className={`composer-voice-btn ${recording ? 'recording' : voiceSession ? 'active' : ''}`}
              onClick={() => recording ? stopRecording() : void startRecording()}
              disabled={!audioConfig?.enabled}
              title={recording ? 'Stop recording and send' : 'Talk to Mauler'}
            >
              {recording ? 'Send voice' : 'Talk'}
            </button>
            {voiceSession && (
              <button className="composer-voice-btn active" onClick={() => { setVoiceSession(false); stopSpeech(); setVoiceStatus('') }} title="Turn spoken replies off">
                Voice on
              </button>
            )}
            <button className="composer-stop-btn danger" onClick={onStopAgent} disabled={!streaming} title={streaming ? 'Stop the current run' : 'No run is active'}>
              Stop
            </button>
            <button className="composer-undo-btn" onClick={() => void Undo()} title="Undo last file change (Ctrl+Z)">Undo</button>
            <button className="chat-clear-btn" onClick={onClearChat} title="Clear chat history (Ctrl+K)" disabled={streaming}>Clear</button>
            <button
              className={`primary composer-send-btn ${streaming ? 'interrupt' : ''}`}
              onClick={() => void handleSend()}
              disabled={(!input.trim() && images.length === 0 && attachments.length === 0) || pendingInterrupt}
              title={streaming ? 'Interrupt the current run and send this draft' : 'Send'}
            >
              <span>{streaming ? 'Interrupt & Send' : 'Send'}</span>
            </button>
          </div>
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
  if (todos.length === 0) return 'none'
  const done = todos.filter(t => t.status === 'done').length
  const blocked = todos.filter(t => t.status === 'blocked').length
  if (blocked > 0) return `${done}/${todos.length}, ${blocked} blocked`
  return `${done}/${todos.length}`
}

function toolSummary(activity: AgentActivity[], countdown: ToolCountdown | null): string {
  if (countdown) return countdown.name
  const running = activity.find(item => item.status === 'running')
  if (running) return running.name
  const last = activity[0]
  return last ? `${last.name} ${last.status}` : 'idle'
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

function PlanPopover({ todos, onClear }: { todos: TodoItem[]; onClear: () => void | Promise<void> }) {
  const current = todos.find(t => t.status === 'in_progress') || todos.find(t => t.status === 'blocked') || todos.find(t => t.status !== 'done')
  return (
    <div className="composer-popover composer-plan-popover">
      <div className="composer-popover-head">
        <span>Active Plan</span>
        <div><strong>{todoSummary(todos)}</strong><button onClick={() => void onClear()} disabled={todos.length === 0}>Clear plan</button></div>
      </div>
      <p className="composer-popover-note">Plan state is not evidence. Confirm important claims in Facts, logs, files, or terminal output.</p>
      {current && (
        <div className={`plan-current plan-current-${current.status}`}>
          <span>Now</span>
          <strong>{current.text}</strong>
          {current.detail && <small>{current.detail}</small>}
        </div>
      )}
      <div className="plan-popover-list">
        {todos.length === 0 ? (
          <div className="empty-popover-row">No active plan yet.</div>
        ) : todos.map(item => (
          <div key={item.id} className={`plan-popover-item plan-${item.status}`}>
            <span>{item.status}</span>
            <strong>{item.text}</strong>
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
  disabled,
  onChoose,
  onSwitch,
  onManage,
}: {
  current: string
  options: ChatWorkspaceOption[]
  disabled: boolean
  onChoose: (bugBounty: boolean) => void | Promise<void>
  onSwitch: (path: string) => void | Promise<void>
  onManage: () => void
}) {
  const currentKey = current.replaceAll('\\', '/').replace(/\/$/, '').toLowerCase()
  return (
    <div className="composer-popover composer-workspace-popover">
      <div className="composer-popover-head">
        <span>Active workspace</span>
        <strong title={current}>{shortPath(current) || 'Not selected'}</strong>
      </div>
      <p className="composer-popover-note">Switching opens a clean project chat and restores the agent remembered for that folder. Files, memory, saved sessions, evidence, and logs are preserved.</p>
      {disabled && <div className="workspace-switch-warning">Stop the active run before changing workspace.</div>}
      <div className="workspace-action-grid">
        <button disabled={disabled} onClick={() => void onChoose(false)}>
          <strong>Choose folder...</strong>
          <span>Open any existing workspace</span>
        </button>
        <button disabled={disabled} onClick={() => void onChoose(true)}>
          <strong>Choose bounty folder...</strong>
          <span>Open it with Bug Bounty Hunter</span>
        </button>
      </div>
      <div className="workspace-popover-list">
        {options.length === 0 ? (
          <div className="empty-popover-row">No recent workspaces yet.</div>
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
              <span>{option.kind}</span>
              <strong>{option.name}</strong>
              <small>{option.path}</small>
            </button>
          )
        })}
      </div>
      <button className="workspace-manage-button" onClick={onManage}>Manage workspaces...</button>
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
    default_toolset: 'balanced', default_autonomy: 'balanced', planning_only: false, builtin: true,
  }]
  return (
    <div className="composer-popover composer-agent-popover">
      <div className="composer-popover-head">
        <span>Workspace agent</span>
        <strong>{selected}</strong>
      </div>
      <p className="composer-popover-note">This choice is remembered for the current workspace. It does not change the local model default or one-task cloud boost.</p>
      <div className="agent-popover-list">
        {items.map(definition => (
          <button
            key={definition.id}
            className={`agent-popover-row${definition.name === selected ? ' active' : ''}`}
            disabled={disabled || definition.name === selected}
            onClick={() => void onSelect(definition.name)}
          >
            <span className="agent-popover-title">
              <strong>{definition.name}</strong>
              {definition.planning_only && <small>Planning only</small>}
            </span>
            <span className="agent-popover-description">{definition.description}</span>
            <span className="agent-popover-policy">{definition.default_autonomy || 'balanced'} / {definition.default_toolset || 'balanced'}</span>
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
  return (
    <div className="composer-popover composer-tools-popover">
      <div className="composer-popover-head">
        <span>Toolbox</span>
        <strong>{formatRunState(runState?.state || 'ready')} / {terminalState?.state || 'terminal unknown'}</strong>
      </div>
      <div className="toolbox-grid">
        <div><span>Profile</span><strong>{profile || 'none'}</strong></div>
        <div><span>Mode</span><strong>{autonomous ? 'Autonomous' : 'Manual'}</strong></div>
        <div><span>Workspace</span><strong title={workspace}>{shortPath(workspace) || 'unknown'}</strong></div>
        <div><span>Context</span><strong>{context}</strong></div>
        <div><span>Terminal</span><strong title={terminalState?.summary}>{terminalState?.state || 'unknown'}</strong></div>
        <div><span>Channel</span><strong>{queued} queued</strong></div>
        <div><span>Telegram</span><strong title={channelStatus.telegram_status}>{channelStatus.telegram_running === 'true' ? 'running' : 'off'}</strong></div>
        <div><span>Agent</span><strong>{channelStatus.agent_running === 'true' ? 'running' : 'idle'}</strong></div>
      </div>
      {countdown && (
        <div className="toolbox-active">
          <span>Running tool</span>
          <strong>{countdown.name}</strong>
        </div>
      )}
      {terminalState?.summary && (
        <div className="toolbox-active toolbox-terminal">
          <span>Terminal summary</span>
          <strong>{terminalState.summary}</strong>
        </div>
      )}
      {channelQueue.length > 0 && (
        <div className="toolbox-activity toolbox-queue">
          {channelQueue.map(item => (
            <div key={item.id} className="toolbox-activity-row tool-running">
              <span>{item.status}</span>
              <strong>{item.route.lane}</strong>
              <small>{truncateMiddle(item.envelope.text, 46)}</small>
            </div>
          ))}
        </div>
      )}
      <div className="toolbox-activity">
        {activity.length === 0 ? (
          <div className="empty-popover-row">No recent tool activity.</div>
        ) : activity.slice(0, 6).map(item => (
          <div key={item.id} className={`toolbox-activity-row tool-${item.status}`}>
            <span>{item.status}</span>
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

function RunPill({
  label,
  value,
  title,
  tone,
}: {
  label: string
  value: string
  title?: string
  tone?: 'idle' | 'live' | 'warn'
}) {
  return (
    <div className={`run-pill ${tone ? `run-pill-${tone}` : ''}`} title={title || `${label}: ${value}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
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
  const roleClass = `msg msg-${msg.role}${msg.queued ? ' msg-queued' : ''}${guardedToolOutput ? ' msg-guardrail' : ''}`

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
        <span className="msg-role">{roleLabel[msg.role]}</span>
        {msg.queued && <span className="msg-queued-badge">queued</span>}
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
          <ToolMessageCard role={msg.role} content={prettyContent} rawContent={msg.content} guarded={guardedToolOutput} onReadResult={onReadResult} />
        ) : (
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
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
  const contentParts = splitLiveAssistantContent(content)
  const hasContent = contentParts.length > 0
  if (!hasThinking && !hasContent) return null
  return (
    <div className="msg msg-assistant msg-streaming msg-live-model">
      {hasThinking && <ThinkingBlock text={thinking} live />}
      {contentParts.map((part, index) => (
        <div className="live-response-message" key={`${index}-${part.slice(0, 24)}`}>
          <div className="msg-header">
            <span className="msg-role">Assistant</span>
            <span className="msg-time">live</span>
          </div>
          <div className="msg-body live-response-body">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{part}</ReactMarkdown>
          </div>
        </div>
      ))}
    </div>
  )
}

function splitLiveAssistantContent(content: string): string[] {
  const text = content.replace(/\r\n/g, '\n').trim()
  if (!text) return []
  const parts = text.split(/\n{2,}/).map(part => part.trim()).filter(Boolean)
  if (parts.length <= 1) return parts
  return parts
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
  content,
  rawContent,
  guarded,
  onReadResult,
}: {
  role: ChatMessage['role']
  content: string
  rawContent: string
  guarded: boolean
  onReadResult: (id: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const parsed = parseToolPayload(content)
  const command = parsed.command || parsed.cmd || parsed.path || parsed.query || ''
  const status = toolStatus(content, role, guarded)
  const summary = command || parsed.detail || parsed.error || firstMeaningfulLine(content) || role
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
          <span>{isResult ? 'Result' : toolCallLabel(parsed, content)}</span>
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
