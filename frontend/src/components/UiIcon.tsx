import './UiIcon.css'

export type UiIconName =
  | 'chats' | 'inspector' | 'terminal' | 'tools' | 'view' | 'run' | 'more'
  | 'security' | 'files' | 'browser' | 'search' | 'follow'
  | 'undo' | 'trash' | 'plan' | 'workspace' | 'agent'

export function UiIcon({ name, className = '' }: { name: UiIconName; className?: string }) {
  const common = {
    className: `ui-icon${className ? ` ${className}` : ''}`,
    viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8,
    strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true,
  }
  switch (name) {
    case 'chats': return <svg {...common}><path d="M5 5.5h14v9H9l-4 3v-12Z" /><path d="M8 9h8M8 12h5" /></svg>
    case 'inspector': return <svg {...common}><rect x="4" y="4" width="16" height="16" rx="2" /><path d="M14.5 4v16M17 8h1M17 11h1" /></svg>
    case 'terminal': return <svg {...common}><rect x="3.5" y="4.5" width="17" height="15" rx="2" /><path d="m7 9 3 3-3 3M12.5 15h4" /></svg>
    case 'tools': return <svg {...common}><path d="M4 7h10M18 7h2M4 17h2M10 17h10M14 4v6M7 14v6" /></svg>
    case 'view': return <svg {...common}><path d="M2.8 12s3.2-5 9.2-5 9.2 5 9.2 5-3.2 5-9.2 5-9.2-5-9.2-5Z" /><circle cx="12" cy="12" r="2.2" /></svg>
    case 'run': return <svg {...common}><path d="m9 7 7 5-7 5V7Z" /></svg>
    case 'more': return <svg {...common}><circle cx="5" cy="12" r="1" fill="currentColor" stroke="none" /><circle cx="12" cy="12" r="1" fill="currentColor" stroke="none" /><circle cx="19" cy="12" r="1" fill="currentColor" stroke="none" /></svg>
    case 'security': return <svg {...common}><path d="M12 3.5 19 6v5.2c0 4.3-2.6 7.5-7 9.3-4.4-1.8-7-5-7-9.3V6l7-2.5Z" /><path d="m9 12 2 2 4-4" /></svg>
    case 'files': return <svg {...common}><path d="M3.5 6.5h6l2 2h9v9.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2V6.5Z" /><path d="M3.5 9h17" /></svg>
    case 'browser': return <svg {...common}><circle cx="12" cy="12" r="8.5" /><path d="M3.8 9h16.4M8.5 4.8A13 13 0 0 0 8.5 19M15.5 4.8a13 13 0 0 1 0 14.2" /></svg>
    case 'search': return <svg {...common}><circle cx="10.5" cy="10.5" r="6" /><path d="m15 15 4.5 4.5" /></svg>
    case 'follow': return <svg {...common}><path d="M12 4v13M7 12l5 5 5-5M5 20h14" /></svg>
    case 'undo': return <svg {...common}><path d="M8 8H4v-4" /><path d="M4.5 8.5A8 8 0 1 1 6 17" /></svg>
    case 'trash': return <svg {...common}><path d="M4.5 7h15M9 7V4.5h6V7M7 7l1 13h8l1-13M10 10.5v6M14 10.5v6" /></svg>
    case 'plan': return <svg {...common}><rect x="5" y="4" width="14" height="16" rx="2" /><path d="m8 9 1.5 1.5L12 8M13.5 10H16M8 15h8" /></svg>
    case 'workspace': return <svg {...common}><path d="M3.5 7h6l2 2h9v9a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2V7Z" /><path d="M8 4h8v5" /></svg>
    case 'agent': return <svg {...common}><circle cx="12" cy="8" r="3.5" /><path d="M5 20c.6-4 3-6 7-6s6.4 2 7 6" /></svg>
  }
}
