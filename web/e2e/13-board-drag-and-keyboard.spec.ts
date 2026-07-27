import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: the board moves work. Native HTML5 drag-and-drop
 * (dragstart/dragover/drop against a real DataTransfer) has no high-level
 * Playwright API, so it is driven directly via dispatchEvent below.
 *
 * The card itself carries no permanent status control — the column it sits
 * in already says its status. The keyboard-reachable equivalent (see
 * TaskBoardPage.tsx's file comment) is a quiet per-card "移动" trigger: a
 * plain, unobtrusive button (not a dropdown sitting open on every card)
 * that reveals a real <select> the moment it's activated. Both the trigger
 * and the revealed <select> share one aria-label that names the task and
 * its current status, so a single accessible-name lookup finds whichever
 * of the two is actually mounted — covered by the second test below, which
 * never simulates a mouse action at all.
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

  const trigger = page.getByRole('button', { name: new RegExp(`移动任务 #${task.number}`) })
  await expect(trigger).toHaveAttribute('aria-label', /当前状态：待办/)

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

  // The move took effect: the card is now inside the "进行中" column, and
  // its (still-quiet, never-opened) move trigger's accessible name reports
  // the new status.
  await expect(targetColumn.getByRole('link', { name: `#${task.number} ${title}`, exact: true })).toBeVisible()
  await expect(trigger).toHaveAttribute('aria-label', /当前状态：进行中/)
  // Wait for the move's PATCH to actually persist (the board applies it
  // optimistically, same as everywhere else) before reloading, so the
  // reload can't race ahead of the server actually saving it.
  await movePatch

  await page.reload()
  await expect(targetColumn.getByRole('link', { name: `#${task.number} ${title}`, exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: new RegExp(`移动任务 #${task.number}`) })).toHaveAttribute(
    'aria-label',
    /当前状态：进行中/,
  )
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

  // No permanent <select> anywhere on the board's cards — only the quiet
  // trigger. (Scoped to the board itself: the header's theme/identity
  // switchers are real <select>s too, elsewhere on the same page.)
  const board = page.locator('.task-board')
  await expect(board.getByRole('combobox')).toHaveCount(0)

  const trigger = page.getByRole('button', { name: new RegExp(`移动任务 #${task.number}`) })
  await expect(trigger).toHaveAttribute('aria-label', /当前状态：待办/)

  // Keyboard only: focus the trigger (Tab would reach it too), Enter
  // reveals the real <select> — no click, no drag.
  await trigger.focus()
  await trigger.press('Enter')

  const select = page.getByLabel(new RegExp(`移动任务 #${task.number}`))
  await expect(select).toHaveValue('todo')

  // Note: literal ArrowDown keypresses on a focused-but-closed native
  // <select> do not change its value in headless Chromium (verified: the
  // value stays put) — that is a well-known headless-testing limitation of
  // native OS select pickers, not a product bug, so `selectOption` is used
  // here as the keyboard-equivalent action Playwright recommends for
  // driving a <select> without a mouse.
  const movePatch = page.waitForResponse(
    (res) => res.url().endsWith(`/api/tasks/${task.number}`) && res.request().method() === 'PATCH',
  )
  await select.selectOption('in_progress')
  // Choosing an option collapses the <select> straight back to the quiet
  // trigger, whose accessible name now reports the new status.
  await expect(page.getByRole('button', { name: new RegExp(`移动任务 #${task.number}`) })).toHaveAttribute(
    'aria-label',
    /当前状态：进行中/,
  )
  await movePatch

  await page.reload()
  await expect(page.getByRole('button', { name: new RegExp(`移动任务 #${task.number}`) })).toHaveAttribute(
    'aria-label',
    /当前状态：进行中/,
  )
})
