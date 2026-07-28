import type { DragEvent } from 'react'
import { useState } from 'react'
import type { Tier } from '@/hooks/useBreakpoint'
import { cn } from '@/lib/utils'
import { STATUS_LABELS, type Task, type TaskPatchBody, type TaskStatus, type UserRef } from '@/task-types'
import BoardCard from './BoardCard'

interface BoardColumnProps {
  status: TaskStatus
  tasks: Task[]
  tier: Tier
  users: UserRef[]
  rowErrors?: Record<number, string>
  onDropTask: (taskNumber: number, status: TaskStatus) => void
  onPatch: (task: Task, patch: TaskPatchBody, optimistic: Partial<Task>) => void
}

/** One status lane. `role="group"` + `aria-labelledby` pointing at its own
 * `<h2>` — the same pairing TaskList's grouped headings use — so
 * `getByRole('group', { name: /进行中/ })` finds exactly this column and
 * `within(...)` scopes further queries to only the cards actually inside
 * it. All six statuses render a column, empty ones included: a status with
 * zero cards is still a valid drop target, not something that should
 * disappear.
 *
 * Drag handlers are only wired up on non-phone tiers — a phone board is
 * read-only (see TaskBoardPage), so there's nothing to accept a drop. */
export default function BoardColumn({ status, tasks, tier, users, rowErrors, onDropTask, onPatch }: BoardColumnProps) {
  const [dragOver, setDragOver] = useState(false)
  const isPhone = tier === 'phone'
  const headingId = `board-column-heading-${status}`

  function handleDragOver(e: DragEvent<HTMLDivElement>) {
    e.preventDefault()
    if (!dragOver) setDragOver(true)
  }

  function handleDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault()
    setDragOver(false)
    const number = Number(e.dataTransfer.getData('text/plain'))
    if (Number.isFinite(number)) onDropTask(number, status)
  }

  return (
    <div
      role="group"
      aria-labelledby={headingId}
      className={cn(
        'flex h-full w-72 shrink-0 flex-col gap-2 rounded-lg border border-border bg-surface-subtle/40 p-2',
        dragOver && 'border-accent bg-accent-subtle',
      )}
      onDragOver={isPhone ? undefined : handleDragOver}
      onDragLeave={isPhone ? undefined : () => setDragOver(false)}
      onDrop={isPhone ? undefined : handleDrop}
    >
      <h2 id={headingId} className="shrink-0 px-1 text-xs font-medium text-fg-muted">
        {STATUS_LABELS[status]} <span className="text-fg-subtle">{tasks.length}</span>
      </h2>
      <div className="flex min-h-24 flex-1 flex-col gap-2 overflow-y-auto">
        {tasks.length === 0 && <p className="px-1 text-xs text-fg-subtle">暂无任务</p>}
        {tasks.map((task) => (
          <BoardCard key={task.id} task={task} tier={tier} users={users} error={rowErrors?.[task.number]} onPatch={onPatch} />
        ))}
      </div>
    </div>
  )
}
