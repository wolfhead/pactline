import { useEffect, useRef, useState, type KeyboardEvent } from 'react'

interface QuietDateFieldProps {
  value: string | null
  onChange: (next: string | null) => void
  ariaLabel: string
}

/**
 * Same reveal-on-interaction pattern as QuietSelect, for the one property
 * that isn't an enum: due date. Quiet display is the date itself, or
 * nothing at all when unset — never a placeholder, matching the same rule
 * applied to priority and to the list's due-date column. Click, Enter or
 * Space reveals a native <input type="date">; a chosen date commits and
 * collapses back; Escape (or blur without a choice) just collapses it.
 */
export default function QuietDateField({ value, onChange, ariaLabel }: QuietDateFieldProps) {
  const [editing, setEditing] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!editing) return
    const el = inputRef.current
    if (!el) return
    el.focus()
    const withPicker = el as HTMLInputElement & { showPicker?: () => void }
    try {
      withPicker.showPicker?.()
    } catch {
      // Ignored — see QuietSelect's identical comment.
    }
  }, [editing])

  function handleTriggerKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      setEditing(true)
    }
  }

  function handleInputKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      setEditing(false)
    }
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        type="date"
        className="quiet-select-control"
        aria-label={ariaLabel}
        value={value ?? ''}
        onChange={(e) => {
          onChange(e.target.value || null)
          setEditing(false)
        }}
        onBlur={() => setEditing(false)}
        onKeyDown={handleInputKeyDown}
      />
    )
  }

  return (
    <button
      type="button"
      className="quiet-value"
      aria-label={ariaLabel}
      onClick={() => setEditing(true)}
      onKeyDown={handleTriggerKeyDown}
    >
      {value ?? ''}
    </button>
  )
}
