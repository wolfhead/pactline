import { useState } from 'react'
import { apiPost } from '../api/client'
import { useIdentity } from '../identity'
import {
  COMPLETIONS,
  COMPLETION_LABELS,
  DIFFICULTIES,
  DIFFICULTY_LABELS,
  VALUE_LEVELS,
  VALUE_LEVEL_LABELS,
  type Bounty,
  type Completion,
  type Difficulty,
  type ValueLevel,
} from '../types'

/**
 * The steward-only correction channel (POST /api/bounties/{id}/amend,
 * bountyHandler.amend in internal/api/bounty_handler.go). It is the only
 * path that can grade historical records: every ordinary channel for value
 * level, difficulty and completion is a one-way door that locks once the
 * bounty leaves DRAFT/OPEN, is settled, or is already COMPLETED — so a
 * grade the pricing group decided after the fact would otherwise be
 * permanently unrecordable. This is why every bounty already in the
 * archive is currently ungraded, and why this panel is not a corner case.
 *
 * Visibility mirrors the backend role check inline (steward only); unlike
 * CanSetValueLevel/CanSetDifficulty there is no status window to mirror
 * here — amend works on a bounty in any status, per its own doc comment —
 * except difficulty, which the select below disables once settled (mirrors
 * domain.CanSetDifficulty's I2 lock).
 */
export default function StewardCorrectionPanel({ bounty, onChanged }: { bounty: Bounty; onChanged: () => void }) {
  const { me } = useIdentity()
  const [retro, setRetro] = useState(bounty.retrospective ?? '')
  const [personDays, setPersonDays] = useState(bounty.person_days != null ? String(bounty.person_days) : '')
  const [valueLevel, setValueLevel] = useState<ValueLevel | ''>('')
  const [difficulty, setDifficulty] = useState<Difficulty | ''>('')
  const [completion, setCompletion] = useState<Completion | ''>('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const isSteward = Boolean(me?.roles.includes('STEWARD'))
  if (!isSteward) return null

  const difficultyLocked = bounty.settled_at != null

  async function submit() {
    setError('')

    let personDaysValue: number | undefined
    if (personDays !== '') {
      const parsed = Number(personDays)
      if (!Number.isFinite(parsed) || parsed < 0) {
        setError(`实际人天「${personDays}」不是合法的非负数字,请检查`)
        return
      }
      personDaysValue = parsed
    }

    setSaving(true)
    try {
      await apiPost(`/api/bounties/${bounty.id}/amend`, {
        retrospective: retro,
        person_days: personDaysValue,
        value_level: valueLevel || undefined,
        difficulty: difficulty || undefined,
        completion: completion || undefined,
      })
      setValueLevel('')
      setDifficulty('')
      setCompletion('')
      onChanged()
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section>
      <h3>Steward 修正通道</h3>
      <p className="hint">
        唯一能为历史记录补档的通道:结论、实际人天可直接改写;价值档 / 难度档 / 完成度档留空表示不改动。
      </p>
      <label>
        结论 / 复盘
        <textarea rows={2} value={retro} onChange={(e) => setRetro(e.target.value)} />
      </label>
      <div className="row">
        <label>实际人天
          <input value={personDays} onChange={(e) => setPersonDays(e.target.value)} inputMode="decimal" />
        </label>
        <label>
          价值档
          <select value={valueLevel} onChange={(e) => setValueLevel(e.target.value as ValueLevel | '')}>
            <option value="">不改动</option>
            {VALUE_LEVELS.map((v) => (
              <option key={v} value={v}>{VALUE_LEVEL_LABELS[v]}</option>
            ))}
          </select>
        </label>
        <label>
          难度档
          <select
            value={difficulty}
            disabled={difficultyLocked}
            onChange={(e) => setDifficulty(e.target.value as Difficulty | '')}
          >
            <option value="">不改动</option>
            {DIFFICULTIES.map((d) => (
              <option key={d} value={d}>{DIFFICULTY_LABELS[d]}</option>
            ))}
          </select>
          {difficultyLocked && <span className="hint">已结算,难度不可再修正</span>}
        </label>
        <label>
          完成度档
          <select value={completion} onChange={(e) => setCompletion(e.target.value as Completion | '')}>
            <option value="">不改动</option>
            {COMPLETIONS.map((c) => (
              <option key={c} value={c}>{COMPLETION_LABELS[c]}</option>
            ))}
          </select>
        </label>
      </div>
      <div className="row">
        <button onClick={submit} disabled={saving}>{saving ? '提交中…' : '提交修正'}</button>
      </div>
      {error && <p className="error">{error}</p>}
    </section>
  )
}
