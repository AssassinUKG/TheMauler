import { useEffect, useState, useCallback, useRef } from 'react'
import { EventsOn } from '../wailsjs/runtime'
import { GetHistoryStats, GetLabStatus, GetSettings, SwitchProfile, GetProfileNames, Ping } from '../wailsjs/go'
import type { HistoryStats, LabStatus } from '../wailsjs/go'
import './StatusBar.css'

type PingStatus = 'unknown' | 'pending' | 'ok' | 'fail'

interface UsageStats {
  prompt_tokens: number
  completion_tokens: number
}

interface Props {
  statsVersion: number
  runState: RunStatePayload | null
  onProfileChanged?: () => void
}

interface RunStatePayload {
  id?: string
  state: string
  detail?: string
}

export function StatusBar({ statsVersion, runState, onProfileChanged }: Props) {
  const [stats, setStats] = useState<HistoryStats | null>(null)
  const [lab, setLab] = useState<LabStatus | null>(null)
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
  const profileMenuRef = useRef<HTMLDivElement>(null)

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
  }, [activeProfile, doPing])

  useEffect(() => {
    void GetHistoryStats().then(s => setStats(s)).catch(() => {})
    void GetLabStatus().then(s => setLab(s)).catch(() => setLab(null))
    void GetSettings().then(s => setActiveProfile(s.active_profile)).catch(() => {})
  }, [statsVersion])

  useEffect(() => {
    if (!showProfiles) return
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node | null
      if (target && profileMenuRef.current?.contains(target)) return
      setShowProfiles(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setShowProfiles(false)
    }
    window.addEventListener('pointerdown', onPointerDown)
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('pointerdown', onPointerDown)
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [showProfiles])

  const openProfiles = async () => {
    try {
      const names = await GetProfileNames()
      setProfiles(names)
      setShowProfiles(v => !v)
    } catch (_e) {}
  }

  const switchTo = async (name: string) => {
    setShowProfiles(false)
    try {
      await SwitchProfile(name)
      setActiveProfile(name)
      onProfileChanged?.()
    } catch (_e) {}
  }

  const pct = stats ? Math.round(stats.fraction * 100) : 0
  const used = stats?.token_count ?? 0
  const budget = stats?.budget ?? 0
  const ctxWindow = stats?.window ?? budget
  const configuredWindow = stats?.configured_window ?? 0
  const reserve = stats?.reserve ?? 0
  const contextShortfall = configuredWindow > 0 && ctxWindow > 0 && ctxWindow < configuredWindow
  const liveState = statusRunState(streaming, runState)
  const workspace = shortPath(lab?.agent_root || '')
  const target = lab?.target || 'no target'
  const vpn = lab?.vpn_cidr || lab?.vpn_interface || ''
  const shell = shellLabel(lab)

  return (
    <div className="status-bar">
      <div className="status-left" ref={profileMenuRef}>
        <button type="button" className="status-item status-profile" onClick={openProfiles} title="Switch profile">
          <span>Profile</span>
          <strong>{activeProfile || '-'}</strong>
        </button>
        {showProfiles && (
          <div className="profile-dropdown">
            {profiles.map(p => (
              <button key={p} type="button" className={`profile-item ${p === activeProfile ? 'active' : ''}`} onClick={() => void switchTo(p)}>
                {p}
              </button>
            ))}
          </div>
        )}
        <span className="status-item status-identity" title={lab?.agent_root || 'Workspace'}>
          <span>Workspace</span>
          <strong>{workspace || 'unknown'}</strong>
        </span>
      </div>

      <div className="status-center">
        <span className="status-item status-identity status-target" title={lab?.hostname ? `${target} / ${lab.hostname}` : target}>
          <span>Target</span>
          <strong>{target}</strong>
        </span>
        {vpn && (
          <span className="status-item status-identity" title={vpn}>
            <span>VPN</span>
            <strong>{vpn}</strong>
          </span>
        )}
        <span className="status-item status-identity" title={shell}>
          <span>Shell</span>
          <strong>{shell}</strong>
        </span>
        {liveState && (
          <span className={`status-run-state status-run-${liveState.state}`} title={liveState.detail || liveState.label}>
            <span className="status-run-dot" />
            {liveState.label}
          </span>
        )}
        <ContextBar pct={pct} used={used} budget={budget} window={ctxWindow} streaming={streaming} shortfall={contextShortfall} configuredWindow={configuredWindow} />
        <div
          className={`status-token-info${contextShortfall ? ' status-token-shortfall' : ''}`}
          title={`${used.toLocaleString()} tokens used of ${budget.toLocaleString()} usable\n+${reserve.toLocaleString()} reserved for the model reply\n= ${ctxWindow.toLocaleString()} actual context window${contextShortfall ? `\nConfigured profile requests ${configuredWindow.toLocaleString()} tokens` : ''}`}
        >
          <span className="status-token-used">{used.toLocaleString()}</span>
          <span className="status-token-sep">/</span>
          <span className="status-token-budget">{budget.toLocaleString()}</span>
          {reserve > 0 && <span className="status-token-reserve">+{reserve.toLocaleString()}</span>}
          <span className="status-token-sep">/</span>
          <span className="status-token-window">{ctxWindow.toLocaleString()}</span>
          {contextShortfall && <span className="status-token-shortfall-label">ctx {Math.round(ctxWindow / 1024)}k/{Math.round(configuredWindow / 1024)}k</span>}
          <span className="status-token-pct">{pct}%</span>
        </div>
      </div>

      <div className="status-right">
        {streaming && charsPerSec !== null && (
          <span className="status-tps" title="Characters per second, live estimate">
            ~{Math.round(charsPerSec)} ch/s
          </span>
        )}
        {!streaming && realTokPerSec !== null && (
          <span className="status-tps status-tps-real" title="Tokens per second, actual completion tokens divided by stream duration">
            {realTokPerSec.toFixed(1)} tok/s
          </span>
        )}
        {!streaming && lastUsage && (
          <span className="status-usage" title="Last response: prompt and completion tokens">
            up {lastUsage.prompt_tokens.toLocaleString()} / down {lastUsage.completion_tokens.toLocaleString()}
          </span>
        )}
        {stats?.rollback_len != null && stats.rollback_len > 0 && (
          <span className="status-item" title="Rollback depth">undo {stats.rollback_len}</span>
        )}
        <span
          className={`ping-dot ping-${pingStatus}`}
          title={pingLabel ? `Backend: ${pingLabel}` : 'Checking backend'}
          onClick={() => void doPing()}
        />
      </div>
    </div>
  )
}

