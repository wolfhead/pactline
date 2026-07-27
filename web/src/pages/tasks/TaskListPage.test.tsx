import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskListPage from './TaskListPage'
import { apiGet, apiPatch, apiPost } from '../../api/client'
import { useIdentity } from '../../identity'
import type { Task, TaskListResponse } from '../../task-types'

// Mocks both modules so apiGet/apiPost/apiPatch resolution and the current
// identity are fully controllable per test, without touching global fetch,
// a real backend, or IdentityProvider.
vi.mock('../../api/client')
vi.mock('../../identity')

const mockedApiGet = vi.mocked(apiGet)
const mockedApiPost = vi.mocked(apiPost)
const mockedApiPatch = vi.mocked(apiPatch)
const mockedUseIdentity = vi.mocked(useIdentity)

// vitest.config's test block doesn't set `globals: true`, so
// @testing-library/react's own auto-cleanup never registers; without this, a
// component rendered by one test stays mounted and pollutes the next test's
// queries.
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const ME = { id: 'u-1', name: 'Alice', email: 'alice@example.com', roles: ['ENGINEER'], active: true }

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: 't-1',
    number: 1,
    title: '修复登录问题',
    description: '',
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

/** Routes every apiGet call to either the label list or the task list,
 * the only two GETs TaskListPage issues on its own. */
function stubListing(tasks: Task[]) {
  mockedApiGet.mockImplementation((path: unknown) => {
    if (typeof path === 'string' && path.startsWith('/api/labels')) {
      return Promise.resolve([])
    }
    return Promise.resolve({ items: tasks, has_more: false } satisfies TaskListResponse)
  })
}

function renderList(tasks: Task[] = []) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  mockedUseIdentity.mockReturnValue({ me: ME as any, users: [ME] as any, switchTo: vi.fn() })
  stubListing(tasks)
  return render(
    <MemoryRouter>
      <TaskListPage />
    </MemoryRouter>,
  )
}

