import { useState, type ReactNode } from 'react'
import { GanttChart, List } from 'lucide-react'
import type { Tier } from '@/hooks/useBreakpoint'
import type { UserRef } from '@/task-types'
import { cn } from '@/lib/utils'
import FilterBar, { DEFAULT_FILTERS } from './FilterBar'
import GanttView from './GanttView'
import TaskList from './TaskList'
import type { TaskCollectionController } from './useTaskCollection'

export type TaskCollectionViewMode = 'list' | 'gantt'

export default function TaskCollection({
  controller,
  users,
  tier,
  selectedNumber = null,
  allowGantt = true,
  empty = '没有任务。',
  actions,
}: {
  controller: TaskCollectionController
  users: UserRef[]
  tier: Tier
  selectedNumber?: number | null
  allowGantt?: boolean
  empty?: string
  actions?: ReactNode
}) {
  const [view, setView] = useState<TaskCollectionViewMode>('list')
  const blocked = controller.tasks.filter((task) => task.blocked).length

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-surface" data-task-collection>
      <div className="shrink-0 border-b border-border bg-surface px-3 py-2.5">
        <div className="flex flex-wrap items-center gap-2">
          {allowGantt && (
            <div
              role="group"
              aria-label="任务视图"
              className="flex rounded-lg bg-surface-subtle p-0.5"
            >
              <button
                type="button"
                aria-pressed={view === 'list'}
                onClick={() => setView('list')}
                className={cn(
                  'flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium text-fg-muted',
                  view === 'list' && 'bg-surface text-fg shadow-[0_2px_8px_rgb(23_43_61/0.08)]',
                )}
              >
                <List className="size-3.5" aria-hidden="true" />
                列表
              </button>
              <button
                type="button"
                aria-pressed={view === 'gantt'}
                onClick={() => setView('gantt')}
                className={cn(
                  'flex h-7 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium text-fg-muted',
                  view === 'gantt' && 'bg-surface text-fg shadow-[0_2px_8px_rgb(23_43_61/0.08)]',
                )}
              >
                <GanttChart className="size-3.5" aria-hidden="true" />
                甘特
              </button>
            </div>
          )}
          <span className="text-xs text-fg-subtle">
            {controller.tasks.length} 项
            {blocked > 0 && <span className="ml-2 text-secondary">{blocked} 项等待依赖</span>}
          </span>
          {actions && <div className="ml-auto">{actions}</div>}
        </div>
        <FilterBar
          filters={controller.filters}
          onChange={controller.setFilters}
          labels={controller.labels}
          onLabelsChanged={controller.setLabels}
        />
      </div>

      <div data-task-list className="min-h-0 flex-1 overflow-y-auto">
        {controller.loading && <p className="p-4 text-sm text-fg-muted">正在加载任务…</p>}
        {!controller.loading && controller.error && (
          <p role="alert" className="p-4 text-sm text-danger">
            加载失败：{controller.error}{' '}
            <button type="button" className="text-accent underline" onClick={controller.reload}>
              重试
            </button>
          </p>
        )}
        {!controller.loading &&
          !controller.error &&
          controller.tasks.length === 0 &&
          controller.hasActiveFilters && (
            <p className="p-4 text-sm text-fg-muted">
              没有符合筛选条件的任务 —{' '}
              <button
                type="button"
                className="text-accent underline"
                onClick={() => controller.setFilters(DEFAULT_FILTERS)}
              >
                清除筛选条件
              </button>
            </p>
          )}
        {!controller.loading &&
          !controller.error &&
          controller.tasks.length === 0 &&
          !controller.hasActiveFilters && (
            <p className="p-4 text-sm text-fg-muted">{empty}</p>
          )}
        {!controller.loading && !controller.error && controller.tasks.length > 0 && (
          view === 'gantt' && allowGantt ? (
            <GanttView
              controller={controller}
              tier={tier}
              selectedNumber={selectedNumber}
            />
          ) : (
            <>
              <TaskList
                tasks={controller.tasks}
                selectedNumber={selectedNumber}
                tier={tier}
                users={users}
                rowErrors={controller.rowErrors}
                grouped={controller.grouped}
                onPatch={controller.patchOptimistic}
                onArchive={controller.archive}
                onRestore={controller.restore}
              />
              {controller.hasMore && (
                <div className="p-3">
                  <button
                    type="button"
                    className="rounded-md border border-border-strong px-3 py-1.5 text-sm text-fg hover:bg-surface-subtle disabled:cursor-wait disabled:opacity-50"
                    onClick={() => void controller.loadMore()}
                    disabled={controller.loadingMore}
                  >
                    {controller.loadingMore ? '加载中…' : '加载更多'}
                  </button>
                </div>
              )}
            </>
          )
        )}
      </div>
    </div>
  )
}
