import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import FilterBar, { DEFAULT_FILTERS, type TaskFilters } from '@/components/tasks/FilterBar'
import InlineCreate, { type InlineCreateHandle } from '@/components/tasks/InlineCreate'
import TaskDetail from '@/components/tasks/TaskDetail'
import TaskList from '@/components/tasks/TaskList'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { archiveTask, getTask, listLabels, listTasks, restoreTask, updateTask } from '@/api/tasks'
import { ProblemError } from '@/api/v1/client'
import { useIdentity } from '@/identity'
import type { Label, Task, TaskPatchBody } from '@/task-types'

const PAGE_SIZE = 50

/**
 * `/tasks` and `/tasks/:number` are the same page in two states, not two
 * pages. The number in the URL selects a row; it never unmounts the list.
 * That is what keeps scroll position and filters across opening a task, and
 * it is why the detail lives here rather than behind its own route element.
 *
 * One consequence the three no-regression tests pin down: `tasks` lives
 * here, above both columns, so a change committed from the detail lands in
 * the list by way of `handlePatched` — never by re-fetching the list.
 */
export default function TaskListPage() {
  const { me, users } = useIdentity()
  const navigate = useNavigate()
  const location = useLocation()
  const tier = useBreakpoint()
  const { number } = useParams()

  // An unparseable :number (e.g. /tasks/abc) is treated as "nothing
  // selected" rather than handed to TaskDetail, which would otherwise fetch
  // /api/v1/tasks/NaN.
  const parsed = number === undefined ? NaN : Number(number)
  const selected = Number.isInteger(parsed) ? parsed : null

  const [filters, setFilters] = useState<TaskFilters>(DEFAULT_FILTERS)
  const [labels, setLabels] = useState<Label[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [rowErrors, setRowErrors] = useState<Record<number, string>>({})
  const [reloadToken, setReloadToken] = useState(0)
  const [ownershipView, setOwnershipView] = useState<'assigned' | 'created'>('assigned')

  // Held so both the "＋ 新建任务" filter-bar button and the focusCreate
  // navigation state (below) can jump focus into the capture row without it
  // ever opening a dialog of its own.
  const createRef = useRef<InlineCreateHandle>(null)

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

  const queryKey = useMemo(
    () => JSON.stringify({ filters, ownershipView }),
    [filters, ownershipView],
  )

  // Whether any filter/search narrows the list — sort/order never do, so
  // they're excluded. Distinguishes "genuinely no tasks yet" from "filters
  // narrowed a non-empty list down to zero": the latter needs to say a
  // filter is the reason and offer to clear it, since a task created in the
  // capture row while filtered out would otherwise seem to vanish.
  const hasActiveFilters =
    filters.statuses.length > 0 ||
    filters.priorities.length > 0 ||
    filters.assignee !== '' ||
    filters.labelId !== '' ||
    filters.search !== ''

  // Fetches whenever a filter changes, a reload is requested, or identity
  // changes — mirrors the cancelled-flag idiom in identity.tsx / WorkFeed.tsx
  // / WorkFeed.tsx: switching identity (or any filter) must replace an
  // already-mounted list's rows, and a slow stale response must never
  // overwrite a newer one's result.
  //
  // `selected` is deliberately NOT a dependency. Opening or closing a task
  // must not re-fetch the list — that is the difference between three
  // columns and two pages wearing a trench coat.
  useEffect(() => {
    setLoading(true)
    setError('')
    let cancelled = false
    listTasks({
      ...(ownershipView === 'assigned'
        ? { assignee: me?.id }
        : { creator_id: me?.id }),
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

  // Which query the rows on screen belong to. loadMore is fired from a click
  // handler, so unlike the effect above it has no cleanup to flip a
  // `cancelled` flag — yet it appends to the same `tasks` array. Change a
  // filter while a page-2 request is in flight and its rows, fetched under
  // the OLD filter, land in the NEW filter's list with nothing to correct
  // them until the next filter change. Hence a ref holding the live key,
  // read again after the await.
  const listKeyRef = useRef('')
  listKeyRef.current = `${queryKey}|${me?.id ?? ''}|${reloadToken}`

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    const forKey = listKeyRef.current
    setLoadingMore(true)
    try {
      const res = await listTasks({
        ...(ownershipView === 'assigned'
          ? { assignee: me?.id }
          : { creator_id: me?.id }),
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
      if (listKeyRef.current !== forKey) return
      setTasks((ts) => [...ts, ...res.items])
      setNextCursor(res.next_cursor)
      setHasMore(res.has_more)
    } catch (err) {
      if (listKeyRef.current !== forKey) return
      setError(String((err as Error).message))
    } finally {
      setLoadingMore(false)
    }
  }

  function handleCreated(task: Task) {
    setTasks((ts) => [task, ...ts])
  }

  // The single seam between the two columns. TaskDetail hands back whatever
  // the server actually persisted; folding it in here is what makes the row
  // update without the list issuing a request of its own. A task that isn't
  // in the current page/filter is left alone rather than spliced in — it
  // would be a row the active filters say should not be there.
  function handlePatched(updated: Task) {
    setTasks((ts) => ts.map((t) => (t.number === updated.number ? updated : t)))
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
    updateTask(task.number, task.version, patch)
      .then((updated) => {
        setTasks((ts) => ts.map((t) => (t.number === task.number ? updated : t)))
      })
      .catch(async (err) => {
        if (err instanceof ProblemError && err.code === 'VERSION_CONFLICT') {
          try {
            const latest = await getTask(task.number)
            setTasks((ts) => ts.map((t) => (t.number === task.number ? latest : t)))
            setRowErrors((errors) => ({
              ...errors,
              [task.number]: '内容已被其他用户或 Agent 更新，已加载最新版本。',
            }))
            return
          } catch {
            // Fall through to the ordinary rollback when refresh also fails.
          }
        }
        setTasks((ts) => ts.map((t) => (t.number === task.number ? { ...t, ...previous } : t)))
        setRowErrors((e) => ({ ...e, [task.number]: `更新失败：${(err as Error).message}，已恢复原状态` }))
      })
  }

  // Not optimistic like patchOptimistic above: archiving/restoring changes
  // the task's lifecycle, not one of its properties, so the row just waits
  // for the server's answer and reports a row error if it refuses.
  function handleArchive(task: Task) {
    archiveTask(task.number, task.version)
      .then((updated) => setTasks((ts) => ts.map((t) => (t.number === task.number ? updated : t))))
      .catch((err) => setRowErrors((e) => ({ ...e, [task.number]: `归档失败：${(err as Error).message}` })))
  }

  function handleRestore(task: Task) {
    restoreTask(task.number, task.version)
      .then((updated) => setTasks((ts) => ts.map((t) => (t.number === task.number ? updated : t))))
      .catch((err) => setRowErrors((e) => ({ ...e, [task.number]: `恢复失败：${(err as Error).message}` })))
  }

  // Arrived here with a request to focus the capture row — the phone bottom
  // bar's 新建 tab navigates this way, since it has no capture row of its
  // own. Keyed on `location.state`, not `[]`: a phone visiting `/tasks/143`
  // keeps this same TaskListPage instance mounted (the URL's :number just
  // selects a row, per the module doc above), so tapping 新建 from there
  // changes location without ever remounting the component. An
  // on-mount-only effect would fire once for the detail view and never
  // again — the capture row would silently stay unfocused every time after
  // the first cold load.
  useEffect(() => {
    const state = location.state as { focusCreate?: boolean } | null
    if (state?.focusCreate) {
      createRef.current?.focus()
      navigate(location.pathname, { replace: true, state: {} })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [location.state])

  function closeDetail() {
    navigate('/tasks')
  }

  // Status grouping only holds under the default creation-order sort; any
  // other sort — or the same field in the non-default direction — interleaves
  // statuses, so a status heading would lie about what sits under it.
  const grouped = filters.sort === DEFAULT_FILTERS.sort && filters.order === DEFAULT_FILTERS.order

  // A phone has room for one thing at a time: an open task replaces the
  // list outright rather than sliding over it. Every other tier keeps the
  // list mounted — as a sibling column at xl, underneath a Sheet at lg/md.
  const detailReplacesList = tier === 'phone' && selected !== null
  const detailIsSheet = (tier === 'md' || tier === 'lg') && selected !== null

  // The page's own copy of the open task, handed down so the seam between
  // the two views runs both ways: `onPatched` carries detail → list,
  // `syncedTask` carries list → detail. At xl the selected row is right
  // beside the open detail with every control live, so a status changed
  // from the row has to move the detail too — otherwise the two panes show
  // different values for the same task, side by side, which is the exact
  // divergence this page exists to prevent. `undefined` when the task isn't
  // in the current page/filter: TaskDetail then simply has nothing to sync
  // against and keeps its own fetched copy.
  const selectedTask = selected === null ? null : (tasks.find((t) => t.number === selected) ?? null)

  const detailPane = selected !== null && (
    <TaskDetail
      number={selected}
      users={users}
      syncedTask={selectedTask}
      onPatched={handlePatched}
      onClose={closeDetail}
    />
  )

  if (detailReplacesList) {
    return <div className="h-full overflow-y-auto">{detailPane}</div>
  }

  return (
    <div className="flex h-full min-h-0">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="shrink-0 border-b border-border bg-surface px-3 py-2">
          {/* mb-2 is stated here because nothing else supplies it: this
              wrapper declares no gap, and preflight zeroes heading margins.
              Without it the heading sits flush on top of the capture row. */}
          <div className="mb-2 flex items-center justify-between gap-3">
            <h2 className="text-sm font-semibold text-fg">我的工作</h2>
            <div className="flex rounded-md bg-surface-subtle p-0.5 text-xs">
              <button
                type="button"
                onClick={() => setOwnershipView('assigned')}
                className={ownershipView === 'assigned' ? 'rounded bg-surface px-2 py-1 font-medium shadow-sm' : 'px-2 py-1 text-fg-muted'}
              >
                分配给我
              </button>
              <button
                type="button"
                onClick={() => setOwnershipView('created')}
                className={ownershipView === 'created' ? 'rounded bg-surface px-2 py-1 font-medium shadow-sm' : 'px-2 py-1 text-fg-muted'}
              >
                我创建的
              </button>
            </div>
          </div>
          <InlineCreate ref={createRef} onCreated={handleCreated} />
          <FilterBar
            filters={filters}
            onChange={setFilters}
            labels={labels}
            onLabelsChanged={setLabels}
            onRequestCreate={() => createRef.current?.focus()}
          />
        </div>

        {/* data-task-list marks the one element that actually scrolls the
            rows. Opening a task must not disturb its scrollTop — the list
            never unmounts — and e2e/21 reads that offset off this attribute
            rather than guessing which div in the tree owns the overflow. */}
        <div data-task-list className="min-h-0 flex-1 overflow-y-auto bg-surface">
          {loading && <p className="p-3 text-sm text-fg-muted">正在加载任务…</p>}
          {!loading && error && (
            <p className="p-3 text-sm text-danger">
              加载失败：{error}{' '}
              <button type="button" className="text-accent underline" onClick={() => setReloadToken((t) => t + 1)}>
                重试
              </button>
            </p>
          )}
          {!loading && !error && tasks.length === 0 && hasActiveFilters && (
            <p className="p-3 text-sm text-fg-muted">
              没有符合筛选条件的任务 —{' '}
              <button type="button" className="text-accent underline" onClick={() => setFilters(DEFAULT_FILTERS)}>
                清除筛选条件
              </button>
            </p>
          )}
          {!loading && !error && tasks.length === 0 && !hasActiveFilters && (
            <p className="p-3 text-sm text-fg-muted">没有任务 — 在上面输入标题就能创建一个</p>
          )}
          {!loading && !error && tasks.length > 0 && (
            <>
              <TaskList
                tasks={tasks}
                selectedNumber={selected}
                tier={tier}
                users={users}
                rowErrors={rowErrors}
                grouped={grouped}
                onPatch={patchOptimistic}
                onArchive={handleArchive}
                onRestore={handleRestore}
              />
              {hasMore && (
                <div className="p-3">
                  <button
                    type="button"
                    className="rounded-md border border-border px-3 py-1.5 text-sm text-fg hover:bg-surface-subtle"
                    onClick={loadMore}
                    disabled={loadingMore}
                  >
                    {loadingMore ? '加载中…' : '加载更多'}
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {/* Desktop keeps the list at full width until a task is selected.
          The detail then becomes a deliberately bounded third column:
          wide enough for acceptance and activity content without taking
          the half-screen footprint of the earlier permanent pane. */}
      {tier === 'xl' && selected !== null && (
        <aside
          aria-label="任务详情"
          className="w-[clamp(28rem,36vw,36rem)] shrink-0 overflow-y-auto border-l border-border bg-surface shadow-[-8px_0_24px_rgb(23_43_61/0.04)]"
        >
          {detailPane}
        </aside>
      )}

      {/* lg/md: the list keeps its full width underneath and stays mounted;
          the detail slides over part of it. Radix's own close button is
          suppressed because TaskDetail already renders one from `onClose`,
          and two stacked ✕ in the same corner is just a bug you can see. */}
      <Sheet
        open={detailIsSheet}
        onOpenChange={(open) => {
          if (!open) closeDetail()
        }}
      >
        <SheetContent
          // outline-none: Radix moves focus to this panel when it opens
          // (tabindex="-1", never reached by Tab). A focus ring drawn around
          // the whole sheet reads as decoration, not as a focus cue, so it
          // is suppressed here exactly as shadcn's own dialog content does.
          // Every control inside the sheet keeps its own ring.
          className="w-full overflow-y-auto p-0 outline-none sm:max-w-md"
          showCloseButton={false}
          aria-describedby={undefined}
        >
          {/* Radix requires an accessible name on dialog content; TaskDetail
              renders no shell of its own, so the name belongs to the shell —
              here. */}
          <SheetTitle className="sr-only">任务详情</SheetTitle>
          {detailPane}
        </SheetContent>
      </Sheet>
    </div>
  )
}
