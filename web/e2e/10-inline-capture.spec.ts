import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: inline capture. The create row is always ready — a pointer
 * lands in it (directly, or via the filter bar's "＋ 新建任务" button, which
 * is the pointer-first replacement for the deleted "c" shortcut), a title
 * is typed, Enter creates the task, and focus genuinely stays in the input
 * so the next one needs no second trigger at all.
 *
 * Adapted for the rewritten UI (Task 14): the app has no keyboard shortcuts
 * any more, so "press c" became "click 新建任务", and the capture input's
 * accessible name is now 新建任务 (InlineCreate.tsx) rather than 新建任务标题.
 * What is asserted is unchanged: the task appears, the field clears, and
 * focus comes back on its own.
 *
 * FIXED (was documented as a bug in the e2e report): InlineCreate's submit()
 * used to call `inputRef.current?.focus()` synchronously right after
 * `setPending(false)` in its `finally` block — but `setPending(false)` only
 * *schedules* the re-render that clears the input's `disabled` attribute, so
 * `.focus()` ran on an element that was, at that instant, still disabled — a
 * silent no-op in every real browser. The fix defers the refocus to an
 * effect keyed on `pending`, which only runs once React has actually
 * committed `disabled={false}`.
 */
test('inline capture creates a task on Enter, and keeps focus so a second task needs no re-trigger', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const titleOne = uniqueTitle('Inline capture one')
  const titleTwo = uniqueTitle('Inline capture two')

  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  const input = page.getByRole('textbox', { name: '新建任务' })
  await expect(input).not.toBeFocused()

  // "＋ 新建任务" opens nothing — it puts the caret in the capture row that
  // is already on screen. That is the whole design of this surface.
  await page.getByRole('button', { name: '新建任务' }).click()
  await expect(input).toBeFocused()

  await page.keyboard.type(titleOne)
  await page.keyboard.press('Enter')

  await expect(page.getByRole('link', { name: titleOne, exact: true })).toBeVisible()
  await expect(input).toHaveValue('')
  // Focus genuinely returns once the input is re-enabled — no click, no
  // second trip to the button, needed to keep capturing.
  await expect(input).toBeFocused()

  // A second task, typed immediately with no re-trigger and no mouse — this
  // is the whole point of an inline capture row.
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
