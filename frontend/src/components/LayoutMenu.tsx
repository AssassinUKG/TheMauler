import { useEffect, useRef, useState } from 'react'
import { UiIcon } from './UiIcon'
import './LayoutMenu.css'

type BottomTab = 'terminal' | 'stream' | 'jobs'

interface Props {
  explorerOpen: boolean
  inspectorOpen: boolean
  bottomOpen: boolean
  bottomTab: BottomTab
  aiCommandsOpen: boolean
  autoOpenRunPanel: boolean
  closedPanelRails: boolean
  onExplorerChange: (open: boolean) => void
  onInspectorChange: (open: boolean) => void
  onBottomChange: (open: boolean) => void
  onShowBottom: (tab: BottomTab) => void
  onAICommandsChange: (open: boolean) => void
  onAutoOpenRunPanelChange: (enabled: boolean) => void
  onClosedPanelRailsChange: (enabled: boolean) => void
  onFocusChat: () => void
  onRestoreDefault: () => void
  onOpenDoctor: () => void
  onOpenSettings: () => void
}

export function LayoutMenu({
  explorerOpen, inspectorOpen, bottomOpen, bottomTab, aiCommandsOpen, autoOpenRunPanel, closedPanelRails,
  onExplorerChange, onInspectorChange, onBottomChange, onShowBottom, onAICommandsChange,
  onAutoOpenRunPanelChange, onClosedPanelRailsChange, onFocusChat, onRestoreDefault,
  onOpenDoctor, onOpenSettings,
}: Props) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.shiftKey && event.key.toLowerCase() === 'p') {
        event.preventDefault()
        setOpen(value => !value)
      } else if (event.key === 'Escape') {
        setOpen(false)
      }
    }
    const pointer = (event: MouseEvent) => {
      if (open && !rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    window.addEventListener('keydown', keyboard)
    window.addEventListener('mousedown', pointer)
    return () => {
      window.removeEventListener('keydown', keyboard)
      window.removeEventListener('mousedown', pointer)
    }
  }, [open])

  const showAICommands = () => {
    onAICommandsChange(true)
    onShowBottom('terminal')
  }

  return (
    <div className="layout-menu" ref={rootRef}>
      <button className={`layout-menu-trigger${open ? ' active' : ''}`} onClick={() => setOpen(value => !value)} aria-haspopup="menu" aria-expanded={open} title="Workbench menu (Ctrl+Shift+P)"><UiIcon name="more" /><span>More</span></button>
      {open && (
        <section className="layout-menu-popover" role="menu" aria-label="Workbench commands">
          <header><div><span>Workbench</span><strong>Panels, diagnostics and preferences</strong></div><kbd>Ctrl+Shift+P</kbd></header>
          <div className="layout-menu-section-title first">Panels</div>
          <div className="layout-command-list">
            <LayoutToggle label="Conversations" shortcut="Ctrl+B" active={explorerOpen} onClick={() => onExplorerChange(!explorerOpen)} />
            <LayoutToggle label="Inspector and files" shortcut="Ctrl+Shift+B" active={inspectorOpen} onClick={() => onInspectorChange(!inspectorOpen)} />
            <LayoutToggle label="Terminal drawer" shortcut="Ctrl+J" active={bottomOpen} onClick={() => onBottomChange(!bottomOpen)} />
            <LayoutToggle label="AI command log" active={bottomOpen && bottomTab === 'terminal' && aiCommandsOpen} onClick={() => aiCommandsOpen ? onAICommandsChange(false) : showAICommands()} />
          </div>
          <div className="layout-menu-section">
            <span>Open work surface</span>
            <div className="layout-tab-commands">
              {(['terminal', 'stream', 'jobs'] as BottomTab[]).map(tab => <button key={tab} className={bottomOpen && bottomTab === tab ? 'active' : ''} onClick={() => onShowBottom(tab)}>{tab === 'stream' ? 'Live output' : tab === 'jobs' ? 'Background jobs' : 'Terminal'}</button>)}
            </div>
          </div>
		  <div className="layout-menu-section-title">Behaviour</div>
          <label className="layout-auto-open">
            <input type="checkbox" checked={autoOpenRunPanel} onChange={event => onAutoOpenRunPanelChange(event.target.checked)} />
            <span><strong>Auto-open run activity</strong><small>Otherwise the bottom panel opens only when you ask.</small></span>
          </label>
          <label className="layout-auto-open">
            <input type="checkbox" checked={closedPanelRails} onChange={event => onClosedPanelRailsChange(event.target.checked)} />
            <span><strong>Keep closed-panel rails</strong><small>Off means the sidebar and Inspector close to zero pixels.</small></span>
          </label>
          <div className="layout-menu-section-title">Application</div>
          <div className="layout-command-list layout-application-list">
            <button role="menuitem" onClick={() => { onOpenDoctor(); setOpen(false) }}><span className="layout-command-icon doctor" aria-hidden="true">+</span><span><strong>Run Doctor</strong><small>Check provider, browser, shell and runtime health</small></span></button>
            <button role="menuitem" onClick={() => { onOpenSettings(); setOpen(false) }}><span className="layout-command-icon" aria-hidden="true">⚙</span><span><strong>Settings</strong><small>Models, profiles, tools, UI and integrations</small></span><kbd>Ctrl+,</kbd></button>
          </div>
          <footer>
            <button onClick={() => { onFocusChat(); setOpen(false) }}>Hide work surfaces</button>
            <button onClick={() => { onRestoreDefault(); setOpen(false) }}>Reset layout</button>
          </footer>
        </section>
      )}
    </div>
  )
}

function LayoutToggle({ label, shortcut, active, onClick }: { label: string; shortcut?: string; active: boolean; onClick: () => void }) {
  return <button role="menuitemcheckbox" aria-checked={active} onClick={onClick}><span className={`layout-check${active ? ' active' : ''}`} aria-hidden="true">{active ? '✓' : ''}</span><strong>{label}</strong>{shortcut && <kbd>{shortcut}</kbd>}</button>
}
