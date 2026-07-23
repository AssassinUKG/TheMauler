import { useEffect, useRef, useState, useCallback, type CSSProperties, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { EventsOn } from '../wailsjs/runtime'
import { OpenShell, ShellInput, ShellResize, ShellClose, RecoverSharedTerminal, GetSharedTerminalState, type TerminalRecoveryResult, type TerminalStateSnapshot } from '../wailsjs/go'
import './TerminalPane.css'

interface Props {
  visible: boolean
}

interface TerminalTab {
  localId: string
  title: string
  sessionId: string | null
}

interface AICommandEvent {
  id: string
  command: string
  tool: string
  status: 'running' | 'done' | 'error' | 'live'
  session?: string
  timeout?: string
  exitCode?: string
  durationMs?: number
  result?: string
  startedAt: string
  endedAt?: string
}

interface GroupedAICommandEvent extends AICommandEvent {
  count: number
  ids: string[]
}

function b64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return bytes
}

const TERM_THEME = {
  background: '#0b0e14',
  foreground: '#cbd5e1',
  cursor: '#4ade80',
  selectionBackground: 'rgba(255,255,255,0.18)',
}

const AI_COMMANDS_DEFAULT_WIDTH = 420
const AI_COMMANDS_DEFAULT_HEIGHT = 120
const AI_COMMANDS_MIN_WIDTH = 300
const AI_COMMANDS_MIN_HEIGHT = 64
const TERMINAL_MIN_WIDTH = 320
const TERMINAL_MIN_HEIGHT = 80
const AI_COMMANDS_SPLITTER_SIZE = 10

