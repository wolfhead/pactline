import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import ProjectDetailPage from './ProjectDetailPage'
import * as projectsApi from '@/api/projects'
import { ProblemError } from '@/api/v1/client'

vi.mock('@/api/projects')
vi.mock('@/api/acceptance')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    me: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    users: [{ id: 'u1', name: 'Alex', email: 'a@example.com' }],
  }),
}))

const PROJECT = {
  id: 'p1',
  number: 12,
  version: 1,
  name: 'Launch',
  outcome: 'Release the workflow',
  description: '',
  owner: { id: 'u1', name: 'Alex', email: 'a@example.com' },
  creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
  status: 'planned' as const,
  target_date: null,
  completed_at: null,
  cancelled_at: null,
  archived_at: null,
  created_at: '',
  updated_at: '',
  completed_tasks: 0,
  eligible_tasks: 0,
  active_criteria: 0,
  satisfied_criteria: 0,
}

const DETAIL = {
  project: PROJECT,
  acceptance_criteria: [],
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

  it('reloads the project after a concurrent Agent update', async () => {
    const latest = {
      ...DETAIL,
      project: { ...PROJECT, version: 2, name: 'Launch updated by Agent' },
    }
    vi.mocked(projectsApi.getProject)
      .mockResolvedValueOnce(DETAIL)
      .mockResolvedValueOnce(latest)
    vi.mocked(projectsApi.updateProject).mockRejectedValue(
      new ProblemError(412, 'VERSION_CONFLICT', 'req-project-conflict', 2),
    )

    render(
      <MemoryRouter initialEntries={['/projects/12']}>
        <Routes>
          <Route path="/projects/:number" element={<ProjectDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )
    await screen.findByRole('heading', { name: 'Launch' })
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    fireEvent.change(screen.getByLabelText('项目名称'), {
      target: { value: 'My stale edit' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))

    await waitFor(() => {
      expect(projectsApi.updateProject).toHaveBeenCalledWith(
        12,
        1,
        expect.objectContaining({ name: 'My stale edit' }),
      )
    })
    expect(await screen.findByText(/内容已被其他用户或 Agent 更新，已加载最新版本。/))
      .toBeVisible()
    expect(screen.getByRole('heading', { name: 'Launch updated by Agent' })).toBeVisible()
  })
})
