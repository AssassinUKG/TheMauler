import { useEffect, useMemo, useRef, useState } from 'react'
import type { SessionSummary } from '../wailsjs/go'
import { ConversationActions } from './ConversationActions'
import { ConversationLibraryRow } from './ConversationLibraryRow'
import { ConversationModeFilters, normalizedConversationMode } from './ConversationModeFilters'
import { ConversationTagFilters } from './ConversationTagFilters'
import './HermesSidebar.css'
import './ConversationNavigationEnhancements.css'

type CenterTab = 'chat' | 'ops' | 'projects' | 'engagement' | 'services' | 'file' | 'logs' | 'memory' | 'brain' | 'context' | 'telegram' | 'benchmarks' | 'doctor'

interface Props {
  sessions: SessionSummary[]
  selectedSession: string
  activeProfile: string
  workspaceLabel: string
  streaming: boolean
  centerTab: CenterTab
  onNewChat: () => void
  onOpenSession: (name: string) => void
  onSaveChat: () => void
  onRenameChat: (name?: string) => void
  onEditChatTags: (name?: string) => void
  onInspectChat: (name?: string) => void
  onOpenCheckpoints: () => void
  onDeleteChat: (name?: string) => void
  onChatModeChange: (name: string, mode: 'adaptive' | 'direct' | 'agent') => void
  onSelectTab: (tab: CenterTab) => void
  onClose: () => void
}

const navItems: Array<{ id: CenterTab; label: string; icon: string }> = [
  { id: 'chat', label: 'Chat', icon: '◉' },
  { id: 'ops', label: 'Run', icon: '▶' },
  { id: 'projects', label: 'Workspaces', icon: '⌂' },
  { id: 'engagement', label: 'Grid', icon: '▦' },
  { id: 'benchmarks', label: 'Bench', icon: '◇' },
]

const secondaryNavItems: Array<{ id: CenterTab; label: string }> = [
  { id: 'services', label: 'Services' },
  { id: 'logs', label: 'Logs' },
  { id: 'memory', label: 'Memory' },
  { id: 'brain', label: 'Brain' },
  { id: 'context', label: 'Context' },
  { id: 'telegram', label: 'Telegram' },
  { id: 'doctor', label: 'Doctor' },
]

