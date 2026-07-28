import { Link } from 'react-router-dom'
import type { Tier } from '@/hooks/useBreakpoint'
import { cn } from '@/lib/utils'
import type { Task, TaskPatchBody, UserRef } from '@/task-types'
import AssigneeControl from './controls/AssigneeControl'
import DueDateControl from './controls/DueDateControl'
import PriorityControl from './controls/PriorityControl'
import StatusControl from './controls/StatusControl'

interface BoardCardProps {
  task: Task
  tier: Tier
  users: UserRef[]
  error?: string
  onPatch: (task: Task, patch: TaskPatchBody, optimistic: Partial<Task>) => void
}

/** One card: a peer of TaskRow, not a lesser view of it — the same four
 * permanently-visible property controls (status/priority/assignee/due
 * date), just stacked instead of laid out in a row. The column a card sits
 * in already implies its status, but the control stays on the card anyway
 * (rather than the old QuietSelect-only "移动" trigger) so status can be
 * changed without a drag, with the same `任务 #<n> 状态` accessible name the
 * list rows use.
 *
 * `draggable` is the only thing that varies by tier: phones are read-only
 * (see TaskBoardPage), every other tier is a native HTML5 drag source. */
export default function BoardCard({ task, tier, users, error, onPatch }: BoardCardProps) {
  const draggable = tier !== 'phone'

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

  return (
    <article
      draggable={draggable}
      onDragStart={
        draggable
          ? (e) => {
              e.dataTransfer.setData('text/plain', String(task.number))
              e.dataTransfer.effectAllowed = 'move'
            }
          : undefined
      }
      className={cn(
        'flex flex-col gap-2 rounded-md border border-border bg-surface p-2.5 text-sm shadow-sm',
        draggable && 'cursor-grab active:cursor-grabbing',
      )}
    >
      <div className="flex min-w-0 items-baseline gap-1.5">
        <span className="shrink-0 font-mono text-xs text-fg-muted">#{task.number}</span>
        <Link to={`/tasks/${task.number}`} className="min-w-0 truncate font-medium text-fg hover:underline">
          {task.title}
        </Link>
      </div>
      {task.labels.length > 0 && (
        <div className="flex flex-wrap items-center gap-1">
          {task.labels.map((l) => (
            <span key={l.ID} className="rounded-full bg-surface-subtle px-2 py-0.5 text-[11px] text-fg-muted">
              {l.Name}
            </span>
          ))}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1.5">
        <StatusControl value={task.status} onChange={handleChangeStatus} ariaLabel={`任务 #${task.number} 状态`} />
        <PriorityControl value={task.priority} onChange={handleChangePriority} ariaLabel={`任务 #${task.number} 优先级`} />
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        <AssigneeControl
          value={task.assignee?.id ?? null}
          users={users}
          onChange={handleChangeAssignee}
          ariaLabel={`任务 #${task.number} 负责人`}
        />
        <DueDateControl value={task.due_date} onChange={handleChangeDueDate} ariaLabel={`任务 #${task.number} 截止日期`} />
      </div>
      {error && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
    </article>
  )
}
