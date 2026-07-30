import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import ProjectDetailPage from './ProjectDetailPage'
import * as acceptanceApi from '@/api/acceptance'
import * as projectsApi from '@/api/projects'
import * as tasksApi from '@/api/tasks'
import type { ProjectDetail } from '@/api/projects'
import type { Task } from '@/task-types'

vi.mock('@/api/projects')
vi.mock('@/api/acceptance')
vi.mock('@/api/tasks')
vi.mock('@/components/tasks/TaskComposer', () => ({
  useTaskComposer: () => ({ openTaskComposer: vi.fn() }),
}))
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    me: { id: 'u1', name: 'Alex' },
    actor: { id: 'u1', name: 'Alex', platform_role: 'ADMIN' },
    impersonation: null,
    users: [{ id: 'u1', name: 'Alex' }],
  }),
}))

const TASK: Task = {
  id: 'task-501',
  number: 501,
  version: 1,
  title: 'Polish compact project workspace',
  context: 'The task area should remain the primary surface.',
  expected_result: 'Project details stay available without crowding tasks.',
  description: '',
  status: 'in_progress',
  priority: 'high',
  assignee: { id: 'u1', name: 'Alex', email: 'a@example.com' },
  creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
  start_date: '2026-07-30',
  due_date: '2026-07-30',
  project: { id: 'p1', number: 12, name: 'Launch' },
  milestone: { id: 'm1', name: 'Tonight' },
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

const DETAIL: ProjectDetail = {
  project: {
    id: 'p1', number: 12, version: 1, name: 'Launch', description: 'Long-lived workspace',
    owner: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    archived_at: null, created_at: '', updated_at: '',
    completed_tasks: 0, eligible_tasks: 0,
  },
  milestones: [],
  tasks: [],
  activity: [],
}

const MILESTONE_DETAIL: ProjectDetail = {
  ...DETAIL,
  milestones: [{
    id: 'm1',
    project_id: 'p1',
    version: 1,
    name: 'Tonight',
    outcome: 'Ship and verify the workspace refinements',
    description: 'A deliberately secondary milestone description.',
    owner_id: 'u1',
    status: 'active',
    target_date: '2026-07-30',
    position: 0,
    completed_at: null,
    cancelled_at: null,
    created_at: '',
    updated_at: '',
    acceptance_criteria: [],
  }],
  tasks: [TASK],
}

describe('ProjectDetailPage', () => {
  beforeEach(() => {
    vi.mocked(projectsApi.getProject).mockResolvedValue(DETAIL)
    vi.mocked(projectsApi.listProjects).mockResolvedValue([DETAIL.project])
    vi.mocked(tasksApi.listTasks).mockResolvedValue({ items: [TASK], has_more: false })
    vi.mocked(tasksApi.listLabels).mockResolvedValue([])
    vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
    vi.mocked(tasksApi.listComments).mockResolvedValue([])
    vi.mocked(tasksApi.listActivity).mockResolvedValue([])
    vi.mocked(acceptanceApi.listTaskCriteria).mockResolvedValue([])
  })
  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('shows the Project-first shell and deterministic attention view', async () => {
    render(
      <MemoryRouter initialEntries={['/projects/12/overview']}>
        <Routes>
          <Route path="/projects/:number/overview" element={<ProjectDetailPage view="overview" />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(await screen.findByRole('heading', { name: 'Launch' })).toBeVisible()
    expect(screen.getByRole('navigation', { name: '项目视图' })).toBeVisible()
    expect(screen.getByText('逾期里程碑')).toBeVisible()
    expect(screen.getByText('Backlog 0 项')).toBeVisible()
  })

  it('keeps low-frequency Project details collapsed until requested', async () => {
    render(
      <MemoryRouter initialEntries={['/projects/12/overview']}>
        <Routes>
          <Route path="/projects/:number/overview" element={<ProjectDetailPage view="overview" />} />
        </Routes>
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: 'Launch' })
    expect(screen.queryByText('Long-lived workspace')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '编辑项目' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '项目详情' }))

    expect(screen.getByText('Long-lived workspace')).toBeVisible()
    expect(screen.getByRole('button', { name: '编辑项目' })).toBeVisible()
  })

  it('prioritizes milestone tasks and opens their inspector in context', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue(MILESTONE_DETAIL)
    render(
      <MemoryRouter initialEntries={['/projects/12/milestones/m1']}>
        <Routes>
          <Route
            path="/projects/:number/milestones/:milestoneID"
            element={<ProjectDetailPage view="milestones" />}
          />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'Tonight' })).toBeVisible()
    expect(screen.queryByText('Ship and verify the workspace refinements'))
      .not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '里程碑详情' }))
      .toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByRole('button', { name: /验收 0\/0/ }))
      .toHaveAttribute('aria-expanded', 'false')

    const taskLink = await screen.findByRole('link', {
      name: 'Polish compact project workspace',
    })
    expect(taskLink).toHaveAttribute(
      'href',
      '/projects/12/milestones/m1?task=501',
    )

    fireEvent.click(taskLink)

    expect(await screen.findByRole('dialog')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Polish compact project workspace' }))
      .toBeVisible()
  })
})
