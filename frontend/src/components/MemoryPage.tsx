import { useEffect, useMemo, useState } from 'react'
import {
  ClearMemoryEntries,
  ClearSessionRecall,
  DeleteMemoryEntry,
  ListMemory,
  ReindexSessionRecall,
  SaveMemoryEntry,
  SearchSessionRecall,
  type MemoryEntry,
  type SessionSearchResult,
} from '../wailsjs/go'
import './MemoryPage.css'

export function MemoryPage({ version }: { version: number }) {
  const [memory, setMemory] = useState<MemoryEntry[]>([])
  const [query, setQuery] = useState('')
  const [selectedId, setSelectedId] = useState('')
  const [draft, setDraft] = useState(blankMemory())
  const [editing, setEditing] = useState<MemoryEntry | null>(null)
  const [recallQuery, setRecallQuery] = useState('')
  const [recallResults, setRecallResults] = useState<SessionSearchResult[]>([])
  const [recallStatus, setRecallStatus] = useState('')

  const load = async () => {
    const entries = await ListMemory().catch(() => [] as MemoryEntry[])
    setMemory(entries)
    setSelectedId(prev => prev && entries.some(item => item.id === prev) ? prev : entries[0]?.id ?? '')
  }

  useEffect(() => {
    void load()
  }, [version])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return memory
    return memory.filter(item => [
      item.title,
      item.kind,
      item.content,
      ...(item.tags ?? []),
    ].join(' ').toLowerCase().includes(q))
  }, [memory, query])

  const selected = filtered.find(item => item.id === selectedId) ?? filtered[0] ?? null

  useEffect(() => {
    if (filtered.length > 0 && (!selectedId || !filtered.some(item => item.id === selectedId))) {
      setSelectedId(filtered[0].id)
    }
  }, [filtered, selectedId])

  const saveNew = async () => {
    if (!draft.title.trim() && !draft.content.trim()) return
    const saved = await SaveMemoryEntry({
      ...blankMemory(),
      title: draft.title.trim() || 'Project note',
      content: draft.content.trim(),
      tags: draft.tags,
      kind: draft.kind || 'note',
      importance: draft.importance || 3,
      pinned: draft.pinned,
    })
    setMemory(items => [saved, ...items.filter(item => item.id !== saved.id)])
    setDraft(blankMemory())
    setSelectedId(saved.id)
  }

  const saveEdit = async () => {
    if (!editing) return
    const saved = await SaveMemoryEntry({
      ...editing,
      title: editing.title.trim() || 'Project note',
      content: editing.content.trim(),
      tags: (editing.tags ?? []).map(tag => tag.trim()).filter(Boolean),
      importance: editing.importance || 3,
    })
    setMemory(items => [saved, ...items.filter(item => item.id !== saved.id)])
    setEditing(null)
    setSelectedId(saved.id)
  }

  const searchRecall = async () => {
    const q = recallQuery.trim()
    if (!q) return
    setRecallStatus('searching')
    try {
      const results = await SearchSessionRecall(q, 20)
      setRecallResults(results)
      setRecallStatus(results.length === 0 ? 'no matches' : `${results.length} match${results.length === 1 ? '' : 'es'}`)
    } catch (e) {
      setRecallStatus(`error: ${String(e)}`)
    }
  }

  const reindexRecall = async () => {
    setRecallStatus('reindexing')
    try {
      const count = await ReindexSessionRecall()
      setRecallStatus(`indexed ${count} session${count === 1 ? '' : 's'}`)
    } catch (e) {
      setRecallStatus(`error: ${String(e)}`)
    }
  }

  return (
    <div className="memory-page">
      <header className="memory-header">
        <div>
          <h1>Memory</h1>
          <p>Curated project memory and searchable recall from saved sessions.</p>
        </div>
        <div className="memory-actions">
          <button onClick={() => void load()}>Refresh</button>
          <button
            className="danger"
            onClick={async () => {
              if (!confirm('Delete all curated memory entries?')) return
              await ClearMemoryEntries()
              setMemory([])
              setSelectedId('')
            }}
          >
            Clear Memory
          </button>
        </div>
      </header>

      <div className="memory-layout">
        <aside className="memory-list">
          <input
            className="memory-search"
            value={query}
            onChange={e => setQuery(e.target.value)}
            placeholder="Search memory..."
          />
          {filtered.length === 0 ? (
            <div className="memory-empty">{memory.length === 0 ? 'No memory saved yet' : 'No matching memory'}</div>
          ) : filtered.map(item => (
            <button
              key={item.id}
              className={`memory-card ${selected?.id === item.id ? 'active' : ''}`}
              onClick={() => { setSelectedId(item.id); setEditing(null) }}
            >
              <div className="memory-card-line">
                <span className={item.pinned ? 'memory-pill pinned' : 'memory-pill'}>{item.kind || 'note'}</span>
                <span className="memory-importance">i{item.importance || 3}</span>
              </div>
              <div className="memory-card-title">{item.title || 'Project note'}</div>
              <div className="memory-card-body">{item.content}</div>
              {(item.tags ?? []).length > 0 && (
                <div className="memory-tags">{item.tags.map(tag => <span key={tag}>{tag}</span>)}</div>
              )}
            </button>
          ))}
        </aside>

        <main className="memory-detail">
          <section className="memory-section">
            <h2>Add Memory</h2>
            <MemoryEditor entry={draft} onChange={setDraft} />
            <div className="memory-button-row">
              <button className="primary" onClick={() => void saveNew()}>Add Memory</button>
              <button onClick={() => setDraft(blankMemory())}>Reset</button>
            </div>
          </section>

          <section className="memory-section">
            <h2>Selected Memory</h2>
            {!selected ? (
              <div className="memory-empty">Select a memory entry to inspect or edit it.</div>
            ) : editing?.id === selected.id ? (
              <>
                <MemoryEditor entry={editing} onChange={setEditing} />
                <div className="memory-button-row">
                  <button className="primary" onClick={() => void saveEdit()}>Save</button>
                  <button onClick={() => setEditing(null)}>Cancel</button>
                </div>
              </>
            ) : (
              <>
                <div className="memory-selected-head">
                  <div>
                    <div className="memory-selected-title">{selected.title || 'Project note'}</div>
                    <div className="memory-selected-meta">{selected.kind || 'note'} / importance {selected.importance || 3}</div>
                  </div>
                  <div className="memory-button-row">
                    <button onClick={() => setEditing(selected)}>Edit</button>
                    <button
                      className="danger"
                      onClick={async () => {
                        await DeleteMemoryEntry(selected.id)
                        setMemory(items => items.filter(item => item.id !== selected.id))
                        setSelectedId('')
                      }}
                    >
                      Delete
                    </button>
                  </div>
                </div>
                <pre className="memory-content">{selected.content}</pre>
                {(selected.tags ?? []).length > 0 && (
                  <div className="memory-tags large">{selected.tags.map(tag => <span key={tag}>{tag}</span>)}</div>
                )}
              </>
            )}
          </section>

          <section className="memory-section">
            <h2>Session Recall</h2>
            <div className="recall-toolbar">
              <input
                value={recallQuery}
                onChange={e => setRecallQuery(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter') void searchRecall() }}
                placeholder="Search previous chats..."
              />
              <button onClick={() => void searchRecall()}>Search</button>
              <button onClick={() => void reindexRecall()}>Reindex</button>
              <button
                onClick={async () => {
                  if (!confirm('Clear the local recall index? Saved sessions are not deleted.')) return
                  await ClearSessionRecall()
                  setRecallResults([])
                  setRecallStatus('recall index cleared')
                }}
              >
                Clear Index
              </button>
            </div>
            {recallStatus && <div className="memory-muted">{recallStatus}</div>}
            <div className="recall-results">
              {recallResults.length === 0 ? (
                <div className="memory-empty">No recall results loaded.</div>
              ) : recallResults.map(result => (
                <details key={`${result.session_id}-${result.message_id}`} className="recall-result">
                  <summary>
                    <span className="memory-pill">{result.role}</span>
                    <span>{result.session_name}</span>
                    {result.updated_at && <time>{new Date(result.updated_at).toLocaleDateString()}</time>}
                  </summary>
                  <pre>{result.content}</pre>
                </details>
              ))}
            </div>
          </section>
        </main>
      </div>
    </div>
  )
}

function blankMemory(): MemoryEntry {
  return {
    id: '',
    scope: '',
    title: '',
    content: '',
    tags: [],
    kind: 'note',
    importance: 3,
    pinned: false,
    created_at: '',
    updated_at: '',
    last_used_at: '',
  }
}

function MemoryEditor({
  entry,
  onChange,
}: {
  entry: MemoryEntry
  onChange: (entry: MemoryEntry) => void
}) {
  return (
    <div className="memory-editor">
      <input
        value={entry.title}
        onChange={e => onChange({ ...entry, title: e.target.value })}
        placeholder="Title"
      />
      <div className="memory-editor-row">
        <select value={entry.kind || 'note'} onChange={e => onChange({ ...entry, kind: e.target.value })}>
          {['note', 'preference', 'constraint', 'fact', 'workflow', 'decision'].map(kind => <option key={kind}>{kind}</option>)}
        </select>
        <input
          type="number"
          min={1}
          max={5}
          value={entry.importance || 3}
          onChange={e => onChange({ ...entry, importance: parseInt(e.target.value, 10) || 3 })}
          title="Importance"
        />
        <label className="memory-check">
          <input
            type="checkbox"
            checked={entry.pinned}
            onChange={e => onChange({ ...entry, pinned: e.target.checked })}
          />
          Pinned
        </label>
      </div>
      <textarea
        value={entry.content}
        onChange={e => onChange({ ...entry, content: e.target.value })}
        placeholder="What should the agent remember for this workspace?"
      />
      <input
        value={(entry.tags ?? []).join(', ')}
        onChange={e => onChange({ ...entry, tags: e.target.value.split(',').map(tag => tag.trim()).filter(Boolean) })}
        placeholder="tags, comma separated"
      />
    </div>
  )
}
