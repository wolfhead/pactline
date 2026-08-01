import { useEffect, useRef, useState } from 'react'
import { Link2, XIcon } from 'lucide-react'
import AcceptanceChecklist from '@/components/projects/AcceptanceChecklist'
import ActivityLog from './ActivityLog'
import AttachmentSection from './AttachmentSection'
import CommentSection from './CommentSection'
import InlineEditable from './InlineEditable'
import AssigneeControl from './controls/AssigneeControl'
import DueDateControl from './controls/DueDateControl'
import LabelControl from './controls/LabelControl'
import PriorityControl from './controls/PriorityControl'
import StatusControl from './controls/StatusControl'
import ProjectControl from './controls/ProjectControl'
import TaskRelations from './TaskRelations'
import { archiveTask, getTask, listLabels, restoreTask, updateTask } from '@/api/tasks'
import {
  checkCriterion,
  createTaskCriterion,
  listTaskCriteria,
  removeCriterion,
  updateCriterion,
  type AcceptanceCriterion,
  type AcceptanceOutcome,
} from '@/api/acceptance'
import { ProblemError } from '@/api/v1/client'
import { useIdentity } from '@/identity'
import type { Label, Task, TaskPatchBody, UserRef } from '@/task-types'

const UNDO_WINDOW_MS = 6000

interface TaskDetailProps {
  number: number
  users: UserRef[]
  // The caller's own copy of this task, when it has one. This is the other
  // half of `onPatched`: the seam has to run both ways or the two views
  // diverge while the inspector is open.
  // Optional and nullable: a task the caller doesn't hold (filtered out, or
  // on a later page) simply isn't synced, and TaskDetail still owns its own
  // fetch for everything else — comments, activity, the initial load.
  syncedTask?: Task | null
  // Lets the list page fold a server-confirmed change
  // into its own copy of the task without a full reload.
  onPatched: (task: Task) => void
  // Only the inspector shell owns navigation and therefore the close action.
  onClose?: () => void
}

/**
 * Detail content for one task — no dialog, page chrome, or back link of its
 * own. The task brief is edited exactly where it is read (see
 * InlineEditable). Every property commits the instant it changes —
 * optimistic update, then reconciled against whatever the server actually
 * persisted, reverting visibly with a reason if it refuses. Archiving does
 * not ask first; it offers an undo instead.
 */
