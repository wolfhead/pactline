import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('a project moves from planned scope through milestone and evidence-backed completion', async ({
  page,
  uniqueTitle,
  trackProject,
  tasksApi,
}) => {
  const projectName = uniqueTitle('Project workflow')
  const taskTitle = uniqueTitle('Project task')

  await page.goto('/projects')
  await switchIdentity(page, USERS.sponsorA.id)
  await page.getByRole('button', { name: '新建项目' }).click()
  await page.getByLabel('项目名称').fill(projectName)
  await page.getByLabel('预期成果').fill('The complete project workflow is verifiable')

  const createProjectResponse = page.waitForResponse(
    (response) => response.url().endsWith('/api/projects') && response.request().method() === 'POST',
  )
  await page.getByRole('button', { name: '创建', exact: true }).click()
  const project = await (await createProjectResponse).json() as { id: string; number: number }
  trackProject(project.id)

  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await page.getByLabel('项目说明').fill('Edited project context')
  await page.getByRole('button', { name: '保存修改' }).click()
  await expect(page.getByRole('button', { name: '保存修改' })).not.toBeVisible()

  const projectAcceptance = page.getByRole('region', { name: '项目验收标准' })
  await projectAcceptance.getByRole('button', { name: '添加验收项' }).click()
  await projectAcceptance.getByPlaceholder('需要成立的可观察事实').fill('The workflow passes end to end')
  await projectAcceptance.getByPlaceholder('如何逐项验证').fill('Run this Playwright scenario')
  await projectAcceptance.getByRole('button', { name: '保存' }).click()
  await expect(projectAcceptance.getByText('The workflow passes end to end')).toBeVisible()

  await page.getByRole('button', { name: '激活项目' }).click()
  await expect(page.getByText('进行中', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: '添加里程碑' }).click()
  await page.getByLabel('名称').fill('API ready')
  await page.getByLabel('阶段成果').fill('The project API is ready')
  await page.getByRole('button', { name: '保存' }).click()

  const milestone = page.getByRole('article').filter({ hasText: 'API ready' })
  await milestone.getByRole('button', { name: '编辑', exact: true }).click()
  await milestone.getByLabel('里程碑说明').fill('Edited milestone context')
  await milestone.getByRole('button', { name: '保存修改' }).click()
  await expect(milestone.getByRole('button', { name: '保存修改' })).not.toBeVisible()

  await milestone.getByRole('button', { name: '添加验收项' }).click()
  await milestone.getByPlaceholder('需要成立的可观察事实').fill('The API check passes')
  await milestone.getByPlaceholder('如何逐项验证').fill('Run API integration tests')
  await milestone.getByRole('button', { name: '保存' }).click()

  const task = await tasksApi.createTask(USERS.sponsorA.id, {
    title: taskTitle,
    status: 'done',
    project_number: project.number,
  })
  await page.goto(`/tasks/${task.number}`)
  await page.getByLabel('里程碑').selectOption({ label: 'API ready' })
  await expect(page.getByLabel('里程碑')).toHaveValue(/.+/)

  await page.goto(`/projects/${project.number}`)
  await expect(page.getByRole('link', { name: taskTitle })).toBeVisible()
  await expect(milestone.getByText(/任务进度：1\/1/)).toBeVisible()

  for (const checklist of [
    page.getByRole('region', { name: '项目验收标准' }),
    page.getByRole('article').filter({ hasText: 'API ready' }).getByRole('region', { name: '里程碑验收标准' }),
  ]) {
    await checklist.getByRole('button', { name: '检查' }).click()
    await checklist.getByPlaceholder('检查证据或原因').fill('Verified by Playwright')
    await checklist.getByRole('button', { name: '记录' }).click()
    await expect(checklist.getByText(/通过：Verified by Playwright/)).toBeVisible()
  }

  await page.getByRole('article').filter({ hasText: 'API ready' })
    .getByRole('button', { name: '完成里程碑' }).click()
  await page.getByRole('button', { name: '完成项目' }).click()
  await expect(page.getByText('已完成', { exact: true }).first()).toBeVisible()

  page.once('dialog', (dialog) => dialog.accept('A follow-up delivery is required'))
  await page.getByRole('button', { name: '重新开启', exact: true }).click()
  await expect(page.getByText('进行中', { exact: true }).first()).toBeVisible()
  await expect(page.getByText(/重新开启了项目：A follow-up delivery is required/)).toBeVisible()
  await page.getByRole('button', { name: '完成项目' }).click()
  await expect(page.getByText('已完成', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: '归档', exact: true }).click()
  await expect(page.getByText('已归档', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '恢复', exact: true }).click()
  await expect(page.getByText('已完成', { exact: true }).first()).toBeVisible()
})
