import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskDetail from './TaskDetail'
import * as acceptanceApi from '@/api/acceptance'
import * as tasksApi from '@/api/tasks'
import * as projectsApi from '@/api/projects'
import * as workflowApi from '@/api/task-workflow'
import type { Task, TaskThreadItem } from '@/task-types'

vi.mock('@/api/tasks')
vi.mock('@/api/projects')
vi.mock('@/api/acceptance')
vi.mock('@/api/task-workflow')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    me: { id: 'u1', name: '张沁', email: 'a@example.test' },
    users: [{ id: 'u1', name: '张沁', email: 'a@example.test' }],
  }),
}))

const USERS = [{ id: 'u1', name: '张沁', email: 'a@example.test' }]
const TASK: Task = {
  id: 'id-142', number: 142, version: 1, title: '修复竞价超时导致的丢量',
  context: '竞价请求近期频繁超时', expected_result: '恢复稳定流量且不再发生异常丢量',
  description: '丢量比例升到 4.2%', phase: 'backlog', activity: null,
  review_cycle: 0, main_thread_id: '11111111-1111-4111-8111-111111111111',
  priority: 'high', assignee: USERS[0], creator: USERS[0], start_date: null,
  due_date: '2026-07-30', project: { id: 'p1', number: 12, name: 'Task Manager' },
  milestone: null, labels: [], parent: null, children: [], dependencies: [],
  dependents: [], blocked: false, created_at: '', updated_at: '', completed_at: null,
  archived_at: null,
}

