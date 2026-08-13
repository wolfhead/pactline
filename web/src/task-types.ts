// Contract types for the versioned /api/v1 work surface.

export type TaskStatus = 'todo' | 'in_progress' | 'in_review' | 'done' | 'cancelled'
export type TaskPhase = 'backlog' | 'ready' | 'in_progress' | 'in_review' | 'done' | 'cancelled'
export type TaskActivityState = 'available' | 'working' | 'needs_resolution'
export type TaskPriority = 'none' | 'low' | 'medium' | 'high' | 'urgent'
export type TaskExecutionMode = 'human_only' | 'agent_allowed'

export const TASK_PHASES: TaskPhase[] = ['backlog', 'ready', 'in_progress', 'in_review', 'done', 'cancelled']
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

export const PHASE_LABELS: Record<TaskPhase, string> = {
  backlog: '待规划',
  ready: '可领取',
  in_progress: '执行中',
  in_review: '验收中',
  done: '已完成',
  cancelled: '已取消',
}

export const ACTIVITY_LABELS: Record<TaskActivityState, string> = {
  available: '等待领取',
  working: '正在处理',
  needs_resolution: '等待解决',
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
  phase: TaskPhase
  archived: boolean
  milestone: TaskMilestoneRef | null
}

export interface Task {
  id: string
  number: number
  version: number
  title: string
  context: string
  expected_result: string
  description: string
  phase: TaskPhase
  activity?: TaskActivityState | null
  review_cycle: number
  active_issue_thread_id?: string | null
  main_thread_id: string
  priority: TaskPriority
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

export type ActorType = 'user' | 'agent' | 'system'

export interface Actor {
  type: ActorType
  user_id?: string
  ref?: string
}

export interface TaskWorkflow {
  task_id: string
  task_number: number
  version: number
  phase: TaskPhase
  activity?: TaskActivityState | null
  review_cycle: number
  active_issue_thread_id?: string | null
  main_thread_id: string
}

export type TaskClaimStage = 'execution' | 'review'
export type TaskStageClaimStatus = 'active' | 'completed' | 'released' | 'expired' | 'cancelled'
export type TaskStageClaimOutcome =
  | 'work_submitted'
  | 'task_accepted'
  | 'changes_requested'
  | 'needs_resolution'
  | 'voluntarily_released'
  | 'deadline_elapsed'
  | 'task_cancelled'

export interface TaskStageClaim {
  id: string
  task_id: string
  task_number: number
  stage: TaskClaimStage
  claimed_by: Actor
  subject_user_id: string
  authentication_method: 'session' | 'api_token' | 'agent_delegate'
  token_name?: string
  client_kind: string
  status: TaskStageClaimStatus
  outcome?: TaskStageClaimOutcome
  version: number
  expires_at: string
  created_at: string
  updated_at: string
  completed_at?: string
}

export type IssueThreadType = 'decision_required' | 'dependency_required'
export type TaskThreadRole = 'main' | 'issue'
export type TaskThreadItemKind =
  | 'message'
  | 'progress'
  | 'handoff'
  | 'work_submission'
  | 'review_outcome'
  | 'resolution_request'
  | 'resolution'
  | 'issue_resolution'
  | 'system_event'

export interface TaskThread {
  id: string
  task_id: string
  role: TaskThreadRole
  issue_type?: IssueThreadType
  issue_status?: 'open' | 'resolved'
  opened_from_phase?: TaskPhase
  opened_by?: Actor
  resolved_by?: Actor
  version: number
  created_at: string
  updated_at: string
  resolved_at?: string
}

export interface IssueResolutionPayload {
  issue_thread_id: string
  issue_type: IssueThreadType
  request: string
  resolution: string
  opened_by: Actor
  resolved_by: Actor
  opened_at: string
  resolved_at: string
}

export interface TaskThreadItem {
  id: string
  thread_id: string
  kind: TaskThreadItemKind
  author: Actor
  body?: string
  issue_resolution?: IssueResolutionPayload
  reply_to_item_id?: string
  mentioned_user_ids: string[]
  version: number
  created_at: string
  updated_at: string
  deleted_at?: string
}

export interface TaskListResponse {
  items: Task[]
  next_cursor?: string
  has_more: boolean
}

export type AttachmentPreviewKind = 'image' | 'markdown' | 'html' | 'download'

export interface TaskAttachment {
  id: string
  task_id: string
  uploader_id: string
  filename: string
  media_type: string
  size_bytes: number
  preview_kind: AttachmentPreviewKind
  version: number
  content_url: string
  download_url: string
  created_at: string
  updated_at: string
}

export interface TaskAttachmentUpload {
  id: string
  provider: 'local' | 'oss' | 'cos'
  filename: string
  media_type: string
  size_bytes: number
  direct: boolean
  method: 'PUT'
  upload_url: string
  headers: Record<string, string>
  expires_at: string
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
  priority?: TaskPriority
  // `null` clears (matches decodeTaskPatch's *Set-boolean semantics for a
  // present-but-null key); omitting the key entirely leaves it unchanged.
  assignee_id?: string | null
  start_date?: string | null
  due_date?: string | null
  label_ids?: string[]
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
  priority?: TaskPriority
  assignee_id?: string | null
  project_number: number
  milestone_id?: string | null
  parent_number?: number
  dependency_numbers?: number[]
  start_date?: string | null
  due_date?: string | null
  label_ids?: string[]
}
