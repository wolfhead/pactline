import { test, expect } from './support/task-fixtures'
import { switchIdentity } from './support/identity'
import { USERS } from './support/config'

test('workflow commands claim, block, resolve, and resume Task execution', async ({
  page,
  uniqueTitle,
  tasksApi,
}) => {
  const task = await tasksApi.createTask(USERS.engineerC.id, {
    title: uniqueTitle('Task workflow commands'),
  })

  await page.goto(`/tasks/${task.number}`)
  await switchIdentity(page, USERS.engineerC.id)

  const workflow = page.getByRole('region', { name: '任务工作流' })
  await expect(workflow.getByText('待规划', { exact: true })).toBeVisible()

  const readyResponse = page.waitForResponse(
    (response) => response.url().endsWith(`/api/v1/tasks/${task.number}/commands/mark-ready`)
      && response.request().method() === 'POST',
  )
  await workflow.getByRole('button', { name: '标记可领取' }).click()
  expect((await readyResponse).status()).toBe(200)
  await expect(workflow.getByText('可领取', { exact: true })).toBeVisible()

  const claimResponse = page.waitForResponse(
    (response) => response.url().endsWith(`/api/v1/tasks/${task.number}/claims`)
      && response.request().method() === 'POST',
  )
  await workflow.getByRole('button', { name: '领取执行' }).click()
  expect((await claimResponse).status()).toBe(201)
  await expect(workflow.getByText('执行中 · 正在处理', { exact: true })).toBeVisible()
  await expect(workflow.getByText(/执行已被你领取/)).toBeVisible()

  await workflow.getByRole('button', { name: '请求解决' }).click()
  await workflow.getByLabel('Issue 类型').selectOption('dependency_required')
  await workflow.getByLabel('请求解决').fill('A dependency needs an explicit resolution')
  await workflow.getByRole('button', { name: '确认' }).click()
  await expect(workflow.getByText('执行中 · 等待解决', { exact: true })).toBeVisible()
  await expect(workflow.getByText('当前等待解决：需要解决依赖项。')).toBeVisible()

  const thread = page.getByRole('region', { name: '任务 Thread' })
  const openIssueTab = thread.getByRole('tab', { name: /待解决 · 依赖/ })
  await expect(openIssueTab).toBeVisible()
  await openIssueTab.click()
  await expect(thread.getByText('A dependency needs an explicit resolution', { exact: true })).toBeVisible()

  await workflow.getByRole('button', { name: '解决 Issue' }).click()
  await workflow.getByLabel('解决 Issue').fill('The dependency is now available')
  await workflow.getByRole('button', { name: '确认' }).click()
  await expect(workflow.getByText('执行中 · 等待领取', { exact: true })).toBeVisible()

  await thread.getByRole('tab', { name: 'Main' }).click()
  const mergedResolution = thread.getByRole('article').filter({ hasText: 'The dependency is now available' })
  await expect(mergedResolution.getByRole('button', { name: '查看完整 Issue' })).toBeVisible()
  await mergedResolution.getByRole('button', { name: '查看完整 Issue' }).click()
  await expect(thread.getByRole('tab', { name: /已解决 · 依赖/ })).toHaveAttribute('aria-selected', 'true')

  await workflow.getByRole('button', { name: '继续执行' }).click()
  await expect(workflow.getByText('执行中 · 正在处理', { exact: true })).toBeVisible()

  await page.reload()
  const reloadedWorkflow = page.getByRole('region', { name: '任务工作流' })
  await expect(reloadedWorkflow.getByText('执行中 · 正在处理', { exact: true })).toBeVisible()

  await reloadedWorkflow.getByRole('button', { name: '提交验收' }).click()
  await reloadedWorkflow.getByLabel('提交验收').fill('The resumed execution produced the requested outcome')
  await reloadedWorkflow.getByRole('button', { name: '确认' }).click()
  await expect(reloadedWorkflow.getByText('验收中 · 等待领取', { exact: true })).toBeVisible()

  await reloadedWorkflow.getByRole('button', { name: '领取验收' }).click()
  await expect(reloadedWorkflow.getByText('验收中 · 正在处理', { exact: true })).toBeVisible()
  await reloadedWorkflow.getByRole('button', { name: '接受并完成' }).click()
  await reloadedWorkflow.getByLabel('接受并完成').fill('The Task outcome satisfies its acceptance contract')
  await reloadedWorkflow.getByRole('button', { name: '确认' }).click()
  await expect(reloadedWorkflow.getByText('已完成', { exact: true })).toBeVisible()
})
