import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import * as projectsApi from '@/api/projects'
import NavSidebar from './NavSidebar'

vi.mock('@/api/projects')
vi.mock('./TaskComposer', () => ({
  useTaskComposer: () => ({ openTaskComposer: vi.fn() }),
}))
vi.mock('@/identity', () => ({
  useIdentity: () => ({
    actor: { id: 'admin', platform_role: 'ADMIN' },
    impersonation: null,
    me: { id: 'admin' },
    isReadOnly: false,
  }),
}))

const PROJECTS = Array.from({ length: 8 }, (_, index) => ({
  id: `project-${index + 1}`,
  number: index + 1,
  version: 1,
  name: `Project ${index + 1}`,
  description: '',
  creator: { id: 'admin', name: 'Admin', email: null },
  archived_at: null,
  created_at: '',
  updated_at: '',
  completed_tasks: 0,
  eligible_tasks: 0,
}))

function renderSidebar(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <NavSidebar />
    </MemoryRouter>,
  )
}

describe('NavSidebar', () => {
  beforeEach(() => {
    vi.mocked(projectsApi.listProjects).mockResolvedValue(PROJECTS)
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('keeps work and projects primary while low-frequency groups start collapsed', async () => {
    renderSidebar('/tasks')

    expect(screen.getByRole('link', { name: '我的工作' }))
      .toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('button', { name: '开发者工具' }))
      .toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByRole('button', { name: 'Agent' }))
      .toHaveAttribute('aria-expanded', 'false')
    expect(screen.getByRole('button', { name: '系统管理' }))
      .toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('link', { name: 'API Token' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '用户' })).not.toBeInTheDocument()
    expect(await screen.findByRole('link', { name: 'Project 1' })).toBeVisible()
  })

  it('reveals developer tools on demand', () => {
    renderSidebar('/tasks')

    fireEvent.click(screen.getByRole('button', { name: '开发者工具' }))

    expect(screen.getByRole('link', { name: 'API Token' })).toBeVisible()
    expect(screen.getByRole('link', { name: 'API 文档' })).toBeVisible()
  })

  it('automatically opens the group containing the current admin route', () => {
    renderSidebar('/admin/users')

    expect(screen.getByRole('button', { name: '系统管理' }))
      .toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('link', { name: '用户' }))
      .toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('button', { name: '开发者工具' }))
      .toHaveAttribute('aria-expanded', 'false')
  })

  it('limits long project lists while always retaining the current project', async () => {
    renderSidebar('/projects/8/milestones')

    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'Project 8' })).toBeVisible(),
    )
    expect(screen.getByRole('link', { name: 'Project 8' }))
      .toHaveAttribute('aria-current', 'page')
    expect(screen.queryByRole('link', { name: 'Project 6' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Project 7' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看全部 8 个项目' })).toBeVisible()
  })
})
