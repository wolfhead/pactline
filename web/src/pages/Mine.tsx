import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import { useIdentity } from '../identity'
import { CREDIT_ROLE_LABELS, STATUS_LABELS, type Bounty, type Credit } from '../types'

/**
 * "Mine" carries the current user's pending attribution confirmations,
 * plus what they claimed and sponsored. Its list is scoped to `me` by
 * construction — /api/credits/pending only ever returns the caller's own
 * credits — so the confirm/decline buttons here are correct without any
 * extra ownership check: nothing here can widen to someone else's credit.
 */
export default function Mine() {
  const { me, users } = useIdentity()
  const [pending, setPending] = useState<Credit[]>([])
  const [claimed, setClaimed] = useState<Bounty[]>([])
  const [sponsored, setSponsored] = useState<Bounty[]>([])
  const [error, setError] = useState('')
  // Distinct from the empty-state checks below: WorkFeed/Board/BountyDetail
  // all fixed a defect where the empty-state text rendered before the
  // initial request resolved. Mirror that fix here rather than
  // reintroducing it.
  const [loading, setLoading] = useState<boolean>(true)
  // Tracks which single credit currently has a respond() request in
  // flight, so only that row's buttons are disabled and — per CreditPanel's
  // established pattern — a slow request can't be double-submitted.
  const [respondingId, setRespondingId] = useState<string | null>(null)
  // Deliberately separate from `error` above: `error` means the page's own
  // data failed to load and there is nothing to show, so it replaces the
  // content. A failed respond() on one credit is different — the rest of
  // the page's data is still valid, so it must not blank out the list the
  // user is looking at. Scoped to the credit id so only that row shows it.
  const [respondError, setRespondError] = useState<{ id: string; message: string } | null>(null)

  const load = useCallback(() => {
    if (!me) return
    setLoading(true)
    setError('')
    Promise.all([
      apiGet<Credit[]>('/api/credits/pending'),
      apiGet<Bounty[]>(`/api/bounties?claimed_by=${me.id}`),
      apiGet<Bounty[]>(`/api/bounties?sponsor_id=${me.id}`),
    ])
      .then(([p, c, s]) => {
        setPending(p)
        setClaimed(c)
        setSponsored(s)
      })
      .catch((err) => setError(String(err.message ?? err)))
      .finally(() => setLoading(false))
  }, [me])

  useEffect(load, [load])

  async function respond(id: string, status: 'CONFIRMED' | 'DECLINED') {
    setRespondError(null)
    setRespondingId(id)
    try {
      await apiPost(`/api/credits/${id}/respond`, { status })
      load()
    } catch (err) {
      setRespondError({ id, message: String((err as Error).message) })
    } finally {
      setRespondingId(null)
    }
  }

  // C3: falls back to the generic '成员' label for a known-but-unresolved
  // user id, never the raw UUID — a human-facing name must never be a bare
  // id. Kept consistent with Portfolio.tsx and CreditPanel.tsx. The '系统'
  // case is distinct: it means no id at all (a system-nominated credit has
  // no nominator), not an unresolved one.
  const nameOf = (id?: string) => (id ? (users.find((u) => u.id === id)?.name ?? '成员') : '系统')

  if (!me) return <p className="hint">加载中…</p>

  return (
    <section>
      <h2>我的</h2>

      {loading && <p className="hint">加载中…</p>}
      {!loading && error && <p className="error">{error}</p>}

      {!loading && !error && (
        <>
          <h3>待我确认的署名（{pending.length}）</h3>
          {pending.length === 0 && <p className="hint">没有待确认的署名。</p>}
          <ul className="credits">
            {pending.map((c) => (
              <li key={c.id}>
                <span className="role">{CREDIT_ROLE_LABELS[c.role]}</span>
                <Link to={`/bounties/${c.bounty_id}`}>查看单</Link>
                <span className="tag">提名人:{nameOf(c.nominated_by)}</span>
                <button onClick={() => respond(c.id, 'CONFIRMED')} disabled={respondingId === c.id}>
                  确认
                </button>
                <button onClick={() => respond(c.id, 'DECLINED')} disabled={respondingId === c.id}>
                  拒绝
                </button>
                {respondError?.id === c.id && <span className="error">响应失败:{respondError.message}</span>}
              </li>
            ))}
          </ul>

          <h3>我认领的单（{claimed.length}）</h3>
          {claimed.map((b) => (
            <p key={b.id}>
              <Link to={`/bounties/${b.id}`}>{b.title}</Link> —— {STATUS_LABELS[b.status]}
            </p>
          ))}

          <h3>我开的单（{sponsored.length}）</h3>
          {sponsored.map((b) => (
            <p key={b.id}>
              <Link to={`/bounties/${b.id}`}>{b.title}</Link> —— {STATUS_LABELS[b.status]}
            </p>
          ))}

          <p>
            <Link to={`/users/${me.id}/portfolio`}>查看我的作品集 →</Link>
          </p>
        </>
      )}
    </section>
  )
}
