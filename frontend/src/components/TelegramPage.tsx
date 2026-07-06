import { useEffect, useMemo, useRef, useState } from 'react'
import {
  DeleteTelegramMessage,
  GetChannelBusStatus,
  ListChannelWorkQueue,
  ListLedgerEvents,
  PruneLedgerEvents,
  SendTelegramMessage,
  type ChannelWorkItem,
  type LedgerEvent,
} from '../wailsjs/go'
import './TelegramPage.css'

type ChatMessage = {
  id: string
  eventIds: string[]
  direction: 'in' | 'out' | 'system'
  chatId: string
  messageId: string
  user: string
  text: string
  status: string
  timestamp: string
  event: LedgerEvent
}

export function TelegramPage({ version }: { version: number }) {
  const [status, setStatus] = useState<Record<string, string>>({})
  const [queue, setQueue] = useState<ChannelWorkItem[]>([])
  const [events, setEvents] = useState<LedgerEvent[]>([])
  const [query, setQuery] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [selectedId, setSelectedId] = useState('')
  const [selectedChat, setSelectedChat] = useState('')
  const [composer, setComposer] = useState('')
  const [actionStatus, setActionStatus] = useState('')
  const chatEndRef = useRef<HTMLDivElement | null>(null)

  const load = async () => {
    const [nextStatus, nextQueue, nextEvents] = await Promise.all([
      GetChannelBusStatus().catch(() => ({} as Record<string, string>)),
      ListChannelWorkQueue().catch(() => [] as ChannelWorkItem[]),
      ListLedgerEvents(700).catch(() => [] as LedgerEvent[]),
    ])
    const telegramEvents = nextEvents.filter(isTelegramEvent)
    setStatus(nextStatus)
    setQueue(nextQueue)
    setEvents(telegramEvents)
    setSelectedId(prev => prev && telegramEvents.some(event => event.id === prev) ? prev : telegramEvents[0]?.id ?? '')
  }

  useEffect(() => {
    void load()
  }, [version])

  useEffect(() => {
    if (!autoRefresh) return
    const id = window.setInterval(() => { void load() }, 2500)
    return () => window.clearInterval(id)
  }, [autoRefresh])

  const messages = useMemo(() => buildChatMessages(events), [events])
  const chats = useMemo(() => buildChats(messages), [messages])
  const activeChat = selectedChat || chats[0]?.id || ''
  const chatMessages = useMemo(() => messages
    .filter(message => !activeChat || message.chatId === activeChat)
    .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()), [messages, activeChat])

  useEffect(() => {
    if (!selectedChat && chats[0]?.id) setSelectedChat(chats[0].id)
    if (selectedChat && !chats.some(chat => chat.id === selectedChat)) setSelectedChat(chats[0]?.id ?? '')
  }, [chats, selectedChat])

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ block: 'end' })
  }, [chatMessages.length, activeChat])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return events
    return events.filter(event => telegramEventText(event).includes(q))
  }, [events, query])

  const selected = filtered.find(event => event.id === selectedId) ?? filtered[0]
  const stats = useMemo(() => buildTelegramStats(events, queue, status), [events, queue, status])

  useEffect(() => {
    if (filtered.length > 0 && (!selectedId || !filtered.some(event => event.id === selectedId))) {
      setSelectedId(filtered[0].id)
    }
  }, [filtered, selectedId])

  const refresh = async () => {
    await load()
    flash('Refreshed')
  }

  const copyJSON = async () => {
    await navigator.clipboard.writeText(JSON.stringify({ status, queue, events }, null, 2))
    flash('Copied')
  }

  const sendMessage = async () => {
    const text = composer.trim()
    if (!text) return
    if (!activeChat) {
      flash('No Telegram chat selected')
      return
    }
    setComposer('')
    try {
      await SendTelegramMessage(activeChat, text)
      flash('Sent')
      await load()
    } catch (err) {
      flash(err instanceof Error ? err.message : String(err))
    }
  }

  const pruneMessages = async (items: ChatMessage[]) => {
    const ids = [...new Set(items.flatMap(item => item.eventIds))]
    if (ids.length === 0) return
    const removed = await PruneLedgerEvents('ids', ids)
    flash(`Removed ${removed} local event${removed === 1 ? '' : 's'}`)
    await load()
  }

  const deleteMessage = async (message: ChatMessage) => {
    if (!message.chatId || !message.messageId) {
      await pruneMessages([message])
      return
    }
    try {
      await DeleteTelegramMessage(message.chatId, message.messageId)
      await pruneMessages([message])
      flash('Deleted from Telegram and local log')
    } catch (err) {
      flash(`Telegram delete failed; removing local copy`)
      await pruneMessages([message])
    }
  }

  const clearChat = async () => {
    await pruneMessages(chatMessages)
  }

  function flash(text: string) {
    setActionStatus(text)
    window.setTimeout(() => setActionStatus(current => current === text ? '' : current), 2200)
  }

  return (
    <div className="telegram-page">
      <header className="telegram-header">
        <div>
          <h1>Telegram</h1>
          <p>Bot chat, queue, and remote control activity.</p>
        </div>
        <div className="telegram-actions">
          <label className="telegram-toggle">
            <input type="checkbox" checked={autoRefresh} onChange={e => setAutoRefresh(e.target.checked)} />
            <span>Auto</span>
          </label>
          <button onClick={() => void refresh()}>Refresh</button>
          <button onClick={() => void copyJSON()}>Copy JSON</button>
          {actionStatus && <span>{actionStatus}</span>}
        </div>
      </header>

      <section className="telegram-kpis">
        {stats.map(item => (
          <div className="telegram-kpi" key={item.label}>
            <span>{item.label}</span>
            <strong title={item.value}>{item.value || '-'}</strong>
          </div>
        ))}
      </section>

      <div className="telegram-layout">
        <aside className="telegram-side">
          <div className="telegram-panel telegram-chat-list">
            <div className="telegram-panel-head">
              <h2>Chats</h2>
              <span>{chats.length}</span>
            </div>
            {chats.length === 0 ? (
              <div className="telegram-empty">No Telegram chats yet.</div>
            ) : chats.map(chat => (
              <button
                className={chat.id === activeChat ? 'telegram-chat-row active' : 'telegram-chat-row'}
                key={chat.id}
                onClick={() => setSelectedChat(chat.id)}
                title={chat.last}
              >
                <strong>{chat.label}</strong>
                <span>{chat.count} messages</span>
                <small>{chat.last || chat.id}</small>
              </button>
            ))}
          </div>

          <div className="telegram-panel">
            <div className="telegram-panel-head">
              <h2>Queue</h2>
              <span>{queue.length}</span>
            </div>
            {queue.length === 0 ? (
              <div className="telegram-empty">No active queued remote work.</div>
            ) : queue.map(item => (
              <button className="telegram-queue-row" key={item.id} title={item.envelope.text}>
                <span>{item.status} - {item.route.lane}</span>
                <strong>{item.route.command || item.route.reason || 'message'}</strong>
                <small>{item.envelope.text || item.route.argument || item.id}</small>
              </button>
            ))}
          </div>

          <div className="telegram-panel">
            <div className="telegram-panel-head">
              <h2>Status</h2>
              <span>{status.telegram_running === 'true' ? 'live' : 'off'}</span>
            </div>
            {Object.entries(status).filter(([key]) => key.startsWith('telegram_') || key === 'queued_work' || key === 'agent_running').map(([key, value]) => (
              <div className="telegram-status-row" key={key}>
                <span>{key.replaceAll('_', ' ')}</span>
                <strong title={value}>{value || '-'}</strong>
              </div>
            ))}
          </div>
        </aside>

        <main className="telegram-main">
          <section className="telegram-console">
            <div className="telegram-console-head">
              <div>
                <span>Selected Chat</span>
                <h2>{activeChat || 'No chat selected'}</h2>
              </div>
              <div className="telegram-console-actions">
                <button onClick={() => void clearChat()} disabled={chatMessages.length === 0}>Clear Local Chat</button>
              </div>
            </div>

            <div className="telegram-transcript">
              {chatMessages.length === 0 ? (
                <div className="telegram-empty">No messages in this chat yet.</div>
              ) : chatMessages.map(message => (
                <div className={`telegram-bubble-row ${message.direction}`} key={message.id}>
                  <article className="telegram-bubble">
                    <div className="telegram-bubble-meta">
                      <span>{message.direction === 'out' ? 'Bot' : message.user || 'User'} - {formatTime(message.timestamp)}</span>
                      <strong>{message.status}</strong>
                    </div>
                    <p>{message.text || '-'}</p>
                    <div className="telegram-bubble-actions">
                      <button onClick={() => setSelectedId(message.event.id)}>Inspect</button>
                      <button onClick={() => void navigator.clipboard.writeText(message.text)}>Copy</button>
                      <button onClick={() => void deleteMessage(message)}>Remove</button>
                    </div>
                  </article>
                </div>
              ))}
              <div ref={chatEndRef} />
            </div>

            <div className="telegram-composer">
              <textarea
                value={composer}
                onChange={e => setComposer(e.target.value)}
                placeholder={activeChat ? 'Send a bot message to this Telegram chat...' : 'Select a chat first...'}
                onKeyDown={event => {
                  if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
                    event.preventDefault()
                    void sendMessage()
                  }
                }}
              />
              <button onClick={() => void sendMessage()} disabled={!composer.trim() || !activeChat}>Send</button>
            </div>
          </section>

          <section className="telegram-events">
            <div className="telegram-filters">
              <input value={query} onChange={e => setQuery(e.target.value)} placeholder="Search raw Telegram events, queue ids, statuses..." />
              <span>{filtered.length} events</span>
            </div>

            <div className="telegram-event-layout">
              <div className="telegram-event-list">
                {filtered.length === 0 ? (
                  <div className="telegram-empty">{events.length === 0 ? 'No Telegram events yet' : 'No matching events'}</div>
                ) : filtered.map(event => (
                  <button
                    key={event.id}
                    className={selected?.id === event.id ? 'active' : ''}
                    onClick={() => setSelectedId(event.id)}
                  >
                    <span>{formatTime(event.timestamp)} - {event.kind}</span>
                    <strong>{event.status || event.source || 'event'}</strong>
                    <small>{event.message || event.detail || event.output || '-'}</small>
                  </button>
                ))}
              </div>

              <div className="telegram-detail">
                {!selected ? (
                  <div className="telegram-empty">Select an event.</div>
                ) : (
                  <>
                    <div className="telegram-detail-head">
                      <div>
                        <span>{selected.source || 'unknown'}</span>
                        <h2>{selected.kind}</h2>
                      </div>
                      <div className="telegram-detail-actions">
                        <strong>{selected.status || '-'}</strong>
                        <button onClick={() => void pruneMessages([{ id: selected.id, eventIds: [selected.id], direction: 'system', chatId: '', messageId: '', user: '', text: selected.message || '', status: selected.status || '', timestamp: selected.timestamp, event: selected }])}>Remove Local</button>
                      </div>
                    </div>
                    <dl>
                      <dt>Time</dt><dd>{selected.timestamp}</dd>
                      <dt>Message</dt><dd>{selected.message || '-'}</dd>
                      <dt>Detail</dt><dd><pre>{selected.detail || selected.error || selected.output || '-'}</pre></dd>
                      <dt>Metadata</dt><dd><pre>{JSON.stringify(selected.metadata ?? {}, null, 2)}</pre></dd>
                    </dl>
                  </>
                )}
              </div>
            </div>
          </section>
        </main>
      </div>
    </div>
  )
}

