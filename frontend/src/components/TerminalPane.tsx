import { useEffect, useRef, useState, useCallback } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { EventsOn } from '../wailsjs/runtime'
import { OpenShell, ShellInput, ShellResize, ShellClose } from '../wailsjs/go'
import './TerminalPane.css'

interface Props {
  visible: boolean
}

// Decode a base64 PTY chunk into raw bytes for xterm.js (preserves colours,
// cursor moves and UTF-8 — never round-trips through a lossy string).
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

export function TerminalPane({ visible }: Props) {
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [showHelp, setShowHelp] = useState(false)

  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const sessionRef = useRef<string | null>(null)
  const hasAutoStarted = useRef(false)
  const visibleRef = useRef(visible)
  const fitRafRef = useRef<number | null>(null)
  const lastFitRef = useRef({ cols: 0, rows: 0 })

  // Mirror sessionId into a ref so the long-lived xterm onData / event handlers
  // always see the current session without being re-bound.
  useEffect(() => { sessionRef.current = sessionId }, [sessionId])
  useEffect(() => { visibleRef.current = visible }, [visible])

  // Create the xterm.js instance once, mounted into the container div.
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
    // Forward every keystroke / paste straight to the PTY.
    term.onData(data => {
      const id = sessionRef.current
      if (id) void ShellInput(id, data).catch(() => null)
    })
    termRef.current = term
    fitRef.current = fit
    return () => {
      term.dispose()
      termRef.current = null
      fitRef.current = null
    }
  }, [])

  const fitAndResize = useCallback(() => {
    const term = termRef.current
    const fit = fitRef.current
    const el = containerRef.current
    // Never fit while hidden or before layout: a zero-sized container makes
    // FitAddon propose 1×1, which would SIGWINCH the PTY down to a sliver and
    // leave bash repainting its prompt into blank rows on the way back up.
    if (!term || !fit || !el) return
    if (!visibleRef.current || el.offsetWidth === 0 || el.offsetHeight === 0) return
    let dims: { cols: number; rows: number } | undefined
    try { dims = fit.proposeDimensions() } catch { return }
    if (!dims || !Number.isFinite(dims.cols) || !Number.isFinite(dims.rows) || dims.cols <= 0 || dims.rows <= 0) return
    // Skip no-op resizes so a drag that doesn't cross a cell boundary doesn't
    // spam the PTY with identical SIGWINCHes.
    if (dims.cols === lastFitRef.current.cols && dims.rows === lastFitRef.current.rows) return
    try { fit.fit() } catch { return }
    lastFitRef.current = { cols: term.cols, rows: term.rows }
    const id = sessionRef.current
    if (id) void ShellResize(id, term.cols, term.rows).catch(() => null)
  }, [])

  // Coalesce bursts of resize callbacks (a drag fires dozens/sec) into one fit
  // per animation frame.
  const scheduleFit = useCallback(() => {
    if (fitRafRef.current != null) return
    fitRafRef.current = requestAnimationFrame(() => {
      fitRafRef.current = null
      fitAndResize()
    })
  }, [fitAndResize])

  // Refit when the pane resizes or becomes visible (xterm needs real layout).
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
    if (visible) {
      // The pane is kept mounted with display:none while hidden, so on the frame it
      // flips back to display:flex the container has no measured size yet. A single
      // rAF can fire before layout flushes, leaving xterm at its narrow default cols
      // (the right-side dead zone). Fit across a couple of frames + a short timeout so
      // we re-fit once the real pane width is known; xterm reflows the backlog to it.
      lastFitRef.current = { cols: 0, rows: 0 } // force a real fit on re-show
      requestAnimationFrame(() => {
        fitAndResize()
        requestAnimationFrame(fitAndResize)
        termRef.current?.focus()
      })
      const t = setTimeout(fitAndResize, 120)
      return () => clearTimeout(t)
    }
  }, [visible, fitAndResize])

  const startShell = useCallback(async () => {
    setStarting(true)
    try {
      const id = await OpenShell()
      setSessionId(id)
      sessionRef.current = id
      requestAnimationFrame(fitAndResize)
    } catch (e) {
      termRef.current?.writeln(`\x1b[31m[error starting shell: ${e}]\x1b[0m`)
    } finally {
      setStarting(false)
    }
  }, [fitAndResize])

  // Auto-start the first time the panel is shown.
  useEffect(() => {
    if (visible && !hasAutoStarted.current && !sessionId) {
      hasAutoStarted.current = true
      void startShell()
    }
  }, [visible, sessionId, startShell])

  // Backend events.
  useEffect(() => {
    const offs = [
      EventsOn('mauler:shell_output', (...args: unknown[]) => {
        const msg = args[0] as { id: string; data: string }
        termRef.current?.write(b64ToBytes(msg.data))
      }),
      EventsOn('mauler:shell_exit', (...args: unknown[]) => {
        const msg = args[0] as { id: string }
        setSessionId(prev => (prev === msg.id ? null : prev))
        termRef.current?.writeln('\r\n\x1b[90m[shell exited]\x1b[0m')
      }),
      EventsOn('mauler:terminal_command_start', (...args: unknown[]) => {
        const msg = args[0] as { command: string; timeout: string }
        termRef.current?.writeln(`\r\n\x1b[36m[AI running ${msg.timeout}s]\x1b[0m ${msg.command}`)
      }),
      EventsOn('mauler:terminal_command_done', (...args: unknown[]) => {
        const msg = args[0] as { exit_code: string }
        termRef.current?.writeln(`\x1b[36m[AI command finished: exit ${msg.exit_code}]\x1b[0m`)
      }),
    ]
    return () => offs.forEach(off => off())
  }, [])

  const killShell = useCallback(async () => {
    const id = sessionRef.current
    if (!id) return
    await ShellClose(id).catch(() => null)
    setSessionId(null)
    termRef.current?.writeln('\r\n\x1b[90m[shell killed]\x1b[0m')
  }, [])

  const restartShell = useCallback(async () => {
    const id = sessionRef.current
    if (id) await ShellClose(id).catch(() => null)
    termRef.current?.clear()
    await startShell()
  }, [startShell])

  const copyOutput = useCallback(async () => {
    const term = termRef.current
    if (!term) return
    const sel = term.getSelection()
    if (sel) await navigator.clipboard.writeText(sel).catch(() => null)
  }, [])

  // Keep mounted so the terminal buffer and session survive panel toggles;
  // visibility is driven by display so xterm keeps its scrollback.
  return (
    <div className="terminal-pane" style={{ display: visible ? 'flex' : 'none' }}>
      <div className="terminal-header">
        <span className="terminal-title">Terminal</span>
        <div className="terminal-header-actions">
          {!sessionId ? (
            <>
              <button className="terminal-btn" onClick={() => void startShell()} disabled={starting}>
                {starting ? 'Starting...' : 'Start shell'}
              </button>
              <button className="terminal-btn" onClick={() => setShowHelp(v => !v)}>Help</button>
            </>
          ) : (
            <>
              <button className="terminal-btn" onClick={() => termRef.current?.clear()}>Clear</button>
              <button className="terminal-btn" onClick={() => void copyOutput()}>Copy</button>
              <button className="terminal-btn" onClick={() => void restartShell()}>Restart</button>
              <button className="terminal-btn" onClick={() => setShowHelp(v => !v)}>Help</button>
              <button className="terminal-btn terminal-btn-danger" onClick={() => void killShell()}>Kill</button>
            </>
          )}
        </div>
      </div>

      {showHelp && <TerminalHelp />}
      <div ref={containerRef} className="terminal-xterm" />
    </div>
  )
}

function TerminalHelp() {
  return (
    <div className="terminal-help">
      <div className="terminal-help-title">Terminal Help</div>
      <div>This is a real terminal — type, use arrows/Tab completion, run TUIs like vim or htop. Ctrl+C interrupts.</div>
      <div><strong>Kill</strong> stops the shell. <strong>Restart</strong> starts clean. <strong>Copy</strong> copies the selection.</div>
      <div>AI shell calls appear inline when shared-terminal mode is on. Use chat Stop to interrupt an AI run.</div>
      <div>Switch Settings &gt; Tools &gt; AI shell mode between shared terminal and isolated one-shot.</div>
    </div>
  )
}
