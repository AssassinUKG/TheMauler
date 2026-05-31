import type { ConfirmPayload } from '../App'
import './ConfirmDialog.css'

interface Props {
  payload?: ConfirmPayload
  title?: string
  message?: string
  detail?: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
  onAllow: () => void
  onAllowRemember?: () => void
  onDeny: () => void
}

export function ConfirmDialog({
  payload,
  title,
  message,
  detail,
  confirmLabel,
  cancelLabel,
  danger = true,
  onAllow,
  onAllowRemember,
  onDeny,
}: Props) {
  const input = detail ?? payload?.input

  return (
    <div className="overlay">
      <div className="confirm-dialog">
        <div className="confirm-header">
          <span className="confirm-icon">!</span>
          <span className="confirm-title">{title ?? 'Confirm Tool Execution'}</span>
        </div>
        {payload && (
          <div className="confirm-tool">
            Tool: <strong>{payload.name}</strong>
          </div>
        )}
        {message && <div className="confirm-message">{message}</div>}
        {input && <pre className="confirm-input">{input}</pre>}
        <div className="confirm-actions">
          <button className={danger ? 'danger' : ''} onClick={onDeny}>{cancelLabel ?? 'Deny'}</button>
          {payload && onAllowRemember && (
            <button onClick={onAllowRemember}>Allow &amp; remember</button>
          )}
          <button className="primary" onClick={onAllow}>{confirmLabel ?? 'Allow'}</button>
        </div>
      </div>
    </div>
  )
}
