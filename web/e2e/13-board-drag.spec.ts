import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: the board moves work. Native HTML5 drag-and-drop
 * (dragstart/dragover/drop against a real DataTransfer) has no high-level
 * Playwright API, so it is driven directly via dispatchEvent below.
 *
 * Rewritten for the new board (Task 14). The old card carried no status
 * control at all — only a quiet "移动任务 #<n>" trigger that revealed a
 * native <select>, whose accessible name was the only place a card's status
 * was readable. That trigger is gone: BoardCard now carries a real,
 * permanently visible StatusControl using the same 任务 #<n> 状态 name the
 * list rows use, so the move's effect is asserted directly on the control's
 * value instead of on a trigger's label. The companion keyboard-only test
 * went with it — the app has no keyboard shortcuts any more, and a Radix
 * Select is keyboard-operable by construction, so there is no product
 * behaviour left there that this suite could uniquely pin.
 *
 * The card is still an <article>, which is what makes the drag source
 * reachable from its title link.
 */
test('dragging a card to another column moves it there and the move survives a reload', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Board drag move')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title, status: 'todo' })
  trackTask(task.id)

  await page.goto('/tasks/board')
  await switchIdentity(page, USERS.engineerC.id)

  // The card's number sits in a sibling <span>, not inside the link, so the
  // link's accessible name is the title alone.
  const cardLink = page.getByRole('link', { name: title, exact: true })
  await expect(cardLink).toBeVisible()
  const card = cardLink.locator('xpath=ancestor::article[1]')

  const statusControl = page.getByRole('combobox', { name: `任务 #${task.number} 状态` })
  await expect(statusControl).toHaveText(/待办/)

  const targetColumn = page.getByRole('group', { name: /进行中/ })

  const movePatch = page.waitForResponse(
    (res) => res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  const dataTransfer = await page.evaluateHandle(() => new DataTransfer())
  await card.dispatchEvent('dragstart', { dataTransfer })
  await targetColumn.dispatchEvent('dragover', { dataTransfer })
  await targetColumn.dispatchEvent('drop', { dataTransfer })
  await card.dispatchEvent('dragend', { dataTransfer })

  // The move took effect: the card is now inside the "进行中" column, and
  // its own status control — never opened — reports the new status.
  await expect(targetColumn.getByRole('link', { name: title, exact: true })).toBeVisible()
  await expect(page.getByRole('combobox', { name: `任务 #${task.number} 状态` })).toHaveText(/进行中/)
  // Wait for the move's PATCH to actually persist (the board applies it
  // optimistically, same as everywhere else) before reloading, so the
  // reload can't race ahead of the server actually saving it.
  await movePatch

  await page.reload()
  await expect(
    page.getByRole('group', { name: /进行中/ }).getByRole('link', { name: title, exact: true }),
  ).toBeVisible()
  await expect(page.getByRole('combobox', { name: `任务 #${task.number} 状态` })).toHaveText(/进行中/)
})
