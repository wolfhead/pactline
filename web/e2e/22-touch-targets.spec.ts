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
 * a control and forgets the floor". The selector set mostly mirrors what
 * index.css's blanket rule covers — plain inline text links (a task title in
 * a desktop row) are deliberately outside both — plus two additions the CSS
 * rule does NOT cover by itself, each closing a gap a prior review missed:
 *
 * - `[role="menuitem"]` — RowActionsMenu's 复制链接/归档/恢复 render as
 *   Radix's default `div[role="menuitem"]`, and 打开详情 as a plain `<a>`
 *   (via `asChild`) that also carries `role="menuitem"`. Neither tag is in
 *   the blanket rule's selector list, so both were floored directly on
 *   DropdownMenuItem instead (`pointer-coarse:min-h-11`); this selector is
 *   what proves that floor is actually in effect rather than just present in
 *   source.
 * - `label:has([role="checkbox"])` — Radix's `CheckboxPrimitive.Root` is a
 *   `<button role="checkbox">`, which the blanket rule's `button` clause used
 *   to match and inflate to 44x44 regardless of the component's own `size-4`
 *   (16px) styling — min-height/min-width clamp the *used* size, a different
 *   property from the competing height/width utility, not a cascade
 *   conflict a utility could win back. The fix excludes `[role="checkbox"]`
 *   from `button` and floors the wrapping `<label>` row instead (a `<button>`
 *   is a labelable element, so a click anywhere in the row still reaches the
 *   checkbox). `button` alone would no longer catch this row at all, so it
 *   is added explicitly, scoped to rows that actually wrap a checkbox — a
 *   bare `label` would also match DueDateControl's plain inline caption,
 *   which is correctly NOT floored (min-height does nothing to an inline
 *   box) and would be a false failure here.
 *
 * `[role="checkbox"]` itself is deliberately NOT in this selector: the whole
 * point of the label-row fix is that the checkbox glyph stays 16px while the
 * row around it grows, so a checkbox matching this "must be >=44" sweep
 * would fail by design. Its glyph size is asserted separately, by
 * `expectCheckboxGlyphsNotOversized` below.
 *
 * 820x1100 is not decoration either: it is a coarse-pointer tablet, the case
 * a width-keyed `sm:min-h-8` gets wrong while a 390px test still passes.
 */

const COVERED =
  'button:not([role="checkbox"]), input, select, textarea, summary, nav a, [role="menuitem"], label:has([role="checkbox"])'

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

interface Oversized {
  name: string
  width: number
  height: number
}

/**
 * The glyph inside `Checkbox` is styled `size-4` (16px). A sweep that only
 * ever checks ">= 44" cannot catch a 44px thing that should have been 16px —
 * this is the assertion that closes that gap. 20px leaves headroom above the
 * real 16px value for sub-pixel rendering without coming anywhere near the
 * 44px the regression produced.
 */
async function oversizedCheckboxGlyphs(page: Page): Promise<Oversized[]> {
  return page.evaluate(() => {
    const out: Oversized[] = []
    for (const el of document.querySelectorAll('[role="checkbox"]')) {
      const rect = el.getBoundingClientRect()
      if (rect.width === 0 && rect.height === 0) continue
      if (rect.height <= 20 && rect.width <= 20) continue
      out.push({
        name: (el.getAttribute('aria-label') || '').trim().slice(0, 30),
        width: Math.round(rect.width * 10) / 10,
        height: Math.round(rect.height * 10) / 10,
      })
    }
    return out
  })
}

async function expectCheckboxGlyphsNotOversized(page: Page, where: string) {
  const oversized = await oversizedCheckboxGlyphs(page)
  expect(
    oversized,
    `${where}: ${oversized.map((s) => `“${s.name}” ${s.width}x${s.height}px`).join('; ')}`,
  ).toEqual([])
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

  // FilterBar's 状态 popover: a `Checkbox` per row, wrapped in a `<label>`.
  // This is the exact case the previous review missed — `button` matched
  // Radix's `role="checkbox"` button and clamped the 16px glyph to 44x44.
  await page.getByRole('button', { name: '状态', exact: true }).click()
  await expect(page.getByRole('group', { name: '按状态筛选' })).toBeVisible()
  await page.waitForTimeout(300)
  await expectAllFloored(page, 'phone 390 状态 popover')
  await expectCheckboxGlyphsNotOversized(page, 'phone 390 状态 popover')
  await page.keyboard.press('Escape')

  // RowActionsMenu's "⋯" menu: 打开详情 renders as a plain `<a>` outside any
  // `<nav>`, 复制链接/归档 as Radix's default `div[role="menuitem"]` — neither
  // tag is in the blanket rule's selector set, so both were 32px.
  await page.getByRole('button', { name: `任务 #${task.number} 更多操作` }).click()
  await expect(page.getByRole('menuitem', { name: '打开详情' })).toBeVisible()
  await page.waitForTimeout(300)
  await expectAllFloored(page, 'phone 390 row actions menu')
  await page.keyboard.press('Escape')

  // The 我的 sheet is where the phone's theme/identity switchers now live —
  // off the standing header, but still real controls a finger has to hit.
  await page.getByRole('button', { name: '我的' }).click()
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
  await page.waitForTimeout(600) // the sheet's slide-in transforms the box
  await expectAllFloored(page, 'phone 390 我的 sheet')
  await page.keyboard.press('Escape')

  await page.goto(`/tasks/${task.number}`)
  await expect(page.getByLabel('任务标题')).toBeVisible()
  await expectAllFloored(page, 'phone 390 detail')

  await page.getByRole('button', { name: '截止日期' }).click()
  const calendar = page.getByRole('dialog', { name: '选择截止日期' })
  await expect(calendar).toBeVisible()
  await page.waitForTimeout(300)
  await expectAllFloored(page, 'phone 390 due-date calendar')
  const calendarBox = await calendar.boundingBox()
  expect(calendarBox).not.toBeNull()
  expect(calendarBox!.x).toBeGreaterThanOrEqual(0)
  expect(calendarBox!.x + calendarBox!.width).toBeLessThanOrEqual(390)
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
