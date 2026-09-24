import { useEffect, useMemo } from 'react'
import type { SessionSummary } from '../wailsjs/go'
import './ConversationTagFilters.css'

interface Props {
  sessions: SessionSummary[]
  active: string
  onChange: (tag: string) => void
}

export function ConversationTagFilters({ sessions, active, onChange }: Props) {
  const tags = useMemo(() => {
    const byKey = new Map<string, string>()
    sessions.forEach(session => session.tags?.forEach(tag => {
      const key = tag.toLowerCase()
      if (!byKey.has(key)) byKey.set(key, tag)
    }))
    return [...byKey.values()].sort((a, b) => a.localeCompare(b))
  }, [sessions])

  useEffect(() => {
    if (active && !tags.some(tag => tag.toLowerCase() === active.toLowerCase())) onChange('')
  }, [active, onChange, tags])

  if (tags.length === 0) return null
  return (
    <div className="conversation-tag-filters" aria-label="Filter conversations by tag">
      <button className={!active ? 'active' : ''} onClick={() => onChange('')}>All</button>
      {tags.map(tag => (
        <button key={tag.toLowerCase()} className={active.toLowerCase() === tag.toLowerCase() ? 'active' : ''} onClick={() => onChange(active.toLowerCase() === tag.toLowerCase() ? '' : tag)}>{tag}</button>
      ))}
    </div>
  )
}
