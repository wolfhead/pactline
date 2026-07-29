// Contract types for the versioned /api/v1 work surface.

export type TaskStatus = 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled'
export type TaskPriority = 'none' | 'low' | 'medium' | 'high' | 'urgent'

export const TASK_STATUSES: TaskStatus[] = ['todo', 'in_progress', 'in_review', 'done', 'cancelled']
export const TASK_PRIORITIES: TaskPriority[] = ['none', 'low', 'medium', 'high', 'urgent']

export const STATUS_LABELS: Record<TaskStatus, string> = {
  todo: '待办',
  in_progress: '进行中',
  in_review: '待评审',
  done: '已完成',
  cancelled: '已取消',
}

export const PRIORITY_LABELS: Record<TaskPriority, string> = {
  none: '无优先级',
  low: '低',
  medium: '中',
  high: '高',
  urgent: '紧急',
}

export interface UserRef {
  id: string
  name: string
  email: string | null
}

// See the file-level comment: this is domain.Label's real, untagged wire
// shape (ID/Name/CreatedAt), not id/name/created_at.
export interface Label {
  id: string
  version: number
  name: string
  created_at: string
}

export interface TaskProjectRef {
  id: string
  number: number
  name: string
}

export interface TaskMilestoneRef {
  id: string
  name: string
}

export interface Task {
  id: string
  number: number
  version: number
  title: string
  description: string
  status: TaskStatus
  priority: TaskPriority
  assignee: UserRef | null
  creator: UserRef
  due_date: string | null
  project: TaskProjectRef
  milestone: TaskMilestoneRef | null
  labels: Label[]
  created_at: string
  updated_at: string
  completed_at: string | null
  archived_at: string | null
}

export interface TaskListResponse {
  items: Task[]
  next_cursor?: string
  has_more: boolean
}

export interface Comment {
  id: string
  task_id: string
  author_id: string
  version: number
  body: string
  created_at: string
  updated_at: string
}

export type ActivityField =
  | 'created'
  | 'title'
  | 'description'
  | 'status'
  | 'priority'
  | 'assignee'
  | 'due_date'
  | 'labels'
  | 'project'
  | 'milestone'
  | 'archived'

export interface Activity {
  id: string
  actor_id: string
  field: ActivityField
  old_value: string | null
  new_value: string | null
  authentication_method?: 'session' | 'api_token'
  token_name?: string
  request_id?: string
  created_at: string
}

export interface TaskPatchBody {
  title?: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  // `null` clears (matches decodeTaskPatch's *Set-boolean semantics for a
  // present-but-null key); omitting the key entirely leaves it unchanged.
  assignee_id?: string | null
  due_date?: string | null
  label_ids?: string[]
  project_number?: number
  milestone_id?: string | null
}

export interface CreateTaskBody {
  title: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  assignee_id?: string | null
  project_number: number
  milestone_id?: string | null
  due_date?: string | null
  label_ids?: string[]
}
