import { useMemo, useState } from 'react'
import { Plus } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import TaskCollection from '@/components/tasks/TaskCollection'
import TaskDetail from '@/components/tasks/TaskDetail'
import { useTaskCollection } from '@/components/tasks/useTaskCollection'
import { useTaskComposer } from '@/components/tasks/TaskComposer'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { useBreakpoint } from '@/hooks/useBreakpoint'
import { useIdentity } from '@/identity'

export default function TaskListPage() {
  const { me, users, isReadOnly } = useIdentity()
  const { openTaskComposer } = useTaskComposer()
  const navigate = useNavigate()
  const tier = useBreakpoint()
  const { number } = useParams()
  const [ownershipView, setOwnershipView] = useState<'assigned' | 'created'>('assigned')

  const parsed = number === undefined ? NaN : Number(number)
  const selected = Number.isInteger(parsed) ? parsed : null
  const baseQuery = useMemo(() => (
    ownershipView === 'assigned'
      ? { assignee: me?.id }
      : { creator_id: me?.id }
  ), [me?.id, ownershipView])
  const collection = useTaskCollection(baseQuery, me?.id)
  const selectedTask = selected === null
    ? null
    : (collection.tasks.find((task) => task.number === selected) ?? null)

  function closeDetail() {
    navigate('/tasks')
  }

  const detailPane = selected !== null && (
    <TaskDetail
      number={selected}
      users={users}
      syncedTask={selectedTask}
      onPatched={collection.replaceTask}
      onClose={closeDetail}
    />
  )

  const detailReplacesList = tier === 'phone' && selected !== null
  const detailIsSheet = (tier === 'md' || tier === 'lg') && selected !== null
  if (detailReplacesList) {
    return <div className="h-full overflow-y-auto">{detailPane}</div>
  }

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
          selectedNumber={selected}
          empty="没有任务。使用“新建任务”补充背景和期望结果后创建。"
        />
      </div>

      {tier === 'xl' && selected !== null && (
        <aside
          aria-label="任务详情"
          className="w-[clamp(28rem,36vw,36rem)] shrink-0 overflow-y-auto border-l border-border bg-surface shadow-[-8px_0_24px_rgb(23_43_61/0.04)]"
        >
          {detailPane}
        </aside>
      )}

      <Sheet
        open={detailIsSheet}
        onOpenChange={(open) => {
          if (!open) closeDetail()
        }}
      >
        <SheetContent
          className="w-full overflow-y-auto p-0 outline-none sm:max-w-md"
          showCloseButton={false}
          aria-describedby={undefined}
        >
          <SheetTitle className="sr-only">任务详情</SheetTitle>
          {detailPane}
        </SheetContent>
      </Sheet>
    </div>
  )
}
