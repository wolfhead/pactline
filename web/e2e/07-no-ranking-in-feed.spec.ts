import { test, expect } from './support/fixtures'
import { USERS } from './support/config'

/**
 * Scenario 7 (spec §2, §8.1): the feed is "a changelog / exhibition" and
 * deliberately produces no ranking. This is asserted concretely rather than
 * trivially: a leaderboard would surface as specific vocabulary (rank,
 * leaderboard, total/aggregate score) or a sortable table, so the test
 * builds a feed with a real completed, credited work and checks that exact
 * vocabulary and structure are absent — a test that would actually fail if
 * a ranking feature were bolted onto this page.
 */
test('the feed shows completed work without any ranking or aggregate-score vocabulary or structure', async ({
  page,
  api,
  uniqueTitle,
}) => {
  // Deliberately does not contain "rank"/"排名" etc. anywhere in the title —
  // this suffix is itself scanned for the banned vocabulary below, and a
  // self-descriptive title like "no ranking" would falsely trip that scan
  // (Playwright's getByText is case-insensitive, so even "ranking" inside a
  // title matches a search for "Rank").
  const titleA = uniqueTitle('E2E feed vocabulary check A')
  const titleB = uniqueTitle('E2E feed vocabulary check B')

  for (const title of [titleA, titleB]) {
    const bounty = await api.createBounty(USERS.sponsorA.id, {
      title,
      goal: 'Populate the feed with a real, credited work.',
      acceptance_criteria: 'n/a',
    })
    await api.transition(USERS.sponsorA.id, bounty.id, 'OPEN')
    await api.transition(USERS.engineerC.id, bounty.id, 'CLAIMED')
    await api.transition(USERS.engineerC.id, bounty.id, 'DELIVERED')
    const credit = await api.nominate(USERS.engineerC.id, bounty.id, {
      user_id: USERS.engineerC.id,
      role: 'LEAD',
    })
    await api.respond(USERS.engineerC.id, credit.id, 'CONFIRMED')
    await api.transition(USERS.sponsorA.id, bounty.id, 'COMPLETED')
  }

  await page.goto('/legacy')
  await expect(page.getByRole('heading', { name: '作品流', exact: true })).toBeVisible()
  const cardA = page.getByRole('article').filter({ has: page.getByRole('heading', { name: titleA, exact: true }) })
  const cardB = page.getByRole('article').filter({ has: page.getByRole('heading', { name: titleB, exact: true }) })
  await expect(cardA).toBeVisible()
  await expect(cardB).toBeVisible()

  // Vocabulary a ranking feature would definitely introduce, in Chinese and
  // English — none of it exists because Phase 1 has no scoring at all
  // (spec §2 lists "no ranking" as a deliberate non-goal, not a feature not
  // yet built).
  const rankingWords = ['排名', '排行榜', '总分', '得分', '积分', 'Leaderboard', 'Rank', 'Score']
  for (const word of rankingWords) {
    await expect(page.getByText(word, { exact: false })).toHaveCount(0)
  }

  // Structure a ranking view would need (a sortable/ordered table) is also
  // absent — the feed is a plain list of cards, not a scoreboard.
  await expect(page.getByRole('table')).toHaveCount(0)
  await expect(page.getByRole('columnheader')).toHaveCount(0)
})
