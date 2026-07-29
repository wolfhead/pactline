import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import ProjectListPage from './ProjectListPage'
import * as projectsApi from '@/api/projects'

vi.mock('@/api/projects')
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    me: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    users: [{ id: 'u1', name: 'Alex', email: 'a@example.com' }],
    switchTo: () => {},
  }),
}))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

beforeEach(() => {
  vi.mocked(projectsApi.listProjects).mockResolvedValue([{
    id: 'p1', number: 12, version: 1, name: 'Launch project surface',
    outcome: 'Teams can manage delivery outcomes', description: '',
    owner: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    status: 'active', target_date: null, completed_at: null, cancelled_at: null,
    archived_at: null, created_at: '', updated_at: '', completed_tasks: 3,
    eligible_tasks: 4, active_criteria: 2, satisfied_criteria: 1,
  }])
})

describe('ProjectListPage', () => {
  it('shows outcome, ownership, task progress, and acceptance readiness', async () => {
    render(<MemoryRouter><ProjectListPage /></MemoryRouter>)
    expect(await screen.findByText('Launch project surface')).toBeVisible()
    expect(screen.getByText('Teams can manage delivery outcomes')).toBeVisible()
    expect(screen.getByText('负责人：Alex')).toBeVisible()
    expect(screen.getByText('任务进度：75%')).toBeVisible()
    expect(screen.getByText('验收：1/2')).toBeVisible()
  })
})
