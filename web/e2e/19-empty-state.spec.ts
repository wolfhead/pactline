import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: empty state. A filter combination matching nothing must say so
 * — distinctly from a genuinely empty table — and offer to clear it.
 *
 * FIXED (was a rough edge in the e2e report): TaskListPage.tsx used to show
 * the same "没有任务 — 按 C 创建一个吧" message whether the table was
 * genuinely empty or a filter/search had narrowed a non-empty list to zero.
 * It now branches on whether any filter is active, showing
 * "没有符合筛选条件的任务" plus a "清除筛选条件" button in the filtered case.
 */
test('a search that matches nothing shows guidance distinct from "no tasks at all", and offers to clear it', async ({
  page,
  uniqueTitle,
  runTag,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Empty state guard')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title })
  trackTask(task.id)

  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  await expect(page.getByRole('link', { name: title, exact: true })).toBeVisible()

  // A search term unique to this run can never match any real task,
  // including the one just created above.
  await page.getByLabel('搜索任务').fill(`nonexistent-${runTag}`)

  await expect(page.getByText(/没有符合筛选条件的任务/)).toBeVisible()
  // Distinct from the genuinely-empty-table message: pointing at the capture
  // row would be misleading here, since a task created there wouldn't itself
  // satisfy the still-active search — a user could create one, watch it not
  // appear, and think capture is broken. (The copy no longer mentions the C
  // shortcut, which the list page no longer has.)
  await expect(page.getByText('没有任务 — 在上面输入标题就能创建一个')).not.toBeVisible()

  // Clearing resets the search and the matching task reappears.
  await page.getByRole('button', { name: '清除筛选条件' }).click()
  await expect(page.getByLabel('搜索任务')).toHaveValue('')
  await expect(page.getByRole('link', { name: title, exact: true })).toBeVisible()
})
