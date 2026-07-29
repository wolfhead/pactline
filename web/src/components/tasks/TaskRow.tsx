import { Link } from 'react-router-dom'
import type { Tier } from '@/hooks/useBreakpoint'
import { cn } from '@/lib/utils'
import type { Task, TaskPatchBody, UserRef } from '@/task-types'
import AssigneeControl from './controls/AssigneeControl'
import DueDateControl from './controls/DueDateControl'
import PriorityControl from './controls/PriorityControl'
import StatusControl from './controls/StatusControl'
import RowActionsMenu from './RowActionsMenu'

interface TaskRowProps {
  task: Task
  selected: boolean
  tier: Tier
  users: UserRef[]
  error?: string
  onPatch: (task: Task, patch: TaskPatchBody, optimistic: Partial<Task>) => void
  onArchive: (task: Task) => void
  onRestore: (task: Task) => void
}

/** One row: every property control is permanently visible — no hover or
 * click needed first, the opposite of the old QuietSelect/InlineEditable
 * reveal-on-interaction pattern this redesign replaces. Desktop renders a
 * single 40px line with the number, title and labels on the left and the
 * five right-aligned controls (status/priority/assignee/due
 * date/actions) in a fixed column so they line up across every row; a
 * phone renders a two-line card instead — see the per-tier branches below.
 *
 * `onPatch` always carries both the wire patch and the optimistic
 * fragment. The assignee path is the one case where those two differ in
 * shape: the wire patch sends `assignee_id` (a bare id, per
 * TaskPatchBody), but the optimistic fragment needs the resolved
 * `UserRef` — the row renders `assignee.name`, so an id there would blank
 * the cell until the server answered. */
export default function TaskRow({ task, selected, tier, users, error, onPatch, onArchive, onRestore }: TaskRowProps) {
  const isPhone = tier === 'phone'
  const dimmed = task.status === 'done' || task.status === 'cancelled'
  // fg-subtle must never sit on the selected row's accent-subtle background
  // (insufficient contrast — see index.css's tier comments); fg-muted is the
  // weakened-text tier that stays legible there, so a selected done/cancelled
  // row's title falls back to it instead.
  const dimmedTextClass = dimmed ? (selected ? 'text-fg-muted' : 'text-fg-subtle') : undefined

  function handleChangeStatus(status: Task['status']) {
    onPatch(task, { status }, { status })
  }

  function handleChangePriority(priority: Task['priority']) {
    onPatch(task, { priority }, { priority })
  }

  function handleChangeAssignee(assigneeId: string | null) {
    const assignee = assigneeId ? (users.find((u) => u.id === assigneeId) ?? null) : null
    onPatch(task, { assignee_id: assigneeId }, { assignee })
  }

  function handleChangeDueDate(dueDate: string | null) {
    onPatch(task, { due_date: dueDate }, { due_date: dueDate })
  }

  function handleCopyLink() {
    const url = `${window.location.origin}/tasks/${task.number}`
    navigator.clipboard?.writeText(url).catch(() => {
      // Best-effort: clipboard access can be unavailable (insecure context,
      // permission denied). 打开详情 in the same menu still reaches the task.
    })
  }

  const statusControl = (
    <StatusControl value={task.status} onChange={handleChangeStatus} ariaLabel={`任务 #${task.number} 状态`} />
  )
  const priorityControl = (
    <PriorityControl value={task.priority} onChange={handleChangePriority} ariaLabel={`任务 #${task.number} 优先级`} />
  )
  const assigneeControl = (
    <AssigneeControl
      value={task.assignee?.id ?? null}
      users={users}
      onChange={handleChangeAssignee}
      ariaLabel={`任务 #${task.number} 负责人`}
    />
  )
  const dueDateControl = (
    <DueDateControl value={task.due_date} onChange={handleChangeDueDate} ariaLabel={`任务 #${task.number} 截止日期`} />
  )
  const actionsMenu = (
    <RowActionsMenu
      taskNumber={task.number}
      archived={Boolean(task.archived_at)}
      onArchive={() => onArchive(task)}
      onRestore={() => onRestore(task)}
      onCopyLink={handleCopyLink}
    />
  )

  const labelChips = task.labels.length > 0 && (
    <div className="flex min-w-0 shrink-0 flex-wrap items-center gap-1">
      {task.labels.map((l) => (
        <span key={l.id} className="rounded-full bg-surface-subtle px-2 py-0.5 text-[11px] text-fg-muted">
          {l.name}
        </span>
      ))}
    </div>
  )

  if (isPhone) {
    return (
      <div
        role="listitem"
        aria-current={selected ? 'true' : undefined}
        data-task-number={task.number}
        className={cn(
          'flex flex-col gap-1.5 border-b border-border px-3 py-2.5',
          selected && 'bg-accent-subtle shadow-[inset_3px_0_0_var(--color-accent)]',
        )}
      >
        <div className="flex items-start justify-between gap-2">
          <Link
            to={`/tasks/${task.number}`}
            className={cn('flex min-h-11 min-w-0 flex-1 items-center text-sm font-medium', dimmedTextClass)}
          >
            {task.title}
          </Link>
          <div className="flex shrink-0 items-center gap-1.5">
            {statusControl}
            {actionsMenu}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {labelChips}
          {priorityControl}
          {dueDateControl}
          <div className="ml-auto shrink-0">{assigneeControl}</div>
        </div>
        {error && (
          <p role="alert" className="text-xs text-danger">
            {error}
          </p>
        )}
      </div>
    )
  }

  return (
    <div
      role="listitem"
      aria-current={selected ? 'true' : undefined}
      data-task-number={task.number}
      className={cn(
        'flex h-10 items-center gap-2 border-b border-border px-3',
        selected && 'bg-accent-subtle shadow-[inset_3px_0_0_var(--color-accent)]',
      )}
    >
      <span className="w-12 shrink-0 font-mono text-xs text-fg-muted">#{task.number}</span>
      <Link
        to={`/tasks/${task.number}`}
        className={cn('min-w-0 flex-1 truncate text-sm', dimmedTextClass)}
      >
        {task.title}
      </Link>
      {labelChips}
      <div className="w-28 shrink-0">{statusControl}</div>
      <div className="w-24 shrink-0">{priorityControl}</div>
      <div className="w-28 shrink-0">{assigneeControl}</div>
      {/* Width-wrapped exactly like the three controls above. It used to be
          left bare on the theory that a due date is "near-fixed-width
          regardless of value" — it is not: 无截止 measures ~36px narrower
          than 2026-08-15, so every row carrying a real date dragged status,
          priority and assignee ~36px left of the rows that don't, and the
          five control columns read as ragged down the whole list (found in
          Task 14's screenshot pass, at every tier and in both themes). */}
      <div className="w-24 shrink-0 max-[1399px]:hidden">{dueDateControl}</div>
      {actionsMenu}
      {error && (
        <p role="alert" className="ml-2 shrink-0 text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  )
}