export function TerminalPane({ visible }: Props) {
	const [tabs, setTabs] = useState<TerminalTab[]>(() => [newTab(1)])
	const [activeId, setActiveId] = useState(tabs[0].localId)
	const [aiCommands, setAICommands] = useState<AICommandEvent[]>([])
	const [showAICommands, setShowAICommands] = useState(() => loadAICommandsVisible())
	const [aiCommandsWidth, setAICommandsWidth] = useState(() => loadAICommandsWidth())
	const [aiCommandsHeight, setAICommandsHeight] = useState(() => loadAICommandsHeight())
	const seq = useRef(1)
	const stackRef = useRef<HTMLDivElement>(null)
	const aiCommandsExpanded = showAICommands

	const setAICommandsVisible = useCallback((visible: boolean | ((value: boolean) => boolean)) => {
		setShowAICommands(prev => {
			const next = typeof visible === 'function' ? visible(prev) : visible
			localStorage.setItem('mauler.aiCommandsVisible', next ? '1' : '0')
			return next
		})
	}, [])

  const addTab = useCallback(() => {
    seq.current += 1
    const tab = newTab(seq.current)
    setTabs(prev => [...prev, tab])
    setActiveId(tab.localId)
  }, [])

  const closeTab = useCallback((localId: string) => {
    setTabs(prev => {
      const tab = prev.find(item => item.localId === localId)
      if (tab?.sessionId) void ShellClose(tab.sessionId).catch(() => null)
      const next = prev.filter(item => item.localId !== localId)
      if (next.length === 0) {
        const fresh = newTab(++seq.current)
        setActiveId(fresh.localId)
        return [fresh]
      }
      if (activeId === localId) setActiveId(next[Math.max(0, prev.findIndex(item => item.localId === localId) - 1)]?.localId ?? next[0].localId)
      return next
    })
  }, [activeId])

  const updateTab = useCallback((localId: string, patch: Partial<TerminalTab>) => {
    setTabs(prev => prev.map(tab => tab.localId === localId ? { ...tab, ...patch } : tab))
  }, [])

  useEffect(() => {
    const offStart = EventsOn('mauler:terminal_command_start', (...args: unknown[]) => {
      const msg = args[0] as { id?: string; session?: string; command?: string; timeout?: string; tool?: string }
      const id = msg.id || `cmd-${Date.now()}`
      const event: AICommandEvent = {
        id,
        session: msg.session,
        command: msg.command || '',
        timeout: msg.timeout,
        tool: msg.tool || 'shell',
        status: 'running',
        startedAt: new Date().toISOString(),
      }
			setAICommandsVisible(true)
      setAICommands(prev => [
        event,
        ...prev.filter(item => item.id !== id),
      ].slice(0, 80))
    })
    const offDone = EventsOn('mauler:terminal_command_done', (...args: unknown[]) => {
      const msg = args[0] as { id?: string; session?: string; exit_code?: string; duration_ms?: string; tool?: string; result?: string }
      const id = msg.id || `cmd-${Date.now()}`
			setAICommandsVisible(true)
      setAICommands(prev => {
        const existing = prev.find(item => item.id === id)
        const exitCode = msg.exit_code || ''
        const status = terminalCommandStatus(msg.tool || existing?.tool, exitCode)
        const next: AICommandEvent = {
          id,
          session: msg.session || existing?.session,
          command: existing?.command || (msg.tool === 'terminal_read' ? 'terminal_read' : ''),
          timeout: existing?.timeout,
          tool: msg.tool || existing?.tool || 'shell',
          status,
          exitCode,
          durationMs: Number(msg.duration_ms || 0) || existing?.durationMs,
          result: msg.result || existing?.result,
          startedAt: existing?.startedAt || new Date().toISOString(),
          endedAt: new Date().toISOString(),
        }
        return [next, ...prev.filter(item => item.id !== id)].slice(0, 80)
      })
    })
    return () => { offStart(); offDone() }
  }, [setAICommandsVisible])

  useEffect(() => {
    const offToolCall = EventsOn('mauler:tool_call', (...args: unknown[]) => {
      const msg = args[0] as { id?: string; name?: string; input?: string; timeout?: string }
      if (!isCommandLikeTool(msg.name)) return
      if (isTerminalTool(msg.name)) return
      const id = msg.id || `tool-${Date.now()}`
      const event: AICommandEvent = {
        id,
        command: commandTextFromToolInput(msg.name, msg.input),
        timeout: msg.timeout,
        tool: msg.name || 'tool',
        status: 'running',
        startedAt: new Date().toISOString(),
      }
			setAICommandsVisible(true)
      setAICommands(prev => [event, ...prev.filter(item => item.id !== id)].slice(0, 80))
    })
    const offToolResult = EventsOn('mauler:tool_result', (...args: unknown[]) => {
      const msg = args[0] as { id?: string; name?: string; result?: string }
      if (!isCommandLikeTool(msg.name)) return
      if (isTerminalTool(msg.name)) return
      const id = msg.id || `tool-${Date.now()}`
			setAICommandsVisible(true)
      setAICommands(prev => {
        const existing = prev.find(item => item.id === id)
        const exitCode = exitCodeFromToolResult(msg.result || '')
        const status: AICommandEvent['status'] = exitCode && exitCode !== '0' ? 'error' : 'done'
        const next: AICommandEvent = {
          id,
          session: existing?.session,
          command: existing?.command || commandTextFromToolInput(msg.name, ''),
          timeout: existing?.timeout,
          tool: msg.name || existing?.tool || 'tool',
          status,
          exitCode,
          result: msg.result || existing?.result,
          startedAt: existing?.startedAt || new Date().toISOString(),
          endedAt: new Date().toISOString(),
        }
        return [next, ...prev.filter(item => item.id !== id)].slice(0, 80)
      })
    })
    return () => { offToolCall(); offToolResult() }
  }, [setAICommandsVisible])

  return (
    <div className="terminal-pane" style={{ display: visible ? 'flex' : 'none' }}>
      <div className="terminal-tabs">
        <div className="terminal-tab-list">
          {tabs.map((tab, index) => (
            <button
              key={tab.localId}
              className={`terminal-tab${tab.localId === activeId ? ' active' : ''}`}
              onClick={() => setActiveId(tab.localId)}
              title={tab.sessionId || 'Starting shell'}
            >
              <span>{tab.title || `Term ${index + 1}`}</span>
              {tabs.length > 1 && (
                <i
                  role="button"
                  tabIndex={0}
                  onClick={event => { event.stopPropagation(); closeTab(tab.localId) }}
                  onKeyDown={event => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault()
                      event.stopPropagation()
                      closeTab(tab.localId)
                    }
                  }}
                  aria-label={`Close ${tab.title}`}
                >x</i>
              )}
            </button>
          ))}
          <button className="terminal-tab add" onClick={addTab} title="New terminal">+</button>
        </div>
        <span className="terminal-agent-hint">Term 1 is the agent/shared terminal</span>
      </div>

			<div ref={stackRef} className={`terminal-stack ${aiCommandsExpanded ? 'with-ai-commands' : 'with-ai-command-rail'}`}>
        {tabs.map((tab, index) => (
          <TerminalSession
            key={tab.localId}
            tab={tab}
            visible={visible && tab.localId === activeId}
            isAgentTerminal={index === 0}
            onUpdate={patch => updateTab(tab.localId, patch)}
							showAICommands={aiCommandsExpanded}
            onToggleAICommands={() => setAICommandsVisible(v => !v)}
          />
        ))}
				<AICommandResizeHandle
					stackRef={stackRef}
					expanded={aiCommandsExpanded}
					width={aiCommandsWidth}
					height={aiCommandsHeight}
					onExpand={() => setAICommandsVisible(true)}
					onResizeWidth={setAICommandsWidth}
					onResizeHeight={setAICommandsHeight}
				/>
				{aiCommandsExpanded ? (
						<AICommandHistory
							commands={aiCommands}
							width={aiCommandsWidth}
							height={aiCommandsHeight}
							onClear={() => setAICommands([])}
							onCollapse={() => setAICommandsVisible(false)}
							onRecover={() => void recoverSharedTerminalFromHistory(setAICommands)}
						/>
				) : (
					<AICommandRail commands={aiCommands} onExpand={() => setAICommandsVisible(true)} />
				)}
			</div>
    </div>
  )
}

