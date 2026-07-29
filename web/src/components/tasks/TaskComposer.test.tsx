import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TaskComposerProvider, useTaskComposer } from './TaskComposer'
import * as projectsAPI from '@/api/projects'
import * as tasksAPI from '@/api/tasks'
import type { Task } from '@/task-types'

vi.mock('@/api/projects')
vi.mock('@/api/tasks')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    users: [{ id: 'u1', name: 'Alex', email: 'a@example.com' }],
    isReadOnly: false,
  }),
}))

const CREATED_TASK: Task = {
  id: 'task-1',
  number: 42,
  version: 1,
  title: 'Clarify the work',
  context: 'The request has no durable context.',
  expected_result: 'The task is understandable without chat history.',
  description: '',
  status: 'todo',
  priority: 'none',
  assignee: null,
  creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
  due_date: null,
  project: { id: 'project-1', number: 12, name: 'Task Manager' },
  milestone: { id: 'milestone-1', name: 'Structured creation' },
  labels: [],
  created_at: '',
  updated_at: '',
  completed_at: null,
  archived_at: null,
}

function ContextualTrigger({ onCreated }: { onCreated: (task: Task) => void }) {
  const { openTaskComposer } = useTaskComposer()
  return (
    <button
      type="button"
      onClick={() => openTaskComposer({
        projectNumber: 12,
        milestoneID: 'milestone-1',
        onCreated,
      })}
    >
      Open composer
    </button>
  )
}

describe('TaskComposer', () => {
  beforeEach(() => {
    vi.mocked(projectsAPI.listProjects).mockResolvedValue([{
      id: 'project-1',
      number: 12,
      version: 1,
      name: 'Task Manager',
      description: '',
      owner: CREATED_TASK.creator,
      creator: CREATED_TASK.creator,
      archived_at: null,
      created_at: '',
      updated_at: '',
      completed_tasks: 0,
      eligible_tasks: 0,
    }])
    vi.mocked(projectsAPI.getProject).mockResolvedValue({
      project: {
        id: 'project-1',
        number: 12,
        version: 1,
        name: 'Task Manager',
        description: '',
        owner: CREATED_TASK.creator,
        creator: CREATED_TASK.creator,
        archived_at: null,
        created_at: '',
        updated_at: '',
        completed_tasks: 0,
        eligible_tasks: 0,
      },
      milestones: [{
        id: 'milestone-1',
        project_id: 'project-1',
        version: 1,
        name: 'Structured creation',
        outcome: 'Tasks have complete briefs',
        description: '',
        owner_id: 'u1',
        status: 'active',
        target_date: null,
        position: 0,
        completed_at: null,
        cancelled_at: null,
        created_at: '',
        updated_at: '',
        acceptance_criteria: [],
      }],
      tasks: [],
      activity: [],
    })
    vi.mocked(tasksAPI.createTask).mockResolvedValue(CREATED_TASK)
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('requires a structured brief and preserves contextual project defaults', async () => {
    const onCreated = vi.fn()
    render(
      <MemoryRouter>
        <TaskComposerProvider>
          <ContextualTrigger onCreated={onCreated} />
        </TaskComposerProvider>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Open composer' }))

    const title = await screen.findByRole('textbox', { name: /标题/ })
    const context = screen.getByRole('textbox', { name: /背景 \/ 问题/ })
    const expectedResult = screen.getByRole('textbox', { name: /期望结果/ })
    expect(title).toBeRequired()
    expect(context).toBeRequired()
    expect(expectedResult).toBeRequired()

    await waitFor(() => expect(screen.getByRole('combobox', { name: /项目/ })).toHaveValue('12'))
    await waitFor(() => expect(screen.getByRole('combobox', { name: '里程碑' })).toHaveValue('milestone-1'))

    fireEvent.change(title, { target: { value: CREATED_TASK.title } })
    fireEvent.change(context, { target: { value: CREATED_TASK.context } })
    fireEvent.change(expectedResult, { target: { value: CREATED_TASK.expected_result } })
    fireEvent.click(screen.getByRole('button', { name: '创建任务' }))

    await waitFor(() => expect(tasksAPI.createTask).toHaveBeenCalledWith({
      title: CREATED_TASK.title,
      context: CREATED_TASK.context,
      expected_result: CREATED_TASK.expected_result,
      project_number: 12,
      milestone_id: 'milestone-1',
      assignee_id: null,
      priority: 'none',
    }))
    expect(onCreated).toHaveBeenCalledWith(CREATED_TASK)
  })
})
