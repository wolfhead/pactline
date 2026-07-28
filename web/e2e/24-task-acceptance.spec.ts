import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('task acceptance criteria block completion until evidence satisfies them', async ({
  page,
  uniqueTitle,
  tasksApi,
}) => {
  const task = await tasksApi.createTask(USERS.engineerC.id, {
    title: uniqueTitle('Acceptance-gated task'),
    status: 'in_review',
  })

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const checklist = page.getByRole('region', { name: '验收标准' })
  const detail = page.getByRole('complementary', { name: '任务详情' })
  const status = detail.getByRole('combobox', { name: '状态', exact: true })
  await checklist.getByRole('button', { name: '添加验收项' }).click()
  await checklist.getByPlaceholder('需要成立的可观察事实').fill('The task result is observable')
  await checklist.getByPlaceholder('如何逐项验证').fill('Run the task workflow test')
  await checklist.getByRole('button', { name: '保存' }).click()
  await expect(checklist.getByText('The task result is observable')).toBeVisible()

  await status.click()
  await page.getByRole('option', { name: '已完成' }).click()
  await expect(page.getByText(/task acceptance is not satisfied/)).toBeVisible()
  await expect(status).toHaveText('待评审')

  await checklist.getByRole('button', { name: '检查' }).click()
  await checklist.getByPlaceholder('检查证据或原因').fill('Verified by Playwright')
  await checklist.getByRole('button', { name: '记录' }).click()
  await expect(checklist.getByText(/通过：Verified by Playwright/)).toBeVisible()

  await status.click()
  await page.getByRole('option', { name: '已完成' }).click()
  await expect(status).toHaveText('已完成')
})
