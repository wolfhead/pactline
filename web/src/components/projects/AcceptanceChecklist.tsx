import { useState, type FormEvent } from 'react'
import MarkdownComposer from '@/components/markdown/MarkdownComposer'
import MarkdownContent from '@/components/markdown/MarkdownContent'
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
  const [newCriterion, setNewCriterion] = useState('')
  const [newInstructions, setNewInstructions] = useState('')
  const [editingCriterion, setEditingCriterion] = useState('')
  const [editingInstructions, setEditingInstructions] = useState('')
  const [checkEvidence, setCheckEvidence] = useState('')
  const [error, setError] = useState('')

  async function submitCriterion(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError('')
    try {
      await onAdd(newCriterion, newInstructions)
      setNewCriterion('')
      setNewInstructions('')
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
        checkEvidence,
      )
      setCheckEvidence('')
      setCheckingID(null)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  async function submitEdit(event: FormEvent<HTMLFormElement>, criterion: AcceptanceCriterion) {
    event.preventDefault()
    setError('')
    try {
      await onUpdate(criterion, editingCriterion, editingInstructions)
      setEditingID(null)
    } catch (reason) {
      setError((reason as Error).message)
    }
  }

  return (
    <section aria-label={title} className="rounded-lg border border-secondary/20 bg-secondary-subtle/60 p-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-semibold text-secondary">{title}</h2>
        <button
          type="button"
          onClick={() => {
            setAdding((value) => !value)
            setNewCriterion('')
            setNewInstructions('')
          }}
          className="text-sm font-medium text-secondary"
        >
          添加验收项
        </button>
      </div>

      {adding && (
        <form onSubmit={submitCriterion} className="mt-3 grid gap-2 rounded-md bg-surface-subtle p-3">
          <input
            required
            aria-label="验收事实"
            value={newCriterion}
            onChange={(event) => setNewCriterion(event.target.value)}
            placeholder="需要成立的可观察事实"
            className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
          />
          <MarkdownComposer
            value={newInstructions}
            onChange={setNewInstructions}
            ariaLabel="验证说明"
            rows={2}
            placeholder="如何逐项验证"
          />
          <div className="flex justify-end">
            <button
              type="submit"
              disabled={!newCriterion.trim() || !newInstructions.trim()}
              className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
            >
              保存
            </button>
          </div>
        </form>
      )}

      <div className="mt-3 divide-y divide-border">
        {criteria.length === 0 && <p className="py-3 text-sm text-fg-muted">尚未定义验收项。</p>}
        {criteria.map((criterion) => {
          const outcome = criterion.current_check?.outcome
          const purposeLabel = criterion.current_check?.purpose === 'execution_verification'
            ? '执行自检'
            : criterion.current_check?.purpose === 'acceptance'
              ? '验收'
              : ''
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
                  <div className="mt-1 text-fg-muted">
                    <MarkdownContent source={criterion.verification_instructions} />
                  </div>
                  {criterion.current_check && (
                    <div className="mt-2 text-fg-muted">
                      <p className="text-xs font-medium">
                        {purposeLabel && <>{purposeLabel} · </>}
                        {OUTCOME_LABELS[criterion.current_check.outcome]}
                      </p>
                      <div className="mt-1">
                        <MarkdownContent source={criterion.current_check.evidence} />
                      </div>
                    </div>
                  )}
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      if (editingID === criterion.id) {
                        setEditingID(null)
                        return
                      }
                      setEditingID(criterion.id)
                      setEditingCriterion(criterion.criterion)
                      setEditingInstructions(criterion.verification_instructions)
                    }}
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
                    onClick={() => {
                      setCheckingID((id) => id === criterion.id ? null : criterion.id)
                      setCheckEvidence('')
                    }}
                    className="text-sm text-accent"
                  >
                    检查
                  </button>
                </div>
              </div>
              {editingID === criterion.id && (
                <form onSubmit={(event) => submitEdit(event, criterion)} className="mt-3 ml-7 grid gap-2">
                  <input
                    required
                    aria-label="编辑验收事实"
                    value={editingCriterion}
                    onChange={(event) => setEditingCriterion(event.target.value)}
                    className="rounded-md border border-border-strong bg-surface px-3 py-2 text-sm"
                  />
                  <MarkdownComposer
                    value={editingInstructions}
                    onChange={setEditingInstructions}
                    ariaLabel="编辑验证说明"
                    rows={2}
                  />
                  <div className="flex justify-end">
                    <button
                      type="submit"
                      disabled={!editingCriterion.trim() || !editingInstructions.trim()}
                      className="rounded-md bg-accent px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50"
                    >
                      保存修改
                    </button>
                  </div>
                </form>
              )}
              {checkingID === criterion.id && (
                <form onSubmit={(event) => submitCheck(event, criterion)} className="mt-3 ml-7 grid gap-2 sm:grid-cols-[9rem_minmax(0,1fr)_auto] sm:items-end">
                  <select name="outcome" className="rounded-md border border-border-strong bg-surface px-2 py-2 text-sm">
                    {Object.entries(OUTCOME_LABELS).map(([value, label]) => (
                      <option key={value} value={value}>{label}</option>
                    ))}
                  </select>
                  <MarkdownComposer
                    value={checkEvidence}
                    onChange={setCheckEvidence}
                    ariaLabel="检查证据或原因"
                    rows={2}
                    placeholder="检查证据或原因"
                  />
                  <button
                    type="submit"
                    disabled={!checkEvidence.trim()}
                    className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
                  >
                    记录
                  </button>
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
