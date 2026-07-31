import { Bot, MessageCircleQuestion, Send, Sparkles } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Task } from '@/task-types'

interface AgentWorkIndicatorProps {
  task: Task
}

/**
 * Read-only Agent state. Task status remains the editable lifecycle control;
 * this adjacent mark only explains whether external Agent work is available,
 * running, waiting on the human, or submitted for review.
 */
export default function AgentWorkIndicator({ task }: AgentWorkIndicatorProps) {
  const summary = task.agent_work
  const eligible = task.status === 'todo'
    && task.execution_mode === 'agent_allowed'
    && task.assignee !== null

  if (!summary && !eligible) return null

  let label = '可由 Agent 领取'
  let className = 'text-fg-subtle'
  let icon = <Bot className="size-4" aria-hidden="true" />
  let waiting = false

  if (summary?.status === 'active') {
    label = 'Agent 执行中'
    className = 'text-secondary'
    icon = <Sparkles className="size-4" aria-hidden="true" />
  } else if (summary?.status === 'waiting_human') {
    label = 'Agent 等待你回复'
    className = 'text-status-in-progress'
    icon = <MessageCircleQuestion className="size-4" aria-hidden="true" />
    waiting = true
  } else if (summary?.status === 'submitted') {
    label = 'Agent 已提交，等待验收'
    className = 'text-status-in-review'
    icon = <Send className="size-4" aria-hidden="true" />
  }

  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={cn(
        'relative inline-flex size-8 shrink-0 items-center justify-center rounded-md',
        className,
      )}
    >
      {waiting && (
        <span
          data-agent-attention
          className="agent-attention-breathe absolute size-6 rounded-full border border-current"
          aria-hidden="true"
        />
      )}
      {icon}
    </span>
  )
}
