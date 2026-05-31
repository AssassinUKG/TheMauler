import { useEffect, useState } from 'react'
import './Toast.css'

export interface ToastItem {
  id: string
  message: string
  level: 'warn' | 'danger'
}

interface Props {
  toasts: ToastItem[]
  onDismiss: (id: string) => void
}

export function ToastContainer({ toasts, onDismiss }: Props) {
  return (
    <div className="toast-container">
      {toasts.map(t => (
        <ToastBubble key={t.id} toast={t} onDismiss={onDismiss} />
      ))}
    </div>
  )
}

function ToastBubble({ toast, onDismiss }: { toast: ToastItem; onDismiss: (id: string) => void }) {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    requestAnimationFrame(() => setVisible(true))
    const timer = setTimeout(() => {
      setVisible(false)
      setTimeout(() => onDismiss(toast.id), 300)
    }, 6000)
    return () => clearTimeout(timer)
  }, [toast.id, onDismiss])

  return (
    <div className={`toast toast-${toast.level} ${visible ? 'toast-visible' : ''}`}>
      <span>{toast.message}</span>
      <button className="toast-close" onClick={() => { setVisible(false); setTimeout(() => onDismiss(toast.id), 300) }}>×</button>
    </div>
  )
}
