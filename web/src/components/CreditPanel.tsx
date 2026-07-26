import { useCallback, useEffect, useState } from 'react'
import { apiGet, apiPost } from '../api/client'
import { useIdentity } from '../identity'
import { CREDIT_ROLE_LABELS, type Bounty, type Credit, type CreditRole } from '../types'

const ROLES: CreditRole[] = ['DEFINE', 'LEAD', 'CO_DELIVER', 'REVIEW', 'SUPPORT', 'BASELINE']

/**
 * Credits are the collaboration record. Only the deliverer (or the steward)
 * nominates; only the nominee confirms — not even the steward may confirm on
 * someone else's behalf, which is the forgery this rule exists to prevent.
 */
export default function CreditPanel({ bounty, onChanged }: { bounty: Bounty; onChanged: () => void }) {
  const { me, users } = useIdentity()
  const [credits, setCredits] = useState<Credit[]>([])
  const [userId, setUserId] = useState('')
  const [role, setRole] = useState<CreditRole>('SUPPORT')
  const [evidence, setEvidence] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(() => {
    apiGet<Credit[]>(`/api/bounties/${bounty.id}/credits`)
      .then(setCredits)
      .catch((err) => setError(String(err.message ?? err)))
  }, [bounty.id])

  useEffect(load, [load])

  const canNominate = Boolean(me && (bounty.claimed_by === me.id || me.roles.includes('STEWARD')))

  async function nominate() {
    setError('')
    try {
      await apiPost(`/api/bounties/${bounty.id}/credits`, {
        user_id: userId,
        role,
        evidence,
      })
      setEvidence('')
      load()
      onChanged()
    } catch (err) {
      setError(String((err as Error).message))
    }
  }

  async function respond(id: string, status: 'CONFIRMED' | 'DECLINED') {
    setError('')
    try {
      await apiPost(`/api/credits/${id}/respond`, { status })
      load()
      onChanged()
    } catch (err) {
      setError(String((err as Error).message))
    }
  }

  const nameOf = (id: string) => users.find((u) => u.id === id)?.name ?? id

  return (
    <section>
      <h3>Credits</h3>
      {credits.length === 0 && <p className="hint">还没有署名。</p>}
      <ul className="credits">
        {credits.map((c) => (
          <li key={c.id}>
            <span className="role">{CREDIT_ROLE_LABELS[c.role]}</span>
            <span>{nameOf(c.user_id)}</span>
            <span className="tag">
              {c.status === 'CONFIRMED' ? '已确认' : c.status === 'DECLINED' ? '已拒绝' : '待确认'}
            </span>
            {c.nominated_by == null && <span className="tag">系统提名</span>}
            {c.evidence && <a href={c.evidence} target="_blank" rel="noreferrer">评审记录</a>}
            {me?.id === c.user_id && c.status === 'PENDING' && (
              <>
                <button onClick={() => respond(c.id, 'CONFIRMED')}>确认</button>
                <button onClick={() => respond(c.id, 'DECLINED')}>拒绝</button>
              </>
            )}
          </li>
        ))}
      </ul>

      {canNominate && (
        <div className="row">
          <select value={userId} onChange={(e) => setUserId(e.target.value)}>
            <option value="">选择成员…</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>{u.name}</option>
            ))}
          </select>
          <select value={role} onChange={(e) => setRole(e.target.value as CreditRole)}>
            {ROLES.map((r) => (
              <option key={r} value={r}>{CREDIT_ROLE_LABELS[r]}</option>
            ))}
          </select>
          {role === 'REVIEW' && (
            <input value={evidence} onChange={(e) => setEvidence(e.target.value)}
              placeholder="评审意见链接(REVIEW 必填)" />
          )}
          <button onClick={nominate} disabled={!userId}>提名</button>
        </div>
      )}

      {error && <p className="error">{error}</p>}
    </section>
  )
}
