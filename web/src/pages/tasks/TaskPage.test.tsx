import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import TaskPage from './TaskPage'
import * as acceptanceApi from '@/api/acceptance'
import * as projectsApi from '@/api/projects'
import * as tasksApi from '@/api/tasks'
import * as workflowApi from '@/api/task-workflow'
import { ProblemError } from '@/api/v1/client'
import type { Task } from '@/task-types'

vi.mock('@/api/tasks')
vi.mock('@/api/projects')
vi.mock('@/api/acceptance')
vi.mock('@/api/task-workflow')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    me: { id: 'u1', name: 'Alex', email: 'alex@example.test' },
    users: [{ id: 'u1', name: 'Alex', email: 'alex@example.test' }],
    isReadOnly: false,
  }),
}))

const TASK: Task = {
  id: 'task-142', number: 142, version: 1, title: 'Standalone task page',
  context: 'Load without a collection.', expected_result: 'A stable detail URL.', description: '',
  phase: 'backlog', activity: null, review_cycle: 0,
  main_thread_id: '11111111-1111-4111-8111-111111111111',
  priority: 'none', assignee: null,
  creator: { id: 'u1', name: 'Alex', email: 'alex@example.test' },
  start_date: null, due_date: null,
  project: { id: 'project-1', number: 12, name: 'Launch' }, milestone: null,
  labels: [], parent: null, children: [], dependencies: [], dependents: [],
  blocked: false, created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
  completed_at: null, archived_at: null,
}

beforeEach(() => {
  vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
  vi.mocked(tasksApi.listActivity).mockResolvedValue([])
  vi.mocked(tasksApi.listTaskAttachments).mockResolvedValue([])
  vi.mocked(tasksApi.listLabels).mockResolvedValue([])
  vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([])
  vi.mocked(projectsApi.getProject).mockRejectedValue(new Error('No milestone fixture'))
  vi.mocked(acceptanceApi.listTaskCriteria).mockResolvedValue([])
  vi.mocked(workflowApi.listTaskStageClaims).mockResolvedValue([])
  vi.mocked(workflowApi.listTaskThreads).mockResolvedValue([])
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function renderPage(state?: unknown) {
  return render(
    <MemoryRouter initialEntries={[{ pathname: '/tasks/142', state }]}>
      <Routes>
        <Route path="/tasks/:number" element={<TaskPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('TaskPage', () => {
  it('loads a task directly by route number without fetching a collection', async () => {
    renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('正在加载任务')
    expect(await screen.findByText(TASK.title)).toBeVisible()
    expect(tasksApi.getTask).toHaveBeenCalledWith(142)
    expect(tasksApi.listTasks).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('renders a page-level not-found state', async () => {
    vi.mocked(tasksApi.getTask).mockRejectedValue(
      new ProblemError(404, 'TASK_NOT_FOUND', 'request-404'),
    )
    renderPage()

    expect(await screen.findByRole('heading', { name: '找不到任务' })).toBeVisible()
  })

  it('renders a retryable page-level error', async () => {
    vi.mocked(tasksApi.getTask).mockRejectedValue(new Error('connection unavailable'))
    renderPage()

    expect(await screen.findByRole('heading', { name: '任务加载失败' })).toBeVisible()
    expect(screen.getByText('connection unavailable')).toBeVisible()
    expect(screen.getByRole('button', { name: '重试' })).toBeVisible()
  })

  it('keeps an archived task readable and identifies its state', async () => {
    vi.mocked(tasksApi.getTask).mockResolvedValue({
      ...TASK,
      archived_at: '2026-08-20T01:00:00Z',
    })
    renderPage()

    expect(await screen.findByText('此任务已归档，仍可查看完整内容。')).toBeVisible()
    expect(screen.getByText(TASK.context)).toBeVisible()
    expect(screen.getByRole('button', { name: '恢复' })).toBeVisible()
  })

  it('accepts only known in-app collection return paths', async () => {
    const { unmount } = renderPage({
      taskSource: '/projects/12/backlog?phase=ready&q=release',
    })
    expect(screen.getByRole('link', { name: '返回任务集合' })).toHaveAttribute(
      'href',
      '/projects/12/backlog?phase=ready&q=release',
    )

    unmount()
    renderPage({ taskSource: '//attacker.example/redirect' })
    expect(screen.getByRole('link', { name: '返回任务集合' })).toHaveAttribute('href', '/tasks')
  })
})