function AICommandRail({ commands, onExpand }: { commands: AICommandEvent[]; onExpand: () => void }) {
	const errors = commands.filter(command => command.status === 'error').length
	const running = commands.filter(command => command.status === 'running' || command.status === 'live').length
	return (
		<button
			type="button"
			className={`terminal-ai-rail${errors ? ' has-error' : running ? ' is-running' : ''}`}
			onClick={onExpand}
			title={commands.length ? 'Show AI command history' : 'AI command history is empty'}
		>
			<strong>AI Commands</strong>
			<span>{commands.length}</span>
			{errors > 0 && <small>{errors} error{errors === 1 ? '' : 's'}</small>}
			{errors === 0 && running > 0 && <small>{running} running</small>}
			{commands.length === 0 && <small>idle</small>}
		</button>
	)
}

function TerminalSession({
  tab,
  visible,
  isAgentTerminal,
  onUpdate,
  showAICommands,
  onToggleAICommands,
}: {
  tab: TerminalTab
  visible: boolean
  isAgentTerminal: boolean
  onUpdate: (patch: Partial<TerminalTab>) => void
  showAICommands: boolean
  onToggleAICommands: () => void
}) {
  const [starting, setStarting] = useState(false)
  const [showHelp, setShowHelp] = useState(false)
  const [startError, setStartError] = useState<string | null>(null)
  const [terminalState, setTerminalState] = useState<TerminalStateSnapshot | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const sessionRef = useRef<string | null>(tab.sessionId)
  const hasAutoStarted = useRef(false)
  const visibleRef = useRef(visible)
  const fitRafRef = useRef<number | null>(null)
  const lastFitRef = useRef({ cols: 0, rows: 0 })

  useEffect(() => { sessionRef.current = tab.sessionId }, [tab.sessionId])
  useEffect(() => { visibleRef.current = visible }, [visible])

  const refreshTerminalState = useCallback(async () => {
    if (!isAgentTerminal) return
    try {
      const state = await GetSharedTerminalState()
      setTerminalState(state)
    } catch {
      setTerminalState(null)
    }
  }, [isAgentTerminal])

  useEffect(() => {
    if (!visible || !isAgentTerminal) return
    void refreshTerminalState()
    const timer = window.setInterval(() => void refreshTerminalState(), 2000)
    return () => window.clearInterval(timer)
  }, [visible, isAgentTerminal, refreshTerminalState])

  useEffect(() => {
    if (!containerRef.current || termRef.current) return
    const term = new Terminal({
      fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', monospace",
      fontSize: 12.5,
      cursorBlink: true,
      scrollback: 5000,
      theme: TERM_THEME,
      allowProposedApi: true,
    })
    term.attachCustomKeyEventHandler((ev: KeyboardEvent) => {
      const isCopyKey = (ev.ctrlKey || ev.metaKey) && ev.code === 'KeyC'
      if (!isCopyKey) return true
      const selection = term.getSelection()
      if (!selection) return true
      if (ev.shiftKey || selection.length > 0) {
        void navigator.clipboard.writeText(selection).catch(() => null)
        return false
      }
      return true
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(containerRef.current)
    term.onData(data => {
      const id = sessionRef.current
      if (id) void ShellInput(id, data).catch(() => null)
    })
    termRef.current = term
    fitRef.current = fit
    return () => {
      const id = sessionRef.current
      if (id) void ShellClose(id).catch(() => null)
      term.dispose()
      termRef.current = null
      fitRef.current = null
    }
  }, [])

  const fitAndResize = useCallback(() => {
    const term = termRef.current
    const fit = fitRef.current
    const el = containerRef.current
    if (!term || !fit || !el) return
    if (!visibleRef.current || el.offsetWidth === 0 || el.offsetHeight === 0) return
    let dims: { cols: number; rows: number } | undefined
    try { dims = fit.proposeDimensions() } catch { return }
    if (!dims || !Number.isFinite(dims.cols) || !Number.isFinite(dims.rows) || dims.cols <= 0 || dims.rows <= 0) return
    if (dims.cols === lastFitRef.current.cols && dims.rows === lastFitRef.current.rows) return
    try { fit.fit() } catch { return }
    lastFitRef.current = { cols: term.cols, rows: term.rows }
    const id = sessionRef.current
    if (id) void ShellResize(id, term.cols, term.rows).catch(() => null)
  }, [])

  const scheduleFit = useCallback(() => {
    if (fitRafRef.current != null) return
    fitRafRef.current = requestAnimationFrame(() => {
      fitRafRef.current = null
      fitAndResize()
    })
  }, [fitAndResize])

  useEffect(() => {
    if (!containerRef.current) return
    const observer = new ResizeObserver(() => scheduleFit())
    observer.observe(containerRef.current)
    return () => {
      observer.disconnect()
      if (fitRafRef.current != null) cancelAnimationFrame(fitRafRef.current)
    }
  }, [scheduleFit])

  useEffect(() => {
    if (!visible) return
    lastFitRef.current = { cols: 0, rows: 0 }
    requestAnimationFrame(() => {
      fitAndResize()
      requestAnimationFrame(fitAndResize)
      termRef.current?.focus()
    })
    const t = setTimeout(fitAndResize, 120)
    return () => clearTimeout(t)
  }, [visible, fitAndResize])

  const startShell = useCallback(async () => {
    setStarting(true)
    setStartError(null)
    try {
      const id = await OpenShell()
      onUpdate({ sessionId: id })
      sessionRef.current = id
      void refreshTerminalState()
      requestAnimationFrame(fitAndResize)
    } catch (e) {
      const message = formatShellStartError(e)
      setStartError(message)
      termRef.current?.writeln(`\x1b[31m[error starting shell]\x1b[0m ${message}`)
    } finally {
      setStarting(false)
    }
  }, [fitAndResize, onUpdate, refreshTerminalState])

  useEffect(() => {
    if (visible && !hasAutoStarted.current && !tab.sessionId) {
      hasAutoStarted.current = true
      void startShell()
    }
  }, [visible, tab.sessionId, startShell])

  useEffect(() => {
    const offs = [
      EventsOn('mauler:shell_output', (...args: unknown[]) => {
        const msg = args[0] as { id: string; data: string }
        if (msg.id !== sessionRef.current) return
        termRef.current?.write(b64ToBytes(msg.data))
        if (visibleRef.current) scheduleFit()
        if (isAgentTerminal) void refreshTerminalState()
      }),
      EventsOn('mauler:shell_exit', (...args: unknown[]) => {
        const msg = args[0] as { id: string }
        if (msg.id !== sessionRef.current) return
        onUpdate({ sessionId: null })
        sessionRef.current = null
        if (isAgentTerminal) void refreshTerminalState()
        termRef.current?.writeln('\r\n\x1b[90m[shell exited]\x1b[0m')
      }),
      EventsOn('mauler:terminal_command_start', (...args: unknown[]) => {
        if (!isAgentTerminal) return
        const msg = args[0] as { command: string; timeout: string }
        termRef.current?.writeln(`\r\n\x1b[36m[AI running ${msg.timeout}s]\x1b[0m ${msg.command}`)
        void refreshTerminalState()
      }),
      EventsOn('mauler:terminal_command_done', (...args: unknown[]) => {
        if (!isAgentTerminal) return
        const msg = args[0] as { exit_code: string }
        termRef.current?.writeln(`\x1b[36m[AI command finished: exit ${msg.exit_code}]\x1b[0m`)
        void refreshTerminalState()
      }),
    ]
    return () => offs.forEach(off => off())
  }, [isAgentTerminal, onUpdate, refreshTerminalState, scheduleFit])

  const killShell = useCallback(async () => {
    const id = sessionRef.current
    if (!id) return
    await ShellClose(id).catch(() => null)
    onUpdate({ sessionId: null })
    sessionRef.current = null
    setStartError(null)
    void refreshTerminalState()
    termRef.current?.writeln('\r\n\x1b[90m[shell killed]\x1b[0m')
  }, [onUpdate])

  const restartShell = useCallback(async () => {
    const id = sessionRef.current
    if (id) await ShellClose(id).catch(() => null)
    onUpdate({ sessionId: null })
    sessionRef.current = null
    setStartError(null)
    termRef.current?.clear()
    await startShell()
    void refreshTerminalState()
  }, [onUpdate, startShell])

  const recoverShell = useCallback(async () => {
    try {
      const res = await RecoverSharedTerminal()
      termRef.current?.writeln(`\r\n\x1b[36m[terminal recover: ${res.status}]\x1b[0m ${res.summary}`)
      if (res.lines?.length) termRef.current?.writeln(ansiSafePreview(res.lines.join('\n'), 3000))
      void refreshTerminalState()
    } catch (e) {
      termRef.current?.writeln(`\r\n\x1b[31m[terminal recover failed]\x1b[0m ${String(e)}`)
      void refreshTerminalState()
    }
  }, [refreshTerminalState])

  const copyOutput = useCallback(async () => {
    const term = termRef.current
    if (!term) return
    const sel = term.getSelection()
    await navigator.clipboard.writeText(sel || visibleTerminalText(term)).catch(() => null)
  }, [])

  return (
    <section className="terminal-session" style={{ display: visible ? 'flex' : 'none' }}>
      <div className="terminal-header">
        <div className="terminal-title-wrap">
          <span className="terminal-title">{tab.title}{isAgentTerminal ? ' / agent' : ''}</span>
          {isAgentTerminal && <TerminalStateBadge snapshot={terminalState} />}
        </div>
        <div className="terminal-header-actions">
          {!tab.sessionId ? (
            <>
              <button className="terminal-btn" onClick={() => void startShell()} disabled={starting}>
                {starting ? 'Starting...' : startError ? 'Retry shell' : 'Start shell'}
              </button>
              <button className="terminal-btn" onClick={() => setShowHelp(v => !v)}>Help</button>
            </>
          ) : (
            <>
              <button className="terminal-btn" onClick={() => termRef.current?.clear()}>Clear</button>
              <button className="terminal-btn" onClick={() => void copyOutput()}>Copy</button>
              {isAgentTerminal && <button className="terminal-btn" onClick={onToggleAICommands}>{showAICommands ? 'Hide AI Commands' : 'Show AI Commands'}</button>}
              {isAgentTerminal && <button className="terminal-btn terminal-btn-warn" onClick={() => void recoverShell()}>Recover</button>}
              <button className="terminal-btn" onClick={() => void restartShell()}>Restart</button>
              <button className="terminal-btn" onClick={() => setShowHelp(v => !v)}>Help</button>
              <button className="terminal-btn terminal-btn-danger" onClick={() => void killShell()}>Kill</button>
            </>
          )}
        </div>
      </div>
      {startError && <TerminalError message={startError} onRetry={() => void startShell()} />}
      {showHelp && <TerminalHelp />}
      <div ref={containerRef} className="terminal-xterm" />
    </section>
  )
}

function AICommandHistory({
	commands,
	width,
	height,
	onClear,
	onCollapse,
	onRecover,
}: {
	commands: AICommandEvent[]
	width: number
	height: number
	onClear: () => void
	onCollapse: () => void
	onRecover: () => void
}) {
	const groupedCommands = groupAICommands(commands)
	const splitStyle = {
		'--ai-commands-width': `${width}px`,
		'--ai-commands-height': `${height}px`,
	} as CSSProperties
	return (
		<aside className="terminal-ai-history" style={splitStyle}>
      <div className="terminal-ai-history-head">
        <div>
          <strong>AI Commands</strong>
          <span>{groupedCommands.length} rows / {commands.length} recent</span>
        </div>
        <button className="terminal-btn terminal-btn-warn" onClick={onRecover}>Recover</button>
        <button className="terminal-btn" onClick={onClear} disabled={commands.length === 0}>Clear</button>
        <button className="terminal-btn" onClick={onCollapse}>Hide</button>
      </div>
      <div className="terminal-ai-command-list">
        {commands.length === 0 ? (
          <div className="terminal-ai-empty">AI terminal commands and result previews will appear here.</div>
        ) : groupedCommands.map(command => (
          <details key={command.ids.join(':')} className={`terminal-ai-command ${command.status} ${command.count > 1 ? 'repeated' : ''}`} open={command.status === 'running' || command.status === 'error'}>
            <summary>
              <span className={`terminal-ai-status ${command.status}`}>{command.status}</span>
              <strong>{command.tool}</strong>
              <code>{command.command || command.id}</code>
              {command.count > 1 && <span className="terminal-ai-repeat" title={`${command.count} consecutive similar commands`}>x{command.count}</span>}
              <time>{formatCommandTime(command)}</time>
            </summary>
            <div className="terminal-ai-command-meta">
              {command.count > 1 && <span>grouped: {command.count}</span>}
              {command.exitCode && <span>exit: {command.exitCode}</span>}
              {command.durationMs != null && <span>{formatDuration(command.durationMs)}</span>}
              {command.timeout && <span>wait/timeout: {command.timeout}s</span>}
              {command.session && <span>{command.session}</span>}
              <button onClick={() => void navigator.clipboard.writeText(command.command)}>Copy cmd</button>
              {command.result && <button onClick={() => void navigator.clipboard.writeText(command.result || '')}>Copy result</button>}
            </div>
            {command.result && <pre>{trimResult(command.result)}</pre>}
          </details>
        ))}
      </div>
    </aside>
	)
}

function groupAICommands(commands: AICommandEvent[]): GroupedAICommandEvent[] {
  const groups: GroupedAICommandEvent[] = []
  for (const command of commands) {
    const last = groups[groups.length - 1]
    if (last && commandGroupKey(last) === commandGroupKey(command)) {
      last.count += 1
      last.ids.push(command.id)
      if (!last.result && command.result) last.result = command.result
      if (!last.exitCode && command.exitCode) last.exitCode = command.exitCode
      if (last.durationMs == null && command.durationMs != null) last.durationMs = command.durationMs
      continue
    }
    groups.push({
      ...command,
      count: 1,
      ids: [command.id],
    })
  }
  return groups
}

function commandGroupKey(command: AICommandEvent): string {
  return [
    command.status,
    command.tool,
    normalizeCommandForGrouping(command.command || command.id),
    command.exitCode || '',
  ].join('\u0000')
}

function normalizeCommandForGrouping(command: string): string {
  return command
    .trim()
    .replace(/\s+/g, ' ')
    .replace(/--max-time\s+\d+/gi, '--max-time #')
    .replace(/\|\s*head\s+-\d+/gi, '| head #')
}

function TerminalStateBadge({ snapshot }: { snapshot: TerminalStateSnapshot | null }) {
  if (!snapshot) return <span className="terminal-state-badge unknown" title="Terminal state unknown">unknown</span>
  const label = terminalStateLabel(snapshot.state)
  return <span className={`terminal-state-badge ${snapshot.state || 'unknown'}`} title={snapshot.summary || label}>{label}</span>
}

function terminalStateLabel(state: string) {
  switch (state) {
    case 'ready':
      return 'ready'
    case 'running':
      return 'running'
    case 'listener':
      return 'listener'
    case 'connected':
      return 'connected'
    case 'interactive_prompt':
      return 'prompt'
    case 'busy':
      return 'busy'
    case 'closed':
      return 'closed'
    case 'missing':
      return 'missing'
    default:
      return state || 'unknown'
  }
}

async function recoverSharedTerminalFromHistory(setAICommands: Dispatch<SetStateAction<AICommandEvent[]>>) {
  const startedAt = new Date().toISOString()
  const id = `recover-${Date.now()}`
  const runningEvent: AICommandEvent = {
    id,
    command: 'Recover shared terminal',
    tool: 'terminal_recover',
    status: 'running',
    startedAt,
  }
  setAICommands(prev => [runningEvent, ...prev].slice(0, 80))
  try {
    const res = await RecoverSharedTerminal()
    const doneEvent: AICommandEvent = {
      id,
      command: 'Recover shared terminal',
      tool: 'terminal_recover',
      status: res.status === 'ready' ? 'done' : 'error',
      exitCode: res.status,
      result: formatTerminalRecoveryResult(res),
      startedAt,
      endedAt: new Date().toISOString(),
    }
    setAICommands(prev => [doneEvent, ...prev.filter(item => item.id !== id)].slice(0, 80))
  } catch (e) {
    const errorEvent: AICommandEvent = {
      id,
      command: 'Recover shared terminal',
      tool: 'terminal_recover',
      status: 'error',
      result: String(e),
      startedAt,
      endedAt: new Date().toISOString(),
    }
    setAICommands(prev => [errorEvent, ...prev.filter(item => item.id !== id)].slice(0, 80))
  }
}

function formatTerminalRecoveryResult(res: TerminalRecoveryResult) {
  const lines = res.lines?.length ? `\n${res.lines.join('\n')}` : ''
  return `[terminal_recover status=${res.status}]\n${res.summary}${lines}`
}

function AICommandResizeHandle({
	stackRef,
	expanded,
	width,
	height,
	onExpand,
	onResizeWidth,
	onResizeHeight,
}: {
	stackRef: RefObject<HTMLDivElement | null>
	expanded: boolean
	width: number
	height: number
	onExpand: () => void
	onResizeWidth: (width: number) => void
	onResizeHeight: (height: number) => void
}) {
	const widthRef = useRef(width)
	const heightRef = useRef(height)
	useEffect(() => { widthRef.current = width }, [width])
	useEffect(() => { heightRef.current = height }, [height])

	const persistSize = useCallback((vertical: boolean) => {
		if (vertical) {
			localStorage.setItem('mauler.aiCommandsHeight', String(heightRef.current))
			return
		}
		localStorage.setItem('mauler.aiCommandsWidth', String(widthRef.current))
	}, [])

	const resizeFromPointer = useCallback((stack: HTMLDivElement, rect: DOMRect, move: PointerEvent) => {
		const vertical = getComputedStyle(stack).flexDirection === 'column'
		if (vertical) {
			const max = Math.max(AI_COMMANDS_MIN_HEIGHT, Math.floor(rect.height - TERMINAL_MIN_HEIGHT - AI_COMMANDS_SPLITTER_SIZE))
			const next = Math.min(max, Math.max(AI_COMMANDS_MIN_HEIGHT, rect.bottom - move.clientY))
			heightRef.current = next
			onResizeHeight(next)
			return vertical
		}
		const max = Math.max(AI_COMMANDS_MIN_WIDTH, Math.floor(rect.width - TERMINAL_MIN_WIDTH))
		const next = Math.min(max, Math.max(AI_COMMANDS_MIN_WIDTH, rect.right - move.clientX))
		widthRef.current = next
		onResizeWidth(next)
		return vertical
	}, [onResizeHeight, onResizeWidth])

	const startResize = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
		event.preventDefault()
		const stack = stackRef.current
		if (!stack) return
		if (!expanded) onExpand()
		const rect = stack.getBoundingClientRect()
		const vertical = getComputedStyle(stack).flexDirection === 'column'
		const resizingClass = vertical ? 'terminal-ai-resizing-row' : 'terminal-ai-resizing-col'
		const pointerId = event.pointerId
		const target = event.currentTarget
		target.setPointerCapture(pointerId)
		document.documentElement.classList.add(resizingClass)
		const onMove = (move: PointerEvent) => {
			resizeFromPointer(stack, rect, move)
		}
		const onUp = () => {
			persistSize(vertical)
			document.documentElement.classList.remove(resizingClass)
			if (target.hasPointerCapture(pointerId)) target.releasePointerCapture(pointerId)
			window.removeEventListener('pointermove', onMove)
			window.removeEventListener('pointerup', onUp)
			window.removeEventListener('pointercancel', onUp)
		}
		window.addEventListener('pointermove', onMove)
		window.addEventListener('pointerup', onUp)
		window.addEventListener('pointercancel', onUp)
	}, [expanded, onExpand, persistSize, resizeFromPointer, stackRef])

	const resizeFromKeyboard = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
		const stack = stackRef.current
		if (!stack) return
		const vertical = getComputedStyle(stack).flexDirection === 'column'
		const amount = event.shiftKey ? 48 : 16
		if (event.key === 'Home') {
			event.preventDefault()
			onExpand()
			widthRef.current = AI_COMMANDS_DEFAULT_WIDTH
			heightRef.current = AI_COMMANDS_DEFAULT_HEIGHT
			onResizeWidth(AI_COMMANDS_DEFAULT_WIDTH)
			onResizeHeight(AI_COMMANDS_DEFAULT_HEIGHT)
			persistSize(vertical)
			return
		}
		if (vertical && event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return
		if (!vertical && event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
		event.preventDefault()
		onExpand()
		const rect = stack.getBoundingClientRect()
		if (vertical) {
			const delta = event.key === 'ArrowUp' ? amount : -amount
			const max = Math.max(AI_COMMANDS_MIN_HEIGHT, Math.floor(rect.height - TERMINAL_MIN_HEIGHT - AI_COMMANDS_SPLITTER_SIZE))
			const next = Math.min(max, Math.max(AI_COMMANDS_MIN_HEIGHT, heightRef.current + delta))
			heightRef.current = next
			onResizeHeight(next)
			persistSize(true)
			return
		}
		const delta = event.key === 'ArrowLeft' ? amount : -amount
		const max = Math.max(AI_COMMANDS_MIN_WIDTH, Math.floor(rect.width - TERMINAL_MIN_WIDTH))
		const next = Math.min(max, Math.max(AI_COMMANDS_MIN_WIDTH, widthRef.current + delta))
		widthRef.current = next
		onResizeWidth(next)
		persistSize(false)
	}, [onExpand, onResizeHeight, onResizeWidth, persistSize, stackRef])

	const resetSize = useCallback(() => {
		onExpand()
		widthRef.current = AI_COMMANDS_DEFAULT_WIDTH
		heightRef.current = AI_COMMANDS_DEFAULT_HEIGHT
		onResizeWidth(AI_COMMANDS_DEFAULT_WIDTH)
		onResizeHeight(AI_COMMANDS_DEFAULT_HEIGHT)
		localStorage.setItem('mauler.aiCommandsWidth', String(AI_COMMANDS_DEFAULT_WIDTH))
		localStorage.setItem('mauler.aiCommandsHeight', String(AI_COMMANDS_DEFAULT_HEIGHT))
	}, [onExpand, onResizeHeight, onResizeWidth])

	return (
		<div
			className={`terminal-ai-resize${expanded ? '' : ' collapsed'}`}
			role="separator"
			tabIndex={0}
			aria-label="Resize Terminal and AI Commands"
			onPointerDown={startResize}
			onKeyDown={resizeFromKeyboard}
			onDoubleClick={resetSize}
			title={expanded ? 'Drag to resize AI Commands; arrow keys resize; double-click resets' : 'Drag or click to reopen AI Commands'}
		/>
	)
}

