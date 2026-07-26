import { test as base, expect } from '@playwright/test'
import { Pool } from 'pg'
import * as api from './api'
import { DATABASE_URL } from './config'

/**
 * Every test creates its own bounties and must leave the database exactly as
 * it found it (see the brief: "after the whole suite runs, bounties and
 * credits row counts must be unchanged"). Credits cascade-delete with their
 * bounty (migrations/0001_init.sql: `credits.bounty_id ... ON DELETE
 * CASCADE`), so cleanup only ever needs to delete rows from `bounties`.
 *
 * `api.createBounty` is wrapped below to auto-register the new bounty for
 * that deletion. The one test that creates a bounty purely through the UI
 * (no API call, by design — it is the single true end-to-end path) has
 * nothing to wrap, so it calls the `trackBounty` fixture directly with the
 * id parsed out of the URL after navigating to the new bounty's page.
 */

type TrackedApi = typeof api

interface WorkerFixtures {
  dbPool: Pool
}

interface TestFixtures {
  runTag: string
  uniqueTitle: (base: string) => string
  trackBounty: (bountyId: string) => void
  api: TrackedApi
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  // Worker-scoped: one pg connection pool per test worker process, closed
  // once when the worker shuts down rather than reconnected per test.
  dbPool: [
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

  trackBounty: async ({ dbPool }, use) => {
    const ids: string[] = []
    await use((id: string) => {
      ids.push(id)
    })
    if (ids.length > 0) {
      await dbPool.query('DELETE FROM bounties WHERE id = ANY($1::uuid[])', [ids])
    }
  },

  api: async ({ trackBounty }, use) => {
    await use({
      ...api,
      createBounty: async (sponsorId, input) => {
        const bounty = await api.createBounty(sponsorId, input)
        trackBounty(bounty.id)
        return bounty
      },
    })
  },
})

export { expect }
