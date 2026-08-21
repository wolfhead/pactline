import { describe, expect, it } from 'vitest'
import type { Activity, TaskThreadItem } from '@/task-types'
import { buildTaskTimeline } from './task-timeline'

function threadItem(overrides: Partial<TaskThreadItem> & Pick<TaskThreadItem, 'id' | 'kind' | 'created_at'>): TaskThreadItem {
  return {
    thread_id: 'thread-main',
    author: { type: 'system' },
    body: '',
    mentioned_user_ids: [],
    version: 1,
    updated_at: overrides.created_at,
    ...overrides,
  }
}

function activity(overrides: Partial<Activity> & Pick<Activity, 'id' | 'field' | 'created_at'>): Activity {
  return {
    actor_id: 'user-2',
    old_value: null,
    new_value: null,
    ...overrides,
  }
}

describe('buildTaskTimeline', () => {
  it('orders fixed timestamps and preserves kind and user, agent, and system provenance', () => {
    const timeline = buildTaskTimeline({
      currentUserID: 'user-1',
      userNameById: { 'user-1': 'Alex', 'user-2': 'Riley' },
      threadItems: [
        threadItem({
          id: 'system-latest',
          kind: 'system_event',
          created_at: '2026-08-20T10:03:00Z',
          body: 'Task marked ready',
        }),
        threadItem({
          id: 'agent-middle',
          kind: 'progress',
          created_at: '2026-08-20T10:02:00Z',
          author: { type: 'agent', ref: 'api-token/codex' },
          body: 'Implementation is underway.',
        }),
        threadItem({
          id: 'user-first',
          kind: 'message',
          created_at: '2026-08-20T10:00:00Z',
          author: { type: 'user', user_id: 'user-1' },
          body: 'Please preserve the Issue workflow.',
        }),
      ],
      activity: [activity({
        id: 'property-between',
        field: 'priority',
        created_at: '2026-08-20T10:01:00Z',
        authentication_method: 'api_token',
        token_name: 'release-agent',
        old_value: 'medium',
        new_value: 'high',
      })],
    })

    expect(timeline.map(({ id }) => id)).toEqual([
      'thread:user-first',
      'activity:property-between',
      'thread:agent-middle',
      'thread:system-latest',
    ])
    expect(timeline.map(({ kindLabel }) => kindLabel)).toEqual(['讨论', '任务变更', '进展', '系统事件'])
    expect(timeline.map(({ actor }) => actor)).toEqual([
      { type: 'user', label: '你' },
      { type: 'agent', label: 'release-agent' },
      { type: 'agent', label: 'api-token/codex' },
      { type: 'system', label: '系统' },
    ])
  })

  it('removes duplicate lifecycle, Claim, delivery, review, and Issue activity while retaining property changes', () => {
    const eventTime = '2026-08-20T10:00:00Z'
    const timeline = buildTaskTimeline({
      userNameById: { 'user-2': 'Riley' },
      threadItems: [
        threadItem({ id: 'phase', kind: 'system_event', created_at: eventTime, body: 'Task marked ready' }),
        threadItem({ id: 'delivery', kind: 'work_submission', created_at: eventTime, body: 'Delivered.' }),
        threadItem({ id: 'review', kind: 'review_outcome', created_at: eventTime, body: 'Accepted.' }),
        threadItem({ id: 'issue', kind: 'issue_resolution', created_at: eventTime }),
      ],
      activity: [
        activity({ id: 'duplicate-phase', field: 'status', created_at: eventTime }),
        activity({ id: 'duplicate-claim', field: 'execution_mode', created_at: eventTime }),
        activity({ id: 'ordinary-title', field: 'title', created_at: eventTime, old_value: 'Before', new_value: 'After' }),
        activity({ id: 'ordinary-priority', field: 'priority', created_at: eventTime, old_value: 'low', new_value: 'high' }),
      ],
    })

    expect(timeline.map(({ id }) => id)).not.toContain('activity:duplicate-phase')
    expect(timeline.map(({ id }) => id)).not.toContain('activity:duplicate-claim')
    expect(timeline.map(({ id }) => id)).toEqual(expect.arrayContaining([
      'activity:ordinary-title',
      'activity:ordinary-priority',
    ]))
  })

  it('keeps an otherwise overlapping activity row when its Thread source is absent or unrelated in time', () => {
    const timeline = buildTaskTimeline({
      userNameById: {},
      threadItems: [threadItem({
        id: 'older-event',
        kind: 'system_event',
        created_at: '2026-08-20T09:00:00Z',
      })],
      activity: [activity({
        id: 'later-status',
        field: 'status',
        created_at: '2026-08-20T10:00:00Z',
        old_value: 'todo',
        new_value: 'in_progress',
      })],
    })

    expect(timeline.map(({ id }) => id)).toContain('activity:later-status')
  })
})
