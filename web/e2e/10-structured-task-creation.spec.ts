import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('task creation requires a durable brief and opens from the primary navigation action', async ({
  page,
  uniqueTitle,
  trackTask,
  tasksApi,
}) => {
  const title = uniqueTitle('Structured task')
  const context = 'The current request exists only in conversation and cannot be understood later.'
  const expectedResult = 'The task retains enough context for a person or Agent to continue independently.'

  await page.goto('/tasks')
  await switchIdentity(page, USERS.engineerC.id)

  await expect(page.getByPlaceholder('输入标题，回车创建任务…')).toHaveCount(0)
  const navigation = page.getByRole('navigation', { name: '主导航' })
  await navigation.getByRole('button', { name: '新建任务' }).click()

  const dialog = page.getByRole('dialog', { name: '新建任务' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('textbox', { name: /背景 \/ 问题/ })).toHaveAttribute('required', '')
  await expect(dialog.getByRole('textbox', { name: /期望结果/ })).toHaveAttribute('required', '')

  await dialog.getByRole('textbox', { name: /标题/ }).fill(title)
  await dialog.getByRole('textbox', { name: /背景 \/ 问题/ }).fill(context)
  await dialog.getByRole('textbox', { name: /期望结果/ }).fill(expectedResult)
  await dialog.getByRole('button', { name: '创建任务' }).click()

  const taskDetails = page.getByRole('dialog', { name: '任务详情' })
  await expect(taskDetails.getByRole('heading', { name: title, exact: true })).toBeVisible()
  const numberLabel = await taskDetails.getByText(/^#\d+$/).textContent()
  const number = Number(numberLabel!.slice(1))
  const created = await tasksApi.getTask(USERS.engineerC.id, number)
  trackTask(created.id)
  expect(created.context).toBe(context)
  expect(created.expected_result).toBe(expectedResult)
})
