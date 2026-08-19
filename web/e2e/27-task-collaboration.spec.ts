import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('task attachments: preview Markdown inline and HTML in an isolated page', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const task = await tasksApi.createTask(USERS.engineerC.id, {
    title: uniqueTitle('Attachment previews'),
  })
  trackTask(task.id)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const attachments = page.getByRole('region', { name: '任务附件' })
  const fileInput = attachments.locator('input[type="file"]')

  await fileInput.setInputFiles({
    name: 'decision.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from(`# Decision

- [x] Use the staged rollout.

| Stage | Owner |
| --- | --- |
| Test | Release team |

![Remote diagram](https://tracker.example/diagram.png)`),
  })
  await expect(attachments.getByText('decision.md', { exact: true })).toBeVisible()
  await attachments.getByText('decision.md', { exact: true }).click()
  await expect(attachments.getByRole('heading', { name: 'Decision' })).toBeVisible()
  await expect(attachments.getByText('Use the staged rollout.', { exact: true })).toBeVisible()
  await expect(attachments.getByRole('table')).toBeVisible()
  await expect(attachments.getByText('[外部图片已隐藏：Remote diagram]')).toBeVisible()
  await expect(attachments.locator('img[alt="Remote diagram"]')).toHaveCount(0)

  await fileInput.setInputFiles({
    name: 'prototype.html',
    mimeType: 'text/html',
    buffer: Buffer.from('<!doctype html><title>Prototype</title><h1>Safe prototype</h1><script>document.body.dataset.ran="yes"</script>'),
  })
  await expect(attachments.getByText('prototype.html', { exact: true })).toBeVisible()

  const popupPromise = page.waitForEvent('popup')
  await attachments.getByText('prototype.html', { exact: true }).click()
  const preview = await popupPromise
  await expect(preview).toHaveURL(new RegExp(`/tasks/${task.number}/attachments/.+/preview$`))
  const frame = preview.frameLocator('iframe[title="prototype.html"]')
  await expect(frame.getByRole('heading', { name: 'Safe prototype' })).toBeVisible()
  await expect(frame.locator('body')).toHaveAttribute('data-ran', 'yes')
  await preview.close()
})
