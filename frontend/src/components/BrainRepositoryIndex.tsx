import { useCallback, useEffect, useState } from 'react'
import {
  CancelWorkspaceRepositoryIndex,
  CancelRepositorySplitReview,
  GetRepositoryIndexStatus,
  GetRepositoryReviewStatus,
  IndexWorkspaceRepository,
  PreviewRepositorySplitReview,
  RefreshWorkspaceRepositoryIndex,
  ResumeRepositorySplitReview,
  SetRepositoryIndexWatch,
  StartRepositorySplitReview,
  RetryRepositoryReviewShard,
  type RepositoryIndexStatus,
  type RepositoryReviewStatus,
  type SplitReviewPreview,
} from '../wailsjs/go'

export function BrainRepositoryIndex({ version }: { version: number }) {
  const [status, setStatus] = useState<RepositoryIndexStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [reviewShards, setReviewShards] = useState(3)
  const [reviewPreview, setReviewPreview] = useState<SplitReviewPreview | null>(null)
  const [reviewStatus, setReviewStatus] = useState<RepositoryReviewStatus | null>(null)

  const load = useCallback(async () => {
    try {
      const next = await GetRepositoryIndexStatus()
      setStatus(next)
      setError('')
      return next
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      return null
    }
  }, [])

  const loadReview = useCallback(async () => {
    try {
      const next = await GetRepositoryReviewStatus()
      setReviewStatus(next)
      return next
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      return null
    }
  }, [])

  useEffect(() => {
    void load()
    void loadReview()
  }, [load, loadReview, version])

  useEffect(() => {
    if (!busy && !status?.indexing && !status?.watch_enabled) return
    const delay = busy || status?.indexing ? 900 : 5000
    const timer = window.setInterval(() => { void load() }, delay)
    return () => window.clearInterval(timer)
  }, [busy, load, status?.indexing, status?.watch_enabled])

  useEffect(() => {
    if (!reviewStatus?.can_cancel && reviewStatus?.state !== 'cancelling') return
    const timer = window.setInterval(() => { void loadReview() }, 900)
    return () => window.clearInterval(timer)
  }, [loadReview, reviewStatus?.can_cancel, reviewStatus?.state])

  useEffect(() => {
    if (reviewPreview && reviewPreview.generation_id !== status?.generation_id) setReviewPreview(null)
  }, [reviewPreview, status?.generation_id])

  const run = async (label: string, action: () => Promise<RepositoryIndexStatus>) => {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const next = await action()
      setStatus(next)
      setNotice(label)
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause)
      if (message.toLowerCase().includes('context canceled')) setNotice('Scan cancelled')
      else setError(message)
      await load()
    } finally {
      setBusy(false)
    }
  }

  const health = status?.health || (error ? 'unavailable' : 'loading')
  const omissions = status?.omissions ?? []
  const sourceCount = 1 + (status?.sources?.length ?? 0)

  const prepareSplitReview = async () => {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const preview = await PreviewRepositorySplitReview(reviewShards)
      setReviewPreview(preview)
      setNotice(`Sealed ${preview.shard_count} read-only review shard${preview.shard_count === 1 ? '' : 's'}`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  const startSplitReview = async () => {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const next = await StartRepositorySplitReview(reviewShards)
      setReviewStatus(next)
      setNotice(`Started ${next.shards.length} sealed review shard${next.shards.length === 1 ? '' : 's'}`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      await loadReview()
    } finally {
      setBusy(false)
    }
  }

  const cancelSplitReview = async () => {
    try {
      setReviewStatus(await CancelRepositorySplitReview())
      setNotice('Review cancellation requested')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }

  const retryReviewShard = async (shardID: string) => {
    setBusy(true)
    setError('')
    try {
      setReviewStatus(await RetryRepositoryReviewShard(shardID))
      setNotice('Shard retry started')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      await loadReview()
    } finally {
      setBusy(false)
    }
  }

  const resumeSplitReview = async () => {
    setBusy(true)
    setError('')
    try {
      const next = await ResumeRepositorySplitReview()
      setReviewStatus(next)
      setNotice('Resumed unfinished sealed shards')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      await loadReview()
    } finally {
      setBusy(false)
    }
  }

  const reviewStale = Boolean(reviewStatus?.generation_id && status?.generation_id && reviewStatus.generation_id !== status.generation_id)

  return (
    <section className="brain-index" data-health={health} aria-label="Workspace knowledge index health">
      <header className="brain-index-header">
        <div className="brain-index-title">
          <span className="brain-index-kicker">Workspace knowledge</span>
          <h2>Repository index</h2>
          <p>{status?.health_detail || (error ? 'Index health could not be loaded.' : 'Loading index health…')}</p>
        </div>
        <div className="brain-index-health">
          <i aria-hidden="true" />
          <strong>{healthLabel(health)}</strong>
          {status?.watch_enabled && <span>Watch {humanState(status.watch_state || 'starting')}</span>}
        </div>
      </header>

      {status?.indexing && (
        <div className="brain-index-progress" role="status" aria-live="polite">
          <div><i style={{ width: `${indexProgress(status)}%` }} /></div>
          <span>{status.status === 'cancelling' ? 'Cancelling safely…' : status.current_path || 'Preparing scanner…'}</span>
          <strong>{status.progress_files_indexed.toLocaleString()} / {status.progress_files_seen.toLocaleString()} files</strong>
        </div>
      )}

      <div className="brain-index-metrics">
        <IndexMetric label="Coverage" value={status?.active ? `${status.files_indexed.toLocaleString()} / ${status.files_seen.toLocaleString()}` : 'Not built'} />
        <IndexMetric label="Search chunks" value={status?.active ? status.chunk_count.toLocaleString() : '-'} />
        <IndexMetric label="Read" value={status?.active ? formatBytes(status.bytes_read) : '-'} />
        <IndexMetric label="Sources" value={sourceCount.toLocaleString()} />
        <IndexMetric label="Omissions" value={status?.active ? status.omission_count.toLocaleString() : '-'} tone={status?.omission_count ? 'warn' : ''} />
        <IndexMetric label="Generation" value={status?.generation_id ? shortID(status.generation_id) : '-'} mono />
      </div>

      {status?.refresh_mode === 'incremental' && (
        <div className="brain-index-refresh-facts">
          <span><strong>{status.files_reused.toLocaleString()}</strong> hash-verified reused</span>
          <span><strong>{status.files_changed.toLocaleString()}</strong> changed/new</span>
          <span><strong>{status.files_deleted.toLocaleString()}</strong> deleted</span>
        </div>
      )}

      <div className="brain-index-meta">
        <span title={status?.workspace}>Workspace <strong>{compactPath(status?.workspace || '-')}</strong></span>
        <span>Policy <strong title={status?.policy_digest}>{shortID(status?.policy_digest)}</strong></span>
        <span>Manifest <strong title={status?.manifest_digest}>{shortID(status?.manifest_digest)}</strong></span>
        {status?.watch_last_check && <span>Watch checked <strong>{new Date(status.watch_last_check).toLocaleTimeString()}</strong></span>}
      </div>

      {(error || status?.watch_error) && <div className="brain-index-error">{error || status?.watch_error}</div>}
      {notice && <div className="brain-index-notice">{notice}</div>}

      {omissions.length > 0 && (
        <details className="brain-index-omissions">
          <summary>Review {status?.omission_count.toLocaleString()} explicit non-indexed entr{status?.omission_count === 1 ? 'y' : 'ies'}</summary>
          <div>
            {omissions.slice(0, 12).map((item, index) => (
              <article key={`${item.path}-${item.status}-${index}`}>
                <strong title={item.path}>{item.path || '(workspace root)'}</strong>
                <span>{humanState(item.status)}</span>
                {item.detail && <small>{item.detail}</small>}
              </article>
            ))}
            {status && status.omission_count > 12 && <p>Showing 12 of {status.omission_count.toLocaleString()}; the immutable manifest retains the full count.</p>}
          </div>
        </details>
      )}

      <section className="brain-split-review" aria-label="Split review preparation">
        <header>
          <div>
            <span className="brain-index-kicker">Evidence-owned review</span>
            <h3>Prepare deterministic shards</h3>
            <p>Balance the active generation into sealed, read-only child contracts. The preview contains metadata only; repository text and exact chunk IDs stay in the backend.</p>
          </div>
          <div className="brain-split-review-controls">
            <label>
              <span>Target</span>
              <select value={reviewShards} onChange={event => setReviewShards(Number(event.target.value))} disabled={busy}>
                {[2, 3, 4, 6, 8].map(count => <option key={count} value={count}>{count} shards</option>)}
              </select>
            </label>
            <button className="primary" onClick={() => void prepareSplitReview()} disabled={busy || !status?.active || status?.indexing}>
              {busy ? 'Preparing…' : reviewPreview ? 'Rebuild preview' : 'Preview split review'}
            </button>
            {reviewStatus?.can_cancel ? (
              <button className="danger" onClick={() => void cancelSplitReview()}>Cancel review</button>
            ) : (
              <>
                {reviewStatus?.can_resume && (
                  <button onClick={() => void resumeSplitReview()} disabled={busy || reviewStale || !status?.active || status?.indexing}>
                    Resume unfinished
                  </button>
                )}
                <button className="primary" onClick={() => void startSplitReview()} disabled={busy || !status?.active || status?.indexing}>
                  {reviewStatus?.review_id ? 'Run new review' : 'Run bounded review'}
                </button>
              </>
            )}
          </div>
        </header>

        {!status?.active && <div className="brain-split-review-empty">Build a complete repository index to prepare a review.</div>}
        {reviewPreview && (
          <div className="brain-split-review-preview">
            <div className="brain-split-review-summary">
              <span><strong>{reviewPreview.shard_count}</strong> sealed shards</span>
              <span><strong>{reviewPreview.file_count.toLocaleString()}</strong> files</span>
              <span><strong>{reviewPreview.chunk_count.toLocaleString()}</strong> chunks</span>
              <span><strong>{formatBytes(reviewPreview.bytes)}</strong> source</span>
              <span title={reviewPreview.plan_digest}>Plan <strong>{shortID(reviewPreview.plan_digest)}</strong></span>
            </div>
            <div className="brain-split-review-grid">
              {reviewPreview.shards.map(shard => (
                <article key={shard.id}>
                  <div>
                    <span>Shard {shard.ordinal}</span>
                    <strong title={shard.id}>{shortID(shard.id)}</strong>
                  </div>
                  <p>{shard.file_count.toLocaleString()} files · {shard.chunk_count.toLocaleString()} chunks · {formatBytes(shard.bytes)}</p>
                  <small>{shard.languages?.length ? shard.languages.join(', ') : 'Plain text / mixed'}</small>
                  <ul>
                    {shard.paths.slice(0, 3).map(path => <li key={path} title={path}>{path}</li>)}
                    {shard.paths.length > 3 && <li>+ {shard.paths.length - 3} more</li>}
                  </ul>
                  <code title={shard.evidence_digest}>evidence {shortID(shard.evidence_digest)}</code>
                </article>
              ))}
            </div>
            <p className="brain-split-review-caveat">Prepared only—not dispatched. A child result will enter the parent bundle only when every cited chunk, file hash, and line range belongs to its sealed shard.</p>
          </div>
        )}

        {reviewStatus?.review_id && (
          <div className="brain-review-run" data-state={reviewStatus.state}>
            <div className="brain-review-run-head">
              <div>
                <span className="brain-index-kicker">Parent review</span>
                <h4>{reviewStateLabel(reviewStatus.state)}</h4>
                <p>{reviewProgressLabel(reviewStatus)} · plan <strong title={reviewStatus.plan_digest}>{shortID(reviewStatus.plan_digest)}</strong></p>
              </div>
              <div className="brain-review-counts">
                <span><strong>{reviewStatus.accepted}</strong> drafts</span>
                <span><strong>{reviewStatus.rejected}</strong> rejected</span>
                <span className={reviewStatus.conflicts ? 'warn' : ''}><strong>{reviewStatus.conflicts}</strong> conflicts</span>
                {(reviewStatus.conflict_checks?.length ?? 0) > 0 && (
                  <span><strong>{reviewStatus.conflict_checks.filter(check => check.state === 'done').length} / {reviewStatus.conflict_checks.length}</strong> checks</span>
                )}
              </div>
            </div>
            <div className="brain-review-progress"><i style={{ width: `${reviewProgressPercent(reviewStatus)}%` }} /></div>
            {reviewStale && <div className="brain-review-warning">The active repository generation changed. These results remain labelled with their original generation, but failed shards cannot be retried against stale evidence.</div>}
            {reviewStatus.error && <div className="brain-index-error">{reviewStatus.error}</div>}
            <div className="brain-review-shards">
              {reviewStatus.shards.map(shard => (
                <article key={shard.id} data-state={shard.state}>
                  <div>
                    <span>Shard {shard.ordinal}</span>
                    <strong>{humanState(shard.state)}</strong>
                  </div>
                  <p>{shard.file_count} files · {shard.chunk_count} chunks · attempt {shard.attempt}</p>
                  {shard.error && <small title={shard.error}>{shard.error}</small>}
                  {(shard.state === 'error' || shard.state === 'cancelled' || shard.state === 'interrupted') && !reviewStatus.can_cancel && (
                    <button disabled={busy || reviewStale} onClick={() => void retryReviewShard(shard.id)}>Retry shard</button>
                  )}
                </article>
              ))}
            </div>
            {(reviewStatus.conflict_checks?.length ?? 0) > 0 && (
              <div className="brain-review-conflicts">
                <div>
                  <h4>Independent conflict checks</h4>
                  <p>Fresh bounded reviewers receive only the disputed immutable chunks. Their verdict adjudicates draft disagreement; it does not prove a finding.</p>
                </div>
                {reviewStatus.conflict_checks.map(check => (
                  <article key={check.id} data-state={check.state}>
                    <div>
                      <strong>{check.state === 'done' ? conflictVerdictLabel(check.verdict || '') : humanState(check.state)}</strong>
                      <span>{check.claims.length} disputed claims · attempt {check.attempt}</span>
                    </div>
                    {check.reason && <p>{check.reason}</p>}
                    {check.error && <small title={check.error}>{check.error}</small>}
                    <code title={check.evidence_digest}>evidence {shortID(check.evidence_digest)}</code>
                  </article>
                ))}
              </div>
            )}
            {reviewStatus.findings.length > 0 && (
              <div className="brain-review-findings">
                <h4>Merged draft findings</h4>
                {reviewStatus.findings.map((finding, index) => (
                  <article key={`${finding.id || finding.claim}-${index}`} data-state={finding.state}>
                    <div>
                      <span className={`severity ${finding.severity}`}>{finding.severity}</span>
                      <strong>{finding.claim}</strong>
                      <em>{finding.confidence} confidence</em>
                    </div>
                    <p>{finding.evidence.length} immutable citation{finding.evidence.length === 1 ? '' : 's'} · {humanState(finding.state)}</p>
                    {finding.follow_up && <small>{finding.follow_up}</small>}
                  </article>
                ))}
              </div>
            )}
          </div>
        )}
      </section>

      <footer className="brain-index-actions">
        <div>
          <button onClick={() => void load()} disabled={busy}>Refresh status</button>
          <button
            className={status?.watch_enabled ? 'active' : ''}
            onClick={() => void run(status?.watch_enabled ? 'Watch disabled' : 'Watch enabled', () => SetRepositoryIndexWatch(!status?.watch_enabled))}
            disabled={busy || !status?.available}
          >
            {status?.watch_enabled ? 'Disable Watch' : 'Enable Watch'}
          </button>
        </div>
        <div>
          {status?.indexing ? (
            <button className="danger" onClick={() => void run('Cancellation requested', CancelWorkspaceRepositoryIndex)} disabled={!status.can_cancel}>
              {status.can_cancel ? 'Cancel scan' : 'Cancelling…'}
            </button>
          ) : (
            <>
              {status?.active && <button onClick={() => void run('Changed files refreshed', RefreshWorkspaceRepositoryIndex)} disabled={busy}>Refresh changes</button>}
              <button className="primary" onClick={() => void run(status?.active ? 'Full rebuild complete' : 'Index complete', IndexWorkspaceRepository)} disabled={busy || status?.available === false}>
                {busy ? 'Working…' : status?.active ? 'Full rebuild' : 'Index workspace'}
              </button>
            </>
          )}
        </div>
      </footer>
    </section>
  )
}

function IndexMetric({ label, value, tone = '', mono = false }: { label: string; value: string; tone?: string; mono?: boolean }) {
  return <div className={tone}><span>{label}</span><strong className={mono ? 'mono' : ''} title={value}>{value}</strong></div>
}

function healthLabel(health: string): string {
  switch (health) {
    case 'healthy': return 'Healthy'
    case 'attention': return 'Needs review'
    case 'indexing': return 'Indexing'
    case 'not_indexed': return 'Not indexed'
    case 'unavailable': return 'Unavailable'
    default: return 'Loading'
  }
}

function humanState(value: string): string {
  return value.replaceAll('_', ' ')
}

function shortID(value?: string): string {
  if (!value) return '-'
  return value.length > 12 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value
}

function compactPath(value: string): string {
  const normalized = value.replaceAll('\\', '/')
  const parts = normalized.split('/').filter(Boolean)
  return parts.length > 3 ? `…/${parts.slice(-3).join('/')}` : normalized
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const index = Math.min(units.length - 1, Math.floor(Math.log(value) / Math.log(1024)))
  return `${(value / Math.pow(1024, index)).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function indexProgress(status: RepositoryIndexStatus): number {
  if (status.progress_files_seen <= 0) return 8
  return Math.max(8, Math.min(100, (status.progress_files_indexed / status.progress_files_seen) * 100))
}

function reviewStateLabel(state: string): string {
  switch (state) {
    case 'running': return 'Reviewing sealed shards'
    case 'cancelling': return 'Cancelling review safely'
    case 'complete': return 'Review complete'
    case 'partial': return 'Review needs attention'
    case 'cancelled': return 'Review cancelled'
    case 'error': return 'Review failed'
    default: return humanState(state)
  }
}

function reviewProgressLabel(status: RepositoryReviewStatus): string {
  const settled = status.shards.filter(shard => ['done', 'error', 'cancelled'].includes(shard.state)).length
  const checks = status.conflict_checks ?? []
  const checked = checks.filter(check => ['done', 'error', 'cancelled'].includes(check.state)).length
  return checks.length > 0
    ? `${settled} / ${status.shards.length} shards · ${checked} / ${checks.length} conflicts checked`
    : `${settled} / ${status.shards.length} shards settled`
}

function reviewProgressPercent(status: RepositoryReviewStatus): number {
  const checks = status.conflict_checks ?? []
  const total = status.shards.length + checks.length
  if (total === 0) return 0
  const settledShards = status.shards.filter(shard => ['done', 'error', 'cancelled'].includes(shard.state)).length
  const settledChecks = checks.filter(check => ['done', 'error', 'cancelled'].includes(check.state)).length
  return Math.max(status.can_cancel ? 4 : 0, ((settledShards + settledChecks) / total) * 100)
}

function conflictVerdictLabel(verdict: string): string {
  switch (verdict) {
    case 'compatible': return 'Claims compatible'
    case 'prefer': return 'Evidence prefers a claim'
    case 'unsupported': return 'Claims unsupported'
    case 'inconclusive': return 'Evidence inconclusive'
    default: return humanState(verdict || 'checked')
  }
}
