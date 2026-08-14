import { etagForVersion, requireVersioned, v1Delete, v1Get, v1Post } from './v1/client'
import type { Actor, TaskStageClaim, TaskWorkflow } from '@/task-types'

export type GitLabObservationStatus =
  | 'confirmed'
  | 'missing'
  | 'unauthorized'
  | 'unreachable'
  | 'disconnected'
export type GitLabMergeRequestState = 'opened' | 'closed' | 'merged' | 'locked'

export interface GitLabMergeRequestObservation {
  status: GitLabObservationStatus
  observed_at: string
  title: string
  state: GitLabMergeRequestState
  draft: boolean
  source_branch: string
  target_branch: string
  head_sha: string
  merge_commit_sha?: string
  merged_at?: string
  provider_updated_at: string
}

export interface TaskMergeRequest {
  id: string
  project_repository_id: string
  repository_url: string
  merge_request_iid: number
  gitlab_merge_request_id: number
  web_url: string
  linked_by: Actor
  linked_through_claim_id: string
  linked_at: string
  latest_observation: GitLabMergeRequestObservation
}

export interface MergeRequestSnapshot {
  task_merge_request_id: string
  project_repository_id: string
  connection_id: string
  gitlab_project_id: number
  merge_request_iid: number
  web_url: string
  title: string
  state: GitLabMergeRequestState
  draft: boolean
  source_branch: string
  target_branch: string
  head_sha: string
  merge_commit_sha?: string
  merged_at?: string
  observation_status: GitLabObservationStatus
  observed_at: string
}

export type DeliveryComparison =
  | 'unchanged'
  | 'moved'
  | 'merged'
  | 'missing'
  | 'unauthorized'
  | 'unreachable'
  | 'disconnected'

export interface TaskDeliveryComparison {
  snapshot: MergeRequestSnapshot
  current?: TaskMergeRequest
  comparison: DeliveryComparison
}

export interface TaskDelivery {
  active_links: TaskMergeRequest[]
  review?: { review_cycle: number; merge_requests: TaskDeliveryComparison[] }
}

export interface TaskMergeRequestMutation {
  task: TaskWorkflow
  merge_request: TaskMergeRequest
}

export function getTaskDelivery(taskNumber: number): Promise<TaskDelivery> {
  return v1Get<TaskDelivery>(`/api/v1/tasks/${taskNumber}/merge-requests`)
    .then(({ value }) => value)
}

export function linkTaskMergeRequest(
  taskNumber: number,
  taskVersion: number,
  claim: TaskStageClaim,
  mergeRequestURL: string,
): Promise<TaskMergeRequestMutation> {
  return v1Post<TaskMergeRequestMutation>(
    `/api/v1/claims/${claim.id}/merge-requests`,
    {
      ifMatch: etagForVersion(taskVersion),
      body: { merge_request_url: mergeRequestURL },
    },
  ).then((response) => requireVersioned(response).value)
}

export function unlinkTaskMergeRequest(
  taskNumber: number,
  taskVersion: number,
  claim: TaskStageClaim,
  linkID: string,
): Promise<TaskMergeRequestMutation> {
  return v1Delete<TaskMergeRequestMutation>(
    `/api/v1/claims/${claim.id}/merge-requests/${linkID}`,
    {
      ifMatch: etagForVersion(taskVersion),
    },
  ).then((response) => requireVersioned(response).value)
}
