import type { Activity, Actor, TaskThreadItem, TaskThreadItemKind } from '@/task-types'
import { describeActivityChange } from './activity-prose'

export type TimelineActorType = 'user' | 'agent' | 'system'

export interface TimelineActor {
  type: TimelineActorType
  label: string
}

export interface TaskTimelineItem {
  id: string
  source: 'thread' | 'activity'
  kind: TaskThreadItemKind | 'task_change'
  kindLabel: string
  actor: TimelineActor
  occurredAt: string
  body: string
  threadItem?: TaskThreadItem
  metadata?: Record<string, string | null>
}

interface BuildTaskTimelineOptions {
  threadItems: TaskThreadItem[]
  activity: Activity[]
  currentUserID?: string
  userNameById: Record<string, string>
}

export const THREAD_KIND_LABELS: Record<TaskThreadItemKind, string> = {
  message: '讨论',
  progress: '进展',
  handoff: '交接',
  work_submission: '工作提交',
  execution_completed: '完成执行',
  review_outcome: '验收结论',
  resolution_request: '请求解决',
  resolution: '解决进展',
  issue_resolution: 'Issue 结论',
  system_event: '系统事件',
}

const DUPLICATE_WINDOW_MS = 10_000

const THREAD_KINDS_BY_ACTIVITY_FIELD: Record<string, ReadonlySet<TaskThreadItemKind>> = {
  status: new Set(['system_event', 'execution_completed', 'review_outcome', 'issue_resolution']),
  phase: new Set(['system_event', 'execution_completed', 'review_outcome', 'issue_resolution']),
  activity: new Set(['system_event', 'resolution_request', 'resolution', 'issue_resolution']),
  claim: new Set(['system_event', 'handoff', 'execution_completed', 'review_outcome', 'resolution_request']),
  execution_mode: new Set(['system_event', 'handoff']),
  delivery: new Set(['work_submission', 'execution_completed']),
  work_submission: new Set(['work_submission', 'execution_completed']),
  review: new Set(['review_outcome']),
  issue: new Set(['resolution_request', 'resolution', 'issue_resolution']),
  resolution: new Set(['resolution_request', 'resolution', 'issue_resolution']),
}

function dateValue(value: string): number {
  const timestamp = Date.parse(value)
  return Number.isNaN(timestamp) ? Number.MAX_SAFE_INTEGER : timestamp
}

function actorFromThread(
  actor: Actor,
  currentUserID: string | undefined,
  userNameById: Record<string, string>,
): TimelineActor {
  if (actor.type === 'system') return { type: 'system', label: '系统' }
  if (actor.type === 'agent') return { type: 'agent', label: actor.ref || 'Agent' }
  if (actor.user_id === currentUserID) return { type: 'user', label: '你' }
  return { type: 'user', label: userNameById[actor.user_id ?? ''] || '成员' }
}

function actorFromActivity(
  activity: Activity,
  currentUserID: string | undefined,
  userNameById: Record<string, string>,
): TimelineActor {
  if (activity.authentication_method === 'api_token' || activity.authentication_method === 'agent_delegate') {
    return { type: 'agent', label: activity.token_name || 'Agent' }
  }
  return {
    type: 'user',
    label: activity.actor_id === currentUserID ? '你' : userNameById[activity.actor_id] || '成员',
  }
}

function threadCoversActivity(activity: Activity, threadItems: TaskThreadItem[]): boolean {
  const matchingKinds = THREAD_KINDS_BY_ACTIVITY_FIELD[activity.field]
  if (!matchingKinds) return false
  const activityTime = dateValue(activity.created_at)
  return threadItems.some((item) => (
    matchingKinds.has(item.kind)
    && Math.abs(dateValue(item.created_at) - activityTime) <= DUPLICATE_WINDOW_MS
  ))
}

/** Combines the Main Thread with complementary Task changes. Workflow, Claim,
 * delivery, review, and Issue rows are dropped only when a matching Thread
 * event exists at the same event time; ordinary definition/property changes
 * are always retained. */
export function buildTaskTimeline({
  threadItems,
  activity,
  currentUserID,
  userNameById,
}: BuildTaskTimelineOptions): TaskTimelineItem[] {
  const fromThread: TaskTimelineItem[] = threadItems.map((item) => ({
    id: `thread:${item.id}`,
    source: 'thread',
    kind: item.kind,
    kindLabel: THREAD_KIND_LABELS[item.kind],
    actor: actorFromThread(item.author, currentUserID, userNameById),
    occurredAt: item.created_at,
    body: item.body ?? '',
    threadItem: item,
    metadata: {
      claim_id: item.task_stage_claim_id ?? null,
      review_cycle: item.task_review_cycle?.toString() ?? null,
    },
  }))

  const fromActivity: TaskTimelineItem[] = activity
    .filter((entry) => !threadCoversActivity(entry, threadItems))
    .map((entry) => ({
      id: `activity:${entry.id}`,
      source: 'activity',
      kind: 'task_change',
      kindLabel: '任务变更',
      actor: actorFromActivity(entry, currentUserID, userNameById),
      occurredAt: entry.created_at,
      body: describeActivityChange(entry, userNameById),
      metadata: {
        field: entry.field,
        old_value: entry.old_value,
        new_value: entry.new_value,
        request_id: entry.request_id ?? null,
      },
    }))

  return [...fromThread, ...fromActivity].sort((left, right) => {
    const byTime = dateValue(left.occurredAt) - dateValue(right.occurredAt)
    return byTime || left.id.localeCompare(right.id)
  })
}
