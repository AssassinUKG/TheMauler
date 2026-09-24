import { useEffect, useRef, useState } from 'react'
import type { SessionSummary } from '../wailsjs/go'
import { conversationModeLabel, normalizedConversationMode } from './ConversationModeFilters'
import './ConversationLibraryRow.css'
import './ConversationModeBadge.css'

interface Props {
  summary: SessionSummary
  current: boolean
  streaming: boolean
  onOpen: () => void
  onRename: () => void
  onEditTags: () => void
  onInspect: () => void
  onDelete: () => void
  onModeChange: (mode: 'adaptive' | 'direct' | 'agent') => void
}

function relativeTime(unix: number) {
  if (!unix) return 'Unknown time'
  const delta = Math.max(0, Math.floor(Date.now() / 1000) - unix)
  if (delta < 60) return 'Just now'
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`
  if (delta < 172800) return 'Yesterday'
  if (delta < 604800) return `${Math.floor(delta / 86400)}d ago`
  return new Date(unix * 1000).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}

export function ConversationLibraryRow({ summary, current, streaming, onOpen, onRename, onEditTags, onInspect, onDelete, onModeChange }: Props) {
  const [menuOpen, setMenuOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!menuOpen) return
    const dismiss = (event: PointerEvent) => {
      if (event.target instanceof Node && !rootRef.current?.contains(event.target)) setMenuOpen(false)
    }
    const keyboard = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenuOpen(false)
    }
    window.addEventListener('pointerdown', dismiss)
    window.addEventListener('keydown', keyboard)
    return () => {
      window.removeEventListener('pointerdown', dismiss)
      window.removeEventListener('keydown', keyboard)
    }
  }, [menuOpen])

  const act = (callback: () => void) => {
    setMenuOpen(false)
    callback()
  }
  const messageLabel = `${summary.message_count} message${summary.message_count === 1 ? '' : 's'}`

  return (
    <div className={`conversation-library-row${current ? ' current' : ''}`} ref={rootRef}>
      <button className="conversation-library-open" onClick={onOpen} disabled={streaming || current} title={`Open ${summary.name}`}>
        <span className="conversation-library-mark" aria-hidden="true">◌</span>
        <span className="conversation-library-copy">
          <strong>{summary.name}</strong>
          <small>{relativeTime(summary.updated_unix)} · {messageLabel}</small>
          <span className={`conversation-library-mode mode-${summary.conversation_mode || 'adaptive'}`}>{conversationModeLabel(summary.conversation_mode)}</span>
          {summary.tags?.length > 0 && <span className="conversation-library-tags">{summary.tags.slice(0, 2).map(tag => <b key={tag.toLowerCase()}>{tag}</b>)}{summary.tags.length > 2 && <b>+{summary.tags.length - 2}</b>}</span>}
        </span>
        {current && <i>Current</i>}
        {summary.status === 'needs-review' && !current && <i className="review">Review</i>}
      </button>
      <button
        className="conversation-library-more"
        onClick={() => setMenuOpen(value => !value)}
        disabled={streaming}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        title={`Actions for ${summary.name}`}
      >•••</button>
      {menuOpen && (
        <div className="conversation-library-menu" role="menu" aria-label={`Actions for ${summary.name}`}>
          <div className="conversation-library-mode-picker" aria-label="Run mode">
            <span>Run mode</span>
            <div>
              {(['adaptive', 'direct', 'agent'] as const).map(mode => (
                <button
                  key={mode}
                  className={normalizedConversationMode(summary.conversation_mode) === mode ? 'active' : ''}
                  onClick={() => act(() => onModeChange(mode))}
                >{conversationModeLabel(mode)}</button>
              ))}
            </div>
          </div>
          <button role="menuitem" onClick={() => act(onRename)}>Rename</button>
          <button role="menuitem" onClick={() => act(onEditTags)}>Edit tags</button>
          <button role="menuitem" onClick={() => act(onInspect)}>Check &amp; repair</button>
          <button role="menuitem" className="danger" onClick={() => act(onDelete)}>Delete</button>
        </div>
      )}
    </div>
  )
}
