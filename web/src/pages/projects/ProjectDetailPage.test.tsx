import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import ProjectDetailPage from './ProjectDetailPage'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/projects')
vi.mock('@/api/acceptance')
vi.mock('@/api/tasks')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    me: { id: 'u1', name: 'Alex' },
    actor: { id: 'u1', name: 'Alex', platform_role: 'ADMIN' },
    impersonation: null,
    users: [{ id: 'u1', name: 'Alex' }],
  }),
}))

const DETAIL = {
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

describe('ProjectDetailPage', () => {
  beforeEach(() => {
    vi.mocked(projectsApi.getProject).mockResolvedValue(DETAIL)
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
})
