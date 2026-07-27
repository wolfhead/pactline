import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: inline capture. The create row is always ready — press the
 * global "c" shortcut, type a title, press Enter, and the task appears in
 * the list.
 *
 * BUG (documented here, not fixed — see the e2e report): InlineCreateRow's
 * own file comment promises "the input is immediately ready for the next
 * one" after a successful create, with no further key needed. It isn't.
 * InlineCreateRow.tsx's submit() does, in its `finally` block:
 *   setPending(false)
 *   inputRef.current?.focus()
 * back to back, synchronously. `setPending(false)` only *schedules* the
 * re-render that clears the input's `disabled` attribute; React hasn't
 * committed that DOM change yet when `.focus()` runs immediately after, so
 * `.focus()` is called on an element that is, at that instant, still
 * disabled — which is a silent no-op in every real browser. Nothing focuses
 * the input once it actually becomes enabled a tick later, so keyboard
 * focus is left on <body>. Reproduced directly: type a title into the
 * create input, press Enter, wait for the request to resolve, then check
 * document.activeElement — it is <body>, not the input, even though the
 * input's `disabled` is by then `false`. A user who just captured one task
 * cannot keep typing the next one; they must press "c" (or click) again.
 */
test('inline capture creates a task on Enter (documents: focus does not return to the input afterward)', async ({
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
  // Per the bug documented above, focus is NOT retained here — it lands
  // back on <body>. This assertion pins down the actual (broken) behavior;
  // if InlineCreateRow is ever fixed to keep focus, this line should be
  // changed to `await expect(input).toBeFocused()`.
  await expect(input).not.toBeFocused()

  // A real user must press "c" again (still mouse-free, but contradicts the
  // "immediately ready" promise) to capture a second task straight after.
  await page.keyboard.press('c')
  await expect(input).toBeFocused()
  await page.keyboard.type(titleTwo)
  await page.keyboard.press('Enter')

  await expect(page.getByRole('link', { name: titleTwo, exact: true })).toBeVisible()
  await expect(input).toHaveValue('')

  // Resolve both created tasks' ids (through the number visible in their
  // row link) so cleanup can delete them.
  for (const title of [titleOne, titleTwo]) {
    const href = await page.getByRole('link', { name: title, exact: true }).getAttribute('href')
    const number = Number(href!.split('/tasks/')[1])
    const created = await tasksApi.getTask(USERS.engineerC.id, number)
    trackTask(created.id)
  }
})
