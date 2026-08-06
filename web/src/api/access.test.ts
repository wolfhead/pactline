import { afterEach, describe, expect, it, vi } from 'vitest'
import { listAdminAPIActivity, listAdminLarkAPIActivity } from './access'

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

  it('serializes safe Lark API audit filters and correlation identifiers', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await listAdminLarkAPIActivity({
      operation: 'send_notification',
      category: 'notification',
      outcome: 'rate_limited',
      status: 429,
      providerRequestID: 'lark-request-id',
      requestID: 'pactline-request-id',
      actorUserID: 'actor-id',
      agentRunID: 'run-id',
      eventID: 'event-id',
      cursor: 'opaque-cursor',
      pageSize: 25,
    })

    const [rawURL, init] = fetchMock.mock.calls[0]
    const url = new URL(String(rawURL), 'http://localhost')
    expect(url.pathname).toBe('/api/admin/lark-api-activity')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      operation: 'send_notification',
      category: 'notification',
      outcome: 'rate_limited',
      status: '429',
      provider_request_id: 'lark-request-id',
      request_id: 'pactline-request-id',
      actor_user_id: 'actor-id',
      agent_run_id: 'run-id',
      event_id: 'event-id',
      cursor: 'opaque-cursor',
      page_size: '25',
    })
    expect(init).toMatchObject({ method: 'GET', credentials: 'same-origin' })
  })
})
