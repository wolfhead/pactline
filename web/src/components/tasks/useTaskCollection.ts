import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  archiveTask,
  getTask,
  listLabels,
  listTasks,
  restoreTask,
  updateTask,
  type TaskListParams,
} from '@/api/tasks'
import { ProblemError } from '@/api/v1/client'
import type { Label, Task, TaskPatchBody } from '@/task-types'
import { DEFAULT_FILTERS, type TaskFilters } from './FilterBar'

const PAGE_SIZE = 50

function shiftDate(value: string | null, days: number): string | null {
  if (!value) return null
  const date = new Date(`${value}T12:00:00Z`)
  date.setUTCDate(date.getUTCDate() + days)
  return date.toISOString().slice(0, 10)
}

export interface TaskCollectionController {
  filters: TaskFilters
  setFilters: (filters: TaskFilters) => void
  labels: Label[]
  setLabels: (labels: Label[]) => void
  tasks: Task[]
  loading: boolean
  loadingMore: boolean
  error: string
  rowErrors: Record<number, string>
  hasMore: boolean
  hasActiveFilters: boolean
  grouped: boolean
  reload: () => void
  loadMore: () => Promise<void>
  prependTask: (task: Task) => void
  replaceTask: (task: Task) => void
  patchOptimistic: (
    task: Task,
    patch: TaskPatchBody,
    optimistic: Partial<Task>,
  ) => void
  shiftSchedule: (task: Task, days: number) => Promise<Task | null>
  archive: (task: Task) => void
  restore: (task: Task) => void
}

