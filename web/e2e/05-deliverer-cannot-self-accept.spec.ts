import { test, expect } from './support/fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario 5 (spec §6.1): acceptance is the sponsor's (or a steward's) call.
 * The claimer who delivered the work must not be able to move it to
 * COMPLETED themselves; the sponsor must be able to.
 */
test('the deliverer cannot accept their own delivery; the sponsor can', async ({ page, api, uniqueTitle }) => {
  const title = uniqueTitle('E2E deliverer cannot self accept')

  const bounty = await api.createBounty(USERS.sponsorA.id, {
    title,
    goal: 'Exercise the self-acceptance restriction.',
    acceptance_criteria: 'n/a',
  })
  await api.transition(USERS.sponsorA.id, bounty.id, 'OPEN')
  await api.transition(USERS.engineerC.id, bounty.id, 'CLAIMED')
  await api.transition(USERS.engineerC.id, bounty.id, 'DELIVERED')

  // The claimer tries to accept their own delivery: refused. (The identity
  // switcher only mounts after the app has loaded, so navigate first.)
  await page.goto(`/legacy/bounties/${bounty.id}`)
  await switchIdentity(page, USERS.engineerC.id)
  await page.getByRole('button', { name: '转为「已完成」' }).click()
  await expect(page.getByText('forbidden', { exact: true })).toBeVisible()
  await expect(page.getByText('待验收', { exact: true })).toBeVisible() // still DELIVERED

  // The sponsor accepts it: succeeds.
  await switchIdentity(page, USERS.sponsorA.id)
  await page.getByRole('button', { name: '转为「已完成」' }).click()
  await expect(page.getByText('已归档为作品,不可再流转。')).toBeVisible()
})
