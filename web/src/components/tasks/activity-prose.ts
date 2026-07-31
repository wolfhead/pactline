import {
  EXECUTION_MODE_LABELS,
  PRIORITY_LABELS,
  STATUS_LABELS,
  type Activity,
  type TaskExecutionMode,
  type TaskPriority,
  type TaskStatus,
} from '../../task-types'

/**
 * Turns one activity_log row into a plain-language sentence. The store
 * (internal/store/task_store.go) writes old/new values as display-ready
 * text for task brief fields/due_date/labels, but as raw enum strings for
 * status/priority and as bare user-UUID strings for assignee — those three
 * need translating here, not shown as `todo` / `a1b2c3d4-...`.
 */
export function describeActivity(activity: Activity, actorName: string, userNameById: Record<string, string>): string {
  const who = actorName || '某用户'
  switch (activity.field) {
    case 'created': {
      const status = activity.new_value ? STATUS_LABELS[activity.new_value as TaskStatus] ?? activity.new_value : ''
      return `${who} 创建了任务${status ? `，初始状态为「${status}」` : ''}`
    }
    case 'title':
      return `${who} 将标题从「${activity.old_value ?? ''}」改为「${activity.new_value ?? ''}」`
    case 'context':
      return `${who} 更新了任务背景`
    case 'expected_result':
      return `${who} 更新了期望结果`
    case 'description':
      return `${who} 更新了描述`
    case 'status': {
      const from = activity.old_value ? STATUS_LABELS[activity.old_value as TaskStatus] ?? activity.old_value : '无'
      const to = activity.new_value ? STATUS_LABELS[activity.new_value as TaskStatus] ?? activity.new_value : '无'
      return `${who} 将状态从「${from}」改为「${to}」`
    }
    case 'priority': {
      const from = activity.old_value ? PRIORITY_LABELS[activity.old_value as TaskPriority] ?? activity.old_value : '无'
      const to = activity.new_value ? PRIORITY_LABELS[activity.new_value as TaskPriority] ?? activity.new_value : '无'
      return `${who} 将优先级从「${from}」改为「${to}」`
    }
    case 'execution_mode': {
      const from = activity.old_value
        ? EXECUTION_MODE_LABELS[activity.old_value as TaskExecutionMode] ?? activity.old_value
        : '仅人工执行'
      const to = activity.new_value
        ? EXECUTION_MODE_LABELS[activity.new_value as TaskExecutionMode] ?? activity.new_value
        : '仅人工执行'
      return `${who} 将执行方式从「${from}」改为「${to}」`
    }
    case 'assignee': {
      const from = activity.old_value ? (userNameById[activity.old_value] ?? '未知用户') : '未分配'
      const to = activity.new_value ? (userNameById[activity.new_value] ?? '未知用户') : '未分配'
      return `${who} 将负责人从「${from}」改为「${to}」`
    }
    case 'start_date':
      return `${who} 将开始日期从「${activity.old_value ?? '无'}」改为「${activity.new_value ?? '无'}」`
    case 'due_date':
      return `${who} 将截止日期从「${activity.old_value ?? '无'}」改为「${activity.new_value ?? '无'}」`
    case 'labels':
      return `${who} 将标签从「${activity.old_value || '无'}」改为「${activity.new_value || '无'}」`
    case 'project':
      return `${who} 将任务移动到其他项目`
    case 'milestone':
      return `${who} 调整了任务所属里程碑`
    case 'parent':
      return activity.new_value ? `${who} 设置了父任务` : `${who} 解除了父任务`
    case 'dependencies':
      return `${who} 更新了任务依赖`
    case 'archived':
      return activity.new_value === 'true' ? `${who} 归档了任务` : `${who} 恢复了任务`
    default:
      return `${who} 更新了 ${activity.field}`
  }
}
