import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import TaskDetailPage from './TaskDetailPage'
import { apiGet, apiPatch } from '../../api/client'
import { useIdentity } from '../../identity'
import type { Task } from '../../task-types'

// Mocks both modules so apiGet/apiPatch resolution and the current identity
// are fully controllable per test, without touching global fetch, a real
// backend, or IdentityProvider. Mirrors the pattern established in
// src/pages/tasks/TaskListPage.test.tsx.
vi.mock('../../api/client')
vi.mock('../../identity')

const mockedApiGet = vi.mocked(apiGet)
const mockedApiPatch = vi.mocked(apiPatch)
const mockedUseIdentity = vi.mocked(useIdentity)

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const ME = { id: 'u-1', name: 'Alice', email: 'alice@example.com', roles: ['ENGINEER'], active: true }

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 't-1',
    number: 42,
    title: '原始标题',
    description: '原始描述',
    status: 'todo',
    priority: 'medium',
    assignee: null,
    creator: { id: ME.id, name: ME.name, email: ME.email },
    due_date: null,
    labels: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    completed_at: null,
    archived_at: null,
    ...overrides,
  }
}

function renderDetail(task: Task) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  mockedUseIdentity.mockReturnValue({ me: ME as any, users: [ME] as any, switchTo: vi.fn() })
  mockedApiGet.mockImplementation((path: unknown) => {
    if (typeof path === 'string' && path === '/api/labels') return Promise.resolve([])
    if (typeof path === 'string' && path === `/api/tasks/${task.number}/comments`) return Promise.resolve([])
    if (typeof path === 'string' && path === `/api/tasks/${task.number}/activity`) return Promise.resolve([])
    if (typeof path === 'string' && path === `/api/tasks/${task.number}`) return Promise.resolve(task)
    return Promise.reject(new Error(`unexpected GET ${String(path)}`))
  })
  return render(
    <MemoryRouter initialEntries={[`/tasks/${task.number}`]}>
      <Routes>
        <Route path="/tasks/:number" element={<TaskDetailPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('TaskDetailPage — in-place title editing', () => {
  it('commits the new title on Enter', async () => {
    const task = makeTask()
    renderDetail(task)

    const title = (await screen.findByLabelText('任务标题')) as HTMLInputElement
    expect(title.value).toBe('原始标题')

    mockedApiPatch.mockResolvedValue({ ...task, title: '修改后的标题' })

    // Enter's handler calls .blur() on the field to trigger the commit;
    // that is a no-op in jsdom (as in a real browser) unless the field is
    // actually focused first, which a real keystroke always would be.
    // fireEvent.focus() only dispatches a synthetic 'focus' event, it does
    // not update document.activeElement — calling the native method is what
    // makes the later .blur() (inside the Enter/Escape key handler) do
    // anything at all, exactly as it would for a real keystroke.
    title.focus()
    fireEvent.change(title, { target: { value: '修改后的标题' } })
    fireEvent.keyDown(title, { key: 'Enter' })

    await waitFor(() =>
      expect(mockedApiPatch).toHaveBeenCalledWith(`/api/tasks/${task.number}`, { title: '修改后的标题' }),
    )
    // The field keeps the edited text (Enter commits, it does not clear).
    expect(title.value).toBe('修改后的标题')
  })

  it('discards the in-progress edit on Escape and never calls the server', async () => {
    const task = makeTask()
    renderDetail(task)

    const title = (await screen.findByLabelText('任务标题')) as HTMLInputElement
    expect(title.value).toBe('原始标题')

    // fireEvent.focus() only dispatches a synthetic 'focus' event, it does
    // not update document.activeElement — calling the native method is what
    // makes the later .blur() (inside the Enter/Escape key handler) do
    // anything at all, exactly as it would for a real keystroke.
    title.focus()
    fireEvent.change(title, { target: { value: '还没提交的标题' } })
    expect(title.value).toBe('还没提交的标题')

    fireEvent.keyDown(title, { key: 'Escape' })

    // Reverted in the DOM, not just eventually via a server round trip —
    // and specifically back to the ORIGINAL text, not merely "some other
    // value", which is what a decoy assertion on "not the typed text" would
    // miss.
    expect(title.value).toBe('原始标题')
    // Escape must fire the blur (which is what would trigger a commit for
    // any other change) without ever calling the server: a plausible bug is
    // reverting the input's DOM value while still — separately — sending
    // the abandoned edit.
    await new Promise((r) => setTimeout(r, 0))
    expect(mockedApiPatch).not.toHaveBeenCalled()
  })
})

describe('TaskDetailPage — activity log live update', () => {
  it('shows a status change in the activity view immediately, without a reload', async () => {
    const task = makeTask({ status: 'todo' })
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    mockedUseIdentity.mockReturnValue({ me: ME as any, users: [ME] as any, switchTo: vi.fn() })

    let currentTask = task
    let activityCalls = 0
    const activityBefore = [
      { id: 'a1', actor_id: ME.id, field: 'created' as const, old_value: null, new_value: 'todo', created_at: '2026-01-01T00:00:00Z' },
    ]
    const activityAfter = [
      ...activityBefore,
      {
        id: 'a2',
        actor_id: ME.id,
        field: 'status' as const,
        old_value: 'todo',
        new_value: 'in_progress',
        created_at: '2026-01-01T01:00:00Z',
      },
    ]
    mockedApiGet.mockImplementation((path: unknown) => {
      if (typeof path !== 'string') return Promise.reject(new Error('unexpected GET'))
      if (path === '/api/labels') return Promise.resolve([])
      if (path === `/api/tasks/${task.number}/comments`) return Promise.resolve([])
      if (path === `/api/tasks/${task.number}/activity`) {
        activityCalls += 1
        return Promise.resolve(activityCalls === 1 ? activityBefore : activityAfter)
      }
      if (path === `/api/tasks/${task.number}`) return Promise.resolve(currentTask)
      return Promise.reject(new Error(`unexpected GET ${path}`))
    })

    render(
      <MemoryRouter initialEntries={[`/tasks/${task.number}`]}>
        <Routes>
          <Route path="/tasks/:number" element={<TaskDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    const activitySection = (await screen.findByRole('heading', { name: '历史记录', level: 3 })).closest('section')!
    await waitFor(() => expect(within(activitySection).getByText(/创建了任务/)).toBeInTheDocument())
    expect(within(activitySection).queryByText(/将状态从/)).not.toBeInTheDocument()
    expect(activityCalls).toBe(1)

    // patchOptimistic's server response carries a bumped updated_at, the
    // same as a real PATCH would (task_store.go sets updated_at=now() on
    // every update) — that is what should re-trigger the activity fetch.
    currentTask = { ...task, status: 'in_progress', updated_at: '2026-01-01T01:00:00Z' }
    mockedApiPatch.mockResolvedValue(currentTask)

    // Status is a quiet display until interacted with — click it to reveal
    // the real <select> before driving it.
    fireEvent.click(await screen.findByRole('button', { name: '状态' }))
    const status = screen.getByRole('combobox', { name: '状态' })
    fireEvent.change(status, { target: { value: 'in_progress' } })

    await waitFor(() => expect(mockedApiPatch).toHaveBeenCalledWith(`/api/tasks/${task.number}`, { status: 'in_progress' }))
    // No page.reload() equivalent anywhere in this test — the new entry
    // must appear purely from the effect re-firing.
    await waitFor(() => expect(within(activitySection).getByText(/将状态从「待办」改为「进行中」/)).toBeInTheDocument())
    expect(activityCalls).toBe(2)
  })
})

describe('TaskDetailPage — Escape', () => {
  function renderDetailWithRoutes(task: Task) {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    mockedUseIdentity.mockReturnValue({ me: ME as any, users: [ME] as any, switchTo: vi.fn() })
    mockedApiGet.mockImplementation((path: unknown) => {
      if (typeof path !== 'string') return Promise.reject(new Error('unexpected GET'))
      if (path === '/api/labels') return Promise.resolve([])
      if (path === `/api/tasks/${task.number}/comments`) return Promise.resolve([])
      if (path === `/api/tasks/${task.number}/activity`) return Promise.resolve([])
      if (path === `/api/tasks/${task.number}`) return Promise.resolve(task)
      return Promise.reject(new Error(`unexpected GET ${path}`))
    })
    return render(
      <MemoryRouter initialEntries={[`/tasks/${task.number}`]}>
        <Routes>
          <Route path="/tasks/:number" element={<TaskDetailPage />} />
          <Route path="/tasks" element={<p>任务列表占位</p>} />
        </Routes>
      </MemoryRouter>,
    )
  }

  it('cancels an open in-place edit on the first Escape, without leaving the page', async () => {
    const task = makeTask()
    renderDetailWithRoutes(task)

    const title = (await screen.findByLabelText('任务标题')) as HTMLInputElement
    title.focus()
    fireEvent.change(title, { target: { value: '还没提交的标题' } })
    expect(title.value).toBe('还没提交的标题')

    fireEvent.keyDown(title, { key: 'Escape' })

    // The edit is discarded, exactly as InlineEditable's own Escape does on
    // its own (see the sibling describe block above) ...
    expect(title.value).toBe('原始标题')
    expect(mockedApiPatch).not.toHaveBeenCalled()
    // ... and cancelling the edit is as far as this Escape goes — it does
    // not also leave the page in the same keystroke.
    expect(screen.queryByText('任务列表占位')).not.toBeInTheDocument()
  })

  it('leaves the page on Escape when nothing is being edited', async () => {
    const task = makeTask()
    renderDetailWithRoutes(task)

    await screen.findByLabelText('任务标题')
    expect(document.activeElement).not.toBeInstanceOf(HTMLInputElement)

    fireEvent.keyDown(window, { key: 'Escape' })

    expect(await screen.findByText('任务列表占位')).toBeInTheDocument()
  })
})
