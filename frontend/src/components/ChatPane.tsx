import { useRef, useEffect, useState, useCallback, useMemo, type KeyboardEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {
  Undo,
  EncodeFileBase64,
  PickSaveFilePath,
  SaveFileContent,
  GetWorkingDir,
  GetHistoryStats,
  GetChannelBusStatus,
  ListChannelWorkQueue,
  GetSharedTerminalState,
  type ChatAttachment,
  type HistoryStats,
  type TodoItem,
  type ChannelWorkItem,
  type TerminalStateSnapshot,
} from '../wailsjs/go'
import type { AgentActivity, ChatMessage, RunStatePayload, ToolCountdown } from '../App'
import './ChatPane.css'

interface Props {
  messages: ChatMessage[]
  streaming: boolean
  streamBuffer: string
  thinkingBuffer: string
  activeProfile: string
  autonomous: boolean
  pendingInterrupt: boolean
  toolCountdown: ToolCountdown | null
  runState: RunStatePayload | null
  todos: TodoItem[]
  activity: AgentActivity[]
  onSubmitMessage: (text: string, images: string[], attachments: ChatAttachment[]) => void
  onCancelPending: () => void
  onCancelTool: (name: string) => void
  onStopAgent: () => void
  onClearChat: () => void
  onArtifact: (code: string, lang: string) => void
  onAutonomousChange: (enabled: boolean) => void
}

export function ChatPane({
  messages,
  streaming,
  streamBuffer,
  thinkingBuffer,
  activeProfile,
  autonomous,
  pendingInterrupt,
  toolCountdown,
  runState,
  todos,
  activity,
  onSubmitMessage,
  onCancelPending,
  onCancelTool,
  onStopAgent,
  onClearChat,
  onArtifact,
  onAutonomousChange,
}: Props) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const [input, setInput] = useState('')
  const [images, setImages] = useState<string[]>([])
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [lightboxImage, setLightboxImage] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [showSearch, setShowSearch] = useState(false)
  const [nowMs, setNowMs] = useState(Date.now())
  const [workspaceRoot, setWorkspaceRoot] = useState('')
  const [historyStats, setHistoryStats] = useState<HistoryStats | null>(null)
  const [channelStatus, setChannelStatus] = useState<Record<string, string>>({})
  const [channelQueue, setChannelQueue] = useState<ChannelWorkItem[]>([])
  const [terminalState, setTerminalState] = useState<TerminalStateSnapshot | null>(null)
  const [openPopover, setOpenPopover] = useState<'plan' | 'tools' | null>(null)

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
    onSubmitMessage(text, imgs, atts)
  }, [input, images, attachments, messages, onSubmitMessage])

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
    if (mime) {
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
  }, [IMAGE_EXTS, addAttachment, readTextFileAttachment])

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
  }, [addAttachment, readTextFileAttachment])

  const removeImage = useCallback((idx: number) => {
    setImages(prev => prev.filter((_, i) => i !== idx))
  }, [])

  const removeAttachment = useCallback((id: string | undefined) => {
    setAttachments(prev => prev.filter(a => a.id !== id))
  }, [])

  // Open code blocks as scratch snippets in the File tab.
  const handleCodeBlock = useCallback((code: string, lang: string) => {
    onArtifact(code, lang || 'plaintext')
  }, [onArtifact])

  return (
    <div className="chat-pane">
      {showSearch && (
        <div className="chat-search-bar">
          <input
            ref={searchRef}
            className="chat-search-input"
            placeholder="Search messages…"
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Escape') { setShowSearch(false); setSearchQuery('') }
            }}
          />
          <span className="chat-search-count">
            {searchQuery.trim() ? `${visibleMessages.length} / ${messages.length}` : ''}
          </span>
          <button className="chat-search-close" onClick={() => { setShowSearch(false); setSearchQuery('') }}>×</button>
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
            onReadResult={(id) => {
              setInput(`Use read_tool_result to read result_id=${id} offset=0 limit=8000`)
              setTimeout(() => inputRef.current?.focus(), 0)
            }}
          />
        ))}

        {/* Live stream bubble */}
        {streaming && (
          <div className="chat-live-strip">
            <div className="chat-live-main">
              <span>Live Run</span>
              <strong>{liveRunLabel(runState?.state || 'working', toolCountdown?.name)}</strong>
              <p>{liveRunDetail(runState?.detail, streamBuffer, thinkingBuffer, activity, toolCountdown)}</p>
            </div>
            {toolCountdown && (
              <ToolCountdownCard countdown={toolCountdown} nowMs={nowMs} onCancel={onCancelTool} />
            )}
          </div>
        )}

        <div ref={bottomRef} />
      </div>

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
              type="button"
              className={`run-popover-trigger ${openPopover === 'plan' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'plan' ? null : 'plan')}
              title="Show the active task plan"
            >
              Plan <strong>{todoSummary(todos)}</strong>
            </button>
            {openPopover === 'plan' && (
              <PlanPopover todos={todos} />
            )}
          </div>
          <div className="run-popover-wrap">
            <button
              type="button"
              className={`run-popover-trigger ${openPopover === 'tools' ? 'active' : ''}`}
              onClick={() => setOpenPopover(v => v === 'tools' ? null : 'tools')}
              title="Show current tool/run state"
            >
              Tools <strong>{toolSummary(activity, toolCountdown)}</strong>
            </button>
            {openPopover === 'tools' && (
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
            )}
          </div>
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
                <AttachmentChip key={att.id} attachment={att} onRemove={() => removeAttachment(att.id)} />
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
        </div>
      </div>
    </div>
  )
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

function PlanPopover({ todos }: { todos: TodoItem[] }) {
  const current = todos.find(t => t.status === 'in_progress') || todos.find(t => t.status === 'blocked') || todos.find(t => t.status !== 'done')
  return (
    <div className="composer-popover composer-plan-popover">
      <div className="composer-popover-head">
        <span>Active Plan</span>
        <strong>{todoSummary(todos)}</strong>
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
        <strong>{formatRunState(runState?.state || 'ready')} · {terminalState?.state || 'terminal unknown'}</strong>
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

function liveRunLabel(state: string, tool?: string): string {
  if (tool) return `Running ${tool}`
  return formatRunState(state)
}

function liveRunDetail(
  detail?: string,
  streamBuffer?: string,
  thinkingBuffer?: string,
  activity: AgentActivity[] = [],
  countdown: ToolCountdown | null = null,
): string {
  if (countdown) return `Running ${countdown.name}; full command/output is in the terminal and AI Commands split.`
  if (detail?.trim()) return detail.trim()
  if (streamBuffer?.trim()) return `Writing: ${truncateMiddle(streamBuffer.trim().replace(/\s+/g, ' '), 120)}`
  if (thinkingBuffer?.trim()) return 'Thinking'
  const running = activity.find(item => item.status === 'running')
  if (running) return `Using ${running.name}; details are in AI Commands.`
  const last = activity[0]
  if (last) {
    const status = last.status === 'done' ? 'finished' : last.status
    return `${last.name} ${status}; next decision is being prepared.`
  }
  return 'Preparing the next action.'
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
  return `${text.slice(0, head)}…${text.slice(-tail)}`
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

function AttachmentChip({
  attachment,
  onRemove,
}: {
  attachment: ChatAttachment
  onRemove?: () => void
}) {
  return (
    <div className="attachment-chip" title={attachment.path || attachment.name}>
      <div className="attachment-icon">TXT</div>
      <div className="attachment-meta">
        <div className="attachment-name">{attachment.name}</div>
        <div className="attachment-kind">{attachmentSubtitle(attachment)}{attachment.truncated ? ' · truncated' : ''}</div>
      </div>
      {onRemove && <button className="attachment-remove" onClick={onRemove} title="Remove attachment">x</button>}
    </div>
  )
}

function ToolCountdownCard({
  countdown,
  nowMs,
  onCancel,
}: {
  countdown: ToolCountdown
  nowMs: number
  onCancel: (name: string) => void
}) {
  const remainingMs = Math.max(0, countdown.deadline - nowMs)
  const remainingSec = Math.ceil(remainingMs / 1000)
  const elapsed = Math.max(0, nowMs - countdown.startedAt)
  const total = Math.max(1, countdown.timeoutSec * 1000)
  const pct = Math.min(100, Math.round((elapsed / total) * 100))
  const isShell = countdown.name === 'shell'
  return (
    <div className="tool-countdown-card">
      <div className="tool-countdown-row">
        <div className="tool-countdown-title">
          <span>{countdown.name}</span>
          <span>{formatDuration(remainingSec)} left</span>
        </div>
        <button className="tool-countdown-cancel" onClick={() => onCancel(countdown.name)} title={isShell ? 'Interrupt this shell call' : 'Stop the current tool call'}>
          {isShell ? 'Cancel shell' : 'Cancel'}
        </button>
      </div>
      <div className="tool-countdown-track">
        <div className="tool-countdown-fill" style={{ width: `${pct}%` }} />
      </div>
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
  onReadResult,
}: {
  msg: ChatMessage
  onCodeBlock: (code: string, lang: string) => void
  onImageClick: (src: string) => void
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
    void navigator.clipboard.writeText(msg.content).then(() => {
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

  const canCopy = msg.role === 'assistant' || msg.role === 'system' || msg.role === 'tool_result'

  return (
    <div className={roleClass}>
      <div className="msg-header">
        <span className="msg-role">{roleLabel[msg.role]}</span>
        {msg.queued && <span className="msg-queued-badge">queued</span>}
        {msg.timestamp > 0 && <span className="msg-time">{formatMsgTime(msg.timestamp)}</span>}
        {msg.role === 'assistant' && (
          <button className="msg-save-btn" onClick={() => void saveMessage()} title="Save reply to file">
            {saved ? '✓' : 'Save'}
          </button>
        )}
        {canCopy && (
          <button className="msg-copy-btn" onClick={copyMessage} title="Copy message">
            {copied ? '✓' : 'Copy'}
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
              <AttachmentChip key={att.id || `${att.name}-${index}`} attachment={att} />
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
            {collapsed ? `▼ Show all ${lineCount} lines` : '▲ Collapse'}
          </button>
        )}
      </div>
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
    <details className={`thinking-block ${live ? 'live' : ''}`} open={open} onToggle={e => setOpen(e.currentTarget.open)}>
      <summary>{live ? 'Thinking...' : 'Thinking'} {open ? '▲' : '▼'}</summary>
      <pre>{text}</pre>
    </details>
  )
}
