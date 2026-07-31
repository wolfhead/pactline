// Contract types for the versioned /api/v1 work surface.

export type TaskStatus = 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled'
export type TaskPriority = 'none' | 'low' | 'medium' | 'high' | 'urgent'
export type TaskExecutionMode = 'human_only' | 'agent_allowed'
export type TaskClaimStatus = 'active' | 'waiting_human' | 'submitted' | 'released' | 'expired'

export const TASK_STATUSES: TaskStatus[] = ['todo', 'in_progress', 'in_review', 'done', 'cancelled']
export const TASK_PRIORITIES: TaskPriority[] = ['none', 'low', 'medium', 'high', 'urgent']
export const EXECUTION_MODE_LABELS: Record<TaskExecutionMode, string> = {
  human_only: '仅人工执行',
  agent_allowed: '允许 Agent 执行',
}

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

export interface TaskRelationRef {
  id: string
  number: number
  title: string
  status: TaskStatus
  archived: boolean
  milestone: TaskMilestoneRef | null
}

export interface TaskAgentWorkSummary {
  claim_id: string
  status: TaskClaimStatus
  token_name: string
  client_kind: string
  updated_at: string
  completed_at?: string
}

export interface Task {
  id: string
  number: number
  version: number
  title: string
  context: string
  expected_result: string
  description: string
  status: TaskStatus
  priority: TaskPriority
  execution_mode?: TaskExecutionMode
  agent_work?: TaskAgentWorkSummary | null
  assignee: UserRef | null
  creator: UserRef
  start_date: string | null
  due_date: string | null
  project: TaskProjectRef
  milestone: TaskMilestoneRef | null
  labels: Label[]
  parent: TaskRelationRef | null
  children: TaskRelationRef[]
  dependencies: TaskRelationRef[]
  dependents: TaskRelationRef[]
  blocked: boolean
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
  | 'context'
  | 'expected_result'
  | 'description'
  | 'status'
  | 'priority'
  | 'execution_mode'
  | 'assignee'
  | 'start_date'
  | 'due_date'
  | 'labels'
  | 'project'
  | 'milestone'
  | 'parent'
  | 'dependencies'
  | 'archived'

export interface Activity {
  id: string
  actor_id: string
  field: ActivityField
  old_value: string | null
  new_value: string | null
  authentication_method?: 'session' | 'api_token' | 'agent_delegate'
  token_name?: string
  request_id?: string
  created_at: string
}

export interface TaskPatchBody {
  title?: string
  context?: string
  expected_result?: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  execution_mode?: TaskExecutionMode
  // `null` clears (matches decodeTaskPatch's *Set-boolean semantics for a
  // present-but-null key); omitting the key entirely leaves it unchanged.
  assignee_id?: string | null
  start_date?: string | null
  due_date?: string | null
  label_ids?: string[]
  project_number?: number
  milestone_id?: string | null
  parent_number?: number | null
  dependency_numbers?: number[]
  schedule_shift_days?: number
}

export interface CreateTaskBody {
  title: string
  context: string
  expected_result: string
  description?: string
  status?: TaskStatus
  priority?: TaskPriority
  execution_mode?: TaskExecutionMode
  assignee_id?: string | null
  project_number: number
  milestone_id?: string | null
  parent_number?: number
  dependency_numbers?: number[]
  start_date?: string | null
  due_date?: string | null
  label_ids?: string[]
}

export interface TaskClaim {
  id: string
  task_id: string
  task_number: number
  claimed_by_user_id: string
  token_name: string
  client_kind: string
  status: TaskClaimStatus
  version: number
  expires_at: string
  terminal_reason?: string
  created_at: string
  updated_at: string
  completed_at?: string
}

export interface TaskClaimMessage {
  id: string
  claim_id: string
  task_id: string
  author_type: 'agent' | 'human'
  author_user_id?: string
  kind: 'progress' | 'question' | 'answer' | 'handoff' | 'submission'
  body: string
  reply_to_message_id?: string
  token_name?: string
  created_at: string
}

export interface TaskClaimConversation {
  claim: TaskClaim
  messages: TaskClaimMessage[]
}
