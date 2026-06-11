import { useRef, useEffect, useState, useCallback, useMemo, type KeyboardEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Undo, EncodeFileBase64, PickSaveFilePath, SaveFileContent, GetWorkingDir, type ChatAttachment } from '../wailsjs/go'
import type { ChatMessage, ToolCountdown } from '../App'
import './ChatPane.css'

interface Props {
  messages: ChatMessage[]
  streaming: boolean
  streamBuffer: string
  thinkingBuffer: string
  profiles: string[]
  activeProfile: string
  autonomous: boolean
  pendingInterrupt: boolean
  toolCountdown: ToolCountdown | null
  onSubmitMessage: (text: string, images: string[], attachments: ChatAttachment[]) => void
  onCancelPending: () => void
  onStopAgent: () => void
  onClearChat: () => void
  onArtifact: (code: string, lang: string) => void
  onProfileChange: (name: string) => void
  onAutonomousChange: (enabled: boolean) => void
}

export function ChatPane({
  messages,
  streaming,
  streamBuffer,
  thinkingBuffer,
  profiles,
  activeProfile,
  autonomous,
  pendingInterrupt,
  toolCountdown,
  onSubmitMessage,
  onCancelPending,
  onStopAgent,
  onClearChat,
  onArtifact,
  onProfileChange,
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
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
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
            <div className="chat-empty-logo">M</div>
            <div className="chat-empty-title">TheMauler</div>
            <div className="chat-empty-sub">Local AI coding assistant</div>
            <div className="chat-empty-shortcuts">
              <div className="chat-shortcut"><kbd>Ctrl+Enter</kbd><span>Send message</span></div>
              <div className="chat-shortcut"><kbd>Enter</kbd><span>New line</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+F</kbd><span>Search chat</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+K</kbd><span>Clear chat</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+,</kbd><span>Settings</span></div>
              <div className="chat-shortcut"><kbd>Ctrl+Z</kbd><span>Undo last edit</span></div>
            </div>
          </div>
        )}
        {visibleMessages.map(msg => (
          <MessageBubble key={msg.id} msg={msg} onCodeBlock={handleCodeBlock} onImageClick={setLightboxImage} />
        ))}

        {/* Live stream bubble */}
        {streaming && (
          <div className="msg msg-assistant msg-streaming">
            <div className="msg-role">Assistant</div>
            {thinkingBuffer && (
              <ThinkingBlock text={thinkingBuffer} live />
            )}
            {toolCountdown && (
              <ToolCountdownCard countdown={toolCountdown} nowMs={nowMs} onCancel={onStopAgent} />
            )}
            {streamBuffer ? (
              <div className="msg-body">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{streamBuffer}</ReactMarkdown>
              </div>
            ) : !thinkingBuffer ? (
              <div className="msg-body thinking-dots">
                <span /><span /><span />
              </div>
            ) : null}
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
        <div className="chat-modebar">
          <label className="chat-mode-control">
            <span>Profile</span>
            <select
              value={activeProfile}
              onChange={e => onProfileChange(e.target.value)}
              disabled={streaming}
            >
              {profiles.map(name => <option key={name} value={name}>{name}</option>)}
            </select>
          </label>
          <label className={`autonomous-toggle ${autonomous ? 'active' : ''}`} title="Autonomous mode lets the agent run tools without confirmation prompts.">
            <input
              type="checkbox"
              checked={autonomous}
              onChange={e => onAutonomousChange(e.target.checked)}
              disabled={streaming}
            />
            Autonomous
          </label>
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
            placeholder="Ask anything... drop a file to attach. Ctrl+Enter sends."
            rows={3}
            disabled={false}
            spellCheck
            lang="en"
            autoCapitalize="sentences"
          />
          <div className="chat-input-actions">
            {streaming && (
              <button className="danger" onClick={onStopAgent}>Stop</button>
            )}
            <button onClick={() => void Undo()} title="Undo last file change (Ctrl+Z)">Undo</button>
            <button className="chat-clear-btn" onClick={onClearChat} title="Clear chat history (Ctrl+K)" disabled={streaming}>Clear</button>
            <button
              className="primary"
              onClick={() => void handleSend()}
              disabled={(!input.trim() && images.length === 0 && attachments.length === 0) || pendingInterrupt}
              title={streaming ? 'Interrupt the current run and send this draft' : 'Send'}
            >
              {streaming ? 'Interrupt & Send' : 'Send'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
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
  onCancel: () => void
}) {
  const remainingMs = Math.max(0, countdown.deadline - nowMs)
  const remainingSec = Math.ceil(remainingMs / 1000)
  const elapsed = Math.max(0, nowMs - countdown.startedAt)
  const total = Math.max(1, countdown.timeoutSec * 1000)
  const pct = Math.min(100, Math.round((elapsed / total) * 100))
  const isShell = countdown.name === 'shell' || countdown.name === 'bash'
  return (
    <div className="tool-countdown-card">
      <div className="tool-countdown-row">
        <div className="tool-countdown-title">
          <span>{countdown.name}</span>
          <span>{formatDuration(remainingSec)} left</span>
        </div>
        <button className="tool-countdown-cancel" onClick={onCancel} title={isShell ? 'Cancel this shell call' : 'Stop the current tool call'}>
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
}: {
  msg: ChatMessage
  onCodeBlock: (code: string, lang: string) => void
  onImageClick: (src: string) => void
}) {
  const [copied, setCopied] = useState(false)
  const [saved, setSaved] = useState(false)
  const COLLAPSE_THRESHOLD = 8
  const isToolMsg = msg.role === 'tool_call' || msg.role === 'tool_result'
  const prettyContent = isToolMsg ? tryPrettyJson(msg.content) : msg.content
  const guardedToolOutput = msg.role === 'tool_result' && prettyContent.startsWith('[Guardrail:')
  const lineCount = prettyContent.split('\n').length
  const collapsible = isToolMsg && lineCount > COLLAPSE_THRESHOLD
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
        {isToolMsg ? (
          <pre className="tool-pre">{prettyContent}</pre>
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
