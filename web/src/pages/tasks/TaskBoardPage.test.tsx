import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskBoardPage from './TaskBoardPage'
import * as tasksApi from '@/api/tasks'
import type { Task } from '@/task-types'

// @testing-library/react's own auto-cleanup never registers here (no
// `globals: true` in vitest.config.ts); without this, each test's render
// leaks into the next one — see TaskList.test.tsx/TaskListPage.test.tsx for
// the same pattern.
afterEach(() => {
  cleanup()
})

vi.mock('@/api/tasks')
vi.mock('@/identity', async () => ({
  ...(await vi.importActual<typeof import('@/identity')>('@/identity')),
  useIdentity: () => ({
    me: { id: 'u1', name: '张沁', email: 'a@x.com' },
    users: [{ id: 'u1', name: '张沁', email: 'a@x.com' }],
    switchTo: () => {},
  }),
}))

function setWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { writable: true, configurable: true, value: px })
  window.dispatchEvent(new Event('resize'))
}

const CREATOR = { id: 'u1', name: '张沁', email: 'a@x.com' }
function task(n: number, status: Task['status']): Task {
  return {
    id: `id-${n}`, number: n, title: `任务 ${n}`, description: '',
    status, priority: 'none', assignee: null, creator: CREATOR,
    due_date: null, labels: [], created_at: '', updated_at: '',
    completed_at: null, archived_at: null,
  }
}

beforeEach(() => {
  vi.mocked(tasksApi.listTasks).mockResolvedValue({
    items: [task(1, 'todo'), task(2, 'in_progress'), task(3, 'in_progress')],
    has_more: false,
  })
  vi.mocked(tasksApi.listLabels).mockResolvedValue([])
})

function renderBoard() {
  return render(<MemoryRouter><TaskBoardPage /></MemoryRouter>)
}

describe('TaskBoardPage', () => {
  it('renders one column per status, each carrying its own count', async () => {
    setWidth(1440)
    renderBoard()
    const inProgress = await screen.findByRole('group', { name: /进行中/ })
    const todo = screen.getByRole('group', { name: /待办/ })
    // Real per-column counts. A decoy printing the total (3) in every column
    // fails here, and so does one that always prints 1.
    expect(within(inProgress).getByRole('heading')).toHaveTextContent('2')
    expect(within(todo).getByRole('heading')).toHaveTextContent('1')
    // Six statuses means six columns, including the empty ones.
    expect(screen.getAllByRole('group')).toHaveLength(6)
  })

  it('puts each card in its own status column, not merely on screen', async () => {
    setWidth(1440)
    renderBoard()
    const inProgress = await screen.findByRole('group', { name: /进行中/ })
    expect(within(inProgress).getByText('任务 2')).toBeInTheDocument()
    expect(within(inProgress).queryByText('任务 1')).not.toBeInTheDocument()
  })

  it('drops the drag affordance on a phone but still shows every card', async () => {
    setWidth(390)
    renderBoard()
    const cards = await screen.findAllByRole('article')
    // Decoy this catches: hiding the board entirely on a phone, or leaving
    // draggable=true where a touch drag would fight the scroll container.
    expect(cards).toHaveLength(3)
    for (const c of cards) expect(c).not.toHaveAttribute('draggable', 'true')
  })

  it('marks cards draggable on the desktop tiers', async () => {
    setWidth(1440)
    renderBoard()
    const cards = await screen.findAllByRole('article')
    expect(cards).toHaveLength(3)
    for (const c of cards) expect(c).toHaveAttribute('draggable', 'true')
  })

  it('gives every card a status control named after its task', async () => {
    setWidth(1440)
    renderBoard()
    // Same accessible-name convention as the list rows (see Task 4), so e2e
    // 13 can assert the post-drag status through a single lookup.
    expect(await screen.findByRole('combobox', { name: '任务 #2 状态' })).toBeVisible()
  })
})
