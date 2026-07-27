import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { apiDelete, apiGet, apiPost } from '../../api/client'
import { useIdentity } from '../../identity'
import {
  ANCHOR_DIMENSION_LABELS,
  DIFFICULTIES,
  VALUE_LEVELS,
  type AnchorDimension,
  type AnchorExample,
  type SettlementResponse,
} from '../../types'

/**
 * Steward-only tools that are not tied to any single bounty: settlement runs
 * a whole period at once (spec §7.2), and the anchor list is a flat,
 * cross-bounty reference table (spec §4.7). Both therefore live on their own
 * page rather than the bounty detail page, unlike calibration (which grades
 * one specific settled bounty) and the correction channel (which corrects
 * one specific bounty's own fields) — those two stay on BountyDetail.
 */
export default function Steward() {
  const { me } = useIdentity()
  const isSteward = Boolean(me?.roles.includes('STEWARD'))

  if (!isSteward) {
    return <p className="hint">仅 Steward 可访问此页面。</p>
  }

  return (
    <section>
      <h2>Steward 工具</h2>
      <SettlementPanel />
      <AnchorListPanel />
    </section>
  )
}

function SettlementPanel() {
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [result, setResult] = useState<SettlementResponse | null>(null)
  const [error, setError] = useState('')
  const [running, setRunning] = useState(false)

  async function run() {
    setError('')
    if (!from || !to) {
      setError('请填写结算区间的起止日期')
      return
    }
    setRunning(true)
    try {
      const resp = await apiPost<SettlementResponse>('/api/legacy/settlements', {
        from: new Date(from).toISOString(),
        to: new Date(to).toISOString(),
      })
      setResult(resp)
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setRunning(false)
    }
  }

  return (
    <section>
      <h3>月度结算</h3>
      <p className="hint">
        对区间 [起, 止) 内所有终态的单结算一次。已结算的跳过,缺档位的报告为无法计分,两者都不是噪音,是结算报告本身要看的东西。
      </p>
      <div className="row">
        <label>起
          <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        </label>
        <label>止(不含)
          <input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
        </label>
        <button onClick={run} disabled={running}>{running ? '结算中…' : '运行结算'}</button>
      </div>
      {error && <p className="error">{error}</p>}

      {result && (
        <div>
          <h3>已结算（{result.settled_count}）</h3>
          {result.settled.length === 0 && <p className="hint">无</p>}
          <ul className="credits">
            {result.settled.map((s) => (
              <li key={s.bounty_id}>
                <Link to={`/legacy/bounties/${s.bounty_id}`}>{s.title}</Link>
                <span className="tag">{s.score}</span>
              </li>
            ))}
          </ul>

          <h3>已结算,本次跳过（{result.already_settled_count}）</h3>

          <h3>无法计分（{result.unscorable_count}）</h3>
          {result.unscorable.length === 0 && <p className="hint">无</p>}
          <ul className="credits">
            {result.unscorable.map((u) => (
              <li key={u.bounty_id}>
                <Link to={`/legacy/bounties/${u.bounty_id}`}>{u.title}</Link>
                <span className="tag">{u.reason}</span>
              </li>
            ))}
          </ul>

          <h3>结算失败（{result.failed_count}）</h3>
          {result.failed.length === 0 && <p className="hint">无</p>}
          <ul className="credits">
            {result.failed.map((f) => (
              <li key={f.bounty_id}>
                <Link to={`/legacy/bounties/${f.bounty_id}`}>{f.title}</Link>
                <span className="tag">{f.reason}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}

function AnchorListPanel() {
  const [anchors, setAnchors] = useState<AnchorExample[]>([])
  const [dimension, setDimension] = useState<AnchorDimension>('VALUE')
  const [level, setLevel] = useState('S')
  const [bountyId, setBountyId] = useState('')
  const [note, setNote] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const load = useCallback(() => {
    apiGet<AnchorExample[]>('/api/legacy/anchors')
      .then(setAnchors)
      .catch((err) => setError(String(err.message ?? err)))
  }, [])

  useEffect(load, [load])

  const levelOptions = dimension === 'VALUE' ? VALUE_LEVELS : DIFFICULTIES

  async function submit() {
    setError('')
    if (!bountyId) {
      setError('请填写作品的 bounty id')
      return
    }
    setSaving(true)
    try {
      await apiPost('/api/legacy/anchors', { dimension, level, bounty_id: bountyId, note })
      setBountyId('')
      setNote('')
      load()
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setSaving(false)
    }
  }

  async function remove(id: string) {
    setError('')
    try {
      await apiDelete(`/api/legacy/anchors/${id}`)
      load()
    } catch (err) {
      setError(String((err as Error).message))
    }
  }

  return (
    <section>
      <h3>定档锚点清单</h3>
      <p className="hint">每档保留几个先例,定档时引用,让争论收敛而非每季度重打。一个列表,无需流程。</p>

      {anchors.length === 0 && <p className="hint">还没有锚点。</p>}
      <ul className="credits">
        {anchors.map((a) => (
          <li key={a.id}>
            <span className="role">{ANCHOR_DIMENSION_LABELS[a.dimension]}</span>
            <span className="tag">{a.level}</span>
            <Link to={`/legacy/bounties/${a.bounty_id}`}>查看作品</Link>
            {a.note && <span>{a.note}</span>}
            <button onClick={() => remove(a.id)}>删除</button>
          </li>
        ))}
      </ul>

      <div className="row">
        <select
          value={dimension}
          onChange={(e) => {
            const d = e.target.value as AnchorDimension
            setDimension(d)
            setLevel(d === 'VALUE' ? 'S' : 'XS')
          }}
        >
          <option value="VALUE">{ANCHOR_DIMENSION_LABELS.VALUE}</option>
          <option value="DIFFICULTY">{ANCHOR_DIMENSION_LABELS.DIFFICULTY}</option>
        </select>
        <select value={level} onChange={(e) => setLevel(e.target.value)}>
          {levelOptions.map((l) => (
            <option key={l} value={l}>{l}</option>
          ))}
        </select>
        <input value={bountyId} onChange={(e) => setBountyId(e.target.value)} placeholder="作品 bounty id" />
        <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="备注(可选)" />
        <button onClick={submit} disabled={saving}>{saving ? '提交中…' : '添加锚点'}</button>
      </div>
      {error && <p className="error">{error}</p>}
    </section>
  )
}
