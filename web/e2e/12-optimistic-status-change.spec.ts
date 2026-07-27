import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: optimistic status change. The store validates status against a
 * fixed enum (internal/store/task_store.go) and the UI only ever offers
 * valid enum values through a <select>, so there is no way to make the real
 * backend refuse a status PATCH through genuine user interaction — route
 * interception is the documented fallback for that half of this test (see
 * the task brief).
 *
 * Status is a quiet display (a colour mark + label) until interacted with
 * (see QuietSelect) — click it to reveal the real <select>. Choosing an
 * option commits it and collapses the <select> straight back to the quiet
 * display, the same way a native <select> closing its own dropdown would,
 * so the optimistic update and its later revert are both observed on the
 * quiet display's text, not on a <select> that's only briefly on screen.
 */
test('status change is optimistic: the UI updates before the server responds, and reverts with a reason if the server refuses', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Optimistic status change')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title, status: 'todo' })
  trackTask(task.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const statusField = page.getByLabel('状态', { exact: true })
  await expect(statusField).toHaveText('待办')

  // Hold the PATCH response open so the test can prove the display updates
  // BEFORE the server ever answers — not merely "eventually, after a round
  // trip fast enough that we didn't actually watch for the gap".
  let releaseHold: () => void = () => {}
  const held = new Promise<void>((resolve) => {
    releaseHold = resolve
  })
  await page.route(`**/api/tasks/${task.number}`, async (route) => {
    if (route.request().method() !== 'PATCH') {
      await route.continue()
      return
    }
    await held
    await route.continue()
  })

  const patchResponse = page.waitForResponse(
    (res) => res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  await statusField.click()
  await page.getByLabel('状态', { exact: true }).selectOption('in_progress')
  // The PATCH is still pending (held) right now, yet the quiet display —
  // the <select> already collapsed back to it the instant a choice was
  // made — already reflects the new status.
  await expect(page.getByLabel('状态', { exact: true })).toHaveText('进行中')
  await expect(page.getByText(/已恢复原状态/)).toHaveCount(0)

  releaseHold()
  // Wait for the held request to actually finish before unrouting — doing
  // it immediately after releaseHold() races the route handler's own
  // pending route.continue() against unroute()'s cleanup, which can call
  // continue() on the same route a second time ("Route is already
  // handled!").
  await patchResponse
  await page.unroute(`**/api/tasks/${task.number}`)

  // Reload to confirm the optimistic value is what the server actually
  // persisted, not just a client-side illusion.
  await page.reload()
  await expect(page.getByLabel('状态', { exact: true })).toHaveText('进行中')

  // Now make the server refuse the next change outright, held the same way
  // so the optimistic bump is observed before the revert.
  let releaseFail: () => void = () => {}
  const heldFail = new Promise<void>((resolve) => {
    releaseFail = resolve
  })
  await page.route(`**/api/tasks/${task.number}`, async (route) => {
    if (route.request().method() !== 'PATCH') {
      await route.continue()
      return
    }
    await heldFail
    await route.fulfill({
      status: 422,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'forced e2e failure' }),
    })
  })

  await page.getByLabel('状态', { exact: true }).click()
  await page.getByLabel('状态', { exact: true }).selectOption('done')
  await expect(page.getByLabel('状态', { exact: true })).toHaveText('已完成')

  releaseFail()

  // Reverts once the server's refusal comes back, with the reason shown.
  await expect(page.getByLabel('状态', { exact: true })).toHaveText('进行中')
  await expect(page.getByText(/forced e2e failure/)).toBeVisible()
  await expect(page.getByText(/已恢复原状态/)).toBeVisible()
})
