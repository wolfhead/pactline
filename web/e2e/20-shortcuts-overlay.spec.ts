import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

/**
 * Scenario: the shortcut overlay. "?" opens it, Escape closes it.
 */
test('"?" opens the shortcuts overlay and Escape closes it', async ({ page }) => {
  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  const dialog = page.getByRole('dialog', { name: '键盘快捷键' })
  await expect(dialog).not.toBeVisible()

  await page.keyboard.press('?')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('新建任务')).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(dialog).not.toBeVisible()
})
