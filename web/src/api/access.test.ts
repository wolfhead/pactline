import { afterEach, describe, expect, it, vi } from 'vitest'
import { listAdminAPIActivity } from './access'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('API access client', () => {
  it('serializes every administrator audit filter without losing the cursor', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await listAdminAPIActivity({
      userID: 'user-id',
      tokenID: 'token-id',
      method: 'patch',
      route: '/api/v1/tasks/{number}',
      status: 412,
      requestID: 'request-id',
      cursor: 'opaque-cursor',
      pageSize: 25,
    })

    const [rawURL, init] = fetchMock.mock.calls[0]
    const url = new URL(String(rawURL), 'http://localhost')
    expect(url.pathname).toBe('/api/admin/api-activity')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      user_id: 'user-id',
      token_id: 'token-id',
      method: 'patch',
      route: '/api/v1/tasks/{number}',
      status: '412',
      request_id: 'request-id',
      cursor: 'opaque-cursor',
      page_size: '25',
    })
    expect(init).toMatchObject({ method: 'GET', credentials: 'same-origin' })
  })
})
