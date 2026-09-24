import { useEffect, useMemo, useRef, useState } from 'react'
import type { SessionSummary } from '../wailsjs/go'
import { ConversationActions } from './ConversationActions'
import { ConversationLibraryRow } from './ConversationLibraryRow'
import { ConversationModeFilters, conversationModeLabel, normalizedConversationMode } from './ConversationModeFilters'
import { ConversationTagFilters } from './ConversationTagFilters'
import './ConversationMenu.css'
import './ConversationNavigationEnhancements.css'

interface Props {
  sessions: SessionSummary[]
  selected: string
  streaming: boolean
  onOpen: (name: string) => void
  onNew: () => void
  onSave: () => void
  onRename: (name?: string) => void
  onEditTags: (name?: string) => void
  onInspect: (name?: string) => void
  onCheckpoints: () => void
  checkpointCount: number
  onDelete: (name?: string) => void
  onModeChange: (name: string, mode: 'adaptive' | 'direct' | 'agent') => void
}

export function ConversationMenu({ sessions, selected, streaming, onOpen, onNew, onSave, onRename, onEditTags, onInspect, onCheckpoints, checkpointCount, onDelete, onModeChange }: Props) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [activeTag, setActiveTag] = useState('')
  const [activeMode, setActiveMode] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const currentSummary = useMemo(() => sessions.find(session => session.name === selected), [selected, sessions])
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return sessions.filter(session => {
      const matchesQuery = !needle || session.name.toLowerCase().includes(needle) || session.tags?.some(tag => tag.toLowerCase().includes(needle))
      const matchesTag = !activeTag || session.tags?.some(tag => tag.toLowerCase() === activeTag.toLowerCase())
      const matchesMode = !activeMode || normalizedConversationMode(session.conversation_mode) === activeMode
      return matchesQuery && matchesTag && matchesMode
    })
  }, [activeMode, activeTag, query, sessions])

  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const keyboard = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    window.addEventListener('mousedown', close)
    window.addEventListener('keydown', keyboard)
    window.setTimeout(() => searchRef.current?.focus(), 0)
    return () => {
      window.removeEventListener('mousedown', close)
      window.removeEventListener('keydown', keyboard)
    }
  }, [open])

  const act = (callback: () => void) => {
    setOpen(false)
    callback()
  }

  return (
    <div className="conversation-menu" ref={rootRef}>
      <button
        className={`conversation-menu-trigger${open ? ' active' : ''}`}
        onClick={() => setOpen(value => !value)}
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Conversation actions"
      >
        <span>Conversation</span>
        <strong>{selected || 'Unsaved chat'}</strong>
        <i aria-hidden="true">⌄</i>
      </button>
      {open && (
        <section className="conversation-menu-popover" role="dialog" aria-label="Conversation menu">
          <header>
            <div className="conversation-current-summary">
              <span>Current conversation</span>
              <strong>{selected || 'Unsaved chat'}</strong>
              <small>
                <b className={`conversation-library-mode mode-${normalizedConversationMode(currentSummary?.conversation_mode)}`}>{conversationModeLabel(currentSummary?.conversation_mode)}</b>
                {currentSummary ? `${currentSummary.message_count} message${currentSummary.message_count === 1 ? '' : 's'}` : 'Not saved yet'}
              </small>
            </div>
            <button onClick={() => setOpen(false)} title="Close">×</button>
          </header>
          <div className="conversation-menu-actions">
            <ConversationActions
              selected={selected}
              streaming={streaming}
              includeNew
              onNew={() => act(onNew)}
              onSave={() => act(onSave)}
              onRename={() => act(() => onRename())}
              onEditTags={() => act(() => onEditTags())}
              onInspect={() => act(() => onInspect())}
              onCheckpoints={() => act(onCheckpoints)}
              onDelete={() => act(() => onDelete())}
            />
          </div>
          <label className="conversation-menu-search">
            <span>Find saved conversations</span>
            <input ref={searchRef} value={query} onChange={event => setQuery(event.target.value)} placeholder="Search titles…" />
          </label>
          <div className="conversation-menu-filters">
            <ConversationModeFilters sessions={sessions} active={activeMode} onChange={setActiveMode} />
            <ConversationTagFilters sessions={sessions} active={activeTag} onChange={setActiveTag} />
          </div>
          <div className="conversation-menu-list">
            {filtered.length === 0 ? <div className="conversation-menu-empty">No matching saved conversations.</div> : filtered.map(session => (
              <ConversationLibraryRow
                key={session.name}
                summary={session}
                current={session.name === selected}
                streaming={streaming}
                onOpen={() => act(() => onOpen(session.name))}
                onRename={() => act(() => onRename(session.name))}
                onEditTags={() => act(() => onEditTags(session.name))}
                onInspect={() => act(() => onInspect(session.name))}
                onDelete={() => act(() => onDelete(session.name))}
                onModeChange={mode => act(() => onModeChange(session.name, mode))}
              />
            ))}
          </div>
          <footer>
            <span>{filtered.length} of {sessions.length} saved chats · {checkpointCount} checkpoint{checkpointCount === 1 ? '' : 's'}.</span>
          </footer>
        </section>
      )}
    </div>
  )
}
