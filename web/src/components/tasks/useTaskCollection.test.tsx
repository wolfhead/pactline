import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import * as tasksApi from '@/api/tasks'
import { useTaskCollection } from './useTaskCollection'

vi.mock('@/api/tasks')

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('useTaskCollection', () => {
  it('preserves the collection scope until an explicit filter overrides it', async () => {
    vi.mocked(tasksApi.listLabels).mockResolvedValue([])
    vi.mocked(tasksApi.listTasks).mockResolvedValue({ items: [], has_more: false })

    const { result } = renderHook(() => useTaskCollection({
      assignee: 'user-1',
      project_number: 7,
      milestone_id: 'milestone-1',
    }, 'user-1'))

    await waitFor(() => expect(tasksApi.listTasks).toHaveBeenCalled())
    expect(tasksApi.listTasks).toHaveBeenLastCalledWith(expect.objectContaining({
      assignee: 'user-1',
      project_number: 7,
      milestone_id: 'milestone-1',
    }))

    act(() => {
      result.current.setFilters({
        ...result.current.filters,
        assignee: 'user-2',
      })
    })
    await waitFor(() => expect(tasksApi.listTasks).toHaveBeenLastCalledWith(
      expect.objectContaining({
        assignee: 'user-2',
        project_number: 7,
        milestone_id: 'milestone-1',
      }),
    ))
  })
})
