import type { Task, TaskPhase } from '@/task-types'

export type MilestoneProgressPhase = 'done' | 'in_progress' | 'in_review' | 'waiting'

export interface MilestoneProgressSummary {
  eligible: number
  done: number
  inProgress: number
  inReview: number
  waiting: number
  completionPercentage: number
}

const PROGRESS_PHASES: Array<{
  phase: MilestoneProgressPhase
  count: (summary: MilestoneProgressSummary) => number
  label: string
  segmentClassName: string
  markerClassName: string
}> = [
  {
    phase: 'done',
    count: (summary) => summary.done,
    label: '已完成',
    segmentClassName: 'bg-secondary',
    markerClassName: 'bg-secondary',
  },
  {
    phase: 'in_progress',
    count: (summary) => summary.inProgress,
    label: '执行中',
    segmentClassName: 'milestone-progress-in-progress bg-accent',
    markerClassName: 'bg-accent',
  },
  {
    phase: 'in_review',
    count: (summary) => summary.inReview,
    label: '验收中',
    segmentClassName: 'bg-status-in-review',
    markerClassName: 'bg-status-in-review',
  },
  {
    phase: 'waiting',
    count: (summary) => summary.waiting,
    label: '等待中',
    segmentClassName: 'bg-border-strong',
    markerClassName: 'bg-border-strong',
  },
]

function progressPhase(phase: TaskPhase): MilestoneProgressPhase | null {
  switch (phase) {
    case 'done':
      return 'done'
    case 'in_progress':
      return 'in_progress'
    case 'in_review':
      return 'in_review'
    case 'backlog':
    case 'ready':
      return 'waiting'
    case 'cancelled':
      return null
  }
}

export function aggregateMilestoneProgress(
  tasks: ReadonlyArray<Pick<Task, 'archived_at' | 'phase'>>,
): MilestoneProgressSummary {
  const summary: MilestoneProgressSummary = {
    eligible: 0,
    done: 0,
    inProgress: 0,
    inReview: 0,
    waiting: 0,
    completionPercentage: 0,
  }

  for (const task of tasks) {
    if (task.archived_at) continue
    const phase = progressPhase(task.phase)
    if (!phase) continue

    summary.eligible += 1
    if (phase === 'done') summary.done += 1
    if (phase === 'in_progress') summary.inProgress += 1
    if (phase === 'in_review') summary.inReview += 1
    if (phase === 'waiting') summary.waiting += 1
  }

  summary.completionPercentage = summary.eligible === 0
    ? 0
    : Math.round((summary.done / summary.eligible) * 100)
  return summary
}

export function milestoneProgressAccessibleName(summary: MilestoneProgressSummary): string {
  if (summary.eligible === 0) return '任务进度：0 个任务。'
  return [
    `任务进度：共 ${summary.eligible} 个任务`,
    `已完成 ${summary.done} 个，执行中 ${summary.inProgress} 个，验收中 ${summary.inReview} 个，等待中 ${summary.waiting} 个`,
    `完成 ${summary.completionPercentage}%`,
  ].join('；') + '。'
}

export default function MilestoneProgress({
  tasks,
}: {
  tasks: ReadonlyArray<Pick<Task, 'archived_at' | 'phase'>>
}) {
  const summary = aggregateMilestoneProgress(tasks)
  const accessibleName = milestoneProgressAccessibleName(summary)

  return (
    <>
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-fg-muted">
        {summary.eligible === 0 ? (
          <span>0 个任务</span>
        ) : (
          <>
            <span className="font-medium text-fg">
              {summary.done}/{summary.eligible} 完成 · {summary.completionPercentage}%
            </span>
            {PROGRESS_PHASES.map(({ count, label, markerClassName, phase }) => (
              <span key={phase} className="inline-flex items-center gap-1.5">
                <span className={`size-1.5 rounded-full ${markerClassName}`} aria-hidden="true" />
                {label} {count(summary)}
              </span>
            ))}
          </>
        )}
      </div>
      <div
        role="img"
        aria-label={accessibleName}
        className="absolute inset-x-0 bottom-0 flex h-1.5 overflow-hidden bg-surface-subtle"
        data-milestone-progress-track="true"
      >
        {summary.eligible === 0 ? (
          <span className="h-full w-full bg-border" data-progress-phase="empty" />
        ) : PROGRESS_PHASES.map(({ count, phase, segmentClassName }) => {
          const phaseCount = count(summary)
          if (phaseCount === 0) return null
          return (
            <span
              key={phase}
              className={`h-full shrink-0 ${segmentClassName}`}
              data-progress-phase={phase}
              style={{ width: `${(phaseCount / summary.eligible) * 100}%` }}
            />
          )
        })}
      </div>
    </>
  )
}
