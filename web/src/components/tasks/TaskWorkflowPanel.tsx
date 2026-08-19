import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Check, Hand, Play, RotateCcw, Send, XCircle } from 'lucide-react'
import MarkdownComposer from '@/components/markdown/MarkdownComposer'
import {
  acceptTask,
  cancelTask,
  claimTaskStage,
  completeTaskExecution,
  listTaskStageClaims,
  listTaskThreads,
  markTaskReady,
  releaseTaskStage,
  recordTaskWorkSubmission,
  requestTaskChanges,
  requestTaskResolution,
  resolveTaskIssue,
  withdrawTaskReadiness,
} from '@/api/task-workflow'
import { useIdentity } from '@/identity'
import type { IssueThreadType, Task, TaskStageClaim, TaskThread } from '@/task-types'
import PhaseBadge from './PhaseBadge'

type PendingAction =
  | 'withdraw'
  | 'release'
  | 'record_work'
  | 'complete_execution'
  | 'request_changes'
  | 'request_resolution'
  | 'resolve_issue'
  | 'accept'
  | 'cancel'

const ACTION_LABELS: Record<PendingAction, string> = {
  withdraw: '退回待规划',
  release: '释放领取',
  record_work: '记录工作',
  complete_execution: '完成执行',
  request_changes: '退回修改',
  request_resolution: '请求解决',
  resolve_issue: '解决 Issue',
  accept: '接受并完成',
  cancel: '取消任务',
}

