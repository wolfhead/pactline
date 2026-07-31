import { useState } from 'react'
import { CheckCircle2, Circle, RotateCcw, ShieldCheck, XCircle } from 'lucide-react'
import type {
  AcceptanceCriterion,
  AcceptanceOutcome,
} from '@/api/acceptance'
import { cn } from '@/lib/utils'

interface SubmissionReviewProps {
  criteria: AcceptanceCriterion[]
  submittedAt: string
  onCheck: (
    criterion: AcceptanceCriterion,
    outcome: AcceptanceOutcome,
    evidence: string,
  ) => Promise<void>
  onComplete: () => Promise<void>
  onReturnForChanges: () => Promise<void>
}

type ReviewAction = 'failed' | 'waived'

export default function SubmissionReview({
  criteria,
  submittedAt,
  onCheck,
  onComplete,
  onReturnForChanges,
}: SubmissionReviewProps) {
  const [pendingCriterionID, setPendingCriterionID] = useState('')
  const [reviewAction, setReviewAction] = useState<Record<string, ReviewAction | undefined>>({})
  const [notes, setNotes] = useState<Record<string, string>>({})
  const [finishing, setFinishing] = useState(false)
  const [returning, setReturning] = useState(false)
  const [error, setError] = useState('')

  const reviewed = criteria.map((criterion) => humanReviewAfter(criterion, submittedAt))
  const completionReady = reviewed.every((check) => (
    check?.outcome === 'passed' || check?.outcome === 'waived'
  ))

  async function record(
    criterion: AcceptanceCriterion,
    outcome: AcceptanceOutcome,
    evidence: string,
  ) {
    if (pendingCriterionID) return
    setPendingCriterionID(criterion.id)
    setError('')
    try {
      await onCheck(criterion, outcome, evidence)
      setReviewAction((current) => ({ ...current, [criterion.id]: undefined }))
      setNotes((current) => ({ ...current, [criterion.id]: '' }))
    } catch (reason) {
      setError(`记录验收结果失败：${(reason as Error).message}`)
    } finally {
      setPendingCriterionID('')
    }
  }

  async function complete() {
    if (!completionReady || finishing) return
    setFinishing(true)
    setError('')
    try {
      await onComplete()
    } catch (reason) {
      setError(`完成任务失败：${(reason as Error).message}`)
    } finally {
      setFinishing(false)
    }
  }

  async function returnForChanges() {
    if (returning) return
    setReturning(true)
    setError('')
    try {
      await onReturnForChanges()
    } catch (reason) {
      setError(`退回任务失败：${(reason as Error).message}`)
    } finally {
      setReturning(false)
    }
  }

  return (
    <div
      role="region"
      aria-label="本次 Agent 提交验收"
      className="mt-3 border-t border-secondary/20 pt-3"
    >
      <div className="flex items-start gap-2">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-secondary" aria-hidden="true" />
        <div>
          <p className="text-sm font-medium text-fg">逐项核对本次提交</p>
          <p className="mt-0.5 text-xs text-fg-muted">
            Agent 自检只作为证据；每一项仍需由你确认。
          </p>
        </div>
      </div>

      <div className="mt-3 grid gap-2">
        {criteria.length === 0 && (
          <p className="rounded-md border border-border bg-surface px-3 py-2 text-sm text-fg-muted">
            此任务没有验收项。确认提交内容后，可直接完成任务。
          </p>
        )}
        {criteria.map((criterion) => {
          const currentReview = humanReviewAfter(criterion, submittedAt)
          const isSatisfied = currentReview?.outcome === 'passed'
            || currentReview?.outcome === 'waived'
          const agentCheck = criterion.current_check?.checker_type === 'agent'
            ? criterion.current_check
            : null
          const action = reviewAction[criterion.id]
          const note = notes[criterion.id] ?? ''
          const pending = pendingCriterionID === criterion.id

          return (
            <div
              key={criterion.id}
              className={cn(
                'rounded-md border bg-surface p-3',
                isSatisfied ? 'border-status-done/35' : 'border-border',
              )}
            >
              <div className="flex items-start gap-2.5">
                {isSatisfied ? (
                  <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-status-done" aria-hidden="true" />
                ) : currentReview ? (
                  <XCircle className="mt-0.5 size-4 shrink-0 text-danger" aria-hidden="true" />
                ) : (
                  <Circle className="mt-0.5 size-4 shrink-0 text-fg-subtle" aria-hidden="true" />
                )}
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium text-fg">{criterion.criterion}</p>
                  <p className="mt-1 text-xs text-fg-muted">
                    {criterion.verification_instructions}
                  </p>
                  {agentCheck && (
                    <p className="mt-2 rounded-sm bg-secondary-subtle px-2 py-1.5 text-xs text-secondary">
                      Agent 自检：{outcomeLabel(agentCheck.outcome)} · {agentCheck.evidence}
                    </p>
                  )}
                  {currentReview && (
                    <p className={cn(
                      'mt-2 text-xs',
                      isSatisfied ? 'text-status-done' : 'text-danger',
                    )}>
                      你的验收：{outcomeLabel(currentReview.outcome)} · {currentReview.evidence}
                    </p>
                  )}
                </div>
              </div>

              {!isSatisfied && (
                <div className="mt-3 ml-6 flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    aria-label={`确认通过：${criterion.criterion}`}
                    disabled={Boolean(pendingCriterionID)}
                    onClick={() => void record(
                      criterion,
                      'passed',
                      '已核对本次 Agent 提交及其自检证据。',
                    )}
                    className="rounded-md bg-secondary px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50"
                  >
                    {pending ? '记录中…' : '确认通过'}
                  </button>
                  <button
                    type="button"
                    onClick={() => setReviewAction((current) => ({
                      ...current,
                      [criterion.id]: current[criterion.id] === 'failed' ? undefined : 'failed',
                    }))}
                    className="rounded-md border border-border-strong px-3 py-1.5 text-xs font-medium text-fg"
                  >
                    未通过
                  </button>
                  <button
                    type="button"
                    onClick={() => setReviewAction((current) => ({
                      ...current,
                      [criterion.id]: current[criterion.id] === 'waived' ? undefined : 'waived',
                    }))}
                    className="px-2 py-1.5 text-xs font-medium text-fg-muted"
                  >
                    豁免
                  </button>
                </div>
              )}

              {action && !isSatisfied && (
                <div className="mt-2 ml-6 grid gap-2 sm:grid-cols-[1fr_auto]">
                  <textarea
                    rows={2}
                    aria-label={`${action === 'failed' ? '未通过' : '豁免'}原因：${criterion.criterion}`}
                    value={note}
                    onChange={(event) => setNotes((current) => ({
                      ...current,
                      [criterion.id]: event.target.value,
                    }))}
                    placeholder={action === 'failed' ? '说明需要调整的地方' : '说明本次豁免原因'}
                    className="rounded-md border border-border-strong bg-surface px-2 py-1.5 text-sm text-fg"
                  />
                  <button
                    type="button"
                    disabled={!note.trim() || Boolean(pendingCriterionID)}
                    onClick={() => void record(criterion, action, note.trim())}
                    className={cn(
                      'self-end rounded-md px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50',
                      action === 'failed' ? 'bg-danger' : 'bg-fg-muted',
                    )}
                  >
                    记录{action === 'failed' ? '未通过' : '豁免'}
                  </button>
                </div>
              )}
            </div>
          )
        })}
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <button
          type="button"
          disabled={returning || finishing}
          onClick={() => void returnForChanges()}
          className="inline-flex items-center gap-1.5 rounded-md px-2 py-1.5 text-xs font-medium text-fg-muted hover:bg-surface-subtle hover:text-fg disabled:opacity-50"
        >
          <RotateCcw className="size-3.5" aria-hidden="true" />
          {returning ? '退回中…' : '退回待办'}
        </button>
        <button
          type="button"
          disabled={!completionReady || finishing || returning}
          onClick={() => void complete()}
          className="rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-accent-fg disabled:cursor-not-allowed disabled:opacity-45"
        >
          {finishing ? '完成中…' : '验收通过并完成任务'}
        </button>
      </div>
      {!completionReady && criteria.length > 0 && (
        <p className="mt-2 text-right text-xs text-fg-muted">完成前需处理全部验收项。</p>
      )}
      {error && <p role="alert" className="mt-2 text-sm text-danger">{error}</p>}
    </div>
  )
}

function humanReviewAfter(
  criterion: AcceptanceCriterion,
  submittedAt: string,
) {
  const check = criterion.current_check
  if (
    !check
    || check.checker_type !== 'user'
    || new Date(check.checked_at).getTime() < new Date(submittedAt).getTime()
  ) {
    return null
  }
  return check
}

function outcomeLabel(outcome: AcceptanceOutcome): string {
  switch (outcome) {
    case 'passed': return '通过'
    case 'failed': return '未通过'
    case 'unable': return '无法检查'
    case 'waived': return '豁免'
  }
}
