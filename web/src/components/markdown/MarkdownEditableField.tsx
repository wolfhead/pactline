import { useEffect, useState, type KeyboardEvent } from 'react'
import MarkdownComposer from './MarkdownComposer'
import MarkdownContent from './MarkdownContent'

interface MarkdownEditableFieldProps {
  label: string
  value: string
  onCommit: (value: string) => void
  placeholder: string
  required?: boolean
}

export default function MarkdownEditableField({
  label,
  value,
  onCommit,
  placeholder,
  required = false,
}: MarkdownEditableFieldProps) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(value)

  useEffect(() => {
    setDraft(value)
  }, [value])

  function beginEditing() {
    setDraft(value)
    setEditing(true)
  }

  function cancelEditing() {
    setDraft(value)
    setEditing(false)
  }

  function save() {
    if (required && !draft.trim()) return
    setEditing(false)
    if (draft !== value) onCommit(draft)
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Escape') return
    event.preventDefault()
    event.stopPropagation()
    cancelEditing()
  }

  return (
    <section role="region" aria-label={label} className="min-w-0">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-xs font-medium text-fg-muted">{label}</h3>
        {!editing && (
          <button
            type="button"
            aria-label={`编辑${label}`}
            onClick={beginEditing}
            className="rounded px-2 py-1 text-xs font-medium text-fg-muted hover:bg-surface-subtle hover:text-fg"
          >
            编辑
          </button>
        )}
      </div>

      {editing ? (
        <div className="mt-1 grid gap-2" onKeyDown={handleKeyDown}>
          <MarkdownComposer
            value={draft}
            onChange={setDraft}
            ariaLabel={`${label} Markdown`}
            placeholder={placeholder}
            rows={Math.max(3, draft.split('\n').length)}
            autoFocus
          />
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={cancelEditing}
              className="rounded-md px-3 py-1.5 text-xs font-medium text-fg-muted hover:bg-surface-subtle hover:text-fg"
            >
              取消
            </button>
            <button
              type="button"
              aria-label={`保存${label}`}
              disabled={required && !draft.trim()}
              onClick={save}
              className="rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50"
            >
              保存
            </button>
          </div>
        </div>
      ) : value.trim() ? (
        <div className="mt-1">
          <MarkdownContent source={value} />
        </div>
      ) : (
        <p className="mt-1 text-sm text-fg-subtle">{placeholder}</p>
      )}
    </section>
  )
}
