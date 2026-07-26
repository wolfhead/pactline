import { useState, type FormEvent } from 'react'
import { apiPost } from '../api/client'
import type { Bounty, BountyType, Commitment, Visibility } from '../types'

/**
 * Opening a bounty states a goal, not a decomposed task list. Value and
 * difficulty levels are absent on purpose — scoring arrives in Phase 2.
 */
export default function NewBountyForm({ onCreated }: { onCreated: () => void }) {
  const [title, setTitle] = useState('')
  const [goal, setGoal] = useState('')
  const [acceptance, setAcceptance] = useState('')
  const [type, setType] = useState<BountyType>('DELIVERY')
  const [commitment, setCommitment] = useState<Commitment>('COMMITTED')
  const [visibility, setVisibility] = useState<Visibility>('PUBLIC')
  const [restriction, setRestriction] = useState('')
  const [tags, setTags] = useState('DSP:1')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      await apiPost<Bounty>('/api/bounties', {
        type,
        title,
        goal,
        acceptance_criteria: acceptance,
        commitment,
        visibility,
        restriction: visibility === 'RESTRICTED' ? restriction : '',
        business_lines: parseTags(tags),
      })
      setTitle('')
      setGoal('')
      setAcceptance('')
      setRestriction('')
      onCreated()
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit}>
      <h3>开单</h3>
      <input placeholder="标题" value={title} onChange={(e) => setTitle(e.target.value)} required />
      <textarea placeholder="业务目标(不是拆解好的开发任务)" value={goal} rows={3}
        onChange={(e) => setGoal(e.target.value)} />
      <textarea placeholder="验收标准(要求可判定)" value={acceptance} rows={2}
        onChange={(e) => setAcceptance(e.target.value)} />
      <div className="row">
        <select value={type} onChange={(e) => setType(e.target.value as BountyType)}>
          <option value="PLAN">方案单</option>
          <option value="DELIVERY">交付单</option>
        </select>
        <select value={commitment} onChange={(e) => setCommitment(e.target.value as Commitment)}>
          <option value="COMMITTED">承诺型</option>
          <option value="EXPLORATORY">探索型(允许失败)</option>
        </select>
        <input value={tags} onChange={(e) => setTags(e.target.value)}
          placeholder="业务线,如 DSP:0.7,ADX:0.3 或 PLATFORM:1" />
        <select value={visibility} onChange={(e) => setVisibility(e.target.value as Visibility)}>
          <option value="PUBLIC">公开池</option>
          <option value="RESTRICTED">限定池</option>
        </select>
      </div>
      {visibility === 'RESTRICTED' && (
        <input value={restriction} onChange={(e) => setRestriction(e.target.value)}
          placeholder="限定条件——写上下文,不写职级(例:需要 Bidding Engine 上下文)" />
      )}
      {error && <p className="error">{error}</p>}
      <button type="submit" disabled={busy}>{busy ? '提交中…' : '创建草稿'}</button>
    </form>
  )
}

/** parseTags turns "DSP:0.7,ADX:0.3" into weighted business lines. */
function parseTags(raw: string) {
  return raw
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part) => {
      const [tag, weight] = part.split(':')
      return { tag: tag.trim(), weight: Number(weight ?? 1) || 0 }
    })
}
