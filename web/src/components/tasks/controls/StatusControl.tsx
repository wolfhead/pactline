import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { STATUS_LABELS, TASK_STATUSES, type TaskStatus } from '@/task-types'
import { cn } from '@/lib/utils'
import { CONTROL_TRIGGER_CLASS } from './trigger'

const DOT: Record<TaskStatus, string> = {
  backlog: 'border-status-backlog',
  todo: 'border-status-todo',
  in_progress: 'border-status-in-progress',
  in_review: 'border-status-in-review',
  done: 'border-status-done',
  cancelled: 'border-status-cancelled',
}

/** Status as a permanently visible control. The dot is a redundant cue —
 * the label text beside it always carries the meaning — so colour alone is
 * never the signal, and the dot only needs the 3:1 non-text contrast bar.
 *
 * Every status may move to every other status: there is no status graph and
 * no terminal state (see internal/domain/task.go). Do not add gating here. */
export default function StatusControl({
  value, onChange, ariaLabel, disabled,
}: {
  value: TaskStatus
  onChange: (next: TaskStatus) => void
  ariaLabel: string
  disabled?: boolean
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as TaskStatus)} disabled={disabled}>
      <SelectTrigger
        aria-label={ariaLabel}
        className={CONTROL_TRIGGER_CLASS}
      >
        {/* No dot of its own here. Radix renders the *selected item's* own
            children into SelectValue, dot included — a second one on the
            trigger showed up as "○ ○ 待办" on every status control in the
            app, on every tier and in both themes (found in Task 14's
            screenshot pass; no test could see it, since both spans are
            aria-hidden and the accessible name never changed). */}
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {TASK_STATUSES.map((s) => (
          <SelectItem key={s} value={s}>
            <span className="flex items-center gap-2">
              <span className={cn('size-2.5 shrink-0 rounded-full border-[1.6px]', DOT[s])} aria-hidden="true" />
              {STATUS_LABELS[s]}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
