import type { Tier } from '@/hooks/useBreakpoint'
import { STATUS_LABELS, TASK_STATUSES, type Task, type TaskPatchBody, type UserRef } from '@/task-types'
import { orderTasksWithChildren } from './task-hierarchy'
import TaskRow from './TaskRow'

interface TaskListProps {
  tasks: Task[]
  selectedNumber: number | null
  tier: Tier
  users: UserRef[]
  rowErrors: Record<number, string>
  // Grouping only makes sense under the default (creation-order) sort — any
  // other sort interleaves statuses, so a status-keyed heading would lie
  // about what's actually under it. The caller passes `false` whenever a
  // non-default sort is active and this degrades to one flat list.
  grouped: boolean
  taskHref?: (task: Task) => string
  onPatch: (task: Task, patch: TaskPatchBody, optimistic: Partial<Task>) => void
  onArchive: (task: Task) => void
  onRestore: (task: Task) => void
}

/** The task collection: either one flat list, or — when `grouped` — one
 * section per status (in TASK_STATUSES order), each with an `<h2>` heading
 * reading "状态 数量" and a `role="group"` wired to it via
 * `aria-labelledby`, so the count is always the real per-group size, not
 * the list's total. Every row is `TaskRow`, which owns its own
 * `role="listitem"` and `aria-current` — this component only decides which
 * rows exist and how they're grouped. */
export default function TaskList({
  tasks,
  selectedNumber,
  tier,
  users,
  rowErrors,
  grouped,
  taskHref = (task) => `/tasks/${task.number}`,
  onPatch,
  onArchive,
  onRestore,
}: TaskListProps) {
  function renderRow(task: Task) {
    return (
      <TaskRow
        key={task.id}
        task={task}
        selected={task.number === selectedNumber}
        tier={tier}
        users={users}
        error={rowErrors[task.number]}
        href={taskHref(task)}
        onPatch={onPatch}
        onArchive={onArchive}
        onRestore={onRestore}
      />
    )
  }

  if (!grouped) {
    return (
      <div role="list">
        {orderTasksWithChildren(tasks).map(renderRow)}
      </div>
    )
  }

  const groups = TASK_STATUSES.map((status) => ({
    status,
    tasks: orderTasksWithChildren(tasks.filter((t) => t.status === status)),
  })).filter((group) => group.tasks.length > 0)

  return (
    <div role="list">
      {groups.map(({ status, tasks: groupTasks }) => {
        const headingId = `task-group-heading-${status}`
        return (
          <div key={status} role="group" aria-labelledby={headingId}>
            <h2 id={headingId} className="px-3 py-1.5 text-xs font-medium text-fg-muted">
              {STATUS_LABELS[status]} <span className="text-fg-subtle">{groupTasks.length}</span>
            </h2>
            {groupTasks.map(renderRow)}
          </div>
        )
      })}
    </div>
  )
}
