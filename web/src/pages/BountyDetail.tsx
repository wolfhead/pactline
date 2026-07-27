import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import CalibrationPanel from '../components/CalibrationPanel'
import CreditPanel from '../components/CreditPanel'
import StewardCorrectionPanel from '../components/StewardCorrectionPanel'
import { useIdentity } from '../identity'
import {
  COMPLETIONS,
  COMPLETION_LABELS,
  DIFFICULTIES,
  DIFFICULTY_LABELS,
  STATUS_LABELS,
  VALUE_LEVELS,
  VALUE_LEVEL_LABELS,
  type Bounty,
  type Completion,
  type Difficulty,
  type Status,
  type ValueLevel,
} from '../types'

// C1: mirrors `allowedTransitions` in internal/domain/bounty.go. The two must
// be changed together — this map exists only so the UI can grey out
// impossible transitions before the round trip; the backend re-validates
// every one of these via ValidateTransition regardless of what this map says.
const NEXT_STATES: Record<Status, Status[]> = {
  DRAFT: ['OPEN', 'ABANDONED'],
  OPEN: ['DRAFT', 'CLAIMED', 'ABANDONED'],
  CLAIMED: ['OPEN', 'DELIVERED', 'ABANDONED'],
  DELIVERED: ['CLAIMED', 'COMPLETED', 'ABANDONED'],
  COMPLETED: [],
  ABANDONED: [],
}

