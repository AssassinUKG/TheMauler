import { useState } from 'react'
import type { RunCheckpoint } from '../wailsjs/go'
import './ConversationCheckpointDialog.css'

interface Props {
  checkpoints: RunCheckpoint[]
  defaultName: string
  canCreate: boolean
  busy: boolean
  runActive: boolean
  onClose: () => void
  onCreate: (name: string) => Promise<boolean>
  onResume: (checkpoint: RunCheckpoint) => Promise<boolean>
  onDelete: (checkpoint: RunCheckpoint) => Promise<boolean>
}

function checkpointTitle(checkpoint: RunCheckpoint) {
  return checkpoint.name || `Automatic recovery · ${checkpoint.run_id.slice(-8)}`
}

function savedLabel(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? 'Unknown time' : date.toLocaleString()
}

export function ConversationCheckpointDialog({ checkpoints, defaultName, canCreate, busy, runActive, onClose, onCreate, onResume, onDelete }: Props) {
  const [draft, setDraft] = useState(defaultName)
  const named = checkpoints.filter(checkpoint => checkpoint.explicit)
  const automatic = checkpoints.filter(checkpoint => !checkpoint.explicit)

  const create = async () => {
    const name = draft.trim()
    if (!name || busy || !canCreate) return
    if (await onCreate(name)) setDraft('')
  }

  return (
    <div className="overlay conversation-checkpoint-overlay">
      <section className="conversation-checkpoint-dialog" role="dialog" aria-modal="true" aria-label="Conversation checkpoints">
        <header>
          <div><span>Conversation recovery</span><strong>Checkpoints</strong></div>
          <button onClick={onClose} disabled={busy} title="Close" aria-label="Close checkpoints">×</button>
        </header>

        <div className="conversation-checkpoint-create">
          <div><strong>Create a named checkpoint</strong><span>Capture this transcript and resume it later as a new linked run.</span></div>
          <div>
            <input
              value={draft}
              onChange={event => setDraft(event.target.value)}
              onKeyDown={event => { if (event.key === 'Enter') void create(); if (event.key === 'Escape' && !busy) onClose() }}
              placeholder="Before validation"
              maxLength={80}
              disabled={busy || !canCreate}
              autoFocus
            />
            <button className="primary" onClick={() => void create()} disabled={busy || !canCreate || !draft.trim()}>Create</button>
          </div>
          {!canCreate && <small>Add a user message before creating a checkpoint.</small>}
        </div>

        <div className="conversation-checkpoint-list">
          <section>
            <div className="conversation-checkpoint-section-title"><strong>Named checkpoints</strong><span>{named.length}</span></div>
            {named.length === 0 ? <p>No named checkpoints yet.</p> : named.map(checkpoint => (
              <article key={checkpoint.run_id}>
                <div>
                  <strong>{checkpointTitle(checkpoint)}</strong>
                  <span>{savedLabel(checkpoint.saved_at)}{checkpoint.conversation_name ? ` · ${checkpoint.conversation_name}` : ''}</span>
                  <p>{checkpoint.prompt || 'Saved conversation state'}</p>
                </div>
                <div>
                  <button className="primary" onClick={() => void onResume(checkpoint)} disabled={busy || runActive}>Resume</button>
                  <button className="danger" onClick={() => { if (window.confirm(`Delete checkpoint “${checkpointTitle(checkpoint)}”?`)) void onDelete(checkpoint) }} disabled={busy || runActive}>Delete</button>
                </div>
              </article>
            ))}
          </section>

          {automatic.length > 0 && <section>
            <div className="conversation-checkpoint-section-title"><strong>Automatic recovery</strong><span>{automatic.length}</span></div>
            {automatic.map(checkpoint => (
              <article key={checkpoint.run_id}>
                <div>
                  <strong>{checkpointTitle(checkpoint)}</strong>
                  <span>{savedLabel(checkpoint.saved_at)} · {checkpoint.mode || 'Agent'}</span>
                  <p>{checkpoint.prompt || 'Interrupted run'}</p>
                </div>
                <div>
                  <button className="primary" onClick={() => void onResume(checkpoint)} disabled={busy || runActive}>Resume</button>
                  <button onClick={() => { if (window.confirm('Remove this automatic recovery snapshot?')) void onDelete(checkpoint) }} disabled={busy || runActive}>Remove</button>
                </div>
              </article>
            ))}
          </section>}
        </div>

        <footer><span>{runActive ? 'Stop the active run to create, resume, or remove checkpoints.' : 'Resume restores the captured transcript and starts a new run generation linked to its parent.'}</span><button onClick={onClose} disabled={busy}>Done</button></footer>
      </section>
    </div>
  )
}
