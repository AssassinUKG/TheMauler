import { useEffect, useMemo } from 'react'
import type { SessionSummary } from '../wailsjs/go'
import './ConversationModeFilters.css'

interface Props {
  sessions: SessionSummary[]
  active: string
  onChange: (mode: string) => void
}

const modes = [
  { id: 'adaptive', label: 'Adaptive' },
  { id: 'direct', label: 'Direct' },
  { id: 'agent', label: 'Agent' },
] as const

export function normalizedConversationMode(mode: string | undefined) {
  return mode === 'direct' || mode === 'agent' ? mode : 'adaptive'
}

export function conversationModeLabel(mode: string | undefined) {
  const normalized = normalizedConversationMode(mode)
  return normalized === 'direct' ? 'Direct' : normalized === 'agent' ? 'Agent' : 'Adaptive'
}

export function ConversationModeFilters({ sessions, active, onChange }: Props) {
  const counts = useMemo(() => sessions.reduce<Record<string, number>>((result, session) => {
    const mode = normalizedConversationMode(session.conversation_mode)
    result[mode] = (result[mode] || 0) + 1
    return result
  }, {}), [sessions])

  useEffect(() => {
    if (active && !counts[active]) onChange('')
  }, [active, counts, onChange])

  if (sessions.length === 0) return null
  return (
    <div className="conversation-mode-filters" aria-label="Filter conversations by run mode">
      <button className={!active ? 'active' : ''} onClick={() => onChange('')}>All <span>{sessions.length}</span></button>
      {modes.map(mode => (
        <button
          key={mode.id}
          className={active === mode.id ? 'active' : ''}
          onClick={() => onChange(active === mode.id ? '' : mode.id)}
          disabled={!counts[mode.id]}
        >{mode.label} <span>{counts[mode.id] || 0}</span></button>
      ))}
    </div>
  )
}
