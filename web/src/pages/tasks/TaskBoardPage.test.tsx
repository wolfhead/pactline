import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskBoardPage from './TaskBoardPage'
import { apiGet, apiPatch } from '../../api/client'
import { useIdentity } from '../../identity'
import type { Task, TaskListResponse, TaskStatus } from '../../task-types'

// Mocks both modules so apiGet/apiPatch resolution and the current identity
// are fully controllable per test, without touching global fetch, a real
// backend, or IdentityProvider. Mirrors the pattern established in
// src/pages/tasks/TaskListPage.test.tsx and TaskDetailPage.test.tsx.
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
    number: 1,
    title: '任务标题',
    description: '',
    status: 'backlog',
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

function renderBoard(tasks: Task[]) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  mockedUseIdentity.mockReturnValue({ me: ME as any, users: [ME] as any, switchTo: vi.fn() })
  mockedApiGet.mockImplementation(() => Promise.resolve({ items: tasks, has_more: false } satisfies TaskListResponse))
  return render(
    <MemoryRouter>
      <TaskBoardPage />
    </MemoryRouter>,
  )
}

/** A minimal stand-in for the browser's real DataTransfer (jsdom doesn't
 * implement it) — enough for TaskBoardPage's dragstart/dragover/drop
 * handlers, which only call setData/getData('text/plain', ...) and set
 * effectAllowed. Mirrors what web/e2e/13-board-drag-and-keyboard.spec.ts
 * exercises against a real DataTransfer in a real browser. */
function makeDataTransfer() {
  let stored = ''
  return {
    setData: (_type: string, value: string) => {
      stored = value
    },
    getData: () => stored,
    effectAllowed: '',
  }
}

function columnFor(status: TaskStatus, labelPattern: RegExp): HTMLElement {
  const heading = screen.getByRole('heading', { name: labelPattern })
  return heading.closest('.board-column') as HTMLElement
}

describe('TaskBoardPage — column render cap', () => {
  it('caps a column at 50 cards, reports how many are hidden, and revealing shows the rest', async () => {
    const tasks = Array.from({ length: 60 }, (_, i) =>
      makeTask({ id: `t-${i + 1}`, number: i + 1, status: 'backlog', title: `积压任务${i + 1}` }),
    )
    renderBoard(tasks)

    // Card text is "#<number> <title>" split across sibling text nodes
    // inside the same <a>, so the full concatenated string is matched here
    // rather than the title alone (which would also substring-match
    // "积压任务10".."积压任务19" etc. and make the query ambiguous).
    await screen.findByText('#1 积压任务1')
    expect(screen.getAllByRole('article')).toHaveLength(50)
    // The 51st through 60th cards are not rendered at all — not merely
    // visually hidden, actually absent — until revealed.
    expect(screen.queryByText('#60 积压任务60')).not.toBeInTheDocument()
    expect(screen.getByText('还有 10 条未显示，点击展开')).toBeInTheDocument()

    fireEvent.click(screen.getByText('还有 10 条未显示，点击展开'))

    expect(await screen.findByText('#60 积压任务60')).toBeInTheDocument()
    expect(screen.getAllByRole('article')).toHaveLength(60)
    expect(screen.queryByText(/还有 \d+ 条未显示/)).not.toBeInTheDocument()
  })

  it('does not swallow a card dragged into a column that is already at the cap', async () => {
    const backlogTasks = Array.from({ length: 50 }, (_, i) =>
      makeTask({ id: `t-${i + 1}`, number: i + 1, status: 'backlog', title: `积压任务${i + 1}` }),
    )
    const moving = makeTask({ id: 't-99', number: 99, status: 'todo', title: '待移动任务' })
    renderBoard([...backlogTasks, moving])
    mockedApiPatch.mockResolvedValue({ ...moving, status: 'backlog' })

    await screen.findByText('#99 待移动任务')
    // Backlog is exactly at the cap already: 50 visible, nothing hidden yet.
    const backlogColumn = columnFor('backlog', /待定/)
    expect(within(backlogColumn).getAllByRole('article')).toHaveLength(50)

    const card = screen.getByText('#99 待移动任务').closest('article') as HTMLElement

    const dataTransfer = makeDataTransfer()
    fireEvent.dragStart(card, { dataTransfer })
    fireEvent.dragOver(backlogColumn, { dataTransfer })
    fireEvent.drop(backlogColumn, { dataTransfer })

    await waitFor(() => expect(mockedApiPatch).toHaveBeenCalledWith('/api/tasks/99', { status: 'backlog' }))

    // The just-moved card is visible immediately, even though the column it
    // landed in was already full — this is the failure mode a naive
    // slice(0, CAP) would produce: the 51st card silently never rendering.
    expect(screen.getByText('#99 待移动任务')).toBeInTheDocument()
    expect(within(backlogColumn).getAllByRole('article')).toHaveLength(51)
  })
})
