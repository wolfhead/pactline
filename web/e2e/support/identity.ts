import type { Page } from '@playwright/test'
import { expect } from '@playwright/test'
import { USERS } from './config'

/**
 * Establishes a normal application session through the development-only
 * authentication endpoint. The user argument remains in the helper contract
 * while older scenarios are migrated, but consolidated seed data means every
 * scenario now runs as the active primary seed account.
 */
export async function switchIdentity(page: Page, _userId: string): Promise<void> {
  const userSelect = page.getByLabel('Local user')
  const logout = page.getByRole('button', { name: '退出登录' })
  const taskDetail = page.getByRole('dialog', { name: '任务详情' })
  await expect(userSelect.or(logout).or(taskDetail).first()).toBeVisible()
  if (await taskDetail.isVisible()) return
  if (await logout.isVisible()) return

  await expect(userSelect).toBeVisible()
  await userSelect.selectOption(USERS.sponsorA.id)
  await page.getByRole('button', { name: 'Development 登录' }).click()
  await expect(logout).toBeVisible()
}