export default function TaskDetail({
  number,
  users,
  syncedTask,
  onPatched,
  onClose,
}: TaskDetailProps) {
  const { me } = useIdentity()

  const [task, setTask] = useState<Task | null>(null)
  const [error, setError] = useState('')
  const [fieldError, setFieldError] = useState('')
  const [allLabels, setAllLabels] = useState<Label[]>([])
  const [acceptanceCriteria, setAcceptanceCriteria] = useState<AcceptanceCriterion[]>([])
  const [reloadToken, setReloadToken] = useState(0)
  const [undoMessage, setUndoMessage] = useState<string | null>(null)
  const undoTimerRef = useRef<number | null>(null)
  const undoActionRef = useRef<(() => void) | null>(null)

  // Which task this component is currently *for*. The load effect's
  // `cancelled` flag only guards its own fetch; every other request here is
  // fired from an event handler, which has no cleanup to flip. `number`
  // changes in place when another task opens, so an in-flight
  // updateTask/archiveTask/restoreTask for the
  // previously shown task can still resolve after the new one has loaded
  // and rendered. Writing that response into `task` would leave A's title,
  // status and comments sitting under the URL /tasks/B with nothing left to
  // correct it. Hence a ref holding the live `number`, read at resolve time
  // and compared against the number captured when the call was made.
  const activeNumberRef = useRef(number)
  activeNumberRef.current = number

  function isStale(forNumber: number) {
    return activeNumberRef.current !== forNumber
  }

  function clearUndo() {
    if (undoTimerRef.current) {
      window.clearTimeout(undoTimerRef.current)
      undoTimerRef.current = null
    }
    undoActionRef.current = null
    setUndoMessage(null)
  }

  // Fetches whenever `number` changes, a reload is requested, or identity
  // changes. Clicking another task updates `number` without remounting the
  // inspector, so this effect replaces the loaded task. `me?.id` is a
  // dependency for the same reason —
  // switching identity has to replace the data, and a slow stale response
  // must never overwrite a newer one.
  useEffect(() => {
    if (!Number.isFinite(number)) return
    setTask(null)
    setAcceptanceCriteria([])
    setError('')
    // Everything else on screen belongs to the task being replaced, not the
    // one arriving. A failed-update message raised against A would otherwise
    // read as B's failure; worse, A's archive undo toast would stay up over B
    // with `undoActionRef` still closed over A, so pressing undo would silently
    // restore a task the user is no longer looking at.
    setFieldError('')
    clearUndo()
    let cancelled = false
    Promise.all([getTask(number), listTaskCriteria(number)])
      .then(([loaded, criteria]) => {
        if (cancelled) return
        setTask(loaded)
        setAcceptanceCriteria(criteria)
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

  // The reverse direction of `onPatched`. When the caller hands down a newer
  // copy of the task currently shown — because its own row control committed
  // a change, or it folded in a patch of ours — adopt it. Guarded on the
  // number so a caller that is momentarily one render behind (still holding
  // the previously selected task) can never overwrite the task just loaded
  // here. This only reacts to the caller's object identity changing, so it
  // does not fight the optimistic value set locally by patchOptimistic.
  useEffect(() => {
    if (!syncedTask || syncedTask.number !== number) return
    setTask((current) => (current && current.number === syncedTask.number ? syncedTask : current))
  }, [syncedTask, number])

  function patchOptimistic(patch: TaskPatchBody, optimistic: Partial<Task>) {
    if (!task) return
    const current = task
    const previous: Partial<Task> = {}
    for (const key of Object.keys(optimistic) as (keyof Task)[]) {
      previous[key] = current[key] as never
    }
    const forNumber = current.number
    setTask({ ...current, ...optimistic })
    setFieldError('')
    updateTask(forNumber, current.version, patch)
      .then((updated) => {
        // Still the task on screen? See activeNumberRef above. onPatched is
        // called either way — the list wants this change regardless of what
        // the detail is showing now.
        if (!isStale(forNumber)) setTask(updated)
        onPatched(updated)
      })
      .catch(async (err) => {
        if (isStale(forNumber)) return
        if (err instanceof ProblemError && err.code === 'VERSION_CONFLICT') {
          try {
            const latest = await getTask(forNumber)
            if (isStale(forNumber)) return
            setTask(latest)
            onPatched(latest)
            setFieldError('内容已被其他用户或 Agent 更新，已加载最新版本。')
            return
          } catch {
            // Fall through to the ordinary rollback when refresh also fails.
          }
        }
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
    archiveTask(taskNumber, task.version)
      .then((updated) => {
        onPatched(updated)
        if (isStale(taskNumber)) return
        setTask(updated)
        showUndo('已归档任务。', () => {
          restoreTask(taskNumber, updated.version)
            .then((restored) => {
              onPatched(restored)
              if (isStale(taskNumber)) return
              setTask(restored)
            })
            .catch((err) => {
              if (isStale(taskNumber)) return
              setFieldError(`撤销失败：${(err as Error).message}`)
            })
        })
      })
      .catch((err) => {
        if (isStale(taskNumber)) return
        setFieldError(`归档失败：${(err as Error).message}`)
      })
  }

  function handleRestore() {
    if (!task) return
    const taskNumber = task.number
    restoreTask(taskNumber, task.version)
      .then((updated) => {
        onPatched(updated)
        if (isStale(taskNumber)) return
        setTask(updated)
      })
      .catch((err) => {
        if (isStale(taskNumber)) return
        setFieldError(`恢复失败：${(err as Error).message}`)
      })
  }

  function toggleLabels(nextIds: string[]) {
    if (!task) return
    const nextLabels = allLabels.filter((l) => nextIds.includes(l.id))
    patchOptimistic({ label_ids: nextIds }, { labels: nextLabels })
  }

  async function reloadTaskAndAcceptance(forNumber: number) {
    const [latest, criteria] = await Promise.all([
      getTask(forNumber),
      listTaskCriteria(forNumber),
    ])
    if (!isStale(forNumber)) {
      setTask(latest)
      onPatched(latest)
      setAcceptanceCriteria(criteria)
      setFieldError('')
    }
  }

  async function addAcceptanceCriterion(criterion: string, instructions: string) {
    if (!task) return
    const forNumber = number
    await createTaskCriterion(forNumber, task.version, {
      criterion,
      verification_instructions: instructions,
      position: acceptanceCriteria.length,
    })
    await reloadTaskAndAcceptance(forNumber)
  }

  async function recordAcceptanceCheck(
    criterion: AcceptanceCriterion,
    outcome: AcceptanceOutcome,
    evidence: string,
  ) {
    const forNumber = number
    await checkCriterion(
      criterion.id, criterion.version, criterion.revision, outcome, evidence,
    )
    await reloadTaskAndAcceptance(forNumber)
  }

  async function editAcceptanceCriterion(
    criterion: AcceptanceCriterion,
    text: string,
    instructions: string,
  ) {
    const forNumber = number
    await updateCriterion(criterion.id, criterion.version, {
      criterion: text,
      verification_instructions: instructions,
    })
    await reloadTaskAndAcceptance(forNumber)
  }

  async function removeAcceptanceCriterion(criterion: AcceptanceCriterion) {
    const forNumber = number
    await removeCriterion(criterion.id, criterion.version)
    await reloadTaskAndAcceptance(forNumber)
  }

  async function finishAgentReview() {
    if (!task) return
    const forNumber = task.number
    const updated = await updateTask(forNumber, task.version, { status: 'done' })
    if (!isStale(forNumber)) setTask(updated)
    onPatched(updated)
  }

  async function returnAgentSubmissionForChanges() {
    if (!task) return
    const forNumber = task.number
    const updated = await updateTask(forNumber, task.version, { status: 'todo' })
    if (!isStale(forNumber)) setTask(updated)
    onPatched(updated)
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
            data-read-only-allowed="true"
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
              clearUndo()
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

      {task.blocked && (
        <div
          role="status"
          className="flex items-start gap-2 rounded-md bg-status-in-progress/10 px-3 py-2 text-sm text-status-in-progress"
        >
          <Link2 className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>
            等待 {task.dependencies.filter((dependency) => (
              !['done', 'cancelled'].includes(dependency.status)
            )).length} 个前置任务完成，当前不能标记为完成。
          </span>
        </div>
      )}

      {fieldError && <p role="alert" className="text-sm text-danger">{fieldError}</p>}

      <div
        data-task-properties
        className="grid grid-cols-[5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-2 [&_[data-property-control]]:text-sm [&_[data-slot=select-trigger]]:justify-start"
      >
        <div className="contents">
          <span className="text-sm text-fg-muted">状态</span>
          <StatusControl
            value={task.status}
            onChange={(status) => patchOptimistic({ status }, { status })}
            ariaLabel="状态"
          />
        </div>
        <div className="contents">
          <span className="text-sm text-fg-muted">优先级</span>
          <PriorityControl
            value={task.priority}
            onChange={(priority) => patchOptimistic({ priority }, { priority })}
            ariaLabel="优先级"
          />
        </div>
        <div className="contents">
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
        <div className="contents">
          <span className="text-sm text-fg-muted">执行方式</span>
          <select
            data-property-control
            aria-label="执行方式"
            value={task.execution_mode ?? 'human_only'}
            onChange={(event) => {
              const executionMode = event.target.value as 'human_only' | 'agent_allowed'
              patchOptimistic({ execution_mode: executionMode }, { execution_mode: executionMode })
            }}
            className="h-8 rounded-md border border-transparent bg-transparent px-2 text-sm text-fg hover:bg-surface-subtle focus:border-accent focus:outline-none"
          >
            <option value="human_only">仅人工执行</option>
            <option value="agent_allowed">允许 Agent 执行</option>
          </select>
        </div>
        <div className="contents">
          <span className="text-sm text-fg-muted">开始日期</span>
          <DueDateControl
            value={task.start_date ?? null}
            onChange={(start) => patchOptimistic({ start_date: start }, { start_date: start })}
            ariaLabel="开始日期"
            emptyLabel="无开始"
            pickerLabel="选择开始日期"
          />
        </div>
        <div className="contents">
          <span className="text-sm text-fg-muted">截止日期</span>
          <DueDateControl
            value={task.due_date}
            onChange={(due) => patchOptimistic({ due_date: due }, { due_date: due })}
            ariaLabel="截止日期"
          />
        </div>
        <div className="contents">
          <span className="text-sm text-fg-muted">标签</span>
          <LabelControl value={task.labels} all={allLabels} onChange={toggleLabels} ariaLabel="标签" />
        </div>
        <ProjectControl
          project={task.project}
          milestone={task.milestone ?? null}
          onProjectChange={(project) => {
            patchOptimistic(
              { project_number: project.number },
              { project, milestone: null },
            )
          }}
          onMilestoneChange={(milestone) => {
            patchOptimistic({ milestone_id: milestone?.id ?? null }, { milestone })
          }}
        />
      </div>

      <TaskRelations
        task={{
          ...task,
          children: task.children ?? [],
          dependencies: task.dependencies ?? [],
          dependents: task.dependents ?? [],
        }}
        onPatch={(patch) => patchOptimistic(patch, {})}
      />

      <section className="flex flex-col gap-3 border-t border-border pt-4">
        <div>
          <h3 className="text-xs font-medium text-fg-muted">背景 / 问题</h3>
          <InlineEditable
            value={task.context}
            onCommit={(next) => patchOptimistic({ context: next }, { context: next })}
            multiline
            placeholder="补充为什么需要做，以及当前遇到的问题…"
            ariaLabel="任务背景"
            className="mt-1 text-sm text-fg"
          />
        </div>
        <div>
          <h3 className="text-xs font-medium text-fg-muted">期望结果</h3>
          <InlineEditable
            value={task.expected_result}
            onCommit={(next) => patchOptimistic({ expected_result: next }, { expected_result: next })}
            multiline
            placeholder="补充完成后应该达到的状态…"
            ariaLabel="任务期望结果"
            className="mt-1 text-sm text-fg"
          />
        </div>
        <div>
          <h3 className="text-xs font-medium text-fg-muted">补充说明</h3>
          <InlineEditable
            value={task.description}
            onCommit={(next) => patchOptimistic({ description: next }, { description: next })}
            multiline
            placeholder="可选：补充约束、参考资料或实现提示…"
            ariaLabel="任务补充说明"
            className="mt-1 text-sm text-fg"
          />
        </div>
      </section>

      <AcceptanceChecklist
        title="验收标准"
        criteria={acceptanceCriteria}
        onAdd={addAcceptanceCriterion}
        onCheck={recordAcceptanceCheck}
        onUpdate={editAcceptanceCriterion}
        onRemove={removeAcceptanceCriterion}
      />

      <p className="text-xs text-fg-muted">
        创建者：{task.creator.name} · 创建于 {new Date(task.created_at).toLocaleString()}
      </p>

      <AttachmentSection
        taskNumber={task.number}
        taskVersion={task.version}
        readOnly={Boolean(task.archived_at)}
        onTaskChanged={() => reloadTaskAndAcceptance(task.number)}
      />

      <CommentSection
        taskNumber={task.number}
        projectNumber={task.project.number}
        taskVersion={task.version}
        taskStatus={task.status}
        acceptanceCriteria={acceptanceCriteria}
        onReviewCheck={recordAcceptanceCheck}
        onCompleteReview={finishAgentReview}
        onReturnForChanges={returnAgentSubmissionForChanges}
        onTaskChanged={() => reloadTaskAndAcceptance(task.number)}
      />
      <ActivityLog task={task} />
    </div>
  )
}