function shellLabel(lab: LabStatus | null) {
  if (!lab) return 'unknown'
  const backend = lab.shell_backend || 'auto'
  if (backend === 'wsl') {
    const distro = lab.shell_distro || 'wsl'
    const user = lab.shell_user || 'default'
    return `${backend}/${distro}/${user}`
  }
  return backend
}

function shortPath(path: string) {
  if (!path) return ''
  const normalized = path.replace(/\\/g, '/')
  const parts = normalized.split('/').filter(Boolean)
  if (parts.length <= 2) return normalized
  return `.../${parts.slice(-2).join('/')}`
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

function ContextBar({ pct, used, budget, window, streaming, shortfall, configuredWindow }: { pct: number; used: number; budget: number; window: number; streaming: boolean; shortfall: boolean; configuredWindow: number }) {
  const WARN = 70
  const DANGER = 87

  const budgetFrac = window > 0 ? Math.min(budget / window, 1) : 1
  const usedW = window > 0 ? Math.min((used / window) * 100, 100) : 0
  const warnX = WARN * budgetFrac
  const dangerX = DANGER * budgetFrac
  const reserveX = budgetFrac * 100
  const greenW = Math.min(usedW, warnX)
  const yellowW = usedW > warnX ? Math.min(usedW - warnX, dangerX - warnX) : 0
  const redW = usedW > dangerX ? usedW - dangerX : 0

  return (
    <div className={`ctx-bar-wrap${shortfall ? ' ctx-bar-shortfall' : ''}`} title={shortfall ? `${pct}% of usable budget; actual context ${window.toLocaleString()} < configured ${configuredWindow.toLocaleString()}` : `${pct}% of usable budget`}>
      <div className="ctx-bar-reserve" style={{ left: `${reserveX}%`, width: `${100 - reserveX}%` }} />
      <div className="ctx-bar-zone-warn" style={{ left: `${warnX}%` }} />
      <div className="ctx-bar-zone-danger" style={{ left: `${dangerX}%` }} />
      <div className="ctx-bar-seg ctx-seg-green" style={{ width: `${greenW}%` }} />
      <div className="ctx-bar-seg ctx-seg-yellow" style={{ width: `${yellowW}%`, left: `${greenW}%` }} />
      <div className="ctx-bar-seg ctx-seg-red" style={{ width: `${redW}%`, left: `${greenW + yellowW}%` }} />
      {streaming && <div className="ctx-bar-pulse" />}
    </div>
  )
}