function isTelegramEvent(event: LedgerEvent): boolean {
  const source = (event.source || '').toLowerCase()
  const kind = (event.kind || '').toLowerCase()
  const metaSource = (event.metadata?.source || '').toLowerCase()
  return source === 'telegram' || source === 'channelbus' || metaSource === 'telegram' || kind.includes('telegram') || kind.includes('channel_')
}

function telegramEventText(event: LedgerEvent): string {
  return [
    event.kind,
    event.source,
    event.status,
    event.message,
    event.detail,
    event.error,
    event.output,
    JSON.stringify(event.metadata ?? {}),
  ].join(' ').toLowerCase()
}

function buildChatMessages(events: LedgerEvent[]): ChatMessage[] {
  return events
    .filter(event => event.kind === 'telegram_message_in' || event.kind === 'telegram_send')
    .map(event => {
      const meta = event.metadata ?? {}
      const direction = event.kind === 'telegram_send' ? 'out' : 'in'
      const chatId = meta.chat_id || 'unknown'
      const messageId = direction === 'out' ? (meta.telegram_message_id || '') : (meta.source_message_id || '')
      return {
        id: event.id,
        eventIds: [event.id],
        direction,
        chatId,
        messageId,
        user: meta.username ? `@${meta.username}` : meta.from_id || 'Telegram',
        text: event.message || event.detail || event.output || '',
        status: event.status || 'event',
        timestamp: event.timestamp,
        event,
      }
    })
}