function loadAICommandsWidth() {
	const raw = Number(localStorage.getItem('mauler.aiCommandsWidth') || '')
	if (Number.isFinite(raw) && raw >= AI_COMMANDS_MIN_WIDTH && raw <= 1400) return raw
	return AI_COMMANDS_DEFAULT_WIDTH
}

function loadAICommandsHeight() {
	const raw = Number(localStorage.getItem('mauler.aiCommandsHeight') || '')
	if (Number.isFinite(raw) && raw >= AI_COMMANDS_MIN_HEIGHT && raw <= 600) return raw
	return AI_COMMANDS_DEFAULT_HEIGHT
}

function loadAICommandsVisible() {
	const raw = localStorage.getItem('mauler.aiCommandsVisible')
	if (raw === '0') return false
	return true
}

function trimResult(result: string) {
  const text = result.trim()
  if (text.length <= 6000) return text
  return `${text.slice(0, 2500)}\n\n... trimmed ...\n\n${text.slice(-2500)}`
}

function isCommandLikeTool(name?: string) {
	return ['shell', 'terminal_send', 'terminal_read', 'start_listener', 'http_probe', 'run_script'].includes(name || '')
}

function isTerminalTool(name?: string) {
  return name === 'terminal_send' || name === 'terminal_read'
}

