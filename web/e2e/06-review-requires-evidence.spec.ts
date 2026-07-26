import { test, expect } from './support/fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario 6 (spec §4.3, §6.2): a REVIEW credit without evidence would
 * degrade into mutual credit-clicking, so it is the one role the backend
 * refuses without evidence. Nominating with empty evidence must be refused
 * with the message shown; supplying evidence must succeed.
 */
test('nominating a REVIEW credit with empty evidence is refused with the evidence message', async ({
  page,
  api,
  uniqueTitle,
}) => {
  const title = uniqueTitle('E2E review requires evidence')

  const bounty = await api.createBounty(USERS.sponsorA.id, {
    title,
    goal: 'Exercise the REVIEW-requires-evidence rule.',
    acceptance_criteria: 'n/a',
  })
  await api.transition(USERS.sponsorA.id, bounty.id, 'OPEN')
  await api.transition(USERS.engineerC.id, bounty.id, 'CLAIMED')
  await api.transition(USERS.engineerC.id, bounty.id, 'DELIVERED')

  // The identity switcher only mounts after the app has loaded (it waits on
  // GET /api/users), so a page must be navigated to before it can be used.
  await page.goto(`/bounties/${bounty.id}`)
  await switchIdentity(page, USERS.engineerC.id)

  const memberSelect = page.getByRole('combobox').filter({ has: page.getByRole('option', { name: '选择成员…' }) })
  const roleSelect = page.getByRole('combobox').filter({ has: page.getByRole('option', { name: '深度评审' }) })

  await memberSelect.selectOption({ label: USERS.engineerD.name })
  await roleSelect.selectOption({ label: '深度评审' })

  // Empty evidence: refused, with the message shown.
  await page.getByRole('button', { name: '提名' }).click()
  await expect(page.getByText('evidence is required for REVIEW credit')).toBeVisible()

  // With evidence: succeeds.
  await page.getByPlaceholder('评审意见链接(REVIEW 必填)').fill('https://review.example/mr/1#note-1')
  await page.getByRole('button', { name: '提名' }).click()

  const creditRow = page.getByRole('listitem').filter({ hasText: USERS.engineerD.name })
  await expect(creditRow.getByText('深度评审', { exact: true })).toBeVisible()
  await expect(creditRow.getByText('待确认', { exact: true })).toBeVisible()
})
