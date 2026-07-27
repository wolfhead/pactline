import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: editing in place. The title is edited exactly where it is
 * displayed (InlineEditable, no modal, no click-to-edit toggle) — Enter
 * commits and the change survives a reload; Escape discards the draft and
 * nothing reaches the server.
 */
test('title edits in place: Enter commits and survives reload, Escape discards without saving', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const original = uniqueTitle('Inline edit original')
  const committed = uniqueTitle('Inline edit committed')
  const abandoned = uniqueTitle('Inline edit abandoned draft')

  const task = await tasksApi.createTask(USERS.engineerC.id, { title: original })
  trackTask(task.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const titleField = page.getByLabel('任务标题')
  await expect(titleField).toHaveValue(original)

  const firstPatch = page.waitForResponse(
    (res) => res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  await titleField.fill(committed)
  await titleField.press('Enter')
  await expect(titleField).toHaveValue(committed)
  // Wait for the commit's PATCH to actually land before reloading — the
  // reload above raced the optimistic UI (already updated) against the
  // still-in-flight request under real network/CPU contention (a full
  // parallel suite run), reloading before the server had persisted it.
  await firstPatch

  await page.reload()
  await expect(page.getByLabel('任务标题')).toHaveValue(committed)

  // Start an edit and abandon it with Escape: the original (last committed)
  // text must return immediately, in the DOM, not eventually via a round
  // trip.
  await page.getByLabel('任务标题').fill(abandoned)
  await expect(page.getByLabel('任务标题')).toHaveValue(abandoned)
  await page.getByLabel('任务标题').press('Escape')
  await expect(page.getByLabel('任务标题')).toHaveValue(committed)

  // And the abandoned draft must genuinely never have been saved.
  await page.reload()
  await expect(page.getByLabel('任务标题')).toHaveValue(committed)
})
