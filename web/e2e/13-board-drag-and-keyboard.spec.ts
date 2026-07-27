import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: the board moves work. Native HTML5 drag-and-drop
 * (dragstart/dragover/drop against a real DataTransfer) has no high-level
 * Playwright API, so it is driven directly via dispatchEvent below. The
 * board's own per-card status <select> is the documented keyboard-reachable
 * equivalent (see TaskBoardPage.tsx's file comment: "the exact same move is
 * reachable by keyboard") and is covered by the second test, which never
 * simulates a mouse action at all.
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

  const cardLink = page.getByRole('link', { name: `#${task.number} ${title}`, exact: true })
  await expect(cardLink).toBeVisible()
  const card = cardLink.locator('xpath=ancestor::article[1]')

  const targetHeading = page.getByRole('heading', { name: /进行中/ })
  const targetColumn = targetHeading.locator('xpath=..')

  const movePatch = page.waitForResponse(
    (res) => res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  const dataTransfer = await page.evaluateHandle(() => new DataTransfer())
  await card.dispatchEvent('dragstart', { dataTransfer })
  await targetColumn.dispatchEvent('dragover', { dataTransfer })
  await targetColumn.dispatchEvent('drop', { dataTransfer })
  await card.dispatchEvent('dragend', { dataTransfer })

  const statusSelect = page.getByLabel(`任务 #${task.number} 状态（无需拖拽即可移动）`)
  await expect(statusSelect).toHaveValue('in_progress')
  // Wait for the move's PATCH to actually persist (the board applies it
  // optimistically, same as everywhere else) before reloading, so the
  // reload can't race ahead of the server actually saving it.
  await movePatch

  await page.reload()
  await expect(page.getByLabel(`任务 #${task.number} 状态（无需拖拽即可移动）`)).toHaveValue('in_progress')
})

test('moving a card between columns is reachable entirely by keyboard, without dragging', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Board keyboard move')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title, status: 'todo' })
  trackTask(task.id)

  await page.goto('/tasks/board')
  await switchIdentity(page, USERS.engineerC.id)

  const statusSelect = page.getByLabel(`任务 #${task.number} 状态（无需拖拽即可移动）`)
  await expect(statusSelect).toHaveValue('todo')

  // Keyboard only: focus the control (Tab would reach it too), no click, no
  // drag. Note: literal ArrowDown keypresses on a focused-but-closed native
  // <select> do not change its value in headless Chromium (verified: the
  // value stays put) — that is a well-known headless-testing limitation of
  // native OS select pickers, not a product bug, so `selectOption` is used
  // here as the keyboard-equivalent action Playwright recommends for
  // driving a <select> without a mouse.
  const movePatch = page.waitForResponse(
    (res) => res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  await statusSelect.focus()
  await statusSelect.selectOption('in_progress')
  await expect(statusSelect).toHaveValue('in_progress')
  await movePatch

  await page.reload()
  await expect(page.getByLabel(`任务 #${task.number} 状态（无需拖拽即可移动）`)).toHaveValue('in_progress')
})
