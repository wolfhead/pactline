import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AdminConnectionsPage from './AdminConnectionsPage'
import { createGitLabConnection, listGitLabConnections } from '@/api/admin-gitlab-connections'

vi.mock('@/api/admin-gitlab-connections', () => ({
  createGitLabConnection: vi.fn(),
  disableGitLabConnection: vi.fn(),
  listGitLabConnections: vi.fn(),
  rotateGitLabCredential: vi.fn(),
  validateGitLabConnection: vi.fn(),
}))

const CONNECTION = {
  id: 'connection-1', version: 1, label: 'App', origin: 'https://gitlab.example',
  gitlab_project_id: 42, path_with_namespace: 'team/app',
  canonical_web_url: 'https://gitlab.example/team/app', default_branch: 'main',
  credential_expires_at: null, status: 'active' as const,
  last_validated_at: '2026-08-13T08:00:00Z', created_at: '2026-08-13T08:00:00Z',
  updated_at: '2026-08-13T08:00:00Z',
}

describe('AdminConnectionsPage', () => {
  afterEach(cleanup)
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(listGitLabConnections).mockResolvedValue([])
    vi.mocked(createGitLabConnection).mockResolvedValue(CONNECTION)
  })

  it('clears the write-only token as soon as creation starts', async () => {
    let resolveCreate: ((value: typeof CONNECTION) => void) | undefined
    vi.mocked(createGitLabConnection).mockReturnValue(new Promise((resolve) => { resolveCreate = resolve }))
    render(<AdminConnectionsPage />)
    await screen.findByText('尚未配置 GitLab Connection。')
    fireEvent.change(screen.getByLabelText('显示名称'), { target: { value: 'App' } })
    fireEvent.change(screen.getByLabelText('GitLab 仓库地址'), { target: { value: CONNECTION.canonical_web_url } })
    const token = screen.getByLabelText(/只读 Access Token/) as HTMLInputElement
    fireEvent.change(token, { target: { value: 'secret-token' } })
    fireEvent.click(screen.getByRole('button', { name: '创建并鉴权' }))
    expect(token.value).toBe('')
    expect(createGitLabConnection).toHaveBeenCalledWith({
      label: 'App', repository_url: CONNECTION.canonical_web_url, credential: 'secret-token',
      credential_expires_at: null,
    })
    resolveCreate?.(CONNECTION)
    await waitFor(() => expect(screen.getByText(/已创建 team\/app/)).toBeInTheDocument())
  })

  it('does not present a loading failure as an empty Connection list', async () => {
    vi.mocked(listGitLabConnections).mockRejectedValue(new Error('provider unavailable'))
    render(<AdminConnectionsPage />)

    expect(await screen.findByRole('alert')).toHaveTextContent('provider unavailable')
    expect(screen.queryByText('尚未配置 GitLab Connection。')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试加载' })).toBeInTheDocument()
  })
})
