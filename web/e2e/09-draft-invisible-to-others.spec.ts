import { test, expect } from './support/fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario 9 (spec §5, bounty_handler.go canViewDraft): a DRAFT bounty is
 * visible only to its own sponsor or a steward. Another engineer must see
 * it neither on the Board nor by navigating to it directly — and the
 * sponsor who owns it must still see it (positive control, so the negative
 * checks above are not simply checking a typo'd title).
 */
test('a sponsor\'s draft is invisible to another engineer, on the Board and by direct link', async ({
  page,
  api,
  uniqueTitle,
}) => {
  const title = uniqueTitle('E2E draft invisible to others')

  const bounty = await api.createBounty(USERS.sponsorA.id, {
    title,
    goal: 'Exercise draft visibility.',
    acceptance_criteria: 'n/a',
  })
  // Left in DRAFT on purpose — no transition applied.

  // The identity switcher only mounts after the app has loaded (it waits on
  // GET /api/users), so a page must be navigated to before it can be used.
  await page.goto('/board')
  await switchIdentity(page, USERS.engineerD.id)
  // Board's own fetch effect depends only on [tag, reloadToken], not on
  // identity — switching identity updates the header used by future
  // requests but does not by itself refetch an already-mounted page. Force
  // a fresh navigation so the list is actually fetched as engineerD.
  await page.goto('/board')
  await expect(page.getByRole('heading', { name: title, exact: true })).toHaveCount(0)

  await page.goto(`/bounties/${bounty.id}`)
  await expect(page.getByRole('heading', { name: title, exact: true })).toHaveCount(0)
  await expect(page.getByText('not found', { exact: true })).toBeVisible()

  // Positive control: the sponsor who owns it still sees it.
  await switchIdentity(page, USERS.sponsorA.id)
  await page.goto(`/bounties/${bounty.id}`)
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()
})
