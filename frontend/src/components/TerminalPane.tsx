import { useEffect, useRef, useState, useCallback } from 'react'
import type { KeyboardEvent } from 'react'
import { EventsOn } from '../wailsjs/runtime'
import { OpenShell, ShellInput, ShellClose } from '../wailsjs/go'
import './TerminalPane.css'

interface TerminalLine {
  data: string
  stream: 'stdout' | 'stderr' | 'system'
}

interface Props {
  visible: boolean
}

export function TerminalPane({ visible }: Props) {
  const [lines, setLines] = useState<TerminalLine[]>([])
  const [input, setInput] = useState('')
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [showHelp, setShowHelp] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const historyRef = useRef<string[]>([])
  const historyIdxRef = useRef(-1)
  const hasAutoStarted = useRef(false)

  // Auto-start shell the first time the panel becomes visible
  useEffect(() => {
    if (visible && !hasAutoStarted.current && !sessionId) {
      hasAutoStarted.current = true
      void startShell()
    }
  // startShell is stable (useCallback with [])
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible])

  // Auto-scroll to bottom when new lines arrive or panel becomes visible
  useEffect(() => {
    if (visible) bottomRef.current?.scrollIntoView({ behavior: 'auto' })
  }, [lines, visible])

  // Focus the input whenever the panel opens
  useEffect(() => {
    if (visible) setTimeout(() => inputRef.current?.focus(), 0)
  }, [visible])

  // Wire up backend events
  useEffect(() => {
    const offs = [
      EventsOn('mauler:shell_output', (...args: unknown[]) => {
        const msg = args[0] as { id: string; data: string; stream: 'stdout' | 'stderr' }
        if (isMaulerWrapperEcho(msg.data)) return
        setLines(prev => [...prev, { data: msg.data, stream: msg.stream }])
      }),
      EventsOn('mauler:shell_exit', (...args: unknown[]) => {
        const msg = args[0] as { id: string }
        setSessionId(prev => (prev === msg.id ? null : prev))
        setLines(prev => [...prev, { data: '[shell exited]', stream: 'system' }])
      }),
      EventsOn('mauler:terminal_command_start', (...args: unknown[]) => {
        const msg = args[0] as { command: string; timeout: string }
        setLines(prev => [...prev, { data: `[AI running ${msg.timeout}s] ${msg.command}`, stream: 'system' }])
      }),
      EventsOn('mauler:terminal_command_done', (...args: unknown[]) => {
        const msg = args[0] as { exit_code: string }
        setLines(prev => [...prev, { data: `[AI command finished: exit ${msg.exit_code}]`, stream: 'system' }])
      }),
    ]
    return () => offs.forEach(off => off())
  }, [])

  const startShell = useCallback(async () => {
    setStarting(true)
    try {
      const id = await OpenShell()
      setSessionId(id)
      setLines([{ data: '[shell started - type commands below]', stream: 'system' }])
    } catch (e) {
      setLines(prev => [...prev, { data: `[error starting shell: ${e}]`, stream: 'stderr' }])
    } finally {
      setStarting(false)
    }
  }, [])

  const sendInput = useCallback(async () => {
    if (!sessionId) return
    const text = input.trimEnd()
    if (!text.trim()) {
      setInput('')
      return
    }
    setInput('')
    // Push to history (most recent first, cap at 100)
    historyRef.current = [text, ...historyRef.current.slice(0, 99)]
    historyIdxRef.current = -1
    try {
      await ShellInput(sessionId, text)
    } catch (e) {
      setLines(prev => [...prev, { data: `[write error: ${e}]`, stream: 'stderr' }])
    }
  }, [sessionId, input])

  const handleKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      void sendInput()
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      const next = Math.min(historyIdxRef.current + 1, historyRef.current.length - 1)
      historyIdxRef.current = next
      if (historyRef.current[next] !== undefined) setInput(historyRef.current[next])
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      const next = Math.max(historyIdxRef.current - 1, -1)
      historyIdxRef.current = next
      setInput(next === -1 ? '' : (historyRef.current[next] ?? ''))
      return
    }
  }, [sendInput])

  const killShell = useCallback(async () => {
    if (!sessionId) return
    try {
      await ShellClose(sessionId)
    } catch { /* ignore */ }
    setSessionId(null)
    setLines(prev => [...prev, { data: '[shell killed]', stream: 'system' }])
  }, [sessionId])

  const restartShell = useCallback(async () => {
    if (sessionId) {
      await ShellClose(sessionId).catch(() => null)
    }
    await startShell()
  }, [sessionId, startShell])

  const copyOutput = useCallback(async () => {
    await navigator.clipboard.writeText(lines.map(line => line.data).join('\n')).catch(() => null)
  }, [lines])

  // Keep mounted so output history and session ID survive panel toggle.
  // Visibility is controlled by the parent container's display/height.
  return (
    <div className="terminal-pane" style={{ display: visible ? 'flex' : 'none' }}>
      <div className="terminal-header">
        <span className="terminal-title">Terminal</span>
        <div className="terminal-header-actions">
          {!sessionId ? (
            <>
              <button
                className="terminal-btn"
                onClick={() => void startShell()}
                disabled={starting}
              >
                {starting ? 'Starting...' : 'Start shell'}
              </button>
              <button className="terminal-btn" onClick={() => setShowHelp(v => !v)}>Help</button>
            </>
          ) : (
            <>
              <button className="terminal-btn" onClick={() => setLines([])}>Clear</button>
              <button className="terminal-btn" onClick={() => void copyOutput()}>Copy</button>
              <button className="terminal-btn" onClick={() => void restartShell()}>Restart</button>
              <button className="terminal-btn" onClick={() => setShowHelp(v => !v)}>Help</button>
              <button className="terminal-btn terminal-btn-danger" onClick={() => void killShell()}>Kill</button>
            </>
          )}
        </div>
      </div>

      <div className="terminal-output">
        {showHelp && <TerminalHelp />}
        {lines.map((line, i) => (
          <div key={i} className={`terminal-line terminal-${line.stream}`}>{line.data || ' '}</div>
        ))}
        <div ref={bottomRef} />
      </div>

      <div className="terminal-input-row">
        <span className="terminal-prompt">{sessionId ? '$' : '-'}</span>
        <input
          ref={inputRef}
          className="terminal-input"
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={!sessionId}
          placeholder={sessionId ? 'Type command and press Enter...' : 'Click "Start shell" above'}
          spellCheck={false}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
        />
      </div>
    </div>
  )
}

function isMaulerWrapperEcho(line: string): boolean {
  return line.includes('__MAULER_START_') || line.includes('__MAULER_DONE_')
}

function TerminalHelp() {
  return (
    <div className="terminal-help">
      <div className="terminal-help-title">Terminal Help</div>
      <div>Type commands and press Enter. AI shell calls appear here when shared terminal mode is enabled.</div>
      <div><strong>Kill</strong> stops the shell. <strong>Restart</strong> starts clean. <strong>Copy</strong> copies visible output.</div>
      <div>Use chat Stop to interrupt an AI run. After manual commands, ask the AI to continue from the terminal output.</div>
      <div>Switch Settings &gt; Tools &gt; AI shell mode between shared terminal and isolated one-shot.</div>
    </div>
  )
}
