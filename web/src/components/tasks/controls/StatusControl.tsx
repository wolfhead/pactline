import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { STATUS_LABELS, TASK_STATUSES, type TaskStatus } from '@/task-types'
import { cn } from '@/lib/utils'
import { Circle, CircleCheck, CircleDot, CircleX, Eye } from 'lucide-react'
import { CONTROL_TRIGGER_CLASS, PROPERTY_ICON_SLOT_CLASS } from './trigger'

const STATUS_ICON_CLASS: Record<TaskStatus, string> = {
  todo: 'text-status-todo',
  in_progress: 'text-status-in-progress',
  in_review: 'text-status-in-review',
  done: 'text-status-done',
  cancelled: 'text-status-cancelled',
}

const STATUS_ICONS = {
  todo: Circle,
  in_progress: CircleDot,
  in_review: Eye,
  done: CircleCheck,
  cancelled: CircleX,
} satisfies Record<TaskStatus, typeof Circle>

function StatusIcon({ status }: { status: TaskStatus }) {
  const Icon = STATUS_ICONS[status]
  return <Icon className={cn('size-4', STATUS_ICON_CLASS[status])} aria-hidden="true" />
}

/** Status as a permanently visible control. Each lifecycle state has a
 * distinct shape as well as colour, and the trigger always has an accessible
 * name. Compact task rows show only the icon; detail views keep icon + text.
 *
 * Every status may move to every other status: there is no status graph and
 * no terminal state (see internal/domain/task.go). Do not add gating here. */
export default function StatusControl({
  value, onChange, ariaLabel, disabled, compact = false,
}: {
  value: TaskStatus
  onChange: (next: TaskStatus) => void
  ariaLabel: string
  disabled?: boolean
  compact?: boolean
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as TaskStatus)} disabled={disabled}>
      <SelectTrigger
        data-property-control
        data-compact={compact ? 'true' : undefined}
        aria-label={ariaLabel}
        title={STATUS_LABELS[value]}
        className={cn(
          CONTROL_TRIGGER_CLASS,
          compact && 'size-8 justify-center gap-0 px-0 [&>[data-select-chevron]]:hidden',
        )}
      >
        {compact ? <StatusIcon status={value} /> : <SelectValue />}
      </SelectTrigger>
      <SelectContent>
        {TASK_STATUSES.map((s) => (
          <SelectItem key={s} value={s}>
            <span className="flex items-center gap-1.5">
              <span className={PROPERTY_ICON_SLOT_CLASS} aria-hidden="true">
                <StatusIcon status={s} />
              </span>
              <span>{STATUS_LABELS[s]}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
