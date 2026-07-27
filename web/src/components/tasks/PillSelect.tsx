interface PillSelectProps<T extends string> {
  value: T
  options: readonly T[]
  labels: Record<T, string>
  onChange: (next: T) => void
  ariaLabel: string
  className?: string
  disabled?: boolean
}

/** A native <select> styled as a quiet pill — status/priority controls that
 * commit the moment they change, no separate save step. Native so keyboard
 * and screen-reader behaviour comes for free. */
export default function PillSelect<T extends string>({
  value,
  options,
  labels,
  onChange,
  ariaLabel,
  className,
  disabled,
}: PillSelectProps<T>) {
  return (
    <select
      className={`pill-select ${className ?? ''}`}
      value={value}
      aria-label={ariaLabel}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value as T)}
    >
      {options.map((o) => (
        <option key={o} value={o}>
          {labels[o]}
        </option>
      ))}
    </select>
  )
}
