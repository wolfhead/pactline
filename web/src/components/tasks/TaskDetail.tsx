import { useEffect, useRef, useState } from 'react'
import { Link2, XIcon } from 'lucide-react'
import MarkdownEditableField from '@/components/markdown/MarkdownEditableField'
import AcceptanceChecklist from '@/components/projects/AcceptanceChecklist'
import ActivityLog from './ActivityLog'
import AttachmentSection from './AttachmentSection'
import InlineEditable from './InlineEditable'
import AssigneeControl from './controls/AssigneeControl'
import DueDateControl from './controls/DueDateControl'
import LabelControl from './controls/LabelControl'
import PriorityControl from './controls/PriorityControl'
import ProjectControl from './controls/ProjectControl'
import TaskRelations from './TaskRelations'
import TaskThreads from './TaskThreads'
import TaskWorkflowPanel from './TaskWorkflowPanel'
import TaskCodeChanges from './TaskCodeChanges'
import PhaseBadge from './PhaseBadge'
import { archiveTask, getTask, listLabels, restoreTask, updateTask } from '@/api/tasks'
import {
  checkTaskCriterionThroughClaim,
  createTaskCriterion,
  listTaskCriteria,
  removeCriterion,
  updateCriterion,
  type AcceptanceCriterion,
  type AcceptanceOutcome,
} from '@/api/acceptance'
import { listTaskStageClaims } from '@/api/task-workflow'
import { ProblemError } from '@/api/v1/client'
import { useIdentity } from '@/identity'
import type { Label, Task, TaskPatchBody, UserRef } from '@/task-types'

const UNDO_WINDOW_MS = 6000

interface TaskDetailProps {
  number: number
  users: UserRef[]
  // Standalone pages own the document's primary heading; embedded details
  // remain a subsection of their surrounding collection or overlay.
  headingLevel?: 1 | 2
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
  // The standalone route gives the same task state a page-scale information
  // architecture. The inspector keeps its compact, linear presentation.
  variant?: 'inspector' | 'page'
}

/**
 * Detail content for one task — no dialog, page chrome, or back link of its
 * own. Fields are edited exactly where they are read. Compact properties
 * commit immediately, while Markdown-rich brief fields use explicit save and
 * cancel controls. Optimistic updates are reconciled against whatever the
 * server actually persisted, reverting visibly with a reason if it refuses.
 * Archiving does not ask first; it offers an undo instead.
 */
