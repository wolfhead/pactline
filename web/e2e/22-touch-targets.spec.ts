import type { Page } from '@playwright/test'
import { test, expect } from './support/task-fixtures'
import { USERS } from './support/config'

/**
 * The 44px touch floor, measured on a genuinely coarse pointer.
 *
 * This file runs ONLY under the `chromium-touch` project (playwright.config),
 * which sets `hasTouch` — that is what makes Chromium answer true to
 * `(pointer: coarse)`, the media query index.css's floor is keyed on. The
 * predecessor of this test ran on Desktop Chrome at 390px and would have
 * passed against every one of the seven under-floor controls it was named
 * for: at 390px an unconditional `min-h-11` supplied the height whether the
 * coarse-pointer rule existed or not.
 *
 * It sweeps by selector rather than by a hand-written list of controls,
 * because the failure mode being guarded against is precisely "someone adds
 * a control and forgets the floor". The selector set is exactly what
 * index.css's blanket rule covers — plain inline text links (a task title in
 * a desktop row) are deliberately outside both.
 *
 * 820x1100 is not decoration either: it is a coarse-pointer tablet, the case
 * a width-keyed `sm:min-h-8` gets wrong while a 390px test still passes.
 */

const COVERED = 'button, input, select, textarea, summary, nav a'

interface Short {
  tag: string
  name: string
  height: number
}

async function shortTargets(page: Page): Promise<Short[]> {
  return page.evaluate((selector) => {
    const out: Short[] = []
    for (const el of document.querySelectorAll(selector)) {
      const rect = el.getBoundingClientRect()
      if (rect.width === 0 && rect.height === 0) continue
      const style = getComputedStyle(el)
      if (style.display === 'none' || style.visibility === 'hidden') continue
      // Sub-pixel: a 44px box measures 43.99… under a fractional device
      // scale, which is a rounding artefact and not a real failure.
      if (rect.height >= 43.5) continue
      out.push({
        tag: el.tagName.toLowerCase(),
        name: (el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 30),
        height: Math.round(rect.height * 10) / 10,
      })
    }
    return out
  }, COVERED) as Promise<Short[]>
}

async function expectAllFloored(page: Page, where: string) {
  const short = await shortTargets(page)
  expect(short, `${where}: ${short.map((s) => `${s.tag} “${s.name}” ${s.height}px`).join('; ')}`).toEqual([])
}

test('the coarse-pointer context this file needs is actually in effect', async ({ page }) => {
  await page.goto('/tasks')
  const media = await page.evaluate(() => ({
    coarse: matchMedia('(pointer: coarse)').matches,
    hoverNone: matchMedia('(hover: none)').matches,
  }))
  // Asserted, not assumed: if this ever flips to false the sweeps below go
  // green while measuring nothing, which is the exact failure this file
  // replaced.
  expect(media.coarse).toBe(true)
  expect(media.hoverNone).toBe(true)
})

test('every covered control clears 44px on a phone', async ({ page, uniqueTitle, trackTask, tasksApi }) => {
  const title = uniqueTitle('Touch target')
  const task = await tasksApi.createTask(USERS.sponsorA.id, { title })
  trackTask(task.id)

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/tasks')
  await expect(page.getByRole('link', { name: title, exact: true })).toBeVisible()
  await expectAllFloored(page, 'phone 390 list')

  // The 我的 sheet is where the phone's theme/identity switchers now live —
  // off the standing header, but still real controls a finger has to hit.
  await page.getByRole('button', { name: '我的' }).click()
  await expect(page.getByLabel('当前身份')).toBeVisible()
  await page.waitForTimeout(600) // the sheet's slide-in transforms the box
  await expectAllFloored(page, 'phone 390 我的 sheet')
  await page.keyboard.press('Escape')

  await page.goto(`/tasks/${task.number}`)
  await expect(page.getByLabel('任务标题')).toBeVisible()
  await expectAllFloored(page, 'phone 390 detail')
})

test('every covered control clears 44px on a coarse-pointer tablet', async ({ page }) => {
  await page.setViewportSize({ width: 820, height: 1100 })
  await page.goto('/tasks')
  await expect(page.getByRole('button', { name: '打开导航' })).toBeVisible()
  await expectAllFloored(page, 'tablet 820 list')

  await page.getByRole('button', { name: '打开导航' }).click()
  await expect(page.getByRole('navigation', { name: '主导航' })).toBeVisible()
  await page.waitForTimeout(600)
  await expectAllFloored(page, 'tablet 820 nav drawer')
  await page.keyboard.press('Escape')

  // The 标签 popover carries LabelManager — its <summary>, rename fields and
  // 新建标签 button are the controls a width-keyed floor got wrong here while
  // passing at 390px.
  await page.getByRole('button', { name: '标签', exact: true }).click()
  await expect(page.getByText('管理标签', { exact: true })).toBeVisible()
  await page.getByText('管理标签', { exact: true }).click()
  await page.waitForTimeout(600)
  await expectAllFloored(page, 'tablet 820 标签 popover with LabelManager open')
})
