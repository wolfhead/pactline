import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

function renderPage(state?: unknown, pathname = '/tasks/142') {
  return render(
    <MemoryRouter initialEntries={[{ pathname, state }]}>
      <Routes>
        <Route path="/tasks/:number" element={<TaskPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

function withinOrder(container: HTMLElement, firstSelector: string, secondSelector: string) {
  const first = container.querySelector(firstSelector)
  const second = container.querySelector(secondSelector)
  return Boolean(first && second && (first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING))
}

describe('TaskPage', () => {
  it('loads a task directly by route number without fetching a collection', async () => {
    const { container } = renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('正在加载任务')
    expect(await screen.findByText(TASK.title)).toBeVisible()
    expect(tasksApi.getTask).toHaveBeenCalledWith(142)
    expect(tasksApi.listTasks).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: TASK.title, level: 1 })).toBeVisible()

    const pageHeader = screen.getByRole('region', { name: '任务页标题' })
    const body = screen.getByRole('region', { name: '任务正文' })
    const sidebar = screen.getByRole('complementary', { name: '任务属性与交付' })
    expect(pageHeader).toBeVisible()
    expect(body).toBeVisible()
    expect(sidebar).toBeVisible()
    expect(withinOrder(container, '[data-task-brief]', '[data-task-acceptance]')).toBe(true)
    expect(withinOrder(container, '[data-task-acceptance]', '[data-task-thread]')).toBe(true)
    expect(withinOrder(container, '[data-task-thread]', '[data-task-sidebar]')).toBe(true)
  })

  it('keeps title editing local to the page and commits through the versioned Task mutation', async () => {
    const updated = { ...TASK, version: 2, title: 'Updated standalone task page' }
    vi.mocked(tasksApi.updateTask).mockResolvedValue(updated)
    renderPage()

    const title = await screen.findByRole('textbox', { name: '任务标题' })
    fireEvent.change(title, { target: { value: updated.title } })
    fireEvent.blur(title)

    await waitFor(() => expect(tasksApi.updateTask).toHaveBeenCalledWith(
      TASK.number,
      TASK.version,
      { title: updated.title },
    ))
    expect(title).toHaveValue(updated.title)
  })

  it('reloads the winning Task version after an inline version conflict', async () => {
    const latest = { ...TASK, version: 2, title: 'Title from another actor' }
    vi.mocked(tasksApi.getTask)
      .mockResolvedValueOnce(TASK)
      .mockResolvedValueOnce(latest)
    vi.mocked(tasksApi.updateTask).mockRejectedValue(
      new ProblemError(412, 'VERSION_CONFLICT', 'request-conflict'),
    )
    renderPage()

    const title = await screen.findByRole('textbox', { name: '任务标题' })
    fireEvent.change(title, { target: { value: 'Local losing title' } })
    fireEvent.blur(title)

    expect(await screen.findByText(
      '内容已被其他用户或 Agent 更新，已加载最新版本。',
    )).toBeVisible()
    expect(title).toHaveValue(latest.title)
  })

  it('offers an inline undo after archiving and restores the same Task', async () => {
    const archived = { ...TASK, version: 2, archived_at: '2026-08-20T01:00:00Z' }
    const restored = { ...TASK, version: 3 }
    vi.mocked(tasksApi.archiveTask).mockResolvedValue(archived)
    vi.mocked(tasksApi.restoreTask).mockResolvedValue(restored)
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: '归档' }))
    expect(await screen.findByText('已归档任务。')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '撤销' }))

    await waitFor(() => expect(tasksApi.restoreTask).toHaveBeenCalledWith(TASK.number, archived.version))
    expect(await screen.findByRole('button', { name: '归档' })).toBeVisible()
  })

  it.each(['/tasks/1e2', '/tasks/0x8e', '/tasks/142.5', '/tasks/-142'])(
    'rejects a non-decimal route segment at %s without loading a task',
    async (pathname) => {
      renderPage(undefined, pathname)

      expect(await screen.findByRole('alert')).toHaveTextContent('任务编号无效。')
      expect(tasksApi.getTask).not.toHaveBeenCalled()
    },
  )

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
      taskSource: '/projects/12?phase=ready&q=release',
    })
    expect(screen.getByRole('link', { name: '返回任务集合' })).toHaveAttribute(
      'href',
      '/projects/12?phase=ready&q=release',
    )

    unmount()
    renderPage({ taskSource: '//attacker.example/redirect' })
    expect(screen.getByRole('link', { name: '返回任务集合' })).toHaveAttribute('href', '/tasks')
  })
})
