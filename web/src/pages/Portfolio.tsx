import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { apiGet } from '../api/client'
import WorkCard from '../components/WorkCard'
import { useIdentity } from '../identity'
import { CREDIT_ROLE_LABELS, type CreditRole, type WorkView } from '../types'

// C1: mirrors creditRoleOrder in internal/api/feed_handler.go. The two must
// be changed together, or a person's role groups here would list in a
// different order than the credit roll within each work.
const ROLE_ORDER: CreditRole[] = ['DEFINE', 'LEAD', 'CO_DELIVER', 'REVIEW', 'SUPPORT', 'BASELINE']

/**
 * A personal page is the set of works someone is credited on — not a score
 * card. Grouping by role shows what parts they play, without ranking anyone.
 * A work where the person holds two roles on the same bounty appears under
 * both groups, since each group answers "what did they do here", not "which
 * single bucket does this work belong to".
 */
export default function Portfolio() {
  const { id } = useParams<{ id: string }>()
  const { users } = useIdentity()
  const [works, setWorks] = useState<WorkView[]>([])
  const [error, setError] = useState('')
  // Distinct from the empty-state check below: WorkFeed/Board/BountyDetail
  // all fixed a defect where the empty-state text rendered before the
  // initial request resolved. Mirror that fix here rather than
  // reintroducing it.
  const [loading, setLoading] = useState<boolean>(true)

  // Guards against request cancellation (C2): if the route id changes again
  // before this request resolves — e.g. the user navigates from one
  // person's portfolio to another's — `cancelled` is set in the cleanup
  // below, so a slow stale response can never overwrite a newer one's
  // result. Without this, a slow response for a previous id could resolve
  // after a faster one for the current id and render one person's works
  // under another person's heading — the worst version of this bug for an
  // attribution system. Mirrors the cancelled-flag idiom in identity.tsx.
  useEffect(() => {
    if (!id) return
    setLoading(true)
    setError('')
    let cancelled = false
    apiGet<WorkView[]>(`/api/users/${id}/portfolio`)
      .then((loaded) => {
        if (cancelled) return
        setWorks(loaded)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String(err.message ?? err))
      })
      .finally(() => {
        if (cancelled) return
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [id])

  const person = users.find((u) => u.id === id)

  if (loading) return <p className="hint">加载中…</p>
  if (error) return <p className="error">{error}</p>

  const byRole = ROLE_ORDER.map((role) => ({
    role,
    works: works.filter((w) => w.credits.some((c) => c.credit.user_id === id && c.credit.role === role)),
  })).filter((g) => g.works.length > 0)

  return (
    <section>
      {/* C3: falls back to '成员' for an unknown user id, never the raw
          UUID. Mine.tsx and CreditPanel.tsx use the same fallback. */}
      <h2>{person?.name ?? '成员'} 的作品集</h2>
      {works.length === 0 && <p className="hint">还没有已确认署名的作品。</p>}
      {byRole.map((g) => (
        <div key={g.role}>
          <h3>
            {CREDIT_ROLE_LABELS[g.role]}（{g.works.length}）
          </h3>
          {g.works.map((w) => (
            <WorkCard key={w.bounty.id} work={w} />
          ))}
        </div>
      ))}
    </section>
  )
}
