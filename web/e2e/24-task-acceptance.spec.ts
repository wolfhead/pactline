import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('review Claim cannot accept until current-cycle acceptance evidence passes', async ({
  page,
  uniqueTitle,
  tasksApi,
}) => {
  const task = await tasksApi.createTask(USERS.engineerC.id, {
    title: uniqueTitle('Acceptance-gated task'),
  })
  await tasksApi.createTaskCriterion(
    USERS.engineerC.id,
    task.number,
    'The task result is observable',
    'Run the task workflow test',
  )
  await tasksApi.markTaskReady(USERS.engineerC.id, task.number)
  const execution = await tasksApi.claimTaskStage(USERS.engineerC.id, task.number)
  await tasksApi.completeTaskExecution(USERS.engineerC.id, task.number, execution.claim)

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const workflow = page.getByRole('region', { name: '任务工作流' })
  await workflow.getByRole('button', { name: '领取验收' }).click()
  await expect(workflow.getByText('验收中 · 正在处理', { exact: true })).toBeVisible()

  await workflow.getByRole('button', { name: '接受并完成' }).click()
  await workflow.getByLabel('接受并完成').fill('Acceptance attempted before evidence')
  await workflow.getByRole('button', { name: '确认' }).click()
  await expect(workflow.getByRole('alert')).toBeVisible()
  await expect(workflow.getByText('验收中 · 正在处理', { exact: true })).toBeVisible()

  const checklist = page.getByRole('region', { name: '验收标准' })
  await checklist.getByRole('button', { name: '检查' }).click()
  await checklist.getByPlaceholder('检查证据或原因').fill('Verified by Playwright')
  await checklist.getByRole('button', { name: '记录' }).click()
  await expect(checklist.getByText('验收 · 通过', { exact: true })).toBeVisible()
  await expect(checklist.getByText('Verified by Playwright', { exact: true })).toBeVisible()

  await workflow.getByRole('button', { name: '接受并完成' }).click()
  await workflow.getByLabel('接受并完成').fill('Acceptance evidence satisfies the current review cycle')
  await workflow.getByRole('button', { name: '确认' }).click()
  await expect(workflow.getByText('已完成', { exact: true })).toBeVisible()
})
