import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { cn } from '@/lib/utils'

/**
 * The "reads as text until you touch it" dress that used to live in
 * styles.css as `.inline-editable`: no visible boundary at rest, a hairline
 * on hover to say it is editable, and the accent border plus an opaque
 * surface once focused. The negative inline margin cancels the horizontal
 * padding so the text still lines up with the prose around it.
 *
 * Callers append their own classes and, via cn()'s tailwind-merge, reliably
 * override any of these — LabelManager, for one, drops the border entirely.
 */
const FIELD_CLASS =
  'block w-full -mx-2 rounded-sm border border-transparent bg-transparent px-2 py-1 outline-none hover:border-border focus:border-accent focus:bg-surface placeholder:text-fg-subtle'

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
      // The task inspector is a modal Sheet. Keep the field-level Escape
      // contract from bubbling into Radix and closing the entire inspector.
      e.stopPropagation()
      revertingRef.current = true
      setDraft(committedRef.current)
      e.currentTarget.blur()
    } else if (e.key === 'Enter' && !multiline) {
      e.preventDefault()
      e.currentTarget.blur()
    }
  }

  if (multiline) {
    // Rows track the actual line count instead of a fixed 4, so a short
    // (or empty) description reads as a short paragraph rather than a form
    // field with a large empty block underneath it. A floor of 2 keeps an
    // empty field from collapsing to a single sliver that's awkward to
    // click into. `resize-none` finishes the job: a drag handle in the
    // corner would advertise a form control the height already manages.
    const rows = Math.max(2, draft.split('\n').length)
    return (
      <textarea
        className={cn(FIELD_CLASS, 'resize-none leading-relaxed', className)}
        value={draft}
        placeholder={placeholder}
        aria-label={ariaLabel}
        rows={rows}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commitIfChanged}
        onKeyDown={handleKeyDown}
      />
    )
  }

  return (
    <input
      className={cn(FIELD_CLASS, className)}
      value={draft}
      placeholder={placeholder}
      aria-label={ariaLabel}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commitIfChanged}
      onKeyDown={handleKeyDown}
    />
  )
}
