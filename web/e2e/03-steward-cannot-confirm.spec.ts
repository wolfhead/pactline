import { test, expect } from './support/fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario 3 (spec §6.2): "not even the steward may confirm on someone
 * else's behalf." Viewing another person's pending credit as the steward
 * must offer no confirm or decline control at all.
 */
test('a steward viewing another person\'s pending credit sees no confirm or decline control', async ({
  page,
  api,
  uniqueTitle,
}) => {
  const title = uniqueTitle('E2E steward cannot confirm for others')

  const bounty = await api.createBounty(USERS.sponsorA.id, {
    title,
    goal: 'Exercise the steward-cannot-confirm-for-others rule.',
    acceptance_criteria: 'n/a',
  })
  await api.transition(USERS.sponsorA.id, bounty.id, 'OPEN')
  await api.transition(USERS.engineerC.id, bounty.id, 'CLAIMED')
  await api.transition(USERS.engineerC.id, bounty.id, 'DELIVERED')
  await api.nominate(USERS.engineerC.id, bounty.id, { user_id: USERS.engineerD.id, role: 'SUPPORT' })

  // The identity switcher only mounts after the app has loaded (it waits on
  // GET /api/users), so a page must be navigated to before it can be used.
  await page.goto(`/legacy/bounties/${bounty.id}`)
  await switchIdentity(page, USERS.stewardF.id)

  const creditRow = page.getByRole('listitem').filter({ hasText: USERS.engineerD.name })
  await expect(creditRow).toBeVisible()
  await expect(creditRow.getByText('待确认', { exact: true })).toBeVisible()
  await expect(creditRow.getByRole('button', { name: '确认' })).toHaveCount(0)
  await expect(creditRow.getByRole('button', { name: '拒绝' })).toHaveCount(0)
})
