import { useEffect, useState } from 'react'
import { getProject, listProjects, type Milestone, type Project } from '@/api/projects'
import type { TaskMilestoneRef, TaskProjectRef } from '@/task-types'

interface ProjectControlProps {
  project: TaskProjectRef | null
  milestone: TaskMilestoneRef | null
  onProjectChange: (project: TaskProjectRef | null) => void
  onMilestoneChange: (milestone: TaskMilestoneRef | null) => void
}

export default function ProjectControl({
  project,
  milestone,
  onProjectChange,
  onMilestoneChange,
}: ProjectControlProps) {
  const [projects, setProjects] = useState<Project[]>([])
  const [milestones, setMilestones] = useState<Milestone[]>([])

  useEffect(() => {
    let cancelled = false
    listProjects()
      .then((items) => {
        if (!cancelled) setProjects(items)
      })
      .catch(() => {
        if (!cancelled) setProjects([])
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!project) {
      setMilestones([])
      return
    }
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
        <label htmlFor="task-project" className="text-sm text-fg-muted">项目</label>
        <div className="grid max-w-64 grid-cols-[1rem_minmax(0,1fr)] items-center gap-1.5">
          <span aria-hidden="true" />
          <select
            id="task-project"
            value={project?.number ?? ''}
            onChange={(event) => {
              const number = Number(event.target.value)
              const selected = projects.find((item) => item.number === number)
              onProjectChange(
                selected
                  ? { id: selected.id, number: selected.number, name: selected.name }
                  : null,
              )
            }}
            className="min-w-0 rounded-md border border-transparent bg-transparent px-2 py-1.5 text-sm text-fg outline-none transition-colors hover:bg-surface-subtle focus-visible:border-accent focus-visible:ring-[3px] focus-visible:ring-accent/30"
          >
            <option value="">无项目</option>
            {projects
              .filter((item) =>
                item.status === 'planned'
                || item.status === 'active'
                || item.status === 'paused'
                || item.number === project?.number,
              )
              .map((item) => (
              <option key={item.id} value={item.number}>#{item.number} {item.name}</option>
              ))}
          </select>
        </div>
      </div>
      {project && (
        <div className="contents">
          <label htmlFor="task-milestone" className="text-sm text-fg-muted">里程碑</label>
          <div className="grid max-w-64 grid-cols-[1rem_minmax(0,1fr)] items-center gap-1.5">
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
      )}
    </>
  )
}
