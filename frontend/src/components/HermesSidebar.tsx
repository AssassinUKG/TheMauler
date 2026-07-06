import { useMemo, useState } from 'react'
import './HermesSidebar.css'

type CenterTab = 'chat' | 'ops' | 'projects' | 'file' | 'logs' | 'memory' | 'brain' | 'telegram' | 'benchmarks'

interface Props {
  sessions: string[]
  selectedSession: string
  activeProfile: string
  centerTab: CenterTab
  onSelectSession: (name: string) => void
  onLoadSession: () => void
  onSaveSession: () => void
  onDeleteSession: () => void
  onClearChat: () => void
  onSelectTab: (tab: CenterTab) => void
  onOpenSettings: () => void
}

const navItems: Array<{ id: CenterTab; label: string; icon: string }> = [
  { id: 'chat', label: 'Chat', icon: 'Chat' },
  { id: 'ops', label: 'Run', icon: 'Run' },
  { id: 'projects', label: 'Projects', icon: 'Projects' },
  { id: 'logs', label: 'Logs', icon: 'Logs' },
  { id: 'memory', label: 'Memory', icon: 'Memory' },
  { id: 'brain', label: 'Brain', icon: 'Brain' },
  { id: 'telegram', label: 'Telegram', icon: 'Telegram' },
  { id: 'benchmarks', label: 'Benchmarks', icon: 'Bench' },
]

export function HermesSidebar({
  sessions,
  selectedSession,
  activeProfile,
  centerTab,
  onSelectSession,
  onLoadSession,
  onSaveSession,
  onDeleteSession,
  onClearChat,
  onSelectTab,
  onOpenSettings,
}: Props) {
  const [filter, setFilter] = useState('')
  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return q ? sessions.filter(name => name.toLowerCase().includes(q)) : sessions
  }, [filter, sessions])

  return (
    <aside className="hermes-sidebar">
      <div className="hermes-brand">
        <div className="hermes-logo">M</div>
        <div>
          <strong>TheMauler</strong>
          <span>local workbench</span>
        </div>
      </div>

      <div className="hermes-nav">
        {navItems.map(item => (
          <button
            key={item.id}
            className={centerTab === item.id ? 'active' : ''}
            onClick={() => onSelectTab(item.id)}
            title={item.label}
          >
            <span>{item.icon}</span>
            <strong>{item.label}</strong>
          </button>
        ))}
      </div>

      <button className="hermes-new-chat" onClick={onClearChat}>+ New conversation</button>
      <input
        className="hermes-session-filter"
        value={filter}
        onChange={e => setFilter(e.target.value)}
        placeholder="Filter conversations..."
      />

      <div className="hermes-session-list">
        {filtered.length === 0 ? (
          <div className="hermes-empty-sessions">No saved sessions</div>
        ) : filtered.map((name, index) => (
          <button
            key={name}
            className={selectedSession === name ? 'active' : ''}
            onClick={() => onSelectSession(name)}
            onDoubleClick={onLoadSession}
            title={name}
          >
            <span>{index < 2 ? 'PINNED' : index < 8 ? 'RECENT' : 'OLDER'}</span>
            <strong>{name}</strong>
          </button>
        ))}
      </div>

      <div className="hermes-sidebar-bottom">
        <div className="hermes-session-head">
          <span>Sessions</span>
          <strong title={`Active profile: ${activeProfile || 'none'}`}>{selectedSession || 'unsaved'}</strong>
        </div>
        <div className="hermes-session-actions">
          <button onClick={onSaveSession}>Save</button>
          <button onClick={onLoadSession} disabled={!selectedSession}>Load</button>
          <button onClick={onDeleteSession} disabled={!selectedSession}>Del</button>
        </div>
        <button className="hermes-settings" onClick={onOpenSettings}>Control Center</button>
      </div>
    </aside>
  )
}
