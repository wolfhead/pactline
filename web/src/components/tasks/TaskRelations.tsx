import { useState } from 'react'
import { CornerDownRight, GitBranch, Link2, Plus, Unlink } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { Task, TaskPatchBody, TaskRelationRef } from '@/task-types'

function RelationLink({ relation }: { relation: TaskRelationRef }) {
  return (
    <Link
      to={`/tasks/${relation.number}`}
      className="min-w-0 truncate text-sm text-fg hover:text-accent"
      title={relation.title}
    >
      <span className="font-mono text-xs text-fg-subtle">#{relation.number}</span>{' '}
      {relation.title}
    </Link>
  )
}

export default function TaskRelations({
  task,
  onPatch,
}: {
  task: Task
  onPatch: (patch: TaskPatchBody) => void
}) {
  const [parentNumber, setParentNumber] = useState('')
  const [dependencyNumber, setDependencyNumber] = useState('')
  const [localError, setLocalError] = useState('')

  function parsed(value: string): number | null {
    const number = Number(value)
    return Number.isInteger(number) && number > 0 ? number : null
  }

  return (
    <section className="border-t border-border pt-4">
      <div className="mb-3 flex items-center gap-2">
        <GitBranch className="size-4 text-fg-muted" aria-hidden="true" />
        <h3 className="text-sm font-semibold text-fg">任务关系</h3>
      </div>
      {localError && (
        <p role="alert" className="mb-3 text-sm text-danger">
          {localError}
        </p>
      )}

      <div className="grid gap-x-4 gap-y-3 sm:grid-cols-[5rem_minmax(0,1fr)]">
        <span className="text-sm text-fg-muted">父任务</span>
        <div className="min-w-0">
          {task.parent ? (
            <div className="flex items-center gap-2">
              <CornerDownRight className="size-3.5 shrink-0 text-fg-subtle" aria-hidden="true" />
              <RelationLink relation={task.parent} />
              <button
                type="button"
                aria-label="解除父任务"
                onClick={() => onPatch({ parent_number: null })}
                className="ml-auto flex size-7 shrink-0 items-center justify-center rounded-md text-fg-muted hover:bg-danger-subtle hover:text-danger"
              >
                <Unlink className="size-3.5" aria-hidden="true" />
              </button>
            </div>
          ) : (
            <form
              className="flex max-w-xs gap-2"
              onSubmit={(event) => {
                event.preventDefault()
                const number = parsed(parentNumber)
                if (!number) {
                  setLocalError('请输入有效的父任务编号。')
                  return
                }
                if (number === task.number) {
                  setLocalError('任务不能成为自己的父任务。')
                  return
                }
                setLocalError('')
                onPatch({ parent_number: number })
                setParentNumber('')
              }}
            >
              <input
                type="number"
                min={1}
                value={parentNumber}
                onChange={(event) => {
                  setParentNumber(event.target.value)
                  setLocalError('')
                }}
                placeholder="输入任务编号"
                aria-label="父任务编号"
                className="h-8 min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-2 text-sm outline-none focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/30"
              />
              <button
                type="submit"
                className="rounded-md bg-surface-subtle px-2.5 text-xs font-medium text-fg hover:bg-accent-subtle hover:text-accent"
              >
                关联
              </button>
            </form>
          )}
        </div>

        <span className="text-sm text-fg-muted">子任务</span>
        <div className="flex min-w-0 flex-col gap-1.5">
          {task.children.length ? task.children.map((child) => (
            <div key={child.id} className="flex items-center gap-2">
              <CornerDownRight className="size-3.5 shrink-0 text-fg-subtle" aria-hidden="true" />
              <RelationLink relation={child} />
            </div>
          )) : <span className="text-sm text-fg-subtle">无子任务</span>}
        </div>

        <span className="text-sm text-fg-muted">依赖</span>
        <div className="min-w-0">
          <div className="flex flex-col gap-1.5">
            {task.dependencies.map((dependency) => (
              <div key={dependency.id} className="flex items-center gap-2">
                <Link2 className="size-3.5 shrink-0 text-secondary" aria-hidden="true" />
                <RelationLink relation={dependency} />
                <span className="ml-auto text-xs text-fg-subtle">
                  {['done', 'cancelled'].includes(dependency.phase) ? '已解除阻塞' : '等待完成'}
                </span>
                <button
                  type="button"
                  aria-label={`移除依赖任务 #${dependency.number}`}
                  onClick={() => onPatch({
                    dependency_numbers: task.dependencies
                      .filter((candidate) => candidate.number !== dependency.number)
                      .map((candidate) => candidate.number),
                  })}
                  className="flex size-7 shrink-0 items-center justify-center rounded-md text-fg-muted hover:bg-danger-subtle hover:text-danger"
                >
                  <Unlink className="size-3.5" aria-hidden="true" />
                </button>
              </div>
            ))}
          </div>
          <form
            className="mt-2 flex max-w-xs gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              const number = parsed(dependencyNumber)
              if (!number) {
                setLocalError('请输入有效的依赖任务编号。')
                return
              }
              if (number === task.number) {
                setLocalError('任务不能依赖自己。')
                return
              }
              if (task.dependencies.some((dependency) => dependency.number === number)) {
                setLocalError('该依赖任务已经存在。')
                return
              }
              setLocalError('')
              onPatch({
                dependency_numbers: [
                  ...task.dependencies.map((dependency) => dependency.number),
                  number,
                ],
              })
              setDependencyNumber('')
            }}
          >
            <input
              type="number"
              min={1}
              value={dependencyNumber}
              onChange={(event) => {
                setDependencyNumber(event.target.value)
                setLocalError('')
              }}
              placeholder="添加前置任务编号"
              aria-label="依赖任务编号"
              className="h-8 min-w-0 flex-1 rounded-md border border-border-strong bg-surface px-2 text-sm outline-none focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/30"
            />
            <button
              type="submit"
              aria-label="添加依赖任务"
              className="flex size-8 items-center justify-center rounded-md bg-secondary-subtle text-secondary hover:bg-secondary hover:text-white"
            >
              <Plus className="size-3.5" aria-hidden="true" />
            </button>
          </form>
        </div>

        <span className="text-sm text-fg-muted">被依赖</span>
        <div className="flex min-w-0 flex-col gap-1.5">
          {task.dependents.length ? task.dependents.map((dependent) => (
            <div key={dependent.id} className="flex items-center gap-2">
              <Link2 className="size-3.5 shrink-0 text-fg-subtle" aria-hidden="true" />
              <RelationLink relation={dependent} />
            </div>
          )) : <span className="text-sm text-fg-subtle">没有后续任务依赖它</span>}
        </div>
      </div>
    </section>
  )
}
