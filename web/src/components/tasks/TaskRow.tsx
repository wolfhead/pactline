import { Link } from 'react-router-dom'
import { useIdentity } from '../../identity'
import { PRIORITY_LABELS, STATUS_LABELS, TASK_PRIORITIES, TASK_STATUSES, type Task, type TaskPriority, type TaskStatus } from '../../task-types'
import { PriorityMark, StatusMark } from './marks'
import QuietSelect from './QuietSelect'

const UNASSIGNED = ''

interface TaskRowProps {
  task: Task
  focused: boolean
  error?: string
  onChangeStatus: (task: Task, status: TaskStatus) => void
  onChangePriority: (task: Task, priority: TaskPriority) => void
  onChangeAssignee: (task: Task, assigneeId: string | null) => void
}

/** One row: everything needed at a glance (number, title, status, priority,
 * assignee, due date). Status/priority/assignee are quiet displays that
 * become editable on interaction (see QuietSelect) — a colour mark, compact
 * coloured text, and a name respectively — not three permanently-open
 * dropdowns; unassigned and no-priority both read as genuinely empty. */
export default function TaskRow({ task, focused, error, onChangeStatus, onChangePriority, onChangeAssignee }: TaskRowProps) {
  const { users } = useIdentity()

  const assigneeOptions = [UNASSIGNED, ...users.map((u) => u.id)]
  const assigneeLabels: Record<string, string> = { [UNASSIGNED]: '未分配' }
  for (const u of users) assigneeLabels[u.id] = u.name

  return (
    <div className={`task-row ${focused ? 'focused' : ''}`} data-task-number={task.number}>
      <span className="task-number hint">#{task.number}</span>
      <Link className="task-title" to={`/tasks/${task.number}`}>{task.title}</Link>
      <QuietSelect
        value={task.status}
        options={TASK_STATUSES}
        labels={STATUS_LABELS}
        onChange={(s) => onChangeStatus(task, s)}
        ariaLabel={`任务 #${task.number} 状态`}
        renderQuiet={(status) => <StatusMark status={status} />}
      />
      <QuietSelect
        value={task.priority}
        options={TASK_PRIORITIES}
        labels={PRIORITY_LABELS}
        onChange={(p) => onChangePriority(task, p)}
        ariaLabel={`任务 #${task.number} 优先级`}
        renderQuiet={(priority, label) => <PriorityMark priority={priority} label={label} />}
      />
      <QuietSelect
        value={task.assignee?.id ?? UNASSIGNED}
        options={assigneeOptions}
        labels={assigneeLabels}
        onChange={(id) => onChangeAssignee(task, id || null)}
        ariaLabel={`任务 #${task.number} 负责人`}
        renderQuiet={(id) => (id ? assigneeLabels[id] : '')}
      />
      <span className="task-due hint">{task.due_date ?? ''}</span>
      {error && <span className="error task-row-error">{error}</span>}
    </div>
  )
}
