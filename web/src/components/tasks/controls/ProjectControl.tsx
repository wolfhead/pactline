import { useEffect, useState } from 'react'
import { getProject, type Milestone } from '@/api/projects'
import type { TaskMilestoneRef, TaskProjectRef } from '@/task-types'

interface ProjectControlProps {
  project: TaskProjectRef
  milestone: TaskMilestoneRef | null
  /** @deprecated A Task's Project is immutable after creation. */
  onProjectChange?: (project: TaskProjectRef) => void
  onMilestoneChange: (milestone: TaskMilestoneRef | null) => void
}

export default function ProjectControl({
  project,
  milestone,
  onMilestoneChange,
}: ProjectControlProps) {
  const [milestones, setMilestones] = useState<Milestone[]>([])

  useEffect(() => {
    let cancelled = false
    getProject(project.number)
      .then((detail) => {
        if (!cancelled) setMilestones(detail.milestones)
      })
      .catch(() => {
        if (!cancelled) setMilestones([])
      })
    return () => {
      cancelled = true
    }
  }, [project])

  return (
    <>
      <div className="contents">
        <span className="text-sm text-fg-muted">项目</span>
        <div className="grid min-w-0 max-w-full grid-cols-[1rem_minmax(0,1fr)] items-center gap-1.5">
          <span aria-hidden="true" />
          <span
            id="task-project"
            className="min-w-0 px-2 py-1.5 text-sm text-fg [overflow-wrap:anywhere]"
            title="任务创建后不能移到其他项目"
          >
            #{project.number} {project.name}
          </span>
        </div>
      </div>
      <div className="contents">
        <label htmlFor="task-milestone" className="text-sm text-fg-muted">里程碑</label>
        <div className="grid min-w-0 max-w-full grid-cols-[1rem_minmax(0,1fr)] items-center gap-1.5">
          <span aria-hidden="true" />
          <select
            id="task-milestone"
            value={milestone?.id ?? ''}
            onChange={(event) => {
              const selected = milestones.find((item) => item.id === event.target.value)
              onMilestoneChange(selected ? { id: selected.id, name: selected.name } : null)
            }}
            className="min-w-0 rounded-md border border-transparent bg-transparent px-2 py-1.5 text-sm text-fg outline-none transition-colors hover:bg-surface-subtle focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/30"
          >
            <option value="">未安排</option>
            {milestones.map((item) => (
              <option key={item.id} value={item.id}>{item.name}</option>
            ))}
          </select>
        </div>
      </div>
    </>
  )
}