export default function TaskWorkflowPanel({
  task,
  onChanged,
}: {
  task: Task
  onChanged: () => Promise<void>
}) {
  const { me } = useIdentity()
  const [claims, setClaims] = useState<TaskStageClaim[]>([])
  const [threads, setThreads] = useState<TaskThread[]>([])
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [body, setBody] = useState('')
  const [issueType, setIssueType] = useState<IssueThreadType>('decision_required')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function reloadWorkflow() {
    const [nextClaims, nextThreads] = await Promise.all([
      listTaskStageClaims(task.number),
      listTaskThreads(task.number),
    ])
    setClaims(nextClaims)
    setThreads(nextThreads)
  }

  async function refreshAfterRejection() {
    try {
      await Promise.all([reloadWorkflow(), onChanged()])
    } catch {
      // Keep the command rejection visible; the next normal refresh retries state recovery.
    }
  }

  useEffect(() => {
    let cancelled = false
    Promise.all([listTaskStageClaims(task.number), listTaskThreads(task.number)])
      .then(([nextClaims, nextThreads]) => {
        if (cancelled) return
        setClaims(nextClaims)
        setThreads(nextThreads)
      })
      .catch((reason) => {
        if (!cancelled) setError(`加载工作流失败：${(reason as Error).message}`)
      })
    return () => { cancelled = true }
  }, [task.number, task.version])

  const activeClaim = useMemo(
    () => claims.find((claim) => claim.status === 'active') ?? null,
    [claims],
  )
  const ownsClaim = activeClaim?.claimed_by.type === 'user'
    && activeClaim.claimed_by.user_id === me?.id
  const activeIssue = threads.find((thread) => thread.issue_status === 'open') ?? null
  const terminal = task.phase === 'done' || task.phase === 'cancelled'

  async function runSimple(action: 'ready' | 'claim') {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      if (action === 'ready') await markTaskReady(task.number, task.version)
      else await claimTaskStage(task.number, task.version)
      await Promise.all([reloadWorkflow(), onChanged()])
    } catch (reason) {
      setError(`${action === 'ready' ? '标记可领取' : '领取'}失败：${(reason as Error).message}`)
      await refreshAfterRejection()
    } finally {
      setBusy(false)
    }
  }

  async function runPending() {
    if (!pendingAction || !body.trim() || busy) return
    setBusy(true)
    setError('')
    try {
      if (pendingAction === 'withdraw') {
        await withdrawTaskReadiness(task.number, task.version, body.trim())
      } else if (pendingAction === 'cancel') {
        await cancelTask(task.number, task.version, body.trim())
      } else if (pendingAction === 'resolve_issue' && activeIssue) {
        await resolveTaskIssue(task.number, task.version, activeIssue, body.trim())
      } else if (activeClaim && ownsClaim) {
        if (pendingAction === 'release') await releaseTaskStage(task.number, task.version, activeClaim, body.trim())
        if (pendingAction === 'record_work') await recordTaskWorkSubmission(task.number, task.version, activeClaim, body.trim())
        if (pendingAction === 'complete_execution') await completeTaskExecution(task.number, task.version, activeClaim, body.trim())
        if (pendingAction === 'request_changes') await requestTaskChanges(task.number, task.version, activeClaim, body.trim())
        if (pendingAction === 'accept') await acceptTask(task.number, task.version, activeClaim, body.trim())
        if (pendingAction === 'request_resolution') {
          await requestTaskResolution(task.number, task.version, activeClaim, issueType, body.trim())
        }
      }
      setPendingAction(null)
      setBody('')
      await Promise.all([reloadWorkflow(), onChanged()])
    } catch (reason) {
      setError(`${ACTION_LABELS[pendingAction]}失败：${(reason as Error).message}`)
      await refreshAfterRejection()
    } finally {
      setBusy(false)
    }
  }

  const actionButton = (action: PendingAction, label: string, danger = false) => (
    <button
      type="button"
      onClick={() => {
        setPendingAction(action)
        setBody('')
      }}
      className={danger
        ? 'rounded-md px-2.5 py-1.5 text-xs font-medium text-danger hover:bg-danger/10'
        : 'rounded-md border border-border-strong px-2.5 py-1.5 text-xs font-medium text-fg hover:bg-surface-subtle'}
    >
      {label}
    </button>
  )

  return (
    <section aria-label="任务工作流" className="border-y border-border py-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium text-fg">任务阶段</h3>
          <p className="mt-0.5 text-xs text-fg-muted">
            可以多次记录工作；只有完成执行才会结束当前领取并进入验收。
          </p>
        </div>
        <PhaseBadge phase={task.phase} activity={task.activity} />
      </div>

      {activeClaim && (
        <div className="mt-3 flex items-start gap-2 rounded-md bg-secondary/10 px-3 py-2 text-sm text-fg">
          <Hand className="mt-0.5 size-4 shrink-0 text-secondary" aria-hidden="true" />
          <span>
            {activeClaim.stage === 'execution' ? '执行' : '验收'}已被
            {ownsClaim ? '你' : activeClaim.claimed_by.type === 'agent' ? ' Agent' : '其他成员'}领取。
          </span>
        </div>
      )}

      {activeIssue && (
        <div className="mt-3 flex items-start gap-2 rounded-md bg-status-in-progress/10 px-3 py-2 text-sm text-status-in-progress">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>
            当前等待解决：{activeIssue.issue_type === 'decision_required' ? '需要决策' : '需要解决依赖项'}。
          </span>
        </div>
      )}

      <div className="mt-3 flex flex-wrap gap-2">
        {task.phase === 'backlog' && (
          <button type="button" disabled={busy} onClick={() => void runSimple('ready')} className="inline-flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50">
            <Play className="size-3.5" aria-hidden="true" />标记可领取
          </button>
        )}
        {task.phase === 'ready' && (
          <>
            <button type="button" disabled={busy} onClick={() => void runSimple('claim')} className="inline-flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50">
              <Hand className="size-3.5" aria-hidden="true" />领取执行
            </button>
            {actionButton('withdraw', '退回待规划')}
          </>
        )}
        {(task.phase === 'in_progress' || task.phase === 'in_review') && task.activity === 'available' && (
          <button type="button" disabled={busy} onClick={() => void runSimple('claim')} className="inline-flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50">
            <Hand className="size-3.5" aria-hidden="true" />
            {task.phase === 'in_review' ? '领取验收' : '继续执行'}
          </button>
        )}
        {task.activity === 'needs_resolution' && activeIssue && actionButton('resolve_issue', '解决 Issue')}
        {activeClaim && ownsClaim && (
          <>
            {activeClaim.stage === 'execution' && actionButton('record_work', '记录工作')}
            {activeClaim.stage === 'execution' && actionButton('complete_execution', '完成执行，进入验收')}
            {activeClaim.stage === 'review' && actionButton('accept', '接受并完成')}
            {activeClaim.stage === 'review' && actionButton('request_changes', '退回修改')}
            {actionButton('request_resolution', '请求解决')}
            {actionButton('release', '释放领取')}
          </>
        )}
        {!terminal && actionButton('cancel', '取消任务', true)}
      </div>

      {pendingAction && (
        <div className="mt-3 grid gap-2 rounded-md bg-surface-subtle p-3">
          <p className="text-xs font-medium text-fg">{ACTION_LABELS[pendingAction]}</p>
          {pendingAction === 'request_resolution' && (
            <select
              aria-label="Issue 类型"
              value={issueType}
              onChange={(event) => setIssueType(event.target.value as IssueThreadType)}
              className="h-9 rounded-md border border-border-strong bg-surface px-2 text-sm text-fg"
            >
              <option value="decision_required">需要决策</option>
              <option value="dependency_required">需要解决依赖项</option>
            </select>
          )}
          <MarkdownComposer
            value={body}
            onChange={setBody}
            ariaLabel={ACTION_LABELS[pendingAction]}
            rows={3}
            autoFocus
            disabled={busy}
            placeholder={pendingAction === 'resolve_issue'
              ? '写明最终结论或依赖解决结果'
              : pendingAction === 'record_work'
                ? '写明这次完成了什么、如何验证，或留下交付引用'
                : pendingAction === 'complete_execution'
                  ? '概括本轮完整交付；确认后将进入验收'
                  : '写明原因、交接内容或结果摘要'}
          />
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setPendingAction(null)} className="px-3 py-1.5 text-xs font-medium text-fg-muted">取消</button>
            <button type="button" disabled={!body.trim() || busy} onClick={() => void runPending()} className="inline-flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50">
              {pendingAction === 'record_work' ? <Send className="size-3.5" aria-hidden="true" />
                : pendingAction === 'request_changes' ? <RotateCcw className="size-3.5" aria-hidden="true" />
                  : pendingAction === 'cancel' ? <XCircle className="size-3.5" aria-hidden="true" />
                    : <Check className="size-3.5" aria-hidden="true" />}
              确认
            </button>
          </div>
        </div>
      )}
      {error && <p role="alert" className="mt-3 text-sm text-danger">{error}</p>}
    </section>
  )
}