export function useTaskCollection(
  baseQuery: TaskListParams,
  identityKey: string | undefined,
): TaskCollectionController {
  const [filters, setFilters] = useState<TaskFilters>(DEFAULT_FILTERS)
  const [labels, setLabels] = useState<Label[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [nextCursor, setNextCursor] = useState<string>()
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [rowErrors, setRowErrors] = useState<Record<number, string>>({})
  const [reloadToken, setReloadToken] = useState(0)

  const baseQueryKey = useMemo(() => JSON.stringify(baseQuery), [baseQuery])
  const queryKey = useMemo(
    () => JSON.stringify({ baseQuery: baseQueryKey, filters }),
    [baseQueryKey, filters],
  )

  const hasActiveFilters =
    filters.phases.length > 0 ||
    filters.priorities.length > 0 ||
    filters.assignee !== '' ||
    filters.labelId !== '' ||
    filters.search !== ''

  const buildQuery = useCallback((cursor?: string): TaskListParams => ({
    ...baseQuery,
    phase: filters.phases.length ? filters.phases : baseQuery.phase,
    priority: filters.priorities.length ? filters.priorities : baseQuery.priority,
    assignee: filters.assignee || baseQuery.assignee,
    label: filters.labelId ? [filters.labelId] : baseQuery.label,
    q: filters.search || baseQuery.q,
    sort: filters.sort,
    order: filters.order,
    cursor,
    limit: PAGE_SIZE,
  }), [baseQueryKey, filters]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let cancelled = false
    listLabels()
      .then((loaded) => {
        if (!cancelled) setLabels(loaded)
      })
      .catch(() => {
        // Labels are an enhancement to the collection, not a loading gate.
      })
    return () => {
      cancelled = true
    }
  }, [identityKey])

  useEffect(() => {
    setLoading(true)
    setError('')
    let cancelled = false
    listTasks(buildQuery())
      .then((result) => {
        if (cancelled) return
        setTasks(result.items)
        setNextCursor(result.next_cursor)
        setHasMore(result.has_more)
      })
      .catch((cause) => {
        if (!cancelled) setError(String((cause as Error).message))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [queryKey, identityKey, reloadToken, buildQuery])

  const liveQueryRef = useRef('')
  liveQueryRef.current = `${queryKey}|${identityKey ?? ''}|${reloadToken}`

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    const requestKey = liveQueryRef.current
    setLoadingMore(true)
    try {
      const result = await listTasks(buildQuery(nextCursor))
      if (liveQueryRef.current !== requestKey) return
      setTasks((current) => [...current, ...result.items])
      setNextCursor(result.next_cursor)
      setHasMore(result.has_more)
    } catch (cause) {
      if (liveQueryRef.current === requestKey) {
        setError(String((cause as Error).message))
      }
    } finally {
      setLoadingMore(false)
    }
  }

  function replaceTask(updated: Task) {
    setTasks((current) => current.map((task) => (
      task.number === updated.number ? updated : task
    )))
  }

  function patchOptimistic(
    task: Task,
    patch: TaskPatchBody,
    optimistic: Partial<Task>,
  ) {
    const previous: Partial<Task> = {}
    for (const key of Object.keys(optimistic) as (keyof Task)[]) {
      previous[key] = task[key] as never
    }
    setTasks((current) => current.map((candidate) => (
      candidate.number === task.number ? { ...candidate, ...optimistic } : candidate
    )))
    setRowErrors((current) => ({ ...current, [task.number]: '' }))
    updateTask(task.number, task.version, patch)
      .then(replaceTask)
      .catch(async (cause) => {
        if (cause instanceof ProblemError && cause.code === 'VERSION_CONFLICT') {
          try {
            const latest = await getTask(task.number)
            replaceTask(latest)
            setRowErrors((current) => ({
              ...current,
              [task.number]: '内容已被其他用户或 Agent 更新，已加载最新版本。',
            }))
            return
          } catch {
            // Use the ordinary rollback when refreshing also fails.
          }
        }
        setTasks((current) => current.map((candidate) => (
          candidate.number === task.number ? { ...candidate, ...previous } : candidate
        )))
        setRowErrors((current) => ({
          ...current,
          [task.number]: `更新失败：${(cause as Error).message}，已恢复原状态`,
        }))
      })
  }

  async function shiftSchedule(task: Task, days: number): Promise<Task | null> {
    const affected = new Set([task.id, ...(task.children ?? []).map((child) => child.id)])
    const before = new Map(
      tasks.filter((candidate) => affected.has(candidate.id))
        .map((candidate) => [candidate.id, candidate]),
    )
    setTasks((current) => current.map((candidate) => (
      affected.has(candidate.id)
        ? {
            ...candidate,
            start_date: shiftDate(candidate.start_date ?? null, days),
            due_date: shiftDate(candidate.due_date, days),
          }
        : candidate
    )))
    setRowErrors((current) => ({ ...current, [task.number]: '' }))
    try {
      const updated = await updateTask(task.number, task.version, {
        schedule_shift_days: days,
      })
      replaceTask(updated)
      setReloadToken((token) => token + 1)
      return updated
    } catch (cause) {
      setTasks((current) => current.map((candidate) => before.get(candidate.id) ?? candidate))
      setRowErrors((current) => ({
        ...current,
        [task.number]: `排期更新失败：${(cause as Error).message}，已恢复原状态`,
      }))
      return null
    }
  }

  function changeArchived(task: Task, archived: boolean) {
    setRowErrors((current) => ({ ...current, [task.number]: '' }))
    const operation = archived ? archiveTask : restoreTask
    operation(task.number, task.version)
      .then(replaceTask)
      .catch((cause) => setRowErrors((current) => ({
        ...current,
        [task.number]: `${archived ? '归档' : '恢复'}失败：${(cause as Error).message}`,
      })))
  }

  return {
    filters,
    setFilters,
    labels,
    setLabels,
    tasks,
    loading,
    loadingMore,
    error,
    rowErrors,
    hasMore,
    hasActiveFilters,
    grouped:
      filters.sort === DEFAULT_FILTERS.sort &&
      filters.order === DEFAULT_FILTERS.order,
    reload: () => setReloadToken((token) => token + 1),
    loadMore,
    prependTask: (task) => setTasks((current) => [task, ...current]),
    replaceTask,
    patchOptimistic,
    shiftSchedule,
    archive: (task) => changeArchived(task, true),
    restore: (task) => changeArchived(task, false),
  }
}
