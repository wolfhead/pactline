import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: keyboard navigation on the list — move with j/k, open with
 * Enter, clear the selection with Escape, and focus never gets trapped.
 *
 * One real product gap surfaced while writing this test (see the e2e
 * report): the task detail view is a full page (/tasks/:number), not an
 * overlay, and unlike the shortcuts overlay or InlineEditable, Escape does
 * nothing there — there is no keyboard path back to the list. The shortcut
 * legend itself claims "Esc 关闭弹层 / 取消编辑" (close overlay / cancel
 * edit), which a user opening a task via Enter would reasonably read as
 * "Escape backs out of what Enter just opened". It doesn't. This test
 * documents the actual behaviour (Escape is a no-op on the detail page) and
 * uses the explicit "← 返回列表" link to go back instead.
 */
test('keyboard-only list navigation: move, open with Enter, clear selection with Escape, focus never traps', async ({
  page,
  uniqueTitle,
  runTag,
  trackTask,
  tasksApi,
}) => {
  const titleA = uniqueTitle('Keyboard nav A')
  const titleB = uniqueTitle('Keyboard nav B')
  const taskA = await tasksApi.createTask(USERS.engineerC.id, { title: titleA })
  const taskB = await tasksApi.createTask(USERS.engineerC.id, { title: titleB })
  trackTask(taskA.id)
  trackTask(taskB.id)

  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  // Isolate the list to just these two tasks so j/k land predictably
  // regardless of what other tests are creating concurrently.
  await page.keyboard.press('/')
  await expect(page.getByLabel('搜索任务')).toBeFocused()
  await page.keyboard.type(runTag)
  await expect(page.getByRole('link', { name: titleA, exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: titleB, exact: true })).toBeVisible()

  // Move focus off the search input (j/k are ignored while a typing target
  // is focused) by clicking a plain, non-interactive element.
  await page.getByRole('heading', { name: '任务列表' }).click()

  // created_at desc is the default sort, so B (created after A) is first.
  await page.keyboard.press('j')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(new RegExp(`/tasks/${taskB.number}$`))

  // Documented gap: Escape does nothing on the detail page.
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(new RegExp(`/tasks/${taskB.number}$`))

  await page.getByRole('link', { name: '← 返回列表' }).click()
  await expect(page).toHaveURL(/\/tasks$/)

  // Filters reset on remount — reapply the same scope, then confirm Escape
  // clears the list's own row selection (Enter then does nothing).
  await page.keyboard.press('/')
  await page.keyboard.type(runTag)
  await expect(page.getByRole('link', { name: titleA, exact: true })).toBeVisible()
  await page.getByRole('heading', { name: '任务列表' }).click()

  await page.keyboard.press('j')
  await page.keyboard.press('j')
  await page.keyboard.press('Escape')
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/tasks$/)

  // Focus is never trapped: repeated Tabs keep moving through the page's
  // controls instead of getting stuck on one element.
  const seen = new Set<string>()
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press('Tab')
    const marker = await page.evaluate(() => {
      const el = document.activeElement
      if (!el) return 'none'
      return `${el.tagName}:${el.getAttribute('aria-label') ?? el.textContent?.slice(0, 20) ?? ''}`
    })
    seen.add(marker)
  }
  expect(seen.size).toBeGreaterThan(1)
})