export function HermesSidebar({
  sessions,
  selectedSession,
  activeProfile,
  workspaceLabel,
  streaming,
  centerTab,
  onNewChat,
  onOpenSession,
  onSaveChat,
  onRenameChat,
  onEditChatTags,
  onInspectChat,
  onOpenCheckpoints,
  onDeleteChat,
  onChatModeChange,
  onSelectTab,
  onClose,
}: Props) {
  const [query, setQuery] = useState('')
  const [activeTag, setActiveTag] = useState('')
  const [activeMode, setActiveMode] = useState('')
  const [actionsOpen, setActionsOpen] = useState(false)
  const [workbenchOpen, setWorkbenchOpen] = useState(centerTab !== 'chat')
  const actionsRef = useRef<HTMLDivElement>(null)
  const filteredSessions = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return sessions.filter(session => {
      const matchesQuery = !needle || session.name.toLowerCase().includes(needle) || session.tags?.some(tag => tag.toLowerCase().includes(needle))
      const matchesTag = !activeTag || session.tags?.some(tag => tag.toLowerCase() === activeTag.toLowerCase())
      const matchesMode = !activeMode || normalizedConversationMode(session.conversation_mode) === activeMode
      return matchesQuery && matchesTag && matchesMode
    })
  }, [activeMode, activeTag, query, sessions])

  useEffect(() => {
    if (centerTab !== 'chat') setWorkbenchOpen(true)
  }, [centerTab])

  useEffect(() => {
    if (!actionsOpen) return
    const dismiss = (event: globalThis.PointerEvent) => {
      if (event.target instanceof Node && !actionsRef.current?.contains(event.target)) setActionsOpen(false)
    }
    const dismissOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') setActionsOpen(false)
    }
    window.addEventListener('pointerdown', dismiss)
    window.addEventListener('keydown', dismissOnEscape)
    return () => {
      window.removeEventListener('pointerdown', dismiss)
      window.removeEventListener('keydown', dismissOnEscape)
    }
  }, [actionsOpen])

  const runConversationAction = (action: () => void) => {
    setActionsOpen(false)
    action()
  }

  return (
    <aside className="hermes-sidebar">
      <div className="hermes-panel-head">
        <button className="hermes-new-conversation" onClick={onNewChat} disabled={streaming} title="New conversation (Ctrl+N)"><span>＋</span><strong>New chat</strong></button>
        <div className={`hermes-chat-actions${actionsOpen ? ' open' : ''}`} ref={actionsRef}>
          <button className="hermes-chat-actions-trigger" onClick={() => setActionsOpen(value => !value)} title="Conversation actions" aria-label="Conversation actions" aria-haspopup="dialog" aria-expanded={actionsOpen}>•••</button>
          {actionsOpen && <div role="dialog" aria-label="Conversation actions">
            <ConversationActions
              selected={selectedSession}
              streaming={streaming}
              compact
              onSave={() => runConversationAction(onSaveChat)}
              onRename={() => runConversationAction(() => onRenameChat())}
              onEditTags={() => runConversationAction(() => onEditChatTags())}
              onInspect={() => runConversationAction(() => onInspectChat())}
              onCheckpoints={() => runConversationAction(onOpenCheckpoints)}
              onDelete={() => runConversationAction(() => onDeleteChat())}
            />
          </div>}
        </div>
        <button onClick={onClose} title="Close chat sidebar (Ctrl+B)" aria-label="Close chat sidebar">×</button>
      </div>

      <label className="hermes-chat-search">
        <span aria-hidden="true">⌕</span>
        <input value={query} onChange={event => setQuery(event.target.value)} placeholder="Search chats" />
      </label>

      <div className="hermes-library-filters">
        <ConversationModeFilters sessions={sessions} active={activeMode} onChange={setActiveMode} />
        <ConversationTagFilters sessions={sessions} active={activeTag} onChange={setActiveTag} />
      </div>

      <div className="hermes-conversations" aria-label="Saved conversations">
        <span className="hermes-section-label"><span>Conversations</span><small>{filteredSessions.length}{query ? ` / ${sessions.length}` : ''}</small></span>
        {filteredSessions.length === 0 ? <div className="hermes-empty-sessions"><span>{query ? 'No matching conversations' : 'No saved chats yet'}</span>{query && <button onClick={() => setQuery('')}>Clear search</button>}</div> : filteredSessions.map(session => (
          <ConversationLibraryRow
            key={session.name}
            summary={session}
            current={selectedSession === session.name}
            streaming={streaming}
            onOpen={() => onOpenSession(session.name)}
            onRename={() => onRenameChat(session.name)}
            onEditTags={() => onEditChatTags(session.name)}
            onInspect={() => onInspectChat(session.name)}
            onDelete={() => onDeleteChat(session.name)}
            onModeChange={mode => onChatModeChange(session.name, mode)}
          />
        ))}
      </div>

      <details className="hermes-workbench" open={workbenchOpen} onToggle={event => setWorkbenchOpen(event.currentTarget.open)}>
        <summary>Workbench</summary>
        <div className="hermes-nav">
          {navItems.map(item => <button key={item.id} className={centerTab === item.id ? 'active' : ''} onClick={() => onSelectTab(item.id)} title={item.label}><span>{item.icon}</span><strong>{item.label}</strong></button>)}
        </div>
        <select className="hermes-more-nav" value={secondaryNavItems.some(item => item.id === centerTab) ? centerTab : ''} onChange={e => { if (e.target.value) onSelectTab(e.target.value as CenterTab) }}>
          <option value="">More pages…</option>
          {secondaryNavItems.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
        </select>
      </details>

      <div className="hermes-context-card" title={`${workspaceLabel || 'No workspace'} · ${activeProfile || 'Local model'}`}>
        <span aria-hidden="true">M</span>
        <div><strong>{workspaceLabel || 'No workspace'}</strong><small>{activeProfile || 'Local model'}</small></div>
      </div>
    </aside>
  )
}
