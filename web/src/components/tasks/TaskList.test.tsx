import { afterEach, describe, expect, it } from 'vitest'
import { render, screen, within, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import TaskList from './TaskList'
import type { Task } from '@/task-types'

const USERS = [{ id: 'u1', name: '张沁', email: 'a@x.com' }]

function task(n: number, over: Partial<Task> = {}): Task {
  return {
    id: `id-${n}`, number: n, title: `任务 ${n}`,
    context: 'Test context', expected_result: 'Test result', description: '',
    status: 'todo', priority: 'none', assignee: null,
    creator: USERS[0], due_date: null, labels: [],
    parent: null, children: [], dependencies: [], dependents: [], blocked: false,
    created_at: '', updated_at: '', completed_at: null, archived_at: null,
    ...over,
    version: over.version ?? 1,
    project: over.project ?? { id: 'p1', number: 12, name: 'Task Manager' },
    milestone: over.milestone ?? null,
    start_date: over.start_date ?? null,
  }
}

function renderList(tasks: Task[], selected: number | null = null, grouped = true) {
  return render(
    <MemoryRouter>
      <TaskList tasks={tasks} selectedNumber={selected} tier="xl" users={USERS}
        rowErrors={{}} grouped={grouped}
        onPatch={() => {}} onArchive={() => {}} onRestore={() => {}} />
    </MemoryRouter>,
  )
}

describe('TaskList', () => {
  // vitest.config's test block doesn't set `globals: true`, so
  // @testing-library/react's own auto-cleanup never registers; see the
  // identical workaround in StatusControl.test.tsx / AssigneeControl.test.tsx.
  afterEach(() => {
    cleanup()
  })

  it('groups by status and counts each group', () => {
    renderList([
      task(1, { status: 'in_progress' }),
      task(2, { status: 'in_progress' }),
      task(3, { status: 'todo' }),
    ])
    // The count must be the real per-group size. A decoy that prints the
    // total (3) everywhere fails here, and so does one that prints 1.
    expect(screen.getByRole('heading', { name: /进行中/ })).toHaveTextContent('2')
    expect(screen.getByRole('heading', { name: /待办/ })).toHaveTextContent('1')
  })

  it('puts each task under its own status heading, not merely on screen', () => {
    renderList([task(1, { status: 'in_progress' }), task(2, { status: 'todo' })])
    const inProgress = screen.getByRole('group', { name: /进行中/ })
    expect(within(inProgress).getByText('任务 1')).toBeInTheDocument()
    expect(within(inProgress).queryByText('任务 2')).not.toBeInTheDocument()
  })

  it('drops the grouping entirely when a non-default sort is active', () => {
    renderList([task(1, { status: 'in_progress' }), task(2, { status: 'todo' })], null, false)
    expect(screen.queryByRole('heading', { name: /进行中/ })).not.toBeInTheDocument()
    expect(screen.getByText('任务 1')).toBeInTheDocument()
    expect(screen.getByText('任务 2')).toBeInTheDocument()
  })

  it('marks exactly one row as the selected one', () => {
    renderList([task(1), task(2)], 2)
    const rows = screen.getAllByRole('listitem')
    const selected = rows.filter((r) => r.getAttribute('aria-current') === 'true')
    expect(selected).toHaveLength(1)
    expect(within(selected[0]).getByText('任务 2')).toBeInTheDocument()
  })

  it('keeps visible children immediately after their parent', () => {
    const parentRef = {
      id: 'id-4',
      number: 4,
      title: '任务 4',
      status: 'todo' as const,
      archived: false,
      milestone: null,
    }
    renderList([
      task(4, {
        children: [{
          id: 'id-1',
          number: 1,
          title: '任务 1',
          status: 'todo',
          archived: false,
          milestone: null,
        }],
      }),
      task(3),
      task(1, { parent: parentRef }),
    ])

    expect(screen.getAllByRole('listitem').map((row) => row.dataset.taskNumber))
      .toEqual(['4', '1', '3'])
  })
})
