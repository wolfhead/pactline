import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('Main Thread messages can be created, edited, and tombstoned', async ({
  page,
  uniqueTitle,
  tasksApi,
}) => {
  const task = await tasksApi.createTask(USERS.engineerC.id, {
    title: uniqueTitle('Thread message lifecycle'),
  })

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const thread = page.getByRole('region', { name: '任务 Thread' })
  await expect(thread.getByRole('tab', { name: 'Main' })).toHaveAttribute('aria-selected', 'true')
  await expect(thread.getByText('这个 Thread 还没有内容。')).toBeVisible()

  await thread.getByLabel('向当前 Thread 发送消息').fill('First Main Thread message')
  await thread.getByRole('button', { name: '发送消息' }).click()
  await expect(thread.getByText('First Main Thread message', { exact: true })).toBeVisible()

  await thread.getByLabel('Thread Item 类型').selectOption('progress')
  await thread.getByLabel('向当前 Thread 发送消息').fill(`## Verification

- Implementation is verified
- Mobile layout is ready

| Check | Result |
| --- | --- |
| TypeScript | Passed |

\`\`\`
this-is-a-long-code-line-that-must-scroll-inside-the-task-detail-column-without-widening-the-page
\`\`\``)
  await thread.getByRole('tab', { name: '预览' }).click()
  await expect(thread.getByRole('heading', { name: 'Verification', level: 2 })).toBeVisible()
  await expect(thread.getByRole('table')).toBeVisible()
  await thread.getByRole('button', { name: '发送消息' }).click()
  const progress = thread.getByRole('article').filter({ hasText: 'Implementation is verified' })
  await expect(progress.getByRole('heading', { name: 'Verification', level: 2 })).toBeVisible()
  await expect(progress.getByRole('region', { name: '代码块' })).toBeVisible()
  await expect(progress.getByText('进展', { exact: true })).toBeVisible()
  await expect(progress.getByRole('button', { name: '编辑' })).toHaveCount(0)
  await expect(progress.getByRole('button', { name: '删除' })).toHaveCount(0)

  const message = thread.getByRole('article').filter({ hasText: 'First Main Thread message' })
  await message.getByRole('button', { name: '编辑' }).click()
  const editor = thread.getByLabel('编辑消息')
  await editor.fill('Edited Main Thread message')
  await editor.locator('xpath=ancestor::article').getByRole('button', { name: '保存' }).click()
  await expect(thread.getByText('Edited Main Thread message', { exact: true })).toBeVisible()

  await page.reload()
  const reloadedThread = page.getByRole('region', { name: '任务 Thread' })
  const reloadedMessage = reloadedThread.getByRole('article').filter({ hasText: 'Edited Main Thread message' })
  await expect(reloadedMessage).toBeVisible()
  await reloadedMessage.getByRole('button', { name: '删除' }).click()
  await expect(reloadedThread.getByText('Edited Main Thread message', { exact: true })).not.toBeVisible()
  await expect(reloadedThread.getByText('消息已删除', { exact: true })).toBeVisible()
})
