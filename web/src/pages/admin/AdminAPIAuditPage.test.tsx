import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import AdminAPIAuditPage from './AdminAPIAuditPage'
import * as accessAPI from '@/api/access'
import * as identityAPI from '@/api/admin-identity'
import type { User } from '@/identity'

vi.mock('@/api/access')
vi.mock('@/api/admin-identity')

const USER: User = {
  id: 'user-1',
  name: 'Alex',
  email: 'alex@example.com',
  avatar_url: null,
  platform_role: 'MEMBER',
  roles: [],
  active: true,
  created_at: '',
  updated_at: '',
}
const TOKEN: accessAPI.APIToken = {
  id: 'token-1',
  user_id: USER.id,
  name: 'Planning agent',
  display_prefix: 'bb_pat',
  scopes: ['work:write'],
  expires_at: '2027-01-01T00:00:00Z',
  last_used_at: null,
  revoked_at: null,
  revoked_by_user_id: null,
  created_at: '2026-07-29T00:00:00Z',
}
const EVENT = {
  id: 'event-1',
  occurred_at: '2026-07-29T00:00:00Z',
  request_id: 'request-1',
  auth_method: 'api_token',
  auth_outcome: 'authenticated',
  user_id: USER.id,
  token_id: TOKEN.id,
  token_name: TOKEN.name,
  method: 'PATCH',
  route_pattern: '/api/v1/tasks/{number}',
  status_code: 412,
  problem_code: 'VERSION_CONFLICT',
  duration_ms: 12,
  response_bytes: 128,
  idempotency_replayed: false,
  user_agent: 'Agent client',
  request_body: 'request-body-must-never-render',
  response_body: 'response-body-must-never-render',
} as unknown as accessAPI.APIAccessEvent

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

beforeEach(() => {
  vi.mocked(identityAPI.listAdminUsers).mockResolvedValue([USER])
  vi.mocked(accessAPI.listAdminAPITokens).mockResolvedValue({ items: [{ user: USER, token: TOKEN }] })
  vi.mocked(accessAPI.listAdminAPIActivity).mockImplementation(async (filters) => ({
    items: [EVENT],
    next_cursor: filters?.cursor ? undefined : 'next-page',
  }))
  vi.mocked(accessAPI.revokeAdminAPIToken).mockResolvedValue()
})

describe('AdminAPIAuditPage', () => {
  it('preserves all active filters while moving to the next cursor page', async () => {
    render(<AdminAPIAuditPage />)
    await waitFor(() => expect(accessAPI.listAdminAPIActivity).toHaveBeenCalled())
    await screen.findByRole('option', { name: 'Alex · Planning agent' })

    fireEvent.change(screen.getByLabelText('用户'), { target: { value: USER.id } })
    fireEvent.change(screen.getByLabelText('Token'), { target: { value: TOKEN.id } })
    fireEvent.change(screen.getByLabelText('方法'), { target: { value: 'PATCH' } })
    fireEvent.change(screen.getByLabelText('路由'), { target: { value: '/api/v1/tasks/{number}' } })
    fireEvent.change(screen.getByLabelText('状态码'), { target: { value: '412' } })
    fireEvent.change(screen.getByLabelText('Request ID'), { target: { value: 'request-1' } })
    fireEvent.click(screen.getByRole('button', { name: '筛选' }))

    const expected = {
      userID: USER.id,
      tokenID: TOKEN.id,
      method: 'PATCH',
      route: '/api/v1/tasks/{number}',
      status: 412,
      requestID: 'request-1',
      cursor: undefined,
      pageSize: 50,
    }
    await waitFor(() => expect(accessAPI.listAdminAPIActivity).toHaveBeenCalledWith(expected))

    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => expect(accessAPI.listAdminAPIActivity).toHaveBeenCalledWith({
      ...expected,
      cursor: 'next-page',
    }))
    expect(document.body.textContent).not.toContain('request-body-must-never-render')
    expect(document.body.textContent).not.toContain('response-body-must-never-render')
  })

  it('lets an administrator revoke another user token without exposing its secret', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(<AdminAPIAuditPage />)

    const revoke = await screen.findByRole('button', { name: '撤销' })
    fireEvent.click(revoke)

    await waitFor(() => expect(accessAPI.revokeAdminAPIToken).toHaveBeenCalledWith(TOKEN.id))
    expect(document.body.textContent).not.toContain('secret')
  })
})
