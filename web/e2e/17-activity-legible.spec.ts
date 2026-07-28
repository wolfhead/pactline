import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: activity is legible and live. After a status change and an
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
test('activity log reads as legible prose and updates live after a status change and an assignee change', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Activity prose')
  const task = await tasksApi.createTask(USERS.leadB.id, { title, status: 'backlog' })
  trackTask(task.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.leadB.id)

  // ActivityLog carries role="region" + aria-label="历史记录", so the block
  // is addressed by name rather than by climbing out of its own heading.
  const activitySection = page.getByRole('region', { name: '历史记录' })
  await expect(
    activitySection.getByText(`${USERS.leadB.name} 创建了任务，初始状态为「待定」`, { exact: true }),
  ).toBeVisible()
  await expect(activitySection.getByText(/将状态从/)).not.toBeVisible()

  const isPatch = (res: import('@playwright/test').Response) =>
    res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH'

  // Status and assignee are permanently visible Radix Selects (Task 14) —
  // not native <select>s, so `selectOption()` does not apply: open the
  // combobox, click the option. `exact: true` on the name because at xl the
  // list column beside the detail names its own controls 任务 #<n> 状态.
  const statusPatch = page.waitForResponse(isPatch)
  await page.getByRole('combobox', { name: '状态', exact: true }).click()
  await page.getByRole('option', { name: '进行中', exact: true }).click()
  await expect(page.getByRole('combobox', { name: '状态', exact: true })).toHaveText(/进行中/)
  await statusPatch

  // No reload: the new entry must appear purely from the activity fetch
  // re-firing off the task's own updated_at.
  await expect(
    activitySection.getByText(`${USERS.leadB.name} 将状态从「待定」改为「进行中」`, { exact: true }),
  ).toBeVisible()

  const assigneePatch = page.waitForResponse(isPatch)
  await page.getByRole('combobox', { name: '负责人', exact: true }).click()
  await page.getByRole('option', { name: USERS.engineerD.name, exact: true }).click()
  await expect(page.getByRole('combobox', { name: '负责人', exact: true })).toHaveText(USERS.engineerD.name)
  await assigneePatch

  await expect(
    activitySection.getByText(`${USERS.leadB.name} 将负责人从「未分配」改为「${USERS.engineerD.name}」`, { exact: true }),
  ).toBeVisible()
})
