import { afterEach, describe, expect, it, vi } from 'vitest'
import { listTasks, updateTask } from '../tasks'
import { updateMilestone } from '../projects'

describe('versioned work resource clients', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('unwraps v1 collections for existing UI consumers', async () => {
    const task = { number: 42, version: 3 }
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      items: [task],
      next_cursor: 'next-page',
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listTasks({ limit: 1 })).resolves.toEqual({
      items: [task],
      next_cursor: 'next-page',
      has_more: true,
    })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/tasks?limit=1')
  })

  it('uses the task version as If-Match and accepts the next ETag', async () => {
    const task = { number: 42, version: 4 }
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(task), {
      status: 200,
      headers: { ETag: '"4"' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(updateTask(42, 3, { title: 'Updated' })).resolves.toEqual(task)
    expect(fetchMock.mock.calls[0]).toEqual([
      '/api/v1/tasks/42',
      expect.objectContaining({
        method: 'PATCH',
        headers: expect.objectContaining({ 'If-Match': '"3"' }),
      }),
    ])
  })

  it('sends both milestone and project preconditions', async () => {
    const milestone = { id: 'm1', version: 6 }
    const fetchMock = vi.fn().mockResolvedValue(new Response(
      JSON.stringify(milestone),
      { status: 200, headers: { ETag: '"6"' } },
    ))
    vi.stubGlobal('fetch', fetchMock)

    await updateMilestone(7, 9, 'm1', 5, { name: 'Ready' })
    expect(fetchMock.mock.calls[0]).toEqual([
      '/api/v1/projects/7/milestones/m1',
      expect.objectContaining({
        method: 'PATCH',
        headers: expect.objectContaining({
          'If-Match': '"5"',
          'X-Project-If-Match': '"9"',
        }),
      }),
    ])
  })
})
