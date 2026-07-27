import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import CreditPanel from '../components/CreditPanel'
import { useIdentity } from '../identity'
import { STATUS_LABELS, type Bounty, type Status } from '../types'

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
  const [error, setError] = useState('')
  const [pending, setPending] = useState<Status | null>(null)
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
    // effort spent on hand-in. Sending either on an unrelated transition
    // would silently persist a stale value the user never meant to submit
    // for that edge (A1).
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
      })
      reload()
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setPending(null)
    }
  }

  if (error && !bounty) return <p className="error">{error}</p>
  if (!bounty) return <p className="hint">加载中…</p>

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
    </section>
  )
}
