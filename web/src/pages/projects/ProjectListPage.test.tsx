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
    description: 'Teams can manage long-lived work',
    owner: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    creator: { id: 'u1', name: 'Alex', email: 'a@example.com' },
    archived_at: null, created_at: '', updated_at: '', completed_tasks: 3,
    eligible_tasks: 4,
  }])
})

describe('ProjectListPage', () => {
  it('shows durable Project context, ownership, and task counts', async () => {
    render(<MemoryRouter><ProjectListPage /></MemoryRouter>)
    expect(await screen.findByText('Launch project surface')).toBeVisible()
    expect(screen.getByText('Teams can manage long-lived work')).toBeVisible()
    expect(screen.getByText('负责人：Alex')).toBeVisible()
    expect(screen.getByText('任务：3/4')).toBeVisible()
  })
})