export default function TaskDetail({
  number,
  users,
  syncedTask,
  onPatched,
  onClose,
  headingLevel = 2,
  variant = 'inspector',
}: TaskDetailProps) {
  const { me } = useIdentity()

  const [task, setTask] = useState<Task | null>(null)
  const [error, setError] = useState<Error | null>(null)
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
    setError(null)
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
        setError(err instanceof Error ? err : new Error(String(err)))
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
    const claims = await listTaskStageClaims(forNumber)
    const claim = claims.find((candidate) => candidate.status === 'active')
    if (!claim) throw new Error('请先领取当前阶段，再记录验证或验收结果。')
    await checkTaskCriterionThroughClaim(forNumber, task!.version, claim, criterion, outcome, evidence)
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

  if (error) {
    const notFound = error instanceof ProblemError && error.status === 404
    return (
      <section role="alert" className="p-5 text-sm text-danger">
        <h1 className="text-base font-semibold">{notFound ? '找不到任务' : '任务加载失败'}</h1>
        <p className="mt-1">
          {notFound ? '这个任务不存在，或你无权查看。' : error.message}
        </p>
        <button type="button" className="underline" onClick={() => setReloadToken((t) => t + 1)}>
          重试
        </button>
      </section>
    )
  }
  if (!task) return <p role="status" className="p-5 text-sm text-fg-muted">正在加载任务…</p>

  const TaskHeading = `h${headingLevel}` as const
  const actionControls = (
    <div className={variant === 'page'
      ? 'flex shrink-0 items-center justify-end gap-2'
      : 'flex w-full items-center justify-between gap-2'}>
      {task.archived_at ? (
        <button
          type="button"
          className="rounded-md border border-border-strong px-3 py-2 text-xs font-medium text-fg hover:bg-surface-subtle"
          onClick={handleRestore}
        >
          恢复
        </button>
      ) : (
        <button
          type="button"
          className="rounded-md px-3 py-2 text-xs font-medium text-fg-muted hover:bg-surface-subtle hover:text-fg"
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
          className="flex size-8 items-center justify-center rounded-md text-fg-muted hover:bg-surface-subtle hover:text-fg"
        >
          <XIcon className="size-4" aria-hidden="true" />
        </button>
      )}
    </div>
  )

  const feedback = (
    <>
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
      {task.archived_at && (
        <p role="status" className="rounded-md border border-border bg-surface-subtle px-3 py-2 text-sm text-fg-muted">
          此任务已归档，仍可查看完整内容。
        </p>
      )}
      {task.blocked && (
        <div
          role="status"
          className="flex items-start gap-2 rounded-md bg-status-in-progress/10 px-3 py-2 text-sm text-status-in-progress"
        >
          <Link2 className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>
            等待 {task.dependencies.filter((dependency) => (
              !['done', 'cancelled'].includes(dependency.phase)
            )).length} 个前置任务完成，当前不能标记为完成。
          </span>
        </div>
      )}
      {fieldError && <p role="alert" className="text-sm text-danger">{fieldError}</p>}
    </>
  )

  const properties = (
    <div
      data-task-properties
      className="grid min-w-0 grid-cols-[5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-2 [&_[data-property-control]]:h-auto [&_[data-property-control]]:min-h-8 [&_[data-property-control]]:min-w-0 [&_[data-property-control]]:max-w-full [&_[data-property-control]]:whitespace-normal [&_[data-property-control]]:text-left [&_[data-property-control]]:text-sm [&_[data-property-control]>span]:min-w-0 [&_[data-property-control]>span]:[overflow-wrap:anywhere] [&_[data-slot=select-trigger]]:justify-start [&_[data-slot=select-value]]:line-clamp-none [&_[data-slot=select-value]]:whitespace-normal [&_[data-slot=select-value]]:[overflow-wrap:anywhere]"
    >
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
        onMilestoneChange={(milestone) => {
          patchOptimistic({ milestone_id: milestone?.id ?? null }, { milestone })
        }}
      />
    </div>
  )

  const workflow = (
    <TaskWorkflowPanel
      task={task}
      onChanged={() => reloadTaskAndAcceptance(task.number)}
    />
  )
  const codeChanges = (
    <TaskCodeChanges
      task={task}
      onChanged={() => reloadTaskAndAcceptance(task.number)}
    />
  )
  const relations = (
    <TaskRelations
      task={{
        ...task,
        children: task.children ?? [],
        dependencies: task.dependencies ?? [],
        dependents: task.dependents ?? [],
      }}
      onPatch={(patch) => patchOptimistic(patch, {})}
    />
  )
  const brief = (
    <section data-task-brief aria-label="任务说明" className="flex min-w-0 flex-col gap-6">
      <MarkdownEditableField
        label="背景 / 问题"
        value={task.context}
        onCommit={(next) => patchOptimistic({ context: next }, { context: next })}
        placeholder="补充为什么需要做，以及当前遇到的问题…"
        required
      />
      <MarkdownEditableField
        label="期望结果"
        value={task.expected_result}
        onCommit={(next) => patchOptimistic({ expected_result: next }, { expected_result: next })}
        placeholder="补充完成后应该达到的状态…"
        required
      />
      <MarkdownEditableField
        label="补充说明"
        value={task.description}
        onCommit={(next) => patchOptimistic({ description: next }, { description: next })}
        placeholder="可选：补充约束、参考资料或实现提示…"
      />
    </section>
  )
  const acceptance = (
    <div data-task-acceptance>
      <AcceptanceChecklist
        title="验收标准"
        criteria={acceptanceCriteria}
        onAdd={addAcceptanceCriterion}
        onCheck={recordAcceptanceCheck}
        onUpdate={editAcceptanceCriterion}
        onRemove={removeAcceptanceCriterion}
      />
    </div>
  )
  const attachments = (
    <AttachmentSection
      taskNumber={task.number}
      taskVersion={task.version}
      readOnly={Boolean(task.archived_at)}
      onTaskChanged={() => reloadTaskAndAcceptance(task.number)}
    />
  )
  const threads = (
    <div data-task-thread>
      <TaskThreads
        taskNumber={task.number}
        projectNumber={task.project.number}
        taskVersion={task.version}
      />
    </div>
  )

  if (variant === 'page') {
    return (
      <article className="min-w-0 px-4 pb-12 pt-6 sm:px-6 lg:px-8">
        <header role="region" aria-label="任务页标题" className="min-w-0 border-b border-border pb-6">
          <TaskHeading className="sr-only">{task.title}</TaskHeading>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 text-xs text-fg-muted">
            <span className="font-mono">#{task.number}</span>
            <span>{task.project.name}</span>
            {task.milestone && <span>· {task.milestone.name}</span>}
          </div>
          <div className="mt-2 flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <InlineEditable
              value={task.title}
              onCommit={(next) => patchOptimistic({ title: next }, { title: next })}
              ariaLabel="任务标题"
              className="min-w-0 flex-1 text-2xl font-semibold leading-tight tracking-tight text-fg sm:text-3xl"
            />
            {actionControls}
          </div>
          <div className="mt-3 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
            <PhaseBadge phase={task.phase} activity={task.activity} />
            <p className="min-w-0 text-sm text-fg-muted">
              负责人：<span className="font-medium text-fg">{task.assignee?.name ?? '未分配'}</span>
            </p>
            <p className="text-xs text-fg-muted">
              创建者：{task.creator.name} · {new Date(task.created_at).toLocaleString()}
            </p>
          </div>
          <div className="mt-4 grid gap-2">{feedback}</div>
        </header>

        <div className="mt-8 grid min-w-0 gap-10 lg:grid-cols-[minmax(0,1fr)_19rem]">
          <section role="region" aria-label="任务正文" className="grid min-w-0 max-w-[75ch] content-start gap-8">
            {brief}
            <section aria-label="任务验收" className="min-w-0 border-t border-border pt-6">
              {acceptance}
            </section>
            <section aria-label="任务附件" className="min-w-0 border-t border-border pt-6">
              {attachments}
            </section>
            <section aria-label="任务讨论" className="min-w-0 border-t border-border pt-6">
              {threads}
            </section>
            <section aria-label="任务历史" className="min-w-0 border-t border-border pt-6">
              <ActivityLog task={task} />
            </section>
          </section>

          <aside
            data-task-sidebar
            aria-label="任务属性与交付"
            className="min-w-0 border-t border-border pt-6 lg:sticky lg:top-20 lg:self-start lg:border-l lg:border-t-0 lg:pl-6 lg:pt-0"
          >
            <div className="grid min-w-0 gap-6">
              <section aria-label="当前工作" className="min-w-0">
                {workflow}
              </section>
              <section aria-labelledby="task-properties-title" className="min-w-0 border-t border-border pt-5">
                <h2 id="task-properties-title" className="mb-3 text-sm font-semibold text-fg">属性</h2>
                {properties}
              </section>
              <section aria-label="代码交付" className="min-w-0">
                {codeChanges}
              </section>
              <section aria-label="任务关系" className="min-w-0 border-t border-border pt-5">
                {relations}
              </section>
            </div>
          </aside>
        </div>
      </article>
    )
  }

  return (
    <article className="flex min-w-0 flex-col gap-4 p-4 sm:p-5">
      {/* The inspector owns no visible heading; its editable title remains the
          visual label while this heading names the surrounding pane. */}
      <TaskHeading className="sr-only">{task.title}</TaskHeading>
      <div className="flex items-center justify-between gap-2">{actionControls}</div>
      {feedback}
      <div className="flex items-baseline gap-3">
        <span className="shrink-0 font-mono text-xs text-fg-muted">#{task.number}</span>
        <InlineEditable
          value={task.title}
          onCommit={(next) => patchOptimistic({ title: next }, { title: next })}
          ariaLabel="任务标题"
          className="flex-1 text-base font-semibold text-fg"
        />
      </div>
      {properties}
      {workflow}
      {codeChanges}
      {relations}
      <div className="border-t border-border pt-4">{brief}</div>
      {acceptance}
      <p className="text-xs text-fg-muted">
        创建者：{task.creator.name} · 创建于 {new Date(task.created_at).toLocaleString()}
      </p>
      {attachments}
      {threads}
      <ActivityLog task={task} />
    </article>
  )
}
