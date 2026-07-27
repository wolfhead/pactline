import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: empty state. A filter combination matching nothing must say
 * what to do next, not show a bare "no data".
 *
 * Rough edge documented in the e2e report: the same message
 * ("没有任务 — 按 C 创建一个吧") is shown both when the table is genuinely
 * empty and when filters/search have narrowed a non-empty list down to
 * zero. In the filtered case, "press C to create one" doesn't explain that
 * a filter is the reason nothing is showing, and a task created via C won't
 * itself satisfy the still-active filter — a real user could easily create
 * a task, watch it not appear, and think capture is broken.
 */
test('a search that matches nothing shows guidance, not a bare empty list', async ({ page, runTag }) => {
  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  // A search term unique to this run can never match any real task.
  await page.getByLabel('搜索任务').fill(`nonexistent-${runTag}`)

  await expect(page.getByText('没有任务 — 按 C 创建一个吧')).toBeVisible()
})
