import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { Task } from '@/task-types'
import TaskRelations from './TaskRelations'

function task(): Task {
  return {
    id: 'task-7',
    number: 7,
    version: 1,
    title: 'Relationship task',
    context: 'Context',
    expected_result: 'Result',
    description: '',
    phase: 'backlog',
    activity: null,
    review_cycle: 0,
    main_thread_id: 'thread-main-7',
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
  }
}

afterEach(cleanup)

describe('TaskRelations', () => {
  it('explains invalid self-dependencies without issuing a patch', () => {
    const onPatch = vi.fn()
    render(
      <MemoryRouter>
        <TaskRelations task={task()} onPatch={onPatch} />
      </MemoryRouter>,
    )

    fireEvent.change(screen.getByRole('spinbutton', { name: '依赖任务编号' }), {
      target: { value: '7' },
    })
    fireEvent.click(screen.getByRole('button', { name: '添加依赖任务' }))

    expect(screen.getByRole('alert')).toHaveTextContent('任务不能依赖自己。')
    expect(onPatch).not.toHaveBeenCalled()
  })
})
