import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * The four arrangements are one component tree, not four. These assertions
 * pin the two things that actually differ: whether the detail takes a column
 * or slides over the list, and whether navigation is permanent, a drawer, or
 * a bottom bar.
 */
test('the detail takes a third column at xl and slides over the list below it', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Three column')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title })
  trackTask(task.id)

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)
  await expect(page.getByRole('link', { name: title, exact: true })).toBeVisible()

  // xl: a real column — no dialog — and the list is still there beside it.
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('listitem').first()).toBeVisible()

  // At the lower edge of xl, the bounded detail still leaves enough room
  // for the task title to remain a real visible link.
  await page.setViewportSize({ width: 1280, height: 820 })
  await expect(page.getByRole('link', { name: title, exact: true })).toBeVisible()

  // The desktop column is not permanent: closing it returns to the full
  // list and clears the selected-task URL.
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page).toHaveURL(/\/tasks$/)
  await expect(page.getByRole('complementary', { name: '任务详情' })).toHaveCount(0)

  await page.goto(`/tasks/${task.number}`)

  // lg: the same URL now slides the detail over an unshrunken list.
  await page.setViewportSize({ width: 1120, height: 820 })
  await expect(page.getByRole('dialog')).toBeVisible()
  // A CSS attribute selector rather than getByRole('listitem'): Radix marks
  // everything outside an open modal `aria-hidden`, so the accessibility
  // tree reports zero listitems here even though the list is very much
  // still mounted underneath — which is the one thing this line is for.
  await expect(page.locator('[role="listitem"]').first()).toBeAttached()

  // Portaled property controls must stay inside the modal sheet's
  // accessibility boundary. A body-level popover is hidden by Radix's modal
  // isolation even though the same control works in the xl column.
  await page.getByRole('dialog', { name: '任务详情' })
    .getByRole('button', { name: '截止日期' }).click()
  await expect(page.getByRole('dialog', { name: '选择截止日期' })).toBeVisible()
})

test('navigation is permanent at lg, a drawer at md, and a bottom bar on a phone', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/tasks')
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible()

  await page.setViewportSize({ width: 900, height: 800 })
  await expect(page.getByRole('button', { name: '打开导航' })).toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('navigation', { name: '底部导航' })).toBeVisible()
})

// The touch-target sweep that used to live here ran on Desktop Chrome at
// 390px — `pointer: fine`, where the rule it claimed to check cannot even
// apply. It moved to 22-touch-targets.spec.ts, which runs under the
// chromium-touch project.

test('the phone header carries no switchers and the filter bar no create button', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/tasks')
  await expect(page.getByRole('navigation', { name: '底部导航' })).toBeVisible()

  // Account controls remain reachable behind 我的, and 新建任务 would be a
  // third route to the capture row directly above it.
  await expect(page.getByRole('button', { name: '退出登录' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '新建任务' })).toHaveCount(0)

  await page.getByRole('button', { name: '我的' }).click()
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()

  // …and they are standing chrome again the moment there is room for them.
  await page.keyboard.press('Escape')
  await page.setViewportSize({ width: 1280, height: 900 })
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
  await expect(page.getByRole('button', { name: '新建任务' })).toBeVisible()
})

test('opening a task from deep in the list keeps the list position', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const made = []
  for (let i = 0; i < 25; i++) {
    const t = await tasksApi.createTask(USERS.engineerC.id, { title: uniqueTitle(`Scroll ${i}`) })
    trackTask(t.id)
    made.push(t)
  }
  await page.setViewportSize({ width: 1440, height: 700 })
  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  // made[0] is the OLDEST of the batch and the default sort is created_at
  // desc, so it sits ~25 rows down — genuinely below the fold, which is
  // what makes the offset below non-zero and this test worth running.
  const deep = page.getByRole('link', { name: made[0].title, exact: true })
  await expect(deep).toBeVisible()
  await deep.scrollIntoViewIfNeeded()
  const before = await page.evaluate(() => document.querySelector('[data-task-list]')!.scrollTop)
  expect(before).toBeGreaterThan(0)

  await deep.click()
  await expect(page).toHaveURL(new RegExp(`/tasks/${made[0].number}$`))
  // The list never unmounted, so its scroll offset survives untouched.
  const after = await page.evaluate(() => document.querySelector('[data-task-list]')!.scrollTop)
  expect(after).toBe(before)
})
