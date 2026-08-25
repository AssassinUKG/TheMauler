import { useState } from 'react'
import type { LabScopeTarget } from '../wailsjs/go'
import { blankScopeTarget, normaliseScopeTargets, scopeKind, splitScopeValues } from './scopeTargets'
import './AuthorisedScopeEditor.css'

interface Props {
  value: LabScopeTarget[]
  onChange: (value: LabScopeTarget[]) => void
}

export function AuthorisedScopeEditor({ value, onChange }: Props) {
  const [pasteDraft, setPasteDraft] = useState('')
  const entries = value || []
  const allowed = entries.filter(entry => !entry.excluded && scopeKind(entry.value) !== 'invalid').length
  const excluded = entries.filter(entry => entry.excluded && scopeKind(entry.value) !== 'invalid').length

  const update = (index: number, patch: Partial<LabScopeTarget>) => {
    const next = entries.map((entry, row) => row === index ? { ...entry, ...patch } : entry)
    if (patch.value !== undefined) next[index].kind = scopeKind(patch.value)
    onChange(next)
  }

  const move = (index: number, offset: number) => {
    const destination = index + offset
    if (destination < 0 || destination >= entries.length) return
    const next = [...entries]
    ;[next[index], next[destination]] = [next[destination], next[index]]
    onChange(next)
  }

  const importList = () => {
    const additions = splitScopeValues(pasteDraft).map(raw => {
      const excluded = raw.startsWith('!')
      const value = excluded ? raw.slice(1).trim() : raw
      return { ...blankScopeTarget(value), excluded }
    })
    if (!additions.length) return
    onChange(normaliseScopeTargets([...entries, ...additions]))
    setPasteDraft('')
  }

  return <section className="authorised-scope-editor">
    <header>
      <div>
        <span>Authorised scope</span>
        <p>Add every approved IP, CIDR, hostname, or HTTP(S) URL. The first allowed row is the primary target used by older Mauler surfaces.</p>
      </div>
      <div className="scope-counts"><strong>{allowed} allowed</strong>{excluded > 0 && <strong className="excluded">{excluded} excluded</strong>}</div>
    </header>

    <div className="scope-import-row">
      <textarea value={pasteDraft} onChange={event => setPasteDraft(event.target.value)} placeholder={'Paste targets separated by commas or new lines\n13.134.229.195\n3.9.20.132\n10.20.30.0/24'} />
      <div><button type="button" onClick={importList} disabled={!pasteDraft.trim()}>Add list</button><small>Prefix an exclusion with <code>!</code>.</small></div>
    </div>

    <div className="scope-target-list">
      {entries.map((entry, index) => {
        const kind = scopeKind(entry.value)
        return <article key={index} className={`${entry.excluded ? 'is-excluded' : ''}${kind === 'invalid' ? ' is-invalid' : ''}`}>
          <div className="scope-row-main">
            <select aria-label={`Scope action ${index + 1}`} value={entry.excluded ? 'exclude' : 'allow'} onChange={event => update(index, { excluded: event.target.value === 'exclude' })}>
              <option value="allow">Allow</option><option value="exclude">Exclude</option>
            </select>
            <label><span>Target</span><input value={entry.value} onChange={event => update(index, { value: event.target.value })} placeholder="IP, CIDR, host, host:port, or URL/path" /></label>
            <span className={`scope-kind kind-${kind}`}>{kind}</span>
            <select aria-label={`Environment ${index + 1}`} value={entry.environment || 'auto'} onChange={event => update(index, { environment: event.target.value as LabScopeTarget['environment'] })}>
              <option value="auto">Auto</option><option value="external">External</option><option value="internal">Internal</option>
            </select>
            <div className="scope-row-actions">
              <button type="button" onClick={() => move(index, -1)} disabled={index === 0} title="Move up">↑</button>
              <button type="button" onClick={() => move(index, 1)} disabled={index === entries.length - 1} title="Move down">↓</button>
              <button type="button" className="remove" onClick={() => onChange(entries.filter((_, row) => row !== index))}>Remove</button>
            </div>
          </div>
          <div className="scope-row-detail">
            <label><span>Label</span><input value={entry.label || ''} maxLength={120} onChange={event => update(index, { label: event.target.value })} placeholder="e.g. Public API, Office subnet" /></label>
            <label><span>Restrictions / notes</span><input value={entry.notes || ''} maxLength={500} onChange={event => update(index, { notes: event.target.value })} placeholder="Authorisation dates, ports, owner, or testing restrictions" /></label>
          </div>
          {kind === 'invalid' && <p className="scope-row-error">Use one valid IP, CIDR, hostname, host:port, or HTTP(S) URL. Put multiple values through “Add list”.</p>}
        </article>
      })}
      {entries.length === 0 && <div className="scope-empty">No authorised targets yet. Paste a list above or add one row.</div>}
    </div>
    <footer>
      <button type="button" onClick={() => onChange([...entries, blankScopeTarget()])}>+ Add target</button>
      <small>Port and path constraints are enforced when included in the target, for example <code>host:8443</code> or <code>https://host/admin</code>. Exclusions override allows.</small>
    </footer>
  </section>
}
