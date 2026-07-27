import { test, expect } from './support/fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario 1 (spec §5 lifecycle, §8.1): the entire loop driven through the
 * UI alone — open, publish, claim, deliver, accept, appears in the feed.
 * This is deliberately the one test in the suite with no API shortcuts: it
 * is the actual proof that the application works end to end in a browser.
 */
test('sponsor opens a bounty, publishes it, an engineer claims and delivers it, the sponsor accepts it, and it appears in the feed', async ({
  page,
  uniqueTitle,
  trackBounty,
}) => {
  const title = uniqueTitle('E2E full loop')

  // The identity switcher only mounts after the app has loaded (it waits on
  // GET /api/users), so a page must be navigated to before it can be used.
  await page.goto('/legacy/board')
  await switchIdentity(page, USERS.sponsorA.id)

  await page.getByPlaceholder('标题').fill(title)
  await page.getByPlaceholder('业务目标(不是拆解好的开发任务)').fill('Prove the whole loop works in a real browser.')
  await page.getByPlaceholder('验收标准(要求可判定)').fill('The finished work shows up in the feed.')
  await page.getByRole('button', { name: '创建草稿' }).click()

  // The board reloads after creation; the new draft's title becomes a link.
  await page.getByRole('link', { name: title, exact: true }).click()
  await expect(page).toHaveURL(/\/bounties\/[0-9a-f-]+$/)
  const bountyId = page.url().split('/bounties/')[1]
  trackBounty(bountyId)

  await expect(page.getByText('草稿', { exact: true })).toBeVisible()

  // Publish: DRAFT -> OPEN.
  await page.getByRole('button', { name: '转为「可认领」' }).click()
  await expect(page.getByText('可认领', { exact: true })).toBeVisible()

  // An engineer claims it: OPEN -> CLAIMED.
  await switchIdentity(page, USERS.engineerC.id)
  await page.getByRole('button', { name: '转为「已认领」' }).click()
  await expect(page.getByText('已认领', { exact: true })).toBeVisible()

  // The engineer hands it in: CLAIMED -> DELIVERED.
  await page.getByRole('button', { name: '转为「待验收」' }).click()
  await expect(page.getByText('待验收', { exact: true })).toBeVisible()

  // The sponsor accepts it: DELIVERED -> COMPLETED.
  await switchIdentity(page, USERS.sponsorA.id)
  await page.getByRole('button', { name: '转为「已完成」' }).click()
  await expect(page.getByText('已归档为作品,不可再流转。')).toBeVisible()

  // The finished work shows up in the feed.
  await page.goto('/legacy')
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()
})
