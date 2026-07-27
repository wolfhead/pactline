import { useEffect, useRef, useState } from 'react'
import { XIcon } from 'lucide-react'
import ActivityLog from './ActivityLog'
import CommentSection from './CommentSection'
import InlineEditable from './InlineEditable'
import AssigneeControl from './controls/AssigneeControl'
import DueDateControl from './controls/DueDateControl'
import LabelControl from './controls/LabelControl'
import PriorityControl from './controls/PriorityControl'
import StatusControl from './controls/StatusControl'
import { archiveTask, getTask, listLabels, restoreTask, updateTask } from '@/api/tasks'
import { useIdentity } from '@/identity'
import type { Label, Task, TaskPatchBody, UserRef } from '@/task-types'

const UNDO_WINDOW_MS = 6000

interface TaskDetailProps {
  number: number
  users: UserRef[]
  // Lets the caller (list column, board) fold a server-confirmed change
  // into its own copy of the task without a full reload.
  onPatched: (task: Task) => void
  // Only the shell knows whether it needs a close affordance at all — a
  // three-column layout at xl never passes this, a Sheet or a phone page
  // always does.
  onClose?: () => void
}

/**
 * Detail *content* for one task — no dialog, no page chrome, no back link
 * of its own. Task 9 places this inside a third column at xl, a slide-over
 * Sheet at lg/md, and a full page on a phone; this component has no idea
 * which. Title and description are edited exactly where they are read (see
 * InlineEditable). Every property commits the instant it changes —
 * optimistic update, then reconciled against whatever the server actually
 * persisted, reverting visibly with a reason if it refuses. Archiving does
 * not ask first; it offers an undo instead.
 */
