import { useId } from 'react'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { CONTROL_TRIGGER_CLASS } from './trigger'
import { cn } from '@/lib/utils'

/** Due date as a permanently visible control. The trigger always shows the
 * date itself, or 无截止 as a real, always-rendered state — never a blank —
 * matching the always-a-value rule PriorityControl and StatusControl
 * follow. No date-picker library: the popover just wraps a native
 * `<input type="date">` plus a 清除 button that reports `onChange(null)`. */
export default function DueDateControl({
  value, onChange, ariaLabel,
}: {
  value: string | null
  onChange: (next: string | null) => void
  ariaLabel: string
}) {
  const inputId = useId()

  return (
    <Popover>
      <PopoverTrigger
        aria-label={ariaLabel}
        className={cn(CONTROL_TRIGGER_CLASS, !value && 'text-fg-muted')}
      >
        {value ?? '无截止'}
      </PopoverTrigger>
      <PopoverContent className="w-auto p-3">
        <div className="flex flex-col gap-2">
          <label htmlFor={inputId} className="text-xs text-fg-muted">
            截止日期
          </label>
          <input
            id={inputId}
            type="date"
            value={value ?? ''}
            onChange={(e) => onChange(e.target.value || null)}
            className="rounded-md border border-border-strong bg-surface px-2 py-1 text-sm text-fg"
          />
          <button
            type="button"
            onClick={() => onChange(null)}
            className="self-start text-xs text-fg-muted hover:text-fg"
          >
            清除
          </button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
