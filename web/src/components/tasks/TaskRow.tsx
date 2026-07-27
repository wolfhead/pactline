import { Link } from 'react-router-dom'
import { useIdentity } from '../../identity'
import { PRIORITY_LABELS, STATUS_LABELS, TASK_PRIORITIES, TASK_STATUSES, type Task, type TaskPriority, type TaskStatus } from '../../task-types'
import PillSelect from './PillSelect'

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
 * assignee, due date), and the three fields that change dozens of times a
 * day are live controls, not a link into a form. */
export default function TaskRow({ task, focused, error, onChangeStatus, onChangePriority, onChangeAssignee }: TaskRowProps) {
  const { users } = useIdentity()

  return (
    <div className={`task-row ${focused ? 'focused' : ''}`} data-task-number={task.number}>
      <span className="task-number hint">#{task.number}</span>
      <Link className="task-title" to={`/tasks/${task.number}`}>{task.title}</Link>
      <PillSelect
        value={task.status}
        options={TASK_STATUSES}
        labels={STATUS_LABELS}
        onChange={(s) => onChangeStatus(task, s)}
        ariaLabel={`任务 #${task.number} 状态`}
      />
      <PillSelect
        value={task.priority}
        options={TASK_PRIORITIES}
        labels={PRIORITY_LABELS}
        onChange={(p) => onChangePriority(task, p)}
        ariaLabel={`任务 #${task.number} 优先级`}
      />
      <select
        className="pill-select"
        value={task.assignee?.id ?? UNASSIGNED}
        aria-label={`任务 #${task.number} 负责人`}
        onChange={(e) => onChangeAssignee(task, e.target.value || null)}
      >
        <option value={UNASSIGNED}>未分配</option>
        {users.map((u) => (
          <option key={u.id} value={u.id}>{u.name}</option>
        ))}
      </select>
      <span className="task-due hint">{task.due_date ?? '—'}</span>
      {error && <span className="error task-row-error">{error}</span>}
    </div>
  )
}