export default function TaskDetail({ number, users, onPatched, onClose }: TaskDetailProps) {
  const { me } = useIdentity()

  const [task, setTask] = useState<Task | null>(null)
  const [error, setError] = useState('')
  const [fieldError, setFieldError] = useState('')
  const [allLabels, setAllLabels] = useState<Label[]>([])
  const [reloadToken, setReloadToken] = useState(0)
  const [undoMessage, setUndoMessage] = useState<string | null>(null)
  const undoTimerRef = useRef<number | null>(null)
  const undoActionRef = useRef<(() => void) | null>(null)

  // Fetches whenever `number` changes, a reload is requested, or identity
  // changes — mirrors the cancelled-flag idiom in identity.tsx /
  // WorkFeed.tsx / Board.tsx / BountyDetail.tsx. `number` changing without a
  // remount is the normal case at xl: clicking another row in the list
  // column changes this prop in place, it never unmounts/remounts
  // TaskDetail, so this effect (not initial-mount logic) is what has to
  // replace the loaded task. `me?.id` is a dependency for the same reason —
  // switching identity has to replace the data, and a slow stale response
  // must never overwrite a newer one.
  useEffect(() => {
    if (!Number.isFinite(number)) return
    setTask(null)
    setError('')
    let cancelled = false
    getTask(number)
      .then((loaded) => {
        if (cancelled) return
        setTask(loaded)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
    return () => {
      cancelled = true
    }
  }, [number, me?.id, reloadToken])

  useEffect(() => {
    let cancelled = false
    listLabels()
      .then((loaded) => {
        if (cancelled) return
        setAllLabels(loaded)
      })
      .catch(() => {
        // Non-fatal: the label picker just stays empty; the task itself
        // still loaded and is fully usable.
      })
    return () => {
      cancelled = true
    }
  }, [me?.id])

  useEffect(() => {
    return () => {
      if (undoTimerRef.current) window.clearTimeout(undoTimerRef.current)
    }
  }, [])

  function patchOptimistic(patch: TaskPatchBody, optimistic: Partial<Task>) {
    if (!task) return
    const current = task
    const previous: Partial<Task> = {}
    for (const key of Object.keys(optimistic) as (keyof Task)[]) {
      previous[key] = current[key] as never
    }
    setTask({ ...current, ...optimistic })
    setFieldError('')
    updateTask(current.number, patch)
      .then((updated) => {
        setTask(updated)
        onPatched(updated)
      })
      .catch((err) => {
        setTask((t) => (t ? { ...t, ...previous } : t))
        setFieldError(`更新失败：${(err as Error).message}，已恢复原状态`)
      })
  }

  function showUndo(text: string, action: () => void) {
    if (undoTimerRef.current) window.clearTimeout(undoTimerRef.current)
    undoActionRef.current = action
    setUndoMessage(text)
    undoTimerRef.current = window.setTimeout(() => {
      setUndoMessage(null)
      undoActionRef.current = null
    }, UNDO_WINDOW_MS)
  }

  function handleArchive() {
    if (!task) return
    const taskNumber = task.number
    archiveTask(taskNumber)
      .then((updated) => {
        setTask(updated)
        onPatched(updated)
        showUndo('已归档任务。', () => {
          restoreTask(taskNumber)
            .then((restored) => {
              setTask(restored)
              onPatched(restored)
            })
            .catch((err) => setFieldError(`撤销失败：${(err as Error).message}`))
        })
      })
      .catch((err) => setFieldError(`归档失败：${(err as Error).message}`))
  }

  function handleRestore() {
    if (!task) return
    restoreTask(task.number)
      .then((updated) => {
        setTask(updated)
        onPatched(updated)
      })
      .catch((err) => setFieldError(`恢复失败：${(err as Error).message}`))
  }

  function toggleLabels(nextIds: string[]) {
    if (!task) return
    const nextLabels = allLabels.filter((l) => nextIds.includes(l.ID))
    patchOptimistic({ label_ids: nextIds }, { labels: nextLabels })
  }

  if (error) {
    return (
      <p className="p-4 text-sm text-danger">
        加载失败：{error}{' '}
        <button type="button" className="underline" onClick={() => setReloadToken((t) => t + 1)}>
          重试
        </button>
      </p>
    )
  }
  if (!task) return <p className="p-4 text-sm text-fg-muted">正在加载任务…</p>

  return (
    <div className="flex flex-col gap-4 p-4">
      {/* A screen-reader-only heading: the shell wrapping this content (a
          third column, a Sheet, a full page) may or may not supply its own
          accessible name, and TaskDetail can't assume which — this gives
          the pane one regardless. The visible, interactive title lives in
          the InlineEditable input below; this is not a second, competing
          display of it. */}
      <h2 className="sr-only">{task.title}</h2>
      <div className="flex items-center justify-between gap-2">
        {task.archived_at ? (
          <button
            type="button"
            className="text-xs text-fg-muted hover:text-fg"
            onClick={handleRestore}
          >
            恢复
          </button>
        ) : (
          <button
            type="button"
            className="text-xs text-fg-muted hover:text-fg"
            onClick={handleArchive}
          >
            归档
          </button>
        )}
        {onClose && (
          <button
            type="button"
            aria-label="关闭"
            onClick={onClose}
            className="flex size-7 items-center justify-center rounded-md text-fg-muted hover:bg-surface-subtle hover:text-fg"
          >
            <XIcon className="size-4" aria-hidden="true" />
          </button>
        )}
      </div>

      {undoMessage && (
        <div
          role="status"
          className="flex items-center justify-between gap-3 rounded-md border border-border bg-surface-subtle px-3 py-2 text-sm"
        >
          <span>{undoMessage}</span>
          <button
            type="button"
            className="font-medium text-accent"
            onClick={() => {
              undoActionRef.current?.()
              if (undoTimerRef.current) window.clearTimeout(undoTimerRef.current)
              setUndoMessage(null)
            }}
          >
            撤销
          </button>
        </div>
      )}

      <div className="flex items-baseline gap-3">
        <span className="shrink-0 font-mono text-xs text-fg-muted">#{task.number}</span>
        <InlineEditable
          value={task.title}
          onCommit={(next) => patchOptimistic({ title: next }, { title: next })}
          ariaLabel="任务标题"
          className="flex-1 text-base font-semibold text-fg"
        />
      </div>

      {task.archived_at && <p className="text-sm text-fg-muted">此任务已归档。</p>}

      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm text-fg-muted">状态</span>
          <StatusControl
            value={task.status}
            onChange={(status) => patchOptimistic({ status }, { status })}
            ariaLabel="状态"
          />
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm text-fg-muted">优先级</span>
          <PriorityControl
            value={task.priority}
            onChange={(priority) => patchOptimistic({ priority }, { priority })}
            ariaLabel="优先级"
          />
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm text-fg-muted">负责人</span>
          <AssigneeControl
            value={task.assignee?.id ?? null}
            users={users}
            onChange={(id) => {
              const assignee = id ? (users.find((u) => u.id === id) ?? null) : null
              patchOptimistic({ assignee_id: id }, { assignee })
            }}
            ariaLabel="负责人"
          />
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm text-fg-muted">截止日期</span>
          <DueDateControl
            value={task.due_date}
            onChange={(due) => patchOptimistic({ due_date: due }, { due_date: due })}
            ariaLabel="截止日期"
          />
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm text-fg-muted">标签</span>
          <LabelControl value={task.labels} all={allLabels} onChange={toggleLabels} ariaLabel="标签" />
        </div>
      </div>

      <InlineEditable
        value={task.description}
        onCommit={(next) => patchOptimistic({ description: next }, { description: next })}
        multiline
        placeholder="添加描述…"
        ariaLabel="任务描述"
        className="text-sm text-fg"
      />

      {fieldError && <p className="text-sm text-danger">{fieldError}</p>}

      <p className="text-xs text-fg-muted">
        创建者：{task.creator.name} · 创建于 {new Date(task.created_at).toLocaleString()}
      </p>

      <CommentSection taskNumber={task.number} />
      <ActivityLog task={task} />
    </div>
  )
}
