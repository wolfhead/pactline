import { ArrowLeft } from 'lucide-react'
import { Link, useLocation, useParams } from 'react-router-dom'
import TaskDetail from '@/components/tasks/TaskDetail'
import { useIdentity } from '@/identity'
import { taskSourceFromState } from '@/components/tasks/task-navigation'

export default function TaskPage() {
  const { number } = useParams()
  const location = useLocation()
  const { users } = useIdentity()
  const parsedNumber = Number(number)
  const taskNumber = Number.isSafeInteger(parsedNumber) && parsedNumber > 0
    ? parsedNumber
    : null
  const taskSource = taskSourceFromState(location.state)

  return (
    <div className="min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto bg-surface">
      <header className="sticky top-0 z-20 border-b border-border bg-surface/95 px-3 py-2.5 backdrop-blur sm:px-5">
        <Link
          to={taskSource}
          data-read-only-allowed="true"
          className="inline-flex min-h-9 items-center gap-2 rounded-md px-2 text-sm font-medium text-fg-muted hover:bg-surface-subtle hover:text-fg"
        >
          <ArrowLeft className="size-4" aria-hidden="true" />
          返回任务集合
        </Link>
      </header>
      <div className="mx-auto w-full min-w-0 max-w-5xl">
        {taskNumber === null ? (
          <section role="alert" className="p-5 text-sm text-danger">
            任务编号无效。
          </section>
        ) : (
          <TaskDetail
            number={taskNumber}
            users={users}
            onPatched={() => {}}
          />
        )}
      </div>
    </div>
  )
}
