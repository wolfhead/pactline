import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import FilterBar, { DEFAULT_FILTERS, type FilterBarHandle, type TaskFilters } from '../../components/tasks/FilterBar'
import InlineCreateRow, { type InlineCreateRowHandle } from '../../components/tasks/InlineCreateRow'
import TaskRow from '../../components/tasks/TaskRow'
import { listLabels, listTasks, updateTask } from '../../api/tasks'
import { useIdentity } from '../../identity'
import { isTypingTarget } from '../../keyboard'
import type { Label, Task, TaskPatchBody, TaskPriority, TaskStatus } from '../../task-types'

const PAGE_SIZE = 50

/**
 * The default view: an always-ready capture row, filters that combine
 * freely, and rows whose status/priority/assignee commit the instant they
 * change (optimistically — see patchOptimistic below) rather than opening
 * anything.
 */
export default function TaskListPage() {
  const { me, users } = useIdentity()
  const navigate = useNavigate()
  const location = useLocation()

  const [filters, setFilters] = useState<TaskFilters>(DEFAULT_FILTERS)
  const [labels, setLabels] = useState<Label[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [rowErrors, setRowErrors] = useState<Record<number, string>>({})
  const [focusedIndex, setFocusedIndex] = useState(-1)
  const [reloadToken, setReloadToken] = useState(0)

  const createRef = useRef<InlineCreateRowHandle>(null)
  const filterBarRef = useRef<FilterBarHandle>(null)

  // Labels load once per identity — shared by the filter dropdown and the
  // inline label manager folded into the filter bar.
  useEffect(() => {
    let cancelled = false
    listLabels()
      .then((loaded) => {
        if (cancelled) return
        setLabels(loaded)
      })
      .catch(() => {
        // A failed label list only empties the filter/manager; it does not
        // block the task list itself, which has its own error state below.
      })
    return () => {
      cancelled = true
    }
  }, [me?.id])

  const queryKey = useMemo(() => JSON.stringify(filters), [filters])

  // Whether any filter/search narrows the list — sort/order never do, so
  // they're excluded. Distinguishes "genuinely no tasks yet" from "filters
  // narrowed a non-empty list down to zero": the latter needs to say a
  // filter is the reason and offer to clear it, since a task created via
  // "c" while filtered out would otherwise seem to vanish.
  const hasActiveFilters =
    filters.statuses.length > 0 ||
    filters.priorities.length > 0 ||
    filters.assignee !== '' ||
    filters.labelId !== '' ||
    filters.search !== ''

  // Fetches whenever a filter changes, a reload is requested, or identity
  // changes — mirrors the cancelled-flag idiom in identity.tsx / WorkFeed.tsx
  // / Board.tsx: switching identity (or any filter) must replace an
  // already-mounted list's rows, and a slow stale response must never
  // overwrite a newer one's result.
  useEffect(() => {
    setLoading(true)
    setError('')
    let cancelled = false
    listTasks({
      status: filters.statuses,
      priority: filters.priorities,
      assignee: filters.assignee || undefined,
      label: filters.labelId ? [filters.labelId] : undefined,
      q: filters.search || undefined,
      sort: filters.sort,
      order: filters.order,
      limit: PAGE_SIZE,
    })
      .then((res) => {
        if (cancelled) return
        setTasks(res.items)
        setNextCursor(res.next_cursor)
        setHasMore(res.has_more)
        setFocusedIndex(-1)
      })
      .catch((err) => {
        if (cancelled) return
        setError(String((err as Error).message))
      })
      .finally(() => {
        if (cancelled) return
        setLoading(false)
      })
    return () => {
      cancelled = true
    }
    // queryKey is filters serialized; listing filters' fields individually
    // here would fetch on identity object churn instead of value changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queryKey, me?.id, reloadToken])

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    setLoadingMore(true)
    try {
      const res = await listTasks({
        status: filters.statuses,
        priority: filters.priorities,
        assignee: filters.assignee || undefined,
        label: filters.labelId ? [filters.labelId] : undefined,
        q: filters.search || undefined,
        sort: filters.sort,
        order: filters.order,
        cursor: nextCursor,
        limit: PAGE_SIZE,
      })
      setTasks((ts) => [...ts, ...res.items])
      setNextCursor(res.next_cursor)
      setHasMore(res.has_more)
    } catch (err) {
      setError(String((err as Error).message))
    } finally {
      setLoadingMore(false)
    }
  }

  function handleCreated(task: Task) {
    setTasks((ts) => [task, ...ts])
  }

  // Mutations feel instant: apply the change to the row immediately, send
  // the patch, and reconcile with whatever the server actually persisted.
  // If the server refuses, the row snaps back to its previous value and
  // says why — never a bare failed request with no visible consequence.
  function patchOptimistic(task: Task, patch: TaskPatchBody, optimistic: Partial<Task>) {
    const previous: Partial<Task> = {}
    for (const key of Object.keys(optimistic) as (keyof Task)[]) {
      previous[key] = task[key] as never
    }
    setTasks((ts) => ts.map((t) => (t.number === task.number ? { ...t, ...optimistic } : t)))
    setRowErrors((e) => ({ ...e, [task.number]: '' }))
    updateTask(task.number, patch)
      .then((updated) => {
        setTasks((ts) => ts.map((t) => (t.number === task.number ? updated : t)))
      })
      .catch((err) => {
        setTasks((ts) => ts.map((t) => (t.number === task.number ? { ...t, ...previous } : t)))
        setRowErrors((e) => ({ ...e, [task.number]: `更新失败：${(err as Error).message}，已恢复原状态` }))
      })
  }

  function handleChangeStatus(task: Task, status: TaskStatus) {
    patchOptimistic(task, { status }, { status })
  }

  function handleChangePriority(task: Task, priority: TaskPriority) {
    patchOptimistic(task, { priority }, { priority })
  }

  function handleChangeAssignee(task: Task, assigneeId: string | null) {
    const assignee = assigneeId ? users.find((u) => u.id === assigneeId) ?? null : null
    patchOptimistic(task, { assignee_id: assigneeId }, { assignee })
  }

  // Jumped here from the board's "c" shortcut (board has no capture row of
  // its own) with a request to focus the create input once mounted.
  useEffect(() => {
    const state = location.state as { focusCreate?: boolean } | null
    if (state?.focusCreate) {
      createRef.current?.focus()
      navigate(location.pathname, { replace: true, state: {} })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Keyboard-first navigation: c creates, / searches, j/k or the arrow keys
  // move the selection, Enter opens it, Escape clears the selection. None
  // of these fire while the user is typing into a field.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'c' && !isTypingTarget(e.target)) {
        e.preventDefault()
        createRef.current?.focus()
        return
      }
      if (e.key === '/' && !isTypingTarget(e.target)) {
        e.preventDefault()
        filterBarRef.current?.focusSearch()
        return
      }
      if (isTypingTarget(e.target)) return

      if (e.key === 'j' || e.key === 'ArrowDown') {
        e.preventDefault()
        setFocusedIndex((i) => Math.min(i + 1, tasks.length - 1))
      } else if (e.key === 'k' || e.key === 'ArrowUp') {
        e.preventDefault()
        setFocusedIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'Enter' && focusedIndex >= 0 && tasks[focusedIndex]) {
        navigate(`/tasks/${tasks[focusedIndex].number}`)
      } else if (e.key === 'Escape') {
        setFocusedIndex(-1)
        ;(document.activeElement as HTMLElement | null)?.blur()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [tasks, navigate, focusedIndex])

  return (
    <section>
      <h2>任务列表</h2>
      <InlineCreateRow ref={createRef} onCreated={handleCreated} />
      <FilterBar ref={filterBarRef} filters={filters} onChange={setFilters} labels={labels} onLabelsChanged={setLabels} />

      {loading && <p className="hint">正在加载任务…</p>}
      {!loading && error && (
        <p className="error">
          加载失败：{error}{' '}
          <button type="button" onClick={() => setReloadToken((t) => t + 1)}>重试</button>
        </p>
      )}
      {!loading && !error && tasks.length === 0 && hasActiveFilters && (
        <p className="hint">
          没有符合筛选条件的任务 —{' '}
          <button type="button" onClick={() => setFilters(DEFAULT_FILTERS)}>清除筛选条件</button>
        </p>
      )}
      {!loading && !error && tasks.length === 0 && !hasActiveFilters && (
        <p className="hint">没有任务 — 按 C 创建一个吧</p>
      )}
      {!loading && !error && tasks.length > 0 && (
        <div className="task-list" role="list">
          {tasks.map((t, i) => (
            <TaskRow
              key={t.id}
              task={t}
              focused={i === focusedIndex}
              error={rowErrors[t.number]}
              onChangeStatus={handleChangeStatus}
              onChangePriority={handleChangePriority}
              onChangeAssignee={handleChangeAssignee}
            />
          ))}
          {hasMore && (
            <button type="button" className="load-more" onClick={loadMore} disabled={loadingMore}>
              {loadingMore ? '加载中…' : '加载更多'}
            </button>
          )}
        </div>
      )}
    </section>
  )
}
