import { useEffect, useState } from 'react'
import { apiGet } from '../api/client'
import WorkCard from '../components/WorkCard'
import type { WorkView } from '../types'

/**
 * The work feed is the home page. It is deliberately a changelog, not a
 * leaderboard: no ranking, no totals, no aggregate numbers.
 */
export default function WorkFeed() {
  const [works, setWorks] = useState<WorkView[]>([])
  const [error, setError] = useState<string>('')
  const [loading, setLoading] = useState<boolean>(true)

  useEffect(() => {
    apiGet<WorkView[]>('/api/works')
      .then(setWorks)
      .catch((err) => setError(String(err.message ?? err)))
      .finally(() => setLoading(false))
  }, [])

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
