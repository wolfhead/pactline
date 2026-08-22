import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import ProjectDetailPage from './ProjectDetailPage'
import * as acceptanceApi from '@/api/acceptance'
import * as projectsApi from '@/api/projects'
import * as tasksApi from '@/api/tasks'
import type { Milestone, ProjectDetail } from '@/api/projects'
import type { Task } from '@/task-types'

vi.mock('@/api/projects')
vi.mock('@/api/acceptance')
vi.mock('@/api/tasks')
const openTaskComposer = vi.hoisted(() => vi.fn())
vi.mock('@/components/tasks/TaskComposer', () => ({
  useTaskComposer: () => ({ openTaskComposer }),
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
  phase: 'in_progress',
  activity: 'working',
  review_cycle: 0,
  main_thread_id: 'thread-main-501',
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

function milestone(
  id: string,
  name: string,
  status: Milestone['status'],
  position: number,
): Milestone {
  return {
    id,
    project_id: 'p1',
    version: 1,
    name,
    outcome: `${name} outcome`,
    description: '',
    owner_id: 'u1',
    status,
    target_date: null,
    position,
    completed_at: status === 'completed' ? '2026-07-30T00:00:00Z' : null,
    cancelled_at: status === 'cancelled' ? '2026-07-30T00:00:00Z' : null,
    created_at: '',
    updated_at: '',
    acceptance_criteria: [],
  }
}

function milestoneTask(
  number: number,
  milestoneID: string,
  phase: Task['phase'],
  archived = false,
): Task {
  return {
    ...TASK,
    id: `task-${number}`,
    number,
    title: `Task ${number}`,
    phase,
    milestone: { id: milestoneID, name: milestoneID },
    archived_at: archived ? '2026-08-20T00:00:00Z' : null,
  }
}

const STATUS_DETAIL: ProjectDetail = {
  ...DETAIL,
  milestones: [
    milestone('m-completed', 'Completed delivery', 'completed', 2),
    milestone('m-planned', 'Planned delivery', 'planned', 1),
    milestone('m-cancelled', 'Cancelled delivery', 'cancelled', 3),
    milestone('m-active', 'Active delivery', 'active', 0),
  ],
  tasks: [{ ...TASK, milestone: { id: 'm-active', name: 'Active delivery' } }],
}

describe('ProjectDetailPage', () => {
  beforeEach(() => {
    vi.mocked(projectsApi.getProject).mockResolvedValue(DETAIL)
    vi.mocked(projectsApi.listProjectMembers).mockResolvedValue([{
      id: 'pm1', project_id: 'p1', user: { id: 'u1', name: 'Alex', email: 'a@example.com' },
      role: 'admin', active: true, created_at: '', updated_at: '',
    }])
    vi.mocked(projectsApi.listProjects).mockResolvedValue([DETAIL.project])
    vi.mocked(tasksApi.listTasks).mockResolvedValue({ items: [TASK], has_more: false })
    vi.mocked(tasksApi.listLabels).mockResolvedValue([])
    vi.mocked(tasksApi.getTask).mockResolvedValue(TASK)
    vi.mocked(tasksApi.listActivity).mockResolvedValue([])
    vi.mocked(tasksApi.listTaskAttachments).mockResolvedValue([])
    vi.mocked(acceptanceApi.listTaskCriteria).mockResolvedValue([])
  })
  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('renders the canonical workspace in Project, Milestones, Backlog order', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue({
      ...DETAIL,
      tasks: [{ ...TASK, milestone: null }],
    })
    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )
    const projectHeading = await screen.findByRole('heading', { name: 'Launch' })
    const milestonesHeading = screen.getByRole('heading', { name: '里程碑' })
    const backlogHeading = screen.getByRole('heading', { name: '项目 Backlog' })

    expect(projectHeading.compareDocumentPosition(milestonesHeading))
      .toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(milestonesHeading.compareDocumentPosition(backlogHeading))
      .toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(screen.queryByText('需要关注')).not.toBeInTheDocument()
    expect(screen.queryByText('最近动态')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: TASK.title })).toBeVisible()
    expect(tasksApi.listTasks).not.toHaveBeenCalled()
  })

  it('preserves the Backlog Gantt view', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue({
      ...DETAIL,
      tasks: [{ ...TASK, milestone: null }],
    })
    render(
      <MemoryRouter initialEntries={['/projects/12?view=gantt']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await screen.findByRole('heading', { name: '项目 Backlog' })
    expect(screen.getByRole('button', { name: '甘特' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '列表' })).toHaveAttribute('aria-pressed', 'false')
    expect(tasksApi.listTasks).not.toHaveBeenCalled()
  })

  it('keeps low-frequency Project details collapsed until requested', async () => {
    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
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

  it('ignores a stale Project response after navigating to another Project', async () => {
    let resolveFirstProject!: (detail: ProjectDetail) => void
    const firstProject = new Promise<ProjectDetail>((resolve) => {
      resolveFirstProject = resolve
    })
    const currentDetail: ProjectDetail = {
      ...DETAIL,
      project: { ...DETAIL.project, id: 'p2', number: 13, name: 'Current Project' },
    }
    vi.mocked(projectsApi.getProject).mockImplementation((projectNumber) => (
      projectNumber === 12 ? firstProject : Promise.resolve(currentDetail)
    ))
    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Link to="/projects/13">Open current Project</Link>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('link', { name: 'Open current Project' }))
    expect(await screen.findByRole('heading', { name: 'Current Project' })).toBeVisible()
    await act(async () => resolveFirstProject(DETAIL))

    expect(screen.getByRole('heading', { name: 'Current Project' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'Launch' })).not.toBeInTheDocument()
  })

  it('prioritizes current milestones and keeps terminal milestones accessible', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue(STATUS_DETAIL)
    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('link', { name: /Active delivery/ })).toBeVisible()
    expect(screen.getByRole('link', { name: /Planned delivery/ })).toBeVisible()

    const completed = screen.getByRole('link', { name: /Completed delivery/ })
    const cancelled = screen.getByRole('link', { name: /Cancelled delivery/ })
    expect(completed).not.toBeVisible()
    expect(cancelled).not.toBeVisible()

    fireEvent.click(screen.getByText('已结束里程碑 · 2'))

    expect(completed).toBeVisible()
    expect(cancelled).toBeVisible()
    expect(completed).toHaveAttribute('href', '/projects/12/milestones/m-completed')
    expect(cancelled).toHaveAttribute('href', '/projects/12/milestones/m-cancelled')
    expect(projectsApi.getProject).toHaveBeenCalledTimes(1)
    expect(projectsApi.listProjectMembers).toHaveBeenCalledTimes(1)
    expect(tasksApi.listTasks).not.toHaveBeenCalled()
  })

  it('renders accessible bottom-border progress for empty, waiting, done, and mixed milestones', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue({
      ...DETAIL,
      milestones: [
        milestone('m-empty', 'Empty progress', 'active', 0),
        milestone('m-waiting', 'Waiting progress', 'active', 1),
        milestone('m-done', 'Done progress', 'active', 2),
        milestone('m-mixed', 'Mixed progress', 'active', 3),
      ],
      tasks: [
        milestoneTask(601, 'm-empty', 'cancelled'),
        milestoneTask(602, 'm-empty', 'backlog', true),
        milestoneTask(603, 'm-waiting', 'backlog'),
        milestoneTask(604, 'm-waiting', 'ready'),
        milestoneTask(605, 'm-done', 'done'),
        milestoneTask(606, 'm-done', 'done'),
        milestoneTask(607, 'm-mixed', 'done'),
        milestoneTask(608, 'm-mixed', 'in_progress'),
        milestoneTask(609, 'm-mixed', 'in_review'),
        milestoneTask(610, 'm-mixed', 'ready'),
      ],
    })
    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('img', { name: '任务进度：0 个任务。' })).toBeVisible()
    expect(screen.getByRole('img', {
      name: '任务进度：共 2 个任务；已完成 0 个，执行中 0 个，验收中 0 个，等待中 2 个；完成 0%。',
    })).toBeVisible()
    expect(screen.getByRole('img', {
      name: '任务进度：共 2 个任务；已完成 2 个，执行中 0 个，验收中 0 个，等待中 0 个；完成 100%。',
    })).toBeVisible()

    const mixedProgress = screen.getByRole('img', {
      name: '任务进度：共 4 个任务；已完成 1 个，执行中 1 个，验收中 1 个，等待中 1 个；完成 25%。',
    })
    expect(mixedProgress).toHaveClass('absolute', 'inset-x-0', 'bottom-0', 'h-1.5')
    expect(mixedProgress.querySelectorAll('[data-progress-phase]')).toHaveLength(4)
    for (const segment of mixedProgress.querySelectorAll<HTMLElement>('[data-progress-phase]')) {
      expect(segment).toHaveStyle({ width: '25%' })
    }
    expect(mixedProgress.querySelector('[data-progress-phase="in_progress"]'))
      .toHaveClass('milestone-progress-in-progress')

    const mixedCard = screen.getByRole('link', { name: /Mixed progress/ })
    expect(within(mixedCard).getByText('1/4 完成 · 25%')).toBeVisible()
    expect(within(mixedCard).getByText('已完成 1')).toBeVisible()
    expect(within(mixedCard).getByText('执行中 1')).toBeVisible()
    expect(within(mixedCard).getByText('验收中 1')).toBeVisible()
    expect(within(mixedCard).getByText('等待中 1')).toBeVisible()
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
  })

  it('explains the next step when milestones and Backlog are empty', async () => {
    vi.mocked(tasksApi.listTasks).mockResolvedValue({ items: [], has_more: false })
    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByText('还没有里程碑。创建里程碑来规划下一阶段交付。'))
      .toBeVisible()
    expect(await screen.findByText('Backlog 为空。创建任务来记录尚未排入里程碑的工作。'))
      .toBeVisible()
    expect(screen.getAllByRole('button', { name: '新建任务' })).toHaveLength(1)
  })

  it('explains how to continue when every milestone has ended', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue({
      ...DETAIL,
      milestones: STATUS_DETAIL.milestones.filter((item) => (
        item.status === 'completed' || item.status === 'cancelled'
      )),
    })
    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByText(
      '当前没有进行中或计划中的里程碑。可以新建里程碑，或从已结束里程碑中重新开启。',
    )).toBeVisible()
    expect(screen.getByText('已结束里程碑 · 2')).toBeVisible()
  })

  it('prioritizes milestone tasks and links to their standalone page', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue(MILESTONE_DETAIL)
    render(
      <MemoryRouter initialEntries={['/projects/12/milestones/m1']}>
        <Routes>
          <Route
            path="/projects/:number/milestones/:milestoneID"
            element={<ProjectDetailPage view="milestone" />}
          />
          <Route path="/tasks/:number" element={<h1>Standalone task route</h1>} />
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
      '/tasks/501',
    )

    fireEvent.click(taskLink)

    expect(await screen.findByRole('heading', { name: 'Standalone task route' })).toBeVisible()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('updates Milestone task counts when the task composer creates a task', async () => {
    vi.mocked(projectsApi.getProject).mockResolvedValue(MILESTONE_DETAIL)
    render(
      <MemoryRouter initialEntries={['/projects/12/milestones/m1']}>
        <Routes>
          <Route
            path="/projects/:number/milestones/:milestoneID"
            element={<ProjectDetailPage view="milestone" />}
          />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByText('0/1 完成')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '新建任务' }))
    const onCreated = openTaskComposer.mock.calls.at(-1)?.[0].onCreated as (task: Task) => void
    act(() => onCreated({ ...TASK, id: 'task-502', number: 502, title: 'Second task' }))

    expect(screen.getByText('0/2 完成')).toBeVisible()
  })
})
