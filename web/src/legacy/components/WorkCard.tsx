import { Link } from 'react-router-dom'
import { CREDIT_ROLE_LABELS, STATUS_LABELS, VALUE_LEVEL_LABELS, DIFFICULTY_LABELS, COMPLETION_LABELS, type WorkView } from '../../types'

/**
 * A work as it appears in the feed: title, attribution, and — for abandoned
 * work — its conclusion. Failed work is shown, not hidden: archiving failure
 * with its reasoning is what makes hard problems safe to attempt.
 *
 * value_level/difficulty/completion are shown here because they are facts
 * about the work (like business_lines), the same way mechanism-design.md
 * §3.1's own worked example lists "承诺价值档:A / 难度档:L / 完成度档:达成"
 * alongside credits. settled_score is a different kind of thing — a
 * computed score, not a graded input — and must NEVER appear here: WorkView
 * (this component's only prop) never carries it at all, because
 * decorate() in internal/legacy/api/feed_handler.go strips it before the feed or
 * any portfolio is ever built. Do not add it back.
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
        {bounty.value_level && <span className="tag">{VALUE_LEVEL_LABELS[bounty.value_level]}</span>}
        {bounty.difficulty && <span className="tag">{DIFFICULTY_LABELS[bounty.difficulty]}</span>}
        {bounty.completion && <span className="tag">{COMPLETION_LABELS[bounty.completion]}</span>}
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
