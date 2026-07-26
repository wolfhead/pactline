import { test, expect } from './support/fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario 4 (spec §3.3, §5): failure is archived, never hidden. Abandoning
 * with an empty conclusion must be refused; supplying one must succeed and
 * the work must appear in the feed showing that exact conclusion.
 */
test('abandoning without a conclusion is refused; with one it succeeds and the feed shows the conclusion', async ({
  page,
  api,
  uniqueTitle,
}) => {
  const title = uniqueTitle('E2E abandon with conclusion')
  const conclusion = `Blocked on an upstream dependency that never landed — abandoning [${title}]`

  const bounty = await api.createBounty(USERS.sponsorA.id, {
    title,
    goal: 'Attempt a risky approach that may not pan out.',
    acceptance_criteria: 'n/a',
    commitment: 'EXPLORATORY',
  })
  await api.transition(USERS.sponsorA.id, bounty.id, 'OPEN')
  await api.transition(USERS.engineerC.id, bounty.id, 'CLAIMED')

  // The identity switcher only mounts after the app has loaded (it waits on
  // GET /api/users), so a page must be navigated to before it can be used.
  await page.goto(`/bounties/${bounty.id}`)
  await switchIdentity(page, USERS.engineerC.id)

  // Empty conclusion: refused.
  await page.getByRole('button', { name: '转为「已放弃」' }).click()
  await expect(page.getByText('retrospective is required when abandoning')).toBeVisible()
  await expect(page.getByText('已认领', { exact: true })).toBeVisible() // still CLAIMED

  // With a conclusion: succeeds.
  await page.getByLabel('结论 / 复盘（放弃时必填）').fill(conclusion)
  await page.getByRole('button', { name: '转为「已放弃」' }).click()
  await expect(page.getByText('已归档为作品,不可再流转。')).toBeVisible()

  // The feed shows the abandoned work with its conclusion.
  await page.goto('/')
  const card = page.getByRole('article').filter({ has: page.getByRole('heading', { name: title, exact: true }) })
  await expect(card).toBeVisible()
  await expect(card.getByText('已放弃', { exact: true })).toBeVisible()
  await expect(card).toContainText(`结论:${conclusion}`)
})
