import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Task detail uses one modal inspector at every breakpoint. The collection
 * remains mounted underneath so closing the inspector restores the exact
 * list context rather than navigating to a separate detail page.
 */
test('task detail uses one closable inspector while preserving the list', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Three column')
  const task = await tasksApi.createTask(USERS.engineerC.id, {
    title,
    // Development E2E sessions intentionally consolidate on sponsorA.
    assignee_id: USERS.sponsorA.id,
  })
  trackTask(task.id)

  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const inspector = page.getByRole('dialog', { name: '任务详情' })
  await expect(inspector.getByRole('heading', { name: title, exact: true })).toBeVisible()
  await expect(page.locator('[role="listitem"]').first()).toBeAttached()

  // The same inspector contract remains stable across desktop breakpoints.
  await page.setViewportSize({ width: 1280, height: 820 })
  await expect(inspector.getByRole('heading', { name: title, exact: true })).toBeVisible()

  // Closing returns to the full list and clears the selected-task URL.
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page).toHaveURL(/\/tasks$/)
  await expect(inspector).toHaveCount(0)

  await page.goto(`/tasks/${task.number}`)
  await page.setViewportSize({ width: 1120, height: 820 })
  await expect(inspector).toBeVisible()
  await expect(page.locator('[role="listitem"]').first()).toBeAttached()

  // Portaled property controls stay inside the inspector's accessibility
  // boundary instead of being hidden by modal isolation.
  await inspector
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

test('the phone header keeps account controls tucked away and task creation visible', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/tasks')
  await expect(page.getByRole('navigation', { name: '底部导航' })).toBeVisible()

  // Account controls remain reachable behind 我的. Task creation stays
  // visible because it is the primary action, not an account switcher.
  await expect(page.getByRole('button', { name: '退出登录' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '新建任务' })).toBeVisible()

  await page.getByRole('button', { name: '我的' }).click()
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()

  // …and they are standing chrome again the moment there is room for them.
  await page.keyboard.press('Escape')
  await page.setViewportSize({ width: 1280, height: 900 })
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
  await expect(
    page.getByRole('main').getByRole('button', { name: '新建任务' }),
  ).toBeVisible()
})

test('opening a task from deep in the list keeps the list position', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const made = []
  for (let i = 0; i < 25; i++) {
    const t = await tasksApi.createTask(USERS.engineerC.id, {
      title: uniqueTitle(`Scroll ${i}`),
      assignee_id: USERS.sponsorA.id,
    })
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
