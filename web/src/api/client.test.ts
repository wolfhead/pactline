import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, apiGet, apiPost } from './client'

describe('api client', () => {
  afterEach(() => {
    document.cookie = 'bb_csrf=; Max-Age=0; path=/'
    vi.restoreAllMocks()
  })

  it('uses same-origin credentials without the retired identity header', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([{ id: 'u1' }]), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiGet<{ id: string }[]>('/api/users')).resolves.toEqual([{ id: 'u1' }])

    const [path, init] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/users')
    expect(init).toMatchObject({ method: 'GET', credentials: 'same-origin' })
    expect(init.headers).not.toHaveProperty('X-User-Id')
  })

  it('adds the CSRF cookie value to mutation requests', async () => {
    document.cookie = 'bb_csrf=csrf-secret; path=/'
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await apiPost('/api/auth/logout')

    const [, init] = fetchMock.mock.calls[0]
    expect(init.headers).toMatchObject({ 'X-CSRF-Token': 'csrf-secret' })
  })

  it('raises ApiError carrying the server message and status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation(() => Promise.resolve(
        new Response(JSON.stringify({ error: 'invalid status transition' }), { status: 409 }),
      )),
    )

    await expect(apiPost('/api/tasks/1', {}))
      .rejects.toMatchObject({ status: 409, message: 'invalid status transition' })
    await expect(apiPost('/api/tasks/1', {})).rejects.toBeInstanceOf(ApiError)
  })

  it('wraps non-JSON responses in ApiError', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response('<html>502 Bad Gateway</html>', { status: 502, statusText: 'Bad Gateway' }),
      ),
    )

    await expect(apiGet('/api/users')).rejects.toMatchObject({ status: 502 })
    await expect(apiGet('/api/users')).rejects.not.toBeInstanceOf(SyntaxError)
  })
})
