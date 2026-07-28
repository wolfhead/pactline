import type { Locator } from '@playwright/test'
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

  // lg: the same URL now slides the detail over an unshrunken list.
  await page.setViewportSize({ width: 1120, height: 820 })
  await expect(page.getByRole('dialog')).toBeVisible()
  // A CSS attribute selector rather than getByRole('listitem'): Radix marks
  // everything outside an open modal `aria-hidden`, so the accessibility
  // tree reports zero listitems here even though the list is very much
  // still mounted underneath — which is the one thing this line is for.
  await expect(page.locator('[role="listitem"]').first()).toBeAttached()
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

test('every interactive target on a phone clears 44px in real layout', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Touch target')
  const task = await tasksApi.createTask(USERS.engineerC.id, { title })
  trackTask(task.id)

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)
  await expect(page.getByRole('link', { name: title, exact: true })).toBeVisible()

  // Measured, not asserted on a class name: a min-h-11 that a parent's
  // flex settings quietly override would pass a className check and fail
  // here, which is the point.
  async function expectTallEnough(target: Locator, what: string) {
    // hover() first, for its actionability checks rather than the hover
    // itself: boundingBox() reports the live, transformed box, so a target
    // inside a popover that is still playing its zoom-in-95 entry animation
    // measures ~42px for a 44px element. hover() waits until the box has
    // been stable for two animation frames — and, as a bonus, proves the
    // target is actually reachable by a pointer rather than merely present.
    await target.hover()
    const box = await target.boundingBox()
    expect(box, `${what} has no box`).not.toBeNull()
    expect(box!.height, `${what} is only ${box!.height}px tall`).toBeGreaterThanOrEqual(44)
  }

  await expectTallEnough(page.getByRole('combobox', { name: `任务 #${task.number} 状态` }), '行内状态控件')
  await expectTallEnough(page.getByRole('button', { name: `任务 #${task.number} 更多操作` }), '行内更多操作')
  await expectTallEnough(page.getByRole('navigation', { name: '底部导航' }).getByRole('link').first(), '底部导航项')
  await expectTallEnough(page.getByRole('button', { name: '标签', exact: true }), '标签筛选触发器')

  // LabelManager's <summary> lives inside the 标签 popover and is the one
  // target the deleted stylesheet used to floor at 44px on coarse pointers;
  // nothing replaced that when it went, so it is measured here explicitly
  // rather than assumed to have come along with the shared trigger class.
  await page.getByRole('button', { name: '标签', exact: true }).click()
  await expectTallEnough(page.getByText('管理标签', { exact: true }), '管理标签展开器')
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
