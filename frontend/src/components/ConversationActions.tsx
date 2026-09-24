import './ConversationActions.css'

interface Props {
  selected: string
  streaming: boolean
  compact?: boolean
  includeNew?: boolean
  onNew?: () => void
  onSave: () => void
  onRename: () => void
  onEditTags: () => void
  onInspect: () => void
  onCheckpoints: () => void
  onDelete: () => void
}

export function ConversationActions({
  selected,
  streaming,
  compact = false,
  includeNew = false,
  onNew,
  onSave,
  onRename,
  onEditTags,
  onInspect,
  onCheckpoints,
  onDelete,
}: Props) {
  return (
    <div className={`shared-conversation-actions${compact ? ' compact' : ''}`}>
      {includeNew && (
        <button type="button" onClick={onNew} disabled={streaming || !onNew}>
          <strong>New chat</strong><span>Ctrl+N</span>
        </button>
      )}
      <button type="button" onClick={onSave} disabled={streaming}>
        <strong>Save as…</strong><span>Ctrl+Shift+S</span>
      </button>
      <button type="button" onClick={onRename} disabled={!selected || streaming}>
        <strong>Rename chat</strong><span>Edit title</span>
      </button>
      <button type="button" onClick={onEditTags} disabled={!selected || streaming}>
        <strong>Edit tags</strong><span>Filter labels</span>
      </button>
      <button type="button" onClick={onInspect} disabled={!selected || streaming}>
        <strong>Check &amp; repair</strong><span>Safe preview</span>
      </button>
      <button type="button" onClick={onCheckpoints}>
        <strong>Checkpoints</strong><span>Save or resume</span>
      </button>
      <button type="button" className="danger" onClick={onDelete} disabled={!selected || streaming}>
        <strong>Delete saved chat</strong><span>Confirmation</span>
      </button>
    </div>
  )
}
