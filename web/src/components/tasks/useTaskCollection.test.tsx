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
  it('uses aggregate tasks initially and fetches when explicitly reloaded', async () => {
    vi.mocked(tasksApi.listLabels).mockResolvedValue([])
    vi.mocked(tasksApi.listTasks).mockResolvedValue({ items: [], has_more: false })
    const initialTask = { id: 'task-1', number: 1 } as never

    const { result } = renderHook(() => useTaskCollection({
      project_number: 7,
      backlog_only: true,
    }, 'user-1', {
      initialTasks: [initialTask],
    }))

    expect(result.current.loading).toBe(false)
    expect(result.current.tasks).toEqual([initialTask])
    expect(tasksApi.listTasks).not.toHaveBeenCalled()

    act(() => result.current.reload())

    await waitFor(() => expect(tasksApi.listTasks).toHaveBeenCalledWith(
      expect.objectContaining({
        project_number: 7,
        backlog_only: true,
      }),
    ))
  })

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

  it('reloads the saved number of cursor pages when a collection remounts', async () => {
    vi.mocked(tasksApi.listLabels).mockResolvedValue([])
    vi.mocked(tasksApi.listTasks)
      .mockResolvedValueOnce({
        items: [{ id: 'task-1', number: 1 } as never],
        next_cursor: 'page-2',
        has_more: true,
      })
      .mockResolvedValueOnce({
        items: [{ id: 'task-2', number: 2 } as never],
        has_more: false,
      })

    const { result } = renderHook(() => useTaskCollection({}, 'user-1', {
      initialPageCount: 2,
    }))

    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(tasksApi.listTasks).toHaveBeenNthCalledWith(1, expect.objectContaining({
      cursor: undefined,
    }))
    expect(tasksApi.listTasks).toHaveBeenNthCalledWith(2, expect.objectContaining({
      cursor: 'page-2',
    }))
    expect(result.current.tasks.map((task) => task.number)).toEqual([1, 2])
  })
})
