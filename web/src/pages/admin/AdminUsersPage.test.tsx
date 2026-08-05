import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import AdminUsersPage from './AdminUsersPage'
import * as adminAPI from '@/api/admin-identity'
import { useIdentity, type User } from '@/identity'

vi.mock('@/api/admin-identity')
vi.mock('@/identity', () => ({ useIdentity: vi.fn() }))

const PENDING_USER: User = {
  id: 'pending-user', name: 'Pending Member', email: 'pending@example.test', avatar_url: null,
  platform_role: 'MEMBER', access_status: 'PENDING', roles: [], active: true,
  created_at: '2026-08-04T00:00:00Z', updated_at: '2026-08-04T00:00:00Z',
}
const ADMIN: User = {
  ...PENDING_USER, id: 'admin', name: 'Administrator', email: 'admin@example.test',
  platform_role: 'ADMIN', access_status: 'APPROVED',
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('AdminUsersPage', () => {
  it('puts pending access requests first and lets the administrator approve them', async () => {
    vi.mocked(useIdentity).mockReturnValue({
      status: 'authenticated', actor: ADMIN, subject: ADMIN, me: ADMIN, users: [ADMIN],
      impersonation: null, isReadOnly: false, error: null, refresh: vi.fn(),
      loginForDevelopment: vi.fn(), logout: vi.fn(), endImpersonation: vi.fn(),
    })
    vi.mocked(adminAPI.listAdminUsers).mockResolvedValue([PENDING_USER])
    vi.mocked(adminAPI.setUserAccessStatus).mockResolvedValue(undefined)

    render(<AdminUsersPage />)

    expect(await screen.findByText('1 项待处理')).toBeInTheDocument()
    expect(screen.getByText('等待审批')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '通过' }))

    await waitFor(() => expect(adminAPI.setUserAccessStatus).toHaveBeenCalledWith(
      PENDING_USER.id,
      'APPROVED',
    ))
  })
})