function buildChats(messages: ChatMessage[]) {
  const byChat = new Map<string, { id: string; label: string; count: number; last: string; timestamp: string }>()
  for (const message of messages) {
    const current = byChat.get(message.chatId)
    const timestamp = current && new Date(current.timestamp).getTime() > new Date(message.timestamp).getTime() ? current.timestamp : message.timestamp
    byChat.set(message.chatId, {
      id: message.chatId,
      label: message.user && message.direction === 'in' ? message.user : `Chat ${message.chatId}`,
      count: (current?.count ?? 0) + 1,
      last: new Date(timestamp).getTime() === new Date(message.timestamp).getTime() ? message.text : current?.last ?? message.text,
      timestamp,
    })
  }
  return [...byChat.values()].sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
}

function buildTelegramStats(events: LedgerEvent[], queue: ChannelWorkItem[], status: Record<string, string>) {
  const inbound = events.filter(event => event.kind === 'telegram_message_in' || event.kind === 'channel_message_in').length
  const outbound = events.filter(event => event.kind === 'telegram_send' || event.kind === 'channel_message_out').length
  const rejects = events.filter(event => event.kind === 'telegram_reject' || event.status === 'rejected').length
  const errors = events.filter(event => (event.status || '').includes('error') || (event.kind || '').includes('error')).length
  return [
    { label: 'Bot', value: status.telegram_status || 'unknown' },
    { label: 'Running', value: status.telegram_running || 'false' },
    { label: 'Updates', value: status.telegram_updates_seen || '0' },
    { label: 'Queued', value: String(queue.length) },
    { label: 'Inbound', value: String(inbound) },
    { label: 'Outbound', value: String(outbound) },
    { label: 'Rejected', value: String(rejects) },
    { label: 'Errors', value: String(errors) },
  ]
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value || '-'
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
