import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: inline capture. The create row is always ready — press the
 * global "c" shortcut, type a title, press Enter, and the task appears in
 * the list, with focus genuinely back in the input for the next one.
 *
 * FIXED (was documented as a bug in the e2e report): InlineCreateRow.tsx's
 * submit() used to call `inputRef.current?.focus()` synchronously right
 * after `setPending(false)` in its `finally` block — but `setPending(false)`
 * only *schedules* the re-render that clears the input's `disabled`
 * attribute, so `.focus()` ran on an element that was, at that instant,
 * still disabled — a silent no-op in every real browser. The fix defers the
 * refocus to an effect keyed on `pending`, which only runs once React has
 * actually committed `disabled={false}`.
 */
test('inline capture creates a task on Enter, and keeps focus so a second task needs no re-trigger of "c"', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const titleOne = uniqueTitle('Inline capture one')
  const titleTwo = uniqueTitle('Inline capture two')

  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  const input = page.getByLabel('新建任务标题')
  await expect(input).not.toBeFocused()

  // "c" is the global create shortcut — it must work from anywhere on the
  // page, not just when already inside the create field.
  await page.keyboard.press('c')
  await expect(input).toBeFocused()

  await page.keyboard.type(titleOne)
  await page.keyboard.press('Enter')

  await expect(page.getByRole('link', { name: titleOne, exact: true })).toBeVisible()
  await expect(input).toHaveValue('')
  // Focus genuinely returns once the input is re-enabled — no click, no
  // re-press of "c" needed to keep capturing.
  await expect(input).toBeFocused()

  // A second task, typed immediately with no shortcut re-trigger and no
  // mouse — this is the whole point of an inline capture row.
  await page.keyboard.type(titleTwo)
  await page.keyboard.press('Enter')

  await expect(page.getByRole('link', { name: titleTwo, exact: true })).toBeVisible()
  await expect(input).toHaveValue('')
  await expect(input).toBeFocused()

  // Resolve both created tasks' ids (through the number visible in their
  // row link) so cleanup can delete them.
  for (const title of [titleOne, titleTwo]) {
    const href = await page.getByRole('link', { name: title, exact: true }).getAttribute('href')
    const number = Number(href!.split('/tasks/')[1])
    const created = await tasksApi.getTask(USERS.engineerC.id, number)
    trackTask(created.id)
  }
})