describe('TaskListPage', () => {
  it('creates a task from the inline capture row and clears the input for the next one', async () => {
    renderList([])
    const created = makeTask({ id: 't-99', number: 99, title: '新任务标题' })
    mockedApiPost.mockResolvedValue(created)

    await waitFor(() => expect(screen.getByText('没有任务 — 按 C 创建一个吧')).toBeInTheDocument())

    const input = screen.getByPlaceholderText('输入标题，回车创建任务…') as HTMLInputElement
    fireEvent.change(input, { target: { value: '新任务标题' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(mockedApiPost).toHaveBeenCalledWith('/api/tasks', { title: '新任务标题' }))
    // The created task is on screen immediately (no reload round trip) ...
    expect(await screen.findByText('新任务标题')).toBeInTheDocument()
    // ... and the input is empty again, ready for the next title.
    await waitFor(() => expect(input.value).toBe(''))
  })

  it('returns focus to the create input after a task is created, so a second task can be captured without re-pressing "c"', async () => {
    renderList([])
    await waitFor(() => expect(screen.getByText('没有任务 — 按 C 创建一个吧')).toBeInTheDocument())

    const input = screen.getByPlaceholderText('输入标题，回车创建任务…') as HTMLInputElement

    let resolveCreateOne!: (task: Task) => void
    mockedApiPost.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveCreateOne = resolve
      }),
    )

    input.focus()
    fireEvent.change(input, { target: { value: '任务一' } })
    fireEvent.submit(input.closest('form')!)

    // While the request is in flight the input is disabled — jsdom (like a
    // real browser) refuses to focus a disabled element, which is exactly
    // what makes a synchronous .focus() call right here a silent no-op.
    expect(input).toBeDisabled()

    // A real browser moves focus away the instant a focused control becomes
    // disabled (jsdom does not model that automatically, so this line
    // stands in for it) — reproducing what the bug report actually observed:
    // focus ends up elsewhere, not still sitting on the input.
    screen.getByRole('button', { name: '进行中' }).focus()

    resolveCreateOne(makeTask({ id: 't-1', number: 1, title: '任务一' }))
    await screen.findByText('任务一')

    // Only once React has actually committed `disabled={false}` does focus
    // genuinely return — not merely "the input exists", but the real
    // document.activeElement.
    await waitFor(() => expect(input).not.toBeDisabled())
    await waitFor(() => expect(input).toHaveFocus())
    expect(input.value).toBe('')

    // A second task, typed immediately, with no re-trigger of the "c"
    // shortcut and no click — proving a burst of captures never requires
    // touching the mouse or the shortcut again.
    mockedApiPost.mockResolvedValueOnce(makeTask({ id: 't-2', number: 2, title: '任务二' }))
    fireEvent.change(input, { target: { value: '任务二' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(mockedApiPost).toHaveBeenCalledWith('/api/tasks', { title: '任务二' }))
    expect(await screen.findByText('任务二')).toBeInTheDocument()
  })

  it('shows a distinct, actionable message when filters narrow the list to zero, offering to clear them', async () => {
    const task = makeTask({ number: 3, title: '唯一匹配的任务' })
    renderList([task])
    await screen.findByText('唯一匹配的任务')

    // A status filter that this task doesn't have narrows the (non-empty)
    // list down to zero results.
    stubListing([])
    fireEvent.click(screen.getByRole('button', { name: '已完成' }))

    await waitFor(() => expect(screen.getByText(/没有符合筛选条件的任务/)).toBeInTheDocument())
    // Distinct from the genuinely-empty message — "press C" would be
    // misleading here since a newly created task wouldn't match the filter.
    expect(screen.queryByText('没有任务 — 按 C 创建一个吧')).not.toBeInTheDocument()

    // Clearing resets the filters and the matching task reappears.
    stubListing([task])
    fireEvent.click(screen.getByRole('button', { name: '清除筛选条件' }))
    expect(await screen.findByText('唯一匹配的任务')).toBeInTheDocument()
    expect(screen.queryByText(/没有符合筛选条件的任务/)).not.toBeInTheDocument()
  })

  it('reverts an optimistic status change and explains why when the server refuses it', async () => {
    const task = makeTask({ number: 7, status: 'todo', title: '待处理的任务' })
    renderList([task])

    // Status is a permanently visible combobox — no reveal-on-interaction
    // step first (that was the old QuietSelect pattern; TaskRow no longer
    // uses it, see task-7-report.md).
    const trigger = await screen.findByRole('combobox', { name: '任务 #7 状态' })
    expect(trigger).toHaveTextContent('待办')

    let rejectPatch!: (err: Error) => void
    mockedApiPatch.mockReturnValue(
      new Promise((_resolve, reject) => {
        rejectPatch = reject
      }),
    )

    fireEvent.click(trigger)
    fireEvent.click(await screen.findByRole('option', { name: '已完成' }))

    // The optimistic update is observed on the trigger immediately, before
    // the server has answered at all.
    expect(trigger).toHaveTextContent('已完成')
    expect(mockedApiPatch).toHaveBeenCalledWith('/api/tasks/7', { status: 'done' })
    expect(screen.queryByText(/已恢复原状态/)).not.toBeInTheDocument()

    rejectPatch(new Error('cannot skip directly to done'))

    // Revert: back to the value it held before the change, plus a message
    // naming what went wrong.
    await waitFor(() => expect(trigger).toHaveTextContent('待办'))
    expect(await screen.findByText(/cannot skip directly to done/)).toBeInTheDocument()
    expect(screen.getByText(/已恢复原状态/)).toBeInTheDocument()
  })

  it('"c" focuses the inline create input from anywhere on the page', async () => {
    renderList([])
    await waitFor(() => expect(screen.getByText('没有任务 — 按 C 创建一个吧')).toBeInTheDocument())

    const input = screen.getByPlaceholderText('输入标题，回车创建任务…')
    expect(document.activeElement).not.toBe(input)

    fireEvent.keyDown(window, { key: 'c' })

    expect(document.activeElement).toBe(input)
  })

  it('combines independently toggled filters into one request, and dropping one leaves the other in place', async () => {
    renderList([])
    await waitFor(() => expect(mockedApiGet).toHaveBeenCalled())
    mockedApiGet.mockClear()
    stubListing([])

    function lastTaskListQuery(): URLSearchParams {
      const call = [...mockedApiGet.mock.calls]
        .reverse()
        .find(([p]) => typeof p === 'string' && (p as string).startsWith('/api/tasks?'))
      if (!call) throw new Error('no /api/tasks request was made')
      return new URLSearchParams((call[0] as string).split('?')[1])
    }

    fireEvent.click(screen.getByRole('button', { name: '进行中' })) // status: in_progress
    await waitFor(() => {
      const q = lastTaskListQuery()
      expect(q.getAll('status')).toEqual(['in_progress'])
    })

    fireEvent.click(screen.getByRole('button', { name: '高' })) // priority: high
    await waitFor(() => {
      const q = lastTaskListQuery()
      // Both filters must be present on the SAME request — a plausible bug
      // here is the second toggle silently clobbering the first instead of
      // merging with it.
      expect(q.getAll('status')).toEqual(['in_progress'])
      expect(q.getAll('priority')).toEqual(['high'])
    })

    // Toggling status back off must drop only that filter — a plausible
    // decoy bug is a "toggle" that never removes, or one that clears every
    // filter instead of just the one clicked.
    fireEvent.click(screen.getByRole('button', { name: '进行中' }))
    await waitFor(() => {
      const q = lastTaskListQuery()
      expect(q.getAll('status')).toEqual([])
      expect(q.getAll('priority')).toEqual(['high'])
    })
  })
})
