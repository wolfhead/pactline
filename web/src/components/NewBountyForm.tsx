import { useState, type FormEvent } from 'react'
import { apiPost } from '../api/client'
import { VALUE_LEVELS, VALUE_LEVEL_LABELS, type Bounty, type BountyType, type Commitment, type ValueLevel, type Visibility } from '../types'

/**
 * Opening a bounty states a goal, not a decomposed task list. Difficulty is
 * absent on purpose: it is never the sponsor's call, even on their own
 * bounty (domain.CanSetDifficulty), so it is set only from the bounty detail
 * page by a TECH_LEAD or STEWARD. Value level IS the sponsor's call
 * (domain.CanSetValueLevel) and is offered here as optional, exactly
 * mirroring createBountyRequest's ValueLevel field in
 * internal/api/bounty_handler.go — leaving it unset now and setting it later
 * from the detail page (while still DRAFT/OPEN) works just as well.
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
  const [valueLevel, setValueLevel] = useState<ValueLevel | ''>('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')

    const parsed = parseTags(tags)
    if (!parsed.ok) {
      setError(parsed.error)
      return
    }

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
        business_lines: parsed.lines,
        value_level: valueLevel || undefined,
      })
      setTitle('')
      setGoal('')
      setAcceptance('')
      setRestriction('')
      setValueLevel('')
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
        <select value={valueLevel} onChange={(e) => setValueLevel(e.target.value as ValueLevel | '')}>
          <option value="">价值档(可选)</option>
          {VALUE_LEVELS.map((v) => (
            <option key={v} value={v}>{VALUE_LEVEL_LABELS[v]}</option>
          ))}
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

type ParsedTags =
  | { ok: true; lines: { tag: string; weight: number }[] }
  | { ok: false; error: string }

/**
 * parseTags turns "DSP:0.7,ADX:0.3" into weighted business lines.
 *
 * A segment with no colon (e.g. "DSP") defaults to weight 1 — that is the
 * intended shorthand for an unweighted single business line. A segment that
 * *does* carry a colon but whose weight is missing, unparseable, or negative
 * (e.g. "DSP:", "DSP:abc", a typo like "DSP:o.7") is a mistake, not a zero
 * weight, so parsing fails loudly instead of silently coercing it to 0 and
 * letting a mis-attributed bounty through.
 */
function parseTags(raw: string): ParsedTags {
  const lines: { tag: string; weight: number }[] = []
  const seenTags = new Set<string>()

  for (const rawPart of raw.split(',')) {
    const part = rawPart.trim()
    if (!part) continue

    const colonIndex = part.indexOf(':')
    const hasWeight = colonIndex !== -1
    const tag = (hasWeight ? part.slice(0, colonIndex) : part).trim()
    const weightStr = hasWeight ? part.slice(colonIndex + 1).trim() : undefined

    if (!tag) {
      return { ok: false, error: `业务线「${part}」缺少标签名,请检查格式` }
    }

    let weight = 1
    if (weightStr !== undefined) {
      if (weightStr === '') {
        return { ok: false, error: `业务线「${part}」缺少权重,请填写数字或去掉冒号` }
      }
      const parsedWeight = Number(weightStr)
      if (!Number.isFinite(parsedWeight) || parsedWeight < 0) {
        return { ok: false, error: `业务线「${part}」的权重不是合法数字,请检查` }
      }
      weight = parsedWeight
    }

    // Two segments with the same tag must not both be submitted — reject
    // rather than silently merge, so a duplicate never changes weights
    // without the sponsor noticing.
    if (seenTags.has(tag)) {
      return { ok: false, error: `业务线「${tag}」重复出现,请合并或删除后再提交` }
    }
    seenTags.add(tag)
    lines.push({ tag, weight })
  }

  return { ok: true, lines }
}
