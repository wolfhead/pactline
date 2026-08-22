import { test, expect } from './support/task-fixtures'
import type { Request } from '@playwright/test'
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

  await expect(page).toHaveURL(`/projects/${project.number}`)
  const workspaceRequests: string[] = []
  const recordWorkspaceRequest = (request: Request) => {
    if (request.method() === 'GET') workspaceRequests.push(new URL(request.url()).pathname)
  }
  page.on('request', recordWorkspaceRequest)
  await page.reload()
  await page.waitForLoadState('networkidle')
  page.off('request', recordWorkspaceRequest)

  const aggregateRequests = workspaceRequests.filter(
    (path) => path === `/api/v1/projects/${project.number}`,
  )
  const memberRequests = workspaceRequests.filter(
    (path) => path === `/api/v1/projects/${project.number}/members`,
  )
  expect(aggregateRequests.length).toBeGreaterThan(0)
  expect(aggregateRequests.length).toBeLessThanOrEqual(2)
  expect(memberRequests.length).toBeGreaterThan(0)
  expect(memberRequests.length).toBeLessThanOrEqual(2)
  expect(workspaceRequests.filter((path) => path.startsWith(`/api/v1/projects/${project.number}/milestones/`)))
    .toHaveLength(0)
  expect(workspaceRequests.filter((path) => path === '/api/v1/tasks'))
    .toHaveLength(0)

  await page.getByRole('button', { name: '项目详情', exact: true }).click()
  await page.getByRole('button', { name: '编辑项目', exact: true }).click()
  await page.getByLabel('项目说明').fill('Edited durable Project context')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('项目说明')).not.toBeVisible()
  await expect(page.getByRole('paragraph').filter({ hasText: 'Edited durable Project context' })).toBeVisible()

  await page.getByRole('button', { name: '新建里程碑' }).click()
  await page.getByLabel('里程碑名称').fill('API ready')
  await page.getByLabel('阶段成果').fill('The Project-first workflow is verified')
  await page.getByRole('button', { name: '创建', exact: true }).click()

  await page.getByRole('link', { name: /API ready/ }).click()
  await page.getByRole('button', { name: '里程碑详情', exact: true }).click()
  await page.getByRole('button', { name: '编辑里程碑', exact: true }).click()
  await page.getByLabel('里程碑说明').fill('Edited Milestone context')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByLabel('里程碑说明')).not.toBeVisible()
  await expect(page.getByText('Edited Milestone context')).toBeVisible()

  await page.getByRole('button', { name: /验收 0\/0/ }).click()
  const checklist = page.getByRole('region', { name: '里程碑验收标准' })
  await checklist.getByRole('button', { name: '添加验收项' }).click()
  await checklist.getByPlaceholder('需要成立的可观察事实').fill('The API check passes')
  await checklist.getByPlaceholder('如何逐项验证').fill('Run API integration tests')
  await checklist.getByRole('button', { name: '保存' }).click()

  // The activation button remains disabled until the versioned criterion
  // mutation has refreshed the Project and Milestone aggregate versions.
  await page.getByRole('button', { name: '激活' }).click()
  await expect(checklist.getByText('The API check passes')).toBeVisible()
  await expect(page.getByText(/状态：进行中/)).toBeVisible()

  const taskResponse = page.waitForResponse(
    (response) => response.url().endsWith('/api/v1/tasks')
      && response.request().method() === 'POST',
  )
  await page.getByRole('main').getByRole('button', { name: '新建任务' }).click()
  const taskComposer = page.getByRole('dialog', { name: '新建任务' })
  await taskComposer.getByRole('textbox', { name: /标题/ }).fill(taskTitle)
  await taskComposer.getByRole('textbox', { name: /背景 \/ 问题/ })
    .fill('The milestone needs a verifiable delivery task.')
  await taskComposer.getByRole('textbox', { name: /期望结果/ })
    .fill('The task completes inside the selected milestone.')
  await taskComposer.getByRole('button', { name: '创建任务', exact: true }).click()
  const task = await (await taskResponse).json() as { id: string; number: number }
  await tasksApi.completeTask(USERS.sponsorA.id, task.number)
  await page.reload()
  await expect(page.getByRole('link', { name: taskTitle })).toBeVisible()

  await checklist.getByRole('button', { name: '检查' }).click()
  await checklist.getByPlaceholder('检查证据或原因').fill('Verified by Playwright')
  await checklist.getByRole('button', { name: '记录' }).click()
  await expect(checklist.locator('p', { hasText: /^通过$/ })).toBeVisible()
  await expect(checklist.getByText('Verified by Playwright', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '里程碑详情', exact: true }).click()
  await page.getByRole('button', { name: '完成', exact: true }).click()
  await expect(page.getByText(/状态：已完成/)).toBeVisible()

  await page.getByRole('link', { name: '← 项目交付' }).click()
  await page.getByRole('button', { name: '项目详情', exact: true }).click()
  await page.getByRole('button', { name: '归档项目', exact: true }).click()
  await expect(page.getByText('已归档', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '恢复项目', exact: true }).click()
  await expect(page.getByText('已归档', { exact: true })).not.toBeVisible()
})