function terminalCommandStatus(tool?: string, exitCode?: string): AICommandEvent['status'] {
  const code = (exitCode || '').trim()
  if (!code || code === '0' || code === 'sent') return 'done'
  if (code === 'live' || code === 'running') return 'live'
  if (tool === 'terminal_read') {
    if (['prompt_or_idle', 'output_ready', 'ready', 'connected', 'listener', 'interactive_prompt', 'no_output_yet'].includes(code)) {
      return code === 'running' || code === 'no_output_yet' ? 'live' : 'done'
    }
  }
  return 'error'
}

function commandTextFromToolInput(name?: string, input?: string) {
  const text = input || ''
  if (!text.trim()) return name || 'tool'
  try {
    const parsed = JSON.parse(text) as Record<string, unknown>
    const command = typeof parsed.command === 'string' ? parsed.command : ''
    const keys = typeof parsed.keys === 'string' ? parsed.keys : ''
    const url = typeof parsed.url === 'string' ? parsed.url : ''
    const code = typeof parsed.code === 'string' ? parsed.code : ''
    if (command) return command
    if (keys) return keys
    if (url) return url
    if (code) return code.split(/\r?\n/).find(line => line.trim()) || 'run_script'
  } catch {
    // Fall through to raw input preview.
  }
  return text.trim()
}

