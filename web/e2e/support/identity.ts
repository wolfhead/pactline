import type { Page } from '@playwright/test'
import { expect } from '@playwright/test'

/**
 * Switches the current identity via the header's "当前身份" switcher — the
 * same interaction a real user performs. Identity carries no separate
 * confirmation UI beyond the select reflecting the choice, so the assertion
 * here is on the control's own state rather than on any follow-on page
 * content (each test asserts what it needs after switching).
 */
export async function switchIdentity(page: Page, userId: string): Promise<void> {
  const select = page.getByLabel('当前身份')
  // On a phone the switchers are not standing header chrome — they live one
  // tap behind the bottom bar's 我的 tab, so the list gets that ~90px back.
  // Every wider tier still has them in the header.
  //
  // The `.or()` is load-bearing, not defensive: without it the branch below
  // would be decided against a page that has merely started loading, where
  // NEITHER control exists yet, and every desktop spec would take the phone
  // path and time out looking for a 我的 tab. This waits until the shell has
  // actually rendered one of the two before choosing.
  const meTab = page.getByRole('button', { name: '我的' })
  await expect(select.or(meTab).first()).toBeVisible()
  if (!(await select.isVisible())) {
    await meTab.click()
    await expect(select).toBeVisible()
  }
  await select.selectOption(userId)
  await expect(select).toHaveValue(userId)
}
