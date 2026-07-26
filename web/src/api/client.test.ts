import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ApiError, apiGet, apiPost, setCurrentUserId } from './client'

describe('api client', () => {
  beforeEach(() => {
    setCurrentUserId('00000000-0000-0000-0000-000000000001')
  })

  it('sends the current user id as X-User-Id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([{ id: 'u1' }]), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const out = await apiGet<{ id: string }[]>('/api/users')

    expect(out).toEqual([{ id: 'u1' }])
    const headers = fetchMock.mock.calls[0][1].headers as Record<string, string>
    expect(headers['X-User-Id']).toBe('00000000-0000-0000-0000-000000000001')
  })

  it('raises ApiError carrying the server message and status', async () => {
    // Each mocked call must return its own Response instance: a Response
    // body can only be read once, and apiPost is invoked twice below.
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(
        () => Promise.resolve(
          new Response(JSON.stringify({ error: 'invalid status transition' }), { status: 409 }),
        ),
      ),
    )

    await expect(apiPost('/api/bounties/x/transition', { to: 'COMPLETED' }))
      .rejects.toMatchObject({ status: 409, message: 'invalid status transition' })
    await expect(apiPost('/api/bounties/x/transition', {})).rejects.toBeInstanceOf(ApiError)
  })
})