const MAIN_THREAD = {
  id: TASK.main_thread_id,
  task_id: TASK.id,
  role: 'main' as const,
  version: 1,
  created_at: '2026-08-13T00:00:00Z',
  updated_at: '2026-08-13T00:00:00Z',
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

beforeEach(() => {
  vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
  vi.mocked(tasksApi.listActivity).mockResolvedValue([])
  vi.mocked(tasksApi.listTaskAttachments).mockResolvedValue([])
  vi.mocked(tasksApi.listLabels).mockResolvedValue([])
  vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([])
  vi.mocked(projectsApi.getProject).mockRejectedValue(new Error('No milestones in fixture'))
  vi.mocked(acceptanceApi.listTaskCriteria).mockResolvedValue([])
  vi.mocked(workflowApi.listTaskStageClaims).mockResolvedValue([])
  vi.mocked(workflowApi.listTaskThreads).mockResolvedValue([MAIN_THREAD])
  vi.mocked(workflowApi.listThreadItems).mockResolvedValue([])
})

function renderDetail(onPatched = vi.fn()) {
  render(
    <MemoryRouter>
      <TaskDetail number={142} users={USERS} onPatched={onPatched} />
    </MemoryRouter>,
  )
  return onPatched
}

describe('TaskDetail', () => {
  it('shows command-driven phase state without status or execution-mode editors', async () => {
    renderDetail()
    await screen.findByText(TASK.title)

    expect(screen.getByRole('status', { name: '待规划' })).toBeVisible()
    expect(screen.getByRole('button', { name: '标记可领取' })).toBeVisible()
    expect(screen.queryByRole('combobox', { name: '状态' })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: '执行方式' })).not.toBeInTheDocument()
  })

  it('keeps Project immutable while leaving Milestone editable', async () => {
    renderDetail()
    await screen.findByText(TASK.title)

    expect(screen.getByText('#12 Task Manager')).toHaveAttribute(
      'title',
      '任务创建后不能移到其他项目',
    )
    expect(screen.queryByLabelText('项目')).not.toBeInTheDocument()
    expect(screen.getByLabelText('里程碑')).toBeVisible()
  })

  it('marks a backlog Task ready through the workflow command and reloads it', async () => {
    const readyTask: Task = { ...TASK, version: 2, phase: 'ready', activity: 'available' }
    vi.mocked(workflowApi.markTaskReady).mockResolvedValue({
      task_id: TASK.id, task_number: TASK.number, version: 2,
      phase: 'ready', activity: 'available', review_cycle: 0,
      main_thread_id: TASK.main_thread_id,
    })
    vi.mocked(tasksApi.getTask)
      .mockResolvedValueOnce(TASK)
      .mockResolvedValueOnce(readyTask)

    const onPatched = renderDetail()
    fireEvent.click(await screen.findByRole('button', { name: '标记可领取' }))

    await waitFor(() => expect(workflowApi.markTaskReady).toHaveBeenCalledWith(142, 1))
    await waitFor(() => expect(onPatched).toHaveBeenCalledWith(readyTask))
  })

  it('separates repeatable work records from completing execution', async () => {
	const workingTask: Task = {
		...TASK, version: 3, phase: 'in_progress', activity: 'working', review_cycle: 0,
	}
	const executionClaim = {
		id: '33333333-3333-4333-8333-333333333333', task_id: TASK.id,
		task_number: TASK.number, stage: 'execution' as const,
		claimed_by: { type: 'user' as const, user_id: 'u1' }, subject_user_id: 'u1',
		authentication_method: 'session' as const, client_kind: 'web', status: 'active' as const,
		version: 1, expires_at: '', created_at: '', updated_at: '',
	}
	vi.mocked(tasksApi.getTask).mockResolvedValue(workingTask)
	vi.mocked(workflowApi.listTaskStageClaims).mockResolvedValue([executionClaim])
	vi.mocked(workflowApi.recordTaskWorkSubmission).mockResolvedValue({
		task: {
			task_id: TASK.id, task_number: TASK.number, version: 3,
			phase: 'in_progress', activity: 'working', review_cycle: 0,
			main_thread_id: TASK.main_thread_id,
		},
		claim: executionClaim,
		submission: {
			id: '44444444-4444-4444-8444-444444444444', thread_id: TASK.main_thread_id,
			kind: 'work_submission', author: executionClaim.claimed_by,
			body: '实现与测试完成。', mentioned_user_ids: [], version: 1,
			created_at: '', updated_at: '',
		},
	})

	renderDetail()
	await screen.findByText(TASK.title)
	expect(screen.getByRole('button', { name: '记录工作' })).toBeVisible()
	expect(screen.getByRole('button', { name: '完成执行，进入验收' })).toBeVisible()

	fireEvent.click(screen.getByRole('button', { name: '记录工作' }))
	fireEvent.change(screen.getByLabelText('记录工作'), { target: { value: '实现与测试完成。' } })
	fireEvent.click(screen.getByRole('button', { name: '确认' }))

	await waitFor(() => expect(workflowApi.recordTaskWorkSubmission).toHaveBeenCalledWith(
		142, 3, executionClaim, '实现与测试完成。',
	))
	expect(workflowApi.completeTaskExecution).not.toHaveBeenCalled()
  })

  it('renders Main Thread and posts a human message through the unified API', async () => {
    vi.mocked(workflowApi.createThreadMessage).mockResolvedValue({
      id: '22222222-2222-4222-8222-222222222222',
      thread_id: MAIN_THREAD.id,
      kind: 'message',
      author: { type: 'user', user_id: 'u1' },
      body: '补充复现日志。',
      mentioned_user_ids: [],
      version: 1,
      created_at: '2026-08-13T01:00:00Z',
      updated_at: '2026-08-13T01:00:00Z',
    })
    renderDetail()

    const composer = await screen.findByRole('textbox', { name: '向当前 Thread 发送消息' })
    fireEvent.change(composer, { target: { value: '补充复现日志。' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => expect(workflowApi.createThreadMessage).toHaveBeenCalledWith(
      MAIN_THREAD.id,
      '补充复现日志。',
      'message',
    ))
    expect(await screen.findByText('补充复现日志。')).toBeVisible()
  })

  it('posts an immutable progress Item through the same Main Thread composer', async () => {
    vi.mocked(workflowApi.createThreadMessage).mockResolvedValue({
      id: '33333333-3333-4333-8333-333333333333',
      thread_id: MAIN_THREAD.id,
      kind: 'progress',
      author: { type: 'agent', ref: 'api-token/codex' },
      body: '实现和定向测试已完成。',
      mentioned_user_ids: [],
      version: 1,
      created_at: '2026-08-13T02:00:00Z',
      updated_at: '2026-08-13T02:00:00Z',
    })
    renderDetail()

    fireEvent.change(await screen.findByLabelText('Thread Item 类型'), {
      target: { value: 'progress' },
    })
    fireEvent.change(screen.getByRole('textbox', { name: '向当前 Thread 发送消息' }), {
      target: { value: '实现和定向测试已完成。' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => expect(workflowApi.createThreadMessage).toHaveBeenCalledWith(
      MAIN_THREAD.id,
      '实现和定向测试已完成。',
      'progress',
    ))
    const progress = screen.getByText('实现和定向测试已完成。').closest('article')
    expect(progress).not.toBeNull()
    expect(within(progress!).getByText('进展', { exact: true })).toBeVisible()
  })

  it('preserves a new draft when the previous Thread Item finishes sending', async () => {
    let finishSending!: (item: TaskThreadItem) => void
    vi.mocked(workflowApi.createThreadMessage).mockReturnValue(new Promise((resolve) => {
      finishSending = resolve
    }))
    renderDetail()

    const composer = await screen.findByRole('textbox', { name: '向当前 Thread 发送消息' })
    fireEvent.change(composer, { target: { value: '第一条消息' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))
    fireEvent.change(composer, { target: { value: '下一条草稿' } })

    finishSending({
      id: '44444444-4444-4444-8444-444444444444',
      thread_id: MAIN_THREAD.id,
      kind: 'message',
      author: { type: 'user', user_id: 'u1' },
      body: '第一条消息',
      mentioned_user_ids: [],
      version: 1,
      created_at: '2026-08-13T03:00:00Z',
      updated_at: '2026-08-13T03:00:00Z',
    })

    await screen.findByText('第一条消息')
    expect(composer).toHaveValue('下一条草稿')
    expect(screen.getByRole('button', { name: '发送消息' })).toBeEnabled()
  })
})
