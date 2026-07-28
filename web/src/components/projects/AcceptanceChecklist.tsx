import { useState, type FormEvent } from 'react'
import type { AcceptanceCriterion, AcceptanceOutcome } from '@/api/acceptance'

const OUTCOME_LABELS: Record<AcceptanceOutcome, string> = {
  passed: '通过',
  failed: '未通过',
  unable: '无法检查',
  waived: '豁免',
}

interface AcceptanceChecklistProps {
  title: string
  criteria: AcceptanceCriterion[]
  onAdd: (criterion: string, instructions: string) => Promise<void>
  onCheck: (
    criterion: AcceptanceCriterion,
    outcome: AcceptanceOutcome,
    evidence: string,
  ) => Promise<void>
  onUpdate: (criterion: AcceptanceCriterion, text: string, instructions: string) => Promise<void>
  onRemove: (criterion: AcceptanceCriterion) => Promise<void>
}

export default function AcceptanceChecklist({
  title,
  criteria,
  onAdd,
  onCheck,
  onUpdate,
  onRemove,
}: AcceptanceChecklistProps) {
  const [adding, setAdding] = useState(false)
  const [checkingID, setCheckingID] = useState<string | null>(null)
  const [editingID, setEditingID] = useState<string | null>(null)
  const [error, setError] = useState('')

  async function submitCriterion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    setError('')
    try {
      await onAdd(String(data.get('criterion') ?? ''), String(data.get('instructions') ?? ''))
      form.reset()
      setAdding(false)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function submitCheck(event: FormEvent<HTMLFormElement>, criterion: AcceptanceCriterion) {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    setError('')
    try {
      await onCheck(
        criterion,
        String(data.get('outcome')) as AcceptanceOutcome,
        String(data.get('evidence') ?? ''),
      )
      setCheckingID(null)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function submitEdit(event: FormEvent<HTMLFormElement>, criterion: AcceptanceCriterion) {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setError('')
    try {
      await onUpdate(
        criterion,
        String(data.get('criterion') ?? ''),
        String(data.get('instructions') ?? ''),
      )
      setEditingID(null)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  return (
    <section aria-label={title} className="rounded-lg border border-border bg-surface-raised p-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-semibold">{title}</h2>
        <button type="button" onClick={() => setAdding((value) => !value)} className="text-sm font-medium text-accent">
          添加验收项
        </button>
      </div>

      {adding && (
        <form onSubmit={submitCriterion} className="mt-3 grid gap-2 rounded-md bg-surface-subtle p-3">
          <input
            name="criterion"
            required
            placeholder="需要成立的可观察事实"
            className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
          />
          <textarea
            name="instructions"
            required
            rows={2}
            placeholder="如何逐项验证"
            className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
          />
          <div className="flex justify-end">
            <button type="submit" className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white">保存</button>
          </div>
        </form>
      )}

      <div className="mt-3 divide-y divide-border">
        {criteria.length === 0 && <p className="py-3 text-sm text-fg-muted">尚未定义验收项。</p>}
        {criteria.map((criterion) => {
          const outcome = criterion.current_check?.outcome
          return (
            <div key={criterion.id} className="py-3">
              <div className="flex items-start gap-3">
                <span
                  aria-hidden="true"
                  className={`mt-1 size-4 shrink-0 rounded border ${
                    outcome === 'passed' || outcome === 'waived'
                      ? 'border-status-done bg-status-done'
                      : outcome === 'failed'
                        ? 'border-danger bg-danger'
                        : 'border-border-strong'
                  }`}
                />
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-medium">{criterion.criterion}</p>
                  <p className="mt-1 text-xs text-fg-muted">{criterion.verification_instructions}</p>
                  {criterion.current_check && (
                    <p className="mt-2 text-xs text-fg-muted">
                      {OUTCOME_LABELS[criterion.current_check.outcome]}：{criterion.current_check.evidence}
                    </p>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setEditingID((id) => id === criterion.id ? null : criterion.id)}
                    className="text-sm text-fg-muted"
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    onClick={async () => {
                      setError('')
                      try {
                        await onRemove(criterion)
                      } catch (reason) {
                        setError((reason as Error).message)
                      }
                    }}
                    className="text-sm text-fg-muted"
                  >
                    移除
                  </button>
                  <button
                    type="button"
                    onClick={() => setCheckingID((id) => id === criterion.id ? null : criterion.id)}
                    className="text-sm text-accent"
                  >
                    检查
                  </button>
                </div>
              </div>
              {editingID === criterion.id && (
                <form onSubmit={(event) => submitEdit(event, criterion)} className="mt-3 ml-7 grid gap-2">
                  <input
                    name="criterion"
                    required
                    defaultValue={criterion.criterion}
                    className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
                  />
                  <textarea
                    name="instructions"
                    required
                    defaultValue={criterion.verification_instructions}
                    rows={2}
                    className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
                  />
                  <div className="flex justify-end">
                    <button type="submit" className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white">
                      保存修改
                    </button>
                  </div>
                </form>
              )}
              {checkingID === criterion.id && (
                <form onSubmit={(event) => submitCheck(event, criterion)} className="mt-3 ml-7 grid gap-2 sm:grid-cols-[9rem_1fr_auto]">
                  <select name="outcome" className="rounded-md border border-border-strong bg-surface px-2 py-2 text-sm">
                    {Object.entries(OUTCOME_LABELS).map(([value, label]) => (
                      <option key={value} value={value}>{label}</option>
                    ))}
                  </select>
                  <input
                    name="evidence"
                    required
                    placeholder="检查证据或原因"
                    className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
                  />
                  <button type="submit" className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white">记录</button>
                </form>
              )}
            </div>
          )
        })}
      </div>
      {error && <p role="alert" className="mt-2 text-sm text-danger">操作失败：{error}</p>}
    </section>
  )
}
