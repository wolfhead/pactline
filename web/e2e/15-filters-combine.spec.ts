import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: filters and search combine. Two decoys are planted, each
 * matching the target on every axis except one, so a filter silently
 * dropped by the UI (e.g. the second toggle clobbering the first instead of
 * combining with it) would let a decoy leak into the result.
 */
test('two filters at once return only their intersection', async ({
  page,
  uniqueTitle,
  runTag,
  trackTask,
  tasksApi,
}) => {
  const target = uniqueTitle('Filter intersection target')
  const decoyStatus = uniqueTitle('Filter intersection decoy status')
  const decoyPriority = uniqueTitle('Filter intersection decoy priority')

  const t1 = await tasksApi.createTask(USERS.engineerC.id, { title: target, status: 'in_progress', priority: 'high' })
  const t2 = await tasksApi.createTask(USERS.engineerC.id, { title: decoyStatus, status: 'todo', priority: 'high' })
  const t3 = await tasksApi.createTask(USERS.engineerC.id, { title: decoyPriority, status: 'in_progress', priority: 'low' })
  trackTask(t1.id)
  trackTask(t2.id)
  trackTask(t3.id)

  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  // Scope to this test's own tasks first (search), independent of the two
  // filters under test, and confirm all three are visible before filtering.
  await page.getByLabel('搜索任务').fill(runTag)
  await expect(page.getByRole('link', { name: target, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: decoyStatus, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: decoyPriority, exact: true })).toBeVisible()

  // The status/priority chips live behind the "筛选" disclosure now — the
  // filter bar reads as one quiet line until it's opened.
  await page.getByText('筛选', { exact: true }).click()

  const statusGroup = page.getByRole('group', { name: '按状态筛选' })
  const priorityGroup = page.getByRole('group', { name: '按优先级筛选' })
  await statusGroup.getByRole('button', { name: '进行中' }).click()
  await priorityGroup.getByRole('button', { name: '高' }).click()

  await expect(page.getByRole('link', { name: target, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: decoyStatus, exact: true })).not.toBeVisible()
  await expect(page.getByRole('link', { name: decoyPriority, exact: true })).not.toBeVisible()
})
