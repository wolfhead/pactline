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
    const [path, init] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/users')
    expect(init.method).toBe('GET')
    const headers = init.headers as Record<string, string>
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

    const fetchMock = fetch as unknown as ReturnType<typeof vi.fn>
    const [path, init] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/bounties/x/transition')
    expect(init.method).toBe('POST')
  })

  it('raises ApiError, not a bare SyntaxError, for a failed response with a non-JSON body (A2)', async () => {
    // The Vite dev proxy returns an HTML error page when the Go backend is
    // down; without a parse guard, JSON.parse throws a SyntaxError that
    // escapes request() entirely, bypassing ApiError.
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('<html>502 Bad Gateway</html>', { status: 502, statusText: 'Bad Gateway' }),
      ),
    )

    let caught: unknown
    try {
      await apiGet('/api/users')
    } catch (err) {
      caught = err
    }

    expect(caught).toBeInstanceOf(ApiError)
    expect(caught).not.toBeInstanceOf(SyntaxError)
    expect((caught as ApiError).status).toBe(502)
  })

  it('raises ApiError, not a bare SyntaxError, for a successful response with a non-JSON body (A2)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('not json', { status: 200, statusText: 'OK' })),
    )

    let caught: unknown
    try {
      await apiGet('/api/users')
    } catch (err) {
      caught = err
    }

    expect(caught).toBeInstanceOf(ApiError)
    expect(caught).not.toBeInstanceOf(SyntaxError)
    expect((caught as ApiError).status).toBe(200)
  })
})
