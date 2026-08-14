import { etagForVersion, requireVersioned, v1Delete, v1Get, v1Patch, v1Post } from './v1/client'
import type {
  IssueThreadType,
  TaskStageClaim,
  TaskThread,
  TaskThreadItem,
  TaskWorkflow,
} from '@/task-types'

export interface ClaimCommandResult {
  task: TaskWorkflow
  claim: TaskStageClaim
}

export function markTaskReady(number: number, version: number): Promise<TaskWorkflow> {
  return v1Post<TaskWorkflow>(`/api/v1/tasks/${number}/commands/mark-ready`, {
    ifMatch: etagForVersion(version),
  }).then(({ value }) => value)
}

export function withdrawTaskReadiness(
  number: number,
  version: number,
  reason: string,
): Promise<TaskWorkflow> {
  return v1Post<TaskWorkflow>(`/api/v1/tasks/${number}/commands/withdraw-readiness`, {
    ifMatch: etagForVersion(version), body: { reason },
  }).then(({ value }) => value)
}

export function listTaskStageClaims(number: number): Promise<TaskStageClaim[]> {
  return v1Get<{ items: TaskStageClaim[] }>(`/api/v1/tasks/${number}/claims`)
    .then(({ value }) => value.items)
}

export function claimTaskStage(number: number, version: number): Promise<ClaimCommandResult> {
  return v1Post<ClaimCommandResult>(`/api/v1/tasks/${number}/claims`, {
    ifMatch: etagForVersion(version),
  }).then(({ value }) => value)
}

function finishClaim(
  number: number,
  taskVersion: number,
  claim: TaskStageClaim,
  command: 'release' | 'request-changes' | 'accept' | 'complete-execution',
  body: string,
): Promise<ClaimCommandResult> {
  return v1Post<ClaimCommandResult>(
    `/api/v1/claims/${claim.id}/${command}`,
    { ifMatch: etagForVersion(taskVersion), body: { body } },
  ).then(({ value }) => value)
}

export function releaseTaskStage(number: number, taskVersion: number, claim: TaskStageClaim, body: string) {
  return finishClaim(number, taskVersion, claim, 'release', body)
}

export function recordTaskWorkSubmission(
  number: number,
  taskVersion: number,
  claim: TaskStageClaim,
  body: string,
): Promise<ClaimCommandResult & { submission: TaskThreadItem }> {
  return v1Post<ClaimCommandResult & { submission: TaskThreadItem }>(
    `/api/v1/claims/${claim.id}/submissions`,
    { ifMatch: etagForVersion(taskVersion), body: { body } },
  ).then(({ value }) => value)
}

export function completeTaskExecution(
  number: number,
  taskVersion: number,
  claim: TaskStageClaim,
  body: string,
): Promise<ClaimCommandResult & { completion: TaskThreadItem }> {
  return v1Post<ClaimCommandResult & { completion: TaskThreadItem }>(
    `/api/v1/claims/${claim.id}/complete-execution`,
    { ifMatch: etagForVersion(taskVersion), body: { body } },
  ).then(({ value }) => value)
}

export function requestTaskChanges(number: number, taskVersion: number, claim: TaskStageClaim, body: string) {
  return finishClaim(number, taskVersion, claim, 'request-changes', body)
}

export function acceptTask(number: number, taskVersion: number, claim: TaskStageClaim, body: string) {
  return finishClaim(number, taskVersion, claim, 'accept', body)
}

export function requestTaskResolution(
  number: number,
  taskVersion: number,
  claim: TaskStageClaim,
  issueType: IssueThreadType,
  request: string,
): Promise<{ task: TaskWorkflow; claim: TaskStageClaim; issue: TaskThread }> {
  return v1Post(`/api/v1/claims/${claim.id}/request-resolution`, {
    ifMatch: etagForVersion(taskVersion),
    body: { issue_type: issueType, request },
  }).then(({ value }) => value as { task: TaskWorkflow; claim: TaskStageClaim; issue: TaskThread })
}

export function resolveTaskIssue(
  number: number,
  taskVersion: number,
  issue: TaskThread,
  resolution: string,
): Promise<{ task: TaskWorkflow; issue: TaskThread }> {
  return v1Post(`/api/v1/tasks/${number}/issues/${issue.id}/resolve`, {
    ifMatch: etagForVersion(taskVersion),
    body: { thread_version: issue.version, resolution },
  }).then(({ value }) => value as { task: TaskWorkflow; issue: TaskThread })
}

export function cancelTask(number: number, version: number, reason: string): Promise<TaskWorkflow> {
  return v1Post<{ task: TaskWorkflow }>(`/api/v1/tasks/${number}/commands/cancel`, {
    ifMatch: etagForVersion(version), body: { reason },
  }).then(({ value }) => value.task)
}

export function listTaskThreads(number: number): Promise<TaskThread[]> {
  return v1Get<{ items: TaskThread[] }>(`/api/v1/tasks/${number}/threads`)
    .then(({ value }) => value.items)
}

export async function listThreadItems(threadID: string): Promise<TaskThreadItem[]> {
  const items: TaskThreadItem[] = []
  let cursor = ''
  do {
    const query = new URLSearchParams({ limit: '100' })
    if (cursor) query.set('cursor', cursor)
    const { value } = await v1Get<{ items: TaskThreadItem[]; next_cursor?: string }>(
      `/api/v1/threads/${threadID}/items?${query}`,
    )
    items.push(...value.items)
    cursor = value.next_cursor ?? ''
  } while (cursor)
  return items
}

export function createThreadMessage(
  threadID: string,
  body: string,
  kind: 'message' | 'progress' = 'message',
  mentionedUserIDs: string[] = [],
): Promise<TaskThreadItem> {
  return v1Post<TaskThreadItem>(`/api/v1/threads/${threadID}/items`, {
    body: { kind, body, mentioned_user_ids: mentionedUserIDs },
  }).then(({ value }) => value)
}

export function updateThreadMessage(item: TaskThreadItem, body: string): Promise<TaskThreadItem> {
  return v1Patch<TaskThreadItem>(`/api/v1/thread-items/${item.id}`, {
    ifMatch: etagForVersion(item.version),
    body: { body, mentioned_user_ids: item.mentioned_user_ids },
  }).then((response) => requireVersioned(response).value)
}

export function deleteThreadMessage(item: TaskThreadItem): Promise<TaskThreadItem> {
  return v1Delete<TaskThreadItem>(`/api/v1/thread-items/${item.id}`, {
    ifMatch: etagForVersion(item.version),
  }).then(({ value }) => value)
}
