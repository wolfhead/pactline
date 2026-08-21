import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('task detail uses one full page at every breakpoint', async ({
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

  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.locator('[role="listitem"]')).toHaveCount(0)

  // The same full-page contract remains stable across desktop breakpoints.
  await page.setViewportSize({ width: 1280, height: 820 })
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()

  await page.getByRole('link', { name: '返回任务集合' }).click()
  await expect(page).toHaveURL(/\/tasks$/)

  await page.goto(`/tasks/${task.number}`)
  await page.setViewportSize({ width: 1120, height: 820 })
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)

  await page.getByRole('button', { name: '截止日期' }).click()
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

test('returning from a task restores a deep list position', async ({
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
  await page.getByRole('link', { name: '返回任务集合' }).click()
  await expect(page).toHaveURL(/\/tasks$/)
  const after = await page.evaluate(() => document.querySelector('[data-task-list]')!.scrollTop)
  expect(after).toBe(before)
})
