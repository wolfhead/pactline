import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import TaskListPage from './TaskListPage'
import * as acceptanceApi from '@/api/acceptance'
import * as tasksApi from '@/api/tasks'
import * as projectsApi from '@/api/projects'
import * as workflowApi from '@/api/task-workflow'
import { ProblemError } from '@/api/v1/client'

vi.mock('@/api/tasks')
vi.mock('@/api/projects')
vi.mock('@/api/acceptance')
vi.mock('@/api/task-workflow')
vi.mock('@/components/tasks/TaskComposer', () => ({
  useTaskComposer: () => ({ openTaskComposer: vi.fn() }),
}))
vi.mock('@/identity', async () => ({
  ...(await vi.importActual<typeof import('@/identity')>('@/identity')),
  useIdentity: () => ({
    me: { id: 'u1', name: '张沁', email: 'a@example.test' },
    users: [{ id: 'u1', name: '张沁', email: 'a@example.test' }],
    switchTo: () => {},
  }),
}))

function setWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: px })
  window.dispatchEvent(new Event('resize'))
}

const TASK = {
  id: 'id-142', number: 142, version: 1, title: '修复竞价超时',
  context: '竞价请求近期频繁超时', expected_result: '恢复稳定流量', description: '',
  phase: 'backlog' as const, activity: null, review_cycle: 0, main_thread_id: 'thread-main-142',
  priority: 'none' as const, assignee: null,
  creator: { id: 'u1', name: '张沁', email: 'a@example.test' },
  start_date: null,
  due_date: null, project: { id: 'p1', number: 12, name: 'Task Manager' },
  milestone: null, labels: [], parent: null, children: [],
  dependencies: [], dependents: [], blocked: false, created_at: '', updated_at: '',
  completed_at: null, archived_at: null,
}

beforeEach(() => {
  vi.mocked(tasksApi.listTasks).mockResolvedValue({ items: [TASK], has_more: false })
  vi.mocked(tasksApi.listLabels).mockResolvedValue([])
  vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
  vi.mocked(tasksApi.listActivity).mockResolvedValue([])
  vi.mocked(tasksApi.listTaskAttachments).mockResolvedValue([])
  vi.mocked(projectsApi.listProjects).mockResolvedValue([])
  vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([])
  vi.mocked(projectsApi.getProject).mockRejectedValue(new Error('Project detail is not needed in this test'))
  vi.mocked(acceptanceApi.listTaskCriteria).mockResolvedValue([])
  vi.mocked(workflowApi.listTaskStageClaims).mockResolvedValue([])
  vi.mocked(workflowApi.listTaskThreads).mockResolvedValue([])
})

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this a
// component rendered by one test stays mounted and pollutes the next test's
// queries — which here would let every "the detail is on screen" assertion
// pass for the wrong reason.
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/tasks" element={<TaskListPage />} />
        <Route path="/tasks/:number" element={<h1>Standalone task route</h1>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('TaskListPage', () => {
  it('keeps the task list full width when nothing is selected', async () => {
    setWidth(1440)
    renderAt('/tasks')
    await screen.findByText('修复竞价超时')
    expect(screen.queryByRole('complementary', { name: '任务详情' })).not.toBeInTheDocument()
  })

  it('opens a task at its standalone URL without a dialog', async () => {
    setWidth(1440)
    renderAt('/tasks')
    fireEvent.click(await screen.findByRole('link', { name: '修复竞价超时' }))

    expect(await screen.findByRole('heading', { name: 'Standalone task route' })).toBeVisible()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('keeps ownership and filter state in the collection URL', async () => {
    setWidth(1440)
    renderAt('/tasks?ownership=created&q=timeout&sort=number&order=asc')

    await screen.findByRole('link', { name: '修复竞价超时' })
    expect(screen.getByRole('button', { name: '我创建的' })).toHaveClass('bg-surface')
    expect(screen.getByRole('textbox', { name: '搜索任务' })).toHaveValue('timeout')
    expect(tasksApi.listTasks).toHaveBeenCalledWith(expect.objectContaining({
      creator_id: 'u1',
      q: 'timeout',
      sort: 'number',
      order: 'asc',
    }))
  })

  it('reverts an optimistic row change and explains why when the server refuses it', async () => {
    // Nothing selected: this is about the list column alone, which still
    // owns its own optimistic update/revert independently of the detail.
    setWidth(1440)
    renderAt('/tasks')

    // Re-query after each change: the compact row trigger exposes its current
    // icon meaning through `title`, while Radix may replace trigger internals.
    const priorityTrigger = () => screen.getByRole('combobox', { name: '任务 #142 优先级' })

    // Status is a permanently visible combobox — no reveal-on-interaction
    // step first.
    await screen.findByRole('combobox', { name: '任务 #142 优先级' })
    expect(priorityTrigger()).toHaveTextContent('无优先级')

    let rejectPatch!: (err: Error) => void
    vi.mocked(tasksApi.updateTask).mockReturnValue(
      new Promise<never>((_resolve, reject) => {
        rejectPatch = reject
      }),
    )

    fireEvent.click(priorityTrigger())
    fireEvent.click(await screen.findByRole('option', { name: '紧急' }))

    // The optimistic value is on the row immediately, before the server has
    // answered at all.
    await waitFor(() => expect(priorityTrigger()).toHaveTextContent('紧急'))
    expect(vi.mocked(tasksApi.updateTask)).toHaveBeenCalledWith(
      142, 1, { priority: 'urgent' },
    )
    expect(screen.queryByText(/已恢复原状态/)).not.toBeInTheDocument()

    rejectPatch(new Error('cannot skip directly to done'))

    // Revert: back to the value it held before, plus a message naming what
    // went wrong. A refused PATCH that silently leaves the new value on
    // screen is the failure this guards.
    await waitFor(() => expect(priorityTrigger()).toHaveTextContent('无优先级'))
    expect(await screen.findByText(/cannot skip directly to done/)).toBeInTheDocument()
    expect(screen.getByText(/已恢复原状态/)).toBeInTheDocument()
  })

  it('loads the latest task after another actor wins an optimistic-write race', async () => {
    setWidth(1440)
    const latest = { ...TASK, version: 2, phase: 'in_progress' as const, activity: 'working' as const }
    vi.mocked(tasksApi.updateTask).mockRejectedValue(
      new ProblemError(412, 'VERSION_CONFLICT', 'req-conflict', 2),
    )
    vi.mocked(tasksApi.getTask).mockResolvedValue(latest)
    renderAt('/tasks')

    const priority = await screen.findByRole('combobox', { name: '任务 #142 优先级' })
    fireEvent.click(priority)
    fireEvent.click(await screen.findByRole('option', { name: '紧急' }))

    await waitFor(() => expect(tasksApi.getTask).toHaveBeenCalledWith(142))
    expect(await screen.findByText('内容已被其他用户或 Agent 更新，已加载最新版本。'))
      .toBeVisible()
    expect(screen.getByRole('status', { name: '执行中 · 正在处理' })).toBeVisible()
    expect(tasksApi.updateTask).toHaveBeenCalledWith(142, 1, { priority: 'urgent' })
  })
})
