import { test, expect } from './support/fixtures'
import { USERS } from './support/config'

/**
 * Scenario 8 (spec §8.1, Portfolio.tsx): a personal page groups a person's
 * works by the role they played, and a work where they hold two roles
 * appears under both groups — because each group answers "what did they do
 * here", not "which single bucket does this work belong to".
 */
test('a work where a person holds two confirmed roles appears under both role groups in their portfolio', async ({
  page,
  api,
  uniqueTitle,
}) => {
  const title = uniqueTitle('E2E portfolio dual role')

  const bounty = await api.createBounty(USERS.sponsorA.id, {
    title,
    goal: 'Exercise dual-role portfolio grouping.',
    acceptance_criteria: 'n/a',
  })
  await api.transition(USERS.sponsorA.id, bounty.id, 'OPEN')
  await api.transition(USERS.engineerD.id, bounty.id, 'CLAIMED')
  await api.transition(USERS.engineerD.id, bounty.id, 'DELIVERED')

  const coDeliver = await api.nominate(USERS.engineerD.id, bounty.id, {
    user_id: USERS.engineerE.id,
    role: 'CO_DELIVER',
  })
  const review = await api.nominate(USERS.engineerD.id, bounty.id, {
    user_id: USERS.engineerE.id,
    role: 'REVIEW',
    evidence: 'https://review.example/mr/2#note-3',
  })
  await api.respond(USERS.engineerE.id, coDeliver.id, 'CONFIRMED')
  await api.respond(USERS.engineerE.id, review.id, 'CONFIRMED')
  await api.transition(USERS.sponsorA.id, bounty.id, 'COMPLETED')

  await page.goto(`/legacy/users/${USERS.engineerE.id}/portfolio`)

  await expect(page.getByRole('heading', { name: /共同交付/ })).toBeVisible()
  await expect(page.getByRole('heading', { name: /深度评审/ })).toBeVisible()
  // The same work's title link appears once per role group: twice in total.
  await expect(page.getByRole('link', { name: title, exact: true })).toHaveCount(2)
})
