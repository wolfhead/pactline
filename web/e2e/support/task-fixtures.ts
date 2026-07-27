import { test as base, expect } from '@playwright/test'
import { Pool } from 'pg'
import * as tasksApi from './tasks-api'
import { DATABASE_URL } from './config'

/**
 * Fixtures for the task-management e2e specs (10+), parallel to
 * e2e/support/fixtures.ts which the legacy bounty specs use. Kept as a
 * separate file rather than extending the existing one: the two domains
 * have unrelated schemas (tasks/labels/task_comments/task_activity vs
 * bounties/credits) and unrelated cleanup targets, and the brief requires
 * the legacy specs stay untouched.
 *
 * Every test here creates its own tasks/labels and must leave the database
 * exactly as it found it. Comments and activity rows cascade-delete with
 * their task (migrations/0005_tasks.sql: `task_comments.task_id ... ON
 * DELETE CASCADE`, same for task_activity and task_labels), so cleanup only
 * ever needs to delete rows from `tasks` and `labels` directly.
 */

type TrackedTasksApi = typeof tasksApi

interface WorkerFixtures {
  taskDbPool: Pool
}

interface TestFixtures {
  runTag: string
  uniqueTitle: (base: string) => string
  trackTask: (taskId: string) => void
  trackLabel: (labelId: string) => void
  tasksApi: TrackedTasksApi
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  // Worker-scoped: one pg connection pool per test worker process, closed
  // once when the worker shuts down rather than reconnected per test.
  taskDbPool: [
    async ({}, use) => {
      const pool = new Pool({ connectionString: DATABASE_URL })
      await use(pool)
      await pool.end()
    },
    { scope: 'worker' },
  ],

  // A short id unique to this test run, embedded in every title this test
  // creates, so assertions can scope to "the thing this test made" instead
  // of ever touching a global count (rows a human left behind must not
  // affect, or be affected by, this suite).
  runTag: async ({}, use, testInfo) => {
    const tag = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}-w${testInfo.workerIndex}`
    await use(tag)
  },

  uniqueTitle: async ({ runTag }, use) => {
    let n = 0
    await use((label: string) => `${label} [e2e ${runTag}-${n++}]`)
  },

  trackTask: async ({ taskDbPool }, use) => {
    const ids: string[] = []
    await use((id: string) => {
      ids.push(id)
    })
    if (ids.length > 0) {
      await taskDbPool.query('DELETE FROM tasks WHERE id = ANY($1::uuid[])', [ids])
    }
  },

  trackLabel: async ({ taskDbPool }, use) => {
    const ids: string[] = []
    await use((id: string) => {
      ids.push(id)
    })
    if (ids.length > 0) {
      await taskDbPool.query('DELETE FROM labels WHERE id = ANY($1::uuid[])', [ids])
    }
  },

  tasksApi: async ({ trackTask, trackLabel }, use) => {
    await use({
      ...tasksApi,
      createTask: async (userId, input) => {
        const task = await tasksApi.createTask(userId, input)
        trackTask(task.id)
        return task
      },
      createLabel: async (userId, name) => {
        const label = await tasksApi.createLabel(userId, name)
        trackLabel(label.ID)
        return label
      },
    })
  },
})

export { expect }
