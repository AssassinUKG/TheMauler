import { useEffect, useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { DispatchSideChatMessage, GetSettings } from '../wailsjs/go'
import './SideChatPage.css'

interface AskMessage { id: string; role: 'user' | 'assistant'; text: string }

interface Props {
  streaming: boolean
  onOpenProjectChat: () => void
}

export function SideChatPage({ streaming, onOpenProjectChat }: Props) {
  const sessionID = useMemo(() => `desktop:ask:${crypto.randomUUID()}`, [])
  const [messages, setMessages] = useState<AskMessage[]>([])
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [activeProfile, setActiveProfile] = useState('')

  useEffect(() => {
    void GetSettings().then(settings => setActiveProfile(settings.active_profile)).catch(() => setActiveProfile(''))
  }, [])

  const send = async () => {
    const text = draft.trim()
    if (!text || sending) return
    setDraft('')
    setMessages(prev => [...prev, { id: crypto.randomUUID(), role: 'user', text }])
    setSending(true)
    try {
      const response = await DispatchSideChatMessage({
        id: crypto.randomUUID(), source: 'desktop-ask', session_id: sessionID, text,
      })
      setMessages(prev => [...prev, { id: crypto.randomUUID(), role: 'assistant', text: response.message }])
    } catch (error) {
      setMessages(prev => [...prev, { id: crypto.randomUUID(), role: 'assistant', text: `Ask failed: ${String(error)}` }])
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="sidechat-page">
      <header className="sidechat-header">
        <div><span className="sidechat-kicker">Chat · fast local lane</span><h1>Fast chat</h1><p>A compact no-tools conversation with no workspace packet, planning pass, or reviewer pass.</p></div>
        <div className="sidechat-header-actions">
          <button onClick={onOpenProjectChat}>Switch to project agent</button>
          <button onClick={() => setMessages([])} disabled={messages.length === 0 || sending}>New ask chat</button>
        </div>
      </header>
      <div className="sidechat-notice">
        <strong>No-tools lane</strong><span>{activeProfile ? `Using ${activeProfile}. ` : ''}Use Project Agent to inspect files, change the workspace, or run commands.</span>
      </div>
      <div className="sidechat-messages">
        {messages.length === 0 && <div className="sidechat-empty"><strong>Ask without loading the project agent</strong><span>Use this for explanations, ideas, syntax help, or a second opinion. It intentionally omits project documents and tools for lower latency.</span></div>}
        {messages.map(message => <article key={message.id} className={`sidechat-message ${message.role}`}><span>{message.role === 'user' ? 'You' : 'Mauler'}</span><ReactMarkdown remarkPlugins={[remarkGfm]}>{message.text}</ReactMarkdown></article>)}
        {sending && <article className="sidechat-message assistant pending"><span>Mauler</span><p>Thinking...</p></article>}
      </div>
      <div className="sidechat-composer">
        {streaming && <div className="sidechat-busy">Project run is active; the local model is currently reserved for it.</div>}
        <textarea value={draft} onChange={e => setDraft(e.target.value)} onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void send() } }} placeholder="Ask a fast no-tools question..." />
        <button onClick={() => void send()} disabled={!draft.trim() || sending || streaming}>Ask</button>
      </div>
    </div>
  )
}
