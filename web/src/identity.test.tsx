import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { IdentityProvider, useIdentity, type MeResponse, type User } from './identity'

const USER: User = {
  id: '00000000-0000-0000-0000-000000000001',
  name: 'Alice',
  email: 'alice@example.com',
  avatar_url: null,
  platform_role: 'ADMIN',
  roles: ['SPONSOR'],
  active: true,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}
const ME: MeResponse = { actor: USER, subject: USER, impersonation: null }

function Probe() {
  const { status, actor, isReadOnly } = useIdentity()
  return <p>{status}:{actor?.name}:{String(isReadOnly)}</p>
}

describe('IdentityProvider', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(cleanup)

  it('loads the server-owned current identity and user references', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(ME), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ items: [USER] }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    render(<IdentityProvider><Probe /></IdentityProvider>)

    await waitFor(() => expect(screen.getByText('authenticated:Alice:false')).toBeInTheDocument())
    expect(fetchMock.mock.calls[0][0]).toBe('/api/me')
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/users')
  })

  it('exposes an unauthenticated state for a 401', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: 'authentication required' }), { status: 401 }),
      ),
    )

    render(<IdentityProvider><Probe /></IdentityProvider>)

    await waitFor(() => expect(screen.getByText('unauthenticated::false')).toBeInTheDocument())
  })

  it('derives read-only state from active impersonation', async () => {
    const impersonated: MeResponse = {
      ...ME,
      subject: { ...USER, id: '00000000-0000-0000-0000-000000000002', name: 'Bob', platform_role: 'MEMBER' },
      impersonation: {
        id: '10000000-0000-0000-0000-000000000001',
        session_id: '10000000-0000-0000-0000-000000000002',
        actor_user_id: USER.id,
        subject_user_id: '00000000-0000-0000-0000-000000000002',
        started_at: '2026-01-01T00:00:00Z',
      },
    }
    vi.stubGlobal(
      'fetch',
      vi.fn()
        .mockResolvedValueOnce(new Response(JSON.stringify(impersonated), { status: 200 }))
        .mockResolvedValueOnce(new Response(JSON.stringify({ items: [USER] }), { status: 200 })),
    )

    render(<IdentityProvider><Probe /></IdentityProvider>)

    await waitFor(() => expect(screen.getByText('authenticated:Alice:true')).toBeInTheDocument())
  })
})
