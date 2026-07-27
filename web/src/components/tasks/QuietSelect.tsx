import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'

interface QuietSelectProps<T extends string> {
  value: T
  options: readonly T[]
  labels: Record<T, string>
  onChange: (next: T) => void
  ariaLabel: string
  /** Custom quiet-state content (a colour mark, a name, or nothing at all
   * for a genuinely-empty value). Defaults to the plain label text. */
  renderQuiet?: (value: T, label: string) => ReactNode
  className?: string
  disabled?: boolean
}

/**
 * A value that reads as plain text (or a small mark) until it's
 * interacted with. Click, Enter or Space on the quiet display reveals the
 * real, native <select> — so keyboard and screen-reader behaviour comes
 * for free — which commits the instant an option is chosen and collapses
 * straight back to the quiet display. Escape (or blur without a choice)
 * collapses it the same way, without calling onChange, since nothing has
 * changed yet: there is no separate committed/draft state to revert here,
 * unlike InlineEditable's text fields — a <select> only ever "changes" by
 * an explicit choice.
 *
 * Both states share one aria-label, so a caller (or a test driving this by
 * accessible name/role) never needs a second locator for "the same
 * property, now open" — `getByLabel(x)` resolves to whichever is mounted.
 *
 * Replaces the old PillSelect, which rendered the <select> permanently —
 * exactly the "every editable value is its own editor, always" pattern the
 * UX redesign report diagnoses as the whole problem across all three views.
 */
export default function QuietSelect<T extends string>({
  value,
  options,
  labels,
  onChange,
  ariaLabel,
  renderQuiet,
  className,
  disabled,
}: QuietSelectProps<T>) {
  const [editing, setEditing] = useState(false)
  const selectRef = useRef<HTMLSelectElement>(null)

  useEffect(() => {
    if (!editing) return
    const el = selectRef.current
    if (!el) return
    el.focus()
    // Best-effort: opens the native picker in the same interaction that
    // revealed the control, so one click (not two) both reveals and opens
    // it. Only recent Chromium implements showPicker() on a <select> — the
    // field is still fully keyboard-usable (arrows/Enter open it natively)
    // where this is unavailable or throws (some engines require the call
    // to originate from the original user gesture, which this timing may
    // have already left).
    const withPicker = el as HTMLSelectElement & { showPicker?: () => void }
    try {
      withPicker.showPicker?.()
    } catch {
      // Ignored — see comment above; focus() already happened.
    }
  }, [editing])

  function startEditing() {
    if (disabled) return
    setEditing(true)
  }

  function handleTriggerKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      startEditing()
    }
  }

  function handleSelectKeyDown(e: KeyboardEvent<HTMLSelectElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      setEditing(false)
    }
  }

  if (editing) {
    return (
      <select
        ref={selectRef}
        className={`quiet-select-control ${className ?? ''}`}
        value={value}
        aria-label={ariaLabel}
        disabled={disabled}
        onChange={(e) => {
          onChange(e.target.value as T)
          setEditing(false)
        }}
        onBlur={() => setEditing(false)}
        onKeyDown={handleSelectKeyDown}
      >
        {options.map((o) => (
          <option key={o} value={o}>
            {labels[o]}
          </option>
        ))}
      </select>
    )
  }

  const label = labels[value]
  return (
    <button
      type="button"
      className={`quiet-value ${className ?? ''}`}
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={startEditing}
      onKeyDown={handleTriggerKeyDown}
    >
      {renderQuiet ? renderQuiet(value, label) : label}
    </button>
  )
}
