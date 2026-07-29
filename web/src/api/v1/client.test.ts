import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ProblemError,
  requireVersioned,
  v1Get,
  v1Patch,
  v1Post,
} from './client'

describe('v1 client', () => {
  afterEach(() => {
    document.cookie = 'bb_csrf=; Max-Age=0; path=/'
    vi.restoreAllMocks()
  })

  it('preserves Problem Details fields', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      type: 'about:blank',
      title: 'Version conflict',
      status: 412,
      detail: 'The resource changed.',
      code: 'VERSION_CONFLICT',
      request_id: 'req-412',
      current_version: 7,
    }), {
      status: 412,
      headers: { 'Content-Type': 'application/problem+json' },
    })))

    const result = v1Patch('/api/v1/tasks/42', {
      ifMatch: '"6"',
      body: { title: 'Changed' },
    })
    await expect(result).rejects.toBeInstanceOf(ProblemError)
    await expect(result).rejects.toMatchObject({
      status: 412,
      code: 'VERSION_CONFLICT',
      requestId: 'req-412',
      currentVersion: 7,
      message: 'The resource changed.',
    })
  })

  it('retains the response ETag with a resource', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ number: 42, version: 3 }),
      { status: 200, headers: { ETag: '"3"', 'X-Request-ID': 'req-ok' } },
    )))

    await expect(v1Get<{ number: number; version: number }>('/api/v1/tasks/42'))
      .resolves.toEqual({
        value: { number: 42, version: 3 },
        etag: '"3"',
        requestId: 'req-ok',
      })
  })

  it('sends optimistic preconditions and browser CSRF without Agent headers', async () => {
    document.cookie = 'bb_csrf=csrf-secret; path=/'
    const fetchMock = vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ version: 4 }),
      { status: 200, headers: { ETag: '"4"' } },
    ))
    vi.stubGlobal('fetch', fetchMock)

    await v1Patch('/api/v1/projects/7/milestones/m1', {
      ifMatch: '"3"',
      projectIfMatch: '"8"',
      body: { name: 'Ready' },
    })

    const [, init] = fetchMock.mock.calls[0]
    expect(init).toMatchObject({
      method: 'PATCH',
      credentials: 'same-origin',
      headers: {
        'Content-Type': 'application/json',
        'If-Match': '"3"',
        'X-Project-If-Match': '"8"',
        'X-CSRF-Token': 'csrf-secret',
      },
    })
    expect(init.headers).not.toHaveProperty('Authorization')
    expect(init.headers).not.toHaveProperty('Idempotency-Key')
  })

  it('requires ETag when a caller requests a versioned result', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ id: 'p1' }),
      { status: 201, headers: { 'X-Request-ID': 'req-missing-etag' } },
    )))

    const response = await v1Post<{ id: string }>('/api/v1/projects', {
      body: { name: 'Project' },
    })
    expect(() => requireVersioned(response)).toThrowError(ProblemError)
    expect(() => requireVersioned(response)).toThrowError('MISSING_ETAG')
  })
})