export default function BountyDetail() {
  const { id } = useParams<{ id: string }>()
  const { me } = useIdentity()
  const [bounty, setBounty] = useState<Bounty | null>(null)
  const [retro, setRetro] = useState('')
  const [personDays, setPersonDays] = useState('')
  const [completion, setCompletion] = useState<Completion>('MET')
  const [error, setError] = useState('')
  const [pending, setPending] = useState<Status | null>(null)

  const [valueLevelInput, setValueLevelInput] = useState<ValueLevel | ''>('')
  const [valueLevelSaving, setValueLevelSaving] = useState(false)
  const [valueLevelError, setValueLevelError] = useState('')

  const [difficultyInput, setDifficultyInput] = useState<Difficulty | ''>('')
  const [difficultySaving, setDifficultySaving] = useState(false)
  const [difficultyError, setDifficultyError] = useState('')

  // Bumped by `reload()` to force the effect below to re-run against the
  // same id (e.g. after a successful transition), without giving it its own
  // separate, unguarded fetch path.
  const [reloadToken, setReloadToken] = useState(0)
  const reload = useCallback(() => setReloadToken((t) => t + 1), [])

  // Fetches whenever the route id changes, a reload is requested, or the
  // current identity changes. A RESTRICTED bounty's visibility depends on
  // who's asking (canViewDraft/canView on the server), so switching
  // identity in the header must refetch an already-mounted detail page
  // rather than leave the previous identity's bounty on screen.
  // setBounty(null)/setError('') below re-enter the loading gate
  // (`if (!bounty) return <hint>`) on every run, mirroring the
  // setLoading(true) convention in Board.tsx/Portfolio.tsx, so a stale
  // bounty (or a stale error from a previous fetch) is never shown as
  // current while a new request is in flight. Also guards against request
  // cancellation (C2): if the id/identity changes again before this
  // request resolves, `cancelled` is set in the cleanup below, so a slow
  // stale response can never overwrite a newer one's result. Mirrors the
  // cancelled-flag idiom in identity.tsx.
  useEffect(() => {
    if (!id) return
    setBounty(null)
    setError('')
    let cancelled = false
    apiGet<Bounty>(`/api/bounties/${id}`)
      .then((b) => {
        if (cancelled) return
        setBounty(b)
        setRetro(b.retrospective ?? '')
        setPersonDays(b.person_days != null ? String(b.person_days) : '')
      })
      .catch((err) => {
        if (cancelled) return
        setError(String(err.message ?? err))
      })
    return () => {
      cancelled = true
    }
  }, [id, reloadToken, me?.id])

  async function move(to: Status) {
    setError('')

    // Each field is meaningful only for the transition it belongs to:
    // retrospective records why work was abandoned, person_days records
    // effort spent on hand-in, completion grades the delivery at the moment
    // it is accepted. Sending any of them on an unrelated transition would
    // silently persist a stale value the user never meant to submit for
    // that edge (A1).
    let personDaysValue: number | undefined
    if (to === 'DELIVERED' && personDays !== '') {
      const parsed = Number(personDays)
      if (!Number.isFinite(parsed) || parsed < 0) {
        setError(`实际人天「${personDays}」不是合法的非负数字,请检查`)
        return
      }
      personDaysValue = parsed
    }

    setPending(to)
    try {
      await apiPost(`/api/bounties/${id}/transition`, {
        to,
        retrospective: to === 'ABANDONED' ? (retro || undefined) : undefined,
        person_days: personDaysValue,
        // Completion belongs to the DELIVERED -> COMPLETED edge only (spec
        // §6.1: the sponsor grades completion "at acceptance"). This is the
        // acceptance path that used to send no completion at all, which is
        // what made every accepted work permanently unscorable.
        completion: to === 'COMPLETED' ? completion : undefined,
      })
      reload()
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setPending(null)
    }
  }

  async function saveValueLevel() {
    if (!bounty || !valueLevelInput) return
    setValueLevelError('')
    setValueLevelSaving(true)
    try {
      await apiPost(`/api/bounties/${bounty.id}/value-level`, { value_level: valueLevelInput })
      setValueLevelInput('')
      reload()
    } catch (err) {
      setValueLevelError(String((err as Error).message))
    } finally {
      setValueLevelSaving(false)
    }
  }

  async function saveDifficulty() {
    if (!bounty || !difficultyInput) return
    setDifficultyError('')
    setDifficultySaving(true)
    try {
      await apiPost(`/api/bounties/${bounty.id}/difficulty`, { difficulty: difficultyInput })
      setDifficultyInput('')
      reload()
    } catch (err) {
      setDifficultyError(String((err as Error).message))
    } finally {
      setDifficultySaving(false)
    }
  }

  if (error && !bounty) return <p className="error">{error}</p>
  if (!bounty) return <p className="hint">加载中…</p>

  // Mirrors domain.CanSetValueLevel in internal/domain/bounty.go: the
  // sponsor (or a steward) may set or amend the value level only while the
  // bounty is still DRAFT or OPEN. This is the frontend's fifth
  // hand-duplicated backend rule (after CanNominate in CreditPanel.tsx,
  // allowedTransitions above, bountyHandler.create's SPONSOR/STEWARD check
  // in Board.tsx, and creditRoleOrder in Portfolio.tsx) — the two must be
  // changed together. The window is not enforced here for its own sake;
  // see CanSetValueLevel's doc comment for why it exists (a lock with no
  // escape hatch made every terminal bounty permanently unscorable, which
  // the steward correction channel below exists to unblock).
  const canSetValueLevel = Boolean(
    me &&
      (me.id === bounty.sponsor_id || me.roles.includes('STEWARD')) &&
      (bounty.status === 'DRAFT' || bounty.status === 'OPEN'),
  )

  // Mirrors domain.CanSetDifficulty: TECH_LEAD or STEWARD only — never the
  // sponsor, even on their own bounty — and refused once the bounty is
  // settled.
  const canSetDifficulty = Boolean(
    me && (me.roles.includes('TECH_LEAD') || me.roles.includes('STEWARD')) && bounty.settled_at == null,
  )

  const canAcceptWithCompletion = NEXT_STATES[bounty.status].includes('COMPLETED')

  return (
    <section>
      <h2>{bounty.title}</h2>
      <div className="meta">
        <span>{STATUS_LABELS[bounty.status]}</span>
        <span>{bounty.type === 'PLAN' ? '方案单' : '交付单'}</span>
        <span>{bounty.commitment === 'EXPLORATORY' ? '探索型' : '承诺型'}</span>
        {bounty.business_lines.map((l) => (
          <span key={l.tag} className="tag">{l.tag} {Math.round(l.weight * 100)}%</span>
        ))}
      </div>

      {/* Levels: value/difficulty are inputs to a future score, not a score
          themselves, so they are shown everywhere the bounty appears (like
          business_lines). settled_score below is the one thing that is a
          score, and only appears here — see the comment on Bounty in
          types.ts. */}
      <div className="meta">
        <span>价值档:{bounty.value_level ? VALUE_LEVEL_LABELS[bounty.value_level] : '未设置'}</span>
        <span>难度档:{bounty.difficulty ? DIFFICULTY_LABELS[bounty.difficulty] : '未设置'}</span>
        {bounty.completion && <span>完成度档:{COMPLETION_LABELS[bounty.completion]}</span>}
      </div>

      {bounty.settled_score != null && (
        <p className="hint">
          结算分值:{bounty.settled_score}
          {bounty.settled_at && <>(结算于 {new Date(bounty.settled_at).toLocaleDateString()}）</>}
        </p>
      )}

      {canSetValueLevel && (
        <div className="row">
          <label>
            设置价值档
            <select value={valueLevelInput} onChange={(e) => setValueLevelInput(e.target.value as ValueLevel | '')}>
              <option value="">选择价值档…</option>
              {VALUE_LEVELS.map((v) => (
                <option key={v} value={v}>{VALUE_LEVEL_LABELS[v]}</option>
              ))}
            </select>
          </label>
          <button onClick={saveValueLevel} disabled={!valueLevelInput || valueLevelSaving}>
            {valueLevelSaving ? '提交中…' : '设置价值档'}
          </button>
          {valueLevelError && <span className="error">{valueLevelError}</span>}
        </div>
      )}

      {canSetDifficulty && (
        <div className="row">
          <label>
            设置难度档
            <select value={difficultyInput} onChange={(e) => setDifficultyInput(e.target.value as Difficulty | '')}>
              <option value="">选择难度档…</option>
              {DIFFICULTIES.map((d) => (
                <option key={d} value={d}>{DIFFICULTY_LABELS[d]}</option>
              ))}
            </select>
          </label>
          <button onClick={saveDifficulty} disabled={!difficultyInput || difficultySaving}>
            {difficultySaving ? '提交中…' : '设置难度档'}
          </button>
          {difficultyError && <span className="error">{difficultyError}</span>}
        </div>
      )}

      {bounty.goal && <p><strong>业务目标</strong>:{bounty.goal}</p>}
      {bounty.acceptance_criteria && <p><strong>验收标准</strong>:{bounty.acceptance_criteria}</p>}

      <div className="row">
        <label>实际人天
          <input value={personDays} onChange={(e) => setPersonDays(e.target.value)} inputMode="decimal" />
        </label>
      </div>
      <label>
        结论 / 复盘（放弃时必填）
        <textarea rows={2} value={retro} onChange={(e) => setRetro(e.target.value)} />
      </label>

      {canAcceptWithCompletion && (
        <label>
          完成度档
          <select value={completion} onChange={(e) => setCompletion(e.target.value as Completion)}>
            {COMPLETIONS.map((c) => (
              <option key={c} value={c}>{COMPLETION_LABELS[c]}</option>
            ))}
          </select>
        </label>
      )}

      <div className="row">
        {NEXT_STATES[bounty.status].map((s) => (
          <button key={s} onClick={() => move(s)} disabled={pending !== null}>
            {pending === s ? '提交中…' : `转为「${STATUS_LABELS[s]}」`}
          </button>
        ))}
        {NEXT_STATES[bounty.status].length === 0 && <span className="hint">已归档为作品,不可再流转。</span>}
      </div>

      {error && <p className="error">{error}</p>}

      <CreditPanel bounty={bounty} onChanged={reload} />

      <CalibrationPanel bounty={bounty} />

      <StewardCorrectionPanel bounty={bounty} onChanged={reload} />
    </section>
  )
}
