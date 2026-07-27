import { STATUS_LABELS, type TaskPriority, type TaskStatus } from '../../task-types'

/** Status's quiet display: a small colour dot plus its label. The dot is a
 * redundant cue — the label text beside it is what actually carries the
 * meaning — so it only needs the 3:1 non-text contrast bar, not the 4.5:1
 * text bar (see the --status-* token comments in styles.css for the
 * numbers). Used as the `renderQuiet` for every status QuietSelect, so the
 * list, board and detail views all draw status the same way. */
export function StatusMark({ status }: { status: TaskStatus }) {
  return (
    <span className="status-mark" data-status={status}>
      <span className="status-dot" aria-hidden="true" />
      {STATUS_LABELS[status]}
    </span>
  )
}

/** Priority's quiet display: compact coloured text, no border or
 * background of its own, so it never competes with a control for width.
 * "无优先级" (none) is treated as a genuinely absent value — it renders
 * nothing, the same rule the redesign applies to a blank due date, rather
 * than a placeholder pill that would just add clutter to every
 * unprioritised task. */
export function PriorityMark({ priority, label }: { priority: TaskPriority; label: string }) {
  if (priority === 'none') return null
  return (
    <span className="priority-mark" data-priority={priority}>
      {label}
    </span>
  )
}
