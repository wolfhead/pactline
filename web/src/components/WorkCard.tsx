import { Link } from 'react-router-dom'
import { CREDIT_ROLE_LABELS, STATUS_LABELS, type WorkView } from '../types'

/**
 * A work as it appears in the feed: title, attribution, and — for abandoned
 * work — its conclusion. Failed work is shown, not hidden: archiving failure
 * with its reasoning is what makes hard problems safe to attempt.
 */
export default function WorkCard({ work }: { work: WorkView }) {
  const { bounty, credits } = work
  return (
    <article className="card">
      <h3>
        <Link to={`/bounties/${bounty.id}`}>{bounty.title}</Link>
      </h3>
      <div className="meta">
        <span>{STATUS_LABELS[bounty.status]}</span>
        {bounty.business_lines.map((l) => (
          <span key={l.tag} className="tag">
            {l.tag} {Math.round(l.weight * 100)}%
          </span>
        ))}
        {bounty.person_days != null && <span>{bounty.person_days} 人天</span>}
        {bounty.completed_at && <span>{new Date(bounty.completed_at).toLocaleDateString()}</span>}
      </div>

      {credits.length > 0 && (
        <ul className="credits">
          {credits.map((c) => (
            <li key={c.credit.id}>
              <span className="role">{CREDIT_ROLE_LABELS[c.credit.role]}</span>
              <Link to={`/users/${c.credit.user_id}/portfolio`}>{c.user_name}</Link>
            </li>
          ))}
        </ul>
      )}

      {bounty.status === 'ABANDONED' && bounty.retrospective && (
        <div className="retro">结论:{bounty.retrospective}</div>
      )}
    </article>
  )
}