function exitCodeFromToolResult(result: string) {
  const shared = result.match(/\[(?:shared_terminal\/)?[^\]\r\n]*?\bexit\s+(-?\d+)/i)
  if (shared) return shared[1]
  const plain = result.match(/\bexit code\s+(-?\d+)/i)
  if (plain) return plain[1]
  return ''
}

function ansiSafePreview(result: string, maxChars: number) {
	const text = trimResult(result).replace(/\r?\n/g, '\r\n')
	if (text.length <= maxChars) return text
	return `${text.slice(0, maxChars)}\r\n... trimmed ...`
}

function formatCommandTime(command: AICommandEvent) {
  const ts = command.endedAt || command.startedAt
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString()
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`
}

function newTab(n: number): TerminalTab {
  return { localId: `term-${Date.now()}-${n}`, title: `Term ${n}`, sessionId: null }
}

function visibleTerminalText(term: Terminal): string {
  const buffer = term.buffer.active
  const start = Math.max(0, buffer.baseY)
  const end = buffer.baseY + term.rows
  const lines: string[] = []
  for (let i = start; i < end; i++) {
    const line = buffer.getLine(i)
    if (!line) continue
    lines.push(line.translateToString(true))
  }
  return lines.join('\n').trimEnd()
}

function formatShellStartError(error: unknown): string {
  const raw = String(error ?? 'unknown error')
  const lower = raw.toLowerCase()
  if (lower.includes('wsl/service/e_unexpected') || lower.includes('catastrophic failure')) {
    return `${raw}\n\nWSL appears to be wedged. Use Restart WSL in the Agent panel, or run wsl --shutdown in PowerShell, then press Retry shell.`
  }
  if (lower.includes('wsl')) {
    return `${raw}\n\nCheck the configured WSL distro/user and try Restart WSL if the terminal will not start.`
  }
  return raw
}

function TerminalError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="terminal-error">
      <div className="terminal-error-title">Terminal failed to start</div>
      <pre>{message}</pre>
      <div className="terminal-error-actions">
        <button className="terminal-btn" onClick={onRetry}>Retry shell</button>
      </div>
    </div>
  )
}

function TerminalHelp() {
  return (
    <div className="terminal-help">
      <div className="terminal-help-title">Terminal Help</div>
      <div>Use <strong>+</strong> for another terminal while the agent is busy. Term 1 is the shared agent terminal.</div>
      <div>This is a real terminal: arrows, Tab completion, Ctrl+C, vim, htop, nc/ncat, and prompt-driven sessions work here.</div>
      <div><strong>Kill</strong> stops only this tab. <strong>Restart</strong> starts this tab clean. <strong>Copy</strong> copies the selection or visible screen.</div>
    </div>
  )
}
