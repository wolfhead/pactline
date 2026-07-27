import { useEffect, useState } from 'react'
import { apiGet } from '../api/client'
import WorkCard from '../components/WorkCard'
import { useIdentity } from '../identity'
import type { WorkView } from '../types'

/**
 * The work feed is the home page. It is deliberately a changelog, not a
 * leaderboard: no ranking, no totals, no aggregate numbers.
 */
export default function WorkFeed() {
  const { me } = useIdentity()
  const [works, setWorks] = useState<WorkView[]>([])
  const [error, setError] = useState<string>('')
  const [loading, setLoading] = useState<boolean>(true)

  // Fetches on mount and whenever the current identity changes — switching
  // identity in the header must refetch an already-mounted page rather than
  // leave the previous identity's rows on screen (visibility such as
  // RESTRICTED business lines depends on who's asking). Re-enters the
  // loading state on every run so the stale response's rows are never
  // rendered as current while the new request is in flight, and guards
  // against request cancellation (C2): if identity changes again before
  // this request resolves, `cancelled` is set in the cleanup below, so a
  // slow stale response can never overwrite a newer one's result. Mirrors
  // the cancelled-flag idiom in identity.tsx / Board.tsx / Portfolio.tsx.
  useEffect(() => {
    setLoading(true)
    setError('')
    let cancelled = false
    apiGet<WorkView[]>('/api/works')
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
  }, [me?.id])

  if (loading) return <p className="hint">正在加载作品流…</p>
  if (error) return <p className="error">加载失败:{error}</p>
  if (works.length === 0) return <p className="hint">还没有已完成的作品。</p>

  return (
    <section>
      <h2>作品流</h2>
      {works.map((w) => (
        <WorkCard key={w.bounty.id} work={w} />
      ))}
    </section>
  )
}
