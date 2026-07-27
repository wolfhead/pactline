import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: activity is legible. After a status change and an assignee
 * change, the activity log names what changed in readable Chinese prose,
 * not raw enum strings or bare user UUIDs.
 *
 * BUG (documented here, not fixed — see the e2e report): the entries are
 * legible prose once shown, but ActivityLog.tsx does not update live. Its
 * fetch effect depends on `[task.number, me?.id]` only — despite the
 * component's own comment claiming it "fetches whenever the task changes"
 * — so a status/assignee/etc. change made on the same page (task.status,
 * task.assignee mutate, task.number does not) never re-triggers the
 * activity fetch. Reproduced directly: change status, wait, read
 * .activity-log's text — still only the "created" entry; reload the same
 * page — the new "changed status" entry is there. A user editing a task and
 * scrolling down to activity sees a history that lags one refresh behind
 * what they just did.
 */
test('activity log reads as legible prose after a status change and an assignee change (requires a reload to appear — see bug above)', async ({
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

  const isPatch = (res: import('@playwright/test').Response) =>
    res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH'

  const statusPatch = page.waitForResponse(isPatch)
  await page.getByLabel('状态', { exact: true }).selectOption('in_progress')
  await expect(page.getByLabel('状态', { exact: true })).toHaveValue('in_progress')
  await statusPatch

  const assigneePatch = page.waitForResponse(isPatch)
  await page.getByLabel('负责人', { exact: true }).selectOption(USERS.engineerD.id)
  await expect(page.getByLabel('负责人', { exact: true })).toHaveValue(USERS.engineerD.id)
  await assigneePatch

  // Per the bug documented above, the activity panel does not pick up
  // either change without a reload. Both PATCHes are awaited above (not
  // just their optimistic UI effect) so the reload below can't race ahead
  // of either one actually persisting.
  await page.reload()

  const activitySection = page.getByRole('heading', { name: '历史记录', level: 3 }).locator('xpath=..')

  await expect(
    activitySection.getByText(`${USERS.leadB.name} 创建了任务，初始状态为「待定」`, { exact: true }),
  ).toBeVisible()
  await expect(
    activitySection.getByText(`${USERS.leadB.name} 将状态从「待定」改为「进行中」`, { exact: true }),
  ).toBeVisible()
  await expect(
    activitySection.getByText(`${USERS.leadB.name} 将负责人从「未分配」改为「${USERS.engineerD.name}」`, { exact: true }),
  ).toBeVisible()
})
