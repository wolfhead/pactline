import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { PRIORITY_LABELS, TASK_PRIORITIES, type TaskPriority } from '@/task-types'
import { cn } from '@/lib/utils'
import { CONTROL_TRIGGER_CLASS } from './trigger'

// Only low/medium/high/urgent have a dedicated --color-priority-* token
// (see index.css); 'none' carries no priority colour of its own and reads
// as muted body text instead — still always-visible text, never a blank.
const TEXT: Record<TaskPriority, string> = {
  none: 'text-fg-muted',
  low: 'text-priority-low',
  medium: 'text-priority-medium',
  high: 'text-priority-high',
  urgent: 'text-priority-urgent',
}

/** Priority as a permanently visible control. Colour lives directly on the
 * label text (no separate redundant mark), which is why these tokens were
 * measured against the 4.5:1 *text* contrast bar, not the 3:1 non-text bar
 * status's dot uses (see index.css / task-3-report.md).
 *
 * "无优先级" (none) is a real, always-rendered option — unlike the old
 * PriorityMark, which returned null for `none` and left the row with a
 * blank gap. The control must always show a value; there is no genuinely
 * empty state here. */
export default function PriorityControl({
  value, onChange, ariaLabel, disabled,
}: {
  value: TaskPriority
  onChange: (next: TaskPriority) => void
  ariaLabel: string
  disabled?: boolean
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as TaskPriority)} disabled={disabled}>
      <SelectTrigger
        aria-label={ariaLabel}
        className={cn(CONTROL_TRIGGER_CLASS, 'font-medium', TEXT[value])}
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {TASK_PRIORITIES.map((p) => (
          <SelectItem key={p} value={p}>
            <span className={cn('font-medium', TEXT[p])}>{PRIORITY_LABELS[p]}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
