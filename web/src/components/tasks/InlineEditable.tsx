import { useEffect, useRef, useState, type KeyboardEvent } from 'react'

interface InlineEditableProps {
  value: string
  onCommit: (next: string) => void
  multiline?: boolean
  placeholder?: string
  ariaLabel: string
  className?: string
}

/**
 * A field that is always in its "editable" state — never a separate modal
 * or a click-to-edit toggle. Blur or Enter (single-line only; multiline
 * fields need Enter for newlines) commits a changed value; Escape discards
 * the in-progress edit and reverts to the last committed value without
 * calling onCommit. Used for a task's title and description (spec: "edited
 * where they are read").
 */
export default function InlineEditable({
  value,
  onCommit,
  multiline = false,
  placeholder,
  ariaLabel,
  className,
}: InlineEditableProps) {
  const [draft, setDraft] = useState(value)
  // Tracks the last value this field committed (or was initialized/reset
  // to), so blur only fires onCommit when the draft actually diverged from
  // it — not on every blur regardless of change.
  const committedRef = useRef(value)
  // Escape calls .blur() to leave the field, which is what normally
  // triggers a commit — but React 18 batches the setDraft(committedRef)
  // reset below, so the blur's onBlur can still run against the *stale*
  // (unreverted) draft closure before that reset is flushed. Without this
  // flag, that stale draft reads as a real, changed value and gets
  // committed anyway — the opposite of "Escape reverts". The flag makes
  // the very next commitIfChanged a no-op regardless of batching timing.
  const revertingRef = useRef(false)

  // A prop update (e.g. a successful save elsewhere, or a revert after a
  // failed optimistic update) always wins over an unsubmitted local draft.
  useEffect(() => {
    setDraft(value)
    committedRef.current = value
  }, [value])

  function commitIfChanged() {
    if (revertingRef.current) {
      revertingRef.current = false
      return
    }
    const next = multiline ? draft : draft.trim()
    if (next !== committedRef.current) {
      committedRef.current = next
      onCommit(next)
    } else if (next !== draft) {
      setDraft(next)
    }
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      revertingRef.current = true
      setDraft(committedRef.current)
      e.currentTarget.blur()
    } else if (e.key === 'Enter' && !multiline) {
      e.preventDefault()
      e.currentTarget.blur()
    }
  }

  if (multiline) {
    return (
      <textarea
        className={`inline-editable ${className ?? ''}`}
        value={draft}
        placeholder={placeholder}
        aria-label={ariaLabel}
        rows={4}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commitIfChanged}
        onKeyDown={handleKeyDown}
      />
    )
  }

  return (
    <input
      className={`inline-editable ${className ?? ''}`}
      value={draft}
      placeholder={placeholder}
      aria-label={ariaLabel}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commitIfChanged}
      onKeyDown={handleKeyDown}
    />
  )
}
