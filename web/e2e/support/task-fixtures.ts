import { test as base, expect } from '@playwright/test'
import { Pool } from 'pg'
import * as tasksApi from './tasks-api'
import { DATABASE_URL, WEB_URL } from './config'

/**
 * Fixtures for the task-management e2e specs (10+).
 *
 * Every test here creates its own tasks/labels and must leave the database
 * exactly as it found it. Most task children cascade-delete; acceptance
 * history is retained by design, so task cleanup removes its checks and
 * criteria explicitly before deleting the task.
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
  trackProject: (projectId: string) => void
  ensureProjectMember: (projectId: string, userId: string) => Promise<void>
  tasksApi: TrackedTasksApi
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  page: async ({ page }, use) => {
    const session = await tasksApi.developmentSession()
    await page.context().addCookies([
      { name: 'bb_session', value: session.session, url: WEB_URL, httpOnly: true, sameSite: 'Lax' },
      { name: 'bb_csrf', value: session.csrf, url: WEB_URL, sameSite: 'Lax' },
    ])
    await use(page)
  },

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
      await taskDbPool.query(
        `DELETE FROM acceptance_checks WHERE criterion_id IN (
          SELECT id FROM acceptance_criteria WHERE task_id = ANY($1::uuid[])
        )`,
        [ids],
      )
      await taskDbPool.query(
        'DELETE FROM acceptance_criteria WHERE task_id = ANY($1::uuid[])',
        [ids],
      )
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

  trackProject: async ({ taskDbPool }, use) => {
    const ids: string[] = []
    await use((id: string) => {
      ids.push(id)
    })
    for (const id of ids) {
      await taskDbPool.query(
        `DELETE FROM acceptance_checks WHERE criterion_id IN (
          SELECT id FROM acceptance_criteria
          WHERE milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)
             OR task_id IN (SELECT id FROM tasks WHERE project_id=$1)
        )`,
        [id],
      )
      await taskDbPool.query(
        `DELETE FROM acceptance_criteria
         WHERE milestone_id IN (SELECT id FROM milestones WHERE project_id=$1)
            OR task_id IN (SELECT id FROM tasks WHERE project_id=$1)`,
        [id],
      )
      await taskDbPool.query('DELETE FROM tasks WHERE project_id=$1', [id])
      await taskDbPool.query('DELETE FROM project_activity WHERE project_id=$1', [id])
      await taskDbPool.query('DELETE FROM milestones WHERE project_id=$1', [id])
      await taskDbPool.query('DELETE FROM projects WHERE id=$1', [id])
    }
  },

  ensureProjectMember: async ({ taskDbPool }, use) => {
    const snapshots: Array<{
      projectId: string
      userId: string
      userActive: boolean
      membershipRole: string | null
    }> = []
    await use(async (projectId, userId) => {
      const user = (await taskDbPool.query<{ active: boolean }>(
        'SELECT active FROM users WHERE id=$1',
        [userId],
      )).rows[0]
      if (!user) throw new Error(`project member fixture user ${userId} does not exist`)
      const membership = (await taskDbPool.query<{ role: string }>(
        'SELECT role FROM project_memberships WHERE project_id=$1 AND user_id=$2',
        [projectId, userId],
      )).rows[0]
      snapshots.push({
        projectId,
        userId,
        userActive: user.active,
        membershipRole: membership?.role ?? null,
      })
      await taskDbPool.query('UPDATE users SET active=true, updated_at=now() WHERE id=$1', [userId])
      await taskDbPool.query(
        `INSERT INTO project_memberships (id, project_id, user_id, role)
         VALUES (gen_random_uuid(), $1, $2, 'member')
         ON CONFLICT (project_id, user_id) DO NOTHING`,
        [projectId, userId],
      )
    })
    for (const snapshot of snapshots.reverse()) {
      if (snapshot.membershipRole === null) {
        await taskDbPool.query(
          'DELETE FROM project_memberships WHERE project_id=$1 AND user_id=$2',
          [snapshot.projectId, snapshot.userId],
        )
      } else {
        await taskDbPool.query(
          'UPDATE project_memberships SET role=$3, updated_at=now() WHERE project_id=$1 AND user_id=$2',
          [snapshot.projectId, snapshot.userId, snapshot.membershipRole],
        )
      }
      await taskDbPool.query(
        'UPDATE users SET active=$2, updated_at=now() WHERE id=$1',
        [snapshot.userId, snapshot.userActive],
      )
    }
  },

  tasksApi: async ({ trackTask, trackLabel, trackProject, taskDbPool, runTag }, use) => {
    const defaultProject = (await taskDbPool.query<{ id: string; number: number }>(`
      WITH created AS (
        INSERT INTO projects (id, name, description, creator_id)
        VALUES (gen_random_uuid(), $1, 'Isolated E2E fixture Project',
          '00000000-0000-0000-0000-000000000001')
        RETURNING id, number
      ), membership AS (
        INSERT INTO project_memberships (id, project_id, user_id, role)
        SELECT gen_random_uuid(), id,
          '00000000-0000-0000-0000-000000000001', 'admin'
        FROM created
      )
      SELECT id, number FROM created
    `, [`E2E Project ${runTag}`])).rows[0]
    trackProject(defaultProject.id)
    await use({
      ...tasksApi,
      createTask: async (userId, input) => {
        const task = await tasksApi.createTask(userId, {
          ...input,
          project_number: input.project_number ?? Number(defaultProject.number),
        })
        trackTask(task.id)
        return task
      },
      createLabel: async (userId, name) => {
        const label = await tasksApi.createLabel(userId, name)
        trackLabel(label.id)
        return label
      },
    })
  },
})

export { expect }
