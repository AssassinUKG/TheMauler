import { useEffect, useState, useCallback, useRef } from 'react'
import { EventsOn } from '../wailsjs/runtime'
import { GetHistoryStats, GetSettings, SwitchProfile, GetProfileNames, Ping } from '../wailsjs/go'
import type { HistoryStats } from '../wailsjs/go'
import './StatusBar.css'

type PingStatus = 'unknown' | 'pending' | 'ok' | 'fail'

interface UsageStats {
  prompt_tokens: number
  completion_tokens: number
}

interface Props {
  statsVersion: number
  runState: RunStatePayload | null
}

interface RunStatePayload {
  id?: string
  state: string
  detail?: string
}

export function StatusBar({ statsVersion, runState }: Props) {
  const [stats, setStats] = useState<HistoryStats | null>(null)
  const [activeProfile, setActiveProfile] = useState('')
  const [profiles, setProfiles] = useState<string[]>([])
  const [showProfiles, setShowProfiles] = useState(false)
  const [pingStatus, setPingStatus] = useState<PingStatus>('unknown')
  const [pingLabel, setPingLabel] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [charsPerSec, setCharsPerSec] = useState<number | null>(null)
  const [realTokPerSec, setRealTokPerSec] = useState<number | null>(null)
  const [lastUsage, setLastUsage] = useState<UsageStats | null>(null)
  const streamStart = useRef<number>(0)
  const streamElapsed = useRef<number>(0)
  const charCount = useRef<number>(0)
  const streamingRef = useRef(false)

  useEffect(() => {
    const offs = [
      EventsOn('mauler:stream_start', () => {
        streamingRef.current = true
        setStreaming(true)
        setCharsPerSec(null)
        setRealTokPerSec(null)
        streamStart.current = Date.now()
        streamElapsed.current = 0
        charCount.current = 0
      }),
      EventsOn('mauler:delta', (...args: unknown[]) => {
        const chunk = args[0] as string
        charCount.current += chunk.length
        const elapsed = (Date.now() - streamStart.current) / 1000
        if (elapsed > 0.5) setCharsPerSec(charCount.current / elapsed)
      }),
      EventsOn('mauler:stream_done', () => {
        streamingRef.current = false
        setStreaming(false)
        streamElapsed.current = (Date.now() - streamStart.current) / 1000
        const elapsed = streamElapsed.current
        if (elapsed > 0) setCharsPerSec(charCount.current / elapsed)
      }),
      EventsOn('mauler:usage', (...args: unknown[]) => {
        const u = args[0] as UsageStats
        setLastUsage(u)
        // Compute real tok/s from actual completion tokens ÷ stream duration
        const elapsed = streamElapsed.current
        if (elapsed > 0 && u.completion_tokens > 0) {
          setRealTokPerSec(u.completion_tokens / elapsed)
        }
      }),
    ]
    return () => offs.forEach(off => off())
  }, [])

  const doPing = useCallback(async () => {
    setPingStatus('pending')
    const result = await Ping().catch(() => 'error')
    setPingStatus(result === 'ok' ? 'ok' : 'fail')
    setPingLabel(result)
  }, [])

  useEffect(() => {
    void doPing()
    const id = window.setInterval(() => {
      if (!streamingRef.current) void doPing()
    }, 90_000)
    return () => window.clearInterval(id)
    // doPing is stable (useCallback with []) so it is intentionally omitted.
    // Re-ping whenever the active profile changes so we verify the new backend.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeProfile])

  useEffect(() => {
    void GetHistoryStats().then(s => setStats(s)).catch(() => {})
    void GetSettings().then(s => setActiveProfile(s.active_profile)).catch(() => {})
  }, [statsVersion])

  const openProfiles = async () => {
    try {
      const names = await GetProfileNames()
      setProfiles(names)
      setShowProfiles(true)
    } catch (_e) {}
  }

  const switchTo = async (name: string) => {
    setShowProfiles(false)
    try {
      await SwitchProfile(name)
      setActiveProfile(name)
    } catch (_e) {}
  }

  const pct = stats ? Math.round(stats.fraction * 100) : 0
  const used = stats?.token_count ?? 0
  const budget = stats?.budget ?? 0
  const liveState = statusRunState(streaming, runState)

  return (
    <div className="status-bar">
      <div className="status-left">
        <span className="status-item status-profile" onClick={openProfiles} title="Switch profile">
          ⚡ {activeProfile || '—'}
        </span>
        {showProfiles && (
          <div className="profile-dropdown">
            {profiles.map(p => (
              <div key={p} className={`profile-item ${p === activeProfile ? 'active' : ''}`} onClick={() => void switchTo(p)}>
                {p}
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="status-center">
        {liveState && (
          <span className={`status-run-state status-run-${liveState.state}`} title={liveState.detail || liveState.label}>
            <span className="status-run-dot" />
            {liveState.label}
          </span>
        )}
        <ContextBar pct={pct} streaming={streaming} />
        <div className="status-token-info">
          <span className="status-token-used">{used.toLocaleString()}</span>
          <span className="status-token-sep">/</span>
          <span className="status-token-budget">{budget.toLocaleString()}</span>
          <span className="status-token-pct" style={{ color: pct > 85 ? 'var(--red)' : pct > 65 ? 'var(--yellow)' : 'rgba(255,255,255,0.6)' }}>
            {pct}%
          </span>
        </div>
      </div>

      <div className="status-right">
        {streaming && (
          <span className="status-thinking" title="Generating response…">
            <span className="status-thinking-dot" />
            Thinking…
          </span>
        )}
        {streaming && charsPerSec !== null && (
          <span className="status-tps" title="Characters per second (live estimate)">
            ~{Math.round(charsPerSec)} ch/s
          </span>
        )}
        {!streaming && realTokPerSec !== null && (
          <span className="status-tps status-tps-real" title="Tokens per second (actual completion tokens ÷ stream duration)">
            ◼ {realTokPerSec.toFixed(1)} tok/s
          </span>
        )}
        {!streaming && lastUsage && (
          <span className="status-usage" title="Last response: prompt + completion tokens">
            ↑{lastUsage.prompt_tokens.toLocaleString()} ↓{lastUsage.completion_tokens.toLocaleString()}
          </span>
        )}
        {stats?.rollback_len != null && stats.rollback_len > 0 && (
          <span className="status-item" title="Rollback depth">↩ {stats.rollback_len}</span>
        )}
        <span
          className={`ping-dot ping-${pingStatus}`}
          title={pingLabel ? `Backend: ${pingLabel}` : 'Checking backend…'}
          onClick={() => void doPing()}
        />
      </div>
    </div>
  )
}

function statusRunState(streaming: boolean, runState: RunStatePayload | null): { state: string; label: string; detail?: string } | null {
  if (!streaming && runState?.state !== 'failed') return null
  const raw = streaming ? (runState?.state || 'starting') : 'failed'
  const labels: Record<string, string> = {
    starting: 'Starting',
    planning: 'Planning',
    model_loading: 'Loading model',
    thinking: 'Thinking',
    researching: 'Researching',
    reading: 'Reading',
    editing: 'Editing',
    testing: 'Testing',
    recovering: 'Recovering',
    blocked: 'Blocked',
    failed: 'Failed',
    done: 'Done',
  }
  return { state: raw, label: labels[raw] || raw.replaceAll('_', ' '), detail: runState?.detail }
}

function ContextBar({ pct, streaming }: { pct: number; streaming: boolean }) {
  // Zone boundaries (% of context)
  const WARN = 70
  const DANGER = 87

  // Fill stops: green zone, yellow zone, red zone
  const fillW = Math.min(pct, 100)
  const greenW = Math.min(fillW, WARN)
  const yellowW = fillW > WARN ? Math.min(fillW - WARN, DANGER - WARN) : 0
  const redW = fillW > DANGER ? fillW - DANGER : 0

  return (
    <div className="ctx-bar-wrap" title={`Context: ${pct}% used`}>
      {/* Zone markers */}
      <div className="ctx-bar-zone-warn" style={{ left: `${WARN}%` }} />
      <div className="ctx-bar-zone-danger" style={{ left: `${DANGER}%` }} />
      {/* Fill segments */}
      <div className="ctx-bar-seg ctx-seg-green" style={{ width: `${greenW}%` }} />
      <div className="ctx-bar-seg ctx-seg-yellow" style={{ width: `${yellowW}%`, left: `${greenW}%` }} />
      <div className="ctx-bar-seg ctx-seg-red" style={{ width: `${redW}%`, left: `${greenW + yellowW}%` }} />
      {/* Streaming pulse overlay */}
      {streaming && <div className="ctx-bar-pulse" />}
    </div>
  )
}
