import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectRepositoryAccessPanel from './ProjectRepositoryAccessPanel'
import {
  bindProjectRepository,
  listProjectRepositories,
  unbindProjectRepository,
} from '@/api/project-repositories'

vi.mock('@/api/project-repositories', () => ({
  bindProjectRepository: vi.fn(),
  listProjectRepositories: vi.fn(),
  unbindProjectRepository: vi.fn(),
}))

describe('ProjectRepositoryAccessPanel', () => {
  afterEach(cleanup)
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(listProjectRepositories).mockResolvedValue([])
    vi.mocked(bindProjectRepository).mockResolvedValue({
      project_version: 4,
      repository: {
        id: 'repository-1', canonical_web_url: 'https://gitlab.example/team/app',
        provider: 'gitlab', origin: 'https://gitlab.example',
        path_with_namespace: 'team/app', bound_at: '2026-08-13T00:00:00Z',
      },
    })
    vi.mocked(unbindProjectRepository).mockReset()
  })

  it('binds a pasted repository URL and reloads the Project', async () => {
    const onChanged = vi.fn().mockResolvedValue(undefined)
    render(
      <ProjectRepositoryAccessPanel
        projectNumber={12}
        projectVersion={3}
        canManage
        archived={false}
        onChanged={onChanged}
      />,
    )
    await screen.findByText('尚未添加代码仓库。项目管理员可以直接粘贴 GitHub 或 GitLab 仓库地址。')
    fireEvent.change(screen.getByLabelText('代码仓库地址'), {
      target: { value: 'https://gitlab.example/team/app' },
    })
    fireEvent.change(screen.getByLabelText('仓库类型'), { target: { value: 'gitlab' } })
    fireEvent.click(screen.getByRole('button', { name: '添加仓库' }))
    await waitFor(() => expect(bindProjectRepository).toHaveBeenCalledWith(
      12, 3, 'https://gitlab.example/team/app', 'gitlab',
    ))
    expect(onChanged).toHaveBeenCalledOnce()
  })

  it('keeps repository authorization read-only for ordinary members', async () => {
    render(
      <ProjectRepositoryAccessPanel
        projectNumber={12}
        projectVersion={3}
        canManage={false}
        archived={false}
        onChanged={vi.fn()}
      />,
    )
    await screen.findByText('尚未添加代码仓库。项目管理员可以直接粘贴 GitHub 或 GitLab 仓库地址。')
    expect(screen.queryByLabelText('代码仓库地址')).not.toBeInTheDocument()
  })

  it('does not present a loading failure as an unbound Project', async () => {
    vi.mocked(listProjectRepositories).mockRejectedValue(new Error('read failed'))
    render(
      <ProjectRepositoryAccessPanel
        projectNumber={12}
        projectVersion={3}
        canManage
        archived={false}
        onChanged={vi.fn()}
      />,
    )

    expect(await screen.findByRole('alert')).toHaveTextContent('read failed')
    expect(screen.queryByText('尚未添加代码仓库。项目管理员可以直接粘贴 GitHub 或 GitLab 仓库地址。')).not.toBeInTheDocument()
  })
})
