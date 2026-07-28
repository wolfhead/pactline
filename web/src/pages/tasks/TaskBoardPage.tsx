import { useEffect, useState } from 'react'
import BoardColumn from '@/components/tasks/BoardColumn'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { listTasks, updateTask } from '@/api/tasks'
import { useIdentity } from '@/identity'
import { TASK_STATUSES, type Task, type TaskPatchBody, type TaskStatus } from '@/task-types'

const BOARD_LIMIT = 200

/**
 * A column per status; dragging a card between columns changes its status.
 * Drag-and-drop uses only the native HTML5 drag events (dragstart on the
 * card, dragover/drop on the column) — no library. Unlike the previous
 * board, the card itself carries a real, permanently-visible StatusControl
 * (see BoardCard) — the same accessible-name convention as the list rows —
 * so a status change never requires a drag at all, on any tier.
 *
 * A phone renders the same six columns read-only and horizontally
 * scrollable: touch drag is outside the "view and lightly edit" scope set
 * for phones, so cards there are not draggable and the column drop targets
 * are not wired up (see BoardColumn).
 */
export default function TaskBoardPage() {
  const { me, users } = useIdentity()
  const tier = useBreakpoint()
  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [reloadToken, setReloadToken] = useState(0)
  const [rowErrors, setRowErrors] = useState<Record<number, string>>({})

  // Fetches on mount, on reload, and whenever identity changes — mirrors
  // the cancelled-flag idiom in identity.tsx / TaskListPage.tsx.
  useEffect(() => {
    setLoading(true)
    setError('')
    let cancelled = false
    listTasks({ limit: BOARD_LIMIT, sort: 'number', order: 'asc' })
      .then((res) => {
        if (cancelled) return
        setTasks(res.items)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
      .finally(() => {
        if (cancelled) return
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [me?.id, reloadToken])

  // Same optimistic-then-reconcile pattern as the list page: the change
  // (a status move from a drop, or any property edited on the card itself)
  // applies immediately; a server refusal snaps it back and says why.
  function patchOptimistic(task: Task, patch: TaskPatchBody, optimistic: Partial<Task>) {
    const previous: Partial<Task> = {}
    for (const key of Object.keys(optimistic) as (keyof Task)[]) {
      previous[key] = task[key] as never
    }
    setTasks((ts) => ts.map((t) => (t.number === task.number ? { ...t, ...optimistic } : t)))
    setRowErrors((e) => ({ ...e, [task.number]: '' }))
    updateTask(task.number, patch)
      .then((updated) => {
        setTasks((ts) => ts.map((t) => (t.number === task.number ? updated : t)))
      })
      .catch((err) => {
        setTasks((ts) => ts.map((t) => (t.number === task.number ? { ...t, ...previous } : t)))
        setRowErrors((e) => ({ ...e, [task.number]: `更新失败：${(err as Error).message}，已恢复原状态` }))
      })
  }

  function handleDropTask(taskNumber: number, status: TaskStatus) {
    const task = tasks.find((t) => t.number === taskNumber)
    if (!task || task.status === status) return
    patchOptimistic(task, { status }, { status })
  }

  if (loading) return <p className="p-3 text-sm text-fg-muted">正在加载看板…</p>
  if (error) {
    return (
      <p className="p-3 text-sm text-danger">
        加载失败：{error}{' '}
        <button
          type="button"
          className="border-0 bg-transparent p-0 text-accent underline"
          onClick={() => setReloadToken((t) => t + 1)}
        >
          重试
        </button>
      </p>
    )
  }

  return (
    <div role="region" aria-label="任务看板" data-task-board className="flex h-full min-h-0 gap-3 overflow-x-auto p-3">
      {TASK_STATUSES.map((status) => (
        <BoardColumn
          key={status}
          status={status}
          tasks={tasks.filter((t) => t.status === status)}
          tier={tier}
          users={users}
          rowErrors={rowErrors}
          onDropTask={handleDropTask}
          onPatch={patchOptimistic}
        />
      ))}
    </div>
  )
}
