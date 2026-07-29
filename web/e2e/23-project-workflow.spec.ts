import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('a long-lived Project plans and completes an evidence-backed Milestone', async ({
  page,
  uniqueTitle,
  trackProject,
  tasksApi,
}) => {
  const projectName = uniqueTitle('Project workspace')
  const taskTitle = uniqueTitle('Milestone task')

  await page.goto('/projects')
  await switchIdentity(page, USERS.sponsorA.id)
  await page.getByRole('button', { name: '新建项目' }).click()
  await page.getByLabel('项目名称').fill(projectName)
  await page.getByLabel('项目说明').fill('A durable workspace for Project-first acceptance')

  const createProjectResponse = page.waitForResponse(
    (response) => response.url().endsWith('/api/v1/projects')
      && response.request().method() === 'POST',
  )
  await page.getByRole('button', { name: '创建', exact: true }).click()
  const project = await (await createProjectResponse).json() as { id: string; number: number }
  trackProject(project.id)

  await expect(page).toHaveURL(`/projects/${project.number}/overview`)
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await page.getByLabel('项目说明').fill('Edited durable Project context')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('项目说明')).not.toBeVisible()
  await expect(page.getByRole('paragraph').filter({ hasText: 'Edited durable Project context' })).toBeVisible()

  await page.getByRole('link', { name: '里程碑', exact: true }).click()
  await page.getByRole('button', { name: '新建里程碑' }).click()
  await page.getByPlaceholder('里程碑名称').fill('API ready')
  await page.getByPlaceholder('可验证的阶段成果').fill('The Project-first workflow is verified')
  await page.getByRole('button', { name: '创建', exact: true }).click()

  await page.getByRole('link', { name: /API ready/ }).click()
  await page.getByRole('button', { name: '编辑', exact: true }).nth(1).click()
  await page.getByLabel('里程碑说明').fill('Edited Milestone context')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('里程碑说明')).not.toBeVisible()
  await expect(page.getByText('Edited Milestone context')).toBeVisible()

  const checklist = page.getByRole('region', { name: '里程碑验收标准' })
  await checklist.getByRole('button', { name: '添加验收项' }).click()
  await checklist.getByPlaceholder('需要成立的可观察事实').fill('The API check passes')
  await checklist.getByPlaceholder('如何逐项验证').fill('Run API integration tests')
  await checklist.getByRole('button', { name: '保存' }).click()

  // The activation button remains disabled until the versioned criterion
  // mutation has refreshed the Project and Milestone aggregate versions.
  await page.getByRole('button', { name: '激活' }).click()
  await expect(checklist.getByText('The API check passes')).toBeVisible()
  await expect(page.getByText('进行中', { exact: true }).first()).toBeVisible()

  const taskResponse = page.waitForResponse(
    (response) => response.url().endsWith('/api/v1/tasks')
      && response.request().method() === 'POST',
  )
  await page.getByLabel('新建里程碑任务').fill(taskTitle)
  await page.getByRole('button', { name: '创建', exact: true }).click()
  const task = await (await taskResponse).json() as { id: string; number: number }
  await tasksApi.updateTask(USERS.sponsorA.id, task.number, { status: 'done' })
  await page.reload()
  await expect(page.getByRole('link', { name: taskTitle })).toBeVisible()

  await checklist.getByRole('button', { name: '检查' }).click()
  await checklist.getByPlaceholder('检查证据或原因').fill('Verified by Playwright')
  await checklist.getByRole('button', { name: '记录' }).click()
  await expect(checklist.getByText(/通过：Verified by Playwright/)).toBeVisible()

  await page.getByRole('button', { name: '完成', exact: true }).click()
  await expect(page.getByText('已完成', { exact: true }).first()).toBeVisible()

  await page.getByRole('link', { name: '整体视图' }).click()
  await page.getByRole('button', { name: '归档', exact: true }).click()
  await expect(page.getByText('已归档', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '恢复', exact: true }).click()
  await expect(page.getByText('已归档', { exact: true })).not.toBeVisible()
})
