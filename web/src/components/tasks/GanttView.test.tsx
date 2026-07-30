import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { Task } from '@/task-types'
import GanttView from './GanttView'
import type { TaskCollectionController } from './useTaskCollection'
import { DEFAULT_FILTERS } from './FilterBar'

function task(number: number, overrides: Partial<Task> = {}): Task {
  return {
    id: `task-${number}`,
    number,
    version: 1,
    title: `Task ${number}`,
    context: 'Context',
    expected_result: 'Result',
    description: '',
    status: 'todo',
    priority: 'none',
    assignee: null,
    creator: { id: 'user-1', name: 'User', email: null },
    start_date: null,
    due_date: null,
    project: { id: 'project-1', number: 1, name: 'Pactline' },
    milestone: null,
    labels: [],
    parent: null,
    children: [],
    dependencies: [],
    dependents: [],
    blocked: false,
    created_at: '',
    updated_at: '',
    completed_at: null,
    archived_at: null,
    ...overrides,
  }
}

function controller(
  tasks: Task[],
  rowErrors: Record<number, string> = {},
): TaskCollectionController {
  return {
    filters: DEFAULT_FILTERS,
    setFilters: vi.fn(),
    labels: [],
    setLabels: vi.fn(),
    tasks,
    loading: false,
    loadingMore: false,
    error: '',
    rowErrors,
    hasMore: false,
    hasActiveFilters: false,
    grouped: true,
    reload: vi.fn(),
    loadMore: vi.fn(),
    prependTask: vi.fn(),
    replaceTask: vi.fn(),
    patchOptimistic: vi.fn(),
    shiftSchedule: vi.fn().mockResolvedValue(null),
    archive: vi.fn(),
    restore: vi.fn(),
  }
}

afterEach(cleanup)

describe('GanttView', () => {
  it('renders a parent summary while keeping child edges directly resizable', () => {
    const parent = task(1, {
      title: 'Parent',
      start_date: '2026-08-03',
      due_date: '2026-08-05',
      children: [{
        id: 'task-2',
        number: 2,
        title: 'Child',
        status: 'todo',
        archived: false,
        milestone: null,
      }],
    })
    const child = task(2, {
      title: 'Child',
      start_date: '2026-08-04',
      due_date: '2026-08-08',
      parent: {
        id: 'task-1',
        number: 1,
        title: 'Parent',
        status: 'todo',
        archived: false,
        milestone: null,
      },
    })
    render(
      <MemoryRouter>
        <GanttView
          controller={controller([parent, child])}
          tier="xl"
          selectedNumber={null}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('button', { name: /Parent，2026-08-03 至 2026-08-08/ }))
      .toBeVisible()
    expect(screen.queryByRole('button', { name: '调整 Parent 开始日期' }))
      .not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '调整 Child 开始日期' }))
      .toBeVisible()
    expect(screen.getByRole('button', { name: '调整 Child 截止日期' }))
      .toBeVisible()
  })

  it('schedules an unscheduled task on the clicked timeline day', () => {
    const unscheduled = task(3, { title: 'Unscheduled' })
    const value = controller([unscheduled])
    render(
      <MemoryRouter>
        <GanttView controller={value} tier="xl" selectedNumber={null} />
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: '为任务 Unscheduled 安排日期' }), {
      clientX: 56,
    })

    expect(value.patchOptimistic).toHaveBeenCalledWith(
      unscheduled,
      expect.objectContaining({
        start_date: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
        due_date: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      }),
      expect.objectContaining({
        start_date: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
        due_date: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/),
      }),
    )
  })

  it('supports keyboard schedule shifts on a focused bar', () => {
    const scheduled = task(4, {
      title: 'Keyboard task',
      start_date: '2026-08-03',
      due_date: '2026-08-05',
    })
    const value = controller([scheduled])
    render(
      <MemoryRouter>
        <GanttView controller={value} tier="xl" selectedNumber={null} />
      </MemoryRouter>,
    )

    fireEvent.keyDown(
      screen.getByRole('button', { name: /Keyboard task，2026-08-03 至 2026-08-05/ }),
      { key: 'ArrowRight' },
    )

    expect(value.shiftSchedule).toHaveBeenCalledWith(scheduled, 1)
  })

  it('opens task detail from the label and keeps schedule failures local', () => {
    const scheduled = task(5, {
      title: 'Failed schedule',
      start_date: '2026-08-03',
      due_date: '2026-08-05',
    })
    render(
      <MemoryRouter>
        <GanttView
          controller={controller([scheduled], { 5: 'Version conflict' })}
          tier="xl"
          selectedNumber={5}
        />
      </MemoryRouter>,
    )

    expect(screen.getByRole('link', { name: 'Failed schedule' }))
      .toHaveAttribute('href', '/tasks/5')
    expect(screen.getByRole('alert')).toHaveAttribute('title', 'Version conflict')
  })
})
