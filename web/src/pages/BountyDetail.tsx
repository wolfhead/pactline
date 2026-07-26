import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { apiGet, apiPost } from '../api/client'
import CreditPanel from '../components/CreditPanel'
import { STATUS_LABELS, type Bounty, type Status } from '../types'

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
  const [bounty, setBounty] = useState<Bounty | null>(null)
  const [retro, setRetro] = useState('')
  const [personDays, setPersonDays] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(() => {
    if (!id) return
    apiGet<Bounty>(`/api/bounties/${id}`)
      .then((b) => {
        setBounty(b)
        setRetro(b.retrospective ?? '')
        setPersonDays(b.person_days != null ? String(b.person_days) : '')
      })
      .catch((err) => setError(String(err.message ?? err)))
  }, [id])

  useEffect(load, [load])

  async function move(to: Status) {
    setError('')
    try {
      await apiPost(`/api/bounties/${id}/transition`, {
        to,
        retrospective: retro || undefined,
        person_days: personDays ? Number(personDays) : undefined,
      })
      load()
    } catch (err) {
      setError(String((err as Error).message))
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
          <button key={s} onClick={() => move(s)}>转为「{STATUS_LABELS[s]}」</button>
        ))}
        {NEXT_STATES[bounty.status].length === 0 && <span className="hint">已归档为作品,不可再流转。</span>}
      </div>

      {error && <p className="error">{error}</p>}

      <CreditPanel bounty={bounty} onChanged={load} />
    </section>
  )
}
