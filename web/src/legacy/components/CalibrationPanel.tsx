import { useCallback, useEffect, useState } from 'react'
import { apiGet, apiPost } from '../../api/client'
import { useIdentity } from '../../identity'
import { VALUE_LEVELS, VALUE_LEVEL_LABELS, type Bounty, type Calibration, type ValueLevel } from '../../types'

/**
 * Spec §4.6's quarterly value calibration: an after-the-fact correction of a
 * settled bounty's claimed value against what it turned out to be worth.
 * Steward-only (calibrationHandler.create in internal/legacy/api/calibration_handler.go
 * refuses anyone else — the sponsor set the original value, so grading
 * themselves would reintroduce the exact inflation incentive calibration
 * exists to check). Requires a settlement snapshot to exist, so this only
 * renders once the bounty has one.
 *
 * The whole point of a calibration row is the before/after: both the
 * original and the calibrated value/score are shown side by side, never one
 * replacing the other.
 */
export default function CalibrationPanel({ bounty }: { bounty: Bounty }) {
  const { me } = useIdentity()
  const [calibrations, setCalibrations] = useState<Calibration[]>([])
  const [quarter, setQuarter] = useState('')
  const [calibratedValue, setCalibratedValue] = useState<ValueLevel>('S')
  const [note, setNote] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const isSteward = Boolean(me?.roles.includes('STEWARD'))
  const shouldLoad = isSteward && bounty.settled_at != null

  // Guarded inside, not around, the effect: the hook itself must still run
  // on every render (rules of hooks), but there is no reason to hit the
  // network for a viewer who will never see this panel — a non-steward, or
  // a bounty with no settlement snapshot yet.
  const load = useCallback(() => {
    if (!shouldLoad) return
    apiGet<Calibration[]>(`/api/legacy/bounties/${bounty.id}/calibrations`)
      .then(setCalibrations)
      .catch((err) => setError(String(err.message ?? err)))
  }, [bounty.id, shouldLoad])

  useEffect(load, [load])

  if (!shouldLoad) return null

  async function submit() {
    setError('')
    if (!quarter) {
      setError('请填写季度,如 2026Q3')
      return
    }
    setSaving(true)
    try {
      await apiPost(`/api/legacy/bounties/${bounty.id}/calibrations`, {
        quarter,
        calibrated_value: calibratedValue,
        note,
      })
      setQuarter('')
      setNote('')
      load()
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section>
      <h3>价值校准</h3>
      {calibrations.length === 0 && <p className="hint">还没有校准记录。</p>}
      <ul className="credits">
        {calibrations.map((c) => (
          <li key={c.id}>
            <span className="role">{c.quarter}</span>
            <span>
              原:{VALUE_LEVEL_LABELS[c.original_value]}({c.original_score}) → 校准:
              {VALUE_LEVEL_LABELS[c.calibrated_value]}({c.calibrated_score})
            </span>
            {c.note && <span className="tag">{c.note}</span>}
          </li>
        ))}
      </ul>

      <div className="row">
        <input value={quarter} onChange={(e) => setQuarter(e.target.value)} placeholder="季度,如 2026Q3" />
        <select value={calibratedValue} onChange={(e) => setCalibratedValue(e.target.value as ValueLevel)}>
          {VALUE_LEVELS.map((v) => (
            <option key={v} value={v}>{VALUE_LEVEL_LABELS[v]}</option>
          ))}
        </select>
        <input value={note} onChange={(e) => setNote(e.target.value)} placeholder="校准说明(可选)" />
        <button onClick={submit} disabled={saving}>{saving ? '提交中…' : '记录校准'}</button>
      </div>
      {error && <p className="error">{error}</p>}
    </section>
  )
}
