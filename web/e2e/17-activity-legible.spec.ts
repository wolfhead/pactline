import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: activity is legible and live. After a priority change and an
 * assignee change, the activity log names what changed in readable Chinese
 * prose, not raw enum strings or bare user UUIDs — and it appears without a
 * reload.
 *
 * FIXED (was a documented bug in the e2e report): ActivityLog.tsx's fetch
 * effect used to depend on `[task.number, me?.id]` only, so an on-page
 * status/assignee change (which mutates task.status/task.assignee, not
 * task.number) never re-triggered it. It now also depends on
 * `task.updated_at`, which every server-confirmed mutation bumps.
 */
test('activity log reads as legible prose and updates live after a priority change and an assignee change', async ({
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

  // ActivityLog carries role="region" + aria-label="历史记录", so the block
  // is addressed by name rather than by climbing out of its own heading.
  const activitySection = page.getByRole('region', { name: '历史记录' })
  await expect(
    activitySection.getByText(`${actorName} 创建了任务，初始状态为「待办」`, { exact: true }),
  ).toBeVisible()
  await expect(activitySection.getByText(/将状态从/)).not.toBeVisible()

  const isPatch = (res: import('@playwright/test').Response) =>
    res.url().endsWith(`/api/v1/tasks/${task.number}`) && res.request().method() === 'PATCH'

  const priorityPatch = page.waitForResponse(isPatch)
  await page.getByRole('combobox', { name: '优先级', exact: true }).click()
  await page.getByRole('option', { name: '高', exact: true }).click()
  await expect(page.getByRole('combobox', { name: '优先级', exact: true })).toHaveText(/高/)
  await priorityPatch

  // No reload: the new entry must appear purely from the activity fetch
  // re-firing off the task's own updated_at.
  await expect(
    activitySection.getByText(`${actorName} 将优先级从「无优先级」改为「高」`, { exact: true }),
  ).toBeVisible()

  const assigneePatch = page.waitForResponse(isPatch)
  await page.getByRole('combobox', { name: '负责人', exact: true }).click()
  await page.getByRole('option', { name: actorName, exact: true }).click()
  await expect(page.getByRole('combobox', { name: '负责人', exact: true })).toHaveText(actorName)
  await assigneePatch

  await expect(
    activitySection.getByText(`${actorName} 将负责人从「未分配」改为「${actorName}」`, { exact: true }),
  ).toBeVisible()
})
