import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiGet } from '../api/client'
import NewBountyForm from '../components/NewBountyForm'
import { useIdentity } from '../identity'
import { STATUS_LABELS, type Bounty, type Status } from '../types'

const BOARD_STATUSES: Status[] = ['DRAFT', 'OPEN', 'CLAIMED', 'DELIVERED']

export default function Board() {
  const { me } = useIdentity()
  const [bounties, setBounties] = useState<Bounty[]>([])
  const [tag, setTag] = useState('')
  const [error, setError] = useState('')
  // Distinct from the empty-state check below: WorkFeed (Task 12) fixed a
  // defect where the empty-state text rendered before the initial request
  // resolved. Mirror that fix here rather than reintroducing it.
  const [loading, setLoading] = useState<boolean>(true)

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    const params = new URLSearchParams()
    BOARD_STATUSES.forEach((s) => params.append('status', s))
    if (tag) params.set('tag', tag)
    apiGet<Bounty[]>(`/api/bounties?${params}`)
      .then(setBounties)
      .catch((err) => setError(String(err.message ?? err)))
      .finally(() => setLoading(false))
  }, [tag])

  useEffect(load, [load])

  const canOpen = me?.roles.includes('SPONSOR') || me?.roles.includes('STEWARD')

  return (
    <section>
      <h2>Board</h2>
      <div className="row">
        <label>
          业务线筛选
          <input value={tag} onChange={(e) => setTag(e.target.value)} placeholder="DSP / ADX / PLATFORM" />
        </label>
      </div>

      {canOpen && <NewBountyForm onCreated={load} />}

      {loading && <p className="hint">正在加载看板…</p>}
      {!loading && error && <p className="error">加载失败:{error}</p>}
      {!loading && !error && bounties.length === 0 && <p className="hint">没有符合条件的单。</p>}

      {!loading && !error && bounties.map((b) => (
        <article key={b.id} className="card">
          <h3>
            <Link to={`/bounties/${b.id}`}>{b.title}</Link>
          </h3>
          <div className="meta">
            <span>{STATUS_LABELS[b.status]}</span>
            <span>{b.type === 'PLAN' ? '方案单' : '交付单'}</span>
            <span>{b.commitment === 'EXPLORATORY' ? '探索型' : '承诺型'}</span>
            {b.business_lines.map((l) => (
              <span key={l.tag} className="tag">{l.tag}</span>
            ))}
            {b.visibility === 'RESTRICTED' && b.restriction && <span>限定:{b.restriction}</span>}
          </div>
          {b.goal && <p>{b.goal}</p>}
        </article>
      ))}
    </section>
  )
}
