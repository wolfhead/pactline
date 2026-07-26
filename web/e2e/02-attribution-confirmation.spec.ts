import { test, expect } from './support/fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario 2 (spec §4.3, §6.2): a nominated credit is the mechanism's one
 * hard constraint — invisible everywhere until the nominee confirms it
 * themselves. Setup (open, claim, deliver, nominate, accept) goes through
 * the API since only the confirmation step and the feed are under test;
 * every assertion runs in the browser.
 */
test('a nominated credit is absent from the feed until the nominee confirms it, then appears', async ({
  page,
  api,
  uniqueTitle,
}) => {
  const title = uniqueTitle('E2E attribution confirmation')

  const bounty = await api.createBounty(USERS.sponsorA.id, {
    title,
    goal: 'Exercise credit confirmation end to end.',
    acceptance_criteria: 'The nominee has confirmed the credit.',
  })
  await api.transition(USERS.sponsorA.id, bounty.id, 'OPEN')
  await api.transition(USERS.engineerC.id, bounty.id, 'CLAIMED')
  await api.transition(USERS.engineerC.id, bounty.id, 'DELIVERED')
  await api.nominate(USERS.engineerC.id, bounty.id, { user_id: USERS.engineerD.id, role: 'CO_DELIVER' })
  await api.transition(USERS.sponsorA.id, bounty.id, 'COMPLETED')

  // Before confirmation: the work is already in the feed (it is COMPLETED),
  // but the nominee's name is nowhere on its card.
  await page.goto('/')
  const card = page.getByRole('article').filter({ has: page.getByRole('heading', { name: title, exact: true }) })
  await expect(card).toBeVisible()
  await expect(card.getByText(USERS.engineerD.name)).toHaveCount(0)

  // The nominee confirms through "我的".
  await switchIdentity(page, USERS.engineerD.id)
  await page.goto('/mine')
  const pendingRow = page.getByRole('listitem').filter({ hasText: '共同交付' })
  await expect(pendingRow).toBeVisible()
  await pendingRow.getByRole('button', { name: '确认' }).click()
  await expect(page.getByText('没有待确认的署名。')).toBeVisible()

  // After confirmation: the name appears on the work in the feed.
  await page.goto('/')
  const cardAfter = page.getByRole('article').filter({ has: page.getByRole('heading', { name: title, exact: true }) })
  await expect(cardAfter.getByRole('link', { name: USERS.engineerD.name, exact: true })).toBeVisible()
})
