import { useEffect, useMemo, useState } from 'react'
import { listActivity } from '../../api/tasks'
import { useIdentity } from '../../identity'
import type { Task } from '../../task-types'
import { describeActivity } from './activity-prose'
import type { Activity } from '../../task-types'

interface ActivityLogProps {
  task: Task
}

/** The activity history as readable prose, not a raw field dump — each
 * entry reads like "王芳 将状态从「待办」改为「进行中」", newest first. */
export default function ActivityLog({ task }: ActivityLogProps) {
  const { me, users } = useIdentity()
  const [activity, setActivity] = useState<Activity[]>([])
  const [error, setError] = useState('')

  // Fetches whenever the task changes, is mutated, or identity changes,
  // guarded against out-of-order resolution — mirrors the cancelled-flag
  // idiom in identity.tsx / WorkFeed.tsx / Board.tsx. `task.updated_at` (not
  // just `task.number`) is a dependency because every server-confirmed
  // mutation (status, priority, assignee, due date, labels, title,
  // description, archive/restore — see task_store.go's `updated_at=now()`
  // on each of those) bumps it, so an on-page change re-triggers this fetch
  // without polling or refetching on every unrelated render.
  useEffect(() => {
    let cancelled = false
    listActivity(task.number)
      .then((loaded) => {
        if (cancelled) return
        setActivity(loaded)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
    return () => {
      cancelled = true
    }
  }, [task.number, task.updated_at, me?.id])

  // Assignee/actor activity entries carry bare user UUIDs (see
  // activity-prose.ts); resolve names from the active-user roster, filling
  // in the task's own creator/assignee too since a since-deactivated user
  // still needs their name shown on history they made while active.
  const userNameById = useMemo(() => {
    const map: Record<string, string> = {}
    for (const u of users) map[u.id] = u.name
    map[task.creator.id] = task.creator.name
    if (task.assignee) map[task.assignee.id] = task.assignee.name
    return map
  }, [users, task.creator, task.assignee])

  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-sm font-medium text-fg">历史记录</h3>
      {error && <p className="text-sm text-danger">{error}</p>}
      {activity.length === 0 && !error && <p className="text-sm text-fg-muted">还没有记录。</p>}
      <ul className="flex flex-col gap-1.5">
        {[...activity].reverse().map((a) => (
          <li key={a.id} className="text-sm text-fg">
            <span>{describeActivity(a, userNameById[a.actor_id] ?? '某用户', userNameById)}</span>
            <span className="text-fg-muted"> · {new Date(a.created_at).toLocaleString()}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}
