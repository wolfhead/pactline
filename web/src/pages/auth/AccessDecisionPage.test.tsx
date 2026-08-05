import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import AccessDecisionPage from './AccessDecisionPage'
import { useIdentity, type AccessStatus, type User } from '@/identity'

vi.mock('@/identity', () => ({ useIdentity: vi.fn() }))

const mockUseIdentity = vi.mocked(useIdentity)

function identityValue(accessStatus: AccessStatus, refresh = vi.fn(), logout = vi.fn()) {
  const actor: User = {
    id: 'user', name: 'Alice', email: 'alice@example.test', avatar_url: null,
    platform_role: 'MEMBER', access_status: accessStatus, roles: [], active: true,
    created_at: '2026-08-04T00:00:00Z', updated_at: '2026-08-04T00:00:00Z',
  }
  return {
    status: 'authenticated' as const, actor, subject: actor, me: actor, users: [actor],
    impersonation: null, isReadOnly: false, error: null, refresh,
    loginForDevelopment: vi.fn(), logout, endImpersonation: vi.fn(),
  }
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('AccessDecisionPage', () => {
  it('lets a pending member recheck approval or log out', () => {
    const refresh = vi.fn().mockResolvedValue(undefined)
    const logout = vi.fn().mockResolvedValue(undefined)
    mockUseIdentity.mockReturnValue(identityValue('PENDING', refresh, logout))

    render(<AccessDecisionPage />)

    expect(screen.getByRole('heading', { name: '访问申请等待审批' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重新检查状态' }))
    fireEvent.click(screen.getByRole('button', { name: '退出登录' }))
    expect(refresh).toHaveBeenCalledOnce()
    expect(logout).toHaveBeenCalledOnce()
  })

  it('explains how a rejected member can recover', () => {
    mockUseIdentity.mockReturnValue(identityValue('REJECTED'))

    render(<AccessDecisionPage />)

    expect(screen.getByRole('heading', { name: '访问申请未通过' })).toBeInTheDocument()
    expect(screen.getByText(/联系系统管理员重新审核/)).toBeInTheDocument()
  })
})
