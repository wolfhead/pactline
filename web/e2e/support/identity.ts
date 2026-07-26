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
  await select.selectOption(userId)
  await expect(select).toHaveValue(userId)
}
