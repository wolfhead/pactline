import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: filters and search combine. Two decoys are planted, each
 * matching the target on every axis except one, so a filter silently
 * dropped by the UI (e.g. the second toggle clobbering the first instead of
 * combining with it) would let a decoy leak into the result.
 *
 * Rewritten for the new filter bar (Task 14). There is no longer one master
 * "筛选" disclosure hiding everything: each filter is its own permanently
 * visible trigger, and 阶段/优先级 — the two multi-value ones — open a
 * Popover holding a checkbox per value, not a row of toggle buttons. Escape
 * closes each popover; that is the only keyboard interaction left anywhere
 * in the app, and it is the one the plan explicitly kept. What this test
 * asserts is unchanged: two independent filters intersect, and neither
 * clears the other.
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

  const t1 = await tasksApi.createTask(USERS.engineerC.id, {
    title: target,
    priority: 'high',
    assignee_id: USERS.sponsorA.id,
  })
  const t2 = await tasksApi.createTask(USERS.engineerC.id, {
    title: decoyStatus,
    priority: 'high',
    assignee_id: USERS.sponsorA.id,
  })
  const t3 = await tasksApi.createTask(USERS.engineerC.id, {
    title: decoyPriority,
    priority: 'low',
    assignee_id: USERS.sponsorA.id,
  })
  trackTask(t1.id)
  trackTask(t2.id)
  trackTask(t3.id)
  await tasksApi.markTaskReady(USERS.engineerC.id, t1.number)
  await tasksApi.claimTaskStage(USERS.engineerC.id, t1.number)
  await tasksApi.markTaskReady(USERS.engineerC.id, t3.number)
  await tasksApi.claimTaskStage(USERS.engineerC.id, t3.number)

  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  // Scope to this test's own tasks first (search), independent of the two
  // filters under test, and confirm all three are visible before filtering.
  await page.getByLabel('搜索任务').fill(runTag)
  await expect(page.getByRole('link', { name: target, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: decoyStatus, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: decoyPriority, exact: true })).toBeVisible()

  // Phase: its own trigger and popover.
  await page.getByRole('button', { name: '阶段', exact: true }).click()
  await page.getByRole('group', { name: '按阶段筛选' }).getByRole('checkbox', { name: '执行中', exact: true }).click()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('group', { name: '按阶段筛选' })).toHaveCount(0)

  // Priority next. If the second toggle clobbered the first rather than
  // combining with it, decoyStatus (backlog + high) would come back below.
  await page.getByRole('button', { name: '优先级', exact: true }).click()
  await page.getByRole('group', { name: '按优先级筛选' }).getByRole('checkbox', { name: '高', exact: true }).click()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('group', { name: '按优先级筛选' })).toHaveCount(0)

  // Both triggers still say they are narrowing — neither was reset.
  await expect(page.getByRole('button', { name: '阶段 · 1' })).toBeVisible()
  await expect(page.getByRole('button', { name: '优先级 · 1' })).toBeVisible()

  await expect(page.getByRole('link', { name: target, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: decoyStatus, exact: true })).not.toBeVisible()
  await expect(page.getByRole('link', { name: decoyPriority, exact: true })).not.toBeVisible()
})
