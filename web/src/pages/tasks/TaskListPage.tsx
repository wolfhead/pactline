import { useMemo, useState } from 'react'
import { Plus } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import TaskCollection from '@/components/tasks/TaskCollection'
import { useTaskCollection } from '@/components/tasks/useTaskCollection'
import {
  searchParamsWithTaskFilters,
  searchParamsWithTaskPageCount,
  taskFiltersFromSearchParams,
  taskPageCountFromSearchParams,
} from '@/components/tasks/task-collection-search'
import { useTaskComposer } from '@/components/tasks/TaskComposer'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { useIdentity } from '@/identity'

export default function TaskListPage() {
  const { me, users, isReadOnly } = useIdentity()
  const { openTaskComposer } = useTaskComposer()
  const tier = useBreakpoint()
  const [searchParams, setSearchParams] = useSearchParams()
  const [ownershipView, setOwnershipViewState] = useState<'assigned' | 'created'>(
    searchParams.get('ownership') === 'created' ? 'created' : 'assigned',
  )

  function setOwnershipView(next: 'assigned' | 'created') {
    setOwnershipViewState(next)
    const updated = new URLSearchParams(searchParams)
    if (next === 'created') updated.set('ownership', next)
    else updated.delete('ownership')
    setSearchParams(updated, { replace: true })
  }
  const baseQuery = useMemo(() => (
    ownershipView === 'assigned'
      ? { assignee: me?.id }
      : { creator_id: me?.id }
  ), [me?.id, ownershipView])
  const initialFilters = useMemo(() => taskFiltersFromSearchParams(searchParams), []) // eslint-disable-line react-hooks/exhaustive-deps
  const initialPageCount = useMemo(() => taskPageCountFromSearchParams(searchParams), []) // eslint-disable-line react-hooks/exhaustive-deps
  const collection = useTaskCollection(baseQuery, me?.id, {
    initialFilters,
    initialPageCount,
    onFiltersChange: (filters) => {
      setSearchParams(searchParamsWithTaskFilters(searchParams, filters), { replace: true })
    },
    onPageCountChange: (pageCount) => {
      setSearchParams(searchParamsWithTaskPageCount(searchParams, pageCount), { replace: true })
    },
  })
  return (
    <div className="flex h-full min-h-0">
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border bg-surface px-3 py-2.5">
          <div>
            <h1 className="text-sm font-semibold text-fg">我的工作</h1>
            <p className="mt-0.5 text-xs text-fg-subtle">先处理等待你的工作，再检查自己发起的任务。</p>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <div className="flex rounded-lg bg-surface-subtle p-0.5 text-xs">
              <button
                type="button"
                onClick={() => setOwnershipView('assigned')}
                className={ownershipView === 'assigned'
                  ? 'rounded-md bg-surface px-2.5 py-1 font-medium text-fg shadow-[0_2px_8px_rgb(23_43_61/0.08)]'
                  : 'px-2.5 py-1 text-fg-muted'}
              >
                分配给我
              </button>
              <button
                type="button"
                onClick={() => setOwnershipView('created')}
                className={ownershipView === 'created'
                  ? 'rounded-md bg-surface px-2.5 py-1 font-medium text-fg shadow-[0_2px_8px_rgb(23_43_61/0.08)]'
                  : 'px-2.5 py-1 text-fg-muted'}
              >
                我创建的
              </button>
            </div>
            {!isReadOnly && (
              <button
                type="button"
                onClick={() => openTaskComposer({
                  onCreated: (task) => {
                    setOwnershipView('created')
                    collection.prependTask(task)
                  },
                })}
                className="flex items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-accent-fg shadow-[0_4px_12px_rgb(37_99_235/0.18)]"
              >
                <Plus className="size-3.5" aria-hidden="true" />
                新建任务
              </button>
            )}
          </div>
        </header>

        <TaskCollection
          controller={collection}
          users={users}
          tier={tier}
          empty="没有任务。使用“新建任务”补充背景和期望结果后创建。"
        />
      </div>
    </div>
  )
}
