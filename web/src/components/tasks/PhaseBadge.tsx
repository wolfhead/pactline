import { Circle, CircleCheck, CircleDot, CircleX, Eye, Inbox } from 'lucide-react'
import { ACTIVITY_LABELS, PHASE_LABELS, type TaskActivityState, type TaskPhase } from '@/task-types'
import { cn } from '@/lib/utils'

const ICONS = {
  backlog: Inbox,
  ready: Circle,
  in_progress: CircleDot,
  in_review: Eye,
  done: CircleCheck,
  cancelled: CircleX,
} satisfies Record<TaskPhase, typeof Circle>

const COLORS: Record<TaskPhase, string> = {
  backlog: 'text-fg-muted',
  ready: 'text-status-todo',
  in_progress: 'text-status-in-progress',
  in_review: 'text-status-in-review',
  done: 'text-status-done',
  cancelled: 'text-status-cancelled',
}

export default function PhaseBadge({
  phase,
  activity,
  compact = false,
}: {
  phase: TaskPhase
  activity?: TaskActivityState | null
  compact?: boolean
}) {
  const Icon = ICONS[phase]
  const label = activity ? `${PHASE_LABELS[phase]} · ${ACTIVITY_LABELS[activity]}` : PHASE_LABELS[phase]
  return (
    <span
      role="status"
      aria-label={label}
      title={label}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-md px-2 text-xs font-medium',
        COLORS[phase],
        activity === 'needs_resolution' && 'bg-status-in-progress/10',
        compact && 'w-8 justify-center px-0',
      )}
    >
      <Icon className="size-4 shrink-0" aria-hidden="true" />
      {!compact && <span>{label}</span>}
    </span>
  )
}
