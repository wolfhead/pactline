import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: Task changes remain legible and live inside the unified work
 * timeline. After a priority and assignee change, the timeline names the
 * actor and change without raw enum strings or user UUIDs, and it updates
 * without a reload.
 *
 * TaskThreads refetches its two auxiliary sources from task.version changes;
 * it does not poll either source.
 */
test('work timeline keeps Task changes legible and updates them without polling', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Activity prose')
  const task = await tasksApi.createTask(USERS.leadB.id, { title })
  trackTask(task.id)
  const actorName = task.creator.name

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.leadB.id)

  const timeline = page.getByRole('region', { name: '工作时间线' })
  await expect(
    timeline.getByText('创建了任务，初始状态为「待办」', { exact: true }),
  ).toBeVisible()
  await expect(timeline.getByText('你', { exact: true }).first()).toBeVisible()
  await expect(timeline.getByText(/将状态从/)).not.toBeVisible()

  const isPatch = (res: import('@playwright/test').Response) =>
    res.url().endsWith(`/api/v1/tasks/${task.number}`) && res.request().method() === 'PATCH'

  const priorityPatch = page.waitForResponse(isPatch)
  await page.getByRole('combobox', { name: '优先级', exact: true }).click()
  await page.getByRole('option', { name: '高', exact: true }).click()
  await expect(page.getByRole('combobox', { name: '优先级', exact: true })).toHaveText(/高/)
  await priorityPatch

  // No reload: the Task version change triggers an on-demand timeline refresh.
  await expect(
    timeline.getByText('将优先级从「无优先级」改为「高」', { exact: true }),
  ).toBeVisible()

  const assigneePatch = page.waitForResponse(isPatch)
  await page.getByRole('combobox', { name: '负责人', exact: true }).click()
  await page.getByRole('option', { name: actorName, exact: true }).click()
  await expect(page.getByRole('combobox', { name: '负责人', exact: true })).toHaveText(actorName)
  await assigneePatch

  await expect(
    timeline.getByText(`将负责人从「未分配」改为「${actorName}」`, { exact: true }),
  ).toBeVisible()
})
