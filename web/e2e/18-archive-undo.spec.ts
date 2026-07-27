import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: archive and undo. Archiving must not ask for confirmation via a
 * native dialog — it applies immediately and offers an undo instead, which
 * must actually restore the task.
 */
test('archiving offers an undo instead of a confirmation dialog, and undo restores the task', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Archive and undo')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title })
  trackTask(task.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  let dialogSeen = false
  page.on('dialog', (dialog) => {
    dialogSeen = true
    void dialog.dismiss()
  })

  await page.getByRole('button', { name: '归档', exact: true }).click()

  expect(dialogSeen).toBe(false)
  await expect(page.getByText('此任务已归档。')).toBeVisible()
  const toast = page.getByRole('status')
  await expect(toast).toContainText('已归档任务。')
  await expect(page.getByRole('button', { name: '恢复', exact: true })).toBeVisible()

  await toast.getByRole('button', { name: '撤销', exact: true }).click()

  await expect(page.getByText('此任务已归档。')).not.toBeVisible()
  await expect(page.getByRole('button', { name: '归档', exact: true })).toBeVisible()

  await page.reload()
  await expect(page.getByText('此任务已归档。')).not.toBeVisible()
  await expect(page.getByRole('button', { name: '归档', exact: true })).toBeVisible()
})
